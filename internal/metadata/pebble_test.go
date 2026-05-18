package metadata

import (
	"context"
	"fmt"
	"os"
	"testing"

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

func TestPebbleStoreDeleteBucketIfEmptyRejectsLateObjectWrite(t *testing.T) {
	store, cleanup := setupPebbleTestStore(t)
	defer cleanup()

	ctx := context.Background()
	bucket := &BucketMetadata{Name: "delete-race-bucket", OwnerID: "u", OwnerType: "user"}
	require.NoError(t, store.CreateBucket(ctx, bucket))
	require.NoError(t, store.DeleteBucketIfEmpty(ctx, "", bucket.Name))

	err := store.PutObject(ctx, &ObjectMetadata{
		Bucket:      bucket.Name,
		Key:         "late-object.txt",
		Size:        10,
		ETag:        "etag",
		ContentType: "text/plain",
	})
	require.ErrorIs(t, err, ErrBucketNotFound)

	_, err = store.GetObject(ctx, bucket.Name, "late-object.txt")
	require.ErrorIs(t, err, ErrObjectNotFound)

	require.NoError(t, store.CreateBucket(ctx, bucket))
	require.NoError(t, store.PutObject(ctx, &ObjectMetadata{
		Bucket:      bucket.Name,
		Key:         "new-object.txt",
		Size:        10,
		ETag:        "etag",
		ContentType: "text/plain",
	}))
}

