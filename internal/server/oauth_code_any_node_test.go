package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Behind a load balancer the browser exchanges the code of an OAuth sign-in on
// any node: the node the provider's redirect reached keeps the code and is
// asked for it. The code works once.
func TestAnOAuthCodeIsExchangedOnAnyNode(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	user := &auth.User{ID: "sso-any-node", Username: "sso-any-node@example.com", Status: auth.UserStatusActive,
		Roles: []string{"admin"}, AuthProvider: "oauth:test-idp"}
	require.NoError(t, p.a.authManager.CreateUser(ctx, user))

	rr := httptest.NewRecorder()
	p.a.finishOAuthSignIn(rr, httptest.NewRequest("GET", "/api/v1/auth/oauth/callback", nil), user, "test-idp")
	require.Equal(t, http.StatusFound, rr.Code)
	location, err := url.Parse(rr.Header().Get("Location"))
	require.NoError(t, err)
	code := location.Query().Get("code")
	require.NotEmpty(t, code)

	exchange := func(s *Server) (int, map[string]interface{}) {
		t.Helper()
		rr := httptest.NewRecorder()
		s.consoleServer.Handler.ServeHTTP(rr, httptest.NewRequest("GET", "/api/v1/auth/oauth/exchange-code?code="+url.QueryEscape(code), nil))
		out := map[string]interface{}{}
		_ = json.Unmarshal(rr.Body.Bytes(), &out)
		return rr.Code, out
	}

	status, out := exchange(p.b)
	require.Equal(t, http.StatusOK, status, out)
	assert.NotEmpty(t, out["access_token"])
	assert.NotEmpty(t, out["refresh_token"])

	status, _ = exchange(p.a)
	assert.Equal(t, http.StatusUnauthorized, status, "the code works once")
	status, _ = exchange(p.b)
	assert.Equal(t, http.StatusUnauthorized, status, "the code works once")
}
