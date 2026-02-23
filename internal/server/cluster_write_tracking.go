package server

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
)

// touchLocalWriteAt stamps last_local_write_at on the local cluster node row
// whenever a client-facing mutation succeeds.  This lets the StaleReconciler
// distinguish ModeOffline (no writes during isolation) from ModePartition
// (had writes → bidirectional LWW merge required).
//
// Best-effort: failures are logged at debug level and never propagated to the
// caller so they cannot affect the HTTP response.
func (s *Server) touchLocalWriteAt(ctx context.Context) {
	if s.clusterManager == nil || !s.clusterManager.IsClusterEnabled() {
		return
	}
	localNodeID, err := s.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		logrus.WithError(err).Debug("touchLocalWriteAt: failed to get local node ID")
		return
	}
	if _, err := s.db.ExecContext(ctx,
		"UPDATE cluster_nodes SET last_local_write_at = ? WHERE id = ?",
		time.Now(), localNodeID,
	); err != nil {
		logrus.WithError(err).Debug("touchLocalWriteAt: failed to update last_local_write_at")
	}
}
