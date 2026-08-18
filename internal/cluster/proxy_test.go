package cluster

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type readCountingReader struct {
	data  *strings.Reader
	reads int
}

func (r *readCountingReader) Read(p []byte) (int, error) {
	r.reads++
	return r.data.Read(p)
}

func TestProxyClient_ProxyRequest(t *testing.T) {
	// Create mock target server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify proxied headers
		assert.Equal(t, "true", r.Header.Get("X-MaxIOFS-Proxied"))
		assert.Equal(t, "test-node-1", r.Header.Get("X-MaxIOFS-Proxy-Node"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/test", r.URL.Path)

		// Read body
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, "test body", string(body))

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer targetServer.Close()

	node := &Node{
		ID:       "test-node-1",
		Name:     "Test Node 1",
		Endpoint: targetServer.URL,
	}

	// Create original request
	originalReq, err := http.NewRequest("POST", "http://localhost:8080/api/test?key=value", bytes.NewBufferString("test body"))
	require.NoError(t, err)
	originalReq.Header.Set("Content-Type", "application/json")
	originalReq.Header.Set("Connection", "keep-alive") // hop-by-hop header, should be filtered

	proxyClient := NewProxyClient(nil)
	ctx := context.Background()

	resp, err := proxyClient.ProxyRequest(ctx, node, originalReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, `{"status":"ok"}`, string(body))
}

func TestProxyClient_ProxyRequest_PreservesContentLength(t *testing.T) {
	payload := strings.Repeat("x", 8192)
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, int64(len(payload)), r.ContentLength)

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, payload, string(body))

		w.WriteHeader(http.StatusNoContent)
	}))
	defer targetServer.Close()

	node := &Node{
		ID:       "test-node-1",
		Name:     "Test Node 1",
		Endpoint: targetServer.URL,
	}

	originalReq, err := http.NewRequest("PUT", "http://localhost:8080/bucket/object", io.NopCloser(strings.NewReader(payload)))
	require.NoError(t, err)
	originalReq.ContentLength = int64(len(payload))

	proxyClient := NewProxyClient(nil)
	resp, err := proxyClient.ProxyRequest(context.Background(), node, originalReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestProxyClient_CreateAuthenticatedRequest_DoesNotDrainStreamingBody(t *testing.T) {
	body := &readCountingReader{data: strings.NewReader("streaming payload")}
	proxyClient := NewProxyClient(nil)

	req, err := proxyClient.CreateAuthenticatedRequest(
		context.Background(),
		http.MethodPut,
		"http://node.local/api/internal/cluster/ha/objects/key",
		body,
		"node-1",
		"node-token",
	)
	require.NoError(t, err)

	assert.Equal(t, 0, body.reads)
	assert.Equal(t, clusterUnsignedPayloadHash, req.Header.Get(clusterBodySHA256Header))

	data, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	assert.Equal(t, "streaming payload", string(data))
}

func TestClusterProxyAuthBindsForwardedIdentityAndRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodPut, "http://node.local/bucket/key?uploads=", nil)
	req.Header.Set(clusterBodySHA256Header, clusterUnsignedPayloadHash)
	req.Header.Set("X-MaxIOFS-Proxied", "true")
	AddClusterProxyHeaders(req, "node-1", "cluster-token", "user-1", "tenant-1", "user")

	userID, tenantID, roles, ok := ValidateClusterProxyAuth(req, "cluster-token")
	require.True(t, ok)
	assert.Equal(t, "user-1", userID)
	assert.Equal(t, "tenant-1", tenantID)
	assert.Equal(t, "user", roles)

	tamperedRoles := req.Clone(context.Background())
	tamperedRoles.Header = req.Header.Clone()
	tamperedRoles.Header.Set("X-MaxIOFS-Forwarded-Roles", "admin")
	_, _, _, ok = ValidateClusterProxyAuth(tamperedRoles, "cluster-token")
	assert.False(t, ok, "forwarded roles must be covered by the proxy HMAC")

	tamperedPath := req.Clone(context.Background())
	tamperedPath.Header = req.Header.Clone()
	tamperedPath.URL.Path = "/other/key"
	_, _, _, ok = ValidateClusterProxyAuth(tamperedPath, "cluster-token")
	assert.False(t, ok, "request URI must be covered by the proxy HMAC")
}

func TestProxyClient_ProxyRequest_ServerError(t *testing.T) {
	// Create mock target server that returns error
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}))
	defer targetServer.Close()

	node := &Node{
		ID:       "test-node-1",
		Name:     "Test Node 1",
		Endpoint: targetServer.URL,
	}

	originalReq, err := http.NewRequest("GET", "http://localhost:8080/api/test", nil)
	require.NoError(t, err)

	proxyClient := NewProxyClient(nil)
	ctx := context.Background()

	resp, err := proxyClient.ProxyRequest(ctx, node, originalReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestProxyClient_CopyResponseToWriter(t *testing.T) {
	// Create mock response
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":    []string{"application/json"},
			"X-Custom-Header": []string{"custom-value"},
			"Connection":      []string{"keep-alive"}, // hop-by-hop, should be filtered
		},
		Body: io.NopCloser(bytes.NewBufferString(`{"result":"success"}`)),
	}

	// Create response recorder
	w := httptest.NewRecorder()

	proxyClient := NewProxyClient(nil)
	err := proxyClient.CopyResponseToWriter(w, resp)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, "custom-value", w.Header().Get("X-Custom-Header"))
	assert.Equal(t, `{"result":"success"}`, w.Body.String())

	// Verify hop-by-hop header was NOT copied (it should be filtered by copyHeaders)
	assert.Empty(t, w.Header().Get("Connection"))
}

func TestProxyClient_ProxyAndWrite(t *testing.T) {
	// Create mock target server
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("accepted"))
	}))
	defer targetServer.Close()

	node := &Node{
		ID:       "test-node-1",
		Name:     "Test Node 1",
		Endpoint: targetServer.URL,
	}

	originalReq, err := http.NewRequest("PUT", "http://localhost:8080/api/data", bytes.NewBufferString("data"))
	require.NoError(t, err)

	proxyClient := NewProxyClient(nil)
	ctx := context.Background()
	w := httptest.NewRecorder()

	err = proxyClient.ProxyAndWrite(ctx, w, originalReq, node)
	require.NoError(t, err)

	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.Equal(t, "text/plain", w.Header().Get("Content-Type"))
	assert.Equal(t, "accepted", w.Body.String())
}

func TestCopyHeaders(t *testing.T) {
	src := http.Header{
		"Content-Type":      []string{"application/json"},
		"X-Custom":          []string{"value1", "value2"},
		"Connection":        []string{"keep-alive"}, // hop-by-hop
		"connection":        []string{"close"},      // hop-by-hop with non-canonical casing
		"Transfer-Encoding": []string{"chunked"},    // hop-by-hop
		"Authorization":     []string{"Bearer token"},
	}

	dst := http.Header{}

	copyHeaders(dst, src)

	// Regular headers should be copied
	assert.Equal(t, "application/json", dst.Get("Content-Type"))
	assert.Equal(t, "Bearer token", dst.Get("Authorization"))

	// Multiple values should be preserved
	assert.Equal(t, []string{"value1", "value2"}, dst["X-Custom"])

	// Hop-by-hop headers should NOT be copied
	assert.Empty(t, dst.Get("Connection"))
	assert.Empty(t, dst.Get("Transfer-Encoding"))
	assert.NotContains(t, dst, "connection")
}

func TestIsHopByHopHeader(t *testing.T) {
	tests := []struct {
		header   string
		expected bool
	}{
		{"Connection", true},
		{"connection", true},
		{"Keep-Alive", true},
		{"keep-alive", true},
		{"Proxy-Authenticate", true},
		{"Proxy-Authorization", true},
		{"Te", true},
		{"te", true},
		{"Trailers", true},
		{"Transfer-Encoding", true},
		{"transfer-encoding", true},
		{"Upgrade", true},
		{"upgrade", true},
		{"Content-Type", false},
		{"Authorization", false},
		{"X-Custom-Header", false},
		{"Host", false},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			result := isHopByHopHeader(tt.header)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestProxyClient_ProxyRequest_InvalidNode(t *testing.T) {
	node := &Node{
		ID:       "invalid-node",
		Name:     "Invalid Node",
		Endpoint: "http://invalid-host-that-does-not-exist-12345:9999",
	}

	originalReq, err := http.NewRequest("GET", "http://localhost:8080/api/test", nil)
	require.NoError(t, err)

	proxyClient := NewProxyClient(nil)
	ctx := context.Background()

	_, err = proxyClient.ProxyRequest(ctx, node, originalReq)
	assert.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "failed to proxy request")
}
