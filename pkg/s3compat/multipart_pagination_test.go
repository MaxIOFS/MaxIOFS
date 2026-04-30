package s3compat

import (
	"testing"

	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
)

func TestPaginateMultipartUploads_DoesNotTruncateExactPage(t *testing.T) {
	uploads := []object.MultipartUpload{
		{Key: "a.txt", UploadID: "upload-1"},
	}

	page, truncated, nextKey, nextUploadID := paginateMultipartUploads(uploads, "", "", 1)

	assert.Len(t, page, 1)
	assert.False(t, truncated)
	assert.Empty(t, nextKey)
	assert.Empty(t, nextUploadID)
}

func TestPaginateMultipartUploads_KeyMarkerIsExclusive(t *testing.T) {
	uploads := []object.MultipartUpload{
		{Key: "a.txt", UploadID: "upload-1"},
		{Key: "b.txt", UploadID: "upload-2"},
	}

	page, truncated, _, _ := paginateMultipartUploads(uploads, "a.txt", "", 1000)

	assert.False(t, truncated)
	assert.Len(t, page, 1)
	assert.Equal(t, "b.txt", page[0].Key)
}

func TestPaginateMultipartUploads_TruncatesOnlyWhenMoreResultsExist(t *testing.T) {
	uploads := []object.MultipartUpload{
		{Key: "a.txt", UploadID: "upload-1"},
		{Key: "b.txt", UploadID: "upload-2"},
		{Key: "c.txt", UploadID: "upload-3"},
	}

	page, truncated, nextKey, nextUploadID := paginateMultipartUploads(uploads, "", "", 2)

	assert.Len(t, page, 2)
	assert.True(t, truncated)
	assert.Equal(t, "b.txt", nextKey)
	assert.Equal(t, "upload-2", nextUploadID)
}

func TestPaginateMultipartParts_DetectsSparseTruncation(t *testing.T) {
	parts := []object.Part{
		{PartNumber: 100},
		{PartNumber: 200},
		{PartNumber: 300},
	}

	page, truncated, nextMarker := paginateMultipartParts(parts, 150, 1)

	assert.Len(t, page, 1)
	assert.Equal(t, 200, page[0].PartNumber)
	assert.True(t, truncated)
	assert.Equal(t, 200, nextMarker)
}

func TestPaginateMultipartParts_DoesNotTruncateExactPage(t *testing.T) {
	parts := []object.Part{
		{PartNumber: 100},
		{PartNumber: 200},
	}

	page, truncated, nextMarker := paginateMultipartParts(parts, 150, 1)

	assert.Len(t, page, 1)
	assert.Equal(t, 200, page[0].PartNumber)
	assert.False(t, truncated)
	assert.Zero(t, nextMarker)
}
