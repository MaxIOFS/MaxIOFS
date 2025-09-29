package object

import (
	"context"
	"fmt"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
)

// RetentionPolicyManager handles retention policies and lifecycle management
type RetentionPolicyManager interface {
	// Retention Policy Management
	CalculateRetentionDate(baseTime time.Time, years int, days int) time.Time
	ExtendRetentionPeriod(ctx context.Context, bucket, key string, newDate time.Time, user *auth.User) error
	ValidateRetentionModification(existing, new *ObjectLockRetention, user *auth.User) error

	// Lifecycle Management
	IsObjectEligibleForDeletion(ctx context.Context, bucket, key string) (bool, error)
	GetExpiringObjects(ctx context.Context, bucket string, beforeTime time.Time) ([]Object, error)
	CleanupExpiredRetentions(ctx context.Context, bucket string) (int, error)

	// Compliance Enforcement
	EnforceRetentionCompliance(ctx context.Context, bucket, key string, user *auth.User) error
	ValidateComplianceDelete(ctx context.Context, bucket, key string, user *auth.User) error
	AuditRetentionAction(ctx context.Context, bucket, key, action string, user *auth.User, details map[string]string) error

	// Retention Reporting
	GetRetentionSummary(ctx context.Context, bucket string) (*RetentionSummary, error)
	GenerateComplianceReport(ctx context.Context, bucket string, startTime, endTime time.Time) (*ComplianceReport, error)
}

// RetentionSummary provides an overview of retention status for a bucket
type RetentionSummary struct {
	Bucket                string    `json:"bucket"`
	TotalObjects          int64     `json:"total_objects"`
	ObjectsWithRetention  int64     `json:"objects_with_retention"`
	ObjectsWithLegalHold  int64     `json:"objects_with_legal_hold"`
	GovernanceObjects     int64     `json:"governance_objects"`
	ComplianceObjects     int64     `json:"compliance_objects"`
	EarliestRetention     time.Time `json:"earliest_retention,omitempty"`
	LatestRetention       time.Time `json:"latest_retention,omitempty"`
	GeneratedAt           time.Time `json:"generated_at"`
}

// ComplianceReport provides detailed compliance information
type ComplianceReport struct {
	Bucket      string              `json:"bucket"`
	StartTime   time.Time           `json:"start_time"`
	EndTime     time.Time           `json:"end_time"`
	Objects     []ComplianceObject  `json:"objects"`
	Summary     RetentionSummary    `json:"summary"`
	Violations  []ComplianceEvent   `json:"violations,omitempty"`
	GeneratedAt time.Time           `json:"generated_at"`
}

// ComplianceObject represents an object's compliance status
type ComplianceObject struct {
	Key               string                 `json:"key"`
	Retention         *ObjectLockRetention   `json:"retention,omitempty"`
	LegalHold         *ObjectLockLegalHold   `json:"legal_hold,omitempty"`
	LastModified      time.Time              `json:"last_modified"`
	ComplianceStatus  string                 `json:"compliance_status"` // active, expired, violated
	DaysUntilExpiry   int                    `json:"days_until_expiry"`
}

// ComplianceEvent represents a compliance-related event
type ComplianceEvent struct {
	ID          string            `json:"id"`
	Timestamp   time.Time         `json:"timestamp"`
	EventType   string            `json:"event_type"` // retention_set, retention_extended, legal_hold_set, violation
	Bucket      string            `json:"bucket"`
	Key         string            `json:"key"`
	UserID      string            `json:"user_id"`
	Details     map[string]string `json:"details"`
	Severity    string            `json:"severity"` // info, warning, error, critical
}

// retentionPolicyManager implements RetentionPolicyManager
type retentionPolicyManager struct {
	objectManager Manager
	objectLocker  ObjectLocker
}

// NewRetentionPolicyManager creates a new RetentionPolicyManager
func NewRetentionPolicyManager(objectManager Manager, objectLocker ObjectLocker) RetentionPolicyManager {
	return &retentionPolicyManager{
		objectManager: objectManager,
		objectLocker:  objectLocker,
	}
}

// CalculateRetentionDate calculates a retention date from a base time
func (rpm *retentionPolicyManager) CalculateRetentionDate(baseTime time.Time, years int, days int) time.Time {
	retentionDate := baseTime

	if years > 0 {
		retentionDate = retentionDate.AddDate(years, 0, 0)
	}

	if days > 0 {
		retentionDate = retentionDate.AddDate(0, 0, days)
	}

	return retentionDate
}

// ExtendRetentionPeriod extends the retention period for an object
func (rpm *retentionPolicyManager) ExtendRetentionPeriod(ctx context.Context, bucket, key string, newDate time.Time, user *auth.User) error {
	// Get current retention
	currentRetention, err := rpm.objectLocker.GetObjectRetention(ctx, bucket, key, user)
	if err != nil {
		return fmt.Errorf("failed to get current retention: %w", err)
	}

	// Validate that new date is later than current date
	if newDate.Before(currentRetention.RetainUntilDate) {
		return fmt.Errorf("new retention date cannot be earlier than current retention date")
	}

	// Create new retention configuration
	newRetention := &ObjectLockRetention{
		Mode:            currentRetention.Mode,
		RetainUntilDate: newDate,
	}

	// Update retention
	if err := rpm.objectLocker.PutObjectRetention(ctx, bucket, key, newRetention, false, user); err != nil {
		return fmt.Errorf("failed to extend retention: %w", err)
	}

	// Audit the extension
	details := map[string]string{
		"old_retention_date": currentRetention.RetainUntilDate.Format(time.RFC3339),
		"new_retention_date": newDate.Format(time.RFC3339),
		"mode":               currentRetention.Mode,
	}

	return rpm.AuditRetentionAction(ctx, bucket, key, "retention_extended", user, details)
}

// ValidateRetentionModification validates if a retention modification is allowed
func (rpm *retentionPolicyManager) ValidateRetentionModification(existing, new *ObjectLockRetention, user *auth.User) error {
	if existing == nil {
		// No existing retention, any valid new retention is allowed
		return rpm.objectLocker.ValidateRetention(new)
	}

	// Validate new retention configuration
	if err := rpm.objectLocker.ValidateRetention(new); err != nil {
		return err
	}

	// Check mode changes
	if existing.Mode != new.Mode {
		// Mode changes are generally not allowed
		// Exception: GOVERNANCE to COMPLIANCE is allowed (upgrade)
		if existing.Mode == RetentionModeGovernance && new.Mode == RetentionModeCompliance {
			// Allowed upgrade
		} else {
			return fmt.Errorf("cannot change retention mode from %s to %s", existing.Mode, new.Mode)
		}
	}

	// Check date modifications
	if new.RetainUntilDate.Before(existing.RetainUntilDate) {
		// Shortening retention period
		if existing.Mode == RetentionModeCompliance {
			return ErrCannotShortenCompliance
		}

		if existing.Mode == RetentionModeGovernance {
			// Check bypass permissions
			hasBypassPermission := false
			for _, role := range user.Roles {
				if role == "admin" {
					hasBypassPermission = true
					break
				}
			}

			if !hasBypassPermission {
				return ErrCannotShortenGovernance
			}
		}
	}

	return nil
}

// IsObjectEligibleForDeletion checks if an object is eligible for deletion
func (rpm *retentionPolicyManager) IsObjectEligibleForDeletion(ctx context.Context, bucket, key string) (bool, error) {
	// Use GetObjectMetadata since we only need metadata, not content
	obj, err := rpm.objectManager.GetObjectMetadata(ctx, bucket, key)
	if err != nil {
		return false, fmt.Errorf("failed to get object metadata: %w", err)
	}

	// Check legal hold
	if obj.LegalHold != nil && rpm.objectLocker.IsLegalHoldActiveInternal(obj.LegalHold) {
		return false, nil
	}

	// Check retention
	if obj.Retention != nil && rpm.objectLocker.IsRetentionActiveInternal(obj.Retention) {
		return false, nil
	}

	return true, nil
}

// GetExpiringObjects returns objects whose retention is expiring before the specified time
func (rpm *retentionPolicyManager) GetExpiringObjects(ctx context.Context, bucket string, beforeTime time.Time) ([]Object, error) {
	// List all objects in the bucket
	objects, _, err := rpm.objectManager.ListObjects(ctx, bucket, "", "", "", 1000)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	var expiringObjects []Object
	for _, obj := range objects {
		if obj.Retention != nil {
			if obj.Retention.RetainUntilDate.Before(beforeTime) &&
			   obj.Retention.RetainUntilDate.After(time.Now()) {
				expiringObjects = append(expiringObjects, obj)
			}
		}
	}

	return expiringObjects, nil
}

// CleanupExpiredRetentions removes retention from objects whose retention period has expired
func (rpm *retentionPolicyManager) CleanupExpiredRetentions(ctx context.Context, bucket string) (int, error) {
	// List all objects in the bucket
	objects, _, err := rpm.objectManager.ListObjects(ctx, bucket, "", "", "", 1000)
	if err != nil {
		return 0, fmt.Errorf("failed to list objects: %w", err)
	}

	cleaned := 0
	now := time.Now()

	for _, obj := range objects {
		if obj.Retention != nil && obj.Retention.RetainUntilDate.Before(now) {
			// Retention has expired, remove it by setting it to nil
			if err := rpm.objectManager.SetObjectRetention(ctx, obj.Bucket, obj.Key, nil); err != nil {
				// Log error but continue with other objects
				continue
			}
			cleaned++
		}
	}

	return cleaned, nil
}

// EnforceRetentionCompliance ensures retention policies are being followed
func (rpm *retentionPolicyManager) EnforceRetentionCompliance(ctx context.Context, bucket, key string, user *auth.User) error {
	obj, err := rpm.objectManager.GetObjectMetadata(ctx, bucket, key)
	if err != nil {
		return fmt.Errorf("failed to get object metadata: %w", err)
	}

	// Check if object is under retention
	if obj.Retention != nil && rpm.objectLocker.IsRetentionActiveInternal(obj.Retention) {
		return ErrObjectLocked
	}

	// Check if object is under legal hold
	if obj.LegalHold != nil && rpm.objectLocker.IsLegalHoldActiveInternal(obj.LegalHold) {
		return ErrObjectUnderLegalHold
	}

	return nil
}

// ValidateComplianceDelete validates if a delete operation is compliant
func (rpm *retentionPolicyManager) ValidateComplianceDelete(ctx context.Context, bucket, key string, user *auth.User) error {
	return rpm.objectLocker.CanDeleteObject(ctx, bucket, key, user)
}

// AuditRetentionAction logs a retention-related action for compliance
func (rpm *retentionPolicyManager) AuditRetentionAction(ctx context.Context, bucket, key, action string, user *auth.User, details map[string]string) error {
	// Create audit event
	event := ComplianceEvent{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()), // Simple ID generation
		Timestamp: time.Now(),
		EventType: action,
		Bucket:    bucket,
		Key:       key,
		UserID:    user.ID,
		Details:   details,
		Severity:  "info",
	}

	// In production, this would be logged to an audit system
	// For MVP, we'll just return success
	_ = event // Use the event variable to avoid unused variable error

	return nil
}

// GetRetentionSummary provides a summary of retention status for a bucket
func (rpm *retentionPolicyManager) GetRetentionSummary(ctx context.Context, bucket string) (*RetentionSummary, error) {
	objects, _, err := rpm.objectManager.ListObjects(ctx, bucket, "", "", "", 10000) // Large limit for summary
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	summary := &RetentionSummary{
		Bucket:      bucket,
		GeneratedAt: time.Now(),
	}

	var earliestRetention, latestRetention time.Time

	for _, obj := range objects {
		summary.TotalObjects++

		if obj.Retention != nil {
			summary.ObjectsWithRetention++

			switch obj.Retention.Mode {
			case RetentionModeGovernance:
				summary.GovernanceObjects++
			case RetentionModeCompliance:
				summary.ComplianceObjects++
			}

			// Track earliest and latest retention dates
			retentionDate := obj.Retention.RetainUntilDate
			if earliestRetention.IsZero() || retentionDate.Before(earliestRetention) {
				earliestRetention = retentionDate
			}
			if latestRetention.IsZero() || retentionDate.After(latestRetention) {
				latestRetention = retentionDate
			}
		}

		if obj.LegalHold != nil && rpm.objectLocker.IsLegalHoldActiveInternal(obj.LegalHold) {
			summary.ObjectsWithLegalHold++
		}
	}

	if !earliestRetention.IsZero() {
		summary.EarliestRetention = earliestRetention
	}
	if !latestRetention.IsZero() {
		summary.LatestRetention = latestRetention
	}

	return summary, nil
}

// GenerateComplianceReport generates a detailed compliance report
func (rpm *retentionPolicyManager) GenerateComplianceReport(ctx context.Context, bucket string, startTime, endTime time.Time) (*ComplianceReport, error) {
	objects, _, err := rpm.objectManager.ListObjects(ctx, bucket, "", "", "", 10000)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	summary, err := rpm.GetRetentionSummary(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to generate summary: %w", err)
	}

	report := &ComplianceReport{
		Bucket:      bucket,
		StartTime:   startTime,
		EndTime:     endTime,
		Summary:     *summary,
		GeneratedAt: time.Now(),
	}

	now := time.Now()

	for _, obj := range objects {
		complianceObj := ComplianceObject{
			Key:              obj.Key,
			LastModified:     obj.LastModified,
			ComplianceStatus: "none",
		}

		if obj.Retention != nil {
			complianceObj.Retention = &ObjectLockRetention{
				Mode:            obj.Retention.Mode,
				RetainUntilDate: obj.Retention.RetainUntilDate,
			}

			if obj.Retention.RetainUntilDate.After(now) {
				complianceObj.ComplianceStatus = "active"
				complianceObj.DaysUntilExpiry = int(obj.Retention.RetainUntilDate.Sub(now).Hours() / 24)
			} else {
				complianceObj.ComplianceStatus = "expired"
			}
		}

		if obj.LegalHold != nil {
			complianceObj.LegalHold = &ObjectLockLegalHold{
				Status: obj.LegalHold.Status,
			}

			if rpm.objectLocker.IsLegalHoldActiveInternal(obj.LegalHold) {
				complianceObj.ComplianceStatus = "legal_hold"
			}
		}

		report.Objects = append(report.Objects, complianceObj)
	}

	return report, nil
}