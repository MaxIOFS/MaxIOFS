package s3compat

// SelectObjectContent — POST /{bucket}/{object}?select&select-type=2
//
// Strategy: load the object data (CSV or JSON Lines) into an in-memory SQLite
// database, execute the SQL expression, and stream the results back using the
// Amazon Event Stream binary protocol. This gives us a real SQL engine for
// free (WHERE, GROUP BY, ORDER BY, aggregate functions) without any custom
// parser.
//
// Limitations (acceptable for MVP):
//   - Only CSV and JSON Lines input formats are supported (no Parquet).
//   - Compressed input (GZIP, BZIP2) is not supported.
//   - Object must fit in memory; very large objects (>500 MB) may be slow.

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/gorilla/mux"
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
// Amazon Event Stream binary encoder
// ============================================================================
//
// Message layout (all integers big-endian, CRC is CRC-32/IEEE):
//
//   [4B total_len][4B headers_len][4B prelude_crc][headers...][payload...][4B msg_crc]
//
// Header layout:
//
//   [1B name_len][name bytes][1B value_type=7 (string)][2B value_len][value bytes]

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

// bulkInsert inserts all rows into s3object in a single transaction.
func bulkInsert(db *sql.DB, cols []string, rows [][]string) error {
	if len(rows) == 0 {
		return nil
	}
	placeholders := strings.Repeat(",?", len(cols))
	placeholders = "(" + placeholders[1:] + ")"

	quotedCols := make([]string, len(cols))
	for i, c := range cols {
		quotedCols[i] = quoteColIdent(c)
	}
	insertSQL := fmt.Sprintf("INSERT INTO s3object (%s) VALUES %s",
		strings.Join(quotedCols, ","), placeholders)

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, row := range rows {
		vals := make([]interface{}, len(cols))
		for i := range cols {
			if i < len(row) {
				vals[i] = row[i]
			}
		}
		if _, err := stmt.Exec(vals...); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// ============================================================================
// Data loaders
// ============================================================================

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
	cr.FieldsPerRecord = -1 // allow variable field counts

	if cfg != nil && cfg.FieldDelimiter != "" {
		cr.Comma = rune(cfg.FieldDelimiter[0])
	}

	records, err := cr.ReadAll()
	scanned = counter.n
	if err != nil {
		return nil, scanned, fmt.Errorf("parsing CSV: %w", err)
	}
	if len(records) == 0 {
		return nil, scanned, nil
	}

	headerInfo := "NONE"
	if cfg != nil && cfg.FileHeaderInfo != "" {
		headerInfo = strings.ToUpper(cfg.FileHeaderInfo)
	}

	var dataRows [][]string
	switch headerInfo {
	case "USE":
		cols = records[0]
		dataRows = records[1:]
	case "IGNORE":
		n := len(records[0])
		cols = make([]string, n)
		for i := range cols {
			cols[i] = fmt.Sprintf("_%d", i+1)
		}
		dataRows = records[1:]
	default: // NONE
		n := len(records[0])
		cols = make([]string, n)
		for i := range cols {
			cols[i] = fmt.Sprintf("_%d", i+1)
		}
		dataRows = records
	}

	if len(cols) == 0 {
		return nil, scanned, nil
	}

	if err := createSelectTable(db, cols); err != nil {
		return nil, scanned, err
	}
	if err := bulkInsert(db, cols, dataRows); err != nil {
		return nil, scanned, err
	}
	return cols, scanned, nil
}

// loadJSONLines reads newline-delimited JSON objects from r into s3object.
// Returns column names (from keys of all records) and bytes read.
func loadJSONLines(db *sql.DB, r io.Reader) (cols []string, scanned int64, err error) {
	counter := &countingReader{r: r}
	dec := json.NewDecoder(counter)

	var allMaps []map[string]interface{}
	for dec.More() {
		var obj map[string]interface{}
		if decErr := dec.Decode(&obj); decErr != nil {
			break // stop on parse error, use what we have
		}
		allMaps = append(allMaps, obj)
	}
	scanned = counter.n

	if len(allMaps) == 0 {
		return nil, scanned, nil
	}

	// Collect all unique keys (first-seen order).
	seen := make(map[string]bool)
	for _, m := range allMaps {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				cols = append(cols, k)
			}
		}
	}

	if err := createSelectTable(db, cols); err != nil {
		return nil, scanned, err
	}

	rows := make([][]string, len(allMaps))
	for i, m := range allMaps {
		row := make([]string, len(cols))
		for j, col := range cols {
			if v, ok := m[col]; ok {
				switch vv := v.(type) {
				case string:
					row[j] = vv
				case nil:
					row[j] = ""
				default:
					b, _ := json.Marshal(vv)
					row[j] = string(b)
				}
			}
		}
		rows[i] = row
	}

	if err := bulkInsert(db, cols, rows); err != nil {
		return nil, scanned, err
	}
	return cols, scanned, nil
}

// ============================================================================
// Result streaming
// ============================================================================

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

// ============================================================================
// Handler
// ============================================================================

// SelectObjectContent handles POST /{bucket}/{object}?select&select-type=2.
//
// The handler:
//  1. Parses the SelectObjectContentRequest XML (Expression, InputSerialization,
//     OutputSerialization).
//  2. Reads the full object and loads it into an in-memory SQLite database.
//  3. Executes the SQL expression (S3Object is replaced with the internal table
//     name s3object).
//  4. Streams the results back as an Amazon Event Stream (Records events,
//     followed by a Stats event and an End event).
func (h *Handler) SelectObjectContent(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]
	objectKey := getObjectKey(r)

	addS3CompatHeaders(w)

	bucketPath := h.resolveBucketPath(r, bucketName, "")

	// ── Parse request ────────────────────────────────────────────────────────

	var req selectObjectContentRequest
	if err := xml.NewDecoder(r.Body).Decode(&req); err != nil {
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

	db, dbErr := sql.Open("sqlite", ":memory:")
	if dbErr != nil {
		h.writeError(w, "InternalError", "failed to initialise query engine", objectKey, r)
		return
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // required: all ops must share the same in-memory DB

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
