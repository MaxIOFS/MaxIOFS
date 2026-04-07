package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/sirupsen/logrus"
)

// clusterNodeAlertStates tracks the last sent alert level per cluster node.
// One entry per nodeID; entries are added on first check and updated on escalation/resolution.
type clusterNodeAlertStates struct {
	mu     sync.Mutex
	levels map[string]alertLevel // nodeID → last sent level
}

func newClusterNodeAlertStates() *clusterNodeAlertStates {
	return &clusterNodeAlertStates{levels: make(map[string]alertLevel)}
}

func (s *clusterNodeAlertStates) get(nodeID string) alertLevel {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.levels[nodeID]
}

func (s *clusterNodeAlertStates) set(nodeID string, level alertLevel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.levels[nodeID] = level
}

// startClusterNodeAlertMonitor starts a background goroutine that checks
// cluster node storage every 5 minutes and sends SSE + email alerts when
// nodes cross the configured warning/critical thresholds.
// Reuses the same system.disk_warning_threshold / system.disk_critical_threshold
// settings as the local disk alert monitor.
func (s *Server) startClusterNodeAlertMonitor(ctx context.Context) {
	states := newClusterNodeAlertStates()
	go func() {
		// Check immediately on startup
		s.checkClusterNodeAlerts(ctx, states)

		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.checkClusterNodeAlerts(ctx, states)
			}
		}
	}()
}

func (s *Server) checkClusterNodeAlerts(ctx context.Context, states *clusterNodeAlertStates) {
	if s.clusterManager == nil {
		return
	}

	nodes, err := s.clusterManager.ListNodes(ctx)
	if err != nil {
		logrus.WithError(err).Debug("Cluster node alert: failed to list nodes")
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

	for _, node := range nodes {
		if node.CapacityTotal <= 0 {
			// No capacity data yet for this node — skip
			continue
		}

		usedPct := float64(node.CapacityUsed) / float64(node.CapacityTotal) * 100.0
		freeBytes := node.CapacityTotal - node.CapacityUsed

		var newLevel alertLevel
		switch {
		case usedPct >= float64(critPct):
			newLevel = alertLevelCritical
		case usedPct >= float64(warnPct):
			newLevel = alertLevelWarning
		default:
			newLevel = alertLevelNone
		}

		prev := states.get(node.ID)
		states.set(node.ID, newLevel)

		// Condition resolved: was alerting, now back to normal
		if newLevel == alertLevelNone && prev != alertLevelNone {
			s.notificationHub.SendNotification(&Notification{
				Type:    "cluster_node_resolved",
				Message: fmt.Sprintf("Node %q storage is back to normal (%.1f%% used)", node.Name, usedPct),
				Data: map[string]interface{}{
					"nodeId":      node.ID,
					"nodeName":    node.Name,
					"usedPercent": usedPct,
					"usedBytes":   node.CapacityUsed,
					"totalBytes":  node.CapacityTotal,
					"freeBytes":   freeBytes,
				},
				Timestamp: time.Now().Unix(),
			})
			if s.auditManager != nil {
				_ = s.auditManager.LogEvent(ctx, &audit.AuditEvent{
					UserID:       "system",
					Username:     "system",
					EventType:    audit.EventTypeClusterNodeAlert,
					ResourceType: audit.ResourceTypeSystem,
					ResourceID:   node.ID,
					ResourceName: node.Name,
					Action:       audit.ActionResolve,
					Status:       audit.StatusSuccess,
					Details: map[string]interface{}{
						"node_id":      node.ID,
						"node_name":    node.Name,
						"used_percent": usedPct,
						"used_gb":      float64(node.CapacityUsed) / 1e9,
						"total_gb":     float64(node.CapacityTotal) / 1e9,
						"free_gb":      float64(freeBytes) / 1e9,
					},
				})
			}
			continue
		}

		// Only fire when the level escalates — don't re-alert at the same level
		if newLevel <= prev {
			continue
		}

		var notifType, subject, logMsg string
		if newLevel == alertLevelCritical {
			notifType = "cluster_node_critical"
			subject = fmt.Sprintf("[MaxIOFS] CRITICAL: Cluster Node %q Disk Space Alert", node.Name)
			logMsg = fmt.Sprintf("CRITICAL: node %q at %.1f%% (%.1f / %.1f GB)",
				node.Name, usedPct, float64(node.CapacityUsed)/1e9, float64(node.CapacityTotal)/1e9)
		} else {
			notifType = "cluster_node_warning"
			subject = fmt.Sprintf("[MaxIOFS] Warning: Cluster Node %q Disk Space Alert", node.Name)
			logMsg = fmt.Sprintf("WARNING: node %q at %.1f%% (%.1f / %.1f GB)",
				node.Name, usedPct, float64(node.CapacityUsed)/1e9, float64(node.CapacityTotal)/1e9)
		}

		logrus.WithFields(logrus.Fields{
			"node_id":   node.ID,
			"node_name": node.Name,
			"used_pct":  usedPct,
			"used_gb":   float64(node.CapacityUsed) / 1e9,
			"total_gb":  float64(node.CapacityTotal) / 1e9,
		}).Warn("Cluster node storage alert triggered")

		if s.auditManager != nil {
			_ = s.auditManager.LogEvent(ctx, &audit.AuditEvent{
				UserID:       "system",
				Username:     "system",
				EventType:    audit.EventTypeClusterNodeAlert,
				ResourceType: audit.ResourceTypeSystem,
				ResourceID:   node.ID,
				ResourceName: node.Name,
				Action:       audit.ActionAlert,
				Status:       audit.StatusSuccess,
				Details: map[string]interface{}{
					"level":        notifType,
					"node_id":      node.ID,
					"node_name":    node.Name,
					"used_percent": usedPct,
					"used_gb":      float64(node.CapacityUsed) / 1e9,
					"total_gb":     float64(node.CapacityTotal) / 1e9,
					"free_gb":      float64(freeBytes) / 1e9,
					"warn_at":      warnPct,
					"critical_at":  critPct,
				},
			})
		}

		s.notificationHub.SendNotification(&Notification{
			Type:    notifType,
			Message: logMsg,
			Data: map[string]interface{}{
				"nodeId":      node.ID,
				"nodeName":    node.Name,
				"usedPercent": usedPct,
				"usedBytes":   node.CapacityUsed,
				"totalBytes":  node.CapacityTotal,
				"freeBytes":   freeBytes,
				"warnAt":      warnPct,
				"criticalAt":  critPct,
			},
			Timestamp: time.Now().Unix(),
		})

		s.sendClusterNodeAlertEmail(subject, logMsg, node.Name, usedPct,
			node.CapacityUsed, node.CapacityTotal, freeBytes)
	}
}

func (s *Server) sendClusterNodeAlertEmail(subject, alertMsg, nodeName string,
	usedPct float64, usedBytes, totalBytes, freeBytes int64) {

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
		logrus.WithError(err).Error("Cluster node alert: failed to list users for email")
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
		logrus.Debug("Cluster node alert: no admin emails configured, skipping email")
		return
	}

	body := fmt.Sprintf(`MaxIOFS Cluster Node Disk Space Alert
=====================================

Node: %s
%s

Disk details:
  Used:  %.1f GB  (%.1f%%)
  Total: %.1f GB
  Free:  %.1f GB

Please free up disk space or expand storage capacity to avoid service interruption.

---
This alert is sent automatically by MaxIOFS when cluster node disk usage crosses configured thresholds.
To adjust thresholds, go to System Settings > System > Disk Warning/Critical Threshold.
`,
		nodeName,
		alertMsg,
		float64(usedBytes)/1e9,
		usedPct,
		float64(totalBytes)/1e9,
		float64(freeBytes)/1e9,
	)

	if err := sender.Send(recipients, subject, body); err != nil {
		logrus.WithError(err).Error("Failed to send cluster node alert email")
		return
	}
	logrus.WithField("recipients", len(recipients)).Info("Cluster node alert email sent")
}
