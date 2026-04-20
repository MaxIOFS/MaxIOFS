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
	"sort"
	"strconv"
	"time"

	"github.com/sirupsen/logrus"
)

// GroupData represents a group plus its membership for synchronization.
// Members are carried in the same payload so that the receiver can apply the
// authoritative membership set in a single transaction (preventing partial states
// where the group exists but its members are missing).
type GroupData struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Description string   `json:"description"`
	TenantID    string   `json:"tenant_id"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
	MemberIDs   []string `json:"member_ids"`
}

// GroupSyncManager handles automatic group synchronization between cluster nodes.
type GroupSyncManager struct {
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	stopChan       chan struct{}
	log            *logrus.Entry
}

// NewGroupSyncManager creates a new group sync manager.
func NewGroupSyncManager(db *sql.DB, clusterManager *Manager) *GroupSyncManager {
	return &GroupSyncManager{
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    NewDynamicProxyClient(clusterManager.GetTLSConfig),
		stopChan:       make(chan struct{}),
		log:            logrus.WithField("component", "group-sync"),
	}
}

// Start begins the group synchronization loop.
func (m *GroupSyncManager) Start(ctx context.Context) {
	m.proxyClient = NewDynamicProxyClient(m.clusterManager.GetTLSConfig)

	intervalStr, err := GetGlobalConfig(ctx, m.db, "group_sync_interval_seconds")
	if err != nil {
		m.log.WithError(err).Warn("Failed to get group sync interval, using default 30s")
		intervalStr = "30"
	}

	interval, err := strconv.Atoi(intervalStr)
	if err != nil {
		m.log.WithError(err).Warn("Invalid group sync interval, using default 30s")
		interval = 30
	}

	enabledStr, err := GetGlobalConfig(ctx, m.db, "auto_group_sync_enabled")
	if err != nil || enabledStr != "true" {
		m.log.Info("Automatic group synchronization is disabled")
		return
	}

	m.log.WithField("interval_seconds", interval).Info("Starting group synchronization manager")

	go m.syncLoop(ctx, time.Duration(interval)*time.Second)
}

func (m *GroupSyncManager) syncLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	m.syncAllGroups(ctx)

	for {
		select {
		case <-ctx.Done():
			m.log.Info("Group sync loop stopped")
			return
		case <-m.stopChan:
			m.log.Info("Group sync loop stopped")
			return
		case <-ticker.C:
			m.syncAllGroups(ctx)
		}
	}
}

// syncAllGroups synchronizes all groups (and their members) to all healthy nodes.
func (m *GroupSyncManager) syncAllGroups(ctx context.Context) {
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

	var targetNodes []*Node
	for _, node := range nodes {
		if node.ID != localNodeID {
			targetNodes = append(targetNodes, node)
		}
	}

	if len(targetNodes) == 0 {
		m.log.Debug("No target nodes for group synchronization")
		return
	}

	groups, err := m.listLocalGroups(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to list local groups")
		return
	}

	m.log.WithFields(logrus.Fields{
		"group_count": len(groups),
		"node_count":  len(targetNodes),
	}).Debug("Starting group synchronization")

	for _, group := range groups {
		for _, node := range targetNodes {
			if err := m.syncGroupToNode(ctx, group, node, localNodeID); err != nil {
				m.log.WithFields(logrus.Fields{
					"group_id": group.ID,
					"name":     group.Name,
					"node_id":  node.ID,
					"error":    err,
				}).Warn("Failed to sync group to node")
			}
		}
	}

	m.syncDeletions(ctx, targetNodes, localNodeID)
}

func (m *GroupSyncManager) syncGroupToNode(ctx context.Context, group *GroupData, node *Node, sourceNodeID string) error {
	checksum := m.computeGroupChecksum(group)

	needsSync, err := m.needsSynchronization(ctx, group.ID, node.ID, checksum)
	if err != nil {
		return fmt.Errorf("failed to check sync status: %w", err)
	}
	if !needsSync {
		return nil
	}

	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node token: %w", err)
	}

	if err := m.sendGroupToNode(ctx, group, node, sourceNodeID, nodeToken); err != nil {
		return fmt.Errorf("failed to send group: %w", err)
	}

	if err := m.updateSyncStatus(ctx, group.ID, sourceNodeID, node.ID, checksum); err != nil {
		m.log.WithError(err).Warn("Failed to update group sync status")
	}

	m.log.WithFields(logrus.Fields{
		"group_id":  group.ID,
		"name":      group.Name,
		"node_id":   node.ID,
		"node_name": node.Name,
	}).Info("Group synchronized successfully")

	return nil
}

func (m *GroupSyncManager) sendGroupToNode(ctx context.Context, group *GroupData, node *Node, sourceNodeID, nodeToken string) error {
	payload, err := json.Marshal(group)
	if err != nil {
		return fmt.Errorf("failed to marshal group: %w", err)
	}

	url := fmt.Sprintf("%s/api/internal/cluster/group-sync", node.Endpoint)

	req, err := m.proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(payload), sourceNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// listLocalGroups loads all groups and their member IDs from the local database.
func (m *GroupSyncManager) listLocalGroups(ctx context.Context) ([]*GroupData, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(display_name, ''), COALESCE(description, ''),
		       COALESCE(tenant_id, ''), created_at, updated_at
		FROM groups
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query groups: %w", err)
	}
	defer rows.Close()

	var groups []*GroupData
	for rows.Next() {
		g := &GroupData{}
		if err := rows.Scan(&g.ID, &g.Name, &g.DisplayName, &g.Description, &g.TenantID, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan group: %w", err)
		}
		groups = append(groups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, g := range groups {
		members, err := m.loadGroupMemberIDs(ctx, g.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to load members for group %s: %w", g.ID, err)
		}
		g.MemberIDs = members
	}

	return groups, nil
}

func (m *GroupSyncManager) loadGroupMemberIDs(ctx context.Context, groupID string) ([]string, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT user_id FROM group_members WHERE group_id = ?`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, rows.Err()
}

// computeGroupChecksum builds a stable hash over the group fields and its sorted member set.
// Membership changes alone must trigger re-sync, so we include the sorted member IDs.
func (m *GroupSyncManager) computeGroupChecksum(g *GroupData) string {
	memberJoined := ""
	for _, id := range g.MemberIDs {
		memberJoined += id + ","
	}
	data := fmt.Sprintf("%s|%s|%s|%s|%d|%s",
		g.Name, g.DisplayName, g.Description, g.TenantID, g.UpdatedAt, memberJoined,
	)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

func (m *GroupSyncManager) needsSynchronization(ctx context.Context, groupID, nodeID, checksum string) (bool, error) {
	var existing string
	err := m.db.QueryRowContext(ctx, `
		SELECT group_checksum FROM cluster_group_sync
		WHERE group_id = ? AND destination_node_id = ?
	`, groupID, nodeID).Scan(&existing)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return existing != checksum, nil
}

func (m *GroupSyncManager) updateSyncStatus(ctx context.Context, groupID, sourceNodeID, destNodeID, checksum string) error {
	now := time.Now().Unix()
	id := fmt.Sprintf("%s-%s", groupID, destNodeID)

	_, err := m.db.ExecContext(ctx, `
		INSERT INTO cluster_group_sync (id, group_id, source_node_id, destination_node_id, group_checksum, status, last_sync_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'synced', ?, ?, ?)
		ON CONFLICT(group_id, destination_node_id) DO UPDATE SET
			group_checksum = excluded.group_checksum,
			last_sync_at = excluded.last_sync_at,
			updated_at = excluded.updated_at
	`, id, groupID, sourceNodeID, destNodeID, checksum, now, now, now)
	return err
}

// syncDeletions sends group deletion tombstones to all target nodes.
// Tombstones already delivered (checksum == "DELETED") are skipped.
func (m *GroupSyncManager) syncDeletions(ctx context.Context, targetNodes []*Node, localNodeID string) {
	deletions, err := ListDeletions(ctx, m.db, EntityTypeGroup)
	if err != nil {
		m.log.WithError(err).Error("Failed to list group deletion tombstones")
		return
	}
	if len(deletions) == 0 {
		return
	}

	nodeToken, err := m.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get node token for group deletion sync")
		return
	}

	for _, deletion := range deletions {
		for _, node := range targetNodes {
			if m.isDeletionDelivered(ctx, deletion.EntityID, node.ID) {
				continue
			}
			if err := m.sendDeletionToNode(ctx, deletion.EntityID, deletion.DeletedAt, node, localNodeID, nodeToken); err != nil {
				m.log.WithFields(logrus.Fields{
					"group_id": deletion.EntityID,
					"node_id":  node.ID,
					"error":    err,
				}).Warn("Failed to send group deletion to node")
				continue
			}
			if err := m.updateSyncStatus(ctx, deletion.EntityID, localNodeID, node.ID, "DELETED"); err != nil {
				m.log.WithError(err).Warn("Failed to record group tombstone delivery status")
			}
		}
	}
}

func (m *GroupSyncManager) isDeletionDelivered(ctx context.Context, groupID, destNodeID string) bool {
	var checksum string
	err := m.db.QueryRowContext(ctx, `
		SELECT group_checksum FROM cluster_group_sync
		WHERE group_id = ? AND destination_node_id = ?
	`, groupID, destNodeID).Scan(&checksum)
	return err == nil && checksum == "DELETED"
}

func (m *GroupSyncManager) sendDeletionToNode(ctx context.Context, groupID string, deletedAt int64, node *Node, sourceNodeID, nodeToken string) error {
	payload, _ := json.Marshal(map[string]interface{}{"id": groupID, "deleted_at": deletedAt})

	url := fmt.Sprintf("%s/api/internal/cluster/group-delete-sync", node.Endpoint)

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

// Stop stops the group sync manager.
func (m *GroupSyncManager) Stop() {
	close(m.stopChan)
}

// TriggerSync runs a full group sync immediately in a goroutine.
func (m *GroupSyncManager) TriggerSync(ctx context.Context) {
	go m.syncAllGroups(ctx)
}

// SyncToNode immediately pushes all local groups to the given node.
// Used to bootstrap a newly-joined node without waiting for the periodic ticker.
func (m *GroupSyncManager) SyncToNode(ctx context.Context, node *Node) {
	if !m.clusterManager.IsClusterEnabled() {
		return
	}

	pc := NewDynamicProxyClient(m.clusterManager.GetTLSConfig)
	savedClient := m.proxyClient
	m.proxyClient = pc
	defer func() { m.proxyClient = savedClient }()

	localNodeID, err := m.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		m.log.WithError(err).Error("SyncToNode(groups): failed to get local node ID")
		return
	}

	groups, err := m.listLocalGroups(ctx)
	if err != nil {
		m.log.WithError(err).Error("SyncToNode(groups): failed to list local groups")
		return
	}

	for _, g := range groups {
		if err := m.syncGroupToNode(ctx, g, node, localNodeID); err != nil {
			m.log.WithFields(logrus.Fields{
				"group_id": g.ID,
				"node_id":  node.ID,
				"error":    err,
			}).Warn("SyncToNode(groups): failed to sync group")
		}
	}

	m.syncDeletions(ctx, []*Node{node}, localNodeID)

	m.log.WithFields(logrus.Fields{
		"group_count": len(groups),
		"node_id":     node.ID,
		"node_name":   node.Name,
	}).Info("Immediate group sync to new node completed")
}
