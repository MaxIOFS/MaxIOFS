package server

import (
	"time"

	"github.com/maxiofs/maxiofs/internal/cluster"
)

// broadcastDeadNodeEvent translates a cluster.DeadNodeEvent into an SSE
// notification payload and dispatches it through the notification hub.
// Wired as the EventEmitter of DeadNodeReconciler in server.New.
func (s *Server) broadcastDeadNodeEvent(ev cluster.DeadNodeEvent) {
	if s.notificationHub == nil {
		return
	}

	var msg string
	switch ev.Kind {
	case cluster.EventNodeDead:
		msg = "Cluster node " + displayNodeName(ev) + " has been marked dead and its replicas are being redistributed"
	case cluster.EventClusterDegraded:
		msg = "Cluster is degraded: " + ev.Reason
	case cluster.EventClusterDegradedResolved:
		msg = "Cluster degraded state resolved: " + ev.Reason
	default:
		msg = string(ev.Kind)
	}

	s.notificationHub.SendNotification(&Notification{
		Type:    string(ev.Kind),
		Message: msg,
		Data: map[string]interface{}{
			"node_id":        ev.NodeID,
			"node_name":      ev.NodeName,
			"reason":         ev.Reason,
			"factor":         ev.Factor,
			"non_dead_nodes": ev.NonDeadNodes,
		},
		Timestamp: time.Now().Unix(),
	})
}

func displayNodeName(ev cluster.DeadNodeEvent) string {
	if ev.NodeName != "" {
		return ev.NodeName
	}
	if ev.NodeID != "" {
		return ev.NodeID
	}
	return "(unknown)"
}
