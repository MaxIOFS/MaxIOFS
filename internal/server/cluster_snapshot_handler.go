package server

import (
	"encoding/json"
	"net/http"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// handleGetStateSnapshot returns the full local entity state as a snapshot.
// Consumed by the StaleReconciler when a node reconnects after a partition or
// stale period.  Authenticated via HMAC (internalClusterRouter).
// GET /api/internal/cluster/state-snapshot
func (s *Server) handleGetStateSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	localNodeID, err := s.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		logrus.WithError(err).Error("Failed to get local node ID for state snapshot")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	snap, err := cluster.BuildLocalSnapshot(ctx, localNodeID, s.db)
	if err != nil {
		logrus.WithError(err).Error("Failed to build state snapshot")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snap); err != nil {
		logrus.WithError(err).Error("Failed to encode state snapshot response")
	}
}
