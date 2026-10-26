package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/metrics"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A user an identity provider vouches for still gives the second factor when
// it has one: the single-use code the console exchanges carries the second
// step's token, not a session, and the user's code then signs it in. A user
// without one gets its session from the exchange.
func TestOAuthSignInAsksForTheSecondFactor(t *testing.T) {
	server, tmpDir, cleanup := setupTestServer(t)
	defer cleanup()
	server.systemMetrics = metrics.NewSystemMetrics(tmpDir)
	ctx := context.Background()
	router := mux.NewRouter()
	server.setupConsoleAPIRoutes(router)

	newSSOUser := func(id string) *auth.User {
		t.Helper()
		user := &auth.User{ID: id, Username: id + "@example.com", Status: auth.UserStatusActive, Roles: []string{"admin"}, AuthProvider: "oauth:test-idp"}
		require.NoError(t, server.authManager.CreateUser(ctx, user))
		return user
	}
	// signIn finishes the provider's sign-in and exchanges the code the
	// browser is sent with.
	signIn := func(user *auth.User) map[string]interface{} {
		t.Helper()
		rr := httptest.NewRecorder()
		server.finishOAuthSignIn(rr, httptest.NewRequest("GET", "/auth/oauth/callback", nil), user, "test-idp", nil)
		require.Equal(t, http.StatusFound, rr.Code)
		location, err := url.Parse(rr.Header().Get("Location"))
		require.NoError(t, err)
		require.True(t, strings.HasSuffix(location.Path, "/auth/oauth/complete"), location.String())
		assert.NotContains(t, location.RawQuery, "user_id")
		code := location.Query().Get("code")
		require.NotEmpty(t, code)

		rr = httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest("GET", "/auth/oauth/exchange-code?code="+url.QueryEscape(code), nil))
		require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
		out := map[string]interface{}{}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
		return out
	}

	plain := signIn(newSSOUser("sso-plain"))
	assert.NotEmpty(t, plain["access_token"])
	assert.NotEmpty(t, plain["refresh_token"])

	user := newSSOUser("sso-two-factor")
	setup, err := auth.Generate2FASecret(user.Username, "MaxIOFS")
	require.NoError(t, err)
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)
	_, err = server.authManager.Enable2FA(ctx, user.ID, code, setup.Secret)
	require.NoError(t, err)

	pending := signIn(user)
	assert.Equal(t, true, pending["requires_2fa"], pending)
	assert.NotContains(t, pending, "access_token")
	token, _ := pending["two_factor_token"].(string)
	require.NotEmpty(t, token)
	_, err = server.authManager.ValidateJWT(ctx, token)
	assert.Error(t, err, "the second step's token is not a session")

	code, err = totp.GenerateCode(setup.Secret, time.Now())
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/auth/2fa/verify", strings.NewReader(`{"two_factor_token":"`+token+`","code":"`+code+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.210:4000"
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), `"token"`)
}
