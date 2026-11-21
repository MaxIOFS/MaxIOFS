package s3compat

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"errors"
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
	sqlite "modernc.org/sqlite"
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

// maxSelectRecord is the longest record S3 Select reads, 1 MB as in AWS: a
// record is held whole in memory while it is read.
var maxSelectRecord int64 = 1 << 20

// selectRecordSlack is how far the readers of the loaders read ahead of the
// record they parse.
const selectRecordSlack = 64 << 10

var errSelectRecordTooLarge = errors.New("a record is longer than the 1,048,576 characters S3 Select reads")

// recordGuard counts what a loader reads and holds each record to
// maxSelectRecord: the loader tells it where each record it read ends, and it
// gives nothing more once the record being read is longer than that.
type recordGuard struct {
	r     io.Reader
	n     int64
	start int64
}

func (g *recordGuard) Read(p []byte) (int, error) {
	if g.n-g.start > maxSelectRecord+selectRecordSlack {
		return 0, errSelectRecordTooLarge
	}
	n, err := g.r.Read(p)
	g.n += int64(n)
	return n, err
}

// recordRead takes the record that ends at offset as read, and refuses it when
// it is longer than a record may be.
func (g *recordGuard) recordRead(offset int64) error {
	length := offset - g.start
	g.start = offset
	if length > maxSelectRecord {
		return errSelectRecordTooLarge
	}
	return nil
}

// loadCSV reads CSV data from r into the s3object SQLite table.
// Returns column names and total bytes read.
func loadCSV(db *sql.DB, r io.Reader, cfg *selectCSVIn) (cols []string, scanned int64, err error) {
	counter := &recordGuard{r: r}
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
	if err == nil {
		err = counter.recordRead(cr.InputOffset())
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
		if readErr == nil {
			readErr = counter.recordRead(cr.InputOffset())
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
	counter := &recordGuard{r: r}
	dec := json.NewDecoder(counter)

	writer := newRowWriter(db)
	defer writer.rollback()

	for {
		var obj map[string]interface{}
		decErr := dec.Decode(&obj)
		if decErr == io.EOF {
			break
		}
		if decErr == nil {
			decErr = counter.recordRead(dec.InputOffset())
		}
		if decErr != nil {
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

// selectOnly lets the client's expression read and compute, and nothing else.
// SQLite runs every statement of it: without this, one ATTACH reads any
// database file the server can read, and VACUUM INTO writes a file anywhere.
func selectOnly(action sqlite.AuthorizerActionCode, _, _, _, _ string) sqlite.AuthorizerReturnCode {
	switch action {
	case sqlite.AuthSelect, sqlite.AuthRead, sqlite.AuthFunction:
		return sqlite.AuthorizerOK
	}
	return sqlite.AuthorizerDeny
}

// streamSelectResults executes the SQL expression, writes Records events to w,
// and returns the total bytes returned. The query stops with ctx.
func streamSelectResults(ctx context.Context, w io.Writer, db *sql.DB, expr string, out selectOutputSerial, flusher http.Flusher) (int64, error) {
	query := reS3Object.ReplaceAllString(expr, "s3object")

	conn, err := db.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if err := conn.Raw(func(driverConn any) error {
		registerer, ok := driverConn.(sqlite.AuthorizerRegisterer)
		if !ok {
			return fmt.Errorf("the query engine cannot be limited to SELECT")
		}
		return registerer.RegisterAuthorizer(selectOnly)
	}); err != nil {
		return 0, err
	}

	sqlRows, err := conn.QueryContext(ctx, query)
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
	if !selectCompressionSupported(req.InputSerialization.CompressionType) {
		h.writeError(w, "InvalidRequest",
			"CompressionType "+req.InputSerialization.CompressionType+" is not supported: it is NONE, GZIP or BZIP2", bucketName, r)
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

	customerKey, ok := h.customerKey(w, r, objectKeyHeaders, objectKey)
	if !ok {
		return
	}
	selected, reader, err := h.objectManager.GetObject(object.WithReadKey(r.Context(), customerKey), bucketPath, objectKey)
	if err != nil {
		if err == object.ErrObjectNotFound {
			h.writeError(w, "NoSuchKey", "The specified key does not exist", objectKey, r)
			return
		}
		if h.writeCustomerKeyError(w, r, objectKey, err) || h.writeDataUnavailable(w, r, objectKey, err) {
			return
		}
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}
	defer reader.Close()
	if h.writeCustomerKeyError(w, r, objectKey, object.CheckCustomerKey(selected, customerKey)) {
		return
	}

	// ── Load into in-memory SQLite ───────────────────────────────────────────

	// The engine loads the object into a database before querying it, so that
	// database is backed by a temporary file rather than by RAM: a large object
	// costs disk, which is bounded and reclaimed, instead of memory the node
	// shares with every other request. It goes in the storage root, as an
	// upload's temporary file does and named like one: the system's /tmp may be
	// RAM, every walk of the root skips the name and a restart removes it.
	scratch, scratchErr := os.CreateTemp(h.dataDir, "maxiofs-upload-select-*.db")
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

	source := &selectSource{r: reader}
	input, err := selectInput(req.InputSerialization.CompressionType, source)
	if errors.Is(err, errSelectCompression) {
		h.writeError(w, "InvalidCompressionFormat",
			"The file is not in a supported compression format. Only GZIP and BZIP2 are supported.", objectKey, r)
		return
	}
	if err != nil {
		h.writeError(w, "InternalError", err.Error(), objectKey, r)
		return
	}

	// BytesScanned is what was read of the stored object, BytesProcessed what
	// the query read once it was decompressed.
	var bytesProcessed int64
	if req.InputSerialization.CSV != nil {
		_, bytesProcessed, err = loadCSV(db, input, req.InputSerialization.CSV)
	} else {
		_, bytesProcessed, err = loadJSONLines(db, input)
	}

	if err != nil {
		logrus.WithFields(logrus.Fields{
			"bucket": bucketName,
			"key":    objectKey,
		}).WithError(err).Warn("SelectObjectContent: failed to load data")
		if errors.Is(err, errSelectRecordTooLarge) {
			h.writeError(w, "OverMaxRecordSize",
				"The character number in one record is more than our max threshold, maxCharsPerRecord: 1,048,576", objectKey, r)
			return
		}
		switch selectInputFailure(input) {
		case errSelectCompression:
			h.writeError(w, "InvalidCompressionFormat",
				"The file is not in a supported compression format. Only GZIP and BZIP2 are supported.", objectKey, r)
		case errSelectTooLarge:
			h.writeError(w, "InvalidRequest",
				fmt.Sprintf("The object decompresses to more than %d bytes, the most S3 Select reads", maxSelectDecompressed), objectKey, r)
		default:
			h.writeError(w, "InvalidRequest", "Failed to parse input: "+err.Error(), objectKey, r)
		}
		return
	}

	// ── Stream event-stream response ─────────────────────────────────────────

	w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)

	bytesReturned, queryErr := streamSelectResults(r.Context(), w, db, req.Expression, req.OutputSerialization, flusher)
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

	_ = writeStatsEvent(w, source.n, bytesProcessed, bytesReturned)
	_ = writeEndEvent(w)
	if flusher != nil {
		flusher.Flush()
	}
}
