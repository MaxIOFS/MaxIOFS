package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// handleMigrateBucket handles POST /api/v1/cluster/buckets/{bucket}/migrate
func (s *Server) handleMigrateBucket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Get user from context
	user, ok := r.Context().Value("user").(*auth.User)
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req struct {
		TargetNodeID string `json:"target_node_id"`
		DeleteSource bool   `json:"delete_source"`
		VerifyData   bool   `json:"verify_data"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.TargetNodeID == "" {
		s.writeError(w, "target_node_id is required", http.StatusBadRequest)
		return
	}

	// Verify bucket exists
	exists, err := s.bucketManager.BucketExists(r.Context(), user.TenantID, bucketName)
	if err != nil {
		logrus.WithError(err).Error("Failed to check bucket existence")
		s.writeError(w, "Failed to check bucket existence", http.StatusInternalServerError)
		return
	}

	if !exists {
		s.writeError(w, "Bucket not found", http.StatusNotFound)
		return
	}

	// Verify cluster is enabled
	if !s.clusterManager.IsClusterEnabled() {
		s.writeError(w, "Cluster is not enabled", http.StatusBadRequest)
		return
	}

	// Create bucket location manager
	bucketLocationCache := cluster.NewBucketLocationCache(5 * time.Minute)
	localNodeID, _ := s.clusterManager.GetLocalNodeID(r.Context())
	locationMgr := cluster.NewBucketLocationManager(s.bucketManager, bucketLocationCache, localNodeID)

	// Start migration (this runs synchronously for now)
	// In future, this should be async with job tracking
	job, err := s.clusterManager.MigrateBucket(
		r.Context(),
		locationMgr,
		user.TenantID,
		bucketName,
		req.TargetNodeID,
		req.DeleteSource,
		req.VerifyData,
	)

	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"bucket":       bucketName,
			"target_node":  req.TargetNodeID,
			"tenant_id":    user.TenantID,
		}).Error("Failed to migrate bucket")
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return migration job
	w.WriteHeader(http.StatusAccepted)
	s.writeJSON(w, job)
}

// handleListMigrations handles GET /api/v1/cluster/migrations
func (s *Server) handleListMigrations(w http.ResponseWriter, r *http.Request) {
	// Get user from context
	user, ok := r.Context().Value("user").(*auth.User)
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	// Check if bucket filter is provided
	bucketName := r.URL.Query().Get("bucket")

	var jobs []*cluster.MigrationJob
	var err error

	if bucketName != "" {
		// Get migrations for specific bucket
		jobs, err = s.clusterManager.GetMigrationJobsByBucket(r.Context(), bucketName)
	} else {
		// Get all migrations
		jobs, err = s.clusterManager.ListMigrationJobs(r.Context())
	}

	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"bucket":    bucketName,
			"tenant_id": user.TenantID,
		}).Error("Failed to list migration jobs")
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
	vars := mux.Vars(r)
	idStr := vars["id"]

	// Get user from context
	_, ok := r.Context().Value("user").(*auth.User)
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	// Parse migration ID
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.writeError(w, "Invalid migration ID", http.StatusBadRequest)
		return
	}

	// Get migration job
	job, err := s.clusterManager.GetMigrationJob(r.Context(), id)
	if err != nil {
		logrus.WithError(err).WithField("migration_id", id).Error("Failed to get migration job")
		s.writeError(w, "Migration not found", http.StatusNotFound)
		return
	}

	s.writeJSON(w, job)
}
