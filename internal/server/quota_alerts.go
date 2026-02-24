package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// quotaAlertTracker holds per-tenant alert deduplication state
type quotaAlertTracker struct {
	mu     sync.Mutex
	levels sync.Map // tenantID -> alertLevel
}

func newQuotaAlertTracker() *quotaAlertTracker {
	return &quotaAlertTracker{}
}

// checkQuotaAlert is called after every successful storage increment for a tenant.
// It fires SSE + email only when the alert level escalates (none→warning, warning→critical).
func (s *Server) checkQuotaAlert(tenantID string, currentBytes, maxBytes int64) {
	if maxBytes == 0 {
		return // unlimited quota
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

	// Load previous level for this tenant
	prevRaw, _ := s.quotaAlerts.levels.Load(tenantID)
	prev, _ := prevRaw.(alertLevel)

	// Only act on escalation
	if newLevel <= prev {
		return
	}
	s.quotaAlerts.levels.Store(tenantID, newLevel)

	// Get tenant display name for messages
	tenantName := tenantID
	if tenant, err := s.authManager.GetTenant(context.Background(), tenantID); err == nil && tenant != nil {
		if tenant.DisplayName != "" {
			tenantName = tenant.DisplayName
		} else {
			tenantName = tenant.Name
		}
	}

	var notifType, subject, logMsg string
	if newLevel == alertLevelCritical {
		notifType = "quota_critical"
		subject = fmt.Sprintf("[MaxIOFS] CRITICAL: Storage Quota Alert — %s", tenantName)
		logMsg = fmt.Sprintf("CRITICAL: tenant %q quota at %.1f%% (%.2f / %.2f GB)",
			tenantName, usedPct, float64(currentBytes)/1e9, float64(maxBytes)/1e9)
	} else {
		notifType = "quota_warning"
		subject = fmt.Sprintf("[MaxIOFS] Warning: Storage Quota Alert — %s", tenantName)
		logMsg = fmt.Sprintf("WARNING: tenant %q quota at %.1f%% (%.2f / %.2f GB)",
			tenantName, usedPct, float64(currentBytes)/1e9, float64(maxBytes)/1e9)
	}

	logrus.WithFields(logrus.Fields{
		"tenant_id":     tenantID,
		"tenant_name":   tenantName,
		"used_pct":      usedPct,
		"current_bytes": currentBytes,
		"max_bytes":     maxBytes,
	}).Warn("Tenant quota alert triggered")

	// SSE notification — TenantID set so tenant admins also receive it
	s.notificationHub.SendNotification(&Notification{
		Type:    notifType,
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

	// Email notification
	s.sendQuotaAlertEmail(subject, logMsg, tenantID, tenantName, usedPct, currentBytes, maxBytes)
}

func (s *Server) sendQuotaAlertEmail(subject, alertMsg, tenantID, tenantName string, usedPct float64, currentBytes, maxBytes int64) {
	enabled, _ := s.settingsManager.GetBool("email.enabled")
	if !enabled {
		return
	}

	sender := s.buildEmailSender()
	if sender == nil || !sender.IsConfigured() {
		return
	}

	// Collect tenant admins + global admins with email addresses
	users, err := s.authManager.ListUsers(context.Background())
	if err != nil {
		logrus.WithError(err).Error("Quota alert: failed to list users for email")
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
				break
			}
			if role == "admin" && u.TenantID == tenantID {
				isTenantAdmin = true
				break
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
		float64(freeBytes)/1e9,
		tenantName,
	)

	if err := sender.Send(recipients, subject, body); err != nil {
		logrus.WithError(err).Error("Failed to send quota alert email")
		return
	}
	logrus.WithFields(logrus.Fields{
		"tenant_id":  tenantID,
		"recipients": len(recipients),
	}).Info("Quota alert email sent")
}
