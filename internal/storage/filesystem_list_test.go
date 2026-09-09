package storage

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilesystemListReturnsEveryObjectInTheBucket(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-list-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, fs.CreateBucket(ctx, "bucket"))
	require.NoError(t, fs.Put(ctx, ObjectRef{Bucket: "bucket", Key: "foo.txt"}, strings.NewReader("a"), nil))
	require.NoError(t, fs.Put(ctx, ObjectRef{Bucket: "bucket", Key: "dir/bar.txt"}, strings.NewReader("b"), nil))
	require.NoError(t, fs.Put(ctx, ObjectRef{Bucket: "bucket", Key: "dir/baz.txt", VersionID: "v1"}, strings.NewReader("c"), nil))
	require.NoError(t, fs.Put(ctx, ObjectRef{Bucket: "other", Key: "elsewhere.txt"}, strings.NewReader("d"), nil))

	objects, err := fs.List(ctx, "bucket")
	require.NoError(t, err)
	require.Equal(t, []string{"dir/bar.txt", "dir/baz.txt@v1", "foo.txt"}, listedRefs(objects))
}

func TestFilesystemListOnAnAbsentBucketIsEmpty(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-list-absent-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)

	objects, err := fs.List(context.Background(), "nope")
	require.NoError(t, err)
	require.Empty(t, objects)
}

func listedRefs(objects []ObjectInfo) []string {
	refs := make([]string, 0, len(objects))
	for _, obj := range objects {
		name := obj.Ref.Key
		if obj.Ref.VersionID != "" {
			name += "@" + obj.Ref.VersionID
		}
		refs = append(refs, name)
	}
	sort.Strings(refs)
	return refs
}
