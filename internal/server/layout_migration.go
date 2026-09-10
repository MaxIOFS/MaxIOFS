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
		return nil
	}

	logrus.WithFields(logrus.Fields{
		"buckets":         report.Buckets,
		"buckets_skipped": report.BucketsSkipped,
		"objects":         report.ObjectsMoved,
		"versions":        report.VersionsMoved,
		"markers_created": report.MarkersCreated,
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
