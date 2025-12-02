package object

import (
	"context"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
)

// TestCalculateRetentionDate tests retention date calculation
func TestCalculateRetentionDate(t *testing.T) {
	rpm := &retentionPolicyManager{}
	baseTime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		years    int
		days     int
		expected time.Time
	}{
		{
			name:     "Add 1 year",
			years:    1,
			days:     0,
			expected: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "Add 30 days",
			years:    0,
			days:     30,
			expected: time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "Add 2 years and 10 days",
			years:    2,
			days:     10,
			expected: time.Date(2027, 1, 11, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "No addition",
			years:    0,
			days:     0,
			expected: baseTime,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rpm.CalculateRetentionDate(baseTime, tt.years, tt.days)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestValidateRetentionModification tests retention modification validation
func TestValidateRetentionModification(t *testing.T) {
	mockObjectLocker := &mockObjectLocker{}
	rpm := &retentionPolicyManager{
		objectLocker: mockObjectLocker,
	}

	adminUser := &auth.User{
		ID:    "admin-1",
		Roles: []string{"admin"},
	}

	regularUser := &auth.User{
		ID:    "user-1",
		Roles: []string{"user"},
	}

	futureDate := time.Now().Add(24 * time.Hour)
	farFutureDate := time.Now().Add(48 * time.Hour)

	t.Run("No existing retention - allow any valid", func(t *testing.T) {
		newRetention := &ObjectLockRetention{
			Mode:            RetentionModeGovernance,
			RetainUntilDate: futureDate,
		}

		err := rpm.ValidateRetentionModification(nil, newRetention, adminUser)
		assert.NoError(t, err)
	})

	t.Run("Extend retention period - allowed", func(t *testing.T) {
		existing := &ObjectLockRetention{
			Mode:            RetentionModeGovernance,
			RetainUntilDate: futureDate,
		}
		new := &ObjectLockRetention{
			Mode:            RetentionModeGovernance,
			RetainUntilDate: farFutureDate,
		}

		err := rpm.ValidateRetentionModification(existing, new, adminUser)
		assert.NoError(t, err)
	})

	t.Run("Shorten compliance retention - not allowed", func(t *testing.T) {
		existing := &ObjectLockRetention{
			Mode:            RetentionModeCompliance,
			RetainUntilDate: farFutureDate,
		}
		new := &ObjectLockRetention{
			Mode:            RetentionModeCompliance,
			RetainUntilDate: futureDate,
		}

		err := rpm.ValidateRetentionModification(existing, new, adminUser)
		assert.Error(t, err)
		assert.Equal(t, ErrCannotShortenCompliance, err)
	})

	t.Run("Shorten governance retention - admin can bypass", func(t *testing.T) {
		existing := &ObjectLockRetention{
			Mode:            RetentionModeGovernance,
			RetainUntilDate: farFutureDate,
		}
		new := &ObjectLockRetention{
			Mode:            RetentionModeGovernance,
			RetainUntilDate: futureDate,
		}

		err := rpm.ValidateRetentionModification(existing, new, adminUser)
		assert.NoError(t, err)
	})

	t.Run("Shorten governance retention - regular user cannot", func(t *testing.T) {
		existing := &ObjectLockRetention{
			Mode:            RetentionModeGovernance,
			RetainUntilDate: farFutureDate,
		}
		new := &ObjectLockRetention{
			Mode:            RetentionModeGovernance,
			RetainUntilDate: futureDate,
		}

		err := rpm.ValidateRetentionModification(existing, new, regularUser)
		assert.Error(t, err)
		assert.Equal(t, ErrCannotShortenGovernance, err)
	})

	t.Run("Upgrade governance to compliance - allowed", func(t *testing.T) {
		existing := &ObjectLockRetention{
			Mode:            RetentionModeGovernance,
			RetainUntilDate: futureDate,
		}
		new := &ObjectLockRetention{
			Mode:            RetentionModeCompliance,
			RetainUntilDate: futureDate,
		}

		err := rpm.ValidateRetentionModification(existing, new, adminUser)
		assert.NoError(t, err)
	})

	t.Run("Downgrade compliance to governance - not allowed", func(t *testing.T) {
		existing := &ObjectLockRetention{
			Mode:            RetentionModeCompliance,
			RetainUntilDate: futureDate,
		}
		new := &ObjectLockRetention{
			Mode:            RetentionModeGovernance,
			RetainUntilDate: futureDate,
		}

		err := rpm.ValidateRetentionModification(existing, new, adminUser)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot change retention mode")
	})
}

// mockObjectLocker for testing
type mockObjectLocker struct {
	retention  *ObjectLockRetention
	legalHold  *ObjectLockLegalHold
	validateOK bool
}

func (m *mockObjectLocker) PutObjectLock(ctx context.Context, bucket, key string, retention *ObjectLockRetention, legalHold *ObjectLockLegalHold, user *auth.User) error {
	m.retention = retention
	m.legalHold = legalHold
	return nil
}

func (m *mockObjectLocker) PutObjectRetention(ctx context.Context, bucket, key string, retention *ObjectLockRetention, bypassGovernance bool, user *auth.User) error {
	m.retention = retention
	return nil
}

func (m *mockObjectLocker) GetObjectRetention(ctx context.Context, bucket, key string, user *auth.User) (*ObjectLockRetention, error) {
	if m.retention == nil {
		return &ObjectLockRetention{}, ErrNoRetentionConfiguration
	}
	return m.retention, nil
}

func (m *mockObjectLocker) PutObjectLegalHold(ctx context.Context, bucket, key string, legalHold *ObjectLockLegalHold, user *auth.User) error {
	m.legalHold = legalHold
	return nil
}

func (m *mockObjectLocker) GetObjectLegalHold(ctx context.Context, bucket, key string, user *auth.User) (*ObjectLockLegalHold, error) {
	if m.legalHold == nil {
		return &ObjectLockLegalHold{}, ErrObjectNotFound
	}
	return m.legalHold, nil
}

func (m *mockObjectLocker) CanDeleteObject(ctx context.Context, bucket, key string, user *auth.User) error {
	if m.retention != nil && m.retention.RetainUntilDate.After(time.Now()) {
		return ErrObjectLocked
	}
	if m.legalHold != nil && m.legalHold.Status == LegalHoldStatusOn {
		return ErrObjectUnderLegalHold
	}
	return nil
}

func (m *mockObjectLocker) CanModifyObject(ctx context.Context, bucket, key string, user *auth.User) error {
	return m.CanDeleteObject(ctx, bucket, key, user)
}

func (m *mockObjectLocker) ValidateRetention(retention *ObjectLockRetention) error {
	if m.validateOK {
		return nil
	}
	if retention == nil {
		return ErrRetentionDateInPast
	}
	if retention.RetainUntilDate.Before(time.Now()) {
		return ErrRetentionDateInPast
	}
	return nil
}

func (m *mockObjectLocker) ValidateLegalHold(legalHold *ObjectLockLegalHold) error {
	if legalHold == nil {
		return ErrInvalidLegalHoldStatus
	}
	return nil
}

func (m *mockObjectLocker) IsRetentionActiveInternal(retention *RetentionConfig) bool {
	if retention == nil {
		return false
	}
	return retention.RetainUntilDate.After(time.Now())
}

func (m *mockObjectLocker) IsLegalHoldActiveInternal(legalHold *LegalHoldConfig) bool {
	if legalHold == nil {
		return false
	}
	return legalHold.Status == LegalHoldStatusOn
}

func (m *mockObjectLocker) SetDefaultRetention(ctx context.Context, bucket string, retention *ObjectLockRetention, user *auth.User) error {
	m.retention = retention
	return nil
}

func (m *mockObjectLocker) GetDefaultRetention(ctx context.Context, bucket string, user *auth.User) (*ObjectLockRetention, error) {
	if m.retention == nil {
		return nil, ErrNoRetentionConfiguration
	}
	return m.retention, nil
}

func (m *mockObjectLocker) IsRetentionActive(retention *ObjectLockRetention) bool {
	if retention == nil {
		return false
	}
	rc := &RetentionConfig{
		Mode:            retention.Mode,
		RetainUntilDate: retention.RetainUntilDate,
	}
	return m.IsRetentionActiveInternal(rc)
}

func (m *mockObjectLocker) IsLegalHoldActive(legalHold *ObjectLockLegalHold) bool {
	if legalHold == nil {
		return false
	}
	lhc := &LegalHoldConfig{
		Status: legalHold.Status,
	}
	return m.IsLegalHoldActiveInternal(lhc)
}

func (m *mockObjectLocker) GetRetentionExpiryTime(retention *ObjectLockRetention) time.Time {
	if retention == nil {
		return time.Time{}
	}
	return retention.RetainUntilDate
}

// TestAuditRetentionAction tests audit logging
func TestAuditRetentionAction(t *testing.T) {
	rpm := &retentionPolicyManager{}

	user := &auth.User{
		ID:       "user-1",
		Username: "testuser",
	}

	details := map[string]string{
		"old_date": "2025-01-01",
		"new_date": "2025-12-31",
	}

	err := rpm.AuditRetentionAction(context.Background(), "test-bucket", "test-key", "retention_extended", user, details)
	assert.NoError(t, err)
}
