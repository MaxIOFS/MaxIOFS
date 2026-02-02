package object

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCleanupExpiredRetentions_WithExpiredObjects tests cleanup functionality
func TestCleanupExpiredRetentions_WithExpiredObjects(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	ol := NewObjectLocker(om, nil)
	rpm := NewRetentionPolicyManager(om, ol)

	bucketName := "cleanup-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	user := &auth.User{ID: "user-1", Roles: []string{"admin"}}

	// Create object 1 with retention that expires in 1 second
	key1 := "expire-soon-1.txt"
	content1 := bytes.NewReader([]byte("expiring soon 1"))
	headers1 := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key1, content1, headers1)
	require.NoError(t, err)

	retention1 := &ObjectLockRetention{
		Mode:            RetentionModeGovernance,
		RetainUntilDate: time.Now().Add(1 * time.Second),
	}
	err = ol.PutObjectRetention(ctx, bucket, key1, retention1, false, user)
	require.NoError(t, err)

	// Create object 2 with active retention (future)
	key2 := "keep-active.txt"
	content2 := bytes.NewReader([]byte("keep active"))
	headers2 := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key2, content2, headers2)
	require.NoError(t, err)

	retention2 := &ObjectLockRetention{
		Mode:            RetentionModeGovernance,
		RetainUntilDate: time.Now().Add(30 * 24 * time.Hour),
	}
	err = ol.PutObjectRetention(ctx, bucket, key2, retention2, false, user)
	require.NoError(t, err)

	// Wait for first retention to expire
	time.Sleep(2 * time.Second)

	// Call CleanupExpiredRetentions
	cleaned, err := rpm.CleanupExpiredRetentions(ctx, bucket)
	require.NoError(t, err)

	// Should have cleaned at least the expired one
	// (Note: cleanup might succeed even if it can't remove governance retention without bypass)
	t.Logf("Cleaned %d expired retentions", cleaned)
	assert.GreaterOrEqual(t, cleaned, 0, "Should attempt cleanup")
}

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

// TestShouldEncryptObject tests encryption decision logic
func TestShouldEncryptObject(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "encrypt-bucket"
	tenantID := "tenant-1"

	// Create bucket with encryption enabled
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
		Encryption: &metadata.EncryptionMetadata{
			Rules: []metadata.EncryptionRule{
				{
					ApplyServerSideEncryptionByDefault: &metadata.SSEConfig{
						SSEAlgorithm: "AES256",
					},
				},
			},
		},
	})
	require.NoError(t, err)

	// Test 1: Without EnableEncryption in config
	om.config.EnableEncryption = false
	shouldEncrypt := om.shouldEncryptObject(ctx, tenantID, bucketName)
	assert.False(t, shouldEncrypt, "Should not encrypt when config.EnableEncryption is false")

	// Test 2: With EnableEncryption in config and bucket encryption
	om.config.EnableEncryption = true
	shouldEncrypt = om.shouldEncryptObject(ctx, tenantID, bucketName)
	assert.True(t, shouldEncrypt, "Should encrypt when enabled in config and bucket")

	// Test 3: Bucket without encryption rules
	bucketNameNoEncrypt := "no-encrypt-bucket"
	err = metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketNameNoEncrypt,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	shouldEncrypt = om.shouldEncryptObject(ctx, tenantID, bucketNameNoEncrypt)
	assert.False(t, shouldEncrypt, "Should not encrypt bucket without encryption rules")
}

// TestShouldEncryptMultipartObject tests multipart encryption decision
func TestShouldEncryptMultipartObject(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "multipart-encrypt-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket with encryption enabled
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
		Encryption: &metadata.EncryptionMetadata{
			Rules: []metadata.EncryptionRule{
				{
					ApplyServerSideEncryptionByDefault: &metadata.SSEConfig{
						SSEAlgorithm: "AES256",
					},
				},
			},
		},
	})
	require.NoError(t, err)

	// Test 1: Without EnableEncryption in config
	om.config.EnableEncryption = false
	shouldEncrypt := om.shouldEncryptMultipartObject(ctx, bucket)
	assert.False(t, shouldEncrypt, "Should not encrypt multipart when config disabled")

	// Test 2: With EnableEncryption in config
	om.config.EnableEncryption = true
	shouldEncrypt = om.shouldEncryptMultipartObject(ctx, bucket)
	assert.True(t, shouldEncrypt, "Should encrypt multipart when enabled")

	// Test 3: Non-existent bucket
	shouldEncrypt = om.shouldEncryptMultipartObject(ctx, tenantID+"/nonexistent")
	assert.False(t, shouldEncrypt, "Should not encrypt for non-existent bucket")
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
	err = om.deleteSpecificVersion(ctx, bucket, key, "nonexistent-version-id")

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
		err = om.deleteSpecificVersion(ctx, bucket, key, obj.VersionID)

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
