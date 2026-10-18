package object

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/erasure"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testLayout = erasure.Layout{Data: 2, Parity: 1, Block: 16}

// shardedEntry is the entry of a write of 40 bytes cut in testLayout's shards
// held by the nodes named.
func shardedEntry(bucket, key, versionID, etag string, at time.Time, gen int64, nodes ...string) *metadata.ObjectMetadata {
	return &metadata.ObjectMetadata{Bucket: bucket, Key: key, VersionID: versionID, Size: 40, ETag: etag,
		LastModified: at, WrittenAt: at.UnixNano(), Locations: nodes, LocationsGen: gen,
		ECData: testLayout.Data, ECParity: testLayout.Parity, ECBlock: testLayout.Block}
}

func shardOfEntry(e *metadata.ObjectMetadata, index int, gen int64) ShardInfo {
	return ShardOf(fromMetadataObject(e), index, gen)
}

func shardBytes(index int) []byte {
	return bytes.Repeat([]byte{byte('a' + index)}, int(testLayout.ShardSize(40)))
}

func readShard(t *testing.T, om *objectManager, info ShardInfo, offset int64) string {
	t.Helper()
	r, err := om.OpenShard(context.Background(), info, offset)
	require.NoError(t, err)
	defer r.Close()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(data)
}

func shardsOn(t *testing.T, om *objectManager, bucket, key, versionID string) []int {
	t.Helper()
	ref := om.objectRef(bucket, key)
	if versionID != "" {
		ref = om.versionRef(bucket, key, versionID)
	}
	indexes, err := om.storage.Shards(context.Background(), ref)
	require.NoError(t, err)
	return indexes
}

// A shard is stored encrypted beside the object's file and read back from
// any offset, only as the shard of its write and layout.
func TestAShardIsStoredEncryptedAndReadBack(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	entry := shardedEntry("b", "k", "", "e1", time.Now().Truncate(time.Second), 1, "other", "self", "third")
	info := shardOfEntry(entry, 1, 1)
	data := shardBytes(1)

	require.NoError(t, om.PutShard(ctx, info, bytes.NewReader(data)))
	ref := om.shardRef(info)
	raw, sidecar, err := om.storage.Get(ctx, ref)
	require.NoError(t, err)
	stored, err := io.ReadAll(raw)
	raw.Close()
	require.NoError(t, err)
	assert.Equal(t, "true", sidecar["encrypted"])
	assert.NotContains(t, string(stored), string(data), "the shard is stored encrypted")
	assert.Equal(t, "1", sidecar[storage.MetadataShardField])

	assert.Equal(t, string(data), readShard(t, om, info, 0))
	assert.Equal(t, string(data[7:]), readShard(t, om, info, 7))

	for name, other := range map[string]ShardInfo{
		"another write":  func() ShardInfo { o := info; o.ETag = "e2"; return o }(),
		"another layout": func() ShardInfo { o := info; o.Layout.Block = 8; return o }(),
		"another index":  func() ShardInfo { o := info; o.Index = 0; return o }(),
	} {
		_, err := om.OpenShard(ctx, other, 0)
		assert.ErrorIs(t, err, ErrObjectNotFound, name)
	}
	moved := info
	moved.Gen = 5
	assert.Equal(t, string(data), readShard(t, om, moved, 0), "a shard is the same whatever placement it was made for")

	assert.Error(t, om.PutShard(ctx, info, bytes.NewReader(data[1:])), "a shard of the wrong size")
	wrong := info
	wrong.Index = 3
	assert.Error(t, om.PutShard(ctx, wrong, bytes.NewReader(data)), "no shard 3 in a layout of 2+1")
	assert.ErrorIs(t, om.PutShard(ctx, ShardInfo{Bucket: "none", Key: "k", Layout: testLayout, Size: 40}, bytes.NewReader(data)), ErrBucketNotFound)

	other := info
	other.ETag = "e2"
	require.NoError(t, om.DeleteShard(ctx, other))
	assert.Equal(t, []int{1}, shardsOn(t, om, "b", "k", ""), "a shard of another write is not deleted")
	require.NoError(t, om.DeleteShard(ctx, info))
	assert.Empty(t, shardsOn(t, om, "b", "k", ""))
}

// No node holds the whole data of an entry cut in shards: a GET names the
// nodes and the layout, and this node holds the shard the entry names it for.
func TestAnEntryInShardsIsReadFromTheNodesHoldingThem(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	_, err := om.PutObject(ctx, "b", "k", strings.NewReader("whole"), http.Header{})
	require.NoError(t, err)

	later := time.Now().Add(time.Hour).Truncate(time.Second)
	entry := shardedEntry("b", "k", "", "e1", later, 1, "self", "other", "third")
	require.NoError(t, om.PutShard(ctx, shardOfEntry(entry, 0, 1), bytes.NewReader(shardBytes(0))))
	require.NoError(t, om.PutReplicaMetadata(ctx, entry))

	_, _, err = om.GetObject(ctx, "b", "k")
	var elsewhere *DataElsewhereError
	require.ErrorAs(t, err, &elsewhere)
	layout, sharded := elsewhere.Object.Erasure()
	require.True(t, sharded)
	assert.Equal(t, testLayout, layout)
	assert.Equal(t, []string{"self", "other", "third"}, elsewhere.Locations)
	exists, err := om.storage.Exists(ctx, om.objectRef("b", "k"))
	require.NoError(t, err)
	assert.False(t, exists, "the whole file of the earlier write is removed")

	held, err := om.HoldsData(ctx, "b", "k", "")
	require.NoError(t, err)
	assert.True(t, held, "the shard it is named for")
	require.NoError(t, om.storage.Delete(ctx, om.objectRef("b", "k").ShardOf(0)))
	held, err = om.HoldsData(ctx, "b", "k", "")
	require.NoError(t, err)
	assert.False(t, held)
	another := shardedEntry("b", "k", "", "e9", later.Add(time.Minute), 1, "self", "other", "third")
	require.NoError(t, om.PutShard(ctx, shardOfEntry(another, 0, 1), bytes.NewReader(shardBytes(0))))
	held, err = om.HoldsData(ctx, "b", "k", "")
	require.NoError(t, err)
	assert.False(t, held, "the shard of another write")

	other := shardedEntry("b", "k2", "", "e1", later, 1, "other", "third", "fourth")
	require.NoError(t, om.PutReplicaMetadata(ctx, other))
	held, err = om.HoldsData(ctx, "b", "k2", "")
	require.NoError(t, err)
	assert.False(t, held, "an entry that names other nodes")
}

// The shards a node holds follow the newest entry of their write: a shard
// made for a newer placement stays until that placement lands, one the
// placement does not name here goes, and the shards of an earlier write go.
func TestAShardFollowsThePlacementOfItsWrite(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	at := time.Now().Truncate(time.Second)

	// Held as shard 0 at placement 1.
	entry := shardedEntry("b", "k", "", "e1", at, 1, "self", "a", "b")
	require.NoError(t, om.PutShard(ctx, shardOfEntry(entry, 0, 1), bytes.NewReader(shardBytes(0))))
	require.NoError(t, om.PutReplicaMetadata(ctx, entry))
	// A move stages shard 2 here for placement 2.
	moved := shardedEntry("b", "k", "", "e1", at, 2, "a", "c", "self")
	require.NoError(t, om.PutShard(ctx, shardOfEntry(moved, 2, 2), bytes.NewReader(shardBytes(2))))
	// An entry of placement 1 arriving again keeps both.
	require.NoError(t, om.PutReplicaMetadata(ctx, entry))
	assert.Equal(t, []int{0, 2}, shardsOn(t, om, "b", "k", ""))

	// Placement 2 lands: shard 0 is not this node's any more.
	require.NoError(t, om.SetLocations(ctx, "b", "k", LocationsChange{ETag: "e1", LastModified: at.Unix(), WrittenAt: at.UnixNano(),
		Locations: moved.Locations, Gen: 2, Layout: testLayout}))
	assert.Equal(t, []int{2}, shardsOn(t, om, "b", "k", ""))
	assert.Equal(t, string(shardBytes(2)), readShard(t, om, shardOfEntry(moved, 2, 2), 0))
	got, err := om.ObjectEntry(ctx, "b", "k", "")
	require.NoError(t, err)
	assert.Equal(t, testLayout.Data, got.ECData)
	assert.EqualValues(t, 2, got.LocationsGen)

	// An entry of whole copies from an earlier placement keeps the shards.
	copies := *entry
	copies.LocationsGen, copies.Locations = 1, []string{"a", "b"}
	copies.ECData, copies.ECParity, copies.ECBlock = 0, 0, 0
	require.NoError(t, om.PutReplicaMetadata(ctx, &copies))
	got, err = om.ObjectEntry(ctx, "b", "k", "")
	require.NoError(t, err)
	assert.Equal(t, testLayout.Data, got.ECData, "the newer placement stays")
	assert.Equal(t, moved.Locations, got.Locations)
	assert.Equal(t, []int{2}, shardsOn(t, om, "b", "k", ""))

	// Placement 3 cuts the data otherwise: the shard of index 2 it names
	// here is not the one held.
	require.NoError(t, om.SetLocations(ctx, "b", "k", LocationsChange{ETag: "e1", LastModified: at.Unix(), WrittenAt: at.UnixNano(),
		Locations: []string{"a", "c", "self", "d"}, Gen: 3, Layout: erasure.Layout{Data: 3, Parity: 1, Block: 16}}))
	assert.Empty(t, shardsOn(t, om, "b", "k", ""))

	// Placement 4 keeps whole copies: no shard stays.
	require.NoError(t, om.PutShard(ctx, shardOfEntry(moved, 2, 2), bytes.NewReader(shardBytes(2))))
	require.NoError(t, om.SetLocations(ctx, "b", "k", LocationsChange{ETag: "e1", LastModified: at.Unix(), WrittenAt: at.UnixNano(),
		Locations: []string{"self", "a"}, Gen: 4}))
	assert.Empty(t, shardsOn(t, om, "b", "k", ""))
	got, err = om.ObjectEntry(ctx, "b", "k", "")
	require.NoError(t, err)
	assert.Zero(t, got.ECData, "whole copies again")

	// A later write's shard staged before its entry stays when an entry of
	// this write is stored; it goes once a write after it is.
	later := shardedEntry("b", "k", "", "e2", at.Add(time.Minute), 1, "self", "a", "b")
	require.NoError(t, om.PutShard(ctx, shardOfEntry(later, 0, 1), bytes.NewReader(shardBytes(0))))
	require.NoError(t, om.SetLocations(ctx, "b", "k", LocationsChange{ETag: "e1", LastModified: at.Unix(), WrittenAt: at.UnixNano(),
		Locations: []string{"a", "b"}, Gen: 5}))
	assert.Equal(t, []int{0}, shardsOn(t, om, "b", "k", ""))
	latest := shardedEntry("b", "k", "", "e3", at.Add(2*time.Minute), 1, "a", "b", "c")
	require.NoError(t, om.PutReplicaMetadata(ctx, latest))
	assert.Empty(t, shardsOn(t, om, "b", "k", ""))

	// The shard of a write older than the one held is not kept.
	require.NoError(t, om.PutShard(ctx, shardOfEntry(later, 0, 1), bytes.NewReader(shardBytes(0))))
	assert.Empty(t, shardsOn(t, om, "b", "k", ""))
}

// A delete removes the shards of the writes it removes, and a write without
// a version the shards of the write it replaces.
func TestTheShardsOfARemovedWriteGo(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "v",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"}}))
	at := time.Now().Truncate(time.Second)
	held := func(e *metadata.ObjectMetadata) {
		t.Helper()
		require.NoError(t, om.PutShard(ctx, shardOfEntry(e, 1, 1), bytes.NewReader(shardBytes(1))))
		require.NoError(t, om.PutReplicaMetadata(ctx, e))
		require.Len(t, shardsOn(t, om, e.Bucket, e.Key, e.VersionID), 1)
	}

	deleted := shardedEntry("b", "deleted", "", "e1", at, 1, "a", "self", "b")
	held(deleted)
	_, err := om.DeleteObject(ctx, "b", "deleted", false)
	require.NoError(t, err)
	assert.Empty(t, shardsOn(t, om, "b", "deleted", ""))

	version := shardedEntry("v", "k", "1760000000000000000-aaaa", "e1", at, 1, "a", "self", "b")
	held(version)
	_, err = om.DeleteObject(ctx, "v", "k", false, version.VersionID)
	require.NoError(t, err)
	assert.Empty(t, shardsOn(t, om, "v", "k", version.VersionID))

	dropped := shardedEntry("b", "dropped", "", "e1", at, 1, "a", "self", "b")
	held(dropped)
	require.NoError(t, om.DropEntry(ctx, "b", "dropped", ""))
	assert.Empty(t, shardsOn(t, om, "b", "dropped", ""))

	replaced := shardedEntry("b", "replaced", "", "e1", at.Add(-time.Hour), 1, "a", "self", "b")
	held(replaced)
	_, err = om.PutObject(ctx, "b", "replaced", strings.NewReader("whole"), http.Header{})
	require.NoError(t, err)
	assert.Empty(t, shardsOn(t, om, "b", "replaced", ""))
	assert.Equal(t, "whole", readAll(t, om, "b", "replaced"))

	completed := shardedEntry("b", "completed", "", "e1", at.Add(-time.Hour), 1, "a", "self", "b")
	held(completed)
	upload, err := om.CreateMultipartUpload(ctx, "b", "completed", http.Header{})
	require.NoError(t, err)
	part, err := om.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("parts"))
	require.NoError(t, err)
	_, err = om.CompleteMultipartUpload(ctx, upload.UploadID, []Part{*part})
	require.NoError(t, err)
	assert.Empty(t, shardsOn(t, om, "b", "completed", ""))
	assert.Equal(t, "parts", readAll(t, om, "b", "completed"))

	_, err = om.PutObject(ctx, "b", "source", strings.NewReader("raw copy"), http.Header{})
	require.NoError(t, err)
	reader, sidecar, source, err := om.GetObjectRaw(ctx, "b", "source", "")
	require.NoError(t, err)
	defer reader.Close()
	copied := shardedEntry("b", "copied", "", "e1", at.Add(-time.Hour), 1, "a", "self", "b")
	held(copied)
	source.Key = "copied"
	require.NoError(t, om.PutObjectRaw(ctx, "b", "copied", reader, sidecar, source))
	assert.Empty(t, shardsOn(t, om, "b", "copied", ""))
	assert.Equal(t, "raw copy", readAll(t, om, "b", "copied"))
}

// A whole copy of a write arriving after the write was cut in shards here is
// not stored: the entry keeps its shards.
func TestAWholeCopyOfAWriteInShardsIsNotStored(t *testing.T) {
	om, store, cleanup := setupTestManagerWithStore(t)
	defer cleanup()
	ctx := context.Background()
	om.SetLocalNode(func() string { return "self" })
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	content := strings.Repeat("0123456789", 4)
	sum := md5.Sum([]byte(content))
	at := time.Now().Truncate(time.Second)
	entry := shardedEntry("b", "k", "", hex.EncodeToString(sum[:]), at, 2, "self", "a", "b")
	require.NoError(t, om.PutShard(ctx, shardOfEntry(entry, 0, 2), bytes.NewReader(shardBytes(0))))
	require.NoError(t, om.PutReplicaMetadata(ctx, entry))

	copyCtx := WithReplicaCopy(WithReplicatedWrittenAt(WithReplicatedLastModified(ctx, at), at.UnixNano()))
	copyCtx = WithLocationsGen(WithLocations(copyCtx, []string{"self", "a"}), 1)
	_, err := om.PutObject(copyCtx, "b", "k", strings.NewReader(content), http.Header{})
	require.NoError(t, err)
	exists, err := om.storage.Exists(ctx, om.objectRef("b", "k"))
	require.NoError(t, err)
	assert.False(t, exists)
	got, err := om.ObjectEntry(ctx, "b", "k", "")
	require.NoError(t, err)
	assert.Equal(t, testLayout.Data, got.ECData)
	assert.EqualValues(t, 2, got.LocationsGen)
	assert.Equal(t, []int{0}, shardsOn(t, om, "b", "k", ""))
}
