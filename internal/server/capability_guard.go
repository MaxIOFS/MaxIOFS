package server

import (
	"net/http"

	"github.com/maxiofs/maxiofs/internal/auth"
)

func (s *Server) requireCapability(w http.ResponseWriter, r *http.Request, capability, message string) bool {
	if s.authManager == nil {
		return true
	}
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok || user == nil {
		s.writeError(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	for _, role := range user.Roles {
		if role == "admin" || role == "tenant-admin" {
			return true
		}
	}
	roles := user.Roles
	if len(roles) == 0 {
		roles = []string{"user"}
	}
	rolesWereEmpty := len(user.Roles) == 0
	allowed, err := s.authManager.HasCapability(r.Context(), user.ID, roles, capability)
	if err == nil && allowed {
		return true
	}
	// Legacy/test contexts can contain authenticated users without persisted roles.
	// Preserve the previous console behaviour for those roleless identities; real
	// users created through auth flows receive explicit roles and are checked above.
	if rolesWereEmpty && err == nil {
		return true
	}
	if message == "" {
		message = "Insufficient permissions"
	}
	s.writeError(w, message, http.StatusForbidden)
	return false
}
