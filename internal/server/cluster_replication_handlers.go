package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// handleCreateClusterReplication creates a new cluster bucket replication rule
// POST /api/console/cluster/replication
func (s *Server) handleCreateClusterReplication(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user from context (set by auth middleware)
	username, ok := ctx.Value("username").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req struct {
		TenantID             string `json:"tenant_id"`
		SourceBucket         string `json:"source_bucket"`
		DestinationNodeID    string `json:"destination_node_id"`
		DestinationBucket    string `json:"destination_bucket"`
		SyncIntervalSeconds  int    `json:"sync_interval_seconds"`
		Enabled              bool   `json:"enabled"`
		ReplicateDeletes     bool   `json:"replicate_deletes"`
		ReplicateMetadata    bool   `json:"replicate_metadata"`
		Prefix               string `json:"prefix"`
		Priority             int    `json:"priority"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logrus.WithError(err).Error("Failed to decode request body")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.SourceBucket == "" || req.DestinationNodeID == "" || req.DestinationBucket == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Default to 60 seconds if not specified
	if req.SyncIntervalSeconds <= 0 {
		req.SyncIntervalSeconds = 60
	}

	// Check minimum sync interval
	minIntervalStr, err := cluster.GetGlobalConfig(ctx, s.db, "min_sync_interval_seconds")
	if err == nil {
		var minInterval int
		if _, err := fmt.Sscanf(minIntervalStr, "%d", &minInterval); err == nil {
			if req.SyncIntervalSeconds < minInterval {
				http.Error(w, fmt.Sprintf("Sync interval must be at least %d seconds", minInterval), http.StatusBadRequest)
				return
			}
		}
	}

	// Check if destination node exists and is healthy
	node, err := s.clusterManager.GetNode(ctx, req.DestinationNodeID)
	if err != nil {
		logrus.WithError(err).Error("Failed to get destination node")
		http.Error(w, "Destination node not found", http.StatusNotFound)
		return
	}

	// Prevent self-replication: A node cannot replicate to itself in cluster mode
	localNodeID, err := s.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		logrus.WithError(err).Error("Failed to get local node ID")
		http.Error(w, "Failed to get local node ID", http.StatusInternalServerError)
		return
	}

	if req.DestinationNodeID == localNodeID {
		logrus.WithFields(logrus.Fields{
			"destination_node": req.DestinationNodeID,
			"local_node":       localNodeID,
		}).Warn("Attempted to create self-replication rule")
		http.Error(w, "Cannot replicate to the same node. Cluster replication is for HA between different MaxIOFS nodes. For local bucket copies, use bucket-level replication settings.", http.StatusBadRequest)
		return
	}

	if node.HealthStatus != "healthy" {
		logrus.WithField("node_status", node.HealthStatus).Warn("Destination node is not healthy")
		// Allow creation but warn
	}

	// Create replication rule
	rule := &cluster.ClusterReplicationRule{
		ID:                  uuid.New().String(),
		TenantID:            req.TenantID,
		SourceBucket:        req.SourceBucket,
		DestinationNodeID:   req.DestinationNodeID,
		DestinationBucket:   req.DestinationBucket,
		SyncIntervalSeconds: req.SyncIntervalSeconds,
		Enabled:             req.Enabled,
		ReplicateDeletes:    req.ReplicateDeletes,
		ReplicateMetadata:   req.ReplicateMetadata,
		Prefix:              req.Prefix,
		Priority:            req.Priority,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}

	// TODO: Use ClusterReplicationManager.CreateReplicationRule() when integrated
	err = s.createClusterReplicationRule(ctx, rule)
	if err != nil {
		logrus.WithError(err).Error("Failed to create cluster replication rule")
		http.Error(w, "Failed to create replication rule", http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"rule_id":       rule.ID,
		"source_bucket": rule.SourceBucket,
		"dest_node":     rule.DestinationNodeID,
		"username":      username,
	}).Info("Created cluster replication rule")

	// Return success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rule)
}

// handleListClusterReplications lists cluster replication rules
// GET /api/console/cluster/replication
func (s *Server) handleListClusterReplications(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get query parameters
	tenantID := r.URL.Query().Get("tenant_id")
	bucket := r.URL.Query().Get("bucket")

	rules, err := s.listClusterReplicationRules(ctx, tenantID, bucket)
	if err != nil {
		logrus.WithError(err).Error("Failed to list cluster replication rules")
		http.Error(w, "Failed to list replication rules", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"rules": rules,
		"count": len(rules),
	})
}

// handleUpdateClusterReplication updates a cluster replication rule
// PUT /api/console/cluster/replication/:id
func (s *Server) handleUpdateClusterReplication(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get rule ID from URL
	vars := mux.Vars(r)
	ruleID := vars["id"]

	// Parse request body
	var req struct {
		SyncIntervalSeconds *int  `json:"sync_interval_seconds,omitempty"`
		Enabled             *bool `json:"enabled,omitempty"`
		ReplicateDeletes    *bool `json:"replicate_deletes,omitempty"`
		ReplicateMetadata   *bool `json:"replicate_metadata,omitempty"`
		Priority            *int  `json:"priority,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logrus.WithError(err).Error("Failed to decode request body")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Update rule
	err := s.updateClusterReplicationRule(ctx, ruleID, req.SyncIntervalSeconds, req.Enabled, req.ReplicateDeletes, req.ReplicateMetadata, req.Priority)
	if err != nil {
		logrus.WithError(err).Error("Failed to update cluster replication rule")
		http.Error(w, "Failed to update replication rule", http.StatusInternalServerError)
		return
	}

	logrus.WithField("rule_id", ruleID).Info("Updated cluster replication rule")

	// Return success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Replication rule updated successfully",
	})
}

// handleDeleteClusterReplication deletes a cluster replication rule
// DELETE /api/console/cluster/replication/:id
func (s *Server) handleDeleteClusterReplication(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get rule ID from URL
	vars := mux.Vars(r)
	ruleID := vars["id"]

	// Delete rule
	err := s.deleteClusterReplicationRule(ctx, ruleID)
	if err != nil {
		logrus.WithError(err).Error("Failed to delete cluster replication rule")
		http.Error(w, "Failed to delete replication rule", http.StatusInternalServerError)
		return
	}

	logrus.WithField("rule_id", ruleID).Info("Deleted cluster replication rule")

	// Return success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Replication rule deleted successfully",
	})
}

// handleCreateBulkClusterReplication creates replication rules for all buckets between two nodes
// POST /api/console/cluster/replication/bulk
func (s *Server) handleCreateBulkClusterReplication(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user from context
	username, ok := ctx.Value("username").(string)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse request body
	var req struct {
		SourceNodeID        string `json:"source_node_id"`
		DestinationNodeID   string `json:"destination_node_id"`
		SyncIntervalSeconds int    `json:"sync_interval_seconds"`
		TenantID            string `json:"tenant_id"`
		Enabled             bool   `json:"enabled"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logrus.WithError(err).Error("Failed to decode request body")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.DestinationNodeID == "" {
		http.Error(w, "Missing destination_node_id", http.StatusBadRequest)
		return
	}

	// Default to 60 seconds if not specified
	if req.SyncIntervalSeconds <= 0 {
		req.SyncIntervalSeconds = 60
	}

	// Get local node ID if source not specified
	sourceNodeID := req.SourceNodeID
	if sourceNodeID == "" {
		localNodeID, err := s.clusterManager.GetLocalNodeID(ctx)
		if err != nil {
			logrus.WithError(err).Error("Failed to get local node ID")
			http.Error(w, "Failed to get local node ID", http.StatusInternalServerError)
			return
		}
		sourceNodeID = localNodeID
	}

	// Prevent self-replication: A node cannot replicate to itself in cluster mode
	if req.DestinationNodeID == sourceNodeID {
		logrus.WithFields(logrus.Fields{
			"destination_node": req.DestinationNodeID,
			"source_node":      sourceNodeID,
		}).Warn("Attempted to create bulk self-replication")
		http.Error(w, "Cannot replicate to the same node. Cluster replication is for HA between different MaxIOFS nodes. For local bucket copies, use bucket-level replication settings.", http.StatusBadRequest)
		return
	}

	// Verify destination node exists
	_, err := s.clusterManager.GetNode(ctx, req.DestinationNodeID)
	if err != nil {
		logrus.WithError(err).Error("Failed to get destination node")
		http.Error(w, "Destination node not found", http.StatusNotFound)
		return
	}

	// Get all buckets
	buckets, err := s.bucketManager.ListBuckets(ctx, req.TenantID)
	if err != nil {
		logrus.WithError(err).Error("Failed to list buckets")
		http.Error(w, "Failed to list buckets", http.StatusInternalServerError)
		return
	}

	// Create replication rule for each bucket
	var createdRules []string
	var failedBuckets []string

	for _, bucket := range buckets {
		rule := &cluster.ClusterReplicationRule{
			ID:                  uuid.New().String(),
			TenantID:            bucket.TenantID,
			SourceBucket:        bucket.Name,
			DestinationNodeID:   req.DestinationNodeID,
			DestinationBucket:   bucket.Name, // Same bucket name on destination
			SyncIntervalSeconds: req.SyncIntervalSeconds,
			Enabled:             req.Enabled,
			ReplicateDeletes:    true,
			ReplicateMetadata:   true,
			Priority:            0,
			CreatedAt:           time.Now(),
			UpdatedAt:           time.Now(),
		}

		err = s.createClusterReplicationRule(ctx, rule)
		if err != nil {
			logrus.WithError(err).WithField("bucket", bucket.Name).Warn("Failed to create replication rule for bucket")
			failedBuckets = append(failedBuckets, bucket.Name)
			continue
		}

		createdRules = append(createdRules, rule.ID)
	}

	logrus.WithFields(logrus.Fields{
		"source_node":   sourceNodeID,
		"dest_node":     req.DestinationNodeID,
		"rules_created": len(createdRules),
		"rules_failed":  len(failedBuckets),
		"username":      username,
	}).Info("Created bulk cluster replication rules")

	// Return success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"rules_created":  len(createdRules),
		"rules_failed":   len(failedBuckets),
		"failed_buckets": failedBuckets,
		"message":        fmt.Sprintf("Created %d replication rules", len(createdRules)),
	})
}

// Database helper functions

func (s *Server) createClusterReplicationRule(ctx context.Context, rule *cluster.ClusterReplicationRule) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cluster_bucket_replication (
			id, tenant_id, source_bucket, destination_node_id, destination_bucket,
			sync_interval_seconds, enabled, replicate_deletes, replicate_metadata,
			prefix, priority, objects_replicated, bytes_replicated, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rule.ID,
		rule.TenantID,
		rule.SourceBucket,
		rule.DestinationNodeID,
		rule.DestinationBucket,
		rule.SyncIntervalSeconds,
		boolToInt(rule.Enabled),
		boolToInt(rule.ReplicateDeletes),
		boolToInt(rule.ReplicateMetadata),
		rule.Prefix,
		rule.Priority,
		0, // objects_replicated
		0, // bytes_replicated
		rule.CreatedAt,
		rule.UpdatedAt,
	)
	return err
}

func (s *Server) listClusterReplicationRules(ctx context.Context, tenantID, bucket string) ([]*cluster.ClusterReplicationRule, error) {
	query := `
		SELECT id, tenant_id, source_bucket, destination_node_id, destination_bucket,
		       sync_interval_seconds, enabled, replicate_deletes, replicate_metadata,
		       prefix, priority, last_sync_at, last_error, objects_replicated,
		       bytes_replicated, created_at, updated_at
		FROM cluster_bucket_replication
		WHERE 1=1
	`

	args := []interface{}{}

	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}

	if bucket != "" {
		query += " AND source_bucket = ?"
		args = append(args, bucket)
	}

	query += " ORDER BY priority DESC, created_at ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*cluster.ClusterReplicationRule
	for rows.Next() {
		rule := &cluster.ClusterReplicationRule{}
		var enabled, replicateDeletes, replicateMetadata int
		var lastSyncAt sql.NullTime

		err := rows.Scan(
			&rule.ID,
			&rule.TenantID,
			&rule.SourceBucket,
			&rule.DestinationNodeID,
			&rule.DestinationBucket,
			&rule.SyncIntervalSeconds,
			&enabled,
			&replicateDeletes,
			&replicateMetadata,
			&rule.Prefix,
			&rule.Priority,
			&lastSyncAt,
			&rule.LastError,
			&rule.ObjectsReplicated,
			&rule.BytesReplicated,
			&rule.CreatedAt,
			&rule.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		rule.Enabled = enabled == 1
		rule.ReplicateDeletes = replicateDeletes == 1
		rule.ReplicateMetadata = replicateMetadata == 1
		if lastSyncAt.Valid {
			rule.LastSyncAt = &lastSyncAt.Time
		}

		rules = append(rules, rule)
	}

	return rules, rows.Err()
}

func (s *Server) updateClusterReplicationRule(ctx context.Context, ruleID string, syncInterval *int, enabled, replicateDeletes, replicateMetadata *bool, priority *int) error {
	// Build dynamic update query
	updates := []string{}
	args := []interface{}{}

	if syncInterval != nil {
		updates = append(updates, "sync_interval_seconds = ?")
		args = append(args, *syncInterval)
	}

	if enabled != nil {
		updates = append(updates, "enabled = ?")
		args = append(args, boolToInt(*enabled))
	}

	if replicateDeletes != nil {
		updates = append(updates, "replicate_deletes = ?")
		args = append(args, boolToInt(*replicateDeletes))
	}

	if replicateMetadata != nil {
		updates = append(updates, "replicate_metadata = ?")
		args = append(args, boolToInt(*replicateMetadata))
	}

	if priority != nil {
		updates = append(updates, "priority = ?")
		args = append(args, *priority)
	}

	if len(updates) == 0 {
		return nil // Nothing to update
	}

	updates = append(updates, "updated_at = ?")
	args = append(args, time.Now())

	args = append(args, ruleID)

	query := fmt.Sprintf("UPDATE cluster_bucket_replication SET %s WHERE id = ?",
		strings.Join(updates, ", "))

	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *Server) deleteClusterReplicationRule(ctx context.Context, ruleID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cluster_bucket_replication WHERE id = ?`, ruleID)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
