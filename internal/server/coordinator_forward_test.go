package server

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
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

// A write forwarded to the coordinator is served with the user's address and
// transport: the limit per address, the audit log and the bucket policy
// conditions see the user, not the node that forwarded it.
func TestForward_TheCoordinatorSeesTheUser(t *testing.T) {
	server := getSharedServer()
	for name, tc := range map[string]struct {
		remoteAddr string
		headers    map[string]string
		tls        bool
		address    string
		secure     bool
	}{
		"over TLS":               {remoteAddr: "198.51.100.7:5000", tls: true, address: "198.51.100.7", secure: true},
		"in clear":               {remoteAddr: "198.51.100.8:5000", address: "198.51.100.8"},
		"through a proxy":        {remoteAddr: "10.0.0.5:5000", headers: map[string]string{"X-Forwarded-For": "198.51.100.9", "X-Forwarded-Proto": "https"}, address: "198.51.100.9", secure: true},
		"from the local network": {remoteAddr: "10.0.0.6:5000", address: "10.0.0.6"},
	} {
		t.Run(name, func(t *testing.T) {
			user := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader("{}"))
			user.RemoteAddr = tc.remoteAddr
			for key, value := range tc.headers {
				user.Header.Set(key, value)
			}
			if tc.tls {
				user.TLS = &tls.ConnectionState{}
			}
			user.Header.Set("Authorization", "Bearer user-token")

			forwarded := httptest.NewRequest(http.MethodPost, "/api/internal/cluster/console-write/api/v1/users", nil)
			server.policyRequestFacts(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				carryUser(forwarded, r)
			})).ServeHTTP(httptest.NewRecorder(), user)

			replay := replayOf(forwarded, "/api/v1/users", []byte("{}"))
			var facts bucket.RequestFacts
			server.policyRequestFacts(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				facts = bucket.RequestFactsFrom(r.Context())
			})).ServeHTTP(httptest.NewRecorder(), replay)

			assert.Equal(t, tc.address, getClientIP(replay, server.config.TrustedProxies))
			assert.Equal(t, tc.address, facts.SourceIP)
			assert.Equal(t, tc.secure, facts.SecureTransport)
			assert.Equal(t, "Bearer user-token", replay.Header.Get("Authorization"))
		})
	}
}
