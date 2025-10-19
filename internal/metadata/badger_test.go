package metadata

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) (*BadgerStore, func()) {
	// Create temporary directory
	tmpDir := filepath.Join(os.TempDir(), "badger_test_"+time.Now().Format("20060102150405"))
	err := os.MkdirAll(tmpDir, 0755)
	require.NoError(t, err)

	// Create logger
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	// Create store
	store, err := NewBadgerStore(BadgerOptions{
		DataDir:           tmpDir,
		SyncWrites:        false, // Faster for tests
		CompactionEnabled: false, // Disable for tests
		Logger:            logger,
	})
	require.NoError(t, err)
	require.NotNil(t, store)

	// Return cleanup function
	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

// TestBucketOperations tests basic bucket CRUD operations
func TestBucketOperations(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("CreateBucket", func(t *testing.T) {
		bucket := &BucketMetadata{
			Name:      "test-bucket",
			TenantID:  "tenant1",
			OwnerID:   "user1",
			OwnerType: "user",
			Region:    "us-east-1",
		}

		err := store.CreateBucket(ctx, bucket)
		assert.NoError(t, err)
		assert.False(t, bucket.CreatedAt.IsZero())
	})

	t.Run("GetBucket", func(t *testing.T) {
		bucket, err := store.GetBucket(ctx, "tenant1", "test-bucket")
		assert.NoError(t, err)
		assert.NotNil(t, bucket)
		assert.Equal(t, "test-bucket", bucket.Name)
		assert.Equal(t, "tenant1", bucket.TenantID)
	})

	t.Run("BucketExists", func(t *testing.T) {
		exists, err := store.BucketExists(ctx, "tenant1", "test-bucket")
		assert.NoError(t, err)
		assert.True(t, exists)

		exists, err = store.BucketExists(ctx, "tenant1", "non-existent")
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("ListBuckets", func(t *testing.T) {
		// Create another bucket
		bucket2 := &BucketMetadata{
			Name:      "test-bucket-2",
			TenantID:  "tenant1",
			OwnerID:   "user1",
			OwnerType: "user",
		}
		err := store.CreateBucket(ctx, bucket2)
		assert.NoError(t, err)

		buckets, err := store.ListBuckets(ctx, "tenant1")
		assert.NoError(t, err)
		assert.Len(t, buckets, 2)
	})

	t.Run("UpdateBucketMetrics", func(t *testing.T) {
		err := store.UpdateBucketMetrics(ctx, "tenant1", "test-bucket", 5, 1024)
		assert.NoError(t, err)

		bucket, err := store.GetBucket(ctx, "tenant1", "test-bucket")
		assert.NoError(t, err)
		assert.Equal(t, int64(5), bucket.ObjectCount)
		assert.Equal(t, int64(1024), bucket.TotalSize)
	})

	t.Run("DeleteBucket", func(t *testing.T) {
		err := store.DeleteBucket(ctx, "tenant1", "test-bucket")
		assert.NoError(t, err)

		_, err = store.GetBucket(ctx, "tenant1", "test-bucket")
		assert.ErrorIs(t, err, ErrBucketNotFound)
	})
}

// TestObjectOperations tests basic object CRUD operations
func TestObjectOperations(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// First create a bucket
	bucket := &BucketMetadata{
		Name:      "test-bucket",
		TenantID:  "tenant1",
		OwnerID:   "user1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	t.Run("PutObject", func(t *testing.T) {
		obj := &ObjectMetadata{
			Bucket:      "test-bucket",
			Key:         "test-object.txt",
			Size:        1024,
			ETag:        "abc123",
			ContentType: "text/plain",
			Metadata: map[string]string{
				"user-meta": "value1",
			},
		}

		err := store.PutObject(ctx, obj)
		assert.NoError(t, err)
		assert.False(t, obj.CreatedAt.IsZero())
	})

	t.Run("GetObject", func(t *testing.T) {
		obj, err := store.GetObject(ctx, "test-bucket", "test-object.txt")
		assert.NoError(t, err)
		assert.NotNil(t, obj)
		assert.Equal(t, "test-object.txt", obj.Key)
		assert.Equal(t, int64(1024), obj.Size)
		assert.Equal(t, "value1", obj.Metadata["user-meta"])
	})

	t.Run("ObjectExists", func(t *testing.T) {
		exists, err := store.ObjectExists(ctx, "test-bucket", "test-object.txt")
		assert.NoError(t, err)
		assert.True(t, exists)

		exists, err = store.ObjectExists(ctx, "test-bucket", "non-existent.txt")
		assert.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("ListObjects", func(t *testing.T) {
		// Create more objects
		for i := 0; i < 5; i++ {
			obj := &ObjectMetadata{
				Bucket:      "test-bucket",
				Key:         "folder/file" + string(rune('0'+i)) + ".txt",
				Size:        100,
				ETag:        "etag",
				ContentType: "text/plain",
			}
			err := store.PutObject(ctx, obj)
			require.NoError(t, err)
		}

		// List all objects
		objects, nextMarker, err := store.ListObjects(ctx, "test-bucket", "", "", 10)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(objects), 5)
		assert.Empty(t, nextMarker)

		// List with prefix
		objects, _, err = store.ListObjects(ctx, "test-bucket", "folder/", "", 10)
		assert.NoError(t, err)
		assert.Len(t, objects, 5)
	})

	t.Run("DeleteObject", func(t *testing.T) {
		err := store.DeleteObject(ctx, "test-bucket", "test-object.txt")
		assert.NoError(t, err)

		_, err = store.GetObject(ctx, "test-bucket", "test-object.txt")
		assert.ErrorIs(t, err, ErrObjectNotFound)
	})
}

// TestObjectTags tests object tagging operations
func TestObjectTags(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Setup
	bucket := &BucketMetadata{
		Name:      "test-bucket",
		TenantID:  "tenant1",
		OwnerID:   "user1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	obj := &ObjectMetadata{
		Bucket:      "test-bucket",
		Key:         "tagged-object.txt",
		Size:        1024,
		ETag:        "abc123",
		ContentType: "text/plain",
	}
	err = store.PutObject(ctx, obj)
	require.NoError(t, err)

	t.Run("PutObjectTags", func(t *testing.T) {
		tags := map[string]string{
			"Environment": "Production",
			"Team":        "Backend",
		}

		err := store.PutObjectTags(ctx, "test-bucket", "tagged-object.txt", tags)
		assert.NoError(t, err)
	})

	t.Run("GetObjectTags", func(t *testing.T) {
		tags, err := store.GetObjectTags(ctx, "test-bucket", "tagged-object.txt")
		assert.NoError(t, err)
		assert.Len(t, tags, 2)
		assert.Equal(t, "Production", tags["Environment"])
		assert.Equal(t, "Backend", tags["Team"])
	})

	t.Run("ListObjectsByTags", func(t *testing.T) {
		// Create another object with same tags
		obj2 := &ObjectMetadata{
			Bucket:      "test-bucket",
			Key:         "tagged-object-2.txt",
			Size:        2048,
			ETag:        "def456",
			ContentType: "text/plain",
			Tags: map[string]string{
				"Environment": "Production",
				"Team":        "Frontend",
			},
		}
		err := store.PutObject(ctx, obj2)
		require.NoError(t, err)

		// Search by tag
		objects, err := store.ListObjectsByTags(ctx, "test-bucket", map[string]string{
			"Environment": "Production",
		})
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(objects), 2)
	})

	t.Run("DeleteObjectTags", func(t *testing.T) {
		err := store.DeleteObjectTags(ctx, "test-bucket", "tagged-object.txt")
		assert.NoError(t, err)

		tags, err := store.GetObjectTags(ctx, "test-bucket", "tagged-object.txt")
		assert.NoError(t, err)
		assert.Empty(t, tags)
	})
}

// TestMultipartUpload tests multipart upload operations
func TestMultipartUpload(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Setup
	bucket := &BucketMetadata{
		Name:      "test-bucket",
		TenantID:  "tenant1",
		OwnerID:   "user1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	uploadID := "test-upload-123"

	t.Run("CreateMultipartUpload", func(t *testing.T) {
		upload := &MultipartUploadMetadata{
			UploadID:    uploadID,
			Bucket:      "test-bucket",
			Key:         "large-file.bin",
			ContentType: "application/octet-stream",
			OwnerID:     "user1",
		}

		err := store.CreateMultipartUpload(ctx, upload)
		assert.NoError(t, err)
	})

	t.Run("GetMultipartUpload", func(t *testing.T) {
		upload, err := store.GetMultipartUpload(ctx, uploadID)
		assert.NoError(t, err)
		assert.NotNil(t, upload)
		assert.Equal(t, "large-file.bin", upload.Key)
	})

	t.Run("PutPart", func(t *testing.T) {
		part := &PartMetadata{
			UploadID:   uploadID,
			PartNumber: 1,
			Size:       5242880, // 5MB
			ETag:       "part1-etag",
		}

		err := store.PutPart(ctx, part)
		assert.NoError(t, err)
	})

	t.Run("ListParts", func(t *testing.T) {
		// Add more parts
		for i := 2; i <= 3; i++ {
			part := &PartMetadata{
				UploadID:   uploadID,
				PartNumber: i,
				Size:       5242880,
				ETag:       "part-etag",
			}
			err := store.PutPart(ctx, part)
			require.NoError(t, err)
		}

		parts, err := store.ListParts(ctx, uploadID)
		assert.NoError(t, err)
		assert.Len(t, parts, 3)
		assert.Equal(t, 1, parts[0].PartNumber)
		assert.Equal(t, 2, parts[1].PartNumber)
	})

	t.Run("AbortMultipartUpload", func(t *testing.T) {
		err := store.AbortMultipartUpload(ctx, uploadID)
		assert.NoError(t, err)

		_, err = store.GetMultipartUpload(ctx, uploadID)
		assert.ErrorIs(t, err, ErrUploadNotFound)
	})
}
