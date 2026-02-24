package server

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

const (
	// maxStoredIssues caps the number of issue records per scan entry.
	maxStoredIssues = 500

	// maxScanHistory is the maximum number of scan records kept per bucket.
	maxScanHistory = 10

	// minManualScanInterval is the minimum time that must elapse between two
	// consecutive manual integrity scans for the same bucket.
	minManualScanInterval = time.Hour
)

// LastScanRecord is a single integrity scan result for a bucket.
// It is stored in a history array (newest first) so the frontend can display
// both the current state and recent scan history.
type LastScanRecord struct {
	BucketPath string                    `json:"bucketPath"`
	ScannedAt  time.Time                 `json:"scannedAt"`
	Duration   string                    `json:"duration"`
	Checked    int                       `json:"checked"`
	OK         int                       `json:"ok"`
	Corrupted  int                       `json:"corrupted"`
	Skipped    int                       `json:"skipped"`
	Errors     int                       `json:"errors"`
	Issues     []*object.IntegrityResult `json:"issues,omitempty"`
	// Source is "manual" when triggered via the API, "scrubber" when automatic.
	Source string `json:"source"`
}

func integrityScanKey(bucketPath string) string {
	return "integrity_scans:" + bucketPath
}

// saveIntegrityResult prepends a new scan record to the bucket's history and
// persists it.  The history is capped at maxScanHistory entries (newest first).
func (s *Server) saveIntegrityResult(ctx context.Context, bucketPath string, report *object.BucketIntegrityReport, source string) {
	kvStore, ok := s.metadataStore.(metadata.RawKVStore)
	if !ok {
		return
	}

	issues := report.Issues
	if len(issues) > maxStoredIssues {
		issues = issues[:maxStoredIssues]
	}

	rec := &LastScanRecord{
		BucketPath: bucketPath,
		ScannedAt:  time.Now(),
		Duration:   report.Duration,
		Checked:    report.Checked,
		OK:         report.OK,
		Corrupted:  report.Corrupted,
		Skipped:    report.Skipped,
		Errors:     report.Errors,
		Issues:     issues,
		Source:     source,
	}

	// Load existing history, prepend the new record, cap at maxScanHistory.
	existing, _ := s.getIntegrityHistory(ctx, bucketPath)
	history := append([]*LastScanRecord{rec}, existing...)
	if len(history) > maxScanHistory {
		history = history[:maxScanHistory]
	}

	data, err := json.Marshal(history)
	if err != nil {
		logrus.WithError(err).Error("integrity: failed to marshal scan history")
		return
	}

	if err := kvStore.PutRaw(ctx, integrityScanKey(bucketPath), data); err != nil {
		logrus.WithError(err).WithField("bucket", bucketPath).Error("integrity: failed to save scan history")
	}
}

// getIntegrityHistory retrieves the stored scan history for a bucket (newest
// first).  Returns nil, nil if no scan has ever been recorded.
// It is backward-compatible: if the stored value is a single object (old
// format) it is wrapped in a one-element slice.
func (s *Server) getIntegrityHistory(ctx context.Context, bucketPath string) ([]*LastScanRecord, error) {
	kvStore, ok := s.metadataStore.(metadata.RawKVStore)
	if !ok {
		return nil, nil
	}

	data, err := kvStore.GetRaw(ctx, integrityScanKey(bucketPath))
	if err != nil {
		if errors.Is(err, metadata.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}

	// Try new format: JSON array.
	var records []*LastScanRecord
	if err := json.Unmarshal(data, &records); err == nil {
		return records, nil
	}

	// Fallback: old format was a single object stored under the old key name.
	var single LastScanRecord
	if err := json.Unmarshal(data, &single); err == nil {
		return []*LastScanRecord{&single}, nil
	}

	return nil, nil
}

// lastManualScanTime returns the ScannedAt time of the most recent manual scan
// for the bucket.  Returns zero time if no manual scan has ever been recorded.
func (s *Server) lastManualScanTime(ctx context.Context, bucketPath string) time.Time {
	history, _ := s.getIntegrityHistory(ctx, bucketPath)
	for _, rec := range history {
		if rec.Source == "manual" {
			return rec.ScannedAt
		}
	}
	return time.Time{}
}
