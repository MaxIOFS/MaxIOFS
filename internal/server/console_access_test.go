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
	"github.com/maxiofs/maxiofs/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsoleAccessCapabilityBlocksExistingTokens(t *testing.T) {
	server, tmpDir, cleanup := setupTestServer(t)
	defer cleanup()
	server.systemMetrics = metrics.NewSystemMetrics(tmpDir)

	ctx := context.Background()
	user := &auth.User{
		ID:       "console-denied-admin",
		Username: "console-denied-admin",
		Status:   "active",
		Roles:    []string{"admin"},
	}
	require.NoError(t, server.authManager.CreateUser(ctx, user))
	require.NoError(t, server.authManager.SetCapabilityOverride(ctx, user.ID, auth.CapConsoleAccess, "test", false))

	pair, err := server.authManager.GenerateTokenPair(ctx, user)
	require.NoError(t, err)

	router := mux.NewRouter()
	server.setupConsoleAPIRoutes(router)

	req := httptest.NewRequest("GET", "/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusForbidden, rr.Code)

	refreshBody, _ := json.Marshal(map[string]string{"refresh_token": pair.RefreshToken})
	refreshReq := httptest.NewRequest("POST", "/auth/refresh", bytes.NewReader(refreshBody))
	refreshReq.Header.Set("Content-Type", "application/json")
	refreshRR := httptest.NewRecorder()
	router.ServeHTTP(refreshRR, refreshReq)
	assert.Equal(t, http.StatusForbidden, refreshRR.Code)
}
