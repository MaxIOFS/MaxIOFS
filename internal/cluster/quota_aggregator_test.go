package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaAggregator_queryStorageFromNode_Success(t *testing.T) {
	storageInfo := TenantStorageInfo{
		TenantID:            "tenant1",
		CurrentStorageBytes: 1024000,
		NodeID:              "node1",
		NodeName:            "node-1",
	}

	server := createMockStorageServer(t, storageInfo)
	defer server.Close()

	aggregator := &QuotaAggregator{
		clusterManager: nil,
		proxyClient:    NewProxyClient(),
	}

	localNodeID := "test-node-id"
	localNodeToken := "test-node-token"

	node := &Node{
		ID:           "node1",
		Name:         "node-1",
		Endpoint:     server.URL,
		HealthStatus: "healthy",
	}

	ctx := context.Background()
	url := fmt.Sprintf("%s/api/internal/cluster/tenant/%s/storage", node.Endpoint, "tenant1")

	req, err := aggregator.proxyClient.CreateAuthenticatedRequest(ctx, "GET", url, nil, localNodeID, localNodeToken)
	require.NoError(t, err)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req = req.WithContext(reqCtx)

	resp, err := aggregator.proxyClient.DoAuthenticatedRequest(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var response TenantStorageInfo
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, "tenant1", response.TenantID)
	assert.Equal(t, int64(1024000), response.CurrentStorageBytes)
}

func TestQuotaAggregator_queryStorageFromNode_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	aggregator := &QuotaAggregator{
		clusterManager: nil,
		proxyClient:    NewProxyClient(),
	}

	localNodeID := "test-node-id"
	localNodeToken := "test-node-token"

	node := &Node{
		ID:           "node1",
		Name:         "node-1",
		Endpoint:     server.URL,
		HealthStatus: "healthy",
	}

	ctx := context.Background()
	url := fmt.Sprintf("%s/api/internal/cluster/tenant/%s/storage", node.Endpoint, "tenant1")

	req, err := aggregator.proxyClient.CreateAuthenticatedRequest(ctx, "GET", url, nil, localNodeID, localNodeToken)
	require.NoError(t, err)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req = req.WithContext(reqCtx)

	resp, err := aggregator.proxyClient.DoAuthenticatedRequest(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestQuotaAggregator_queryStorageFromNode_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	aggregator := &QuotaAggregator{
		clusterManager: nil,
		proxyClient:    NewProxyClient(),
	}

	localNodeID := "test-node-id"
	localNodeToken := "test-node-token"

	node := &Node{
		ID:           "node1",
		Name:         "node-1",
		Endpoint:     server.URL,
		HealthStatus: "healthy",
	}

	ctx := context.Background()
	url := fmt.Sprintf("%s/api/internal/cluster/tenant/%s/storage", node.Endpoint, "tenant1")

	req, err := aggregator.proxyClient.CreateAuthenticatedRequest(ctx, "GET", url, nil, localNodeID, localNodeToken)
	require.NoError(t, err)

	reqCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	req = req.WithContext(reqCtx)

	_, err = aggregator.proxyClient.DoAuthenticatedRequest(req)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestQuotaAggregator_queryStorageFromNode_ZeroStorage(t *testing.T) {
	storageInfo := TenantStorageInfo{
		TenantID:            "tenant1",
		CurrentStorageBytes: 0,
		NodeID:              "node1",
		NodeName:            "node-1",
	}

	server := createMockStorageServer(t, storageInfo)
	defer server.Close()

	aggregator := &QuotaAggregator{
		clusterManager: nil,
		proxyClient:    NewProxyClient(),
	}

	localNodeID := "test-node-id"
	localNodeToken := "test-node-token"

	node := &Node{
		ID:           "node1",
		Name:         "node-1",
		Endpoint:     server.URL,
		HealthStatus: "healthy",
	}

	ctx := context.Background()
	url := fmt.Sprintf("%s/api/internal/cluster/tenant/%s/storage", node.Endpoint, "tenant1")

	req, err := aggregator.proxyClient.CreateAuthenticatedRequest(ctx, "GET", url, nil, localNodeID, localNodeToken)
	require.NoError(t, err)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req = req.WithContext(reqCtx)

	resp, err := aggregator.proxyClient.DoAuthenticatedRequest(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var response TenantStorageInfo
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, int64(0), response.CurrentStorageBytes)
}

func TestQuotaAggregator_queryStorageFromNode_LargeStorage(t *testing.T) {
	// Test with large storage value (10TB)
	storageInfo := TenantStorageInfo{
		TenantID:            "tenant1",
		CurrentStorageBytes: 10 * 1024 * 1024 * 1024 * 1024, // 10TB
		NodeID:              "node1",
		NodeName:            "node-1",
	}

	server := createMockStorageServer(t, storageInfo)
	defer server.Close()

	aggregator := &QuotaAggregator{
		clusterManager: nil,
		proxyClient:    NewProxyClient(),
	}

	localNodeID := "test-node-id"
	localNodeToken := "test-node-token"

	node := &Node{
		ID:           "node1",
		Name:         "node-1",
		Endpoint:     server.URL,
		HealthStatus: "healthy",
	}

	ctx := context.Background()
	url := fmt.Sprintf("%s/api/internal/cluster/tenant/%s/storage", node.Endpoint, "tenant1")

	req, err := aggregator.proxyClient.CreateAuthenticatedRequest(ctx, "GET", url, nil, localNodeID, localNodeToken)
	require.NoError(t, err)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req = req.WithContext(reqCtx)

	resp, err := aggregator.proxyClient.DoAuthenticatedRequest(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var response TenantStorageInfo
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, int64(10*1024*1024*1024*1024), response.CurrentStorageBytes)
}

func TestQuotaAggregator_queryStorageFromNode_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	aggregator := &QuotaAggregator{
		clusterManager: nil,
		proxyClient:    NewProxyClient(),
	}

	localNodeID := "test-node-id"
	localNodeToken := "test-node-token"

	node := &Node{
		ID:           "node1",
		Name:         "node-1",
		Endpoint:     server.URL,
		HealthStatus: "healthy",
	}

	ctx := context.Background()
	url := fmt.Sprintf("%s/api/internal/cluster/tenant/%s/storage", node.Endpoint, "tenant1")

	req, err := aggregator.proxyClient.CreateAuthenticatedRequest(ctx, "GET", url, nil, localNodeID, localNodeToken)
	require.NoError(t, err)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req = req.WithContext(reqCtx)

	resp, err := aggregator.proxyClient.DoAuthenticatedRequest(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	var response TenantStorageInfo
	err = json.NewDecoder(resp.Body).Decode(&response)
	assert.Error(t, err, "Should fail to decode invalid JSON")
}

func TestQuotaAggregator_TenantStorageInfo_JSON(t *testing.T) {
	storageInfo := TenantStorageInfo{
		TenantID:            "tenant123",
		CurrentStorageBytes: 5242880, // 5MB
		NodeID:              "node-abc",
		NodeName:            "primary-node",
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(storageInfo)
	require.NoError(t, err)

	// Unmarshal back
	var decoded TenantStorageInfo
	err = json.Unmarshal(jsonData, &decoded)
	require.NoError(t, err)

	// Verify fields
	assert.Equal(t, storageInfo.TenantID, decoded.TenantID)
	assert.Equal(t, storageInfo.CurrentStorageBytes, decoded.CurrentStorageBytes)
	assert.Equal(t, storageInfo.NodeID, decoded.NodeID)
	assert.Equal(t, storageInfo.NodeName, decoded.NodeName)
}

func TestQuotaAggregator_TenantStorageInfo_EmptyNodeInfo(t *testing.T) {
	// Test with empty node info (as returned by handleGetTenantStorage before aggregation)
	storageInfo := TenantStorageInfo{
		TenantID:            "tenant1",
		CurrentStorageBytes: 1024,
		NodeID:              "",
		NodeName:            "",
	}

	server := createMockStorageServer(t, storageInfo)
	defer server.Close()

	aggregator := &QuotaAggregator{
		clusterManager: nil,
		proxyClient:    NewProxyClient(),
	}

	localNodeID := "test-node-id"
	localNodeToken := "test-node-token"

	node := &Node{
		ID:           "node1",
		Name:         "node-1",
		Endpoint:     server.URL,
		HealthStatus: "healthy",
	}

	ctx := context.Background()
	url := fmt.Sprintf("%s/api/internal/cluster/tenant/%s/storage", node.Endpoint, "tenant1")

	req, err := aggregator.proxyClient.CreateAuthenticatedRequest(ctx, "GET", url, nil, localNodeID, localNodeToken)
	require.NoError(t, err)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req = req.WithContext(reqCtx)

	resp, err := aggregator.proxyClient.DoAuthenticatedRequest(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	var response TenantStorageInfo
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, "tenant1", response.TenantID)
	assert.Equal(t, int64(1024), response.CurrentStorageBytes)
	assert.Empty(t, response.NodeID)
	assert.Empty(t, response.NodeName)
}

// Helper function to create a mock storage server
func createMockStorageServer(t *testing.T, storageInfo TenantStorageInfo) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "/api/internal/cluster/tenant/")
		assert.Contains(t, r.URL.Path, "/storage")

		// Note: HMAC authentication headers are tested in ProxyClient tests
		// Here we just verify the request structure and return the expected data

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(storageInfo)
	}))
}
