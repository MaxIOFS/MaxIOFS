package storage

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeletingAFolderMarkerLeavesTheObjectsUnderItAlone(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-folder-delete-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)
	ctx := context.Background()

	marker := ObjectRef{Bucket: "b1", Key: "photos/"}
	object := ObjectRef{Bucket: "b1", Key: "photos/cat.jpg"}

	require.NoError(t, fs.Put(ctx, marker, strings.NewReader(""), nil))
	require.NoError(t, fs.Put(ctx, object, strings.NewReader("the bytes"), nil))

	require.NoError(t, fs.Delete(ctx, marker))

	exists, err := fs.Exists(ctx, object)
	require.NoError(t, err)
	require.True(t, exists, "deleting the folder marker destroyed the object under it")

	exists, err = fs.Exists(ctx, marker)
	require.NoError(t, err)
	require.False(t, exists)
}
