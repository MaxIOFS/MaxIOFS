package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// Router handles routing of S3 operations to appropriate cluster nodes
type Router struct {
	manager           *Manager
	bucketManager     BucketManager
	replicationManager ReplicationManager
	cache             *BucketLocationCache
	proxyClient       *ProxyClient
	localNodeID       string
	log               *logrus.Entry
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
// This is determined by checking where the bucket exists (locally or on a remote node)
func (r *Router) GetBucketNode(ctx context.Context, bucket string) (*Node, error) {
	// First, check if bucket exists locally
	_, err := r.bucketManager.GetBucketTenant(ctx, bucket)
	if err == nil {
		// Bucket exists locally, return nil to indicate local node
		r.log.WithField("bucket", bucket).Debug("Bucket found locally")
		return nil, nil
	}

	// If not local, check if any remote node has it
	// For now, we'll return an error. In a full implementation, you would:
	// 1. Query all nodes for the bucket
	// 2. Return the node that has it
	// 3. Cache this information for performance

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
