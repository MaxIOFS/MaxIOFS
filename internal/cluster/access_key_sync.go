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

// AccessKeyData represents access key information to be synchronized
type AccessKeyData struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	UserID          string `json:"user_id"`
	Status          string `json:"status"`
	CreatedAt       int64  `json:"created_at"`
	LastUsed        *int64 `json:"last_used,omitempty"`
}

// AccessKeySyncManager handles automatic access key synchronization between cluster nodes
type AccessKeySyncManager struct {
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	stopChan       chan struct{}
	log            *logrus.Entry
}

// NewAccessKeySyncManager creates a new access key sync manager
func NewAccessKeySyncManager(db *sql.DB, clusterManager *Manager) *AccessKeySyncManager {
	return &AccessKeySyncManager{
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    NewProxyClient(clusterManager.GetTLSConfig()),
		stopChan:       make(chan struct{}),
		log:            logrus.WithField("component", "access-key-sync"),
	}
}

// Start begins the access key synchronization loop
func (m *AccessKeySyncManager) Start(ctx context.Context) {
	// Get sync interval from config
	intervalStr, err := GetGlobalConfig(ctx, m.db, "access_key_sync_interval_seconds")
	if err != nil {
		m.log.WithError(err).Warn("Failed to get access key sync interval, using default 30s")
		intervalStr = "30"
	}

	interval, err := strconv.Atoi(intervalStr)
	if err != nil {
		m.log.WithError(err).Warn("Invalid access key sync interval, using default 30s")
		interval = 30
	}

	// Check if auto access key sync is enabled
	enabledStr, err := GetGlobalConfig(ctx, m.db, "auto_access_key_sync_enabled")
	if err != nil || enabledStr != "true" {
		m.log.Info("Automatic access key synchronization is disabled")
		return
	}

	m.log.WithField("interval_seconds", interval).Info("Starting access key synchronization manager")

	go m.syncLoop(ctx, time.Duration(interval)*time.Second)
}

// syncLoop runs the synchronization loop
func (m *AccessKeySyncManager) syncLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	m.syncAllAccessKeys(ctx)

	for {
		select {
		case <-ctx.Done():
			m.log.Info("Access key sync loop stopped")
			return
		case <-m.stopChan:
			m.log.Info("Access key sync loop stopped")
			return
		case <-ticker.C:
			m.syncAllAccessKeys(ctx)
		}
	}
}

// syncAllAccessKeys synchronizes all access keys to all healthy nodes
func (m *AccessKeySyncManager) syncAllAccessKeys(ctx context.Context) {
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
		m.log.Debug("No target nodes for access key synchronization")
		return
	}

	// Get all access keys from local database
	accessKeys, err := m.listLocalAccessKeys(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to list local access keys")
		return
	}

	m.log.WithFields(logrus.Fields{
		"access_key_count": len(accessKeys),
		"node_count":       len(targetNodes),
	}).Debug("Starting access key synchronization")

	// Sync each access key to each target node
	for _, accessKey := range accessKeys {
		for _, node := range targetNodes {
			if err := m.syncAccessKeyToNode(ctx, accessKey, node, localNodeID); err != nil {
				m.log.WithFields(logrus.Fields{
					"access_key_id": accessKey.AccessKeyID,
					"user_id":       accessKey.UserID,
					"node_id":       node.ID,
					"error":         err,
				}).Warn("Failed to sync access key to node")
			}
		}
	}

	// Phase 2: Sync deletion tombstones
	m.syncDeletions(ctx, targetNodes, localNodeID)
}

// syncAccessKeyToNode synchronizes a single access key to a target node
func (m *AccessKeySyncManager) syncAccessKeyToNode(ctx context.Context, accessKey *AccessKeyData, node *Node, sourceNodeID string) error {
	// Compute checksum for access key data
	checksum := m.computeAccessKeyChecksum(accessKey)

	// Check if access key is already synced with same checksum
	needsSync, err := m.needsSynchronization(ctx, accessKey.AccessKeyID, node.ID, checksum)
	if err != nil {
		return fmt.Errorf("failed to check sync status: %w", err)
	}

	if !needsSync {
		m.log.WithFields(logrus.Fields{
			"access_key_id": accessKey.AccessKeyID,
			"node_id":       node.ID,
		}).Debug("Access key already synchronized, skipping")
		return nil
	}

	// Get node token for authentication
	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node token: %w", err)
	}

	// Send access key data to remote node
	if err := m.sendAccessKeyToNode(ctx, accessKey, node, sourceNodeID, nodeToken); err != nil {
		return fmt.Errorf("failed to send access key data: %w", err)
	}

	// Update sync status
	if err := m.updateSyncStatus(ctx, accessKey.AccessKeyID, sourceNodeID, node.ID, checksum); err != nil {
		m.log.WithError(err).Warn("Failed to update sync status")
	}

	m.log.WithFields(logrus.Fields{
		"access_key_id": accessKey.AccessKeyID,
		"user_id":       accessKey.UserID,
		"node_id":       node.ID,
		"node_name":     node.Name,
	}).Info("Access key synchronized successfully")

	return nil
}

// sendAccessKeyToNode sends access key data to a target node via HMAC-authenticated request
func (m *AccessKeySyncManager) sendAccessKeyToNode(ctx context.Context, accessKey *AccessKeyData, node *Node, sourceNodeID, nodeToken string) error {
	// Marshal access key data
	accessKeyData, err := json.Marshal(accessKey)
	if err != nil {
		return fmt.Errorf("failed to marshal access key data: %w", err)
	}

	// Build URL
	url := fmt.Sprintf("%s/api/internal/cluster/access-key-sync", node.Endpoint)

	// Create authenticated request
	req, err := m.proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(accessKeyData), sourceNodeID, nodeToken)
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

// listLocalAccessKeys retrieves all access keys from the local database
func (m *AccessKeySyncManager) listLocalAccessKeys(ctx context.Context) ([]*AccessKeyData, error) {
	query := `
		SELECT access_key_id, secret_access_key, user_id, status, created_at, last_used
		FROM access_keys
		WHERE status = 'active'
	`

	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query access keys: %w", err)
	}
	defer rows.Close()

	var accessKeys []*AccessKeyData
	for rows.Next() {
		accessKey := &AccessKeyData{}
		var lastUsed sql.NullInt64
		err := rows.Scan(
			&accessKey.AccessKeyID,
			&accessKey.SecretAccessKey,
			&accessKey.UserID,
			&accessKey.Status,
			&accessKey.CreatedAt,
			&lastUsed,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan access key: %w", err)
		}
		if lastUsed.Valid {
			accessKey.LastUsed = &lastUsed.Int64
		}
		accessKeys = append(accessKeys, accessKey)
	}

	return accessKeys, nil
}

// computeAccessKeyChecksum computes a checksum for access key data to detect changes
func (m *AccessKeySyncManager) computeAccessKeyChecksum(accessKey *AccessKeyData) string {
	// Create a string representation of relevant access key fields
	data := fmt.Sprintf("%s|%s|%s|%s|%d",
		accessKey.AccessKeyID,
		accessKey.SecretAccessKey,
		accessKey.UserID,
		accessKey.Status,
		accessKey.CreatedAt,
	)

	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// needsSynchronization checks if an access key needs to be synced to a node
func (m *AccessKeySyncManager) needsSynchronization(ctx context.Context, accessKeyID, nodeID, checksum string) (bool, error) {
	query := `
		SELECT key_checksum FROM cluster_access_key_sync
		WHERE access_key_id = ? AND destination_node_id = ?
	`

	var existingChecksum string
	err := m.db.QueryRowContext(ctx, query, accessKeyID, nodeID).Scan(&existingChecksum)
	if err == sql.ErrNoRows {
		return true, nil // Never synced before
	}
	if err != nil {
		return false, err
	}

	return existingChecksum != checksum, nil
}

// updateSyncStatus updates the access key sync status in the database
func (m *AccessKeySyncManager) updateSyncStatus(ctx context.Context, accessKeyID, sourceNodeID, destNodeID, checksum string) error {
	now := time.Now().Unix()

	query := `
		INSERT INTO cluster_access_key_sync (id, access_key_id, source_node_id, destination_node_id, key_checksum, status, last_sync_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'synced', ?, ?, ?)
		ON CONFLICT(access_key_id, destination_node_id) DO UPDATE SET
			key_checksum = excluded.key_checksum,
			last_sync_at = excluded.last_sync_at,
			updated_at = excluded.updated_at
	`

	id := fmt.Sprintf("%s-%s", accessKeyID, destNodeID)
	_, err := m.db.ExecContext(ctx, query, id, accessKeyID, sourceNodeID, destNodeID, checksum, now, now, now)
	return err
}

// syncDeletions sends deletion tombstones for access keys to all target nodes
func (m *AccessKeySyncManager) syncDeletions(ctx context.Context, targetNodes []*Node, localNodeID string) {
	deletions, err := ListDeletions(ctx, m.db, EntityTypeAccessKey)
	if err != nil {
		m.log.WithError(err).Error("Failed to list access key deletion tombstones")
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
					"access_key_id": deletion.EntityID,
					"node_id":       node.ID,
					"error":         err,
				}).Warn("Failed to send access key deletion to node")
			}
		}
	}
}

// sendDeletionToNode sends an access key deletion request to a target node
func (m *AccessKeySyncManager) sendDeletionToNode(ctx context.Context, accessKeyID string, node *Node, sourceNodeID, nodeToken string) error {
	payload, _ := json.Marshal(map[string]string{"id": accessKeyID})

	url := fmt.Sprintf("%s/api/internal/cluster/access-key-delete-sync", node.Endpoint)

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

// Stop stops the access key sync manager
func (m *AccessKeySyncManager) Stop() {
	close(m.stopChan)
}
