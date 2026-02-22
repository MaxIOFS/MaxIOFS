package cluster

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// CheckNodeHealth performs a health check on a specific node
func (m *Manager) CheckNodeHealth(ctx context.Context, nodeID string) (*HealthStatus, error) {
	node, err := m.GetNode(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	// Perform health check
	result := m.performHealthCheck(node.Endpoint)

	// Determine health status
	status := HealthStatusHealthy
	if !result.Healthy {
		status = HealthStatusUnavailable
	} else if result.LatencyMs > 1000 {
		status = HealthStatusDegraded
	}

	// Update node health in database
	now := time.Now()
	_, err = m.db.ExecContext(ctx, `
		UPDATE cluster_nodes
		SET health_status = ?, last_health_check = ?, last_seen = ?, latency_ms = ?, updated_at = ?
		WHERE id = ?
	`, status, now, now, result.LatencyMs, now, nodeID)
	if err != nil {
		return nil, fmt.Errorf("failed to update node health: %w", err)
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

// performHealthCheck performs an HTTP health check on the given endpoint
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

	// Check response status
	if resp.StatusCode != http.StatusOK {
		return &HealthCheckResult{
			Healthy:      false,
			LatencyMs:    latency,
			ErrorMessage: fmt.Sprintf("unexpected status code: %d", resp.StatusCode),
		}
	}

	return &HealthCheckResult{
		Healthy:   true,
		LatencyMs: latency,
	}
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
