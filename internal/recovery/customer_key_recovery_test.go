package recovery

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/kek"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecovery_CustomerKeyObject: recover rebuilds the entry of an object
// encrypted with a customer key, whose DEK the bundle cannot open, with the
// wrap that the client's key still opens.
func TestRecovery_CustomerKeyObject(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	dbDir := filepath.Join(dataDir, "db")
	require.NoError(t, os.MkdirAll(dbDir, 0750))
	db, err := sql.Open("sqlite", filepath.Join(dbDir, "maxiofs.db"))
	require.NoError(t, err)
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
	objectsRoot := filepath.Join(dataDir, "objects")
	backend, err := storage.NewFilesystemBackend(storage.Config{Root: objectsRoot})
	require.NoError(t, err)
	metaStore, err := metadata.NewPebbleStore(metadata.PebbleOptions{DataDir: filepath.Join(dataDir, "live"), Logger: logrus.StandardLogger()})
	require.NoError(t, err)
	objMgr := object.NewManager(backend, metaStore, config.StorageConfig{Backend: "filesystem", Root: objectsRoot},
		object.WithKEKProvider(kekStore))
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: "ssec", OwnerID: "admin"}))
	require.NoError(t, backend.CreateBucket(ctx, "ssec"))

	key := bytes.Repeat([]byte{0x42}, object.CustomerKeySize)
	written, err := objMgr.PutObject(object.WithWriteKey(ctx, key), "ssec", "doc", bytes.NewReader([]byte("secret")), http.Header{})
	require.NoError(t, err)
	_, err = objMgr.PutObject(ctx, "ssec", "plain", bytes.NewReader([]byte("plain")), http.Header{})
	require.NoError(t, err)
	require.NoError(t, metaStore.Close())
	require.NoError(t, db.Close())
	require.NoError(t, os.RemoveAll(filepath.Join(dataDir, "live")))

	report, err := Run(Options{DataDir: dataDir, OutDB: filepath.Join(dataDir, "metadata-recovered")})
	require.NoError(t, err)
	require.Empty(t, report.Failures)
	assert.Equal(t, 1, report.CustomerKey)
	store := openRecoveredStore(t, report.OutDB)
	defer store.Close()

	got, err := store.GetObject(ctx, "ssec", "doc")
	require.NoError(t, err)
	assert.Equal(t, written.ETag, got.ETag)
	assert.Equal(t, written.Size, got.Size)
	assert.Empty(t, got.SSEAlgorithm)
	recovered := &object.Object{CustomerDEK: got.CustomerDEK, CustomerDEKIV: got.CustomerDEKIV}
	assert.NoError(t, object.CheckCustomerKey(recovered, key), "the client's key still opens it")
}
