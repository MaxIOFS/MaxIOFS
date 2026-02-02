package object

import (
	"context"
	"fmt"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
)

// ObjectLock constants
const (
	RetentionModeGovernance = "GOVERNANCE"
	RetentionModeCompliance = "COMPLIANCE"
)

// ObjectLocker defines the interface for Object Lock operations
type ObjectLocker interface {
	// Object Lock Configuration
	PutObjectLock(ctx context.Context, bucket, key string, retention *ObjectLockRetention, legalHold *ObjectLockLegalHold, user *auth.User) error
	GetObjectRetention(ctx context.Context, bucket, key string, user *auth.User) (*ObjectLockRetention, error)
	PutObjectRetention(ctx context.Context, bucket, key string, retention *ObjectLockRetention, bypassGovernance bool, user *auth.User) error

	// Legal Hold Operations
	GetObjectLegalHold(ctx context.Context, bucket, key string, user *auth.User) (*ObjectLockLegalHold, error)
	PutObjectLegalHold(ctx context.Context, bucket, key string, legalHold *ObjectLockLegalHold, user *auth.User) error

	// Validation and Enforcement
	CanDeleteObject(ctx context.Context, bucket, key string, user *auth.User) error
	CanModifyObject(ctx context.Context, bucket, key string, user *auth.User) error
	ValidateRetention(retention *ObjectLockRetention) error
	ValidateLegalHold(legalHold *ObjectLockLegalHold) error

	// Default Retention
	SetDefaultRetention(ctx context.Context, bucket string, retention *ObjectLockRetention, user *auth.User) error
	GetDefaultRetention(ctx context.Context, bucket string, user *auth.User) (*ObjectLockRetention, error)

	// Compliance and Audit
	IsRetentionActive(retention *ObjectLockRetention) bool
	IsLegalHoldActive(legalHold *ObjectLockLegalHold) bool
	GetRetentionExpiryTime(retention *ObjectLockRetention) time.Time

	// Internal helpers for retention manager
	IsRetentionActiveInternal(retention *RetentionConfig) bool
	IsLegalHoldActiveInternal(legalHold *LegalHoldConfig) bool
}

// BucketConfigManager defines the interface for bucket configuration operations needed by ObjectLocker
type BucketConfigManager interface {
	GetObjectLockConfig(ctx context.Context, tenantID, name string) (*bucket.ObjectLockConfig, error)
	SetObjectLockConfig(ctx context.Context, tenantID, name string, config *bucket.ObjectLockConfig) error
}

// objectLock implements the ObjectLocker interface
type objectLock struct {
	manager       Manager
	bucketManager BucketConfigManager
}

// NewObjectLocker creates a new ObjectLocker instance
// bucketManager is optional and can be nil if bucket-level operations are not needed
func NewObjectLocker(manager Manager, bucketManager BucketConfigManager) ObjectLocker {
	return &objectLock{
		manager:       manager,
		bucketManager: bucketManager,
	}
}

// PutObjectLock sets Object Lock configuration (retention and/or legal hold) on an object
func (ol *objectLock) PutObjectLock(ctx context.Context, bucket, key string, retention *ObjectLockRetention, legalHold *ObjectLockLegalHold, user *auth.User) error {
	// Validate retention if provided
	if retention != nil {
		if err := ol.ValidateRetention(retention); err != nil {
			return fmt.Errorf("invalid retention configuration: %w", err)
		}

		// Check if existing retention can be modified
		existing, err := ol.manager.GetObjectRetention(ctx, bucket, key)
		if err == nil && existing != nil && ol.isRetentionActiveInternal(existing) {
			if err := ol.canModifyRetention(existing, retention, user); err != nil {
				return err
			}
		}

		// Convert to internal format and set
		internalRetention := &RetentionConfig{
			Mode:            retention.Mode,
			RetainUntilDate: retention.RetainUntilDate,
		}

		if err := ol.manager.SetObjectRetention(ctx, bucket, key, internalRetention); err != nil {
			return fmt.Errorf("failed to set retention: %w", err)
		}
	}

	// Validate and set legal hold if provided
	if legalHold != nil {
		if err := ol.ValidateLegalHold(legalHold); err != nil {
			return fmt.Errorf("invalid legal hold configuration: %w", err)
		}

		// Convert to internal format and set
		internalLegalHold := &LegalHoldConfig{
			Status: legalHold.Status,
		}

		if err := ol.manager.SetObjectLegalHold(ctx, bucket, key, internalLegalHold); err != nil {
			return fmt.Errorf("failed to set legal hold: %w", err)
		}
	}

	return nil
}

// GetObjectRetention retrieves the retention configuration for an object
func (ol *objectLock) GetObjectRetention(ctx context.Context, bucket, key string, user *auth.User) (*ObjectLockRetention, error) {
	retention, err := ol.manager.GetObjectRetention(ctx, bucket, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get object retention: %w", err)
	}

	if retention == nil {
		return nil, ErrNoRetentionConfiguration
	}

	return &ObjectLockRetention{
		Mode:            retention.Mode,
		RetainUntilDate: retention.RetainUntilDate,
	}, nil
}

// PutObjectRetention sets or updates the retention configuration for an object
func (ol *objectLock) PutObjectRetention(ctx context.Context, bucket, key string, retention *ObjectLockRetention, bypassGovernance bool, user *auth.User) error {
	if err := ol.ValidateRetention(retention); err != nil {
		return fmt.Errorf("invalid retention configuration: %w", err)
	}

	// Check if existing retention can be modified
	existing, err := ol.manager.GetObjectRetention(ctx, bucket, key)
	if err == nil && existing != nil && ol.isRetentionActiveInternal(existing) {
		if err := ol.canModifyRetention(existing, retention, user); err != nil {
			if !bypassGovernance {
				return err
			}

			// Check if user has bypass permissions
			if err := ol.checkBypassGovernancePermission(user); err != nil {
				return err
			}
		}
	}

	// Update retention
	newRetention := &RetentionConfig{
		Mode:            retention.Mode,
		RetainUntilDate: retention.RetainUntilDate,
	}

	return ol.manager.SetObjectRetention(ctx, bucket, key, newRetention)
}

// GetObjectLegalHold retrieves the legal hold status for an object
func (ol *objectLock) GetObjectLegalHold(ctx context.Context, bucket, key string, user *auth.User) (*ObjectLockLegalHold, error) {
	legalHold, err := ol.manager.GetObjectLegalHold(ctx, bucket, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get object legal hold: %w", err)
	}

	if legalHold == nil {
		return &ObjectLockLegalHold{Status: LegalHoldStatusOff}, nil
	}

	return &ObjectLockLegalHold{
		Status: legalHold.Status,
	}, nil
}

// PutObjectLegalHold sets or updates the legal hold status for an object
func (ol *objectLock) PutObjectLegalHold(ctx context.Context, bucket, key string, legalHold *ObjectLockLegalHold, user *auth.User) error {
	if err := ol.ValidateLegalHold(legalHold); err != nil {
		return fmt.Errorf("invalid legal hold configuration: %w", err)
	}

	// Update legal hold
	newLegalHold := &LegalHoldConfig{
		Status: legalHold.Status,
	}

	return ol.manager.SetObjectLegalHold(ctx, bucket, key, newLegalHold)
}

// CanDeleteObject checks if an object can be deleted (not protected by retention or legal hold)
func (ol *objectLock) CanDeleteObject(ctx context.Context, bucket, key string, user *auth.User) error {
	// Check legal hold
	legalHold, err := ol.manager.GetObjectLegalHold(ctx, bucket, key)
	if err == nil && legalHold != nil && ol.isLegalHoldActiveInternal(legalHold) {
		return ErrObjectUnderLegalHold
	}

	// Check retention
	retention, err := ol.manager.GetObjectRetention(ctx, bucket, key)
	if err == nil && retention != nil && ol.isRetentionActiveInternal(retention) {
		// COMPLIANCE mode cannot be bypassed
		if retention.Mode == RetentionModeCompliance {
			return NewComplianceRetentionError(retention.RetainUntilDate)
		}

		// GOVERNANCE mode can be bypassed with proper permissions
		if retention.Mode == RetentionModeGovernance {
			if err := ol.checkBypassGovernancePermission(user); err != nil {
				return NewGovernanceRetentionError(retention.RetainUntilDate)
			}
		}
	}

	return nil
}

// CanModifyObject checks if an object can be modified
func (ol *objectLock) CanModifyObject(ctx context.Context, bucket, key string, user *auth.User) error {
	// For Object Lock, modification rules are similar to deletion rules
	return ol.CanDeleteObject(ctx, bucket, key, user)
}

// ValidateRetention validates a retention configuration
func (ol *objectLock) ValidateRetention(retention *ObjectLockRetention) error {
	if retention == nil {
		return fmt.Errorf("retention configuration is required")
	}

	// Validate mode
	if retention.Mode != RetentionModeGovernance && retention.Mode != RetentionModeCompliance {
		return fmt.Errorf("invalid retention mode: %s. Must be %s or %s",
			retention.Mode, RetentionModeGovernance, RetentionModeCompliance)
	}

	// Validate retain until date
	if retention.RetainUntilDate.IsZero() {
		return fmt.Errorf("retain until date is required")
	}

	if retention.RetainUntilDate.Before(time.Now()) {
		return fmt.Errorf("retain until date must be in the future")
	}

	return nil
}

// ValidateLegalHold validates a legal hold configuration
func (ol *objectLock) ValidateLegalHold(legalHold *ObjectLockLegalHold) error {
	if legalHold == nil {
		return fmt.Errorf("legal hold configuration is required")
	}

	if legalHold.Status != LegalHoldStatusOn && legalHold.Status != LegalHoldStatusOff {
		return fmt.Errorf("invalid legal hold status: %s. Must be %s or %s",
			legalHold.Status, LegalHoldStatusOn, LegalHoldStatusOff)
	}

	return nil
}

// SetDefaultRetention sets default retention configuration for a bucket
func (ol *objectLock) SetDefaultRetention(ctx context.Context, bucketName string, retention *ObjectLockRetention, user *auth.User) error {
	if ol.bucketManager == nil {
		return fmt.Errorf("bucket manager not available")
	}

	if err := ol.ValidateRetention(retention); err != nil {
		return fmt.Errorf("invalid default retention: %w", err)
	}

	// Get tenant ID from context
	tenantID := auth.GetTenantIDFromContext(ctx)
	if tenantID == "" {
		// Fall back to extracting from user if available
		if user != nil {
			tenantID = user.TenantID
		}
		if tenantID == "" {
			return fmt.Errorf("tenant ID not found in context or user")
		}
	}

	// Get current bucket Object Lock configuration
	config, err := ol.bucketManager.GetObjectLockConfig(ctx, tenantID, bucketName)
	if err != nil {
		return fmt.Errorf("failed to get bucket object lock config: %w", err)
	}

	// Ensure Object Lock is enabled
	if !config.ObjectLockEnabled {
		return fmt.Errorf("object lock is not enabled for bucket %s", bucketName)
	}

	// Convert ObjectLockRetention (with RetainUntilDate) to DefaultRetention (with Days/Years)
	// Calculate days from now until the retention date
	now := time.Now()
	duration := retention.RetainUntilDate.Sub(now)
	days := int(duration.Hours() / 24)

	if days < 0 {
		return fmt.Errorf("retention date must be in the future")
	}

	// Create default retention configuration
	defaultRetention := &bucket.DefaultRetention{
		Mode: retention.Mode,
		Days: &days,
	}

	// Update the Object Lock configuration with the new default retention
	if config.Rule == nil {
		config.Rule = &bucket.ObjectLockRule{}
	}
	config.Rule.DefaultRetention = defaultRetention

	// Save the updated configuration
	if err := ol.bucketManager.SetObjectLockConfig(ctx, tenantID, bucketName, config); err != nil {
		return fmt.Errorf("failed to set object lock config: %w", err)
	}

	return nil
}

// GetDefaultRetention gets default retention configuration for a bucket
func (ol *objectLock) GetDefaultRetention(ctx context.Context, bucketName string, user *auth.User) (*ObjectLockRetention, error) {
	if ol.bucketManager == nil {
		return nil, fmt.Errorf("bucket manager not available")
	}

	// Get tenant ID from context
	tenantID := auth.GetTenantIDFromContext(ctx)
	if tenantID == "" {
		// Fall back to extracting from user if available
		if user != nil {
			tenantID = user.TenantID
		}
		if tenantID == "" {
			return nil, fmt.Errorf("tenant ID not found in context or user")
		}
	}

	// Get bucket Object Lock configuration
	config, err := ol.bucketManager.GetObjectLockConfig(ctx, tenantID, bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to get bucket object lock config: %w", err)
	}

	// Check if Object Lock is enabled
	if !config.ObjectLockEnabled {
		return nil, ErrNoRetentionConfiguration
	}

	// Check if default retention is configured
	if config.Rule == nil || config.Rule.DefaultRetention == nil {
		return nil, ErrNoRetentionConfiguration
	}

	defaultRetention := config.Rule.DefaultRetention

	// Convert DefaultRetention (with Days/Years) to ObjectLockRetention (with RetainUntilDate)
	// Calculate RetainUntilDate from current time + days/years
	now := time.Now()
	var retainUntilDate time.Time

	if defaultRetention.Years != nil && *defaultRetention.Years > 0 {
		retainUntilDate = now.AddDate(*defaultRetention.Years, 0, 0)
	} else if defaultRetention.Days != nil && *defaultRetention.Days > 0 {
		retainUntilDate = now.AddDate(0, 0, *defaultRetention.Days)
	} else {
		return nil, fmt.Errorf("invalid default retention configuration: neither days nor years specified")
	}

	return &ObjectLockRetention{
		Mode:            defaultRetention.Mode,
		RetainUntilDate: retainUntilDate,
	}, nil
}

// IsRetentionActive checks if retention is currently active for the given configuration
func (ol *objectLock) IsRetentionActive(retention *ObjectLockRetention) bool {
	if retention == nil {
		return false
	}

	return time.Now().Before(retention.RetainUntilDate)
}

// IsLegalHoldActive checks if legal hold is currently active
func (ol *objectLock) IsLegalHoldActive(legalHold *ObjectLockLegalHold) bool {
	if legalHold == nil {
		return false
	}

	return legalHold.Status == LegalHoldStatusOn
}

// IsRetentionActiveInternal checks if internal retention config is active
func (ol *objectLock) IsRetentionActiveInternal(retention *RetentionConfig) bool {
	if retention == nil {
		return false
	}

	return time.Now().Before(retention.RetainUntilDate)
}

// IsLegalHoldActiveInternal checks if internal legal hold config is active
func (ol *objectLock) IsLegalHoldActiveInternal(legalHold *LegalHoldConfig) bool {
	if legalHold == nil {
		return false
	}

	return legalHold.Status == LegalHoldStatusOn
}

// isRetentionActiveInternal is a private helper (lowercase)
func (ol *objectLock) isRetentionActiveInternal(retention *RetentionConfig) bool {
	return ol.IsRetentionActiveInternal(retention)
}

// isLegalHoldActiveInternal is a private helper (lowercase)
func (ol *objectLock) isLegalHoldActiveInternal(legalHold *LegalHoldConfig) bool {
	return ol.IsLegalHoldActiveInternal(legalHold)
}

// GetRetentionExpiryTime returns the expiry time for retention
func (ol *objectLock) GetRetentionExpiryTime(retention *ObjectLockRetention) time.Time {
	if retention == nil {
		return time.Time{}
	}

	return retention.RetainUntilDate
}

// Helper methods

// canModifyRetention checks if an existing retention can be modified
func (ol *objectLock) canModifyRetention(existing *RetentionConfig, new *ObjectLockRetention, user *auth.User) error {
	// COMPLIANCE mode retention cannot be shortened or removed
	if existing.Mode == RetentionModeCompliance {
		if new.RetainUntilDate.Before(existing.RetainUntilDate) {
			return ErrCannotShortenCompliance
		}
	}

	// GOVERNANCE mode can be modified with proper permissions
	if existing.Mode == RetentionModeGovernance {
		if new.RetainUntilDate.Before(existing.RetainUntilDate) {
			if err := ol.checkBypassGovernancePermission(user); err != nil {
				return ErrCannotShortenGovernance
			}
		}
	}

	return nil
}

// checkBypassGovernancePermission checks if user has permission to bypass governance retention
func (ol *objectLock) checkBypassGovernancePermission(user *auth.User) error {
	// Check if user has the bypass governance retention permission
	// This is typically a special permission in AWS S3
	for _, role := range user.Roles {
		if role == "admin" {
			return nil // Admin can bypass governance
		}
	}

	return ErrInsufficientPermissions
}
