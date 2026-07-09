package recovery

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
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

// buildTestDeployment creates a realistic data dir with the REAL components:
// global bucket, tenant bucket, versioned bucket, envelope-encrypted objects,
// and exports the recovery bundle. Returns dataDir, bundlePath, passphrase.
func buildTestDeployment(t *testing.T) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()

	// KEK store on the standard SQLite location.
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

	// Storage + Pebble + managers (the store that will be "lost").
	objectsRoot := filepath.Join(dataDir, "objects")
	backend, err := storage.NewFilesystemBackend(storage.Config{Root: objectsRoot})
	require.NoError(t, err)
	metaStore, err := metadata.NewPebbleStore(metadata.PebbleOptions{
		DataDir: filepath.Join(dataDir, "live"), // live store, separate from recovery output
		Logger:  logrus.StandardLogger(),
	})
	require.NoError(t, err)

	bucketMgr := bucket.NewManager(backend, metaStore)
	objMgr := object.NewManager(backend, metaStore, config.StorageConfig{Backend: "filesystem", Root: objectsRoot},
		object.WithKEKProvider(kekStore))

	// Global bucket with two objects (one nested key).
	require.NoError(t, bucketMgr.CreateBucket(ctx, "", "global-bucket", "admin"))
	_, err = objMgr.PutObject(ctx, "global-bucket", "hello.txt", bytes.NewReader([]byte("global object content")), http.Header{})
	require.NoError(t, err)
	_, err = objMgr.PutObject(ctx, "global-bucket", "docs/nested/file.bin", bytes.NewReader(bytes.Repeat([]byte("nested "), 2000)), http.Header{})
	require.NoError(t, err)

	// Tenant bucket.
	require.NoError(t, bucketMgr.CreateBucket(ctx, "tenant-x", "tenant-bucket", "user-1"))
	_, err = objMgr.PutObject(ctx, "tenant-x/tenant-bucket", "data.txt", bytes.NewReader([]byte("tenant object content")), http.Header{})
	require.NoError(t, err)

	// Versioned bucket with two versions of the same key.
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: "versioned-bucket", OwnerID: "admin",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
	}))
	require.NoError(t, backend.Put(ctx, "versioned-bucket/.maxiofs-bucket", bytes.NewReader(nil),
		map[string]string{"tenant-id": ""}))
	_, err = objMgr.PutObject(ctx, "versioned-bucket", "doc.txt", bytes.NewReader([]byte("version one")), http.Header{})
	require.NoError(t, err)
	_, err = objMgr.PutObject(ctx, "versioned-bucket", "doc.txt", bytes.NewReader([]byte("version two — latest")), http.Header{})
	require.NoError(t, err)

	// Export the recovery bundle, then close everything (simulating the loss
	// of the live Pebble store — recovery must not need it).
	passphrase := "recovery-e2e-passphrase"
	bundle, err := kekStore.ExportBundle(passphrase)
	require.NoError(t, err)
	bundlePath := filepath.Join(dataDir, "bundle.json")
	require.NoError(t, os.WriteFile(bundlePath, bundle, 0600))

	require.NoError(t, metaStore.Close())
	require.NoError(t, db.Close())

	// The disaster: the live metadata store is gone.
	require.NoError(t, os.RemoveAll(filepath.Join(dataDir, "live")))

	return dataDir, bundlePath, passphrase
}

// openRecoveredStore performs the documented activation step (the rebuilt
// outDB directory becomes <parent>/metadata) and opens it as the server would.
func openRecoveredStore(t *testing.T, outDB string) metadata.Store {
	t.Helper()
	parent := t.TempDir()
	require.NoError(t, os.Rename(outDB, filepath.Join(parent, "metadata")))
	store, err := metadata.NewPebbleStore(metadata.PebbleOptions{
		DataDir: parent,
		Logger:  logrus.StandardLogger(),
	})
	require.NoError(t, err)
	return store
}

func TestRecovery_FullRebuild(t *testing.T) {
	ctx := context.Background()
	dataDir, bundlePath, passphrase := buildTestDeployment(t)

	report, err := Run(Options{
		DataDir:    dataDir,
		BundlePath: bundlePath,
		Passphrase: passphrase,
		OutDB:      filepath.Join(dataDir, "metadata-recovered"),
	})
	require.NoError(t, err)

	assert.Equal(t, 3, report.Buckets)
	assert.Equal(t, 3, report.Objects, "global x2 + tenant x1 (non-versioned)")
	assert.Equal(t, 2, report.Versions, "two versions of doc.txt")
	assert.Equal(t, 5, report.EncryptedVerified, "every object's DEK must unwrap with the bundle")
	assert.Zero(t, report.EncryptedUnverified)
	assert.Empty(t, report.Failures)
	assert.GreaterOrEqual(t, report.KEKsRestored, 0) // keys already present in this scenario

	// Activate the rebuilt store exactly like the documented step
	// (`mv <outDB> <data-dir>/metadata`) and open it the way the server does.
	store := openRecoveredStore(t, report.OutDB)
	defer store.Close()

	// Buckets with tenants and metrics.
	globalBkt, err := store.GetBucket(ctx, "", "global-bucket")
	require.NoError(t, err)
	assert.Equal(t, int64(2), globalBkt.ObjectCount)

	tenantBkt, err := store.GetBucket(ctx, "tenant-x", "tenant-bucket")
	require.NoError(t, err)
	assert.Equal(t, "tenant-x", tenantBkt.TenantID)

	versionedBkt, err := store.GetBucket(ctx, "", "versioned-bucket")
	require.NoError(t, err)
	require.NotNil(t, versionedBkt.Versioning)
	assert.Equal(t, "Enabled", versionedBkt.Versioning.Status)

	// Objects with original (plaintext) size/etag.
	obj, err := store.GetObject(ctx, "global-bucket", "hello.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(len("global object content")), obj.Size)

	nested, err := store.GetObject(ctx, "global-bucket", "docs/nested/file.bin")
	require.NoError(t, err)
	assert.Equal(t, int64(7*2000), nested.Size)

	_, err = store.GetObject(ctx, "tenant-x/tenant-bucket", "data.txt")
	require.NoError(t, err)

	// Versioned: latest points at version two.
	latest, err := store.GetObject(ctx, "versioned-bucket", "doc.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(len("version two — latest")), latest.Size)
	versions, err := store.GetObjectVersions(ctx, "versioned-bucket", "doc.txt")
	require.NoError(t, err)
	assert.Len(t, versions, 2)
}

// TestRecovery_ObjectsServableAfterRebuild is the end-to-end money test: a
// fresh object manager over the RECOVERED store + restored KEKs must serve
// every object decrypted.
func TestRecovery_ObjectsServableAfterRebuild(t *testing.T) {
	ctx := context.Background()
	dataDir, bundlePath, passphrase := buildTestDeployment(t)

	report, err := Run(Options{
		DataDir: dataDir, BundlePath: bundlePath, Passphrase: passphrase,
		OutDB: filepath.Join(dataDir, "metadata-recovered"),
	})
	require.NoError(t, err)
	require.Empty(t, report.Failures)

	// Bring the "recovered server" up: KEK store from the restored SQLite,
	// object manager over the rebuilt Pebble.
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "db", "maxiofs.db"))
	require.NoError(t, err)
	defer db.Close()
	kekStore, err := kek.Bootstrap(db, "")
	require.NoError(t, err)

	objectsRoot := filepath.Join(dataDir, "objects")
	backend, err := storage.NewFilesystemBackend(storage.Config{Root: objectsRoot})
	require.NoError(t, err)
	metaStore := openRecoveredStore(t, report.OutDB)
	defer metaStore.Close()

	objMgr := object.NewManager(backend, metaStore, config.StorageConfig{Backend: "filesystem", Root: objectsRoot},
		object.WithKEKProvider(kekStore))

	checks := map[string][2]string{
		"global-bucket|hello.txt":            {"global-bucket", "hello.txt"},
		"tenant-x/tenant-bucket|data.txt":    {"tenant-x/tenant-bucket", "data.txt"},
		"global-bucket|docs/nested/file.bin": {"global-bucket", "docs/nested/file.bin"},
		"versioned-bucket|doc.txt":           {"versioned-bucket", "doc.txt"},
	}
	expected := map[string][]byte{
		"global-bucket|hello.txt":            []byte("global object content"),
		"tenant-x/tenant-bucket|data.txt":    []byte("tenant object content"),
		"global-bucket|docs/nested/file.bin": bytes.Repeat([]byte("nested "), 2000),
		"versioned-bucket|doc.txt":           []byte("version two — latest"),
	}
	for id, bk := range checks {
		_, reader, err := objMgr.GetObject(ctx, bk[0], bk[1])
		require.NoError(t, err, id)
		buf := new(bytes.Buffer)
		_, err = buf.ReadFrom(reader)
		reader.Close()
		require.NoError(t, err, id)
		assert.Equal(t, expected[id], buf.Bytes(), id)
	}
}

func TestRecovery_DryRunWritesNothing(t *testing.T) {
	dataDir, bundlePath, passphrase := buildTestDeployment(t)

	outDB := filepath.Join(dataDir, "metadata-recovered")
	report, err := Run(Options{
		DataDir: dataDir, BundlePath: bundlePath, Passphrase: passphrase,
		OutDB: outDB, DryRun: true,
	})
	require.NoError(t, err)
	assert.Equal(t, 3, report.Buckets)

	_, err = os.Stat(outDB)
	assert.True(t, os.IsNotExist(err), "dry run must not create the output store")
}

func TestRecovery_RefusesNonEmptyOutput(t *testing.T) {
	dataDir, bundlePath, passphrase := buildTestDeployment(t)

	outDB := filepath.Join(dataDir, "metadata-recovered")
	require.NoError(t, os.MkdirAll(outDB, 0750))
	require.NoError(t, os.WriteFile(filepath.Join(outDB, "existing.file"), []byte("x"), 0600))

	_, err := Run(Options{
		DataDir: dataDir, BundlePath: bundlePath, Passphrase: passphrase, OutDB: outDB,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not empty")
}

func TestRecovery_WrongPassphrase(t *testing.T) {
	dataDir, bundlePath, _ := buildTestDeployment(t)

	_, err := Run(Options{
		DataDir: dataDir, BundlePath: bundlePath, Passphrase: "totally-wrong",
		OutDB: filepath.Join(dataDir, "metadata-recovered"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrong passphrase")
}
