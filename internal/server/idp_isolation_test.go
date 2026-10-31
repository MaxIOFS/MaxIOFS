package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// through is n signing in through provider.
func (n *ssoNode) through(provider string) *ssoNode {
	other := *n
	other.provider = provider
	return &other
}

// addProvider adds to n an OIDC provider with an issuer of its own, which
// gives the same user as n's, of tenant and with mappings (group to role); it
// gives the provider's ID.
func (n *ssoNode) addProvider(t *testing.T, tenant string, mappings map[string]string) string {
	t.Helper()
	ctx := context.Background()
	if tenant != "" {
		require.NoError(t, n.server.authManager.CreateTenant(ctx, &auth.Tenant{ID: tenant, Name: tenant, Status: "active"}))
	}
	u := startTestIssuer(t).server.URL
	provider := &idp.IdentityProvider{Name: "Other", Type: idp.TypeOAuth2, Status: idp.StatusActive, TenantID: tenant,
		Config: idp.ProviderConfig{OAuth2: &idp.OAuth2Config{Preset: "custom", ClientID: "maxiofs", ClientSecret: "secret",
			AuthURL: u + "/auth", TokenURL: u + "/token", UserInfoURL: u + "/userinfo", RedirectURI: "http://localhost/callback",
			Scopes: []string{"openid", "email"}, ClaimGroups: "groups", Issuer: u}}}
	require.NoError(t, n.server.idpManager.CreateProvider(ctx, provider))
	for group, role := range mappings {
		require.NoError(t, n.server.idpManager.CreateGroupMapping(ctx, &idp.GroupMapping{ProviderID: provider.ID, ExternalGroup: group, Role: role}))
	}
	return provider.ID
}

// An email is one user's, signed in by that user's provider only: another
// provider that gives the email signs nobody in, by the console or by STS.
func TestAnEmailSignsInThroughItsOwnProviderOnly(t *testing.T) {
	n := newSSONode(t)
	ctx := context.Background()
	_, failure := n.signIn(t)
	require.Empty(t, failure)

	other := n.addProvider(t, "tenant-b", map[string]string{"admins": auth.RoleAdmin})
	tokens, failure := n.through(other).signIn(t)
	assert.Nil(t, tokens)
	assert.Equal(t, "email_conflict", failure)

	_, err := n.server.resolveWebIdentity(ctx, httptest.NewRequest("POST", "/", nil), other, "at")
	var denied *federationDenied
	assert.ErrorAs(t, err, &denied)

	alice, err := n.server.authManager.GetUserByUsername(ctx, "alice@example.org")
	require.NoError(t, err)
	assert.Equal(t, "oauth:"+n.provider, alice.AuthProvider)
	assert.Empty(t, alice.TenantID)
}

// A provider provisions a user with its own group mappings only.
func TestAProviderProvisionsWithItsOwnMappingsOnly(t *testing.T) {
	n := newSSONode(t)
	_, failure := n.through(n.addProvider(t, "tenant-b", nil)).signIn(t)
	assert.Equal(t, "not_in_authorized_group", failure)
	_, err := n.server.authManager.GetUserByUsername(context.Background(), "alice@example.org")
	assert.Error(t, err, "nobody is provisioned")
}

// A provider does not provision the email of a local user.
func TestALocalUsersEmailIsNotProvisioned(t *testing.T) {
	n := newSSONode(t)
	ctx := context.Background()
	require.NoError(t, n.server.authManager.CreateUser(ctx, &auth.User{ID: "alice", Username: "alice", Password: "Secret-pass-1", Email: "alice@example.org"}))
	_, failure := n.signIn(t)
	assert.Equal(t, "email_conflict", failure)
	_, err := n.server.authManager.GetUserByUsername(ctx, "alice@example.org")
	assert.Error(t, err, "nobody is provisioned")
}

// A tenant administrator reaches its own tenant's providers only, and a
// tenant's provider gives users of its tenant only.
func TestATenantAdministratorReachesItsTenantsProvidersOnly(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	for _, tenant := range []string{"tenant-iso-x", "tenant-iso-y"} {
		require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{ID: tenant, Name: tenant, Status: "active"}))
	}
	providerOf := func(tenant string) (string, string) {
		provider := &idp.IdentityProvider{Name: "LDAP " + tenant, Type: idp.TypeLDAP, TenantID: tenant,
			Config: idp.ProviderConfig{LDAP: &idp.LDAPConfig{Host: "127.0.0.1", Port: 1, BaseDN: "dc=example,dc=org", Security: "none"}}}
		require.NoError(t, server.idpManager.CreateProvider(ctx, provider))
		t.Cleanup(func() { _ = server.idpManager.DeleteProvider(ctx, provider.ID) })
		mapping := &idp.GroupMapping{ProviderID: provider.ID, ExternalGroup: "cn=staff", Role: auth.RoleUser}
		require.NoError(t, server.idpManager.CreateGroupMapping(ctx, mapping))
		return provider.ID, mapping.ID
	}
	x, xMapping := providerOf("tenant-iso-x")
	parent := t
	y, _ := providerOf("tenant-iso-y")

	call := func(handler http.HandlerFunc, tenant, provider, mapping, body string) *httptest.ResponseRecorder {
		req := createAuthenticatedRequest("POST", "/", strings.NewReader(body), tenant, "admin-"+tenant, true)
		req = mux.SetURLVars(req, map[string]string{"id": provider, "mapId": mapping})
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr
	}
	mappingBody := `{"externalGroup":"cn=other","role":"user"}`
	handlers := map[string]struct {
		handler http.HandlerFunc
		body    string
	}{
		"test connection":   {server.handleTestIDPConnection, ""},
		"search users":      {server.handleIDPSearchUsers, `{"query":"a"}`},
		"search groups":     {server.handleIDPSearchGroups, `{"query":"a"}`},
		"group members":     {server.handleIDPGroupMembers, `{"group_id":"cn=staff"}`},
		"import users":      {server.handleIDPImportUsers, `{"users":[{"external_id":"uid=iso","username":"iso-y"}]}`},
		"list mappings":     {server.handleListGroupMappings, ""},
		"create mapping":    {server.handleCreateGroupMapping, mappingBody},
		"update mapping":    {server.handleUpdateGroupMapping, mappingBody},
		"delete mapping":    {server.handleDeleteGroupMapping, ""},
		"sync mapping":      {server.handleSyncGroupMapping, ""},
		"sync all mappings": {server.handleSyncAllMappings, ""},
	}
	for name, h := range handlers {
		t.Run(name+" of another tenant's provider", func(t *testing.T) {
			assert.Equal(t, http.StatusForbidden, call(h.handler, "tenant-iso-y", x, xMapping, h.body).Code)
		})
	}
	for _, name := range []string{"update mapping", "delete mapping", "sync mapping"} {
		t.Run(name+" of another provider's mapping", func(t *testing.T) {
			h := handlers[name]
			assert.Equal(t, http.StatusNotFound, call(h.handler, "tenant-iso-y", y, xMapping, h.body).Code)
		})
	}
	stored, err := server.idpManager.GetGroupMapping(ctx, xMapping)
	require.NoError(t, err, "the mapping is still there")
	assert.Equal(t, "cn=staff", stored.ExternalGroup)
	_, err = server.authManager.GetUserByUsername(ctx, "iso-y")
	assert.Error(t, err, "nobody is imported")

	t.Run("an edited mapping of a tenant's provider maps into its tenant", func(t *testing.T) {
		rr := call(server.handleUpdateGroupMapping, "tenant-iso-x", x, xMapping, `{"externalGroup":"cn=staff","role":"user","tenantId":"tenant-iso-y"}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		edited, err := server.idpManager.GetGroupMapping(ctx, xMapping)
		require.NoError(t, err)
		assert.Equal(t, "tenant-iso-x", edited.TenantID)
	})

	t.Run("a mapping of a tenant's provider maps into its tenant", func(t *testing.T) {
		rr := call(server.handleCreateGroupMapping, "tenant-iso-x", x, "", `{"externalGroup":"cn=ops","role":"user","tenantId":"tenant-iso-y"}`)
		require.Equal(t, http.StatusCreated, rr.Code, rr.Body.String())
		var resp struct{ Data idp.GroupMapping }
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
		created, err := server.idpManager.GetGroupMapping(ctx, resp.Data.ID)
		require.NoError(t, err)
		assert.Equal(t, "tenant-iso-x", created.TenantID)
	})

	t.Run("an import from a tenant's provider goes into its tenant, with the email", func(t *testing.T) {
		rr := call(server.handleIDPImportUsers, "", x, "", `{"tenant_id":"tenant-iso-y","users":[{"external_id":"uid=iso-x,dc=example,dc=org","username":"iso-x","email":"iso-x@example.org"}]}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		imported, err := server.authManager.GetUserByUsername(ctx, "iso-x")
		require.NoError(t, err)
		parent.Cleanup(func() { _ = server.authManager.DeleteUser(ctx, imported.ID) })
		assert.Equal(t, "tenant-iso-x", imported.TenantID)
		assert.Equal(t, "iso-x@example.org", imported.Email)
	})

	t.Run("an import of an email another user has", func(t *testing.T) {
		rr := call(server.handleIDPImportUsers, "", x, "", `{"users":[{"external_id":"uid=iso-z,dc=example,dc=org","username":"iso-z","email":"ISO-X@example.org"}]}`)
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		assert.Contains(t, rr.Body.String(), "belongs to another user")
		_, err := server.authManager.GetUserByUsername(ctx, "iso-z")
		assert.Error(t, err, "nobody is imported")
	})
}

// The console answers a user given an email another user has with a conflict.
func TestTheConsoleRefusesAnEmailAnotherUserHas(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	for _, user := range []*auth.User{
		{ID: "email-owner", Username: "email-owner", Password: "Secret-pass-1", Email: "owner@example.org"},
		{ID: "email-other", Username: "email-other", Password: "Secret-pass-1"},
	} {
		require.NoError(t, server.authManager.CreateUser(ctx, user))
		t.Cleanup(func() { _ = server.authManager.DeleteUser(ctx, user.ID) })
	}

	create := createAuthenticatedRequest("POST", "/api/v1/users",
		strings.NewReader(`{"username":"email-copy","password":"Secret-pass-1","email":"Owner@example.org"}`), "", "admin-1", true)
	rr := httptest.NewRecorder()
	server.handleCreateUser(rr, create)
	assert.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), "belongs to another user")

	update := createAuthenticatedRequest("PUT", "/api/v1/users/email-other", strings.NewReader(`{"email":"owner@example.org"}`), "", "admin-1", true)
	update = mux.SetURLVars(update, map[string]string{"user": "email-other"})
	rr = httptest.NewRecorder()
	server.handleUpdateUser(rr, update)
	assert.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), "belongs to another user")
}

// memberDirectory is a provider whose every group has members.
type memberDirectory struct{ members []idp.ExternalUser }

func (d *memberDirectory) Type() string                               { return "member-directory" }
func (d *memberDirectory) TestConnection(context.Context) []idp.Check { return nil }
func (d *memberDirectory) AuthenticateUser(context.Context, string, string) (*idp.ExternalUser, error) {
	return nil, errors.New("no sign-in")
}
func (d *memberDirectory) SearchUsers(context.Context, string, int) ([]idp.ExternalUser, error) {
	return nil, nil
}
func (d *memberDirectory) SearchGroups(context.Context, string, int) ([]idp.ExternalGroup, error) {
	return nil, nil
}
func (d *memberDirectory) GetGroupMembers(context.Context, string) ([]idp.ExternalUser, error) {
	return d.members, nil
}
func (d *memberDirectory) GetAuthURL(string, string) (string, error) {
	return "", errors.New("no sign-in")
}
func (d *memberDirectory) ExchangeCode(context.Context, string) (*idp.ExternalUser, error) {
	return nil, errors.New("no sign-in")
}

// A sync of a tenant's provider gives users of its tenant only, even by a
// mapping that names another, and not an email another user has.
func TestASyncGivesUsersOfTheProvidersTenant(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	for _, tenant := range []string{"tenant-sync-x", "tenant-sync-y"} {
		require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{ID: tenant, Name: tenant, Status: "active"}))
	}
	owner := &auth.User{ID: "sync-owner", Username: "sync-owner", Password: "Secret-pass-1", Email: "taken@example.org"}
	require.NoError(t, server.authManager.CreateUser(ctx, owner))
	t.Cleanup(func() { _ = server.authManager.DeleteUser(ctx, owner.ID) })
	idp.RegisterProvider("member-directory", func(provider *idp.IdentityProvider, _ string) (idp.Provider, error) {
		name := provider.Name
		return &memberDirectory{members: []idp.ExternalUser{
			{ExternalID: "uid=" + name, Username: name, Email: name + "@example.org"},
			{ExternalID: "uid=" + name + "-copy", Username: name + "-copy", Email: "TAKEN@example.org"},
		}}, nil
	})

	for name, handler := range map[string]http.HandlerFunc{
		"sync-one": server.handleSyncGroupMapping,
		"sync-all": server.handleSyncAllMappings,
	} {
		t.Run(name, func(t *testing.T) {
			provider := &idp.IdentityProvider{Name: name, Type: "member-directory", TenantID: "tenant-sync-x"}
			require.NoError(t, server.idpManager.CreateProvider(ctx, provider))
			t.Cleanup(func() { _ = server.idpManager.DeleteProvider(ctx, provider.ID) })
			mapping := &idp.GroupMapping{ProviderID: provider.ID, ExternalGroup: "cn=staff", Role: auth.RoleUser,
				TenantID: "tenant-sync-y", AutoSync: true}
			require.NoError(t, server.idpManager.CreateGroupMapping(ctx, mapping))

			req := createAuthenticatedRequest("POST", "/", nil, "", "admin-1", true)
			req = mux.SetURLVars(req, map[string]string{"id": provider.ID, "mapId": mapping.ID})
			rr := httptest.NewRecorder()
			handler(rr, req)
			require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

			synced, err := server.authManager.GetUserByUsername(ctx, name)
			require.NoError(t, err)
			t.Cleanup(func() { _ = server.authManager.DeleteUser(ctx, synced.ID) })
			assert.Equal(t, "tenant-sync-x", synced.TenantID)
			_, err = server.authManager.GetUserByUsername(ctx, name+"-copy")
			assert.Error(t, err, "the email belongs to another user")
		})
	}
}
