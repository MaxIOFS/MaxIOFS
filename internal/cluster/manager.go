package cluster

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
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
	tlsConfig           *tls.Config
	clusterHTTPClient   *http.Client
	currentCert         atomic.Pointer[tls.Certificate]
}

// NewManager creates a new cluster manager
func NewManager(db *sql.DB, publicAPIURL string) *Manager {
	m := &Manager{
		db:                  db,
		publicAPIURL:        publicAPIURL,
		healthCheckInterval: 30 * time.Second,
		stopChan:            make(chan struct{}),
		log:                 logrus.WithField("component", "cluster-manager"),
		clusterHTTPClient:   &http.Client{Timeout: 10 * time.Second},
	}

	// Try to load TLS config from DB (if cluster already initialized with certs)
	if err := m.loadTLSConfig(); err != nil {
		m.log.WithError(err).Debug("No TLS config loaded (cluster may not be initialized yet)")
	}

	return m
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

	// Generate internal CA and node certificate for inter-node TLS
	caCertPEM, caKeyPEM, err := GenerateCA()
	if err != nil {
		return "", fmt.Errorf("failed to generate internal CA: %w", err)
	}

	nodeCertPEM, nodeKeyPEM, err := GenerateNodeCert(caCertPEM, caKeyPEM, nodeName)
	if err != nil {
		return "", fmt.Errorf("failed to generate node certificate: %w", err)
	}

	// Insert cluster config with TLS certs
	_, err = m.db.ExecContext(ctx, `
		INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled, region, ca_cert, ca_key, node_cert, node_key)
		VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?)
	`, nodeID, nodeName, clusterToken, region, string(caCertPEM), string(caKeyPEM), string(nodeCertPEM), string(nodeKeyPEM))
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

	// Load TLS config into memory
	if err := m.loadTLSConfig(); err != nil {
		m.log.WithError(err).Warn("Failed to load TLS config after initialization")
	} else {
		m.log.Info("Inter-node TLS enabled with auto-generated certificates")
	}

	return clusterToken, nil
}

// JoinCluster joins an existing cluster
func (m *Manager) JoinCluster(ctx context.Context, clusterToken, nodeEndpoint string) error {
	// Step 1: Validate cluster token with the existing cluster node (also receives CA cert+key)
	valid, nodeInfo, caCertPEM, caKeyPEM, err := m.validateClusterToken(ctx, clusterToken, nodeEndpoint)
	if err != nil {
		return fmt.Errorf("failed to validate cluster token: %w", err)
	}
	if !valid {
		return fmt.Errorf("invalid cluster token")
	}

	m.log.WithFields(logrus.Fields{
		"cluster_id": nodeInfo.ClusterID,
		"region":     nodeInfo.Region,
	}).Info("Cluster token validated successfully")

	// Step 2: Generate node information for this node
	thisNodeID := uuid.New().String()
	thisNodeToken, err := generateClusterToken()
	if err != nil {
		return fmt.Errorf("failed to generate node token: %w", err)
	}
	thisNodeName := fmt.Sprintf("node-%s", thisNodeID[:8])

	// Step 3: Register this node with the existing cluster node
	registeredNode, err := m.registerWithCluster(ctx, nodeEndpoint, clusterToken, &Node{
		ID:        thisNodeID,
		Name:      thisNodeName,
		Endpoint:  m.publicAPIURL,
		NodeToken: thisNodeToken,
		Region:    nodeInfo.Region,
		Priority:  5, // Default priority
	})
	if err != nil {
		return fmt.Errorf("failed to register with cluster: %w", err)
	}

	m.log.WithFields(logrus.Fields{
		"node_id":   registeredNode.ID,
		"node_name": registeredNode.Name,
	}).Info("Successfully registered with cluster")

	// Step 3.5: Generate node certificate using the CA received from the cluster
	var nodeCertPEM, nodeKeyPEM string
	if caCertPEM != "" && caKeyPEM != "" {
		certPEM, keyPEM, err := GenerateNodeCert([]byte(caCertPEM), []byte(caKeyPEM), thisNodeName)
		if err != nil {
			m.log.WithError(err).Warn("Failed to generate node certificate during join")
		} else {
			nodeCertPEM = string(certPEM)
			nodeKeyPEM = string(keyPEM)
			m.log.Info("Generated node certificate signed by cluster CA")
		}
	}

	// Step 4: Update local cluster_config to enable cluster mode
	// Delete any existing config and insert new one (since node_id is primary key)
	_, err = m.db.ExecContext(ctx, `DELETE FROM cluster_config`)
	if err != nil {
		return fmt.Errorf("failed to clear cluster config: %w", err)
	}

	_, err = m.db.ExecContext(ctx, `
		INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled, region, ca_cert, ca_key, node_cert, node_key)
		VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?)
	`, thisNodeID, thisNodeName, clusterToken, nodeInfo.Region, caCertPEM, caKeyPEM, nodeCertPEM, nodeKeyPEM)

	if err != nil {
		return fmt.Errorf("failed to update cluster config: %w", err)
	}

	// Step 5: Fetch and store all other nodes from the cluster
	nodes, err := m.fetchClusterNodes(ctx, nodeEndpoint, clusterToken)
	if err != nil {
		m.log.WithError(err).Warn("Failed to fetch cluster nodes, will sync later")
	} else {
		for _, node := range nodes {
			// Skip self
			if node.ID == thisNodeID {
				continue
			}
			// Add each node to local cluster_nodes table
			if err := m.AddNode(ctx, node); err != nil {
				m.log.WithError(err).WithField("node_id", node.ID).Warn("Failed to add node to local registry")
			}
		}
		m.log.WithField("node_count", len(nodes)-1).Info("Synchronized cluster nodes")
	}

	// Load TLS config into memory after join
	if err := m.loadTLSConfig(); err != nil {
		m.log.WithError(err).Warn("Failed to load TLS config after joining cluster")
	} else if m.tlsConfig != nil {
		m.log.Info("Inter-node TLS enabled after joining cluster")
	}

	m.log.WithFields(logrus.Fields{
		"node_id":     thisNodeID,
		"node_name":   thisNodeName,
		"cluster_id":  nodeInfo.ClusterID,
	}).Info("Successfully joined cluster")

	return nil
}

// validateClusterToken validates a cluster token with an existing cluster node.
// Returns validity, cluster info, CA cert PEM, and CA key PEM (for TLS setup on join).
func (m *Manager) validateClusterToken(ctx context.Context, clusterToken, nodeEndpoint string) (bool, *ClusterInfo, string, string, error) {
	// Build URL for validation endpoint
	url := fmt.Sprintf("%s/api/internal/cluster/validate-token", nodeEndpoint)

	// Create request payload
	payload := map[string]string{
		"cluster_token": clusterToken,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return false, nil, "", "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return false, nil, "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Execute request (use insecure TLS for join — we don't have the CA cert yet)
	client := m.insecureHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return false, nil, "", "", fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusUnauthorized {
			return false, nil, "", "", fmt.Errorf("invalid cluster token")
		}
		return false, nil, "", "", fmt.Errorf("validation failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var response struct {
		Valid       bool         `json:"valid"`
		ClusterInfo *ClusterInfo `json:"cluster_info"`
		CACert      string       `json:"ca_cert"`
		CAKey       string       `json:"ca_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return false, nil, "", "", fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Valid, response.ClusterInfo, response.CACert, response.CAKey, nil
}

// registerWithCluster registers this node with an existing cluster node
func (m *Manager) registerWithCluster(ctx context.Context, nodeEndpoint, clusterToken string, node *Node) (*Node, error) {
	// Build URL for node registration endpoint
	url := fmt.Sprintf("%s/api/internal/cluster/register-node", nodeEndpoint)

	// Create request payload
	payload := map[string]interface{}{
		"cluster_token": clusterToken,
		"node":          node,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Execute request (use insecure TLS for join — we don't have the CA cert yet)
	client := m.insecureHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registration failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response — includes CA cert from the existing cluster
	var response struct {
		Node   *Node  `json:"node"`
		CACert string `json:"ca_cert"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Node, nil
}

// fetchClusterNodes fetches all nodes from an existing cluster node
func (m *Manager) fetchClusterNodes(ctx context.Context, nodeEndpoint, clusterToken string) ([]*Node, error) {
	// Build URL for nodes list endpoint
	url := fmt.Sprintf("%s/api/internal/cluster/nodes?cluster_token=%s", nodeEndpoint, clusterToken)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request (use insecure TLS for join — we don't have the CA cert yet)
	client := m.insecureHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch nodes with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var response struct {
		Nodes []*Node `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Nodes, nil
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
	var lastHealthCheck, lastSeen, lastLocalWriteAt sql.NullTime

	err := m.db.QueryRowContext(ctx, `
		SELECT id, name, endpoint, node_token, region, priority,
		       health_status, last_health_check, last_seen, latency_ms,
		       capacity_total, capacity_used, bucket_count, metadata,
		       created_at, updated_at, is_stale, last_local_write_at
		FROM cluster_nodes
		WHERE id = ?
	`, nodeID).Scan(
		&node.ID, &node.Name, &node.Endpoint, &node.NodeToken, &node.Region, &node.Priority,
		&node.HealthStatus, &lastHealthCheck, &lastSeen, &node.LatencyMs,
		&node.CapacityTotal, &node.CapacityUsed, &node.BucketCount, &node.Metadata,
		&node.CreatedAt, &node.UpdatedAt, &node.IsStale, &lastLocalWriteAt,
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
	if lastLocalWriteAt.Valid {
		node.LastLocalWriteAt = &lastLocalWriteAt.Time
	}

	return &node, nil
}

// ListNodes returns all nodes in the cluster
func (m *Manager) ListNodes(ctx context.Context) ([]*Node, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, name, endpoint, node_token, region, priority,
		       health_status, last_health_check, last_seen, latency_ms,
		       capacity_total, capacity_used, bucket_count, metadata,
		       created_at, updated_at, is_stale, last_local_write_at
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
		var lastHealthCheck, lastSeen, lastLocalWriteAt sql.NullTime

		err := rows.Scan(
			&node.ID, &node.Name, &node.Endpoint, &node.NodeToken, &node.Region, &node.Priority,
			&node.HealthStatus, &lastHealthCheck, &lastSeen, &node.LatencyMs,
			&node.CapacityTotal, &node.CapacityUsed, &node.BucketCount, &node.Metadata,
			&node.CreatedAt, &node.UpdatedAt, &node.IsStale, &lastLocalWriteAt,
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
		if lastLocalWriteAt.Valid {
			node.LastLocalWriteAt = &lastLocalWriteAt.Time
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
		       created_at, updated_at, is_stale, last_local_write_at
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
		var lastHealthCheck, lastSeen, lastLocalWriteAt sql.NullTime

		err := rows.Scan(
			&node.ID, &node.Name, &node.Endpoint, &node.NodeToken, &node.Region, &node.Priority,
			&node.HealthStatus, &lastHealthCheck, &lastSeen, &node.LatencyMs,
			&node.CapacityTotal, &node.CapacityUsed, &node.BucketCount, &node.Metadata,
			&node.CreatedAt, &node.UpdatedAt, &node.IsStale, &lastLocalWriteAt,
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
		if lastLocalWriteAt.Valid {
			node.LastLocalWriteAt = &lastLocalWriteAt.Time
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

// FetchJWTSecretFromNode fetches the JWT secret from a cluster node using HMAC authentication
func (m *Manager) FetchJWTSecretFromNode(ctx context.Context, nodeEndpoint string) (string, error) {
	// Get local node credentials for HMAC signing
	localNodeID, err := m.GetLocalNodeID(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get local node ID: %w", err)
	}
	localNodeToken, err := m.GetLocalNodeToken(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get local node token: %w", err)
	}

	// Create HMAC-authenticated request
	targetURL := fmt.Sprintf("%s/api/internal/cluster/jwt-secret", nodeEndpoint)
	proxy := NewProxyClient(m.GetTLSConfig())
	req, err := proxy.CreateAuthenticatedRequest(ctx, "GET", targetURL, nil, localNodeID, localNodeToken)
	if err != nil {
		return "", fmt.Errorf("failed to create authenticated request: %w", err)
	}

	resp, err := proxy.DoAuthenticatedRequest(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch JWT secret from node: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to fetch JWT secret: status %d: %s", resp.StatusCode, string(body))
	}

	var response struct {
		JWTSecret string `json:"jwt_secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to decode JWT secret response: %w", err)
	}

	if response.JWTSecret == "" {
		return "", fmt.Errorf("empty JWT secret returned from node")
	}

	return response.JWTSecret, nil
}

// GetTLSConfig returns the cluster TLS config, or nil if TLS is not configured.
func (m *Manager) GetTLSConfig() *tls.Config {
	return m.tlsConfig
}

// GetCACertPEM returns the PEM-encoded CA certificate from the database.
func (m *Manager) GetCACertPEM() string {
	var caCert string
	err := m.db.QueryRow("SELECT ca_cert FROM cluster_config LIMIT 1").Scan(&caCert)
	if err != nil {
		return ""
	}
	return caCert
}

// GetCAKeyPEM returns the PEM-encoded CA private key from the database.
func (m *Manager) GetCAKeyPEM() string {
	var caKey string
	err := m.db.QueryRow("SELECT ca_key FROM cluster_config LIMIT 1").Scan(&caKey)
	if err != nil {
		return ""
	}
	return caKey
}

// loadTLSConfig loads TLS certificates from the database and builds the TLS config.
func (m *Manager) loadTLSConfig() error {
	var caCert, caKey, nodeCert, nodeKey string
	err := m.db.QueryRow("SELECT ca_cert, ca_key, node_cert, node_key FROM cluster_config LIMIT 1").Scan(
		&caCert, &caKey, &nodeCert, &nodeKey,
	)
	if err != nil {
		return fmt.Errorf("failed to load TLS certs from DB: %w", err)
	}

	if caCert == "" || nodeCert == "" || nodeKey == "" {
		return fmt.Errorf("TLS certificates not configured")
	}

	tlsCfg, err := BuildClusterTLSConfig([]byte(caCert), []byte(nodeCert), []byte(nodeKey), &m.currentCert)
	if err != nil {
		return fmt.Errorf("failed to build TLS config: %w", err)
	}

	m.tlsConfig = tlsCfg
	m.clusterHTTPClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
		},
	}

	return nil
}

// insecureHTTPClient returns an HTTP client that skips TLS verification.
// Used during the initial join handshake before we have the cluster CA cert.
func (m *Manager) insecureHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: InsecureClusterTLSConfig(),
		},
	}
}

// StartCertRenewal starts a background goroutine that checks monthly for cert renewal.
func (m *Manager) StartCertRenewal(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(30 * 24 * time.Hour) // Monthly
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-m.stopChan:
				return
			case <-ticker.C:
				m.checkAndRenewCert()
			}
		}
	}()
}

// checkAndRenewCert checks if the node certificate is expiring soon and renews it.
func (m *Manager) checkAndRenewCert() {
	var nodeCert, caCert, caKey string
	err := m.db.QueryRow("SELECT node_cert, ca_cert, ca_key FROM cluster_config LIMIT 1").Scan(
		&nodeCert, &caCert, &caKey,
	)
	if err != nil || nodeCert == "" || caCert == "" || caKey == "" {
		return
	}

	expiring, err := IsCertExpiringSoon([]byte(nodeCert), 30)
	if err != nil {
		m.log.WithError(err).Warn("Failed to check certificate expiry")
		return
	}

	if !expiring {
		return
	}

	m.log.Info("Node certificate expiring soon, renewing...")

	// Get node name for the new cert
	var nodeName string
	if err := m.db.QueryRow("SELECT node_name FROM cluster_config LIMIT 1").Scan(&nodeName); err != nil {
		m.log.WithError(err).Error("Failed to get node name for cert renewal")
		return
	}

	// Generate new node cert
	newCertPEM, newKeyPEM, err := GenerateNodeCert([]byte(caCert), []byte(caKey), nodeName)
	if err != nil {
		m.log.WithError(err).Error("Failed to generate renewed node certificate")
		return
	}

	// Store in DB
	_, err = m.db.Exec("UPDATE cluster_config SET node_cert = ?, node_key = ?",
		string(newCertPEM), string(newKeyPEM))
	if err != nil {
		m.log.WithError(err).Error("Failed to store renewed certificate in database")
		return
	}

	// Hot-swap the cert via atomic pointer
	newCert, err := ParseCertKeyPEM(newCertPEM, newKeyPEM)
	if err != nil {
		m.log.WithError(err).Error("Failed to parse renewed certificate")
		return
	}
	m.currentCert.Store(newCert)

	m.log.Info("Node certificate renewed successfully")

	// Check if CA cert is expiring within 1 year and log a warning
	caExpiring, err := IsCertExpiringSoon([]byte(caCert), 365)
	if err == nil && caExpiring {
		m.log.Warn("Cluster CA certificate is expiring within 1 year — consider regenerating via admin endpoint")
	}
}

// Close stops the cluster manager
func (m *Manager) Close() error {
	if m.stopChan != nil {
		close(m.stopChan)
	}
	return nil
}
