package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

// CheckNodeHealth performs a health check on a specific node
func (m *Manager) CheckNodeHealth(ctx context.Context, nodeID string) (*HealthStatus, error) {
	node, err := m.GetNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	// Perform health check
	result := m.performHealthCheck(node.Endpoint)

	status := HealthStatusHealthy
	usagePct := 0.0
	if result.CapacityTotal > 0 {
		usagePct = float64(result.CapacityUsed) / float64(result.CapacityTotal) * 100
	}
	if !result.Healthy {
		status = HealthStatusUnavailable
	} else {
		// A full disk outweighs a slow answer: a slow node takes new data, one
		// under storage pressure does not.
		threshold, release := m.loadStoragePressureThresholds(ctx)
		switch {
		case node.HealthStatus == HealthStatusStoragePressure && result.CapacityTotal > 0 && usagePct >= release:
			// Sticky: stay in storage_pressure until usage drops below release.
			status = HealthStatusStoragePressure
		case result.CapacityTotal > 0 && usagePct >= threshold:
			status = HealthStatusStoragePressure
		case result.LatencyMs > 1000:
			status = HealthStatusDegraded
		default:
			status = HealthStatusHealthy
		}
	}

	// A node dead by this node's own probes is back in service once it answers
	// again, and is caught up from its first missed write. A drained node stays
	// dead.
	if node.HealthStatus == HealthStatusDead && (node.Drained || !result.Healthy) {
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

	now := time.Now()
	if result.Healthy {
		if result.CapacityTotal > 0 {
			_, err = m.db.ExecContext(ctx, `
				UPDATE cluster_nodes
				SET health_status = ?, last_health_check = ?, last_seen = ?, latency_ms = ?,
				    capacity_total = ?, capacity_used = ?, bucket_count = ?, updated_at = ?,
				    unavailable_since = NULL
				WHERE id = ?
			`, status, now, now, result.LatencyMs, result.CapacityTotal, result.CapacityUsed, result.BucketCount, now, nodeID)
		} else {
			_, err = m.db.ExecContext(ctx, `
				UPDATE cluster_nodes
				SET health_status = ?, last_health_check = ?, last_seen = ?, latency_ms = ?,
				    bucket_count = ?, updated_at = ?, unavailable_since = NULL
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
	if (&Node{HealthStatus: status}).InService() {
		m.catchUpReplica(ctx, nodeID)
	}

	if m.storagePressureFn != nil {
		threshold, _ := m.loadStoragePressureThresholds(ctx)
		var event *StoragePressureEvent
		switch {
		case node.HealthStatus != HealthStatusStoragePressure && status == HealthStatusStoragePressure:
			event = &StoragePressureEvent{
				NodeID: nodeID, NodeName: node.Name,
				Kind:             "node_storage_pressure",
				UsagePercent:     usagePct,
				ThresholdPercent: threshold,
			}
		case node.HealthStatus == HealthStatusStoragePressure && status != HealthStatusStoragePressure:
			event = &StoragePressureEvent{
				NodeID: nodeID, NodeName: node.Name,
				Kind:             "node_storage_pressure_resolved",
				UsagePercent:     usagePct,
				ThresholdPercent: threshold,
			}
		}
		if event != nil {
			m.emitStoragePressure(*event)
		}
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

// emitStoragePressure calls the emitter inline; the emitter contract forbids
// blocking. The recover keeps a faulty one from killing the health check.
func (m *Manager) emitStoragePressure(ev StoragePressureEvent) {
	defer func() {
		if r := recover(); r != nil {
			logrus.WithField("panic", r).Error("storage pressure callback panicked")
		}
	}()
	m.storagePressureFn(ev)
}

// performHealthCheck performs an HTTP health check on the given endpoint.
// healthClient returns the shared client used to probe nodes, rebuilding it
// only when the cluster TLS configuration has changed.
func (m *Manager) healthClient() *http.Client {
	current := m.tlsConfig.Load()

	m.healthClientMu.Lock()
	defer m.healthClientMu.Unlock()

	if m.healthHTTPClient == nil || m.healthClientTLS != current {
		if m.healthHTTPClient != nil {
			m.healthHTTPClient.CloseIdleConnections()
		}
		transport := &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     90 * time.Second,
		}
		if current != nil {
			transport.TLSClientConfig = current.Clone()
		}
		m.healthHTTPClient = &http.Client{Timeout: 5 * time.Second, Transport: transport}
		m.healthClientTLS = current
	}
	return m.healthHTTPClient
}

// It also reads capacity_total and capacity_used from the health response
// so that node storage stats are kept current in the cluster DB.
func (m *Manager) performHealthCheck(endpoint string) *HealthCheckResult {
	start := time.Now()

	client := m.healthClient()

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
		case <-m.Stopped():
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

// noteMissedWrites records that the given nodes, or when none are given every
// node that is neither local, healthy nor dead, missed a write made at
// modified. The earliest miss is kept until the node is caught up.
func (m *Manager) noteMissedWrites(ctx context.Context, localID string, modified time.Time, nodeIDs ...string) {
	const earliest = `UPDATE cluster_nodes SET replica_missed_since = CASE
		WHEN replica_missed_since IS NULL OR replica_missed_since > ? THEN ?
		ELSE replica_missed_since END `
	since := modified.Unix()
	ctx = context.WithoutCancel(ctx)
	var err error
	if len(nodeIDs) == 0 {
		_, err = m.db.ExecContext(ctx, earliest+`WHERE id != ? AND health_status NOT IN (?, ?, ?, ?)`,
			since, since, localID, HealthStatusHealthy, HealthStatusDegraded, HealthStatusStoragePressure, HealthStatusDead)
	}
	for _, id := range nodeIDs {
		_, idErr := m.db.ExecContext(ctx, earliest+`WHERE id = ?`, since, since, id)
		err = errors.Join(err, idErr)
	}
	if err != nil {
		m.log.WithError(err).Warn("Failed to record a missed replica write")
	}
}

// catchUpReplica takes the node's record of missed writes, if any, and hands
// it to the OnReplicaBack hook. The record is cleared only if it did not change
// since it was read: an earlier miss recorded meanwhile stays for the next check.
// Until the catch-up ends (CatchUpEnded) the node keeps the time it began from,
// so the deletions it is to be sent are not forgotten meanwhile.
func (m *Manager) catchUpReplica(ctx context.Context, nodeID string) {
	fn := m.replicaCaughtUp.Load()
	if fn == nil {
		return
	}
	var since sql.NullInt64
	err := m.db.QueryRowContext(ctx,
		`SELECT replica_missed_since FROM cluster_nodes WHERE id = ?`, nodeID).Scan(&since)
	if err != nil || !since.Valid {
		return
	}
	res, err := m.db.ExecContext(ctx,
		`UPDATE cluster_nodes SET replica_missed_since = NULL,
			replica_catchup_since = MIN(COALESCE(replica_catchup_since, ?), ?)
		WHERE id = ? AND replica_missed_since = ?`,
		since.Int64, since.Int64, nodeID, since.Int64)
	if err != nil {
		m.log.WithError(err).WithField("node_id", nodeID).Warn("Failed to clear missed replica writes")
		return
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return
	}
	(*fn)(nodeID, time.Unix(since.Int64, 0))
}

// CatchUpEnded records that the node has everything written since since
// (unix seconds): a catch-up begun at or after that time is over.
func (m *Manager) CatchUpEnded(ctx context.Context, nodeID string, since int64) {
	if _, err := m.db.ExecContext(ctx,
		`UPDATE cluster_nodes SET replica_catchup_since = NULL WHERE id = ? AND replica_catchup_since >= ?`,
		nodeID, since); err != nil {
		m.log.WithError(err).WithField("node_id", nodeID).Warn("Failed to record the end of a catch-up")
	}
}

// oldestUnsentChange is the earliest time (unix seconds) from which a member
// of the cluster may lack this node's changes: its first missed write, or the
// start of a catch-up that has not ended. ok is false when no member lacks any.
func oldestUnsentChange(ctx context.Context, db *sql.DB) (since int64, ok bool) {
	var oldest sql.NullInt64
	err := db.QueryRowContext(ctx, `
		SELECT MIN(t) FROM (
			SELECT replica_missed_since AS t FROM cluster_nodes
			WHERE replica_missed_since IS NOT NULL AND id NOT IN (SELECT node_id FROM cluster_config)
			UNION ALL
			SELECT replica_catchup_since FROM cluster_nodes
			WHERE replica_catchup_since IS NOT NULL AND id NOT IN (SELECT node_id FROM cluster_config)
		)`).Scan(&oldest)
	if err != nil || !oldest.Valid {
		return 0, false
	}
	return oldest.Int64, true
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
			"rows_deleted":   rowsAffected,
			"retention_days": retentionDays,
		}).Info("Cleaned up old health check history")
	}

	return nil
}
