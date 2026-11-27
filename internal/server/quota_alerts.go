package server

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// checkQuotaAlert alerts on a tenant's storage when its level changes.
func (s *Server) checkQuotaAlert(tenantID string, currentBytes, maxBytes int64) {
	if maxBytes == 0 {
		return // unlimited quota
	}
	warnPct, critPct := s.alertThresholds()
	usedPct := float64(currentBytes) / float64(maxBytes) * 100.0
	newLevel := alertLevelOf(usedPct, warnPct, critPct)
	prev := s.quotaAlerts.change(tenantID, newLevel)
	resolved := newLevel == alertLevelNone && prev != alertLevelNone
	if !resolved && newLevel <= prev {
		return
	}
	tenantName := s.tenantDisplayName(tenantID)

	if resolved {
		s.notificationHub.SendNotification(&Notification{
			Type:    "quota_resolved",
			Message: fmt.Sprintf("Tenant %q storage quota is back to normal (%.1f%% used)", tenantName, usedPct),
			Data: map[string]interface{}{
				"tenantId":     tenantID,
				"tenantName":   tenantName,
				"usedPercent":  usedPct,
				"currentBytes": currentBytes,
				"maxBytes":     maxBytes,
			},
			Timestamp: time.Now().Unix(),
			TenantID:  tenantID,
		})
		return
	}

	kind, title, shout := alertWords(newLevel)
	subject := fmt.Sprintf("[MaxIOFS] %s: Storage Quota Alert — %s", title, tenantName)
	logMsg := fmt.Sprintf("%s: tenant %q quota at %.1f%% (%.2f / %.2f GB)",
		shout, tenantName, usedPct, float64(currentBytes)/1e9, float64(maxBytes)/1e9)

	logrus.WithFields(logrus.Fields{
		"tenant_id":     tenantID,
		"tenant_name":   tenantName,
		"used_pct":      usedPct,
		"current_bytes": currentBytes,
		"max_bytes":     maxBytes,
	}).Warn("Tenant quota alert triggered")

	// SSE notification — TenantID set so tenant admins also receive it
	s.notificationHub.SendNotification(&Notification{
		Type:    "quota_" + kind,
		Message: logMsg,
		Data: map[string]interface{}{
			"tenantId":     tenantID,
			"tenantName":   tenantName,
			"usedPercent":  usedPct,
			"currentBytes": currentBytes,
			"maxBytes":     maxBytes,
			"warnAt":       warnPct,
			"criticalAt":   critPct,
		},
		Timestamp: time.Now().Unix(),
		TenantID:  tenantID,
	})

	s.sendQuotaAlertEmail(subject, logMsg, tenantID, tenantName, usedPct, currentBytes, maxBytes)
}

// tenantDisplayName is the name a tenant is shown by: its display name, else
// its name, else its ID.
func (s *Server) tenantDisplayName(tenantID string) string {
	tenant, err := s.authManager.GetTenant(context.Background(), tenantID)
	switch {
	case err != nil || tenant == nil:
		return tenantID
	case tenant.DisplayName != "":
		return tenant.DisplayName
	}
	return tenant.Name
}

func (s *Server) sendQuotaAlertEmail(subject, alertMsg, tenantID, tenantName string, usedPct float64, currentBytes, maxBytes int64) {
	body := fmt.Sprintf(`MaxIOFS Storage Quota Alert
============================

Tenant: %s
%s

Storage details:
  Used:  %.2f GB  (%.1f%%)
  Total: %.2f GB
  Free:  %.2f GB

Please review the tenant's storage usage or increase the quota limit.
Go to Console → Tenants → %s to manage the quota.

---
This alert is sent automatically when a tenant's quota crosses configured thresholds.
To adjust thresholds, go to System Settings > System > Disk Warning/Critical Threshold.
`,
		tenantName,
		alertMsg,
		float64(currentBytes)/1e9,
		usedPct,
		float64(maxBytes)/1e9,
		float64(max(maxBytes-currentBytes, 0))/1e9,
		tenantName,
	)
	s.emailAdmins(tenantID, subject, body, "Quota alert", logrus.Fields{"tenant_id": tenantID})
}
