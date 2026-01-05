package inventory

import (
	"fmt"
	"time"
)

// InventoryConfig represents a bucket inventory configuration
type InventoryConfig struct {
	ID                string    `json:"id"`
	BucketName        string    `json:"bucket_name"`
	TenantID          string    `json:"tenant_id,omitempty"`
	Enabled           bool      `json:"enabled"`
	Frequency         string    `json:"frequency"` // "daily" or "weekly"
	Format            string    `json:"format"`    // "csv" or "json"
	DestinationBucket string    `json:"destination_bucket"`
	DestinationPrefix string    `json:"destination_prefix"`
	IncludedFields    []string  `json:"included_fields"`
	ScheduleTime      string    `json:"schedule_time"` // HH:MM format
	LastRunAt         *int64    `json:"last_run_at,omitempty"`
	NextRunAt         *int64    `json:"next_run_at,omitempty"`
	CreatedAt         int64     `json:"created_at"`
	UpdatedAt         int64     `json:"updated_at"`
}

// InventoryReport represents a generated inventory report
type InventoryReport struct {
	ID           string  `json:"id"`
	ConfigID     string  `json:"config_id"`
	BucketName   string  `json:"bucket_name"`
	ReportPath   string  `json:"report_path"`
	ObjectCount  int64   `json:"object_count"`
	TotalSize    int64   `json:"total_size"`
	Status       string  `json:"status"` // "pending", "completed", "failed"
	StartedAt    *int64  `json:"started_at,omitempty"`
	CompletedAt  *int64  `json:"completed_at,omitempty"`
	ErrorMessage *string `json:"error_message,omitempty"`
	CreatedAt    int64   `json:"created_at"`
}

// ObjectInventoryItem represents a single object in an inventory report
type ObjectInventoryItem struct {
	Bucket              string `json:"bucket" csv:"Bucket"`
	Key                 string `json:"key" csv:"Key"`
	VersionID           string `json:"version_id,omitempty" csv:"VersionId"`
	IsLatest            bool   `json:"is_latest" csv:"IsLatest"`
	Size                int64  `json:"size" csv:"Size"`
	LastModified        string `json:"last_modified" csv:"LastModifiedDate"`
	ETag                string `json:"etag" csv:"ETag"`
	StorageClass        string `json:"storage_class" csv:"StorageClass"`
	IsMultipartUploaded bool   `json:"is_multipart_uploaded" csv:"IsMultipartUploaded"`
	EncryptionStatus    string `json:"encryption_status" csv:"EncryptionStatus"`
	ReplicationStatus   string `json:"replication_status,omitempty" csv:"ReplicationStatus"`
	ObjectACL           string `json:"object_acl,omitempty" csv:"ObjectACL"`
}

// Available field names for inventory configuration
const (
	FieldBucketName           = "bucket_name"
	FieldObjectKey            = "object_key"
	FieldVersionID            = "version_id"
	FieldIsLatest             = "is_latest"
	FieldSize                 = "size"
	FieldLastModified         = "last_modified"
	FieldETag                 = "etag"
	FieldStorageClass         = "storage_class"
	FieldIsMultipartUploaded  = "is_multipart_uploaded"
	FieldEncryptionStatus     = "encryption_status"
	FieldReplicationStatus    = "replication_status"
	FieldObjectACL            = "object_acl"
)

// DefaultIncludedFields returns the default set of fields for inventory reports
func DefaultIncludedFields() []string {
	return []string{
		FieldBucketName,
		FieldObjectKey,
		FieldSize,
		FieldLastModified,
		FieldETag,
		FieldStorageClass,
	}
}

// AllAvailableFields returns all available fields for inventory reports
func AllAvailableFields() []string {
	return []string{
		FieldBucketName,
		FieldObjectKey,
		FieldVersionID,
		FieldIsLatest,
		FieldSize,
		FieldLastModified,
		FieldETag,
		FieldStorageClass,
		FieldIsMultipartUploaded,
		FieldEncryptionStatus,
		FieldReplicationStatus,
		FieldObjectACL,
	}
}

// ValidateIncludedFields validates that all provided fields are valid
func ValidateIncludedFields(fields []string) bool {
	validFields := make(map[string]bool)
	for _, field := range AllAvailableFields() {
		validFields[field] = true
	}

	for _, field := range fields {
		if !validFields[field] {
			return false
		}
	}
	return true
}

// CalculateNextRunTime calculates the next run time based on frequency and schedule
func CalculateNextRunTime(frequency, scheduleTime string, lastRun *int64) (int64, error) {
	// Validate frequency
	if frequency != "daily" && frequency != "weekly" {
		return 0, fmt.Errorf("invalid frequency: must be 'daily' or 'weekly'")
	}

	now := time.Now()

	// Parse schedule time (HH:MM format)
	t, err := time.Parse("15:04", scheduleTime)
	if err != nil {
		return 0, err
	}

	hour, minute := t.Hour(), t.Minute()

	// Start with today at the scheduled time
	nextRun := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())

	// If the time has already passed today, move to tomorrow
	if nextRun.Before(now) {
		nextRun = nextRun.Add(24 * time.Hour)
	}

	// For weekly frequency, if we already ran this week, move to next week
	if frequency == "weekly" && lastRun != nil {
		lastRunTime := time.Unix(*lastRun, 0)
		// If last run was less than 7 days ago, schedule for next week
		if now.Sub(lastRunTime) < 7*24*time.Hour {
			daysUntilNext := 7 - int(now.Weekday()-lastRunTime.Weekday())
			if daysUntilNext <= 0 {
				daysUntilNext += 7
			}
			nextRun = time.Date(now.Year(), now.Month(), now.Day()+daysUntilNext, hour, minute, 0, 0, now.Location())
		}
	}

	return nextRun.Unix(), nil
}
