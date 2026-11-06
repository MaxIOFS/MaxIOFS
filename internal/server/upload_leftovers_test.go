package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A start removes the temporary files of uploads cut short by a crash, and
// nothing else: a bucket may hold a name that looks like one.
func TestAStartRemovesUploadLeftovers(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"maxiofs-upload-123", "maxiofs-upload-456", "other-file"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644))
	}
	require.NoError(t, os.Mkdir(filepath.Join(root, "maxiofs-upload-bucket"), 0o755))

	s := &Server{config: &config.Config{Storage: config.StorageConfig{Root: root}}}
	s.removeUploadLeftovers()

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	var left []string
	for _, entry := range entries {
		left = append(left, entry.Name())
	}
	assert.ElementsMatch(t, []string{"maxiofs-upload-bucket", "other-file"}, left)
}
