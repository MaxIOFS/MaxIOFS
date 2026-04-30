package metadata

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupStoreVariants(t *testing.T) map[string]Store {
	t.Helper()

	stores := make(map[string]Store)

	pebbleDir, err := os.MkdirTemp("", "metadata-pebble-consistency-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(pebbleDir) })

	pebbleStore, err := NewPebbleStore(PebbleOptions{
		DataDir: pebbleDir,
		Logger:  logrus.New(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = pebbleStore.Close() })
	stores["pebble"] = pebbleStore

	badgerDir, err := os.MkdirTemp("", "metadata-badger-consistency-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(badgerDir) })

	badgerStore, err := NewBadgerStore(BadgerOptions{
		DataDir: badgerDir,
		Logger:  logrus.New(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = badgerStore.Close() })
	stores["badger"] = badgerStore

	return stores
}

func TestPutObjectOverwriteRemovesStaleTagIndexes(t *testing.T) {
	ctx := context.Background()

	for name, store := range setupStoreVariants(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, store.CreateBucket(ctx, &BucketMetadata{Name: "tag-overwrite-bucket"}))

			require.NoError(t, store.PutObject(ctx, &ObjectMetadata{
				Bucket: "tag-overwrite-bucket",
				Key:    "object.txt",
				Size:   10,
				ETag:   "etag-1",
				Tags:   map[string]string{"state": "old"},
			}))
			require.NoError(t, store.PutObject(ctx, &ObjectMetadata{
				Bucket: "tag-overwrite-bucket",
				Key:    "object.txt",
				Size:   20,
				ETag:   "etag-2",
				Tags:   map[string]string{"state": "new"},
			}))

			oldMatches, err := store.ListObjectsByTags(ctx, "tag-overwrite-bucket", map[string]string{"state": "old"})
			require.NoError(t, err)
			assert.Empty(t, oldMatches)

			newMatches, err := store.ListObjectsByTags(ctx, "tag-overwrite-bucket", map[string]string{"state": "new"})
			require.NoError(t, err)
			require.Len(t, newMatches, 1)
			assert.Equal(t, "object.txt", newMatches[0].Key)
		})
	}
}

func TestObjectVersionPrefixDoesNotLeakColonPrefixedKeys(t *testing.T) {
	ctx := context.Background()

	for name, store := range setupStoreVariants(t) {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, store.CreateBucket(ctx, &BucketMetadata{Name: "version-prefix-bucket"}))

			now := time.Now()
			require.NoError(t, store.PutObjectVersion(ctx, &ObjectMetadata{
				Bucket:       "version-prefix-bucket",
				Key:          "a",
				Size:         1,
				ETag:         "etag-a",
				LastModified: now,
				Retention: &RetentionMetadata{
					Mode:            "COMPLIANCE",
					RetainUntilDate: now.Add(time.Hour),
				},
			}, &ObjectVersion{
				VersionID:    "v1",
				IsLatest:     true,
				Key:          "a",
				Size:         1,
				ETag:         "etag-a",
				LastModified: now,
			}))
			require.NoError(t, store.PutObjectVersion(ctx, &ObjectMetadata{
				Bucket:       "version-prefix-bucket",
				Key:          "a:b",
				Size:         2,
				ETag:         "etag-ab",
				LastModified: now.Add(time.Second),
			}, &ObjectVersion{
				VersionID:    "v2",
				IsLatest:     true,
				Key:          "a:b",
				Size:         2,
				ETag:         "etag-ab",
				LastModified: now.Add(time.Second),
			}))

			versions, err := store.GetObjectVersions(ctx, "version-prefix-bucket", "a")
			require.NoError(t, err)
			require.Len(t, versions, 1)
			assert.Equal(t, "a", versions[0].Key)
			assert.Equal(t, "v1", versions[0].VersionID)

			versionMeta, err := store.GetObject(ctx, "version-prefix-bucket", "a", "v1")
			require.NoError(t, err)
			require.NotNil(t, versionMeta.Retention)
			assert.Equal(t, "COMPLIANCE", versionMeta.Retention.Mode)
		})
	}
}
