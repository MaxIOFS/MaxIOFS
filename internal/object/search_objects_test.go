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

func TestSearchObjects_Basic(t *testing.T) {
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := "search-test-bucket"
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Upload objects with different content types
	_, err = om.PutObject(ctx, bucket, "image.jpg", bytes.NewReader([]byte("fake image data")), http.Header{
		"Content-Type": []string{"image/jpeg"},
	})
	require.NoError(t, err)

	_, err = om.PutObject(ctx, bucket, "doc.pdf", bytes.NewReader([]byte("fake pdf data here with more bytes")), http.Header{
		"Content-Type": []string{"application/pdf"},
	})
	require.NoError(t, err)

	_, err = om.PutObject(ctx, bucket, "readme.txt", bytes.NewReader([]byte("hello")), http.Header{
		"Content-Type": []string{"text/plain"},
	})
	require.NoError(t, err)

	// Test no filter - returns all
	result, err := om.SearchObjects(ctx, bucket, "", "", "", 100, nil)
	require.NoError(t, err)
	assert.Len(t, result.Objects, 3)

	// Test content type filter
	filter := &metadata.ObjectFilter{
		ContentTypes: []string{"image/"},
	}
	result, err = om.SearchObjects(ctx, bucket, "", "", "", 100, filter)
	require.NoError(t, err)
	assert.Len(t, result.Objects, 1)
	assert.Equal(t, "image.jpg", result.Objects[0].Key)

	// Test size filter
	minSize := int64(10)
	filter = &metadata.ObjectFilter{
		MinSize: &minSize,
	}
	result, err = om.SearchObjects(ctx, bucket, "", "", "", 100, filter)
	require.NoError(t, err)
	assert.Len(t, result.Objects, 2) // image.jpg and doc.pdf have >10 bytes
}

func TestSearchObjects_BucketNotFound(t *testing.T) {
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	_, err := om.SearchObjects(context.Background(), "nonexistent-bucket", "", "", "", 100, nil)
	assert.Error(t, err)
	assert.Equal(t, ErrBucketNotFound, err)
}

func TestSearchObjects_WithDelimiter(t *testing.T) {
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := "delimiter-search-bucket"
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Upload files in folders
	for _, key := range []string{"images/a.jpg", "images/b.png", "docs/readme.txt", "root.txt"} {
		_, err = om.PutObject(ctx, bucket, key, bytes.NewReader([]byte("data")), http.Header{
			"Content-Type": []string{"text/plain"},
		})
		require.NoError(t, err)
	}

	// Search with delimiter
	result, err := om.SearchObjects(ctx, bucket, "", "/", "", 100, nil)
	require.NoError(t, err)
	assert.Len(t, result.Objects, 1) // root.txt
	assert.Len(t, result.CommonPrefixes, 2) // images/, docs/
}

func TestSearchObjects_EmptyFilter(t *testing.T) {
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()

	bucket := "empty-filter-bucket"
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	_, err = om.PutObject(ctx, bucket, "test.txt", bytes.NewReader([]byte("data")), http.Header{
		"Content-Type": []string{"text/plain"},
	})
	require.NoError(t, err)

	// Empty filter should return everything (same as nil)
	filter := &metadata.ObjectFilter{}
	result, err := om.SearchObjects(ctx, bucket, "", "", "", 100, filter)
	require.NoError(t, err)
	assert.Len(t, result.Objects, 1)
}
