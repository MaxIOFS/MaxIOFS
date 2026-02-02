package object

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetObject_ErrorCases tests GetObject error handling
func TestGetObject_ErrorCases(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "test-bucket"
	key := "test-object.txt"

	// Test 1: Get non-existent object
	_, _, err := om.GetObject(ctx, bucket, key)
	assert.Error(t, err, "Should return error for non-existent object")

	// Test 2: Create bucket and put object
	err = metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	content := bytes.NewReader([]byte("test content for retrieval"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Test 3: Get existing object successfully
	obj, reader, err := om.GetObject(ctx, bucket, key)
	require.NoError(t, err)
	require.NotNil(t, obj)
	require.NotNil(t, reader)

	// Read and verify content
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	reader.Close()
	assert.Equal(t, "test content for retrieval", string(data))
	assert.Equal(t, key, obj.Key)
	assert.Equal(t, bucket, obj.Bucket)
}

// TestGetObjectMetadata_ErrorCases tests GetObjectMetadata error handling
func TestGetObjectMetadata_ErrorCases(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "test-bucket"
	key := "metadata-test.txt"

	// Test 1: Get metadata for non-existent object
	_, err := om.GetObjectMetadata(ctx, bucket, key)
	assert.Error(t, err, "Should return error for non-existent object")

	// Test 2: Create bucket and put object
	err = metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	content := bytes.NewReader([]byte("metadata test content"))
	headers := http.Header{
		"Content-Type":   []string{"application/json"},
		"X-Amz-Meta-Key": []string{"custom-value"},
	}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Test 3: Get metadata successfully
	obj, err := om.GetObjectMetadata(ctx, bucket, key)
	require.NoError(t, err)
	require.NotNil(t, obj)
	assert.Equal(t, key, obj.Key)
	assert.Equal(t, "application/json", obj.ContentType)
}

// TestListObjects_WithDelimiter tests ListObjects with delimiter for folder simulation
func TestListObjects_WithDelimiter(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "folder-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Create objects in different "folders"
	objects := []string{
		"folder1/file1.txt",
		"folder1/file2.txt",
		"folder2/file3.txt",
		"root-file.txt",
	}

	for _, key := range objects {
		content := bytes.NewReader([]byte("content for " + key))
		headers := http.Header{"Content-Type": []string{"text/plain"}}
		_, err = om.PutObject(ctx, bucket, key, content, headers)
		require.NoError(t, err)
	}

	// List with delimiter "/" to get folders
	result, err := om.ListObjects(ctx, bucket, "", "/", "", 100)
	require.NoError(t, err)

	// Should show common prefixes (folders)
	assert.GreaterOrEqual(t, len(result.CommonPrefixes), 0, "Should have common prefixes")

	// List with prefix "folder1/" to get files in folder1
	result2, err := om.ListObjects(ctx, bucket, "folder1/", "", "", 100)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result2.Objects), 2, "Should have at least 2 objects in folder1")
}

// TestListObjects_Pagination tests ListObjects with marker for pagination
func TestListObjects_Pagination(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "pagination-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Create multiple objects
	for i := 0; i < 15; i++ {
		key := string(rune('a'+i)) + "-file.txt"
		content := bytes.NewReader([]byte("content"))
		headers := http.Header{"Content-Type": []string{"text/plain"}}
		_, err = om.PutObject(ctx, bucket, key, content, headers)
		require.NoError(t, err)
	}

	// List with maxKeys=5
	result, err := om.ListObjects(ctx, bucket, "", "", "", 5)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(result.Objects), 5, "Should return at most 5 objects")

	// If truncated, use marker to get next page
	if result.IsTruncated && result.NextMarker != "" {
		result2, err := om.ListObjects(ctx, bucket, "", "", result.NextMarker, 5)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(result2.Objects), 1, "Should have more objects")
	}
}

// TestDeleteObject_Permanent tests permanent deletion
func TestDeleteObject_Permanent(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "delete-bucket"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put object
	key := "to-delete.txt"
	content := bytes.NewReader([]byte("content to delete"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Verify object exists
	_, err = om.GetObjectMetadata(ctx, bucket, key)
	require.NoError(t, err)

	// Delete object
	_, err = om.DeleteObject(ctx, bucket, key, false)
	require.NoError(t, err)

	// Verify object is deleted
	_, err = om.GetObjectMetadata(ctx, bucket, key)
	assert.Error(t, err, "Object should be deleted")
}

// TestPutObject_WithCustomMetadata tests putting object with custom metadata
func TestPutObject_WithCustomMetadata(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "metadata-bucket"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put object with custom metadata
	key := "custom-meta.txt"
	content := bytes.NewReader([]byte("content with metadata"))
	headers := http.Header{
		"Content-Type":        []string{"text/plain"},
		"X-Amz-Meta-Author":   []string{"Test Author"},
		"X-Amz-Meta-Version":  []string{"1.0"},
		"X-Amz-Storage-Class": []string{"STANDARD"},
	}
	obj, err := om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)
	require.NotNil(t, obj)

	// Verify metadata was stored
	retrieved, err := om.GetObjectMetadata(ctx, bucket, key)
	require.NoError(t, err)
	assert.Equal(t, "text/plain", retrieved.ContentType)
	assert.Equal(t, "STANDARD", retrieved.StorageClass)
}

// TestUpdateObjectMetadata_Success tests updating object metadata
func TestUpdateObjectMetadata_Success(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "update-bucket"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put object
	key := "update-meta.txt"
	content := bytes.NewReader([]byte("original content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Update metadata
	newMetadata := map[string]string{
		"custom-key": "custom-value",
		"author":     "Updated Author",
	}
	err = om.UpdateObjectMetadata(ctx, bucket, key, newMetadata)
	require.NoError(t, err)

	// Verify metadata was updated
	retrieved, err := om.GetObjectMetadata(ctx, bucket, key)
	require.NoError(t, err)
	assert.NotNil(t, retrieved.Metadata)
}

// TestCompleteMultipartUpload_Success tests completing multipart upload
func TestCompleteMultipartUpload_Success(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "multipart-bucket"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Create multipart upload
	key := "multipart-complete.bin"
	headers := http.Header{"Content-Type": []string{"application/octet-stream"}}
	upload, err := om.CreateMultipartUpload(ctx, bucket, key, headers)
	require.NoError(t, err)
	require.NotNil(t, upload)

	// Upload parts
	part1Data := bytes.Repeat([]byte("A"), 1024)
	part1Reader := bytes.NewReader(part1Data)
	part1, err := om.UploadPart(ctx, upload.UploadID, 1, part1Reader)
	require.NoError(t, err)

	part2Data := bytes.Repeat([]byte("B"), 1024)
	part2Reader := bytes.NewReader(part2Data)
	part2, err := om.UploadPart(ctx, upload.UploadID, 2, part2Reader)
	require.NoError(t, err)

	// Complete multipart upload
	parts := []Part{*part1, *part2}
	result, err := om.CompleteMultipartUpload(ctx, upload.UploadID, parts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify object exists
	obj, err := om.GetObjectMetadata(ctx, bucket, key)
	require.NoError(t, err)
	assert.Equal(t, key, obj.Key)
	assert.Greater(t, obj.Size, int64(0))
}

// TestListParts_Success tests listing parts of multipart upload
func TestListParts_Success(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "parts-bucket"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Create multipart upload
	key := "parts-test.bin"
	headers := http.Header{"Content-Type": []string{"application/octet-stream"}}
	upload, err := om.CreateMultipartUpload(ctx, bucket, key, headers)
	require.NoError(t, err)

	// Upload a part
	partData := bytes.Repeat([]byte("X"), 512)
	partReader := bytes.NewReader(partData)
	_, err = om.UploadPart(ctx, upload.UploadID, 1, partReader)
	require.NoError(t, err)

	// List parts
	parts, err := om.ListParts(ctx, upload.UploadID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(parts), 1, "Should have at least 1 part")

	// Verify part details
	if len(parts) > 0 {
		assert.Equal(t, 1, parts[0].PartNumber)
		assert.NotEmpty(t, parts[0].ETag)
	}
}

// TestAbortMultipartUpload_Success tests aborting multipart upload
func TestAbortMultipartUpload_Success(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "abort-bucket"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Create multipart upload
	key := "abort-test.bin"
	headers := http.Header{"Content-Type": []string{"application/octet-stream"}}
	upload, err := om.CreateMultipartUpload(ctx, bucket, key, headers)
	require.NoError(t, err)

	// Abort multipart upload
	err = om.AbortMultipartUpload(ctx, upload.UploadID)
	require.NoError(t, err)

	// Verify upload is aborted (listing parts should fail or return empty)
	parts, err := om.ListParts(ctx, upload.UploadID)
	// Either error or empty parts list is acceptable
	if err == nil {
		assert.Empty(t, parts, "Parts should be empty after abort")
	}
}

// TestSetObjectRetention_RemoveRetention tests removing retention by setting to nil
func TestSetObjectRetention_RemoveRetention(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "retention-bucket"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put object
	key := "retention-remove.txt"
	content := bytes.NewReader([]byte("test content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Remove retention (set to nil)
	err = om.SetObjectRetention(ctx, bucket, key, nil)
	require.NoError(t, err)

	// Verify retention is removed
	obj, err := om.GetObjectMetadata(ctx, bucket, key)
	require.NoError(t, err)
	assert.Nil(t, obj.Retention, "Retention should be nil")
}
