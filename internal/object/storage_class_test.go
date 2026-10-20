package object

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func classChangeOf(obj *Object, class string) ClassChange {
	return ClassChange{VersionID: obj.VersionID, ETag: obj.ETag, LastModified: obj.LastModified.Unix(),
		WrittenAt: obj.WrittenAt, StorageClass: class}
}

// A write is moved to another class as the write named, not another one of
// its key, nor a delete marker; one already of the class is not moved.
func TestAWriteMovesToAnotherStorageClass(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	written, err := om.PutObject(ctx, "b", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)

	other := classChangeOf(written, "GLACIER")
	other.ETag = "another"
	moved, err := om.ChangeStorageClass(ctx, "b", "k", other)
	require.NoError(t, err)
	assert.False(t, moved, "another write of the key")

	moved, err = om.ChangeStorageClass(ctx, "b", "k", classChangeOf(written, "GLACIER"))
	require.NoError(t, err)
	assert.True(t, moved)
	head, err := om.GetObjectMetadata(ctx, "b", "k")
	require.NoError(t, err)
	assert.Equal(t, "GLACIER", head.StorageClass)
	listed, err := om.ListObjects(ctx, "b", "", "", "", 10)
	require.NoError(t, err)
	require.Len(t, listed.Objects, 1)
	assert.Equal(t, "GLACIER", listed.Objects[0].StorageClass)
	assert.Equal(t, "data", readAll(t, om, "b", "k"), "the data stays as it was")

	moved, err = om.ChangeStorageClass(ctx, "b", "k", classChangeOf(written, "GLACIER"))
	require.NoError(t, err)
	assert.False(t, moved, "already of the class")

	moved, err = om.ChangeStorageClass(ctx, "b", "absent", classChangeOf(written, "GLACIER"))
	require.NoError(t, err)
	assert.False(t, moved, "no such key")
}

// A version is moved alone; the versions listed show each its class.
func TestAVersionMovesToAnotherStorageClass(t *testing.T) {
	om, _, cleanup := setupVersionedBucket(t, "v")
	defer cleanup()
	ctx := context.Background()
	first, err := om.PutObject(ctx, "v", "k", strings.NewReader("one"), http.Header{})
	require.NoError(t, err)
	_, err = om.PutObject(ctx, "v", "k", strings.NewReader("two"), http.Header{})
	require.NoError(t, err)

	moved, err := om.ChangeStorageClass(ctx, "v", "k", classChangeOf(first, "DEEP_ARCHIVE"))
	require.NoError(t, err)
	require.True(t, moved)
	versions, err := om.GetObjectVersions(ctx, "v", "k")
	require.NoError(t, err)
	classes := map[string]string{}
	for _, v := range versions {
		classes[v.VersionID] = v.StorageClass
	}
	assert.Equal(t, "DEEP_ARCHIVE", classes[first.VersionID])
	current, err := om.GetObjectMetadata(ctx, "v", "k")
	require.NoError(t, err)
	assert.Equal(t, "STANDARD", current.StorageClass)

	marker, err := om.DeleteObject(ctx, "v", "k", false)
	require.NoError(t, err)
	entry, err := om.ObjectEntry(ctx, "v", "k", marker)
	require.NoError(t, err)
	moved, err = om.ChangeStorageClass(ctx, "v", "k", ClassChange{VersionID: marker, ETag: entry.ETag,
		LastModified: entry.LastModified.Unix(), WrittenAt: entry.WrittenAt, StorageClass: "GLACIER"})
	require.NoError(t, err)
	assert.False(t, moved, "a delete marker")
}

// A change of a write's placement may carry its class; an entry with an
// older placement keeps the class of the newer one.
func TestThePlacementOfAWriteCarriesItsClass(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	written, err := om.PutObject(WithLocations(ctx, []string{"self", "other"}), "b", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	change := LocationsChange{ETag: written.ETag, LastModified: written.LastModified.Unix(), WrittenAt: written.WrittenAt,
		Locations: []string{"self", "other"}, Gen: 2, Class: "GLACIER"}
	require.NoError(t, om.SetLocations(ctx, "b", "k", change))
	head, err := om.GetObjectMetadata(ctx, "b", "k")
	require.NoError(t, err)
	assert.Equal(t, "GLACIER", head.StorageClass)
	assert.True(t, holdsFile(t, om, "b", "k"))

	change.Gen, change.Class = 3, ""
	require.NoError(t, om.SetLocations(ctx, "b", "k", change))
	head, err = om.GetObjectMetadata(ctx, "b", "k")
	require.NoError(t, err)
	assert.Equal(t, "GLACIER", head.StorageClass, "a change without a class keeps it")

	entry, err := om.ObjectEntry(ctx, "b", "k", "")
	require.NoError(t, err)
	stale := *entry
	stale.StorageClass, stale.LocationsGen = "STANDARD", 1
	require.NoError(t, om.PutReplicaMetadata(ctx, &stale))
	head, err = om.GetObjectMetadata(ctx, "b", "k")
	require.NoError(t, err)
	assert.Equal(t, "GLACIER", head.StorageClass, "the entry of an older placement")
}

// A multipart upload completes in the class it was started with.
func TestTheStorageClassOfAMultipartUpload(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	upload, err := om.CreateMultipartUpload(ctx, "b", "k", http.Header{"X-Amz-Storage-Class": {"GLACIER"}})
	require.NoError(t, err)
	class, err := om.UploadStorageClass(ctx, upload.UploadID)
	require.NoError(t, err)
	assert.Equal(t, "GLACIER", class)
	_, err = om.UploadStorageClass(ctx, "absent")
	assert.Error(t, err)
}

// The listing of every version of a bucket gives each its class, that of an
// object of an unversioned bucket too.
func TestEveryVersionListedHasItsClass(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	_, err := om.PutObject(ctx, "b", "k", strings.NewReader("data"), http.Header{"X-Amz-Storage-Class": {"STANDARD_IA"}})
	require.NoError(t, err)
	versions, err := store.ListAllObjectVersions(ctx, "b", "", 0)
	require.NoError(t, err)
	require.Len(t, versions, 1)
	assert.Equal(t, "STANDARD_IA", versions[0].StorageClass)
}

// A copy of a write carrying an older placement keeps the class the newer
// one here gave the write; one carrying a newer placement gives its own.
func TestACopyKeepsTheClassOfTheNewestPlacement(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	at := time.Now().Truncate(time.Second)
	copyWith := func(gen int64, class string) {
		t.Helper()
		copyCtx := WithLocationsGen(WithLocations(WithReplicatedWrittenAt(WithReplicatedLastModified(WithReplicaCopy(ctx), at),
			at.UnixNano()), []string{"self", "other"}), gen)
		copyCtx = WithReplicatedAttributes(copyCtx, ReplicatedAttributes{ETag: "0123456789abcdef0123456789abcdef-2"})
		_, err := om.PutObject(copyCtx, "b", "k", strings.NewReader("data"), http.Header{"X-Amz-Storage-Class": {class}})
		require.NoError(t, err)
	}
	copyWith(1, "STANDARD")
	entry, err := om.ObjectEntry(ctx, "b", "k", "")
	require.NoError(t, err)
	require.NoError(t, om.SetLocations(ctx, "b", "k", LocationsChange{ETag: entry.ETag, LastModified: entry.LastModified.Unix(),
		WrittenAt: entry.WrittenAt, Locations: []string{"self", "other"}, Gen: 3, Class: "GLACIER"}))

	copyWith(2, "STANDARD")
	head, err := om.GetObjectMetadata(ctx, "b", "k")
	require.NoError(t, err)
	assert.Equal(t, "GLACIER", head.StorageClass)
	assert.Equal(t, "data", readAll(t, om, "b", "k"))

	copyWith(4, "DEEP_ARCHIVE")
	head, err = om.GetObjectMetadata(ctx, "b", "k")
	require.NoError(t, err)
	assert.Equal(t, "DEEP_ARCHIVE", head.StorageClass)
}
