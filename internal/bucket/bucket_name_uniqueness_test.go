package bucket

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

func TestCreateBucketRejectsNameTakenInAnotherTenant(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "maxiofs-bucket-unique-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	storageBackend, err := storage.NewBackend(config.StorageConfig{
		Backend: "filesystem",
		Root:    filepath.Join(tempDir, "storage"),
	})
	require.NoError(t, err)

	metadataStore, err := metadata.NewPebbleStore(metadata.PebbleOptions{
		DataDir: filepath.Join(tempDir, "metadata"),
		Logger:  logrus.StandardLogger(),
	})
	require.NoError(t, err)
	defer metadataStore.Close()

	manager := NewManager(storageBackend, metadataStore)
	ctx := context.Background()

	require.NoError(t, manager.CreateBucket(ctx, "tenant-a", "backups", "user-a"))

	require.ErrorIs(t, manager.CreateBucket(ctx, "tenant-b", "backups", "user-b"), ErrBucketAlreadyExists)
	require.ErrorIs(t, manager.CreateBucket(ctx, "", "backups", "user-c"), ErrBucketAlreadyExists)
	require.ErrorIs(t, manager.CreateBucket(ctx, "tenant-a", "backups", "user-a"), ErrBucketAlreadyExists)

	require.NoError(t, manager.CreateBucket(ctx, "tenant-b", "backups-b", "user-b"))

	found, err := metadataStore.GetBucketByName(ctx, "backups")
	require.NoError(t, err)
	require.Equal(t, "tenant-a", found.TenantID)
}
