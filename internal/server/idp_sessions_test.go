package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testIssuer is an OpenID Connect provider for the server's tests. Its refresh
// answers as refresh says: "ok", "ended" (invalid_grant) or "down" (503); its
// userinfo gives groups. Rotating, a refresh token is good once, as with
// rotation and reuse detection; slow holds each refresh's answer.
type testIssuer struct {
	server   *httptest.Server
	key      *rsa.PrivateKey
	groups   []string
	refresh  string
	rotating bool
	slow     time.Duration

	mu     sync.Mutex
	issued int
	used   map[string]bool
}

func startTestIssuer(t *testing.T) *testIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &testIssuer{key: key, groups: []string{"admins"}, refresh: "ok", used: map[string]bool{}}
	routes := http.NewServeMux()
	f.server = httptest.NewServer(routes)
	t.Cleanup(f.server.Close)
	reply := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	routes.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		reply(w, http.StatusOK, map[string]any{"issuer": f.server.URL, "authorization_endpoint": f.server.URL + "/auth",
			"token_endpoint": f.server.URL + "/token", "userinfo_endpoint": f.server.URL + "/userinfo",
			"jwks_uri": f.server.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	routes.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		enc := base64.RawURLEncoding
		reply(w, http.StatusOK, map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
			"n": enc.EncodeToString(key.N.Bytes()), "e": enc.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
	})
	routes.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "authorization_code" {
			reply(w, http.StatusOK, map[string]any{"access_token": "at", "token_type": "Bearer", "refresh_token": "rt", "expires_in": 300,
				"id_token": f.sign(t, map[string]any{"sub": "sub-alice", "sid": "sid-alice", "exp": time.Now().Add(time.Hour).Unix()})})
			return
		}
		f.mu.Lock()
		reused := f.rotating && f.used[r.Form.Get("refresh_token")]
		f.used[r.Form.Get("refresh_token")] = true
		f.issued++
		next := fmt.Sprintf("rt-%d", f.issued)
		f.mu.Unlock()
		time.Sleep(f.slow)
		switch {
		case f.refresh == "ended" || reused:
			reply(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
		case f.refresh == "down":
			reply(w, http.StatusServiceUnavailable, map[string]any{"error": "temporarily_unavailable"})
		default:
			reply(w, http.StatusOK, map[string]any{"access_token": "at", "token_type": "Bearer", "refresh_token": next, "expires_in": 300})
		}
	})
	routes.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		reply(w, http.StatusOK, map[string]any{"sub": "sub-alice", "email": "alice@example.org", "name": "Alice", "groups": f.groups})
	})
	return f
}

// refreshes counts the refreshes the issuer was asked for.
func (f *testIssuer) refreshes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.issued
}

func (f *testIssuer) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	full := jwt.MapClaims{"iss": f.server.URL, "aud": "maxiofs", "iat": time.Now().Unix()}
	for k, v := range claims {
		full[k] = v
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, full)
	token.Header["kid"] = "k1"
	signed, err := token.SignedString(f.key)
	require.NoError(t, err)
	return signed
}

// ssoNode is a server with the issuer as an OIDC provider whose group admins
// is mapped to the admin role and devs to the user role.
type ssoNode struct {
	server   *Server
	router   *mux.Router
	issuer   *testIssuer
	provider string
}

func newSSONode(t *testing.T) *ssoNode {
	t.Helper()
	n := &ssoNode{server: newClusterTestNode(t), router: mux.NewRouter(), issuer: startTestIssuer(t)}
	n.server.setupConsoleAPIRoutes(n.router)
	ctx := context.Background()
	u := n.issuer.server.URL
	provider := &idp.IdentityProvider{Name: "Keycloak", Type: idp.TypeOAuth2, Status: idp.StatusActive,
		Config: idp.ProviderConfig{OAuth2: &idp.OAuth2Config{Preset: "custom", ClientID: "maxiofs", ClientSecret: "secret",
			AuthURL: u + "/auth", TokenURL: u + "/token", UserInfoURL: u + "/userinfo", RedirectURI: "http://localhost/callback",
			Scopes: []string{"openid", "email"}, ClaimGroups: "groups", Issuer: u}}}
	require.NoError(t, n.server.idpManager.CreateProvider(ctx, provider))
	n.provider = provider.ID
	for group, role := range map[string]string{"admins": auth.RoleAdmin, "devs": auth.RoleUser} {
		require.NoError(t, n.server.idpManager.CreateGroupMapping(ctx, &idp.GroupMapping{ProviderID: provider.ID, ExternalGroup: group, Role: role}))
	}
	return n
}

func (n *ssoNode) do(t *testing.T, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	req.RemoteAddr = "198.51.100.60:4000"
	rr := httptest.NewRecorder()
	n.router.ServeHTTP(rr, req)
	return rr
}

// signIn goes through the callback and the code exchange as the console does;
// it gives the tokens, or the login page's error.
func (n *ssoNode) signIn(t *testing.T) (map[string]string, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/auth/oauth/callback?code=c&state="+url.QueryEscape(n.provider+":csrf"), nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "csrf"})
	rr := n.do(t, req)
	require.Equal(t, http.StatusFound, rr.Code)
	location, err := url.Parse(rr.Header().Get("Location"))
	require.NoError(t, err)
	if e := location.Query().Get("error"); e != "" {
		return nil, e
	}
	rr = n.do(t, httptest.NewRequest("GET", "/auth/oauth/exchange-code?code="+url.QueryEscape(location.Query().Get("code")), nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	tokens := map[string]string{}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &tokens))
	return tokens, ""
}

// renew renews the session as the console does, and gives the status and the
// new tokens.
func (n *ssoNode) renew(t *testing.T, refreshToken string) (int, map[string]string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	req := httptest.NewRequest("POST", "/auth/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := n.do(t, req)
	var resp struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	tokens := map[string]string{}
	for k, v := range resp.Data {
		if s, ok := v.(string); ok {
			tokens[k] = s
		}
	}
	return rr.Code, tokens
}

func (n *ssoNode) me(t *testing.T, accessToken string) int {
	t.Helper()
	req := httptest.NewRequest("GET", "/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	return n.do(t, req).Code
}

func (n *ssoNode) roleOf(t *testing.T, username string) []string {
	t.Helper()
	user, err := n.server.authManager.GetUserByUsername(context.Background(), username)
	require.NoError(t, err)
	return user.Roles
}

// A provider's sign-in is asked about again at each renewal: the groups give
// the role, a provider that cannot answer ends nothing, and a refresh the
// provider refuses ends the session at once.
func TestAProviderSessionFollowsTheProvider(t *testing.T) {
	n := newSSONode(t)
	tokens, failure := n.signIn(t)
	require.Empty(t, failure)
	assert.Equal(t, []string{auth.RoleAdmin}, n.roleOf(t, "alice@example.org"))

	n.issuer.groups = []string{"devs"}
	status, renewed := n.renew(t, tokens["refresh_token"])
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, []string{auth.RoleUser}, n.roleOf(t, "alice@example.org"), "the role the groups give now")

	n.issuer.refresh = "down"
	status, renewed = n.renew(t, renewed["refresh_token"])
	require.Equal(t, http.StatusOK, status, "a provider that cannot answer ends nothing")

	n.issuer.refresh = "ended"
	status, _ = n.renew(t, renewed["refresh_token"])
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, http.StatusUnauthorized, n.me(t, renewed["access_token"]), "its access token is refused at once")
}

// Renewals of one session that arrive together, as from two tabs, do not end
// it: a provider that rotates refresh tokens refuses a second use of one.
func TestRenewalsTogetherKeepTheSession(t *testing.T) {
	n := newSSONode(t)
	n.issuer.rotating, n.issuer.slow = true, 300*time.Millisecond
	tokens, failure := n.signIn(t)
	require.Empty(t, failure)

	statuses := make(chan int, 2)
	for range 2 {
		go func() {
			status, _ := n.renew(t, tokens["refresh_token"])
			statuses <- status
		}()
	}
	assert.Equal(t, http.StatusOK, <-statuses)
	assert.Equal(t, http.StatusOK, <-statuses)
	status, _ := n.renew(t, tokens["refresh_token"])
	assert.Equal(t, http.StatusOK, status, "a later renewal, with the token the provider gave last")
}

// A user its groups no longer authorize loses its session at the next renewal
// and cannot sign in again; a user an administrator made is not held to them.
func TestAUserTakenOutOfItsGroupsLosesItsSession(t *testing.T) {
	n := newSSONode(t)
	tokens, failure := n.signIn(t)
	require.Empty(t, failure)

	n.issuer.groups = []string{"accounting"}
	status, _ := n.renew(t, tokens["refresh_token"])
	assert.Equal(t, http.StatusUnauthorized, status)
	_, failure = n.signIn(t)
	assert.Equal(t, "not_in_authorized_group", failure)

	user, err := n.server.authManager.GetUserByUsername(context.Background(), "alice@example.org")
	require.NoError(t, err)
	delete(user.Metadata, provisionedByKey)
	require.NoError(t, n.server.authManager.UpdateUser(context.Background(), user))
	tokens, failure = n.signIn(t)
	require.Empty(t, failure, "a user the administrator keeps")
	status, _ = n.renew(t, tokens["refresh_token"])
	assert.Equal(t, http.StatusOK, status)
}

// The provider's back-channel logout ends the sessions of its session at once;
// a logout token that does not verify ends nothing.
func TestABackChannelLogoutEndsTheSession(t *testing.T) {
	n := newSSONode(t)
	tokens, failure := n.signIn(t)
	require.Empty(t, failure)
	logout := func(token string) int {
		req := httptest.NewRequest("POST", "/auth/oauth/"+n.provider+"/backchannel-logout",
			strings.NewReader(url.Values{"logout_token": {token}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return n.do(t, req).Code
	}
	event := map[string]any{"http://schemas.openid.net/event/backchannel-logout": map[string]any{}}

	assert.Equal(t, http.StatusBadRequest, logout(n.issuer.sign(t, map[string]any{"sid": "sid-alice", "jti": "j1"})), "without the event")
	assert.Equal(t, http.StatusOK, n.me(t, tokens["access_token"]))

	assert.Equal(t, http.StatusOK, logout(n.issuer.sign(t, map[string]any{"sid": "sid-alice", "jti": "j2", "events": event})))
	assert.Equal(t, http.StatusUnauthorized, n.me(t, tokens["access_token"]), "its access token")
	status, _ := n.renew(t, tokens["refresh_token"])
	assert.Equal(t, http.StatusUnauthorized, status, "its refresh token")
}

// Signing out ends the session: its tokens are refused from then on.
func TestASignOutEndsTheSession(t *testing.T) {
	server := newClusterTestNode(t)
	router := mux.NewRouter()
	server.setupConsoleAPIRoutes(router)
	require.NoError(t, server.authManager.CreateUser(context.Background(), &auth.User{
		ID: "dora", Username: "dora", Password: "Secret123!", Status: auth.UserStatusActive, Roles: []string{"admin"}}))
	raw, _ := json.Marshal(map[string]string{"username": "dora", "password": "Secret123!"})
	req := httptest.NewRequest("POST", "/auth/login", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100.61:4000"
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	var signedIn map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &signedIn))
	access, refresh := signedIn["access_token"].(string), signedIn["refresh_token"].(string)

	call := func(method, path, bearer string, body []byte) int {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		return rr.Code
	}
	require.Equal(t, http.StatusOK, call("GET", "/auth/me", access, nil))
	require.Equal(t, http.StatusOK, call("POST", "/auth/logout", access, nil))
	assert.Equal(t, http.StatusUnauthorized, call("GET", "/auth/me", access, nil), "the access token")
	body, _ := json.Marshal(map[string]string{"refresh_token": refresh})
	assert.Equal(t, http.StatusUnauthorized, call("POST", "/auth/refresh", "", body), "the refresh token")
}

// An administrator marks a user of an identity provider as one whose role and
// access follow the provider's groups; a local user has none to follow.
func TestAnAdministratorMarksAUserAsFollowingItsGroups(t *testing.T) {
	n := newSSONode(t)
	ctx := context.Background()
	require.NoError(t, n.server.authManager.CreateUser(ctx, &auth.User{ID: "user-ext", Username: "ext@example.org",
		Status: auth.UserStatusActive, Roles: []string{"user"}, AuthProvider: "oauth:" + n.provider, ExternalID: "ext@example.org"}))
	require.NoError(t, n.server.authManager.CreateUser(ctx, &auth.User{ID: "local-one", Username: "local-one",
		Password: "Secret123!", Status: auth.UserStatusActive, Roles: []string{"user"}}))
	raw, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin"})
	login := httptest.NewRequest("POST", "/auth/login", bytes.NewReader(raw))
	login.Header.Set("Content-Type", "application/json")
	rr := n.do(t, login)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var signedIn map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &signedIn))
	mark := func(id string, managed bool) int {
		body, _ := json.Marshal(map[string]bool{"managedByGroups": managed})
		req := httptest.NewRequest("PUT", "/users/"+id, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+signedIn["access_token"].(string))
		return n.do(t, req).Code
	}
	managed := func(id string) bool {
		user, err := n.server.authManager.GetUser(ctx, id)
		require.NoError(t, err)
		return groupManaged(user)
	}

	require.Equal(t, http.StatusOK, mark("user-ext", true))
	assert.True(t, managed("user-ext"))
	require.Equal(t, http.StatusOK, mark("user-ext", false))
	assert.False(t, managed("user-ext"))
	assert.Equal(t, http.StatusBadRequest, mark("local-one", true), "a local user")
}

// A console session made on one node is renewed through another, and one ended
// on one node is refused by the other.
func TestConsoleSessionsReachEveryNode(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	require.NoError(t, p.a.authManager.CreateUser(ctx, &auth.User{ID: "roaming", Username: "roaming", Password: "Secret123!",
		Roles: []string{"admin"}, Status: auth.UserStatusActive}))
	require.Eventually(t, func() bool {
		_, err := p.b.authManager.GetUser(ctx, "roaming")
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "the user reaches b")
	user, err := p.a.authManager.GetUser(ctx, "roaming")
	require.NoError(t, err)

	pair, err := p.a.authManager.StartSession(ctx, user, nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, sess, err := p.b.authManager.RefreshSession(ctx, pair.RefreshToken)
		return err == nil && sess != nil
	}, 10*time.Second, 20*time.Millisecond, "b renews the session a made")

	require.NoError(t, p.a.authManager.EndSession(ctx, pair.AccessToken, "signed out"))
	assert.Eventually(t, func() bool {
		_, err := p.b.authManager.ValidateJWT(ctx, pair.AccessToken)
		return errors.Is(err, auth.ErrSessionEnded)
	}, 10*time.Second, 20*time.Millisecond, "b refuses the session a ended")
}

// ssoPair is two nodes of a cluster with the issuer as an OIDC provider on
// both, as the provider's sync leaves it, its group admins mapped to admin.
type ssoPair struct {
	*haPair
	issuer   *testIssuer
	provider string
}

func newSSOPair(t *testing.T) *ssoPair {
	t.Helper()
	p := &ssoPair{haPair: newHAPair(t), issuer: startTestIssuer(t), provider: "idp-sso-pair"}
	ctx := context.Background()
	u := p.issuer.server.URL
	for _, s := range []*Server{p.a, p.b} {
		provider := &idp.IdentityProvider{ID: p.provider, Name: "Keycloak", Type: idp.TypeOAuth2, Status: idp.StatusActive,
			Config: idp.ProviderConfig{OAuth2: &idp.OAuth2Config{Preset: "custom", ClientID: "maxiofs", ClientSecret: "secret",
				AuthURL: u + "/auth", TokenURL: u + "/token", UserInfoURL: u + "/userinfo", RedirectURI: "http://localhost/callback",
				Scopes: []string{"openid", "email"}, ClaimGroups: "groups", Issuer: u}}}
		require.NoError(t, s.idpManager.CreateProvider(ctx, provider))
		require.NoError(t, s.idpManager.CreateGroupMapping(ctx, &idp.GroupMapping{ProviderID: p.provider, ExternalGroup: "admins", Role: auth.RoleAdmin}))
	}
	return p
}

// signIn signs in on s as the console does, and gives the tokens.
func (p *ssoPair) signIn(t *testing.T, s *Server) map[string]string {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/auth/oauth/callback?code=c&state="+url.QueryEscape(p.provider+":csrf"), nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "csrf"})
	rr := httptest.NewRecorder()
	s.consoleServer.Handler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusFound, rr.Code, rr.Body.String())
	location, err := url.Parse(rr.Header().Get("Location"))
	require.NoError(t, err)
	require.Empty(t, location.Query().Get("error"))
	rr = httptest.NewRecorder()
	s.consoleServer.Handler.ServeHTTP(rr, httptest.NewRequest("GET",
		"/api/v1/auth/oauth/exchange-code?code="+url.QueryEscape(location.Query().Get("code")), nil))
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	tokens := map[string]string{}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &tokens))
	return tokens
}

func renewOn(s *Server, refreshToken string) int {
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	req := httptest.NewRequest("POST", "/api/v1/auth/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	s.consoleServer.Handler.ServeHTTP(rr, req)
	return rr.Code
}

// signInUnseenByB signs in on a while b refuses a's requests, and waits until
// a stops trying: the session, and a user it made, have not reached b.
func (p *ssoPair) signInUnseenByB(t *testing.T) map[string]string {
	t.Helper()
	p.bDown.Store(true)
	defer p.bDown.Store(false)
	before := p.bRefused.Load()
	tokens := p.signIn(t, p.a)
	require.Eventually(t, func() bool { return p.bRefused.Load() > before }, 10*time.Second, 20*time.Millisecond)
	for last := int32(-1); last != p.bRefused.Load(); {
		last = p.bRefused.Load()
		time.Sleep(500 * time.Millisecond)
	}
	return tokens
}

func (p *ssoPair) reachesB(t *testing.T, refreshToken string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, sess, err := p.b.authManager.RefreshSession(context.Background(), refreshToken)
		return err == nil && sess != nil
	}, 10*time.Second, 20*time.Millisecond, "the session and its user reach b")
}

// The node that started a provider's session renews it: a node the session
// and its user have not reached yet passes its renewal there, and renewals on
// two nodes at once keep it with a provider that rotates refresh tokens.
func TestTheNodeThatStartedAProviderSessionRenewsIt(t *testing.T) {
	p := newSSOPair(t)
	p.issuer.rotating, p.issuer.slow = true, 300*time.Millisecond

	tokens := p.signInUnseenByB(t)
	_, err := p.b.authManager.GetUserByUsername(context.Background(), "alice@example.org")
	require.Error(t, err, "the user has not reached b")
	assert.Equal(t, http.StatusOK, renewOn(p.b, tokens["refresh_token"]), "b passes the renewal to a")

	// The user reaches b with a's next user sync.
	p.a.userSyncMgr.TriggerSync(context.Background())
	p.reachesB(t, tokens["refresh_token"])
	statuses := make(chan int, 2)
	for _, s := range []*Server{p.a, p.b} {
		go func() { statuses <- renewOn(s, tokens["refresh_token"]) }()
	}
	assert.Equal(t, http.StatusOK, <-statuses)
	assert.Equal(t, http.StatusOK, <-statuses)
	assert.Equal(t, http.StatusOK, renewOn(p.b, tokens["refresh_token"]), "a later renewal")
}

// When the node that started a session does not answer, another keeps it
// without asking the provider: only that node uses the provider's refresh
// token, and back, it renews the session with the one it has, which a provider
// that rotates them still takes. A node the session has not reached yet asks
// to try again.
func TestARenewalGoesOnWhenTheStartingNodeIsDown(t *testing.T) {
	p := newSSOPair(t)
	p.issuer.rotating = true
	tokens := p.signIn(t, p.a)
	p.reachesB(t, tokens["refresh_token"])
	unknown := p.signInUnseenByB(t)

	p.aDown.Store(true)
	asked := p.issuer.refreshes()
	assert.Equal(t, http.StatusOK, renewOn(p.b, tokens["refresh_token"]), "b keeps the session")
	assert.Equal(t, asked, p.issuer.refreshes(), "without asking the provider")
	assert.Equal(t, http.StatusServiceUnavailable, renewOn(p.b, unknown["refresh_token"]), "b has not the session")

	p.aDown.Store(false)
	_, err := p.b.db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusUnavailable, p.aID)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, renewOn(p.b, tokens["refresh_token"]), "b keeps it while it sees a down")
	assert.Equal(t, asked, p.issuer.refreshes(), "without asking the provider")

	markHealthy(t, p.b, p.aID)
	assert.Equal(t, http.StatusOK, renewOn(p.b, tokens["refresh_token"]), "a, back, renews it with the provider")
	assert.Equal(t, asked+1, p.issuer.refreshes())
}

// The coordinator renews the sessions of a node no longer in the cluster, so
// still only one node uses their provider's refresh token; without one, they
// are kept without asking the provider.
func TestTheCoordinatorRenewsTheSessionsOfARemovedNode(t *testing.T) {
	p := newSSOPair(t)
	p.issuer.rotating = true
	p.a.authManager.SetSessionNode(func(context.Context) string { return "removed-node" })
	tokens := p.signIn(t, p.a)
	p.a.authManager.SetSessionNode(func(context.Context) string { return p.aID })
	p.reachesB(t, tokens["refresh_token"])

	asked := p.issuer.refreshes()
	assert.Equal(t, http.StatusOK, renewOn(p.b, tokens["refresh_token"]), "no coordinator: b keeps it")
	assert.Equal(t, asked, p.issuer.refreshes(), "without asking the provider")

	require.NoError(t, p.b.leaderMgr.EnsureSchema())
	now := time.Now().Unix()
	_, err := p.b.db.Exec(`INSERT OR REPLACE INTO cluster_leader (id, leader_id, term, granted_at, expires_at) VALUES (1, ?, 1, ?, ?)`,
		p.aID, now, now+3600)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, renewOn(p.b, tokens["refresh_token"]), "b passes it to the coordinator")
	assert.Equal(t, asked+1, p.issuer.refreshes(), "which asks the provider")
	assert.Equal(t, http.StatusOK, renewOn(p.b, tokens["refresh_token"]), "a later renewal")
	assert.Equal(t, asked+2, p.issuer.refreshes())
}
