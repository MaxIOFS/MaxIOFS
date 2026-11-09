package recovery

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A recovered bucket gets back the settings its marker keeps: its owner, its
// default retention, its quota, its tags and its policy. A suspended bucket
// stays suspended though it holds versions, and a bucket whose marker keeps no
// settings comes back as before.
func TestARecoveredBucketGetsItsSettingsFromItsMarker(t *testing.T) {
	days := 30
	dataDir, bundlePath, passphrase := buildTestDeploymentWith(t, func(ctx context.Context, buckets bucket.Manager, objects object.Manager) {
		require.NoError(t, buckets.CreateBucket(ctx, "", "locked-bucket", "user-7"))
		require.NoError(t, buckets.SetObjectLockConfig(ctx, "", "locked-bucket", &bucket.ObjectLockConfig{
			ObjectLockEnabled: true,
			Rule:              &bucket.ObjectLockRule{DefaultRetention: &bucket.DefaultRetention{Mode: "COMPLIANCE", Days: &days}},
		}))
		require.NoError(t, buckets.SetQuota(ctx, "", "locked-bucket", &metadata.BucketQuota{MaxSizeBytes: 1 << 30}))
		require.NoError(t, buckets.SetBucketTags(ctx, "", "locked-bucket", map[string]string{"team": "backup"}))
		require.NoError(t, buckets.SetBucketPolicy(ctx, "", "locked-bucket", &bucket.Policy{
			Version: "2012-10-17",
			Statement: []bucket.Statement{{
				Sid: "Read", Effect: "Allow", Principal: "*", Action: "s3:GetObject", Resource: "arn:aws:s3:::locked-bucket/*",
			}},
		}))
		_, err := objects.PutObject(ctx, "locked-bucket", "a.txt", strings.NewReader("kept"), http.Header{})
		require.NoError(t, err)

		require.NoError(t, buckets.CreateBucket(ctx, "", "suspended-bucket", "user-8"))
		require.NoError(t, buckets.SetVersioning(ctx, "", "suspended-bucket", &bucket.VersioningConfig{Status: "Enabled"}))
		for i := 0; i < 2; i++ {
			_, err := objects.PutObject(ctx, "suspended-bucket", "k", strings.NewReader("v"), http.Header{})
			require.NoError(t, err)
		}
		require.NoError(t, buckets.SetVersioning(ctx, "", "suspended-bucket", &bucket.VersioningConfig{Status: "Suspended"}))
	})

	report, err := Run(Options{DataDir: dataDir, BundlePath: bundlePath, Passphrase: passphrase})
	require.NoError(t, err)
	require.Empty(t, report.Failures)
	assert.Equal(t, 2, report.BucketSettings, "the two buckets whose settings changed; the others were only created")

	store := openRecoveredStore(t, report.OutDB)
	defer store.Close()
	ctx := context.Background()

	locked, err := store.GetBucket(ctx, "", "locked-bucket")
	require.NoError(t, err)
	assert.Equal(t, "user-7", locked.OwnerID)
	require.NotNil(t, locked.ObjectLock)
	assert.True(t, locked.ObjectLock.Enabled)
	require.NotNil(t, locked.ObjectLock.Rule)
	require.NotNil(t, locked.ObjectLock.Rule.DefaultRetention)
	assert.Equal(t, "COMPLIANCE", locked.ObjectLock.Rule.DefaultRetention.Mode)
	require.NotNil(t, locked.ObjectLock.Rule.DefaultRetention.Days)
	assert.Equal(t, 30, *locked.ObjectLock.Rule.DefaultRetention.Days)
	require.NotNil(t, locked.Quota)
	assert.Equal(t, int64(1<<30), locked.Quota.MaxSizeBytes)
	assert.Equal(t, "backup", locked.Tags["team"])
	require.NotNil(t, locked.Policy)
	assert.Len(t, locked.Policy.Statement, 1)
	assert.Equal(t, int64(1), locked.ObjectCount, "usage is counted again from the objects")

	suspended, err := store.GetBucket(ctx, "", "suspended-bucket")
	require.NoError(t, err)
	assert.Equal(t, "user-8", suspended.OwnerID)
	require.NotNil(t, suspended.Versioning)
	assert.Equal(t, "Suspended", suspended.Versioning.Status)

	tenant, err := store.GetBucket(ctx, "tenant-x", "tenant-bucket")
	require.NoError(t, err)
	assert.Equal(t, "admin", tenant.OwnerID, "a marker without settings names no owner")
}
