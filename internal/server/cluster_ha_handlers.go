package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/erasure"
	"github.com/sirupsen/logrus"
)

// handleGetClusterHA returns how the cluster keeps its data — the replication
// factor, or parity with the shards of a write — and node status.
// GET /cluster/ha
func (s *Server) handleGetClusterHA(w http.ResponseWriter, r *http.Request) {
	if currentUser := s.getAuthUser(r); currentUser == nil || !s.isGlobalAdmin(currentUser) {
		s.writeError(w, "Access denied: global admin required", http.StatusForbidden)
		return
	}

	if !s.clusterManager.IsClusterEnabled() {
		s.writeError(w, "Cluster is not enabled", http.StatusBadRequest)
		return
	}

	protection, err := s.clusterManager.GetProtection(r.Context())
	if err != nil {
		s.writeError(w, "Failed to get replication factor: "+err.Error(), http.StatusInternalServerError)
		return
	}
	factor := protection.Factor

	// Refresh all nodes' capacity by running a health check on each before listing.
	allNodes, _ := s.clusterManager.ListNodes(r.Context())
	for _, n := range allNodes {
		s.clusterManager.CheckNodeHealth(r.Context(), n.ID) //nolint:errcheck
	}

	nodes, err := s.clusterManager.ListNodes(r.Context())
	if err != nil {
		s.writeError(w, "Failed to list nodes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Calculate usable capacity based on factor and cluster total
	var totalBytes int64
	for _, n := range nodes {
		totalBytes += n.CapacityTotal
	}
	var usableBytes int64
	if factor > 0 {
		usableBytes = totalBytes / int64(factor)
	}

	// How many node failures the cluster can tolerate
	toleratedFailures := factor - 1

	// With parity a write is cut in data and parity shards, each on its own
	// node; too few nodes keep it in two whole copies.
	var dataShards, parityShards int
	if protection.Parity {
		usableBytes, toleratedFailures = totalBytes/2, 1
		if layout, ok, err := s.clusterManager.ParityLayout(r.Context()); err == nil && ok {
			dataShards, parityShards = layout.Data, layout.Parity
			usableBytes = totalBytes * int64(layout.Data) / int64(layout.Width())
			toleratedFailures = layout.Parity
		}
	}

	type nodeStatus struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		HealthStatus  string `json:"health_status"`
		CapacityTotal int64  `json:"capacity_total"`
		CapacityUsed  int64  `json:"capacity_used"`
		CapacityFree  int64  `json:"capacity_free"`
		Drained       bool   `json:"drained"`
		// DrainProgress is how far a node being drained is in moving its
		// objects.
		DrainProgress *cluster.DrainProgress `json:"drain_progress,omitempty"`
	}

	var localNodeID string
	if cfg, cfgErr := s.clusterManager.GetConfig(r.Context()); cfgErr == nil {
		localNodeID = cfg.NodeID
	}

	nodeStatuses := make([]nodeStatus, 0, len(nodes))
	for _, n := range nodes {
		st := nodeStatus{
			ID:            n.ID,
			Name:          n.Name,
			HealthStatus:  n.HealthStatus,
			CapacityTotal: n.CapacityTotal,
			CapacityUsed:  n.CapacityUsed,
			CapacityFree:  n.CapacityTotal - n.CapacityUsed,
			Drained:       n.Drained,
		}
		if n.Drained && n.HealthStatus != cluster.HealthStatusDead {
			if n.ID == localNodeID && s.haSyncWorker != nil {
				st.DrainProgress = s.haSyncWorker.DrainProgress()
			} else {
				st.DrainProgress = s.clusterManager.DrainProgressOf(n.ID)
			}
		}
		nodeStatuses = append(nodeStatuses, st)
	}

	s.writeJSON(w, map[string]interface{}{
		"replication_factor": factor,
		"parity":             protection.Parity,
		"data_shards":        dataShards,
		"parity_shards":      parityShards,
		"node_count":         len(nodes),
		"tolerated_failures": toleratedFailures,
		"total_bytes":        totalBytes,
		"usable_bytes":       usableBytes,
		"local_node_id":      localNodeID,
		"nodes":              nodeStatuses,
	})
}

// handleGetHASyncJobs returns all HA initial-sync and delta-sync job records.
// GET /cluster/ha/sync-jobs
func (s *Server) handleGetHASyncJobs(w http.ResponseWriter, r *http.Request) {
	if currentUser := s.getAuthUser(r); currentUser == nil || !s.isGlobalAdmin(currentUser) {
		s.writeError(w, "Access denied: global admin required", http.StatusForbidden)
		return
	}
	if s.haSyncWorker == nil {
		s.writeJSON(w, map[string]interface{}{"sync_jobs": []struct{}{}})
		return
	}
	jobs, err := s.haSyncWorker.GetSyncJobs(r.Context())
	if err != nil {
		s.writeError(w, "Failed to get sync jobs: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if jobs == nil {
		jobs = []cluster.SyncJobStatus{}
	}
	s.writeJSON(w, map[string]interface{}{"sync_jobs": jobs})
}

// handleGetHAScrubStatus returns the most recent anti-entropy scrubber runs
// plus the in-progress checkpoint when a cycle is active.
// GET /cluster/ha/scrub-status
func (s *Server) handleGetHAScrubStatus(w http.ResponseWriter, r *http.Request) {
	if currentUser := s.getAuthUser(r); currentUser == nil || !s.isGlobalAdmin(currentUser) {
		s.writeError(w, "Access denied: global admin required", http.StatusForbidden)
		return
	}
	if s.antiEntropyScrubber == nil {
		s.writeJSON(w, map[string]interface{}{"runs": []struct{}{}, "current": nil})
		return
	}

	runs, err := s.antiEntropyScrubber.ListRecentRuns(r.Context(), 10)
	if err != nil {
		s.writeError(w, "Failed to list scrub runs: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []cluster.ScrubRun{}
	}

	current := s.antiEntropyScrubber.CurrentCheckpoint()
	s.writeJSON(w, map[string]interface{}{
		"runs":    runs,
		"current": current,
	})
}

// handleDrainClusterNode starts the drain of a node, this one included (see
// DeadNodeReconciler.DrainNode).
func (s *Server) handleDrainClusterNode(w http.ResponseWriter, r *http.Request) {
	if currentUser := s.getAuthUser(r); currentUser == nil || !s.isGlobalAdmin(currentUser) {
		s.writeError(w, "Access denied: global admin required", http.StatusForbidden)
		return
	}

	if !s.clusterManager.IsClusterEnabled() {
		s.writeError(w, "Cluster is not enabled", http.StatusBadRequest)
		return
	}
	if s.deadNodeReconciler == nil {
		s.writeError(w, "Dead-node reconciler is not running", http.StatusServiceUnavailable)
		return
	}

	vars := mux.Vars(r)
	nodeID := vars["nodeId"]
	if nodeID == "" {
		s.writeError(w, "Node ID is required", http.StatusBadRequest)
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	// Body is optional — ignore decode errors when no body is supplied.
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Reason == "" {
		body.Reason = "manual drain"
	}

	if err := s.deadNodeReconciler.DrainNode(r.Context(), nodeID, body.Reason); err != nil {
		logrus.WithError(err).WithField("node_id", nodeID).Error("Drain failed")
		status := http.StatusInternalServerError
		if errors.Is(err, cluster.ErrBelowReplicationFactor) || errors.Is(err, cluster.ErrDrainBelowReplicationFactor) {
			status = http.StatusConflict
		}
		s.writeError(w, "Drain failed: "+err.Error(), status)
		return
	}
	s.syncMembershipNow()

	s.writeJSON(w, map[string]interface{}{
		"message": "Drain started: the node takes no new data and its objects are moved to the other nodes",
		"node_id": nodeID,
		"reason":  body.Reason,
	})
}

// handleGetClusterDegradedState returns the cluster-wide degraded reason ("" when healthy).
// Used by the UI to render the degraded banner.
// GET /cluster/ha/degraded-state
func (s *Server) handleGetClusterDegradedState(w http.ResponseWriter, r *http.Request) {
	if currentUser := s.getAuthUser(r); currentUser == nil || !s.isGlobalAdmin(currentUser) {
		s.writeError(w, "Access denied: global admin required", http.StatusForbidden)
		return
	}
	reason := cluster.ClusterDegradedReason(r.Context(), s.db)
	s.writeJSON(w, map[string]interface{}{
		"degraded": reason != "",
		"reason":   reason,
	})
}

// handleSetClusterHA changes how the cluster keeps its data: a replication
// factor of whole copies, or parity.
func (s *Server) handleSetClusterHA(w http.ResponseWriter, r *http.Request) {
	if currentUser := s.getAuthUser(r); currentUser == nil || !s.isGlobalAdmin(currentUser) {
		s.writeError(w, "Access denied: global admin required", http.StatusForbidden)
		return
	}

	if !s.clusterManager.IsClusterEnabled() {
		s.writeError(w, "Cluster is not enabled", http.StatusBadRequest)
		return
	}

	var req struct {
		Factor int  `json:"factor"`
		Parity bool `json:"parity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if !req.Parity && (req.Factor < 1 || req.Factor > 3) {
		s.writeError(w, "factor must be 1, 2, or 3", http.StatusBadRequest)
		return
	}
	// Verify enough healthy nodes exist for the requested factor
	nodes, err := s.clusterManager.GetHealthyNodes(r.Context())
	if err != nil {
		s.writeError(w, "Failed to retrieve cluster nodes: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if req.Parity && len(nodes) < 3 {
		s.writeError(w, fmt.Sprintf(
			"Not enough healthy nodes: parity requires 3 healthy nodes, cluster has %d", len(nodes),
		), http.StatusBadRequest)
		return
	}
	if !req.Parity && len(nodes) < req.Factor {
		s.writeError(w, fmt.Sprintf(
			"Not enough healthy nodes: factor %d requires %d healthy node(s), cluster has %d",
			req.Factor, req.Factor, len(nodes),
		), http.StatusBadRequest)
		return
	}

	// Space validation: sum total data size across all nodes
	// Each node that will hold a replica must have enough free space
	var totalDataBytes int64
	for _, n := range nodes {
		totalDataBytes += n.CapacityUsed
	}
	// With replication, each replica node needs totalDataBytes / currentFactor worth of data
	// Simplified: each node needs totalDataBytes / factor free space
	var requiredPerNode int64
	if req.Parity {
		// Each node holds a shard of each write: Width/Data of the data over
		// the nodes.
		if layout, ok := erasure.For(len(nodes)); ok {
			requiredPerNode = totalDataBytes * int64(layout.Width()) / int64(layout.Data) / int64(len(nodes))
		}
	} else {
		requiredPerNode = totalDataBytes / int64(req.Factor)
	}
	requiredWithHeadroom := int64(float64(requiredPerNode) * 1.2)

	type insufficientNode struct {
		NodeID    string `json:"node_id"`
		NodeName  string `json:"node_name"`
		FreeBytes int64  `json:"free_bytes"`
		NeedBytes int64  `json:"need_bytes"`
	}
	var insufficientNodes []insufficientNode

	for _, n := range nodes {
		freeBytes := n.CapacityTotal - n.CapacityUsed
		if freeBytes < requiredWithHeadroom {
			insufficientNodes = append(insufficientNodes, insufficientNode{
				NodeID:    n.ID,
				NodeName:  n.Name,
				FreeBytes: freeBytes,
				NeedBytes: requiredWithHeadroom,
			})
		}
	}

	if len(insufficientNodes) > 0 {
		s.writeError(w, fmt.Sprintf(
			"Insufficient free space on %d node(s). Each node needs at least %d bytes free (current data × 1.2 headroom).",
			len(insufficientNodes), requiredWithHeadroom,
		), http.StatusBadRequest)
		return
	}

	previous, _ := s.clusterManager.GetProtection(r.Context())

	if req.Parity {
		err = s.clusterManager.SetParity(r.Context(), true)
	} else {
		err = s.clusterManager.SetReplicationFactor(r.Context(), req.Factor)
		if err == nil && previous.Parity {
			err = s.clusterManager.SetParity(r.Context(), false)
		}
	}
	if err != nil {
		s.writeError(w, "Failed to set replication factor: "+err.Error(), http.StatusInternalServerError)
		return
	}
	current, _ := s.clusterManager.GetProtection(r.Context())

	logrus.WithFields(logrus.Fields{
		"previous_factor": previous.Factor,
		"previous_parity": previous.Parity,
		"new_factor":      current.Factor,
		"new_parity":      current.Parity,
	}).Info("Cluster replication factor changed")

	// Every node acts on the new protection; the others are told now.
	if current != previous {
		s.protectionChanged(previous, current)
		s.syncMembershipNow()
	}

	message := "Replication factor updated"
	if current.Parity {
		message = "Parity turned on"
	}
	s.writeJSON(w, map[string]interface{}{
		"message":         message,
		"previous_factor": previous.Factor,
		"previous_parity": previous.Parity,
		"new_factor":      current.Factor,
		"parity":          current.Parity,
	})
}

// protectionChanged makes this node act on a change of how the cluster keeps
// its data, made here or received from another node: its copies and shards
// follow the new protection. The work outlives the request that brings it.
func (s *Server) protectionChanged(previous, current cluster.Protection) {
	if s.haSyncWorker == nil || previous == current {
		return
	}
	s.haSyncWorker.Trigger(s.backgroundContext())
}
