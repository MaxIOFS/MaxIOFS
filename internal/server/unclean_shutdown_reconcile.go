package server

import (
	"context"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/recovery"
	"github.com/sirupsen/logrus"
)

// startUncleanShutdownReconcile launches a background metadata↔disk
func (s *Server) startUncleanShutdownReconcile(ctx context.Context) {
	ps, ok := s.metadataStore.(*metadata.PebbleStore)
	if !ok || ps.WasCleanShutdown() {
		return
	}

	logrus.Warn("Unclean shutdown detected — reconciling metadata store against on-disk objects in the background")
	go func() {
		report, err := recovery.Reconcile(ctx, s.config.DataDir, s.metadataStore, logrus.StandardLogger())
		if err != nil {
			if report != nil {
				logReconcileReport(report)
			}
			logrus.WithError(err).Error("Unclean-shutdown reconciliation aborted")
			return
		}
		logReconcileReport(report)
	}()
}

func logReconcileReport(report *recovery.ReconcileReport) {
	fields := logrus.Fields{
		"buckets":           report.Buckets,
		"files_scanned":     report.FilesScanned,
		"entries_restored":  report.EntriesRestored,
		"versions_restored": report.VersionsRestored,
		"failures":          len(report.Failures),
	}
	for _, f := range report.Failures {
		logrus.WithField("detail", f).Warn("Reconcile failure")
	}
	if report.Changed() || len(report.Failures) > 0 {
		logrus.WithFields(fields).Info("Unclean-shutdown reconciliation finished — repairs applied")
	} else {
		logrus.WithFields(fields).Info("Unclean-shutdown reconciliation finished — store was consistent")
	}
}
