package server

import (
	"context"
	"strings"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// finishPendingBucketRemovals completes bucket deletions whose directory could
// not be removed at the time. The record of the removal is what makes this safe:
// the objects go only because someone ordered the bucket gone, never because the
// index happens not to mention them. A bucket created at the same path since
// owns the directory now: it is kept, and only the record goes.
func finishPendingBucketRemovals(ctx context.Context, backend storage.Backend, store metadata.Store) {
	pending, err := store.PendingBucketRemovals(ctx)
	if err != nil {
		logrus.WithError(err).Warn("Could not list pending bucket removals")
		return
	}

	finished := 0
	for _, bucketPath := range pending {
		tenantID, name := "", bucketPath
		if i := strings.Index(bucketPath, "/"); i >= 0 {
			tenantID, name = bucketPath[:i], bucketPath[i+1:]
		}
		_, err := store.GetBucket(ctx, tenantID, name)
		switch {
		case err == nil:
			logrus.WithField("bucket", bucketPath).
				Warn("A bucket was created where a deleted one had left its directory; keeping the directory")
		case err != metadata.ErrBucketNotFound:
			logrus.WithError(err).WithField("bucket", bucketPath).Warn("Could not check a bucket whose removal is pending")
			continue
		default:
			if err := backend.DeleteBucket(ctx, bucketPath); err != nil && err != storage.ErrObjectNotFound {
				logrus.WithError(err).WithField("bucket", bucketPath).Warn("Could not finish removing a deleted bucket")
				continue
			}
		}
		if err := store.ClearBucketRemoval(ctx, bucketPath); err != nil {
			logrus.WithError(err).WithField("bucket", bucketPath).Warn("Could not clear the bucket removal record")
			continue
		}
		finished++
	}

	if finished > 0 {
		logrus.WithField("buckets", finished).Info("Finished removing buckets whose directory outlived their deletion")
	}
}
