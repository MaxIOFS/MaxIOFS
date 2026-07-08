package object

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/kek"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// newKEKStore builds a DB-backed KEK store on a fresh SQLite file.
func newKEKStore(t *testing.T) *kek.Store {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "kek.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
		CREATE TABLE encryption_keys (
			version INTEGER PRIMARY KEY,
			key_hex TEXT NOT NULL,
			is_current INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			cluster_shared INTEGER NOT NULL DEFAULT 0
		)
	`)
	require.NoError(t, err)
	store, err := kek.Bootstrap(db, "")
	require.NoError(t, err)
	return store
}

// newNodeManager builds an object manager with its own storage + Pebble +
// KEK provider, simulating one cluster node.
func newNodeManager(t *testing.T, provider kek.Provider) (*objectManager, metadata.Store) {
	t.Helper()
	dir := t.TempDir()
	backend, err := storage.NewFilesystemBackend(storage.Config{Root: dir})
	require.NoError(t, err)
	metaStore, err := metadata.NewPebbleStore(metadata.PebbleOptions{
		DataDir: filepath.Join(dir, "metadata"),
		Logger:  logrus.StandardLogger(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { metaStore.Close() })

	om := NewManager(backend, metaStore, config.StorageConfig{Backend: "filesystem", Root: dir},
		WithKEKProvider(provider)).(*objectManager)
	return om, metaStore
}

// TestRawReplicationRoundtrip simulates the full Phase-3 flow at manager
// level: node A (initiator) creates the cluster KEK, node B adopts it; a new
// object written on A is transferred RAW (ciphertext + sidecar + metadata) to
// B, and B serves the decrypted content without A ever decrypting for the
// transfer.
func TestRawReplicationRoundtrip(t *testing.T) {
	ctx := context.Background()

	// Cluster KEK setup: A creates, B adopts (as the join package would).
	kekA := newKEKStore(t)
	clusterKeys, err := kekA.EnsureClusterKey()
	require.NoError(t, err)

	kekB := newKEKStore(t)
	require.NoError(t, kekB.AdoptClusterKeys(clusterKeys))

	nodeA, metaA := newNodeManager(t, kekA)
	nodeB, metaB := newNodeManager(t, kekB)

	bucketName := "raw-bucket"
	require.NoError(t, metaA.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucketName, OwnerID: "u"}))
	require.NoError(t, metaB.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucketName, OwnerID: "u"}))

	content := bytes.Repeat([]byte("ciphertext replication payload "), 5000) // ~150 KB

	// Write on A — wrapped with the cluster-shared KEK (v2, current).
	objA, err := nodeA.PutObject(ctx, bucketName, "doc.bin", bytes.NewReader(content), http.Header{})
	require.NoError(t, err)

	// Raw read on A: bytes must be ciphertext and eligible for raw transfer.
	rawReader, sidecar, metaObj, err := nodeA.GetObjectRaw(ctx, bucketName, "doc.bin", "")
	require.NoError(t, err)
	assert.Equal(t, "2", sidecar["kek-version"], "new writes wrap with the cluster KEK")
	require.True(t, nodeA.CanReplicateRaw(sidecar), "cluster-shared envelope must be raw-replicable")

	rawBytes, err := io.ReadAll(rawReader)
	rawReader.Close()
	require.NoError(t, err)
	assert.NotEqual(t, content, rawBytes, "raw transfer must carry ciphertext, not plaintext")

	// Raw write on B (as the receiving handler would).
	require.NoError(t, nodeB.PutObjectRaw(ctx, bucketName, "doc.bin", bytes.NewReader(rawBytes), sidecar, metaObj))

	// B decrypts locally with the adopted cluster KEK.
	objB, reader, err := nodeB.GetObject(ctx, bucketName, "doc.bin")
	require.NoError(t, err)
	readBack, err := io.ReadAll(reader)
	reader.Close()
	require.NoError(t, err)
	assert.Equal(t, content, readBack, "replica must decrypt the raw-replicated object")
	assert.Equal(t, objA.ETag, objB.ETag)
	assert.Equal(t, objA.Size, objB.Size)

	// The replica's sidecar preserved the envelope.
	sidecarB, err := nodeB.storage.GetMetadata(ctx, nodeB.getObjectPath(bucketName, "doc.bin"))
	require.NoError(t, err)
	assert.Equal(t, sidecar["wrapped-dek"], sidecarB["wrapped-dek"])
	assert.Equal(t, sidecar["original-etag"], sidecarB["original-etag"])
}

// TestCanReplicateRaw_Negatives: plaintext, legacy and local-KEK objects must
// use the legacy replication path.
func TestCanReplicateRaw_Negatives(t *testing.T) {
	kekStore := newKEKStore(t) // v1 local only, nothing cluster-shared
	om, _ := newNodeManager(t, kekStore)

	// Plaintext object.
	assert.False(t, om.CanReplicateRaw(map[string]string{"size": "10"}))

	// Legacy direct-encrypted (no wrapped DEK).
	assert.False(t, om.CanReplicateRaw(map[string]string{"encrypted": "true"}))

	// Envelope with a local (non-shared) KEK version.
	assert.False(t, om.CanReplicateRaw(map[string]string{
		"encrypted": "true", "wrapped-dek": "abcd", "kek-version": "1",
	}))

	// After the cluster key exists, v2 envelopes qualify.
	_, err := kekStore.EnsureClusterKey()
	require.NoError(t, err)
	assert.True(t, om.CanReplicateRaw(map[string]string{
		"encrypted": "true", "wrapped-dek": "abcd", "kek-version": "2",
	}))
}

// TestRawReplicationRoundtrip_Versioned covers versioned buckets: the raw
// transfer pins a specific version and the replica stores it as a version.
func TestRawReplicationRoundtrip_Versioned(t *testing.T) {
	ctx := context.Background()

	kekA := newKEKStore(t)
	clusterKeys, err := kekA.EnsureClusterKey()
	require.NoError(t, err)
	kekB := newKEKStore(t)
	require.NoError(t, kekB.AdoptClusterKeys(clusterKeys))

	nodeA, metaA := newNodeManager(t, kekA)
	nodeB, metaB := newNodeManager(t, kekB)

	bucketName := "raw-versioned"
	versioning := &metadata.VersioningMetadata{Status: "Enabled"}
	require.NoError(t, metaA.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucketName, OwnerID: "u", Versioning: versioning}))
	require.NoError(t, metaB.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucketName, OwnerID: "u", Versioning: versioning}))

	content := []byte("versioned raw replication")
	objA, err := nodeA.PutObject(ctx, bucketName, "v.txt", bytes.NewReader(content), http.Header{})
	require.NoError(t, err)
	require.NotEmpty(t, objA.VersionID)

	rawReader, sidecar, metaObj, err := nodeA.GetObjectRaw(ctx, bucketName, "v.txt", objA.VersionID)
	require.NoError(t, err)
	rawBytes, _ := io.ReadAll(rawReader)
	rawReader.Close()

	require.NoError(t, nodeB.PutObjectRaw(ctx, bucketName, "v.txt", bytes.NewReader(rawBytes), sidecar, metaObj))

	// The replica serves the pinned version decrypted.
	_, reader, err := nodeB.GetObject(ctx, bucketName, "v.txt", objA.VersionID)
	require.NoError(t, err)
	readBack, _ := io.ReadAll(reader)
	reader.Close()
	assert.Equal(t, content, readBack)
}
