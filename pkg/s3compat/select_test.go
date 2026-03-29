package s3compat

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// ============================================================================
// Event stream helpers
// ============================================================================

// parseEventMessages reads all event-stream messages from data and returns them
// as a slice of (eventType, payload) pairs for easy assertions.
func parseEventMessages(t *testing.T, data []byte) []struct{ EventType, Payload string } {
	t.Helper()
	var out []struct{ EventType, Payload string }
	r := bytes.NewReader(data)
	for r.Len() >= 12 {
		// Read prelude
		var totalLen, headersLen uint32
		_ = binary.Read(r, binary.BigEndian, &totalLen)
		_ = binary.Read(r, binary.BigEndian, &headersLen)
		var preludeCRC uint32
		_ = binary.Read(r, binary.BigEndian, &preludeCRC)

		payloadLen := int(totalLen) - 4 - 4 - 4 - int(headersLen) - 4
		if payloadLen < 0 || int(totalLen) > r.Len()+12 {
			break
		}

		hBytes := make([]byte, headersLen)
		_, _ = io.ReadFull(r, hBytes)

		payload := make([]byte, payloadLen)
		_, _ = io.ReadFull(r, payload)

		var msgCRC uint32
		_ = binary.Read(r, binary.BigEndian, &msgCRC)

		// Parse headers to get :event-type
		eventType := parseHeaderValue(hBytes, ":event-type")
		out = append(out, struct{ EventType, Payload string }{eventType, string(payload)})
	}
	return out
}

// parseHeaderValue scans the binary headers block for a specific header name.
func parseHeaderValue(data []byte, name string) string {
	i := 0
	for i < len(data) {
		if i >= len(data) {
			break
		}
		nameLen := int(data[i])
		i++
		if i+nameLen > len(data) {
			break
		}
		hName := string(data[i : i+nameLen])
		i += nameLen
		if i >= len(data) {
			break
		}
		valueType := data[i]
		i++
		if valueType == 7 { // string
			if i+2 > len(data) {
				break
			}
			vLen := int(binary.BigEndian.Uint16(data[i : i+2]))
			i += 2
			if i+vLen > len(data) {
				break
			}
			val := string(data[i : i+vLen])
			i += vLen
			if hName == name {
				return val
			}
		}
	}
	return ""
}

// ============================================================================
// Event stream encoding unit tests
// ============================================================================

func TestWriteEventMessage_Structure(t *testing.T) {
	headers := [][2]string{
		{":message-type", "event"},
		{":event-type", "End"},
	}
	var buf bytes.Buffer
	err := writeEventMessage(&buf, headers, nil)
	require.NoError(t, err)

	data := buf.Bytes()
	require.True(t, len(data) >= 12, "message must have at least 12 bytes")

	totalLen := binary.BigEndian.Uint32(data[0:4])
	assert.Equal(t, uint32(len(data)), totalLen, "total length field must match actual length")

	// Verify prelude CRC
	preludeCRC := binary.BigEndian.Uint32(data[8:12])
	expectedPreludeCRC := crc32.ChecksumIEEE(data[0:8])
	assert.Equal(t, expectedPreludeCRC, preludeCRC, "prelude CRC must cover first 8 bytes")

	// Verify message CRC (last 4 bytes, covers everything before)
	msgCRC := binary.BigEndian.Uint32(data[len(data)-4:])
	expectedMsgCRC := crc32.ChecksumIEEE(data[:len(data)-4])
	assert.Equal(t, expectedMsgCRC, msgCRC, "message CRC must cover all bytes except last 4")
}

func TestWriteEventMessage_WithPayload(t *testing.T) {
	payload := []byte("hello world")
	var buf bytes.Buffer
	err := writeEventMessage(&buf, [][2]string{
		{":message-type", "event"},
		{":event-type", "Records"},
		{":content-type", "application/octet-stream"},
	}, payload)
	require.NoError(t, err)

	data := buf.Bytes()
	// payload should appear in the message body (between headers and trailing CRC)
	assert.Contains(t, string(data), "hello world")
}

func TestWriteEndEvent(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeEndEvent(&buf))
	msgs := parseEventMessages(t, buf.Bytes())
	require.Len(t, msgs, 1)
	assert.Equal(t, "End", msgs[0].EventType)
	assert.Empty(t, msgs[0].Payload)
}

func TestWriteStatsEvent(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeStatsEvent(&buf, 1024, 1024, 512))
	msgs := parseEventMessages(t, buf.Bytes())
	require.Len(t, msgs, 1)
	assert.Equal(t, "Stats", msgs[0].EventType)
	assert.Contains(t, msgs[0].Payload, "<BytesScanned>1024</BytesScanned>")
	assert.Contains(t, msgs[0].Payload, "<BytesReturned>512</BytesReturned>")
}

// ============================================================================
// SQLite loader unit tests
// ============================================================================

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestLoadCSV_UseHeaders(t *testing.T) {
	csvData := "name,age,city\nAlice,30,NYC\nBob,25,LA\n"
	db := openTestDB(t)

	cols, scanned, err := loadCSV(db, strings.NewReader(csvData), &selectCSVIn{FileHeaderInfo: "USE"})
	require.NoError(t, err)
	assert.Equal(t, []string{"name", "age", "city"}, cols)
	assert.Greater(t, scanned, int64(0))

	// Verify rows were inserted
	rows, err := db.Query(`SELECT name, age, city FROM s3object ORDER BY name`)
	require.NoError(t, err)
	defer rows.Close()
	var results [][]string
	for rows.Next() {
		var n, a, c string
		require.NoError(t, rows.Scan(&n, &a, &c))
		results = append(results, []string{n, a, c})
	}
	require.Len(t, results, 2)
	assert.Equal(t, []string{"Alice", "30", "NYC"}, results[0])
	assert.Equal(t, []string{"Bob", "25", "LA"}, results[1])
}

func TestLoadCSV_NoHeaders(t *testing.T) {
	csvData := "Alice,30,NYC\nBob,25,LA\n"
	db := openTestDB(t)

	cols, _, err := loadCSV(db, strings.NewReader(csvData), &selectCSVIn{FileHeaderInfo: "NONE"})
	require.NoError(t, err)
	assert.Equal(t, []string{"_1", "_2", "_3"}, cols)

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM s3object`).Scan(&count))
	assert.Equal(t, 2, count)
}

func TestLoadCSV_IgnoreHeaders(t *testing.T) {
	csvData := "name,age\nAlice,30\n"
	db := openTestDB(t)

	cols, _, err := loadCSV(db, strings.NewReader(csvData), &selectCSVIn{FileHeaderInfo: "IGNORE"})
	require.NoError(t, err)
	assert.Equal(t, []string{"_1", "_2"}, cols)

	// Only the data row (Alice) should be in the table
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM s3object`).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestLoadCSV_CustomDelimiter(t *testing.T) {
	csvData := "name|age\nAlice|30\n"
	db := openTestDB(t)

	cols, _, err := loadCSV(db, strings.NewReader(csvData), &selectCSVIn{
		FileHeaderInfo: "USE",
		FieldDelimiter: "|",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"name", "age"}, cols)
}

func TestLoadCSV_Empty(t *testing.T) {
	db := openTestDB(t)
	cols, scanned, err := loadCSV(db, strings.NewReader(""), &selectCSVIn{})
	require.NoError(t, err)
	assert.Nil(t, cols)
	assert.Equal(t, int64(0), scanned)
}

func TestLoadJSONLines(t *testing.T) {
	lines := `{"name":"Alice","age":"30","city":"NYC"}
{"name":"Bob","age":"25","city":"LA"}
`
	db := openTestDB(t)

	cols, scanned, err := loadJSONLines(db, strings.NewReader(lines))
	require.NoError(t, err)
	assert.Contains(t, cols, "name")
	assert.Contains(t, cols, "age")
	assert.Greater(t, scanned, int64(0))

	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM s3object`).Scan(&count))
	assert.Equal(t, 2, count)
}

func TestLoadJSONLines_Empty(t *testing.T) {
	db := openTestDB(t)
	cols, _, err := loadJSONLines(db, strings.NewReader(""))
	require.NoError(t, err)
	assert.Nil(t, cols)
}

func TestLoadJSONLines_SparseFields(t *testing.T) {
	// Second record has an extra field not in first record
	lines := `{"a":"1","b":"2"}
{"a":"3","c":"4"}
`
	db := openTestDB(t)
	cols, _, err := loadJSONLines(db, strings.NewReader(lines))
	require.NoError(t, err)
	assert.Contains(t, cols, "a")
	assert.Contains(t, cols, "b")
	assert.Contains(t, cols, "c")
}

// ============================================================================
// streamSelectResults unit tests
// ============================================================================

func setupSelectDB(t *testing.T, csvData string) *sql.DB {
	t.Helper()
	db := openTestDB(t)
	_, _, err := loadCSV(db, strings.NewReader(csvData), &selectCSVIn{FileHeaderInfo: "USE"})
	require.NoError(t, err)
	return db
}

func TestStreamSelectResults_SelectStar_CSVOut(t *testing.T) {
	db := setupSelectDB(t, "name,age\nAlice,30\nBob,25\n")

	var buf bytes.Buffer
	returned, err := streamSelectResults(&buf, db, "SELECT * FROM S3Object",
		selectOutputSerial{CSV: &selectCSVOut{}}, nil)
	require.NoError(t, err)
	assert.Greater(t, returned, int64(0))

	// Parse records events
	msgs := parseEventMessages(t, buf.Bytes())
	require.NotEmpty(t, msgs)
	allRecords := ""
	for _, m := range msgs {
		if m.EventType == "Records" {
			allRecords += m.Payload
		}
	}
	assert.Contains(t, allRecords, "Alice")
	assert.Contains(t, allRecords, "Bob")
}

func TestStreamSelectResults_SelectStar_JSONOut(t *testing.T) {
	db := setupSelectDB(t, "name,age\nAlice,30\nBob,25\n")

	var buf bytes.Buffer
	returned, err := streamSelectResults(&buf, db, "SELECT * FROM S3Object",
		selectOutputSerial{JSON: &selectJSONOut{}}, nil)
	require.NoError(t, err)
	assert.Greater(t, returned, int64(0))

	msgs := parseEventMessages(t, buf.Bytes())
	allRecords := ""
	for _, m := range msgs {
		if m.EventType == "Records" {
			allRecords += m.Payload
		}
	}

	// Each line should be a valid JSON object
	for _, line := range strings.Split(strings.TrimSpace(allRecords), "\n") {
		var obj map[string]string
		assert.NoError(t, json.Unmarshal([]byte(line), &obj), "line should be valid JSON: %s", line)
	}
	assert.Contains(t, allRecords, "Alice")
}

func TestStreamSelectResults_WhereClause(t *testing.T) {
	db := setupSelectDB(t, "name,age\nAlice,30\nBob,25\nCarol,30\n")

	var buf bytes.Buffer
	_, err := streamSelectResults(&buf, db, "SELECT name FROM S3Object WHERE age = '30'",
		selectOutputSerial{CSV: &selectCSVOut{}}, nil)
	require.NoError(t, err)

	msgs := parseEventMessages(t, buf.Bytes())
	allRecords := ""
	for _, m := range msgs {
		if m.EventType == "Records" {
			allRecords += m.Payload
		}
	}
	assert.Contains(t, allRecords, "Alice")
	assert.Contains(t, allRecords, "Carol")
	assert.NotContains(t, allRecords, "Bob")
}

func TestStreamSelectResults_Projection(t *testing.T) {
	db := setupSelectDB(t, "name,age,city\nAlice,30,NYC\n")

	var buf bytes.Buffer
	_, err := streamSelectResults(&buf, db, "SELECT name, city FROM S3Object",
		selectOutputSerial{JSON: &selectJSONOut{}}, nil)
	require.NoError(t, err)

	msgs := parseEventMessages(t, buf.Bytes())
	allRecords := ""
	for _, m := range msgs {
		if m.EventType == "Records" {
			allRecords += m.Payload
		}
	}
	var obj map[string]string
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(allRecords)), &obj))
	assert.Equal(t, "Alice", obj["name"])
	assert.Equal(t, "NYC", obj["city"])
	_, hasAge := obj["age"]
	assert.False(t, hasAge, "age should not be in projection")
}

func TestStreamSelectResults_AggregateCount(t *testing.T) {
	db := setupSelectDB(t, "name,dept\nAlice,eng\nBob,eng\nCarol,hr\n")

	var buf bytes.Buffer
	_, err := streamSelectResults(&buf, db,
		"SELECT dept, COUNT(*) AS cnt FROM S3Object GROUP BY dept ORDER BY dept",
		selectOutputSerial{CSV: &selectCSVOut{}}, nil)
	require.NoError(t, err)

	msgs := parseEventMessages(t, buf.Bytes())
	allRecords := ""
	for _, m := range msgs {
		if m.EventType == "Records" {
			allRecords += m.Payload
		}
	}
	assert.Contains(t, allRecords, "eng")
	assert.Contains(t, allRecords, "2")
	assert.Contains(t, allRecords, "hr")
}

func TestStreamSelectResults_EmptyTable(t *testing.T) {
	db := openTestDB(t)
	// Create table but insert no rows
	_, err := db.Exec(`CREATE TABLE s3object (name TEXT)`)
	require.NoError(t, err)

	var buf bytes.Buffer
	returned, err := streamSelectResults(&buf, db, "SELECT * FROM S3Object",
		selectOutputSerial{CSV: &selectCSVOut{}}, nil)
	require.NoError(t, err)
	assert.Equal(t, int64(0), returned)
	// No Records events expected, but also no error
}

func TestStreamSelectResults_InvalidSQL(t *testing.T) {
	db := openTestDB(t)
	_, err := db.Exec(`CREATE TABLE s3object (name TEXT)`)
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = streamSelectResults(&buf, db, "SELECT * FROM nonexistent_table",
		selectOutputSerial{CSV: &selectCSVOut{}}, nil)
	assert.Error(t, err)
}

func TestStreamSelectResults_BatchFlushing(t *testing.T) {
	// Generate more than 1000 rows to test batch flushing
	var sb strings.Builder
	sb.WriteString("id,val\n")
	for i := 0; i < 2500; i++ {
		fmt.Fprintf(&sb, "%d,value%d\n", i, i)
	}

	db := setupSelectDB(t, sb.String())

	flushed := 0
	flusher := &mockFlusher{onFlush: func() { flushed++ }}

	var buf bytes.Buffer
	returned, err := streamSelectResults(&buf, db, "SELECT * FROM S3Object",
		selectOutputSerial{CSV: &selectCSVOut{}}, flusher)
	require.NoError(t, err)
	assert.Greater(t, returned, int64(0))
	assert.Greater(t, flushed, 1, "should flush multiple times for 2500 rows")

	// Count records events (should be 3: 1000 + 1000 + 500)
	msgs := parseEventMessages(t, buf.Bytes())
	recordCount := 0
	for _, m := range msgs {
		if m.EventType == "Records" {
			recordCount++
		}
	}
	assert.Equal(t, 3, recordCount)
}

// mockFlusher implements http.Flusher for testing.
type mockFlusher struct {
	onFlush func()
}

func (f *mockFlusher) Flush() {
	if f.onFlush != nil {
		f.onFlush()
	}
}

// ============================================================================
// reS3Object regex test
// ============================================================================

func TestReS3Object_Replacement(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"SELECT * FROM S3Object", "SELECT * FROM s3object"},
		{"SELECT * FROM s3object", "SELECT * FROM s3object"}, // already lowercase
		{"SELECT * FROM S3OBJECT", "SELECT * FROM s3object"},
		{"SELECT s.name FROM S3Object s WHERE s.age > 25", "SELECT s.name FROM s3object s WHERE s.age > 25"},
		{"SELECT * FROM S3Object WHERE S3Object.id = 1", "SELECT * FROM s3object WHERE s3object.id = 1"},
	}
	for _, tc := range cases {
		result := reS3Object.ReplaceAllString(tc.input, "s3object")
		assert.Equal(t, tc.expected, result, "input: %s", tc.input)
	}
}
