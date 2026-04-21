package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

// Storage-pressure config keys + sane fallback defaults used when the cluster
// global config row is missing or malformed (first boot, manual deletion, etc.).
const (
	storagePressureThresholdKey       = "ha.storage_pressure_threshold_percent"
	storagePressureReleaseKey         = "ha.storage_pressure_release_percent"
	defaultStoragePressureThresholdPc = 90.0
	defaultStoragePressureReleasePc   = 85.0
)

// loadStoragePressureThresholds reads (threshold, release) from cluster_global_config.
// Falls back to defaults on missing/invalid values, and clamps release < threshold
// so a misconfiguration cannot disable the hysteresis loop.
func (m *Manager) loadStoragePressureThresholds(ctx context.Context) (float64, float64) {
	threshold := defaultStoragePressureThresholdPc
	release := defaultStoragePressureReleasePc
	if v, err := GetGlobalConfig(ctx, m.db, storagePressureThresholdKey); err == nil {
		if f, perr := strconv.ParseFloat(v, 64); perr == nil && f > 0 && f <= 100 {
			threshold = f
		}
	}
	if v, err := GetGlobalConfig(ctx, m.db, storagePressureReleaseKey); err == nil {
		if f, perr := strconv.ParseFloat(v, 64); perr == nil && f >= 0 && f <= 100 {
			release = f
		}
	}
	if release >= threshold {
		// Misconfiguration: collapse to default gap to keep hysteresis alive.
		release = threshold - 5
		if release < 0 {
			release = 0
		}
	}
	return threshold, release
}

// StalenessThreshold is the duration after which an unreachable node is considered stale.
// Matches the tombstone TTL so that stale detection and deletion-log cleanup are aligned.
const StalenessThreshold = 7 * 24 * time.Hour

// CheckNodeHealth performs a health check on a specific node
func (m *Manager) CheckNodeHealth(ctx context.Context, nodeID string) (*HealthStatus, error) {
	node, err := m.GetNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	// Perform health check
	result := m.performHealthCheck(node.Endpoint)

	// Determine health status. Storage-pressure is evaluated only for reachable
	// nodes with acceptable latency — a node that is unavailable or degraded must
	// not be hidden behind the storage-pressure flag.
	status := HealthStatusHealthy
	usagePct := 0.0
	if result.CapacityTotal > 0 {
		usagePct = float64(result.CapacityUsed) / float64(result.CapacityTotal) * 100
	}
	if !result.Healthy {
		status = HealthStatusUnavailable
	} else if result.LatencyMs > 1000 {
		status = HealthStatusDegraded
	} else {
		threshold, release := m.loadStoragePressureThresholds(ctx)
		switch {
		case node.HealthStatus == HealthStatusStoragePressure && result.CapacityTotal > 0 && usagePct >= release:
			// Sticky: stay in storage_pressure until usage drops below release.
			status = HealthStatusStoragePressure
		case result.CapacityTotal > 0 && usagePct >= threshold:
			status = HealthStatusStoragePressure
		default:
			status = HealthStatusHealthy
		}
	}

	// Once a node has been marked dead, the dead-node reconciler owns its
	// lifecycle. Health checks must not silently flip it back to healthy/
	// unavailable — that would re-arm it for redistribution loops or worse,
	// hide that the operator must explicitly re-add the node. Skip the update.
	if node.HealthStatus == HealthStatusDead {
		// Still record the probe in history for visibility.
		_, _ = m.db.ExecContext(ctx, `
			INSERT INTO cluster_health_history (node_id, health_status, latency_ms, error_message)
			VALUES (?, ?, ?, ?)
		`, nodeID, HealthStatusDead, result.LatencyMs, result.ErrorMessage)
		return &HealthStatus{
			NodeID: nodeID, Status: HealthStatusDead,
			LatencyMs: result.LatencyMs, LastCheck: time.Now(),
			ErrorMessage: result.ErrorMessage,
		}, nil
	}

	// Update node health in database.
	// last_seen is only updated when the node is reachable, so it tracks
	// "last time alive" rather than "last time we tried".
	// unavailable_since tracks the start of an outage so the dead-node
	// reconciler can measure continuous unavailability — set it on the
	// healthy→unavailable transition, clear it on the inverse.
	now := time.Now()
	if result.Healthy {
		if result.CapacityTotal > 0 {
			_, err = m.db.ExecContext(ctx, `
				UPDATE cluster_nodes
				SET health_status = ?, last_health_check = ?, last_seen = ?, latency_ms = ?,
				    capacity_total = ?, capacity_used = ?, bucket_count = ?, updated_at = ?,
				    is_stale = 0, unavailable_since = NULL
				WHERE id = ?
			`, status, now, now, result.LatencyMs, result.CapacityTotal, result.CapacityUsed, result.BucketCount, now, nodeID)
		} else {
			_, err = m.db.ExecContext(ctx, `
				UPDATE cluster_nodes
				SET health_status = ?, last_health_check = ?, last_seen = ?, latency_ms = ?,
				    bucket_count = ?, updated_at = ?, is_stale = 0, unavailable_since = NULL
				WHERE id = ?
			`, status, now, now, result.LatencyMs, result.BucketCount, now, nodeID)
		}
	} else {
		// Set unavailable_since only on transition (preserve the original
		// outage start across repeated failed probes).
		_, err = m.db.ExecContext(ctx, `
			UPDATE cluster_nodes
			SET health_status = ?, last_health_check = ?, latency_ms = ?, updated_at = ?,
			    unavailable_since = COALESCE(unavailable_since, ?)
			WHERE id = ?
		`, status, now, result.LatencyMs, now, now, nodeID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update node health: %w", err)
	}

	// Emit storage-pressure transitions (cross the storage_pressure boundary
	// in either direction). The emitter is best-effort and never blocks; if no
	// callback is wired we silently skip.
	if m.storagePressureFn != nil {
		threshold, _ := m.loadStoragePressureThresholds(ctx)
		switch {
		case node.HealthStatus != HealthStatusStoragePressure && status == HealthStatusStoragePressure:
			m.storagePressureFn(StoragePressureEvent{
				NodeID: nodeID, NodeName: node.Name,
				Kind:             "node_storage_pressure",
				UsagePercent:     usagePct,
				ThresholdPercent: threshold,
			})
		case node.HealthStatus == HealthStatusStoragePressure && status != HealthStatusStoragePressure:
			m.storagePressureFn(StoragePressureEvent{
				NodeID: nodeID, NodeName: node.Name,
				Kind:             "node_storage_pressure_resolved",
				UsagePercent:     usagePct,
				ThresholdPercent: threshold,
			})
		}
	}

	// If the node is unreachable, check whether it has crossed the staleness threshold.
	if !result.Healthy {
		m.checkAndMarkStale(ctx, node, now)
	}

	// Record health check in history
	_, err = m.db.ExecContext(ctx, `
		INSERT INTO cluster_health_history (node_id, health_status, latency_ms, error_message)
		VALUES (?, ?, ?, ?)
	`, nodeID, status, result.LatencyMs, result.ErrorMessage)
	if err != nil {
		m.log.WithError(err).Warn("Failed to record health check history")
	}

	healthStatus := &HealthStatus{
		NodeID:       nodeID,
		Status:       status,
		LatencyMs:    result.LatencyMs,
		LastCheck:    now,
		ErrorMessage: result.ErrorMessage,
	}

	return healthStatus, nil
}

// performHealthCheck performs an HTTP health check on the given endpoint.
// It also reads capacity_total and capacity_used from the health response
// so that node storage stats are kept current in the cluster DB.
func (m *Manager) performHealthCheck(endpoint string) *HealthCheckResult {
	start := time.Now()

	// Create HTTP client with timeout, using cluster TLS if available
	transport := &http.Transport{}
	if m.tlsConfig != nil {
		transport.TLSClientConfig = m.tlsConfig.Clone()
	}
	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
	}

	// Perform GET request to /health endpoint
	healthURL := fmt.Sprintf("%s/health", endpoint)
	resp, err := client.Get(healthURL)
	if err != nil {
		return &HealthCheckResult{
			Healthy:      false,
			LatencyMs:    int(time.Since(start).Milliseconds()),
			ErrorMessage: err.Error(),
		}
	}
	defer resp.Body.Close()

	latency := int(time.Since(start).Milliseconds())

	if resp.StatusCode != http.StatusOK {
		return &HealthCheckResult{
			Healthy:      false,
			LatencyMs:    latency,
			ErrorMessage: fmt.Sprintf("unexpected status code: %d", resp.StatusCode),
		}
	}

	// Parse capacity and bucket count from the health response (best-effort, flat JSON).
	var body struct {
		CapacityTotal uint64 `json:"capacity_total"`
		CapacityUsed  uint64 `json:"capacity_used"`
		BucketCount   int    `json:"bucket_count"`
	}
	result := &HealthCheckResult{Healthy: true, LatencyMs: latency}
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
		result.CapacityTotal = int64(body.CapacityTotal)
		result.CapacityUsed = int64(body.CapacityUsed)
		result.BucketCount = body.BucketCount
	}
	return result
}

// StartHealthChecker starts the background health checker
func (m *Manager) StartHealthChecker(ctx context.Context) {
	m.log.WithField("interval", m.healthCheckInterval).Info("Starting health checker")

	ticker := time.NewTicker(m.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.log.Info("Health checker stopped due to context cancellation")
			return
		case <-m.stopChan:
			m.log.Info("Health checker stopped")
			return
		case <-ticker.C:
			m.performHealthChecks(ctx)
		}
	}
}

// performHealthChecks checks health of all nodes
func (m *Manager) performHealthChecks(ctx context.Context) {
	nodes, err := m.ListNodes(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to list nodes for health check")
		return
	}

	if len(nodes) == 0 {
		return
	}

	m.log.WithField("node_count", len(nodes)).Debug("Performing health checks")

	for _, node := range nodes {
		// Create a timeout context for each health check
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)

		_, err := m.CheckNodeHealth(checkCtx, node.ID)
		if err != nil {
			m.log.WithFields(logrus.Fields{
				"node_id":   node.ID,
				"node_name": node.Name,
				"error":     err,
			}).Warn("Health check failed")
		}

		cancel()
	}
}

// checkAndMarkStale marks a node as stale if it has been unreachable longer than StalenessThreshold.
// It uses the node's pre-check LastSeen value (the last time it was actually alive).
func (m *Manager) checkAndMarkStale(ctx context.Context, node *Node, now time.Time) {
	if node.LastSeen == nil {
		// Node was never successfully reached; nothing to compare against yet.
		return
	}
	if now.Sub(*node.LastSeen) < StalenessThreshold {
		return
	}
	if node.IsStale {
		// Already marked — avoid redundant writes.
		return
	}

	_, err := m.db.ExecContext(ctx,
		"UPDATE cluster_nodes SET is_stale = 1, updated_at = ? WHERE id = ? AND is_stale = 0",
		now, node.ID,
	)
	if err != nil {
		m.log.WithError(err).Warn("Failed to mark node as stale")
		return
	}

	m.log.WithFields(logrus.Fields{
		"node_id":   node.ID,
		"node_name": node.Name,
		"last_seen": node.LastSeen.Format(time.RFC3339),
		"offline_for": now.Sub(*node.LastSeen).String(),
	}).Warn("Node marked as stale: offline beyond staleness threshold")
}

// CleanupHealthHistory removes old health check history entries
func (m *Manager) CleanupHealthHistory(ctx context.Context, retentionDays int) error {
	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)

	result, err := m.db.ExecContext(ctx, `
		DELETE FROM cluster_health_history
		WHERE timestamp < ?
	`, cutoffTime)
	if err != nil {
		return fmt.Errorf("failed to cleanup health history: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected > 0 {
		m.log.WithFields(logrus.Fields{
			"rows_deleted":    rowsAffected,
			"retention_days": retentionDays,
		}).Info("Cleaned up old health check history")
	}

	return nil
}
