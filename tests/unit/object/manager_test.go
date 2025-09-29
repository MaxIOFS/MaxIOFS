package object

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestObjectManager(t *testing.T) (object.Manager, bucket.Manager, func()) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "maxiofs-object-test")
	require.NoError(t, err)

	// Create storage backend
	storageConfig := config.StorageConfig{
		Root: tempDir,
	}

	backend, err := storage.NewFilesystemBackend(storageConfig)
	require.NoError(t, err)

	// Create managers
	bucketManager := bucket.NewManager(backend)
	objectManager := object.NewManager(backend, storageConfig)

	// Cleanup function
	cleanup := func() {
		os.RemoveAll(tempDir)
	}

	return objectManager, bucketManager, cleanup
}

func TestObjectManager(t *testing.T) {
	objectManager, bucketManager, cleanup := setupTestObjectManager(t)
	defer cleanup()

	// Create test bucket
	ctx := context.Background()
	testBucket := "test-bucket"
	err := bucketManager.CreateBucket(ctx, testBucket)
	require.NoError(t, err)

	// Test basic operations
	t.Run("PutAndGetObject", func(t *testing.T) {
		testPutAndGetObject(t, objectManager, testBucket)
	})

	t.Run("GetObjectMetadata", func(t *testing.T) {
		testGetObjectMetadata(t, objectManager, testBucket)
	})

	t.Run("UpdateObjectMetadata", func(t *testing.T) {
		testUpdateObjectMetadata(t, objectManager, testBucket)
	})

	t.Run("DeleteObject", func(t *testing.T) {
		testDeleteObject(t, objectManager, testBucket)
	})

	t.Run("ListObjects", func(t *testing.T) {
		testListObjects(t, objectManager, testBucket)
	})

	t.Run("ObjectNameValidation", func(t *testing.T) {
		testObjectNameValidation(t, objectManager, testBucket)
	})

	t.Run("ObjectNotFound", func(t *testing.T) {
		testObjectNotFound(t, objectManager, testBucket)
	})

	t.Run("MultipartUpload", func(t *testing.T) {
		testMultipartUpload(t, objectManager, testBucket)
	})
}

func testPutAndGetObject(t *testing.T, manager object.Manager, bucket string) {
	ctx := context.Background()
	testKey := "test/object.txt"
	testData := "Hello MaxIOFS Object Manager!"

	// Create headers
	headers := http.Header{
		"Content-Type": []string{"text/plain"},
		"Cache-Control": []string{"no-cache"},
		"X-Amz-Meta-User": []string{"test-user"},
	}

	// Put object
	obj, err := manager.PutObject(ctx, bucket, testKey, strings.NewReader(testData), headers)
	require.NoError(t, err)
	require.NotNil(t, obj)

	// Verify object properties
	assert.Equal(t, testKey, obj.Key)
	assert.Equal(t, bucket, obj.Bucket)
	assert.Equal(t, int64(len(testData)), obj.Size)
	assert.Equal(t, "text/plain", obj.ContentType)
	assert.NotEmpty(t, obj.ETag)
	assert.False(t, obj.LastModified.IsZero())
	assert.Equal(t, object.StorageClassStandard, obj.StorageClass)
	assert.NotNil(t, obj.Metadata)
	assert.Equal(t, "text/plain", obj.Metadata["content-type"])
	assert.Equal(t, "test-user", obj.Metadata["x-amz-meta-user"])

	// Get object
	retrievedObj, reader, err := manager.GetObject(ctx, bucket, testKey)
	require.NoError(t, err)
	require.NotNil(t, retrievedObj)
	require.NotNil(t, reader)
	defer reader.Close()

	// Read and verify data
	buf := make([]byte, len(testData))
	n, err := reader.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, len(testData), n)
	assert.Equal(t, testData, string(buf[:n]))

	// Verify retrieved object properties
	assert.Equal(t, testKey, retrievedObj.Key)
	assert.Equal(t, bucket, retrievedObj.Bucket)
	assert.Equal(t, int64(len(testData)), retrievedObj.Size)
	assert.NotEmpty(t, retrievedObj.ETag)
	assert.False(t, retrievedObj.LastModified.IsZero())
}

func testGetObjectMetadata(t *testing.T, manager object.Manager, bucket string) {
	ctx := context.Background()
	testKey := "test/metadata-object.txt"
	testData := "Metadata test"

	headers := http.Header{
		"Content-Type": []string{"application/json"},
		"X-Amz-Meta-Author": []string{"test-author"},
	}

	// Put object
	_, err := manager.PutObject(ctx, bucket, testKey, strings.NewReader(testData), headers)
	require.NoError(t, err)

	// Get object metadata
	obj, err := manager.GetObjectMetadata(ctx, bucket, testKey)
	require.NoError(t, err)
	require.NotNil(t, obj)

	// Verify metadata
	assert.Equal(t, testKey, obj.Key)
	assert.Equal(t, bucket, obj.Bucket)
	assert.Equal(t, int64(len(testData)), obj.Size)
	assert.Equal(t, "application/json", obj.ContentType)
	assert.NotEmpty(t, obj.ETag)
	assert.False(t, obj.LastModified.IsZero())
}

func testUpdateObjectMetadata(t *testing.T, manager object.Manager, bucket string) {
	ctx := context.Background()
	testKey := "test/update-metadata.txt"
	testData := "Update metadata test"

	// Put object
	_, err := manager.PutObject(ctx, bucket, testKey, strings.NewReader(testData), http.Header{
		"Content-Type": []string{"text/plain"},
	})
	require.NoError(t, err)

	// Update metadata
	newMetadata := map[string]string{
		"content-type": "text/html",
		"x-amz-meta-updated": "true",
		"cache-control": "max-age=3600",
	}

	err = manager.UpdateObjectMetadata(ctx, bucket, testKey, newMetadata)
	require.NoError(t, err)

	// Get updated metadata
	obj, err := manager.GetObjectMetadata(ctx, bucket, testKey)
	require.NoError(t, err)

	assert.Equal(t, "text/html", obj.ContentType)
	assert.Equal(t, "true", obj.Metadata["x-amz-meta-updated"])
	assert.Equal(t, "max-age=3600", obj.Metadata["cache-control"])
}

func testDeleteObject(t *testing.T, manager object.Manager, bucket string) {
	ctx := context.Background()
	testKey := "test/delete-me.txt"
	testData := "Delete me!"

	// Put object
	_, err := manager.PutObject(ctx, bucket, testKey, strings.NewReader(testData), http.Header{})
	require.NoError(t, err)

	// Verify object exists
	_, err = manager.GetObjectMetadata(ctx, bucket, testKey)
	require.NoError(t, err)

	// Delete object
	err = manager.DeleteObject(ctx, bucket, testKey)
	require.NoError(t, err)

	// Verify object doesn't exist
	_, err = manager.GetObjectMetadata(ctx, bucket, testKey)
	assert.Equal(t, object.ErrObjectNotFound, err)

	// Try to delete non-existent object
	err = manager.DeleteObject(ctx, bucket, "non-existent.txt")
	assert.Equal(t, object.ErrObjectNotFound, err)
}

func testListObjects(t *testing.T, manager object.Manager, bucket string) {
	ctx := context.Background()

	// Put multiple objects
	testObjects := map[string]string{
		"list/file1.txt":     "content1",
		"list/file2.txt":     "content2",
		"list/sub/file3.txt": "content3",
		"other/file4.txt":    "content4",
		"data.json":          `{"test": true}`,
	}

	for key, content := range testObjects {
		_, err := manager.PutObject(ctx, bucket, key, strings.NewReader(content), http.Header{
			"Content-Type": []string{"text/plain"},
		})
		require.NoError(t, err)
	}

	// List all objects
	objects, isTruncated, err := manager.ListObjects(ctx, bucket, "", "", "", 1000)
	require.NoError(t, err)
	assert.False(t, isTruncated)
	assert.GreaterOrEqual(t, len(objects), len(testObjects))

	// Verify objects are sorted
	for i := 1; i < len(objects); i++ {
		assert.True(t, objects[i-1].Key <= objects[i].Key, "Objects should be sorted by key")
	}

	// List with prefix
	objects, isTruncated, err = manager.ListObjects(ctx, bucket, "list/", "", "", 1000)
	require.NoError(t, err)
	assert.False(t, isTruncated)
	assert.GreaterOrEqual(t, len(objects), 3) // file1.txt, file2.txt, sub/file3.txt

	for _, obj := range objects {
		assert.True(t, strings.HasPrefix(obj.Key, "list/"), "All objects should have 'list/' prefix")
		assert.Equal(t, bucket, obj.Bucket)
		assert.Greater(t, obj.Size, int64(0))
		assert.NotEmpty(t, obj.ETag)
		assert.False(t, obj.LastModified.IsZero())
	}

	// List with maxKeys limit
	objects, isTruncated, err = manager.ListObjects(ctx, bucket, "", "", "", 2)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(objects), 2)
	if len(objects) == 2 {
		assert.True(t, isTruncated)
	}
}

func testObjectNameValidation(t *testing.T, manager object.Manager, bucket string) {
	ctx := context.Background()

	// Invalid object names
	invalidNames := []string{
		"",                    // empty
		"/absolute/path",      // absolute path
		"../etc/passwd",       // path traversal
		"test/../passwd",      // path traversal
		strings.Repeat("a", 1025), // too long
	}

	for _, invalidName := range invalidNames {
		_, err := manager.PutObject(ctx, bucket, invalidName, strings.NewReader("test"), http.Header{})
		assert.Equal(t, object.ErrInvalidObjectName, err, "Object name %s should be invalid", invalidName)

		_, _, err = manager.GetObject(ctx, bucket, invalidName)
		assert.Equal(t, object.ErrInvalidObjectName, err, "Object name %s should be invalid", invalidName)

		err = manager.DeleteObject(ctx, bucket, invalidName)
		assert.Equal(t, object.ErrInvalidObjectName, err, "Object name %s should be invalid", invalidName)
	}

	// Valid object names
	validNames := []string{
		"simple.txt",
		"path/to/file.txt",
		"very/deep/path/to/file.txt",
		"file-with-dashes.txt",
		"file_with_underscores.txt",
		"file.with.dots.txt",
		"UPPERCASE.txt",
		"123numbers.txt",
		"file+with+plus.txt",
		"file%20encoded.txt",
	}

	for _, validName := range validNames {
		_, err := manager.PutObject(ctx, bucket, validName, strings.NewReader("test"), http.Header{})
		assert.NoError(t, err, "Object name %s should be valid", validName)

		// Clean up
		manager.DeleteObject(ctx, bucket, validName)
	}
}

func testObjectNotFound(t *testing.T, manager object.Manager, bucket string) {
	ctx := context.Background()
	nonExistentKey := "non-existent-object.txt"

	// Get non-existent object
	_, _, err := manager.GetObject(ctx, bucket, nonExistentKey)
	assert.Equal(t, object.ErrObjectNotFound, err)

	// Get metadata for non-existent object
	_, err = manager.GetObjectMetadata(ctx, bucket, nonExistentKey)
	assert.Equal(t, object.ErrObjectNotFound, err)

	// Update metadata for non-existent object
	err = manager.UpdateObjectMetadata(ctx, bucket, nonExistentKey, map[string]string{"test": "value"})
	assert.Equal(t, object.ErrObjectNotFound, err)

	// Delete non-existent object
	err = manager.DeleteObject(ctx, bucket, nonExistentKey)
	assert.Equal(t, object.ErrObjectNotFound, err)
}

func testMultipartUpload(t *testing.T, manager object.Manager, bucket string) {
	ctx := context.Background()
	testKey := "test/multipart-object.txt"

	// Create multipart upload
	headers := http.Header{
		"Content-Type": []string{"text/plain"},
		"X-Amz-Meta-Test": []string{"multipart-test"},
	}

	multipart, err := manager.CreateMultipartUpload(ctx, bucket, testKey, headers)
	require.NoError(t, err)
	require.NotNil(t, multipart)

	// Verify multipart upload properties
	assert.NotEmpty(t, multipart.UploadID)
	assert.Equal(t, bucket, multipart.Bucket)
	assert.Equal(t, testKey, multipart.Key)
	assert.False(t, multipart.Initiated.IsZero())
	assert.Equal(t, "text/plain", multipart.Metadata["content-type"])
	assert.Equal(t, "multipart-test", multipart.Metadata["x-amz-meta-test"])

	// Upload parts
	part1Data := "First part of multipart upload"
	part1, err := manager.UploadPart(ctx, multipart.UploadID, 1, strings.NewReader(part1Data))
	require.NoError(t, err)
	require.NotNil(t, part1)

	assert.Equal(t, 1, part1.PartNumber)
	assert.Equal(t, int64(len(part1Data)), part1.Size)
	assert.NotEmpty(t, part1.ETag)
	assert.False(t, part1.LastModified.IsZero())

	part2Data := "Second part of multipart upload"
	part2, err := manager.UploadPart(ctx, multipart.UploadID, 2, strings.NewReader(part2Data))
	require.NoError(t, err)
	require.NotNil(t, part2)

	assert.Equal(t, 2, part2.PartNumber)
	assert.Equal(t, int64(len(part2Data)), part2.Size)

	// List parts
	parts, err := manager.ListParts(ctx, multipart.UploadID)
	require.NoError(t, err)
	assert.Len(t, parts, 2)

	// Verify parts are sorted by part number
	assert.Equal(t, 1, parts[0].PartNumber)
	assert.Equal(t, 2, parts[1].PartNumber)

	// List multipart uploads
	uploads, err := manager.ListMultipartUploads(ctx, bucket)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(uploads), 1)

	// Find our upload in the list
	found := false
	for _, upload := range uploads {
		if upload.UploadID == multipart.UploadID {
			found = true
			assert.Equal(t, bucket, upload.Bucket)
			assert.Equal(t, testKey, upload.Key)
			break
		}
	}
	assert.True(t, found, "Multipart upload should be found in list")

	// Complete multipart upload
	completedObject, err := manager.CompleteMultipartUpload(ctx, multipart.UploadID, parts)
	require.NoError(t, err)
	require.NotNil(t, completedObject)

	// Verify completed object
	assert.Equal(t, testKey, completedObject.Key)
	assert.Equal(t, bucket, completedObject.Bucket)
	assert.Greater(t, completedObject.Size, int64(0))
	assert.NotEmpty(t, completedObject.ETag)
	assert.Equal(t, "text/plain", completedObject.ContentType)

	// Verify object exists and can be retrieved
	retrievedObj, _, err := manager.GetObject(ctx, bucket, testKey)
	require.NoError(t, err)
	assert.Equal(t, completedObject.Key, retrievedObj.Key)
	assert.Equal(t, completedObject.ETag, retrievedObj.ETag)

	// Test abort multipart upload with a new upload
	abortMultipart, err := manager.CreateMultipartUpload(ctx, bucket, "test/abort-me.txt", http.Header{})
	require.NoError(t, err)

	// Upload a part
	_, err = manager.UploadPart(ctx, abortMultipart.UploadID, 1, strings.NewReader("abort test"))
	require.NoError(t, err)

	// Abort the upload
	err = manager.AbortMultipartUpload(ctx, abortMultipart.UploadID)
	require.NoError(t, err)

	// Verify upload is no longer listed
	uploads, err = manager.ListMultipartUploads(ctx, bucket)
	require.NoError(t, err)
	for _, upload := range uploads {
		assert.NotEqual(t, abortMultipart.UploadID, upload.UploadID, "Aborted upload should not be in list")
	}

	// Test error cases
	// Invalid part number
	_, err = manager.UploadPart(ctx, "invalid-upload-id", 1, strings.NewReader("test"))
	assert.Error(t, err)

	// Invalid upload ID for complete
	_, err = manager.CompleteMultipartUpload(ctx, "invalid-upload-id", []object.Part{{PartNumber: 1, ETag: "test"}})
	assert.Error(t, err)

	// Invalid upload ID for list parts
	_, err = manager.ListParts(ctx, "invalid-upload-id")
	assert.Error(t, err)

	// Clean up
	manager.DeleteObject(ctx, bucket, testKey)
}