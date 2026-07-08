package kek

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportBundle_Roundtrip(t *testing.T) {
	db := createTestDB(t)
	store, err := Bootstrap(db, "")
	require.NoError(t, err)

	passphrase := "correct horse battery staple"
	data, err := store.ExportBundle(passphrase)
	require.NoError(t, err)
	assert.Contains(t, string(data), "maxiofs-kek-bundle-v1")

	// The KEK must not appear in the bundle in the clear.
	key, _ := store.CurrentKEK()
	assert.NotContains(t, string(data), hex.EncodeToString(key), "bundle must not leak the KEK in plaintext")

	records, err := DecryptBundle(data, passphrase)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, 1, records[0].Version)
	assert.True(t, records[0].IsCurrent)
	assert.Equal(t, hex.EncodeToString(key), records[0].KeyHex)
}

func TestDecryptBundle_WrongPassphrase(t *testing.T) {
	db := createTestDB(t)
	store, err := Bootstrap(db, "")
	require.NoError(t, err)

	data, err := store.ExportBundle("the-right-passphrase")
	require.NoError(t, err)

	_, err = DecryptBundle(data, "the-wrong-passphrase")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrong passphrase")
}

func TestExportBundle_RejectsShortPassphrase(t *testing.T) {
	db := createTestDB(t)
	store, err := Bootstrap(db, "")
	require.NoError(t, err)

	_, err = store.ExportBundle("short")
	require.Error(t, err)
}

func TestDecryptBundle_RejectsGarbage(t *testing.T) {
	_, err := DecryptBundle([]byte("not json"), "whatever-pass")
	assert.Error(t, err)

	_, err = DecryptBundle([]byte(`{"format":"other-format"}`), "whatever-pass")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported bundle format")
}

func TestBundleDownloadedTracking(t *testing.T) {
	db := createTestDB(t)

	// system_settings table (same schema as migration 3).
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS system_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			type TEXT NOT NULL,
			category TEXT NOT NULL,
			description TEXT,
			editable INTEGER DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`)
	require.NoError(t, err)

	store, err := Bootstrap(db, "")
	require.NoError(t, err)

	ts, err := store.BundleDownloadedAt()
	require.NoError(t, err)
	assert.Zero(t, ts, "never downloaded yet")

	require.NoError(t, store.MarkBundleDownloaded())

	ts, err = store.BundleDownloadedAt()
	require.NoError(t, err)
	assert.Greater(t, ts, int64(0))
}

func TestExportBundle_MultipleVersions(t *testing.T) {
	db := createTestDB(t)
	store, err := Bootstrap(db, "")
	require.NoError(t, err)

	// Simulate a rotated deployment: add a second key version.
	second := strings.Repeat("ab", 32)
	secondKey, _ := hex.DecodeString(second)
	require.NoError(t, store.insertKey(2, secondKey, true, false))

	data, err := store.ExportBundle("a-decent-passphrase")
	require.NoError(t, err)

	records, err := DecryptBundle(data, "a-decent-passphrase")
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, 1, records[0].Version)
	assert.False(t, records[0].IsCurrent)
	assert.Equal(t, 2, records[1].Version)
	assert.True(t, records[1].IsCurrent)
	assert.Equal(t, second, records[1].KeyHex)
}
