package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/rollback"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/require"
)

func TestStartRejectsIncompleteRollback(t *testing.T) {
	root := t.TempDir()
	b, err := storage.NewFilesystemBackend(storage.Config{Root: root})
	require.NoError(t, err)
	m, err := metadata.NewPebbleStore(metadata.PebbleOptions{DataDir: root, CacheSizeMB: 8})
	require.NoError(t, err)
	defer m.Close()
	manifest := filepath.Join(root, rollback.ObjectPrefix+"broken"+rollback.ManifestSuffix)
	require.NoError(t, os.WriteFile(manifest, []byte("invalid json"), 0600))
	s := &Server{config: &config.Config{DataDir: root, Storage: config.StorageConfig{Root: root}}, storageBackend: b, metadataStore: m}
	require.ErrorContains(t, s.Start(t.Context()), "rollback has 1 failures")
	require.FileExists(t, manifest)
}
