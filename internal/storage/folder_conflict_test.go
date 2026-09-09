package storage

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPutFolderMarkerRefusesToDisplaceObject(t *testing.T) {
	root, err := os.MkdirTemp("", "folderconflict")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)
	ctx := context.Background()

	const payload = "PAYLOAD-THAT-MATTERS"
	require.NoError(t, fs.Put(ctx, ObjectRef{Bucket: "bucket", Key: "report"}, strings.NewReader(payload), nil))

	require.ErrorIs(t, fs.Put(ctx, ObjectRef{Bucket: "bucket", Key: "report/"}, strings.NewReader(""), nil), ErrPathConflict)
	require.ErrorIs(t, fs.Put(ctx, ObjectRef{Bucket: "bucket", Key: "report/sub/"}, strings.NewReader(""), nil), ErrPathConflict)

	rc, meta, err := fs.Get(ctx, ObjectRef{Bucket: "bucket", Key: "report"})
	require.NoError(t, err)
	defer rc.Close()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, payload, string(got))
	require.Equal(t, "20", meta["size"])
}
