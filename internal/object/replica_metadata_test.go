package object

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readAll(t *testing.T, om *objectManager, bucket, key string, versionID ...string) string {
	t.Helper()
	_, reader, err := om.GetObject(context.Background(), bucket, key, versionID...)
	require.NoError(t, err)
	defer reader.Close()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	return string(data)
}

// The entry of an object whose data other nodes hold answers HEAD and listings
// here; a GET names the nodes to read it from, and a file this node kept for
// the key is removed. An entry written before the current one is not taken.
func TestAnEntryWhoseDataIsElsewhere(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	_, err := om.PutObject(ctx, "b", "k", strings.NewReader("old"), http.Header{})
	require.NoError(t, err)

	later := time.Now().Add(time.Hour).Truncate(time.Second)
	entry := &metadata.ObjectMetadata{Bucket: "b", Key: "k", Size: 5, ETag: "e5", LastModified: later, Locations: []string{"other"}}
	require.NoError(t, om.PutReplicaMetadata(ctx, entry))

	_, _, err = om.GetObject(ctx, "b", "k")
	var elsewhere *DataElsewhereError
	require.ErrorAs(t, err, &elsewhere)
	assert.Equal(t, []string{"other"}, elsewhere.Locations)
	assert.Equal(t, "e5", elsewhere.Object.ETag)
	exists, err := om.storage.Exists(ctx, om.objectRef("b", "k"))
	require.NoError(t, err)
	assert.False(t, exists, "the file of the earlier write is removed")
	head, err := om.GetObjectMetadata(ctx, "b", "k")
	require.NoError(t, err)
	assert.EqualValues(t, 5, head.Size)
	listed, err := om.ListObjects(ctx, "b", "", "", "", 10)
	require.NoError(t, err)
	require.Len(t, listed.Objects, 1)
	assert.Equal(t, "e5", listed.Objects[0].ETag)

	earlier := &metadata.ObjectMetadata{Bucket: "b", Key: "k", Size: 3, ETag: "e3", LastModified: later.Add(-time.Minute), Locations: []string{"other"}}
	require.NoError(t, om.PutReplicaMetadata(ctx, earlier))
	head, err = om.GetObjectMetadata(ctx, "b", "k")
	require.NoError(t, err)
	assert.Equal(t, "e5", head.ETag, "an entry written before the current one is not taken")

	assert.Error(t, om.PutReplicaMetadata(ctx, &metadata.ObjectMetadata{Bucket: "b", Key: "marker", LastModified: later, Locations: []string{"other"}}),
		"a delete marker is replicated as a delete")
}

// A version whose data other nodes hold lands as the latest only when it is
// the newest, and is read by its ID from them.
func TestAVersionEntryWhoseDataIsElsewhere(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "v",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"}}))
	written, err := om.PutObject(ctx, "v", "k", strings.NewReader("here"), http.Header{})
	require.NoError(t, err)

	older := &metadata.ObjectMetadata{Bucket: "v", Key: "k", VersionID: "1000000000000000000.older", Size: 2, ETag: "e2",
		LastModified: written.LastModified.Add(-time.Hour), Locations: []string{"other"}}
	require.NoError(t, om.PutReplicaMetadata(ctx, older))
	assert.Equal(t, "here", readAll(t, om, "v", "k"), "an older version does not become the latest")
	_, _, err = om.GetObject(ctx, "v", "k", older.VersionID)
	var elsewhere *DataElsewhereError
	require.ErrorAs(t, err, &elsewhere)
	versions, err := om.GetObjectVersions(ctx, "v", "k")
	require.NoError(t, err)
	assert.Len(t, versions, 2)

	newer := &metadata.ObjectMetadata{Bucket: "v", Key: "k", VersionID: "9000000000000000000.newer", Size: 3, ETag: "e3",
		LastModified: written.LastModified.Add(time.Hour), Locations: []string{"other"}}
	require.NoError(t, om.PutReplicaMetadata(ctx, newer))
	head, err := om.GetObjectMetadata(ctx, "v", "k")
	require.NoError(t, err)
	assert.Equal(t, newer.VersionID, head.VersionID)
	head, err = om.GetObjectMetadata(ctx, "v", "k", older.VersionID)
	require.NoError(t, err)
	assert.Equal(t, "e2", head.ETag)
}

// A node serves its own file of an entry that names it, and outside a cluster
// every entry is served from its file.
func TestAnEntryHeldHereIsServedFromItsFile(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := WithLocations(context.Background(), []string{"self", "other"})
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	_, err := om.PutObject(ctx, "b", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)

	om.SetLocalNode(func() string { return "self" })
	assert.Equal(t, "data", readAll(t, om, "b", "k"))
	om.SetLocalNode(func() string { return "" })
	assert.Equal(t, "data", readAll(t, om, "b", "k"), "outside a cluster")
	om.SetLocalNode(func() string { return "third" })
	_, _, err = om.GetObject(ctx, "b", "k")
	assert.True(t, errors.As(err, new(*DataElsewhereError)), "a node the entry does not name reads it elsewhere")
}

// A copy of a write made before the current one, without a version, does not
// replace it; a copy of a later write does.
func TestAnEarlierCopyDoesNotReplaceALaterWrite(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	current, err := om.PutObject(ctx, "b", "k", strings.NewReader("current"), http.Header{})
	require.NoError(t, err)

	copyAt := func(at time.Time) context.Context {
		return WithReplicatedLastModified(WithReplicaCopy(ctx), at)
	}
	_, err = om.PutObject(copyAt(current.LastModified.Add(-time.Hour)), "b", "k", strings.NewReader("earlier"), http.Header{})
	require.NoError(t, err)
	assert.Equal(t, "current", readAll(t, om, "b", "k"))
	_, err = om.PutObject(copyAt(current.LastModified.Add(time.Hour)), "b", "k", strings.NewReader("later"), http.Header{})
	require.NoError(t, err)
	assert.Equal(t, "later", readAll(t, om, "b", "k"))
}

// A node that holds an entry's data and lost its file names the other nodes
// that hold it.
func TestALostCopyNamesTheOtherHolders(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := WithLocations(context.Background(), []string{"self", "other"})
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	_, err := om.PutObject(ctx, "b", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, om.storage.Delete(ctx, om.objectRef("b", "k")))

	_, _, err = om.GetObject(ctx, "b", "k")
	var elsewhere *DataElsewhereError
	require.ErrorAs(t, err, &elsewhere)
	assert.Equal(t, []string{"self", "other"}, elsewhere.Locations)
}

// The user metadata of an object whose data other nodes hold changes in its
// entry; an entry naming this node keeps the file it describes.
func TestTheEntryOfAnObjectHeldElsewhereChanges(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	at := time.Now().Add(time.Hour).Truncate(time.Second)
	require.NoError(t, om.PutReplicaMetadata(ctx, &metadata.ObjectMetadata{Bucket: "b", Key: "elsewhere", Size: 4, ETag: "e4",
		LastModified: at, Locations: []string{"other"}}))
	require.NoError(t, om.UpdateObjectMetadata(ctx, "b", "elsewhere", map[string]string{"team": "blue"}))
	head, err := om.GetObjectMetadata(ctx, "b", "elsewhere")
	require.NoError(t, err)
	assert.Equal(t, "blue", head.Metadata["team"])

	_, err = om.PutObject(ctx, "b", "here", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	require.NoError(t, om.PutReplicaMetadata(ctx, &metadata.ObjectMetadata{Bucket: "b", Key: "here", Size: 4, ETag: "e4",
		LastModified: at, Locations: []string{"self", "other"}}))
	exists, err := om.storage.Exists(ctx, om.objectRef("b", "here"))
	require.NoError(t, err)
	assert.True(t, exists, "a node the entry names keeps the file")

	entry, err := om.ObjectEntry(ctx, "b", "elsewhere", "")
	require.NoError(t, err)
	assert.Equal(t, "e4", entry.ETag)
	assert.Equal(t, "b", entry.Bucket)
}

// The entry of one version is that version's, not the latest's.
func TestTheEntryOfAVersion(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "v",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"}}))
	first, err := om.PutObject(ctx, "v", "k", strings.NewReader("one"), http.Header{})
	require.NoError(t, err)
	_, err = om.PutObject(ctx, "v", "k", strings.NewReader("two!"), http.Header{})
	require.NoError(t, err)
	entry, err := om.ObjectEntry(ctx, "v", "k", first.VersionID)
	require.NoError(t, err)
	assert.Equal(t, first.VersionID, entry.VersionID)
	assert.EqualValues(t, 3, entry.Size)
}

// A ciphertext copy of a write made before the current one, without a
// version, does not replace it; one of a later write does.
func TestAnEarlierRawCopyDoesNotReplaceALaterWrite(t *testing.T) {
	ctx := context.Background()
	kekA := newKEKStore(t)
	clusterKeys, err := kekA.EnsureClusterKey()
	require.NoError(t, err)
	kekB := newKEKStore(t)
	require.NoError(t, kekB.AdoptClusterKeys(clusterKeys))
	nodeA, metaA := newNodeManager(t, kekA)
	nodeB, metaB := newNodeManager(t, kekB)
	for _, m := range []metadata.Store{metaA, metaB} {
		require.NoError(t, m.CreateBucket(ctx, &metadata.BucketMetadata{Name: "raw-order", OwnerID: "u"}))
	}
	copyFromA := func() {
		t.Helper()
		reader, sidecar, meta, err := nodeA.GetObjectRaw(ctx, "raw-order", "k", "")
		require.NoError(t, err)
		data, err := io.ReadAll(reader)
		reader.Close()
		require.NoError(t, err)
		require.NoError(t, nodeB.PutObjectRaw(ctx, "raw-order", "k", strings.NewReader(string(data)), sidecar, meta))
	}
	read := func() string {
		t.Helper()
		_, reader, err := nodeB.GetObject(ctx, "raw-order", "k")
		require.NoError(t, err)
		defer reader.Close()
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		return string(data)
	}

	_, err = nodeA.PutObject(ctx, "raw-order", "k", strings.NewReader("earlier"), http.Header{})
	require.NoError(t, err)
	time.Sleep(1100 * time.Millisecond)
	_, err = nodeB.PutObject(ctx, "raw-order", "k", strings.NewReader("current"), http.Header{})
	require.NoError(t, err)
	copyFromA()
	assert.Equal(t, "current", read())

	time.Sleep(1100 * time.Millisecond)
	_, err = nodeA.PutObject(ctx, "raw-order", "k", strings.NewReader("later"), http.Header{})
	require.NoError(t, err)
	copyFromA()
	assert.Equal(t, "later", read())
}

// The integrity of an object whose data other nodes hold is not checked here.
func TestTheIntegrityOfDataHeldElsewhereIsNotCheckedHere(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	require.NoError(t, om.PutReplicaMetadata(ctx, &metadata.ObjectMetadata{Bucket: "b", Key: "k", Size: 4,
		ETag: "0123456789abcdef0123456789abcdef", LastModified: time.Now(), Locations: []string{"other"}}))
	result, err := om.VerifyObjectIntegrity(ctx, "b", "k")
	require.NoError(t, err)
	assert.Equal(t, IntegritySkipped, result.Status)
}

// The nodes holding a write's data change while its entry is that write; a
// node the entry no longer names removes its file. A change naming another
// write of the key is not applied.
func TestTheLocationsOfAWrite(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := WithLocations(context.Background(), []string{"self", "other"})
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	written, err := om.PutObject(ctx, "b", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	change := LocationsChange{ETag: written.ETag, LastModified: written.LastModified.Unix(), WrittenAt: written.WrittenAt}

	another := change
	another.ETag = "another write"
	another.Locations = []string{"elsewhere"}
	require.NoError(t, om.SetLocations(ctx, "b", "k", another))
	entry, err := om.ObjectEntry(ctx, "b", "k", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"self", "other"}, entry.Locations)

	change.Locations, change.Gen = []string{"other", "third"}, 1
	require.NoError(t, om.SetLocations(ctx, "b", "k", change))
	entry, err = om.ObjectEntry(ctx, "b", "k", "")
	require.NoError(t, err)
	assert.Equal(t, []string{"other", "third"}, entry.Locations)
	exists, err := om.storage.Exists(ctx, om.objectRef("b", "k"))
	require.NoError(t, err)
	assert.False(t, exists, "a node the entry no longer names removes its file")
	assert.ErrorIs(t, om.SetLocations(ctx, "b", "missing", change), ErrObjectNotFound)
}

// A version's locations change on its entry and, while it is the latest, on
// the current one.
func TestTheLocationsOfAVersion(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := WithLocations(context.Background(), []string{"self", "other"})
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "v",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"}}))
	written, err := om.PutObject(ctx, "v", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)

	require.NoError(t, om.SetLocations(ctx, "v", "k", LocationsChange{VersionID: written.VersionID, ETag: written.ETag,
		LastModified: written.LastModified.Unix(), WrittenAt: written.WrittenAt, Locations: []string{"self", "third"}, Gen: 1}))
	for _, versionID := range []string{written.VersionID, ""} {
		entry, err := om.ObjectEntry(ctx, "v", "k", versionID)
		require.NoError(t, err)
		assert.Equal(t, []string{"self", "third"}, entry.Locations, versionID)
	}
	assert.Equal(t, "data", readAll(t, om, "v", "k"))

	require.NoError(t, om.SetLocations(ctx, "v", "k", LocationsChange{VersionID: written.VersionID, ETag: written.ETag,
		LastModified: written.LastModified.Unix(), WrittenAt: written.WrittenAt, Locations: []string{"other", "third"}, Gen: 2}))
	exists, err := om.storage.Exists(ctx, om.versionRef("v", "k", written.VersionID))
	require.NoError(t, err)
	assert.False(t, exists, "a node the version no longer names removes its file")
}

// A copy of a write is stored only where the locations it keeps name this
// node: one naming other nodes only is not, and an entry of the same write
// with older locations does not remove one that is. A version too.
func TestACopyIsStoredWhereItsLocationsNameThisNode(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "v",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"}}))
	at := time.Now().Truncate(time.Second)
	for _, w := range []struct{ bucket, versionID string }{{"b", ""}, {"v", "1000000000000000000.named"}} {
		copyAs := func(locations ...string) {
			t.Helper()
			copyCtx := WithLocationsGen(WithLocations(WithReplicatedWrittenAt(WithReplicatedLastModified(WithReplicaCopy(ctx), at),
				at.UnixNano()), locations), 1)
			if w.versionID != "" {
				copyCtx = WithReplicatedVersionID(copyCtx, w.versionID)
			}
			_, err := om.PutObject(copyCtx, w.bucket, "k", strings.NewReader("data"), http.Header{})
			require.NoError(t, err)
		}
		ref := om.objectRef(w.bucket, "k")
		if w.versionID != "" {
			ref = om.versionRef(w.bucket, "k", w.versionID)
		}

		copyAs("other")
		_, err := om.ObjectEntry(ctx, w.bucket, "k", w.versionID)
		assert.Error(t, err, "a copy naming other nodes only is not stored: %s", w.bucket)

		copyAs("other", "self")
		entry, err := om.ObjectEntry(ctx, w.bucket, "k", w.versionID)
		require.NoError(t, err)
		stale := *entry
		stale.Locations, stale.LocationsGen = []string{"other"}, 0
		require.NoError(t, om.PutReplicaMetadata(ctx, &stale))
		entry, err = om.ObjectEntry(ctx, w.bucket, "k", w.versionID)
		require.NoError(t, err)
		assert.Equal(t, []string{"other", "self"}, entry.Locations, w.bucket)
		exists, err := om.storage.Exists(ctx, ref)
		require.NoError(t, err)
		assert.True(t, exists, "an entry of the same write with older locations does not remove the copy: %s", w.bucket)
	}
}

// locationsOf is this node's locations of the current write of key and the
// number of the change that set them.
func locationsOf(t *testing.T, om *objectManager, bucket, key string) ([]string, int64) {
	t.Helper()
	entry, err := om.ObjectEntry(context.Background(), bucket, key, "")
	require.NoError(t, err)
	return entry.Locations, entry.LocationsGen
}

func holdsFile(t *testing.T, om *objectManager, bucket, key string) bool {
	t.Helper()
	exists, err := om.storage.Exists(context.Background(), om.objectRef(bucket, key))
	require.NoError(t, err)
	return exists
}

// Every node keeps the locations of a write set by the change with the highest
// number, whichever order the changes and the entries arrive in; of two with
// one number, the larger list. The file is removed where the locations kept do
// not name this node.
func TestTheNewestLocationsOfAWriteAreKept(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	written, err := om.PutObject(WithLocations(ctx, []string{"self", "other"}), "b", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	change := func(gen int64, locations ...string) LocationsChange {
		return LocationsChange{ETag: written.ETag, LastModified: written.LastModified.Unix(), WrittenAt: written.WrittenAt,
			Locations: locations, Gen: gen}
	}

	require.NoError(t, om.SetLocations(ctx, "b", "k", change(2, "other", "self", "third")))
	require.NoError(t, om.SetLocations(ctx, "b", "k", change(1, "other")))
	locations, gen := locationsOf(t, om, "b", "k")
	assert.Equal(t, []string{"other", "self", "third"}, locations, "an older change is not applied")
	assert.EqualValues(t, 2, gen)

	entry, err := om.ObjectEntry(ctx, "b", "k", "")
	require.NoError(t, err)
	stale := *entry
	stale.Locations, stale.LocationsGen = []string{"other"}, 0
	require.NoError(t, om.PutReplicaMetadata(ctx, &stale))
	locations, gen = locationsOf(t, om, "b", "k")
	assert.Equal(t, []string{"other", "self", "third"}, locations, "an entry with older locations keeps these")
	assert.EqualValues(t, 2, gen)
	assert.True(t, holdsFile(t, om, "b", "k"))

	require.NoError(t, om.SetLocations(ctx, "b", "k", change(2, "other", "third")))
	require.NoError(t, om.SetLocations(ctx, "b", "k", change(2, "other", "self", "third")))
	locations, _ = locationsOf(t, om, "b", "k")
	assert.Equal(t, []string{"other", "third"}, locations, "of one number, the larger list")
	assert.False(t, holdsFile(t, om, "b", "k"))

	newer := *entry
	newer.Locations, newer.LocationsGen = []string{"fourth"}, 3
	require.NoError(t, om.PutReplicaMetadata(ctx, &newer))
	locations, gen = locationsOf(t, om, "b", "k")
	assert.Equal(t, []string{"fourth"}, locations, "an entry with newer locations sets them")
	assert.EqualValues(t, 3, gen)

	later := *entry
	later.ETag, later.LastModified, later.Locations, later.LocationsGen = "later", entry.LastModified.Add(time.Hour), []string{"fifth"}, 0
	require.NoError(t, om.PutReplicaMetadata(ctx, &later))
	locations, gen = locationsOf(t, om, "b", "k")
	assert.Equal(t, []string{"fifth"}, locations, "the entry of a later write has its own locations")
	assert.EqualValues(t, 0, gen)
}

// The locations of a version that is not the latest are kept against an entry
// of it with older ones.
func TestTheNewestLocationsOfAnOlderVersionAreKept(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := WithLocations(context.Background(), []string{"self", "other"})
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "v",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"}}))
	first, err := om.PutObject(ctx, "v", "k", strings.NewReader("one"), http.Header{})
	require.NoError(t, err)
	_, err = om.PutObject(ctx, "v", "k", strings.NewReader("two"), http.Header{})
	require.NoError(t, err)

	require.NoError(t, om.SetLocations(ctx, "v", "k", LocationsChange{VersionID: first.VersionID, ETag: first.ETag,
		LastModified: first.LastModified.Unix(), WrittenAt: first.WrittenAt, Locations: []string{"self", "third"}, Gen: 1}))
	entry, err := om.ObjectEntry(ctx, "v", "k", first.VersionID)
	require.NoError(t, err)
	stale := *entry
	stale.Locations, stale.LocationsGen = []string{"other"}, 0
	require.NoError(t, om.PutReplicaMetadata(ctx, &stale))
	entry, err = om.ObjectEntry(ctx, "v", "k", first.VersionID)
	require.NoError(t, err)
	assert.Equal(t, []string{"self", "third"}, entry.Locations)
	assert.Equal(t, "one", readAll(t, om, "v", "k", first.VersionID))
}

// A copy of a write keeps the newest locations, those it carries or those
// here, and is stored only where they name this node. The write is told by
// the ETag it was sent with: here, a multipart upload's.
func TestACopyKeepsTheNewestLocationsOfItsWrite(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	at := time.Now().Truncate(time.Second)
	copyWith := func(gen int64, locations ...string) {
		t.Helper()
		copyCtx := WithLocationsGen(WithLocations(WithReplicatedWrittenAt(WithReplicatedLastModified(WithReplicaCopy(ctx), at),
			at.UnixNano()), locations), gen)
		copyCtx = WithReplicatedAttributes(copyCtx, ReplicatedAttributes{ETag: "0123456789abcdef0123456789abcdef-2"})
		_, err := om.PutObject(copyCtx, "b", "k", strings.NewReader("data"), http.Header{})
		require.NoError(t, err)
	}
	copyWith(0, "self", "other")
	entry, err := om.ObjectEntry(ctx, "b", "k", "")
	require.NoError(t, err)
	change := func(gen int64, locations ...string) LocationsChange {
		return LocationsChange{ETag: entry.ETag, LastModified: entry.LastModified.Unix(), WrittenAt: entry.WrittenAt,
			Locations: locations, Gen: gen}
	}
	require.NoError(t, om.SetLocations(ctx, "b", "k", change(2, "other", "third")))
	require.False(t, holdsFile(t, om, "b", "k"))

	copyWith(1, "self", "other")
	assert.False(t, holdsFile(t, om, "b", "k"), "an older copy is not stored where the newest locations do not name this node")
	locations, gen := locationsOf(t, om, "b", "k")
	assert.Equal(t, []string{"other", "third"}, locations)
	assert.EqualValues(t, 2, gen)
	copyWith(3, "other")
	assert.False(t, holdsFile(t, om, "b", "k"), "nor a newer one that does not name it")

	require.NoError(t, om.SetLocations(ctx, "b", "k", change(4, "self", "other")))
	copyWith(1, "other")
	assert.Equal(t, "data", readAll(t, om, "b", "k"), "an older copy is stored where the newest locations name this node")
	locations, gen = locationsOf(t, om, "b", "k")
	assert.Equal(t, []string{"self", "other"}, locations)
	assert.EqualValues(t, 4, gen)

	copyWith(5, "self")
	locations, gen = locationsOf(t, om, "b", "k")
	assert.Equal(t, []string{"self"}, locations, "a copy with newer locations sets them")
	assert.EqualValues(t, 5, gen)
}

// A ciphertext copy of a write keeps the newest locations, those it carries or
// those here, and is stored only where they name this node.
func TestARawCopyKeepsTheNewestLocationsOfItsWrite(t *testing.T) {
	ctx := context.Background()
	kekA := newKEKStore(t)
	clusterKeys, err := kekA.EnsureClusterKey()
	require.NoError(t, err)
	kekB := newKEKStore(t)
	require.NoError(t, kekB.AdoptClusterKeys(clusterKeys))
	nodeA, metaA := newNodeManager(t, kekA)
	nodeB, metaB := newNodeManager(t, kekB)
	nodeB.SetLocalNode(func() string { return "b" })
	for _, m := range []metadata.Store{metaA, metaB} {
		require.NoError(t, m.CreateBucket(ctx, &metadata.BucketMetadata{Name: "raw-gen", OwnerID: "u"}))
	}
	written, err := nodeA.PutObject(WithLocations(ctx, []string{"a", "b"}), "raw-gen", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	copyWith := func(gen int64, locations ...string) {
		t.Helper()
		reader, sidecar, meta, err := nodeA.GetObjectRaw(ctx, "raw-gen", "k", "")
		require.NoError(t, err)
		data, err := io.ReadAll(reader)
		reader.Close()
		require.NoError(t, err)
		meta.LocationsGen = gen
		if len(locations) > 0 {
			meta.Locations = locations
		}
		require.NoError(t, nodeB.PutObjectRaw(ctx, "raw-gen", "k", strings.NewReader(string(data)), sidecar, meta))
	}
	change := func(gen int64, locations ...string) LocationsChange {
		return LocationsChange{ETag: written.ETag, LastModified: written.LastModified.Unix(), WrittenAt: written.WrittenAt,
			Locations: locations, Gen: gen}
	}
	copyWith(0, "a", "other")
	_, err = nodeB.ObjectEntry(ctx, "raw-gen", "k", "")
	assert.Error(t, err, "a copy naming other nodes only is not stored")

	copyWith(0)
	require.True(t, holdsFile(t, nodeB, "raw-gen", "k"))
	require.NoError(t, nodeB.SetLocations(ctx, "raw-gen", "k", change(2, "a", "third")))
	require.False(t, holdsFile(t, nodeB, "raw-gen", "k"))

	copyWith(1)
	assert.False(t, holdsFile(t, nodeB, "raw-gen", "k"), "an older copy is not stored where the newest locations do not name this node")
	locations, gen := locationsOf(t, nodeB, "raw-gen", "k")
	assert.Equal(t, []string{"a", "third"}, locations)
	assert.EqualValues(t, 2, gen)

	require.NoError(t, nodeB.SetLocations(ctx, "raw-gen", "k", change(3, "b", "a")))
	copyWith(1)
	assert.Equal(t, "data", readAll(t, nodeB, "raw-gen", "k"), "an older copy is stored where the newest locations name this node")
	locations, gen = locationsOf(t, nodeB, "raw-gen", "k")
	assert.Equal(t, []string{"b", "a"}, locations)
	assert.EqualValues(t, 3, gen)
}
