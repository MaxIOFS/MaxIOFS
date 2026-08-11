package migrations

// The SQLite DSN this project must use.
//
// The driver is modernc.org/sqlite, whose connection parameters are written
// `_pragma=name(value)`. The other common driver, mattn/go-sqlite3, uses
// `_busy_timeout=...` and `_journal_mode=...` — and modernc accepts those
// WITHOUT COMPLAINT and applies none of them.
//
// A database opened that way is left in rollback-journal mode with a zero busy
// timeout, so a second writer gets SQLITE_BUSY immediately instead of waiting.
// It surfaced as tests failing at random under parallel load, and the same
// syntax was in three production databases.

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestSQLiteDSN_TheOtherDriversSyntaxIsSilentlyIgnored(t *testing.T) {
	open := func(t *testing.T, name, params string) (journal string, busy int) {
		t.Helper()
		db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), name)+params)
		require.NoError(t, err)
		defer db.Close()
		require.NoError(t, db.QueryRow("PRAGMA journal_mode").Scan(&journal))
		require.NoError(t, db.QueryRow("PRAGMA busy_timeout").Scan(&busy))
		return journal, busy
	}

	journal, busy := open(t, "wrong.db", "?_journal_mode=WAL&_busy_timeout=10000")
	assert.NotEqual(t, "wal", journal,
		"if this ever starts working, the warning below can go")
	assert.Zero(t, busy,
		"the other driver's syntax sets nothing, and says nothing about it")

	journal, busy = open(t, "right.db", "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)")
	assert.Equal(t, "wal", journal)
	assert.Equal(t, 10000, busy)
}
