package server

import (
	"context"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// sweepOrphanedUploads discards stored parts whose upload is not in the index.
// Every other cleanup path lists uploads from the index, so parts that outlived
// their record cannot be reached by any of them, lifecycle rules included.
func sweepOrphanedUploads(ctx context.Context, backend storage.Backend, store metadata.Store) {
	uploadIDs, err := backend.ListUploads(ctx)
	if err != nil {
		logrus.WithError(err).Warn("Could not list stored multipart uploads")
		return
	}

	removed := 0
	for _, uploadID := range uploadIDs {
		if _, err := store.GetMultipartUpload(ctx, uploadID); err != metadata.ErrUploadNotFound {
			continue // present, or the store could not answer — either way, leave it
		}
		if err := backend.DeleteUpload(ctx, uploadID); err != nil {
			logrus.WithError(err).WithField("uploadID", uploadID).Warn("Could not discard an orphaned multipart upload")
			continue
		}
		removed++
	}

	if removed > 0 {
		logrus.WithField("uploads", removed).Info("Discarded multipart uploads that are no longer in the metadata store")
	}
}
