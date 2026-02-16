package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/sirupsen/logrus"
)

// writeJSONWithStatus writes a JSON response with a specific HTTP status code
func (s *Server) writeJSONWithStatus(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// =============================================================================
// Identity Provider CRUD Handlers
// =============================================================================

func (s *Server) handleListIDPs(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	tenantID := ""
	if !s.isGlobalAdmin(user) {
		tenantID = user.TenantID
	}

	providers, err := s.idpManager.ListProviders(r.Context(), tenantID)
	if err != nil {
		s.writeError(w, "Failed to list identity providers", http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, providers)
}

func (s *Server) handleCreateIDP(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	var provider idp.IdentityProvider
	if err := json.NewDecoder(r.Body).Decode(&provider); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if provider.Name == "" || provider.Type == "" {
		s.writeError(w, "Name and type are required", http.StatusBadRequest)
		return
	}

	if provider.Type != idp.TypeLDAP && provider.Type != idp.TypeOAuth2 {
		s.writeError(w, "Invalid provider type. Must be 'ldap' or 'oauth2'", http.StatusBadRequest)
		return
	}

	provider.CreatedBy = user.ID

	if !s.isGlobalAdmin(user) {
		provider.TenantID = user.TenantID
	}

	// Auto-fill redirect_uri for OAuth providers if not set
	if provider.Type == idp.TypeOAuth2 && provider.Config.OAuth2 != nil && provider.Config.OAuth2.RedirectURI == "" {
		if s.config.PublicConsoleURL != "" {
			provider.Config.OAuth2.RedirectURI = strings.TrimRight(s.config.PublicConsoleURL, "/") + "/api/v1/auth/oauth/callback"
		}
	}

	if err := s.idpManager.CreateProvider(r.Context(), &provider); err != nil {
		logrus.WithError(err).Error("Failed to create identity provider")
		s.writeError(w, "Failed to create identity provider", http.StatusInternalServerError)
		return
	}

	s.logAuditEvent(r.Context(), &audit.AuditEvent{
		TenantID:     user.TenantID,
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    audit.EventTypeUserCreated,
		ResourceType: "identity_provider",
		ResourceID:   provider.ID,
		ResourceName: provider.Name,
		Action:       "create_identity_provider",
		Status:       audit.StatusSuccess,
		IPAddress:    getClientIP(r),
		UserAgent:    r.Header.Get("User-Agent"),
	})

	s.writeJSONWithStatus(w, http.StatusCreated, APIResponse{Success: true, Data: provider})
}

func (s *Server) handleGetIDP(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	providerID := mux.Vars(r)["id"]
	provider, err := s.idpManager.GetProviderMasked(r.Context(), providerID)
	if err != nil {
		s.writeError(w, "Identity provider not found", http.StatusNotFound)
		return
	}

	if !s.isGlobalAdmin(user) && provider.TenantID != "" && provider.TenantID != user.TenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	s.writeJSON(w, provider)
}

func (s *Server) handleUpdateIDP(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	providerID := mux.Vars(r)["id"]

	existing, err := s.idpManager.GetProvider(r.Context(), providerID)
	if err != nil {
		s.writeError(w, "Identity provider not found", http.StatusNotFound)
		return
	}

	if !s.isGlobalAdmin(user) && existing.TenantID != "" && existing.TenantID != user.TenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	var updated idp.IdentityProvider
	if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	updated.ID = providerID
	updated.CreatedBy = existing.CreatedBy
	updated.CreatedAt = existing.CreatedAt

	if updated.Config.LDAP != nil && updated.Config.LDAP.BindPassword == "********" {
		if existing.Config.LDAP != nil {
			updated.Config.LDAP.BindPassword = existing.Config.LDAP.BindPassword
		}
	}
	if updated.Config.OAuth2 != nil && updated.Config.OAuth2.ClientSecret == "********" {
		if existing.Config.OAuth2 != nil {
			updated.Config.OAuth2.ClientSecret = existing.Config.OAuth2.ClientSecret
		}
	}

	// Auto-fill redirect_uri for OAuth providers if not set
	if updated.Type == idp.TypeOAuth2 && updated.Config.OAuth2 != nil && updated.Config.OAuth2.RedirectURI == "" {
		if s.config.PublicConsoleURL != "" {
			updated.Config.OAuth2.RedirectURI = strings.TrimRight(s.config.PublicConsoleURL, "/") + "/api/v1/auth/oauth/callback"
		}
	}

	if err := s.idpManager.UpdateProvider(r.Context(), &updated); err != nil {
		logrus.WithError(err).Error("Failed to update identity provider")
		s.writeError(w, "Failed to update identity provider", http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, updated)
}

func (s *Server) handleDeleteIDP(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	providerID := mux.Vars(r)["id"]

	existing, err := s.idpManager.GetProviderMasked(r.Context(), providerID)
	if err != nil {
		s.writeError(w, "Identity provider not found", http.StatusNotFound)
		return
	}

	if !s.isGlobalAdmin(user) && existing.TenantID != "" && existing.TenantID != user.TenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	linkedCount, _ := s.idpManager.CountLinkedUsers(r.Context(), providerID, existing.Type)

	if err := s.idpManager.DeleteProvider(r.Context(), providerID); err != nil {
		logrus.WithError(err).Error("Failed to delete identity provider")
		s.writeError(w, "Failed to delete identity provider", http.StatusInternalServerError)
		return
	}

	// Record tombstone for cluster deletion sync
	if s.clusterManager != nil && s.clusterManager.IsClusterEnabled() {
		nodeID, _ := s.clusterManager.GetLocalNodeID(r.Context())
		if err := cluster.RecordDeletion(r.Context(), s.db, cluster.EntityTypeIDPProvider, providerID, nodeID); err != nil {
			logrus.WithError(err).WithField("provider_id", providerID).Warn("Failed to record IDP provider deletion tombstone")
		}
	}

	s.logAuditEvent(r.Context(), &audit.AuditEvent{
		TenantID:     user.TenantID,
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    audit.EventTypeUserDeleted,
		ResourceType: "identity_provider",
		ResourceID:   providerID,
		ResourceName: existing.Name,
		Action:       "delete_identity_provider",
		Status:       audit.StatusSuccess,
		IPAddress:    getClientIP(r),
		UserAgent:    r.Header.Get("User-Agent"),
		Details: map[string]interface{}{
			"linked_users": linkedCount,
		},
	})

	s.writeJSON(w, map[string]interface{}{
		"message":      "Identity provider deleted",
		"linked_users": linkedCount,
	})
}

func (s *Server) handleTestIDPConnection(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	providerID := mux.Vars(r)["id"]
	if err := s.idpManager.TestConnection(r.Context(), providerID); err != nil {
		s.writeJSONWithStatus(w, http.StatusOK, APIResponse{
			Success: false,
			Data: map[string]interface{}{
				"status":  "error",
				"message": err.Error(),
			},
		})
		return
	}

	s.writeJSONWithStatus(w, http.StatusOK, APIResponse{
		Success: true,
		Data: map[string]interface{}{
			"success": true,
			"status":  "success",
			"message": "Connection successful",
		},
	})
}

// =============================================================================
// LDAP Browse & Import Handlers
// =============================================================================

func (s *Server) handleIDPSearchUsers(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	providerID := mux.Vars(r)["id"]

	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 50
	}

	users, err := s.idpManager.SearchUsers(r.Context(), providerID, req.Query, req.Limit)
	if err != nil {
		s.writeError(w, fmt.Sprintf("Search failed: %s", err.Error()), http.StatusBadRequest)
		return
	}

	s.writeJSON(w, users)
}

func (s *Server) handleIDPSearchGroups(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	providerID := mux.Vars(r)["id"]

	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 50
	}

	groups, err := s.idpManager.SearchGroups(r.Context(), providerID, req.Query, req.Limit)
	if err != nil {
		s.writeError(w, fmt.Sprintf("Group search failed: %s", err.Error()), http.StatusBadRequest)
		return
	}

	s.writeJSON(w, groups)
}

func (s *Server) handleIDPGroupMembers(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	providerID := mux.Vars(r)["id"]

	var req struct {
		GroupID string `json:"group_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	members, err := s.idpManager.GetGroupMembers(r.Context(), providerID, req.GroupID)
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to get group members: %s", err.Error()), http.StatusBadRequest)
		return
	}

	s.writeJSON(w, members)
}

func (s *Server) handleIDPImportUsers(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	providerID := mux.Vars(r)["id"]

	provider, err := s.idpManager.GetProviderMasked(r.Context(), providerID)
	if err != nil {
		s.writeError(w, "Identity provider not found", http.StatusNotFound)
		return
	}

	var req idp.ImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Users) == 0 {
		s.writeError(w, "No users specified for import", http.StatusBadRequest)
		return
	}

	if req.Role == "" {
		req.Role = "user"
	}

	if !s.isGlobalAdmin(user) {
		req.TenantID = user.TenantID
	}

	authProviderValue := provider.Type + ":" + providerID

	var results []idp.ImportResult
	imported := 0
	skipped := 0

	for _, entry := range req.Users {
		existing, _ := s.authManager.FindUserByExternalID(r.Context(), entry.ExternalID, authProviderValue)
		if existing != nil {
			results = append(results, idp.ImportResult{
				ExternalID: entry.ExternalID,
				Username:   entry.Username,
				Status:     "skipped",
				Error:      "user already imported",
			})
			skipped++
			continue
		}

		existingByName, _ := s.authManager.GetUser(r.Context(), entry.Username)
		if existingByName != nil {
			results = append(results, idp.ImportResult{
				ExternalID: entry.ExternalID,
				Username:   entry.Username,
				Status:     "error",
				Error:      "username already exists",
			})
			continue
		}

		newUser := &auth.User{
			ID:           "user-" + uuid.New().String()[:8],
			Username:     entry.Username,
			Password:     "",
			DisplayName:  entry.Username,
			Email:        "",
			Status:       auth.UserStatusActive,
			TenantID:     req.TenantID,
			Roles:        []string{req.Role},
			AuthProvider: authProviderValue,
			ExternalID:   entry.ExternalID,
			CreatedAt:    time.Now().Unix(),
			UpdatedAt:    time.Now().Unix(),
		}

		if err := s.authManager.CreateUser(r.Context(), newUser); err != nil {
			results = append(results, idp.ImportResult{
				ExternalID: entry.ExternalID,
				Username:   entry.Username,
				Status:     "error",
				Error:      err.Error(),
			})
			continue
		}

		results = append(results, idp.ImportResult{
			ExternalID: entry.ExternalID,
			Username:   entry.Username,
			Status:     "imported",
		})
		imported++
	}

	s.logAuditEvent(r.Context(), &audit.AuditEvent{
		TenantID:     user.TenantID,
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    audit.EventTypeUserCreated,
		ResourceType: "identity_provider",
		ResourceID:   providerID,
		ResourceName: provider.Name,
		Action:       "import_users",
		Status:       audit.StatusSuccess,
		IPAddress:    getClientIP(r),
		UserAgent:    r.Header.Get("User-Agent"),
		Details: map[string]interface{}{
			"imported": imported,
			"skipped":  skipped,
			"total":    len(req.Users),
		},
	})

	s.writeJSON(w, map[string]interface{}{
		"imported": imported,
		"skipped":  skipped,
		"errors":   len(req.Users) - imported - skipped,
		"results":  results,
	})
}

// =============================================================================
// Group Mapping Handlers
// =============================================================================

func (s *Server) handleListGroupMappings(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	providerID := mux.Vars(r)["id"]
	mappings, err := s.idpManager.ListGroupMappings(r.Context(), providerID)
	if err != nil {
		s.writeError(w, "Failed to list group mappings", http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, mappings)
}

func (s *Server) handleCreateGroupMapping(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	providerID := mux.Vars(r)["id"]

	var mapping idp.GroupMapping
	if err := json.NewDecoder(r.Body).Decode(&mapping); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	mapping.ProviderID = providerID

	if mapping.ExternalGroup == "" || mapping.Role == "" {
		s.writeError(w, "external_group and role are required", http.StatusBadRequest)
		return
	}

	if err := s.idpManager.CreateGroupMapping(r.Context(), &mapping); err != nil {
		logrus.WithError(err).Error("Failed to create group mapping")
		s.writeError(w, "Failed to create group mapping", http.StatusInternalServerError)
		return
	}

	s.writeJSONWithStatus(w, http.StatusCreated, APIResponse{Success: true, Data: mapping})
}

func (s *Server) handleUpdateGroupMapping(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	vars := mux.Vars(r)
	providerID := vars["id"]
	mappingID := vars["mapId"]

	var mapping idp.GroupMapping
	if err := json.NewDecoder(r.Body).Decode(&mapping); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	mapping.ID = mappingID
	mapping.ProviderID = providerID

	if err := s.idpManager.UpdateGroupMapping(r.Context(), &mapping); err != nil {
		s.writeError(w, "Failed to update group mapping", http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, mapping)
}

func (s *Server) handleDeleteGroupMapping(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	mappingID := mux.Vars(r)["mapId"]

	if err := s.idpManager.DeleteGroupMapping(r.Context(), mappingID); err != nil {
		s.writeError(w, "Failed to delete group mapping", http.StatusInternalServerError)
		return
	}

	// Record tombstone for cluster deletion sync
	if s.clusterManager != nil && s.clusterManager.IsClusterEnabled() {
		nodeID, _ := s.clusterManager.GetLocalNodeID(r.Context())
		if err := cluster.RecordDeletion(r.Context(), s.db, cluster.EntityTypeGroupMapping, mappingID, nodeID); err != nil {
			logrus.WithError(err).WithField("mapping_id", mappingID).Warn("Failed to record group mapping deletion tombstone")
		}
	}

	s.writeJSON(w, map[string]string{"message": "Group mapping deleted"})
}

func (s *Server) handleSyncGroupMapping(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	vars := mux.Vars(r)
	providerID := vars["id"]
	mappingID := vars["mapId"]

	mapping, err := s.idpManager.GetGroupMapping(r.Context(), mappingID)
	if err != nil {
		s.writeError(w, "Group mapping not found", http.StatusNotFound)
		return
	}

	provider, err := s.idpManager.GetProviderMasked(r.Context(), providerID)
	if err != nil {
		s.writeError(w, "Identity provider not found", http.StatusNotFound)
		return
	}

	members, err := s.idpManager.GetGroupMembers(r.Context(), providerID, mapping.ExternalGroup)
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to get group members: %s", err.Error()), http.StatusBadRequest)
		return
	}

	authProviderValue := provider.Type + ":" + providerID
	targetTenant := mapping.TenantID
	if targetTenant == "" {
		targetTenant = provider.TenantID
	}

	imported := 0
	syncErrors := 0

	for _, member := range members {
		existing, _ := s.authManager.FindUserByExternalID(r.Context(), member.ExternalID, authProviderValue)
		if existing != nil {
			continue
		}

		// OAuth providers use email as username for consistency with auto-provisioning
		username := member.Username
		if provider.Type == idp.TypeOAuth2 {
			username = member.Email
		}
		if username == "" {
			username = member.Email
		}
		if username == "" {
			syncErrors++
			continue
		}

		newUser := &auth.User{
			ID:           "user-" + uuid.New().String()[:8],
			Username:     username,
			Password:     "",
			DisplayName:  member.DisplayName,
			Email:        member.Email,
			Status:       auth.UserStatusActive,
			TenantID:     targetTenant,
			Roles:        []string{mapping.Role},
			AuthProvider: authProviderValue,
			ExternalID:   member.ExternalID,
			CreatedAt:    time.Now().Unix(),
			UpdatedAt:    time.Now().Unix(),
		}

		if err := s.authManager.CreateUser(r.Context(), newUser); err != nil {
			syncErrors++
			continue
		}
		imported++
	}

	s.idpManager.Store().UpdateGroupMappingSyncTime(mappingID)

	s.writeJSON(w, idp.SyncResult{
		MappingID: mappingID,
		Imported:  imported,
		Errors:    syncErrors,
	})
}

func (s *Server) handleSyncAllMappings(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	providerID := mux.Vars(r)["id"]

	mappings, err := s.idpManager.ListGroupMappings(r.Context(), providerID)
	if err != nil {
		s.writeError(w, "Failed to list group mappings", http.StatusInternalServerError)
		return
	}

	totalImported := 0
	totalErrors := 0

	for _, mapping := range mappings {
		if !mapping.AutoSync {
			continue
		}

		provider, err := s.idpManager.GetProviderMasked(r.Context(), providerID)
		if err != nil {
			continue
		}

		members, err := s.idpManager.GetGroupMembers(r.Context(), providerID, mapping.ExternalGroup)
		if err != nil {
			totalErrors++
			continue
		}

		authProviderValue := provider.Type + ":" + providerID
		targetTenant := mapping.TenantID
		if targetTenant == "" {
			targetTenant = provider.TenantID
		}

		for _, member := range members {
			existing, _ := s.authManager.FindUserByExternalID(r.Context(), member.ExternalID, authProviderValue)
			if existing != nil {
				continue
			}

			// OAuth providers use email as username for consistency with auto-provisioning
			username := member.Username
			if provider.Type == idp.TypeOAuth2 {
				username = member.Email
			}
			if username == "" {
				username = member.Email
			}
			if username == "" {
				totalErrors++
				continue
			}

			newUser := &auth.User{
				ID:           "user-" + uuid.New().String()[:8],
				Username:     username,
				Password:     "",
				DisplayName:  member.DisplayName,
				Email:        member.Email,
				Status:       auth.UserStatusActive,
				TenantID:     targetTenant,
				Roles:        []string{mapping.Role},
				AuthProvider: authProviderValue,
				ExternalID:   member.ExternalID,
				CreatedAt:    time.Now().Unix(),
				UpdatedAt:    time.Now().Unix(),
			}

			if err := s.authManager.CreateUser(r.Context(), newUser); err != nil {
				totalErrors++
				continue
			}
			totalImported++
		}

		s.idpManager.Store().UpdateGroupMappingSyncTime(mapping.ID)
	}

	s.writeJSON(w, map[string]interface{}{
		"imported": totalImported,
		"errors":   totalErrors,
	})
}

// =============================================================================
// OAuth Flow Handlers
// =============================================================================

func (s *Server) handleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	providerID := mux.Vars(r)["id"]
	loginHint := r.URL.Query().Get("login_hint")

	s.startOAuthFlow(w, r, providerID, loginHint)
}

// handleOAuthStart resolves the correct provider from a preset+email and starts the OAuth flow.
// This allows the login page to show one button per type (Google, Microsoft) instead of one per provider.
// Accepts both form data (HTML form submit) and JSON.
func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	var email, preset string

	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		var req struct {
			Email  string `json:"email"`
			Preset string `json:"preset"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Redirect(w, r, "/login?error=invalid_request", http.StatusFound)
			return
		}
		email = req.Email
		preset = req.Preset
	} else {
		r.ParseForm()
		email = r.FormValue("email")
		preset = r.FormValue("preset")
	}

	if email == "" || preset == "" {
		http.Redirect(w, r, "/login?error=invalid_request", http.StatusFound)
		return
	}

	candidates, err := s.idpManager.FindOAuthProvidersByPreset(r.Context(), preset)
	if err != nil || len(candidates) == 0 {
		http.Redirect(w, r, "/login?error=provider_unavailable", http.StatusFound)
		return
	}

	// If only one provider for this preset, use it directly
	provider := candidates[0]

	// If multiple providers, try to find one where the user already exists
	if len(candidates) > 1 {
		for _, p := range candidates {
			authProviderValue := "oauth:" + p.ID
			existing, _ := s.authManager.FindUserByExternalID(r.Context(), email, authProviderValue)
			if existing != nil {
				provider = p
				break
			}
		}
	}

	s.startOAuthFlow(w, r, provider.ID, email)
}

// startOAuthFlow initiates the OAuth redirect with CSRF protection and optional login_hint
func (s *Server) startOAuthFlow(w http.ResponseWriter, r *http.Request, providerID, loginHint string) {
	stateBytes := make([]byte, 16)
	rand.Read(stateBytes)
	csrfToken := base64.URLEncoding.EncodeToString(stateBytes)

	state := providerID + ":" + csrfToken

	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    csrfToken,
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	authURL, err := s.idpManager.GetOAuthAuthURL(r.Context(), providerID, state, loginHint)
	if err != nil {
		logrus.WithError(err).Error("Failed to get OAuth auth URL")
		http.Redirect(w, r, "/login?error=provider_unavailable", http.StatusFound)
		return
	}

	logrus.WithFields(logrus.Fields{
		"provider_id": providerID,
		"login_hint":  loginHint,
		"auth_url":    authURL,
	}).Info("OAuth: redirecting to provider")

	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")

	if errorParam != "" {
		logrus.WithField("error", errorParam).Warn("OAuth callback received error")
		http.Redirect(w, r, "/login?error=oauth_denied", http.StatusFound)
		return
	}

	if code == "" || state == "" {
		http.Redirect(w, r, "/login?error=invalid_callback", http.StatusFound)
		return
	}

	parts := strings.SplitN(state, ":", 2)
	if len(parts) != 2 {
		http.Redirect(w, r, "/login?error=invalid_state", http.StatusFound)
		return
	}
	providerID := parts[0]
	csrfToken := parts[1]

	cookie, err := r.Cookie("oauth_state")
	if err != nil || cookie.Value != csrfToken {
		logrus.Warn("OAuth CSRF validation failed")
		http.Redirect(w, r, "/login?error=csrf_failed", http.StatusFound)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	externalUser, err := s.idpManager.HandleOAuthCallback(r.Context(), providerID, code)
	if err != nil {
		logrus.WithError(err).Error("OAuth code exchange failed")
		http.Redirect(w, r, "/login?error=exchange_failed", http.StatusFound)
		return
	}

	if externalUser.Email == "" {
		logrus.WithField("provider", providerID).Warn("OAuth login: no email from provider")
		http.Redirect(w, r, "/login?error=missing_email", http.StatusFound)
		return
	}

	// Step 1: Look for existing user across ALL OAuth providers (not just the one used for auth)
	user, resolvedProviderID := s.findOAuthUser(r.Context(), externalUser.Email)

	// Step 2: If no existing user, try auto-provisioning via group mappings
	if user == nil {
		var errCode string
		user, resolvedProviderID, errCode = s.tryAutoProvision(r.Context(), r, externalUser)
		if user == nil {
			http.Redirect(w, r, "/login?error="+errCode, http.StatusFound)
			return
		}
	}

	_ = resolvedProviderID

	// Ensure email field is populated for SSO users (username is email, so sync it)
	if user.Email == "" && externalUser.Email != "" {
		user.Email = externalUser.Email
		s.authManager.UpdateUser(r.Context(), user)
	}

	if user.Status != auth.UserStatusActive {
		http.Redirect(w, r, "/login?error=account_inactive", http.StatusFound)
		return
	}

	isLocked, _, _ := s.authManager.IsAccountLocked(r.Context(), user.ID)
	if isLocked {
		http.Redirect(w, r, "/login?error=account_locked", http.StatusFound)
		return
	}

	twoFactorEnabled, _, _ := s.authManager.Get2FAStatus(r.Context(), user.ID)
	if twoFactorEnabled {
		http.Redirect(w, r, fmt.Sprintf("/login?pending_2fa=true&user_id=%s", user.ID), http.StatusFound)
		return
	}

	s.authManager.RecordSuccessfulLogin(r.Context(), user.ID)

	token, err := s.authManager.GenerateJWT(r.Context(), user)
	if err != nil {
		logrus.WithError(err).Error("Failed to generate JWT after OAuth login")
		http.Redirect(w, r, "/login?error=token_failed", http.StatusFound)
		return
	}

	s.logAuditEvent(r.Context(), &audit.AuditEvent{
		TenantID:     user.TenantID,
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    audit.EventTypeLoginSuccess,
		ResourceType: audit.ResourceTypeUser,
		ResourceID:   user.ID,
		ResourceName: user.Username,
		Action:       audit.ActionLogin,
		Status:       audit.StatusSuccess,
		IPAddress:    getClientIP(r),
		UserAgent:    r.Header.Get("User-Agent"),
		Details: map[string]interface{}{
			"method":      "oauth",
			"provider_id": providerID,
		},
	})

	logrus.WithFields(logrus.Fields{
		"user_id":  user.ID,
		"username": user.Username,
		"provider": providerID,
	}).Info("Successful OAuth login")

	http.Redirect(w, r, "/auth/oauth/complete?token="+token, http.StatusFound)
}

func (s *Server) handleListOAuthProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.idpManager.ListActiveOAuthProviders(r.Context())
	if err != nil {
		s.writeJSON(w, []interface{}{})
		return
	}

	type oauthProviderInfo struct {
		Preset string `json:"preset"`
		Name   string `json:"name"`
	}

	// Deduplicate by preset — one button per provider type (google, microsoft, etc.)
	seen := make(map[string]bool)
	var result []oauthProviderInfo
	for _, p := range providers {
		preset := "custom"
		if p.Config.OAuth2 != nil && p.Config.OAuth2.Preset != "" {
			preset = p.Config.OAuth2.Preset
		}
		if seen[preset] {
			continue
		}
		seen[preset] = true
		result = append(result, oauthProviderInfo{
			Preset: preset,
			Name:   p.Name,
		})
	}

	s.writeJSON(w, result)
}

// =============================================================================
// Helper methods
// =============================================================================

func (s *Server) getAuthUser(r *http.Request) *auth.User {
	user, ok := r.Context().Value("user").(*auth.User)
	if !ok {
		return nil
	}
	return user
}

func (s *Server) isAdmin(user *auth.User) bool {
	for _, role := range user.Roles {
		if role == auth.RoleAdmin {
			return true
		}
	}
	return false
}

func (s *Server) isGlobalAdmin(user *auth.User) bool {
	return s.isAdmin(user) && user.TenantID == ""
}

// findOAuthUser searches for an existing user across ALL active OAuth providers
func (s *Server) findOAuthUser(ctx context.Context, email string) (*auth.User, string) {
	providers, err := s.idpManager.ListActiveOAuthProviders(ctx)
	if err != nil {
		return nil, ""
	}

	for _, p := range providers {
		authProviderValue := "oauth:" + p.ID
		user, err := s.authManager.FindUserByExternalID(ctx, email, authProviderValue)
		if err == nil && user != nil {
			return user, p.ID
		}
	}

	return nil, ""
}

// tryAutoProvision attempts to auto-provision an OAuth user by checking group mappings
// across ALL active OAuth providers. Returns (nil, "", errorCode) if not authorized.
func (s *Server) tryAutoProvision(ctx context.Context, r *http.Request, externalUser *idp.ExternalUser) (*auth.User, string, string) {
	// Safety check: reject if a local/LDAP user already has this email as username
	existingUser, _ := s.authManager.GetUser(ctx, externalUser.Email)
	if existingUser != nil && !strings.HasPrefix(existingUser.AuthProvider, "oauth:") {
		logrus.WithFields(logrus.Fields{
			"email":    externalUser.Email,
			"conflict": existingUser.AuthProvider,
		}).Warn("OAuth auto-provision blocked: email conflicts with existing user")
		return nil, "", "email_conflict"
	}

	// Check group mappings across ALL OAuth providers
	providers, err := s.idpManager.ListActiveOAuthProviders(ctx)
	if err != nil || len(providers) == 0 {
		return nil, "", "provider_unavailable"
	}

	for _, provider := range providers {
		mappings, _ := s.idpManager.ListGroupMappings(ctx, provider.ID)
		if len(mappings) == 0 {
			continue
		}

		role, matched := resolveRoleFromMappings(externalUser.Groups, mappings)
		if !matched {
			continue
		}

		// Found a matching provider with authorized group — provision the user
		authProviderValue := "oauth:" + provider.ID
		newUser := &auth.User{
			ID:           "user-" + uuid.New().String()[:8],
			Username:     externalUser.Email,
			Password:     "",
			DisplayName:  externalUser.DisplayName,
			Email:        externalUser.Email,
			Status:       auth.UserStatusActive,
			TenantID:     provider.TenantID,
			Roles:        []string{role},
			AuthProvider: authProviderValue,
			ExternalID:   externalUser.Email,
			CreatedAt:    time.Now().Unix(),
			UpdatedAt:    time.Now().Unix(),
		}

		if createErr := s.authManager.CreateUser(ctx, newUser); createErr != nil {
			logrus.WithError(createErr).WithField("email", externalUser.Email).Error("OAuth auto-provision: failed to create user")
			continue
		}

		s.logAuditEvent(ctx, &audit.AuditEvent{
			UserID:       newUser.ID,
			Username:     newUser.Username,
			TenantID:     provider.TenantID,
			EventType:    audit.EventTypeUserCreated,
			ResourceType: audit.ResourceTypeUser,
			ResourceID:   newUser.ID,
			ResourceName: newUser.Username,
			Action:       "auto_provision_oauth",
			Status:       audit.StatusSuccess,
			IPAddress:    getClientIP(r),
			UserAgent:    r.Header.Get("User-Agent"),
			Details: map[string]interface{}{
				"provider_id": provider.ID,
				"role":        role,
			},
		})

		logrus.WithFields(logrus.Fields{
			"user_id":  newUser.ID,
			"username": newUser.Username,
			"provider": provider.ID,
			"role":     role,
		}).Info("Auto-provisioned OAuth user")

		return newUser, provider.ID, ""
	}

	logrus.WithFields(logrus.Fields{
		"email":       externalUser.Email,
		"user_groups": externalUser.Groups,
	}).Warn("OAuth login rejected: user not authorized in any provider")
	return nil, "", "not_in_authorized_group"
}

func resolveRoleFromMappings(userGroups []string, mappings []*idp.GroupMapping) (string, bool) {
	groupSet := make(map[string]bool, len(userGroups))
	for _, g := range userGroups {
		groupSet[g] = true
	}

	bestRole := ""
	for _, m := range mappings {
		if !groupSet[m.ExternalGroup] && !groupSet[m.ExternalGroupName] {
			continue
		}
		switch m.Role {
		case auth.RoleAdmin:
			return auth.RoleAdmin, true // highest possible, return immediately
		case auth.RoleUser:
			bestRole = auth.RoleUser
		case auth.RoleReadOnly:
			if bestRole == "" {
				bestRole = auth.RoleReadOnly
			}
		}
	}

	if bestRole == "" {
		return "", false
	}
	return bestRole, true
}
