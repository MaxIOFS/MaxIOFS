package bucket

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteBucket_WithObjects tests that DeleteBucket fails when bucket has objects
// CRITICAL: Prevents accidental data loss
func TestDeleteBucket_WithObjects(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "maxiofs-delete-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Setup storage and metadata
	storageDir := filepath.Join(tempDir, "storage")
	metadataDir := filepath.Join(tempDir, "metadata")

	storageBackend, err := storage.NewBackend(config.StorageConfig{
		Backend: "filesystem",
		Root:    storageDir,
	})
	require.NoError(t, err)

	metadataStore, err := metadata.NewBadgerStore(metadata.BadgerOptions{
		DataDir:           metadataDir,
		SyncWrites:        false,
		CompactionEnabled: false,
		Logger:            logrus.StandardLogger(),
	})
	require.NoError(t, err)
	defer metadataStore.Close()

	manager := NewManager(storageBackend, metadataStore)
	ctx := context.Background()

	// Create bucket
	err = manager.CreateBucket(ctx, "tenant-1", "test-bucket", "")
	require.NoError(t, err)

	// Add an object to the bucket (both physical file AND metadata)
	bucketPath := "tenant-1/test-bucket"
	objectKey := "test-object.txt"
	objectData := strings.NewReader("test data content")

	// Put physical file in storage
	objectPath := bucketPath + "/" + objectKey
	err = storageBackend.Put(ctx, objectPath, objectData, map[string]string{
		"Content-Type": "text/plain",
	})
	require.NoError(t, err)

	// Put metadata in BadgerDB
	objMeta := &metadata.ObjectMetadata{
		Bucket:       bucketPath,
		Key:          objectKey,
		Size:         18,
		ETag:         "test-etag",
		ContentType:  "text/plain",
		LastModified: time.Now(),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	err = metadataStore.PutObject(ctx, objMeta)
	require.NoError(t, err)

	// Try to delete bucket - should fail with ErrBucketNotEmpty
	err = manager.DeleteBucket(ctx, "tenant-1", "test-bucket")
	assert.Error(t, err, "DeleteBucket should fail when bucket has objects")
	assert.ErrorIs(t, err, ErrBucketNotEmpty, "Error should be ErrBucketNotEmpty")

	// Verify bucket still exists
	exists, err := manager.BucketExists(ctx, "tenant-1", "test-bucket")
	require.NoError(t, err)
	assert.True(t, exists, "Bucket should still exist after failed delete")
}

// TestDeleteBucket_CleansStorage tests that DeleteBucket removes physical storage
// CRITICAL: Prevents storage leaks
func TestDeleteBucket_CleansStorage(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "maxiofs-delete-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	storageDir := filepath.Join(tempDir, "storage")
	metadataDir := filepath.Join(tempDir, "metadata")

	storageBackend, err := storage.NewBackend(config.StorageConfig{
		Backend: "filesystem",
		Root:    storageDir,
	})
	require.NoError(t, err)

	metadataStore, err := metadata.NewBadgerStore(metadata.BadgerOptions{
		DataDir:           metadataDir,
		SyncWrites:        false,
		CompactionEnabled: false,
		Logger:            logrus.StandardLogger(),
	})
	require.NoError(t, err)
	defer metadataStore.Close()

	manager := NewManager(storageBackend, metadataStore)
	ctx := context.Background()

	// Create bucket
	err = manager.CreateBucket(ctx, "tenant-1", "storage-test-bucket", "")
	require.NoError(t, err)

	// Verify bucket directory was created
	bucketDir := filepath.Join(storageDir, "tenant-1", "storage-test-bucket")
	_, err = os.Stat(bucketDir)
	assert.NoError(t, err, "Bucket directory should exist after creation")

	// Delete empty bucket
	err = manager.DeleteBucket(ctx, "tenant-1", "storage-test-bucket")
	require.NoError(t, err, "DeleteBucket should succeed for empty bucket")

	// Verify bucket directory was removed
	_, err = os.Stat(bucketDir)
	assert.True(t, os.IsNotExist(err), "Bucket directory should be removed after deletion")

	// Verify bucket is gone from metadata
	exists, err := manager.BucketExists(ctx, "tenant-1", "storage-test-bucket")
	require.NoError(t, err)
	assert.False(t, exists, "Bucket should not exist in metadata after deletion")
}

// TestForceDeleteBucket_DeletesAllObjects tests that ForceDeleteBucket removes all objects
// CRITICAL: Prevents storage leaks and orphaned data
func TestForceDeleteBucket_DeletesAllObjects(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "maxiofs-force-delete-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	storageDir := filepath.Join(tempDir, "storage")
	metadataDir := filepath.Join(tempDir, "metadata")

	storageBackend, err := storage.NewBackend(config.StorageConfig{
		Backend: "filesystem",
		Root:    storageDir,
	})
	require.NoError(t, err)

	metadataStore, err := metadata.NewBadgerStore(metadata.BadgerOptions{
		DataDir:           metadataDir,
		SyncWrites:        false,
		CompactionEnabled: false,
		Logger:            logrus.StandardLogger(),
	})
	require.NoError(t, err)
	defer metadataStore.Close()

	manager := NewManager(storageBackend, metadataStore)
	ctx := context.Background()

	// Create bucket
	err = manager.CreateBucket(ctx, "tenant-1", "force-delete-bucket", "")
	require.NoError(t, err)

	bucketPath := "tenant-1/force-delete-bucket"

	// Add multiple objects to the bucket (both physical files AND metadata)
	// Use simple names without subdirectories to ensure they get created
	objects := []string{"object1.txt", "object2.txt", "object3.txt", "object4.txt", "object5.txt"}
	for _, objectKey := range objects {
		objectPath := bucketPath + "/" + objectKey
		objectData := strings.NewReader("test data for " + objectKey)

		// Put physical file in storage
		err = storageBackend.Put(ctx, objectPath, objectData, map[string]string{
			"Content-Type": "text/plain",
		})
		require.NoError(t, err, "Should create physical file: "+objectKey)

		// Put metadata in BadgerDB
		objMeta := &metadata.ObjectMetadata{
			Bucket:       bucketPath,
			Key:          objectKey,
			Size:         int64(len("test data for " + objectKey)),
			ETag:         "test-etag-" + objectKey,
			ContentType:  "text/plain",
			LastModified: time.Now(),
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		err = metadataStore.PutObject(ctx, objMeta)
		require.NoError(t, err, "Should create metadata: "+objectKey)
	}

	// Verify objects exist in storage before ForceDelete
	objectsInBucket, err := storageBackend.List(ctx, bucketPath+"/", false)
	require.NoError(t, err)
	actualObjectCount := 0
	for _, obj := range objectsInBucket {
		if !strings.HasSuffix(obj.Path, ".maxiofs-bucket") && !strings.Contains(obj.Path, "/.maxiofs-") {
			actualObjectCount++
		}
	}
	assert.Equal(t, len(objects), actualObjectCount, "All objects should exist before ForceDelete")

	// Force delete bucket with all objects
	err = manager.ForceDeleteBucket(ctx, "tenant-1", "force-delete-bucket")
	require.NoError(t, err, "ForceDeleteBucket should succeed")

	// Verify bucket is gone from metadata
	exists, err := manager.BucketExists(ctx, "tenant-1", "force-delete-bucket")
	require.NoError(t, err)
	assert.False(t, exists, "Bucket should not exist in metadata after ForceDelete")

	// Verify all objects are deleted from storage
	bucketDir := filepath.Join(storageDir, "tenant-1", "force-delete-bucket")
	_, err = os.Stat(bucketDir)
	assert.True(t, os.IsNotExist(err), "Bucket directory should be completely removed")
}

// TestForceDeleteBucket_CleansMetadata tests that ForceDeleteBucket removes all metadata
// CRITICAL: Prevents metadata corruption and orphaned entries
func TestForceDeleteBucket_CleansMetadata(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "maxiofs-force-delete-metadata-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	storageDir := filepath.Join(tempDir, "storage")
	metadataDir := filepath.Join(tempDir, "metadata")

	storageBackend, err := storage.NewBackend(config.StorageConfig{
		Backend: "filesystem",
		Root:    storageDir,
	})
	require.NoError(t, err)

	metadataStore, err := metadata.NewBadgerStore(metadata.BadgerOptions{
		DataDir:           metadataDir,
		SyncWrites:        false,
		CompactionEnabled: false,
		Logger:            logrus.StandardLogger(),
	})
	require.NoError(t, err)
	defer metadataStore.Close()

	manager := NewManager(storageBackend, metadataStore)
	ctx := context.Background()

	// Create bucket
	err = manager.CreateBucket(ctx, "tenant-1", "metadata-test-bucket", "")
	require.NoError(t, err)

	// Verify bucket exists in metadata before deletion
	bucketInfo, err := manager.GetBucketInfo(ctx, "tenant-1", "metadata-test-bucket")
	require.NoError(t, err)
	assert.Equal(t, "metadata-test-bucket", bucketInfo.Name)

	// Force delete bucket
	err = manager.ForceDeleteBucket(ctx, "tenant-1", "metadata-test-bucket")
	require.NoError(t, err)

	// Verify bucket is completely removed from metadata
	_, err = manager.GetBucketInfo(ctx, "tenant-1", "metadata-test-bucket")
	assert.Error(t, err, "GetBucketInfo should fail after ForceDelete")
	assert.ErrorIs(t, err, ErrBucketNotFound, "Error should be ErrBucketNotFound")

	// Verify bucket doesn't appear in list
	buckets, err := manager.ListBuckets(ctx, "tenant-1")
	require.NoError(t, err)
	for _, b := range buckets {
		assert.NotEqual(t, "metadata-test-bucket", b.Name, "Deleted bucket should not appear in list")
	}
}

// TestForceDeleteBucket_NonExistent tests ForceDeleteBucket on non-existent bucket
func TestForceDeleteBucket_NonExistent(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "maxiofs-force-delete-nonexistent-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	storageDir := filepath.Join(tempDir, "storage")
	metadataDir := filepath.Join(tempDir, "metadata")

	storageBackend, err := storage.NewBackend(config.StorageConfig{
		Backend: "filesystem",
		Root:    storageDir,
	})
	require.NoError(t, err)

	metadataStore, err := metadata.NewBadgerStore(metadata.BadgerOptions{
		DataDir:           metadataDir,
		SyncWrites:        false,
		CompactionEnabled: false,
		Logger:            logrus.StandardLogger(),
	})
	require.NoError(t, err)
	defer metadataStore.Close()

	manager := NewManager(storageBackend, metadataStore)
	ctx := context.Background()

	// Try to force delete non-existent bucket
	err = manager.ForceDeleteBucket(ctx, "tenant-1", "nonexistent-bucket")
	assert.Error(t, err, "ForceDeleteBucket should fail for non-existent bucket")
	assert.ErrorIs(t, err, ErrBucketNotFound, "Error should be ErrBucketNotFound")
}
