package server

import (
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
)

// TestAdmin_TenantScopeStillSeparatesAdministrators is the escalation this
func TestAdmin_TenantScopeStillSeparatesAdministrators(t *testing.T) {
	server := getSharedServer()

	global := &auth.User{ID: "adm-global", Username: "adm-global", Roles: []string{auth.RoleAdmin}}
	scoped := &auth.User{ID: "adm-tenant", Username: "adm-tenant", Roles: []string{auth.RoleAdmin}, TenantID: "acme"}

	assert.True(t, server.isGlobalAdmin(global), "an administrator with no tenant administers everything")
	assert.False(t, server.isGlobalAdmin(scoped),
		"an administrator inside a tenant administers that tenant, not the system")

	assert.True(t, server.isAdmin(global))
	assert.True(t, server.isAdmin(scoped), "they still administer their own tenant")
}

// TestAdmin_OnlyGlobalAdminGrantsAdministration covers the rule the permission
// exists for: administering one tenant must not let someone hand out authority
// over all of them.
func TestAdmin_GrantsAdministrationDetectsBothPermissions(t *testing.T) {
	assert.True(t, grantsAdministration(&auth.UserPermissions{
		Global: []string{auth.ActionSuperAdmin},
	}))
	assert.True(t, grantsAdministration(&auth.UserPermissions{
		Global: []string{"s3:GetObject", auth.ActionTenantAdmin},
	}))
	assert.True(t, grantsAdministration(&auth.UserPermissions{
		Buckets: []auth.BucketGrant{{Bucket: "b", Actions: []string{auth.ActionSuperAdmin}}},
	}))
	assert.False(t, grantsAdministration(&auth.UserPermissions{
		Global: []string{"s3:GetObject", "console:Access"},
	}))
}

// TestAdmin_NonAdminIsNeither confirms an ordinary user is not promoted by any
// of this.
func TestAdmin_NonAdminIsNeither(t *testing.T) {
	server := getSharedServer()
	user := &auth.User{ID: "plain-user", Username: "plain-user", Roles: []string{"user"}}

	assert.False(t, server.isAdmin(user))
	assert.False(t, server.isGlobalAdmin(user))
}
