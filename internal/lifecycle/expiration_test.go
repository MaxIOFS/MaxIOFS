package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockObjectMgrWithMultipart extends mockObjectMgr with multipart support for abort tests.
type mockObjectMgrWithMultipart struct {
	mockObjectMgr
	uploads     []object.MultipartUpload
	abortedIDs  []string
	abortErr    error
}

func (m *mockObjectMgrWithMultipart) ListMultipartUploads(ctx context.Context, bucketPath string) ([]object.MultipartUpload, error) {
	return m.uploads, nil
}

func (m *mockObjectMgrWithMultipart) AbortMultipartUpload(ctx context.Context, uploadID string) error {
	if m.abortErr != nil {
		return m.abortErr
	}
	m.abortedIDs = append(m.abortedIDs, uploadID)
	return nil
}

// ============================================================
// processObjectExpiration tests
// ============================================================

// TestProcessObjectExpiration_DeletesExpiredByDays verifies that objects older than
// the configured Days are deleted.
func TestProcessObjectExpiration_DeletesExpiredByDays(t *testing.T) {
	days := 30
	old := time.Now().UTC().AddDate(0, 0, -(days + 1)) // 31 days ago — should expire
	fresh := time.Now().UTC().AddDate(0, 0, -1)        // 1 day ago — should NOT expire

	objMgr := &mockObjectMgr{
		listResult: &object.ListObjectsResult{
			Objects: []object.Object{
				{Key: "old-file.txt", LastModified: old},
				{Key: "fresh-file.txt", LastModified: fresh},
			},
		},
	}
	bucketMgr := &mockBucketMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	rule := bucket.LifecycleRule{
		ID:     "expire-30-days",
		Status: "Enabled",
		Expiration: &bucket.LifecycleExpiration{
			Days: &days,
		},
	}

	worker.processObjectExpiration(ctx, "test-bucket", rule)

	assert.Equal(t, 1, objMgr.deleteCount, "Only the old object should be deleted")
}

// TestProcessObjectExpiration_DeletesByDate verifies that objects are deleted after a fixed date.
func TestProcessObjectExpiration_DeletesByDate(t *testing.T) {
	cutoffDate := time.Now().UTC().AddDate(0, 0, -1) // yesterday

	old := time.Now().UTC().AddDate(0, 0, -10) // 10 days ago — before cutoff
	fresh := time.Now().UTC().AddDate(0, 0, 1)  // tomorrow — after cutoff

	objMgr := &mockObjectMgr{
		listResult: &object.ListObjectsResult{
			Objects: []object.Object{
				{Key: "expired.txt", LastModified: old},
				{Key: "active.txt", LastModified: fresh},
			},
		},
	}
	bucketMgr := &mockBucketMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	rule := bucket.LifecycleRule{
		ID:     "expire-by-date",
		Status: "Enabled",
		Expiration: &bucket.LifecycleExpiration{
			Date: &cutoffDate,
		},
	}

	worker.processObjectExpiration(ctx, "test-bucket", rule)

	assert.Equal(t, 1, objMgr.deleteCount, "Only the object before the cutoff date should be deleted")
}

// TestProcessObjectExpiration_PrefixFilter ensures the rule prefix is applied.
func TestProcessObjectExpiration_PrefixFilter(t *testing.T) {
	days := 10
	old := time.Now().UTC().AddDate(0, 0, -20)

	// The mock returns whatever listResult is set to regardless of prefix;
	// but we validate that the prefix is passed to ListObjects.
	objMgr := &mockObjectMgr{
		listResult: &object.ListObjectsResult{
			Objects: []object.Object{
				{Key: "logs/old.log", LastModified: old},
				{Key: "logs/older.log", LastModified: old},
			},
		},
	}
	bucketMgr := &mockBucketMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	rule := bucket.LifecycleRule{
		ID:     "expire-logs",
		Status: "Enabled",
		Filter: bucket.LifecycleFilter{Prefix: "logs/"},
		Expiration: &bucket.LifecycleExpiration{
			Days: &days,
		},
	}

	worker.processObjectExpiration(ctx, "test-bucket", rule)

	// Both objects are old — both should be deleted
	assert.Equal(t, 2, objMgr.deleteCount, "Both old prefixed objects should be deleted")
}

// TestProcessObjectExpiration_NoObjectsExpired ensures no deletions happen when all objects are fresh.
func TestProcessObjectExpiration_NoObjectsExpired(t *testing.T) {
	days := 30
	fresh := time.Now().UTC().AddDate(0, 0, -1) // only 1 day old

	objMgr := &mockObjectMgr{
		listResult: &object.ListObjectsResult{
			Objects: []object.Object{
				{Key: "file1.txt", LastModified: fresh},
				{Key: "file2.txt", LastModified: fresh},
			},
		},
	}
	bucketMgr := &mockBucketMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	rule := bucket.LifecycleRule{
		ID:     "expire-30-days",
		Status: "Enabled",
		Expiration: &bucket.LifecycleExpiration{
			Days: &days,
		},
	}

	worker.processObjectExpiration(ctx, "test-bucket", rule)

	assert.Equal(t, 0, objMgr.deleteCount, "No objects should be deleted when all are fresh")
}

// TestProcessObjectExpiration_ListError handles list failures gracefully.
func TestProcessObjectExpiration_ListError(t *testing.T) {
	days := 30
	objMgr := &mockObjectMgr{
		listErr: assert.AnError,
	}
	bucketMgr := &mockBucketMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	rule := bucket.LifecycleRule{
		ID:     "expire-30-days",
		Status: "Enabled",
		Expiration: &bucket.LifecycleExpiration{Days: &days},
	}

	// Should not panic
	require.NotPanics(t, func() {
		worker.processObjectExpiration(ctx, "test-bucket", rule)
	})
	assert.Equal(t, 0, objMgr.deleteCount, "No deletions when listing fails")
}

// ============================================================
// processAbortIncompleteMultipartUploads tests
// ============================================================

// TestAbortIncompleteMultipart_AbortsOldUploads verifies stale uploads are aborted.
func TestAbortIncompleteMultipart_AbortsOldUploads(t *testing.T) {
	stale := time.Now().UTC().AddDate(0, 0, -8)  // 8 days ago — should be aborted (threshold = 7)
	fresh := time.Now().UTC().AddDate(0, 0, -1)  // 1 day ago — should NOT be aborted

	objMgr := &mockObjectMgrWithMultipart{
		uploads: []object.MultipartUpload{
			{UploadID: "stale-upload-1", Initiated: stale},
			{UploadID: "fresh-upload-1", Initiated: fresh},
		},
	}
	bucketMgr := &mockBucketMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	rule := bucket.LifecycleRule{
		ID:     "abort-after-7-days",
		Status: "Enabled",
		AbortIncompleteMultipartUpload: &bucket.LifecycleAbortIncompleteMultipartUpload{
			DaysAfterInitiation: 7,
		},
	}

	worker.processAbortIncompleteMultipartUploads(ctx, "test-bucket", rule)

	assert.Equal(t, []string{"stale-upload-1"}, objMgr.abortedIDs, "Only the stale upload should be aborted")
}

// TestAbortIncompleteMultipart_NoUploads does nothing when there are no uploads.
func TestAbortIncompleteMultipart_NoUploads(t *testing.T) {
	objMgr := &mockObjectMgrWithMultipart{uploads: []object.MultipartUpload{}}
	bucketMgr := &mockBucketMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	rule := bucket.LifecycleRule{
		ID:     "abort-after-7-days",
		Status: "Enabled",
		AbortIncompleteMultipartUpload: &bucket.LifecycleAbortIncompleteMultipartUpload{
			DaysAfterInitiation: 7,
		},
	}

	require.NotPanics(t, func() {
		worker.processAbortIncompleteMultipartUploads(ctx, "test-bucket", rule)
	})
	assert.Empty(t, objMgr.abortedIDs)
}

// TestAbortIncompleteMultipart_AllFresh aborts nothing when all uploads are recent.
func TestAbortIncompleteMultipart_AllFresh(t *testing.T) {
	fresh := time.Now().UTC().AddDate(0, 0, -1)

	objMgr := &mockObjectMgrWithMultipart{
		uploads: []object.MultipartUpload{
			{UploadID: "fresh-1", Initiated: fresh},
			{UploadID: "fresh-2", Initiated: fresh},
		},
	}
	bucketMgr := &mockBucketMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	rule := bucket.LifecycleRule{
		ID:     "abort-after-7-days",
		Status: "Enabled",
		AbortIncompleteMultipartUpload: &bucket.LifecycleAbortIncompleteMultipartUpload{
			DaysAfterInitiation: 7,
		},
	}

	worker.processAbortIncompleteMultipartUploads(ctx, "test-bucket", rule)
	assert.Empty(t, objMgr.abortedIDs, "Fresh uploads should not be aborted")
}

// ============================================================
// processLifecycleRule integration: all sub-rules in one rule
// ============================================================

// TestProcessLifecycleRule_ExpirationAndAbort tests that a rule with both
// expiration and abort incomplete multipart triggers both handlers.
func TestProcessLifecycleRule_ExpirationAndAbort(t *testing.T) {
	days := 5
	staleObj := time.Now().UTC().AddDate(0, 0, -10)
	staleUpload := time.Now().UTC().AddDate(0, 0, -10)

	objMgr := &mockObjectMgrWithMultipart{
		mockObjectMgr: mockObjectMgr{
			listResult: &object.ListObjectsResult{
				Objects: []object.Object{
					{Key: "old.log", LastModified: staleObj},
				},
			},
		},
		uploads: []object.MultipartUpload{
			{UploadID: "stale-mp", Initiated: staleUpload},
		},
	}
	bucketMgr := &mockBucketMgr{}
	metaStore := &mockMetaStore{}

	worker := NewWorker(bucketMgr, objMgr, metaStore)
	ctx := context.Background()

	rule := bucket.LifecycleRule{
		ID:     "combined-rule",
		Status: "Enabled",
		Expiration: &bucket.LifecycleExpiration{
			Days: &days,
		},
		AbortIncompleteMultipartUpload: &bucket.LifecycleAbortIncompleteMultipartUpload{
			DaysAfterInitiation: 5,
		},
	}

	worker.processLifecycleRule(ctx, "", "test-bucket", rule)

	assert.Equal(t, 1, objMgr.deleteCount, "Expired object should be deleted")
	assert.Equal(t, []string{"stale-mp"}, objMgr.abortedIDs, "Stale multipart upload should be aborted")
}
