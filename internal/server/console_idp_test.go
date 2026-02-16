package server

import (
	"bytes"
	"context"
	"encoding/json"
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

// =============================================================================
// resolveRoleFromMappings Tests
// =============================================================================

func TestResolveRoleFromMappings(t *testing.T) {
	t.Run("no matching groups returns false", func(t *testing.T) {
		userGroups := []string{"cn=team-a,dc=example,dc=com"}
		mappings := []*idp.GroupMapping{
			{ExternalGroup: "cn=team-b,dc=example,dc=com", Role: auth.RoleUser},
		}

		role, matched := resolveRoleFromMappings(userGroups, mappings)
		assert.False(t, matched)
		assert.Equal(t, "", role)
	})

	t.Run("matches by ExternalGroup", func(t *testing.T) {
		userGroups := []string{"cn=developers,dc=example,dc=com"}
		mappings := []*idp.GroupMapping{
			{ExternalGroup: "cn=developers,dc=example,dc=com", Role: auth.RoleUser},
		}

		role, matched := resolveRoleFromMappings(userGroups, mappings)
		assert.True(t, matched)
		assert.Equal(t, auth.RoleUser, role)
	})

	t.Run("matches by ExternalGroupName", func(t *testing.T) {
		userGroups := []string{"Engineering"}
		mappings := []*idp.GroupMapping{
			{ExternalGroupName: "Engineering", Role: auth.RoleUser},
		}

		role, matched := resolveRoleFromMappings(userGroups, mappings)
		assert.True(t, matched)
		assert.Equal(t, auth.RoleUser, role)
	})

	t.Run("admin role takes highest priority", func(t *testing.T) {
		userGroups := []string{"team-a", "team-b", "team-c"}
		mappings := []*idp.GroupMapping{
			{ExternalGroup: "team-a", Role: auth.RoleReadOnly},
			{ExternalGroup: "team-b", Role: auth.RoleAdmin},
			{ExternalGroup: "team-c", Role: auth.RoleUser},
		}

		role, matched := resolveRoleFromMappings(userGroups, mappings)
		assert.True(t, matched)
		assert.Equal(t, auth.RoleAdmin, role)
	})

	t.Run("user role beats readonly", func(t *testing.T) {
		userGroups := []string{"readers", "writers"}
		mappings := []*idp.GroupMapping{
			{ExternalGroup: "readers", Role: auth.RoleReadOnly},
			{ExternalGroup: "writers", Role: auth.RoleUser},
		}

		role, matched := resolveRoleFromMappings(userGroups, mappings)
		assert.True(t, matched)
		assert.Equal(t, auth.RoleUser, role)
	})

	t.Run("readonly role works alone", func(t *testing.T) {
		userGroups := []string{"viewers"}
		mappings := []*idp.GroupMapping{
			{ExternalGroup: "viewers", Role: auth.RoleReadOnly},
		}

		role, matched := resolveRoleFromMappings(userGroups, mappings)
		assert.True(t, matched)
		assert.Equal(t, auth.RoleReadOnly, role)
	})

	t.Run("empty user groups returns false", func(t *testing.T) {
		userGroups := []string{}
		mappings := []*idp.GroupMapping{
			{ExternalGroup: "admins", Role: auth.RoleAdmin},
		}

		role, matched := resolveRoleFromMappings(userGroups, mappings)
		assert.False(t, matched)
		assert.Equal(t, "", role)
	})

	t.Run("empty mappings returns false", func(t *testing.T) {
		userGroups := []string{"admins"}
		mappings := []*idp.GroupMapping{}

		role, matched := resolveRoleFromMappings(userGroups, mappings)
		assert.False(t, matched)
		assert.Equal(t, "", role)
	})

	t.Run("admin returns immediately without checking remaining mappings", func(t *testing.T) {
		userGroups := []string{"superadmins"}
		mappings := []*idp.GroupMapping{
			{ExternalGroup: "superadmins", Role: auth.RoleAdmin},
			{ExternalGroup: "other", Role: auth.RoleUser}, // should not matter
		}

		role, matched := resolveRoleFromMappings(userGroups, mappings)
		assert.True(t, matched)
		assert.Equal(t, auth.RoleAdmin, role)
	})

	t.Run("multiple groups multiple mappings complex scenario", func(t *testing.T) {
		userGroups := []string{"engineering", "qa-team", "all-employees"}
		mappings := []*idp.GroupMapping{
			{ExternalGroup: "hr-team", Role: auth.RoleReadOnly},
			{ExternalGroup: "all-employees", Role: auth.RoleReadOnly},
			{ExternalGroup: "engineering", Role: auth.RoleUser},
			// qa-team has no mapping
		}

		role, matched := resolveRoleFromMappings(userGroups, mappings)
		assert.True(t, matched)
		assert.Equal(t, auth.RoleUser, role)
	})
}

// =============================================================================
// IDP CRUD Handler Tests
// =============================================================================

func TestHandleListIDPs(t *testing.T) {
	server := getSharedServer()

	t.Run("rejects non-admin users", func(t *testing.T) {
		req := createAuthenticatedRequest("GET", "/api/v1/identity-providers", nil, "tenant-1", "user-1", false)
		rr := httptest.NewRecorder()
		server.handleListIDPs(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("returns list for admin", func(t *testing.T) {
		req := createAuthenticatedRequest("GET", "/api/v1/identity-providers", nil, "", "admin-1", true)
		rr := httptest.NewRecorder()
		server.handleListIDPs(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	})
}

func TestHandleCreateIDP(t *testing.T) {
	server := getSharedServer()

	t.Run("rejects non-admin users", func(t *testing.T) {
		body := `{"name":"test","type":"ldap"}`
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers", strings.NewReader(body), "tenant-1", "user-1", false)
		rr := httptest.NewRecorder()
		server.handleCreateIDP(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("rejects invalid JSON", func(t *testing.T) {
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers", strings.NewReader("invalid json"), "", "admin-1", true)
		rr := httptest.NewRecorder()
		server.handleCreateIDP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("rejects missing name", func(t *testing.T) {
		body := `{"type":"ldap"}`
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers", strings.NewReader(body), "", "admin-1", true)
		rr := httptest.NewRecorder()
		server.handleCreateIDP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("rejects missing type", func(t *testing.T) {
		body := `{"name":"test"}`
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers", strings.NewReader(body), "", "admin-1", true)
		rr := httptest.NewRecorder()
		server.handleCreateIDP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("rejects invalid type", func(t *testing.T) {
		body := `{"name":"test","type":"saml"}`
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers", strings.NewReader(body), "", "admin-1", true)
		rr := httptest.NewRecorder()
		server.handleCreateIDP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("creates LDAP provider successfully", func(t *testing.T) {
		provider := idp.IdentityProvider{
			Name: "Test LDAP IDP",
			Type: "ldap",
			Config: idp.ProviderConfig{
				LDAP: &idp.LDAPConfig{
					Host:         "ldap.test.com",
					Port:         389,
					Security:     "none",
					BindDN:       "cn=admin,dc=test,dc=com",
					BindPassword: "secret",
					BaseDN:       "dc=test,dc=com",
				},
			},
		}
		body, _ := json.Marshal(provider)
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers", bytes.NewReader(body), "", "admin-1", true)
		rr := httptest.NewRecorder()
		server.handleCreateIDP(rr, req)
		assert.Equal(t, http.StatusCreated, rr.Code)

		var resp APIResponse
		json.NewDecoder(rr.Body).Decode(&resp)
		assert.True(t, resp.Success)

		// Clean up
		if data, ok := resp.Data.(map[string]interface{}); ok {
			if id, ok := data["id"].(string); ok && id != "" {
				server.idpManager.DeleteProvider(context.Background(), id)
			}
		}
	})

	t.Run("creates OAuth2 provider successfully", func(t *testing.T) {
		provider := idp.IdentityProvider{
			Name: "Test OAuth IDP",
			Type: "oauth2",
			Config: idp.ProviderConfig{
				OAuth2: &idp.OAuth2Config{
					Preset:       "google",
					ClientID:     "test-client-id",
					ClientSecret: "test-client-secret",
					RedirectURI:  "http://localhost/callback",
				},
			},
		}
		body, _ := json.Marshal(provider)
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers", bytes.NewReader(body), "", "admin-1", true)
		rr := httptest.NewRecorder()
		server.handleCreateIDP(rr, req)
		assert.Equal(t, http.StatusCreated, rr.Code)

		// Clean up
		var resp APIResponse
		json.NewDecoder(rr.Body).Decode(&resp)
		if data, ok := resp.Data.(map[string]interface{}); ok {
			if id, ok := data["id"].(string); ok && id != "" {
				server.idpManager.DeleteProvider(context.Background(), id)
			}
		}
	})
}

func TestHandleGetIDP(t *testing.T) {
	server := getSharedServer()

	t.Run("rejects non-admin users", func(t *testing.T) {
		req := createAuthenticatedRequest("GET", "/api/v1/identity-providers/nonexistent", nil, "tenant-1", "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"id": "nonexistent"})
		rr := httptest.NewRecorder()
		server.handleGetIDP(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("returns 404 for nonexistent provider", func(t *testing.T) {
		req := createAuthenticatedRequest("GET", "/api/v1/identity-providers/nonexistent", nil, "", "admin-1", true)
		req = mux.SetURLVars(req, map[string]string{"id": "nonexistent"})
		rr := httptest.NewRecorder()
		server.handleGetIDP(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestHandleDeleteIDP(t *testing.T) {
	server := getSharedServer()

	t.Run("rejects non-admin users", func(t *testing.T) {
		req := createAuthenticatedRequest("DELETE", "/api/v1/identity-providers/test", nil, "tenant-1", "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"id": "test"})
		rr := httptest.NewRecorder()
		server.handleDeleteIDP(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("returns 404 for nonexistent provider", func(t *testing.T) {
		req := createAuthenticatedRequest("DELETE", "/api/v1/identity-providers/nonexistent", nil, "", "admin-1", true)
		req = mux.SetURLVars(req, map[string]string{"id": "nonexistent"})
		rr := httptest.NewRecorder()
		server.handleDeleteIDP(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestHandleUpdateIDP(t *testing.T) {
	server := getSharedServer()

	t.Run("rejects non-admin users", func(t *testing.T) {
		body := `{"name":"updated"}`
		req := createAuthenticatedRequest("PUT", "/api/v1/identity-providers/test", strings.NewReader(body), "tenant-1", "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"id": "test"})
		rr := httptest.NewRecorder()
		server.handleUpdateIDP(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("returns 404 for nonexistent provider", func(t *testing.T) {
		body := `{"name":"updated"}`
		req := createAuthenticatedRequest("PUT", "/api/v1/identity-providers/nonexistent", strings.NewReader(body), "", "admin-1", true)
		req = mux.SetURLVars(req, map[string]string{"id": "nonexistent"})
		rr := httptest.NewRecorder()
		server.handleUpdateIDP(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestHandleTestIDPConnection(t *testing.T) {
	server := getSharedServer()

	t.Run("rejects non-admin users", func(t *testing.T) {
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers/test/test", nil, "tenant-1", "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"id": "test"})
		rr := httptest.NewRecorder()
		server.handleTestIDPConnection(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})
}

// =============================================================================
// Group Mapping Handler Tests
// =============================================================================

func TestHandleListGroupMappings(t *testing.T) {
	server := getSharedServer()

	t.Run("rejects non-admin users", func(t *testing.T) {
		req := createAuthenticatedRequest("GET", "/api/v1/identity-providers/test/group-mappings", nil, "tenant-1", "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"id": "test"})
		rr := httptest.NewRecorder()
		server.handleListGroupMappings(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("returns list for admin", func(t *testing.T) {
		req := createAuthenticatedRequest("GET", "/api/v1/identity-providers/test/group-mappings", nil, "", "admin-1", true)
		req = mux.SetURLVars(req, map[string]string{"id": "test"})
		rr := httptest.NewRecorder()
		server.handleListGroupMappings(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	})
}

func TestHandleCreateGroupMapping(t *testing.T) {
	server := getSharedServer()

	t.Run("rejects non-admin users", func(t *testing.T) {
		body := `{"externalGroup":"admins","role":"admin"}`
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers/test/group-mappings", strings.NewReader(body), "tenant-1", "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"id": "test"})
		rr := httptest.NewRecorder()
		server.handleCreateGroupMapping(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("rejects missing external_group", func(t *testing.T) {
		body := `{"role":"admin"}`
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers/test/group-mappings", strings.NewReader(body), "", "admin-1", true)
		req = mux.SetURLVars(req, map[string]string{"id": "test"})
		rr := httptest.NewRecorder()
		server.handleCreateGroupMapping(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("rejects missing role", func(t *testing.T) {
		body := `{"externalGroup":"admins"}`
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers/test/group-mappings", strings.NewReader(body), "", "admin-1", true)
		req = mux.SetURLVars(req, map[string]string{"id": "test"})
		rr := httptest.NewRecorder()
		server.handleCreateGroupMapping(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})
}

func TestHandleDeleteGroupMapping(t *testing.T) {
	server := getSharedServer()

	t.Run("rejects non-admin users", func(t *testing.T) {
		req := createAuthenticatedRequest("DELETE", "/api/v1/identity-providers/test/group-mappings/map1", nil, "tenant-1", "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"id": "test", "mapId": "map1"})
		rr := httptest.NewRecorder()
		server.handleDeleteGroupMapping(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})
}

// =============================================================================
// LDAP Browse Handler Tests
// =============================================================================

func TestHandleIDPSearchUsers(t *testing.T) {
	server := getSharedServer()

	t.Run("rejects non-admin users", func(t *testing.T) {
		body := `{"query":"john","limit":10}`
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers/test/search/users", strings.NewReader(body), "tenant-1", "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"id": "test"})
		rr := httptest.NewRecorder()
		server.handleIDPSearchUsers(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("rejects invalid JSON", func(t *testing.T) {
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers/test/search/users", strings.NewReader("invalid"), "", "admin-1", true)
		req = mux.SetURLVars(req, map[string]string{"id": "test"})
		rr := httptest.NewRecorder()
		server.handleIDPSearchUsers(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})
}

func TestHandleIDPSearchGroups(t *testing.T) {
	server := getSharedServer()

	t.Run("rejects non-admin users", func(t *testing.T) {
		body := `{"query":"admins","limit":10}`
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers/test/search/groups", strings.NewReader(body), "tenant-1", "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"id": "test"})
		rr := httptest.NewRecorder()
		server.handleIDPSearchGroups(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})
}

func TestHandleIDPGroupMembers(t *testing.T) {
	server := getSharedServer()

	t.Run("rejects non-admin users", func(t *testing.T) {
		body := `{"group_id":"cn=admins,dc=example,dc=com"}`
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers/test/group-members", strings.NewReader(body), "tenant-1", "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"id": "test"})
		rr := httptest.NewRecorder()
		server.handleIDPGroupMembers(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})
}

func TestHandleIDPImportUsers(t *testing.T) {
	server := getSharedServer()

	t.Run("rejects non-admin users", func(t *testing.T) {
		body := `{"users":[{"external_id":"test","username":"test"}],"role":"user"}`
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers/test/import-users", strings.NewReader(body), "tenant-1", "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"id": "test"})
		rr := httptest.NewRecorder()
		server.handleIDPImportUsers(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("rejects empty users list", func(t *testing.T) {
		// First create a provider so it doesn't 404
		provider := idp.IdentityProvider{
			Name: "Import Test LDAP",
			Type: "ldap",
			Config: idp.ProviderConfig{
				LDAP: &idp.LDAPConfig{
					Host:     "ldap.test.com",
					Port:     389,
					BaseDN:   "dc=test,dc=com",
					BindDN:   "cn=admin,dc=test,dc=com",
					Security: "none",
				},
			},
		}
		err := server.idpManager.CreateProvider(context.Background(), &provider)
		require.NoError(t, err)
		defer server.idpManager.DeleteProvider(context.Background(), provider.ID)

		body := `{"users":[],"role":"user"}`
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers/"+provider.ID+"/import-users", strings.NewReader(body), "", "admin-1", true)
		req = mux.SetURLVars(req, map[string]string{"id": provider.ID})
		rr := httptest.NewRecorder()
		server.handleIDPImportUsers(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("returns 404 for nonexistent provider", func(t *testing.T) {
		body := `{"users":[{"external_id":"test","username":"test"}],"role":"user"}`
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers/nonexistent/import-users", strings.NewReader(body), "", "admin-1", true)
		req = mux.SetURLVars(req, map[string]string{"id": "nonexistent"})
		rr := httptest.NewRecorder()
		server.handleIDPImportUsers(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

// =============================================================================
// OAuth Flow Handler Tests
// =============================================================================

func TestHandleListOAuthProviders(t *testing.T) {
	server := getSharedServer()

	t.Run("returns empty list when no providers", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/auth/oauth/providers", nil)
		rr := httptest.NewRecorder()
		server.handleListOAuthProviders(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("deduplicates by preset", func(t *testing.T) {
		// Create two Google OAuth providers
		p1 := idp.IdentityProvider{
			Name:   "Google 1",
			Type:   "oauth2",
			Status: idp.StatusActive,
			Config: idp.ProviderConfig{
				OAuth2: &idp.OAuth2Config{
					Preset:       "google",
					ClientID:     "client1",
					ClientSecret: "secret1",
					RedirectURI:  "http://localhost/callback",
				},
			},
		}
		p2 := idp.IdentityProvider{
			Name:   "Google 2",
			Type:   "oauth2",
			Status: idp.StatusActive,
			Config: idp.ProviderConfig{
				OAuth2: &idp.OAuth2Config{
					Preset:       "google",
					ClientID:     "client2",
					ClientSecret: "secret2",
					RedirectURI:  "http://localhost/callback",
				},
			},
		}
		p3 := idp.IdentityProvider{
			Name:   "Microsoft 1",
			Type:   "oauth2",
			Status: idp.StatusActive,
			Config: idp.ProviderConfig{
				OAuth2: &idp.OAuth2Config{
					Preset:       "microsoft",
					ClientID:     "ms-client",
					ClientSecret: "ms-secret",
					RedirectURI:  "http://localhost/callback",
				},
			},
		}

		ctx := context.Background()
		err := server.idpManager.CreateProvider(ctx, &p1)
		require.NoError(t, err)
		defer server.idpManager.DeleteProvider(ctx, p1.ID)

		err = server.idpManager.CreateProvider(ctx, &p2)
		require.NoError(t, err)
		defer server.idpManager.DeleteProvider(ctx, p2.ID)

		err = server.idpManager.CreateProvider(ctx, &p3)
		require.NoError(t, err)
		defer server.idpManager.DeleteProvider(ctx, p3.ID)

		// Activate providers
		p1.Status = idp.StatusActive
		server.idpManager.UpdateProvider(ctx, &p1)
		p2.Status = idp.StatusActive
		server.idpManager.UpdateProvider(ctx, &p2)
		p3.Status = idp.StatusActive
		server.idpManager.UpdateProvider(ctx, &p3)

		req := httptest.NewRequest("GET", "/api/v1/auth/oauth/providers", nil)
		rr := httptest.NewRecorder()
		server.handleListOAuthProviders(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)

		var resp APIResponse
		json.NewDecoder(rr.Body).Decode(&resp)

		// Should have at most 2 (google deduplicated to 1, microsoft = 1)
		if providers, ok := resp.Data.([]interface{}); ok {
			presets := make(map[string]bool)
			for _, p := range providers {
				if pm, ok := p.(map[string]interface{}); ok {
					if preset, ok := pm["preset"].(string); ok {
						presets[preset] = true
					}
				}
			}
			// Google should appear only once even though we created 2
			googleCount := 0
			for _, p := range providers {
				if pm, ok := p.(map[string]interface{}); ok {
					if pm["preset"] == "google" {
						googleCount++
					}
				}
			}
			assert.LessOrEqual(t, googleCount, 1, "Google preset should be deduplicated")
		}
	})
}

func TestHandleOAuthCallback_ErrorParam(t *testing.T) {
	server := getSharedServer()

	t.Run("redirects to login with error when error param present", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/auth/oauth/callback?error=access_denied", nil)
		rr := httptest.NewRecorder()
		server.handleOAuthCallback(rr, req)
		assert.Equal(t, http.StatusFound, rr.Code)
		assert.Contains(t, rr.Header().Get("Location"), "error=oauth_denied")
	})

	t.Run("redirects with error when code missing", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/auth/oauth/callback?state=test:token", nil)
		rr := httptest.NewRecorder()
		server.handleOAuthCallback(rr, req)
		assert.Equal(t, http.StatusFound, rr.Code)
		assert.Contains(t, rr.Header().Get("Location"), "error=invalid_callback")
	})

	t.Run("redirects with error when state missing", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/auth/oauth/callback?code=abc", nil)
		rr := httptest.NewRecorder()
		server.handleOAuthCallback(rr, req)
		assert.Equal(t, http.StatusFound, rr.Code)
		assert.Contains(t, rr.Header().Get("Location"), "error=invalid_callback")
	})

	t.Run("redirects with error on invalid state format", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/auth/oauth/callback?code=abc&state=invalidstate", nil)
		rr := httptest.NewRecorder()
		server.handleOAuthCallback(rr, req)
		assert.Equal(t, http.StatusFound, rr.Code)
		assert.Contains(t, rr.Header().Get("Location"), "error=invalid_state")
	})

	t.Run("redirects with error on CSRF mismatch", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/auth/oauth/callback?code=abc&state=provider-id:csrf-token", nil)
		// No cookie set = CSRF mismatch
		rr := httptest.NewRecorder()
		server.handleOAuthCallback(rr, req)
		assert.Equal(t, http.StatusFound, rr.Code)
		assert.Contains(t, rr.Header().Get("Location"), "error=csrf_failed")
	})

	t.Run("CSRF validation succeeds with matching cookie", func(t *testing.T) {
		csrfToken := "test-csrf-123"
		req := httptest.NewRequest("GET", "/api/v1/auth/oauth/callback?code=abc&state=nonexistent-provider:"+csrfToken, nil)
		req.AddCookie(&http.Cookie{Name: "oauth_state", Value: csrfToken})
		rr := httptest.NewRecorder()
		server.handleOAuthCallback(rr, req)
		// Should get past CSRF and fail on code exchange (provider doesn't exist)
		assert.Equal(t, http.StatusFound, rr.Code)
		location := rr.Header().Get("Location")
		assert.NotContains(t, location, "error=csrf_failed")
	})
}

func TestHandleOAuthStart(t *testing.T) {
	server := getSharedServer()

	t.Run("redirects with error on empty email", func(t *testing.T) {
		body := `{"email":"","preset":"google"}`
		req := httptest.NewRequest("POST", "/api/v1/auth/oauth/start", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		server.handleOAuthStart(rr, req)
		assert.Equal(t, http.StatusFound, rr.Code)
		assert.Contains(t, rr.Header().Get("Location"), "error=invalid_request")
	})

	t.Run("redirects with error on empty preset", func(t *testing.T) {
		body := `{"email":"user@test.com","preset":""}`
		req := httptest.NewRequest("POST", "/api/v1/auth/oauth/start", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		server.handleOAuthStart(rr, req)
		assert.Equal(t, http.StatusFound, rr.Code)
		assert.Contains(t, rr.Header().Get("Location"), "error=invalid_request")
	})

	t.Run("redirects with error for unavailable provider", func(t *testing.T) {
		body := `{"email":"user@test.com","preset":"nonexistent"}`
		req := httptest.NewRequest("POST", "/api/v1/auth/oauth/start", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		server.handleOAuthStart(rr, req)
		assert.Equal(t, http.StatusFound, rr.Code)
		assert.Contains(t, rr.Header().Get("Location"), "error=provider_unavailable")
	})

	t.Run("handles form data", func(t *testing.T) {
		body := "email=user@test.com&preset=nonexistent"
		req := httptest.NewRequest("POST", "/api/v1/auth/oauth/start", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		server.handleOAuthStart(rr, req)
		assert.Equal(t, http.StatusFound, rr.Code)
		assert.Contains(t, rr.Header().Get("Location"), "error=provider_unavailable")
	})
}

// =============================================================================
// Sync Handler Tests
// =============================================================================

func TestHandleSyncGroupMapping(t *testing.T) {
	server := getSharedServer()

	t.Run("rejects non-admin users", func(t *testing.T) {
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers/test/group-mappings/map1/sync", nil, "tenant-1", "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"id": "test", "mapId": "map1"})
		rr := httptest.NewRecorder()
		server.handleSyncGroupMapping(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})

	t.Run("returns 404 for nonexistent mapping", func(t *testing.T) {
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers/test/group-mappings/nonexistent/sync", nil, "", "admin-1", true)
		req = mux.SetURLVars(req, map[string]string{"id": "test", "mapId": "nonexistent"})
		rr := httptest.NewRecorder()
		server.handleSyncGroupMapping(rr, req)
		assert.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestHandleSyncAllMappings(t *testing.T) {
	server := getSharedServer()

	t.Run("rejects non-admin users", func(t *testing.T) {
		req := createAuthenticatedRequest("POST", "/api/v1/identity-providers/test/sync-all", nil, "tenant-1", "user-1", false)
		req = mux.SetURLVars(req, map[string]string{"id": "test"})
		rr := httptest.NewRecorder()
		server.handleSyncAllMappings(rr, req)
		assert.Equal(t, http.StatusForbidden, rr.Code)
	})
}

// =============================================================================
// Helper function tests
// =============================================================================

func TestGetAuthUser(t *testing.T) {
	server := getSharedServer()

	t.Run("returns nil when no user in context", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		user := server.getAuthUser(req)
		assert.Nil(t, user)
	})

	t.Run("returns user from context", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		testUser := &auth.User{ID: "test-user", Username: "testuser"}
		ctx := context.WithValue(req.Context(), "user", testUser)
		req = req.WithContext(ctx)

		user := server.getAuthUser(req)
		require.NotNil(t, user)
		assert.Equal(t, "test-user", user.ID)
	})
}

func TestIsAdmin(t *testing.T) {
	server := getSharedServer()

	t.Run("returns false for non-admin user", func(t *testing.T) {
		user := &auth.User{Roles: []string{"user"}}
		assert.False(t, server.isAdmin(user))
	})

	t.Run("returns true for admin user", func(t *testing.T) {
		user := &auth.User{Roles: []string{"admin"}}
		assert.True(t, server.isAdmin(user))
	})

	t.Run("returns true when admin is one of many roles", func(t *testing.T) {
		user := &auth.User{Roles: []string{"user", "admin", "readonly"}}
		assert.True(t, server.isAdmin(user))
	})
}

func TestIsGlobalAdmin(t *testing.T) {
	server := getSharedServer()

	t.Run("returns true for admin with no tenant", func(t *testing.T) {
		user := &auth.User{Roles: []string{"admin"}, TenantID: ""}
		assert.True(t, server.isGlobalAdmin(user))
	})

	t.Run("returns false for admin with tenant", func(t *testing.T) {
		user := &auth.User{Roles: []string{"admin"}, TenantID: "tenant-1"}
		assert.False(t, server.isGlobalAdmin(user))
	})

	t.Run("returns false for non-admin without tenant", func(t *testing.T) {
		user := &auth.User{Roles: []string{"user"}, TenantID: ""}
		assert.False(t, server.isGlobalAdmin(user))
	})
}
