package server

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

func bucketAlertKey(tenantID, bucketName string) string {
	return tenantID + "/" + bucketName
}

// checkBucketQuotaAlert is invoked after a bucket's cached size grows: it
// alerts on the bucket's quota when its level changes.
func (s *Server) checkBucketQuotaAlert(tenantID, bucketName string, currentBytes, maxBytes int64) {
	if maxBytes == 0 {
		return // unlimited
	}
	warnPct, critPct := s.alertThresholds()
	usedPct := float64(currentBytes) / float64(maxBytes) * 100.0
	newLevel := alertLevelOf(usedPct, warnPct, critPct)
	prev := s.bucketQuotaAlerts.change(bucketAlertKey(tenantID, bucketName), newLevel)

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
	if newLevel <= prev {
		return
	}

	kind, title, shout := alertWords(newLevel)
	subject := fmt.Sprintf("[MaxIOFS] %s: Bucket Quota Alert — %s", title, bucketName)
	logMsg := fmt.Sprintf("%s: bucket %q quota at %.1f%% (%.2f / %.2f GB)",
		shout, bucketName, usedPct, float64(currentBytes)/1e9, float64(maxBytes)/1e9)

	logrus.WithFields(logrus.Fields{
		"bucket":        bucketName,
		"tenant_id":     tenantID,
		"used_pct":      usedPct,
		"current_bytes": currentBytes,
		"max_bytes":     maxBytes,
	}).Warn("Bucket quota alert triggered")

	s.notificationHub.SendNotification(&Notification{
		Type:    "bucket_quota_" + kind,
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
		float64(max(maxBytes-currentBytes, 0))/1e9,
		bucketName,
	)
	s.emailAdmins(tenantID, subject, body, "Bucket quota alert", logrus.Fields{"bucket": bucketName, "tenant_id": tenantID})
}
