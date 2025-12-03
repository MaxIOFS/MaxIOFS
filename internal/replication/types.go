package replication

import (
	"time"
)

// ReplicationStatus represents the state of a replication operation
type ReplicationStatus string

const (
	StatusPending    ReplicationStatus = "pending"
	StatusInProgress ReplicationStatus = "in_progress"
	StatusCompleted  ReplicationStatus = "completed"
	StatusFailed     ReplicationStatus = "failed"
	StatusRetrying   ReplicationStatus = "retrying"
)

// ReplicationMode defines how replication is triggered
type ReplicationMode string

const (
	ModeRealTime  ReplicationMode = "realtime"  // Replicate immediately on object change
	ModeScheduled ReplicationMode = "scheduled" // Replicate on schedule
	ModeBatch     ReplicationMode = "batch"     // Replicate in batches
)

// ConflictResolution defines how to handle conflicts
type ConflictResolution string

const (
	ConflictLWW         ConflictResolution = "last_write_wins"  // Use timestamp
	ConflictVersionBased ConflictResolution = "version_based"   // Use version number
	ConflictPrimaryWins  ConflictResolution = "primary_wins"    // Primary always wins
)

// ReplicationRule defines a replication configuration
type ReplicationRule struct {
	ID                    string             `json:"id"`
	TenantID              string             `json:"tenant_id"`
	SourceBucket          string             `json:"source_bucket"`
	DestinationEndpoint   string             `json:"destination_endpoint"`           // S3 endpoint URL (e.g., https://s3.amazonaws.com, http://localhost:8080)
	DestinationBucket     string             `json:"destination_bucket"`
	DestinationAccessKey  string             `json:"destination_access_key"`
	DestinationSecretKey  string             `json:"destination_secret_key"`
	DestinationRegion     string             `json:"destination_region,omitempty"`   // S3 region (e.g., us-east-1)
	Prefix                string             `json:"prefix,omitempty"`
	Enabled               bool               `json:"enabled"`
	Priority              int                `json:"priority"`
	Mode                  ReplicationMode    `json:"mode"`
	ScheduleInterval      int                `json:"schedule_interval,omitempty"`    // Interval in minutes for scheduled mode
	ConflictResolution    ConflictResolution `json:"conflict_resolution"`
	ReplicateDeletes      bool               `json:"replicate_deletes"`
	ReplicateMetadata     bool               `json:"replicate_metadata"`
	CreatedAt             time.Time          `json:"created_at"`
	UpdatedAt             time.Time          `json:"updated_at"`
}

// QueueItem represents an item in the replication queue
type QueueItem struct {
	ID            int64             `json:"id"`
	RuleID        string            `json:"rule_id"`
	TenantID      string            `json:"tenant_id"`
	Bucket        string            `json:"bucket"`
	ObjectKey     string            `json:"object_key"`
	VersionID     string            `json:"version_id,omitempty"`
	Action        string            `json:"action"` // PUT, DELETE, COPY
	Status        ReplicationStatus `json:"status"`
	Attempts      int               `json:"attempts"`
	MaxRetries    int               `json:"max_retries"`
	LastError     string            `json:"last_error,omitempty"`
	ScheduledAt   time.Time         `json:"scheduled_at"`
	ProcessedAt   *time.Time        `json:"processed_at,omitempty"`
	CompletedAt   *time.Time        `json:"completed_at,omitempty"`
	BytesReplicated int64           `json:"bytes_replicated"`
}

// ReplicationStatus tracks the status of replicated objects
type ReplicationStatusRecord struct {
	ID                int64             `json:"id"`
	RuleID            string            `json:"rule_id"`
	TenantID          string            `json:"tenant_id"`
	SourceBucket      string            `json:"source_bucket"`
	SourceKey         string            `json:"source_key"`
	SourceVersionID   string            `json:"source_version_id,omitempty"`
	DestinationBucket string            `json:"destination_bucket"`
	DestinationKey    string            `json:"destination_key"`
	Status            ReplicationStatus `json:"status"`
	LastAttempt       time.Time         `json:"last_attempt"`
	ReplicatedAt      *time.Time        `json:"replicated_at,omitempty"`
	ErrorMessage      string            `json:"error_message,omitempty"`
}

// ReplicationMetrics contains statistics about replication
type ReplicationMetrics struct {
	RuleID          string    `json:"rule_id"`
	TenantID        string    `json:"tenant_id"`
	TotalObjects    int64     `json:"total_objects"`
	PendingObjects  int64     `json:"pending_objects"`
	CompletedObjects int64    `json:"completed_objects"`
	FailedObjects   int64     `json:"failed_objects"`
	BytesReplicated int64     `json:"bytes_replicated"`
	LastSuccess     *time.Time `json:"last_success,omitempty"`
	LastFailure     *time.Time `json:"last_failure,omitempty"`
}

// ReplicationConfig contains configuration for the replication manager
type ReplicationConfig struct {
	Enable           bool          `json:"enable"`
	WorkerCount      int           `json:"worker_count"`
	QueueSize        int           `json:"queue_size"`
	BatchSize        int           `json:"batch_size"`
	RetryInterval    time.Duration `json:"retry_interval"`
	MaxRetries       int           `json:"max_retries"`
	CleanupInterval  time.Duration `json:"cleanup_interval"`
	RetentionDays    int           `json:"retention_days"`
}
