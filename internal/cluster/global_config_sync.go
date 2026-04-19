package cluster

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// GlobalConfigEntry represents a single key-value pair from cluster_global_config.
type GlobalConfigEntry struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt int64  `json:"updated_at"` // Unix timestamp
}

// GlobalConfigSyncManager synchronizes cluster_global_config and cluster_nodes
// across all cluster members.
type GlobalConfigSyncManager struct {
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	stopChan       chan struct{}
	log            *logrus.Entry
}

// NewGlobalConfigSyncManager creates a new global config sync manager.
func NewGlobalConfigSyncManager(db *sql.DB, clusterManager *Manager) *GlobalConfigSyncManager {
	return &GlobalConfigSyncManager{
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    NewProxyClient(clusterManager.GetTLSConfig()),
		stopChan:       make(chan struct{}),
		log:            logrus.WithField("component", "global-config-sync"),
	}
}

// Start begins the global config synchronization loop.
func (m *GlobalConfigSyncManager) Start(ctx context.Context) {
	m.log.Info("Starting global config synchronization manager (interval: 60s)")
	go m.syncLoop(ctx, 60*time.Second)
}

// Stop gracefully stops the sync loop.
func (m *GlobalConfigSyncManager) Stop() {
	close(m.stopChan)
}

func (m *GlobalConfigSyncManager) syncLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run immediately on start
	m.syncAll(ctx)

	for {
		select {
		case <-ctx.Done():
			m.log.Info("Global config sync loop stopped (context cancelled)")
			return
		case <-m.stopChan:
			m.log.Info("Global config sync loop stopped")
			return
		case <-ticker.C:
			m.syncAll(ctx)
		}
	}
}

func (m *GlobalConfigSyncManager) syncAll(ctx context.Context) {
	if !m.clusterManager.IsClusterEnabled() {
		return
	}

	localNodeID, err := m.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get local node ID")
		return
	}

	nodes, err := m.clusterManager.GetHealthyNodes(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get healthy nodes")
		return
	}

	// --- Phase 1: Sync global config entries ---
	entries, err := m.listGlobalConfig(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to list global config")
		return
	}

	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get local node token")
		return
	}

	for _, node := range nodes {
		if node.ID == localNodeID {
			continue
		}
		if err := m.sendGlobalConfigToNode(ctx, entries, node, localNodeID, nodeToken); err != nil {
			m.log.WithError(err).WithField("node_id", node.ID).Warn("Failed to sync global config to node")
		}
	}

	// --- Phase 2: Sync node list (reconcile cluster_nodes) ---
	allNodes, err := m.clusterManager.ListNodes(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to list all nodes for node-sync")
		return
	}

	for _, targetNode := range nodes {
		if targetNode.ID == localNodeID {
			continue
		}
		if err := m.sendNodeListToNode(ctx, allNodes, targetNode, localNodeID, nodeToken); err != nil {
			m.log.WithError(err).WithField("node_id", targetNode.ID).Warn("Failed to sync node list to node")
		}
	}
}

// listGlobalConfig returns all entries from cluster_global_config.
func (m *GlobalConfigSyncManager) listGlobalConfig(ctx context.Context) ([]GlobalConfigEntry, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT key, value, COALESCE(CAST(strftime('%s', updated_at) AS INTEGER), 0) FROM cluster_global_config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []GlobalConfigEntry
	for rows.Next() {
		var e GlobalConfigEntry
		if err := rows.Scan(&e.Key, &e.Value, &e.UpdatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// sendGlobalConfigToNode sends all global config entries to a remote node.
func (m *GlobalConfigSyncManager) sendGlobalConfigToNode(ctx context.Context, entries []GlobalConfigEntry, node *Node, sourceNodeID, nodeToken string) error {
	body, err := json.Marshal(map[string]interface{}{
		"entries":        entries,
		"source_node_id": sourceNodeID,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	url := fmt.Sprintf("%s/api/internal/cluster/global-config-sync", node.Endpoint)
	req, err := m.proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(body), sourceNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// sendNodeListToNode sends the full node list to a remote node for reconciliation.
func (m *GlobalConfigSyncManager) sendNodeListToNode(ctx context.Context, allNodes []*Node, target *Node, sourceNodeID, nodeToken string) error {
	// Build a payload that includes node_token (which is json:"-" on Node).
	type nodePayload struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Endpoint  string `json:"endpoint"`
		NodeToken string `json:"node_token"`
		Region    string `json:"region"`
		Priority  int    `json:"priority"`
	}
	payload := make([]nodePayload, 0, len(allNodes))
	for _, n := range allNodes {
		payload = append(payload, nodePayload{
			ID:        n.ID,
			Name:      n.Name,
			Endpoint:  n.Endpoint,
			NodeToken: n.NodeToken,
			Region:    n.Region,
			Priority:  n.Priority,
		})
	}

	body, err := json.Marshal(map[string]interface{}{
		"nodes":          payload,
		"source_node_id": sourceNodeID,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal node list: %w", err)
	}

	url := fmt.Sprintf("%s/api/internal/cluster/node-list-sync", target.Endpoint)
	req, err := m.proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(body), sourceNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
