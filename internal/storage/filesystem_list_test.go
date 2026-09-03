package storage

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilesystemListPrefixDoesNotRequirePhysicalDirectory(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-list-prefix-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)

	require.NoError(t, fs.Put(context.Background(), "foo.txt", strings.NewReader("a"), nil))
	require.NoError(t, fs.Put(context.Background(), "foobar.txt", strings.NewReader("b"), nil))
	require.NoError(t, fs.Put(context.Background(), "bar.txt", strings.NewReader("c"), nil))

	objects, err := fs.List(context.Background(), "foo", true)
	require.NoError(t, err)

	require.Equal(t, []string{"foo.txt", "foobar.txt"}, listObjectPaths(objects))
}

func TestFilesystemListNestedPrefixDoesNotRequireLeafDirectory(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-list-nested-prefix-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)

	require.NoError(t, fs.Put(context.Background(), "dir/foo.txt", strings.NewReader("a"), nil))
	require.NoError(t, fs.Put(context.Background(), "dir/foobar.txt", strings.NewReader("b"), nil))
	require.NoError(t, fs.Put(context.Background(), "dir/bar.txt", strings.NewReader("c"), nil))

	objects, err := fs.List(context.Background(), "dir/foo", true)
	require.NoError(t, err)

	require.Equal(t, []string{"dir/foo.txt", "dir/foobar.txt"}, listObjectPaths(objects))
}

func listObjectPaths(objects []ObjectInfo) []string {
	paths := make([]string, 0, len(objects))
	for _, obj := range objects {
		paths = append(paths, obj.Path)
	}
	sort.Strings(paths)
	return paths
}
