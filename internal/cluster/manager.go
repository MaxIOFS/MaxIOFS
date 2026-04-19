package cluster

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"fmt"
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
	clusterURL          string // cluster inter-node URL (scheme://host:clusterPort)
	healthCheckInterval time.Duration
	stopChan            chan struct{}
	log                 *logrus.Entry
	storage             storage.Backend
	aclManager          acl.Manager
	bucketManager       bucketManagerForMigration
	tlsConfig           *tls.Config
	clusterHTTPClient   *http.Client
	currentCert         atomic.Pointer[tls.Certificate]
	readCounter         uint64 // atomic — round-robin read balancing
}

// bucketManagerForMigration is the minimal bucket.Manager interface needed for source deletion.
type bucketManagerForMigration interface {
	ForceDeleteBucket(ctx context.Context, tenantID, name string) error
}

// NewManager creates a new cluster manager.
// publicAPIURL is the S3 API URL (port 8080); clusterURL is the inter-node URL (port 8082).
func NewManager(db *sql.DB, publicAPIURL, clusterURL string) *Manager {
	m := &Manager{
		db:                  db,
		publicAPIURL:        publicAPIURL,
		clusterURL:          clusterURL,
		healthCheckInterval: 30 * time.Second,
		stopChan:            make(chan struct{}),
		log:                 logrus.WithField("component", "cluster-manager"),
		clusterHTTPClient:   &http.Client{Timeout: 10 * time.Second},
	}

	// Try to load TLS config from DB (if cluster already initialized with certs)
	if err := m.loadTLSConfig(); err != nil {
		m.log.WithError(err).Debug("No TLS config loaded (cluster may not be initialized yet)")
	}

	// Repair any cluster_nodes rows with empty node_token — can happen when nodes were
	// joined with an older binary that stripped NodeToken during JSON serialization.
	// All nodes in a cluster share the same cluster_token, so it is safe to back-fill.
	m.repairEmptyNodeTokens()

	return m
}

// repairEmptyNodeTokens fills node_token for any cluster_nodes row where it is empty,
// using the cluster_token from cluster_config. This heals deployments that were joined
// before the JoinPackageNode fix was in place.
func (m *Manager) repairEmptyNodeTokens() {
	var clusterToken string
	if err := m.db.QueryRow("SELECT cluster_token FROM cluster_config LIMIT 1").Scan(&clusterToken); err != nil || clusterToken == "" {
		return // cluster not initialized yet — nothing to repair
	}

	result, err := m.db.Exec(
		"UPDATE cluster_nodes SET node_token = ? WHERE node_token = '' OR node_token IS NULL",
		clusterToken,
	)
	if err != nil {
		m.log.WithError(err).Warn("Failed to repair empty node tokens")
		return
	}
	if n, _ := result.RowsAffected(); n > 0 {
		m.log.WithField("rows_fixed", n).Info("Repaired cluster nodes with missing node_token")
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

// SetBucketManager sets the bucket manager used for source deletion during migrations.
func (m *Manager) SetBucketManager(bm bucketManagerForMigration) {
	m.bucketManager = bm
}

// InitializeCluster initializes a new cluster with this node.
// nodeEndpoint is the cluster inter-node address (scheme://ip:clusterPort) other nodes will use to reach this node.
func (m *Manager) InitializeCluster(ctx context.Context, nodeName, region, nodeEndpoint string) (string, error) {
	if nodeEndpoint == "" {
		return "", fmt.Errorf("node cluster endpoint is required")
	}
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

	nodeCertPEM, nodeKeyPEM, err := GenerateNodeCert(caCertPEM, caKeyPEM, nodeName, nodeEndpoint)
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
	_, err = m.db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (
			id, name, endpoint, api_url, node_token, region, priority,
			health_status, latency_ms, capacity_total, capacity_used, bucket_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, nodeID, nodeName, nodeEndpoint, m.publicAPIURL, clusterToken, region, 100,
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

// JoinPackageNode is the wire format for a Node inside a ClusterJoinPackage.
// It explicitly includes node_token (unlike Node, which has json:"-" to prevent
// token leakage through the public console API). Without the token, the receiving
// node cannot verify HMAC signatures from the senders in the existing cluster.
type JoinPackageNode struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	APIURL    string `json:"api_url,omitempty"`
	NodeToken string `json:"node_token"`
	Region    string `json:"region"`
	Priority  int    `json:"priority"`
}

// NodesToJoinPackage converts a slice of *Node into []*JoinPackageNode, preserving
// the NodeToken that json:"-" would otherwise strip during serialization.
func NodesToJoinPackage(nodes []*Node) []*JoinPackageNode {
	out := make([]*JoinPackageNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, &JoinPackageNode{
			ID:        n.ID,
			Name:      n.Name,
			Endpoint:  n.Endpoint,
			APIURL:    n.APIURL,
			NodeToken: n.NodeToken,
			Region:    n.Region,
			Priority:  n.Priority,
		})
	}
	return out
}

// ClusterJoinPackage is the complete join payload that Node A pushes to Node B via port 8081.
// Node B uses the CA key to generate its OWN cert+key (with its own IP in the SANs),
// so every node has a cert valid for its own address, all signed by the same CA.
type ClusterJoinPackage struct {
	NodeID       string             `json:"node_id"`
	NodeName     string             `json:"node_name"`
	ClusterToken string             `json:"cluster_token"`
	Region       string             `json:"region"`
	CACertPEM    string             `json:"ca_cert"`
	CAKeyPEM     string             `json:"ca_key"` // sent once so Node B can sign its own cert
	JWTSecret    string             `json:"jwt_secret"`
	SelfEndpoint string             `json:"self_endpoint"` // Node B's 8082 URL — used for cert SANs
	NodeEndpoint string             `json:"node_endpoint"` // Node A's 8082 URL
	APIURL       string             `json:"api_url"`       // Node B's S3 API public URL
	Nodes        []*JoinPackageNode `json:"nodes"`
}

// AcceptClusterJoin applies a ClusterJoinPackage sent by Node A.
// Generates this node's own TLS cert+key using the cluster CA (with SelfEndpoint IP in the SANs),
// stores config in DB, and loads TLS into memory.
func (m *Manager) AcceptClusterJoin(ctx context.Context, pkg *ClusterJoinPackage) error {
	if pkg.ClusterToken == "" || pkg.CACertPEM == "" || pkg.CAKeyPEM == "" || pkg.SelfEndpoint == "" {
		return fmt.Errorf("incomplete join package: missing required fields")
	}

	// Generate this node's own cert+key. SelfEndpoint carries the node's real IP so
	// the cert SANs are correct for TLS verification by any peer in the cluster.
	nodeCertPEM, nodeKeyPEM, err := GenerateNodeCert([]byte(pkg.CACertPEM), []byte(pkg.CAKeyPEM), pkg.NodeName, pkg.SelfEndpoint)
	if err != nil {
		return fmt.Errorf("failed to generate node certificate: %w", err)
	}

	_, err = m.db.ExecContext(ctx, `DELETE FROM cluster_config`)
	if err != nil {
		return fmt.Errorf("failed to clear cluster config: %w", err)
	}

	// Remove all global users (tenant_id IS NULL or empty) from this node so the cluster
	// primary can sync them with the correct UUIDs. Without this, the local admin user
	// conflicts with the primary's admin by username UNIQUE constraint, blocking all user sync.
	// Tenant-scoped users are left untouched; they will be synced separately.
	if _, err := m.db.ExecContext(ctx, `DELETE FROM users WHERE tenant_id IS NULL OR tenant_id = ''`); err != nil {
		m.log.WithError(err).Warn("Failed to clear local global users before join — user sync may fail")
	}
	// Remove stale sync tracking for those users so the primary syncs them fresh.
	if _, err := m.db.ExecContext(ctx, `DELETE FROM cluster_user_sync WHERE user_id NOT IN (SELECT id FROM users)`); err != nil {
		m.log.WithError(err).Warn("Failed to clean up stale user sync records")
	}

	// Store CA cert but NOT the CA key — this node doesn't need to sign more certs.
	_, err = m.db.ExecContext(ctx, `
		INSERT INTO cluster_config (node_id, node_name, cluster_token, is_cluster_enabled, region, ca_cert, ca_key, node_cert, node_key)
		VALUES (?, ?, ?, 1, ?, ?, '', ?, ?)
	`, pkg.NodeID, pkg.NodeName, pkg.ClusterToken, pkg.Region, pkg.CACertPEM, string(nodeCertPEM), string(nodeKeyPEM))
	if err != nil {
		return fmt.Errorf("failed to store cluster config: %w", err)
	}

	// Add all existing cluster nodes (convert JoinPackageNode → Node to preserve NodeToken)
	for _, jn := range pkg.Nodes {
		if jn == nil || jn.ID == pkg.NodeID {
			continue
		}
		node := &Node{
			ID:        jn.ID,
			Name:      jn.Name,
			Endpoint:  jn.Endpoint,
			APIURL:    jn.APIURL,
			NodeToken: jn.NodeToken,
			Region:    jn.Region,
			Priority:  jn.Priority,
		}
		if err := m.AddNode(ctx, node); err != nil {
			m.log.WithError(err).WithField("node_id", jn.ID).Warn("Failed to add node during join")
		}
	}

	// Add this node itself to cluster_nodes so it appears in ListNodes()
	_, err = m.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO cluster_nodes (
			id, name, endpoint, api_url, node_token, region, priority,
			health_status, latency_ms, capacity_total, capacity_used, bucket_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, pkg.NodeID, pkg.NodeName, pkg.SelfEndpoint, pkg.APIURL, pkg.ClusterToken, pkg.Region, 5,
		HealthStatusHealthy, 0, 0, 0, 0)
	if err != nil {
		m.log.WithError(err).Warn("Failed to add self to cluster_nodes during join")
	}

	if err := m.loadTLSConfig(); err != nil {
		return fmt.Errorf("failed to load TLS config after join: %w", err)
	}

	m.log.WithField("node_id", pkg.NodeID).Info("Cluster join package applied, TLS ready")
	return nil
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

// AddNode adds a new node to the cluster (or updates key fields if it already exists).
// Uses INSERT ... ON CONFLICT to preserve health_status, latency, capacity, etc.
func (m *Manager) AddNode(ctx context.Context, node *Node) error {
	if node.ID == "" {
		node.ID = uuid.New().String()
	}

	now := time.Now()
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO cluster_nodes (
			id, name, endpoint, api_url, node_token, region, priority,
			health_status, latency_ms, capacity_total, capacity_used,
			bucket_count, metadata, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name       = excluded.name,
			endpoint   = excluded.endpoint,
			api_url    = CASE WHEN excluded.api_url != '' THEN excluded.api_url ELSE cluster_nodes.api_url END,
			node_token = CASE WHEN excluded.node_token != '' THEN excluded.node_token ELSE cluster_nodes.node_token END,
			region     = excluded.region,
			priority   = excluded.priority,
			updated_at = excluded.updated_at
	`, node.ID, node.Name, node.Endpoint, node.APIURL, node.NodeToken, node.Region, node.Priority,
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
		SELECT id, name, endpoint, api_url, node_token, region, priority,
		       health_status, last_health_check, last_seen, latency_ms,
		       capacity_total, capacity_used, bucket_count, metadata,
		       created_at, updated_at, is_stale, last_local_write_at
		FROM cluster_nodes
		WHERE id = ?
	`, nodeID).Scan(
		&node.ID, &node.Name, &node.Endpoint, &node.APIURL, &node.NodeToken, &node.Region, &node.Priority,
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
		SELECT id, name, endpoint, api_url, node_token, region, priority,
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
			&node.ID, &node.Name, &node.Endpoint, &node.APIURL, &node.NodeToken, &node.Region, &node.Priority,
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
		SET name = ?, region = ?, priority = ?, metadata = ?, updated_at = ?
		WHERE id = ?
	`, node.Name, node.Region, node.Priority, node.Metadata, now, node.ID)

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
		SELECT id, name, endpoint, api_url, node_token, region, priority,
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
			&node.ID, &node.Name, &node.Endpoint, &node.APIURL, &node.NodeToken, &node.Region, &node.Priority,
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

// GetReadyReplicaNodes returns healthy non-local nodes that have a completed initial sync.
// These nodes are safe to serve reads — they have a full copy of all objects.
func (m *Manager) GetReadyReplicaNodes(ctx context.Context) ([]*Node, error) {
	localID, err := m.GetLocalNodeID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := m.db.QueryContext(ctx, `
		SELECT DISTINCT cn.id, cn.name, cn.endpoint, cn.api_url, cn.node_token, cn.region, cn.priority,
		       cn.health_status, cn.last_health_check, cn.last_seen, cn.latency_ms,
		       cn.capacity_total, cn.capacity_used, cn.bucket_count, cn.metadata,
		       cn.created_at, cn.updated_at, cn.is_stale, cn.last_local_write_at
		FROM cluster_nodes cn
		INNER JOIN ha_sync_jobs hsj ON hsj.target_node_id = cn.id
		WHERE cn.id != ? AND cn.health_status = ? AND hsj.status = ?
		ORDER BY cn.priority ASC, cn.name ASC
	`, localID, HealthStatusHealthy, SyncJobDone)
	if err != nil {
		return nil, fmt.Errorf("get ready replica nodes: %w", err)
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		var node Node
		var lastHealthCheck, lastSeen, lastLocalWriteAt sql.NullTime
		err := rows.Scan(
			&node.ID, &node.Name, &node.Endpoint, &node.APIURL, &node.NodeToken, &node.Region, &node.Priority,
			&node.HealthStatus, &lastHealthCheck, &lastSeen, &node.LatencyMs,
			&node.CapacityTotal, &node.CapacityUsed, &node.BucketCount, &node.Metadata,
			&node.CreatedAt, &node.UpdatedAt, &node.IsStale, &lastLocalWriteAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan ready replica node: %w", err)
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
	return nodes, rows.Err()
}

// SelectReadNode selects the best node to serve a read request for the given bucket.
// Returns nil when the local node should handle the request.
// With factor > 1 and ready replicas, the local node and all ready replicas participate
// in round-robin to distribute read load evenly.
func (m *Manager) SelectReadNode(ctx context.Context, bucket string) (*Node, error) {
	if !m.IsClusterEnabled() {
		return nil, nil
	}
	factor, err := m.GetReplicationFactor(ctx)
	if err != nil || factor <= 1 {
		return nil, nil
	}
	replicas, err := m.GetReadyReplicaNodes(ctx)
	if err != nil || len(replicas) == 0 {
		return nil, nil
	}
	// Slot 0 = local node; slots 1..N = replicas.
	total := uint64(1 + len(replicas))
	idx := atomic.AddUint64(&m.readCounter, 1) % total
	if idx == 0 {
		return nil, nil // local node's turn
	}
	return replicas[idx-1], nil
}

// ProxyRead forwards a client read request to the given replica node and copies the
// response back.  If the replica returns 404 the caller should retry locally.
func (m *Manager) ProxyRead(ctx context.Context, w http.ResponseWriter, r *http.Request, node *Node) error {
	client := NewProxyClient(m.GetTLSConfig())
	resp, err := client.ProxyRequest(ctx, node, r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return client.CopyResponseToWriter(w, resp)
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

// GetTLSConfig returns the cluster TLS config for outbound client connections,
// or nil if TLS is not configured.
func (m *Manager) GetTLSConfig() *tls.Config {
	return m.tlsConfig
}

// GetServerTLSConfig returns a TLS config for the cluster server listener.
// It always returns a valid config: a temporary self-signed cert is used before
// the cluster is initialized, and the real CA-signed cert is served afterward via
// the atomic hot-swap mechanism — no listener restart required.
func (m *Manager) GetServerTLSConfig() (*tls.Config, error) {
	return BuildServerTLSConfig(&m.currentCert)
}

// WaitForNodeReady polls the given health URL using the cluster TLS client until it
// responds successfully or timeoutSeconds elapses. Used by Node A to confirm Node B's
// 8082 TLS server is up after the async startup triggered by the join response.
func (m *Manager) WaitForNodeReady(ctx context.Context, healthURL string, timeoutSeconds int) error {
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		resp, err := m.clusterHTTPClient.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("node did not become ready within %d seconds", timeoutSeconds)
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
	newCertPEM, newKeyPEM, err := GenerateNodeCert([]byte(caCert), []byte(caKey), nodeName, m.publicAPIURL)
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

// GetReplicationFactor returns the cluster-wide replication factor (1, 2, or 3).
// Returns 1 (no replication) if not set.
func (m *Manager) GetReplicationFactor(ctx context.Context) (int, error) {
	val, err := GetGlobalConfig(ctx, m.db, "ha.replication_factor")
	if err != nil {
		// Not set yet — default to 1
		return 1, nil
	}
	factor := 1
	fmt.Sscanf(val, "%d", &factor)
	if factor < 1 || factor > 3 {
		factor = 1
	}
	return factor, nil
}

// GetLocalTenantStorage returns the current_storage_bytes for a tenant from the
// local node's DB only.  Used by QuotaAggregator to avoid double-counting when
// HA replication is active (every node holds the same data).
func (m *Manager) GetLocalTenantStorage(ctx context.Context, tenantID string) (int64, error) {
	var bytes int64
	err := m.db.QueryRowContext(ctx,
		`SELECT current_storage_bytes FROM tenants WHERE id = ?`, tenantID).Scan(&bytes)
	if err == sql.ErrNoRows {
		return 0, nil // tenant not found — no storage used
	}
	return bytes, err
}

// SetReplicationFactor persists the cluster-wide replication factor.
// Callers must validate space before calling this.
func (m *Manager) SetReplicationFactor(ctx context.Context, factor int) error {
	if factor < 1 || factor > 3 {
		return fmt.Errorf("invalid replication factor %d: must be 1, 2, or 3", factor)
	}
	return SetGlobalConfig(ctx, m.db, "ha.replication_factor", fmt.Sprintf("%d", factor))
}
