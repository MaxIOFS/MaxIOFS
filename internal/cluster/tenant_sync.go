package cluster

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

// TenantData represents tenant information to be synchronized
type TenantData struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	DisplayName         string            `json:"display_name"`
	Description         string            `json:"description"`
	Status              string            `json:"status"`
	MaxAccessKeys       int               `json:"max_access_keys"`
	MaxStorageBytes     int64             `json:"max_storage_bytes"`
	CurrentStorageBytes int64             `json:"current_storage_bytes"`
	MaxBuckets          int               `json:"max_buckets"`
	CurrentBuckets      int               `json:"current_buckets"`
	Metadata            map[string]string `json:"metadata"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

// TenantSyncManager handles automatic tenant synchronization between cluster nodes
type TenantSyncManager struct {
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	stopChan       chan struct{}
	log            *logrus.Entry
}

// NewTenantSyncManager creates a new tenant sync manager
func NewTenantSyncManager(db *sql.DB, clusterManager *Manager) *TenantSyncManager {
	return &TenantSyncManager{
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    NewProxyClient(),
		stopChan:       make(chan struct{}),
		log:            logrus.WithField("component", "tenant-sync"),
	}
}

// Start begins the tenant synchronization loop
func (m *TenantSyncManager) Start(ctx context.Context) {
	// Get sync interval from config
	intervalStr, err := GetGlobalConfig(ctx, m.db, "tenant_sync_interval_seconds")
	if err != nil {
		m.log.WithError(err).Warn("Failed to get tenant sync interval, using default 30s")
		intervalStr = "30"
	}

	interval, err := strconv.Atoi(intervalStr)
	if err != nil {
		m.log.WithError(err).Warn("Invalid tenant sync interval, using default 30s")
		interval = 30
	}

	// Check if auto tenant sync is enabled
	enabledStr, err := GetGlobalConfig(ctx, m.db, "auto_tenant_sync_enabled")
	if err != nil || enabledStr != "true" {
		m.log.Info("Automatic tenant synchronization is disabled")
		return
	}

	m.log.WithField("interval_seconds", interval).Info("Starting tenant synchronization manager")

	go m.syncLoop(ctx, time.Duration(interval)*time.Second)
}

// syncLoop runs the synchronization loop
func (m *TenantSyncManager) syncLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	m.syncAllTenants(ctx)

	for {
		select {
		case <-ctx.Done():
			m.log.Info("Tenant sync loop stopped")
			return
		case <-m.stopChan:
			m.log.Info("Tenant sync loop stopped")
			return
		case <-ticker.C:
			m.syncAllTenants(ctx)
		}
	}
}

// syncAllTenants synchronizes all tenants to all healthy nodes
func (m *TenantSyncManager) syncAllTenants(ctx context.Context) {
	// Check if cluster is enabled
	if !m.clusterManager.IsClusterEnabled() {
		return
	}

	// Get local node ID
	localNodeID, err := m.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get local node ID")
		return
	}

	// Get all healthy nodes (excluding self)
	nodes, err := m.clusterManager.GetHealthyNodes(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get healthy nodes")
		return
	}

	// Filter out local node
	var targetNodes []*Node
	for _, node := range nodes {
		if node.ID != localNodeID {
			targetNodes = append(targetNodes, node)
		}
	}

	if len(targetNodes) == 0 {
		m.log.Debug("No target nodes for tenant synchronization")
		return
	}

	// Get all tenants from local database
	tenants, err := m.listLocalTenants(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to list local tenants")
		return
	}

	m.log.WithFields(logrus.Fields{
		"tenant_count": len(tenants),
		"node_count":   len(targetNodes),
	}).Debug("Starting tenant synchronization")

	// Sync each tenant to each target node
	for _, tenant := range tenants {
		for _, node := range targetNodes {
			if err := m.syncTenantToNode(ctx, tenant, node, localNodeID); err != nil {
				m.log.WithError(err).WithFields(logrus.Fields{
					"tenant_id": tenant.ID,
					"node_id":   node.ID,
					"node_name": node.Name,
				}).Warn("Failed to sync tenant to node")
			}
		}
	}
}

// syncTenantToNode synchronizes a single tenant to a target node
func (m *TenantSyncManager) syncTenantToNode(ctx context.Context, tenant *TenantData, node *Node, sourceNodeID string) error {
	// Compute tenant checksum
	checksum := m.computeTenantChecksum(tenant)

	// Check if tenant is already synced with same checksum
	needsSync, err := m.needsSynchronization(ctx, tenant.ID, node.ID, checksum)
	if err != nil {
		return fmt.Errorf("failed to check sync status: %w", err)
	}

	if !needsSync {
		m.log.WithFields(logrus.Fields{
			"tenant_id": tenant.ID,
			"node_id":   node.ID,
		}).Debug("Tenant already synchronized, skipping")
		return nil
	}

	// Get node token for authentication
	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node token: %w", err)
	}

	// Send tenant data to remote node
	if err := m.sendTenantToNode(ctx, tenant, node, sourceNodeID, nodeToken); err != nil {
		return fmt.Errorf("failed to send tenant data: %w", err)
	}

	// Update sync status
	if err := m.updateSyncStatus(ctx, tenant.ID, sourceNodeID, node.ID, checksum); err != nil {
		m.log.WithError(err).Warn("Failed to update sync status")
	}

	m.log.WithFields(logrus.Fields{
		"tenant_id":   tenant.ID,
		"tenant_name": tenant.Name,
		"node_id":     node.ID,
		"node_name":   node.Name,
	}).Info("Tenant synchronized successfully")

	return nil
}

// sendTenantToNode sends tenant data to a remote node via authenticated HTTP request
func (m *TenantSyncManager) sendTenantToNode(ctx context.Context, tenant *TenantData, node *Node, sourceNodeID, nodeToken string) error {
	// Prepare request body
	body, err := json.Marshal(tenant)
	if err != nil {
		return fmt.Errorf("failed to marshal tenant data: %w", err)
	}

	// Build URL
	url := fmt.Sprintf("%s/api/internal/cluster/tenant-sync", node.Endpoint)

	// Create authenticated request
	req, err := m.proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(body), sourceNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := m.proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// listLocalTenants retrieves all tenants from the local database
func (m *TenantSyncManager) listLocalTenants(ctx context.Context) ([]*TenantData, error) {
	query := `
		SELECT id, name, display_name, description, status, max_access_keys,
		       max_storage_bytes, current_storage_bytes, max_buckets, current_buckets,
		       metadata, created_at, updated_at
		FROM tenants
		WHERE status != 'deleted'
	`

	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query tenants: %w", err)
	}
	defer rows.Close()

	var tenants []*TenantData
	for rows.Next() {
		var tenant TenantData
		var metadataJSON string

		err := rows.Scan(
			&tenant.ID,
			&tenant.Name,
			&tenant.DisplayName,
			&tenant.Description,
			&tenant.Status,
			&tenant.MaxAccessKeys,
			&tenant.MaxStorageBytes,
			&tenant.CurrentStorageBytes,
			&tenant.MaxBuckets,
			&tenant.CurrentBuckets,
			&metadataJSON,
			&tenant.CreatedAt,
			&tenant.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan tenant: %w", err)
		}

		// Parse metadata JSON
		if metadataJSON != "" {
			if err := json.Unmarshal([]byte(metadataJSON), &tenant.Metadata); err != nil {
				m.log.WithError(err).WithField("tenant_id", tenant.ID).Warn("Failed to parse tenant metadata")
				tenant.Metadata = make(map[string]string)
			}
		} else {
			tenant.Metadata = make(map[string]string)
		}

		tenants = append(tenants, &tenant)
	}

	return tenants, rows.Err()
}

// computeTenantChecksum computes a SHA256 checksum of tenant data
func (m *TenantSyncManager) computeTenantChecksum(tenant *TenantData) string {
	// Create deterministic representation
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%d|%d|%d|%d|%s|%s",
		tenant.ID,
		tenant.Name,
		tenant.DisplayName,
		tenant.Description,
		tenant.Status,
		tenant.MaxAccessKeys,
		tenant.MaxStorageBytes,
		tenant.MaxBuckets,
		tenant.CurrentBuckets,
		tenant.UpdatedAt.Format(time.RFC3339),
		formatMetadata(tenant.Metadata),
	)

	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// formatMetadata formats metadata map to deterministic string
func formatMetadata(metadata map[string]string) string {
	if len(metadata) == 0 {
		return ""
	}
	data, _ := json.Marshal(metadata)
	return string(data)
}

// needsSynchronization checks if a tenant needs to be synced to a node
func (m *TenantSyncManager) needsSynchronization(ctx context.Context, tenantID, nodeID, checksum string) (bool, error) {
	var existingChecksum string
	err := m.db.QueryRowContext(ctx, `
		SELECT tenant_checksum FROM cluster_tenant_sync
		WHERE tenant_id = ? AND destination_node_id = ?
	`, tenantID, nodeID).Scan(&existingChecksum)

	if err == sql.ErrNoRows {
		return true, nil // Never synced before
	}
	if err != nil {
		return false, err
	}

	// Need sync if checksum changed
	return existingChecksum != checksum, nil
}

// updateSyncStatus updates the tenant sync status in the database
func (m *TenantSyncManager) updateSyncStatus(ctx context.Context, tenantID, sourceNodeID, destNodeID, checksum string) error {
	now := time.Now()
	id := fmt.Sprintf("%s-%s-%d", tenantID, destNodeID, now.UnixNano())

	_, err := m.db.ExecContext(ctx, `
		INSERT INTO cluster_tenant_sync (id, tenant_id, source_node_id, destination_node_id, tenant_checksum, status, last_sync_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'synced', ?, ?, ?)
		ON CONFLICT(tenant_id, destination_node_id) DO UPDATE SET
			tenant_checksum = ?,
			status = 'synced',
			last_sync_at = ?,
			updated_at = ?
	`, id, tenantID, sourceNodeID, destNodeID, checksum, now, now, now, checksum, now, now)

	return err
}

// Stop stops the tenant sync manager
func (m *TenantSyncManager) Stop() {
	close(m.stopChan)
}
