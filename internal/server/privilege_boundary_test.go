package server

// The boundary between a tenant administrator and a global one.
//
// The role name is identical inside a tenant and outside it, so the name has
// never been what separates them — the tenant is. Every check that reads the
// role without also reading the tenant erases the distinction, and two of them
// together were enough to walk from one to the other: mint an administrator
// inside your own tenant, then reset the global administrator's password with
// it.

import (
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
)

// TestContainsAdminRole_CoversBothNames: administration is not scoped by which
// of the two names is used, so handing out either is handing out
// administration.
func TestContainsAdminRole_CoversBothNames(t *testing.T) {
	assert.True(t, containsAdminRole([]string{auth.RoleAdmin}))
	assert.True(t, containsAdminRole([]string{auth.RoleTenantAdmin}))
	assert.True(t, containsAdminRole([]string{"user", "read", auth.RoleAdmin}))

	assert.False(t, containsAdminRole([]string{"user"}))
	assert.False(t, containsAdminRole([]string{"read", "readonly", "guest"}))
	assert.False(t, containsAdminRole(nil))
}

// TestIsGlobalAdmin_RequiresNoTenant pins the distinction the password handler
// was missing. A user carrying the admin role inside a tenant administers that
// tenant; only one with no tenant administers the system.
func TestIsGlobalAdmin_RequiresNoTenant(t *testing.T) {
	s := getSharedServer()

	global := &auth.User{ID: "pb-global", Username: "pb-global",
		Roles: []string{auth.RoleAdmin}, TenantID: ""}
	scoped := &auth.User{ID: "pb-scoped", Username: "pb-scoped",
		Roles: []string{auth.RoleAdmin}, TenantID: "pb-tenant"}

	assert.True(t, s.isGlobalAdmin(global))
	assert.False(t, s.isGlobalAdmin(scoped),
		"the same role inside a tenant does not administer the system")

	// Both administer something, which is why the raw role check passed for the
	// tenant one and let it through.
	assert.True(t, s.isAdmin(global))
	assert.True(t, s.isAdmin(scoped))
}
