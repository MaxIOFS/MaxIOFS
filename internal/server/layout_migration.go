package server

import (
	"context"
	"fmt"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/layout"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
)

func migrateStorageLayout(cfg *config.Config, store metadata.Store) error {
	report, err := layout.Migrate(context.Background(), layout.Options{
		Root:   cfg.Storage.Root,
		Store:  store,
		Logger: logrus.StandardLogger(),
	})
	if err != nil {
		return fmt.Errorf("storage layout migration failed: %w", err)
	}
	if report.AlreadyCurrent {
		if report.FoldersPurged > 0 {
			logrus.WithField("folders", report.FoldersPurged).
				Info("Removed folder objects earlier releases invented for parent prefixes")
			recountBuckets(store)
		}
		return nil
	}
	recountBuckets(store)

	logrus.WithFields(logrus.Fields{
		"buckets":         report.Buckets,
		"buckets_skipped": report.BucketsSkipped,
		"objects":         report.ObjectsMoved,
		"versions":        report.VersionsMoved,
		"markers_created": report.MarkersCreated,
		"folders_purged":  report.FoldersPurged,
		"bytes":           report.BytesMoved,
	}).Info("Storage layout migrated")

	for _, entry := range report.Stranded {
		logrus.WithField("bucket", entry).Warn("Storage layout migration: bucket directory on disk is not in the metadata store — left in the old layout")
	}
	for _, entry := range report.MissingData {
		logrus.WithField("object", entry).Warn("Storage layout migration: indexed object has no data on disk")
	}
	for _, entry := range report.Damaged {
		logrus.WithField("object", entry).Warn("Storage layout migration: object could not be migrated")
	}
	for _, entry := range report.Failures {
		logrus.WithField("detail", entry).Error("Storage layout migration: failure")
	}
	return nil
}

// recountBuckets rebuilds the cached object count and size of every bucket. The
// migration writes index entries directly — it removes the invented folder
// objects and adds the marker files — so the counters the console reads are out
// by whatever it changed until something recalculates them.
func recountBuckets(store metadata.Store) {
	ctx := context.Background()
	buckets, err := store.ListBuckets(ctx, "")
	if err != nil {
		logrus.WithError(err).Warn("Could not list buckets to rebuild their counters after the layout migration")
		return
	}
	for _, b := range buckets {
		if err := store.RecalculateBucketStats(ctx, b.TenantID, b.Name); err != nil {
			logrus.WithError(err).WithField("bucket", b.Name).
				Warn("Could not rebuild the bucket counters after the layout migration")
		}
	}
	logrus.WithField("buckets", len(buckets)).Info("Bucket counters rebuilt after the layout migration")
}
