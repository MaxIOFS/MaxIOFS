package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signInFrom signs username in through the console's login from address.
func signInFrom(t *testing.T, router *mux.Router, address, username, password string) int {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"username": username, "password": password})
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/auth/login", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = address + ":4000"
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr.Code
}

// Only failed sign-ins count against the address they come from: software
// that signs in on every request, or many users behind one NAT, is never held
// up, and the failures of the limit hold the address up, an unknown user's as
// well, whatever it tries next.
func TestOnlyFailedSignInsHoldUpAnAddress(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()
	router := mux.NewRouter()
	server.setupConsoleAPIRoutes(router)
	require.NoError(t, server.authManager.CreateUser(context.Background(), &auth.User{
		ID: "busy", Username: "busy", Password: "Secret123!", Status: auth.UserStatusActive, Roles: []string{"admin"}}))
	perMinute, err := server.settingsManager.GetInt("security.ratelimit_login_per_minute")
	require.NoError(t, err)
	const address = "198.51.100.20"

	for i := range 3 * perMinute {
		require.Equal(t, http.StatusOK, signInFrom(t, router, address, "busy", "Secret123!"), "sign-in %d", i+1)
	}
	for i := range perMinute {
		user := "busy"
		if i%2 == 1 {
			user = "nobody"
		}
		require.Equal(t, http.StatusUnauthorized, signInFrom(t, router, address, user, "wrong"), "failure %d", i+1)
	}
	assert.Equal(t, http.StatusTooManyRequests, signInFrom(t, router, address, "busy", "Secret123!"))
	assert.Equal(t, http.StatusOK, signInFrom(t, router, "198.51.100.21", "busy", "Secret123!"), "another address")
}

// The security settings reach the sign-in of a server as it starts: the limit
// of failures per address is the one the settings give.
func TestTheSecuritySettingsReachTheSignIn(t *testing.T) {
	server := newClusterTestNode(t)
	router := mux.NewRouter()
	server.setupConsoleAPIRoutes(router)
	require.NoError(t, server.settingsManager.Set("security.ratelimit_login_per_minute", "2"))
	const address = "198.51.100.30"

	for range 2 {
		require.Equal(t, http.StatusUnauthorized, signInFrom(t, router, address, "nobody", "wrong"))
	}
	assert.Equal(t, http.StatusTooManyRequests, signInFrom(t, router, address, "nobody", "wrong"))
}
