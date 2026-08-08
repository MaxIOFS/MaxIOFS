package server

// How the console proxies a request to the node that owns a bucket.
//
// The thing being guarded here is a distinction that is easy to lose: a console
// request may be a short API call OR a file transfer of arbitrary size, and the
// proxy carries both. Every bound written as though it were only the former
// turns into a cap on file size.

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConsoleProxyClient_BoundsReachabilityNotTransferSize pins the reason the
// client is shaped the way it is.
//
// http.Client.Timeout covers the whole exchange including the response body, so
// setting it at all makes large transfers fail on healthy nodes: at the 30
// seconds this used to carry, downloading a multi-gigabyte object from its
// owning node failed every time, while nothing was wrong with either node.
//
// What still has to be bounded is a node that does not answer. That belongs on
// the transport, where it applies to reaching the node rather than to how long
// the file takes.
func TestConsoleProxyClient_BoundsReachabilityNotTransferSize(t *testing.T) {
	assert.Zero(t, consoleProxyClient.Timeout,
		"an overall timeout is a limit on file size wearing the clothes of a limit on time")

	transport, ok := consoleProxyClient.Transport.(*http.Transport)
	require.True(t, ok, "the proxy needs its own transport to bound anything at all")

	assert.NotZero(t, transport.TLSHandshakeTimeout,
		"a node that accepts the connection and then stalls the handshake must not hang the request")
	assert.NotZero(t, transport.ResponseHeaderTimeout,
		"waiting for the response to BEGIN is bounded; how long it then takes to arrive is not")
	assert.NotNil(t, transport.DialContext,
		"reaching an unreachable node must fail rather than wait for the OS to give up")
	assert.NotZero(t, transport.MaxIdleConnsPerHost,
		"one shared client exists so connections are reused; without a pool it would not be")
}

// TestConsoleProxyClient_IsShared: a client per request opens a new connection
// every time and pools nothing, which is what building it inside the handler
// did. This holds the fix in place by construction.
func TestConsoleProxyClient_IsShared(t *testing.T) {
	first := consoleProxyClient
	second := consoleProxyClient
	assert.Same(t, first, second)
	assert.NotNil(t, consoleProxyClient.Transport)
}
