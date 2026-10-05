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
		if cluster.IsNodeLocalConfig(entry.Key) {
			continue
		}
		// Last-writer-wins: only apply if the incoming entry is newer.
		localVal, err := cluster.GetGlobalConfig(ctx, s.db, entry.Key)
		if err == nil {
			// Key exists locally — check timestamp
			var rawLocalUpdatedAt interface{}
			_ = s.db.QueryRowContext(ctx,
				`SELECT updated_at FROM cluster_global_config WHERE key = ?`,
				entry.Key,
			).Scan(&rawLocalUpdatedAt)
			localUpdatedAt, _ := cluster.SQLiteTimestampUnix(rawLocalUpdatedAt)
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

// handleReceiveNodeListSync takes another node's view of the membership.
func (s *Server) handleReceiveNodeListSync(w http.ResponseWriter, r *http.Request) {
	sourceNodeID, ok := r.Context().Value("cluster_node_id").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var list cluster.NodeList
	if err := json.NewDecoder(r.Body).Decode(&list); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	list.SourceNodeID = sourceNodeID

	drained, err := s.clusterManager.ApplyNodeList(r.Context(), &list)
	if err != nil {
		logrus.WithError(err).WithField("source_node_id", sourceNodeID).Warn("Failed to apply a node list")
		http.Error(w, "Failed to apply node list", http.StatusInternalServerError)
		return
	}
	if len(drained) > 0 && s.deadNodeReconciler != nil {
		s.goWorker("apply drained nodes", func() { s.deadNodeReconciler.ApplyDrains(s.backgroundContext()) })
	}

	logrus.WithFields(logrus.Fields{
		"source_node_id": sourceNodeID,
		"nodes_received": len(list.Nodes),
		"removed":        len(list.Removed),
	}).Debug("Node list sync received")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// syncMembershipNow sends the membership to the other nodes without waiting
// for the next periodic sync.
func (s *Server) syncMembershipNow() {
	if s.globalConfigSyncMgr == nil {
		return
	}
	s.goWorker("sync membership", func() { s.globalConfigSyncMgr.SyncNow(s.backgroundContext()) })
}
