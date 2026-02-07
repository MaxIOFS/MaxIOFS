package metadata

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSearchTestData(t *testing.T) (Store, func()) {
	store, cleanup := setupObjectTestStore(t)
	ctx := context.Background()

	// Create bucket
	err := store.CreateBucket(ctx, &BucketMetadata{
		Name:     "search-bucket",
		TenantID: "tenant-1",
	})
	require.NoError(t, err)

	now := time.Now()

	objects := []*ObjectMetadata{
		{
			Bucket:       "search-bucket",
			Key:          "photo.jpg",
			Size:         2048,
			ContentType:  "image/jpeg",
			LastModified: now.Add(-24 * time.Hour),
			ETag:         "etag1",
			Tags:         map[string]string{"env": "prod", "team": "alpha"},
		},
		{
			Bucket:       "search-bucket",
			Key:          "doc.pdf",
			Size:         1048576, // 1MB
			ContentType:  "application/pdf",
			LastModified: now.Add(-48 * time.Hour),
			ETag:         "etag2",
			Tags:         map[string]string{"env": "prod"},
		},
		{
			Bucket:       "search-bucket",
			Key:          "video.mp4",
			Size:         10485760, // 10MB
			ContentType:  "video/mp4",
			LastModified: now.Add(-72 * time.Hour),
			ETag:         "etag3",
		},
		{
			Bucket:       "search-bucket",
			Key:          "small.txt",
			Size:         128,
			ContentType:  "text/plain",
			LastModified: now.Add(-1 * time.Hour),
			ETag:         "etag4",
			Tags:         map[string]string{"env": "dev"},
		},
		{
			Bucket:       "search-bucket",
			Key:          "logo.png",
			Size:         4096,
			ContentType:  "image/png",
			LastModified: now.Add(-12 * time.Hour),
			ETag:         "etag5",
			Tags:         map[string]string{"env": "prod", "team": "alpha"},
		},
	}

	for _, obj := range objects {
		err := store.PutObject(ctx, obj)
		require.NoError(t, err)
	}

	return store, cleanup
}

func TestSearchObjects_NoFilter(t *testing.T) {
	store, cleanup := setupSearchTestData(t)
	defer cleanup()

	objects, nextMarker, err := store.SearchObjects(context.Background(), "search-bucket", "", "", 100, nil)
	require.NoError(t, err)
	assert.Len(t, objects, 5)
	assert.Empty(t, nextMarker)
}

func TestSearchObjects_ContentTypeFilter(t *testing.T) {
	store, cleanup := setupSearchTestData(t)
	defer cleanup()

	filter := &ObjectFilter{
		ContentTypes: []string{"image/"},
	}

	objects, _, err := store.SearchObjects(context.Background(), "search-bucket", "", "", 100, filter)
	require.NoError(t, err)
	assert.Len(t, objects, 2)
	for _, obj := range objects {
		assert.Contains(t, obj.ContentType, "image/")
	}
}

func TestSearchObjects_SizeRangeFilter(t *testing.T) {
	store, cleanup := setupSearchTestData(t)
	defer cleanup()

	minSize := int64(1024)
	maxSize := int64(5000)

	filter := &ObjectFilter{
		MinSize: &minSize,
		MaxSize: &maxSize,
	}

	objects, _, err := store.SearchObjects(context.Background(), "search-bucket", "", "", 100, filter)
	require.NoError(t, err)
	assert.Len(t, objects, 2) // photo.jpg (2048) and logo.png (4096)
	for _, obj := range objects {
		assert.GreaterOrEqual(t, obj.Size, minSize)
		assert.LessOrEqual(t, obj.Size, maxSize)
	}
}

func TestSearchObjects_DateRangeFilter(t *testing.T) {
	store, cleanup := setupSearchTestData(t)
	defer cleanup()

	// PutObject sets LastModified to time.Now(), so all objects have ~current time.
	// Test ModifiedAfter: all 5 objects were just created, so they should all be after 1 hour ago
	after := time.Now().Add(-1 * time.Hour)
	filter := &ObjectFilter{
		ModifiedAfter: &after,
	}

	objects, _, err := store.SearchObjects(context.Background(), "search-bucket", "", "", 100, filter)
	require.NoError(t, err)
	assert.Len(t, objects, 5, "All objects were just created, so all should match after 1h ago")

	// Test ModifiedBefore: none should be before 1 hour ago since they were just created
	before := time.Now().Add(-1 * time.Hour)
	filter = &ObjectFilter{
		ModifiedBefore: &before,
	}

	objects, _, err = store.SearchObjects(context.Background(), "search-bucket", "", "", 100, filter)
	require.NoError(t, err)
	assert.Len(t, objects, 0, "No objects should be older than 1 hour")

	// Test range: all objects should be between 1h ago and 1h from now
	future := time.Now().Add(1 * time.Hour)
	filter = &ObjectFilter{
		ModifiedAfter:  &after,
		ModifiedBefore: &future,
	}

	objects, _, err = store.SearchObjects(context.Background(), "search-bucket", "", "", 100, filter)
	require.NoError(t, err)
	assert.Len(t, objects, 5)
}

func TestSearchObjects_TagFilter(t *testing.T) {
	store, cleanup := setupSearchTestData(t)
	defer cleanup()

	filter := &ObjectFilter{
		Tags: map[string]string{"env": "prod"},
	}

	objects, _, err := store.SearchObjects(context.Background(), "search-bucket", "", "", 100, filter)
	require.NoError(t, err)
	assert.Len(t, objects, 3) // photo.jpg, doc.pdf, logo.png
}

func TestSearchObjects_TagFilterMultiple(t *testing.T) {
	store, cleanup := setupSearchTestData(t)
	defer cleanup()

	filter := &ObjectFilter{
		Tags: map[string]string{"env": "prod", "team": "alpha"},
	}

	objects, _, err := store.SearchObjects(context.Background(), "search-bucket", "", "", 100, filter)
	require.NoError(t, err)
	assert.Len(t, objects, 2) // photo.jpg, logo.png
}

func TestSearchObjects_CombinedFilters(t *testing.T) {
	store, cleanup := setupSearchTestData(t)
	defer cleanup()

	minSize := int64(1000)

	filter := &ObjectFilter{
		ContentTypes: []string{"image/"},
		MinSize:      &minSize,
	}

	objects, _, err := store.SearchObjects(context.Background(), "search-bucket", "", "", 100, filter)
	require.NoError(t, err)
	assert.Len(t, objects, 2) // photo.jpg (2048, image/jpeg) and logo.png (4096, image/png)
}

func TestSearchObjects_Pagination(t *testing.T) {
	store, cleanup := setupSearchTestData(t)
	defer cleanup()

	// Get first 2
	objects1, nextMarker, err := store.SearchObjects(context.Background(), "search-bucket", "", "", 2, nil)
	require.NoError(t, err)
	assert.Len(t, objects1, 2)
	assert.NotEmpty(t, nextMarker)

	// Get next page
	objects2, _, err := store.SearchObjects(context.Background(), "search-bucket", "", nextMarker, 2, nil)
	require.NoError(t, err)
	assert.Len(t, objects2, 2)

	// Ensure no overlap
	for _, o1 := range objects1 {
		for _, o2 := range objects2 {
			assert.NotEqual(t, o1.Key, o2.Key)
		}
	}
}

func TestSearchObjects_PrefixFilter(t *testing.T) {
	store, cleanup := setupSearchTestData(t)
	defer cleanup()

	// Add objects with prefix
	ctx := context.Background()
	err := store.PutObject(ctx, &ObjectMetadata{
		Bucket:       "search-bucket",
		Key:          "images/cat.jpg",
		Size:         500,
		ContentType:  "image/jpeg",
		LastModified: time.Now(),
		ETag:         "etag-prefix",
	})
	require.NoError(t, err)

	objects, _, err := store.SearchObjects(ctx, "search-bucket", "images/", "", 100, nil)
	require.NoError(t, err)
	assert.Len(t, objects, 1)
	assert.Equal(t, "images/cat.jpg", objects[0].Key)
}

func TestSearchObjects_EmptyBucket(t *testing.T) {
	store, cleanup := setupObjectTestStore(t)
	defer cleanup()

	err := store.CreateBucket(context.Background(), &BucketMetadata{
		Name:     "empty-bucket",
		TenantID: "tenant-1",
	})
	require.NoError(t, err)

	objects, nextMarker, err := store.SearchObjects(context.Background(), "empty-bucket", "", "", 100, nil)
	require.NoError(t, err)
	assert.Empty(t, objects)
	assert.Empty(t, nextMarker)
}

func TestSearchObjects_MinSizeOnly(t *testing.T) {
	store, cleanup := setupSearchTestData(t)
	defer cleanup()

	minSize := int64(1048576) // 1MB

	filter := &ObjectFilter{
		MinSize: &minSize,
	}

	objects, _, err := store.SearchObjects(context.Background(), "search-bucket", "", "", 100, filter)
	require.NoError(t, err)
	assert.Len(t, objects, 2) // doc.pdf (1MB) and video.mp4 (10MB)
}

func TestSearchObjects_MultipleContentTypes(t *testing.T) {
	store, cleanup := setupSearchTestData(t)
	defer cleanup()

	filter := &ObjectFilter{
		ContentTypes: []string{"image/", "text/"},
	}

	objects, _, err := store.SearchObjects(context.Background(), "search-bucket", "", "", 100, filter)
	require.NoError(t, err)
	assert.Len(t, objects, 3) // photo.jpg, logo.png, small.txt
}
