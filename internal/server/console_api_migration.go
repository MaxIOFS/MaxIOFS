package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// handleMigrateBucket handles POST /api/v1/cluster/buckets/{bucket}/migrate.
// The migration runs on the node the bucket lives on; a request that reaches
// another node is forwarded there. It answers 202 with the job, which moves
// the bucket in the background.
func (s *Server) handleMigrateBucket(w http.ResponseWriter, r *http.Request) {
	if !s.isGlobalAdmin(s.getAuthUser(r)) {
		s.writeError(w, "Only global administrators can migrate buckets", http.StatusForbidden)
		return
	}
	if s.clusterManager == nil || !s.clusterManager.IsClusterEnabled() {
		s.writeError(w, "Cluster is not enabled", http.StatusBadRequest)
		return
	}
	bucketName := mux.Vars(r)["bucket"]
	if s.proxyConsoleRequest(w, r, bucketName) {
		return
	}

	var req struct {
		TargetNodeID string `json:"target_node_id"`
		DeleteSource *bool  `json:"delete_source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.TargetNodeID == "" {
		s.writeError(w, "target_node_id is required", http.StatusBadRequest)
		return
	}
	if req.DeleteSource != nil && !*req.DeleteSource {
		s.writeError(w, "A migration moves the bucket: once the target holds it, it is removed from this node", http.StatusBadRequest)
		return
	}

	job, err := s.bucketMigrator.Migrate(r.Context(), bucketName, req.TargetNodeID)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, cluster.ErrMigrationNotFound):
			status = http.StatusNotFound
		case errors.Is(err, cluster.ErrMigrationInvalid):
			status = http.StatusBadRequest
		case errors.Is(err, cluster.ErrMigrationConflict):
			status = http.StatusConflict
		default:
			logrus.WithError(err).WithFields(logrus.Fields{
				"bucket":      bucketName,
				"target_node": req.TargetNodeID,
			}).Error("Failed to start a bucket migration")
		}
		s.writeError(w, err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(APIResponse{Success: true, Data: job}) //nolint:errcheck
}

// handleListMigrations handles GET /api/v1/cluster/migrations: the migrations
// this node ran as their source.
func (s *Server) handleListMigrations(w http.ResponseWriter, r *http.Request) {
	if !s.isGlobalAdmin(s.getAuthUser(r)) {
		s.writeError(w, "Only global administrators can see bucket migrations", http.StatusForbidden)
		return
	}

	bucketName := r.URL.Query().Get("bucket")

	var jobs []*cluster.MigrationJob
	var err error
	if bucketName != "" {
		jobs, err = s.clusterManager.GetMigrationJobsByBucket(r.Context(), bucketName)
	} else {
		jobs, err = s.clusterManager.ListMigrationJobs(r.Context())
	}
	if err != nil {
		logrus.WithError(err).WithField("bucket", bucketName).Error("Failed to list migration jobs")
		s.writeError(w, "Failed to list migration jobs", http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"migrations": jobs,
		"count":      len(jobs),
	})
}

// handleGetMigration handles GET /api/v1/cluster/migrations/{id}
func (s *Server) handleGetMigration(w http.ResponseWriter, r *http.Request) {
	if !s.isGlobalAdmin(s.getAuthUser(r)) {
		s.writeError(w, "Only global administrators can see bucket migrations", http.StatusForbidden)
		return
	}

	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		s.writeError(w, "Invalid migration ID", http.StatusBadRequest)
		return
	}

	job, err := s.clusterManager.GetMigrationJob(r.Context(), id)
	if err != nil {
		s.writeError(w, "Migration not found", http.StatusNotFound)
		return
	}

	s.writeJSON(w, job)
}
