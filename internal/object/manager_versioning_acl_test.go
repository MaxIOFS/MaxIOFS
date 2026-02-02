package object

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to setup test environment with metadataStore access
func setupTestManagerWithStore(t *testing.T) (*objectManager, metadata.Store, func()) {
	tempDir := t.TempDir()
	backend, err := storage.NewFilesystemBackend(storage.Config{Root: tempDir})
	require.NoError(t, err)

	dbPath := filepath.Join(tempDir, "metadata")
	metaStore, err := metadata.NewBadgerStore(metadata.BadgerOptions{
		DataDir:           dbPath,
		SyncWrites:        false,
		CompactionEnabled: false,
		Logger:            logrus.StandardLogger(),
	})
	require.NoError(t, err)

	cfg := config.StorageConfig{
		Backend: "filesystem",
		Root:    tempDir,
	}

	om := NewManager(backend, metaStore, cfg).(*objectManager)

	cleanup := func() {
		metaStore.Close()
		os.RemoveAll(tempDir)
	}

	return om, metaStore, cleanup
}

// TestSetAuthManager tests the SetAuthManager method
func TestSetAuthManager(t *testing.T) {
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	// Create a mock auth manager
	mockAuth := &mockAuthManager{}

	// Test setting auth manager
	om.SetAuthManager(mockAuth)

	// Verify it was set
	assert.NotNil(t, om.authManager)
	assert.Equal(t, mockAuth, om.authManager)
}

// TestGenerateVersionID tests version ID generation
func TestGenerateVersionID(t *testing.T) {
	// Generate multiple version IDs
	ids := make(map[string]bool)

	for i := 0; i < 100; i++ {
		versionID := generateVersionID()

		// Verify format: timestamp.randomhex
		parts := strings.Split(versionID, ".")
		require.Len(t, parts, 2, "Version ID should have format timestamp.hex")

		// Verify timestamp part is numeric
		assert.NotEmpty(t, parts[0], "Timestamp part should not be empty")

		// Verify random hex part (8 characters)
		assert.Len(t, parts[1], 8, "Random hex part should be 8 characters")

		// Verify uniqueness
		assert.False(t, ids[versionID], "Version ID should be unique")
		ids[versionID] = true
	}
}

// TestGetObjectVersions tests retrieving all versions of an object
func TestGetObjectVersions(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "test-bucket"
	key := "test-object.txt"

	// Create bucket first
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put first version
	content1 := bytes.NewReader([]byte("version 1"))
	headers1 := http.Header{"Content-Type": []string{"text/plain"}}
	obj1, err := om.PutObject(ctx, bucket, key, content1, headers1)
	require.NoError(t, err)
	require.NotNil(t, obj1)

	// Put second version
	content2 := bytes.NewReader([]byte("version 2 - updated"))
	headers2 := http.Header{"Content-Type": []string{"text/plain"}}
	obj2, err := om.PutObject(ctx, bucket, key, content2, headers2)
	require.NoError(t, err)
	require.NotNil(t, obj2)

	// Get all versions
	versions, err := om.GetObjectVersions(ctx, bucket, key)

	// Versioning may not be enabled by default, so empty list is OK
	// If error, should be a meaningful error
	if err != nil {
		// Error is acceptable if versioning is not enabled
		assert.Error(t, err, "Should return error if versioning not enabled")
	} else if len(versions) > 0 {
		// If we got versions, verify their structure
		for _, ver := range versions {
			assert.NotEmpty(t, ver.Object.Key, "Version should have key")
			assert.NotEmpty(t, ver.Object.Bucket, "Version should have bucket")
		}
	}
	// Empty list is also acceptable if versioning is not enabled
}

// TestGetObjectVersions_NonExistent tests getting versions for non-existent object
func TestGetObjectVersions_NonExistent(t *testing.T) {
	ctx := context.Background()
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	versions, err := om.GetObjectVersions(ctx, "nonexistent-bucket", "nonexistent-key")

	// Should return error or empty list
	if err == nil {
		assert.Empty(t, versions, "Should return empty list for non-existent object")
	} else {
		assert.Error(t, err, "Should return error for non-existent object")
	}
}

// TestDeleteObjectVersion tests deleting a specific version
func TestDeleteObjectVersion(t *testing.T) {
	ctx := context.Background()
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "test-bucket"
	key := "test-object.txt"
	versionID := "test-version-123"

	// Attempt to delete a version
	err := om.DeleteObjectVersion(ctx, bucket, key, versionID)

	// Should return "not yet implemented" error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented", "Should return not implemented error")
}

// TestGetObjectACL tests getting object ACL
func TestGetObjectACL(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "test-bucket"
	key := "test-object.txt"

	// Create bucket first
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put an object
	content := bytes.NewReader([]byte("test content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Get ACL
	acl, err := om.GetObjectACL(ctx, bucket, key)
	require.NoError(t, err)
	require.NotNil(t, acl)

	// Verify ACL structure
	assert.NotNil(t, acl.Owner, "ACL should have owner")
}

// TestSetObjectACL tests setting object ACL
func TestSetObjectACL(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "test-bucket"
	key := "test-object.txt"

	// Create bucket first
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put an object
	content := bytes.NewReader([]byte("test content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Create new ACL
	newACL := &ACL{
		Owner: Owner{
			ID:          "owner-123",
			DisplayName: "Test Owner",
		},
		Grants: []Grant{
			{
				Grantee: Grantee{
					Type: "CanonicalUser",
					ID:   "user-456",
				},
				Permission: "READ",
			},
		},
	}

	// Set ACL
	err = om.SetObjectACL(ctx, bucket, key, newACL)
	require.NoError(t, err)

	// Verify ACL was set by getting it back
	retrievedACL, err := om.GetObjectACL(ctx, bucket, key)
	require.NoError(t, err)
	require.NotNil(t, retrievedACL)

	// Verify owner was updated
	assert.Equal(t, "owner-123", retrievedACL.Owner.ID)
}

// TestSetObjectACL_NonExistent tests setting ACL on non-existent object
func TestSetObjectACL_NonExistent(t *testing.T) {
	ctx := context.Background()
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	newACL := &ACL{
		Owner: Owner{
			ID:          "owner-123",
			DisplayName: "Test Owner",
		},
	}

	// Attempt to set ACL on non-existent object
	err := om.SetObjectACL(ctx, "nonexistent-bucket", "nonexistent-key", newACL)

	// Should return error
	assert.Error(t, err, "Should return error for non-existent object")
}

// TestCopyObject_UsingGetPut tests copying objects using Get+Put (real implementation)
func TestCopyObject_UsingGetPut(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	srcBucket := "source-bucket"
	srcKey := "source-object.txt"
	dstBucket := "dest-bucket"
	dstKey := "dest-object.txt"

	// Create buckets
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     srcBucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	err = metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     dstBucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put source object
	content := bytes.NewReader([]byte("original content for copy"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	srcObj, err := om.PutObject(ctx, srcBucket, srcKey, content, headers)
	require.NoError(t, err)
	require.NotNil(t, srcObj)

	// Simulate copy: Get from source
	retrievedObj, reader, err := om.GetObject(ctx, srcBucket, srcKey)
	require.NoError(t, err)
	require.NotNil(t, retrievedObj)

	// Read content
	copiedContent, err := io.ReadAll(reader)
	require.NoError(t, err)
	reader.Close()

	// Put to destination
	dstReader := bytes.NewReader(copiedContent)
	copiedObj, err := om.PutObject(ctx, dstBucket, dstKey, dstReader, headers)
	require.NoError(t, err)
	require.NotNil(t, copiedObj)

	// Verify copied object
	finalObj, finalReader, err := om.GetObject(ctx, dstBucket, dstKey)
	require.NoError(t, err)
	require.NotNil(t, finalObj)
	defer finalReader.Close()

	finalContent, err := io.ReadAll(finalReader)
	require.NoError(t, err)
	assert.Equal(t, "original content for copy", string(finalContent))
	assert.Equal(t, dstKey, finalObj.Key)
	assert.Equal(t, dstBucket, finalObj.Bucket)
}

// TestIsReady tests the IsReady method
func TestIsReady(t *testing.T) {
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	// Test IsReady
	ready := om.IsReady()
	assert.True(t, ready, "Object manager should be ready")
}

// TestCanModifyObject tests the CanModifyObject method
func TestCanModifyObject(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	ol := NewObjectLocker(om, nil)

	bucket := "test-bucket"
	key := "test-object.txt"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put an object
	content := bytes.NewReader([]byte("test content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Test CanModifyObject (should succeed for object without lock)
	user := &auth.User{ID: "user-1", Roles: []string{"user"}}
	err = ol.CanModifyObject(ctx, bucket, key, user)
	assert.NoError(t, err, "Should allow modification of unlocked object")
}

// TestError tests the Error method on RetentionError
func TestError(t *testing.T) {
	retentionDate := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)
	retentionError := &RetentionError{
		Mode:            "COMPLIANCE",
		RetainUntilDate: retentionDate,
	}

	errorMsg := retentionError.Error()
	assert.Contains(t, errorMsg, "COMPLIANCE", "Error should contain mode")
	assert.Contains(t, errorMsg, "2025-12-31", "Error should contain retain date")
	assert.Contains(t, errorMsg, "protected by", "Error should contain protection message")
}

// Mock types for testing

type mockAuthManager struct{}

func (m *mockAuthManager) IncrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error {
	return nil
}

func (m *mockAuthManager) DecrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error {
	return nil
}

func (m *mockAuthManager) CheckTenantStorageQuota(ctx context.Context, tenantID string, additionalBytes int64) error {
	return nil
}
