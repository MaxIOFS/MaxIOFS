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

// GroupMappingData represents IDP group mapping information to be synchronized
type GroupMappingData struct {
	ID                string `json:"id"`
	ProviderID        string `json:"provider_id"`
	ExternalGroup     string `json:"external_group"`
	ExternalGroupName string `json:"external_group_name"`
	Role              string `json:"role"`
	TenantID          string `json:"tenant_id"`
	AutoSync          bool   `json:"auto_sync"`
	LastSyncedAt      int64  `json:"last_synced_at"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

// GroupMappingSyncManager handles automatic IDP group mapping synchronization between cluster nodes
type GroupMappingSyncManager struct {
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	stopChan       chan struct{}
	log            *logrus.Entry
}

// NewGroupMappingSyncManager creates a new group mapping sync manager
func NewGroupMappingSyncManager(db *sql.DB, clusterManager *Manager) *GroupMappingSyncManager {
	return &GroupMappingSyncManager{
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    NewProxyClient(clusterManager.GetTLSConfig()),
		stopChan:       make(chan struct{}),
		log:            logrus.WithField("component", "group-mapping-sync"),
	}
}

// Start begins the group mapping synchronization loop
func (m *GroupMappingSyncManager) Start(ctx context.Context) {
	// Get sync interval from config
	intervalStr, err := GetGlobalConfig(ctx, m.db, "group_mapping_sync_interval_seconds")
	if err != nil {
		m.log.WithError(err).Warn("Failed to get group mapping sync interval, using default 30s")
		intervalStr = "30"
	}

	interval, err := strconv.Atoi(intervalStr)
	if err != nil {
		m.log.WithError(err).Warn("Invalid group mapping sync interval, using default 30s")
		interval = 30
	}

	// Check if auto group mapping sync is enabled
	enabledStr, err := GetGlobalConfig(ctx, m.db, "auto_group_mapping_sync_enabled")
	if err != nil || enabledStr != "true" {
		m.log.Info("Automatic group mapping synchronization is disabled")
		return
	}

	m.log.WithField("interval_seconds", interval).Info("Starting group mapping synchronization manager")

	go m.syncLoop(ctx, time.Duration(interval)*time.Second)
}

// syncLoop runs the synchronization loop
func (m *GroupMappingSyncManager) syncLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	m.syncAllMappings(ctx)

	for {
		select {
		case <-ctx.Done():
			m.log.Info("Group mapping sync loop stopped")
			return
		case <-m.stopChan:
			m.log.Info("Group mapping sync loop stopped")
			return
		case <-ticker.C:
			m.syncAllMappings(ctx)
		}
	}
}

// syncAllMappings synchronizes all group mappings to all healthy nodes
func (m *GroupMappingSyncManager) syncAllMappings(ctx context.Context) {
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
		m.log.Debug("No target nodes for group mapping synchronization")
		return
	}

	// Get all group mappings from local database
	mappings, err := m.listLocalMappings(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to list local group mappings")
		return
	}

	m.log.WithFields(logrus.Fields{
		"mapping_count": len(mappings),
		"node_count":    len(targetNodes),
	}).Debug("Starting group mapping synchronization")

	// Sync each mapping to each target node
	for _, mapping := range mappings {
		for _, node := range targetNodes {
			if err := m.syncMappingToNode(ctx, mapping, node, localNodeID); err != nil {
				m.log.WithFields(logrus.Fields{
					"mapping_id":  mapping.ID,
					"provider_id": mapping.ProviderID,
					"node_id":     node.ID,
					"error":       err,
				}).Warn("Failed to sync group mapping to node")
			}
		}
	}

	// Phase 2: Sync deletion tombstones
	m.syncDeletions(ctx, targetNodes, localNodeID)
}

// syncMappingToNode synchronizes a single group mapping to a target node
func (m *GroupMappingSyncManager) syncMappingToNode(ctx context.Context, mapping *GroupMappingData, node *Node, sourceNodeID string) error {
	// Compute checksum for mapping data
	checksum := m.computeMappingChecksum(mapping)

	// Check if mapping is already synced with same checksum
	needsSync, err := m.needsSynchronization(ctx, mapping.ID, node.ID, checksum)
	if err != nil {
		return fmt.Errorf("failed to check sync status: %w", err)
	}

	if !needsSync {
		m.log.WithFields(logrus.Fields{
			"mapping_id": mapping.ID,
			"node_id":    node.ID,
		}).Debug("Group mapping already synchronized, skipping")
		return nil
	}

	// Get node token for authentication
	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node token: %w", err)
	}

	// Send mapping data to remote node
	if err := m.sendMappingToNode(ctx, mapping, node, sourceNodeID, nodeToken); err != nil {
		return fmt.Errorf("failed to send mapping data: %w", err)
	}

	// Update sync status
	if err := m.updateSyncStatus(ctx, mapping.ID, sourceNodeID, node.ID, checksum); err != nil {
		m.log.WithError(err).Warn("Failed to update sync status")
	}

	m.log.WithFields(logrus.Fields{
		"mapping_id":     mapping.ID,
		"provider_id":    mapping.ProviderID,
		"external_group": mapping.ExternalGroupName,
		"node_id":        node.ID,
		"node_name":      node.Name,
	}).Info("Group mapping synchronized successfully")

	return nil
}

// sendMappingToNode sends group mapping data to a target node via HMAC-authenticated request
func (m *GroupMappingSyncManager) sendMappingToNode(ctx context.Context, mapping *GroupMappingData, node *Node, sourceNodeID, nodeToken string) error {
	// Marshal mapping data
	mappingJSON, err := json.Marshal(mapping)
	if err != nil {
		return fmt.Errorf("failed to marshal mapping data: %w", err)
	}

	// Build URL
	url := fmt.Sprintf("%s/api/internal/cluster/group-mapping-sync", node.Endpoint)

	// Create authenticated request
	req, err := m.proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(mappingJSON), sourceNodeID, nodeToken)
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

// listLocalMappings retrieves all group mappings from the local database
func (m *GroupMappingSyncManager) listLocalMappings(ctx context.Context) ([]*GroupMappingData, error) {
	query := `
		SELECT id, provider_id, external_group, COALESCE(external_group_name, ''),
		       role, COALESCE(tenant_id, ''), auto_sync, COALESCE(last_synced_at, 0),
		       created_at, updated_at
		FROM idp_group_mappings
	`

	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query group mappings: %w", err)
	}
	defer rows.Close()

	var mappings []*GroupMappingData
	for rows.Next() {
		gm := &GroupMappingData{}
		err := rows.Scan(
			&gm.ID,
			&gm.ProviderID,
			&gm.ExternalGroup,
			&gm.ExternalGroupName,
			&gm.Role,
			&gm.TenantID,
			&gm.AutoSync,
			&gm.LastSyncedAt,
			&gm.CreatedAt,
			&gm.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan group mapping: %w", err)
		}
		mappings = append(mappings, gm)
	}

	return mappings, rows.Err()
}

// computeMappingChecksum computes a checksum for group mapping data to detect changes
func (m *GroupMappingSyncManager) computeMappingChecksum(mapping *GroupMappingData) string {
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%v|%d|%d",
		mapping.ID,
		mapping.ProviderID,
		mapping.ExternalGroup,
		mapping.ExternalGroupName,
		mapping.Role,
		mapping.TenantID,
		mapping.AutoSync,
		mapping.LastSyncedAt,
		mapping.UpdatedAt,
	)

	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// needsSynchronization checks if a group mapping needs to be synced to a node
func (m *GroupMappingSyncManager) needsSynchronization(ctx context.Context, mappingID, nodeID, checksum string) (bool, error) {
	var existingChecksum string
	err := m.db.QueryRowContext(ctx, `
		SELECT mapping_checksum FROM cluster_group_mapping_sync
		WHERE mapping_id = ? AND destination_node_id = ?
	`, mappingID, nodeID).Scan(&existingChecksum)

	if err == sql.ErrNoRows {
		return true, nil // Never synced before
	}
	if err != nil {
		return false, err
	}

	return existingChecksum != checksum, nil
}

// updateSyncStatus updates the group mapping sync status in the database
func (m *GroupMappingSyncManager) updateSyncStatus(ctx context.Context, mappingID, sourceNodeID, destNodeID, checksum string) error {
	now := time.Now().Unix()

	query := `
		INSERT INTO cluster_group_mapping_sync (id, mapping_id, source_node_id, destination_node_id, mapping_checksum, status, last_sync_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'synced', ?, ?, ?)
		ON CONFLICT(mapping_id, destination_node_id) DO UPDATE SET
			mapping_checksum = excluded.mapping_checksum,
			status = 'synced',
			last_sync_at = excluded.last_sync_at,
			updated_at = excluded.updated_at
	`

	id := fmt.Sprintf("%s-%s", mappingID, destNodeID)
	_, err := m.db.ExecContext(ctx, query, id, mappingID, sourceNodeID, destNodeID, checksum, now, now, now)
	return err
}

// syncDeletions sends deletion tombstones for group mappings to all target nodes
func (m *GroupMappingSyncManager) syncDeletions(ctx context.Context, targetNodes []*Node, localNodeID string) {
	deletions, err := ListDeletions(ctx, m.db, EntityTypeGroupMapping)
	if err != nil {
		m.log.WithError(err).Error("Failed to list group mapping deletion tombstones")
		return
	}

	if len(deletions) == 0 {
		return
	}

	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get node token for deletion sync")
		return
	}

	for _, deletion := range deletions {
		for _, node := range targetNodes {
			if err := m.sendDeletionToNode(ctx, deletion.EntityID, node, localNodeID, nodeToken); err != nil {
				m.log.WithFields(logrus.Fields{
					"mapping_id": deletion.EntityID,
					"node_id":    node.ID,
					"error":      err,
				}).Warn("Failed to send group mapping deletion to node")
			}
		}
	}
}

// sendDeletionToNode sends a group mapping deletion request to a target node
func (m *GroupMappingSyncManager) sendDeletionToNode(ctx context.Context, mappingID string, node *Node, sourceNodeID, nodeToken string) error {
	payload, _ := json.Marshal(map[string]string{"id": mappingID})

	url := fmt.Sprintf("%s/api/internal/cluster/group-mapping-delete-sync", node.Endpoint)

	req, err := m.proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(payload), sourceNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("failed to create deletion request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := m.proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("failed to execute deletion request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// Stop stops the group mapping sync manager
func (m *GroupMappingSyncManager) Stop() {
	close(m.stopChan)
}
