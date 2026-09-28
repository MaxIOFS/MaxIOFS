package object

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

// suspendedBucket creates a bucket with versioning enabled, runs before, then
// suspends versioning.
func suspendedBucket(t *testing.T, m *objectManager, s metadata.Store, name string, before func()) {
	t.Helper()
	ctx := context.Background()
	b := &metadata.BucketMetadata{Name: name, OwnerID: "u", Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
		ObjectLock: &metadata.ObjectLockMetadata{Enabled: true}}
	require.NoError(t, s.CreateBucket(ctx, b))
	before()
	b.Versioning.Status = "Suspended"
	require.NoError(t, s.UpdateBucket(ctx, b))
}

func markerCount(t *testing.T, m *objectManager, bucket, key string) (markers int) {
	t.Helper()
	versions, err := m.GetObjectVersions(context.Background(), bucket, key)
	require.NoError(t, err)
	for _, v := range versions {
		if v.IsDeleteMarker {
			markers++
		}
	}
	return markers
}

// A delete without a version ID in a bucket whose versioning is suspended
// hides the key behind a delete marker and keeps the versions from before,
// with their bytes counted. The current object without a version ID has no
// version to be kept as: it is removed, unless a legal hold protects it.
func TestSuspendedDeleteKeepsTheVersionsFromBefore(t *testing.T) {
	ctx := context.Background()

	t.Run("current version from before", func(t *testing.T) {
		m, s := setupAccountingManager(t)
		var kept *Object
		suspendedBucket(t, m, s, "sus-version", func() {
			var err error
			kept, err = m.PutObject(ctx, "sus-version", "k", strings.NewReader("kept version"), http.Header{})
			require.NoError(t, err)
		})
		marker, err := m.DeleteObject(ctx, "sus-version", "k", false)
		require.NoError(t, err)
		require.NotEmpty(t, marker)
		_, _, err = m.GetObject(ctx, "sus-version", "k")
		require.ErrorIs(t, err, ErrObjectNotFound, "the key is hidden")
		require.Equal(t, "kept version", readObject(t, m, "sus-version", "k", kept.VersionID))
		require.Equal(t, 1, markerCount(t, m, "sus-version", "k"))
		assertAccountingAgrees(t, m, s, "sus-version")
	})

	t.Run("current object without a version ID", func(t *testing.T) {
		m, s := setupAccountingManager(t)
		var older *Object
		suspendedBucket(t, m, s, "sus-null", func() {
			var err error
			older, err = m.PutObject(ctx, "sus-null", "k", strings.NewReader("older version"), http.Header{})
			require.NoError(t, err)
		})
		_, err := m.PutObject(ctx, "sus-null", "k", strings.NewReader("written while suspended"), http.Header{})
		require.NoError(t, err)

		_, err = m.DeleteObject(ctx, "sus-null", "k", false)
		require.NoError(t, err)
		_, _, err = m.GetObject(ctx, "sus-null", "k")
		require.ErrorIs(t, err, ErrObjectNotFound)
		require.Equal(t, "older version", readObject(t, m, "sus-null", "k", older.VersionID))
		exists, err := m.storage.Exists(ctx, m.objectRef("sus-null", "k"))
		require.NoError(t, err)
		require.False(t, exists, "the object without a version ID is removed")
		assertAccountingAgrees(t, m, s, "sus-null")
	})

	t.Run("under a legal hold", func(t *testing.T) {
		m, s := setupAccountingManager(t)
		suspendedBucket(t, m, s, "sus-held", func() {})
		_, err := m.PutObject(ctx, "sus-held", "k", strings.NewReader("held"), http.Header{"X-Amz-Object-Lock-Legal-Hold": {"ON"}})
		require.NoError(t, err)
		_, err = m.DeleteObject(ctx, "sus-held", "k", false)
		require.ErrorIs(t, err, ErrObjectUnderLegalHold)
		require.Equal(t, "held", readObject(t, m, "sus-held", "k"))
		require.Zero(t, markerCount(t, m, "sus-held", "k"), "a refused delete leaves no marker")
	})

	t.Run("a newer copied marker", func(t *testing.T) {
		m, s := setupAccountingManager(t)
		suspendedBucket(t, m, s, "sus-copy", func() {})
		current, err := m.PutObject(ctx, "sus-copy", "k", strings.NewReader("replaced by the delete"), http.Header{})
		require.NoError(t, err)
		later := current.LastModified.Add(2 * time.Second)
		_, err = m.DeleteObject(replicaCtx("1900000000000000000.aaaaaaaa", later), "sus-copy", "k", false)
		require.NoError(t, err)
		_, _, err = m.GetObject(ctx, "sus-copy", "k")
		require.ErrorIs(t, err, ErrObjectNotFound, "the node that sent the marker removed the object too")
		assertAccountingAgrees(t, m, s, "sus-copy")
	})
}
