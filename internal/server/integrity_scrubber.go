package server

import (
	"context"
	"fmt"
	"time"

	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// startIntegrityScrubber launches a background goroutine that runs a full
// integrity scan every 24 hours.  It does NOT run immediately on startup —
// the first scan fires after the initial 24-hour tick.
func (s *Server) startIntegrityScrubber(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runIntegrityScrub(ctx)
			}
		}
	}()
}

// runIntegrityScrub iterates over every bucket → object page and calls
// VerifyBucketIntegrity.  Corrupted / missing objects trigger an audit event,
// an SSE notification and an email to all global admins.
func (s *Server) runIntegrityScrub(ctx context.Context) {
	logrus.Info("Integrity scrubber: starting full scan")
	started := time.Now()

	// ListBuckets("") returns every bucket across all tenants in one call.
	// Each BucketMetadata carries its own TenantID field.
	allBuckets, err := s.metadataStore.ListBuckets(ctx, "")
	if err != nil {
		logrus.WithError(err).Error("Integrity scrubber: failed to list buckets")
		return
	}

	totalCorrupted := 0
	totalChecked := 0

	for _, bkt := range allBuckets {
		// Objects are stored under "tenantID/bucketName" for tenant buckets
		// and just "bucketName" for global buckets.
		tenantID := bkt.TenantID
		bucketPath := bkt.Name
		if tenantID != "" {
			bucketPath = tenantID + "/" + bkt.Name
		}

		// Per-bucket accumulators — used to persist the full-bucket result.
		var bucketChecked, bucketOK, bucketCorrupted, bucketSkipped, bucketErrors int
		var bucketIssues []*object.IntegrityResult
		bucketStart := time.Now()

		marker := ""
		for {
			report, err := s.objectManager.(interface {
				VerifyBucketIntegrity(ctx context.Context, bucket, prefix, marker string, maxKeys int) (*object.BucketIntegrityReport, error)
			}).VerifyBucketIntegrity(ctx, bucketPath, "", marker, 500)
			if err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"tenant": tenantID,
					"bucket": bkt.Name,
				}).Error("Integrity scrubber: verification failed")
				break
			}

			bucketChecked += report.Checked
			bucketOK += report.OK
			bucketCorrupted += report.Corrupted
			bucketSkipped += report.Skipped
			bucketErrors += report.Errors
			totalChecked += report.Checked

			// Accumulate all issues for persistence (capped later by saveIntegrityResult).
			if len(bucketIssues) < maxStoredIssues {
				remaining := maxStoredIssues - len(bucketIssues)
				toAdd := report.Issues
				if len(toAdd) > remaining {
					toAdd = toAdd[:remaining]
				}
				bucketIssues = append(bucketIssues, toAdd...)
			}

			for _, issue := range report.Issues {
				if issue.Status != object.IntegrityCorrupted && issue.Status != object.IntegrityMissing {
					continue
				}

				totalCorrupted++

				logrus.WithFields(logrus.Fields{
					"bucket":       bkt.Name,
					"key":          issue.Key,
					"status":       issue.Status,
					"storedETag":   issue.StoredETag,
					"computedETag": issue.ComputedETag,
				}).Error("Integrity scrubber: data corruption detected")

				// Audit event
				_ = s.auditManager.LogEvent(ctx, &audit.AuditEvent{
					TenantID:     tenantID,
					EventType:    audit.EventTypeDataCorruption,
					ResourceType: audit.ResourceTypeSystem,
					ResourceID:   fmt.Sprintf("%s/%s", bkt.Name, issue.Key),
					ResourceName: issue.Key,
					Action:       audit.ActionVerifyIntegrity,
					Status:       audit.StatusFailed,
					Details: map[string]interface{}{
						"bucket":       bkt.Name,
						"key":          issue.Key,
						"status":       string(issue.Status),
						"storedETag":   issue.StoredETag,
						"computedETag": issue.ComputedETag,
						"expectedSize": issue.ExpectedSize,
						"actualSize":   issue.ActualSize,
					},
				})

				// SSE notification
				s.notificationHub.SendNotification(&Notification{
					Type:    "data_corruption",
					Message: fmt.Sprintf("Data corruption detected in %s/%s", bkt.Name, issue.Key),
					Data: map[string]interface{}{
						"bucket":       bkt.Name,
						"key":          issue.Key,
						"status":       string(issue.Status),
						"storedETag":   issue.StoredETag,
						"computedETag": issue.ComputedETag,
					},
					Timestamp: time.Now().Unix(),
					TenantID:  tenantID,
				})

				// Email alert
				s.sendCorruptionAlertEmail(bkt.Name, issue.Key, issue.StoredETag, issue.ComputedETag)
			}

			// Throttle slightly to avoid saturating IO
			time.Sleep(10 * time.Millisecond)

			if report.NextMarker == "" {
				break
			}
			marker = report.NextMarker
		}

		// Persist the accumulated bucket result so the UI can display it at any time.
		s.saveIntegrityResult(ctx, bucketPath, &object.BucketIntegrityReport{
			Bucket:    bkt.Name,
			Duration:  time.Since(bucketStart).String(),
			Checked:   bucketChecked,
			OK:        bucketOK,
			Corrupted: bucketCorrupted,
			Skipped:   bucketSkipped,
			Errors:    bucketErrors,
			Issues:    bucketIssues,
		}, "scrubber")
	}

	logrus.WithFields(logrus.Fields{
		"duration":  time.Since(started).String(),
		"checked":   totalChecked,
		"corrupted": totalCorrupted,
	}).Info("Integrity scrubber: scan complete")
}

// sendCorruptionAlertEmail sends an email to all active global admins
// notifying them of a corrupted or missing object.
func (s *Server) sendCorruptionAlertEmail(bucket, key, storedETag, computedETag string) {
	enabled, _ := s.settingsManager.GetBool("email.enabled")
	if !enabled {
		return
	}

	sender := s.buildEmailSender()
	if sender == nil || !sender.IsConfigured() {
		return
	}

	users, err := s.authManager.ListUsers(context.Background())
	if err != nil {
		logrus.WithError(err).Error("Corruption alert: failed to list users for email")
		return
	}

	var recipients []string
	seen := map[string]bool{}
	for _, u := range users {
		if u.Status != "active" || u.Email == "" {
			continue
		}
		for _, role := range u.Roles {
			if role == "admin" {
				if !seen[u.Email] {
					recipients = append(recipients, u.Email)
					seen[u.Email] = true
				}
				break
			}
		}
	}

	if len(recipients) == 0 {
		logrus.Debug("Corruption alert: no admin emails configured, skipping email")
		return
	}

	subject := "[MaxIOFS] ALERT: Data Corruption Detected"

	var detail string
	if computedETag == "" {
		detail = fmt.Sprintf("  Status:    missing (object data not found on storage)\n  Bucket:    %s\n  Key:       %s\n  Stored ETag: %s", bucket, key, storedETag)
	} else {
		detail = fmt.Sprintf("  Status:      corrupted (ETag mismatch)\n  Bucket:      %s\n  Key:         %s\n  Stored ETag: %s\n  Actual ETag: %s", bucket, key, storedETag, computedETag)
	}

	body := fmt.Sprintf(`MaxIOFS Data Integrity Alert
============================

The background integrity scrubber has detected a problem with stored data.

%s

Recommended actions:
  1. Investigate the affected object immediately.
  2. Restore from backup if available.
  3. Check disk health (S.M.A.R.T.) and filesystem integrity.

---
This alert is sent automatically by MaxIOFS when data corruption is detected during periodic integrity scans.
`, detail)

	if err := sender.Send(recipients, subject, body); err != nil {
		logrus.WithError(err).Error("Failed to send corruption alert email")
		return
	}
	logrus.WithFields(logrus.Fields{
		"bucket":     bucket,
		"key":        key,
		"recipients": len(recipients),
	}).Info("Corruption alert email sent")
}
