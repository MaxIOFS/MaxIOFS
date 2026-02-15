package server

import (
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

	s.writeJSON(w, map[string]interface{}{
		"status":  "success",
		"message": "Connection successful",
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

		username := member.Username
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

			username := member.Username
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

	authURL, err := s.idpManager.GetOAuthAuthURL(r.Context(), providerID, state)
	if err != nil {
		logrus.WithError(err).Error("Failed to get OAuth auth URL")
		http.Redirect(w, r, "/login?error=provider_unavailable", http.StatusTemporaryRedirect)
		return
	}

	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	errorParam := r.URL.Query().Get("error")

	if errorParam != "" {
		logrus.WithField("error", errorParam).Warn("OAuth callback received error")
		http.Redirect(w, r, "/login?error=oauth_denied", http.StatusTemporaryRedirect)
		return
	}

	if code == "" || state == "" {
		http.Redirect(w, r, "/login?error=invalid_callback", http.StatusTemporaryRedirect)
		return
	}

	parts := strings.SplitN(state, ":", 2)
	if len(parts) != 2 {
		http.Redirect(w, r, "/login?error=invalid_state", http.StatusTemporaryRedirect)
		return
	}
	providerID := parts[0]
	csrfToken := parts[1]

	cookie, err := r.Cookie("oauth_state")
	if err != nil || cookie.Value != csrfToken {
		logrus.Warn("OAuth CSRF validation failed")
		http.Redirect(w, r, "/login?error=csrf_failed", http.StatusTemporaryRedirect)
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
		http.Redirect(w, r, "/login?error=exchange_failed", http.StatusTemporaryRedirect)
		return
	}

	authProviderValue := "oauth:" + providerID
	user, err := s.authManager.FindUserByExternalID(r.Context(), externalUser.Email, authProviderValue)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"email":    externalUser.Email,
			"provider": providerID,
		}).Warn("OAuth login: user not registered")
		http.Redirect(w, r, "/login?error=user_not_registered", http.StatusTemporaryRedirect)
		return
	}

	if user.Status != auth.UserStatusActive {
		http.Redirect(w, r, "/login?error=account_inactive", http.StatusTemporaryRedirect)
		return
	}

	isLocked, _, _ := s.authManager.IsAccountLocked(r.Context(), user.ID)
	if isLocked {
		http.Redirect(w, r, "/login?error=account_locked", http.StatusTemporaryRedirect)
		return
	}

	twoFactorEnabled, _, _ := s.authManager.Get2FAStatus(r.Context(), user.ID)
	if twoFactorEnabled {
		http.Redirect(w, r, fmt.Sprintf("/login?pending_2fa=true&user_id=%s", user.ID), http.StatusTemporaryRedirect)
		return
	}

	s.authManager.RecordSuccessfulLogin(r.Context(), user.ID)

	token, err := s.authManager.GenerateJWT(r.Context(), user)
	if err != nil {
		logrus.WithError(err).Error("Failed to generate JWT after OAuth login")
		http.Redirect(w, r, "/login?error=token_failed", http.StatusTemporaryRedirect)
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

	http.Redirect(w, r, "/auth/oauth/complete?token="+token, http.StatusTemporaryRedirect)
}

func (s *Server) handleListOAuthProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.idpManager.ListActiveOAuthProviders(r.Context())
	if err != nil {
		s.writeJSON(w, []interface{}{})
		return
	}

	type oauthProviderInfo struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Preset string `json:"preset"`
	}

	var result []oauthProviderInfo
	for _, p := range providers {
		preset := ""
		if p.Config.OAuth2 != nil {
			preset = p.Config.OAuth2.Preset
		}
		result = append(result, oauthProviderInfo{
			ID:     p.ID,
			Name:   p.Name,
			Preset: preset,
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
