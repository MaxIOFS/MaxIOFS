package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memberDirectory is a directory for the server's tests: its members, by DN,
// each with its groups. One that is down answers nothing.
type memberDirectory struct {
	mu      sync.Mutex
	members map[string]idp.ExternalUser
	down    bool
}

// memberDirectories are the directories of the providers of type
// member-directory, by provider name.
var memberDirectories sync.Map

func init() {
	idp.RegisterProvider("member-directory", func(provider *idp.IdentityProvider, _ string) (idp.Provider, error) {
		return directoryOf(provider.Name), nil
	})
}

// directoryOf is the directory of the member-directory provider named name.
func directoryOf(name string) *memberDirectory {
	d, _ := memberDirectories.LoadOrStore(name, &memberDirectory{members: map[string]idp.ExternalUser{}})
	return d.(*memberDirectory)
}

func (d *memberDirectory) set(members ...idp.ExternalUser) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, m := range members {
		d.members[m.ExternalID] = m
	}
}

func (d *memberDirectory) remove(dn string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.members, dn)
}

func (d *memberDirectory) setDown(down bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.down = down
}

func (d *memberDirectory) Type() string                               { return "member-directory" }
func (d *memberDirectory) ReadsDirectory() bool                       { return true }
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
func (d *memberDirectory) GetGroupMembers(_ context.Context, group string) ([]idp.ExternalUser, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.down {
		return nil, fmt.Errorf("%w: down", idp.ErrUnavailable)
	}
	var members []idp.ExternalUser
	for _, m := range d.members {
		if slices.Contains(m.Groups, group) {
			members = append(members, m)
		}
	}
	return members, nil
}
func (d *memberDirectory) Revalidate(_ context.Context, grant idp.Grant) (*idp.ExternalUser, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.down {
		return nil, fmt.Errorf("%w: down", idp.ErrUnavailable)
	}
	m, ok := d.members[grant.Subject]
	if !ok {
		return nil, fmt.Errorf("%w: %s is gone", idp.ErrSessionEnded, grant.Subject)
	}
	return &m, nil
}
func (d *memberDirectory) GetAuthURL(string, string) (string, error) {
	return "", errors.New("no sign-in")
}
func (d *memberDirectory) ExchangeCode(context.Context, string) (*idp.ExternalUser, error) {
	return nil, errors.New("no sign-in")
}

// directoryProvider makes an active member-directory provider named name with
// mappings, and gives it.
func directoryProvider(t *testing.T, s *Server, name string, mappings ...*idp.GroupMapping) *idp.IdentityProvider {
	t.Helper()
	ctx := context.Background()
	provider := &idp.IdentityProvider{Name: name, Type: "member-directory", Status: idp.StatusActive}
	require.NoError(t, s.idpManager.CreateProvider(ctx, provider))
	t.Cleanup(func() { _ = s.idpManager.DeleteProvider(ctx, provider.ID) })
	for _, m := range mappings {
		m.ProviderID = provider.ID
		require.NoError(t, s.idpManager.CreateGroupMapping(ctx, m))
	}
	return provider
}

// userNamed is the user named username; it fails the test when there is none.
func userNamed(t *testing.T, s *Server, username string) *auth.User {
	t.Helper()
	user, err := s.authManager.GetUserByUsername(context.Background(), username)
	require.NoError(t, err, username)
	return user
}

// The directory sync reads, for the users that do not sign in, what a sign-in
// would: it makes the members of the mappings set to sync, gives the role their
// groups map to, deactivates the users the directory no longer lets in, and
// activates them again once it does. A user an administrator deactivated stays
// so, and a directory that does not answer changes nobody.
func TestTheDirectorySyncFollowsTheDirectory(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	member := func(uid string, groups ...string) idp.ExternalUser {
		return idp.ExternalUser{ExternalID: "uid=" + uid + ",ou=people", Username: uid, Email: uid + "@follow.example.org",
			DisplayName: uid, Groups: groups}
	}
	dir := directoryOf("follow")
	dir.set(member("ds-ana", "cn=staff"), member("ds-beto", "cn=staff"), member("ds-carla", "cn=admins"),
		member("ds-dario"), member("ds-fede", "cn=staff"))
	provider := directoryProvider(t, server, "follow",
		&idp.GroupMapping{ExternalGroup: "cn=staff", Role: auth.RoleUser, AutoSync: true},
		&idp.GroupMapping{ExternalGroup: "cn=admins", Role: auth.RoleAdmin})
	bound := provider.Type + ":" + provider.ID
	t.Cleanup(func() {
		for _, name := range []string{"ds-ana", "ds-beto", "ds-carla", "ds-dario", "ds-eva", "ds-fede"} {
			if u, err := server.authManager.GetUserByUsername(ctx, name); err == nil {
				_ = server.authManager.DeleteUser(ctx, u.ID)
			}
		}
	})
	// An administrator imported dario, and deactivated fede.
	require.NoError(t, server.authManager.CreateUser(ctx, &auth.User{ID: "ds-dario", Username: "ds-dario", Roles: []string{auth.RoleUser},
		AuthProvider: bound, ExternalID: "uid=ds-dario,ou=people"}))
	fede := &auth.User{ID: "ds-fede", Username: "ds-fede", Roles: []string{auth.RoleUser}, Status: auth.UserStatusInactive,
		AuthProvider: bound, ExternalID: "uid=ds-fede,ou=people"}
	markGroupManaged(fede)
	require.NoError(t, server.authManager.CreateUser(ctx, fede))

	req := createAuthenticatedRequest("POST", "/", nil, "", "admin-1", true)
	rr := httptest.NewRecorder()
	server.handleSyncAllMappings(rr, mux.SetURLVars(req, map[string]string{"id": provider.ID}))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var first struct{ Data idp.SyncResult }
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &first))
	assert.Equal(t, idp.SyncResult{Imported: 2}, first.Data, "ana and beto; carla's mapping is not set to sync")
	assert.Equal(t, []string{auth.RoleUser}, userNamed(t, server, "ds-ana").Roles)
	_, err := server.authManager.GetUserByUsername(ctx, "ds-carla")
	assert.Error(t, err)
	key, err := server.authManager.GenerateAccessKey(ctx, userNamed(t, server, "ds-beto").ID)
	require.NoError(t, err)

	t.Run("the directory changes", func(t *testing.T) {
		dir.remove("uid=ds-beto,ou=people")
		dir.remove("uid=ds-dario,ou=people")
		dir.set(member("ds-ana", "cn=admins"), member("ds-eva", "cn=staff"))
		server.syncDirectories(ctx)

		assert.Equal(t, auth.UserStatusInactive, userNamed(t, server, "ds-beto").Status, "gone from the directory")
		assert.Equal(t, auth.UserStatusInactive, userNamed(t, server, "ds-dario").Status, "gone, though not following groups")
		assert.Equal(t, []string{auth.RoleAdmin}, userNamed(t, server, "ds-ana").Roles)
		assert.Equal(t, auth.UserStatusActive, userNamed(t, server, "ds-eva").Status)
		_, err := server.authManager.ValidateCredentials(ctx, key.AccessKeyID, key.SecretAccessKey)
		assert.Error(t, err, "a deactivated user's access key is refused")
	})

	t.Run("a user back in the directory", func(t *testing.T) {
		dir.set(member("ds-beto", "cn=staff"))
		server.syncDirectories(ctx)
		assert.Equal(t, auth.UserStatusActive, userNamed(t, server, "ds-beto").Status)
		assert.Equal(t, auth.UserStatusInactive, userNamed(t, server, "ds-fede").Status, "an administrator deactivated it")
	})

	t.Run("a user in no group mapped", func(t *testing.T) {
		dir.set(member("ds-eva"))
		server.syncDirectories(ctx)
		assert.Equal(t, auth.UserStatusInactive, userNamed(t, server, "ds-eva").Status)
	})

	t.Run("a directory that does not answer", func(t *testing.T) {
		dir.remove("uid=ds-ana,ou=people")
		dir.setDown(true)
		server.syncDirectories(ctx)
		assert.Equal(t, auth.UserStatusActive, userNamed(t, server, "ds-ana").Status)
	})
}

// The sync reads the directories of the active providers with a mapping set to
// sync only.
func TestTheDirectorySyncReadsTheProvidersSetToSync(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	unsynced := directoryProvider(t, server, "unsynced", &idp.GroupMapping{ExternalGroup: "cn=staff", Role: auth.RoleUser})
	inactive := directoryProvider(t, server, "inactive", &idp.GroupMapping{ExternalGroup: "cn=staff", Role: auth.RoleUser, AutoSync: true})
	inactive.Status = idp.StatusInactive
	require.NoError(t, server.idpManager.UpdateProvider(ctx, inactive))
	for _, provider := range []*idp.IdentityProvider{unsynced, inactive} {
		user := &auth.User{ID: "ds-of-" + provider.Name, Username: "ds-of-" + provider.Name, Roles: []string{auth.RoleUser},
			AuthProvider: provider.Type + ":" + provider.ID, ExternalID: "uid=gone"}
		require.NoError(t, server.authManager.CreateUser(ctx, user))
		t.Cleanup(func() { _ = server.authManager.DeleteUser(ctx, user.ID) })
	}

	server.syncDirectories(ctx)

	assert.Equal(t, auth.UserStatusActive, userNamed(t, server, "ds-of-unsynced").Status)
	assert.Equal(t, auth.UserStatusActive, userNamed(t, server, "ds-of-inactive").Status)
}

// An OAuth provider other than a Keycloak realm has no directory to read: Sync
// All says so, and asks nothing about its users.
func TestASyncOfAnOAuthProviderWithoutADirectory(t *testing.T) {
	n := newSSONode(t)
	_, failure := n.signIn(t)
	require.Empty(t, failure)

	req := createAuthenticatedRequest("POST", "/", nil, "", "admin-1", true)
	rr := httptest.NewRecorder()
	n.server.handleSyncAllMappings(rr, mux.SetURLVars(req, map[string]string{"id": n.provider}))
	assert.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), "no directory")

	alice, err := n.server.authManager.GetUserByUsername(context.Background(), "alice@example.org")
	require.NoError(t, err)
	assert.Equal(t, auth.UserStatusActive, alice.Status)
}

// realm is a Keycloak realm's admin API for the server's tests: its users by
// email, with whether each is enabled, and the members of one group.
type realm struct {
	server  *httptest.Server
	mu      sync.Mutex
	enabled map[string]bool // email to enabled
	group   string
}

func startRealm(t *testing.T, group string, emails ...string) *realm {
	t.Helper()
	k := &realm{enabled: map[string]bool{}, group: group}
	for _, e := range emails {
		k.enabled[e] = true
	}
	user := func(email string, enabled bool) map[string]any {
		return map[string]any{"id": "id-" + email, "email": email, "firstName": "Ana", "enabled": enabled, "emailVerified": true}
	}
	reply := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	routes := http.NewServeMux()
	routes.HandleFunc("/realms/empresa/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"access_token": "admin", "token_type": "Bearer", "expires_in": 300})
	})
	routes.HandleFunc("/admin/realms/empresa/", func(w http.ResponseWriter, r *http.Request) {
		k.mu.Lock()
		defer k.mu.Unlock()
		path := strings.TrimPrefix(r.URL.Path, "/admin/realms/empresa/")
		switch {
		case path == "users":
			found := []any{}
			if enabled, ok := k.enabled[r.URL.Query().Get("email")]; ok {
				found = append(found, user(r.URL.Query().Get("email"), enabled))
			}
			reply(w, found)
		case strings.HasSuffix(path, "/groups") && strings.HasPrefix(path, "users/"):
			reply(w, []any{map[string]string{"id": "g", "name": k.group, "path": "/" + k.group}})
		case path == "groups":
			reply(w, []any{map[string]any{"id": "g", "name": k.group, "path": "/" + k.group}})
		case path == "groups/g/members":
			members := []any{}
			if r.URL.Query().Get("first") == "0" {
				for e, enabled := range k.enabled {
					members = append(members, user(e, enabled))
				}
			}
			reply(w, members)
		default:
			http.NotFound(w, r)
		}
	})
	k.server = httptest.NewServer(routes)
	t.Cleanup(k.server.Close)
	return k
}

// Auto Sync reads a Keycloak realm as it reads an LDAP directory: the members
// of a mapping set to sync get a user of the provider, signed in through it,
// and a user the realm disabled, or no longer has, is deactivated.
func TestAutoSyncReadsAKeycloakRealm(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	k := startRealm(t, "maxiofs-users", "ks-ana@empresa.com", "ks-beto@empresa.com")
	issuer := k.server.URL + "/realms/empresa"
	provider := &idp.IdentityProvider{Name: "Keycloak", Type: idp.TypeOAuth2, Status: idp.StatusActive,
		Config: idp.ProviderConfig{OAuth2: &idp.OAuth2Config{Preset: "custom", ClientID: "maxiofs", ClientSecret: "secret",
			AuthURL: issuer + "/protocol/openid-connect/auth", TokenURL: issuer + "/protocol/openid-connect/token",
			UserInfoURL: issuer + "/protocol/openid-connect/userinfo", Issuer: issuer, ClaimGroups: "groups"}}}
	require.NoError(t, server.idpManager.CreateProvider(ctx, provider))
	t.Cleanup(func() { _ = server.idpManager.DeleteProvider(ctx, provider.ID) })
	require.NoError(t, server.idpManager.CreateGroupMapping(ctx, &idp.GroupMapping{ProviderID: provider.ID,
		ExternalGroup: "maxiofs-users", Role: auth.RoleUser, AutoSync: true}))
	gone := &auth.User{ID: "ks-gone", Username: "ks-gone@empresa.com", Email: "ks-gone@empresa.com", Roles: []string{auth.RoleUser},
		AuthProvider: "oauth:" + provider.ID, ExternalID: "ks-gone@empresa.com"}
	require.NoError(t, server.authManager.CreateUser(ctx, gone))
	t.Cleanup(func() {
		for _, name := range []string{"ks-ana@empresa.com", "ks-beto@empresa.com", "ks-gone@empresa.com"} {
			if u, err := server.authManager.GetUserByUsername(ctx, name); err == nil {
				_ = server.authManager.DeleteUser(ctx, u.ID)
			}
		}
	})

	server.syncDirectories(ctx)
	ana := userNamed(t, server, "ks-ana@empresa.com")
	assert.Equal(t, "oauth:"+provider.ID, ana.AuthProvider, "signed in through the provider")
	assert.Equal(t, "ks-ana@empresa.com", ana.ExternalID)
	assert.Equal(t, auth.UserStatusActive, ana.Status)
	assert.Equal(t, auth.UserStatusInactive, userNamed(t, server, "ks-gone@empresa.com").Status, "gone from the realm")

	k.mu.Lock()
	k.enabled["ks-beto@empresa.com"] = false
	k.mu.Unlock()
	server.syncDirectories(ctx)
	assert.Equal(t, auth.UserStatusInactive, userNamed(t, server, "ks-beto@empresa.com").Status, "disabled in the realm")
	assert.Equal(t, auth.UserStatusActive, userNamed(t, server, "ks-ana@empresa.com").Status)
}
