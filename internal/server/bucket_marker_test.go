package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A bucket's settings reach its marker, for maxiofs recover to give them back:
// at its creation, as they change, and at the statistics pass for a marker an
// earlier release wrote.
func TestABucketsSettingsReachItsMarker(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	name := "marker-settings-bucket"
	cleanupTestData(t, "", name)

	require.NoError(t, server.bucketManager.CreateBucket(ctx, "", name, "admin"))
	dir := filepath.Join(server.config.Storage.Root, name)
	kept := func() metadata.BucketMetadata {
		t.Helper()
		marker, err := storage.ReadBucketMarker(dir)
		require.NoError(t, err)
		require.Equal(t, name, marker.Bucket)
		require.NotEmpty(t, marker.Config, "the marker keeps the bucket's settings")
		var meta metadata.BucketMetadata
		require.NoError(t, json.Unmarshal(marker.Config, &meta))
		return meta
	}
	assert.Equal(t, "admin", kept().OwnerID)

	days := 30
	require.NoError(t, server.bucketManager.SetObjectLockConfig(ctx, "", name, &bucket.ObjectLockConfig{
		ObjectLockEnabled: true,
		Rule:              &bucket.ObjectLockRule{DefaultRetention: &bucket.DefaultRetention{Mode: "COMPLIANCE", Days: &days}},
	}))
	lock := kept().ObjectLock
	require.NotNil(t, lock)
	assert.True(t, lock.Enabled)
	require.NotNil(t, lock.Rule)
	require.NotNil(t, lock.Rule.DefaultRetention)
	assert.Equal(t, "COMPLIANCE", lock.Rule.DefaultRetention.Mode)
	require.NotNil(t, lock.Rule.DefaultRetention.Days)
	assert.Equal(t, 30, *lock.Rule.DefaultRetention.Days)

	require.NoError(t, storage.WriteBucketMarker(server.config.Storage.Root, name))
	require.NoError(t, server.reconcileBucketStats(ctx))
	assert.Equal(t, "COMPLIANCE", kept().ObjectLock.Rule.DefaultRetention.Mode, "the statistics pass writes it again")
}
