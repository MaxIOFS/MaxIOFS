package cluster

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// Router handles routing of S3 operations to appropriate cluster nodes
type Router struct {
	manager            *Manager
	bucketManager      BucketManager
	replicationManager ReplicationManager
	cache              *BucketLocationCache
	proxyClient        *ProxyClient
	localNodeID        string
	log                *logrus.Entry
	readCounter        uint64 // atomic counter for read round-robin
}

// BucketManager interface for bucket operations (to avoid circular dependencies)
type BucketManager interface {
	// GetBucketTenant returns the tenant ID for a bucket
	GetBucketTenant(ctx context.Context, bucket string) (string, error)
	// BucketExists checks if a bucket exists
	BucketExists(ctx context.Context, tenant, bucket string) (bool, error)
}

// ReplicationManager interface for replication operations
type ReplicationManager interface {
	// GetReplicationRules returns replication rules for a bucket
	GetReplicationRules(ctx context.Context, tenantID, bucket string) ([]ReplicationRule, error)
}

// ReplicationRule represents a replication rule (simplified interface)
type ReplicationRule struct {
	ID                  string
	DestinationEndpoint string
	DestinationBucket   string
	Enabled             bool
}

// NewRouter creates a new router
func NewRouter(manager *Manager, bucketMgr BucketManager, replMgr ReplicationManager, localNodeID string) *Router {
	return &Router{
		manager:           manager,
		bucketManager:     bucketMgr,
		replicationManager: replMgr,
		cache:             NewBucketLocationCache(5 * time.Minute), // 5 min TTL
		proxyClient:       NewProxyClient(manager.GetTLSConfig()),
		localNodeID:       localNodeID,
		log:               logrus.WithField("component", "cluster-router"),
	}
}

// GetBucketNode returns the primary node for a bucket
// This is determined by checking where the bucket exists (locally or on a remote node).
// Returns nil when the bucket is on the local node.
func (r *Router) GetBucketNode(ctx context.Context, bucket string) (*Node, error) {
	// First, check if bucket exists locally
	_, err := r.bucketManager.GetBucketTenant(ctx, bucket)
	if err == nil {
		// Bucket exists locally, return nil to indicate local node
		r.log.WithField("bucket", bucket).Debug("Bucket found locally")
		return nil, nil
	}

	// Bucket not found locally — query remote nodes in parallel
	localNodeID, _ := r.manager.GetLocalNodeID(ctx)
	nodes, err := r.manager.GetHealthyNodes(ctx)
	if err != nil || len(nodes) == 0 {
		// No healthy nodes to query (cluster not initialized or no peers)
		return nil, fmt.Errorf("bucket not found on any node: %s", bucket)
	}

	localNodeToken, err := r.manager.GetLocalNodeToken(ctx)
	if err != nil {
		// Can't authenticate cluster requests — treat as bucket not found
		return nil, fmt.Errorf("bucket not found on any node: %s", bucket)
	}

	type result struct {
		node *Node
		err  error
	}
	queried := 0
	ch := make(chan result, len(nodes))

	for _, node := range nodes {
		if node.ID == localNodeID {
			continue // already checked locally
		}
		queried++
		go func(n *Node) {
			reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()

			url := fmt.Sprintf("%s/api/internal/cluster/bucket-exists/%s", n.Endpoint, bucket)
			req, reqErr := r.proxyClient.CreateAuthenticatedRequest(reqCtx, http.MethodGet, url, nil, localNodeID, localNodeToken)
			if reqErr != nil {
				ch <- result{nil, reqErr}
				return
			}
			resp, doErr := r.proxyClient.DoAuthenticatedRequest(req)
			if doErr != nil {
				ch <- result{nil, doErr}
				return
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ch <- result{n, nil}
			} else {
				ch <- result{nil, nil} // not found on this node, not an error
			}
		}(node)
	}

	for i := 0; i < queried; i++ {
		res := <-ch
		if res.err != nil {
			r.log.WithError(res.err).Debug("Error querying remote node for bucket existence")
			continue
		}
		if res.node != nil {
			r.log.WithFields(logrus.Fields{
				"bucket": bucket,
				"node":   res.node.Name,
			}).Debug("Bucket found on remote node")
			// Cache the result
			r.cache.Set(bucket, res.node.ID)
			return res.node, nil
		}
	}

	return nil, fmt.Errorf("bucket not found on any node: %s", bucket)
}

// GetBucketReplicas returns all nodes that have replicas of this bucket
func (r *Router) GetBucketReplicas(ctx context.Context, bucket string) ([]*Node, error) {
	// Get tenant for the bucket
	tenant, err := r.bucketManager.GetBucketTenant(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to get bucket tenant: %w", err)
	}

	// Get replication rules for this bucket
	rules, err := r.replicationManager.GetReplicationRules(ctx, tenant, bucket)
	if err != nil {
		r.log.WithError(err).Warn("Failed to get replication rules")
		return nil, nil
	}

	if len(rules) == 0 {
		// No replication rules, no replicas
		return nil, nil
	}

	// Get all nodes
	allNodes, err := r.manager.ListNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	// Match destination endpoints from replication rules with cluster nodes
	var replicaNodes []*Node
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		// Find node with matching endpoint
		for _, node := range allNodes {
			if node.Endpoint == rule.DestinationEndpoint {
				replicaNodes = append(replicaNodes, node)
				break
			}
		}
	}

	return replicaNodes, nil
}

// GetHealthyNodeForBucket returns a healthy node for the bucket (primary or replica)
func (r *Router) GetHealthyNodeForBucket(ctx context.Context, bucket string) (*Node, error) {
	// 1. Try to get primary node
	primaryNode, err := r.GetBucketNode(ctx, bucket)
	if err != nil {
		return nil, err
	}

	// If primary is local (nil), check if this node is healthy
	if primaryNode == nil {
		// Local node - assume healthy
		return nil, nil
	}

	// 2. Check if primary node is healthy
	if r.isNodeHealthy(primaryNode) {
		return primaryNode, nil
	}

	// 3. Primary is unhealthy, try replicas
	replicas, err := r.GetBucketReplicas(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to get bucket replicas: %w", err)
	}

	if len(replicas) == 0 {
		return nil, fmt.Errorf("primary node unhealthy and no replicas available")
	}

	// 4. Find first healthy replica
	for _, replica := range replicas {
		if r.isNodeHealthy(replica) {
			r.log.WithFields(logrus.Fields{
				"bucket":       bucket,
				"primary_node": primaryNode.Name,
				"replica_node": replica.Name,
			}).Warn("Primary node unhealthy, using replica")
			return replica, nil
		}
	}

	// 5. No healthy nodes available
	return nil, fmt.Errorf("no healthy nodes available for bucket: %s", bucket)
}

// isNodeHealthy checks if a node is healthy
func (r *Router) isNodeHealthy(node *Node) bool {
	if node == nil {
		return false
	}
	return node.HealthStatus == HealthStatusHealthy
}

// ShouldRouteToRemoteNode determines if a request should be routed to a remote node
func (r *Router) ShouldRouteToRemoteNode(ctx context.Context, bucket string) (bool, *Node, error) {
	// Check if cluster is enabled
	if !r.manager.IsClusterEnabled() {
		return false, nil, nil
	}

	// Get the node for this bucket
	node, err := r.GetHealthyNodeForBucket(ctx, bucket)
	if err != nil {
		return false, nil, err
	}

	// If node is nil, it means bucket is local
	if node == nil {
		return false, nil, nil
	}

	// Route to remote node
	return true, node, nil
}

// RouteRequest routes an S3 request to the appropriate node with caching
// This is the main entry point for proxying requests
func (r *Router) RouteRequest(ctx context.Context, bucket string) (*Node, bool, error) {
	// Check if cluster is enabled
	if !r.manager.IsClusterEnabled() {
		return nil, true, nil // true = bucket is local
	}

	// 1. Check cache first
	cachedNodeID := r.cache.Get(bucket)
	if cachedNodeID != "" {
		if cachedNodeID == r.localNodeID {
			// Cache says bucket is local
			r.log.WithField("bucket", bucket).Debug("Cache hit: bucket is local")
			return nil, true, nil
		}

		// Cache says bucket is on remote node
		node, err := r.manager.GetNode(ctx, cachedNodeID)
		if err == nil && r.isNodeHealthy(node) {
			r.log.WithFields(logrus.Fields{
				"bucket": bucket,
				"node":   node.Name,
			}).Debug("Cache hit: routing to remote node")
			return node, false, nil
		}

		// Node not found or unhealthy, invalidate cache
		r.cache.Delete(bucket)
	}

	// 2. Cache miss - check if bucket exists locally
	_, err := r.bucketManager.GetBucketTenant(ctx, bucket)
	if err == nil {
		// Bucket exists locally
		r.cache.Set(bucket, r.localNodeID)
		r.log.WithField("bucket", bucket).Debug("Bucket found locally, updating cache")
		return nil, true, nil
	}

	// 3. Bucket not local - find it in the cluster
	node, err := r.GetHealthyNodeForBucket(ctx, bucket)
	if err != nil {
		return nil, false, err
	}

	if node == nil {
		// Should not happen, but handle it
		return nil, false, fmt.Errorf("bucket not found in cluster: %s", bucket)
	}

	// Update cache
	r.cache.Set(bucket, node.ID)
	r.log.WithFields(logrus.Fields{
		"bucket": bucket,
		"node":   node.Name,
	}).Debug("Bucket found on remote node, updating cache")

	return node, false, nil
}

// InvalidateCache removes a bucket from the cache
// Call this when a bucket is deleted or moved
func (r *Router) InvalidateCache(bucket string) {
	r.cache.Delete(bucket)
}

// GetCacheStats returns cache statistics
func (r *Router) GetCacheStats() map[string]interface{} {
	return r.cache.GetStats()
}

// SelectReadNode selects the best node to serve a read request for the given bucket.
// Returns nil when the local node should handle the request.
// When factor > 1 and ready replicas exist, the local node and all ready replicas
// participate in a round-robin rotation to distribute read load.
// Falls back to nil (local) if no ready replicas are available.
func (r *Router) SelectReadNode(ctx context.Context, bucket string) (*Node, error) {
	if !r.manager.IsClusterEnabled() {
		return nil, nil
	}
	factor, err := r.manager.GetReplicationFactor(ctx)
	if err != nil || factor <= 1 {
		return nil, nil
	}
	replicas, err := r.manager.GetReadyReplicaNodes(ctx)
	if err != nil || len(replicas) == 0 {
		// No ready replicas yet — serve locally.
		return nil, nil
	}

	// Build the candidate list: local (nil) + ready replicas.
	// Slot 0 always represents the local node.
	total := uint64(1 + len(replicas))
	idx := atomic.AddUint64(&r.readCounter, 1) % total
	if idx == 0 {
		// Local node's turn.
		return nil, nil
	}
	selected := replicas[idx-1]
	r.log.WithFields(logrus.Fields{
		"bucket":  bucket,
		"node_id": selected.ID,
		"slot":    idx,
		"total":   total,
	}).Debug("read load balancing: routing to replica")
	return selected, nil
}
