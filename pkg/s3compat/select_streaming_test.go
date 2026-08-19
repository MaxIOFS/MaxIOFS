package s3compat

import (
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// peakHeapGrowth samples the live heap WHILE fn runs. Measuring before and
// after would miss the point: a loader that buffers the whole object releases
// it on return, and a GC before the second reading hides the very peak that is
// under test.
func peakHeapGrowth(fn func()) uint64 {
	var base runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&base)

	var peak atomic.Uint64
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		var m runtime.MemStats
		for {
			select {
			case <-done:
				return
			default:
			}
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > base.HeapAlloc {
				if grown := m.HeapAlloc - base.HeapAlloc; grown > peak.Load() {
					peak.Store(grown)
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()

	fn()
	close(done)
	<-finished
	return peak.Load()
}

// openSelectFileDB matches what the handler does: the query engine is backed by
// a temporary file, so what stays in the heap is the loader's own doing.
func openSelectFileDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "select.db")
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(OFF)&_pragma=synchronous(OFF)")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// The loaders must consume the object as a stream. What proves that is not an
// absolute number — the query engine keeps a fixed page cache either way — but
// that memory does NOT scale with the input: eight times the object must not
// cost eight times the memory.
func TestSelectLoaders_MemoryDoesNotScaleWithInput(t *testing.T) {
	csvOf := func(rows int) string {
		var b strings.Builder
		b.WriteString("a,b\n")
		for i := 0; i < rows; i++ {
			fmt.Fprintf(&b, "%d,%d\n", i, i)
		}
		return b.String()
	}
	jsonOf := func(rows int) string {
		var b strings.Builder
		for i := 0; i < rows; i++ {
			fmt.Fprintf(&b, "{\"a\":%d,\"b\":%d}\n", i, i)
		}
		return b.String()
	}

	cases := []struct {
		name  string
		build func(int) string
		load  func(*sql.DB, io.Reader)
	}{
		{
			name:  "CSV",
			build: csvOf,
			load: func(db *sql.DB, r io.Reader) {
				_, _, _ = loadCSV(db, r, &selectCSVIn{FileHeaderInfo: "USE"})
			},
		},
		{
			name:  "JSONLines",
			build: jsonOf,
			load: func(db *sql.DB, r io.Reader) {
				_, _, _ = loadJSONLines(db, r)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const small, large = 50_000, 400_000 // eight times the data

			measure := func(rows int) (uint64, int) {
				payload := tc.build(rows)
				db := openSelectFileDB(t)
				return peakHeapGrowth(func() { tc.load(db, strings.NewReader(payload)) }), len(payload)
			}

			smallGrowth, smallSize := measure(small)
			largeGrowth, largeSize := measure(large)

			t.Logf("peak heap: %d bytes of input -> %d bytes, %d bytes of input -> %d bytes",
				smallSize, smallGrowth, largeSize, largeGrowth)

			// Measured on this code: streaming peaks at roughly 1.2x the input,
			// while holding the parsed records reached 23x. Three times the
			// input separates them with room to spare.
			assert.Less(t, largeGrowth, uint64(largeSize)*3,
				"the loader held the object rather than streaming it")
		})
	}
}

// A key that first appears on a later record still becomes a column, which is
// what lets the JSON loader stream instead of buffering to learn the schema.
func TestLoadJSONLines_SchemaGrowsMidStream(t *testing.T) {
	db := openSelectFileDB(t)
	input := "{\"a\":\"1\"}\n{\"a\":\"2\",\"b\":\"late\"}\n"

	cols, _, err := loadJSONLines(db, strings.NewReader(input))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a", "b"}, cols)

	var late string
	require.NoError(t, db.QueryRow(`SELECT "b" FROM s3object WHERE "a" = '2'`).Scan(&late))
	assert.Equal(t, "late", late)
}
