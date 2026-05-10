package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/sirupsen/logrus"
)

// handleListGroups lists all groups visible to the current user.
// Global admins see all global groups; tenant admins/users see their tenant's groups.
func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	currentUser := s.getAuthUser(r)
	if currentUser == nil {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if !s.isAdmin(currentUser) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	var groups []*auth.Group
	var err error

	if s.isGlobalAdmin(currentUser) {
		tenantFilter := r.URL.Query().Get("tenantId")
		scopeGlobal := r.URL.Query().Get("scope") == "global"
		switch {
		case scopeGlobal:
			// Explicitly requested global-only groups (e.g. for global bucket permission grants)
			groups, err = s.authManager.ListGroups(r.Context(), "")
		case tenantFilter != "":
			// Global admin filtering by a specific tenant
			groups, err = s.authManager.ListGroups(r.Context(), tenantFilter)
		default:
			// Global admin: show all groups across all tenants
			groups, err = s.authManager.ListAllGroups(r.Context())
		}
	} else {
		// Tenant admin: only their tenant's groups
		groups, err = s.authManager.ListGroups(r.Context(), currentUser.TenantID)
	}

	if err != nil {
		s.writeError(w, "Failed to list groups", http.StatusInternalServerError)
		return
	}
	if groups == nil {
		groups = []*auth.Group{}
	}
	s.writeJSON(w, map[string]interface{}{"groups": groups, "total": len(groups)})
}

// handleCreateGroup creates a new group.
func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	currentUser := s.getAuthUser(r)
	if currentUser == nil || !s.isAdmin(currentUser) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	var req struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
		TenantID    string `json:"tenantId,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		s.writeError(w, "Group name is required", http.StatusBadRequest)
		return
	}

	// Tenant admins can only create groups in their own tenant
	if !s.isGlobalAdmin(currentUser) {
		req.TenantID = currentUser.TenantID
	}

	// Check for duplicate name
	if existing, _ := s.authManager.GetGroupByName(r.Context(), req.Name, req.TenantID); existing != nil {
		s.writeError(w, "A group with that name already exists", http.StatusConflict)
		return
	}

	now := time.Now().Unix()
	group := &auth.Group{
		ID:          generateGroupID(),
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Description: req.Description,
		TenantID:    req.TenantID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.authManager.CreateGroup(r.Context(), group); err != nil {
		s.writeError(w, "Failed to create group", http.StatusInternalServerError)
		return
	}

	s.touchLocalWriteAt(r.Context())
	if s.groupSyncMgr != nil {
		s.groupSyncMgr.TriggerSync(r.Context())
	}
	w.WriteHeader(http.StatusCreated)
	s.writeJSON(w, group)
}

// handleGetGroup returns a single group by ID.
func (s *Server) handleGetGroup(w http.ResponseWriter, r *http.Request) {
	currentUser := s.getAuthUser(r)
	if currentUser == nil || !s.isAdmin(currentUser) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	groupID := mux.Vars(r)["group"]
	group, err := s.authManager.GetGroup(r.Context(), groupID)
	if err != nil {
		s.writeError(w, "Group not found", http.StatusNotFound)
		return
	}

	// Tenant admins can only see their own tenant's groups
	if !s.isGlobalAdmin(currentUser) && group.TenantID != currentUser.TenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	s.writeJSON(w, group)
}

// handleUpdateGroup updates a group's display name and description.
func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	currentUser := s.getAuthUser(r)
	if currentUser == nil || !s.isAdmin(currentUser) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	groupID := mux.Vars(r)["group"]
	group, err := s.authManager.GetGroup(r.Context(), groupID)
	if err != nil {
		s.writeError(w, "Group not found", http.StatusNotFound)
		return
	}

	if !s.isGlobalAdmin(currentUser) && group.TenantID != currentUser.TenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	var req struct {
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	group.DisplayName = req.DisplayName
	group.Description = req.Description
	group.UpdatedAt = time.Now().Unix()

	if err := s.authManager.UpdateGroup(r.Context(), group); err != nil {
		s.writeError(w, "Failed to update group", http.StatusInternalServerError)
		return
	}

	s.touchLocalWriteAt(r.Context())
	if s.groupSyncMgr != nil {
		s.groupSyncMgr.TriggerSync(r.Context())
	}
	s.writeJSON(w, group)
}

// handleDeleteGroup deletes a group and all its memberships and bucket permissions.
func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	currentUser := s.getAuthUser(r)
	if currentUser == nil || !s.isAdmin(currentUser) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	groupID := mux.Vars(r)["group"]
	group, err := s.authManager.GetGroup(r.Context(), groupID)
	if err != nil {
		s.writeError(w, "Group not found", http.StatusNotFound)
		return
	}

	if !s.isGlobalAdmin(currentUser) && group.TenantID != currentUser.TenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	if err := s.authManager.DeleteGroup(r.Context(), groupID); err != nil {
		s.writeError(w, "Failed to delete group", http.StatusInternalServerError)
		return
	}

	s.touchLocalWriteAt(r.Context())

	// Record tombstone + trigger sync so other nodes also drop the group.
	if s.clusterManager != nil && s.clusterManager.IsClusterEnabled() {
		localNodeID, _ := s.clusterManager.GetLocalNodeID(r.Context())
		if err := cluster.RecordDeletion(r.Context(), s.db, cluster.EntityTypeGroup, groupID, localNodeID); err != nil {
			logrus.WithError(err).WithField("group_id", groupID).Warn("Failed to record group deletion tombstone")
		}
	}
	if s.groupSyncMgr != nil {
		s.groupSyncMgr.TriggerSync(r.Context())
	}

	s.writeJSON(w, map[string]string{"message": "Group deleted successfully"})
}

// handleListGroupMembers returns all members of a group.
func (s *Server) handleListGroupMembers(w http.ResponseWriter, r *http.Request) {
	currentUser := s.getAuthUser(r)
	if currentUser == nil || !s.isAdmin(currentUser) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	groupID := mux.Vars(r)["group"]
	group, err := s.authManager.GetGroup(r.Context(), groupID)
	if err != nil {
		s.writeError(w, "Group not found", http.StatusNotFound)
		return
	}

	if !s.isGlobalAdmin(currentUser) && group.TenantID != currentUser.TenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	members, err := s.authManager.ListGroupMembers(r.Context(), groupID)
	if err != nil {
		s.writeError(w, "Failed to list group members", http.StatusInternalServerError)
		return
	}
	if members == nil {
		members = []*auth.GroupMember{}
	}
	s.writeJSON(w, map[string]interface{}{"members": members, "total": len(members)})
}

// handleAddGroupMember adds a user to a group.
func (s *Server) handleAddGroupMember(w http.ResponseWriter, r *http.Request) {
	currentUser := s.getAuthUser(r)
	if currentUser == nil || !s.isAdmin(currentUser) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	groupID := mux.Vars(r)["group"]
	group, err := s.authManager.GetGroup(r.Context(), groupID)
	if err != nil {
		s.writeError(w, "Group not found", http.StatusNotFound)
		return
	}

	if !s.isGlobalAdmin(currentUser) && group.TenantID != currentUser.TenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	var req struct {
		UserID string `json:"userId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		s.writeError(w, "userId is required", http.StatusBadRequest)
		return
	}

	// Verify user exists and belongs to the same scope as the group.
	targetUser, err := s.authManager.GetUser(r.Context(), req.UserID)
	if err != nil {
		s.writeError(w, "User not found", http.StatusNotFound)
		return
	}
	if targetUser.TenantID != group.TenantID {
		s.writeError(w, "User and group must belong to the same tenant scope", http.StatusForbidden)
		return
	}

	if err := s.authManager.AddGroupMember(r.Context(), groupID, req.UserID, currentUser.ID); err != nil {
		s.writeError(w, "Failed to add member", http.StatusInternalServerError)
		return
	}

	s.touchLocalWriteAt(r.Context())
	if s.groupSyncMgr != nil {
		s.groupSyncMgr.TriggerSync(r.Context())
	}
	s.writeJSON(w, map[string]string{"message": "Member added successfully"})
}

// handleRemoveGroupMember removes a user from a group.
func (s *Server) handleRemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	currentUser := s.getAuthUser(r)
	if currentUser == nil || !s.isAdmin(currentUser) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	vars := mux.Vars(r)
	groupID := vars["group"]
	userID := vars["user"]

	group, err := s.authManager.GetGroup(r.Context(), groupID)
	if err != nil {
		s.writeError(w, "Group not found", http.StatusNotFound)
		return
	}

	if !s.isGlobalAdmin(currentUser) && group.TenantID != currentUser.TenantID {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	if err := s.authManager.RemoveGroupMember(r.Context(), groupID, userID); err != nil {
		s.writeError(w, "Failed to remove member", http.StatusInternalServerError)
		return
	}

	s.touchLocalWriteAt(r.Context())
	if s.groupSyncMgr != nil {
		s.groupSyncMgr.TriggerSync(r.Context())
	}
	s.writeJSON(w, map[string]string{"message": "Member removed successfully"})
}

// handleListUserGroups returns all groups a user belongs to.
func (s *Server) handleListUserGroups(w http.ResponseWriter, r *http.Request) {
	currentUser := s.getAuthUser(r)
	if currentUser == nil {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	targetUserID := mux.Vars(r)["user"]

	// Users can see their own groups; admins can see any user's groups
	if targetUserID != currentUser.ID && !s.isAdmin(currentUser) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}

	groups, err := s.authManager.ListUserGroups(r.Context(), targetUserID)
	if err != nil {
		s.writeError(w, "Failed to list user groups", http.StatusInternalServerError)
		return
	}
	if groups == nil {
		groups = []*auth.Group{}
	}
	s.writeJSON(w, map[string]interface{}{"groups": groups, "total": len(groups)})
}

// generateGroupID re-exports from the auth package for use in the server package.
func generateGroupID() string {
	return auth.GenerateGroupID()
}
