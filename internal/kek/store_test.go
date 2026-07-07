package kek

import (
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func createTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Same schema as migration16_v150_EncryptionKeys.
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS encryption_keys (
			version INTEGER PRIMARY KEY,
			key_hex TEXT NOT NULL,
			is_current INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL
		)
	`)
	require.NoError(t, err)

	return db
}

// Case 3: no DB key, no config key → a fresh KEK is generated and persisted.
func TestBootstrap_GeneratesKEKWhenNothingConfigured(t *testing.T) {
	db := createTestDB(t)

	store, err := Bootstrap(db, "")
	require.NoError(t, err)

	key, version := store.CurrentKEK()
	assert.Equal(t, 1, version)
	assert.Len(t, key, 32)

	// The generated key must be persisted: a second bootstrap returns it.
	store2, err := Bootstrap(db, "")
	require.NoError(t, err)
	key2, version2 := store2.CurrentKEK()
	assert.Equal(t, 1, version2)
	assert.Equal(t, key, key2, "restart must reuse the persisted KEK")
}

// Case 2: no DB key but config has one → seeded as KEK version 1.
func TestBootstrap_SeedsKEKFromConfig(t *testing.T) {
	db := createTestDB(t)

	configKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	store, err := Bootstrap(db, configKey)
	require.NoError(t, err)

	key, version := store.CurrentKEK()
	assert.Equal(t, 1, version)
	assert.Equal(t, configKey, hex.EncodeToString(key))
}

// Case 1: DB key exists → used even if config carries a different key.
func TestBootstrap_DBKeyWinsOverConfig(t *testing.T) {
	db := createTestDB(t)

	seedKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	_, err := Bootstrap(db, seedKey)
	require.NoError(t, err)

	otherKey := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	store, err := Bootstrap(db, otherKey)
	require.NoError(t, err)

	key, version := store.CurrentKEK()
	assert.Equal(t, 1, version)
	assert.Equal(t, seedKey, hex.EncodeToString(key), "DB KEK is authoritative over config")
}

func TestBootstrap_RejectsInvalidConfigKey(t *testing.T) {
	db := createTestDB(t)

	_, err := Bootstrap(db, "too-short")
	assert.Error(t, err)

	_, err = Bootstrap(db, "zz23456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	assert.Error(t, err)
}

func TestKEKByVersion(t *testing.T) {
	db := createTestDB(t)

	store, err := Bootstrap(db, "")
	require.NoError(t, err)

	current, version := store.CurrentKEK()
	byVersion, err := store.KEKByVersion(version)
	require.NoError(t, err)
	assert.Equal(t, current, byVersion)

	_, err = store.KEKByVersion(99)
	assert.Error(t, err)
}

func TestEphemeralProvider(t *testing.T) {
	p, err := Ephemeral()
	require.NoError(t, err)

	key, version := p.CurrentKEK()
	assert.Equal(t, 1, version)
	assert.Len(t, key, 32)

	byVersion, err := p.KEKByVersion(1)
	require.NoError(t, err)
	assert.Equal(t, key, byVersion)

	_, err = p.KEKByVersion(2)
	assert.Error(t, err)

	fixed := make([]byte, 32)
	fixed[0] = 0xAB
	p2, err := EphemeralFromKey(fixed)
	require.NoError(t, err)
	k2, _ := p2.CurrentKEK()
	assert.Equal(t, fixed, k2)

	_, err = EphemeralFromKey([]byte("short"))
	assert.Error(t, err)
}
