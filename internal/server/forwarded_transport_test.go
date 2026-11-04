package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
)

// TestForwardedFacts: a signed forwarded request is taken as its first node
// found it, its client's address and transport, whatever networks the nodes
// are on; a request that only claims it, without a signed forwarding, loses
// the claim.
func TestForwardedFacts(t *testing.T) {
	// As the receiving node found it: from the forwarding node, over TLS.
	here := bucket.WithRequestFacts(context.Background(), bucket.RequestFacts{SourceIP: "203.0.113.20", SecureTransport: true})

	r := httptest.NewRequest("PUT", "/b/k", nil)
	r.Header.Set(cluster.ClientIPHeader, "198.51.100.7")
	facts := bucket.RequestFactsFrom(withForwardedFacts(here, r))
	assert.Equal(t, "198.51.100.7", facts.SourceIP, "the client's address, not the forwarding node's")
	assert.False(t, facts.SecureTransport, "the client came over HTTP")

	r.Header.Set(cluster.SecureTransportHeader, "true")
	assert.True(t, bucket.RequestFactsFrom(withForwardedFacts(here, r)).SecureTransport)

	r.Header.Set(cluster.ClientIPHeader, "not an address")
	assert.Equal(t, "203.0.113.20", bucket.RequestFactsFrom(withForwardedFacts(here, r)).SourceIP)

	cluster.StripInternalClusterHeaders(r)
	assert.Empty(t, r.Header.Get(cluster.SecureTransportHeader), "an unsigned request loses the claim")
	assert.Empty(t, r.Header.Get(cluster.ClientIPHeader))
}

// TestClientIPFromProxies: the client's address is the rightmost entry of
// X-Forwarded-For past the proxies of trusted_proxies, from a trusted peer
// only; trust_private_networks off trusts trusted_proxies alone.
func TestClientIPFromProxies(t *testing.T) {
	private := proxyTrust{private: true}
	listed := proxyTrust{proxies: []string{"198.51.100.0/24"}, private: false}
	for name, tc := range map[string]struct {
		peer, forwardedFor, proto string
		trust                     proxyTrust
		ip                        string
		secure                    bool
	}{
		"a proxy on a private network":              {"10.0.0.5:1", "203.0.113.9", "https", private, "203.0.113.9", true},
		"a client's own entry left of the proxy's":  {"10.0.0.5:1", "10.9.9.9, 203.0.113.9", "https", private, "203.0.113.9", true},
		"a chain through a listed proxy":            {"198.51.100.1:1", "203.0.113.9, 198.51.100.2", "https", listed, "203.0.113.9", true},
		"a private peer with private trust off":     {"10.0.0.5:1", "203.0.113.9", "https", listed, "10.0.0.5", false},
		"a listed public proxy":                     {"198.51.100.1:1", "203.0.113.9", "https", listed, "203.0.113.9", true},
		"a public peer that is no proxy":            {"203.0.113.50:1", "10.0.0.1", "https", private, "203.0.113.50", false},
		"a private client of the LAN behind no one": {"10.0.0.6:1", "", "", private, "10.0.0.6", false},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.peer
			if tc.forwardedFor != "" {
				r.Header.Set("X-Forwarded-For", tc.forwardedFor)
			}
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			assert.Equal(t, tc.ip, getClientIP(r, tc.trust))
			s := &Server{config: &config.Config{TrustedProxies: tc.trust.proxies, TrustPrivateNetworks: &tc.trust.private}}
			var facts bucket.RequestFacts
			s.policyRequestFacts(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				facts = bucket.RequestFactsFrom(r.Context())
			})).ServeHTTP(httptest.NewRecorder(), r)
			assert.Equal(t, tc.ip, facts.SourceIP)
			assert.Equal(t, tc.secure, facts.SecureTransport)
		})
	}
}
