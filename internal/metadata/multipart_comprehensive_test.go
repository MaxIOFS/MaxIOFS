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
// Helper
// ============================================================================

func setupMultipartTestStore(t *testing.T) (*PebbleStore, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "pebble-multipart-test-*")
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

// ============================================================================
// CreateMultipartUpload Tests
// ============================================================================

func TestCreateMultipartUpload_Success(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	upload := &MultipartUploadMetadata{
		UploadID:    "upload-123",
		Bucket:      "test-bucket",
		Key:         "large-file.zip",
		ContentType: "application/zip",
		OwnerID:     "user-1",
		Metadata: map[string]string{
			"x-amz-meta-custom": "value",
		},
	}

	err := store.CreateMultipartUpload(ctx, upload)
	assert.NoError(t, err)

	// Verify
	retrieved, err := store.GetMultipartUpload(ctx, "upload-123")
	assert.NoError(t, err)
	assert.Equal(t, "large-file.zip", retrieved.Key)
	assert.Equal(t, "application/zip", retrieved.ContentType)
	assert.False(t, retrieved.Initiated.IsZero())
}

func TestCreateMultipartUpload_NilUpload(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.CreateMultipartUpload(ctx, nil)
	assert.Error(t, err)
}

func TestCreateMultipartUpload_EmptyUploadID(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	upload := &MultipartUploadMetadata{
		UploadID: "",
		Bucket:   "bucket",
		Key:      "key",
	}

	err := store.CreateMultipartUpload(ctx, upload)
	assert.Error(t, err)
}

func TestCreateMultipartUpload_SetsInitiatedTimestamp(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	upload := &MultipartUploadMetadata{
		UploadID: "timestamp-upload",
		Bucket:   "bucket",
		Key:      "key",
	}

	beforeCreate := time.Now()
	err := store.CreateMultipartUpload(ctx, upload)
	require.NoError(t, err)

	retrieved, err := store.GetMultipartUpload(ctx, "timestamp-upload")
	assert.NoError(t, err)
	assert.True(t, retrieved.Initiated.After(beforeCreate) || retrieved.Initiated.Equal(beforeCreate))
}

// ============================================================================
// GetMultipartUpload Tests
// ============================================================================

func TestGetMultipartUpload_NotFound(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := store.GetMultipartUpload(ctx, "non-existent-upload")
	assert.ErrorIs(t, err, ErrUploadNotFound)
}

// ============================================================================
// ListMultipartUploads Tests
// ============================================================================

func TestListMultipartUploads_Success(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create multiple uploads
	for i := 0; i < 5; i++ {
		upload := &MultipartUploadMetadata{
			UploadID: "list-upload-" + string(rune('a'+i)),
			Bucket:   "list-bucket",
			Key:      "file-" + string(rune('a'+i)) + ".zip",
			OwnerID:  "user-1",
		}
		err := store.CreateMultipartUpload(ctx, upload)
		require.NoError(t, err)
	}

	uploads, err := store.ListMultipartUploads(ctx, "list-bucket", "", 10)
	assert.NoError(t, err)
	assert.Len(t, uploads, 5)
}

func TestListMultipartUploads_WithPrefix(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create uploads with different key prefixes
	keys := []string{"photos/img1.jpg", "photos/img2.jpg", "docs/file.pdf"}
	for i, key := range keys {
		upload := &MultipartUploadMetadata{
			UploadID: "prefix-upload-" + string(rune('a'+i)),
			Bucket:   "prefix-bucket",
			Key:      key,
			OwnerID:  "user-1",
		}
		err := store.CreateMultipartUpload(ctx, upload)
		require.NoError(t, err)
	}

	// List with prefix
	photosUploads, err := store.ListMultipartUploads(ctx, "prefix-bucket", "photos/", 10)
	assert.NoError(t, err)
	assert.Len(t, photosUploads, 2)

	docsUploads, err := store.ListMultipartUploads(ctx, "prefix-bucket", "docs/", 10)
	assert.NoError(t, err)
	assert.Len(t, docsUploads, 1)
}

func TestListMultipartUploads_MaxUploads(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create 10 uploads
	for i := 0; i < 10; i++ {
		upload := &MultipartUploadMetadata{
			UploadID: "max-upload-" + string(rune('a'+i)),
			Bucket:   "max-bucket",
			Key:      "file-" + string(rune('a'+i)),
			OwnerID:  "user-1",
		}
		err := store.CreateMultipartUpload(ctx, upload)
		require.NoError(t, err)
	}

	// List with limit
	uploads, err := store.ListMultipartUploads(ctx, "max-bucket", "", 5)
	assert.NoError(t, err)
	assert.Len(t, uploads, 5)
}

func TestListMultipartUploads_DefaultMaxUploads(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	upload := &MultipartUploadMetadata{
		UploadID: "default-limit-upload",
		Bucket:   "default-limit-bucket",
		Key:      "file.zip",
		OwnerID:  "user-1",
	}
	err := store.CreateMultipartUpload(ctx, upload)
	require.NoError(t, err)

	// Pass 0 or negative - should use default
	uploads, err := store.ListMultipartUploads(ctx, "default-limit-bucket", "", 0)
	assert.NoError(t, err)
	assert.Len(t, uploads, 1)

	uploads, err = store.ListMultipartUploads(ctx, "default-limit-bucket", "", -1)
	assert.NoError(t, err)
	assert.Len(t, uploads, 1)
}

func TestListMultipartUploads_SortedByInitiated(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create uploads with different initiated times
	for i := 0; i < 3; i++ {
		upload := &MultipartUploadMetadata{
			UploadID:  "sorted-upload-" + string(rune('a'+i)),
			Bucket:    "sorted-bucket",
			Key:       "file-" + string(rune('a'+i)),
			OwnerID:   "user-1",
			Initiated: time.Now().Add(time.Duration(i) * time.Hour),
		}
		err := store.CreateMultipartUpload(ctx, upload)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond) // Small delay to ensure different timestamps
	}

	uploads, err := store.ListMultipartUploads(ctx, "sorted-bucket", "", 10)
	assert.NoError(t, err)

	// Should be sorted by initiated time descending (newest first)
	for i := 0; i < len(uploads)-1; i++ {
		assert.True(t, uploads[i].Initiated.After(uploads[i+1].Initiated) ||
			uploads[i].Initiated.Equal(uploads[i+1].Initiated))
	}
}

func TestListMultipartUploads_EmptyBucket(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	uploads, err := store.ListMultipartUploads(ctx, "empty-bucket", "", 10)
	assert.NoError(t, err)
	assert.Empty(t, uploads)
}

// ============================================================================
// AbortMultipartUpload Tests
// ============================================================================

func TestAbortMultipartUpload_Success(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create upload
	upload := &MultipartUploadMetadata{
		UploadID: "abort-upload",
		Bucket:   "abort-bucket",
		Key:      "abort-file.zip",
		OwnerID:  "user-1",
	}
	err := store.CreateMultipartUpload(ctx, upload)
	require.NoError(t, err)

	// Add some parts
	for i := 1; i <= 3; i++ {
		part := &PartMetadata{
			UploadID:   "abort-upload",
			PartNumber: i,
			Size:       1024 * 1024 * 5, // 5MB
			ETag:       "part-etag-" + string(rune('0'+i)),
		}
		err := store.PutPart(ctx, part)
		require.NoError(t, err)
	}

	// Abort
	err = store.AbortMultipartUpload(ctx, "abort-upload")
	assert.NoError(t, err)

	// Verify upload is gone
	_, err = store.GetMultipartUpload(ctx, "abort-upload")
	assert.ErrorIs(t, err, ErrUploadNotFound)

	// Verify parts are gone
	parts, err := store.ListParts(ctx, "abort-upload")
	assert.NoError(t, err)
	assert.Empty(t, parts)
}

func TestAbortMultipartUpload_NotFound(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.AbortMultipartUpload(ctx, "non-existent-upload")
	assert.ErrorIs(t, err, ErrUploadNotFound)
}

// ============================================================================
// CompleteMultipartUpload Tests
// ============================================================================

func TestCompleteMultipartUpload_Success(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create bucket
	bucket := &BucketMetadata{
		Name:      "complete-bucket",
		TenantID:  "tenant-1",
		OwnerID:   "user-1",
		OwnerType: "user",
	}
	err := store.CreateBucket(ctx, bucket)
	require.NoError(t, err)

	// Create upload
	upload := &MultipartUploadMetadata{
		UploadID:    "complete-upload",
		Bucket:      "complete-bucket",
		Key:         "completed-file.zip",
		ContentType: "application/zip",
		OwnerID:     "user-1",
	}
	err = store.CreateMultipartUpload(ctx, upload)
	require.NoError(t, err)

	// Add parts
	totalSize := int64(0)
	for i := 1; i <= 3; i++ {
		partSize := int64(1024 * 1024 * 5) // 5MB
		totalSize += partSize
		part := &PartMetadata{
			UploadID:   "complete-upload",
			PartNumber: i,
			Size:       partSize,
			ETag:       "part-" + string(rune('0'+i)) + "-etag",
		}
		err := store.PutPart(ctx, part)
		require.NoError(t, err)
	}

	// Complete the upload
	finalObject := &ObjectMetadata{
		Bucket:      "complete-bucket",
		Key:         "completed-file.zip",
		Size:        totalSize,
		ETag:        "final-etag-abc123",
		ContentType: "application/zip",
	}

	err = store.CompleteMultipartUpload(ctx, "complete-upload", finalObject)
	assert.NoError(t, err)

	// Verify object exists
	obj, err := store.GetObject(ctx, "complete-bucket", "completed-file.zip")
	assert.NoError(t, err)
	assert.Equal(t, totalSize, obj.Size)
	assert.Equal(t, "final-etag-abc123", obj.ETag)

	// Verify upload metadata is cleaned up
	_, err = store.GetMultipartUpload(ctx, "complete-upload")
	assert.ErrorIs(t, err, ErrUploadNotFound)

	// Verify parts are cleaned up
	parts, err := store.ListParts(ctx, "complete-upload")
	assert.NoError(t, err)
	assert.Empty(t, parts)
}

func TestCompleteMultipartUpload_NotFound(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	finalObject := &ObjectMetadata{
		Bucket: "bucket",
		Key:    "key",
		Size:   100,
	}

	err := store.CompleteMultipartUpload(ctx, "non-existent-upload", finalObject)
	assert.ErrorIs(t, err, ErrUploadNotFound)
}

// ============================================================================
// PutPart Tests
// ============================================================================

func TestPutPart_Success(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create upload first
	upload := &MultipartUploadMetadata{
		UploadID: "part-upload",
		Bucket:   "part-bucket",
		Key:      "part-file.zip",
		OwnerID:  "user-1",
	}
	err := store.CreateMultipartUpload(ctx, upload)
	require.NoError(t, err)

	// Add part
	part := &PartMetadata{
		UploadID:   "part-upload",
		PartNumber: 1,
		Size:       5 * 1024 * 1024, // 5MB
		ETag:       "part-1-etag",
	}

	err = store.PutPart(ctx, part)
	assert.NoError(t, err)

	// Verify
	retrieved, err := store.GetPart(ctx, "part-upload", 1)
	assert.NoError(t, err)
	assert.Equal(t, int64(5*1024*1024), retrieved.Size)
	assert.Equal(t, "part-1-etag", retrieved.ETag)
	assert.False(t, retrieved.LastModified.IsZero())
}

func TestPutPart_NilPart(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := store.PutPart(ctx, nil)
	assert.Error(t, err)
}

func TestPutPart_InvalidPartNumber(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	part := &PartMetadata{
		UploadID:   "some-upload",
		PartNumber: 0, // Invalid
		Size:       100,
	}

	err := store.PutPart(ctx, part)
	assert.Error(t, err)
}

func TestPutPart_EmptyUploadID(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	part := &PartMetadata{
		UploadID:   "",
		PartNumber: 1,
		Size:       100,
	}

	err := store.PutPart(ctx, part)
	assert.Error(t, err)
}

func TestPutPart_UploadNotFound(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	part := &PartMetadata{
		UploadID:   "non-existent-upload",
		PartNumber: 1,
		Size:       100,
	}

	err := store.PutPart(ctx, part)
	assert.ErrorIs(t, err, ErrUploadNotFound)
}

func TestPutPart_OverwriteExisting(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create upload
	upload := &MultipartUploadMetadata{
		UploadID: "overwrite-upload",
		Bucket:   "bucket",
		Key:      "key",
		OwnerID:  "user-1",
	}
	err := store.CreateMultipartUpload(ctx, upload)
	require.NoError(t, err)

	// Add part
	part1 := &PartMetadata{
		UploadID:   "overwrite-upload",
		PartNumber: 1,
		Size:       100,
		ETag:       "original-etag",
	}
	err = store.PutPart(ctx, part1)
	require.NoError(t, err)

	// Overwrite with new content
	part2 := &PartMetadata{
		UploadID:   "overwrite-upload",
		PartNumber: 1,
		Size:       200,
		ETag:       "new-etag",
	}
	err = store.PutPart(ctx, part2)
	assert.NoError(t, err)

	// Verify overwritten
	retrieved, err := store.GetPart(ctx, "overwrite-upload", 1)
	assert.NoError(t, err)
	assert.Equal(t, int64(200), retrieved.Size)
	assert.Equal(t, "new-etag", retrieved.ETag)
}

// ============================================================================
// GetPart Tests
// ============================================================================

func TestGetPart_NotFound(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := store.GetPart(ctx, "some-upload", 999)
	assert.ErrorIs(t, err, ErrPartNotFound)
}

// ============================================================================
// ListParts Tests
// ============================================================================

func TestListParts_Success(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Create upload
	upload := &MultipartUploadMetadata{
		UploadID: "list-parts-upload",
		Bucket:   "bucket",
		Key:      "key",
		OwnerID:  "user-1",
	}
	err := store.CreateMultipartUpload(ctx, upload)
	require.NoError(t, err)

	// Add parts in random order
	partNumbers := []int{3, 1, 5, 2, 4}
	for _, pn := range partNumbers {
		part := &PartMetadata{
			UploadID:   "list-parts-upload",
			PartNumber: pn,
			Size:       int64(pn * 1024),
			ETag:       "etag-" + string(rune('0'+pn)),
		}
		err := store.PutPart(ctx, part)
		require.NoError(t, err)
	}

	// List parts
	parts, err := store.ListParts(ctx, "list-parts-upload")
	assert.NoError(t, err)
	assert.Len(t, parts, 5)

	// Verify sorted by part number
	for i := 0; i < len(parts); i++ {
		assert.Equal(t, i+1, parts[i].PartNumber)
	}
}

func TestListParts_EmptyUpload(t *testing.T) {
	store, cleanup := setupMultipartTestStore(t)
	defer cleanup()
	ctx := context.Background()

	parts, err := store.ListParts(ctx, "empty-upload")
	assert.NoError(t, err)
	assert.Empty(t, parts)
}

// ============================================================================
// hasPrefix Tests (helper function)
// ============================================================================

func TestHasPrefix(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		prefix   string
		expected bool
	}{
		{"exact match", "photos/", "photos/", true},
		{"prefix match", "photos/img1.jpg", "photos/", true},
		{"no match", "docs/file.pdf", "photos/", false},
		{"empty prefix", "anything", "", true},
		{"empty string", "", "prefix", false},
		{"longer prefix", "ph", "photos/", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasPrefix(tt.s, tt.prefix)
			assert.Equal(t, tt.expected, result)
		})
	}
}
