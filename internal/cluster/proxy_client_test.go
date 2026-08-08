package cluster

// What the S3 proxy client bounds.
//
// This client carries object data between nodes, so anything it bounds by total
// elapsed time is really bounding file size. That distinction is the whole
// point of these tests.

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildHTTPClient_BoundsReachabilityNotTransferSize pins the shape of the
// client against the mistake it used to make.
//
// http.Client.Timeout covers the response body, so the 60 seconds it carried
// meant an object bigger than a minute of bandwidth could never cross between
// nodes — roughly 7 GB on a saturated 1 Gbps link — and failed as a timeout,
// which reads like a network fault rather than a size limit.
func TestBuildHTTPClient_BoundsReachabilityNotTransferSize(t *testing.T) {
	client := buildHTTPClient(nil)
	require.NotNil(t, client)

	assert.Zero(t, client.Timeout,
		"this client moves files; a total timeout is a cap on their size")

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)

	assert.NotZero(t, transport.ResponseHeaderTimeout,
		"waiting for the response to begin is bounded; the transfer itself is not")
	assert.NotZero(t, transport.TLSHandshakeTimeout)
	assert.NotNil(t, transport.DialContext,
		"an unreachable node must fail quickly instead of waiting for the OS")
}

// TestBuildHTTPClient_KeepsTLSAndPooling: the bounds were added to an existing
// transport, and neither the cluster TLS config nor connection reuse may be
// lost in the process.
func TestBuildHTTPClient_KeepsTLSAndPooling(t *testing.T) {
	transport, ok := buildHTTPClient(nil).Transport.(*http.Transport)
	require.True(t, ok)
	assert.NotZero(t, transport.MaxIdleConnsPerHost)
	assert.NotZero(t, transport.IdleConnTimeout)
	assert.Nil(t, transport.TLSClientConfig, "no TLS config in, none out")
}
