package storage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPut_FlushesDataAndSidecar reads the source: an fsync that is not called
// cannot be observed from the outside, and the failure being guarded against is
// someone removing the call, not the filesystem misbehaving.
func TestPut_FlushesDataAndSidecar(t *testing.T) {
	source, err := os.ReadFile("filesystem.go")
	require.NoError(t, err)
	text := string(source)

	assert.Contains(t, text, "tempFile.Sync()",
		"the data file must reach the platter before the rename that publishes it")
	assert.Contains(t, text, "syncDir(dir)",
		"the renames must be made durable, or which one survived is undefined")

	// Close errors are where a delayed write failure surfaces. Discarding one
	// reports a successful write for data that never landed.
	discarded := regexp.MustCompile(`\n\ttempFile\.Close\(\)\n`)
	assert.False(t, discarded.MatchString(text),
		"tempFile.Close() must be checked, not discarded")

	assert.GreaterOrEqual(t, strings.Count(text, ".Sync()"), 2,
		"both the data file and the metadata sidecar are flushed")
}

// TestPut_SurvivesAndReadsBack is the behavioural half: the flushes must not
// break the write path they protect.
func TestPut_SurvivesAndReadsBack(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-durability-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	backend, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)

	ctx := context.Background()
	payload := bytes.Repeat([]byte("durable"), 512)

	require.NoError(t, backend.Put(ctx, "bucket/object.bin",
		bytes.NewReader(payload), map[string]string{"content-type": "application/octet-stream"}))

	reader, metadata, err := backend.Get(ctx, "bucket/object.bin")
	require.NoError(t, err)
	defer reader.Close()

	got := make([]byte, len(payload))
	_, err = reader.Read(got)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
	assert.Equal(t, "application/octet-stream", metadata["content-type"])

	// The sidecar is on disk beside the object, and no staging file is left.
	_, err = os.Stat(filepath.Join(root, "bucket", "object.bin.metadata"))
	assert.NoError(t, err, "the sidecar is committed")
	_, err = os.Stat(filepath.Join(root, "bucket", "object.bin.metadata-staging"))
	assert.True(t, os.IsNotExist(err), "no stage is left behind by a successful write")
}

// TestGeneratedMetadata_IsMarked: the map derived from the bytes on disk has to
func TestGeneratedMetadata_IsMarked(t *testing.T) {
	root, err := os.MkdirTemp("", "maxiofs-generated-*")
	require.NoError(t, err)
	defer os.RemoveAll(root)

	backend, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, backend.Put(ctx, "bucket/plain.txt",
		bytes.NewReader([]byte("hello")), nil))

	withSidecar, err := backend.GetMetadata(ctx, "bucket/plain.txt")
	require.NoError(t, err)
	assert.NotEqual(t, "true", withSidecar[MetadataGeneratedKey],
		"a real sidecar is not a derived map")

	require.NoError(t, os.Remove(filepath.Join(root, "bucket", "plain.txt.metadata")))

	derived, err := backend.GetMetadata(ctx, "bucket/plain.txt")
	require.NoError(t, err)
	assert.Equal(t, "true", derived[MetadataGeneratedKey],
		"without a sidecar the map is derived, and must admit it")
}
