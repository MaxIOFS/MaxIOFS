package storage

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenObjectSurvivesReplacementAndDeletion(t *testing.T) {
	fs, _ := newStagedTestBackend(t)
	ctx := t.Context()
	ref := ObjectRef{Bucket: "snapshot", Key: "key"}
	require.NoError(t, fs.Put(ctx, ref, strings.NewReader("old"), map[string]string{"generation": "old"}))
	r, meta, err := fs.Get(ctx, ref)
	require.NoError(t, err)
	defer r.Close()
	require.NoError(t, fs.Put(ctx, ref, strings.NewReader("new content"), map[string]string{"generation": "new"}))
	r2, meta2, err := fs.Get(ctx, ref)
	require.NoError(t, err)
	defer r2.Close()
	require.NoError(t, fs.Delete(ctx, ref))
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "old", string(b))
	require.Equal(t, "old", meta["generation"])
	b, err = io.ReadAll(r2)
	require.NoError(t, err)
	require.Equal(t, "new content", string(b))
	require.Equal(t, "new", meta2["generation"])
}

func TestOpenObjectLongStorageRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat("a", 100), strings.Repeat("b", 100))
	fs, err := NewFilesystemBackend(Config{Root: root})
	require.NoError(t, err)
	ref := ObjectRef{Bucket: "long-path", Key: "key"}
	require.NoError(t, fs.Put(t.Context(), ref, strings.NewReader("old"), nil))
	reader, _, err := fs.Get(t.Context(), ref)
	require.NoError(t, err)
	defer reader.Close()
	require.NoError(t, fs.Put(t.Context(), ref, strings.NewReader("replacement"), nil))
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, "old", string(content))
}
