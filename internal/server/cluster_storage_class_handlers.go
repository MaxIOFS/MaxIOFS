package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/storageclass"
	"github.com/sirupsen/logrus"
)

// handleGetStorageClasses returns the storage classes the cluster keeps on
// nodes of their own, STANDARD first, how each keeps its data and the room it
// has, the nodes with their class, and the classes that can be added.
// GET /cluster/storage-classes
func (s *Server) handleGetStorageClasses(w http.ResponseWriter, r *http.Request) {
	if !s.requireClusterAdmin(w, r) {
		return
	}
	ctx := r.Context()
	classes, err := s.clusterManager.StorageClassStatus(ctx)
	if err != nil {
		s.writeError(w, "Failed to read the storage classes: "+err.Error(), http.StatusInternalServerError)
		return
	}
	nodes, err := s.clusterManager.ListNodes(ctx)
	if err != nil {
		s.writeError(w, "Failed to list nodes: "+err.Error(), http.StatusInternalServerError)
		return
	}
	type nodeClass struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		StorageClass  string `json:"storage_class"`
		HealthStatus  string `json:"health_status"`
		Drained       bool   `json:"drained"`
		CapacityTotal int64  `json:"capacity_total"`
		CapacityUsed  int64  `json:"capacity_used"`
	}
	kept := make([]string, 0, len(classes))
	for _, c := range classes {
		kept = append(kept, c.Name)
	}
	nodeClasses := make([]nodeClass, 0, len(nodes))
	for _, n := range nodes {
		class := n.StorageClass
		if !slices.Contains(kept, class) {
			class = storageclass.Standard
		}
		nodeClasses = append(nodeClasses, nodeClass{
			ID: n.ID, Name: n.Name, StorageClass: class, HealthStatus: n.HealthStatus, Drained: n.Drained,
			CapacityTotal: n.CapacityTotal, CapacityUsed: n.CapacityUsed,
		})
	}
	definable := []string{}
	for _, name := range storageclass.Definable {
		if !slices.Contains(kept, name) {
			definable = append(definable, name)
		}
	}
	s.writeJSON(w, map[string]interface{}{
		"classes":   classes,
		"nodes":     nodeClasses,
		"definable": definable,
	})
}

// handleSetStorageClass keeps a storage class on nodes of its own, or changes
// its protection: a replication factor of whole copies, or parity. STANDARD's
// is the cluster's, set on the HA page.
// PUT /cluster/storage-classes/{name}
func (s *Server) handleSetStorageClass(w http.ResponseWriter, r *http.Request) {
	if !s.requireClusterAdmin(w, r) {
		return
	}
	name := mux.Vars(r)["name"]
	if name == storageclass.Standard {
		s.writeError(w, "The protection of STANDARD is the cluster's: set it on the HA page", http.StatusBadRequest)
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
	ctx := r.Context()
	previous := s.clusterManager.PlacementSettings(ctx)
	err := s.clusterManager.SetStorageClass(ctx, cluster.StorageClass{Name: name, Factor: req.Factor, Parity: req.Parity})
	if errors.Is(err, cluster.ErrStorageClassUnknown) {
		s.writeError(w, "A cluster keeps no storage class "+name+" on nodes of its own", http.StatusBadRequest)
		return
	}
	if err != nil {
		s.writeError(w, "Failed to set the storage class: "+err.Error(), http.StatusInternalServerError)
		return
	}
	logrus.WithFields(logrus.Fields{"storage_class": name, "factor": req.Factor, "parity": req.Parity}).Info("Storage class set")
	s.protectionChanged(previous, s.clusterManager.PlacementSettings(ctx))
	s.syncMembershipNow()
	s.writeJSON(w, map[string]interface{}{"message": "Storage class set", "name": name})
}

// handleDeleteStorageClass stops keeping a storage class on nodes of its own:
// its nodes go back to STANDARD and its objects are kept there.
// DELETE /cluster/storage-classes/{name}
func (s *Server) handleDeleteStorageClass(w http.ResponseWriter, r *http.Request) {
	if !s.requireClusterAdmin(w, r) {
		return
	}
	name := mux.Vars(r)["name"]
	ctx := r.Context()
	previous := s.clusterManager.PlacementSettings(ctx)
	err := s.clusterManager.DeleteStorageClass(ctx, name)
	if errors.Is(err, cluster.ErrStorageClassUnknown) {
		s.writeError(w, "The cluster keeps no storage class "+name, http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, "Failed to delete the storage class: "+err.Error(), http.StatusInternalServerError)
		return
	}
	logrus.WithField("storage_class", name).Info("Storage class deleted")
	s.protectionChanged(previous, s.clusterManager.PlacementSettings(ctx))
	s.syncMembershipNow()
	s.writeJSON(w, map[string]interface{}{"message": "Storage class deleted", "name": name})
}

// handleSetNodeStorageClass puts a node in a storage class: its data is moved
// to the nodes of the classes it keeps no longer, and it takes the data of
// its new class.
// PUT /cluster/nodes/{nodeId}/storage-class
func (s *Server) handleSetNodeStorageClass(w http.ResponseWriter, r *http.Request) {
	if !s.requireClusterAdmin(w, r) {
		return
	}
	nodeID := mux.Vars(r)["nodeId"]
	var req struct {
		StorageClass string `json:"storage_class"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	err := s.clusterManager.SetNodeStorageClass(ctx, nodeID, req.StorageClass)
	switch {
	case errors.Is(err, cluster.ErrStorageClassUnknown):
		s.writeError(w, "The cluster keeps no storage class "+req.StorageClass, http.StatusBadRequest)
		return
	case errors.Is(err, cluster.ErrNodeNotFound):
		s.writeError(w, "Node not found", http.StatusNotFound)
		return
	case errors.Is(err, cluster.ErrStorageClassBelowProtection):
		s.writeError(w, "The node cannot leave its storage class: "+err.Error(), http.StatusConflict)
		return
	case err != nil:
		s.writeError(w, "Failed to set the storage class of the node: "+err.Error(), http.StatusInternalServerError)
		return
	}
	logrus.WithFields(logrus.Fields{"node_id": nodeID, "storage_class": req.StorageClass}).Info("Node storage class set")
	if s.haSyncWorker != nil {
		s.haSyncWorker.Trigger(s.backgroundContext())
	}
	s.syncMembershipNow()
	s.writeJSON(w, map[string]interface{}{"message": "Node storage class set", "node_id": nodeID, "storage_class": req.StorageClass})
}

// requireClusterAdmin lets a global admin of a cluster through.
func (s *Server) requireClusterAdmin(w http.ResponseWriter, r *http.Request) bool {
	if currentUser := s.getAuthUser(r); currentUser == nil || !s.isGlobalAdmin(currentUser) {
		s.writeError(w, "Access denied: global admin required", http.StatusForbidden)
		return false
	}
	if !s.clusterManager.IsClusterEnabled() {
		s.writeError(w, "Cluster is not enabled", http.StatusBadRequest)
		return false
	}
	return true
}
