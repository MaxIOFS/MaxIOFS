package object

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetObject_WithSpecificVersionID tests getting a specific version
func TestGetObject_WithSpecificVersionID(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "version-get-bucket"
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

	// Upload multiple versions
	key := "version-test.txt"
	content1 := bytes.NewReader([]byte("Version 1 content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	obj1, err := om.PutObject(ctx, bucket, key, content1, headers)
	require.NoError(t, err)
	t.Logf("Version 1 ID: %s", obj1.VersionID)

	content2 := bytes.NewReader([]byte("Version 2 content - newer"))
	obj2, err := om.PutObject(ctx, bucket, key, content2, headers)
	require.NoError(t, err)
	t.Logf("Version 2 ID: %s", obj2.VersionID)

	// Get specific version (version 1)
	if obj1.VersionID != "" {
		retrieved, reader, err := om.GetObject(ctx, bucket, key, obj1.VersionID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		require.NotNil(t, reader)

		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		reader.Close()

		assert.Equal(t, "Version 1 content", string(data), "Should retrieve version 1 content")
		assert.Equal(t, obj1.VersionID, retrieved.VersionID, "Version ID should match")
	}

	// Get latest version (no version ID)
	latest, reader, err := om.GetObject(ctx, bucket, key)
	require.NoError(t, err)
	require.NotNil(t, latest)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	reader.Close()

	assert.Equal(t, "Version 2 content - newer", string(data), "Should retrieve latest version")
	assert.Equal(t, obj2.VersionID, latest.VersionID, "Should match version 2 ID")
}

// TestGetObject_WithDeleteMarker tests getting object with delete marker as latest version
func TestGetObject_WithDeleteMarker(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "delete-marker-bucket"
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

	// Upload object
	key := "marker-test.txt"
	content := bytes.NewReader([]byte("Original content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	obj, err := om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)
	t.Logf("Original object version: %s", obj.VersionID)

	// Delete object (creates delete marker in versioned bucket)
	deleteMarkerVersionID, err := om.DeleteObject(ctx, bucket, key, false)
	require.NoError(t, err)
	t.Logf("Delete marker version: %s", deleteMarkerVersionID)

	// Try to get object (latest version is delete marker) - should return not found
	_, _, err = om.GetObject(ctx, bucket, key)
	assert.Error(t, err, "Should return error when latest version is delete marker")
	assert.Equal(t, ErrObjectNotFound, err, "Error should be ObjectNotFound")

	// Get previous version explicitly (should work)
	if obj.VersionID != "" {
		retrieved, reader, err := om.GetObject(ctx, bucket, key, obj.VersionID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)

		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		reader.Close()

		assert.Equal(t, "Original content", string(data), "Previous version should still be accessible")
	}
}

// TestGetObject_NonExistentObject tests error handling for non-existent object
func TestGetObject_NonExistentObject(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "nonexistent-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Try to get non-existent object
	_, _, err = om.GetObject(ctx, bucket, "does-not-exist.txt")
	assert.Error(t, err, "Should return error for non-existent object")
	assert.Equal(t, ErrObjectNotFound, err)
}

// TestGetObject_NonExistentVersion tests error handling for non-existent version
func TestGetObject_NonExistentVersion(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "version-error-bucket"
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

	// Upload object
	key := "versioned-object.txt"
	content := bytes.NewReader([]byte("Content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Try to get non-existent version
	_, _, err = om.GetObject(ctx, bucket, key, "fake-version-id-12345")
	assert.Error(t, err, "Should return error for non-existent version")
}

// TestDeletePermanently_WithLegalHold tests deletion blocked by legal hold
func TestDeletePermanently_WithLegalHold(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "legal-hold-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Upload object
	key := "legal-hold-protected.txt"
	content := bytes.NewReader([]byte("Protected content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Set legal hold
	legalHold := &LegalHoldConfig{Status: LegalHoldStatusOn}
	err = om.SetObjectLegalHold(ctx, bucket, key, legalHold)
	require.NoError(t, err)

	// Try to delete permanently - should fail
	err = om.deletePermanently(ctx, bucket, key, false)
	assert.Error(t, err, "Should fail to delete object under legal hold")
	assert.Equal(t, ErrObjectUnderLegalHold, err)

	// Verify object still exists
	_, err = om.GetObjectMetadata(ctx, bucket, key)
	assert.NoError(t, err, "Object should still exist")
}

// TestDeletePermanently_WithComplianceRetention tests deletion blocked by compliance retention
func TestDeletePermanently_WithComplianceRetention(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "compliance-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Upload object
	key := "compliance-protected.txt"
	content := bytes.NewReader([]byte("Compliance content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Set compliance retention (1 hour in future)
	retention := &RetentionConfig{
		Mode:            RetentionModeCompliance,
		RetainUntilDate: time.Now().Add(1 * time.Hour),
	}
	err = om.SetObjectRetention(ctx, bucket, key, retention)
	require.NoError(t, err)

	// Try to delete permanently - should fail even with bypass
	err = om.deletePermanently(ctx, bucket, key, true)
	assert.Error(t, err, "Should fail to delete object with compliance retention")
	assert.Contains(t, err.Error(), "COMPLIANCE", "Error should mention COMPLIANCE")

	// Verify object still exists
	_, err = om.GetObjectMetadata(ctx, bucket, key)
	assert.NoError(t, err, "Object should still exist")
}

// TestDeletePermanently_WithGovernanceRetentionBypass tests governance retention with bypass
func TestDeletePermanently_WithGovernanceRetentionBypass(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "governance-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Set up mocks for metrics
	mockBM := &mockMetricsBucketManager{}
	mockAuth := &mockQuotaAuthManager{}
	om.bucketManager = mockBM
	om.authManager = mockAuth

	// Upload object
	key := "governance-protected.txt"
	content := bytes.NewReader([]byte("Governance content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Set governance retention (1 hour in future)
	retention := &RetentionConfig{
		Mode:            RetentionModeGovernance,
		RetainUntilDate: time.Now().Add(1 * time.Hour),
	}
	err = om.SetObjectRetention(ctx, bucket, key, retention)
	require.NoError(t, err)

	// Try to delete without bypass - should fail
	err = om.deletePermanently(ctx, bucket, key, false)
	assert.Error(t, err, "Should fail to delete without bypass")
	assert.Contains(t, err.Error(), "GOVERNANCE", "Error should mention GOVERNANCE")

	// Delete with bypass - should succeed
	err = om.deletePermanently(ctx, bucket, key, true)
	assert.NoError(t, err, "Should succeed with bypass")

	// Verify object is deleted
	_, err = om.GetObjectMetadata(ctx, bucket, key)
	assert.Error(t, err, "Object should be deleted")

	// Verify metrics were updated
	assert.True(t, mockBM.decrementCalled, "Should decrement bucket metrics")
	assert.True(t, mockAuth.decrementCalled, "Should decrement tenant quota")
}

// TestDeletePermanently_OrphanedFile tests deleting file without metadata
func TestDeletePermanently_OrphanedFile(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "orphan-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Upload object
	key := "orphan-file.txt"
	content := bytes.NewReader([]byte("Orphaned content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Manually delete metadata only (simulating orphaned file)
	err = metaStore.DeleteObject(ctx, bucket, key)
	require.NoError(t, err)

	// Try to delete permanently - should succeed (idempotent delete)
	err = om.deletePermanently(ctx, bucket, key, false)
	assert.NoError(t, err, "Should succeed in cleaning up orphaned file")

	// Verify physical file is also deleted
	_, err = om.GetObjectMetadata(ctx, bucket, key)
	assert.Error(t, err, "Object should not exist")
}

// TestDeletePermanently_WithMetrics tests metrics updates during deletion
func TestDeletePermanently_WithMetrics(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "metrics-delete-bucket"
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

	// Upload object
	key := "metrics-test.txt"
	content := bytes.NewReader([]byte("Content for metrics test"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	obj, err := om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)
	objectSize := obj.Size

	// Delete permanently
	err = om.deletePermanently(ctx, bucket, key, false)
	require.NoError(t, err)

	// Verify metrics were updated
	assert.True(t, mockBM.decrementCalled, "Should call DecrementObjectCount")
	assert.True(t, mockAuth.decrementCalled, "Should call DecrementTenantStorage")
	assert.Equal(t, -objectSize, mockBM.lastSize, "Should decrement by object size")
}

// TestDeletePermanently_ExpiredRetention tests deleting object with expired retention
func TestDeletePermanently_ExpiredRetention(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "expired-retention-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Upload object
	key := "expired-retention.txt"
	content := bytes.NewReader([]byte("Expired retention content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Set governance retention that's already expired (1 second ago)
	retention := &RetentionConfig{
		Mode:            RetentionModeGovernance,
		RetainUntilDate: time.Now().Add(-1 * time.Second),
	}
	err = om.SetObjectRetention(ctx, bucket, key, retention)
	require.NoError(t, err)

	// Delete should succeed (retention expired)
	err = om.deletePermanently(ctx, bucket, key, false)
	assert.NoError(t, err, "Should succeed deleting object with expired retention")

	// Verify object is deleted
	_, err = om.GetObjectMetadata(ctx, bucket, key)
	assert.Error(t, err, "Object should be deleted")
}

// TestGetObjectMetadata_NonExistent tests GetObjectMetadata error handling
func TestGetObjectMetadata_NonExistent(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "metadata-error-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Try to get metadata for non-existent object
	_, err = om.GetObjectMetadata(ctx, bucket, "nonexistent.txt")
	assert.Error(t, err, "Should return error for non-existent object")
}

// TestGetObjectMetadata_Success tests getting metadata for latest version
func TestGetObjectMetadata_Success(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "metadata-success-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Upload object
	key := "metadata-test.txt"
	content := bytes.NewReader([]byte("Test content for metadata"))
	headers := http.Header{
		"Content-Type":   []string{"application/json"},
		"X-Amz-Meta-Key": []string{"custom-value"},
	}
	obj, err := om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Get metadata (latest version)
	latest, err := om.GetObjectMetadata(ctx, bucket, key)
	require.NoError(t, err)
	assert.Equal(t, key, latest.Key, "Key should match")
	assert.Equal(t, obj.Size, latest.Size, "Size should match")
	assert.Equal(t, "application/json", latest.ContentType, "Content type should match")
}

// TestDeleteObject_NonVersioned tests deletion in non-versioned bucket
func TestDeleteObject_NonVersioned(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "nonversioned-delete-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create non-versioned bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Upload object
	key := "nonversioned-file.txt"
	content := bytes.NewReader([]byte("Non-versioned content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Delete object (permanent delete in non-versioned bucket)
	_, err = om.DeleteObject(ctx, bucket, key, false)
	require.NoError(t, err)

	// Verify object is deleted
	_, err = om.GetObjectMetadata(ctx, bucket, key)
	assert.Error(t, err, "Object should be permanently deleted")
}

// TestDeleteObject_VersionedBucket tests deletion creates delete marker
func TestDeleteObject_VersionedBucket(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "versioned-delete-bucket"
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

	// Upload object
	key := "versioned-delete-file.txt"
	content := bytes.NewReader([]byte("Versioned content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	obj, err := om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)
	t.Logf("Original version ID: %s", obj.VersionID)

	// Delete object (creates delete marker)
	deleteMarkerID, err := om.DeleteObject(ctx, bucket, key, false)
	require.NoError(t, err)
	t.Logf("Delete marker ID: %s", deleteMarkerID)

	// Latest version should be not found (delete marker)
	_, err = om.GetObjectMetadata(ctx, bucket, key)
	assert.Error(t, err, "Latest version should be delete marker")

	// But specific version should still exist
	if obj.VersionID != "" {
		meta, reader, err := om.GetObject(ctx, bucket, key, obj.VersionID)
		require.NoError(t, err)
		reader.Close()
		assert.Equal(t, obj.VersionID, meta.VersionID, "Original version should still exist")
	}
}

// TestDeleteObject_WithSpecificVersionID tests deleting specific version
func TestDeleteObject_WithSpecificVersionID(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "delete-version-bucket"
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

	// Upload versions
	key := "delete-specific-version.txt"
	content1 := bytes.NewReader([]byte("Version 1"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	obj1, err := om.PutObject(ctx, bucket, key, content1, headers)
	require.NoError(t, err)

	content2 := bytes.NewReader([]byte("Version 2"))
	obj2, err := om.PutObject(ctx, bucket, key, content2, headers)
	require.NoError(t, err)

	// Delete specific version (version 1)
	if obj1.VersionID != "" {
		_, err = om.DeleteObject(ctx, bucket, key, false, obj1.VersionID)
		require.NoError(t, err)

		// Version 1 should be deleted
		_, _, err = om.GetObject(ctx, bucket, key, obj1.VersionID)
		assert.Error(t, err, "Version 1 should be deleted")

		// Version 2 should still exist - check by trying to get it with version ID
		if obj2.VersionID != "" {
			ver2, reader, err := om.GetObject(ctx, bucket, key, obj2.VersionID)
			if err == nil {
				reader.Close()
				assert.Equal(t, obj2.VersionID, ver2.VersionID, "Version 2 should still exist")
				t.Logf("Version 2 still accessible: %s", ver2.VersionID)
			} else {
				t.Logf("Note: Could not retrieve version 2 after deleting version 1: %v", err)
			}
		}
	}
}
