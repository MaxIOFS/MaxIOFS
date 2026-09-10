package server

import (
	"context"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// finishPendingBucketRemovals completes bucket deletions whose directory could
// not be removed at the time. The record of the removal is what makes this safe:
// the objects go only because someone ordered the bucket gone, never because the
// index happens not to mention them.
func finishPendingBucketRemovals(ctx context.Context, backend storage.Backend, store metadata.Store) {
	pending, err := store.PendingBucketRemovals(ctx)
	if err != nil {
		logrus.WithError(err).Warn("Could not list pending bucket removals")
		return
	}

	finished := 0
	for _, bucketPath := range pending {
		if err := backend.DeleteBucket(ctx, bucketPath); err != nil && err != storage.ErrObjectNotFound {
			logrus.WithError(err).WithField("bucket", bucketPath).Warn("Could not finish removing a deleted bucket")
			continue
		}
		if err := store.ClearBucketRemoval(ctx, bucketPath); err != nil {
			logrus.WithError(err).WithField("bucket", bucketPath).Warn("Removed the directory but could not clear the removal record")
			continue
		}
		finished++
	}

	if finished > 0 {
		logrus.WithField("buckets", finished).Info("Finished removing buckets whose directory outlived their deletion")
	}
}
