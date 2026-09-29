package bucket

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
)

// createMu serializes the creation of buckets in this process: the directory
// an earlier bucket of the same path left behind is removed before the new
// bucket exists, never after it holds objects.
var createMu sync.Mutex

// CreateEntry adds meta to the index as a new bucket. When an earlier bucket
// at the same path was deleted and its directory outlived the deletion, the
// directory is removed first; if it cannot be, the bucket is not created.
func CreateEntry(ctx context.Context, store metadata.Store, backend storage.Backend, meta *metadata.BucketMetadata) error {
	createMu.Lock()
	defer createMu.Unlock()

	if _, err := store.GetBucketByName(ctx, meta.Name); err == nil {
		return metadata.ErrBucketAlreadyExists
	} else if !errors.Is(err, metadata.ErrBucketNotFound) {
		return err
	}
	if err := finishPendingRemoval(ctx, store, backend, entryPath(meta)); err != nil {
		return err
	}
	return store.CreateBucket(ctx, meta)
}

// replicaStore stores a bucket as another node of the cluster holds it.
type replicaStore interface {
	ApplyBucketReplica(ctx context.Context, bucket *metadata.BucketMetadata) (bool, error)
}

// ApplyReplicaEntry stores meta as another node of the cluster holds it, when
// it is newer than this node's copy, and reports whether it was stored and
// whether the bucket is new here. A new bucket gets its directory, after the
// removal of an earlier bucket at the same path, as CreateEntry does.
func ApplyReplicaEntry(ctx context.Context, store metadata.Store, backend storage.Backend, meta *metadata.BucketMetadata) (applied, created bool, err error) {
	rs, ok := store.(replicaStore)
	if !ok {
		return false, false, fmt.Errorf("the metadata store cannot hold replicated buckets")
	}
	createMu.Lock()
	defer createMu.Unlock()

	if _, err := store.GetBucket(ctx, meta.TenantID, meta.Name); err == nil {
		applied, err := rs.ApplyBucketReplica(ctx, meta)
		return applied, false, err
	} else if !errors.Is(err, metadata.ErrBucketNotFound) {
		return false, false, err
	}

	path := entryPath(meta)
	if err := finishPendingRemoval(ctx, store, backend, path); err != nil {
		return false, false, err
	}
	if applied, err := rs.ApplyBucketReplica(ctx, meta); err != nil || !applied {
		return applied, false, err
	}
	if err := backend.CreateBucket(ctx, path); err != nil {
		if delErr := store.DeleteBucket(ctx, meta.TenantID, meta.Name); delErr != nil && !errors.Is(delErr, metadata.ErrBucketNotFound) {
			return false, false, errors.Join(err, delErr)
		}
		return false, false, err
	}
	return true, true, nil
}

// finishPendingRemoval removes the directory an earlier bucket at path left
// behind when its deletion did not finish.
func finishPendingRemoval(ctx context.Context, store metadata.Store, backend storage.Backend, path string) error {
	pending, err := store.PendingBucketRemovals(ctx)
	if err != nil {
		return err
	}
	if !slices.Contains(pending, path) {
		return nil
	}
	if err := backend.DeleteBucket(ctx, path); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
		return fmt.Errorf("the directory of an earlier bucket %s could not be removed: %w", path, err)
	}
	return store.ClearBucketRemoval(ctx, path)
}

func entryPath(meta *metadata.BucketMetadata) string {
	if meta.TenantID == "" {
		return meta.Name
	}
	return meta.TenantID + "/" + meta.Name
}
