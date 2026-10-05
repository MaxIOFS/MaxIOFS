package cluster

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/maxiofs/maxiofs/internal/bgwork"
	"io"
	"net/http"
	"time"

	"github.com/maxiofs/maxiofs/internal/kek"
	"github.com/sirupsen/logrus"
)

// GlobalConfigEntry represents a single key-value pair from cluster_global_config.
type GlobalConfigEntry struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt int64  `json:"updated_at"` // Unix timestamp
}

// KEKProvider exposes the cluster-shared encryption keys for periodic sync.
type KEKProvider interface {
	ClusterSharedKeys() []kek.KeyRecord
}

// GlobalConfigSyncManager synchronizes cluster_global_config and cluster_nodes
type GlobalConfigSyncManager struct {
	bgwork.Worker
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	log            *logrus.Entry
	kekProvider    KEKProvider
}

// NewGlobalConfigSyncManager creates a new global config sync manager.
func NewGlobalConfigSyncManager(db *sql.DB, clusterManager *Manager) *GlobalConfigSyncManager {
	return &GlobalConfigSyncManager{
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    NewDynamicProxyClient(clusterManager.GetTLSConfig),
		log:            logrus.WithField("component", "global-config-sync"),
	}
}

// SetKEKProvider wires the encryption key store for cluster-shared KEK sync.
func (m *GlobalConfigSyncManager) SetKEKProvider(p KEKProvider) {
	m.kekProvider = p
}

// Start begins the global config synchronization loop.
func (m *GlobalConfigSyncManager) Start(ctx context.Context) {
	m.log.Info("Starting global config synchronization manager (interval: 60s)")
	m.Spawn(func() { m.syncLoop(ctx, 60*time.Second) })
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
		case <-m.Stopped():
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
			if m.leaveIfRemoved(ctx, err, node) {
				return
			}
			m.log.WithError(err).WithField("node_id", node.ID).Warn("Failed to sync global config to node")
		}
	}

	// --- Phase 2: Sync the membership ---
	list, err := m.clusterManager.NodeListToSend(ctx, localNodeID)
	if err != nil {
		m.log.WithError(err).Error("Failed to list the membership for node-sync")
		return
	}

	for _, targetNode := range nodes {
		if targetNode.ID == localNodeID {
			continue
		}
		if err := m.sendNodeListToNode(ctx, list, targetNode, localNodeID, nodeToken); err != nil {
			m.log.WithError(err).WithField("node_id", targetNode.ID).Warn("Failed to sync node list to node")
		}
	}

	m.SyncKEKs(ctx)
}

// SyncKEKs pushes every cluster-shared encryption key to all healthy peers.
// Also called synchronously right after a KEK rotation.
func (m *GlobalConfigSyncManager) SyncKEKs(ctx context.Context) {
	if m.kekProvider == nil || !m.clusterManager.IsClusterEnabled() {
		return
	}
	records := m.kekProvider.ClusterSharedKeys()
	if len(records) == 0 {
		return
	}

	localNodeID, err := m.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		return
	}
	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return
	}
	nodes, err := m.clusterManager.GetHealthyNodes(ctx)
	if err != nil {
		return
	}

	body, err := json.Marshal(map[string]interface{}{
		"keys":           records,
		"source_node_id": localNodeID,
	})
	if err != nil {
		return
	}

	for _, node := range nodes {
		if node.ID == localNodeID {
			continue
		}
		url := fmt.Sprintf("%s/api/internal/cluster/kek-sync", node.Endpoint)
		req, err := m.proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(body), localNodeID, nodeToken)
		if err != nil {
			m.log.WithError(err).WithField("node_id", node.ID).Warn("Failed to build KEK sync request")
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := m.proxyClient.DoAuthenticatedRequest(req)
		if err != nil {
			m.log.WithError(err).WithField("node_id", node.ID).Warn("Failed to sync encryption KEKs to node")
			continue
		}
		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			m.log.WithFields(logrus.Fields{"node_id": node.ID, "status": resp.StatusCode, "body": string(respBody)}).
				Warn("KEK sync rejected by node")
		}
		resp.Body.Close()
	}
}

// IsNodeLocalConfig reports whether a global configuration entry is this
// node's own: the degraded reason is what this node sees of the others' health.
// It is neither sent nor taken from another node.
func IsNodeLocalConfig(key string) bool {
	return key == clusterDegradedReasonKey
}

// listGlobalConfig returns the entries of cluster_global_config sent to
// other nodes: all but this node's own.
func (m *GlobalConfigSyncManager) listGlobalConfig(ctx context.Context) ([]GlobalConfigEntry, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT key, value, updated_at FROM cluster_global_config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []GlobalConfigEntry
	for rows.Next() {
		var e GlobalConfigEntry
		var updatedAt interface{}
		if err := rows.Scan(&e.Key, &e.Value, &updatedAt); err != nil {
			return nil, err
		}
		if ts, ok := SQLiteTimestampUnix(updatedAt); ok {
			e.UpdatedAt = ts
		}
		if IsNodeLocalConfig(e.Key) {
			continue
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

	return m.post(ctx, node, "/api/internal/cluster/global-config-sync", body, sourceNodeID, nodeToken)
}

// sendNodeListToNode sends this node's view of the membership to target.
func (m *GlobalConfigSyncManager) sendNodeListToNode(ctx context.Context, list *NodeList, target *Node, sourceNodeID, nodeToken string) error {
	body, err := json.Marshal(list)
	if err != nil {
		return fmt.Errorf("failed to marshal node list: %w", err)
	}
	return m.post(ctx, target, "/api/internal/cluster/node-list-sync", body, sourceNodeID, nodeToken)
}

// post sends body to path on node. A node that removed this one answers 410.
func (m *GlobalConfigSyncManager) post(ctx context.Context, node *Node, path string, body []byte, sourceNodeID, nodeToken string) error {
	req, err := m.proxyClient.CreateAuthenticatedRequest(ctx, "POST", node.Endpoint+path, bytes.NewReader(body), sourceNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusGone {
		return ErrNodeRemoved
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// SyncNow sends the global configuration and the membership to every healthy
// node now.
func (m *GlobalConfigSyncManager) SyncNow(ctx context.Context) {
	m.syncAll(ctx)
}

// AnnounceLeave sends every other node the removal of this node, before it
// leaves the cluster. A node it does not reach takes the removal from the
// others.
func (m *GlobalConfigSyncManager) AnnounceLeave(ctx context.Context) error {
	list, err := m.clusterManager.LeaveRemoval(ctx)
	if err != nil {
		return err
	}
	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return err
	}
	nodes, err := m.clusterManager.ListNodes(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(list)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if n.ID == list.SourceNodeID {
			continue
		}
		if err := m.post(ctx, n, "/api/internal/cluster/node-list-sync", body, list.SourceNodeID, nodeToken); err != nil {
			m.log.WithError(err).WithField("node_id", n.ID).Warn("Failed to tell a node this one leaves the cluster")
		}
	}
	return nil
}

// leaveIfRemoved takes this node out of the cluster when another node answered
// that it was removed.
func (m *GlobalConfigSyncManager) leaveIfRemoved(ctx context.Context, err error, from *Node) bool {
	if !errors.Is(err, ErrNodeRemoved) {
		return false
	}
	m.log.WithField("node_id", from.ID).Warn("This node was removed from the cluster; leaving it")
	if err := m.clusterManager.LeaveCluster(ctx); err != nil {
		m.log.WithError(err).Error("Failed to leave the cluster")
	}
	return true
}
