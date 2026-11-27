package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signInAnswer is the console's answer to a sign-in with body, from address.
func signInAnswer(s *Server, body, address string) (*httptest.ResponseRecorder, map[string]any) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(body)))
	req.RemoteAddr = address + ":5000"
	rec := httptest.NewRecorder()
	s.handleLogin(rec, req)
	answer := map[string]any{}
	if json.Unmarshal(rec.Body.Bytes(), &answer) != nil {
		answer = nil // not one JSON document: answered twice
	}
	return rec, answer
}

// A sign-in gives a console session to an active user with access to the
// console whose credentials its provider takes; a user of an OAuth provider is
// sent to its button, a locked one told until when, and the rest refused
// alike.
func TestASignInGivesASessionOnlyToWhoMayHaveOne(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	const password = "Sign-in-password-123!"
	now := time.Now().Unix()
	user := func(id, provider, status string, roles ...string) {
		require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: id, Username: id, Password: password,
			AuthProvider: provider, ExternalID: id, Status: status, Roles: roles, CreatedAt: now, UpdatedAt: now}))
	}
	user("frank", "", auth.UserStatusActive, auth.RoleUser)
	user("sso-only", "oauth:corp", auth.UserStatusActive, auth.RoleUser)
	user("odd-provider", "saml:corp", auth.UserStatusActive, auth.RoleUser)
	user("gone-quiet", "", auth.UserStatusInactive, auth.RoleUser)
	user("no-console", "", auth.UserStatusActive)
	user("locked-out", "", auth.UserStatusActive, auth.RoleUser)
	require.NoError(t, s.authManager.LockAccount(ctx, "locked-out"))
	credentials := func(username string) string {
		return `{"username":"` + username + `","password":"` + password + `"}`
	}

	rec, answer := signInAnswer(s, credentials("frank"), "192.0.2.1")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, true, answer["success"])
	assert.NotEmpty(t, answer["access_token"])
	assert.NotEmpty(t, answer["refresh_token"])
	assert.Equal(t, answer["access_token"], answer["token"])
	assert.Equal(t, false, answer["default_password"])
	assert.Equal(t, false, answer["must_change_password"])

	rec, answer = signInAnswer(s, credentials("sso-only"), "192.0.2.2")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, true, answer["sso_hint"])
	rec, answer = signInAnswer(s, credentials("locked-out"), "192.0.2.3")
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, true, answer["locked"])
	assert.Greater(t, answer["locked_until"], float64(now))
	for body, status := range map[string]int{
		`not json`:                                http.StatusBadRequest,
		credentials("odd-provider"):               http.StatusInternalServerError,
		credentials("gone-quiet"):                 http.StatusUnauthorized,
		credentials("no-console"):                 http.StatusForbidden,
		credentials("nobody"):                     http.StatusUnauthorized,
		`{"username":"frank","password":"wrong"}`: http.StatusUnauthorized,
	} {
		rec, answer := signInAnswer(s, body, "192.0.2.4")
		assert.Equal(t, status, rec.Code, body)
		assert.NotNil(t, answer, "one answer: %s", rec.Body.String())
	}

	// An email no user has, while an OAuth provider signs users in, is sent to
	// its button, and counts as no failed sign-in.
	require.NoError(t, s.idpManager.CreateProvider(ctx, &idp.IdentityProvider{Name: "SSO", Type: idp.TypeOAuth2, Status: idp.StatusActive,
		Config: idp.ProviderConfig{OAuth2: &idp.OAuth2Config{Preset: "custom", ClientID: "client", ClientSecret: "secret",
			AuthURL: "http://127.0.0.1:1/auth", TokenURL: "http://127.0.0.1:1/token", UserInfoURL: "http://127.0.0.1:1/userinfo",
			RedirectURI: "http://localhost/callback"}}}))
	rec, answer = signInAnswer(s, `{"username":"someone@corp.test","password":"x"}`, "192.0.2.5")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, true, answer["sso_hint"])
	rec, _ = signInAnswer(s, `{"username":"nobody-at-all","password":"x"}`, "192.0.2.5")
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "not an email")
	frank, err := s.authManager.GetUser(ctx, "frank")
	require.NoError(t, err)
	assert.Equal(t, 1, frank.FailedLoginAttempts, "a user's record has its failed sign-ins")
	assert.GreaterOrEqual(t, frank.LastFailedLogin, now)
}

// An address that failed too often in a minute is refused before its
// credentials are read.
func TestASignInFromAnAddressThatFailsTooOftenWaits(t *testing.T) {
	s := newClusterTestNode(t)
	require.NoError(t, s.settingsManager.Set("security.ratelimit_login_per_minute", "2"))
	time.Sleep(1100 * time.Millisecond) // settings are read through a cache of one second
	for range 2 {
		rec, _ := signInAnswer(s, `{"username":"admin","password":"wrong"}`, "198.51.100.9")
		require.Equal(t, http.StatusUnauthorized, rec.Code)
	}
	rec, _ := signInAnswer(s, `{"username":"admin","password":"admin"}`, "198.51.100.9")
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	rec, _ = signInAnswer(s, `{"username":"admin","password":"admin"}`, "198.51.100.10")
	assert.Equal(t, http.StatusOK, rec.Code, "another address")
}
