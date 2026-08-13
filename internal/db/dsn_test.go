package migrations

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestSQLiteDSN_TheCanonicalFormAppliesItsPragmas(t *testing.T) {
	open := func(t *testing.T, name, params string) (journal string, busy int) {
		t.Helper()
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), name)+params)
		require.NoError(t, err)
		defer db.Close()
		require.NoError(t, db.QueryRow("PRAGMA journal_mode").Scan(&journal))
		require.NoError(t, db.QueryRow("PRAGMA busy_timeout").Scan(&busy))
		return journal, busy
	}

	// The form every database in this project is opened with.
	journal, busy := open(t, "right.db", "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)")
	assert.Equal(t, "wal", journal, "the canonical form must put the database in WAL")
	assert.Equal(t, 10000, busy, "the canonical form must set the busy timeout")

	journal, busy = open(t, "bare.db", "")
	assert.NotEqual(t, "wal", journal, "without parameters there is no WAL")
	assert.Zero(t, busy, "without parameters there is no busy timeout")
}
