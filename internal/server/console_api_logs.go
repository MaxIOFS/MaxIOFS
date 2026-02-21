package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/logging"
	"github.com/sirupsen/logrus"
)

// FrontendLogRequest represents a log entry from the frontend
type FrontendLogRequest struct {
	Level     string                 `json:"level"` // debug, info, warn, error
	Message   string                 `json:"message"`
	Context   map[string]interface{} `json:"context"`
	Timestamp string                 `json:"timestamp"`
	UserAgent string                 `json:"userAgent"`
	URL       string                 `json:"url"`
}

// handlePostFrontendLogs receives logs from the frontend
func (s *Server) handlePostFrontendLogs(w http.ResponseWriter, r *http.Request) {
	// Check if frontend logging is enabled
	enabled, err := s.settingsManager.GetBool("logging.frontend_enabled")
	if err != nil {
		enabled = true // default to enabled
	}
	if !enabled {
		http.Error(w, "Frontend logging is disabled", http.StatusForbidden)
		return
	}

	// Parse request
	var logReq FrontendLogRequest
	if err := json.NewDecoder(r.Body).Decode(&logReq); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Get minimum log level for frontend logs
	minLevel, err := s.settingsManager.Get("logging.frontend_level")
	if err != nil {
		minLevel = "error" // default
	}

	// Check if this log level should be logged
	if !shouldLogLevel(logReq.Level, minLevel) {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Get user from context (if authenticated)
	var userID, username, tenantID string
	if user, exists := auth.GetUserFromContext(r.Context()); exists {
		userID = user.ID
		username = user.Username
		tenantID = user.TenantID
	}

	// Build log fields
	fields := logrus.Fields{
		"component": "frontend",
		"url":       logReq.URL,
		"userAgent": logReq.UserAgent,
	}

	// Add user info if available
	if userID != "" {
		fields["userId"] = userID
		fields["username"] = username
		fields["tenantId"] = tenantID
	}

	// Add context fields
	for k, v := range logReq.Context {
		fields[k] = v
	}

	// Log with appropriate level
	entry := logrus.WithFields(fields)
	switch logReq.Level {
	case "debug":
		entry.Debug(logReq.Message)
	case "info":
		entry.Info(logReq.Message)
	case "warn", "warning":
		entry.Warn(logReq.Message)
	case "error":
		entry.Error(logReq.Message)
	default:
		entry.Info(logReq.Message)
	}

	w.WriteHeader(http.StatusNoContent)
}

// shouldLogLevel checks if a log level should be logged based on minimum level
func shouldLogLevel(level, minLevel string) bool {
	levels := map[string]int{
		"debug": 0,
		"info":  1,
		"warn":  2,
		"error": 3,
	}

	logLevel, ok := levels[level]
	if !ok {
		logLevel = 1 // default to info
	}

	minLogLevel, ok := levels[minLevel]
	if !ok {
		minLogLevel = 3 // default to error
	}

	return logLevel >= minLogLevel
}

// handleTestLogOutput tests a specific output type (legacy endpoint)
func (s *Server) handleTestLogOutput(w http.ResponseWriter, r *http.Request) {
	// Only global admins can test log outputs
	user, exists := auth.GetUserFromContext(r.Context())
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if !exists || !isGlobalAdmin {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Get output type from query
	outputType := r.URL.Query().Get("type")
	if outputType == "" {
		s.writeError(w, "Missing output type", http.StatusBadRequest)
		return
	}

	// Test the output
	err := s.loggingManager.TestOutput(outputType)
	if err != nil {
		logrus.WithError(err).Errorf("Failed to test %s output", outputType)
		s.writeError(w, "Failed to test output: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("%s output test successful", outputType),
	})
}

// handleReconfigureLogging reconfigures logging based on current settings
func (s *Server) handleReconfigureLogging(w http.ResponseWriter, r *http.Request) {
	// Only global admins can reconfigure logging
	user, exists := auth.GetUserFromContext(r.Context())
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if !exists || !isGlobalAdmin {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Reconfigure logging
	s.loggingManager.Reconfigure()

	logrus.WithFields(logrus.Fields{
		"userId":   user.ID,
		"username": user.Username,
	}).Info("Logging reconfigured by admin")

	s.writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "Logging reconfigured successfully",
	})
}

// ---- Logging Targets CRUD ----

// handleListLoggingTargets returns all logging targets
func (s *Server) handleListLoggingTargets(w http.ResponseWriter, r *http.Request) {
	user, exists := auth.GetUserFromContext(r.Context())
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if !exists || !isGlobalAdmin {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}

	store := s.loggingManager.GetTargetStore()
	if store == nil {
		s.writeError(w, "Logging targets not available", http.StatusServiceUnavailable)
		return
	}

	targets, err := store.List()
	if err != nil {
		s.writeError(w, "Failed to list logging targets: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Sanitize: don't return TLS keys or auth tokens in list view
	for i := range targets {
		targets[i].TLSKey = ""
		if targets[i].AuthToken != "" {
			targets[i].AuthToken = "••••••••"
		}
	}

	s.writeJSON(w, map[string]interface{}{
		"targets":      targets,
		"active_count": s.loggingManager.GetActiveOutputs(),
	})
}

// handleGetLoggingTarget returns a single logging target
func (s *Server) handleGetLoggingTarget(w http.ResponseWriter, r *http.Request) {
	user, exists := auth.GetUserFromContext(r.Context())
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if !exists || !isGlobalAdmin {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}

	store := s.loggingManager.GetTargetStore()
	if store == nil {
		s.writeError(w, "Logging targets not available", http.StatusServiceUnavailable)
		return
	}

	id := mux.Vars(r)["id"]
	target, err := store.Get(id)
	if err != nil {
		s.writeError(w, "Logging target not found", http.StatusNotFound)
		return
	}

	// Sanitize secrets
	target.TLSKey = ""
	if target.AuthToken != "" {
		target.AuthToken = "••••••••"
	}

	s.writeJSON(w, target)
}

// handleCreateLoggingTarget creates a new logging target
func (s *Server) handleCreateLoggingTarget(w http.ResponseWriter, r *http.Request) {
	user, exists := auth.GetUserFromContext(r.Context())
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if !exists || !isGlobalAdmin {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}

	store := s.loggingManager.GetTargetStore()
	if store == nil {
		s.writeError(w, "Logging targets not available", http.StatusServiceUnavailable)
		return
	}

	var cfg logging.TargetConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		s.writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := store.Create(&cfg); err != nil {
		s.writeError(w, "Failed to create logging target: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Reconfigure to pick up the new target
	s.loggingManager.Reconfigure()

	logrus.WithFields(logrus.Fields{
		"userId":      user.ID,
		"username":    user.Username,
		"target_id":   cfg.ID,
		"target_name": cfg.Name,
		"target_type": cfg.Type,
	}).Info("Logging target created by admin")

	s.writeJSONWithStatus(w, http.StatusCreated, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"id":      cfg.ID,
			"name":    cfg.Name,
			"message": "Logging target created successfully",
		},
	})
}

// handleUpdateLoggingTarget updates an existing logging target
func (s *Server) handleUpdateLoggingTarget(w http.ResponseWriter, r *http.Request) {
	user, exists := auth.GetUserFromContext(r.Context())
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if !exists || !isGlobalAdmin {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}

	store := s.loggingManager.GetTargetStore()
	if store == nil {
		s.writeError(w, "Logging targets not available", http.StatusServiceUnavailable)
		return
	}

	id := mux.Vars(r)["id"]

	var cfg logging.TargetConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		s.writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	cfg.ID = id

	// If TLS key is masked or empty, preserve the existing one
	if cfg.TLSKey == "" || cfg.TLSKey == "••••••••" {
		existing, err := store.Get(id)
		if err == nil {
			cfg.TLSKey = existing.TLSKey
		}
	}
	// Same for auth token
	if cfg.AuthToken == "••••••••" {
		existing, err := store.Get(id)
		if err == nil {
			cfg.AuthToken = existing.AuthToken
		}
	}

	if err := store.Update(&cfg); err != nil {
		s.writeError(w, "Failed to update logging target: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Reconfigure to apply changes
	s.loggingManager.Reconfigure()

	logrus.WithFields(logrus.Fields{
		"userId":      user.ID,
		"username":    user.Username,
		"target_id":   id,
		"target_name": cfg.Name,
	}).Info("Logging target updated by admin")

	s.writeJSON(w, map[string]interface{}{
		"id":      id,
		"message": "Logging target updated successfully",
	})
}

// handleDeleteLoggingTarget deletes a logging target
func (s *Server) handleDeleteLoggingTarget(w http.ResponseWriter, r *http.Request) {
	user, exists := auth.GetUserFromContext(r.Context())
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if !exists || !isGlobalAdmin {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}

	store := s.loggingManager.GetTargetStore()
	if store == nil {
		s.writeError(w, "Logging targets not available", http.StatusServiceUnavailable)
		return
	}

	id := mux.Vars(r)["id"]

	if err := store.Delete(id); err != nil {
		s.writeError(w, "Failed to delete logging target: "+err.Error(), http.StatusNotFound)
		return
	}

	// Reconfigure to remove the output
	s.loggingManager.Reconfigure()

	logrus.WithFields(logrus.Fields{
		"userId":    user.ID,
		"username":  user.Username,
		"target_id": id,
	}).Info("Logging target deleted by admin")

	s.writeJSON(w, map[string]interface{}{
		"id":      id,
		"message": "Logging target deleted successfully",
	})
}

// handleTestLoggingTarget tests an existing logging target by ID
func (s *Server) handleTestLoggingTarget(w http.ResponseWriter, r *http.Request) {
	user, exists := auth.GetUserFromContext(r.Context())
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if !exists || !isGlobalAdmin {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}

	id := mux.Vars(r)["id"]

	err := s.loggingManager.TestTarget(id)
	if err != nil {
		logrus.WithError(err).WithField("target_id", id).Error("Failed to test logging target")
		s.writeError(w, "Test failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "Test message sent successfully",
	})
}

// handleTestLoggingTargetConfig tests a logging target configuration without saving
func (s *Server) handleTestLoggingTargetConfig(w http.ResponseWriter, r *http.Request) {
	user, exists := auth.GetUserFromContext(r.Context())
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && user.TenantID == ""
	if !exists || !isGlobalAdmin {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}

	var cfg logging.TargetConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		s.writeError(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	err := s.loggingManager.TestTargetConfig(&cfg)
	if err != nil {
		logrus.WithError(err).WithField("target_type", cfg.Type).Error("Failed to test logging target config")
		s.writeError(w, "Test failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"success": true,
		"message": "Test message sent successfully",
	})
}
