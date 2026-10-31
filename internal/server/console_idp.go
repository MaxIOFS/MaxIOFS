package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

// oauthCodeEntry holds a one-time token pair issued by handleOAuthCallback.
// Keeping tokens out of the redirect URL prevents exposure via Referer/History (RFC 6819 §4.2.2).
type oauthCodeEntry struct {
	pair auth.TokenPair
	// twoFactorToken, when set, stands in for the session: the user still
	// gives its second factor.
	twoFactorToken string
	expiresAt      time.Time
}

// generateOAuthCode returns a cryptographically random 64-char hex string.
func generateOAuthCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Server) consoleRelativePath(path string) string {
	basePath := strings.TrimRight(extractBasePath(s.config.PublicConsoleURL), "/")
	if basePath == "" {
		return path
	}
	return basePath + path
}

func (s *Server) pruneExpiredOAuthCodes(now time.Time) {
	s.oauthCodeStore.Range(func(key, value interface{}) bool {
		entry, ok := value.(oauthCodeEntry)
		if ok && now.After(entry.expiresAt) {
			s.oauthCodeStore.Delete(key)
		}
		return true
	})
}

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
		IPAddress:    getClientIP(r, s.config.TrustedProxies),
		UserAgent:    r.Header.Get("User-Agent"),
	})

	if s.idpProviderSyncMgr != nil {
		s.idpProviderSyncMgr.TriggerSync(r.Context())
	}

	s.writeJSONWithStatus(w, http.StatusCreated, APIResponse{Success: true, Data: s.maskedProvider(r.Context(), &provider)})
}

// maskedProvider is provider as a read answers it: its secrets masked.
func (s *Server) maskedProvider(ctx context.Context, provider *idp.IdentityProvider) *idp.IdentityProvider {
	if masked, err := s.idpManager.GetProviderMasked(ctx, provider.ID); err == nil {
		return masked
	}
	copied := *provider
	copied.Config = idp.ProviderConfig{}
	return &copied
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

	if !s.isGlobalAdmin(user) && provider.TenantID != user.TenantID {
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

	if !s.isGlobalAdmin(user) && existing.TenantID != user.TenantID {
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

	if s.idpProviderSyncMgr != nil {
		s.idpProviderSyncMgr.TriggerSync(r.Context())
	}

	s.writeJSON(w, s.maskedProvider(r.Context(), &updated))
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

	if !s.isGlobalAdmin(user) && existing.TenantID != user.TenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	linkedCount, _ := s.idpManager.CountLinkedUsers(r.Context(), providerID, existing.Type)

	providerChanged := s.changedAt(r.Context(), cluster.EntityTypeIDPProvider, providerID)
	if err := s.idpManager.DeleteProvider(r.Context(), providerID); err != nil {
		logrus.WithError(err).Error("Failed to delete identity provider")
		s.writeError(w, "Failed to delete identity provider", http.StatusInternalServerError)
		return
	}

	// Record tombstone for cluster deletion sync
	if s.clusterManager != nil && s.clusterManager.IsClusterEnabled() {
		nodeID, _ := s.clusterManager.GetLocalNodeID(r.Context())
		if err := cluster.RecordDeletion(r.Context(), s.db, cluster.EntityTypeIDPProvider, providerID, nodeID, cluster.DeletedAfter(providerChanged)); err != nil {
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
		IPAddress:    getClientIP(r, s.config.TrustedProxies),
		UserAgent:    r.Header.Get("User-Agent"),
		Details: map[string]interface{}{
			"linked_users": linkedCount,
		},
	})

	if s.idpProviderSyncMgr != nil {
		s.idpProviderSyncMgr.TriggerSync(r.Context())
	}

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

	provider := s.managedProvider(w, r, user)
	if provider == nil {
		return
	}
	checks, err := s.idpManager.TestConnection(r.Context(), provider.ID)
	if err != nil {
		s.writeJSONWithStatus(w, http.StatusOK, APIResponse{
			Success: false,
			Data: map[string]interface{}{
				"status":  "error",
				"message": err.Error(),
			},
		})
		return
	}
	if failed := idp.Failed(checks); failed != nil {
		s.writeJSONWithStatus(w, http.StatusOK, APIResponse{
			Success: false,
			Data: map[string]interface{}{
				"success": false,
				"status":  "error",
				"message": failed.Name + ": " + failed.Detail,
				"checks":  checks,
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
			"checks":  checks,
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

	provider := s.managedProvider(w, r, user)
	if provider == nil {
		return
	}
	users, err := s.idpManager.SearchUsers(r.Context(), provider.ID, req.Query, req.Limit)
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

	provider := s.managedProvider(w, r, user)
	if provider == nil {
		return
	}
	groups, err := s.idpManager.SearchGroups(r.Context(), provider.ID, req.Query, req.Limit)
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

	var req struct {
		GroupID string `json:"group_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	provider := s.managedProvider(w, r, user)
	if provider == nil {
		return
	}
	members, err := s.idpManager.GetGroupMembers(r.Context(), provider.ID, req.GroupID)
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

	provider := s.managedProvider(w, r, user)
	if provider == nil {
		return
	}
	providerID := provider.ID

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

	// A tenant's provider gives users of its tenant only.
	if provider.TenantID != "" {
		req.TenantID = provider.TenantID
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

		existingByName, _ := s.authManager.GetUserByUsername(r.Context(), entry.Username)
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
			Email:        entry.Email,
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
		IPAddress:    getClientIP(r, s.config.TrustedProxies),
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

	provider := s.managedProvider(w, r, user)
	if provider == nil {
		return
	}
	mappings, err := s.idpManager.ListGroupMappings(r.Context(), provider.ID)
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

	var mapping idp.GroupMapping
	if err := json.NewDecoder(r.Body).Decode(&mapping); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if mapping.ExternalGroup == "" || mapping.Role == "" {
		s.writeError(w, "external_group and role are required", http.StatusBadRequest)
		return
	}

	provider := s.managedProvider(w, r, user)
	if provider == nil {
		return
	}
	mapping.ProviderID = provider.ID
	// A tenant's provider gives users of its tenant only.
	if provider.TenantID != "" {
		mapping.TenantID = provider.TenantID
	}

	if err := s.idpManager.CreateGroupMapping(r.Context(), &mapping); err != nil {
		logrus.WithError(err).Error("Failed to create group mapping")
		s.writeError(w, "Failed to create group mapping", http.StatusInternalServerError)
		return
	}

	if s.groupMappingSyncMgr != nil {
		s.groupMappingSyncMgr.TriggerSync(r.Context())
	}

	s.writeJSONWithStatus(w, http.StatusCreated, APIResponse{Success: true, Data: mapping})
}

func (s *Server) handleUpdateGroupMapping(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	var mapping idp.GroupMapping
	if err := json.NewDecoder(r.Body).Decode(&mapping); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	provider := s.managedProvider(w, r, user)
	if provider == nil {
		return
	}
	existing := s.providerMapping(w, r, provider)
	if existing == nil {
		return
	}
	mapping.ID = existing.ID
	mapping.ProviderID = provider.ID
	if provider.TenantID != "" {
		mapping.TenantID = provider.TenantID
	}

	if err := s.idpManager.UpdateGroupMapping(r.Context(), &mapping); err != nil {
		s.writeError(w, "Failed to update group mapping", http.StatusInternalServerError)
		return
	}

	if s.groupMappingSyncMgr != nil {
		s.groupMappingSyncMgr.TriggerSync(r.Context())
	}

	s.writeJSON(w, mapping)
}

func (s *Server) handleDeleteGroupMapping(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	provider := s.managedProvider(w, r, user)
	if provider == nil {
		return
	}
	mapping := s.providerMapping(w, r, provider)
	if mapping == nil {
		return
	}
	mappingID := mapping.ID

	mappingChanged := s.changedAt(r.Context(), cluster.EntityTypeGroupMapping, mappingID)
	if err := s.idpManager.DeleteGroupMapping(r.Context(), mappingID); err != nil {
		s.writeError(w, "Failed to delete group mapping", http.StatusInternalServerError)
		return
	}

	// Record tombstone for cluster deletion sync
	if s.clusterManager != nil && s.clusterManager.IsClusterEnabled() {
		nodeID, _ := s.clusterManager.GetLocalNodeID(r.Context())
		if err := cluster.RecordDeletion(r.Context(), s.db, cluster.EntityTypeGroupMapping, mappingID, nodeID, cluster.DeletedAfter(mappingChanged)); err != nil {
			logrus.WithError(err).WithField("mapping_id", mappingID).Warn("Failed to record group mapping deletion tombstone")
		}
	}

	if s.groupMappingSyncMgr != nil {
		s.groupMappingSyncMgr.TriggerSync(r.Context())
	}

	s.writeJSON(w, map[string]string{"message": "Group mapping deleted"})
}

func (s *Server) handleSyncGroupMapping(w http.ResponseWriter, r *http.Request) {
	user := s.getAuthUser(r)
	if user == nil || !s.isAdmin(user) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	provider := s.managedProvider(w, r, user)
	if provider == nil {
		return
	}
	mapping := s.providerMapping(w, r, provider)
	if mapping == nil {
		return
	}
	providerID, mappingID := provider.ID, mapping.ID

	members, err := s.idpManager.GetGroupMembers(r.Context(), providerID, mapping.ExternalGroup)
	if err != nil {
		s.writeError(w, fmt.Sprintf("Failed to get group members: %s", err.Error()), http.StatusBadRequest)
		return
	}

	authProviderValue := provider.Type + ":" + providerID
	// A tenant's provider gives users of its tenant only.
	targetTenant := provider.TenantID
	if targetTenant == "" {
		targetTenant = mapping.TenantID
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

		markGroupManaged(newUser)
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

	provider := s.managedProvider(w, r, user)
	if provider == nil {
		return
	}
	providerID := provider.ID

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

		members, err := s.idpManager.GetGroupMembers(r.Context(), providerID, mapping.ExternalGroup)
		if err != nil {
			totalErrors++
			continue
		}

		authProviderValue := provider.Type + ":" + providerID
		// A tenant's provider gives users of its tenant only.
		targetTenant := provider.TenantID
		if targetTenant == "" {
			targetTenant = mapping.TenantID
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

			markGroupManaged(newUser)
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
		Secure:   isHTTPS(r),
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

// handleBackchannelLogout ends the console sessions an identity provider signs
// out (OpenID Connect Back-Channel Logout 1.0): the provider posts a logout
// token that names the user or the provider's session.
// POST /api/v1/auth/oauth/{id}/backchannel-logout
func (s *Server) handleBackchannelLogout(w http.ResponseWriter, r *http.Request) {
	providerID := mux.Vars(r)["id"]
	w.Header().Set("Cache-Control", "no-store")
	refuse := func(reason string) {
		logrus.WithFields(logrus.Fields{"provider_id": providerID, "reason": reason}).Warn("Back-channel logout refused")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_request", "error_description": reason})
	}
	if s.idpManager == nil {
		refuse("identity providers are not available")
		return
	}
	if err := r.ParseForm(); err != nil {
		refuse("the body is not a form")
		return
	}
	token := r.PostForm.Get("logout_token")
	if token == "" {
		refuse("logout_token is required")
		return
	}
	subject, session, err := s.idpManager.VerifyLogoutToken(r.Context(), providerID, token)
	if err != nil {
		refuse(err.Error())
		return
	}
	ended, err := s.authManager.RevokeIdPSessions(r.Context(), providerID, subject, session, "identity provider logout")
	if err != nil {
		logrus.WithError(err).WithField("provider_id", providerID).Error("Failed to end the sessions of a back-channel logout")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	logrus.WithFields(logrus.Fields{"provider_id": providerID, "subject": subject, "session": session, "ended": ended}).
		Info("Back-channel logout: console sessions ended")
	w.WriteHeader(http.StatusOK)
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
	// Use constant-time comparison to prevent timing-based CSRF token enumeration.
	if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(csrfToken)) != 1 {
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
	if errors.Is(err, idp.ErrUnavailable) {
		logrus.WithError(err).WithField("provider_id", providerID).Warn("OAuth sign-in: provider unavailable")
		http.Redirect(w, r, s.consoleRelativePath("/login?error=provider_unavailable"), http.StatusFound)
		return
	}
	if err != nil {
		logrus.WithError(err).Error("OAuth code exchange failed")
		http.Redirect(w, r, s.consoleRelativePath("/login?error=exchange_failed"), http.StatusFound)
		return
	}

	if externalUser.Email == "" {
		logrus.WithField("provider", providerID).Warn("OAuth login: no email from provider")
		http.Redirect(w, r, s.consoleRelativePath("/login?error=missing_email"), http.StatusFound)
		return
	}

	// An email is one user's, signed in by that user's provider only.
	user, _ := s.authManager.FindUserByExternalID(r.Context(), externalUser.Email, "oauth:"+providerID)
	if user != nil {
		var allowed bool
		if user, allowed = s.applyProviderGroups(r.Context(), user, externalUser); !allowed {
			logrus.WithField("user", user.Username).Info("OAuth sign-in refused: the user is in no group mapped any more")
			http.Redirect(w, r, s.consoleRelativePath("/login?error=not_in_authorized_group"), http.StatusFound)
			return
		}
	}

	if user == nil {
		var errCode string
		user, errCode = s.tryAutoProvision(r.Context(), r, providerID, externalUser)
		if user == nil {
			http.Redirect(w, r, s.consoleRelativePath("/login?error="+url.QueryEscape(errCode)), http.StatusFound)
			return
		}
	}

	// Ensure email field is populated for SSO users (username is email, so sync it)
	if user.Email == "" && externalUser.Email != "" {
		user.Email = externalUser.Email
		s.authManager.UpdateUser(r.Context(), user)
	}

	s.finishOAuthSignIn(w, r, user, providerID, externalUser)
}

// finishOAuthSignIn signs in the user an identity provider vouched for: the
// browser is sent to the console with a single-use code it exchanges for the
// session or, when the user has two-factor authentication, for the second
// step's token.
func (s *Server) finishOAuthSignIn(w http.ResponseWriter, r *http.Request, user *auth.User, providerID string, external *idp.ExternalUser) {
	if user.Status != auth.UserStatusActive {
		http.Redirect(w, r, s.consoleRelativePath("/login?error=account_inactive"), http.StatusFound)
		return
	}

	isLocked, _, _ := s.authManager.IsAccountLocked(r.Context(), user.ID)
	if isLocked {
		http.Redirect(w, r, s.consoleRelativePath("/login?error=account_locked"), http.StatusFound)
		return
	}

	twoFactorEnabled, _, _ := s.authManager.Get2FAStatus(r.Context(), user.ID)
	if twoFactorEnabled {
		twoFactorToken, err := s.authManager.StartTwoFactorSession(r.Context(), user, s.sessionOrigin(user, external))
		if err != nil {
			logrus.WithError(err).Error("Failed to issue the two-factor token after OAuth login")
			http.Redirect(w, r, s.consoleRelativePath("/login?error=token_failed"), http.StatusFound)
			return
		}
		s.redirectWithOAuthCode(w, r, oauthCodeEntry{twoFactorToken: twoFactorToken})
		return
	}

	s.authManager.RecordSuccessfulLogin(r.Context(), user.ID)

	pair, err := s.authManager.StartSession(r.Context(), user, s.sessionOrigin(user, external))
	if err != nil {
		logrus.WithError(err).Error("Failed to generate JWT after OAuth login")
		http.Redirect(w, r, s.consoleRelativePath("/login?error=token_failed"), http.StatusFound)
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
		IPAddress:    getClientIP(r, s.config.TrustedProxies),
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

	s.redirectWithOAuthCode(w, r, oauthCodeEntry{pair: *pair})
}

// redirectWithOAuthCode stores entry under a single-use code valid for a
// minute and sends the browser to the console page that exchanges it. In a
// cluster the code names this node: the exchange may reach another one.
func (s *Server) redirectWithOAuthCode(w http.ResponseWriter, r *http.Request, entry oauthCodeEntry) {
	exchangeCode, err := generateOAuthCode()
	if err != nil {
		logrus.WithError(err).Error("Failed to generate OAuth exchange code")
		http.Redirect(w, r, s.consoleRelativePath("/login?error=token_failed"), http.StatusFound)
		return
	}
	if s.clusterManager != nil && s.clusterManager.IsClusterEnabled() {
		if nodeID, err := s.clusterManager.GetLocalNodeID(r.Context()); err == nil && nodeID != "" {
			exchangeCode = nodeID + "." + exchangeCode
		}
	}
	now := time.Now()
	s.pruneExpiredOAuthCodes(now)
	entry.expiresAt = now.Add(60 * time.Second)
	s.oauthCodeStore.Store(exchangeCode, entry)
	http.Redirect(w, r, s.consoleRelativePath("/auth/oauth/complete?code="+url.QueryEscape(exchangeCode)), http.StatusFound)
}

// handleOAuthExchangeCode exchanges a one-time code (issued by handleOAuthCallback) for JWT tokens.
// The code is consumed atomically on first use and expires after 60 seconds.
func (s *Server) handleOAuthExchangeCode(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		s.writeError(w, "missing code parameter", http.StatusBadRequest)
		return
	}
	if nodeID, _, named := strings.Cut(code, "."); named && s.clusterManager != nil && s.clusterManager.IsClusterEnabled() {
		if local, err := s.clusterManager.GetLocalNodeID(r.Context()); err == nil && nodeID != local {
			s.forwardOAuthExchange(w, r, nodeID, code)
			return
		}
	}
	s.exchangeOAuthCode(w, code)
}

// handleClusterOAuthExchange exchanges, for another node of the cluster, a
// code this node issued.
func (s *Server) handleClusterOAuthExchange(w http.ResponseWriter, r *http.Request) {
	s.exchangeOAuthCode(w, r.URL.Query().Get("code"))
}

// forwardOAuthExchange asks the node that issued code to exchange it.
func (s *Server) forwardOAuthExchange(w http.ResponseWriter, r *http.Request, nodeID, code string) {
	ctx := r.Context()
	node, err := s.clusterManager.GetNode(ctx, nodeID)
	if err != nil {
		s.writeError(w, "invalid or expired code", http.StatusUnauthorized)
		return
	}
	localNodeID, err := s.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		s.writeError(w, "Could not identify this node", http.StatusInternalServerError)
		return
	}
	nodeToken, err := s.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		s.writeError(w, "Could not authenticate to the node that completed the sign-in", http.StatusInternalServerError)
		return
	}
	forwardCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	proxy := s.clusterManager.ProxyClient()
	target := node.Endpoint + "/api/internal/cluster/oauth-exchange?code=" + url.QueryEscape(code)
	req, err := proxy.CreateAuthenticatedRequest(forwardCtx, http.MethodGet, target, nil, localNodeID, nodeToken)
	if err != nil {
		s.writeError(w, "Could not build the exchange request", http.StatusInternalServerError)
		return
	}
	resp, err := proxy.DoAuthenticatedRequest(req)
	if err != nil {
		logrus.WithError(err).WithField("node_id", nodeID).Warn("OAuth code exchange: the issuing node did not answer")
		s.writeError(w, "The node that completed the sign-in did not answer. Sign in again.", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// exchangeOAuthCode answers the session or second-step token stored under
// code, once.
func (s *Server) exchangeOAuthCode(w http.ResponseWriter, code string) {
	now := time.Now()
	s.pruneExpiredOAuthCodes(now)

	if code == "" {
		s.writeError(w, "missing code parameter", http.StatusBadRequest)
		return
	}

	raw, loaded := s.oauthCodeStore.LoadAndDelete(code)
	if !loaded {
		s.writeError(w, "invalid or expired code", http.StatusUnauthorized)
		return
	}

	entry := raw.(oauthCodeEntry)
	if now.After(entry.expiresAt) {
		s.writeError(w, "code expired", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "application/json")
	if entry.twoFactorToken != "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"requires_2fa":     true,
			"two_factor_token": entry.twoFactorToken,
		})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{
		"access_token":  entry.pair.AccessToken,
		"refresh_token": entry.pair.RefreshToken,
	})
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

func (s *Server) getAuthUser(r *http.Request) *auth.User {
	user, ok := r.Context().Value("user").(*auth.User)
	if !ok {
		return nil
	}
	return user
}

// managedProvider is the provider the request names when user manages it: a
// tenant administrator manages its own tenant's providers only. Otherwise it
// answers the request and gives nil.
func (s *Server) managedProvider(w http.ResponseWriter, r *http.Request, user *auth.User) *idp.IdentityProvider {
	provider, err := s.idpManager.GetProviderMasked(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		s.writeError(w, "Identity provider not found", http.StatusNotFound)
		return nil
	}
	if !s.isGlobalAdmin(user) && provider.TenantID != user.TenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return nil
	}
	return provider
}

// providerMapping is the group mapping the request names when it is one of
// provider's. Otherwise it answers the request and gives nil.
func (s *Server) providerMapping(w http.ResponseWriter, r *http.Request, provider *idp.IdentityProvider) *idp.GroupMapping {
	mapping, err := s.idpManager.GetGroupMapping(r.Context(), mux.Vars(r)["mapId"])
	if err != nil || mapping.ProviderID != provider.ID {
		s.writeError(w, "Group mapping not found", http.StatusNotFound)
		return nil
	}
	return mapping
}

// isAdmin reports whether a user administers anything: one tenant, or all of
// them. Held as a permission, so it can be granted to somebody who does not
// carry the admin role.
func (s *Server) isAdmin(user *auth.User) bool {
	if user == nil {
		return false
	}
	if s.userHoldsPermission(user, auth.ActionTenantAdmin) || s.isGlobalAdmin(user) {
		return true
	}
	for _, role := range user.Roles {
		if role == auth.RoleAdmin || role == auth.RoleTenantAdmin {
			return true
		}
	}
	return false
}

// isGlobalAdmin reports whether a user administers every tenant.
func (s *Server) isGlobalAdmin(user *auth.User) bool {
	if user == nil {
		return false
	}
	if user.TenantID != "" {
		return false
	}
	if s.userHoldsPermissionExactly(user, auth.ActionSuperAdmin) {
		return true
	}
	for _, role := range user.Roles {
		if role == auth.RoleAdmin {
			return true
		}
	}
	return false
}

// countGlobalAdmins returns the number of active global-admin users
// (role == "admin", TenantID == ""). Used to prevent removing the last admin.
func (s *Server) countGlobalAdmins(ctx context.Context) (int, error) {
	users, err := s.authManager.ListUsers(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, u := range users {
		if u.TenantID != "" {
			continue
		}
		for _, r := range u.Roles {
			if r == auth.RoleAdmin {
				count++
				break
			}
		}
	}
	return count, nil
}

// countTenantAdmins returns the number of active users that can administer a
// tenant. Used to keep a tenant from being left without an operator; deleting
// the tenant itself is the operation that recursively removes tenant resources.
func (s *Server) countTenantAdmins(ctx context.Context, tenantID string) (int, error) {
	users, err := s.authManager.ListUsers(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for i := range users {
		user := &users[i]
		if user.TenantID != tenantID || user.Status != auth.UserStatusActive {
			continue
		}
		if s.isAdmin(user) {
			count++
		}
	}
	return count, nil
}

// tryAutoProvision creates the user an OAuth provider signs in for the first
// time, with the role the provider's own group mappings give, in the
// provider's tenant. Without a user it gives the login page's error code.
func (s *Server) tryAutoProvision(ctx context.Context, r *http.Request, providerID string, externalUser *idp.ExternalUser) (*auth.User, string) {
	provider, err := s.idpManager.GetProviderMasked(ctx, providerID)
	if err != nil {
		return nil, "provider_unavailable"
	}
	mappings, _ := s.idpManager.ListGroupMappings(ctx, provider.ID)
	role, matched := resolveRoleFromMappings(externalUser.Groups, mappings)
	if !matched {
		logrus.WithFields(logrus.Fields{
			"email":       externalUser.Email,
			"provider":    provider.ID,
			"user_groups": externalUser.Groups,
		}).Warn("OAuth login rejected: user in no group the provider maps")
		return nil, "not_in_authorized_group"
	}

	newUser := &auth.User{
		ID:           "user-" + uuid.New().String()[:8],
		Username:     externalUser.Email,
		Password:     "",
		DisplayName:  externalUser.DisplayName,
		Email:        externalUser.Email,
		Status:       auth.UserStatusActive,
		TenantID:     provider.TenantID,
		Roles:        []string{role},
		AuthProvider: "oauth:" + provider.ID,
		ExternalID:   externalUser.Email,
		CreatedAt:    time.Now().Unix(),
		UpdatedAt:    time.Now().Unix(),
	}

	markGroupManaged(newUser)
	if err := s.authManager.CreateUser(ctx, newUser); err != nil {
		if errors.Is(err, auth.ErrEmailInUse) {
			logrus.WithFields(logrus.Fields{
				"email":    externalUser.Email,
				"provider": provider.ID,
			}).Warn("OAuth auto-provision refused: the email belongs to another user")
			return nil, "email_conflict"
		}
		logrus.WithError(err).WithField("email", externalUser.Email).Error("OAuth auto-provision: failed to create user")
		return nil, "provisioning_failed"
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
		IPAddress:    getClientIP(r, s.config.TrustedProxies),
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

	return newUser, ""
}

func resolveRoleFromMappings(userGroups []string, mappings []*idp.GroupMapping) (string, bool) {
	// Group names and DNs are compared as directories compare them: in any
	// case.
	groupSet := make(map[string]bool, len(userGroups))
	for _, g := range userGroups {
		groupSet[strings.ToLower(g)] = true
	}

	bestRole := ""
	for _, m := range mappings {
		if !groupSet[strings.ToLower(m.ExternalGroup)] && !groupSet[strings.ToLower(m.ExternalGroupName)] {
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

// isHTTPS returns true when the request was received over a TLS connection
// or when a trusted reverse-proxy forwarded it as HTTPS.
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	return false
}
