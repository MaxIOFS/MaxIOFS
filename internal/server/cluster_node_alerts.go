package server

import (
	"context"
	"fmt"
	"time"

	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// startClusterNodeAlertMonitor starts a background goroutine that checks
func (s *Server) startClusterNodeAlertMonitor(ctx context.Context) {
	states := &alertLevels{}
	s.goWorker("cluster node alert monitor", func() {
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
	})
}

func (s *Server) checkClusterNodeAlerts(ctx context.Context, states *alertLevels) {
	if s.clusterManager == nil {
		return
	}
	nodes, err := s.clusterManager.ListNodes(ctx)
	if err != nil {
		logrus.WithError(err).Debug("Cluster node alert: failed to list nodes")
		return
	}
	warnPct, critPct := s.alertThresholds()
	for _, node := range nodes {
		s.clusterNodeAlert(ctx, states, node, warnPct, critPct)
	}
}

// clusterNodeAlert alerts on a node's storage when its level changes.
func (s *Server) clusterNodeAlert(ctx context.Context, states *alertLevels, node *cluster.Node, warnPct, critPct int) {
	if node.CapacityTotal <= 0 {
		// No capacity data yet for this node — skip
		return
	}

	usedPct := float64(node.CapacityUsed) / float64(node.CapacityTotal) * 100.0
	freeBytes := node.CapacityTotal - node.CapacityUsed

	newLevel := alertLevelOf(usedPct, warnPct, critPct)
	prev := states.change(node.ID, newLevel)

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
		s.logAuditEvent(ctx, &audit.AuditEvent{
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
		return
	}

	// Only fire when the level escalates — don't re-alert at the same level
	if newLevel <= prev {
		return
	}

	kind, title, shout := alertWords(newLevel)
	notifType := "cluster_node_" + kind
	subject := fmt.Sprintf("[MaxIOFS] %s: Cluster Node %q Disk Space Alert", title, node.Name)
	logMsg := fmt.Sprintf("%s: node %q at %.1f%% (%.1f / %.1f GB)",
		shout, node.Name, usedPct, float64(node.CapacityUsed)/1e9, float64(node.CapacityTotal)/1e9)

	logrus.WithFields(logrus.Fields{
		"node_id":   node.ID,
		"node_name": node.Name,
		"used_pct":  usedPct,
		"used_gb":   float64(node.CapacityUsed) / 1e9,
		"total_gb":  float64(node.CapacityTotal) / 1e9,
	}).Warn("Cluster node storage alert triggered")

	s.logAuditEvent(ctx, &audit.AuditEvent{
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

func (s *Server) sendClusterNodeAlertEmail(subject, alertMsg, nodeName string,
	usedPct float64, usedBytes, totalBytes, freeBytes int64) {
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

	s.emailAdmins("", subject, body, "Cluster node alert", logrus.Fields{"node_name": nodeName})
}
