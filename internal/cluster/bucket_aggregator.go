package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/sirupsen/logrus"
)

// BucketWithLocation represents a bucket with its cluster node location
type BucketWithLocation struct {
	Name        string            `json:"name"`
	TenantID    string            `json:"tenant_id"`
	OwnerID     string            `json:"owner_id"`
	OwnerType   string            `json:"owner_type"`
	CreatedAt   time.Time         `json:"created_at"`
	Versioning  string            `json:"versioning"`
	ObjectCount int64             `json:"object_count"`
	SizeBytes   int64             `json:"size_bytes"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	NodeID      string            `json:"node_id"`
	NodeName    string            `json:"node_name"`
	NodeStatus  string            `json:"node_status"`
}

// BucketAggregator aggregates bucket listings from all cluster nodes
type BucketAggregator struct {
	clusterManager  ClusterManagerInterface
	bucketManager   bucket.Manager
	proxyClient     *ProxyClient
	circuitBreakers *CircuitBreakerManager
	log             *logrus.Entry
}

// NewBucketAggregator creates a new bucket aggregator
func NewBucketAggregator(clusterManager ClusterManagerInterface, bucketManager bucket.Manager) *BucketAggregator {
	// Circuit breaker config:
	// - Open after 3 consecutive failures
	// - Require 2 successes to close from half-open
	// - 30 seconds timeout before retry
	circuitBreakers := NewCircuitBreakerManager(3, 2, 30*time.Second)

	return &BucketAggregator{
		clusterManager:  clusterManager,
		bucketManager:   bucketManager,
		proxyClient:     NewProxyClient(),
		circuitBreakers: circuitBreakers,
		log:             logrus.WithField("component", "bucket-aggregator"),
	}
}

// ListAllBuckets queries all healthy nodes and aggregates bucket listings
func (ba *BucketAggregator) ListAllBuckets(ctx context.Context, tenantID string) ([]BucketWithLocation, error) {
	startTime := time.Now()

	// ALWAYS start with local buckets (source of truth)
	localBuckets, err := ba.bucketManager.ListBuckets(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to list local buckets: %w", err)
	}

	// Get local node info for metadata
	localNodeID, _ := ba.clusterManager.GetLocalNodeID(ctx)
	localNodeName, _ := ba.clusterManager.GetLocalNodeName(ctx)

	// Convert local buckets to BucketWithLocation
	allBuckets := make([]BucketWithLocation, 0, len(localBuckets))
	for _, b := range localBuckets {
		allBuckets = append(allBuckets, BucketWithLocation{
			Name:        b.Name,
			TenantID:    b.TenantID,
			OwnerID:     b.OwnerID,
			OwnerType:   b.OwnerType,
			CreatedAt:   b.CreatedAt,
			ObjectCount: b.ObjectCount,
			SizeBytes:   b.TotalSize,
			Metadata:    b.Metadata,
			Tags:        b.Tags,
			NodeID:      localNodeID,
			NodeName:    localNodeName,
			NodeStatus:  "local",
		})
	}

	ba.log.WithFields(logrus.Fields{
		"local_buckets": len(allBuckets),
		"tenant_id":     tenantID,
	}).Debug("Listed local buckets")

	// Get all healthy nodes (excluding self)
	nodes, err := ba.clusterManager.GetHealthyNodes(ctx)
	if err != nil {
		ba.log.WithError(err).Warn("Failed to get healthy nodes, returning local buckets only")
		return allBuckets, nil
	}

	if len(nodes) == 0 {
		ba.log.Debug("No remote nodes in cluster, returning local buckets only")
		return allBuckets, nil
	}

	ba.log.WithFields(logrus.Fields{
		"node_count": len(nodes),
		"tenant_id":  tenantID,
	}).Debug("Querying buckets from all cluster nodes")

	// Query all nodes in parallel
	type nodeResult struct {
		nodeID   string
		nodeName string
		buckets  []BucketWithLocation
		err      error
	}

	resultsChan := make(chan nodeResult, len(nodes))
	var wg sync.WaitGroup

	for _, node := range nodes {
		wg.Add(1)
		go func(n *Node) {
			defer wg.Done()

			// Get circuit breaker for this node
			circuitBreaker := ba.circuitBreakers.GetBreaker(n.ID)

			// Wrap query in circuit breaker
			var buckets []BucketWithLocation
			err := circuitBreaker.Call(func() error {
				b, err := ba.queryBucketsFromNode(ctx, n, tenantID)
				if err != nil {
					return err
				}
				buckets = b
				return nil
			})

			if err == ErrCircuitOpen {
				ba.log.WithFields(logrus.Fields{
					"node_id":   n.ID,
					"node_name": n.Name,
				}).Warn("Circuit breaker open for node, skipping query")
			}

			resultsChan <- nodeResult{
				nodeID:   n.ID,
				nodeName: n.Name,
				buckets:  buckets,
				err:      err,
			}
		}(node)
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// Collect results from remote nodes and add to local buckets
	successCount := 0
	failureCount := 0
	remoteBucketsCount := 0

	for result := range resultsChan {
		if result.err != nil {
			ba.log.WithError(result.err).WithFields(logrus.Fields{
				"node_id":   result.nodeID,
				"node_name": result.nodeName,
			}).Warn("Failed to query buckets from remote node")
			failureCount++
			continue
		}

		// Add node information to each bucket
		for i := range result.buckets {
			result.buckets[i].NodeID = result.nodeID
			result.buckets[i].NodeName = result.nodeName
			result.buckets[i].NodeStatus = "remote"
		}

		allBuckets = append(allBuckets, result.buckets...)
		remoteBucketsCount += len(result.buckets)
		successCount++
	}

	duration := time.Since(startTime)

	ba.log.WithFields(logrus.Fields{
		"total_buckets":  len(allBuckets),
		"local_buckets":  len(allBuckets) - remoteBucketsCount,
		"remote_buckets": remoteBucketsCount,
		"success_nodes":  successCount,
		"failed_nodes":   failureCount,
		"duration_ms":    duration.Milliseconds(),
	}).Info("Bucket aggregation completed")

	// Note: We always have local buckets, so never return error
	// Even if all remote nodes fail, local buckets are still available
	return allBuckets, nil
}

// queryBucketsFromNode queries bucket list from a specific node via internal API
func (ba *BucketAggregator) queryBucketsFromNode(ctx context.Context, node *Node, tenantID string) ([]BucketWithLocation, error) {
	// Get local node credentials for authentication
	localNodeID, err := ba.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get local node ID: %w", err)
	}

	localNodeToken, err := ba.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get local node token: %w", err)
	}

	// Build URL for internal cluster API
	url := fmt.Sprintf("%s/api/internal/cluster/buckets?tenant_id=%s", node.Endpoint, tenantID)

	// Create authenticated request
	req, err := ba.proxyClient.CreateAuthenticatedRequest(ctx, "GET", url, nil, localNodeID, localNodeToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request with timeout
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req = req.WithContext(reqCtx)

	resp, err := ba.proxyClient.DoAuthenticatedRequest(req)
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
	var response struct {
		Buckets []BucketWithLocation `json:"buckets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	ba.log.WithFields(logrus.Fields{
		"node_id":      node.ID,
		"node_name":    node.Name,
		"bucket_count": len(response.Buckets),
	}).Debug("Successfully queried buckets from node")

	return response.Buckets, nil
}

// IsLocalNode checks if a node is the local node
func (ba *BucketAggregator) IsLocalNode(ctx context.Context, nodeID string) (bool, error) {
	localNodeID, err := ba.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		return false, err
	}
	return localNodeID == nodeID, nil
}

// GetCircuitBreakerStats returns statistics for all circuit breakers
func (ba *BucketAggregator) GetCircuitBreakerStats() map[string]interface{} {
	return ba.circuitBreakers.GetAllStats()
}
