package server

import (
	"context"
	"fmt"
	"time"

	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/metrics"
	"github.com/sirupsen/logrus"
)

// diskAlertKey is the one key of the disk's alert level.
const diskAlertKey = "disk"

// startDiskAlertMonitor starts a background goroutine that monitors disk usage
// every 5 minutes and sends SSE notifications + emails when thresholds are crossed.
func (s *Server) startDiskAlertMonitor(ctx context.Context) {
	state := &alertLevels{}
	s.goWorker("disk alert monitor", func() {
		// Check immediately on startup
		s.checkDiskAlerts(state)

		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.checkDiskAlerts(state)
			}
		}
	})
}

func (s *Server) checkDiskAlerts(state *alertLevels) {
	stats, err := s.systemMetrics.GetDiskUsage()
	if err != nil {
		logrus.WithError(err).Debug("Disk alert: failed to get disk usage")
		return
	}
	s.diskAlert(state, stats)
}

// diskAlert alerts on the disk's usage when its level changes.
func (s *Server) diskAlert(state *alertLevels, stats *metrics.DiskStats) {
	warnPct, critPct := s.alertThresholds()
	used := stats.UsedPercent
	newLevel := alertLevelOf(used, warnPct, critPct)
	prev := state.change(diskAlertKey, newLevel)

	// Condition resolved: was alerting, now back to normal
	if newLevel == alertLevelNone && prev != alertLevelNone {
		s.notificationHub.SendNotification(&Notification{
			Type:    "disk_resolved",
			Message: fmt.Sprintf("Disk space is back to normal (%.1f%% used, %.1f / %.1f GB)", used, float64(stats.UsedBytes)/1e9, float64(stats.TotalBytes)/1e9),
			Data: map[string]interface{}{
				"usedPercent": used,
				"usedBytes":   stats.UsedBytes,
				"totalBytes":  stats.TotalBytes,
				"freeBytes":   stats.FreeBytes,
			},
			Timestamp: time.Now().Unix(),
		})
		s.logAuditEvent(context.Background(), &audit.AuditEvent{
			UserID:       "system",
			Username:     "system",
			EventType:    audit.EventTypeDiskAlert,
			ResourceType: audit.ResourceTypeSystem,
			Action:       audit.ActionResolve,
			Status:       audit.StatusSuccess,
			Details: map[string]interface{}{
				"used_percent": used,
				"used_gb":      float64(stats.UsedBytes) / 1e9,
				"total_gb":     float64(stats.TotalBytes) / 1e9,
				"free_gb":      float64(stats.FreeBytes) / 1e9,
			},
		})
		return
	}
	// Only fire when the level escalates — don't re-alert at the same level
	if newLevel <= prev {
		return
	}

	kind, title, shout := alertWords(newLevel)
	notifType := "disk_" + kind
	subject := "[MaxIOFS] " + title + ": Disk Space Alert"
	logMsg := fmt.Sprintf("%s: disk at %.1f%% (%.1f / %.1f GB)",
		shout, used, float64(stats.UsedBytes)/1e9, float64(stats.TotalBytes)/1e9)

	logrus.WithFields(logrus.Fields{
		"used_pct":  used,
		"used_gb":   float64(stats.UsedBytes) / 1e9,
		"total_gb":  float64(stats.TotalBytes) / 1e9,
		"threshold": critPct,
	}).Warn("Disk space alert triggered")

	s.logAuditEvent(context.Background(), &audit.AuditEvent{
		UserID:       "system",
		Username:     "system",
		EventType:    audit.EventTypeDiskAlert,
		ResourceType: audit.ResourceTypeSystem,
		Action:       audit.ActionAlert,
		Status:       audit.StatusSuccess,
		Details: map[string]interface{}{
			"level":        notifType,
			"used_percent": used,
			"used_gb":      float64(stats.UsedBytes) / 1e9,
			"total_gb":     float64(stats.TotalBytes) / 1e9,
			"free_gb":      float64(stats.FreeBytes) / 1e9,
			"warn_at":      warnPct,
			"critical_at":  critPct,
		},
	})

	// SSE notification (global admins only — no TenantID set)
	s.notificationHub.SendNotification(&Notification{
		Type:    notifType,
		Message: logMsg,
		Data: map[string]interface{}{
			"usedPercent": used,
			"usedBytes":   stats.UsedBytes,
			"totalBytes":  stats.TotalBytes,
			"freeBytes":   stats.FreeBytes,
			"warnAt":      warnPct,
			"criticalAt":  critPct,
		},
		Timestamp: time.Now().Unix(),
	})

	s.sendDiskAlertEmail(subject, logMsg, stats.UsedPercent, stats.UsedBytes, stats.TotalBytes, stats.FreeBytes)
}

func (s *Server) sendDiskAlertEmail(subject, alertMsg string, usedPct float64, usedBytes, totalBytes, freeBytes uint64) {
	body := fmt.Sprintf(`MaxIOFS Disk Space Alert
========================

%s

Disk details:
  Used:  %.1f GB  (%.1f%%)
  Total: %.1f GB
  Free:  %.1f GB

Please free up disk space or expand storage capacity to avoid service interruption.

---
This alert is sent automatically by MaxIOFS when disk usage crosses configured thresholds.
To adjust thresholds, go to System Settings > System > Disk Warning/Critical Threshold.
`,
		alertMsg,
		float64(usedBytes)/1e9,
		usedPct,
		float64(totalBytes)/1e9,
		float64(freeBytes)/1e9,
	)
	s.emailAdmins("", subject, body, "Disk alert", nil)
}
