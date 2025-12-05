package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/replication"
	"github.com/sirupsen/logrus"
)

// ReplicationRuleRequest represents the request body for creating/updating a replication rule
type ReplicationRuleRequest struct {
	DestinationEndpoint   string                        `json:"destination_endpoint"`
	DestinationBucket     string                        `json:"destination_bucket"`
	DestinationAccessKey  string                        `json:"destination_access_key"`
	DestinationSecretKey  string                        `json:"destination_secret_key"`
	DestinationRegion     string                        `json:"destination_region,omitempty"`
	Prefix                string                        `json:"prefix,omitempty"`
	Enabled               bool                          `json:"enabled"`
	Priority              int                           `json:"priority"`
	Mode                  replication.ReplicationMode   `json:"mode"`
	ScheduleInterval      int                           `json:"schedule_interval,omitempty"`
	ConflictResolution    replication.ConflictResolution `json:"conflict_resolution"`
	ReplicateDeletes      bool                          `json:"replicate_deletes"`
	ReplicateMetadata     bool                          `json:"replicate_metadata"`
}

// ReplicationRuleResponse represents the response for a replication rule
type ReplicationRuleResponse struct {
	ID                    string                        `json:"id"`
	TenantID              string                        `json:"tenant_id"`
	SourceBucket          string                        `json:"source_bucket"`
	DestinationEndpoint   string                        `json:"destination_endpoint"`
	DestinationBucket     string                        `json:"destination_bucket"`
	DestinationAccessKey  string                        `json:"destination_access_key"`
	DestinationSecretKey  string                        `json:"destination_secret_key"`
	DestinationRegion     string                        `json:"destination_region,omitempty"`
	Prefix                string                        `json:"prefix,omitempty"`
	Enabled               bool                          `json:"enabled"`
	Priority              int                           `json:"priority"`
	Mode                  replication.ReplicationMode   `json:"mode"`
	ScheduleInterval      int                           `json:"schedule_interval,omitempty"`
	ConflictResolution    replication.ConflictResolution `json:"conflict_resolution"`
	ReplicateDeletes      bool                          `json:"replicate_deletes"`
	ReplicateMetadata     bool                          `json:"replicate_metadata"`
	CreatedAt             string                        `json:"created_at"`
	UpdatedAt             string                        `json:"updated_at"`
}

// ReplicationMetricsResponse represents the response for replication metrics
type ReplicationMetricsResponse struct {
	RuleID           string  `json:"rule_id"`
	TotalObjects     int64   `json:"total_objects"`
	PendingObjects   int64   `json:"pending_objects"`
	CompletedObjects int64   `json:"completed_objects"`
	FailedObjects    int64   `json:"failed_objects"`
	BytesReplicated  int64   `json:"bytes_replicated"`
	LastSuccess      *string `json:"last_success,omitempty"`
	LastFailure      *string `json:"last_failure,omitempty"`
}

func (s *Server) handleCreateReplicationRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	user, userExists := auth.GetUserFromContext(ctx)
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	tenantID := user.TenantID

	// Check if bucket exists
	_, err := s.bucketManager.GetBucketInfo(ctx, tenantID, bucketName)
	if err != nil {
		s.writeError(w, "Bucket not found", http.StatusNotFound)
		return
	}

	var req ReplicationRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.DestinationEndpoint == "" || req.DestinationBucket == "" {
		s.writeError(w, "destination_endpoint and destination_bucket are required", http.StatusBadRequest)
		return
	}

	if req.DestinationAccessKey == "" || req.DestinationSecretKey == "" {
		s.writeError(w, "destination_access_key and destination_secret_key are required", http.StatusBadRequest)
		return
	}

	rule := &replication.ReplicationRule{
		TenantID:              tenantID,
		SourceBucket:          bucketName,
		DestinationEndpoint:   req.DestinationEndpoint,
		DestinationBucket:     req.DestinationBucket,
		DestinationAccessKey:  req.DestinationAccessKey,
		DestinationSecretKey:  req.DestinationSecretKey,
		DestinationRegion:     req.DestinationRegion,
		Prefix:                req.Prefix,
		Enabled:               req.Enabled,
		Priority:              req.Priority,
		Mode:                  req.Mode,
		ScheduleInterval:      req.ScheduleInterval,
		ConflictResolution:    req.ConflictResolution,
		ReplicateDeletes:      req.ReplicateDeletes,
		ReplicateMetadata:     req.ReplicateMetadata,
	}

	if err := s.replicationManager.CreateRule(ctx, rule); err != nil {
		logrus.WithError(err).Error("Failed to create replication rule")
		s.writeError(w, "Failed to create replication rule", http.StatusInternalServerError)
		return
	}

	if s.auditManager != nil {
		_ = s.auditManager.LogEvent(ctx, &audit.AuditEvent{
			TenantID:     tenantID,
			UserID:       user.ID,
			Username:     user.Username,
			EventType:    "replication.rule.created",
			ResourceType: "replication_rule",
			ResourceID:   rule.ID,
			ResourceName: bucketName,
			Action:       "create",
			Status:       "success",
			Details: map[string]interface{}{
				"rule_id":       rule.ID,
				"source_bucket": bucketName,
				"dest_bucket":   req.DestinationBucket,
			},
		})
	}

	response := ReplicationRuleResponse{
		ID:                    rule.ID,
		TenantID:              rule.TenantID,
		SourceBucket:          rule.SourceBucket,
		DestinationEndpoint:   rule.DestinationEndpoint,
		DestinationBucket:     rule.DestinationBucket,
		DestinationAccessKey:  rule.DestinationAccessKey,
		DestinationSecretKey:  rule.DestinationSecretKey,
		DestinationRegion:     rule.DestinationRegion,
		Prefix:                rule.Prefix,
		Enabled:               rule.Enabled,
		Priority:              rule.Priority,
		Mode:                  rule.Mode,
		ScheduleInterval:      rule.ScheduleInterval,
		ConflictResolution:    rule.ConflictResolution,
		ReplicateDeletes:      rule.ReplicateDeletes,
		ReplicateMetadata:     rule.ReplicateMetadata,
		CreatedAt:             rule.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:             rule.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	w.WriteHeader(http.StatusCreated)
	s.writeJSON(w, response)
}

func (s *Server) handleListReplicationRules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	user, userExists := auth.GetUserFromContext(ctx)
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	tenantID := user.TenantID

	rules, err := s.replicationManager.ListRules(ctx, tenantID)
	if err != nil {
		logrus.WithError(err).Error("Failed to list replication rules")
		s.writeError(w, "Failed to list replication rules", http.StatusInternalServerError)
		return
	}

	var bucketRules []ReplicationRuleResponse
	for _, rule := range rules {
		if rule.SourceBucket == bucketName {
			bucketRules = append(bucketRules, ReplicationRuleResponse{
				ID:                    rule.ID,
				TenantID:              rule.TenantID,
				SourceBucket:          rule.SourceBucket,
				DestinationEndpoint:   rule.DestinationEndpoint,
				DestinationBucket:     rule.DestinationBucket,
				DestinationAccessKey:  rule.DestinationAccessKey,
				DestinationSecretKey:  rule.DestinationSecretKey,
				DestinationRegion:     rule.DestinationRegion,
				Prefix:                rule.Prefix,
				Enabled:               rule.Enabled,
				Priority:              rule.Priority,
				Mode:                  rule.Mode,
				ScheduleInterval:      rule.ScheduleInterval,
				ConflictResolution:    rule.ConflictResolution,
				ReplicateDeletes:      rule.ReplicateDeletes,
				ReplicateMetadata:     rule.ReplicateMetadata,
				CreatedAt:             rule.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
				UpdatedAt:             rule.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			})
		}
	}

	s.writeJSON(w, map[string]interface{}{
		"rules": bucketRules,
	})
}

func (s *Server) handleGetReplicationRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	ruleID := vars["ruleId"]

	user, userExists := auth.GetUserFromContext(ctx)
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	tenantID := user.TenantID

	rule, err := s.replicationManager.GetRule(ctx, ruleID)
	if err != nil {
		logrus.WithError(err).Error("Failed to get replication rule")
		s.writeError(w, "Failed to get replication rule", http.StatusInternalServerError)
		return
	}

	if rule == nil {
		s.writeError(w, "Replication rule not found", http.StatusNotFound)
		return
	}

	if rule.TenantID != tenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	response := ReplicationRuleResponse{
		ID:                    rule.ID,
		TenantID:              rule.TenantID,
		SourceBucket:          rule.SourceBucket,
		DestinationEndpoint:   rule.DestinationEndpoint,
		DestinationBucket:     rule.DestinationBucket,
		DestinationAccessKey:  rule.DestinationAccessKey,
		DestinationSecretKey:  rule.DestinationSecretKey,
		DestinationRegion:     rule.DestinationRegion,
		Prefix:                rule.Prefix,
		Enabled:               rule.Enabled,
		Priority:              rule.Priority,
		Mode:                  rule.Mode,
		ScheduleInterval:      rule.ScheduleInterval,
		ConflictResolution:    rule.ConflictResolution,
		ReplicateDeletes:      rule.ReplicateDeletes,
		ReplicateMetadata:     rule.ReplicateMetadata,
		CreatedAt:             rule.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:             rule.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	s.writeJSON(w, response)
}

func (s *Server) handleUpdateReplicationRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	ruleID := vars["ruleId"]

	user, userExists := auth.GetUserFromContext(ctx)
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	tenantID := user.TenantID

	existingRule, err := s.replicationManager.GetRule(ctx, ruleID)
	if err != nil {
		logrus.WithError(err).Error("Failed to get replication rule")
		s.writeError(w, "Failed to get replication rule", http.StatusInternalServerError)
		return
	}

	if existingRule == nil {
		s.writeError(w, "Replication rule not found", http.StatusNotFound)
		return
	}

	if existingRule.TenantID != tenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	var req ReplicationRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	existingRule.DestinationEndpoint = req.DestinationEndpoint
	existingRule.DestinationBucket = req.DestinationBucket
	existingRule.DestinationAccessKey = req.DestinationAccessKey
	existingRule.DestinationSecretKey = req.DestinationSecretKey
	existingRule.DestinationRegion = req.DestinationRegion
	existingRule.Prefix = req.Prefix
	existingRule.Enabled = req.Enabled
	existingRule.Priority = req.Priority
	existingRule.Mode = req.Mode
	existingRule.ScheduleInterval = req.ScheduleInterval
	existingRule.ConflictResolution = req.ConflictResolution
	existingRule.ReplicateDeletes = req.ReplicateDeletes
	existingRule.ReplicateMetadata = req.ReplicateMetadata

	if err := s.replicationManager.UpdateRule(ctx, existingRule); err != nil {
		logrus.WithError(err).Error("Failed to update replication rule")
		s.writeError(w, "Failed to update replication rule", http.StatusInternalServerError)
		return
	}

	if s.auditManager != nil {
		_ = s.auditManager.LogEvent(ctx, &audit.AuditEvent{
			TenantID:     tenantID,
			UserID:       user.ID,
			Username:     user.Username,
			EventType:    "replication.rule.updated",
			ResourceType: "replication_rule",
			ResourceID:   ruleID,
			ResourceName: existingRule.SourceBucket,
			Action:       "update",
			Status:       "success",
			Details: map[string]interface{}{
				"rule_id": ruleID,
				"enabled": req.Enabled,
			},
		})
	}

	response := ReplicationRuleResponse{
		ID:                    existingRule.ID,
		TenantID:              existingRule.TenantID,
		SourceBucket:          existingRule.SourceBucket,
		DestinationEndpoint:   existingRule.DestinationEndpoint,
		DestinationBucket:     existingRule.DestinationBucket,
		DestinationAccessKey:  existingRule.DestinationAccessKey,
		DestinationSecretKey:  existingRule.DestinationSecretKey,
		DestinationRegion:     existingRule.DestinationRegion,
		Prefix:                existingRule.Prefix,
		Enabled:               existingRule.Enabled,
		Priority:              existingRule.Priority,
		Mode:                  existingRule.Mode,
		ScheduleInterval:      existingRule.ScheduleInterval,
		ConflictResolution:    existingRule.ConflictResolution,
		ReplicateDeletes:      existingRule.ReplicateDeletes,
		ReplicateMetadata:     existingRule.ReplicateMetadata,
		CreatedAt:             existingRule.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:          existingRule.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	s.writeJSON(w, response)
}

func (s *Server) handleDeleteReplicationRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	ruleID := vars["ruleId"]

	user, userExists := auth.GetUserFromContext(ctx)
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	tenantID := user.TenantID

	existingRule, err := s.replicationManager.GetRule(ctx, ruleID)
	if err != nil {
		logrus.WithError(err).Error("Failed to get replication rule")
		s.writeError(w, "Failed to get replication rule", http.StatusInternalServerError)
		return
	}

	if existingRule == nil {
		s.writeError(w, "Replication rule not found", http.StatusNotFound)
		return
	}

	if existingRule.TenantID != tenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	if err := s.replicationManager.DeleteRule(ctx, tenantID, ruleID); err != nil {
		logrus.WithError(err).Error("Failed to delete replication rule")
		s.writeError(w, "Failed to delete replication rule", http.StatusInternalServerError)
		return
	}

	if s.auditManager != nil {
		_ = s.auditManager.LogEvent(ctx, &audit.AuditEvent{
			TenantID:     tenantID,
			UserID:       user.ID,
			Username:     user.Username,
			EventType:    "replication.rule.deleted",
			ResourceType: "replication_rule",
			ResourceID:   ruleID,
			ResourceName: existingRule.SourceBucket,
			Action:       "delete",
			Status:       "success",
			Details: map[string]interface{}{
				"rule_id": ruleID,
			},
		})
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetReplicationMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	ruleID := vars["ruleId"]

	user, userExists := auth.GetUserFromContext(ctx)
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	tenantID := user.TenantID

	rule, err := s.replicationManager.GetRule(ctx, ruleID)
	if err != nil {
		logrus.WithError(err).Error("Failed to get replication rule")
		s.writeError(w, "Failed to get replication rule", http.StatusInternalServerError)
		return
	}

	if rule == nil {
		s.writeError(w, "Replication rule not found", http.StatusNotFound)
		return
	}

	if rule.TenantID != tenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	metrics, err := s.replicationManager.GetMetrics(ctx, ruleID)
	if err != nil {
		logrus.WithError(err).Error("Failed to get replication metrics")
		s.writeError(w, "Failed to get replication metrics", http.StatusInternalServerError)
		return
	}

	response := ReplicationMetricsResponse{
		RuleID:           metrics.RuleID,
		TotalObjects:     metrics.TotalObjects,
		PendingObjects:   metrics.PendingObjects,
		CompletedObjects: metrics.CompletedObjects,
		FailedObjects:    metrics.FailedObjects,
		BytesReplicated:  metrics.BytesReplicated,
	}

	if metrics.LastSuccess != nil {
		lastSuccess := metrics.LastSuccess.Format("2006-01-02T15:04:05Z07:00")
		response.LastSuccess = &lastSuccess
	}

	if metrics.LastFailure != nil {
		lastFailure := metrics.LastFailure.Format("2006-01-02T15:04:05Z07:00")
		response.LastFailure = &lastFailure
	}

	s.writeJSON(w, response)
}

// handleTriggerReplicationSync triggers a manual replication sync for a rule
func (s *Server) handleTriggerReplicationSync(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	ruleID := vars["ruleId"]

	user, userExists := auth.GetUserFromContext(ctx)
	if !userExists {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	tenantID := user.TenantID

	// Get rule to verify tenant ownership
	rule, err := s.replicationManager.GetRule(ctx, ruleID)
	if err != nil {
		logrus.WithError(err).Error("Failed to get replication rule")
		s.writeError(w, "Failed to get replication rule", http.StatusInternalServerError)
		return
	}

	if rule == nil {
		s.writeError(w, "Replication rule not found", http.StatusNotFound)
		return
	}

	if rule.TenantID != tenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	// Trigger manual sync
	queuedCount, err := s.replicationManager.SyncRule(ctx, ruleID)
	if err != nil {
		logrus.WithError(err).Error("Failed to trigger replication sync")
		s.writeError(w, fmt.Sprintf("Failed to trigger sync: %v", err), http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"rule_id":      ruleID,
		"tenant_id":    tenantID,
		"queued_count": queuedCount,
	}).Info("Manual replication sync triggered")

	response := map[string]interface{}{
		"success":      true,
		"message":      "Replication sync triggered successfully",
		"queued_count": queuedCount,
		"rule_id":      ruleID,
	}

	s.writeJSON(w, response)
}
