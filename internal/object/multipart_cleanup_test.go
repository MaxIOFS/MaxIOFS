package object

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/require"
)

func uploadIDsOnDisk(t *testing.T, om *objectManager) []string {
	t.Helper()
	ids, err := om.storage.ListUploads(context.Background())
	require.NoError(t, err)
	return ids
}

func TestAbortLeavesNoUploadDirectoryBehind(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	const bucket = "multipart-abort"
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucket, OwnerID: "u"}))

	upload, err := om.CreateMultipartUpload(ctx, bucket, "big.bin", http.Header{})
	require.NoError(t, err)

	_, err = om.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(bytes.Repeat([]byte("a"), 1024)))
	require.NoError(t, err)
	require.Equal(t, []string{upload.UploadID}, uploadIDsOnDisk(t, om))

	require.NoError(t, om.AbortMultipartUpload(ctx, upload.UploadID))
	require.Empty(t, uploadIDsOnDisk(t, om), "aborting must not leave the upload directory behind")
}

func TestCompleteLeavesNoUploadDirectoryBehind(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	const bucket = "multipart-complete"
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucket, OwnerID: "u"}))

	upload, err := om.CreateMultipartUpload(ctx, bucket, "big.bin", http.Header{})
	require.NoError(t, err)

	part, err := om.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(bytes.Repeat([]byte("b"), 1024)))
	require.NoError(t, err)

	_, err = om.CompleteMultipartUpload(ctx, upload.UploadID, []Part{{PartNumber: 1, ETag: part.ETag}})
	require.NoError(t, err)

	require.Empty(t, uploadIDsOnDisk(t, om), "completing must not leave the upload directory behind")

	obj, err := om.GetObjectMetadata(ctx, bucket, "big.bin")
	require.NoError(t, err)
	require.Equal(t, int64(1024), obj.Size)
}

// The upload record is the only way back to stored parts, so a cleanup that
// could not remove them must not remove it either.
func TestUploadRecordSurvivesWhenPartsCannotBeRemoved(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	const bucket = "multipart-stuck"
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucket, OwnerID: "u"}))

	upload, err := om.CreateMultipartUpload(ctx, bucket, "big.bin", http.Header{})
	require.NoError(t, err)
	_, err = om.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(bytes.Repeat([]byte("c"), 1024)))
	require.NoError(t, err)

	om.storage = &stuckPartBackend{Backend: om.storage}

	require.Error(t, om.AbortMultipartUpload(ctx, upload.UploadID),
		"an abort that could not remove the parts must not report success")

	_, err = store.GetMultipartUpload(ctx, upload.UploadID)
	require.NoError(t, err, "the upload record must survive so the cleanup can be retried")
}

// stuckPartBackend refuses to delete parts, standing in for a file another
// process is holding open.
type stuckPartBackend struct {
	storage.Backend
}

func (s *stuckPartBackend) DeletePart(context.Context, string, int) error {
	return storage.NewError("Busy", "the file is in use")
}
