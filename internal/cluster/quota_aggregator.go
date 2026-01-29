package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// TenantStorageInfo represents storage usage information for a tenant on a node
type TenantStorageInfo struct {
	TenantID           string `json:"tenant_id"`
	CurrentStorageBytes int64  `json:"current_storage_bytes"`
	NodeID             string `json:"node_id"`
	NodeName           string `json:"node_name"`
}

// ClusterManagerInterface defines the interface needed by QuotaAggregator
type ClusterManagerInterface interface {
	GetHealthyNodes(ctx context.Context) ([]*Node, error)
	GetLocalNodeID(ctx context.Context) (string, error)
	GetLocalNodeToken(ctx context.Context) (string, error)
}

// QuotaAggregator aggregates storage quota usage from all cluster nodes
type QuotaAggregator struct {
	clusterManager ClusterManagerInterface
	proxyClient    *ProxyClient
	log            *logrus.Entry
}

// NewQuotaAggregator creates a new quota aggregator
func NewQuotaAggregator(clusterManager ClusterManagerInterface) *QuotaAggregator {
	return &QuotaAggregator{
		clusterManager: clusterManager,
		proxyClient:    NewProxyClient(),
		log:            logrus.WithField("component", "quota-aggregator"),
	}
}

// GetTenantTotalStorage queries all healthy nodes and aggregates tenant storage usage
// Returns the total storage bytes used by the tenant across all nodes
func (qa *QuotaAggregator) GetTenantTotalStorage(ctx context.Context, tenantID string) (int64, error) {
	startTime := time.Now()

	// Get all healthy nodes
	nodes, err := qa.clusterManager.GetHealthyNodes(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get healthy nodes: %w", err)
	}

	if len(nodes) == 0 {
		qa.log.Warn("No healthy nodes found in cluster")
		return 0, nil
	}

	qa.log.WithFields(logrus.Fields{
		"node_count": len(nodes),
		"tenant_id":  tenantID,
	}).Debug("Querying tenant storage from all cluster nodes")

	// Query all nodes in parallel
	type nodeResult struct {
		nodeID       string
		nodeName     string
		storageBytes int64
		err          error
	}

	resultsChan := make(chan nodeResult, len(nodes))
	var wg sync.WaitGroup

	for _, node := range nodes {
		wg.Add(1)
		go func(n *Node) {
			defer wg.Done()

			storageInfo, err := qa.queryStorageFromNode(ctx, n, tenantID)
			if err != nil {
				resultsChan <- nodeResult{
					nodeID:   n.ID,
					nodeName: n.Name,
					err:      err,
				}
				return
			}

			resultsChan <- nodeResult{
				nodeID:       n.ID,
				nodeName:     n.Name,
				storageBytes: storageInfo.CurrentStorageBytes,
				err:          nil,
			}
		}(node)
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results and sum storage
	var totalStorage int64
	successCount := 0
	failureCount := 0
	nodeStorageMap := make(map[string]int64)

	for result := range resultsChan {
		if result.err != nil {
			qa.log.WithError(result.err).WithFields(logrus.Fields{
				"node_id":   result.nodeID,
				"node_name": result.nodeName,
			}).Warn("Failed to query storage from node")
			failureCount++
			continue
		}

		totalStorage += result.storageBytes
		nodeStorageMap[result.nodeID] = result.storageBytes
		successCount++

		qa.log.WithFields(logrus.Fields{
			"node_id":       result.nodeID,
			"node_name":     result.nodeName,
			"storage_bytes": result.storageBytes,
		}).Debug("Received storage info from node")
	}

	duration := time.Since(startTime)

	qa.log.WithFields(logrus.Fields{
		"tenant_id":       tenantID,
		"total_storage":   totalStorage,
		"success_nodes":   successCount,
		"failed_nodes":    failureCount,
		"duration_ms":     duration.Milliseconds(),
		"node_breakdown":  nodeStorageMap,
	}).Info("Storage quota aggregation completed")

	// Return error only if ALL nodes failed
	if failureCount > 0 && successCount == 0 {
		return 0, fmt.Errorf("failed to query storage from all %d nodes", failureCount)
	}

	// If some nodes failed, log warning but return partial results
	if failureCount > 0 {
		qa.log.WithFields(logrus.Fields{
			"tenant_id":     tenantID,
			"failed_nodes":  failureCount,
			"success_nodes": successCount,
			"total_storage": totalStorage,
		}).Warn("Partial storage aggregation - some nodes failed")
	}

	return totalStorage, nil
}

// queryStorageFromNode queries storage usage from a specific node via internal API
func (qa *QuotaAggregator) queryStorageFromNode(ctx context.Context, node *Node, tenantID string) (*TenantStorageInfo, error) {
	// Get local node credentials for authentication
	localNodeID, err := qa.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get local node ID: %w", err)
	}

	localNodeToken, err := qa.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get local node token: %w", err)
	}

	// Build URL for internal cluster API
	url := fmt.Sprintf("%s/api/internal/cluster/tenant/%s/storage", node.Endpoint, tenantID)

	// Create authenticated request
	req, err := qa.proxyClient.CreateAuthenticatedRequest(ctx, "GET", url, nil, localNodeID, localNodeToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request with timeout
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req = req.WithContext(reqCtx)

	resp, err := qa.proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var storageInfo TenantStorageInfo
	if err := json.NewDecoder(resp.Body).Decode(&storageInfo); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	qa.log.WithFields(logrus.Fields{
		"node_id":       node.ID,
		"node_name":     node.Name,
		"tenant_id":     tenantID,
		"storage_bytes": storageInfo.CurrentStorageBytes,
	}).Debug("Successfully queried storage from node")

	return &storageInfo, nil
}

// GetTenantStorageByNode returns storage breakdown by node for monitoring/debugging
func (qa *QuotaAggregator) GetTenantStorageByNode(ctx context.Context, tenantID string) (map[string]int64, error) {
	nodes, err := qa.clusterManager.GetHealthyNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get healthy nodes: %w", err)
	}

	result := make(map[string]int64)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, node := range nodes {
		wg.Add(1)
		go func(n *Node) {
			defer wg.Done()

			storageInfo, err := qa.queryStorageFromNode(ctx, n, tenantID)
			if err != nil {
				qa.log.WithError(err).WithField("node_id", n.ID).Warn("Failed to get storage from node")
				return
			}

			mu.Lock()
			result[n.Name] = storageInfo.CurrentStorageBytes
			mu.Unlock()
		}(node)
	}

	wg.Wait()

	return result, nil
}
