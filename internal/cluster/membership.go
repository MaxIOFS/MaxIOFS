package cluster

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// NodeList is what a node sends the others of the cluster's membership: every
// node it holds, with the fields every node keeps alike, and the nodes
// removed from the cluster with the time of their removal.
type NodeList struct {
	Nodes        []*JoinPackageNode `json:"nodes"`
	Removed      map[string]int64   `json:"removed,omitempty"`
	SourceNodeID string             `json:"source_node_id"`
}

// ErrNodeRemoved is the answer a node gets from the others once it has been
// removed from the cluster.
var ErrNodeRemoved = errors.New("this node was removed from the cluster")

// nodeChangeAfter is the time to stamp a change of a node last changed at
// last: now, or later than last.
func nodeChangeAfter(last int64) int64 {
	if now := time.Now().Unix(); now > last {
		return now
	}
	return last + 1
}

// NodeListToSend is this node's view of the membership.
func (m *Manager) NodeListToSend(ctx context.Context, sourceNodeID string) (*NodeList, error) {
	nodes, err := m.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	removed, err := ListDeletions(ctx, m.db, EntityTypeClusterNode)
	if err != nil {
		return nil, err
	}
	list := &NodeList{Nodes: NodesToJoinPackage(nodes), SourceNodeID: sourceNodeID}
	if len(removed) > 0 {
		list.Removed = make(map[string]int64, len(removed))
		for _, d := range removed {
			list.Removed[d.EntityID] = d.DeletedAt
		}
	}
	return list, nil
}

// ApplyNodeList takes another node's view of the membership: its removals
// first, then each node it holds that changed after this node's copy and was
// never removed. It returns the nodes it marked drained.
func (m *Manager) ApplyNodeList(ctx context.Context, list *NodeList) (drained []string, err error) {
	localID, _ := m.GetLocalNodeID(ctx)
	for id, at := range list.Removed {
		if id == "" || id == localID || at <= 0 {
			continue
		}
		if err := m.RemoveNodeAt(ctx, id, at, list.SourceNodeID); err != nil {
			return drained, err
		}
	}
	for _, n := range list.Nodes {
		if n == nil || n.ID == "" {
			continue
		}
		if m.RemovedFromCluster(ctx, n.ID) {
			continue
		}
		var localChanged int64
		var wasDrained bool
		err := m.db.QueryRowContext(ctx, `SELECT changed_at, drained FROM cluster_nodes WHERE id = ?`, n.ID).Scan(&localChanged, &wasDrained)
		switch {
		case err == nil && localChanged >= n.ChangedAt:
			continue
		case err != nil && err != sql.ErrNoRows:
			return drained, fmt.Errorf("read node %s: %w", n.ID, err)
		}
		if _, err := m.db.ExecContext(ctx, `
			INSERT INTO cluster_nodes (id, name, endpoint, api_url, node_token, region, priority,
				health_status, metadata, changed_at, drained)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, '{}', ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name       = excluded.name,
				endpoint   = excluded.endpoint,
				api_url    = CASE WHEN excluded.api_url != '' THEN excluded.api_url ELSE cluster_nodes.api_url END,
				node_token = CASE WHEN excluded.node_token != '' THEN excluded.node_token ELSE cluster_nodes.node_token END,
				region     = excluded.region,
				priority   = excluded.priority,
				changed_at = excluded.changed_at,
				drained    = excluded.drained
		`, n.ID, n.Name, n.Endpoint, n.APIURL, n.NodeToken, n.Region, n.Priority,
			HealthStatusUnknown, n.ChangedAt, n.Drained); err != nil {
			return drained, fmt.Errorf("store node %s: %w", n.ID, err)
		}
		if n.Drained && !wasDrained {
			drained = append(drained, n.ID)
		}
	}
	return drained, nil
}

// RemoveNodeAt removes a node from this node's membership with a removal made
// at deletedAt by byNode. The removal is kept so that no copy of the node
// brings it back.
func (m *Manager) RemoveNodeAt(ctx context.Context, nodeID string, deletedAt int64, byNode string) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.ExecContext(ctx, `DELETE FROM cluster_nodes WHERE id = ?`, nodeID); err != nil {
		return fmt.Errorf("remove node %s: %w", nodeID, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM cluster_deletion_log_delivery WHERE node_id = ?`, nodeID); err != nil {
		return fmt.Errorf("remove node %s: %w", nodeID, err)
	}
	if err := RecordDeletion(ctx, tx, EntityTypeClusterNode, nodeID, byNode, deletedAt); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	m.dropQueuedMetadataOps(ctx, nodeID)
	return nil
}

// RemovedFromCluster reports whether nodeID was removed from the cluster.
func (m *Manager) RemovedFromCluster(ctx context.Context, nodeID string) bool {
	removed, err := HasDeletion(ctx, m.db, EntityTypeClusterNode, nodeID)
	return err == nil && removed
}

// LeaveRemoval is the removal of this node the others are sent when it leaves.
func (m *Manager) LeaveRemoval(ctx context.Context) (*NodeList, error) {
	localID, err := m.GetLocalNodeID(ctx)
	if err != nil {
		return nil, err
	}
	var changed int64
	if err := m.db.QueryRowContext(ctx, `SELECT changed_at FROM cluster_nodes WHERE id = ?`, localID).Scan(&changed); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return &NodeList{Removed: map[string]int64{localID: DeletedAfter(changed)}, SourceNodeID: localID}, nil
}

// setDrained marks a node drained on every node: the mark travels with the
// node's other fields.
func (m *Manager) setDrained(ctx context.Context, q sqlQuerier, nodeID string) error {
	var changed int64
	if err := q.QueryRowContext(ctx, `SELECT changed_at FROM cluster_nodes WHERE id = ?`, nodeID).Scan(&changed); err != nil {
		return fmt.Errorf("read node %s: %w", nodeID, err)
	}
	_, err := q.ExecContext(ctx, `UPDATE cluster_nodes SET drained = 1, changed_at = ? WHERE id = ?`,
		nodeChangeAfter(changed), nodeID)
	if err == nil {
		m.log.WithField("node_id", nodeID).Info("Node drained")
	}
	return err
}
