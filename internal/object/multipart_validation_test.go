package object

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultipartUploadMissingUploadDoesNotLeavePartFile(t *testing.T) {
	ctx := context.Background()
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	_, err := om.UploadPart(ctx, "missing-upload", 1, bytes.NewReader([]byte("orphan")))
	require.ErrorIs(t, err, ErrUploadNotFound)

	exists, existsErr := om.storage.Exists(ctx, om.getMultipartPartPath("missing-upload", 1))
	require.NoError(t, existsErr)
	assert.False(t, exists, "UploadPart should reject missing uploads before writing part data")
}

func TestMultipartListAndAbortMissingUploadReturnNoSuchUpload(t *testing.T) {
	ctx := context.Background()
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	_, err := om.ListParts(ctx, "missing-upload")
	require.ErrorIs(t, err, ErrUploadNotFound)

	err = om.AbortMultipartUpload(ctx, "missing-upload")
	require.ErrorIs(t, err, ErrUploadNotFound)
}

func TestCompleteMultipartUploadValidatesETagAndOrder(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	bucket := "multipart-validation-bucket"
	key := "object.bin"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	}))

	upload, err := om.CreateMultipartUpload(ctx, bucket, key, http.Header{"Content-Type": []string{"application/octet-stream"}})
	require.NoError(t, err)
	part1, err := om.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader([]byte("part-one")))
	require.NoError(t, err)
	part2, err := om.UploadPart(ctx, upload.UploadID, 2, bytes.NewReader([]byte("part-two")))
	require.NoError(t, err)

	_, err = om.CompleteMultipartUpload(ctx, upload.UploadID, []Part{
		{PartNumber: 1, ETag: "00000000000000000000000000000000"},
		*part2,
	})
	require.ErrorIs(t, err, ErrInvalidPart)

	_, err = om.CompleteMultipartUpload(ctx, upload.UploadID, []Part{*part2, *part1})
	require.ErrorIs(t, err, ErrInvalidPartOrder)

	_, err = om.metadataStore.GetMultipartUpload(ctx, upload.UploadID)
	require.NoError(t, err, "failed completions must leave the upload available for retry")

	obj, err := om.CompleteMultipartUpload(ctx, upload.UploadID, []Part{*part1, *part2})
	require.NoError(t, err)
	require.NotNil(t, obj)

	_, err = om.metadataStore.GetMultipartUpload(ctx, upload.UploadID)
	assert.True(t, errors.Is(err, metadata.ErrUploadNotFound), "successful completion should remove upload metadata")
}
