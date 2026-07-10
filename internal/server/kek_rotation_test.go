package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/kek"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// newIsolatedEncryptionServer builds a minimal dedicated Server (own SQLite
// KEK store, Pebble, storage backend and worker slot). Rotation/worker tests
// on the shared server are racy: another test's asynchronous pass can hold
// the single-flight guard for minutes under suite CPU (load-aware backoff),
// so a pass kicked here would silently no-op or never reach this bucket.
func newIsolatedEncryptionServer(t *testing.T) *Server {
	t.Helper()
	tempDir := t.TempDir()

	db, err := sql.Open("sqlite", filepath.Join(tempDir, "auth.db"))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
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
	kekStore, err := kek.Bootstrap(db, "")
	require.NoError(t, err)

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
	}, object.WithKEKProvider(kekStore))

	return &Server{
		storageBackend: backend,
		metadataStore:  metaStore,
		objectManager:  om,
		kekStore:       kekStore,
		// systemMetrics nil → no load backoff; encWorkerRunning zero → free;
		// clusterManager/globalConfigSyncMgr nil → no cluster distribution.
	}
}

// TestRotateKEKEndpoint: rotation creates a new current version, resets the
// bundle-downloaded flag, and old objects remain readable; the worker
// re-wraps them on its next pass.
func TestRotateKEKEndpoint(t *testing.T) {
	server := newIsolatedEncryptionServer(t)
	ctx := context.Background()

	bucketName := "rotation-bucket"
	require.NoError(t, server.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: bucketName, OwnerID: "admin",
	}))

	// Object written under the pre-rotation KEK.
	content := []byte("object written before the rotation")
	_, err := server.objectManager.PutObject(ctx, bucketName, "pre-rotate.txt", bytes.NewReader(content), http.Header{})
	require.NoError(t, err)

	metaBefore, err := server.storageBackend.GetMetadata(ctx, bucketName+"/pre-rotate.txt")
	require.NoError(t, err)
	versionBefore := metaBefore["kek-version"]

	// Simulate a downloaded bundle so we can observe the reset.
	require.NoError(t, server.kekStore.MarkBundleDownloaded())

	// Non-admin rejected.
	req := createAuthenticatedRequest("POST", "/api/v1/settings/encryption/rotate-kek", nil, "tenant-1", "tadmin", true)
	w := httptest.NewRecorder()
	server.handleRotateKEK(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// Global admin rotates.
	req = createAuthenticatedRequest("POST", "/api/v1/settings/encryption/rotate-kek", nil, "", "admin-user", true)
	w = httptest.NewRecorder()
	server.handleRotateKEK(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Data struct {
			NewVersion int `json:"newVersion"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	_, current := server.kekStore.CurrentKEK()
	assert.Equal(t, current, resp.Data.NewVersion)
	assert.NotEqual(t, versionBefore, resp.Data.NewVersion)

	// Bundle flag reset → banner logic sees not-downloaded.
	ts, err := server.kekStore.BundleDownloadedAt()
	require.NoError(t, err)
	assert.Zero(t, ts, "rotation must reset the bundle-downloaded flag")

	// The pre-rotation object still reads fine (old version kept).
	_, reader, err := server.objectManager.GetObject(ctx, bucketName, "pre-rotate.txt")
	require.NoError(t, err)
	readBack, _ := io.ReadAll(reader)
	reader.Close()
	assert.Equal(t, content, readBack)

	// A worker pass re-wraps it to the new version without touching data.
	rawBefore, _, err := server.storageBackend.Get(ctx, bucketName+"/pre-rotate.txt")
	require.NoError(t, err)
	dataBefore, _ := io.ReadAll(rawBefore)
	rawBefore.Close()

	// The rotate handler already kicked an async worker pass (single-flight
	// makes a direct call a no-op) — wait for the re-wrap to land.
	var metaAfter map[string]string
	require.Eventually(t, func() bool {
		server.runEncryptionPass(ctx) // no-op while the async pass runs
		metaAfter, err = server.storageBackend.GetMetadata(ctx, bucketName+"/pre-rotate.txt")
		return err == nil && metaAfter["kek-version"] != versionBefore
	}, 15*time.Second, 200*time.Millisecond, "worker must re-wrap to the new KEK version")

	rawAfter, _, err := server.storageBackend.Get(ctx, bucketName+"/pre-rotate.txt")
	require.NoError(t, err)
	dataAfter, _ := io.ReadAll(rawAfter)
	rawAfter.Close()
	assert.Equal(t, dataBefore, dataAfter, "re-wrap must not rewrite object data")

	// Still readable after the re-wrap.
	_, reader, err = server.objectManager.GetObject(ctx, bucketName, "pre-rotate.txt")
	require.NoError(t, err)
	readBack, _ = io.ReadAll(reader)
	reader.Close()
	assert.Equal(t, content, readBack)
}

// TestReceiveKEKSync: a peer's cluster-shared keys are adopted; conflicting
// material is rejected with 409.
func TestReceiveKEKSync(t *testing.T) {
	server := getSharedServer()

	// Valid adoption: a brand-new higher version.
	_, current := server.kekStore.CurrentKEK()
	newKey := make([]byte, 32)
	for i := range newKey {
		newKey[i] = byte(i)
	}
	records := []kek.KeyRecord{{Version: current + 10, KeyHex: hexOf(newKey), IsCurrent: true}}
	body, _ := json.Marshal(map[string]interface{}{"keys": records, "source_node_id": "peer-1"})

	req := httptest.NewRequest("POST", "/api/internal/cluster/kek-sync", bytes.NewReader(body))
	w := httptest.NewRecorder()
	server.handleReceiveKEKSync(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	_, nowCurrent := server.kekStore.CurrentKEK()
	assert.Equal(t, current+10, nowCurrent)
	assert.True(t, server.kekStore.IsClusterShared(current+10))

	// Conflict: same version, different material → 409.
	otherKey := make([]byte, 32)
	for i := range otherKey {
		otherKey[i] = byte(255 - i)
	}
	conflict := []kek.KeyRecord{{Version: current + 10, KeyHex: hexOf(otherKey), IsCurrent: true}}
	body, _ = json.Marshal(map[string]interface{}{"keys": conflict, "source_node_id": "peer-2"})
	req = httptest.NewRequest("POST", "/api/internal/cluster/kek-sync", bytes.NewReader(body))
	w = httptest.NewRecorder()
	server.handleReceiveKEKSync(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func hexOf(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}
