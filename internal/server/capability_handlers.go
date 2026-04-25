package server

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/sirupsen/logrus"
)

// handleGetUserCapabilities returns the effective capability matrix for a user.
// GET /api/v1/users/{id}/capabilities
// Admin (global or tenant-scoped) only.
func (s *Server) handleGetUserCapabilities(w http.ResponseWriter, r *http.Request) {
	caller, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if !auth.IsAdminUser(r.Context()) {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}

	userID := mux.Vars(r)["id"]
	target, err := s.authManager.GetUser(r.Context(), userID)
	if err != nil {
		s.writeError(w, "User not found", http.StatusNotFound)
		return
	}

	// Tenant admin can only manage users in their own tenant.
	isGlobalAdmin := auth.IsAdminUser(r.Context()) && caller.TenantID == ""
	if !isGlobalAdmin && caller.TenantID != target.TenantID {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}

	effective, err := s.authManager.GetEffectiveCapabilities(r.Context(), target.ID, target.Roles)
	if err != nil {
		logrus.WithError(err).Error("Failed to get effective capabilities")
		s.writeError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"capabilities": effective,
	})
}

// handleSetUserCapability sets or removes a single capability override for a user.
// PUT /api/v1/users/{id}/capabilities/{capability}
// Body: {"granted": true|false}  — omit body or send null to delete the override.
func (s *Server) handleSetUserCapability(w http.ResponseWriter, r *http.Request) {
	caller, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if !auth.IsAdminUser(r.Context()) {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}

	vars := mux.Vars(r)
	userID := vars["id"]
	capability := vars["capability"]

	target, err := s.authManager.GetUser(r.Context(), userID)
	if err != nil {
		s.writeError(w, "User not found", http.StatusNotFound)
		return
	}

	isGlobalAdmin := auth.IsAdminUser(r.Context()) && caller.TenantID == ""
	if !isGlobalAdmin && caller.TenantID != target.TenantID {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}

	// Validate capability name.
	valid := false
	for _, c := range auth.AllCapabilities {
		if c == capability {
			valid = true
			break
		}
	}
	if !valid {
		s.writeError(w, "Unknown capability: "+capability, http.StatusBadRequest)
		return
	}

	var req struct {
		Granted *bool `json:"granted"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Granted == nil {
		s.writeError(w, "Body must be {\"granted\": true|false}", http.StatusBadRequest)
		return
	}

	if err := s.authManager.SetCapabilityOverride(r.Context(), userID, capability, caller.ID, *req.Granted); err != nil {
		logrus.WithError(err).Error("Failed to set capability override")
		s.writeError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Invalidate user sync checksum so cluster picks up the change.
	if s.userSyncMgr != nil {
		s.userSyncMgr.TriggerSync(r.Context())
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Capability override set",
	})
}

// handleDeleteUserCapability removes an explicit override, reverting to role default.
// DELETE /api/v1/users/{id}/capabilities/{capability}
func (s *Server) handleDeleteUserCapability(w http.ResponseWriter, r *http.Request) {
	caller, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if !auth.IsAdminUser(r.Context()) {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}

	vars := mux.Vars(r)
	userID := vars["id"]
	capability := vars["capability"]

	target, err := s.authManager.GetUser(r.Context(), userID)
	if err != nil {
		s.writeError(w, "User not found", http.StatusNotFound)
		return
	}

	isGlobalAdmin := auth.IsAdminUser(r.Context()) && caller.TenantID == ""
	if !isGlobalAdmin && caller.TenantID != target.TenantID {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}

	if err := s.authManager.DeleteCapabilityOverride(r.Context(), userID, capability); err != nil {
		logrus.WithError(err).Error("Failed to delete capability override")
		s.writeError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if s.userSyncMgr != nil {
		s.userSyncMgr.TriggerSync(r.Context())
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Capability override removed",
	})
}

// handleGetRoleCapabilities returns the capability defaults for all roles.
// GET /api/v1/roles/capabilities
// Global admin only.
func (s *Server) handleGetRoleCapabilities(w http.ResponseWriter, r *http.Request) {
	if !auth.IsAdminUser(r.Context()) {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}
	caller, _ := auth.GetUserFromContext(r.Context())
	if caller.TenantID != "" {
		s.writeError(w, "Forbidden: global admin only", http.StatusForbidden)
		return
	}

	all, err := s.authManager.GetAllRoleCapabilities(r.Context())
	if err != nil {
		logrus.WithError(err).Error("Failed to get role capabilities")
		s.writeError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":             true,
		"role_capabilities":   all,
		"all_capabilities":    auth.AllCapabilities,
	})
}

// handleSetRoleCapabilities replaces the capability set for a single role.
// PUT /api/v1/roles/{role}/capabilities
// Body: {"capabilities": ["bucket:create", ...]}
// Global admin only.
func (s *Server) handleSetRoleCapabilities(w http.ResponseWriter, r *http.Request) {
	if !auth.IsAdminUser(r.Context()) {
		s.writeError(w, "Forbidden", http.StatusForbidden)
		return
	}
	caller, _ := auth.GetUserFromContext(r.Context())
	if caller.TenantID != "" {
		s.writeError(w, "Forbidden: global admin only", http.StatusForbidden)
		return
	}

	role := mux.Vars(r)["role"]

	var req struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate each capability name.
	capSet := make(map[string]bool, len(auth.AllCapabilities))
	for _, c := range auth.AllCapabilities {
		capSet[c] = true
	}
	for _, c := range req.Capabilities {
		if !capSet[c] {
			s.writeError(w, "Unknown capability: "+c, http.StatusBadRequest)
			return
		}
	}

	if err := s.authManager.SetRoleCapabilities(r.Context(), role, req.Capabilities); err != nil {
		logrus.WithError(err).Error("Failed to set role capabilities")
		s.writeError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Role capabilities updated",
	})
}
