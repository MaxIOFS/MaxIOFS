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
	"testing"

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
