package audit

import "context"

// Event Types - Authentication Events
const (
	EventTypeLoginSuccess      = "login_success"
	EventTypeLoginFailed       = "login_failed"
	EventTypeLogout            = "logout"
	EventTypeUserBlocked       = "user_blocked"
	EventTypeUserUnblocked     = "user_unblocked"
	EventTypePasswordChanged   = "password_changed"
	EventType2FAEnabled        = "2fa_enabled"
	EventType2FADisabled       = "2fa_disabled"
	EventType2FAVerifySuccess  = "2fa_verify_success"
	EventType2FAVerifyFailed   = "2fa_verify_failed"
)

// Event Types - User Management Events
const (
	EventTypeUserCreated       = "user_created"
	EventTypeUserDeleted       = "user_deleted"
	EventTypeUserUpdated       = "user_updated"
	EventTypeUserRoleChanged   = "user_role_changed"
	EventTypeUserStatusChanged = "user_status_changed"
)

// Event Types - Bucket Management Events
const (
	EventTypeBucketCreated = "bucket_created"
	EventTypeBucketDeleted = "bucket_deleted"
)

// Event Types - Access Key Events
const (
	EventTypeAccessKeyCreated       = "access_key_created"
	EventTypeAccessKeyDeleted       = "access_key_deleted"
	EventTypeAccessKeyStatusChanged = "access_key_status_changed"
)

// Event Types - Tenant Management Events
const (
	EventTypeTenantCreated = "tenant_created"
	EventTypeTenantDeleted = "tenant_deleted"
	EventTypeTenantUpdated = "tenant_updated"
)

// Resource Types
const (
	ResourceTypeUser      = "user"
	ResourceTypeBucket    = "bucket"
	ResourceTypeAccessKey = "access_key"
	ResourceTypeTenant    = "tenant"
	ResourceTypeSystem    = "system"
)

// Actions
const (
	ActionCreate  = "create"
	ActionDelete  = "delete"
	ActionUpdate  = "update"
	ActionLogin   = "login"
	ActionLogout  = "logout"
	ActionBlock   = "block"
	ActionUnblock = "unblock"
	ActionEnable  = "enable"
	ActionDisable = "disable"
	ActionVerify  = "verify"
)

// Status
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
)

// AuditEvent represents a single audit log event to be recorded
type AuditEvent struct {
	TenantID     string                 // Tenant ID (empty for global events)
	UserID       string                 // User who performed the action
	Username     string                 // Username for display
	EventType    string                 // Event category (see Event Types constants)
	ResourceType string                 // Type of resource affected
	ResourceID   string                 // ID of affected resource
	ResourceName string                 // Name of affected resource (for display)
	Action       string                 // Action performed (create, delete, update, etc.)
	Status       string                 // success or failed
	IPAddress    string                 // Client IP address
	UserAgent    string                 // Client user agent
	Details      map[string]interface{} // Additional details (stored as JSON)
}

// AuditLog represents a stored audit log record
type AuditLog struct {
	ID           int64                  `json:"id"`
	Timestamp    int64                  `json:"timestamp"`     // Unix timestamp (seconds)
	TenantID     string                 `json:"tenant_id"`     // Empty for global events
	UserID       string                 `json:"user_id"`       // User who performed the action
	Username     string                 `json:"username"`      // Username for display
	EventType    string                 `json:"event_type"`    // Event category
	ResourceType string                 `json:"resource_type"` // Type of resource affected
	ResourceID   string                 `json:"resource_id"`   // ID of affected resource
	ResourceName string                 `json:"resource_name"` // Name of affected resource
	Action       string                 `json:"action"`        // Action performed
	Status       string                 `json:"status"`        // success or failed
	IPAddress    string                 `json:"ip_address"`    // Client IP address
	UserAgent    string                 `json:"user_agent"`    // Client user agent
	Details      map[string]interface{} `json:"details"`       // Additional details
	CreatedAt    int64                  `json:"created_at"`    // Record creation timestamp
}

// AuditLogFilters for querying logs
type AuditLogFilters struct {
	TenantID     string // Filter by tenant ID
	UserID       string // Filter by user ID
	EventType    string // Filter by event type
	ResourceType string // Filter by resource type
	Action       string // Filter by action
	Status       string // Filter by status (success/failed)
	StartDate    int64  // Filter by start date (Unix timestamp)
	EndDate      int64  // Filter by end date (Unix timestamp)
	Page         int    // Page number (1-based)
	PageSize     int    // Results per page
}

// Store defines the interface for audit log storage
type Store interface {
	// LogEvent records an audit event
	LogEvent(ctx context.Context, event *AuditEvent) error

	// GetLogs retrieves audit logs with filters (for global admin)
	GetLogs(ctx context.Context, filters *AuditLogFilters) ([]*AuditLog, int, error)

	// GetLogsByTenant retrieves logs for a specific tenant (for tenant admin)
	GetLogsByTenant(ctx context.Context, tenantID string, filters *AuditLogFilters) ([]*AuditLog, int, error)

	// GetLogByID retrieves a single log entry
	GetLogByID(ctx context.Context, id int64) (*AuditLog, error)

	// PurgeLogs deletes logs older than specified days (maintenance)
	PurgeLogs(ctx context.Context, olderThanDays int) (int, error)

	// Close closes the store
	Close() error
}
