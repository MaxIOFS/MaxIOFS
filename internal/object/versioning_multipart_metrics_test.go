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

func createVersionedTestBucket(t *testing.T, ctx context.Context, metaStore metadata.Store, tenantID, bucketName string) string {
	t.Helper()

	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
		Versioning: &metadata.VersioningMetadata{
			Enabled: true,
			Status:  "Enabled",
		},
	}))

	return tenantID + "/" + bucketName
}

func TestCompleteMultipartUpload_VersionedBucketCreatesVersions(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := createVersionedTestBucket(t, ctx, metaStore, "tenant-1", "multipart-versioned-bucket")
	key := "large-object.bin"
	headers := http.Header{"Content-Type": []string{"application/octet-stream"}}

	upload1, err := om.CreateMultipartUpload(ctx, bucket, key, headers)
	require.NoError(t, err)
	part1, err := om.UploadPart(ctx, upload1.UploadID, 1, bytes.NewReader([]byte("first multipart version")))
	require.NoError(t, err)
	obj1, err := om.CompleteMultipartUpload(ctx, upload1.UploadID, []Part{*part1})
	require.NoError(t, err)
	require.NotEmpty(t, obj1.VersionID)

	upload2, err := om.CreateMultipartUpload(ctx, bucket, key, headers)
	require.NoError(t, err)
	part2, err := om.UploadPart(ctx, upload2.UploadID, 1, bytes.NewReader([]byte("second multipart version")))
	require.NoError(t, err)
	obj2, err := om.CompleteMultipartUpload(ctx, upload2.UploadID, []Part{*part2})
	require.NoError(t, err)
	require.NotEmpty(t, obj2.VersionID)
	assert.NotEqual(t, obj1.VersionID, obj2.VersionID)

	versions, err := om.GetObjectVersions(ctx, bucket, key)
	require.NoError(t, err)
	assert.Len(t, versions, 2)

	for _, versionID := range []string{obj1.VersionID, obj2.VersionID} {
		_, reader, err := om.GetObject(ctx, bucket, key, versionID)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
	}
}

func TestVersionedDeleteMarkerMetricsAreOnlyAdjustedOnVisibilityChanges(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := createVersionedTestBucket(t, ctx, metaStore, "tenant-1", "delete-marker-metrics-bucket")
	metrics := &mockMetricsBucketManager{}
	om.bucketManager = metrics

	key := "hidden-then-restored.txt"
	_, err := om.PutObject(ctx, bucket, key, bytes.NewReader([]byte("visible")), http.Header{"Content-Type": []string{"text/plain"}})
	require.NoError(t, err)

	metrics.decrementCalled = false
	_, err = om.DeleteObject(ctx, bucket, key, false)
	require.NoError(t, err)
	assert.True(t, metrics.decrementCalled, "first delete marker should hide a visible object")

	metrics.decrementCalled = false
	_, err = om.DeleteObject(ctx, bucket, key, false)
	require.NoError(t, err)
	assert.False(t, metrics.decrementCalled, "repeated delete marker must not decrement visible object count again")

	metrics.incrementCalled = false
	metrics.adjustCalled = false
	_, err = om.PutObject(ctx, bucket, key, bytes.NewReader([]byte("visible again")), http.Header{"Content-Type": []string{"text/plain"}})
	require.NoError(t, err)
	assert.True(t, metrics.incrementCalled, "PUT over latest delete marker should make the object visible again")
	assert.False(t, metrics.adjustCalled, "restoring visibility should not be treated as a plain additional version")
}
