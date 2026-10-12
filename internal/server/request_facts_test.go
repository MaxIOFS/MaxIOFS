package server

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
)

// Bucket policy conditions see the client address and TLS; a trusted proxy's
// forwarding headers count, anyone else's do not.
func TestPolicyRequestFacts(t *testing.T) {
	s := &Server{config: &config.Config{TrustedProxies: []string{"203.0.113.10"}}}
	var got bucket.RequestFacts
	handler := s.policyRequestFacts(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = bucket.RequestFactsFrom(r.Context())
	}))
	facts := func(remote string, secure bool) bucket.RequestFacts {
		req := httptest.NewRequest("GET", "/bucket/key", nil)
		req.RemoteAddr = remote
		req.Header.Set("X-Forwarded-For", "10.9.9.9")
		req.Header.Set("X-Forwarded-Proto", "https")
		if secure {
			req.TLS = &tls.ConnectionState{}
		}
		handler.ServeHTTP(httptest.NewRecorder(), req)
		return got
	}

	assert.Equal(t, bucket.RequestFacts{SourceIP: "198.51.100.7"}, facts("198.51.100.7:5000", false),
		"a client's own forwarding headers are not taken")
	assert.Equal(t, bucket.RequestFacts{SourceIP: "10.9.9.9", SecureTransport: true}, facts("203.0.113.10:443", false),
		"a trusted proxy's are")
	assert.Equal(t, bucket.RequestFacts{SourceIP: "198.51.100.7", SecureTransport: true}, facts("198.51.100.7:5000", true))
}
