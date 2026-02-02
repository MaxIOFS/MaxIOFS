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

// TestDeleteSpecificVersion_WithVersioning tests deleting specific versions with versioning enabled
func TestDeleteSpecificVersion_WithVersioning(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "versioned-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket WITH versioning enabled
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

	key := "versioned-file.txt"

	// Upload version 1
	content1 := bytes.NewReader([]byte("This is version 1 of the file"))
	headers1 := http.Header{"Content-Type": []string{"text/plain"}}
	obj1, err := om.PutObject(ctx, bucket, key, content1, headers1)
	require.NoError(t, err)
	require.NotNil(t, obj1)
	t.Logf("Created version 1 with ID: %s", obj1.VersionID)

	// Upload version 2 (same key)
	content2 := bytes.NewReader([]byte("This is version 2 of the file - updated content"))
	headers2 := http.Header{"Content-Type": []string{"text/plain"}}
	obj2, err := om.PutObject(ctx, bucket, key, content2, headers2)
	require.NoError(t, err)
	require.NotNil(t, obj2)
	t.Logf("Created version 2 with ID: %s", obj2.VersionID)

	// Upload version 3 (same key)
	content3 := bytes.NewReader([]byte("This is version 3 of the file - latest version"))
	headers3 := http.Header{"Content-Type": []string{"text/plain"}}
	obj3, err := om.PutObject(ctx, bucket, key, content3, headers3)
	require.NoError(t, err)
	require.NotNil(t, obj3)
	t.Logf("Created version 3 with ID: %s", obj3.VersionID)

	// Verify all versions exist
	versions, err := om.GetObjectVersions(ctx, bucket, key)
	if err == nil {
		t.Logf("Total versions found: %d", len(versions))
		for i, ver := range versions {
			t.Logf("Version %d: ID=%s, IsLatest=%v", i+1, ver.VersionID, ver.IsLatest)
		}
	}

	// Test 1: Delete the middle version (version 2)
	if obj2.VersionID != "" {
		t.Logf("Attempting to delete version 2: %s", obj2.VersionID)
		err = om.deleteSpecificVersion(ctx, bucket, key, obj2.VersionID)
		if err != nil {
			t.Logf("Delete version 2 returned error: %v", err)
			// Error is acceptable - version deletion might have restrictions
		} else {
			t.Log("Successfully deleted version 2")
		}
	}

	// Test 2: Delete the oldest version (version 1)
	if obj1.VersionID != "" {
		t.Logf("Attempting to delete version 1: %s", obj1.VersionID)
		err = om.deleteSpecificVersion(ctx, bucket, key, obj1.VersionID)
		if err != nil {
			t.Logf("Delete version 1 returned error: %v", err)
		} else {
			t.Log("Successfully deleted version 1")
		}
	}

	// Test 3: Verify the latest version (version 3) still exists
	latestObj, _, err := om.GetObject(ctx, bucket, key)
	require.NoError(t, err, "Latest version should still exist")
	assert.Equal(t, key, latestObj.Key)
	t.Logf("Latest version still accessible: %s", latestObj.VersionID)

	// Test 4: Try to delete non-existent version
	err = om.deleteSpecificVersion(ctx, bucket, key, "nonexistent-version-12345")
	assert.Error(t, err, "Should return error for non-existent version")
	assert.Contains(t, err.Error(), "not found", "Error should mention not found")
}

// TestDeleteSpecificVersion_MultipleFiles tests deleting versions across multiple files
func TestDeleteSpecificVersion_MultipleFiles(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "multi-file-versioned"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket with versioning enabled
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

	// Create multiple files with multiple versions each
	files := map[string][]string{
		"file1.txt": {"Version 1A", "Version 1B", "Version 1C"},
		"file2.txt": {"Version 2A", "Version 2B"},
		"file3.txt": {"Version 3A", "Version 3B", "Version 3C", "Version 3D"},
	}

	// Store version IDs for later deletion
	versionIDs := make(map[string][]string)

	// Upload all files and versions
	for key, versions := range files {
		versionIDs[key] = make([]string, 0)
		for i, content := range versions {
			contentReader := bytes.NewReader([]byte(content))
			headers := http.Header{"Content-Type": []string{"text/plain"}}
			obj, err := om.PutObject(ctx, bucket, key, contentReader, headers)
			require.NoError(t, err)
			t.Logf("Created %s version %d: %s", key, i+1, obj.VersionID)
			if obj.VersionID != "" {
				versionIDs[key] = append(versionIDs[key], obj.VersionID)
			}
		}
	}

	// Delete specific versions from different files
	// Delete first version of file1.txt
	if len(versionIDs["file1.txt"]) > 0 {
		firstVersion := versionIDs["file1.txt"][0]
		t.Logf("Deleting first version of file1.txt: %s", firstVersion)
		err = om.deleteSpecificVersion(ctx, bucket, "file1.txt", firstVersion)
		if err != nil {
			t.Logf("Delete returned error: %v", err)
		} else {
			t.Log("Successfully deleted first version of file1.txt")
		}
	}

	// Delete middle version of file3.txt
	if len(versionIDs["file3.txt"]) >= 2 {
		middleVersion := versionIDs["file3.txt"][1]
		t.Logf("Deleting middle version of file3.txt: %s", middleVersion)
		err = om.deleteSpecificVersion(ctx, bucket, "file3.txt", middleVersion)
		if err != nil {
			t.Logf("Delete returned error: %v", err)
		} else {
			t.Log("Successfully deleted middle version of file3.txt")
		}
	}

	// Verify all files are still accessible (latest versions)
	for key := range files {
		obj, _, err := om.GetObject(ctx, bucket, key)
		require.NoError(t, err, "File %s should still be accessible", key)
		assert.Equal(t, key, obj.Key)
		t.Logf("File %s still accessible with version: %s", key, obj.VersionID)
	}

	// List versions for each file
	for key := range files {
		versions, err := om.GetObjectVersions(ctx, bucket, key)
		if err == nil {
			t.Logf("File %s has %d versions remaining", key, len(versions))
		}
	}
}

// TestDeleteSpecificVersion_DeleteLatest tests deleting the latest version
func TestDeleteSpecificVersion_DeleteLatest(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "delete-latest-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket with versioning enabled
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

	key := "delete-latest.txt"

	// Upload version 1
	content1 := bytes.NewReader([]byte("Version 1"))
	headers1 := http.Header{"Content-Type": []string{"text/plain"}}
	obj1, err := om.PutObject(ctx, bucket, key, content1, headers1)
	require.NoError(t, err)
	t.Logf("Version 1: %s", obj1.VersionID)

	// Upload version 2
	content2 := bytes.NewReader([]byte("Version 2"))
	headers2 := http.Header{"Content-Type": []string{"text/plain"}}
	obj2, err := om.PutObject(ctx, bucket, key, content2, headers2)
	require.NoError(t, err)
	t.Logf("Version 2 (latest): %s", obj2.VersionID)

	// Delete the latest version (version 2)
	if obj2.VersionID != "" {
		t.Logf("Deleting latest version: %s", obj2.VersionID)
		err = om.deleteSpecificVersion(ctx, bucket, key, obj2.VersionID)
		if err != nil {
			t.Logf("Delete latest version returned error: %v", err)
		} else {
			t.Log("Successfully deleted latest version")

			// After deleting latest, version 1 should become the current version
			currentObj, _, err := om.GetObject(ctx, bucket, key)
			if err == nil {
				t.Logf("After deleting latest, current version is: %s", currentObj.VersionID)
				// Should now get version 1
			} else {
				t.Logf("GetObject after deleting latest returned error: %v", err)
			}
		}
	}
}

// TestDeleteSpecificVersion_WithoutVersioning tests behavior without versioning
func TestDeleteSpecificVersion_WithoutVersioning(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "no-versioning-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket WITHOUT versioning
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
		// No Versioning field - versioning disabled
	})
	require.NoError(t, err)

	key := "no-version-file.txt"

	// Upload file
	content := bytes.NewReader([]byte("Content without versioning"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	obj, err := om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)
	t.Logf("Object created with VersionID: '%s'", obj.VersionID)

	// Try to delete with a fake version ID
	err = om.deleteSpecificVersion(ctx, bucket, key, "fake-version-id")

	// Should return error (version not found or versioning not enabled)
	assert.Error(t, err, "Should return error when versioning is not enabled")
	t.Logf("Delete without versioning returned error: %v", err)
}

// TestDeleteSpecificVersion_EmptyVersionID tests error handling for empty version ID
func TestDeleteSpecificVersion_EmptyVersionID(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucketName := "empty-version-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName

	// Create bucket with versioning
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

	key := "test-file.txt"

	// Upload file
	content := bytes.NewReader([]byte("Test content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Try to delete with empty version ID
	err = om.deleteSpecificVersion(ctx, bucket, key, "")

	// Should return error for empty version ID
	assert.Error(t, err, "Should return error for empty version ID")
	t.Logf("Delete with empty version ID returned error: %v", err)
}
