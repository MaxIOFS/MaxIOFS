package metadata

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupPebbleTestStore creates a temporary PebbleStore for unit tests.
// Uses os.MkdirTemp instead of t.TempDir() because Pebble may hold OS-level
// file handles briefly after Close() on Windows, causing TempDir's automatic
// cleanup to fail with "directory not empty".
func setupPebbleTestStore(t *testing.T) (*PebbleStore, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "pebble-test-*")
	require.NoError(t, err)

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	store, storeErr := NewPebbleStore(PebbleOptions{
		DataDir: tmpDir,
		Logger:  logger,
	})
	require.NoError(t, storeErr)
	require.NotNil(t, store)

	cleanup := func() {
		_ = store.Close()
		_ = os.RemoveAll(tmpDir) // ignore error on Windows file locking
	}
	return store, cleanup
}

// ==================== Pebble store basic smoke test ====================

func TestPebbleStoreBucketOperations(t *testing.T) {
	store, cleanup := setupPebbleTestStore(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("CreateAndGetBucket", func(t *testing.T) {
		bkt := &BucketMetadata{
			Name:      "pebble-bucket",
			TenantID:  "tenant1",
			OwnerID:   "user1",
			OwnerType: "user",
			Region:    "us-east-1",
		}
		require.NoError(t, store.CreateBucket(ctx, bkt))
		assert.False(t, bkt.CreatedAt.IsZero())

		got, err := store.GetBucket(ctx, "tenant1", "pebble-bucket")
		require.NoError(t, err)
		assert.Equal(t, "pebble-bucket", got.Name)
		assert.Equal(t, "tenant1", got.TenantID)
	})

	t.Run("DuplicateBucketRejected", func(t *testing.T) {
		bkt := &BucketMetadata{Name: "pebble-bucket", TenantID: "tenant1", OwnerID: "u", OwnerType: "user"}
		err := store.CreateBucket(ctx, bkt)
		assert.ErrorIs(t, err, ErrBucketAlreadyExists)
	})

	t.Run("BucketNotFound", func(t *testing.T) {
		_, err := store.GetBucket(ctx, "tenant1", "no-such-bucket")
		assert.ErrorIs(t, err, ErrBucketNotFound)
	})

	t.Run("UpdateBucketMetrics", func(t *testing.T) {
		require.NoError(t, store.UpdateBucketMetrics(ctx, "tenant1", "pebble-bucket", 3, 512))
		bkt, err := store.GetBucket(ctx, "tenant1", "pebble-bucket")
		require.NoError(t, err)
		assert.Equal(t, int64(3), bkt.ObjectCount)
		assert.Equal(t, int64(512), bkt.TotalSize)
	})

	t.Run("ListBuckets", func(t *testing.T) {
		bkt2 := &BucketMetadata{Name: "pebble-bucket-2", TenantID: "tenant1", OwnerID: "u", OwnerType: "user"}
		require.NoError(t, store.CreateBucket(ctx, bkt2))

		buckets, err := store.ListBuckets(ctx, "tenant1")
		require.NoError(t, err)
		assert.Len(t, buckets, 2)
	})

	t.Run("DeleteBucket", func(t *testing.T) {
		require.NoError(t, store.DeleteBucket(ctx, "tenant1", "pebble-bucket"))
		_, err := store.GetBucket(ctx, "tenant1", "pebble-bucket")
		assert.ErrorIs(t, err, ErrBucketNotFound)
	})
}

func TestPebbleStoreObjectOperations(t *testing.T) {
	store, cleanup := setupPebbleTestStore(t)
	defer cleanup()

	ctx := context.Background()

	bkt := &BucketMetadata{Name: "obj-bucket", TenantID: "t1", OwnerID: "u", OwnerType: "user"}
	require.NoError(t, store.CreateBucket(ctx, bkt))

	t.Run("PutGetObject", func(t *testing.T) {
		obj := &ObjectMetadata{
			Bucket:      "obj-bucket",
			Key:         "hello.txt",
			Size:        42,
			ETag:        "abc",
			ContentType: "text/plain",
		}
		require.NoError(t, store.PutObject(ctx, obj))
		assert.False(t, obj.CreatedAt.IsZero())

		got, err := store.GetObject(ctx, "obj-bucket", "hello.txt")
		require.NoError(t, err)
		assert.Equal(t, int64(42), got.Size)
	})

	t.Run("ObjectExists", func(t *testing.T) {
		exists, err := store.ObjectExists(ctx, "obj-bucket", "hello.txt")
		require.NoError(t, err)
		assert.True(t, exists)

		exists, err = store.ObjectExists(ctx, "obj-bucket", "nope.txt")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("ListObjects", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			obj := &ObjectMetadata{
				Bucket: "obj-bucket",
				Key:    fmt.Sprintf("dir/file%d.txt", i),
				Size:   10,
				ETag:   "x",
			}
			require.NoError(t, store.PutObject(ctx, obj))
		}
		objects, _, err := store.ListObjects(ctx, "obj-bucket", "dir/", "", 10)
		require.NoError(t, err)
		assert.Len(t, objects, 5)
	})

	t.Run("DeleteObject", func(t *testing.T) {
		require.NoError(t, store.DeleteObject(ctx, "obj-bucket", "hello.txt"))
		_, err := store.GetObject(ctx, "obj-bucket", "hello.txt")
		assert.ErrorIs(t, err, ErrObjectNotFound)
	})
}

// ==================== Migration test ====================

// TestMigrateFromBadger verifies that the migration copies all keys from
// a BadgerDB directory to Pebble and that the data is accessible via PebbleStore.
func TestMigrateFromBadger(t *testing.T) {
	// Use os.MkdirTemp instead of t.TempDir() because Pebble/BadgerDB may hold
	// OS-level file handles briefly after Close() on Windows, causing the
	// automatic TempDir cleanup to fail with "directory not empty".
	dataDir, err := os.MkdirTemp("", "TestMigrateFromBadger*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dataDir) }) //nolint:errcheck
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// ── Step 1: create a BadgerDB with known data ──────────────────────────────
	badgerPath := fmt.Sprintf("%s/metadata", dataDir)
	badgerOpts := badger.DefaultOptions(badgerPath).
		WithLogger(newBadgerLogger(logger)).
		WithSyncWrites(false)

	bdb, err := badger.Open(badgerOpts)
	require.NoError(t, err, "open BadgerDB for seed")

	// Seed a bucket and an object
	seedBucket := &BucketMetadata{
		Name:      "migrated-bucket",
		TenantID:  "t1",
		OwnerID:   "u1",
		OwnerType: "user",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	seedObject := &ObjectMetadata{
		Bucket:      "t1/migrated-bucket",
		Key:         "file.txt",
		Size:        999,
		ETag:        "deadbeef",
		ContentType: "text/plain",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		LastModified: time.Now(),
	}

	badgerStore := &BadgerStore{db: bdb, logger: logger}
	ctx := context.Background()
	require.NoError(t, badgerStore.CreateBucket(ctx, seedBucket))
	require.NoError(t, badgerStore.PutObject(ctx, seedObject))

	// Close BadgerDB — migration opens it read-only
	require.NoError(t, bdb.Close())

	// ── Step 2: run migration ──────────────────────────────────────────────────
	require.NoError(t, MigrateFromBadgerIfNeeded(dataDir, logger))

	// After migration, metadata/ should be Pebble (KEYREGISTRY is gone)
	_, err = os.Stat(fmt.Sprintf("%s/metadata/KEYREGISTRY", dataDir))
	assert.True(t, os.IsNotExist(err), "KEYREGISTRY should not exist after migration")

	// metadata_badger_backup_* should exist
	entries, err := os.ReadDir(dataDir)
	require.NoError(t, err)
	var foundBackup bool
	for _, e := range entries {
		if e.IsDir() && len(e.Name()) > len("metadata_badger_backup") && e.Name()[:len("metadata_badger_backup")] == "metadata_badger_backup" {
			foundBackup = true
		}
	}
	assert.True(t, foundBackup, "backup directory should exist")

	// ── Step 3: open PebbleStore and verify data ───────────────────────────────
	pstore, err := NewPebbleStore(PebbleOptions{DataDir: dataDir, Logger: logger})
	require.NoError(t, err)
	// Register close before the dataDir cleanup so files are released first.
	t.Cleanup(func() { _ = pstore.Close() })

	// Check bucket
	bkt, err := pstore.GetBucket(ctx, "t1", "migrated-bucket")
	require.NoError(t, err, "bucket should be accessible after migration")
	assert.Equal(t, "migrated-bucket", bkt.Name)

	// Check object
	obj, err := pstore.GetObject(ctx, "t1/migrated-bucket", "file.txt")
	require.NoError(t, err, "object should be accessible after migration")
	assert.Equal(t, int64(999), obj.Size)
	assert.Equal(t, "deadbeef", obj.ETag)

	// ── Step 4: second startup should not re-migrate ──────────────────────────
	require.NoError(t, MigrateFromBadgerIfNeeded(dataDir, logger), "second call should be no-op")
}

// TestMigrateIdempotent verifies that when no BadgerDB is present, migration is a no-op.
func TestMigrateIdempotent(t *testing.T) {
	dataDir := t.TempDir()
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// No KEYREGISTRY exists; should return nil immediately
	require.NoError(t, MigrateFromBadgerIfNeeded(dataDir, logger))
}
