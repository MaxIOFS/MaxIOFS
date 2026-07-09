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

	// Same schema as migrations 16 + 17.
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS encryption_keys (
			version INTEGER PRIMARY KEY,
			key_hex TEXT NOT NULL,
			is_current INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			cluster_shared INTEGER NOT NULL DEFAULT 0
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

// TestEnsureClusterKey verifies the cluster KEK is created once (next free
// version, current, cluster-shared) and reused thereafter.
func TestEnsureClusterKey(t *testing.T) {
	db := createTestDB(t)
	store, err := Bootstrap(db, "")
	require.NoError(t, err)

	records, err := store.EnsureClusterKey()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, 2, records[0].Version, "cluster key takes the next free version after local v1")
	assert.True(t, records[0].IsCurrent)
	assert.True(t, store.IsClusterShared(2))
	assert.False(t, store.IsClusterShared(1), "local v1 stays node-local")

	_, current := store.CurrentKEK()
	assert.Equal(t, 2, current, "new writes wrap with the cluster key")

	// Second call reuses the existing key.
	again, err := store.EnsureClusterKey()
	require.NoError(t, err)
	require.Len(t, again, 1)
	assert.Equal(t, records[0].KeyHex, again[0].KeyHex)

	// Survives a reload (restart).
	store2, err := Bootstrap(db, "")
	require.NoError(t, err)
	_, current2 := store2.CurrentKEK()
	assert.Equal(t, 2, current2)
	assert.True(t, store2.IsClusterShared(2))
}

// TestAdoptClusterKeys covers the joining-node side: merge, current switch,
// idempotency, and version-conflict rejection.
func TestAdoptClusterKeys(t *testing.T) {
	// Node A (initiator) creates the cluster key.
	dbA := createTestDB(t)
	storeA, err := Bootstrap(dbA, "")
	require.NoError(t, err)
	clusterKeys, err := storeA.EnsureClusterKey()
	require.NoError(t, err)

	// Node B (joining) has its own local v1.
	dbB := createTestDB(t)
	storeB, err := Bootstrap(dbB, "")
	require.NoError(t, err)
	localV1B, _ := storeB.CurrentKEK()

	require.NoError(t, storeB.AdoptClusterKeys(clusterKeys))

	// B now wraps new objects with the cluster key…
	keyB, currentB := storeB.CurrentKEK()
	assert.Equal(t, 2, currentB)
	assert.Equal(t, clusterKeys[0].KeyHex, hexEncode(keyB))
	assert.True(t, storeB.IsClusterShared(2))

	// …and keeps its local v1 for its pre-join objects.
	v1, err := storeB.KEKByVersion(1)
	require.NoError(t, err)
	assert.Equal(t, localV1B, v1)
	assert.False(t, storeB.IsClusterShared(1))

	// Idempotent re-adopt (rejoin).
	require.NoError(t, storeB.AdoptClusterKeys(clusterKeys))

	// Conflict: same version, different material → rejected.
	conflict := []KeyRecord{{Version: 1, KeyHex: clusterKeys[0].KeyHex, IsCurrent: true}}
	err = storeB.AdoptClusterKeys(conflict)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "different material")

	// Adoption persists across restart.
	storeB2, err := Bootstrap(dbB, "")
	require.NoError(t, err)
	_, current := storeB2.CurrentKEK()
	assert.Equal(t, 2, current)
	assert.True(t, storeB2.IsClusterShared(2))
}

func hexEncode(b []byte) string { return hex.EncodeToString(b) }

// TestRotate verifies rotation creates the next version, moves the current
// marker, keeps old versions decryptable and survives reload.
func TestRotate(t *testing.T) {
	db := createTestDB(t)
	// system_settings so the bundle-flag reset works.
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS system_settings (
			key TEXT PRIMARY KEY, value TEXT NOT NULL, type TEXT NOT NULL,
			category TEXT NOT NULL, description TEXT, editable INTEGER DEFAULT 1,
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
		)
	`)
	require.NoError(t, err)

	store, err := Bootstrap(db, "")
	require.NoError(t, err)
	oldKey, oldVersion := store.CurrentKEK()
	require.Equal(t, 1, oldVersion)

	// Simulate a downloaded bundle, then rotate.
	require.NoError(t, store.MarkBundleDownloaded())

	newVersion, err := store.Rotate(false)
	require.NoError(t, err)
	assert.Equal(t, 2, newVersion)

	newKey, current := store.CurrentKEK()
	assert.Equal(t, 2, current)
	assert.NotEqual(t, oldKey, newKey)
	assert.False(t, store.IsClusterShared(2))

	// The old version remains available for unwrapping existing DEKs.
	v1, err := store.KEKByVersion(1)
	require.NoError(t, err)
	assert.Equal(t, oldKey, v1)

	// Rotation invalidates the downloaded-bundle marker (banner reappears).
	ts, err := store.BundleDownloadedAt()
	require.NoError(t, err)
	assert.Zero(t, ts, "rotation must reset the bundle-downloaded flag")

	// Cluster-shared rotation flags the new version as shared.
	sharedVersion, err := store.Rotate(true)
	require.NoError(t, err)
	assert.Equal(t, 3, sharedVersion)
	assert.True(t, store.IsClusterShared(3))

	// Survives reload.
	store2, err := Bootstrap(db, "")
	require.NoError(t, err)
	_, current2 := store2.CurrentKEK()
	assert.Equal(t, 3, current2)
	assert.True(t, store2.IsClusterShared(3))
	_, err = store2.KEKByVersion(1)
	assert.NoError(t, err, "old versions are never deleted")
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
