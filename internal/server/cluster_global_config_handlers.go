package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// handleReceiveGlobalConfigSync handles incoming global config synchronization.
// Uses last-writer-wins with updated_at timestamp to resolve conflicts.
func (s *Server) handleReceiveGlobalConfigSync(w http.ResponseWriter, r *http.Request) {
	sourceNodeID, ok := r.Context().Value("cluster_node_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Entries      []cluster.GlobalConfigEntry `json:"entries"`
		SourceNodeID string                      `json:"source_node_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	applied := 0
	for _, entry := range req.Entries {
		// Last-writer-wins: only apply if the incoming entry is newer.
		localVal, err := cluster.GetGlobalConfig(ctx, s.db, entry.Key)
		if err == nil {
			// Key exists locally — check timestamp
			var localUpdatedAt int64
			_ = s.db.QueryRowContext(ctx,
				`SELECT CAST(strftime('%s', updated_at) AS INTEGER) FROM cluster_global_config WHERE key = ?`,
				entry.Key,
			).Scan(&localUpdatedAt)
			if localUpdatedAt >= entry.UpdatedAt {
				continue // local is same age or newer, skip
			}
			_ = localVal // suppress unused warning
		}
		// Apply
		now := time.Unix(entry.UpdatedAt, 0)
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO cluster_global_config (key, value, created_at, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = ?, updated_at = ?
		`, entry.Key, entry.Value, now, now, entry.Value, now)
		if err != nil {
			logrus.WithError(err).WithField("key", entry.Key).Warn("Failed to apply synced global config")
			continue
		}
		applied++
	}

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"entries":        len(req.Entries),
		"applied":        applied,
	}).Debug("Global config sync received")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "applied": applied})
}

// handleReceiveNodeListSync handles incoming node list reconciliation.
// Adds any nodes that are missing from the local cluster_nodes table.
func (s *Server) handleReceiveNodeListSync(w http.ResponseWriter, r *http.Request) {
	sourceNodeID, ok := r.Context().Value("cluster_node_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Nodes []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Endpoint  string `json:"endpoint"`
			NodeToken string `json:"node_token"`
			Region    string `json:"region"`
			Priority  int    `json:"priority"`
		} `json:"nodes"`
		SourceNodeID string `json:"source_node_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	added := 0
	for _, n := range req.Nodes {
		node := &cluster.Node{
			ID:        n.ID,
			Name:      n.Name,
			Endpoint:  n.Endpoint,
			NodeToken: n.NodeToken,
			Region:    n.Region,
			Priority:  n.Priority,
		}
		// AddNode uses INSERT OR REPLACE — safe to call even if node exists.
		// This updates the priority and token if they changed.
		if err := s.clusterManager.AddNode(ctx, node); err != nil {
			logrus.WithError(err).WithField("node_id", n.ID).Warn("Failed to add synced node")
			continue
		}
		added++
	}

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"nodes_received": len(req.Nodes),
		"nodes_applied":  added,
	}).Debug("Node list sync received")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "added": added})
}
