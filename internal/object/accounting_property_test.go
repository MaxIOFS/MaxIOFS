package object

import (
	"context"
	"math/rand"
	"net/http"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

// setupAccountingManager wires the bucket manager, without which none of the
// incremental counters run and the property would pass vacuously.
func setupAccountingManager(t *testing.T) (*objectManager, metadata.Store) {
	t.Helper()
	om, backend, store := setupManagerWithConfigKey(t)
	om.SetBucketManager(bucket.NewManager(backend, store))
	return om, store
}

// A bucket's size is defined once, by the recalculation. The incremental
// counters kept on every write must arrive at the same number by a different
// route — if they cannot, some path is failing to account for what it changed.
//
// This is a property over sequences rather than a check of one operation: a
// delete path that forgets to give the bytes back fails here whichever path it
// is, including one added later.
func assertAccountingAgrees(t *testing.T, om *objectManager, store metadata.Store, bucket string) {
	t.Helper()
	ctx := context.Background()

	incremental, err := store.GetBucket(ctx, "", bucket)
	require.NoError(t, err)
	incrementalSize, incrementalCount := incremental.TotalSize, incremental.ObjectCount

	require.NoError(t, store.RecalculateBucketStats(ctx, "", bucket))

	recalculated, err := store.GetBucket(ctx, "", bucket)
	require.NoError(t, err)

	require.Equal(t, recalculated.TotalSize, incrementalSize,
		"the running total disagrees with a full recount of the bucket")
	require.Equal(t, recalculated.ObjectCount, incrementalCount,
		"the running object count disagrees with a full recount of the bucket")
}

func TestAccounting_AgreesAfterAnySequence_Unversioned(t *testing.T) {
	ctx := context.Background()
	om, store := setupAccountingManager(t)

	const bucket = "acct-unversioned"
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: bucket, OwnerID: "u"}))

	rng := rand.New(rand.NewSource(1))
	keys := []string{"a.txt", "b/c.txt", "d.txt"}
	for i := 0; i < 40; i++ {
		key := keys[rng.Intn(len(keys))]
		switch rng.Intn(3) {
		case 0, 1:
			payload := strings.Repeat("x", rng.Intn(50)+1)
			_, err := om.PutObject(ctx, bucket, key, strings.NewReader(payload), http.Header{})
			require.NoError(t, err)
		case 2:
			_, _ = om.DeleteObject(ctx, bucket, key, false)
		}
		assertAccountingAgrees(t, om, store, bucket)
	}
}

func TestAccounting_AgreesAfterAnySequence_Versioned(t *testing.T) {
	ctx := context.Background()
	om, store := setupAccountingManager(t)

	const bucket = "acct-versioned"
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:       bucket,
		OwnerID:    "u",
		Versioning: &metadata.VersioningMetadata{Enabled: true, Status: "Enabled"},
	}))

	rng := rand.New(rand.NewSource(2))
	keys := []string{"a.txt", "b.txt"}

	for i := 0; i < 30; i++ {
		key := keys[rng.Intn(len(keys))]
		payload := strings.Repeat("y", rng.Intn(40)+1)
		_, err := om.PutObject(ctx, bucket, key, strings.NewReader(payload), http.Header{})
		require.NoError(t, err)
		assertAccountingAgrees(t, om, store, bucket)
	}

	// Deleting versions must give their bytes back, including versions that are
	// not the latest.
	for _, key := range keys {
		versions, err := store.GetObjectVersions(ctx, bucket, key)
		require.NoError(t, err)
		for _, v := range versions {
			_ = om.DeleteObjectVersion(ctx, bucket, key, v.VersionID)
			assertAccountingAgrees(t, om, store, bucket)
		}
	}
}

// The counters agree with a recount whatever the versioning goes through:
// objects written before it, kept as null versions, replaced while it is
// suspended, and deleted by their ID.
func TestAccounting_AgreesAcrossVersioningStates(t *testing.T) {
	ctx := context.Background()
	om, store := setupAccountingManager(t)

	const name = "acct-states"
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: name, OwnerID: "u"}))
	rng := rand.New(rand.NewSource(3))
	keys := []string{"a.txt", "b.txt"}
	for i := 0; i < 80; i++ {
		key := keys[rng.Intn(len(keys))]
		switch rng.Intn(6) {
		case 0:
			meta, err := store.GetBucket(ctx, "", name)
			require.NoError(t, err)
			status := []string{"Enabled", "Suspended"}[rng.Intn(2)]
			meta.Versioning = &metadata.VersioningMetadata{Status: status, Enabled: status == "Enabled"}
			require.NoError(t, store.UpdateBucket(ctx, meta))
		case 1, 2:
			_, err := om.PutObject(ctx, name, key, strings.NewReader(strings.Repeat("z", rng.Intn(40)+1)), http.Header{})
			require.NoError(t, err)
		case 3:
			_, _ = om.DeleteObject(ctx, name, key, false)
		case 4:
			_, _ = om.DeleteObject(ctx, name, key, false, metadata.NullVersionID)
		case 5:
			if versions, err := store.GetObjectVersions(ctx, name, key); err == nil && len(versions) > 0 {
				_ = om.DeleteObjectVersion(ctx, name, key, versions[rng.Intn(len(versions))].VersionID)
			}
		}
		assertAccountingAgrees(t, om, store, name)
	}
}

// A write while versioning is suspended that replaces the null version kept
// beside another version gives its bytes back.
func TestAccounting_AReplacedNullVersionGivesItsBytesBack(t *testing.T) {
	ctx := context.Background()
	om, store := setupAccountingManager(t)

	const name = "acct-null"
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: name, OwnerID: "u"}))
	status := func(s string) {
		t.Helper()
		meta, err := store.GetBucket(ctx, "", name)
		require.NoError(t, err)
		meta.Versioning = &metadata.VersioningMetadata{Status: s, Enabled: s == "Enabled"}
		require.NoError(t, store.UpdateBucket(ctx, meta))
	}
	put := func(size int) {
		t.Helper()
		_, err := om.PutObject(ctx, name, "k", strings.NewReader(strings.Repeat("n", size)), http.Header{})
		require.NoError(t, err)
		assertAccountingAgrees(t, om, store, name)
	}
	put(100)
	status("Enabled")
	put(10)
	status("Suspended")
	put(7)
}
