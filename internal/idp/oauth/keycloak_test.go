package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realmUser is a user of fakeKeycloak's realm, as its admin API gives it.
type realmUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Email         string `json:"email"`
	FirstName     string `json:"firstName"`
	LastName      string `json:"lastName"`
	Enabled       bool   `json:"enabled"`
	EmailVerified bool   `json:"emailVerified"`
}

// fakeKeycloak is the admin API of a Keycloak realm, empresa: its users, its
// groups and their members, read with a token of the client's credentials.
// Refusing, it answers 403, as to a service account without view-users.
type fakeKeycloak struct {
	server  *httptest.Server
	mu      sync.Mutex
	users   map[string]realmUser
	members map[string][]string // group name to user IDs
	refuse  bool
}

func startKeycloak(t *testing.T) *fakeKeycloak {
	t.Helper()
	k := &fakeKeycloak{users: map[string]realmUser{}, members: map[string][]string{}}
	reply := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/realms/empresa/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		id, secret, ok := r.BasicAuth()
		if !ok {
			id, secret = r.Form.Get("client_id"), r.Form.Get("client_secret")
		}
		if r.Form.Get("grant_type") != "client_credentials" || id != "maxiofs" || secret != "secret" {
			reply(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized_client"})
			return
		}
		reply(w, http.StatusOK, map[string]any{"access_token": "admin-token", "token_type": "Bearer", "expires_in": 300})
	})
	mux.HandleFunc("/admin/realms/empresa/", func(w http.ResponseWriter, r *http.Request) {
		k.mu.Lock()
		defer k.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer admin-token" {
			reply(w, http.StatusUnauthorized, map[string]string{"error": "HTTP 401 Unauthorized"})
			return
		}
		if k.refuse {
			reply(w, http.StatusForbidden, map[string]string{"error": "HTTP 403 Forbidden"})
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/admin/realms/empresa/"), "/")
		switch {
		case len(parts) == 1 && parts[0] == "users":
			found := []realmUser{}
			for _, u := range k.users {
				if strings.EqualFold(u.Email, r.URL.Query().Get("email")) {
					found = append(found, u)
				}
			}
			reply(w, http.StatusOK, found)
		case len(parts) == 2 && parts[0] == "users":
			u, ok := k.users[parts[1]]
			if !ok {
				reply(w, http.StatusNotFound, map[string]string{"error": "User not found"})
				return
			}
			reply(w, http.StatusOK, u)
		case len(parts) == 3 && parts[0] == "users" && parts[2] == "groups":
			groups := []map[string]string{}
			for name, ids := range k.members {
				for _, id := range ids {
					if id == parts[1] {
						groups = append(groups, map[string]string{"id": "g-" + name, "name": name, "path": "/" + name})
					}
				}
			}
			reply(w, http.StatusOK, groups)
		case len(parts) == 1 && parts[0] == "groups":
			found := []map[string]any{}
			if _, ok := k.members[r.URL.Query().Get("search")]; ok {
				name := r.URL.Query().Get("search")
				found = append(found, map[string]any{"id": "g-" + name, "name": name, "path": "/" + name, "subGroups": []any{}})
			}
			reply(w, http.StatusOK, found)
		case len(parts) == 3 && parts[0] == "groups" && parts[2] == "members":
			ids := k.members[strings.TrimPrefix(parts[1], "g-")]
			first, _ := strconv.Atoi(r.URL.Query().Get("first"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("max"))
			page := []realmUser{}
			for i := first; i < len(ids) && i < first+limit; i++ {
				page = append(page, k.users[ids[i]])
			}
			reply(w, http.StatusOK, page)
		default:
			reply(w, http.StatusNotFound, map[string]string{"error": "Could not find resource"})
		}
	})
	k.server = httptest.NewServer(mux)
	t.Cleanup(k.server.Close)
	return k
}

func (k *fakeKeycloak) add(u realmUser, groups ...string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.users[u.ID] = u
	for _, g := range groups {
		k.members[g] = append(k.members[g], u.ID)
	}
}

func (k *fakeKeycloak) provider() *Provider {
	realm := k.server.URL + "/realms/empresa"
	return NewProvider(&idp.OAuth2Config{
		Preset: "custom", ClientID: "maxiofs", ClientSecret: "secret", RedirectURI: fakeRedirectURI,
		AuthURL: realm + "/protocol/openid-connect/auth", TokenURL: realm + "/protocol/openid-connect/token",
		UserInfoURL: realm + "/protocol/openid-connect/userinfo", Issuer: realm, ClaimGroups: "groups",
	})
}

func person(id, email string) realmUser {
	return realmUser{ID: id, Username: strings.Split(email, "@")[0], Email: email, FirstName: "Ana", LastName: "Ruiz",
		Enabled: true, EmailVerified: true}
}

// Only a Keycloak realm has a directory MaxIOFS reads: its issuer names the
// realm, and its admin API is beside it.
func TestOnlyAKeycloakRealmHasADirectory(t *testing.T) {
	assert.True(t, startKeycloak(t).provider().ReadsDirectory())
	for _, issuer := range []string{"", "https://accounts.google.com", "https://login.microsoftonline.com/tenant/v2.0"} {
		assert.False(t, NewProvider(&idp.OAuth2Config{Preset: "custom", Issuer: issuer}).ReadsDirectory(), issuer)
	}
}

// Without a sign-in to renew, a Keycloak user is looked up in its realm, by
// email or by subject: one gone, disabled or whose email the realm did not
// verify is no longer let in. A realm that refuses the client, or does not
// answer, ends nothing.
func TestAKeycloakUserIsLookedUpInItsRealm(t *testing.T) {
	k := startKeycloak(t)
	k.add(person("id-ana", "ana@empresa.com"), "maxiofs-users")
	disabled := person("id-beto", "beto@empresa.com")
	disabled.Enabled = false
	k.add(disabled, "maxiofs-users")
	unverified := person("id-carla", "carla@empresa.com")
	unverified.EmailVerified = false
	k.add(unverified, "maxiofs-users")
	p := k.provider()
	ctx := context.Background()

	for _, subject := range []string{"ana@empresa.com", "ANA@empresa.com", "id-ana"} {
		user, err := p.Revalidate(ctx, idp.Grant{Subject: subject})
		require.NoError(t, err, subject)
		require.NotNil(t, user, subject)
		assert.Equal(t, "ana@empresa.com", user.Email)
		assert.Equal(t, "ana@empresa.com", user.ExternalID)
		assert.Equal(t, "Ana Ruiz", user.DisplayName)
		assert.Contains(t, user.Groups, "maxiofs-users")
		assert.Contains(t, user.Groups, "/maxiofs-users")
	}
	for _, subject := range []string{"gone@empresa.com", "id-gone", "beto@empresa.com", "carla@empresa.com"} {
		_, err := p.Revalidate(ctx, idp.Grant{Subject: subject})
		assert.ErrorIs(t, err, idp.ErrSessionEnded, subject)
	}

	k.mu.Lock()
	k.refuse = true
	k.mu.Unlock()
	_, err := p.Revalidate(ctx, idp.Grant{Subject: "ana@empresa.com"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, idp.ErrSessionEnded)
	assert.NotErrorIs(t, err, idp.ErrUnavailable)
	assert.Contains(t, err.Error(), "view-users")

	k.server.Close()
	_, err = p.Revalidate(ctx, idp.Grant{Subject: "ana@empresa.com"})
	assert.ErrorIs(t, err, idp.ErrUnavailable)
}

// The members of a Keycloak group are read page by page; those disabled, or
// whose email the realm did not verify, are left out.
func TestTheMembersOfAKeycloakGroup(t *testing.T) {
	k := startKeycloak(t)
	for i := range 150 {
		k.add(person(fmt.Sprintf("id-%d", i), fmt.Sprintf("user%d@empresa.com", i)), "maxiofs-users")
	}
	disabled := person("id-off", "off@empresa.com")
	disabled.Enabled = false
	unverified := person("id-unverified", "unverified@empresa.com")
	unverified.EmailVerified = false
	k.add(disabled, "maxiofs-users")
	k.add(unverified, "maxiofs-users")

	members, err := k.provider().GetGroupMembers(context.Background(), "maxiofs-users")
	require.NoError(t, err)
	assert.Len(t, members, 150)
	for _, m := range members {
		assert.Equal(t, m.Email, m.ExternalID)
		assert.NotEqual(t, "off@empresa.com", m.Email)
		assert.NotEqual(t, "unverified@empresa.com", m.Email)
	}

	_, err = k.provider().GetGroupMembers(context.Background(), "nobody-group")
	assert.Error(t, err)
}

// A Keycloak sign-in takes its groups from the claim: reading them would need
// the client's service account, which a realm used only to sign in has not.
func TestAKeycloakSignInDoesNotReadTheRealm(t *testing.T) {
	k := startKeycloak(t)
	k.add(person("id-ana", "ana@empresa.com"), "maxiofs-users")
	cfg := *k.provider().config
	cfg.ClaimGroups = ""

	user := &idp.ExternalUser{Email: "ana@empresa.com"}
	require.NoError(t, NewProvider(&cfg).directoryGroups(context.Background(), user))
	assert.Empty(t, user.Groups)
}
