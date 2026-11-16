package s3compat

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An S3 Select expression comes from whoever may read the object. It reads the
// object and nothing else: no other database file, no file written, no schema
// changed, whatever statements it chains.
func TestSelectExpression_RunsNothingButASelect(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "other.db")
	otherDB, err := sql.Open("sqlite", other)
	require.NoError(t, err)
	_, err = otherDB.Exec(`CREATE TABLE secrets (s TEXT); INSERT INTO secrets VALUES ('the-other-database')`)
	require.NoError(t, err)
	require.NoError(t, otherDB.Close())
	written := filepath.Join(dir, "written.db")

	for _, expr := range []string{
		"ATTACH DATABASE '" + filepath.ToSlash(other) + "' AS o",
		"SELECT 1; ATTACH DATABASE '" + filepath.ToSlash(other) + "' AS o; SELECT s FROM o.secrets",
		"VACUUM INTO '" + filepath.ToSlash(written) + "'",
		"SELECT 1; CREATE TABLE planted (x TEXT)",
		"SELECT 1; DROP TABLE s3object",
		"SELECT 1; PRAGMA writable_schema = 1",
	} {
		t.Run(expr, func(t *testing.T) {
			db := setupSelectDB(t, "name\nAlice\n")

			var buf bytes.Buffer
			_, err := streamSelectResults(context.Background(), &buf, db, expr,
				selectOutputSerial{CSV: &selectCSVOut{}}, nil)
			assert.Error(t, err)
			assert.NotContains(t, buf.String(), "the-other-database")

			_, statErr := os.Stat(written)
			assert.True(t, os.IsNotExist(statErr), "VACUUM INTO wrote a file")

			var tables int
			require.NoError(t, db.QueryRow(
				`SELECT count(*) FROM sqlite_master WHERE name IN ('planted', 's3object')`).Scan(&tables))
			assert.Equal(t, 1, tables, "the schema changed")
		})
	}
}

// What S3 Select asks still runs: a projection, a filter, an aggregate, a
// function, a subquery.
func TestSelectExpression_ASelectStillRuns(t *testing.T) {
	for _, expr := range []string{
		"SELECT s.name FROM S3Object s WHERE s.age > '20' LIMIT 1",
		"SELECT COUNT(*), MAX(age) FROM S3Object",
		"SELECT UPPER(name), CAST(age AS INTEGER) FROM S3Object",
		"SELECT name FROM S3Object WHERE age = (SELECT MAX(age) FROM S3Object)",
	} {
		t.Run(expr, func(t *testing.T) {
			db := setupSelectDB(t, "name,age\nAlice,30\nBob,25\n")

			var buf bytes.Buffer
			_, err := streamSelectResults(context.Background(), &buf, db, expr,
				selectOutputSerial{CSV: &selectCSVOut{}}, nil)
			require.NoError(t, err)
			assert.NotEmpty(t, buf.Bytes())
		})
	}
}
