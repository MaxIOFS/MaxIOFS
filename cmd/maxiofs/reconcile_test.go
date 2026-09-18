package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/require"
)

func TestReconcileCommandStorageRoot(t *testing.T) {
	for _, custom := range []bool{false, true} {
		name := "default"
		if custom {
			name = "custom"
		}
		t.Run(name, func(t *testing.T) {
			dataDir := t.TempDir()
			objectsRoot := filepath.Join(dataDir, "objects")
			if custom {
				objectsRoot = t.TempDir()
			}
			s, err := metadata.NewPebbleStore(metadata.PebbleOptions{DataDir: dataDir})
			require.NoError(t, err)
			t.Cleanup(func() { _ = s.Close() })
			require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "bucket"}))
			backend, err := storage.NewFilesystemBackend(storage.Config{Root: objectsRoot})
			require.NoError(t, err)
			require.NoError(t, backend.CreateBucket(t.Context(), "bucket"))
			require.NoError(t, backend.Put(t.Context(), storage.ObjectRef{Bucket: "bucket", Key: "key"}, strings.NewReader("body"), nil))
			require.NoError(t, s.Close())
			cmd := newReconcileCmd()
			cmd.Flags().String("data-dir", "", "")
			args := []string{"--data-dir", dataDir}
			if custom {
				args = append(args, "--storage-root", objectsRoot)
			}
			cmd.SetArgs(args)
			require.NoError(t, cmd.Execute())
			s, err = metadata.NewPebbleStore(metadata.PebbleOptions{DataDir: dataDir})
			require.NoError(t, err)
			obj, err := s.GetObject(t.Context(), "bucket", "key")
			require.NoError(t, err)
			require.Equal(t, int64(4), obj.Size)
		})
	}
}
