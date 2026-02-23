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

// BucketPermissionData represents bucket permission information to be synchronized
type BucketPermissionData struct {
	ID              string  `json:"id"`
	BucketName      string  `json:"bucket_name"`
	UserID          *string `json:"user_id,omitempty"`
	TenantID        *string `json:"tenant_id,omitempty"`
	PermissionLevel string  `json:"permission_level"`
	GrantedBy       string  `json:"granted_by"`
	GrantedAt       int64   `json:"granted_at"`
	ExpiresAt       *int64  `json:"expires_at,omitempty"`
}

// BucketPermissionSyncManager handles automatic bucket permission synchronization between cluster nodes
type BucketPermissionSyncManager struct {
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	stopChan       chan struct{}
	log            *logrus.Entry
}

// NewBucketPermissionSyncManager creates a new bucket permission sync manager
func NewBucketPermissionSyncManager(db *sql.DB, clusterManager *Manager) *BucketPermissionSyncManager {
	return &BucketPermissionSyncManager{
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    NewProxyClient(clusterManager.GetTLSConfig()),
		stopChan:       make(chan struct{}),
		log:            logrus.WithField("component", "bucket-permission-sync"),
	}
}

// Start begins the bucket permission synchronization loop
func (m *BucketPermissionSyncManager) Start(ctx context.Context) {
	// Get sync interval from config
	intervalStr, err := GetGlobalConfig(ctx, m.db, "bucket_permission_sync_interval_seconds")
	if err != nil {
		m.log.WithError(err).Warn("Failed to get bucket permission sync interval, using default 30s")
		intervalStr = "30"
	}

	interval, err := strconv.Atoi(intervalStr)
	if err != nil {
		m.log.WithError(err).Warn("Invalid bucket permission sync interval, using default 30s")
		interval = 30
	}

	// Check if auto bucket permission sync is enabled
	enabledStr, err := GetGlobalConfig(ctx, m.db, "auto_bucket_permission_sync_enabled")
	if err != nil || enabledStr != "true" {
		m.log.Info("Automatic bucket permission synchronization is disabled")
		return
	}

	m.log.WithField("interval_seconds", interval).Info("Starting bucket permission synchronization manager")

	go m.syncLoop(ctx, time.Duration(interval)*time.Second)
}

// syncLoop runs the synchronization loop
func (m *BucketPermissionSyncManager) syncLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	m.syncAllBucketPermissions(ctx)

	for {
		select {
		case <-ctx.Done():
			m.log.Info("Bucket permission sync loop stopped")
			return
		case <-m.stopChan:
			m.log.Info("Bucket permission sync loop stopped")
			return
		case <-ticker.C:
			m.syncAllBucketPermissions(ctx)
		}
	}
}

// syncAllBucketPermissions synchronizes all bucket permissions to all healthy nodes
func (m *BucketPermissionSyncManager) syncAllBucketPermissions(ctx context.Context) {
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
		m.log.Debug("No target nodes for bucket permission synchronization")
		return
	}

	// Get all bucket permissions from local database
	permissions, err := m.listLocalBucketPermissions(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to list local bucket permissions")
		return
	}

	m.log.WithFields(logrus.Fields{
		"permission_count": len(permissions),
		"node_count":       len(targetNodes),
	}).Debug("Starting bucket permission synchronization")

	// Sync each permission to each target node
	for _, permission := range permissions {
		for _, node := range targetNodes {
			if err := m.syncPermissionToNode(ctx, permission, node, localNodeID); err != nil {
				m.log.WithFields(logrus.Fields{
					"permission_id": permission.ID,
					"bucket":        permission.BucketName,
					"node_id":       node.ID,
					"error":         err,
				}).Warn("Failed to sync bucket permission to node")
			}
		}
	}

	// Phase 2: Sync deletion tombstones
	m.syncDeletions(ctx, targetNodes, localNodeID)
}

// syncPermissionToNode synchronizes a single bucket permission to a target node
func (m *BucketPermissionSyncManager) syncPermissionToNode(ctx context.Context, permission *BucketPermissionData, node *Node, sourceNodeID string) error {
	// Compute checksum for permission data
	checksum := m.computePermissionChecksum(permission)

	// Check if permission is already synced with same checksum
	needsSync, err := m.needsSynchronization(ctx, permission.ID, node.ID, checksum)
	if err != nil {
		return fmt.Errorf("failed to check sync status: %w", err)
	}

	if !needsSync {
		m.log.WithFields(logrus.Fields{
			"permission_id": permission.ID,
			"bucket":        permission.BucketName,
			"node_id":       node.ID,
		}).Debug("Bucket permission already synchronized, skipping")
		return nil
	}

	// Get node token for authentication
	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node token: %w", err)
	}

	// Send permission data to remote node
	if err := m.sendPermissionToNode(ctx, permission, node, sourceNodeID, nodeToken); err != nil {
		return fmt.Errorf("failed to send permission data: %w", err)
	}

	// Update sync status
	if err := m.updateSyncStatus(ctx, permission.ID, sourceNodeID, node.ID, checksum); err != nil {
		m.log.WithError(err).Warn("Failed to update sync status")
	}

	m.log.WithFields(logrus.Fields{
		"permission_id": permission.ID,
		"bucket":        permission.BucketName,
		"node_id":       node.ID,
		"node_name":     node.Name,
	}).Info("Bucket permission synchronized successfully")

	return nil
}

// sendPermissionToNode sends permission data to a target node via HMAC-authenticated request
func (m *BucketPermissionSyncManager) sendPermissionToNode(ctx context.Context, permission *BucketPermissionData, node *Node, sourceNodeID, nodeToken string) error {
	// Marshal permission data
	permissionData, err := json.Marshal(permission)
	if err != nil {
		return fmt.Errorf("failed to marshal permission data: %w", err)
	}

	// Build URL - reuse the existing endpoint from migration
	url := fmt.Sprintf("%s/api/internal/cluster/bucket-permission-sync", node.Endpoint)

	// Create authenticated request
	req, err := m.proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(permissionData), sourceNodeID, nodeToken)
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

// listLocalBucketPermissions retrieves all bucket permissions from the local database
func (m *BucketPermissionSyncManager) listLocalBucketPermissions(ctx context.Context) ([]*BucketPermissionData, error) {
	query := `
		SELECT id, bucket_name, user_id, tenant_id, permission_level, granted_by, granted_at, expires_at
		FROM bucket_permissions
	`

	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query bucket permissions: %w", err)
	}
	defer rows.Close()

	var permissions []*BucketPermissionData
	for rows.Next() {
		permission := &BucketPermissionData{}
		var userID, tenantID sql.NullString
		var expiresAt sql.NullInt64

		err := rows.Scan(
			&permission.ID,
			&permission.BucketName,
			&userID,
			&tenantID,
			&permission.PermissionLevel,
			&permission.GrantedBy,
			&permission.GrantedAt,
			&expiresAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan bucket permission: %w", err)
		}

		if userID.Valid {
			permission.UserID = &userID.String
		}
		if tenantID.Valid {
			permission.TenantID = &tenantID.String
		}
		if expiresAt.Valid {
			permission.ExpiresAt = &expiresAt.Int64
		}

		permissions = append(permissions, permission)
	}

	return permissions, nil
}

// computePermissionChecksum computes a checksum for permission data to detect changes
func (m *BucketPermissionSyncManager) computePermissionChecksum(permission *BucketPermissionData) string {
	// Create a string representation of relevant permission fields
	userID := ""
	if permission.UserID != nil {
		userID = *permission.UserID
	}
	tenantID := ""
	if permission.TenantID != nil {
		tenantID = *permission.TenantID
	}
	expiresAt := int64(0)
	if permission.ExpiresAt != nil {
		expiresAt = *permission.ExpiresAt
	}

	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%d|%d",
		permission.ID,
		permission.BucketName,
		userID,
		tenantID,
		permission.PermissionLevel,
		permission.GrantedBy,
		permission.GrantedAt,
		expiresAt,
	)

	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// needsSynchronization checks if a permission needs to be synced to a node
func (m *BucketPermissionSyncManager) needsSynchronization(ctx context.Context, permissionID, nodeID, checksum string) (bool, error) {
	query := `
		SELECT permission_checksum FROM cluster_bucket_permission_sync
		WHERE permission_id = ? AND destination_node_id = ?
	`

	var existingChecksum string
	err := m.db.QueryRowContext(ctx, query, permissionID, nodeID).Scan(&existingChecksum)
	if err == sql.ErrNoRows {
		return true, nil // Never synced before
	}
	if err != nil {
		return false, err
	}

	return existingChecksum != checksum, nil
}

// updateSyncStatus updates the bucket permission sync status in the database
func (m *BucketPermissionSyncManager) updateSyncStatus(ctx context.Context, permissionID, sourceNodeID, destNodeID, checksum string) error {
	now := time.Now().Unix()

	query := `
		INSERT INTO cluster_bucket_permission_sync (id, permission_id, source_node_id, destination_node_id, permission_checksum, status, last_sync_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'synced', ?, ?, ?)
		ON CONFLICT(permission_id, destination_node_id) DO UPDATE SET
			permission_checksum = excluded.permission_checksum,
			last_sync_at = excluded.last_sync_at,
			updated_at = excluded.updated_at
	`

	id := fmt.Sprintf("%s-%s", permissionID, destNodeID)
	_, err := m.db.ExecContext(ctx, query, id, permissionID, sourceNodeID, destNodeID, checksum, now, now, now)
	return err
}

// syncDeletions sends deletion tombstones for bucket permissions to all target nodes
func (m *BucketPermissionSyncManager) syncDeletions(ctx context.Context, targetNodes []*Node, localNodeID string) {
	deletions, err := ListDeletions(ctx, m.db, EntityTypeBucketPermission)
	if err != nil {
		m.log.WithError(err).Error("Failed to list bucket permission deletion tombstones")
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
			if err := m.sendDeletionToNode(ctx, deletion.EntityID, deletion.DeletedAt, node, localNodeID, nodeToken); err != nil {
				m.log.WithFields(logrus.Fields{
					"permission_id": deletion.EntityID,
					"node_id":       node.ID,
					"error":         err,
				}).Warn("Failed to send bucket permission deletion to node")
			}
		}
	}
}

// sendDeletionToNode sends a bucket permission deletion request to a target node
func (m *BucketPermissionSyncManager) sendDeletionToNode(ctx context.Context, permissionID string, deletedAt int64, node *Node, sourceNodeID, nodeToken string) error {
	payload, _ := json.Marshal(map[string]interface{}{"id": permissionID, "deleted_at": deletedAt})

	url := fmt.Sprintf("%s/api/internal/cluster/bucket-permission-delete-sync", node.Endpoint)

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

// Stop stops the bucket permission sync manager
func (m *BucketPermissionSyncManager) Stop() {
	close(m.stopChan)
}
