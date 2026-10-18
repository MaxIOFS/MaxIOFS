package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shards of a write are files of their own beside the write's file, each
// with its index in its sidecar; the shards a write has are found by its ref.
func TestTheShardsOfAWriteAreStoredBesideIt(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-shards-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)
	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)
	ctx := context.Background()

	write := ObjectRef{Bucket: "tenant-1/b", Key: "k", VersionID: "v1"}
	require.NoError(t, fs.Put(ctx, write, strings.NewReader("whole"), nil))
	require.NoError(t, fs.Put(ctx, write.ShardOf(0), strings.NewReader("first"), map[string]string{"etag": "x"}))
	require.NoError(t, fs.Put(ctx, write.ShardOf(2), strings.NewReader("third"), nil))
	other := ObjectRef{Bucket: "tenant-1/b", Key: "k"}
	require.NoError(t, fs.Put(ctx, other.ShardOf(1), strings.NewReader("unversioned"), nil))

	assert.Equal(t, RefPath(write)+".shard-0", RefPath(write.ShardOf(0)))
	assert.FileExists(t, filepath.Join(root, filepath.FromSlash(RefPath(write.ShardOf(2)))))
	assert.True(t, IsShardFileName(filepath.Base(RefPath(write.ShardOf(2)))))
	assert.False(t, IsShardFileName(filepath.Base(RefPath(write))))
	assert.False(t, IsShardFileName(filepath.Base(RefPath(write.ShardOf(2)))+".metadata"))

	read := func(ref ObjectRef) string {
		r, meta, err := fs.Get(ctx, ref)
		require.NoError(t, err)
		defer r.Close()
		data, err := io.ReadAll(r)
		require.NoError(t, err)
		if index, isShard := ref.ShardIndex(); isShard {
			assert.Equal(t, strconv.Itoa(index), meta[MetadataShardField])
		} else {
			assert.Empty(t, meta[MetadataShardField])
		}
		return string(data)
	}
	assert.Equal(t, "whole", read(write))
	assert.Equal(t, "first", read(write.ShardOf(0)))
	assert.Equal(t, "third", read(write.ShardOf(2)))

	shards, err := fs.Shards(ctx, write)
	require.NoError(t, err)
	assert.Equal(t, []int{0, 2}, shards)
	shards, err = fs.Shards(ctx, write.ShardOf(2))
	require.NoError(t, err)
	assert.Equal(t, []int{0, 2}, shards, "a shard's ref finds its write's shards")
	shards, err = fs.Shards(ctx, other)
	require.NoError(t, err)
	assert.Equal(t, []int{1}, shards)

	require.NoError(t, fs.Delete(ctx, write.ShardOf(0)))
	shards, err = fs.Shards(ctx, write)
	require.NoError(t, err)
	assert.Equal(t, []int{2}, shards)
	assert.Equal(t, "whole", read(write), "the write's file stays")

	// A shard keeps its index when its sidecar is written again.
	require.NoError(t, fs.SetMetadata(ctx, write.ShardOf(2), map[string]string{"etag": "y"}))
	meta, err := fs.GetMetadata(ctx, write.ShardOf(2))
	require.NoError(t, err)
	assert.Equal(t, "2", meta[MetadataShardField])
	// The write's own sidecar written from a shard's does not take the index.
	require.NoError(t, fs.SetMetadata(ctx, write, meta))
	meta, err = fs.GetMetadata(ctx, write)
	require.NoError(t, err)
	assert.Empty(t, meta[MetadataShardField])

	listed, err := fs.List(ctx, "tenant-1/b")
	require.NoError(t, err)
	var refs []ObjectRef
	for _, o := range listed {
		refs = append(refs, o.Ref)
	}
	assert.ElementsMatch(t, []ObjectRef{write, write.ShardOf(2), other.ShardOf(1)}, refs)
	assert.True(t, slices.ContainsFunc(refs, func(r ObjectRef) bool { i, ok := r.ShardIndex(); return ok && i == 2 }))

	shards, err = fs.Shards(ctx, ObjectRef{Bucket: "tenant-1/b", Key: "none"})
	require.NoError(t, err)
	assert.Empty(t, shards)
}
