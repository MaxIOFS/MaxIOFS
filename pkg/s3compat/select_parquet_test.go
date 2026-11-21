package s3compat

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/parquet-go/parquet-go/format"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The fixtures were written by pyarrow 26: pyarrow.parquet with Snappy and
// dictionary encoding, int96.parquet with GZIP and the INT96 timestamps of
// Spark and Hive, lz4.parquet with LZ4 (LZ4_RAW).
func parquetFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "select", name))
	require.NoError(t, err)
	return data
}

func loadParquetFixture(t *testing.T, name string) *sql.DB {
	t.Helper()
	data := parquetFixture(t, name)
	db := openSelectFileDB(t)
	_, processed, err := loadParquet(db, bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	assert.Positive(t, processed)
	return db
}

func queryRows(t *testing.T, db *sql.DB, query string) [][]any {
	t.Helper()
	rows, err := db.Query(query)
	require.NoError(t, err)
	defer rows.Close()
	cols, err := rows.Columns()
	require.NoError(t, err)
	var got [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		require.NoError(t, rows.Scan(ptrs...))
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		got = append(got, vals)
	}
	require.NoError(t, rows.Err())
	return got
}

// A Parquet object keeps its types: numbers are numbers, dates and times ISO
// 8601 text, booleans true or false, a list or a struct its JSON, a null NULL.
func TestSelectReadsParquetAsItsWriterTypedIt(t *testing.T) {
	db := loadParquetFixture(t, "pyarrow.parquet")

	got := queryRows(t, db, `SELECT id, small, name, price, ratio, active, born, seen, tags, address FROM s3object ORDER BY id`)
	require.Len(t, got, 3)
	assert.Equal(t, []any{int64(1), int64(10), "Alice", 12.34, 0.5, "true", "1990-05-17", "2026-11-21T10:30:00Z",
		`["a","b"]`, `{"city":"Lima","zip":15001}`}, got[0])
	assert.Equal(t, []any{int64(2), nil, "Bob", nil, 2.25, "false", nil, nil, nil, nil}, got[1])
	assert.Equal(t, []any{int64(3), int64(-5), "Alice", -0.5, nil, nil, "2000-01-01", "1999-12-31T23:59:59.123456Z",
		`[]`, `{"city":"Cusco","zip":null}`}, got[2])

	// Compared and sorted as numbers: 10 > 2, and -5 before 10.
	assert.Equal(t, [][]any{{"Alice", int64(10)}}, queryRows(t, db, `SELECT name, small FROM s3object WHERE small > 2`))
	assert.Equal(t, [][]any{{int64(-5)}, {int64(10)}}, queryRows(t, db, `SELECT small FROM s3object WHERE small IS NOT NULL ORDER BY small`))
}

// The INT96 timestamps of Spark and Hive read as the instants they are.
func TestSelectReadsInt96Timestamps(t *testing.T) {
	db := loadParquetFixture(t, "int96.parquet")
	assert.Equal(t, [][]any{{int64(1), "2026-11-21T10:30:00Z"}, {int64(2), "1970-01-02T00:00:01Z"}},
		queryRows(t, db, `SELECT id, at FROM s3object ORDER BY id`))
}

// Columns compressed with LZ4, as pyarrow writes it (LZ4_RAW), are read.
func TestSelectReadsLZ4Columns(t *testing.T) {
	db := loadParquetFixture(t, "lz4.parquet")
	assert.Equal(t, [][]any{{int64(1), "a"}, {int64(2), "b"}, {int64(3), "c"}},
		queryRows(t, db, `SELECT id, name FROM s3object ORDER BY id`))
}

// A codec the reader has not, LZO or the LZ4 Parquet deprecated, is refused
// before anything is read.
func TestSelectRefusesParquetCodecsItHasNot(t *testing.T) {
	for codec, read := range map[format.CompressionCodec]bool{
		format.Uncompressed: true, format.Snappy: true, format.Gzip: true, format.Zstd: true, format.Brotli: true, format.Lz4Raw: true,
		format.LZO: false, format.Lz4: false,
	} {
		assert.Equal(t, read, parquetCodecRead(codec), codec.String())
	}
}

// Through the API, as a client asks: the object is Parquet, the output CSV or
// JSON, and the statistics count the object read and what its row groups hold.
func TestSelectQueriesAParquetObject(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "sel", ""))
	grantExactly(t, env.authManager, env.userID, "sel", auth.ActionGetObject)
	data := parquetFixture(t, "pyarrow.parquet")
	for key, body := range map[string][]byte{"people.parquet": data, "plain.csv": []byte("name\nAlice\n")} {
		_, err := env.objectManager.PutObject(ctx, env.tenantID+"/sel", key, bytes.NewReader(body), http.Header{})
		require.NoError(t, err)
	}
	root := t.TempDir()
	env.handler.SetDataDir(root)

	rec := selectOn(t, env, "sel", "people.parquet", "NONE", `<Parquet/>`, "SELECT s.name, s.small FROM S3Object s WHERE s.small > 2")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var records, stat string
	for _, ev := range parseEventMessages(t, rec.Body.Bytes()) {
		switch ev.EventType {
		case "Records":
			records += ev.Payload
		case "Stats":
			stat = ev.Payload
		}
	}
	assert.Equal(t, "Alice,10\n", records)
	assert.Contains(t, stat, "<BytesScanned>"+strconv.Itoa(len(data))+"</BytesScanned>", "the object read")
	assert.NotContains(t, stat, "<BytesProcessed>0</BytesProcessed>")
	left, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, left, "the copy of the object and the database are removed")

	code := regexp.MustCompile(`<Code>([^<]*)</Code>`)
	refused := func(key, compression string) string {
		rec := selectOn(t, env, "sel", key, compression, `<Parquet/>`, "SELECT * FROM S3Object")
		assert.Equal(t, http.StatusBadRequest, rec.Code, key)
		if m := code.FindStringSubmatch(rec.Body.String()); m != nil {
			return m[1]
		}
		return rec.Body.String()
	}
	assert.Equal(t, "InvalidRequest", refused("people.parquet", "GZIP"), "Parquet compresses its columns, not the object")
	assert.Equal(t, "InvalidRequest", refused("plain.csv", "NONE"), "an object that is not Parquet")

	defer func(limit int64) { maxSelectRowGroup = limit }(maxSelectRowGroup)
	maxSelectRowGroup = 10
	assert.Equal(t, "InvalidRequest", refused("people.parquet", "NONE"), "a row group over the limit")
}

// A damaged Parquet object is refused, whatever byte is wrong: the reader
// never takes the server down.
func TestSelectSurvivesADamagedParquetObject(t *testing.T) {
	for _, name := range []string{"pyarrow.parquet", "int96.parquet"} {
		data := parquetFixture(t, name)
		for at := 0; at < len(data); at++ {
			damaged := append([]byte(nil), data...)
			damaged[at] ^= 0xff
			db, err := sql.Open("sqlite", ":memory:")
			require.NoError(t, err)
			db.SetMaxOpenConns(1)
			assert.NotPanics(t, func() { _, _, _ = loadParquet(db, bytes.NewReader(damaged), int64(len(damaged))) },
				"%s with byte %d damaged", name, at)
			_ = db.Close()
		}
	}
}

// A JSON record gives a number as a number and text as text, as S3 does.
func TestSelectJSONRecordsKeepNumbers(t *testing.T) {
	db := loadParquetFixture(t, "pyarrow.parquet")
	var buf bytes.Buffer
	_, err := streamSelectResults(context.Background(), &buf, db,
		"SELECT name, small, ratio, active, COUNT(*) AS n FROM S3Object WHERE id = 1", selectOutputSerial{JSON: &selectJSONOut{}}, nil)
	require.NoError(t, err)
	var records string
	for _, ev := range parseEventMessages(t, buf.Bytes()) {
		if ev.EventType == "Records" {
			records += ev.Payload
		}
	}
	assert.Equal(t, `{"name":"Alice","small":10,"ratio":0.5,"active":"true","n":1}`+"\n", records)
}
