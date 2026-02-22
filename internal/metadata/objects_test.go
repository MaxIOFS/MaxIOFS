package metadata

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupObjectTestStore(t *testing.T) (Store, func()) {
	tmpDir, err := os.MkdirTemp("", "metadata-objects-test-*")
	require.NoError(t, err)

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	store, err := NewBadgerStore(BadgerOptions{
		DataDir:           tmpDir,
		SyncWrites:        false,
		CompactionEnabled: false,
		Logger:            logger,
	})
	require.NoError(t, err)
	require.NotNil(t, store)

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

// TestPutAndGetObject tests basic object operations
func TestPutAndGetObject(t *testing.T) {
	store, cleanup := setupObjectTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket first
	bucket := &BucketMetadata{
		Name:      "test-bucket",
		TenantID:  "tenant-1",
		CreatedAt: time.Now(),
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	t.Run("Put and get object", func(t *testing.T) {
		obj := &ObjectMetadata{
			Bucket:       "test-bucket",
			Key:          "test-object.txt",
			Size:         1024,
			ETag:         "abc123",
			ContentType:  "text/plain",
			LastModified: time.Now(),
			Metadata: map[string]string{
				"user-meta": "custom-value",
			},
		}

		err := store.PutObject(ctx, obj)
		assert.NoError(t, err)

		// Retrieve object
		retrieved, err := store.GetObject(ctx, "test-bucket", "test-object.txt")
		assert.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Equal(t, "test-object.txt", retrieved.Key)
		assert.Equal(t, int64(1024), retrieved.Size)
		assert.Equal(t, "abc123", retrieved.ETag)
		assert.Equal(t, "text/plain", retrieved.ContentType)
		assert.Equal(t, "custom-value", retrieved.Metadata["user-meta"])
	})

	t.Run("Get non-existent object", func(t *testing.T) {
		retrieved, err := store.GetObject(ctx, "test-bucket", "does-not-exist.txt")
		assert.Error(t, err)
		assert.Equal(t, ErrObjectNotFound, err)
		assert.Nil(t, retrieved)
	})

	t.Run("Update existing object", func(t *testing.T) {
		obj := &ObjectMetadata{
			Bucket:      "test-bucket",
			Key:         "update-test.txt",
			Size:        100,
			ETag:        "old-etag",
			ContentType: "text/plain",
		}
		err := store.PutObject(ctx, obj)
		require.NoError(t, err)

		// Update
		obj.Size = 200
		obj.ETag = "new-etag"
		err = store.PutObject(ctx, obj)
		assert.NoError(t, err)

		// Verify update
		retrieved, err := store.GetObject(ctx, "test-bucket", "update-test.txt")
		assert.NoError(t, err)
		assert.Equal(t, int64(200), retrieved.Size)
		assert.Equal(t, "new-etag", retrieved.ETag)
	})
}

// TestDeleteObject tests object deletion
func TestDeleteObject(t *testing.T) {
	store, cleanup := setupObjectTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket
	bucket := &BucketMetadata{
		Name:      "delete-bucket",
		TenantID:  "tenant-1",
		CreatedAt: time.Now(),
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	t.Run("Delete existing object", func(t *testing.T) {
		obj := &ObjectMetadata{
			Bucket: "delete-bucket",
			Key:    "to-delete.txt",
			Size:   500,
		}
		err := store.PutObject(ctx, obj)
		require.NoError(t, err)

		// Delete
		err = store.DeleteObject(ctx, "delete-bucket", "to-delete.txt")
		assert.NoError(t, err)

		// Verify deleted
		retrieved, err := store.GetObject(ctx, "delete-bucket", "to-delete.txt")
		assert.Error(t, err)
		assert.Equal(t, ErrObjectNotFound, err)
		assert.Nil(t, retrieved)
	})

	t.Run("Delete non-existent object", func(t *testing.T) {
		err := store.DeleteObject(ctx, "delete-bucket", "never-existed.txt")
		assert.Error(t, err)
		assert.Equal(t, ErrObjectNotFound, err)
	})
}

// TestObjectExists tests existence check
func TestObjectExists(t *testing.T) {
	store, cleanup := setupObjectTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket
	bucket := &BucketMetadata{
		Name:      "exists-bucket",
		TenantID:  "tenant-1",
		CreatedAt: time.Now(),
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	t.Run("Check existing object", func(t *testing.T) {
		obj := &ObjectMetadata{
			Bucket: "exists-bucket",
			Key:    "exists.txt",
			Size:   100,
		}
		err := store.PutObject(ctx, obj)
		require.NoError(t, err)

		exists, err := store.ObjectExists(ctx, "exists-bucket", "exists.txt")
		assert.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("Check non-existent object", func(t *testing.T) {
		exists, err := store.ObjectExists(ctx, "exists-bucket", "not-exists.txt")
		assert.NoError(t, err)
		assert.False(t, exists)
	})
}

// TestListObjects tests object listing
func TestListObjects(t *testing.T) {
	store, cleanup := setupObjectTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket
	bucket := &BucketMetadata{
		Name:      "list-bucket",
		TenantID:  "tenant-1",
		CreatedAt: time.Now(),
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Create test objects
	objects := []string{
		"file1.txt",
		"file2.txt",
		"folder/file3.txt",
		"folder/file4.txt",
		"other/file5.txt",
	}

	for _, key := range objects {
		obj := &ObjectMetadata{
			Bucket: "list-bucket",
			Key:    key,
			Size:   100,
			ETag:   "etag-" + key,
		}
		err := store.PutObject(ctx, obj)
		require.NoError(t, err)
	}

	t.Run("List all objects", func(t *testing.T) {
		objs, nextMarker, err := store.ListObjects(ctx, "list-bucket", "", "", 100)
		assert.NoError(t, err)
		assert.Empty(t, nextMarker)
		assert.Len(t, objs, 5)
	})

	t.Run("List with prefix", func(t *testing.T) {
		objs, _, err := store.ListObjects(ctx, "list-bucket", "folder/", "", 100)
		assert.NoError(t, err)
		assert.Len(t, objs, 2)

		keys := []string{objs[0].Key, objs[1].Key}
		assert.Contains(t, keys, "folder/file3.txt")
		assert.Contains(t, keys, "folder/file4.txt")
	})

	t.Run("List with max keys", func(t *testing.T) {
		objs, nextMarker, err := store.ListObjects(ctx, "list-bucket", "", "", 2)
		assert.NoError(t, err)
		assert.Len(t, objs, 2)
		assert.NotEmpty(t, nextMarker)
	})

	t.Run("List with marker for pagination", func(t *testing.T) {
		// Get first page
		objs1, marker, err := store.ListObjects(ctx, "list-bucket", "", "", 2)
		assert.NoError(t, err)
		assert.Len(t, objs1, 2)
		assert.NotEmpty(t, marker)

		// Get second page
		objs2, _, err := store.ListObjects(ctx, "list-bucket", "", marker, 2)
		assert.NoError(t, err)
		assert.LessOrEqual(t, len(objs2), 2)

		// Keys should be different
		if len(objs2) > 0 {
			assert.NotEqual(t, objs1[0].Key, objs2[0].Key)
		}
	})

	t.Run("List empty prefix", func(t *testing.T) {
		objs, _, err := store.ListObjects(ctx, "list-bucket", "nonexistent/", "", 100)
		assert.NoError(t, err)
		assert.Empty(t, objs)
	})
}

// TestObjectTagsOperations tests object tagging operations
func TestObjectTagsOperations(t *testing.T) {
	store, cleanup := setupObjectTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket
	bucket := &BucketMetadata{
		Name:      "tags-bucket",
		TenantID:  "tenant-1",
		CreatedAt: time.Now(),
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Create object
	obj := &ObjectMetadata{
		Bucket: "tags-bucket",
		Key:    "tagged-object.txt",
		Size:   100,
	}
	err = store.PutObject(ctx, obj)
	require.NoError(t, err)

	t.Run("Put and get tags", func(t *testing.T) {
		tags := map[string]string{
			"environment": "production",
			"owner":       "admin",
			"version":     "1.0",
		}

		err := store.PutObjectTags(ctx, "tags-bucket", "tagged-object.txt", tags)
		assert.NoError(t, err)

		// Retrieve tags
		retrievedTags, err := store.GetObjectTags(ctx, "tags-bucket", "tagged-object.txt")
		assert.NoError(t, err)
		assert.Equal(t, 3, len(retrievedTags))
		assert.Equal(t, "production", retrievedTags["environment"])
		assert.Equal(t, "admin", retrievedTags["owner"])
		assert.Equal(t, "1.0", retrievedTags["version"])
	})

	t.Run("Update tags", func(t *testing.T) {
		newTags := map[string]string{
			"environment": "staging",
		}

		err := store.PutObjectTags(ctx, "tags-bucket", "tagged-object.txt", newTags)
		assert.NoError(t, err)

		retrievedTags, err := store.GetObjectTags(ctx, "tags-bucket", "tagged-object.txt")
		assert.NoError(t, err)
		assert.Equal(t, 1, len(retrievedTags))
		assert.Equal(t, "staging", retrievedTags["environment"])
	})

	t.Run("Delete tags", func(t *testing.T) {
		err := store.DeleteObjectTags(ctx, "tags-bucket", "tagged-object.txt")
		assert.NoError(t, err)

		tags, err := store.GetObjectTags(ctx, "tags-bucket", "tagged-object.txt")
		assert.NoError(t, err)
		assert.Empty(t, tags)
	})

	t.Run("Get tags for non-existent object", func(t *testing.T) {
		tags, err := store.GetObjectTags(ctx, "tags-bucket", "no-tags.txt")
		// Implementation returns error for non-existent objects
		if err != nil {
			assert.Equal(t, ErrObjectNotFound, err)
		} else {
			assert.Empty(t, tags)
		}
	})
}

// TestBucketMetrics tests bucket statistics updates
func TestBucketMetrics(t *testing.T) {
	store, cleanup := setupObjectTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket
	bucket := &BucketMetadata{
		Name:      "metrics-bucket",
		TenantID:  "tenant-1",
		CreatedAt: time.Now(),
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	t.Run("Update bucket metrics - add objects", func(t *testing.T) {
		err := store.UpdateBucketMetrics(ctx, "tenant-1", "metrics-bucket", 3, 3000)
		assert.NoError(t, err)

		count, size, err := store.GetBucketStats(ctx, "tenant-1", "metrics-bucket")
		assert.NoError(t, err)
		assert.Equal(t, int64(3), count)
		assert.Equal(t, int64(3000), size)
	})

	t.Run("Update bucket metrics - remove objects", func(t *testing.T) {
		err := store.UpdateBucketMetrics(ctx, "tenant-1", "metrics-bucket", -1, -1000)
		assert.NoError(t, err)

		count, size, err := store.GetBucketStats(ctx, "tenant-1", "metrics-bucket")
		assert.NoError(t, err)
		assert.Equal(t, int64(2), count)
		assert.Equal(t, int64(2000), size)
	})

	t.Run("Get stats for non-existent bucket", func(t *testing.T) {
		count, size, err := store.GetBucketStats(ctx, "tenant-1", "no-bucket")
		assert.Error(t, err)
		assert.Equal(t, int64(0), count)
		assert.Equal(t, int64(0), size)
	})
}

// TestRecalculateBucketStats tests bucket statistics recalculation
func TestRecalculateBucketStats(t *testing.T) {
	store, cleanup := setupObjectTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket
	bucket := &BucketMetadata{
		Name:      "recalc-bucket",
		TenantID:  "tenant-1",
		CreatedAt: time.Now(),
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Create objects — bucket path must include tenantID prefix, matching production behaviour
	for i := 0; i < 5; i++ {
		obj := &ObjectMetadata{
			Bucket: "tenant-1/recalc-bucket",
			Key:    "file" + string(rune('A'+i)) + ".txt",
			Size:   int64((i + 1) * 100),
		}
		err := store.PutObject(ctx, obj)
		require.NoError(t, err)
	}

	t.Run("Recalculate bucket stats", func(t *testing.T) {
		err := store.RecalculateBucketStats(ctx, "tenant-1", "recalc-bucket")
		assert.NoError(t, err)

		count, size, err := store.GetBucketStats(ctx, "tenant-1", "recalc-bucket")
		assert.NoError(t, err)
		assert.Equal(t, int64(5), count)
		assert.Equal(t, int64(1500), size) // 100+200+300+400+500
	})
}

// TestConcurrentObjectOperations tests thread safety
func TestConcurrentObjectOperations(t *testing.T) {
	store, cleanup := setupObjectTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket
	bucket := &BucketMetadata{
		Name:      "concurrent-bucket",
		TenantID:  "tenant-1",
		CreatedAt: time.Now(),
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	t.Run("Concurrent puts", func(t *testing.T) {
		done := make(chan bool, 20)

		for i := 0; i < 20; i++ {
			go func(n int) {
				obj := &ObjectMetadata{
					Bucket: "concurrent-bucket",
					Key:    "concurrent-" + string(rune('A'+n)) + ".txt",
					Size:   int64(n * 100),
				}
				err := store.PutObject(ctx, obj)
				assert.NoError(t, err)
				done <- true
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < 20; i++ {
			<-done
		}

		// Verify all objects were created
		objs, _, err := store.ListObjects(ctx, "concurrent-bucket", "", "", 100)
		assert.NoError(t, err)
		assert.Equal(t, 20, len(objs))
	})
}
