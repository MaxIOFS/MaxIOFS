package cluster

import "time"

// Node represents a cluster node
type Node struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Endpoint      string    `json:"endpoint"`
	NodeToken     string    `json:"-"` // Never expose in JSON
	Region        string    `json:"region"`
	Priority      int       `json:"priority"`
	HealthStatus  string    `json:"health_status"`
	LastHealthCheck *time.Time `json:"last_health_check"`
	LastSeen      *time.Time `json:"last_seen"`
	LatencyMs     int       `json:"latency_ms"`
	CapacityTotal int64     `json:"capacity_total"`
	CapacityUsed  int64     `json:"capacity_used"`
	BucketCount   int       `json:"bucket_count"`
	Metadata           string     `json:"metadata"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	IsStale            bool       `json:"is_stale"`
	LastLocalWriteAt   *time.Time `json:"last_local_write_at,omitempty"`
}

// HealthStatus represents node health status
type HealthStatus struct {
	NodeID       string    `json:"node_id"`
	Status       string    `json:"status"` // healthy, degraded, unavailable, unknown
	LatencyMs    int       `json:"latency_ms"`
	LastCheck    time.Time `json:"last_check"`
	ErrorMessage string    `json:"error_message,omitempty"`
}

// ClusterStatus represents overall cluster status
type ClusterStatus struct {
	IsEnabled         bool      `json:"is_enabled"`
	TotalNodes        int       `json:"total_nodes"`
	HealthyNodes      int       `json:"healthy_nodes"`
	DegradedNodes     int       `json:"degraded_nodes"`
	UnavailableNodes  int       `json:"unavailable_nodes"`
	TotalBuckets      int       `json:"total_buckets"`
	ReplicatedBuckets int       `json:"replicated_buckets"`
	LocalBuckets      int       `json:"local_buckets"`
	LastUpdated       time.Time `json:"last_updated"`
}

// ClusterConfig represents this node's cluster configuration
type ClusterConfig struct {
	NodeID           string    `json:"node_id"`
	NodeName         string    `json:"node_name"`
	ClusterToken     string    `json:"-"` // Never expose in JSON
	IsClusterEnabled bool      `json:"is_cluster_enabled"`
	Region           string    `json:"region"`
	CreatedAt        time.Time `json:"created_at"`
}

// ClusterInfo contains information about a cluster for validation
type ClusterInfo struct {
	ClusterID string `json:"cluster_id"`
	Region    string `json:"region"`
	NodeCount int    `json:"node_count"`
}

// HealthCheckResult is returned by health check operations
type HealthCheckResult struct {
	Healthy      bool   `json:"healthy"`
	LatencyMs    int    `json:"latency_ms"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// Health status constants
const (
	HealthStatusHealthy     = "healthy"
	HealthStatusDegraded    = "degraded"
	HealthStatusUnavailable = "unavailable"
	HealthStatusUnknown     = "unknown"
)
