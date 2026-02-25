package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	emailpkg "github.com/maxiofs/maxiofs/internal/email"
	"github.com/sirupsen/logrus"
)

// alertLevel represents the severity of a disk alert
type alertLevel int

const (
	alertLevelNone     alertLevel = 0
	alertLevelWarning  alertLevel = 1
	alertLevelCritical alertLevel = 2
)

// diskAlertState tracks the last sent alert to avoid duplicate notifications
type diskAlertState struct {
	mu    sync.Mutex
	level alertLevel
}

// startDiskAlertMonitor starts a background goroutine that monitors disk usage
// every 5 minutes and sends SSE notifications + emails when thresholds are crossed.
func (s *Server) startDiskAlertMonitor(ctx context.Context) {
	state := &diskAlertState{}
	go func() {
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
	}()
}

func (s *Server) checkDiskAlerts(state *diskAlertState) {
	stats, err := s.systemMetrics.GetDiskUsage()
	if err != nil {
		logrus.WithError(err).Debug("Disk alert: failed to get disk usage")
		return
	}

	warnPct := 80
	critPct := 90
	if v, err := s.settingsManager.GetInt("system.disk_warning_threshold"); err == nil && v > 0 {
		warnPct = v
	}
	if v, err := s.settingsManager.GetInt("system.disk_critical_threshold"); err == nil && v > 0 {
		critPct = v
	}

	used := stats.UsedPercent
	var newLevel alertLevel
	switch {
	case used >= float64(critPct):
		newLevel = alertLevelCritical
	case used >= float64(warnPct):
		newLevel = alertLevelWarning
	default:
		newLevel = alertLevelNone
	}

	state.mu.Lock()
	prev := state.level
	state.level = newLevel
	state.mu.Unlock()

	// Only fire when the level escalates — don't re-alert at the same level
	if newLevel <= prev {
		return
	}

	var notifType, subject, logMsg string
	if newLevel == alertLevelCritical {
		notifType = "disk_critical"
		subject = "[MaxIOFS] CRITICAL: Disk Space Alert"
		logMsg = fmt.Sprintf("CRITICAL: disk at %.1f%% (%.1f / %.1f GB)",
			used, float64(stats.UsedBytes)/1e9, float64(stats.TotalBytes)/1e9)
	} else {
		notifType = "disk_warning"
		subject = "[MaxIOFS] Warning: Disk Space Alert"
		logMsg = fmt.Sprintf("WARNING: disk at %.1f%% (%.1f / %.1f GB)",
			used, float64(stats.UsedBytes)/1e9, float64(stats.TotalBytes)/1e9)
	}

	logrus.WithFields(logrus.Fields{
		"used_pct":  used,
		"used_gb":   float64(stats.UsedBytes) / 1e9,
		"total_gb":  float64(stats.TotalBytes) / 1e9,
		"threshold": critPct,
	}).Warn("Disk space alert triggered")

	// SSE notification (global admins only — no TenantID set)
	s.notificationHub.SendNotification(&Notification{
		Type:    notifType,
		Message: logMsg,
		Data: map[string]interface{}{
			"usedPercent":  used,
			"usedBytes":    stats.UsedBytes,
			"totalBytes":   stats.TotalBytes,
			"freeBytes":    stats.FreeBytes,
			"warnAt":       warnPct,
			"criticalAt":   critPct,
		},
		Timestamp: time.Now().Unix(),
	})

	// Email notification
	s.sendDiskAlertEmail(subject, logMsg, stats.UsedPercent, stats.UsedBytes, stats.TotalBytes, stats.FreeBytes)
}

func (s *Server) sendDiskAlertEmail(subject, alertMsg string, usedPct float64, usedBytes, totalBytes, freeBytes uint64) {
	enabled, _ := s.settingsManager.GetBool("email.enabled")
	if !enabled {
		return
	}

	sender := s.buildEmailSender()
	if sender == nil || !sender.IsConfigured() {
		return
	}

	// Collect emails of all active global admins
	users, err := s.authManager.ListUsers(context.Background())
	if err != nil {
		logrus.WithError(err).Error("Disk alert: failed to list users for email")
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
		logrus.Debug("Disk alert: no admin emails configured, skipping email")
		return
	}

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

	if err := sender.Send(recipients, subject, body); err != nil {
		logrus.WithError(err).Error("Failed to send disk alert email")
		return
	}
	logrus.WithField("recipients", len(recipients)).Info("Disk alert email sent")
}

// buildEmailSender constructs an email.Sender from current settings.
// Returns nil if email.smtp_host is not configured.
func (s *Server) buildEmailSender() *emailpkg.Sender {
	host, _ := s.settingsManager.Get("email.smtp_host")
	if host == "" {
		return nil
	}
	port, err := s.settingsManager.GetInt("email.smtp_port")
	if err != nil || port == 0 {
		port = 587
	}
	user, _ := s.settingsManager.Get("email.smtp_user")
	password, _ := s.settingsManager.Get("email.smtp_password")
	from, _ := s.settingsManager.Get("email.from_address")
	tlsMode, _ := s.settingsManager.Get("email.tls_mode")
	skipVerify, _ := s.settingsManager.GetBool("email.skip_tls_verify")

	return emailpkg.NewSender(emailpkg.Config{
		Host:               host,
		Port:               port,
		User:               user,
		Password:           password,
		From:               from,
		TLSMode:            tlsMode,
		InsecureSkipVerify: skipVerify,
	})
}
