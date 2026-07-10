package server

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncryptionWorkerPass seeds plaintext objects (as a pre-envelope,
// key-less deployment would have written them), runs a full worker pass and
// verifies they are converted to envelope encryption and still readable.
func TestEncryptionWorkerPass(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()

	bucketName := "encworker-bucket"
	require.NoError(t, server.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:    bucketName,
		OwnerID: "admin",
	}))
	cleanupTestData(t, "", bucketName)

	// Seed 3 plaintext objects directly on storage + Pebble (legacy format).
	contents := map[string][]byte{
		"plain-1.txt":        []byte("first plaintext object"),
		"plain-2.txt":        bytes.Repeat([]byte("second, bigger plaintext object "), 3000),
		"nested/plain-3.txt": []byte("third plaintext object in a folder"),
	}
	for key, body := range contents {
		md5sum := md5.Sum(body)
		sidecar := map[string]string{
			"size":         fmt.Sprintf("%d", len(body)),
			"etag":         hex.EncodeToString(md5sum[:]),
			"content-type": "text/plain",
		}
		require.NoError(t, server.storageBackend.Put(ctx, bucketName+"/"+key, bytes.NewReader(body), sidecar))
		require.NoError(t, server.metadataStore.PutObject(ctx, &metadata.ObjectMetadata{
			Bucket: bucketName, Key: key,
			Size: int64(len(body)), ETag: hex.EncodeToString(md5sum[:]), ContentType: "text/plain",
		}))
	}

	// One already-encrypted object via the normal path (must be skipped).
	req := httptest.NewRequest("PUT", "/ignored", bytes.NewReader([]byte("already envelope")))
	_, err := server.objectManager.PutObject(ctx, bucketName, "encrypted.txt", req.Body, http.Header{})
	require.NoError(t, err)

	// Run a full pass synchronously.
	server.runEncryptionPass(ctx)

	state := server.loadEncryptionWorkerState(ctx)
	assert.Equal(t, "done", state.Status)
	assert.GreaterOrEqual(t, state.Converted, int64(3), "the three plaintext objects must be converted")
	assert.Empty(t, state.LastError)

	// Every seeded object: sidecar is envelope now, content reads back intact.
	for key, body := range contents {
		meta, err := server.storageBackend.GetMetadata(ctx, bucketName+"/"+key)
		require.NoError(t, err)
		assert.Equal(t, "true", meta["encrypted"], key)
		assert.NotEmpty(t, meta["wrapped-dek"], key)

		_, reader, err := server.objectManager.GetObject(ctx, bucketName, key)
		require.NoError(t, err, key)
		readBack, err := io.ReadAll(reader)
		reader.Close()
		require.NoError(t, err, key)
		assert.Equal(t, body, readBack, key)
	}

	// Second pass is a no-op (idempotent): a fresh pass converts nothing.
	server.runEncryptionPass(ctx)
	state = server.loadEncryptionWorkerState(ctx)
	assert.Equal(t, "done", state.Status)
	assert.Equal(t, int64(0), state.Converted, "no further conversions expected")
	assert.Greater(t, state.Skipped, int64(0), "already-encrypted objects are skipped")
}

func TestEncryptionWorkerRunEndpoint(t *testing.T) {
	server := getSharedServer()

	// Tenant admin cannot trigger a pass.
	req := createAuthenticatedRequest("POST", "/api/v1/settings/encryption/worker-run", nil, "tenant-1", "tadmin", true)
	w := httptest.NewRecorder()
	server.handleEncryptionWorkerRun(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// Global admin can.
	req = createAuthenticatedRequest("POST", "/api/v1/settings/encryption/worker-run", nil, "", "admin-user", true)
	w = httptest.NewRecorder()
	server.handleEncryptionWorkerRun(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"started":true`)
}

func TestEncryptionWorkerStatusEndpoint(t *testing.T) {
	server := getSharedServer()

	// Global admin can read the status.
	req := createAuthenticatedRequest("GET", "/api/v1/settings/encryption/worker-status", nil, "", "admin-user", true)
	w := httptest.NewRecorder()
	server.handleEncryptionWorkerStatus(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "status")

	// Tenant admin cannot.
	req = createAuthenticatedRequest("GET", "/api/v1/settings/encryption/worker-status", nil, "tenant-1", "tadmin", true)
	w = httptest.NewRecorder()
	server.handleEncryptionWorkerStatus(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// A checkpoint pointing at a bucket that no longer exists (deleted between
// runs) must fall back to a full pass instead of skipping every bucket.
//
// Runs against a DEDICATED minimal Server (own Pebble + storage + worker
// slot): on the shared server another test's asynchronous pass can hold the
// single-flight guard for minutes (load-aware backoff under suite CPU),
// turning this synchronous run into a silent no-op.
func TestEncryptionWorkerResumeWithDeletedBucket(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	backend, err := storage.NewFilesystemBackend(storage.Config{Root: tempDir})
	require.NoError(t, err)
	metaStore, err := metadata.NewPebbleStore(metadata.PebbleOptions{
		DataDir: filepath.Join(tempDir, "metadata"),
		Logger:  logrus.StandardLogger(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { metaStore.Close() })

	om := object.NewManager(backend, metaStore, config.StorageConfig{
		Backend: "filesystem", Root: tempDir,
	})
	srv := &Server{
		storageBackend: backend,
		metadataStore:  metaStore,
		objectManager:  om,
		// systemMetrics nil → no load backoff; encWorkerRunning zero → free.
	}

	bucketName := "encworker-ghost-resume"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:    bucketName,
		OwnerID: "admin",
	}))

	// One plaintext object that a full pass must pick up.
	body := []byte("plaintext that must be converted despite the stale checkpoint")
	md5sum := md5.Sum(body)
	sidecar := map[string]string{
		"size":         fmt.Sprintf("%d", len(body)),
		"etag":         hex.EncodeToString(md5sum[:]),
		"content-type": "text/plain",
	}
	require.NoError(t, backend.Put(ctx, bucketName+"/victim.txt", bytes.NewReader(body), sidecar))
	require.NoError(t, metaStore.PutObject(ctx, &metadata.ObjectMetadata{
		Bucket: bucketName, Key: "victim.txt",
		Size: int64(len(body)), ETag: hex.EncodeToString(md5sum[:]), ContentType: "text/plain",
	}))

	// Simulate an interrupted pass checkpointed on a bucket that was deleted.
	srv.saveEncryptionWorkerState(ctx, &encryptionWorkerState{
		Status:        "running",
		CurrentBucket: "ghost-bucket-that-was-deleted",
		Marker:        "some/marker",
	})

	srv.runEncryptionPass(ctx)

	state := srv.loadEncryptionWorkerState(ctx)
	assert.Equal(t, "done", state.Status)

	// The full pass must have converted the plaintext object.
	meta, err := backend.GetMetadata(ctx, bucketName+"/victim.txt")
	require.NoError(t, err)
	assert.Equal(t, "true", meta["encrypted"], "stale checkpoint must not skip the whole pass")
	assert.NotEmpty(t, meta["wrapped-dek"])
}
