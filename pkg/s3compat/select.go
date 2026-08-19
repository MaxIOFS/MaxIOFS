package s3compat

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

// reS3Object matches the literal "S3Object" (whole-word, case-insensitive) in
// a SQL expression so it can be replaced with the internal SQLite table name.
var reS3Object = regexp.MustCompile(`(?i)\bS3Object\b`)

// ============================================================================
// Request XML types
// ============================================================================

type selectObjectContentRequest struct {
	Expression          string             `xml:"Expression"`
	ExpressionType      string             `xml:"ExpressionType"`
	InputSerialization  selectInputSerial  `xml:"InputSerialization"`
	OutputSerialization selectOutputSerial `xml:"OutputSerialization"`
}

type selectInputSerial struct {
	CompressionType string        `xml:"CompressionType"`
	CSV             *selectCSVIn  `xml:"CSV"`
	JSON            *selectJSONIn `xml:"JSON"`
}

type selectCSVIn struct {
	FileHeaderInfo       string `xml:"FileHeaderInfo"`       // NONE | IGNORE | USE
	RecordDelimiter      string `xml:"RecordDelimiter"`      // default \n
	FieldDelimiter       string `xml:"FieldDelimiter"`       // default ,
	QuoteCharacter       string `xml:"QuoteCharacter"`       // default "
	QuoteEscapeCharacter string `xml:"QuoteEscapeCharacter"` // default "
	Comments             string `xml:"Comments"`             // comment prefix char
}

type selectJSONIn struct {
	Type string `xml:"Type"` // DOCUMENT | LINES
}

type selectOutputSerial struct {
	CSV  *selectCSVOut  `xml:"CSV"`
	JSON *selectJSONOut `xml:"JSON"`
}

type selectCSVOut struct {
	RecordDelimiter      string `xml:"RecordDelimiter"`
	FieldDelimiter       string `xml:"FieldDelimiter"`
	QuoteCharacter       string `xml:"QuoteCharacter"`
	QuoteEscapeCharacter string `xml:"QuoteEscapeCharacter"`
	QuoteFields          string `xml:"QuoteFields"` // ALWAYS | ASNEEDED
}

type selectJSONOut struct {
	RecordDelimiter string `xml:"RecordDelimiter"`
}

// ============================================================================

func putUint32BE(b *bytes.Buffer, v uint32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	b.Write(buf[:])
}

func encodeEventHeaders(headers [][2]string) []byte {
	var b bytes.Buffer
	for _, h := range headers {
		name, value := h[0], h[1]
		b.WriteByte(byte(len(name)))
		b.WriteString(name)
		b.WriteByte(7) // string type
		vlen := uint16(len(value))
		b.WriteByte(byte(vlen >> 8))
		b.WriteByte(byte(vlen))
		b.WriteString(value)
	}
	return b.Bytes()
}

// writeEventMessage writes one event-stream message to w.
func writeEventMessage(w io.Writer, headers [][2]string, payload []byte) error {
	hBytes := encodeEventHeaders(headers)

	totalLen := uint32(4 + 4 + 4 + len(hBytes) + len(payload) + 4)

	var msg bytes.Buffer
	msg.Grow(int(totalLen))

	putUint32BE(&msg, totalLen)
	putUint32BE(&msg, uint32(len(hBytes)))

	preludeCRC := crc32.ChecksumIEEE(msg.Bytes())
	putUint32BE(&msg, preludeCRC)

	msg.Write(hBytes)
	msg.Write(payload)

	msgCRC := crc32.ChecksumIEEE(msg.Bytes())
	putUint32BE(&msg, msgCRC)

	_, err := w.Write(msg.Bytes())
	return err
}

func writeRecordsEvent(w io.Writer, data []byte) error {
	return writeEventMessage(w, [][2]string{
		{":message-type", "event"},
		{":event-type", "Records"},
		{":content-type", "application/octet-stream"},
	}, data)
}

func writeStatsEvent(w io.Writer, scanned, processed, returned int64) error {
	payload := fmt.Sprintf(
		"<Stats><BytesScanned>%d</BytesScanned><BytesProcessed>%d</BytesProcessed><BytesReturned>%d</BytesReturned></Stats>",
		scanned, processed, returned,
	)
	return writeEventMessage(w, [][2]string{
		{":message-type", "event"},
		{":event-type", "Stats"},
		{":content-type", "application/xml"},
	}, []byte(payload))
}

func writeEndEvent(w io.Writer) error {
	return writeEventMessage(w, [][2]string{
		{":message-type", "event"},
		{":event-type", "End"},
	}, nil)
}

func writeSelectErrorEvent(w io.Writer, code, message string) error {
	return writeEventMessage(w, [][2]string{
		{":message-type", "error"},
		{":error-code", code},
		{":error-message", message},
	}, nil)
}

// ============================================================================
// SQLite helpers
// ============================================================================

func quoteColIdent(s string) string {
	// SQLite double-quoted identifier; escape embedded double-quotes.
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// createSelectTable creates a table named "s3object" in db with TEXT columns.
func createSelectTable(db *sql.DB, cols []string) error {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = quoteColIdent(c) + " TEXT"
	}
	_, err := db.Exec(`CREATE TABLE s3object (` + strings.Join(parts, ", ") + `)`)
	return err
}

// rowWriter inserts rows into s3object as they are read, inside one
// transaction, so the loaders never hold the object in memory. Columns can
// appear mid-stream (JSON lines have no fixed schema), and the table grows to
// match rather than the input being buffered to discover its shape first.
type rowWriter struct {
	db    *sql.DB
	tx    *sql.Tx
	cols  []string
	index map[string]int
	stmt  *sql.Stmt
}

func newRowWriter(db *sql.DB) *rowWriter {
	return &rowWriter{db: db, index: map[string]int{}}
}

// ensureColumns extends the table so every named column exists.
func (rw *rowWriter) ensureColumns(names []string) error {
	var fresh []string
	for _, name := range names {
		if _, ok := rw.index[name]; ok {
			continue
		}
		rw.index[name] = len(rw.cols)
		rw.cols = append(rw.cols, name)
		fresh = append(fresh, name)
	}
	if len(fresh) == 0 {
		return nil
	}

	if rw.tx == nil {
		if err := createSelectTable(rw.db, rw.cols); err != nil {
			return err
		}
		tx, err := rw.db.Begin()
		if err != nil {
			return err
		}
		rw.tx = tx
	} else {
		for _, name := range fresh {
			if _, err := rw.tx.Exec(`ALTER TABLE s3object ADD COLUMN ` + quoteColIdent(name) + ` TEXT`); err != nil {
				return err
			}
		}
	}

	// The prepared insert names a fixed column list, so it is rebuilt.
	if rw.stmt != nil {
		_ = rw.stmt.Close()
		rw.stmt = nil
	}
	return nil
}

func (rw *rowWriter) prepare() error {
	if rw.stmt != nil {
		return nil
	}
	quoted := make([]string, len(rw.cols))
	for i, c := range rw.cols {
		quoted[i] = quoteColIdent(c)
	}
	placeholders := "(" + strings.TrimPrefix(strings.Repeat(",?", len(rw.cols)), ",") + ")"
	stmt, err := rw.tx.Prepare(fmt.Sprintf("INSERT INTO s3object (%s) VALUES %s",
		strings.Join(quoted, ","), placeholders))
	if err != nil {
		return err
	}
	rw.stmt = stmt
	return nil
}

// writeValues inserts one row, positionally against the current columns.
func (rw *rowWriter) writeValues(values []string) error {
	if len(rw.cols) == 0 {
		return nil
	}
	if err := rw.prepare(); err != nil {
		return err
	}
	vals := make([]interface{}, len(rw.cols))
	for i := range rw.cols {
		if i < len(values) {
			vals[i] = values[i]
		}
	}
	_, err := rw.stmt.Exec(vals...)
	return err
}

// writeNamed inserts one row given values by column name.
func (rw *rowWriter) writeNamed(values map[string]string) error {
	row := make([]string, len(rw.cols))
	for name, v := range values {
		if i, ok := rw.index[name]; ok {
			row[i] = v
		}
	}
	return rw.writeValues(row)
}

func (rw *rowWriter) commit() error {
	if rw.stmt != nil {
		_ = rw.stmt.Close()
		rw.stmt = nil
	}
	if rw.tx == nil {
		return nil
	}
	tx := rw.tx
	rw.tx = nil
	return tx.Commit()
}

func (rw *rowWriter) rollback() {
	if rw.stmt != nil {
		_ = rw.stmt.Close()
		rw.stmt = nil
	}
	if rw.tx != nil {
		_ = rw.tx.Rollback()
		rw.tx = nil
	}
}

// countingReader wraps an io.Reader and counts total bytes read.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// loadCSV reads CSV data from r into the s3object SQLite table.
// Returns column names and total bytes read.
func loadCSV(db *sql.DB, r io.Reader, cfg *selectCSVIn) (cols []string, scanned int64, err error) {
	counter := &countingReader{r: r}
	cr := csv.NewReader(counter)
	cr.LazyQuotes = true
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = -1
	cr.ReuseRecord = true // the record is copied into the insert before the next read

	if cfg != nil && cfg.FieldDelimiter != "" {
		cr.Comma = rune(cfg.FieldDelimiter[0])
	}

	headerInfo := "NONE"
	if cfg != nil && cfg.FileHeaderInfo != "" {
		headerInfo = strings.ToUpper(cfg.FileHeaderInfo)
	}

	writer := newRowWriter(db)
	defer writer.rollback()

	first, err := cr.Read()
	if err == io.EOF {
		return nil, counter.n, nil
	}
	if err != nil {
		return nil, counter.n, fmt.Errorf("parsing CSV: %w", err)
	}

	switch headerInfo {
	case "USE":
		cols = append([]string(nil), first...)
	default: // IGNORE and NONE both use positional names
		cols = make([]string, len(first))
		for i := range cols {
			cols[i] = fmt.Sprintf("_%d", i+1)
		}
	}
	if len(cols) == 0 {
		return nil, counter.n, nil
	}
	if err := writer.ensureColumns(cols); err != nil {
		return nil, counter.n, err
	}

	// NONE keeps the first record as data; USE and IGNORE consume it as a header.
	if headerInfo != "USE" && headerInfo != "IGNORE" {
		if err := writer.writeValues(first); err != nil {
			return nil, counter.n, err
		}
	}

	for {
		record, readErr := cr.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, counter.n, fmt.Errorf("parsing CSV: %w", readErr)
		}
		if err := writer.writeValues(record); err != nil {
			return nil, counter.n, err
		}
	}

	if err := writer.commit(); err != nil {
		return nil, counter.n, err
	}
	return cols, counter.n, nil
}

// loadJSONLines reads newline-delimited JSON objects from r into s3object.
// The schema is discovered as the stream goes: a key that first appears on a
// later record adds a column, so the input never has to be buffered to learn
// its shape.
func loadJSONLines(db *sql.DB, r io.Reader) (cols []string, scanned int64, err error) {
	counter := &countingReader{r: r}
	dec := json.NewDecoder(counter)

	writer := newRowWriter(db)
	defer writer.rollback()

	for {
		var obj map[string]interface{}
		if decErr := dec.Decode(&obj); decErr != nil {
			if decErr == io.EOF {
				break
			}
			return nil, counter.n, fmt.Errorf("parsing JSON lines: %w", decErr)
		}

		names := make([]string, 0, len(obj))
		for k := range obj {
			names = append(names, k)
		}
		sort.Strings(names) // a map iterates at random; columns must not
		if err := writer.ensureColumns(names); err != nil {
			return nil, counter.n, err
		}

		values := make(map[string]string, len(obj))
		for k, v := range obj {
			values[k] = jsonFieldToText(v)
		}
		if err := writer.writeNamed(values); err != nil {
			return nil, counter.n, err
		}
	}

	if err := writer.commit(); err != nil {
		return nil, counter.n, err
	}
	return writer.cols, counter.n, nil
}

// jsonFieldToText renders a JSON value as the text the query engine stores.
func jsonFieldToText(v interface{}) string {
	switch vv := v.(type) {
	case string:
		return vv
	case nil:
		return ""
	default:
		b, _ := json.Marshal(vv)
		return string(b)
	}
}

// streamSelectResults executes the SQL expression, writes Records events to w,
// and returns the total bytes returned.
func streamSelectResults(w io.Writer, db *sql.DB, expr string, out selectOutputSerial, flusher http.Flusher) (int64, error) {
	query := reS3Object.ReplaceAllString(expr, "s3object")

	sqlRows, err := db.Query(query)
	if err != nil {
		return 0, fmt.Errorf("query error: %w", err)
	}
	defer sqlRows.Close()

	resCols, err := sqlRows.Columns()
	if err != nil {
		return 0, err
	}

	useJSON := out.JSON != nil

	// Set up CSV writer once, writing into a shared record buffer.
	var recBuf bytes.Buffer
	var csvWriter *csv.Writer
	if !useJSON {
		csvWriter = csv.NewWriter(&recBuf)
		if out.CSV != nil && out.CSV.FieldDelimiter != "" {
			csvWriter.Comma = rune(out.CSV.FieldDelimiter[0])
		}
	}

	var totalReturned int64
	rowCount := 0
	const batchRows = 1000

	vals := make([]interface{}, len(resCols))
	ptrs := make([]interface{}, len(resCols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}

	flush := func() error {
		if recBuf.Len() == 0 {
			return nil
		}
		data := recBuf.Bytes()
		totalReturned += int64(len(data))
		if err := writeRecordsEvent(w, data); err != nil {
			return err
		}
		if flusher != nil {
			flusher.Flush()
		}
		recBuf.Reset()
		return nil
	}

	for sqlRows.Next() {
		if err := sqlRows.Scan(ptrs...); err != nil {
			return totalReturned, err
		}

		// Convert scanned values to strings.
		strs := make([]string, len(resCols))
		for i, v := range vals {
			switch vv := v.(type) {
			case nil:
				strs[i] = ""
			case []byte:
				strs[i] = string(vv)
			default:
				strs[i] = fmt.Sprintf("%v", vv)
			}
		}

		if useJSON {
			// Write JSON object preserving SELECT column order.
			var sb strings.Builder
			sb.WriteByte('{')
			for i, col := range resCols {
				if i > 0 {
					sb.WriteByte(',')
				}
				keyJSON, _ := json.Marshal(col)
				valJSON, _ := json.Marshal(strs[i])
				sb.Write(keyJSON)
				sb.WriteByte(':')
				sb.Write(valJSON)
			}
			sb.WriteByte('}')
			recBuf.WriteString(sb.String())
			recBuf.WriteByte('\n')
		} else {
			_ = csvWriter.Write(strs)
			csvWriter.Flush()
		}

		rowCount++
		if rowCount >= batchRows {
			if err := flush(); err != nil {
				return totalReturned, err
			}
			rowCount = 0
		}
	}

	if err := sqlRows.Err(); err != nil {
		return totalReturned, err
	}

	return totalReturned, flush()
}

// SelectObjectContent handles POST /{bucket}/{object}?select&select-type=2.
func (h *Handler) SelectObjectContent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := getObjectKey(r)

	addS3CompatHeaders(w)

	if !h.requireObjectS3Action(w, r, bucketName, objectKey, auth.ActionGetObject) {
		return
	}

	bucketPath := h.resolveBucketPath(r, bucketName, "")

	// ── Parse request ────────────────────────────────────────────────────────

	var req selectObjectContentRequest
	if err := decodeS3ControlXML(w, r, &req); err != nil {
		h.writeError(w, "MalformedXML", "The XML you provided was not well-formed", bucketName, r)
		return
	}

	if strings.ToUpper(req.ExpressionType) != "SQL" {
		h.writeError(w, "InvalidRequest", "ExpressionType must be SQL", bucketName, r)
		return
	}
	if strings.TrimSpace(req.Expression) == "" {
		h.writeError(w, "InvalidRequest", "Expression is required", bucketName, r)
		return
	}
	if ct := strings.ToUpper(req.InputSerialization.CompressionType); ct != "" && ct != "NONE" {
		h.writeError(w, "InvalidRequest",
			"CompressionType "+req.InputSerialization.CompressionType+" is not supported", bucketName, r)
		return
	}
	if req.InputSerialization.CSV == nil && req.InputSerialization.JSON == nil {
		h.writeError(w, "InvalidRequest", "InputSerialization must specify CSV or JSON", bucketName, r)
		return
	}
	if req.InputSerialization.JSON != nil {
		jsonType := strings.ToUpper(req.InputSerialization.JSON.Type)
		if jsonType != "" && jsonType != "LINES" {
			h.writeError(w, "InvalidRequest", "JSON InputSerialization Type must be LINES", bucketName, r)
			return
		}
	}
	if req.OutputSerialization.CSV == nil && req.OutputSerialization.JSON == nil {
		h.writeError(w, "InvalidRequest", "OutputSerialization must specify CSV or JSON", bucketName, r)
		return
	}

	// ── Fetch object data ────────────────────────────────────────────────────

	_, reader, err := h.objectManager.GetObject(r.Context(), bucketPath, objectKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}
	defer reader.Close()

	// ── Load into in-memory SQLite ───────────────────────────────────────────

	// The engine loads the object into a database before querying it, so that
	// database is backed by a temporary file rather than by RAM: a large object
	// costs disk, which is bounded and reclaimed, instead of memory the node
	// shares with every other request.
	scratch, scratchErr := os.CreateTemp("", "maxiofs-select-*.db")
	if scratchErr != nil {
		h.writeError(w, "InternalError", "failed to initialise query engine", objectKey, r)
		return
	}
	scratchPath := scratch.Name()
	_ = scratch.Close()
	defer os.Remove(scratchPath)

	db, dbErr := sql.Open("sqlite", scratchPath+"?_pragma=journal_mode(OFF)&_pragma=synchronous(OFF)")
	if dbErr != nil {
		h.writeError(w, "InternalError", "failed to initialise query engine", objectKey, r)
		return
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // all operations must share one connection

	var bytesScanned int64
	if req.InputSerialization.CSV != nil {
		_, bytesScanned, err = loadCSV(db, reader, req.InputSerialization.CSV)
	} else {
		_, bytesScanned, err = loadJSONLines(db, reader)
	}

	if err != nil {
		logrus.WithFields(logrus.Fields{
			"bucket": bucketName,
			"key":    objectKey,
		}).WithError(err).Warn("SelectObjectContent: failed to load data")
		h.writeError(w, "InvalidRequest", "Failed to parse input: "+err.Error(), objectKey, r)
		return
	}

	// ── Stream event-stream response ─────────────────────────────────────────

	w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)

	bytesReturned, queryErr := streamSelectResults(w, db, req.Expression, req.OutputSerialization, flusher)
	if queryErr != nil {
		logrus.WithFields(logrus.Fields{
			"bucket": bucketName,
			"key":    objectKey,
			"expr":   req.Expression,
		}).WithError(queryErr).Warn("SelectObjectContent: query failed")
		// Headers already sent; write an error event instead of changing status.
		_ = writeSelectErrorEvent(w, "QueryFailed", queryErr.Error())
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	_ = writeStatsEvent(w, bytesScanned, bytesScanned, bytesReturned)
	_ = writeEndEvent(w)
	if flusher != nil {
		flusher.Flush()
	}
}
