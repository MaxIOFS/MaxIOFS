package object

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func part(t *testing.T, m *objectManager, uploadID string, number, size int) error {
	t.Helper()
	_, err := m.UploadPart(t.Context(), uploadID, number, strings.NewReader(strings.Repeat("x", size)))
	return err
}

func startUpload(t *testing.T, m *objectManager, bucket, key string) string {
	t.Helper()
	upload, err := m.CreateMultipartUpload(t.Context(), bucket, key, http.Header{})
	require.NoError(t, err)
	return upload.UploadID
}

// The parts of the uploads in progress count against the bucket quota: an
// upload that could never complete does not fill the disk part by part.
func TestMultipartPartsInProgressCountAgainstTheBucketQuota(t *testing.T) {
	m, s := setupAccountingManager(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "mp", Quota: &metadata.BucketQuota{MaxSizeBytes: 100}}))
	inFlight := func() int64 { m.inFlight.mu.Lock(); defer m.inFlight.mu.Unlock(); return m.inFlight.buckets["mp"] }

	first := startUpload(t, m, "mp", "first")
	require.NoError(t, part(t, m, first, 1, 40))
	require.NoError(t, part(t, m, first, 1, 40), "a part sent again replaces itself")
	assert.EqualValues(t, 40, inFlight(), "and is counted once")
	require.NoError(t, part(t, m, first, 2, 40))
	require.NoError(t, part(t, m, first, 3, 40), "the part that passes the quota was not known to pass it")
	assert.EqualValues(t, 120, inFlight())

	assert.ErrorIs(t, part(t, m, first, 4, 1), ErrBucketQuotaExceeded, "the next part of that upload")
	second := startUpload(t, m, "mp", "second")
	assert.ErrorIs(t, part(t, m, second, 1, 1), ErrBucketQuotaExceeded, "a part of another upload of the bucket")
	_, err := m.PutObject(t.Context(), "mp", "small", strings.NewReader("x"), http.Header{})
	assert.NoError(t, err, "an object is judged by what is stored, as before")

	require.NoError(t, m.AbortMultipartUpload(t.Context(), first))
	assert.EqualValues(t, 0, inFlight(), "an aborted upload holds nothing")
	require.NoError(t, part(t, m, second, 1, 30))
	require.NoError(t, part(t, m, second, 2, 30))
	parts, err := m.ListParts(t.Context(), second)
	require.NoError(t, err)
	_, err = m.CompleteMultipartUpload(t.Context(), second, parts)
	require.NoError(t, err)
	assert.EqualValues(t, 0, inFlight(), "a completed upload holds nothing: its bytes are the bucket's")

	// 61 bytes stored: 39 are left.
	third := startUpload(t, m, "mp", "third")
	require.NoError(t, part(t, m, third, 1, 39))
	assert.ErrorIs(t, part(t, m, third, 2, 1), ErrBucketQuotaExceeded, "what is stored and what is in progress count together")
}

// After a restart the count is empty: an upload is counted again, from the
// parts it stored, when its next part arrives.
func TestMultipartPartsAreCountedAgainAfterARestart(t *testing.T) {
	m, s := setupAccountingManager(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "mp", Quota: &metadata.BucketQuota{MaxSizeBytes: 100}}))
	upload := startUpload(t, m, "mp", "key")
	require.NoError(t, part(t, m, upload, 1, 60))
	require.NoError(t, part(t, m, upload, 2, 60))

	m.inFlight = inFlightUploads{}

	assert.ErrorIs(t, part(t, m, upload, 3, 1), ErrBucketQuotaExceeded)
	assert.EqualValues(t, 120, m.inFlight.buckets["mp"])
}

// An upload that overwrites an object in place is not refused for the room
// that object has, in a bucket that is full; one of a new key is.
func TestMultipartOverAnObjectInPlaceUsesItsRoom(t *testing.T) {
	m, s := setupAccountingManager(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "full", Quota: &metadata.BucketQuota{MaxSizeBytes: 100}}))
	_, err := m.PutObject(t.Context(), "full", "big", strings.NewReader(strings.Repeat("x", 100)), http.Header{})
	require.NoError(t, err)

	other := startUpload(t, m, "full", "other")
	assert.ErrorIs(t, part(t, m, other, 1, 1), ErrBucketQuotaExceeded, "a new key, in a full bucket")

	over := startUpload(t, m, "full", "big")
	require.NoError(t, part(t, m, over, 1, 50))
	require.NoError(t, part(t, m, over, 2, 50))
	assert.EqualValues(t, 0, m.inFlight.buckets["full"], "it grows nothing so far")
	assert.ErrorIs(t, part(t, m, over, 3, 1), ErrBucketQuotaExceeded, "past the object's size it grows the bucket")
}

// An upload the store swept, without the manager, is not counted for ever.
func TestMultipartUploadEndedElsewhereIsNotCounted(t *testing.T) {
	m, s := setupAccountingManager(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "mp", Quota: &metadata.BucketQuota{MaxSizeBytes: 100}}))
	swept := startUpload(t, m, "mp", "swept")
	require.NoError(t, part(t, m, swept, 1, 100))
	other := startUpload(t, m, "mp", "other")
	require.ErrorIs(t, part(t, m, other, 1, 1), ErrBucketQuotaExceeded)

	require.NoError(t, s.AbortMultipartUpload(t.Context(), swept))

	assert.NoError(t, part(t, m, other, 1, 1))
	assert.EqualValues(t, 1, m.inFlight.buckets["mp"])
}

type tenantOfHundred struct{ stored int64 }

func (*tenantOfHundred) IncrementTenantStorage(context.Context, string, int64) error { return nil }
func (*tenantOfHundred) DecrementTenantStorage(context.Context, string, int64) error { return nil }
func (a *tenantOfHundred) CheckTenantStorageQuota(_ context.Context, _ string, add int64) error {
	if a.stored+add > 100 {
		return errors.New("tenant over quota")
	}
	return nil
}

// The parts in progress in every bucket of a tenant count against its quota.
func TestMultipartPartsInProgressCountAgainstTheTenantQuota(t *testing.T) {
	m, s := setupAccountingManager(t)
	for _, name := range []string{"one", "two"} {
		require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: name, TenantID: "t1"}))
	}
	m.SetAuthManager(&tenantOfHundred{})

	one := startUpload(t, m, "t1/one", "key")
	require.NoError(t, part(t, m, one, 1, 100))
	two := startUpload(t, m, "t1/two", "key")
	err := part(t, m, two, 1, 1)
	require.Error(t, err, "a part in another bucket of the tenant")
	assert.Contains(t, err.Error(), "storage quota exceeded")

	require.NoError(t, m.AbortMultipartUpload(t.Context(), one))
	assert.NoError(t, part(t, m, two, 1, 1))
}

// An upload is started in a bucket that exists: in one that does not, every
// part was taken and only the completion failed, with an internal error.
func TestMultipartUploadNeedsItsBucket(t *testing.T) {
	m, _ := setupAccountingManager(t)
	_, err := m.CreateMultipartUpload(t.Context(), "missing", "key", http.Header{})
	assert.ErrorIs(t, err, ErrBucketNotFound)
}
