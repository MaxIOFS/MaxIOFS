package server

import (
	"cmp"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
)

// newUserRequest is what the console makes a user with.
type newUserRequest struct {
	Username     string   `json:"username"`
	Email        string   `json:"email,omitempty"`
	Password     string   `json:"password"`
	Roles        []string `json:"roles,omitempty"`
	Status       string   `json:"status,omitempty"`
	TenantID     string   `json:"tenantId,omitempty"`
	AuthProvider string   `json:"authProvider,omitempty"`
	ExternalID   string   `json:"externalId,omitempty"`
}

// external reports whether the user signs in through an identity provider.
func (req *newUserRequest) external() bool {
	return req.AuthProvider != "" && req.AuthProvider != "local"
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req newUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.validateNewUser(&req); err != nil {
		s.writeRefusal(w, err)
		return
	}
	caller, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		s.writeError(w, "User not found in context", http.StatusUnauthorized)
		return
	}
	if !s.isAdmin(caller) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}
	globalAdmin := auth.IsAdminUser(r.Context()) && caller.TenantID == ""
	if err := placeNewUser(caller, globalAdmin, &req); err != nil {
		s.writeRefusal(w, err)
		return
	}
	if s.refuseRolesBeyondCaller(w, r, req.Roles, req.TenantID) {
		return
	}
	user := newUserOf(&req)
	if err := s.authManager.CreateUser(r.Context(), user); err != nil {
		if strings.Contains(err.Error(), "already exists") || errors.Is(err, auth.ErrEmailInUse) {
			s.writeError(w, err.Error(), http.StatusConflict)
		} else {
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	s.writeJSON(w, userResponseOf(user))
}

// validateNewUser refuses a user without a name, a local user without a
// password the policy takes, and a status users are not put in.
func (s *Server) validateNewUser(req *newUserRequest) error {
	if req.Username == "" {
		return refused(http.StatusBadRequest, "Username is required")
	}
	if req.Status != "" && !slices.Contains(userStatuses, req.Status) {
		return refused(http.StatusBadRequest, "status must be active, inactive, suspended or locked")
	}
	if req.external() {
		return nil
	}
	if req.Password == "" {
		return refused(http.StatusBadRequest, "Password is required for local users")
	}
	if msg := s.validatePasswordPolicy(req.Password); msg != "" {
		return refused(http.StatusBadRequest, msg)
	}
	return nil
}

// placeNewUser puts a user made by the administrator of a tenant in that
// tenant, and refuses the administrator role from anyone but a global
// administrator. A user is active, with the read role, unless asked otherwise.
func placeNewUser(caller *auth.User, globalAdmin bool, req *newUserRequest) error {
	if !globalAdmin && caller.TenantID != "" {
		if req.TenantID != "" && req.TenantID != caller.TenantID {
			return refused(http.StatusForbidden, "Tenant admins can only create users in their own tenant")
		}
		req.TenantID = caller.TenantID
	}
	req.Status = cmp.Or(req.Status, auth.UserStatusActive)
	if len(req.Roles) == 0 {
		req.Roles = []string{"read"}
	}
	if !globalAdmin && containsAdminRole(req.Roles) {
		return refused(http.StatusForbidden, "Only a global administrator can grant the administrator role")
	}
	return nil
}

// newUserOf is the user req makes. One of an identity provider is named by
// its email, its username unless another is given.
func newUserOf(req *newUserRequest) *auth.User {
	user := &auth.User{
		ID:           newUserID(),
		Username:     req.Username,
		Password:     req.Password, // hashed by the store
		DisplayName:  req.Username,
		Email:        req.Email,
		Status:       req.Status,
		Roles:        req.Roles,
		TenantID:     req.TenantID,
		AuthProvider: req.AuthProvider,
		ExternalID:   req.ExternalID,
		CreatedAt:    time.Now().Unix(),
	}
	if req.external() {
		user.Email = cmp.Or(user.Email, user.Username)
		user.ExternalID = cmp.Or(user.ExternalID, user.Email)
	}
	return user
}
