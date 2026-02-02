package object

import (
	"context"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBucketManager implements BucketConfigManager for testing
type mockBucketManager struct {
	config *bucket.ObjectLockConfig
	err    error
}

func (m *mockBucketManager) GetObjectLockConfig(ctx context.Context, tenantID, name string) (*bucket.ObjectLockConfig, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.config == nil {
		return &bucket.ObjectLockConfig{ObjectLockEnabled: false}, nil
	}
	return m.config, nil
}

func (m *mockBucketManager) SetObjectLockConfig(ctx context.Context, tenantID, name string, config *bucket.ObjectLockConfig) error {
	if m.err != nil {
		return m.err
	}
	m.config = config
	return nil
}

// TestSetDefaultRetention tests setting bucket-level default retention
func TestSetDefaultRetention(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-1"
	bucketName := "test-bucket"

	// Add tenant ID to context
	user := &auth.User{
		ID:       "user-1",
		TenantID: tenantID,
	}
	ctx = context.WithValue(ctx, "user", user)

	futureDate := time.Now().Add(30 * 24 * time.Hour) // 30 days from now

	tests := []struct {
		name          string
		retention     *ObjectLockRetention
		initialConfig *bucket.ObjectLockConfig
		wantError     bool
		errorContains string
	}{
		{
			name: "Set default retention successfully",
			retention: &ObjectLockRetention{
				Mode:            RetentionModeGovernance,
				RetainUntilDate: futureDate,
			},
			initialConfig: &bucket.ObjectLockConfig{
				ObjectLockEnabled: true,
			},
			wantError: false,
		},
		{
			name: "Set compliance mode retention",
			retention: &ObjectLockRetention{
				Mode:            RetentionModeCompliance,
				RetainUntilDate: futureDate,
			},
			initialConfig: &bucket.ObjectLockConfig{
				ObjectLockEnabled: true,
			},
			wantError: false,
		},
		{
			name: "Object Lock not enabled",
			retention: &ObjectLockRetention{
				Mode:            RetentionModeGovernance,
				RetainUntilDate: futureDate,
			},
			initialConfig: &bucket.ObjectLockConfig{
				ObjectLockEnabled: false,
			},
			wantError:     true,
			errorContains: "object lock is not enabled",
		},
		{
			name: "Invalid retention configuration",
			retention: &ObjectLockRetention{
				Mode:            "INVALID",
				RetainUntilDate: futureDate,
			},
			initialConfig: &bucket.ObjectLockConfig{
				ObjectLockEnabled: true,
			},
			wantError:     true,
			errorContains: "invalid default retention",
		},
		{
			name: "Past retention date",
			retention: &ObjectLockRetention{
				Mode:            RetentionModeGovernance,
				RetainUntilDate: time.Now().Add(-24 * time.Hour),
			},
			initialConfig: &bucket.ObjectLockConfig{
				ObjectLockEnabled: true,
			},
			wantError:     true,
			errorContains: "retain until date must be in the future",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockBucket := &mockBucketManager{
				config: tt.initialConfig,
			}
			ol := NewObjectLocker(nil, mockBucket)

			err := ol.SetDefaultRetention(ctx, bucketName, tt.retention, user)

			if tt.wantError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)

				// Verify the configuration was updated correctly
				assert.NotNil(t, mockBucket.config)
				assert.True(t, mockBucket.config.ObjectLockEnabled)
				assert.NotNil(t, mockBucket.config.Rule)
				assert.NotNil(t, mockBucket.config.Rule.DefaultRetention)
				assert.Equal(t, tt.retention.Mode, mockBucket.config.Rule.DefaultRetention.Mode)

				// Verify days were calculated correctly (approximately)
				expectedDays := int(tt.retention.RetainUntilDate.Sub(time.Now()).Hours() / 24)
				assert.NotNil(t, mockBucket.config.Rule.DefaultRetention.Days)
				// Allow some tolerance in days calculation due to time passing during test
				assert.InDelta(t, expectedDays, *mockBucket.config.Rule.DefaultRetention.Days, 1)
			}
		})
	}
}

// TestSetDefaultRetentionNoBucketManager tests error when bucket manager is not available
func TestSetDefaultRetentionNoBucketManager(t *testing.T) {
	ctx := context.Background()
	user := &auth.User{
		ID:       "user-1",
		TenantID: "tenant-1",
	}
	ctx = context.WithValue(ctx, "user", user)

	ol := NewObjectLocker(nil, nil)

	futureDate := time.Now().Add(30 * 24 * time.Hour)
	retention := &ObjectLockRetention{
		Mode:            RetentionModeGovernance,
		RetainUntilDate: futureDate,
	}

	err := ol.SetDefaultRetention(ctx, "test-bucket", retention, user)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bucket manager not available")
}

// TestGetDefaultRetention tests retrieving bucket-level default retention
func TestGetDefaultRetention(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-1"
	bucketName := "test-bucket"

	// Add tenant ID to context
	user := &auth.User{
		ID:       "user-1",
		TenantID: tenantID,
	}
	ctx = context.WithValue(ctx, "user", user)

	days30 := 30
	years1 := 1

	tests := []struct {
		name          string
		config        *bucket.ObjectLockConfig
		wantError     bool
		errorContains string
		validate      func(*testing.T, *ObjectLockRetention)
	}{
		{
			name: "Get default retention with days",
			config: &bucket.ObjectLockConfig{
				ObjectLockEnabled: true,
				Rule: &bucket.ObjectLockRule{
					DefaultRetention: &bucket.DefaultRetention{
						Mode: RetentionModeGovernance,
						Days: &days30,
					},
				},
			},
			wantError: false,
			validate: func(t *testing.T, retention *ObjectLockRetention) {
				assert.Equal(t, RetentionModeGovernance, retention.Mode)

				// Verify RetainUntilDate is approximately 30 days from now
				expectedDate := time.Now().AddDate(0, 0, 30)
				// Allow 1 minute tolerance
				assert.WithinDuration(t, expectedDate, retention.RetainUntilDate, time.Minute)
			},
		},
		{
			name: "Get default retention with years",
			config: &bucket.ObjectLockConfig{
				ObjectLockEnabled: true,
				Rule: &bucket.ObjectLockRule{
					DefaultRetention: &bucket.DefaultRetention{
						Mode:  RetentionModeCompliance,
						Years: &years1,
					},
				},
			},
			wantError: false,
			validate: func(t *testing.T, retention *ObjectLockRetention) {
				assert.Equal(t, RetentionModeCompliance, retention.Mode)

				// Verify RetainUntilDate is approximately 1 year from now
				expectedDate := time.Now().AddDate(1, 0, 0)
				// Allow 1 minute tolerance
				assert.WithinDuration(t, expectedDate, retention.RetainUntilDate, time.Minute)
			},
		},
		{
			name: "Object Lock not enabled",
			config: &bucket.ObjectLockConfig{
				ObjectLockEnabled: false,
			},
			wantError:     true,
			errorContains: "no retention configuration",
		},
		{
			name: "No default retention rule",
			config: &bucket.ObjectLockConfig{
				ObjectLockEnabled: true,
			},
			wantError:     true,
			errorContains: "no retention configuration",
		},
		{
			name: "Empty default retention",
			config: &bucket.ObjectLockConfig{
				ObjectLockEnabled: true,
				Rule: &bucket.ObjectLockRule{
					DefaultRetention: &bucket.DefaultRetention{
						Mode: RetentionModeGovernance,
					},
				},
			},
			wantError:     true,
			errorContains: "invalid default retention configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockBucket := &mockBucketManager{
				config: tt.config,
			}
			ol := NewObjectLocker(nil, mockBucket)

			retention, err := ol.GetDefaultRetention(ctx, bucketName, user)

			if tt.wantError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, retention)

				if tt.validate != nil {
					tt.validate(t, retention)
				}
			}
		})
	}
}

// TestGetDefaultRetentionNoBucketManager tests error when bucket manager is not available
func TestGetDefaultRetentionNoBucketManager(t *testing.T) {
	ctx := context.Background()
	user := &auth.User{
		ID:       "user-1",
		TenantID: "tenant-1",
	}
	ctx = context.WithValue(ctx, "user", user)

	ol := NewObjectLocker(nil, nil)

	_, err := ol.GetDefaultRetention(ctx, "test-bucket", user)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bucket manager not available")
}

// TestDefaultRetentionRoundTrip tests setting and then getting default retention
func TestDefaultRetentionRoundTrip(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-1"
	bucketName := "test-bucket"

	user := &auth.User{
		ID:       "user-1",
		TenantID: tenantID,
	}
	ctx = context.WithValue(ctx, "user", user)

	mockBucket := &mockBucketManager{
		config: &bucket.ObjectLockConfig{
			ObjectLockEnabled: true,
		},
	}
	ol := NewObjectLocker(nil, mockBucket)

	// Set default retention for 30 days
	futureDate := time.Now().Add(30 * 24 * time.Hour)
	retention := &ObjectLockRetention{
		Mode:            RetentionModeGovernance,
		RetainUntilDate: futureDate,
	}

	err := ol.SetDefaultRetention(ctx, bucketName, retention, user)
	require.NoError(t, err)

	// Get the default retention back
	retrieved, err := ol.GetDefaultRetention(ctx, bucketName, user)
	require.NoError(t, err)
	require.NotNil(t, retrieved)

	// Verify mode matches
	assert.Equal(t, RetentionModeGovernance, retrieved.Mode)

	// Verify the retention date is approximately correct (within 1 day due to conversion)
	assert.WithinDuration(t, futureDate, retrieved.RetainUntilDate, 24*time.Hour)
}
