package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/sirupsen/logrus"
)


// getDiskUsage returns disk usage stats for the partition containing the given path.
func getDiskUsage(path string) (*disk.UsageStat, error) {
	return disk.Usage(path)
}


// handleGetClusterHA returns the current cluster replication factor and node status.
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

	factor, err := s.clusterManager.GetReplicationFactor(r.Context())
	if err != nil {
		s.writeError(w, "Failed to get replication factor: "+err.Error(), http.StatusInternalServerError)
		return
	}

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

	type nodeStatus struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		HealthStatus  string `json:"health_status"`
		CapacityTotal int64  `json:"capacity_total"`
		CapacityUsed  int64  `json:"capacity_used"`
		CapacityFree  int64  `json:"capacity_free"`
	}

	nodeStatuses := make([]nodeStatus, 0, len(nodes))
	for _, n := range nodes {
		nodeStatuses = append(nodeStatuses, nodeStatus{
			ID:            n.ID,
			Name:          n.Name,
			HealthStatus:  n.HealthStatus,
			CapacityTotal: n.CapacityTotal,
			CapacityUsed:  n.CapacityUsed,
			CapacityFree:  n.CapacityTotal - n.CapacityUsed,
		})
	}

	s.writeJSON(w, map[string]interface{}{
		"replication_factor": factor,
		"node_count":         len(nodes),
		"tolerated_failures": toleratedFailures,
		"total_bytes":        totalBytes,
		"usable_bytes":       usableBytes,
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

// handleSetClusterHA changes the cluster-wide replication factor.
// PUT /cluster/ha
//
// Body: { "factor": 2 }
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
		Factor int `json:"factor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Factor < 1 || req.Factor > 3 {
		s.writeError(w, "factor must be 1, 2, or 3", http.StatusBadRequest)
		return
	}

	// Verify enough healthy nodes exist for the requested factor
	nodes, err := s.clusterManager.GetHealthyNodes(r.Context())
	if err != nil {
		s.writeError(w, "Failed to retrieve cluster nodes: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if len(nodes) < req.Factor {
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
	requiredPerNode := totalDataBytes / int64(req.Factor)
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

	currentFactor, _ := s.clusterManager.GetReplicationFactor(r.Context())

	if err := s.clusterManager.SetReplicationFactor(r.Context(), req.Factor); err != nil {
		s.writeError(w, "Failed to set replication factor: "+err.Error(), http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"previous_factor": currentFactor,
		"new_factor":      req.Factor,
	}).Info("Cluster replication factor changed")

	// If the factor increased, kick off background sync to new replica nodes.
	if req.Factor > currentFactor && s.haSyncWorker != nil {
		s.haSyncWorker.Trigger(r.Context())
	}

	s.writeJSON(w, map[string]interface{}{
		"message":         "Replication factor updated",
		"previous_factor": currentFactor,
		"new_factor":      req.Factor,
	})
}
