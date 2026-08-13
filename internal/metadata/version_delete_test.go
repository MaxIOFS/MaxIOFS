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

func newVersionTestStore(t *testing.T) (*PebbleStore, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "maxiofs-versiondelete-*")
	require.NoError(t, err)

	store, err := NewPebbleStore(PebbleOptions{DataDir: dir, Logger: logrus.StandardLogger()})
	require.NoError(t, err)

	return store, func() {
		store.Close()
		os.RemoveAll(dir)
	}
}

// putVersion writes one version of an object, marking it latest or not.
func putVersion(t *testing.T, store *PebbleStore, bucket, key, versionID string, size int64, at time.Time, latest bool) {
	t.Helper()
	obj := &ObjectMetadata{
		Bucket:       bucket,
		Key:          key,
		VersionID:    versionID,
		Size:         size,
		ETag:         versionID + "-etag",
		LastModified: at,
		IsLatest:     latest,
	}
	require.NoError(t, store.PutObjectVersion(context.Background(), obj,
		&ObjectVersion{VersionID: versionID, LastModified: at, IsLatest: latest, Size: size}))
}

func TestDeleteObjectVersion_PromotesTheNextVersion(t *testing.T) {
	store, cleanup := newVersionTestStore(t)
	defer cleanup()
	ctx := context.Background()

	const bucket, key = "vb", "report.txt"
	base := time.Now().Add(-time.Hour)

	putVersion(t, store, bucket, key, "v1", 100, base, false)
	putVersion(t, store, bucket, key, "v2", 200, base.Add(time.Minute), true)

	// Sanity: the main entry describes v2.
	main, err := store.GetObject(ctx, bucket, key)
	require.NoError(t, err)
	require.Equal(t, "v2", main.VersionID)

	require.NoError(t, store.DeleteObjectVersion(ctx, bucket, key, "v2"))

	main, err = store.GetObject(ctx, bucket, key)
	require.NoError(t, err, "the object still has a version, so it still exists")
	assert.Equal(t, "v1", main.VersionID,
		"the main entry must describe a version that still exists, not the deleted one")
	assert.Equal(t, int64(100), main.Size)
	assert.True(t, main.IsLatest, "the promoted version is the latest one")

	// The promoted version's own record agrees, or the two copies disagree the
	// moment anything lists them.
	versions, err := store.GetObjectVersions(ctx, bucket, key)
	require.NoError(t, err)
	require.Len(t, versions, 1)
	assert.True(t, versions[0].IsLatest)
}

func TestDeleteObjectVersion_RemovesTheEntryWhenNothingRemains(t *testing.T) {
	store, cleanup := newVersionTestStore(t)
	defer cleanup()
	ctx := context.Background()

	const bucket, key = "vb2", "only.txt"
	putVersion(t, store, bucket, key, "v1", 42, time.Now(), true)

	require.NoError(t, store.DeleteObjectVersion(ctx, bucket, key, "v1"))

	_, err := store.GetObject(ctx, bucket, key)
	assert.ErrorIs(t, err, ErrObjectNotFound,
		"with no versions left the object must be gone, not left dangling")
}

// TestDeleteObjectVersion_LeavesAnOlderVersionAlone: deleting a version that is
// NOT the latest must not disturb the main entry, which already describes a
// different version and is correct.
func TestDeleteObjectVersion_LeavesAnOlderVersionAlone(t *testing.T) {
	store, cleanup := newVersionTestStore(t)
	defer cleanup()
	ctx := context.Background()

	const bucket, key = "vb3", "keep.txt"
	base := time.Now().Add(-time.Hour)

	putVersion(t, store, bucket, key, "v1", 100, base, false)
	putVersion(t, store, bucket, key, "v2", 200, base.Add(time.Minute), true)

	require.NoError(t, store.DeleteObjectVersion(ctx, bucket, key, "v1"))

	main, err := store.GetObject(ctx, bucket, key)
	require.NoError(t, err)
	assert.Equal(t, "v2", main.VersionID, "the latest version is untouched")
	assert.Equal(t, int64(200), main.Size)
}

func TestDeleteObjectVersion_UnknownVersion(t *testing.T) {
	store, cleanup := newVersionTestStore(t)
	defer cleanup()

	const bucket, key = "vb4", "x.txt"
	putVersion(t, store, bucket, key, "v1", 1, time.Now(), true)

	err := store.DeleteObjectVersion(context.Background(), bucket, key, "nope")
	assert.ErrorIs(t, err, ErrVersionNotFound)
}
