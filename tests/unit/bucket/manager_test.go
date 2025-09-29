package bucket

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBucketManager(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "maxiofs-bucket-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create storage backend
	storageConfig := config.StorageConfig{
		Root: tempDir,
	}

	backend, err := storage.NewFilesystemBackend(storageConfig)
	require.NoError(t, err)

	// Create bucket manager
	manager := bucket.NewManager(backend)
	require.NotNil(t, manager)

	// Test basic operations
	t.Run("CreateAndListBuckets", func(t *testing.T) {
		testCreateAndListBuckets(t, manager)
	})

	t.Run("BucketExists", func(t *testing.T) {
		testBucketExists(t, manager)
	})

	t.Run("GetBucketInfo", func(t *testing.T) {
		testGetBucketInfo(t, manager)
	})

	t.Run("DeleteBucket", func(t *testing.T) {
		testDeleteBucket(t, manager)
	})

	t.Run("BucketNameValidation", func(t *testing.T) {
		testBucketNameValidation(t, manager)
	})

	t.Run("BucketAlreadyExists", func(t *testing.T) {
		testBucketAlreadyExists(t, manager)
	})
}

func testCreateAndListBuckets(t *testing.T, manager bucket.Manager) {
	ctx := context.Background()

	// Initially no buckets
	buckets, err := manager.ListBuckets(ctx)
	require.NoError(t, err)
	initialCount := len(buckets)

	// Create test buckets
	testBuckets := []string{"test-bucket-1", "test-bucket-2", "my-data-bucket"}

	for _, bucketName := range testBuckets {
		err := manager.CreateBucket(ctx, bucketName)
		assert.NoError(t, err, "Failed to create bucket %s", bucketName)
	}

	// List buckets and verify
	buckets, err = manager.ListBuckets(ctx)
	require.NoError(t, err)
	assert.Len(t, buckets, initialCount+len(testBuckets))

	// Verify all test buckets are in the list
	bucketNames := make(map[string]bool)
	for _, bucket := range buckets {
		bucketNames[bucket.Name] = true
		// Check bucket properties
		assert.NotEmpty(t, bucket.Name)
		assert.False(t, bucket.CreatedAt.IsZero())
		assert.Equal(t, "us-east-1", bucket.Region)
		assert.NotNil(t, bucket.Metadata)
	}

	for _, expectedBucket := range testBuckets {
		assert.True(t, bucketNames[expectedBucket], "Bucket %s not found in list", expectedBucket)
	}
}

func testBucketExists(t *testing.T, manager bucket.Manager) {
	ctx := context.Background()
	bucketName := "existence-test-bucket"

	// Should not exist initially
	exists, err := manager.BucketExists(ctx, bucketName)
	require.NoError(t, err)
	assert.False(t, exists)

	// Create bucket
	err = manager.CreateBucket(ctx, bucketName)
	require.NoError(t, err)

	// Should exist now
	exists, err = manager.BucketExists(ctx, bucketName)
	require.NoError(t, err)
	assert.True(t, exists)
}

func testGetBucketInfo(t *testing.T, manager bucket.Manager) {
	ctx := context.Background()
	bucketName := "info-test-bucket"

	// Should not exist initially
	_, err := manager.GetBucketInfo(ctx, bucketName)
	assert.Equal(t, bucket.ErrBucketNotFound, err)

	// Create bucket
	err = manager.CreateBucket(ctx, bucketName)
	require.NoError(t, err)

	// Get bucket info
	bucketInfo, err := manager.GetBucketInfo(ctx, bucketName)
	require.NoError(t, err)
	require.NotNil(t, bucketInfo)

	assert.Equal(t, bucketName, bucketInfo.Name)
	assert.False(t, bucketInfo.CreatedAt.IsZero())
	assert.Equal(t, "us-east-1", bucketInfo.Region)
	assert.NotNil(t, bucketInfo.Metadata)
}

func testDeleteBucket(t *testing.T, manager bucket.Manager) {
	ctx := context.Background()
	bucketName := "delete-test-bucket"

	// Create bucket
	err := manager.CreateBucket(ctx, bucketName)
	require.NoError(t, err)

	// Verify it exists
	exists, err := manager.BucketExists(ctx, bucketName)
	require.NoError(t, err)
	assert.True(t, exists)

	// Delete bucket
	err = manager.DeleteBucket(ctx, bucketName)
	assert.NoError(t, err)

	// Verify it doesn't exist
	exists, err = manager.BucketExists(ctx, bucketName)
	require.NoError(t, err)
	assert.False(t, exists)

	// Try to delete non-existent bucket
	err = manager.DeleteBucket(ctx, "non-existent-bucket")
	assert.Equal(t, bucket.ErrBucketNotFound, err)
}

func testBucketNameValidation(t *testing.T, manager bucket.Manager) {
	ctx := context.Background()

	// Invalid bucket names
	invalidNames := []string{
		"",                    // empty
		"ab",                  // too short
		"a",                   // too short
		"UPPERCASE",           // uppercase not allowed
		"bucket_with_underscores", // underscores not allowed
		"bucket..name",        // consecutive dots
		"bucket--name",        // consecutive dashes
		"-bucket",             // starts with dash
		"bucket-",             // ends with dash
		"192.168.1.1",        // IP address format
		"xn--bucket",          // starts with xn--
		"bucket-s3alias",      // ends with -s3alias
	}

	for _, invalidName := range invalidNames {
		err := manager.CreateBucket(ctx, invalidName)
		assert.Error(t, err, "Bucket name %s should be invalid", invalidName)
		assert.ErrorIs(t, err, bucket.ErrInvalidBucketName, "Wrong error type for %s", invalidName)
	}

	// Valid bucket names
	validNames := []string{
		"valid-bucket-name",
		"bucket123",
		"my-data-bucket-2023",
		"abc",                     // minimum length
		"a" + strings.Repeat("b", 61) + "z", // maximum length (63 chars)
	}

	for _, validName := range validNames {
		err := manager.CreateBucket(ctx, validName)
		assert.NoError(t, err, "Bucket name %s should be valid", validName)

		// Clean up
		manager.DeleteBucket(ctx, validName)
	}
}

func testBucketAlreadyExists(t *testing.T, manager bucket.Manager) {
	ctx := context.Background()
	bucketName := "duplicate-bucket"

	// Create bucket first time
	err := manager.CreateBucket(ctx, bucketName)
	require.NoError(t, err)

	// Try to create same bucket again
	err = manager.CreateBucket(ctx, bucketName)
	assert.Equal(t, bucket.ErrBucketAlreadyExists, err)

	// Clean up
	manager.DeleteBucket(ctx, bucketName)
}