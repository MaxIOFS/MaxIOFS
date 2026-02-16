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

// IDPProviderData represents identity provider information to be synchronized
type IDPProviderData struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	TenantID  string `json:"tenant_id"`
	Status    string `json:"status"`
	Config    string `json:"config"`
	CreatedBy string `json:"created_by"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// IDPProviderSyncManager handles automatic identity provider synchronization between cluster nodes
type IDPProviderSyncManager struct {
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	stopChan       chan struct{}
	log            *logrus.Entry
}

// NewIDPProviderSyncManager creates a new IDP provider sync manager
func NewIDPProviderSyncManager(db *sql.DB, clusterManager *Manager) *IDPProviderSyncManager {
	return &IDPProviderSyncManager{
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    NewProxyClient(),
		stopChan:       make(chan struct{}),
		log:            logrus.WithField("component", "idp-provider-sync"),
	}
}

// Start begins the IDP provider synchronization loop
func (m *IDPProviderSyncManager) Start(ctx context.Context) {
	// Get sync interval from config
	intervalStr, err := GetGlobalConfig(ctx, m.db, "idp_provider_sync_interval_seconds")
	if err != nil {
		m.log.WithError(err).Warn("Failed to get IDP provider sync interval, using default 30s")
		intervalStr = "30"
	}

	interval, err := strconv.Atoi(intervalStr)
	if err != nil {
		m.log.WithError(err).Warn("Invalid IDP provider sync interval, using default 30s")
		interval = 30
	}

	// Check if auto IDP provider sync is enabled
	enabledStr, err := GetGlobalConfig(ctx, m.db, "auto_idp_provider_sync_enabled")
	if err != nil || enabledStr != "true" {
		m.log.Info("Automatic IDP provider synchronization is disabled")
		return
	}

	m.log.WithField("interval_seconds", interval).Info("Starting IDP provider synchronization manager")

	go m.syncLoop(ctx, time.Duration(interval)*time.Second)
}

// syncLoop runs the synchronization loop
func (m *IDPProviderSyncManager) syncLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	m.syncAllProviders(ctx)

	for {
		select {
		case <-ctx.Done():
			m.log.Info("IDP provider sync loop stopped")
			return
		case <-m.stopChan:
			m.log.Info("IDP provider sync loop stopped")
			return
		case <-ticker.C:
			m.syncAllProviders(ctx)
		}
	}
}

// syncAllProviders synchronizes all IDP providers to all healthy nodes
func (m *IDPProviderSyncManager) syncAllProviders(ctx context.Context) {
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
		m.log.Debug("No target nodes for IDP provider synchronization")
		return
	}

	// Get all IDP providers from local database
	providers, err := m.listLocalProviders(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to list local IDP providers")
		return
	}

	m.log.WithFields(logrus.Fields{
		"provider_count": len(providers),
		"node_count":     len(targetNodes),
	}).Debug("Starting IDP provider synchronization")

	// Sync each provider to each target node
	for _, provider := range providers {
		for _, node := range targetNodes {
			if err := m.syncProviderToNode(ctx, provider, node, localNodeID); err != nil {
				m.log.WithFields(logrus.Fields{
					"provider_id":   provider.ID,
					"provider_name": provider.Name,
					"node_id":       node.ID,
					"error":         err,
				}).Warn("Failed to sync IDP provider to node")
			}
		}
	}

	// Phase 2: Sync deletion tombstones
	m.syncDeletions(ctx, targetNodes, localNodeID)
}

// syncProviderToNode synchronizes a single IDP provider to a target node
func (m *IDPProviderSyncManager) syncProviderToNode(ctx context.Context, provider *IDPProviderData, node *Node, sourceNodeID string) error {
	// Compute checksum for provider data
	checksum := m.computeProviderChecksum(provider)

	// Check if provider is already synced with same checksum
	needsSync, err := m.needsSynchronization(ctx, provider.ID, node.ID, checksum)
	if err != nil {
		return fmt.Errorf("failed to check sync status: %w", err)
	}

	if !needsSync {
		m.log.WithFields(logrus.Fields{
			"provider_id": provider.ID,
			"node_id":     node.ID,
		}).Debug("IDP provider already synchronized, skipping")
		return nil
	}

	// Get node token for authentication
	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node token: %w", err)
	}

	// Send provider data to remote node
	if err := m.sendProviderToNode(ctx, provider, node, sourceNodeID, nodeToken); err != nil {
		return fmt.Errorf("failed to send provider data: %w", err)
	}

	// Update sync status
	if err := m.updateSyncStatus(ctx, provider.ID, sourceNodeID, node.ID, checksum); err != nil {
		m.log.WithError(err).Warn("Failed to update sync status")
	}

	m.log.WithFields(logrus.Fields{
		"provider_id":   provider.ID,
		"provider_name": provider.Name,
		"node_id":       node.ID,
		"node_name":     node.Name,
	}).Info("IDP provider synchronized successfully")

	return nil
}

// sendProviderToNode sends IDP provider data to a target node via HMAC-authenticated request
func (m *IDPProviderSyncManager) sendProviderToNode(ctx context.Context, provider *IDPProviderData, node *Node, sourceNodeID, nodeToken string) error {
	// Marshal provider data
	providerJSON, err := json.Marshal(provider)
	if err != nil {
		return fmt.Errorf("failed to marshal provider data: %w", err)
	}

	// Build URL
	url := fmt.Sprintf("%s/api/internal/cluster/idp-provider-sync", node.Endpoint)

	// Create authenticated request
	req, err := m.proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(providerJSON), sourceNodeID, nodeToken)
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

// listLocalProviders retrieves all identity providers from the local database
func (m *IDPProviderSyncManager) listLocalProviders(ctx context.Context) ([]*IDPProviderData, error) {
	query := `
		SELECT id, name, type, COALESCE(tenant_id, ''), status, config, created_by, created_at, updated_at
		FROM identity_providers
	`

	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query IDP providers: %w", err)
	}
	defer rows.Close()

	var providers []*IDPProviderData
	for rows.Next() {
		p := &IDPProviderData{}
		err := rows.Scan(
			&p.ID,
			&p.Name,
			&p.Type,
			&p.TenantID,
			&p.Status,
			&p.Config,
			&p.CreatedBy,
			&p.CreatedAt,
			&p.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan IDP provider: %w", err)
		}
		providers = append(providers, p)
	}

	return providers, rows.Err()
}

// computeProviderChecksum computes a checksum for IDP provider data to detect changes
func (m *IDPProviderSyncManager) computeProviderChecksum(provider *IDPProviderData) string {
	data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%d",
		provider.ID,
		provider.Name,
		provider.Type,
		provider.TenantID,
		provider.Status,
		provider.Config,
		provider.CreatedBy,
		provider.UpdatedAt,
	)

	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// needsSynchronization checks if a provider needs to be synced to a node
func (m *IDPProviderSyncManager) needsSynchronization(ctx context.Context, providerID, nodeID, checksum string) (bool, error) {
	var existingChecksum string
	err := m.db.QueryRowContext(ctx, `
		SELECT provider_checksum FROM cluster_idp_provider_sync
		WHERE provider_id = ? AND destination_node_id = ?
	`, providerID, nodeID).Scan(&existingChecksum)

	if err == sql.ErrNoRows {
		return true, nil // Never synced before
	}
	if err != nil {
		return false, err
	}

	return existingChecksum != checksum, nil
}

// updateSyncStatus updates the IDP provider sync status in the database
func (m *IDPProviderSyncManager) updateSyncStatus(ctx context.Context, providerID, sourceNodeID, destNodeID, checksum string) error {
	now := time.Now().Unix()

	query := `
		INSERT INTO cluster_idp_provider_sync (id, provider_id, source_node_id, destination_node_id, provider_checksum, status, last_sync_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'synced', ?, ?, ?)
		ON CONFLICT(provider_id, destination_node_id) DO UPDATE SET
			provider_checksum = excluded.provider_checksum,
			status = 'synced',
			last_sync_at = excluded.last_sync_at,
			updated_at = excluded.updated_at
	`

	id := fmt.Sprintf("%s-%s", providerID, destNodeID)
	_, err := m.db.ExecContext(ctx, query, id, providerID, sourceNodeID, destNodeID, checksum, now, now, now)
	return err
}

// syncDeletions sends deletion tombstones for IDP providers to all target nodes
func (m *IDPProviderSyncManager) syncDeletions(ctx context.Context, targetNodes []*Node, localNodeID string) {
	deletions, err := ListDeletions(ctx, m.db, EntityTypeIDPProvider)
	if err != nil {
		m.log.WithError(err).Error("Failed to list IDP provider deletion tombstones")
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
					"provider_id": deletion.EntityID,
					"node_id":     node.ID,
					"error":       err,
				}).Warn("Failed to send IDP provider deletion to node")
			}
		}
	}
}

// sendDeletionToNode sends an IDP provider deletion request to a target node
func (m *IDPProviderSyncManager) sendDeletionToNode(ctx context.Context, providerID string, node *Node, sourceNodeID, nodeToken string) error {
	payload, _ := json.Marshal(map[string]string{"id": providerID})

	url := fmt.Sprintf("%s/api/internal/cluster/idp-provider-delete-sync", node.Endpoint)

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

// Stop stops the IDP provider sync manager
func (m *IDPProviderSyncManager) Stop() {
	close(m.stopChan)
}
