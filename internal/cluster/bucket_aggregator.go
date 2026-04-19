package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/sirupsen/logrus"
)

// BucketWithLocation represents a bucket with its cluster node location
type BucketWithLocation struct {
	Name        string                    `json:"name"`
	TenantID    string                    `json:"tenant_id"`
	OwnerID     string                    `json:"owner_id"`
	OwnerType   string                    `json:"owner_type"`
	CreatedAt   time.Time                 `json:"created_at"`
	Versioning  string                    `json:"versioning"`
	ObjectCount int64                     `json:"object_count"`
	SizeBytes   int64                     `json:"size_bytes"`
	ObjectLock  *bucket.ObjectLockConfig  `json:"object_lock,omitempty"`
	Encryption  *bucket.EncryptionConfig  `json:"encryption,omitempty"`
	Metadata    map[string]string         `json:"metadata,omitempty"`
	Tags        map[string]string         `json:"tags,omitempty"`
	NodeID      string                    `json:"node_id"`
	NodeName    string                    `json:"node_name"`
	NodeStatus  string                    `json:"node_status"`
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
		proxyClient:     NewDynamicProxyClient(clusterManager.GetTLSConfig),
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
			ObjectLock:  b.ObjectLock,
			Encryption:  b.Encryption,
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

	// Each node is authoritative for its own buckets only.
	// factor > 1 (HA): every node holds a full copy — local listing is complete.
	// factor <= 1 (independent): each node owns its own buckets — local only.
	// Cross-node aggregation is not done here: showing remote buckets via the local
	// S3 API causes empty-bucket confusion because objects live on the remote node.
	ba.log.WithFields(logrus.Fields{
		"local_buckets": len(allBuckets),
		"duration_ms":   time.Since(startTime).Milliseconds(),
	}).Debug("Bucket listing completed (local only)")
	return allBuckets, nil
}

// ListAllBucketsFromAllNodes queries local buckets plus all healthy peer nodes and
// returns a merged list tagged with NodeID/NodeName. This is intended for the
// management console UI only — the S3 API must NOT use this method because
// objects on remote nodes are inaccessible via the local S3 endpoint.
func (ba *BucketAggregator) ListAllBucketsFromAllNodes(ctx context.Context, tenantID string) ([]BucketWithLocation, error) {
	startTime := time.Now()

	// Start with local buckets
	localBuckets, err := ba.bucketManager.ListBuckets(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to list local buckets: %w", err)
	}
	localNodeID, _ := ba.clusterManager.GetLocalNodeID(ctx)
	localNodeName, _ := ba.clusterManager.GetLocalNodeName(ctx)

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
			ObjectLock:  b.ObjectLock,
			Encryption:  b.Encryption,
			Metadata:    b.Metadata,
			Tags:        b.Tags,
			NodeID:      localNodeID,
			NodeName:    localNodeName,
			NodeStatus:  "local",
		})
	}

	// Query all healthy peer nodes in parallel
	peerNodes, err := ba.clusterManager.GetHealthyNodes(ctx)
	if err != nil {
		ba.log.WithError(err).Warn("Failed to get peer nodes; returning local buckets only")
		return allBuckets, nil
	}

	type result struct {
		buckets []BucketWithLocation
		node    *Node
		err     error
	}
	ch := make(chan result, len(peerNodes))

	queried := 0
	for _, node := range peerNodes {
		if node.ID == localNodeID {
			continue // skip self
		}
		queried++
		go func(n *Node) {
			buckets, err := ba.queryBucketsFromNode(ctx, n, tenantID)
			if err != nil {
				ba.log.WithError(err).WithField("node", n.Name).Warn("Failed to query buckets from peer node")
				ch <- result{nil, n, err}
				return
			}
			// Tag each bucket with the remote node's info
			for i := range buckets {
				buckets[i].NodeID = n.ID
				buckets[i].NodeName = n.Name
				buckets[i].NodeStatus = "remote"
			}
			ch <- result{buckets, n, nil}
		}(node)
	}

	for i := 0; i < queried; i++ {
		res := <-ch
		if res.err == nil {
			allBuckets = append(allBuckets, res.buckets...)
		}
	}

	ba.log.WithFields(logrus.Fields{
		"total_buckets": len(allBuckets),
		"local_buckets": len(localBuckets),
		"peer_nodes":    queried,
		"duration_ms":   time.Since(startTime).Milliseconds(),
	}).Debug("Full cluster bucket listing completed")

	return allBuckets, nil
}

// deduplicateBucketsByTenantAndName returns one entry per (TenantID, Name).
// Prefers the entry with NodeStatus == "local"; otherwise keeps the first by NodeID.
func deduplicateBucketsByTenantAndName(buckets []BucketWithLocation) []BucketWithLocation {
	type key struct {
		tenantID string
		name     string
	}
	byKey := make(map[key]BucketWithLocation)
	for _, b := range buckets {
		k := key{tenantID: b.TenantID, name: b.Name}
		existing, ok := byKey[k]
		if !ok {
			byKey[k] = b
			continue
		}
		// Prefer local; if both local or both remote, keep first (existing).
		if b.NodeStatus == "local" && existing.NodeStatus != "local" {
			byKey[k] = b
		}
		// If existing is local, keep it. If both remote, keep existing (first seen).
	}
	out := make([]BucketWithLocation, 0, len(byKey))
	for _, b := range byKey {
		out = append(out, b)
	}
	return out
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
