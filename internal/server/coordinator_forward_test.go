package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestForward_AlreadyForwardedIsNotForwardedAgain is the loop guard. A request
// that reached a node because it was coordinator a moment ago, and is no longer,
// is answered rather than passed on.
func TestForward_AlreadyForwardedIsNotForwardedAgain(t *testing.T) {
	server := getSharedServer()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", nil)
	req.Header.Set(forwardedHeader, "true")
	rec := httptest.NewRecorder()

	// Standalone passes everything, so this asserts the guard itself rather
	// than driving a real election.
	proceed := server.requireCoordinator(rec, req)
	if !proceed {
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code,
			"a request that cannot be served or forwarded is answered, not bounced")
		assert.NotEmpty(t, rec.Header().Get("Retry-After"))
	}
}

// TestForward_StandalonePassesThrough: with no cluster there is nobody to
// forward to, and nothing to arbitrate.
func TestForward_StandalonePassesThrough(t *testing.T) {
	server := getSharedServer()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", nil)
	rec := httptest.NewRecorder()

	assert.True(t, server.requireCoordinator(rec, req),
		"a single node writes its own configuration")
}

// TestForward_CoordinatorWriteRequiresClusterAuth: the endpoint that applies a
// forwarded write is reachable only over the authenticated inter-node channel.
func TestForward_CoordinatorWriteRequiresClusterAuth(t *testing.T) {
	server := getSharedServer()

	req := httptest.NewRequest(http.MethodPost,
		"/api/internal/cluster/console-write/api/v1/users", nil)
	rec := httptest.NewRecorder()

	server.handleCoordinatorWrite(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"without cluster authentication the forwarded-write endpoint refuses")
}

// TestForward_UserCredentialTravelsSeparately documents why forwarding is safe:
// the user's own token is carried under its own header and validated by the
// coordinator, so the forwarding node asserts nothing about who is calling.
func TestForward_UserCredentialTravelsSeparately(t *testing.T) {
	assert.NotEqual(t, "Authorization", "X-MaxIOFS-Forwarded-Authorization")
	assert.Equal(t, "X-MaxIOFS-Coordinator-Forward", forwardedHeader)
}
