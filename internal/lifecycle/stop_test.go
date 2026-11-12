package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A pass stops with the server: it deletes nothing once it is cancelled.
func TestAPassStopsWhenItIsCancelled(t *testing.T) {
	old := time.Now().AddDate(0, 0, -60)
	var versions []*metadata.ObjectVersion
	for _, key := range []string{"a", "b", "c"} {
		versions = append(versions,
			&metadata.ObjectVersion{Key: key, VersionID: key + "2", LastModified: old.AddDate(0, 0, 10), IsLatest: true},
			&metadata.ObjectVersion{Key: key, VersionID: key + "1", LastModified: old})
	}
	noncurrent := bucket.LifecycleRule{ID: "noncurrent", Status: "Enabled",
		NoncurrentVersionExpiration: &bucket.NoncurrentVersionExpiration{NoncurrentDays: 30}}
	yes := true
	markers := bucket.LifecycleRule{ID: "markers", Status: "Enabled",
		Expiration: &bucket.LifecycleExpiration{ExpiredObjectDeleteMarker: &yes}}
	onlyMarkers := []*metadata.ObjectVersion{
		{Key: "a", VersionID: "dm1", IsLatest: true},
		{Key: "b", VersionID: "dm2", IsLatest: true},
	}

	stale := bucket.LifecycleRule{ID: "stale", Status: "Enabled",
		AbortIncompleteMultipartUpload: &bucket.LifecycleAbortIncompleteMultipartUpload{DaysAfterInitiation: 7}}
	aborted := func(ctx context.Context) int {
		objects := &mockObjectMgrWithMultipart{uploads: []object.MultipartUpload{
			{UploadID: "u1", Key: "a", Initiated: old}, {UploadID: "u2", Key: "b", Initiated: old}}}
		NewWorker(&mockBucketMgr{}, objects, &mockMetaStore{}).processAbortIncompleteMultipartUploads(ctx, "b", stale)
		return len(objects.abortedIDs)
	}

	run := func(ctx context.Context) (int, int, error) {
		objects := &mockObjectMgr{}
		worker := NewWorker(&mockBucketMgr{}, objects, &mockMetaStore{versions: versions})
		worker.processNoncurrentVersionExpiration(ctx, "b", noncurrent)
		expired := objects.deleteCount

		objects = &mockObjectMgr{}
		worker = NewWorker(&mockBucketMgr{}, objects, &mockMetaStore{versions: onlyMarkers})
		worker.processExpiredDeleteMarkers(ctx, "b", markers)

		buckets := &mockBucketMgr{buckets: []bucket.Bucket{{Name: "b"}}}
		err := NewWorker(buckets, &mockObjectMgr{}, &mockMetaStore{}).processLifecyclePolicies(ctx)
		return expired, objects.deleteCount, err
	}

	expired, removed, err := run(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, expired, "the rule expires the noncurrent versions")
	require.Equal(t, 2, removed, "the rule removes the delete markers")
	require.Equal(t, 2, aborted(context.Background()), "the rule aborts the stale uploads")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.Zero(t, aborted(ctx), "uploads aborted after the stop")
	expired, removed, err = run(ctx)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, expired, "versions expired after the stop")
	assert.Zero(t, removed, "delete markers removed after the stop")
}
