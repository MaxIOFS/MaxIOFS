package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// bucketQuotaAlertTracker holds per-bucket alert deduplication state so that an
// SSE/email alert fires only when the alert level escalates, not on every write.
type bucketQuotaAlertTracker struct {
	levels sync.Map // "tenantID/bucketName" -> alertLevel
}

func newBucketQuotaAlertTracker() *bucketQuotaAlertTracker {
	return &bucketQuotaAlertTracker{}
}

func bucketAlertKey(tenantID, bucketName string) string {
	return tenantID + "/" + bucketName
}

// checkBucketQuotaAlert is invoked after a bucket's cached size grows. It fires
// SSE + email only when the alert level escalates (none→warning, warning→critical),
// and emits a "resolved" event when usage falls back to normal. Mirrors the
// tenant-level checkQuotaAlert but is keyed per bucket and works for global
// buckets (empty tenantID) too.
func (s *Server) checkBucketQuotaAlert(tenantID, bucketName string, currentBytes, maxBytes int64) {
	if maxBytes == 0 {
		return // unlimited
	}

	warnPct := 80
	critPct := 90
	if v, err := s.settingsManager.GetInt("system.disk_warning_threshold"); err == nil && v > 0 {
		warnPct = v
	}
	if v, err := s.settingsManager.GetInt("system.disk_critical_threshold"); err == nil && v > 0 {
		critPct = v
	}

	usedPct := float64(currentBytes) / float64(maxBytes) * 100.0

	var newLevel alertLevel
	switch {
	case usedPct >= float64(critPct):
		newLevel = alertLevelCritical
	case usedPct >= float64(warnPct):
		newLevel = alertLevelWarning
	default:
		newLevel = alertLevelNone
	}

	key := bucketAlertKey(tenantID, bucketName)
	prevRaw, _ := s.bucketQuotaAlerts.levels.Load(key)
	prev, _ := prevRaw.(alertLevel)
	s.bucketQuotaAlerts.levels.Store(key, newLevel)

	// Condition resolved: was alerting, now back to normal.
	if newLevel == alertLevelNone && prev != alertLevelNone {
		s.notificationHub.SendNotification(&Notification{
			Type:    "bucket_quota_resolved",
			Message: fmt.Sprintf("Bucket %q storage quota is back to normal (%.1f%% used)", bucketName, usedPct),
			Data: map[string]interface{}{
				"bucket":       bucketName,
				"tenantId":     tenantID,
				"usedPercent":  usedPct,
				"currentBytes": currentBytes,
				"maxBytes":     maxBytes,
			},
			Timestamp: time.Now().Unix(),
			TenantID:  tenantID,
		})
		return
	}

	// Only act on escalation.
	if newLevel <= prev {
		return
	}

	var notifType, subject, logMsg string
	if newLevel == alertLevelCritical {
		notifType = "bucket_quota_critical"
		subject = fmt.Sprintf("[MaxIOFS] CRITICAL: Bucket Quota Alert — %s", bucketName)
		logMsg = fmt.Sprintf("CRITICAL: bucket %q quota at %.1f%% (%.2f / %.2f GB)",
			bucketName, usedPct, float64(currentBytes)/1e9, float64(maxBytes)/1e9)
	} else {
		notifType = "bucket_quota_warning"
		subject = fmt.Sprintf("[MaxIOFS] Warning: Bucket Quota Alert — %s", bucketName)
		logMsg = fmt.Sprintf("WARNING: bucket %q quota at %.1f%% (%.2f / %.2f GB)",
			bucketName, usedPct, float64(currentBytes)/1e9, float64(maxBytes)/1e9)
	}

	logrus.WithFields(logrus.Fields{
		"bucket":        bucketName,
		"tenant_id":     tenantID,
		"used_pct":      usedPct,
		"current_bytes": currentBytes,
		"max_bytes":     maxBytes,
	}).Warn("Bucket quota alert triggered")

	s.notificationHub.SendNotification(&Notification{
		Type:    notifType,
		Message: logMsg,
		Data: map[string]interface{}{
			"bucket":       bucketName,
			"tenantId":     tenantID,
			"usedPercent":  usedPct,
			"currentBytes": currentBytes,
			"maxBytes":     maxBytes,
			"warnAt":       warnPct,
			"criticalAt":   critPct,
		},
		Timestamp: time.Now().Unix(),
		TenantID:  tenantID,
	})

	s.sendBucketQuotaAlertEmail(subject, logMsg, tenantID, bucketName, usedPct, currentBytes, maxBytes)
}

// sendBucketQuotaAlertEmail notifies global admins (and the bucket's tenant admins
// when the bucket belongs to a tenant) that a bucket is approaching its quota.
func (s *Server) sendBucketQuotaAlertEmail(subject, alertMsg, tenantID, bucketName string, usedPct float64, currentBytes, maxBytes int64) {
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
		logrus.WithError(err).Error("Bucket quota alert: failed to list users for email")
		return
	}

	var recipients []string
	seen := map[string]bool{}
	for _, u := range users {
		if u.Status != "active" || u.Email == "" {
			continue
		}
		isGlobalAdmin := false
		isTenantAdmin := false
		for _, role := range u.Roles {
			if role == "admin" && u.TenantID == "" {
				isGlobalAdmin = true
			}
			if role == "admin" && tenantID != "" && u.TenantID == tenantID {
				isTenantAdmin = true
			}
		}
		if (isGlobalAdmin || isTenantAdmin) && !seen[u.Email] {
			recipients = append(recipients, u.Email)
			seen[u.Email] = true
		}
	}

	if len(recipients) == 0 {
		return
	}

	freeBytes := maxBytes - currentBytes
	if freeBytes < 0 {
		freeBytes = 0
	}

	body := fmt.Sprintf(`MaxIOFS Bucket Quota Alert
===========================

Bucket: %s
%s

Storage details:
  Used:  %.2f GB  (%.1f%%)
  Total: %.2f GB
  Free:  %.2f GB

Please review the bucket's usage or increase its quota limit.
Go to Console → Buckets → %s → Settings → Quota to manage the limit.

---
This alert is sent automatically when a bucket's quota crosses configured thresholds.
To adjust thresholds, go to System Settings > System > Disk Warning/Critical Threshold.
`,
		bucketName,
		alertMsg,
		float64(currentBytes)/1e9,
		usedPct,
		float64(maxBytes)/1e9,
		float64(freeBytes)/1e9,
		bucketName,
	)

	if err := sender.Send(recipients, subject, body); err != nil {
		logrus.WithError(err).Error("Failed to send bucket quota alert email")
		return
	}
	logrus.WithFields(logrus.Fields{
		"bucket":     bucketName,
		"tenant_id":  tenantID,
		"recipients": len(recipients),
	}).Info("Bucket quota alert email sent")
}
