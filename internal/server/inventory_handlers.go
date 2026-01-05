package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/inventory"
	"github.com/sirupsen/logrus"
)

// handlePutBucketInventory configures inventory for a bucket
// PUT /api/v1/buckets/{bucket}/inventory
func (s *Server) handlePutBucketInventory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Get current user
	currentUser, ok := auth.GetUserFromContext(ctx)
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	// Check if tenantId is provided in query params (for global admins)
	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := currentUser.TenantID

	// Global admins can access buckets from any tenant
	isGlobalAdmin := auth.IsAdminUser(ctx) && currentUser.TenantID == ""
	if queryTenantID != "" && isGlobalAdmin {
		tenantID = queryTenantID
	}

	// Verify bucket exists (this also checks access)
	_, err := s.bucketManager.GetBucketInfo(ctx, tenantID, bucketName)
	if err != nil {
		s.writeError(w, "Bucket not found or access denied", http.StatusNotFound)
		return
	}

	// Parse request body
	var req struct {
		Enabled           bool     `json:"enabled"`
		Frequency         string   `json:"frequency"`          // "daily" or "weekly"
		Format            string   `json:"format"`             // "csv" or "json"
		DestinationBucket string   `json:"destination_bucket"`
		DestinationPrefix string   `json:"destination_prefix,omitempty"`
		IncludedFields    []string `json:"included_fields"`
		ScheduleTime      string   `json:"schedule_time"` // HH:MM format
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Frequency != "daily" && req.Frequency != "weekly" {
		s.writeError(w, "Frequency must be 'daily' or 'weekly'", http.StatusBadRequest)
		return
	}

	if req.Format != "csv" && req.Format != "json" {
		s.writeError(w, "Format must be 'csv' or 'json'", http.StatusBadRequest)
		return
	}

	if req.DestinationBucket == "" {
		s.writeError(w, "Destination bucket is required", http.StatusBadRequest)
		return
	}

	// Check for circular reference
	if req.DestinationBucket == bucketName {
		s.writeError(w, "Circular reference: destination bucket cannot be the same as source bucket", http.StatusBadRequest)
		return
	}

	// Validate destination bucket exists
	_, err = s.bucketManager.GetBucketInfo(ctx, tenantID, req.DestinationBucket)
	if err != nil {
		s.writeError(w, "Destination bucket not found or access denied", http.StatusBadRequest)
		return
	}

	// Validate included fields
	if len(req.IncludedFields) == 0 {
		req.IncludedFields = inventory.DefaultIncludedFields()
	} else if !inventory.ValidateIncludedFields(req.IncludedFields) {
		s.writeError(w, "Invalid included fields", http.StatusBadRequest)
		return
	}

	// Validate schedule time format
	if req.ScheduleTime == "" {
		req.ScheduleTime = "00:00"
	}

	// Check if configuration already exists
	existingConfig, err := s.inventoryManager.GetConfig(ctx, bucketName, tenantID)
	if err == nil {
		// Update existing configuration
		existingConfig.Enabled = req.Enabled
		existingConfig.Frequency = req.Frequency
		existingConfig.Format = req.Format
		existingConfig.DestinationBucket = req.DestinationBucket
		existingConfig.DestinationPrefix = req.DestinationPrefix
		existingConfig.IncludedFields = req.IncludedFields
		existingConfig.ScheduleTime = req.ScheduleTime

		if err := s.inventoryManager.UpdateConfig(ctx, existingConfig); err != nil {
			s.writeError(w, fmt.Sprintf("Failed to update inventory configuration: %v", err), http.StatusInternalServerError)
			return
		}

		s.writeJSON(w, existingConfig)
		return
	}

	// Create new configuration
	config := &inventory.InventoryConfig{
		BucketName:        bucketName,
		TenantID:          tenantID,
		Enabled:           req.Enabled,
		Frequency:         req.Frequency,
		Format:            req.Format,
		DestinationBucket: req.DestinationBucket,
		DestinationPrefix: req.DestinationPrefix,
		IncludedFields:    req.IncludedFields,
		ScheduleTime:      req.ScheduleTime,
	}

	if err := s.inventoryManager.CreateConfig(ctx, config); err != nil {
		s.writeError(w, fmt.Sprintf("Failed to create inventory configuration: %v", err), http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"bucket":    bucketName,
		"tenant_id": tenantID,
		"frequency": req.Frequency,
	}).Info("Bucket inventory configuration created")

	s.writeJSON(w, config)
}

// handleGetBucketInventory gets inventory configuration for a bucket
// GET /api/v1/buckets/{bucket}/inventory
func (s *Server) handleGetBucketInventory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Get current user
	currentUser, ok := auth.GetUserFromContext(ctx)
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	// Check if tenantId is provided in query params (for global admins)
	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := currentUser.TenantID

	// Global admins can access buckets from any tenant
	isGlobalAdmin := auth.IsAdminUser(ctx) && currentUser.TenantID == ""
	if queryTenantID != "" && isGlobalAdmin {
		tenantID = queryTenantID
	}

	// Verify bucket exists
	_, err := s.bucketManager.GetBucketInfo(ctx, tenantID, bucketName)
	if err != nil {
		s.writeError(w, "Bucket not found or access denied", http.StatusNotFound)
		return
	}

	// Get configuration
	config, err := s.inventoryManager.GetConfig(ctx, bucketName, tenantID)
	if err != nil {
		s.writeError(w, "Inventory configuration not found", http.StatusNotFound)
		return
	}

	s.writeJSON(w, config)
}

// handleDeleteBucketInventory deletes inventory configuration for a bucket
// DELETE /api/v1/buckets/{bucket}/inventory
func (s *Server) handleDeleteBucketInventory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Get current user
	currentUser, ok := auth.GetUserFromContext(ctx)
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	// Check if tenantId is provided in query params (for global admins)
	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := currentUser.TenantID

	// Global admins can access buckets from any tenant
	isGlobalAdmin := auth.IsAdminUser(ctx) && currentUser.TenantID == ""
	if queryTenantID != "" && isGlobalAdmin {
		tenantID = queryTenantID
	}

	// Verify bucket exists
	_, err := s.bucketManager.GetBucketInfo(ctx, tenantID, bucketName)
	if err != nil {
		s.writeError(w, "Bucket not found or access denied", http.StatusNotFound)
		return
	}

	// Delete configuration
	if err := s.inventoryManager.DeleteConfig(ctx, bucketName, tenantID); err != nil {
		s.writeError(w, fmt.Sprintf("Failed to delete inventory configuration: %v", err), http.StatusInternalServerError)
		return
	}

	logrus.WithFields(logrus.Fields{
		"bucket":    bucketName,
		"tenant_id": tenantID,
	}).Info("Bucket inventory configuration deleted")

	s.writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "Inventory configuration deleted successfully",
	})
}

// handleListBucketInventoryReports lists inventory reports for a bucket
// GET /api/v1/buckets/{bucket}/inventory/reports
func (s *Server) handleListBucketInventoryReports(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	vars := mux.Vars(r)
	bucketName := vars["bucket"]

	// Get current user
	currentUser, ok := auth.GetUserFromContext(ctx)
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}

	// Check if tenantId is provided in query params (for global admins)
	queryTenantID := r.URL.Query().Get("tenantId")
	tenantID := currentUser.TenantID

	// Global admins can access buckets from any tenant
	isGlobalAdmin := auth.IsAdminUser(ctx) && currentUser.TenantID == ""
	if queryTenantID != "" && isGlobalAdmin {
		tenantID = queryTenantID
	}

	// Verify bucket exists
	_, err := s.bucketManager.GetBucketInfo(ctx, tenantID, bucketName)
	if err != nil {
		s.writeError(w, "Bucket not found or access denied", http.StatusNotFound)
		return
	}

	// Parse pagination parameters
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 50 // Default limit
	offset := 0 // Default offset

	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
			if limit > 1000 {
				limit = 1000 // Max limit
			}
		}
	}

	if offsetStr != "" {
		if parsedOffset, err := strconv.Atoi(offsetStr); err == nil && parsedOffset >= 0 {
			offset = parsedOffset
		}
	}

	// List reports
	reports, err := s.inventoryManager.ListReports(ctx, bucketName, tenantID, limit, offset)
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to list inventory reports: %v", err), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"reports": reports,
		"limit":   limit,
		"offset":  offset,
		"count":   len(reports),
	})
}
