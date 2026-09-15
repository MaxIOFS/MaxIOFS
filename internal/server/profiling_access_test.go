package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The pprof routes hang off the root router, outside the subrouter that
// authenticates, so they answered 401 to everyone including a global admin.
func TestProfiling_ReachableByAGlobalAdmin(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()
	require.NoError(t, server.setupRoutes())

	ts := httptest.NewServer(server.consoleServer.Handler)
	defer ts.Close()

	admin := &auth.User{
		ID: "pprof-admin", Username: "pprof-admin", Status: auth.UserStatusActive,
		Roles: []string{auth.RoleAdmin}, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix(),
	}
	require.NoError(t, server.authManager.CreateUser(t.Context(), admin))
	t.Cleanup(func() { _ = server.authManager.DeleteUser(t.Context(), admin.ID) })

	token, err := server.authManager.GenerateJWT(t.Context(), admin)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/debug/pprof/heap", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestProfiling_RefusedWithoutACredential(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()
	require.NoError(t, server.setupRoutes())

	ts := httptest.NewServer(server.consoleServer.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/debug/pprof/heap")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
