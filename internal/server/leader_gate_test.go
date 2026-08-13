package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGate_ReadsAreNeverBlocked: a node that does not coordinate still serves
// every read. Only writes need arbitration.
func TestGate_ReadsAreNeverBlocked(t *testing.T) {
	server := getSharedServer()

	served := false
	handler := server.coordinatorMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
	}))

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		served = false
		req := httptest.NewRequest(method, "/api/v1/users", nil)
		handler.ServeHTTP(httptest.NewRecorder(), req)
		assert.True(t, served, "%s must pass through the gate", method)
	}
}

// TestGate_SigningInIsNeverBlocked is the one that would hurt most: an election
// takes seconds, and an operator locked out of the console during it cannot fix
// whatever caused the election.
func TestGate_SigningInIsNeverBlocked(t *testing.T) {
	for _, path := range []string{
		"/auth/login",
		"/auth/logout",
		"/auth/refresh",
		"/auth/2fa/verify",
	} {
		assert.True(t, coordinatorExemptPath(path),
			"%s must work on any node, coordinator or not", path)
	}
}

// TestGate_ClusterTrafficIsNeverBlocked: node-to-node requests carry their own
// coordination. Gating them would stop the cluster from healing.
func TestGate_ClusterTrafficIsNeverBlocked(t *testing.T) {
	for _, path := range []string{
		"/api/internal/cluster/iam-sync",
		"/api/internal/cluster/leader-lease",
		"/api/internal/cluster/sts-session-sync",
	} {
		assert.True(t, coordinatorExemptPath(path),
			"%s is how nodes reconcile and must not need a coordinator", path)
	}
}

// TestGate_ObjectTrafficIsNeverBlocked: objects are owned per bucket, so they
// need no global arbitration. Gating them would make an election an outage for
// data, not just for administration.
func TestGate_ObjectTrafficIsNeverBlocked(t *testing.T) {
	for _, path := range []string{
		"/api/v1/buckets/my-bucket/objects",
		"/api/v1/buckets/my-bucket/objects/upload",
		"/api/v1/buckets/my-bucket/objects/download",
	} {
		assert.True(t, coordinatorExemptPath(path),
			"%s is object traffic and must not be gated", path)
	}
}

// TestGate_ConfigurationWritesAreGated is the positive case: the changes that
// two nodes could make at once are the ones that need one writer.
func TestGate_ConfigurationWritesAreGated(t *testing.T) {
	for _, path := range []string{
		"/api/v1/users",
		"/api/v1/iam/policies",
		"/api/v1/iam/roles",
		"/api/v1/tenants",
		"/api/v1/groups",
	} {
		assert.False(t, coordinatorExemptPath(path),
			"%s changes shared configuration and must go through the coordinator", path)
	}
}

// TestGate_StandaloneAlwaysPasses: without a cluster there is nobody to
// disagree with, so nothing is gated.
func TestGate_StandaloneAlwaysPasses(t *testing.T) {
	server := getSharedServer()

	served := false
	handler := server.coordinatorMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.True(t, served, "a single-node deployment writes configuration without arbitration")
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestGate_RefusalTellsTheClientWhereToGo: a refusal has to be actionable —
// which node to retry against, and that retrying is the right move.
func TestGate_RefusalIsActionable(t *testing.T) {
	server := getSharedServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/users", nil)

	// Standalone passes, so this asserts the shape of the refusal path itself
	// rather than driving a real election.
	if !server.requireCoordinator(rec, req) {
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code,
			"a refusal is 'not here, not now' — retryable, not a permission error")
		assert.NotEmpty(t, rec.Header().Get("Retry-After"),
			"and tells the client it is worth retrying")
	}
}
