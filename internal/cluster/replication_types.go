package cluster

import "time"

// ClusterReplicationRule represents a bucket replication rule between cluster nodes
type ClusterReplicationRule struct {
	ID                 string    `json:"id"`
	TenantID           string    `json:"tenant_id"`
	SourceBucket       string    `json:"source_bucket"`
	DestinationNodeID  string    `json:"destination_node_id"`
	DestinationBucket  string    `json:"destination_bucket"`
	SyncIntervalSeconds int      `json:"sync_interval_seconds"`
	Enabled            bool      `json:"enabled"`
	ReplicateDeletes   bool      `json:"replicate_deletes"`
	ReplicateMetadata  bool      `json:"replicate_metadata"`
	Prefix             string    `json:"prefix,omitempty"`
	Priority           int       `json:"priority"`
	LastSyncAt         *time.Time `json:"last_sync_at,omitempty"`
	LastError          string    `json:"last_error,omitempty"`
	ObjectsReplicated  int64     `json:"objects_replicated"`
	BytesReplicated    int64     `json:"bytes_replicated"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// ClusterReplicationQueueItem represents a pending replication task
type ClusterReplicationQueueItem struct {
	ID                  string    `json:"id"`
	ReplicationRuleID   string    `json:"replication_rule_id"`
	TenantID            string    `json:"tenant_id"`
	SourceBucket        string    `json:"source_bucket"`
	ObjectKey           string    `json:"object_key"`
	DestinationNodeID   string    `json:"destination_node_id"`
	DestinationBucket   string    `json:"destination_bucket"`
	Operation           string    `json:"operation"` // PUT or DELETE
	Status              string    `json:"status"`    // pending, processing, completed, failed
	Attempts            int       `json:"attempts"`
	MaxAttempts         int       `json:"max_attempts"`
	LastError           string    `json:"last_error,omitempty"`
	Priority            int       `json:"priority"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
	ScheduledAt         *time.Time `json:"scheduled_at,omitempty"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
}

// ClusterReplicationStatus represents the replication status of an object
type ClusterReplicationStatus struct {
	ID                    string    `json:"id"`
	ReplicationRuleID     string    `json:"replication_rule_id"`
	TenantID              string    `json:"tenant_id"`
	SourceBucket          string    `json:"source_bucket"`
	ObjectKey             string    `json:"object_key"`
	DestinationNodeID     string    `json:"destination_node_id"`
	DestinationBucket     string    `json:"destination_bucket"`
	SourceVersionID       string    `json:"source_version_id,omitempty"`
	DestinationVersionID  string    `json:"destination_version_id,omitempty"`
	SourceETag            string    `json:"source_etag,omitempty"`
	DestinationETag       string    `json:"destination_etag,omitempty"`
	SourceSize            int64     `json:"source_size"`
	Status                string    `json:"status"` // pending, replicated, failed
	LastSyncAt            *time.Time `json:"last_sync_at,omitempty"`
	LastError             string    `json:"last_error,omitempty"`
	ReplicatedAt          *time.Time `json:"replicated_at,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}
