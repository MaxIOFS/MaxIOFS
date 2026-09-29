package metadata

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A bucket another node holds is stored with the times it carries when it is
// newer than this node's; this node's usage counters are kept. An older or
// equal version changes nothing, and a name another tenant holds is refused.
func TestApplyBucketReplica(t *testing.T) {
	s, cleanup := setupPebbleTestStore(t)
	defer cleanup()
	ctx := context.Background()
	created := time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)
	remote := func(updated time.Time, status string) *BucketMetadata {
		return &BucketMetadata{Name: "b", CreatedAt: created, UpdatedAt: updated, ObjectCount: 99, TotalSize: 999,
			Versioning: &VersioningMetadata{Status: status}}
	}

	applied, err := s.ApplyBucketReplica(ctx, remote(created.Add(time.Second), "Enabled"))
	require.NoError(t, err)
	require.True(t, applied)
	got, err := s.GetBucket(ctx, "", "b")
	require.NoError(t, err)
	assert.True(t, got.CreatedAt.Equal(created))
	assert.True(t, got.UpdatedAt.Equal(created.Add(time.Second)))
	assert.Zero(t, got.ObjectCount, "a bucket new here holds nothing here")
	assert.Zero(t, got.TotalSize)

	require.NoError(t, s.UpdateBucketMetrics(ctx, "", "b", 2, 20))
	applied, err = s.ApplyBucketReplica(ctx, remote(created.Add(2*time.Second), "Suspended"))
	require.NoError(t, err)
	require.True(t, applied)
	got, err = s.GetBucket(ctx, "", "b")
	require.NoError(t, err)
	assert.Equal(t, "Suspended", got.Versioning.Status)
	assert.EqualValues(t, 2, got.ObjectCount, "the node's own usage is kept")
	assert.EqualValues(t, 20, got.TotalSize)

	for _, stale := range []time.Time{created.Add(2 * time.Second), created.Add(time.Second)} {
		applied, err = s.ApplyBucketReplica(ctx, remote(stale, "Enabled"))
		require.NoError(t, err)
		assert.False(t, applied)
	}
	got, err = s.GetBucket(ctx, "", "b")
	require.NoError(t, err)
	assert.Equal(t, "Suspended", got.Versioning.Status)

	require.NoError(t, s.CreateBucket(ctx, &BucketMetadata{Name: "taken", TenantID: "t1"}))
	_, err = s.ApplyBucketReplica(ctx, &BucketMetadata{Name: "taken", UpdatedAt: time.Now()})
	assert.ErrorIs(t, err, ErrBucketAlreadyExists)
	_, err = s.GetBucket(ctx, "", "taken")
	assert.ErrorIs(t, err, ErrBucketNotFound)
}

// UpdatedAt dates a bucket's configuration, which nodes compare: usage, counted
// by each node, leaves it alone.
func TestBucketUsageLeavesTheConfigurationDate(t *testing.T) {
	s, cleanup := setupPebbleTestStore(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, s.CreateBucket(ctx, &BucketMetadata{Name: "usage"}))
	before, err := s.GetBucket(ctx, "", "usage")
	require.NoError(t, err)

	require.NoError(t, s.UpdateBucketMetrics(ctx, "", "usage", 1, 10))
	require.NoError(t, s.RecalculateBucketStats(ctx, "", "usage"))
	after, err := s.GetBucket(ctx, "", "usage")
	require.NoError(t, err)
	assert.True(t, after.UpdatedAt.Equal(before.UpdatedAt))
}
