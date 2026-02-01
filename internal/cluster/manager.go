package cluster

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// Manager handles cluster operations
type Manager struct {
	db                  *sql.DB
	publicAPIURL        string
	healthCheckInterval time.Duration
	stopChan            chan struct{}
	log                 *logrus.Entry
	storage             storage.Backend
	aclManager          acl.Manager
}

// NewManager creates a new cluster manager
func NewManager(db *sql.DB, publicAPIURL string) *Manager {
	return &Manager{
		db:                  db,
		publicAPIURL:        publicAPIURL,
		healthCheckInterval: 30 * time.Second,
		stopChan:            make(chan struct{}),
		log:                 logrus.WithField("component", "cluster-manager"),
	}
}

// SetStorage sets the storage backend for the cluster manager
func (m *Manager) SetStorage(s storage.Backend) {
	m.storage = s
}

// SetACLManager sets the ACL manager for the cluster manager
func (m *Manager) SetACLManager(aclMgr acl.Manager) {
	m.aclManager = aclMgr
}

// InitializeCluster initializes a new cluster with this node
func (m *Manager) InitializeCluster(ctx context.Context, nodeName, region string) (string, error) {
	// Check if cluster is already initialized
	var exists int
	err := m.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM cluster_config").Scan(&exists)
	if err != nil && err != sql.ErrNoRows {
		return "", fmt.Errorf("failed to check cluster config: %w", err)
	}

	if exists > 0 {
		return "", fmt.Errorf("cluster already initialized")
	}

	// Generate node ID and cluster token
	nodeID := uuid.New().String()
	clusterToken, err := generateClusterToken()
	if err != nil {
		return "", fmt.Errorf("failed to generate cluster token: %w", err)
	}

	// Insert cluster config
	_, err = m.db.ExecContext(ctx, `
		INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled, region)
		VALUES (?, ?, ?, 1, ?)
	`, nodeID, nodeName, clusterToken, region)
	if err != nil {
		return "", fmt.Errorf("failed to initialize cluster: %w", err)
	}

	// Add this node to cluster_nodes table
	// Use public API URL from configuration as endpoint
	_, err = m.db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (
			id, name, endpoint, node_token, region, priority,
			health_status, latency_ms, capacity_total, capacity_used, bucket_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, nodeID, nodeName, m.publicAPIURL, clusterToken, region, 100,
		HealthStatusHealthy, 0, 0, 0, 0)
	if err != nil {
		return "", fmt.Errorf("failed to add node to cluster: %w", err)
	}

	m.log.WithFields(logrus.Fields{
		"node_id":   nodeID,
		"node_name": nodeName,
		"region":    region,
	}).Info("Cluster initialized")

	return clusterToken, nil
}

// JoinCluster joins an existing cluster
func (m *Manager) JoinCluster(ctx context.Context, clusterToken, nodeEndpoint string) error {
	// TODO: Implement cluster join logic
	// This would involve:
	// 1. Validate cluster token with another node
	// 2. Exchange node information
	// 3. Update cluster_config
	// 4. Add this node to the cluster
	return fmt.Errorf("not implemented yet")
}

// LeaveCluster removes this node from the cluster
func (m *Manager) LeaveCluster(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, `
		UPDATE cluster_config SET is_cluster_enabled = 0
	`)
	if err != nil {
		return fmt.Errorf("failed to leave cluster: %w", err)
	}

	m.log.Info("Left cluster")
	return nil
}

// IsClusterEnabled checks if cluster mode is enabled
func (m *Manager) IsClusterEnabled() bool {
	var enabled int
	err := m.db.QueryRow("SELECT is_cluster_enabled FROM cluster_config LIMIT 1").Scan(&enabled)
	if err != nil {
		return false
	}
	return enabled == 1
}

// GetConfig returns this node's cluster configuration
func (m *Manager) GetConfig(ctx context.Context) (*ClusterConfig, error) {
	var config ClusterConfig
	var isEnabled int

	err := m.db.QueryRowContext(ctx, `
		SELECT node_id, node_name, cluster_token, is_cluster_enabled, region, created_at
		FROM cluster_config
		LIMIT 1
	`).Scan(&config.NodeID, &config.NodeName, &config.ClusterToken, &isEnabled, &config.Region, &config.CreatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("cluster not initialized")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster config: %w", err)
	}

	config.IsClusterEnabled = isEnabled == 1
	return &config, nil
}

// AddNode adds a new node to the cluster
func (m *Manager) AddNode(ctx context.Context, node *Node) error {
	if node.ID == "" {
		node.ID = uuid.New().String()
	}

	now := time.Now()
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (
			id, name, endpoint, node_token, region, priority,
			health_status, latency_ms, capacity_total, capacity_used,
			bucket_count, metadata, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, node.ID, node.Name, node.Endpoint, node.NodeToken, node.Region, node.Priority,
		HealthStatusUnknown, 0, 0, 0, 0, node.Metadata, now, now)

	if err != nil {
		return fmt.Errorf("failed to add node: %w", err)
	}

	m.log.WithFields(logrus.Fields{
		"node_id":   node.ID,
		"node_name": node.Name,
		"endpoint":  node.Endpoint,
	}).Info("Node added to cluster")

	return nil
}

// GetNode retrieves a node by ID
func (m *Manager) GetNode(ctx context.Context, nodeID string) (*Node, error) {
	var node Node
	var lastHealthCheck, lastSeen sql.NullTime

	err := m.db.QueryRowContext(ctx, `
		SELECT id, name, endpoint, node_token, region, priority,
		       health_status, last_health_check, last_seen, latency_ms,
		       capacity_total, capacity_used, bucket_count, metadata,
		       created_at, updated_at
		FROM cluster_nodes
		WHERE id = ?
	`, nodeID).Scan(
		&node.ID, &node.Name, &node.Endpoint, &node.NodeToken, &node.Region, &node.Priority,
		&node.HealthStatus, &lastHealthCheck, &lastSeen, &node.LatencyMs,
		&node.CapacityTotal, &node.CapacityUsed, &node.BucketCount, &node.Metadata,
		&node.CreatedAt, &node.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("node not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get node: %w", err)
	}

	if lastHealthCheck.Valid {
		node.LastHealthCheck = &lastHealthCheck.Time
	}
	if lastSeen.Valid {
		node.LastSeen = &lastSeen.Time
	}

	return &node, nil
}

// ListNodes returns all nodes in the cluster
func (m *Manager) ListNodes(ctx context.Context) ([]*Node, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, name, endpoint, node_token, region, priority,
		       health_status, last_health_check, last_seen, latency_ms,
		       capacity_total, capacity_used, bucket_count, metadata,
		       created_at, updated_at
		FROM cluster_nodes
		ORDER BY priority ASC, name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		var node Node
		var lastHealthCheck, lastSeen sql.NullTime

		err := rows.Scan(
			&node.ID, &node.Name, &node.Endpoint, &node.NodeToken, &node.Region, &node.Priority,
			&node.HealthStatus, &lastHealthCheck, &lastSeen, &node.LatencyMs,
			&node.CapacityTotal, &node.CapacityUsed, &node.BucketCount, &node.Metadata,
			&node.CreatedAt, &node.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan node: %w", err)
		}

		if lastHealthCheck.Valid {
			node.LastHealthCheck = &lastHealthCheck.Time
		}
		if lastSeen.Valid {
			node.LastSeen = &lastSeen.Time
		}

		nodes = append(nodes, &node)
	}

	return nodes, nil
}

// UpdateNode updates node information
func (m *Manager) UpdateNode(ctx context.Context, node *Node) error {
	now := time.Now()
	_, err := m.db.ExecContext(ctx, `
		UPDATE cluster_nodes
		SET name = ?, endpoint = ?, region = ?, priority = ?, metadata = ?, updated_at = ?
		WHERE id = ?
	`, node.Name, node.Endpoint, node.Region, node.Priority, node.Metadata, now, node.ID)

	if err != nil {
		return fmt.Errorf("failed to update node: %w", err)
	}

	m.log.WithFields(logrus.Fields{
		"node_id": node.ID,
	}).Info("Node updated")

	return nil
}

// RemoveNode removes a node from the cluster
func (m *Manager) RemoveNode(ctx context.Context, nodeID string) error {
	_, err := m.db.ExecContext(ctx, "DELETE FROM cluster_nodes WHERE id = ?", nodeID)
	if err != nil {
		return fmt.Errorf("failed to remove node: %w", err)
	}

	m.log.WithFields(logrus.Fields{
		"node_id": nodeID,
	}).Info("Node removed from cluster")

	return nil
}

// GetHealthyNodes returns all healthy nodes
func (m *Manager) GetHealthyNodes(ctx context.Context) ([]*Node, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, name, endpoint, node_token, region, priority,
		       health_status, last_health_check, last_seen, latency_ms,
		       capacity_total, capacity_used, bucket_count, metadata,
		       created_at, updated_at
		FROM cluster_nodes
		WHERE health_status = ?
		ORDER BY priority ASC, name ASC
	`, HealthStatusHealthy)
	if err != nil {
		return nil, fmt.Errorf("failed to list healthy nodes: %w", err)
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		var node Node
		var lastHealthCheck, lastSeen sql.NullTime

		err := rows.Scan(
			&node.ID, &node.Name, &node.Endpoint, &node.NodeToken, &node.Region, &node.Priority,
			&node.HealthStatus, &lastHealthCheck, &lastSeen, &node.LatencyMs,
			&node.CapacityTotal, &node.CapacityUsed, &node.BucketCount, &node.Metadata,
			&node.CreatedAt, &node.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan node: %w", err)
		}

		if lastHealthCheck.Valid {
			node.LastHealthCheck = &lastHealthCheck.Time
		}
		if lastSeen.Valid {
			node.LastSeen = &lastSeen.Time
		}

		nodes = append(nodes, &node)
	}

	return nodes, nil
}

// GetClusterStatus returns overall cluster status
func (m *Manager) GetClusterStatus(ctx context.Context) (*ClusterStatus, error) {
	status := &ClusterStatus{
		IsEnabled:   m.IsClusterEnabled(),
		LastUpdated: time.Now(),
	}

	// Count nodes by health status
	err := m.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN health_status = ? THEN 1 ELSE 0 END), 0) as healthy,
			COALESCE(SUM(CASE WHEN health_status = ? THEN 1 ELSE 0 END), 0) as degraded,
			COALESCE(SUM(CASE WHEN health_status = ? THEN 1 ELSE 0 END), 0) as unavailable
		FROM cluster_nodes
	`, HealthStatusHealthy, HealthStatusDegraded, HealthStatusUnavailable).Scan(
		&status.TotalNodes,
		&status.HealthyNodes,
		&status.DegradedNodes,
		&status.UnavailableNodes,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster status: %w", err)
	}

	// TODO: Get bucket counts from bucket manager or replication manager
	// For now, set to 0
	status.TotalBuckets = 0
	status.ReplicatedBuckets = 0
	status.LocalBuckets = 0

	return status, nil
}

// generateClusterToken generates a secure random cluster token
func generateClusterToken() (string, error) {
	b := make([]byte, 32) // 256 bits
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// UpdateNodeBucketCount updates the bucket count for a specific node
func (m *Manager) UpdateNodeBucketCount(ctx context.Context, nodeID string, bucketCount int) error {
	_, err := m.db.ExecContext(ctx, `
		UPDATE cluster_nodes
		SET bucket_count = ?, updated_at = ?
		WHERE id = ?
	`, bucketCount, time.Now(), nodeID)

	if err != nil {
		return fmt.Errorf("failed to update bucket count: %w", err)
	}

	return nil
}

// UpdateLocalNodeBucketCount updates the bucket count for the local node
func (m *Manager) UpdateLocalNodeBucketCount(ctx context.Context, bucketCount int) error {
	config, err := m.GetConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to get cluster config: %w", err)
	}

	return m.UpdateNodeBucketCount(ctx, config.NodeID, bucketCount)
}

// GetNodeToken retrieves the node_token for a given node ID
// This is used for HMAC authentication in cluster replication
func (m *Manager) GetNodeToken(ctx context.Context, nodeID string) (string, error) {
	var nodeToken string
	err := m.db.QueryRowContext(ctx, `
		SELECT node_token FROM cluster_nodes WHERE id = ? AND health_status != 'removed'
	`, nodeID).Scan(&nodeToken)

	if err == sql.ErrNoRows {
		return "", fmt.Errorf("node not found: %s", nodeID)
	}
	if err != nil {
		return "", fmt.Errorf("failed to get node token: %w", err)
	}

	return nodeToken, nil
}

// GetLocalNodeID returns the ID of the local node
func (m *Manager) GetLocalNodeID(ctx context.Context) (string, error) {
	config, err := m.GetConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get cluster config: %w", err)
	}
	return config.NodeID, nil
}

// GetLocalNodeName returns the node_name of the local node
func (m *Manager) GetLocalNodeName(ctx context.Context) (string, error) {
	config, err := m.GetConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get cluster config: %w", err)
	}
	return config.NodeName, nil
}

// GetLocalNodeToken returns the node_token of the local node
// This is used for signing outgoing cluster replication requests
func (m *Manager) GetLocalNodeToken(ctx context.Context) (string, error) {
	nodeID, err := m.GetLocalNodeID(ctx)
	if err != nil {
		return "", err
	}
	return m.GetNodeToken(ctx, nodeID)
}

// Close stops the cluster manager
func (m *Manager) Close() error {
	if m.stopChan != nil {
		close(m.stopChan)
	}
	return nil
}
