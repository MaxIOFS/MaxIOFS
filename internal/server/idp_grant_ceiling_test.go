package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A tenant administrator gives users through an identity provider what it may
// give by hand, and no more: an import or a group mapping with an
// administrator role, or with a role granting what it does not hold, is
// refused. A mapping it edits keeps the role it had; a global administrator
// gives any role.
func TestAnIdentityProviderGivesNoMoreThanTheTenantAdministratorHolds(t *testing.T) {
	s := newClusterTestNode(t)
	require.NotNil(t, s.idpManager)
	ctx := context.Background()
	require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: "acme", Name: "acme", Status: "active"}))
	im := s.authManager.(auth.IAMManager)
	role := func(name, action string) {
		_, err := im.CreateIAMRole(ctx, name, "/", "", "", 0, "acme")
		require.NoError(t, err)
		require.NoError(t, im.PutIAMInlinePolicy(ctx, auth.IAMTargetRole, name, "grant",
			`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["`+action+`"],"Resource":["arn:aws:s3:::*/*"]}]}`))
	}
	role("acme-reader", auth.ActionGetObject)
	role("acme-writer", auth.ActionPutObject)
	userHolding(t, s, "limited-admin", "acme", auth.ActionTenantAdmin, auth.ActionConsoleAccess, auth.ActionGetObject)
	now := time.Now().Unix()
	require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: "global-admin", Username: "global-admin",
		Status: auth.UserStatusActive, Roles: []string{auth.RoleAdmin}, CreatedAt: now, UpdatedAt: now}))

	provider := idp.IdentityProvider{Name: "acme-ldap", Type: idp.TypeLDAP, TenantID: "acme", Status: "active",
		Config: idp.ProviderConfig{LDAP: &idp.LDAPConfig{Host: "ldap.acme.test", Port: 389, BaseDN: "dc=acme,dc=test", Security: "none"}}}
	require.NoError(t, s.idpManager.CreateProvider(ctx, &provider))
	given := idp.GroupMapping{ProviderID: provider.ID, ExternalGroup: "cn=ops", Role: auth.RoleAdmin, TenantID: "acme"}
	require.NoError(t, s.idpManager.CreateGroupMapping(ctx, &given), "as a global administrator gave it")

	ofProvider := map[string]string{"id": provider.ID}
	importAs := func(caller, role string) int {
		return callAs(t, s, caller, s.handleIDPImportUsers, ofProvider,
			`{"users":[{"external_id":"uid=`+role+`","username":"imported-`+role+`-by-`+caller+`"}],"role":"`+role+`"}`)
	}
	mapAs := func(role string) int {
		return callAs(t, s, "limited-admin", s.handleCreateGroupMapping, ofProvider, `{"externalGroup":"cn=`+role+`","role":"`+role+`"}`)
	}

	for _, r := range []string{auth.RoleAdmin, auth.RoleTenantAdmin, "acme-writer"} {
		assert.Equal(t, http.StatusForbidden, importAs("limited-admin", r), "an import with "+r)
		assert.Equal(t, http.StatusForbidden, mapAs(r), "a mapping to "+r)
	}
	assert.NotEqual(t, http.StatusForbidden, importAs("limited-admin", "acme-reader"), "an import within what it holds")
	assert.Equal(t, http.StatusCreated, mapAs("acme-reader"), "a mapping within what it holds")

	users, err := s.authManager.ListUsers(ctx)
	require.NoError(t, err)
	for _, u := range users {
		assert.NotContains(t, []string{"imported-admin-by-limited-admin", "imported-tenant-admin-by-limited-admin",
			"imported-acme-writer-by-limited-admin"}, u.Username, "a refused import created no user")
	}

	ofMapping := map[string]string{"id": provider.ID, "mapId": given.ID}
	assert.NotEqual(t, http.StatusForbidden, callAs(t, s, "limited-admin", s.handleUpdateGroupMapping, ofMapping,
		`{"externalGroup":"cn=ops","role":"admin","autoSync":true}`), "a mapping keeps the role it had")
	assert.Equal(t, http.StatusForbidden, callAs(t, s, "limited-admin", s.handleUpdateGroupMapping, ofMapping,
		`{"externalGroup":"cn=ops","role":"tenant-admin"}`), "a mapping changed to a role beyond")

	assert.NotEqual(t, http.StatusForbidden, importAs("global-admin", auth.RoleAdmin), "a global administrator")

	// An administrator of the tenant by the admin role holds what a user is
	// given, and gives it.
	require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: "acme-admin", Username: "acme-admin", TenantID: "acme",
		Status: auth.UserStatusActive, Roles: []string{auth.RoleAdmin}, CreatedAt: now, UpdatedAt: now}))
	assert.Equal(t, http.StatusCreated, callAs(t, s, "acme-admin", s.handleCreateGroupMapping, ofProvider,
		`{"externalGroup":"cn=staff","role":"user"}`), "the admin role of a tenant maps to user")
	assert.NotEqual(t, http.StatusForbidden, importAs("acme-admin", auth.RoleUser), "the admin role of a tenant imports users")
	assert.Equal(t, http.StatusForbidden, importAs("acme-admin", auth.RoleAdmin), "but not administrators")
}
