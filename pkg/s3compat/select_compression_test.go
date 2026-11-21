package s3compat

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bzip2Lines is {"name":"Alice","age":30} and {"name":"Bob","age":25}, one per
// line, compressed with bzip2: Go reads bzip2 and does not write it.
var bzip2Lines = []byte{
	0x42, 0x5a, 0x68, 0x39, 0x31, 0x41, 0x59, 0x26, 0x53, 0x59, 0xa1, 0x9c, 0x6b, 0xd7, 0x00, 0x00, 0x17, 0x5d, 0x80, 0x00,
	0x10, 0x10, 0x04, 0x5a, 0x10, 0x30, 0x00, 0x3a, 0xa7, 0x80, 0x0a, 0x20, 0x00, 0x21, 0x2a, 0x13, 0x46, 0x8f, 0x49, 0xa6,
	0x26, 0xda, 0x85, 0x1a, 0x32, 0x06, 0x8d, 0x32, 0x34, 0x47, 0xbf, 0x40, 0x63, 0xa1, 0x14, 0x1a, 0x63, 0x70, 0x03, 0x8b,
	0x5f, 0xa1, 0xc7, 0x13, 0xc8, 0xf0, 0x5a, 0x12, 0xd8, 0xbb, 0x92, 0x29, 0xc2, 0x84, 0x85, 0x0c, 0xe3, 0x5e, 0xb8,
}

func gzipped(t *testing.T, data string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write([]byte(data))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

// selectOn runs an S3 Select on key of bucket, as a user that may read it.
func selectOn(t *testing.T, env *coverageTestEnv, bucket, key, compression, input, expression string) *httptest.ResponseRecorder {
	t.Helper()
	body := `<SelectObjectContentRequest><Expression>` + expression + `</Expression><ExpressionType>SQL</ExpressionType>` +
		`<InputSerialization><CompressionType>` + compression + `</CompressionType>` + input + `</InputSerialization>` +
		`<OutputSerialization><CSV/></OutputSerialization></SelectObjectContentRequest>`
	user, err := env.authManager.GetUser(context.Background(), env.userID)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/"+bucket+"/"+key+"?select&select-type=2", strings.NewReader(body))
	req = mux.SetURLVars(req.WithContext(setUserInContext(req.Context(), user)), map[string]string{"bucket": bucket, "object": key})
	rec := httptest.NewRecorder()
	env.handler.SelectObjectContent(rec, req)
	return rec
}

// S3 Select reads objects compressed with GZIP or BZIP2, as AWS does, and its
// statistics tell what was read of the stored object from what the query read
// once decompressed. Without compression both are the object's size.
func TestSelectReadsCompressedObjects(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "sel", ""))
	grantExactly(t, env.authManager, env.userID, "sel", auth.ActionGetObject)
	csvData := "name,age\nAlice,30\nBob,25\n"
	put := func(key string, data []byte) {
		_, err := env.objectManager.PutObject(ctx, env.tenantID+"/sel", key, bytes.NewReader(data), http.Header{})
		require.NoError(t, err)
	}
	put("plain.csv", []byte(csvData))
	put("data.csv.gz", gzipped(t, csvData))
	put("data.jsonl.bz2", bzip2Lines)
	csvIn := `<CSV><FileHeaderInfo>USE</FileHeaderInfo></CSV>`
	stats := regexp.MustCompile(`<BytesScanned>(\d+)</BytesScanned><BytesProcessed>(\d+)</BytesProcessed>`)

	for _, tc := range []struct {
		name, key, compression, input string
		scanned                       int
	}{
		{"plain", "plain.csv", "NONE", csvIn, len(csvData)},
		{"gzip", "data.csv.gz", "GZIP", csvIn, len(gzipped(t, csvData))},
		{"bzip2", "data.jsonl.bz2", "BZIP2", `<JSON><Type>LINES</Type></JSON>`, len(bzip2Lines)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := selectOn(t, env, "sel", tc.key, tc.compression, tc.input, "SELECT name FROM S3Object WHERE age = '30' OR age = 30")
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
			assert.Equal(t, "Alice\n", records)
			m := stats.FindStringSubmatch(stat)
			require.Len(t, m, 3, stat)
			assert.Equal(t, fmt.Sprint(tc.scanned), m[1], "BytesScanned is what was read of the stored object")
			processed := len(csvData)
			if tc.name == "bzip2" {
				processed = 50
			}
			assert.Equal(t, fmt.Sprint(processed), m[2], "BytesProcessed is what the query read")
		})
	}
}

// An object that is not compressed as the request says is refused with
// InvalidCompressionFormat; a type S3 Select does not read, with
// InvalidRequest; one that decompresses to more than a query reads, too.
func TestSelectRefusesWhatItCannotDecompress(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "sel", ""))
	grantExactly(t, env.authManager, env.userID, "sel", auth.ActionGetObject)
	big := "name,age\n" + strings.Repeat("Alice,30\n", 2000)
	for key, data := range map[string][]byte{"plain.csv": []byte("name,age\nAlice,30\n"), "big.csv.gz": gzipped(t, big)} {
		_, err := env.objectManager.PutObject(ctx, env.tenantID+"/sel", key, bytes.NewReader(data), http.Header{})
		require.NoError(t, err)
	}
	csvIn := `<CSV><FileHeaderInfo>USE</FileHeaderInfo></CSV>`
	code := regexp.MustCompile(`<Code>([^<]*)</Code>`)
	refused := func(key, compression string) (int, string) {
		rec := selectOn(t, env, "sel", key, compression, csvIn, "SELECT * FROM S3Object")
		m := code.FindStringSubmatch(rec.Body.String())
		if m == nil {
			body := rec.Body.String()
			return rec.Code, body[:min(len(body), 200)]
		}
		return rec.Code, m[1]
	}

	for _, tc := range []struct{ name, key, compression, want string }{
		{"a plain object read as GZIP", "plain.csv", "GZIP", "InvalidCompressionFormat"},
		{"a plain object read as BZIP2", "plain.csv", "BZIP2", "InvalidCompressionFormat"},
		{"a compression S3 Select does not read", "plain.csv", "ZSTD", "InvalidRequest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, got := refused(tc.key, tc.compression)
			assert.Equal(t, http.StatusBadRequest, status)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("more than a query reads", func(t *testing.T) {
		defer func(limit int64) { maxSelectDecompressed = limit }(maxSelectDecompressed)
		maxSelectDecompressed = int64(len(big)) - 1
		status, got := refused("big.csv.gz", "GZIP")
		assert.Equal(t, http.StatusBadRequest, status)
		assert.Equal(t, "InvalidRequest", got)
		maxSelectDecompressed = int64(len(big))
		status, _ = refused("big.csv.gz", "GZIP")
		assert.Equal(t, http.StatusOK, status, "exactly the limit is read")
	})
}

// endless gives the byte forever.
type endless byte

func (e endless) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(e)
	}
	return len(p), nil
}

// A record is at most 1,048,576 characters, as in AWS, in CSV and in JSON
// Lines: one under that is read, a longer one refused.
func TestSelectRecordsAreAtMostOneMegabyte(t *testing.T) {
	line := func(length int) string { return strings.Repeat("a", length-1) + "\n" }
	jsonLine := func(length int) string {
		return `{"s":"` + strings.Repeat("a", length-len(`{"s":""}`)-1) + "\"}\n"
	}
	limit := int(maxSelectRecord)
	for _, tc := range []struct {
		name string
		data string
		load func(*sql.DB, io.Reader) error
	}{
		{"CSV", line(limit - 100), func(db *sql.DB, r io.Reader) error { _, _, err := loadCSV(db, r, &selectCSVIn{}); return err }},
		{"JSON Lines", jsonLine(limit - 100), func(db *sql.DB, r io.Reader) error { _, _, err := loadJSONLines(db, r); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, tc.load(openSelectFileDB(t), strings.NewReader(tc.data)), "under the limit")
			longer := strings.Replace(tc.data, "a", strings.Repeat("a", 201), 1)
			assert.ErrorIs(t, tc.load(openSelectFileDB(t), strings.NewReader(longer)), errSelectRecordTooLarge, "over it")
		})
	}
}

// A record that does not end is not held in memory: the loader stops once it
// is longer than a record may be, whatever the object holds.
func TestSelectAnEndlessRecordIsNotHeldInMemory(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input func() io.Reader
		load  func(*sql.DB, io.Reader) error
	}{
		{"CSV", func() io.Reader { return io.LimitReader(endless('a'), 256<<20) },
			func(db *sql.DB, r io.Reader) error { _, _, err := loadCSV(db, r, &selectCSVIn{}); return err }},
		{"JSON Lines", func() io.Reader {
			return io.MultiReader(strings.NewReader(`{"s":"`), io.LimitReader(endless('a'), 256<<20))
		},
			func(db *sql.DB, r io.Reader) error { _, _, err := loadJSONLines(db, r); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openSelectFileDB(t)
			var err error
			grown := peakHeapGrowth(func() { err = tc.load(db, tc.input()) })
			assert.ErrorIs(t, err, errSelectRecordTooLarge)
			assert.Less(t, grown, uint64(16<<20), "the record was held in memory")
		})
	}
}

// Through the API, a record longer than that is refused with OverMaxRecordSize,
// compressed or not.
func TestSelectRefusesARecordOverOneMegabyte(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "sel", ""))
	grantExactly(t, env.authManager, env.userID, "sel", auth.ActionGetObject)
	long := "name\n" + strings.Repeat("a", 2<<20) + "\n"
	for key, data := range map[string][]byte{"long.csv": []byte(long), "long.csv.gz": gzipped(t, long)} {
		_, err := env.objectManager.PutObject(ctx, env.tenantID+"/sel", key, bytes.NewReader(data), http.Header{})
		require.NoError(t, err)
	}
	for key, compression := range map[string]string{"long.csv": "NONE", "long.csv.gz": "GZIP"} {
		rec := selectOn(t, env, "sel", key, compression, `<CSV><FileHeaderInfo>USE</FileHeaderInfo></CSV>`, "SELECT * FROM S3Object")
		assert.Equal(t, http.StatusBadRequest, rec.Code, key)
		assert.Contains(t, rec.Body.String(), "<Code>OverMaxRecordSize</Code>", key)
	}
}

// The query's database is a temporary file of the storage root, not of the
// system's temporary directory, which may be RAM; none is left behind.
func TestSelectWorksInTheStorageRoot(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "sel", ""))
	grantExactly(t, env.authManager, env.userID, "sel", auth.ActionGetObject)
	_, err := env.objectManager.PutObject(ctx, env.tenantID+"/sel", "plain.csv", strings.NewReader("name\nAlice\n"), http.Header{})
	require.NoError(t, err)
	csvIn := `<CSV><FileHeaderInfo>USE</FileHeaderInfo></CSV>`

	root := t.TempDir()
	env.handler.SetDataDir(root)
	rec := selectOn(t, env, "sel", "plain.csv", "NONE", csvIn, "SELECT * FROM S3Object")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	left, err := os.ReadDir(root)
	require.NoError(t, err)
	assert.Empty(t, left, "a query leaves nothing behind")

	env.handler.SetDataDir(filepath.Join(root, "not-there"))
	rec = selectOn(t, env, "sel", "plain.csv", "NONE", csvIn, "SELECT * FROM S3Object")
	assert.Equal(t, http.StatusInternalServerError, rec.Code, "the database goes where the storage root is")
}
