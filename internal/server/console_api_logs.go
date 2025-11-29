package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/sirupsen/logrus"
)

// FrontendLogRequest represents a log entry from the frontend
type FrontendLogRequest struct {
	Level     string                 `json:"level"`     // debug, info, warn, error
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

// handleTestLogOutput tests a specific log output configuration
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
