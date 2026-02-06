package metadata

import (
	"context"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Helper
// ============================================================================

func setupTagsTestStore(t *testing.T) (*BadgerStore, func()) {
	tmpDir, err := os.MkdirTemp("", "badger-tags-test-*")
	require.NoError(t, err)

	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)

	store, err := NewBadgerStore(BadgerOptions{
		DataDir:           tmpDir,
		SyncWrites:        false,
		CompactionEnabled: false,
		Logger:            logger,
	})
	require.NoError(t, err)

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

// ============================================================================
// PutObjectTags Tests
// ============================================================================

func TestPutObjectTags_Success(t *testing.T) {
	store, cleanup := setupTagsTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create object first
	obj := &ObjectMetadata{
		Bucket: "tags-bucket",
		Key:    "tag-object",
		Size:   100,
	}
	err := store.PutObject(ctx, obj)
	require.NoError(t, err)

	// Put tags
	tags := map[string]string{
		"environment": "production",
		"team":        "backend",
		"cost-center": "12345",
	}
	err = store.PutObjectTags(ctx, "tags-bucket", "tag-object", tags)
	assert.NoError(t, err)

	// Verify
	retrieved, err := store.GetObjectTags(ctx, "tags-bucket", "tag-object")
	assert.NoError(t, err)
	assert.Len(t, retrieved, 3)
	assert.Equal(t, "production", retrieved["environment"])
	assert.Equal(t, "backend", retrieved["team"])
	assert.Equal(t, "12345", retrieved["cost-center"])
}

func TestPutObjectTags_ObjectNotFound(t *testing.T) {
	store, cleanup := setupTagsTestStore(t)
	defer cleanup()
	ctx := context.Background()

	tags := map[string]string{"key": "value"}
	err := store.PutObjectTags(ctx, "bucket", "non-existent-key", tags)
	assert.ErrorIs(t, err, ErrObjectNotFound)
}

func TestPutObjectTags_ReplacesExistingTags(t *testing.T) {
	store, cleanup := setupTagsTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create object with initial tags
	obj := &ObjectMetadata{
		Bucket: "replace-bucket",
		Key:    "replace-object",
		Size:   100,
		Tags: map[string]string{
			"old-key": "old-value",
		},
	}
	err := store.PutObject(ctx, obj)
	require.NoError(t, err)

	// Replace tags
	newTags := map[string]string{
		"new-key": "new-value",
	}
	err = store.PutObjectTags(ctx, "replace-bucket", "replace-object", newTags)
	assert.NoError(t, err)

	// Verify old tags are gone
	tags, err := store.GetObjectTags(ctx, "replace-bucket", "replace-object")
	assert.NoError(t, err)
	assert.Len(t, tags, 1)
	assert.Empty(t, tags["old-key"])
	assert.Equal(t, "new-value", tags["new-key"])
}

func TestPutObjectTags_EmptyTags(t *testing.T) {
	store, cleanup := setupTagsTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create object with tags
	obj := &ObjectMetadata{
		Bucket: "empty-tags-bucket",
		Key:    "empty-tags-object",
		Size:   100,
		Tags: map[string]string{
			"key": "value",
		},
	}
	err := store.PutObject(ctx, obj)
	require.NoError(t, err)

	// Set empty tags
	err = store.PutObjectTags(ctx, "empty-tags-bucket", "empty-tags-object", map[string]string{})
	assert.NoError(t, err)

	// Verify tags are cleared
	tags, err := store.GetObjectTags(ctx, "empty-tags-bucket", "empty-tags-object")
	assert.NoError(t, err)
	assert.Empty(t, tags)
}

// ============================================================================
// GetObjectTags Tests
// ============================================================================

func TestGetObjectTags_ObjectNotFound(t *testing.T) {
	store, cleanup := setupTagsTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := store.GetObjectTags(ctx, "bucket", "non-existent")
	assert.ErrorIs(t, err, ErrObjectNotFound)
}

func TestGetObjectTags_NoTags(t *testing.T) {
	store, cleanup := setupTagsTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create object without tags
	obj := &ObjectMetadata{
		Bucket: "no-tags-bucket",
		Key:    "no-tags-object",
		Size:   100,
	}
	err := store.PutObject(ctx, obj)
	require.NoError(t, err)

	tags, err := store.GetObjectTags(ctx, "no-tags-bucket", "no-tags-object")
	assert.NoError(t, err)
	assert.NotNil(t, tags) // Should return empty map, not nil
	assert.Empty(t, tags)
}

// ============================================================================
// DeleteObjectTags Tests
// ============================================================================

func TestDeleteObjectTags_Success(t *testing.T) {
	store, cleanup := setupTagsTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create object with tags
	obj := &ObjectMetadata{
		Bucket: "delete-tags-bucket",
		Key:    "delete-tags-object",
		Size:   100,
		Tags: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}
	err := store.PutObject(ctx, obj)
	require.NoError(t, err)

	// Delete tags
	err = store.DeleteObjectTags(ctx, "delete-tags-bucket", "delete-tags-object")
	assert.NoError(t, err)

	// Verify
	tags, err := store.GetObjectTags(ctx, "delete-tags-bucket", "delete-tags-object")
	assert.NoError(t, err)
	assert.Empty(t, tags)
}

func TestDeleteObjectTags_ObjectNotFound(t *testing.T) {
	store, cleanup := setupTagsTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.DeleteObjectTags(ctx, "bucket", "non-existent")
	assert.ErrorIs(t, err, ErrObjectNotFound)
}

// ============================================================================
// ListObjectsByTags Tests
// ============================================================================

func TestListObjectsByTags_Success(t *testing.T) {
	store, cleanup := setupTagsTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create objects with various tags
	objects := []struct {
		key  string
		tags map[string]string
	}{
		{"obj-1", map[string]string{"env": "prod", "team": "backend"}},
		{"obj-2", map[string]string{"env": "prod", "team": "frontend"}},
		{"obj-3", map[string]string{"env": "dev", "team": "backend"}},
		{"obj-4", map[string]string{"env": "prod", "team": "backend"}},
	}

	for _, o := range objects {
		obj := &ObjectMetadata{
			Bucket: "search-bucket",
			Key:    o.key,
			Size:   100,
			Tags:   o.tags,
		}
		err := store.PutObject(ctx, obj)
		require.NoError(t, err)
	}

	t.Run("single tag match", func(t *testing.T) {
		found, err := store.ListObjectsByTags(ctx, "search-bucket", map[string]string{"env": "prod"})
		assert.NoError(t, err)
		assert.Len(t, found, 3)
	})

	t.Run("multiple tags match", func(t *testing.T) {
		found, err := store.ListObjectsByTags(ctx, "search-bucket", map[string]string{
			"env":  "prod",
			"team": "backend",
		})
		assert.NoError(t, err)
		assert.Len(t, found, 2)
	})

	t.Run("no match", func(t *testing.T) {
		found, err := store.ListObjectsByTags(ctx, "search-bucket", map[string]string{"env": "staging"})
		assert.NoError(t, err)
		assert.Empty(t, found)
	})
}

func TestListObjectsByTags_EmptyTags(t *testing.T) {
	store, cleanup := setupTagsTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := store.ListObjectsByTags(ctx, "bucket", map[string]string{})
	assert.Error(t, err)
}

func TestListObjectsByTags_EmptyBucket(t *testing.T) {
	store, cleanup := setupTagsTestStore(t)
	defer cleanup()
	ctx := context.Background()

	found, err := store.ListObjectsByTags(ctx, "empty-bucket", map[string]string{"key": "value"})
	assert.NoError(t, err)
	assert.Empty(t, found)
}

// ============================================================================
// matchesTags Tests (helper function)
// ============================================================================

func TestMatchesTags(t *testing.T) {
	tests := []struct {
		name         string
		objectTags   map[string]string
		requiredTags map[string]string
		expected     bool
	}{
		{
			name:         "exact match",
			objectTags:   map[string]string{"env": "prod", "team": "backend"},
			requiredTags: map[string]string{"env": "prod", "team": "backend"},
			expected:     true,
		},
		{
			name:         "partial match - all required present",
			objectTags:   map[string]string{"env": "prod", "team": "backend", "extra": "value"},
			requiredTags: map[string]string{"env": "prod"},
			expected:     true,
		},
		{
			name:         "missing required tag",
			objectTags:   map[string]string{"env": "prod"},
			requiredTags: map[string]string{"env": "prod", "team": "backend"},
			expected:     false,
		},
		{
			name:         "value mismatch",
			objectTags:   map[string]string{"env": "dev"},
			requiredTags: map[string]string{"env": "prod"},
			expected:     false,
		},
		{
			name:         "empty required - always matches",
			objectTags:   map[string]string{"env": "prod"},
			requiredTags: map[string]string{},
			expected:     true,
		},
		{
			name:         "empty object tags",
			objectTags:   map[string]string{},
			requiredTags: map[string]string{"env": "prod"},
			expected:     false,
		},
		{
			name:         "nil object tags",
			objectTags:   nil,
			requiredTags: map[string]string{"env": "prod"},
			expected:     false,
		},
		{
			name:         "nil required",
			objectTags:   map[string]string{"env": "prod"},
			requiredTags: nil,
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesTags(tt.objectTags, tt.requiredTags)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ============================================================================
// Tag Index Integrity Tests
// ============================================================================

func TestTagIndex_CreatedOnObjectPut(t *testing.T) {
	store, cleanup := setupTagsTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create object with tags
	obj := &ObjectMetadata{
		Bucket: "index-bucket",
		Key:    "indexed-object",
		Size:   100,
		Tags: map[string]string{
			"category": "important",
		},
	}
	err := store.PutObject(ctx, obj)
	require.NoError(t, err)

	// Search by tag should find it
	found, err := store.ListObjectsByTags(ctx, "index-bucket", map[string]string{"category": "important"})
	assert.NoError(t, err)
	assert.Len(t, found, 1)
	assert.Equal(t, "indexed-object", found[0].Key)
}

func TestTagIndex_CleanedOnObjectDelete(t *testing.T) {
	store, cleanup := setupTagsTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create object with tags
	obj := &ObjectMetadata{
		Bucket: "cleanup-bucket",
		Key:    "cleanup-object",
		Size:   100,
		Tags: map[string]string{
			"searchable": "yes",
		},
	}
	err := store.PutObject(ctx, obj)
	require.NoError(t, err)

	// Verify it's searchable
	found, err := store.ListObjectsByTags(ctx, "cleanup-bucket", map[string]string{"searchable": "yes"})
	require.NoError(t, err)
	require.Len(t, found, 1)

	// Delete object
	err = store.DeleteObject(ctx, "cleanup-bucket", "cleanup-object")
	require.NoError(t, err)

	// Tag index should be cleaned (search should return empty)
	found, err = store.ListObjectsByTags(ctx, "cleanup-bucket", map[string]string{"searchable": "yes"})
	assert.NoError(t, err)
	assert.Empty(t, found)
}

func TestTagIndex_UpdatedOnTagChange(t *testing.T) {
	store, cleanup := setupTagsTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create object with initial tags
	obj := &ObjectMetadata{
		Bucket: "update-bucket",
		Key:    "update-object",
		Size:   100,
		Tags: map[string]string{
			"status": "active",
		},
	}
	err := store.PutObject(ctx, obj)
	require.NoError(t, err)

	// Verify initial search works
	found, err := store.ListObjectsByTags(ctx, "update-bucket", map[string]string{"status": "active"})
	require.NoError(t, err)
	require.Len(t, found, 1)

	// Update tags
	err = store.PutObjectTags(ctx, "update-bucket", "update-object", map[string]string{"status": "archived"})
	require.NoError(t, err)

	// Old tag should not find object
	found, err = store.ListObjectsByTags(ctx, "update-bucket", map[string]string{"status": "active"})
	assert.NoError(t, err)
	assert.Empty(t, found)

	// New tag should find object
	found, err = store.ListObjectsByTags(ctx, "update-bucket", map[string]string{"status": "archived"})
	assert.NoError(t, err)
	assert.Len(t, found, 1)
}
