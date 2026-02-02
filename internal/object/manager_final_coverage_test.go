package object

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateObjectName_AllCases tests all validation scenarios
func TestValidateObjectName_AllCases(t *testing.T) {
	om := &objectManager{}

	tests := []struct {
		name      string
		key       string
		wantError bool
	}{
		{
			name:      "Valid simple key",
			key:       "file.txt",
			wantError: false,
		},
		{
			name:      "Valid key with path",
			key:       "folder/subfolder/file.txt",
			wantError: false,
		},
		{
			name:      "Valid key with special chars",
			key:       "file-with_special.chars-2024.txt",
			wantError: false,
		},
		{
			name:      "Empty key",
			key:       "",
			wantError: true,
		},
		{
			name:      "Key with parent directory reference 1",
			key:       "folder/../file.txt",
			wantError: true,
		},
		{
			name:      "Key with parent directory reference 2",
			key:       "folder/subfolder/..",
			wantError: true,
		},
		{
			name:      "Key with parent directory reference 3",
			key:       "../file.txt",
			wantError: true,
		},
		{
			name:      "Key with parent directory reference 4",
			key:       "folder/../../file.txt",
			wantError: true,
		},
		{
			name:      "Absolute path",
			key:       "/absolute/path.txt",
			wantError: true,
		},
		{
			name:      "Absolute path 2",
			key:       "/file.txt",
			wantError: true,
		},
		{
			name:      "Key exceeding max length (1024)",
			key:       string(make([]byte, 1025)),
			wantError: true,
		},
		{
			name:      "Key at max length (1024)",
			key:       string(make([]byte, 1024)),
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := om.validateObjectName(tt.key)
			if tt.wantError {
				assert.Error(t, err, "Expected error for key: %s", tt.key)
				assert.Equal(t, ErrInvalidObjectName, err)
			} else {
				assert.NoError(t, err, "Expected no error for key: %s", tt.key)
			}
		})
	}
}

// TestCleanupEmptyDirectories_WithFilesystem tests directory cleanup after deletions
func TestCleanupEmptyDirectories_WithFilesystem(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "cleanup-dirs-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Create nested folder structure
	key1 := "level1/level2/level3/file1.txt"
	key2 := "level1/level2/file2.txt"
	key3 := "level1/file3.txt"

	// Upload files
	for _, key := range []string{key1, key2, key3} {
		content := bytes.NewReader([]byte("test content"))
		headers := http.Header{"Content-Type": []string{"text/plain"}}
		_, err = om.PutObject(ctx, bucket, key, content, headers)
		require.NoError(t, err)
	}

	// Delete the deepest file (level3/file1.txt)
	_, err = om.DeleteObject(ctx, bucket, key1, false)
	require.NoError(t, err)

	// After deletion, level3 directory should be cleaned up if empty
	// Call cleanup explicitly
	om.cleanupEmptyDirectories(bucket, key1)

	// Verify the file is deleted
	_, err = om.GetObjectMetadata(ctx, bucket, key1)
	assert.Error(t, err, "File should be deleted")

	// Other files should still exist
	_, err = om.GetObjectMetadata(ctx, bucket, key2)
	assert.NoError(t, err, "File 2 should still exist")
	_, err = om.GetObjectMetadata(ctx, bucket, key3)
	assert.NoError(t, err, "File 3 should still exist")

	// Delete file2 - level2 should NOT be cleaned up because it still has file1 in parent
	_, err = om.DeleteObject(ctx, bucket, key2, false)
	require.NoError(t, err)
	om.cleanupEmptyDirectories(bucket, key2)

	// Delete file3 - now level1 should be cleaned up entirely
	_, err = om.DeleteObject(ctx, bucket, key3, false)
	require.NoError(t, err)
	om.cleanupEmptyDirectories(bucket, key3)

	t.Log("Directory cleanup test completed successfully")
}

// TestCleanupEmptyDirectories_WithNonFilesystemBackend tests no-op for non-filesystem backends
func TestCleanupEmptyDirectories_WithNonFilesystemBackend(t *testing.T) {
	om := &objectManager{
		storage: nil, // Non-filesystem backend
	}

	// Should not panic, just return early
	om.cleanupEmptyDirectories("bucket", "key")
	t.Log("Non-filesystem backend handled correctly")
}

// mockMetricsBucketManager for testing bucket metrics
type mockMetricsBucketManager struct {
	incrementCalled bool
	decrementCalled bool
	incrementCount  int
	lastSize        int64
}

func (m *mockMetricsBucketManager) IncrementObjectCount(ctx context.Context, tenantID, bucketName string, sizeBytes int64) error {
	m.incrementCalled = true
	m.incrementCount++
	m.lastSize = sizeBytes
	return nil
}

func (m *mockMetricsBucketManager) DecrementObjectCount(ctx context.Context, tenantID, bucketName string, sizeBytes int64) error {
	m.decrementCalled = true
	m.lastSize = -sizeBytes
	return nil
}

// TestUpdateBucketMetricsAfterPut_NonVersioned_NewObject tests metrics for new object in non-versioned bucket
func TestUpdateBucketMetricsAfterPut_NonVersioned_NewObject(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "metrics-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket (non-versioned)
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Set up mock bucket manager
	mockBM := &mockMetricsBucketManager{}
	om.bucketManager = mockBM

	// Test 1: New object (existingObj is nil)
	key := "new-file.txt"
	size := int64(1024)
	om.updateBucketMetricsAfterPut(ctx, tenantID, bucketName, bucket, key, size, false, nil)

	assert.True(t, mockBM.incrementCalled, "Should increment for new object")
	assert.Equal(t, int64(1024), mockBM.lastSize, "Should increment by full size")
}

// TestUpdateBucketMetricsAfterPut_NonVersioned_Overwrite tests metrics for overwriting object
func TestUpdateBucketMetricsAfterPut_NonVersioned_Overwrite(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "metrics-overwrite-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Set up mock bucket manager
	mockBM := &mockMetricsBucketManager{}
	om.bucketManager = mockBM

	// Test 2: Overwrite - size increases
	key := "overwrite-file.txt"
	existingObj := &metadata.ObjectMetadata{
		Size: 500,
	}
	newSize := int64(1024)

	om.updateBucketMetricsAfterPut(ctx, tenantID, bucketName, bucket, key, newSize, false, existingObj)

	assert.True(t, mockBM.incrementCalled, "Should increment for overwrite")
	assert.Equal(t, int64(524), mockBM.lastSize, "Should increment by size difference (1024-500=524)")
}

// TestUpdateBucketMetricsAfterPut_NonVersioned_OverwriteSmaller tests overwrite with smaller file
func TestUpdateBucketMetricsAfterPut_NonVersioned_OverwriteSmaller(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "metrics-smaller-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Set up mock bucket manager
	mockBM := &mockMetricsBucketManager{}
	om.bucketManager = mockBM

	// Test 3: Overwrite - size decreases
	key := "smaller-file.txt"
	existingObj := &metadata.ObjectMetadata{
		Size: 2048,
	}
	newSize := int64(512)

	om.updateBucketMetricsAfterPut(ctx, tenantID, bucketName, bucket, key, newSize, false, existingObj)

	assert.True(t, mockBM.incrementCalled, "Should still call increment (with negative value)")
	assert.Equal(t, int64(-1536), mockBM.lastSize, "Should increment by negative difference (512-2048=-1536)")
}

// TestUpdateBucketMetricsAfterPut_Versioned_FirstVersion tests metrics for first version
func TestUpdateBucketMetricsAfterPut_Versioned_FirstVersion(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "metrics-versioned-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create versioned bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
		Versioning: &metadata.VersioningMetadata{
			Enabled: true,
			Status:  "Enabled",
		},
	})
	require.NoError(t, err)

	// Set up mock bucket manager
	mockBM := &mockMetricsBucketManager{}
	om.bucketManager = mockBM

	// Put first version
	key := "versioned-file.txt"
	content := bytes.NewReader([]byte("version 1"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	obj, err := om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Call updateBucketMetricsAfterPut for first version
	om.updateBucketMetricsAfterPut(ctx, tenantID, bucketName, bucket, key, obj.Size, true, nil)

	assert.True(t, mockBM.incrementCalled, "Should increment for first version")
}

// TestUpdateBucketMetricsAfterPut_Versioned_AdditionalVersions tests metrics for additional versions
func TestUpdateBucketMetricsAfterPut_Versioned_AdditionalVersions(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "metrics-multiversion-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create versioned bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
		Versioning: &metadata.VersioningMetadata{
			Enabled: true,
			Status:  "Enabled",
		},
	})
	require.NoError(t, err)

	// Set up mock bucket manager
	mockBM := &mockMetricsBucketManager{}
	om.bucketManager = mockBM

	// Put version 1
	key := "multiversion-file.txt"
	content1 := bytes.NewReader([]byte("version 1"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	obj1, err := om.PutObject(ctx, bucket, key, content1, headers)
	require.NoError(t, err)

	// Put version 2
	content2 := bytes.NewReader([]byte("version 2 - longer content"))
	obj2, err := om.PutObject(ctx, bucket, key, content2, headers)
	require.NoError(t, err)

	// Reset mock for version 2
	mockBM.incrementCalled = false
	mockBM.incrementCount = 0

	// Call updateBucketMetricsAfterPut for second version
	om.updateBucketMetricsAfterPut(ctx, tenantID, bucketName, bucket, key, obj2.Size, true, nil)

	assert.True(t, mockBM.incrementCalled, "Should increment for additional version (size only)")
	t.Logf("Version 1 size: %d, Version 2 size: %d", obj1.Size, obj2.Size)
}

// TestUpdateBucketMetricsAfterPut_NoBucketManager tests no-op when bucket manager is nil
func TestUpdateBucketMetricsAfterPut_NoBucketManager(t *testing.T) {
	ctx := context.Background()
	om := &objectManager{
		bucketManager: nil,
	}

	// Should not panic
	om.updateBucketMetricsAfterPut(ctx, "tenant-1", "bucket", "tenant-1/bucket", "key", 1024, false, nil)
	t.Log("No bucket manager handled correctly")
}

// TestUpdateMetricsAndCleanupMultipart_NewObject tests cleanup for new multipart object
func TestUpdateMetricsAndCleanupMultipart_NewObject(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "multipart-cleanup-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Set up mocks
	mockBM := &mockMetricsBucketManager{}
	mockAuth := &mockQuotaAuthManager{}
	om.bucketManager = mockBM
	om.authManager = mockAuth

	// Create and complete multipart upload
	key := "multipart-new.bin"
	headers := http.Header{"Content-Type": []string{"application/octet-stream"}}
	upload, err := om.CreateMultipartUpload(ctx, bucket, key, headers)
	require.NoError(t, err)

	// Upload parts
	part1Data := bytes.Repeat([]byte("A"), 1024)
	part1Reader := bytes.NewReader(part1Data)
	part1, err := om.UploadPart(ctx, upload.UploadID, 1, part1Reader)
	require.NoError(t, err)

	// Call updateMetricsAndCleanupMultipart
	parts := []Part{*part1}
	om.updateMetricsAndCleanupMultipart(ctx, bucket, upload.UploadID, int64(1024), true, nil, parts)

	assert.True(t, mockBM.incrementCalled, "Should increment bucket metrics for new object")
	assert.True(t, mockAuth.incrementCalled, "Should increment tenant quota for new object")
	assert.Equal(t, int64(1024), mockBM.lastSize, "Should increment by object size")
}

// TestUpdateMetricsAndCleanupMultipart_OverwriteObject tests cleanup for multipart overwrite
func TestUpdateMetricsAndCleanupMultipart_OverwriteObject(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "multipart-overwrite-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Set up mocks
	mockBM := &mockMetricsBucketManager{}
	mockAuth := &mockQuotaAuthManager{}
	om.bucketManager = mockBM
	om.authManager = mockAuth

	// Create multipart upload
	key := "multipart-overwrite.bin"
	headers := http.Header{"Content-Type": []string{"application/octet-stream"}}
	upload, err := om.CreateMultipartUpload(ctx, bucket, key, headers)
	require.NoError(t, err)

	// Upload part
	partData := bytes.Repeat([]byte("B"), 2048)
	partReader := bytes.NewReader(partData)
	part, err := om.UploadPart(ctx, upload.UploadID, 1, partReader)
	require.NoError(t, err)

	// Simulate existing object
	existingObj := &metadata.ObjectMetadata{
		Size: 512,
	}

	// Call updateMetricsAndCleanupMultipart for overwrite
	parts := []Part{*part}
	om.updateMetricsAndCleanupMultipart(ctx, bucket, upload.UploadID, int64(2048), false, existingObj, parts)

	assert.True(t, mockBM.incrementCalled, "Should increment bucket metrics")
	assert.True(t, mockAuth.incrementCalled, "Should increment tenant quota")
	// Should increment by size difference: 2048 - 512 = 1536
	assert.Equal(t, int64(1536), mockBM.lastSize, "Should increment by size difference")
}

// TestUpdateMetricsAndCleanupMultipart_NoManagers tests no-op when managers are nil
func TestUpdateMetricsAndCleanupMultipart_NoManagers(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	// Ensure managers are nil
	om.bucketManager = nil
	om.authManager = nil

	bucketName := "no-manager-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Create multipart upload
	key := "no-manager.bin"
	headers := http.Header{"Content-Type": []string{"application/octet-stream"}}
	upload, err := om.CreateMultipartUpload(ctx, bucket, key, headers)
	require.NoError(t, err)

	// Call cleanup - should not panic
	om.updateMetricsAndCleanupMultipart(ctx, bucket, upload.UploadID, 1024, true, nil, []Part{})
	t.Log("No managers handled correctly")
}
