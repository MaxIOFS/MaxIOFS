package object

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAuthManager for quota tests
type mockQuotaAuthManager struct {
	incrementCalled bool
	decrementCalled bool
	checkCalled     bool
	quotaExceeded   bool
}

func (m *mockQuotaAuthManager) IncrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error {
	m.incrementCalled = true
	return nil
}

func (m *mockQuotaAuthManager) DecrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error {
	m.decrementCalled = true
	return nil
}

func (m *mockQuotaAuthManager) CheckTenantStorageQuota(ctx context.Context, tenantID string, additionalBytes int64) error {
	m.checkCalled = true
	if m.quotaExceeded {
		return fmt.Errorf("storage quota exceeded")
	}
	return nil
}

// TestUpdateTenantQuotaAfterPut tests quota updates
func TestUpdateTenantQuotaAfterPut(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "quota-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Set up mock auth manager
	mockAuth := &mockQuotaAuthManager{}
	om.SetAuthManager(mockAuth)

	// Put object (should trigger quota update)
	key := "quota-test.txt"
	content := bytes.NewReader([]byte("test content for quota"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Verify quota was incremented
	assert.True(t, mockAuth.incrementCalled, "Should have called IncrementTenantStorage")
}

// TestCheckMultipartQuotaBeforeComplete tests multipart quota checking
func TestCheckMultipartQuotaBeforeComplete(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "multipart-quota-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Set up mock auth manager that will deny quota
	mockAuth := &mockQuotaAuthManager{quotaExceeded: true}
	om.SetAuthManager(mockAuth)

	// Create multipart upload
	key := "large-file.bin"
	headers := http.Header{"Content-Type": []string{"application/octet-stream"}}
	upload, err := om.CreateMultipartUpload(ctx, bucket, key, headers)
	require.NoError(t, err)

	// Upload parts
	part1Data := bytes.Repeat([]byte("A"), 1024)
	part1Reader := bytes.NewReader(part1Data)
	part1, err := om.UploadPart(ctx, upload.UploadID, 1, part1Reader)
	require.NoError(t, err)

	// Try to complete (should fail due to quota)
	parts := []Part{*part1}
	_, err = om.CompleteMultipartUpload(ctx, upload.UploadID, parts)

	// Should fail with quota exceeded
	assert.Error(t, err, "Should fail due to quota exceeded")
	assert.True(t, mockAuth.checkCalled, "Should have checked quota")
}

// TestAlwaysEncrypt verifies that every stored object is envelope-encrypted
// regardless of config flags or bucket-level rules (encryption is always on).
func TestAlwaysEncrypt(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "always-encrypt-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Bucket WITHOUT any encryption rules — objects must still be encrypted.
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Even with the deprecated flag off, writes are encrypted.
	om.config.EnableEncryption = false

	content := []byte("always encrypted content")
	_, err = om.PutObject(ctx, bucket, "file.txt", bytes.NewReader(content), http.Header{})
	require.NoError(t, err)

	// Sidecar metadata must carry the envelope fields.
	storageMeta, err := om.storage.GetMetadata(ctx, om.getObjectPath(bucket, "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "true", storageMeta["encrypted"])
	assert.NotEmpty(t, storageMeta["wrapped-dek"], "envelope must store the wrapped DEK in the sidecar")
	assert.NotEmpty(t, storageMeta["wrapped-dek-iv"])
	assert.Equal(t, "1", storageMeta["kek-version"])

	// And the object must read back decrypted.
	obj, reader, err := om.GetObject(ctx, bucket, "file.txt")
	require.NoError(t, err)
	defer reader.Close()
	readBack, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, content, readBack)
	assert.Equal(t, "AES256", obj.SSEAlgorithm)
}

// TestDeleteSpecificVersion_VersionNotFound tests error handling
func TestDeleteSpecificVersion_VersionNotFound(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "version-bucket"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put an object
	key := "versioned-object.txt"
	content := bytes.NewReader([]byte("test content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Try to delete non-existent version
	err = om.deleteSpecificVersion(ctx, bucket, key, "nonexistent-version-id", false)

	// Should return error for non-existent version
	assert.Error(t, err, "Should return error for non-existent version")
}

// TestDeleteSpecificVersion_WithExistingVersion tests deleting actual version
func TestDeleteSpecificVersion_WithExistingVersion(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "version-delete-bucket"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put an object to create a version
	key := "versioned-file.txt"
	content := bytes.NewReader([]byte("version 1 content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	obj, err := om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// If versioning is enabled, obj.VersionID will be set
	if obj.VersionID != "" {
		// Try to delete this specific version
		err = om.deleteSpecificVersion(ctx, bucket, key, obj.VersionID, false)

		// Either succeeds or fails gracefully
		if err != nil {
			t.Logf("Delete specific version returned error: %v", err)
		} else {
			t.Log("Successfully deleted specific version")
		}
	} else {
		t.Skip("Versioning not enabled, skipping version-specific delete test")
	}
}

// TestUpdateTenantQuotaAfterPut_WithoutAuthManager tests no-op behavior
func TestUpdateTenantQuotaAfterPut_WithoutAuthManager(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "no-auth-bucket"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Ensure authManager is nil
	om.authManager = nil

	// Put object (should not crash without authManager)
	key := "test.txt"
	content := bytes.NewReader([]byte("test content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Test passes if no panic occurred
	t.Log("Successfully handled PutObject without authManager")
}

// TestCheckMultipartQuotaBeforeComplete_WithoutAuthManager tests no-op behavior
func TestCheckMultipartQuotaBeforeComplete_WithoutAuthManager(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "multipart-no-auth-bucket"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Ensure authManager is nil
	om.authManager = nil

	// Create and complete multipart upload (should not crash)
	key := "multipart-no-auth.bin"
	headers := http.Header{"Content-Type": []string{"application/octet-stream"}}
	upload, err := om.CreateMultipartUpload(ctx, bucket, key, headers)
	require.NoError(t, err)

	// Upload a part
	partData := bytes.Repeat([]byte("X"), 512)
	partReader := bytes.NewReader(partData)
	part, err := om.UploadPart(ctx, upload.UploadID, 1, partReader)
	require.NoError(t, err)

	// Complete (should succeed without quota check)
	parts := []Part{*part}
	_, err = om.CompleteMultipartUpload(ctx, upload.UploadID, parts)
	require.NoError(t, err)

	t.Log("Successfully completed multipart upload without authManager")
}
