package server

import (
	"fmt"
	"time"

	"github.com/maxiofs/maxiofs/internal/cluster"
)

// broadcastStoragePressureEvent translates a cluster.StoragePressureEvent into
// an SSE notification payload and dispatches it through the notification hub.
// Wired as the StoragePressureEmitter on the cluster Manager in server.New.
func (s *Server) broadcastStoragePressureEvent(ev cluster.StoragePressureEvent) {
	if s.notificationHub == nil {
		return
	}

	name := ev.NodeName
	if name == "" {
		name = ev.NodeID
	}
	if name == "" {
		name = "(unknown)"
	}

	var msg string
	switch ev.Kind {
	case "node_storage_pressure":
		msg = fmt.Sprintf(
			"Cluster node %s crossed the storage-pressure threshold (%.1f%% ≥ %.1f%%) — excluded from new-write target selection",
			name, ev.UsagePercent, ev.ThresholdPercent,
		)
	case "node_storage_pressure_resolved":
		msg = fmt.Sprintf(
			"Cluster node %s recovered from storage pressure (now at %.1f%%) — re-enabled for new writes",
			name, ev.UsagePercent,
		)
	default:
		msg = ev.Kind
	}

	s.notificationHub.SendNotification(&Notification{
		Type:    ev.Kind,
		Message: msg,
		Data: map[string]interface{}{
			"node_id":           ev.NodeID,
			"node_name":         ev.NodeName,
			"usage_percent":     ev.UsagePercent,
			"threshold_percent": ev.ThresholdPercent,
		},
		Timestamp: time.Now().Unix(),
	})
}
