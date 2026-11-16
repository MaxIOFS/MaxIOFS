package server

import (
	"context"

	"github.com/maxiofs/maxiofs/internal/auth"
)

// adminEmails are the addresses of the active administrators an alert goes to:
// the global ones, and with tenantID also the administrators of that tenant.
// An alert about the servers themselves passes no tenant: it names what only a
// global administrator may see.
func (s *Server) adminEmails(ctx context.Context, tenantID string) ([]string, error) {
	users, err := s.authManager.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	var emails []string
	seen := map[string]bool{}
	for i := range users {
		u := &users[i]
		if u.Status != auth.UserStatusActive || u.Email == "" || seen[u.Email] {
			continue
		}
		if s.isGlobalAdmin(u) || (tenantID != "" && u.TenantID == tenantID && s.isAdmin(u)) {
			emails = append(emails, u.Email)
			seen[u.Email] = true
		}
	}
	return emails, nil
}

// seesTenant reports whether an administrator sees what concerns tenantID: a
// global one everything, the administrator of a tenant only its tenant's.
func seesTenant(user *auth.User, globalAdmin bool, tenantID string) bool {
	return globalAdmin || (user.TenantID != "" && user.TenantID == tenantID)
}
