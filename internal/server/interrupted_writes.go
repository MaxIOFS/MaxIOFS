package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/maxiofs/maxiofs/internal/rollback"
	"github.com/sirupsen/logrus"
)

// removeUploadLeftovers removes the temporary files of the uploads a previous run
// was cut short in the middle of: no entry names them, and nothing is writing yet.
func (s *Server) removeUploadLeftovers() {
	entries, err := os.ReadDir(s.config.Storage.Root)
	if err != nil {
		logrus.WithError(err).Warn("Failed to read the storage root for upload leftovers")
		return
	}
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "maxiofs-upload-") {
			continue
		}
		if err := os.Remove(filepath.Join(s.config.Storage.Root, entry.Name())); err != nil {
			logrus.WithError(err).WithField("file", entry.Name()).Warn("Failed to remove an upload leftover")
			continue
		}
		removed++
	}
	if removed > 0 {
		logrus.WithField("files", removed).Info("Removed the temporary files of interrupted uploads")
	}
}

// undoInterruptedWrites puts back every in-place write that was cut short
// between publishing its bytes and committing its index entry.
//
// It runs before the server serves anything and before any background pass
// starts: it writes over live object paths, and reconcile must see the settled
// state rather than race it.
func (s *Server) undoInterruptedWrites(ctx context.Context) error {
	report, err := rollback.Undo(ctx, s.config.Storage.Root, s.storageBackend, s.metadataStore, logrus.StandardLogger())
	if err != nil {
		return fmt.Errorf("interrupted-write rollback: %w", err)
	}
	if report == nil {
		return nil
	}
	for _, failure := range report.Failures {
		logrus.WithField("detail", failure).Warn("Interrupted-write rollback failure")
	}
	if len(report.Failures) > 0 {
		return fmt.Errorf("interrupted-write rollback has %d failures; retained copies require review", len(report.Failures))
	}
	if !report.Changed() && report.Discarded == 0 && report.Committed == 0 && report.Retained == 0 {
		return nil
	}
	logrus.WithFields(logrus.Fields{
		"objects_restored":  report.ObjectsRestored,
		"parts_restored":    report.PartsRestored,
		"already_committed": report.Committed,
		"discarded":         report.Discarded,
		"retained":          report.Retained,
		"failures":          len(report.Failures),
	}).Warn("Interrupted-write recovery finished")
	return nil
}
