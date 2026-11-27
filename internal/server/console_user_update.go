package server

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
)

// userUpdate is what the console changes of a user.
type userUpdate struct {
	Email              *string  `json:"email,omitempty"`
	Roles              []string `json:"roles,omitempty"`
	Status             string   `json:"status,omitempty"`
	TenantID           *string  `json:"tenantId,omitempty"`
	MustChangePassword *bool    `json:"mustChangePassword,omitempty"`
	ManagedByGroups    *bool    `json:"managedByGroups,omitempty"`
}

// privileged reports whether the update changes more of a user than its
// email.
func (u *userUpdate) privileged() bool {
	return u.Roles != nil || u.Status != "" || u.TenantID != nil || u.MustChangePassword != nil || u.ManagedByGroups != nil
}

// userStatuses are the states a user is put in.
var userStatuses = []string{auth.UserStatusActive, auth.UserStatusInactive, auth.UserStatusSuspended, auth.UserStatusLocked}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	caller := s.getAuthUser(r)
	id := mux.Vars(r)["user"]
	administers := s.isAdmin(caller)
	if !administers && (caller == nil || caller.ID != id) {
		s.writeError(w, "Access denied", http.StatusForbidden)
		return
	}
	var update userUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	// A user changes its own email and nothing else of itself.
	if !administers && update.privileged() {
		s.writeError(w, "Insufficient permissions to change roles, status, or tenant", http.StatusForbidden)
		return
	}
	user, err := s.authManager.GetUser(r.Context(), id)
	if errors.Is(err, auth.ErrUserNotFound) {
		s.writeError(w, "User not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	updated, err := s.updatedUser(caller, administers, user, &update)
	if err == nil && update.Roles != nil && s.refuseRoleGrant(w, r, caller, user, update.Roles) {
		return
	}
	if err == nil {
		err = s.keepsAnAdministrator(r.Context(), user, updated)
	}
	if err == nil {
		err = s.authManager.UpdateUser(r.Context(), updated)
		if errors.Is(err, auth.ErrEmailInUse) {
			err = refused(http.StatusConflict, err.Error())
		}
	}
	if err != nil {
		s.writeRefusal(w, err)
		return
	}
	s.writeJSON(w, userResponseOf(updated))
}

// updatedUser is user as update leaves it, when the caller may change it so:
// the administrator of a tenant changes the users of its tenant, and only a
// global administrator moves a user to another tenant.
func (s *Server) updatedUser(caller *auth.User, administers bool, user *auth.User, update *userUpdate) (*auth.User, error) {
	globalAdmin := s.isGlobalAdmin(caller)
	if administers && !globalAdmin && user.TenantID != caller.TenantID {
		return nil, refused(http.StatusForbidden, "Access denied")
	}
	if update.TenantID != nil && !globalAdmin {
		return nil, refused(http.StatusForbidden, "Only global admins can change tenant assignment")
	}
	if update.Status != "" && !slices.Contains(userStatuses, update.Status) {
		return nil, refused(http.StatusBadRequest, "status must be active, inactive, suspended or locked")
	}
	updated := *user
	updated.Metadata = maps.Clone(user.Metadata)
	if update.Email != nil {
		updated.Email = *update.Email
	}
	if update.Roles != nil {
		updated.Roles = update.Roles
	}
	if update.Status != "" {
		updated.Status = update.Status
	}
	if update.TenantID != nil {
		updated.TenantID = *update.TenantID
	}
	if err := setPasswordObligation(&updated, update.MustChangePassword); err != nil {
		return nil, err
	}
	return &updated, setGroupManagement(&updated, update.ManagedByGroups)
}

// setPasswordObligation imposes the obligation to change the password. An
// administrator does not lift it by hand: only setting a password clears it.
func setPasswordObligation(user *auth.User, must *bool) error {
	if must == nil {
		return nil
	}
	if !*must && user.MustChangePassword {
		return refused(http.StatusBadRequest,
			"The obligation to change the password is lifted by setting a new password, not by clearing the flag")
	}
	user.MustChangePassword = *must
	return nil
}

// setGroupManagement makes a user of an identity provider follow its groups,
// or stop following them.
func setGroupManagement(user *auth.User, managed *bool) error {
	if managed == nil {
		return nil
	}
	if method, _ := signInMethod(user); method == auth.SessionMethodPassword {
		return refused(http.StatusBadRequest, "Only a user of an identity provider follows its groups")
	}
	if *managed {
		markGroupManaged(user)
	} else {
		delete(user.Metadata, provisionedByKey)
	}
	return nil
}

// refuseRoleGrant refuses roles the caller may not give user: the
// administrator role, but by a global administrator, and roles granting more
// than the caller holds.
func (s *Server) refuseRoleGrant(w http.ResponseWriter, r *http.Request, caller, user *auth.User, roles []string) bool {
	if !s.isGlobalAdmin(caller) && containsAdminRole(roles) && !containsAdminRole(user.Roles) {
		s.writeError(w, "Only a global administrator can grant the administrator role", http.StatusForbidden)
		return true
	}
	return s.refuseRolesBeyondCaller(w, r, newRoles(roles, user.Roles), user.TenantID)
}

// keepsAnAdministrator refuses a change that leaves the deployment without an
// active global administrator by the admin role, or a tenant without an
// active administrator, as deleting the last of either is refused.
func (s *Server) keepsAnAdministrator(ctx context.Context, before, after *auth.User) error {
	if activeGlobalAdminByRole(before) && !activeGlobalAdminByRole(after) {
		another, err := s.anotherGlobalAdminByRole(ctx, before.ID)
		if err != nil {
			return errors.New("Failed to verify admin count")
		}
		if !another {
			return refused(http.StatusConflict,
				"Cannot remove the last global admin with the admin role. Give the role to another user first.")
		}
	}
	if s.activeTenantAdmin(before) && !(s.activeTenantAdmin(after) && after.TenantID == before.TenantID) {
		admins, err := s.countTenantAdmins(ctx, before.TenantID)
		if err != nil {
			return errors.New("Failed to verify tenant admin count")
		}
		if admins <= 1 {
			return refused(http.StatusConflict,
				"Cannot remove the last tenant administrator. Assign another tenant administrator first.")
		}
	}
	return nil
}

func activeGlobalAdminByRole(user *auth.User) bool {
	return isGlobalAdminByRole(user) && user.Status == auth.UserStatusActive
}

func (s *Server) activeTenantAdmin(user *auth.User) bool {
	return user.TenantID != "" && user.Status == auth.UserStatusActive && s.isAdmin(user)
}

// newUserID is the ID of a user made: never one a user deleted had, so
// nothing the deleted user left, a console session first, is the new one's.
func newUserID() string {
	return "user-" + uuid.NewString()
}

// userResponseOf is a user as the console shows it.
func userResponseOf(user *auth.User) UserResponse {
	return UserResponse{
		ID:                  user.ID,
		Username:            user.Username,
		DisplayName:         user.DisplayName,
		Email:               user.Email,
		Roles:               user.Roles,
		Status:              user.Status,
		TenantID:            user.TenantID,
		TwoFactorEnabled:    user.TwoFactorEnabled,
		LockedUntil:         user.LockedUntil,
		FailedLoginAttempts: user.FailedLoginAttempts,
		LastFailedLogin:     user.LastFailedLogin,
		ThemePreference:     user.ThemePreference,
		LanguagePreference:  user.LanguagePreference,
		MustChangePassword:  user.MustChangePassword,
		AuthProvider:        user.AuthProvider,
		ExternalID:          user.ExternalID,
		ManagedByGroups:     groupManaged(user),
		CreatedAt:           user.CreatedAt,
	}
}
