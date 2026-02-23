package metadata

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// PutObjectVersion Tests
// ============================================================================

func setupVersioningTestStore(t *testing.T) (*PebbleStore, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "pebble-versioning-test-*")
	require.NoError(t, err)

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	store, err := NewPebbleStore(PebbleOptions{
		DataDir: tmpDir,
		Logger:  logger,
	})
	require.NoError(t, err)

	cleanup := func() {
		_ = store.Close()
		_ = os.RemoveAll(tmpDir) // ignore error on Windows file locking
	}

	return store, cleanup
}

func TestPutObjectVersion_Success(t *testing.T) {
	store, cleanup := setupVersioningTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket
	bucket := &BucketMetadata{
		Name:       "versioning-bucket",
		TenantID:   "tenant-1",
		OwnerID:    "user-1",
		OwnerType:  "user",
		Versioning: &VersioningMetadata{Enabled: true, Status: "Enabled"},
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Create first version
	obj := &ObjectMetadata{
		Bucket:      "versioning-bucket",
		Key:         "versioned-object",
		Size:        100,
		ETag:        "etag-v1",
		ContentType: "text/plain",
		VersionID:   "v1",
	}

	version1 := &ObjectVersion{
		VersionID:    "v1",
		IsLatest:     true,
		Key:          "versioned-object",
		Size:         100,
		ETag:         "etag-v1",
		LastModified: time.Now(),
	}

	err = store.PutObjectVersion(ctx, obj, version1)
	assert.NoError(t, err)

	// Create second version
	obj2 := &ObjectMetadata{
		Bucket:      "versioning-bucket",
		Key:         "versioned-object",
		Size:        200,
		ETag:        "etag-v2",
		ContentType: "text/plain",
		VersionID:   "v2",
	}

	version2 := &ObjectVersion{
		VersionID:    "v2",
		IsLatest:     true,
		Key:          "versioned-object",
		Size:         200,
		ETag:         "etag-v2",
		LastModified: time.Now(),
	}

	err = store.PutObjectVersion(ctx, obj2, version2)
	assert.NoError(t, err)
}

func TestPutObjectVersion_NilInputs(t *testing.T) {
	store, cleanup := setupVersioningTestStore(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("nil object", func(t *testing.T) {
		version := &ObjectVersion{VersionID: "v1"}
		err := store.PutObjectVersion(ctx, nil, version)
		assert.Error(t, err)
	})

	t.Run("nil version", func(t *testing.T) {
		obj := &ObjectMetadata{Bucket: "bucket", Key: "key"}
		err := store.PutObjectVersion(ctx, obj, nil)
		assert.Error(t, err)
	})
}

func TestPutObjectVersion_MarksOldVersionsAsNotLatest(t *testing.T) {
	store, cleanup := setupVersioningTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket
	bucket := &BucketMetadata{
		Name:      "version-latest-bucket",
		TenantID:  "tenant-1",
		OwnerID:   "user-1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Create first version as latest
	obj1 := &ObjectMetadata{
		Bucket:    "version-latest-bucket",
		Key:       "my-object",
		VersionID: "v1",
		Size:      100,
	}
	version1 := &ObjectVersion{
		VersionID:    "v1",
		Key:          "my-object",
		IsLatest:     true,
		Size:         100,
		LastModified: time.Now().Add(-time.Hour),
	}
	err = store.PutObjectVersion(ctx, obj1, version1)
	require.NoError(t, err)

	// Create second version as latest
	obj2 := &ObjectMetadata{
		Bucket:    "version-latest-bucket",
		Key:       "my-object",
		VersionID: "v2",
		Size:      200,
	}
	version2 := &ObjectVersion{
		VersionID:    "v2",
		Key:          "my-object",
		IsLatest:     true,
		Size:         200,
		LastModified: time.Now(),
	}
	err = store.PutObjectVersion(ctx, obj2, version2)
	require.NoError(t, err)

	// Get all versions
	versions, err := store.GetObjectVersions(ctx, "version-latest-bucket", "my-object")
	assert.NoError(t, err)
	assert.Len(t, versions, 2)

	// Only the latest should be marked as IsLatest
	latestCount := 0
	for _, v := range versions {
		if v.IsLatest {
			latestCount++
			assert.Equal(t, "v2", v.VersionID)
		}
	}
	assert.Equal(t, 1, latestCount)
}

// ============================================================================
// GetObjectVersions Tests
// ============================================================================

func TestGetObjectVersions_Success(t *testing.T) {
	store, cleanup := setupVersioningTestStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := &BucketMetadata{
		Name:      "get-versions-bucket",
		TenantID:  "tenant-1",
		OwnerID:   "user-1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Create multiple versions
	for i := 0; i < 5; i++ {
		obj := &ObjectMetadata{
			Bucket:    "get-versions-bucket",
			Key:       "multi-version-obj",
			VersionID: "v" + string(rune('1'+i)),
			Size:      int64(100 * (i + 1)),
		}
		version := &ObjectVersion{
			VersionID:    "v" + string(rune('1'+i)),
			Key:          "multi-version-obj",
			IsLatest:     i == 4, // Last one is latest
			Size:         int64(100 * (i + 1)),
			LastModified: time.Now().Add(time.Duration(i) * time.Minute),
		}
		err := store.PutObjectVersion(ctx, obj, version)
		require.NoError(t, err)
	}

	versions, err := store.GetObjectVersions(ctx, "get-versions-bucket", "multi-version-obj")
	assert.NoError(t, err)
	assert.Len(t, versions, 5)

	// Should be sorted by LastModified descending (newest first)
	for i := 0; i < len(versions)-1; i++ {
		assert.True(t, versions[i].LastModified.After(versions[i+1].LastModified) ||
			versions[i].LastModified.Equal(versions[i+1].LastModified))
	}
}

func TestGetObjectVersions_NoVersions(t *testing.T) {
	store, cleanup := setupVersioningTestStore(t)
	defer cleanup()
	ctx := context.Background()

	versions, err := store.GetObjectVersions(ctx, "non-existent-bucket", "non-existent-key")
	assert.NoError(t, err)
	assert.Empty(t, versions)
}

// ============================================================================
// ListAllObjectVersions Tests
// ============================================================================

func TestListAllObjectVersions_Success(t *testing.T) {
	store, cleanup := setupVersioningTestStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := &BucketMetadata{
		Name:      "list-all-versions-bucket",
		TenantID:  "tenant-1",
		OwnerID:   "user-1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Create versioned objects
	objects := []string{"obj-a", "obj-b", "obj-c"}
	for _, objKey := range objects {
		for v := 1; v <= 3; v++ {
			obj := &ObjectMetadata{
				Bucket:    "list-all-versions-bucket",
				Key:       objKey,
				VersionID: objKey + "-v" + string(rune('0'+v)),
				Size:      int64(100 * v),
			}
			version := &ObjectVersion{
				VersionID:    objKey + "-v" + string(rune('0'+v)),
				Key:          objKey,
				IsLatest:     v == 3,
				Size:         int64(100 * v),
				LastModified: time.Now().Add(time.Duration(v) * time.Minute),
			}
			err := store.PutObjectVersion(ctx, obj, version)
			require.NoError(t, err)
		}
	}

	// List all versions
	versions, err := store.ListAllObjectVersions(ctx, "list-all-versions-bucket", "", 0)
	assert.NoError(t, err)
	assert.Len(t, versions, 9) // 3 objects * 3 versions
}

func TestListAllObjectVersions_WithPrefix(t *testing.T) {
	store, cleanup := setupVersioningTestStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := &BucketMetadata{
		Name:      "prefix-versions-bucket",
		TenantID:  "tenant-1",
		OwnerID:   "user-1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Create objects with different prefixes
	keys := []string{"photos/2024/img1.jpg", "photos/2024/img2.jpg", "docs/file.pdf", "docs/report.pdf"}
	for _, key := range keys {
		obj := &ObjectMetadata{
			Bucket:    "prefix-versions-bucket",
			Key:       key,
			VersionID: key + "-v1",
			Size:      100,
		}
		version := &ObjectVersion{
			VersionID:    key + "-v1",
			Key:          key,
			IsLatest:     true,
			Size:         100,
			LastModified: time.Now(),
		}
		err := store.PutObjectVersion(ctx, obj, version)
		require.NoError(t, err)
	}

	// List with prefix
	photosVersions, err := store.ListAllObjectVersions(ctx, "prefix-versions-bucket", "photos/", 0)
	assert.NoError(t, err)
	assert.Len(t, photosVersions, 2)

	docsVersions, err := store.ListAllObjectVersions(ctx, "prefix-versions-bucket", "docs/", 0)
	assert.NoError(t, err)
	assert.Len(t, docsVersions, 2)
}

func TestListAllObjectVersions_WithMaxKeys(t *testing.T) {
	store, cleanup := setupVersioningTestStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := &BucketMetadata{
		Name:      "maxkeys-versions-bucket",
		TenantID:  "tenant-1",
		OwnerID:   "user-1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Create 10 versions
	for i := 0; i < 10; i++ {
		obj := &ObjectMetadata{
			Bucket:    "maxkeys-versions-bucket",
			Key:       "obj-" + string(rune('a'+i)),
			VersionID: "v" + string(rune('0'+i)),
			Size:      100,
		}
		version := &ObjectVersion{
			VersionID:    "v" + string(rune('0'+i)),
			Key:          "obj-" + string(rune('a'+i)),
			IsLatest:     true,
			Size:         100,
			LastModified: time.Now(),
		}
		err := store.PutObjectVersion(ctx, obj, version)
		require.NoError(t, err)
	}

	// Limit to 5
	versions, err := store.ListAllObjectVersions(ctx, "maxkeys-versions-bucket", "", 5)
	assert.NoError(t, err)
	assert.Len(t, versions, 5)
}

func TestListAllObjectVersions_IncludesNonVersionedObjects(t *testing.T) {
	store, cleanup := setupVersioningTestStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := &BucketMetadata{
		Name:      "mixed-versions-bucket",
		TenantID:  "tenant-1",
		OwnerID:   "user-1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Create a regular object (non-versioned)
	regularObj := &ObjectMetadata{
		Bucket: "mixed-versions-bucket",
		Key:    "regular-object.txt",
		Size:   100,
		ETag:   "etag-regular",
	}
	err = store.PutObject(ctx, regularObj)
	require.NoError(t, err)

	// List all versions - should include the non-versioned object
	versions, err := store.ListAllObjectVersions(ctx, "mixed-versions-bucket", "", 0)
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(versions), 1)

	// Find the regular object
	found := false
	for _, v := range versions {
		if v.Key == "regular-object.txt" {
			found = true
			assert.True(t, v.IsLatest)
			assert.Empty(t, v.VersionID) // Non-versioned objects have empty version ID
		}
	}
	assert.True(t, found)
}

// ============================================================================
// DeleteObjectVersion Tests
// ============================================================================

func TestDeleteObjectVersion_Success(t *testing.T) {
	store, cleanup := setupVersioningTestStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := &BucketMetadata{
		Name:      "delete-version-bucket",
		TenantID:  "tenant-1",
		OwnerID:   "user-1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Create a version
	obj := &ObjectMetadata{
		Bucket:    "delete-version-bucket",
		Key:       "delete-me",
		VersionID: "v1-to-delete",
		Size:      100,
	}
	version := &ObjectVersion{
		VersionID:    "v1-to-delete",
		Key:          "delete-me",
		IsLatest:     true,
		Size:         100,
		LastModified: time.Now(),
	}
	err = store.PutObjectVersion(ctx, obj, version)
	require.NoError(t, err)

	// Verify it exists
	versions, err := store.GetObjectVersions(ctx, "delete-version-bucket", "delete-me")
	assert.NoError(t, err)
	assert.Len(t, versions, 1)

	// Delete the version
	err = store.DeleteObjectVersion(ctx, "delete-version-bucket", "delete-me", "v1-to-delete")
	assert.NoError(t, err)

	// Verify deleted
	versions, err = store.GetObjectVersions(ctx, "delete-version-bucket", "delete-me")
	assert.NoError(t, err)
	assert.Empty(t, versions)
}

func TestDeleteObjectVersion_NotFound(t *testing.T) {
	store, cleanup := setupVersioningTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.DeleteObjectVersion(ctx, "bucket", "key", "non-existent-version")
	assert.ErrorIs(t, err, ErrVersionNotFound)
}

// ============================================================================
// GetObject with VersionID Tests
// ============================================================================

func TestGetObject_WithVersionID(t *testing.T) {
	store, cleanup := setupVersioningTestStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := &BucketMetadata{
		Name:      "get-with-version-bucket",
		TenantID:  "tenant-1",
		OwnerID:   "user-1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Create two versions
	for i := 1; i <= 2; i++ {
		obj := &ObjectMetadata{
			Bucket:    "get-with-version-bucket",
			Key:       "versioned-key",
			VersionID: "version-" + string(rune('0'+i)),
			Size:      int64(i * 100),
			ETag:      "etag-" + string(rune('0'+i)),
		}
		version := &ObjectVersion{
			VersionID:    "version-" + string(rune('0'+i)),
			Key:          "versioned-key",
			IsLatest:     i == 2,
			Size:         int64(i * 100),
			LastModified: time.Now(),
		}
		err := store.PutObjectVersion(ctx, obj, version)
		require.NoError(t, err)
	}

	// Get specific version
	retrieved, err := store.GetObject(ctx, "get-with-version-bucket", "versioned-key", "version-1")
	assert.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, int64(100), retrieved.Size)
}

// ============================================================================
// DeleteObject with VersionID Tests
// ============================================================================

func TestDeleteObject_WithVersionID(t *testing.T) {
	store, cleanup := setupVersioningTestStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := &BucketMetadata{
		Name:      "delete-with-version-bucket",
		TenantID:  "tenant-1",
		OwnerID:   "user-1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Create versioned object
	obj := &ObjectMetadata{
		Bucket:    "delete-with-version-bucket",
		Key:       "delete-versioned",
		VersionID: "specific-version",
		Size:      100,
	}
	version := &ObjectVersion{
		VersionID:    "specific-version",
		Key:          "delete-versioned",
		IsLatest:     true,
		Size:         100,
		LastModified: time.Now(),
	}
	err = store.PutObjectVersion(ctx, obj, version)
	require.NoError(t, err)

	// Delete with version ID
	err = store.DeleteObject(ctx, "delete-with-version-bucket", "delete-versioned", "specific-version")
	assert.NoError(t, err)
}
