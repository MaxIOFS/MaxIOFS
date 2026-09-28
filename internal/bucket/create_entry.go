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
	path := meta.Name
	if meta.TenantID != "" {
		path = meta.TenantID + "/" + meta.Name
	}
	pending, err := store.PendingBucketRemovals(ctx)
	if err != nil {
		return err
	}
	if slices.Contains(pending, path) {
		if err := backend.DeleteBucket(ctx, path); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
			return fmt.Errorf("the directory of an earlier bucket %s could not be removed: %w", path, err)
		}
		if err := store.ClearBucketRemoval(ctx, path); err != nil {
			return err
		}
	}
	return store.CreateBucket(ctx, meta)
}
