package recovery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/require"
)

type reviewReadHookStore struct {
	metadata.Store
	beforeRead func()
}

func (s *reviewReadHookStore) GetObject(ctx context.Context, bucket, key string, versions ...string) (*metadata.ObjectMetadata, error) {
	if s.beforeRead != nil {
		hook := s.beforeRead
		s.beforeRead = nil
		hook()
	}
	return s.Store.GetObject(ctx, bucket, key, versions...)
}

func TestReviewReconcileDoesNotOverwriteConcurrentPut(t *testing.T) {
	root, store, cleanup := setupReconcileTest(t)
	defer cleanup()
	writeObjectPair(t, root, "key", "old", 1700000000)
	require.NoError(t, store.PutObject(t.Context(), &metadata.ObjectMetadata{
		Bucket: "bkt", Key: "key", Size: 3, ETag: "old-etag", LastModified: time.Unix(1700000000, 0),
	}))
	wrapped := &reviewReadHookStore{Store: store, beforeRead: func() {
		writeObjectWithSidecar(t, root, "key", "new body", map[string]string{
			"size": "8", "etag": "new-etag", "last_modified": "1700000010",
		})
		require.NoError(t, store.PutObject(t.Context(), &metadata.ObjectMetadata{
			Bucket: "bkt", Key: "key", Size: 8, ETag: "new-etag", LastModified: time.Unix(1700000010, 0),
		}))
	}}
	_, err := Reconcile(t.Context(), root, wrapped, nil)
	require.NoError(t, err)
	entry, err := store.GetObject(t.Context(), "bkt", "key")
	require.NoError(t, err)
	require.Equal(t, int64(8), entry.Size, "a stale sidecar snapshot must not overwrite a newer PUT")
	require.Equal(t, "new-etag", entry.ETag)
}

func TestReviewReconcileEqualLengthWithinSameSecond(t *testing.T) {
	root, store, cleanup := setupReconcileTest(t)
	defer cleanup()
	writeObjectPair(t, root, "key", "new", 1700000000)
	require.NoError(t, store.PutObject(t.Context(), &metadata.ObjectMetadata{
		Bucket: "bkt", Key: "key", Size: 3, ETag: "old-etag", LastModified: time.Unix(1700000000, 0),
	}))
	_, err := Reconcile(t.Context(), root, store, nil)
	require.NoError(t, err)
	entry, err := store.GetObject(t.Context(), "bkt", "key")
	require.NoError(t, err)
	require.Equal(t, "test-etag", entry.ETag, "same-second overwrite must not keep the old ETag")
}

func TestReconcileDoesNotInventKeyWithoutSidecar(t *testing.T) {
	for _, staged := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "staged"}[staged], func(t *testing.T) {
			root, store, cleanup := setupReconcileTest(t)
			defer cleanup()
			objectsRoot := filepath.Join(root, "objects")
			require.NoError(t, storage.WriteLayoutVersion(objectsRoot, storage.LayoutVersion))
			b, err := storage.NewFilesystemBackend(storage.Config{Root: objectsRoot})
			require.NoError(t, err)
			require.NoError(t, b.CreateBucket(t.Context(), "bkt"))
			ref := storage.ObjectRef{Bucket: "bkt", Key: "real-key"}
			require.NoError(t, b.Put(t.Context(), ref, strings.NewReader("body"), nil))
			path := filepath.Join(objectsRoot, filepath.FromSlash(storage.RefPath(ref)))
			if staged {
				require.NoError(t, os.Rename(path+".metadata", path+".metadata-staging"))
			} else {
				require.NoError(t, os.Remove(path+".metadata"))
			}
			_, err = Reconcile(t.Context(), root, store, nil)
			require.NoError(t, err)
			entries, _, err := store.ListObjects(t.Context(), "bkt", "", "", 100)
			require.NoError(t, err)
			require.Empty(t, entries, "a physical digest is not an S3 key")
			require.FileExists(t, path)
			if staged {
				_, err := b.GetMetadata(t.Context(), ref)
				require.NoError(t, err)
				_, err = Reconcile(t.Context(), root, store, nil)
				require.NoError(t, err)
				entries, _, err = store.ListObjects(t.Context(), "bkt", "", "", 100)
				require.NoError(t, err)
				require.Len(t, entries, 1)
				require.Equal(t, ref.Key, entries[0].Key)
			}
		})
	}
}
