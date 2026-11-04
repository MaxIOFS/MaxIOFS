package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/stretchr/testify/assert"
)

// TestForwardedTransport: a signed forwarded request that reached its first
// node over HTTPS is taken as secure; a request that only claims it, without
// a signed forwarding, loses the claim.
func TestForwardedTransport(t *testing.T) {
	plain := bucket.WithRequestFacts(context.Background(), bucket.RequestFacts{SourceIP: "10.0.0.2"})

	r := httptest.NewRequest("PUT", "/b/k", nil)
	assert.False(t, bucket.RequestFactsFrom(withForwardedTransport(plain, r)).SecureTransport)

	r.Header.Set(cluster.SecureTransportHeader, "true")
	facts := bucket.RequestFactsFrom(withForwardedTransport(plain, r))
	assert.True(t, facts.SecureTransport)
	assert.Equal(t, "10.0.0.2", facts.SourceIP, "the other facts are kept")

	cluster.StripInternalClusterHeaders(r)
	assert.Empty(t, r.Header.Get(cluster.SecureTransportHeader), "an unsigned request loses the claim")
}
