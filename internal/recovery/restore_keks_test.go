package recovery

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/kek"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// buildTwoVersionBundle creates a KEK store with v1 + v2 (v2 current after a
// rotation) and exports it as a recovery bundle file.
func buildTwoVersionBundle(t *testing.T) (bundlePath, passphrase string, records []kek.KeyRecord) {
	t.Helper()
	dir := t.TempDir()

	db, err := sql.Open("sqlite", filepath.Join(dir, "source.db"))
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE encryption_keys (
			version INTEGER PRIMARY KEY, key_hex TEXT NOT NULL,
			is_current INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL,
			cluster_shared INTEGER NOT NULL DEFAULT 0
		);
		CREATE TABLE system_settings (
			key TEXT PRIMARY KEY, value TEXT NOT NULL, type TEXT NOT NULL,
			category TEXT NOT NULL, description TEXT, editable INTEGER DEFAULT 1,
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
		);
	`)
	require.NoError(t, err)

	store, err := kek.Bootstrap(db, "")
	require.NoError(t, err)
	_, err = store.Rotate(false)
	require.NoError(t, err)

	passphrase = "bundle-pass-123"
	data, err := store.ExportBundle(passphrase)
	require.NoError(t, err)
	bundlePath = filepath.Join(dir, "bundle.json")
	require.NoError(t, os.WriteFile(bundlePath, data, 0o600))

	records, err = kek.DecryptBundle(data, passphrase)
	require.NoError(t, err)
	require.Len(t, records, 2)
	return bundlePath, passphrase, records
}

func currentRows(t *testing.T, dbPath string) (count, currentVersion int) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM encryption_keys WHERE is_current = 1`).Scan(&count))
	if count > 0 {
		require.NoError(t, db.QueryRow(`SELECT MAX(version) FROM encryption_keys WHERE is_current = 1`).Scan(&currentVersion))
	}
	return count, currentVersion
}

// Fresh DB: the bundle's current version wins and exactly one row is current.
func TestRestoreKEKs_FreshDBUsesBundleCurrent(t *testing.T) {
	bundlePath, passphrase, records := buildTwoVersionBundle(t)
	dbPath := filepath.Join(t.TempDir(), "db", "maxiofs.db")

	restored, err := restoreKEKs(dbPath, bundlePath, passphrase)
	require.NoError(t, err)
	assert.Equal(t, 2, restored)

	bundleCurrent := 0
	for _, r := range records {
		if r.IsCurrent {
			bundleCurrent = r.Version
		}
	}
	require.NotZero(t, bundleCurrent)

	count, version := currentRows(t, dbPath)
	assert.Equal(t, 1, count, "exactly one current row")
	assert.Equal(t, bundleCurrent, version)
}

// DB that already holds keys with its own current marker: the DB's current is
// kept (it is more recent than any external bundle) and no second is_current
// row may appear.
func TestRestoreKEKs_ExistingCurrentIsPreserved(t *testing.T) {
	bundlePath, passphrase, records := buildTwoVersionBundle(t)

	// Pre-create the target DB holding the bundle's v1 (same material, so no
	// conflict) marked as ITS current.
	var v1 kek.KeyRecord
	for _, r := range records {
		if r.Version == 1 {
			v1 = r
		}
	}
	require.NotEmpty(t, v1.KeyHex)

	dbPath := filepath.Join(t.TempDir(), "db", "maxiofs.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o750))
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE encryption_keys (
			version INTEGER PRIMARY KEY, key_hex TEXT NOT NULL,
			is_current INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL
		)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO encryption_keys (version, key_hex, is_current, created_at) VALUES (1, ?, 1, ?)`,
		v1.KeyHex, time.Now().Unix())
	require.NoError(t, err)
	require.NoError(t, db.Close())

	restored, err := restoreKEKs(dbPath, bundlePath, passphrase)
	require.NoError(t, err)
	assert.Equal(t, 1, restored, "only v2 is new")

	count, version := currentRows(t, dbPath)
	assert.Equal(t, 1, count, "must never end with two is_current rows")
	assert.Equal(t, 1, version, "the DB's own current marker wins over the bundle's")
}

// Same version with different key material must still be refused.
func TestRestoreKEKs_ConflictingMaterialRefused(t *testing.T) {
	bundlePath, passphrase, _ := buildTwoVersionBundle(t)

	dbPath := filepath.Join(t.TempDir(), "db", "maxiofs.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o750))
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE encryption_keys (
			version INTEGER PRIMARY KEY, key_hex TEXT NOT NULL,
			is_current INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL
		)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO encryption_keys (version, key_hex, is_current, created_at) VALUES (1, ?, 1, ?)`,
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", time.Now().Unix())
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = restoreKEKs(dbPath, bundlePath, passphrase)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DIFFERENT material")
}
