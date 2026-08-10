package metadata

// Two writers, one key.
//
// UpdateBucketMetrics does a read-modify-write of the bucket document under a
// mutex; UpdateBucket wrote the whole document with no lock at all. Every
// bucket setting is saved as GetBucket → mutate → UpdateBucket, so the two
// clobbered each other in both directions: an increment landing inside that
// window was reverted by the setting being saved, and a setting was reverted by
// an increment that had read the bucket first.

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newBucketConcurrencyStore(t *testing.T) (*PebbleStore, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "maxiofs-bucketconc-*")
	require.NoError(t, err)

	store, err := NewPebbleStore(PebbleOptions{DataDir: dir, Logger: logrus.StandardLogger()})
	require.NoError(t, err)

	return store, func() {
		store.Close()
		os.RemoveAll(dir)
	}
}

// TestUpdateBucket_DoesNotLoseConcurrentMetrics runs the two writers against
// one bucket at the same time. Every increment must survive.
func TestUpdateBucket_DoesNotLoseConcurrentMetrics(t *testing.T) {
	store, cleanup := newBucketConcurrencyStore(t)
	defer cleanup()
	ctx := context.Background()

	const bucket, tenant = "counted", "t1"
	const increments = 200

	require.NoError(t, store.CreateBucket(ctx, &BucketMetadata{
		Name: bucket, TenantID: tenant, OwnerID: "u1"}))

	var wg sync.WaitGroup
	wg.Add(2)

	// Writer A: object PUTs arriving.
	go func() {
		defer wg.Done()
		for i := 0; i < increments; i++ {
			_ = store.UpdateBucketMetrics(ctx, tenant, bucket, 1, 10)
		}
	}()

	// Writer B: an administrator saving bucket settings, the read-mutate-write
	// sequence every setter performs.
	go func() {
		defer wg.Done()
		for i := 0; i < increments; i++ {
			current, err := store.GetBucket(ctx, tenant, bucket)
			if err != nil {
				continue
			}
			current.Versioning = &VersioningMetadata{Status: "Enabled"}
			_ = store.UpdateBucket(ctx, current)
		}
	}()

	wg.Wait()

	final, err := store.GetBucket(ctx, tenant, bucket)
	require.NoError(t, err)

	assert.Equal(t, int64(increments), final.ObjectCount,
		"every increment must survive a concurrent configuration write")
	assert.Equal(t, int64(increments*10), final.TotalSize)
	require.NotNil(t, final.Versioning,
		"and the configuration write must survive the increments")
	assert.Equal(t, "Enabled", final.Versioning.Status)
}

// TestUpdateBucket_KeepsTheStoredCounters pins the ownership rule directly: a
// caller writing configuration carries whatever the counters were when it read
// the bucket, which is not an opinion about their value.
func TestUpdateBucket_KeepsTheStoredCounters(t *testing.T) {
	store, cleanup := newBucketConcurrencyStore(t)
	defer cleanup()
	ctx := context.Background()

	const bucket, tenant = "owned", "t1"
	require.NoError(t, store.CreateBucket(ctx, &BucketMetadata{
		Name: bucket, TenantID: tenant, OwnerID: "u1"}))

	require.NoError(t, store.UpdateBucketMetrics(ctx, tenant, bucket, 7, 700))

	// A stale snapshot, as a caller that read the bucket before those arrived.
	stale := &BucketMetadata{
		Name: bucket, TenantID: tenant, OwnerID: "u1",
		ObjectCount: 0, TotalSize: 0,
		Versioning: &VersioningMetadata{Status: "Enabled"},
	}
	require.NoError(t, store.UpdateBucket(ctx, stale))

	final, err := store.GetBucket(ctx, tenant, bucket)
	require.NoError(t, err)
	assert.Equal(t, int64(7), final.ObjectCount, "the stored counters win")
	assert.Equal(t, int64(700), final.TotalSize)
	require.NotNil(t, final.Versioning)
	assert.Equal(t, "Enabled", final.Versioning.Status, "the configuration is applied")
}
