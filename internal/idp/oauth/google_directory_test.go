package oauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const workspaceAccount = "maxiofs@project.iam.gserviceaccount.com"

// workspaceMember is a member of a group of fakeWorkspace, as the Directory API
// gives it.
type workspaceMember struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

// fakeWorkspace is a Google Workspace: its token endpoint, which takes the
// service account's signed JWT and a user's sign-in, its userinfo, and its
// Directory API. A user's key may be its email or its ID. Refusing, it answers
// 403, as to a service account without an administrator role.
type fakeWorkspace struct {
	server    *httptest.Server
	key       string
	mu        sync.Mutex
	users     map[string]googleUser
	groups    map[string][]workspaceMember // group email to members
	refuse    bool
	down      bool
	tokens    atomic.Int32
	userInfo  map[string]any
	lastLimit int
	// badKey refuses the service account, as Google does a key it revoked.
	badKey bool
}

func startWorkspace(t *testing.T) *fakeWorkspace {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)
	key, err := json.Marshal(map[string]string{
		"type": "service_account", "client_email": workspaceAccount, "private_key_id": "key-1",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
	})
	require.NoError(t, err)

	w := &fakeWorkspace{key: string(key), users: map[string]googleUser{}, groups: map[string][]workspaceMember{}}
	reply := func(rw http.ResponseWriter, status int, v any) {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(status)
		_ = json.NewEncoder(rw).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(rw http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "urn:ietf:params:oauth:grant-type:jwt-bearer":
			claims := jwt.MapClaims{}
			_, err := jwt.ParseWithClaims(r.Form.Get("assertion"), claims, func(*jwt.Token) (any, error) { return &private.PublicKey, nil })
			scopes := strings.Fields(fmt.Sprint(claims["scope"]))
			w.mu.Lock()
			bad := w.badKey
			w.mu.Unlock()
			if bad || err != nil || claims["iss"] != workspaceAccount || claims["sub"] != nil || !slices.Equal(scopes, googleDirectoryScopes) {
				reply(rw, http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": "Invalid JWT Signature."})
				return
			}
			w.tokens.Add(1)
			reply(rw, http.StatusOK, map[string]any{"access_token": "directory-token", "token_type": "Bearer", "expires_in": 3600})
		case "authorization_code", "refresh_token":
			reply(rw, http.StatusOK, map[string]any{"access_token": "user-token", "token_type": "Bearer", "refresh_token": "refresh", "expires_in": 3600})
		default:
			reply(rw, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
		}
	})
	mux.HandleFunc("/userinfo", func(rw http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		defer w.mu.Unlock()
		reply(rw, http.StatusOK, w.userInfo)
	})
	mux.HandleFunc("/admin/directory/v1/", func(rw http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		defer w.mu.Unlock()
		switch {
		case r.Header.Get("Authorization") != "Bearer directory-token":
			reply(rw, http.StatusUnauthorized, map[string]string{"error": "no token"})
			return
		case w.down:
			reply(rw, http.StatusServiceUnavailable, map[string]string{"error": "down"})
			return
		case w.refuse:
			reply(rw, http.StatusForbidden, map[string]string{"error": "Not Authorized to access this resource/api"})
			return
		}
		query := r.URL.Query()
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/admin/directory/v1/"), "/")
		switch {
		case len(parts) == 1 && parts[0] == "users" && query.Get("customer") == "my_customer":
			reply(rw, http.StatusOK, map[string]any{"users": []googleUser{}})
		case len(parts) == 2 && parts[0] == "users":
			if user, ok := w.user(parts[1]); ok {
				reply(rw, http.StatusOK, user)
				return
			}
			reply(rw, http.StatusNotFound, map[string]string{"error": "Resource Not Found: userKey"})
		case len(parts) == 1 && parts[0] == "groups" && query.Get("userKey") != "":
			groups := []map[string]string{}
			for email, members := range w.groups {
				if slices.ContainsFunc(members, func(m workspaceMember) bool { return m.ID == query.Get("userKey") }) {
					groups = append(groups, map[string]string{"id": "id-" + email, "email": email, "name": "Staff"})
				}
			}
			reply(rw, http.StatusOK, map[string]any{"groups": groups})
		case len(parts) == 3 && parts[0] == "groups" && parts[2] == "members":
			members, ok := w.groups[parts[1]]
			if !ok {
				reply(rw, http.StatusNotFound, map[string]string{"error": "Resource Not Found: groupKey"})
				return
			}
			limit, _ := strconv.Atoi(query.Get("maxResults"))
			w.lastLimit = limit
			from, _ := strconv.Atoi(query.Get("pageToken"))
			to := min(from+limit, len(members))
			page := map[string]any{"members": members[from:to]}
			if to < len(members) {
				page["nextPageToken"] = strconv.Itoa(to)
			}
			reply(rw, http.StatusOK, page)
		default:
			reply(rw, http.StatusBadRequest, map[string]string{"error": "Bad Request"})
		}
	})
	w.server = httptest.NewServer(mux)
	t.Cleanup(w.server.Close)

	former := googleDirectoryAPI
	googleDirectoryAPI = w.server.URL + "/admin/directory/v1"
	t.Cleanup(func() { googleDirectoryAPI = former })
	return w
}

// user is the user of key, its email or its ID.
func (w *fakeWorkspace) user(key string) (googleUser, bool) {
	for _, u := range w.users {
		if u.ID == key || strings.EqualFold(u.PrimaryEmail, key) {
			return u, true
		}
	}
	return googleUser{}, false
}

func (w *fakeWorkspace) add(id, email string, groups ...string) *fakeWorkspace {
	w.mu.Lock()
	defer w.mu.Unlock()
	user := googleUser{ID: id, PrimaryEmail: email}
	user.Name.FullName = "Ana Ruiz"
	w.users[id] = user
	for _, g := range groups {
		w.groups[g] = append(w.groups[g], workspaceMember{ID: id, Email: email, Type: "USER", Status: "ACTIVE"})
	}
	return w
}

func (w *fakeWorkspace) set(change func(*fakeWorkspace)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	change(w)
}

func (w *fakeWorkspace) config() *idp.OAuth2Config {
	u := w.server.URL
	return &idp.OAuth2Config{
		Preset: "google", ClientID: "maxiofs", ClientSecret: "secret", RedirectURI: fakeRedirectURI, DirectoryKey: w.key,
		AuthURL: u + "/auth", TokenURL: u + "/token", UserInfoURL: u + "/userinfo",
	}
}

func (w *fakeWorkspace) provider() *Provider { return NewProvider(w.config()) }

// A Google provider has a directory with the service account's key, a
// Microsoft one with a tenant in its issuer, and a Keycloak one with a realm.
func TestAProviderHasADirectoryOnceItIsNamed(t *testing.T) {
	for name, tc := range map[string]struct {
		config idp.OAuth2Config
		want   bool
	}{
		"google without a key":        {idp.OAuth2Config{Preset: "google"}, false},
		"google with a key":           {idp.OAuth2Config{Preset: "google", DirectoryKey: "{}"}, true},
		"microsoft without an issuer": {idp.OAuth2Config{Preset: "microsoft"}, false},
		"microsoft of any tenant": {idp.OAuth2Config{Preset: "microsoft",
			Issuer: "https://login.microsoftonline.com/common/v2.0"}, false},
		"microsoft of organizations": {idp.OAuth2Config{Preset: "microsoft",
			Issuer: "https://login.microsoftonline.com/organizations/v2.0"}, false},
		"microsoft of a tenant": {idp.OAuth2Config{Preset: "microsoft",
			Issuer: "https://login.microsoftonline.com/6f1c-tenant/v2.0"}, true},
		"keycloak":                  {idp.OAuth2Config{Preset: "custom", Issuer: "https://sso.example.com/realms/empresa"}, true},
		"a key on another provider": {idp.OAuth2Config{Preset: "custom", DirectoryKey: "{}"}, false},
	} {
		assert.Equal(t, tc.want, NewProvider(&tc.config).ReadsDirectory(), name)
	}
}

// Without a sign-in to renew, a Google user is looked up in the directory by
// email or by Google's subject: one gone, suspended or archived is no longer
// let in. A directory that refuses, or does not answer, ends nothing.
func TestAGoogleUserIsLookedUpInTheDirectory(t *testing.T) {
	w := startWorkspace(t)
	w.add("100", "ana@empresa.com", "staff@empresa.com")
	w.add("101", "beto@empresa.com", "staff@empresa.com")
	w.add("102", "carla@empresa.com")
	w.set(func(w *fakeWorkspace) {
		beto, carla := w.users["101"], w.users["102"]
		beto.Suspended, carla.Archived = true, true
		w.users["101"], w.users["102"] = beto, carla
	})
	p := w.provider()
	ctx := context.Background()

	for _, subject := range []string{"ana@empresa.com", "ANA@empresa.com", "100"} {
		user, err := p.Revalidate(ctx, idp.Grant{Subject: subject})
		require.NoError(t, err, subject)
		require.NotNil(t, user, subject)
		assert.Equal(t, "ana@empresa.com", user.ExternalID, subject)
		assert.Equal(t, "Ana Ruiz", user.DisplayName, subject)
		assert.Contains(t, user.Groups, "staff@empresa.com", subject)
		assert.NotContains(t, user.Groups, "Staff", "a group's name is not unique")
	}
	for _, subject := range []string{"gone@empresa.com", "999", "beto@empresa.com", "carla@empresa.com"} {
		_, err := p.Revalidate(ctx, idp.Grant{Subject: subject})
		assert.ErrorIs(t, err, idp.ErrSessionEnded, subject)
	}

	w.set(func(w *fakeWorkspace) { w.refuse = true })
	_, err := p.Revalidate(ctx, idp.Grant{Subject: "ana@empresa.com"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, idp.ErrSessionEnded)
	assert.NotErrorIs(t, err, idp.ErrUnavailable)
	assert.Contains(t, err.Error(), "administrator role")

	w.set(func(w *fakeWorkspace) { w.refuse, w.down = false, true })
	_, err = p.Revalidate(ctx, idp.Grant{Subject: "ana@empresa.com"})
	assert.ErrorIs(t, err, idp.ErrUnavailable)
}

// The members of a group are read page by page; those suspended, and groups
// inside it, are left out.
func TestTheMembersOfAGoogleGroup(t *testing.T) {
	w := startWorkspace(t)
	for i := range 450 {
		w.add(strconv.Itoa(i), fmt.Sprintf("user%d@empresa.com", i), "staff@empresa.com")
	}
	w.set(func(w *fakeWorkspace) {
		w.groups["staff@empresa.com"] = append(w.groups["staff@empresa.com"],
			workspaceMember{ID: "g1", Email: "inner@empresa.com", Type: "GROUP", Status: "ACTIVE"},
			workspaceMember{ID: "off", Email: "off@empresa.com", Type: "USER", Status: "SUSPENDED"},
			workspaceMember{ID: "all", Type: "CUSTOMER"})
	})

	members, err := w.provider().GetGroupMembers(context.Background(), "staff@empresa.com")
	require.NoError(t, err)
	assert.Len(t, members, 450)
	assert.Equal(t, googlePage, w.lastLimit, "the API gives at most 200 a page")
	for _, m := range members {
		assert.Equal(t, m.Email, m.ExternalID)
		assert.NotContains(t, []string{"inner@empresa.com", "off@empresa.com"}, m.Email)
	}

	_, err = w.provider().GetGroupMembers(context.Background(), "Staff")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "its email")
}

// Google's userinfo gives no groups: a sign-in and a renewal read them from the
// directory, or a user made by a group mapping could not sign in.
func TestAGoogleSignInGetsItsGroupsFromTheDirectory(t *testing.T) {
	w := startWorkspace(t)
	w.add("100", "ana@empresa.com", "staff@empresa.com")
	w.set(func(w *fakeWorkspace) {
		w.userInfo = map[string]any{"sub": "100", "email": "ana@empresa.com", "email_verified": true, "name": "Ana Ruiz"}
	})
	p := w.provider()
	ctx := context.Background()

	user, err := p.ExchangeCode(ctx, "code")
	require.NoError(t, err)
	assert.Contains(t, user.Groups, "staff@empresa.com")
	assert.Equal(t, "refresh", user.Grant.RefreshToken)

	renewed, err := p.Revalidate(ctx, idp.Grant{Subject: "100", RefreshToken: "refresh"})
	require.NoError(t, err)
	assert.Contains(t, renewed.Groups, "staff@empresa.com")

	t.Run("a directory that refuses signs in with no groups", func(t *testing.T) {
		w.set(func(w *fakeWorkspace) { w.refuse = true })
		defer w.set(func(w *fakeWorkspace) { w.refuse = false })
		user, err := p.ExchangeCode(ctx, "code")
		require.NoError(t, err)
		assert.Empty(t, user.Groups)

		_, err = p.Revalidate(ctx, idp.Grant{Subject: "100", RefreshToken: "refresh"})
		require.Error(t, err, "a renewal without groups would end the session of a user that follows them")
		assert.NotErrorIs(t, err, idp.ErrSessionEnded)
	})

	t.Run("a directory that does not answer", func(t *testing.T) {
		w.set(func(w *fakeWorkspace) { w.down = true })
		defer w.set(func(w *fakeWorkspace) { w.down = false })
		_, err := p.ExchangeCode(ctx, "code")
		assert.ErrorIs(t, err, idp.ErrUnavailable)
		_, err = p.Revalidate(ctx, idp.Grant{Subject: "100", RefreshToken: "refresh"})
		assert.ErrorIs(t, err, idp.ErrUnavailable)
	})

	t.Run("a user gone from the directory ends a renewal", func(t *testing.T) {
		w.set(func(w *fakeWorkspace) { delete(w.users, "100") })
		_, err := p.Revalidate(ctx, idp.Grant{Subject: "100", RefreshToken: "refresh"})
		assert.ErrorIs(t, err, idp.ErrSessionEnded)
	})
}

// Groups the provider gives in a claim are not asked for again.
func TestAGroupsClaimIsNotAskedOfTheDirectory(t *testing.T) {
	w := startWorkspace(t)
	w.add("100", "ana@empresa.com", "staff@empresa.com")
	w.set(func(w *fakeWorkspace) {
		w.userInfo = map[string]any{"sub": "100", "email": "ana@empresa.com", "groups": []string{"claimed"}}
		w.down = true
	})
	cfg := w.config()
	cfg.ClaimGroups = "groups"
	user, err := NewProvider(cfg).ExchangeCode(context.Background(), "code")
	require.NoError(t, err)
	assert.Equal(t, []string{"claimed"}, user.Groups)
}

// One token serves a whole sync: a request per user would ask Google for one
// each time.
func TestOneDirectoryTokenServesManyRequests(t *testing.T) {
	w := startWorkspace(t)
	w.add("100", "ana@empresa.com", "staff@empresa.com")
	p := w.provider()
	for range 20 {
		_, err := p.Revalidate(context.Background(), idp.Grant{Subject: "ana@empresa.com"})
		require.NoError(t, err)
	}
	assert.EqualValues(t, 1, w.tokens.Load())
}

// A key that is not a service account's ends nobody's sign-in, and says so.
func TestABrokenDirectoryKeyChangesNobody(t *testing.T) {
	w := startWorkspace(t)
	for name, key := range map[string]string{
		"not json":     "not a key",
		"an OAuth one": `{"web":{"client_id":"x"}}`,
		"no key":       `{"type":"service_account","client_email":"a@b"}`,
		"no PEM":       `{"type":"service_account","client_email":"a@b","private_key":"nope"}`,
	} {
		cfg := w.config()
		cfg.DirectoryKey = key
		p := NewProvider(cfg)
		_, err := p.Revalidate(context.Background(), idp.Grant{Subject: "ana@empresa.com"})
		require.Error(t, err, name)
		assert.NotErrorIs(t, err, idp.ErrSessionEnded, name)
		assert.Contains(t, err.Error(), "service account's JSON key", name)
		assert.NotContains(t, err.Error(), "nope", name)
	}
}

// The connection test reads the directory; one that refuses is a warning, as
// sign-ins work without it.
func TestTheConnectionTestReadsTheGoogleDirectory(t *testing.T) {
	w := startWorkspace(t)
	dir := w.provider().directory()
	ctx := context.Background()
	require.NoError(t, dir.check(ctx))

	check := w.provider().checkDirectory(ctx)
	assert.Equal(t, idp.CheckPassed, check.Status, check.Detail)

	w.set(func(w *fakeWorkspace) { w.refuse = true })
	check = w.provider().checkDirectory(ctx)
	assert.Equal(t, idp.CheckWarning, check.Status)
	assert.Contains(t, check.Detail, "administrator role")
}

// A directory that limits the requests says nothing of a user: it is
// unavailable, and the sync stops without changing anybody.
func TestALimitedDirectoryIsUnavailable(t *testing.T) {
	w := startWorkspace(t)
	limited := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusTooManyRequests)
	}))
	defer limited.Close()
	googleDirectoryAPI = limited.URL

	_, err := w.provider().Revalidate(context.Background(), idp.Grant{Subject: "ana@empresa.com"})
	assert.ErrorIs(t, err, idp.ErrUnavailable)
	assert.NotErrorIs(t, err, idp.ErrSessionEnded)
}

// A refused service account says why: the flow gives the error in the body only.
func TestARefusedServiceAccountSaysWhy(t *testing.T) {
	w := startWorkspace(t)
	w.set(func(w *fakeWorkspace) { w.badKey = true })
	_, err := w.provider().Revalidate(context.Background(), idp.Grant{Subject: "ana@empresa.com"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, idp.ErrSessionEnded)
	assert.NotErrorIs(t, err, idp.ErrUnavailable)
	assert.Contains(t, err.Error(), "invalid_grant (Invalid JWT Signature.)")
}
