package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUploadsAreListedAndDiscarded(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-uploads-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, fs.PutPart(ctx, "upload-a", 1, strings.NewReader("part one"), nil))
	require.NoError(t, fs.PutPart(ctx, "upload-b", 1, strings.NewReader("part one"), nil))

	ids, err := fs.ListUploads(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"upload-a", "upload-b"}, ids)

	require.NoError(t, fs.DeleteUpload(ctx, "upload-a"))

	ids, err = fs.ListUploads(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"upload-b"}, ids)

	_, err = os.Stat(filepath.Join(root, ".maxiofs", "multipart", "parts", "upload-a"))
	require.True(t, os.IsNotExist(err), "the upload directory should be gone")

	exists, err := fs.PartExists(ctx, "upload-b", 1)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestDeleteUploadRefusesASeparatorInTheID(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-uploads-escape-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)

	for _, id := range []string{"", "..", "../..", "a/b", `a\b`} {
		require.ErrorIs(t, fs.DeleteUpload(context.Background(), id), ErrInvalidPath, "id %q", id)
	}
}

func TestListUploadsOnAFreshRootIsEmpty(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-uploads-fresh-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)

	ids, err := fs.ListUploads(context.Background())
	require.NoError(t, err)
	require.Empty(t, ids)
}

// An upload ID that is not one directory name reaches nothing outside the
// parts' directory.
func TestAPartOfAnUploadIDThatClimbsIsRefused(t *testing.T) {
	root := t.TempDir()
	fs, err := NewFilesystemBackend(Config{Root: filepath.Join(root, "store")})
	require.NoError(t, err)
	ctx := context.Background()
	for _, id := range []string{"", ".", "..", "../../outside", `..\outside`, "a/b"} {
		require.ErrorIs(t, fs.PutPart(ctx, id, 1, strings.NewReader("x"), nil), ErrInvalidPath, id)
		_, _, err := fs.GetPart(ctx, id, 1)
		require.ErrorIs(t, err, ErrInvalidPath, id)
		_, err = fs.PartMetadata(ctx, id, 1)
		require.ErrorIs(t, err, ErrInvalidPath, id)
		_, err = fs.PartExists(ctx, id, 1)
		require.ErrorIs(t, err, ErrInvalidPath, id)
		require.ErrorIs(t, fs.DeletePart(ctx, id, 1), ErrInvalidPath, id)
		require.ErrorIs(t, fs.DeleteUpload(ctx, id), ErrInvalidPath, id)
	}
	_, err = os.Stat(filepath.Join(root, "outside"))
	require.True(t, os.IsNotExist(err), "nothing written outside the store")
}
