package server

// Who is an administrator.
//
// This used to be read off the user's role and tenant: role "admin" with no
// tenant was a super administrator, and the same role inside a tenant was that
// tenant's administrator. Those are permissions now, so they are asked of the
// policies like everything else.

import "github.com/maxiofs/maxiofs/internal/auth"

// userHoldsPermission reports whether a user's policies grant an action
// somewhere. Used for the administration permissions, which name no bucket.
func (s *Server) userHoldsPermission(user *auth.User, action string) bool {
	if user == nil || s.authManager == nil {
		return false
	}
	resolver, ok := s.authManager.(interface {
		HasPermission(userID string, roles []string, action string) bool
	})
	if !ok {
		return false
	}
	return resolver.HasPermission(user.ID, user.Roles, action)
}

// userHoldsPermissionExactly reports whether an action is named outright by one
// of the user's policies, ignoring wildcards.
//
// Super administrator is granted deliberately or not at all: a policy that says
// "*" says what a user may do with the system's resources, not that they may
// administer every tenant in it.
func (s *Server) userHoldsPermissionExactly(user *auth.User, action string) bool {
	if user == nil || s.authManager == nil {
		return false
	}
	resolver, ok := s.authManager.(interface {
		HasExactPermission(userID string, roles []string, action string) bool
	})
	if !ok {
		return false
	}
	return resolver.HasExactPermission(user.ID, user.Roles, action)
}
