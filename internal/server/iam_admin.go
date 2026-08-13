package server

import "github.com/maxiofs/maxiofs/internal/auth"

// userHoldsPermission reports whether a user's policies grant an action
// somewhere. Used for the administration permissions, which name no bucket.
func (s *Server) userHoldsPermission(user *auth.User, action string) bool {
	if user == nil || s.authManager == nil {
		return false
	}
	resolver, ok := s.authManager.(interface {
		HasPermissionInTenant(userID string, roles []string, tenantID, action string) bool
	})
	if !ok {
		return false
	}
	return resolver.HasPermissionInTenant(user.ID, user.Roles, user.TenantID, action)
}

// userHoldsPermissionExactly reports whether an action is named outright by one
func (s *Server) userHoldsPermissionExactly(user *auth.User, action string) bool {
	if user == nil || s.authManager == nil {
		return false
	}
	resolver, ok := s.authManager.(interface {
		HasExactPermissionInTenant(userID string, roles []string, tenantID, action string) bool
	})
	if !ok {
		return false
	}
	return resolver.HasExactPermissionInTenant(user.ID, user.Roles, user.TenantID, action)
}
