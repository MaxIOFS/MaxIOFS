package storage

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func read(t *testing.T, fs *FilesystemBackend, ref ObjectRef) string {
	t.Helper()
	rc, _, err := fs.Get(context.Background(), ref)
	require.NoError(t, err)
	defer rc.Close()
	body, err := io.ReadAll(rc)
	require.NoError(t, err)
	return string(body)
}

// A key and a folder marker of the same name are two distinct S3 keys, and the
// layout keeps them apart.
func TestObjectAndFolderMarkerOfTheSameNameCoexist(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-coexist-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)
	ctx := context.Background()

	object := ObjectRef{Bucket: "bucket", Key: "2026"}
	marker := ObjectRef{Bucket: "bucket", Key: "2026/"}
	nested := ObjectRef{Bucket: "bucket", Key: "2026/enero/informe.pdf"}

	require.NoError(t, fs.Put(ctx, object, strings.NewReader("PAYLOAD-THAT-MATTERS"), nil))
	require.NoError(t, fs.Put(ctx, marker, strings.NewReader(""), nil))
	require.NoError(t, fs.Put(ctx, nested, strings.NewReader("nested"), nil))

	require.Equal(t, "PAYLOAD-THAT-MATTERS", read(t, fs, object))
	require.Equal(t, "", read(t, fs, marker))
	require.Equal(t, "nested", read(t, fs, nested))
}

// Two keys differing only in case are two distinct S3 keys, on a
// case-insensitive filesystem as well.
func TestKeysDifferingOnlyInCaseCoexist(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-case-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)
	ctx := context.Background()

	upper := ObjectRef{Bucket: "bucket", Key: "Report"}
	lower := ObjectRef{Bucket: "bucket", Key: "report"}

	require.NoError(t, fs.Put(ctx, upper, strings.NewReader("UPPERCASE-ORIGINAL"), nil))
	require.NoError(t, fs.Put(ctx, lower, strings.NewReader("lowercase"), nil))

	require.Equal(t, "UPPERCASE-ORIGINAL", read(t, fs, upper))
	require.Equal(t, "lowercase", read(t, fs, lower))
}

// A key whose name would clash with the sidecar suffix is just another key.
func TestKeyEndingInMetadataIsAnOrdinaryKey(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-sidecarname-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)
	ctx := context.Background()

	object := ObjectRef{Bucket: "bucket", Key: "notes.txt"}
	lookalike := ObjectRef{Bucket: "bucket", Key: "notes.txt.metadata"}

	require.NoError(t, fs.Put(ctx, object, strings.NewReader("real"), nil))
	require.NoError(t, fs.Put(ctx, lookalike, strings.NewReader("decoy"), nil))

	require.Equal(t, "real", read(t, fs, object))
	require.Equal(t, "decoy", read(t, fs, lookalike))
}
