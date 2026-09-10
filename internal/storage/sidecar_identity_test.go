package storage

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPutRecordsTheObjectIdentityInTheSidecar(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-identity-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)
	ctx := context.Background()

	current := ObjectRef{Bucket: "tenant-1/reports", Key: "2026/informe.pdf"}
	require.NoError(t, fs.Put(ctx, current, strings.NewReader("a"), map[string]string{"content-type": "application/pdf"}))

	stored, err := fs.GetMetadata(ctx, current)
	require.NoError(t, err)
	require.Equal(t, "tenant-1/reports", stored[MetadataBucketField])
	require.Equal(t, "2026/informe.pdf", stored[MetadataKeyField])
	require.Empty(t, stored[MetadataVersionField])
	require.Equal(t, "application/pdf", stored["content-type"])

	version := ObjectRef{Bucket: "tenant-1/reports", Key: "2026/informe.pdf", VersionID: "v9"}
	require.NoError(t, fs.Put(ctx, version, strings.NewReader("b"), nil))

	versionStored, err := fs.GetMetadata(ctx, version)
	require.NoError(t, err)
	require.Equal(t, "v9", versionStored[MetadataVersionField])

	// Reusing a version's sidecar for an unversioned write must not carry the
	// version along.
	other := ObjectRef{Bucket: "tenant-1/reports", Key: "other.txt"}
	require.NoError(t, fs.Put(ctx, other, strings.NewReader("c"), versionStored))

	otherStored, err := fs.GetMetadata(ctx, other)
	require.NoError(t, err)
	require.Equal(t, "other.txt", otherStored[MetadataKeyField])
	require.Empty(t, otherStored[MetadataVersionField])
}

func TestSetMetadataKeepsTheObjectIdentity(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-identity-set-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)
	ctx := context.Background()

	ref := ObjectRef{Bucket: "b", Key: "k.txt", VersionID: "v1"}
	require.NoError(t, fs.Put(ctx, ref, strings.NewReader("a"), nil))
	require.NoError(t, fs.SetMetadata(ctx, ref, map[string]string{"size": "1"}))

	stored, err := fs.GetMetadata(ctx, ref)
	require.NoError(t, err)
	require.Equal(t, "b", stored[MetadataBucketField])
	require.Equal(t, "k.txt", stored[MetadataKeyField])
	require.Equal(t, "v1", stored[MetadataVersionField])
}
