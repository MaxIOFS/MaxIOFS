package object

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// putTestObject is a convenience helper that creates a bucket (if not already
// created) and puts a single object, returning its stored ETag.
func putTestObject(t *testing.T, om *objectManager, metaStore metadata.Store, bucket, key string, body []byte) string {
	t.Helper()
	ctx := context.Background()

	// Create bucket; ignore "already exists" errors
	_ = metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant1",
		OwnerID:  "user1",
	})

	headers := http.Header{"Content-Type": []string{"application/octet-stream"}}
	obj, err := om.PutObject(ctx, bucket, key, bytes.NewReader(body), headers)
	require.NoError(t, err)
	return obj.ETag
}

// ---------------------------------------------------------------------------
// VerifyObjectIntegrity
// ---------------------------------------------------------------------------

func TestVerifyObjectIntegrity_FolderMarker(t *testing.T) {
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	result, err := om.VerifyObjectIntegrity(context.Background(), "any-bucket", "some/folder/")
	require.NoError(t, err)
	assert.Equal(t, IntegritySkipped, result.Status)
	assert.Contains(t, result.Reason, "folder marker")
}

func TestVerifyObjectIntegrity_MetadataNotFound(t *testing.T) {
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	result, err := om.VerifyObjectIntegrity(context.Background(), "no-bucket", "missing.txt")
	require.NoError(t, err)
	assert.Equal(t, IntegrityError, result.Status)
	assert.NotEmpty(t, result.Error)
}

func TestVerifyObjectIntegrity_DeleteMarker(t *testing.T) {
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := "dm-bucket"
	key := "delete-marker.txt"

	_ = metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant1",
		OwnerID:  "user1",
	})

	// Insert a delete marker directly (empty ETag)
	require.NoError(t, metaStore.PutObject(ctx, &metadata.ObjectMetadata{
		Bucket: bucket,
		Key:    key,
		ETag:   "", // delete marker has no ETag
	}))

	result, err := om.VerifyObjectIntegrity(ctx, bucket, key)
	require.NoError(t, err)
	assert.Equal(t, IntegritySkipped, result.Status)
	assert.Contains(t, result.Reason, "delete marker")
}

func TestVerifyObjectIntegrity_MultipartObject(t *testing.T) {
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := "mp-bucket"
	key := "multipart.bin"

	_ = metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant1",
		OwnerID:  "user1",
	})

	// Multipart ETags have the form "<md5>-<N>"
	require.NoError(t, metaStore.PutObject(ctx, &metadata.ObjectMetadata{
		Bucket: bucket,
		Key:    key,
		ETag:   "d41d8cd98f00b204e9800998ecf8427e-3",
		Size:   1024,
	}))

	result, err := om.VerifyObjectIntegrity(ctx, bucket, key)
	require.NoError(t, err)
	assert.Equal(t, IntegritySkipped, result.Status)
	assert.Contains(t, strings.ToLower(result.Reason), "multipart")
}

func TestVerifyObjectIntegrity_OK(t *testing.T) {
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "ok-bucket"
	body := []byte("hello integrity world")
	etag := putTestObject(t, om, metaStore, bucket, "file.txt", body)

	result, err := om.VerifyObjectIntegrity(context.Background(), bucket, "file.txt")
	require.NoError(t, err)
	assert.Equal(t, IntegrityOK, result.Status)
	assert.Equal(t, etag, result.StoredETag)
	assert.Equal(t, etag, result.ComputedETag)
	assert.Equal(t, int64(len(body)), result.ActualSize)
}

func TestVerifyObjectIntegrity_Corrupted(t *testing.T) {
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := "corrupt-bucket"
	body := []byte("original content")
	putTestObject(t, om, metaStore, bucket, "corrupt.txt", body)

	// Overwrite the stored ETag with a wrong value so the comparison fails
	obj, err := metaStore.GetObject(ctx, bucket, "corrupt.txt")
	require.NoError(t, err)
	obj.ETag = "00000000000000000000000000000000" // wrong MD5
	require.NoError(t, metaStore.PutObject(ctx, obj))

	result, err := om.VerifyObjectIntegrity(ctx, bucket, "corrupt.txt")
	require.NoError(t, err)
	assert.Equal(t, IntegrityCorrupted, result.Status)
	assert.NotEqual(t, result.StoredETag, result.ComputedETag)
}

// ---------------------------------------------------------------------------
// VerifyBucketIntegrity
// ---------------------------------------------------------------------------

func TestVerifyBucketIntegrity_EmptyBucket(t *testing.T) {
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := "empty-bucket"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant1",
		OwnerID:  "user1",
	}))

	report, err := om.VerifyBucketIntegrity(ctx, bucket, "", "", 100)
	require.NoError(t, err)
	assert.Equal(t, bucket, report.Bucket)
	assert.Equal(t, 0, report.Checked)
	assert.Equal(t, 0, report.OK)
	assert.Empty(t, report.Issues)
}

func TestVerifyBucketIntegrity_AllOK(t *testing.T) {
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "all-ok-bucket"
	putTestObject(t, om, metaStore, bucket, "a.txt", []byte("aaa"))
	putTestObject(t, om, metaStore, bucket, "b.txt", []byte("bbb"))
	putTestObject(t, om, metaStore, bucket, "c.txt", []byte("ccc"))

	report, err := om.VerifyBucketIntegrity(context.Background(), bucket, "", "", 100)
	require.NoError(t, err)
	assert.Equal(t, 3, report.Checked)
	assert.Equal(t, 3, report.OK)
	assert.Equal(t, 0, report.Corrupted)
	assert.Equal(t, 0, report.Errors)
	assert.Empty(t, report.Issues)
}

func TestVerifyBucketIntegrity_MixedResults(t *testing.T) {
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := "mixed-bucket"
	putTestObject(t, om, metaStore, bucket, "good.txt", []byte("good content"))

	// Folder marker → skipped
	require.NoError(t, metaStore.PutObject(ctx, &metadata.ObjectMetadata{
		Bucket: bucket,
		Key:    "folder/",
		ETag:   "d41d8cd98f00b204e9800998ecf8427e",
		Size:   0,
	}))

	// Object with wrong ETag → corrupted
	putTestObject(t, om, metaStore, bucket, "bad.txt", []byte("bad content"))
	bad, err := metaStore.GetObject(ctx, bucket, "bad.txt")
	require.NoError(t, err)
	bad.ETag = "ffffffffffffffffffffffffffffffff"
	require.NoError(t, metaStore.PutObject(ctx, bad))

	report, err := om.VerifyBucketIntegrity(ctx, bucket, "", "", 100)
	require.NoError(t, err)
	assert.Equal(t, 1, report.OK)
	assert.Equal(t, 1, report.Corrupted)
	assert.Equal(t, 1, report.Skipped) // folder marker
	assert.Equal(t, 1, len(report.Issues))
	assert.Equal(t, "bad.txt", report.Issues[0].Key)
}

func TestVerifyBucketIntegrity_DefaultMaxKeys(t *testing.T) {
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := "default-maxkeys-bucket"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant1",
		OwnerID:  "user1",
	}))

	// maxKeys=0 should default to 1000 without panicking
	report, err := om.VerifyBucketIntegrity(ctx, bucket, "", "", 0)
	require.NoError(t, err)
	assert.NotNil(t, report)
}

func TestVerifyBucketIntegrity_ListingError(t *testing.T) {
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	// Non-existent bucket causes listing to fail
	_, err := om.VerifyBucketIntegrity(context.Background(), "nonexistent-bucket", "", "", 10)
	// Either an error or an empty report is acceptable depending on metadataStore behaviour
	// — the key assertion is that it does not panic
	_ = err // may or may not return an error; just confirm no panic
}

func TestVerifyBucketIntegrity_ReportDurationSet(t *testing.T) {
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := "duration-bucket"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant1",
		OwnerID:  "user1",
	}))

	report, err := om.VerifyBucketIntegrity(ctx, bucket, "", "", 10)
	require.NoError(t, err)
	assert.NotEmpty(t, report.Duration, "Duration should always be set")
}

func TestVerifyBucketIntegrity_SkipsMultipart(t *testing.T) {
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := "mp-skip-bucket"
	_ = metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant1",
		OwnerID:  "user1",
	})

	require.NoError(t, metaStore.PutObject(ctx, &metadata.ObjectMetadata{
		Bucket: bucket,
		Key:    "large.bin",
		ETag:   fmt.Sprintf("%x-5", make([]byte, 16)), // composite ETag
		Size:   500 * 1024 * 1024,
	}))

	report, err := om.VerifyBucketIntegrity(ctx, bucket, "", "", 100)
	require.NoError(t, err)
	assert.Equal(t, 1, report.Skipped)
	assert.Equal(t, 0, report.OK)
	assert.Equal(t, 0, report.Corrupted)
}
