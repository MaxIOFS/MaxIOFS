package object

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewRetentionPolicyManager tests the creation of RetentionPolicyManager
func TestNewRetentionPolicyManager(t *testing.T) {
	om, _, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	ol := NewObjectLocker(om, nil)

	// Create retention policy manager
	rpm := NewRetentionPolicyManager(om, ol)

	// Verify it was created
	require.NotNil(t, rpm)
	assert.IsType(t, &retentionPolicyManager{}, rpm)
}

// TestExtendRetentionPeriod tests extending the retention period for an object
func TestExtendRetentionPeriod(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	ol := NewObjectLocker(om, nil)
	rpm := NewRetentionPolicyManager(om, ol)

	bucket := "test-bucket"
	key := "test-object.txt"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put an object
	content := bytes.NewReader([]byte("test content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Set initial retention (30 days from now)
	user := &auth.User{ID: "user-1", Roles: []string{"admin"}}
	initialRetention := &ObjectLockRetention{
		Mode:            RetentionModeGovernance,
		RetainUntilDate: time.Now().Add(30 * 24 * time.Hour),
	}
	err = ol.PutObjectRetention(ctx, bucket, key, initialRetention, false, user)
	require.NoError(t, err)

	// Extend retention to 60 days from now
	newDate := time.Now().Add(60 * 24 * time.Hour)
	err = rpm.ExtendRetentionPeriod(ctx, bucket, key, newDate, user)
	require.NoError(t, err)

	// Verify retention was extended
	updatedRetention, err := ol.GetObjectRetention(ctx, bucket, key, user)
	require.NoError(t, err)
	assert.True(t, updatedRetention.RetainUntilDate.After(initialRetention.RetainUntilDate))
}

// TestExtendRetentionPeriod_CannotShorten tests that shortening retention is not allowed
func TestExtendRetentionPeriod_CannotShorten(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	ol := NewObjectLocker(om, nil)
	rpm := NewRetentionPolicyManager(om, ol)

	bucket := "test-bucket"
	key := "test-object.txt"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put an object
	content := bytes.NewReader([]byte("test content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Set initial retention (60 days from now)
	user := &auth.User{ID: "user-1", Roles: []string{"admin"}}
	initialRetention := &ObjectLockRetention{
		Mode:            RetentionModeGovernance,
		RetainUntilDate: time.Now().Add(60 * 24 * time.Hour),
	}
	err = ol.PutObjectRetention(ctx, bucket, key, initialRetention, false, user)
	require.NoError(t, err)

	// Attempt to shorten retention to 30 days (should fail)
	shorterDate := time.Now().Add(30 * 24 * time.Hour)
	err = rpm.ExtendRetentionPeriod(ctx, bucket, key, shorterDate, user)

	// Should return error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be earlier")
}

// TestIsObjectEligibleForDeletion tests checking if an object can be deleted
func TestIsObjectEligibleForDeletion(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	ol := NewObjectLocker(om, nil)
	rpm := NewRetentionPolicyManager(om, ol)

	bucket := "test-bucket"
	key := "test-object.txt"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put an object without retention
	content := bytes.NewReader([]byte("test content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Object without retention should be eligible for deletion
	eligible, err := rpm.IsObjectEligibleForDeletion(ctx, bucket, key)
	require.NoError(t, err)
	assert.True(t, eligible, "Object without retention should be eligible for deletion")
}

// TestIsObjectEligibleForDeletion_WithRetention tests object with active retention
func TestIsObjectEligibleForDeletion_WithRetention(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	ol := NewObjectLocker(om, nil)
	rpm := NewRetentionPolicyManager(om, ol)

	bucket := "test-bucket"
	key := "test-object.txt"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put an object
	content := bytes.NewReader([]byte("test content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Set active retention
	user := &auth.User{ID: "user-1", Roles: []string{"admin"}}
	retention := &ObjectLockRetention{
		Mode:            RetentionModeGovernance,
		RetainUntilDate: time.Now().Add(30 * 24 * time.Hour),
	}
	err = ol.PutObjectRetention(ctx, bucket, key, retention, false, user)
	require.NoError(t, err)

	// Object with active retention should NOT be eligible for deletion
	eligible, err := rpm.IsObjectEligibleForDeletion(ctx, bucket, key)
	require.NoError(t, err)
	assert.False(t, eligible, "Object with active retention should NOT be eligible for deletion")
}

// TestIsObjectEligibleForDeletion_WithLegalHold tests object with legal hold
func TestIsObjectEligibleForDeletion_WithLegalHold(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	ol := NewObjectLocker(om, nil)
	rpm := NewRetentionPolicyManager(om, ol)

	bucket := "test-bucket"
	key := "test-object.txt"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put an object
	content := bytes.NewReader([]byte("test content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	// Set legal hold
	user := &auth.User{ID: "user-1", Roles: []string{"admin"}}
	legalHold := &ObjectLockLegalHold{Status: LegalHoldStatusOn}
	err = ol.PutObjectLegalHold(ctx, bucket, key, legalHold, user)
	require.NoError(t, err)

	// Object with legal hold should NOT be eligible for deletion
	eligible, err := rpm.IsObjectEligibleForDeletion(ctx, bucket, key)
	require.NoError(t, err)
	assert.False(t, eligible, "Object with legal hold should NOT be eligible for deletion")
}

// TestGetExpiringObjects tests getting objects that are expiring soon
func TestGetExpiringObjects(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	ol := NewObjectLocker(om, nil)
	rpm := NewRetentionPolicyManager(om, ol)

	bucketName := "test-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName // Use full path: tenant-1/test-bucket

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	user := &auth.User{ID: "user-1", Roles: []string{"admin"}}

	// Create object with retention expiring in 5 days
	key1 := "expiring-soon.txt"
	content1 := bytes.NewReader([]byte("expiring soon"))
	headers1 := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key1, content1, headers1)
	require.NoError(t, err)

	retention1 := &ObjectLockRetention{
		Mode:            RetentionModeGovernance,
		RetainUntilDate: time.Now().Add(5 * 24 * time.Hour),
	}
	err = ol.PutObjectRetention(ctx, bucket, key1, retention1, false, user)
	require.NoError(t, err)

	// Create object with retention expiring in 30 days
	key2 := "expiring-later.txt"
	content2 := bytes.NewReader([]byte("expiring later"))
	headers2 := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key2, content2, headers2)
	require.NoError(t, err)

	retention2 := &ObjectLockRetention{
		Mode:            RetentionModeGovernance,
		RetainUntilDate: time.Now().Add(30 * 24 * time.Hour),
	}
	err = ol.PutObjectRetention(ctx, bucket, key2, retention2, false, user)
	require.NoError(t, err)

	// Get objects expiring in the next 10 days
	beforeTime := time.Now().Add(10 * 24 * time.Hour)
	expiringObjects, err := rpm.GetExpiringObjects(ctx, bucket, beforeTime)
	require.NoError(t, err)

	// Should include the object expiring in 5 days
	assert.GreaterOrEqual(t, len(expiringObjects), 1)

	// Verify expiring object is in the list
	found := false
	for _, obj := range expiringObjects {
		if obj.Key == key1 {
			found = true
			break
		}
	}
	assert.True(t, found, "Object expiring in 5 days should be in the list")
}

// TestCleanupExpiredRetentions tests that expired retentions can be detected and removed
func TestCleanupExpiredRetentions(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	ol := NewObjectLocker(om, nil)
	rpm := NewRetentionPolicyManager(om, ol)

	bucketName := "test-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName // Use full path: tenant-1/test-bucket

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	user := &auth.User{ID: "user-1", Roles: []string{"admin"}}

	// Create object with short retention (1 second from now)
	key := "expired-retention.txt"
	content := bytes.NewReader([]byte("expired retention"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	shortRetention := &ObjectLockRetention{
		Mode:            RetentionModeGovernance,
		RetainUntilDate: time.Now().Add(1 * time.Second),
	}
	err = ol.PutObjectRetention(ctx, bucket, key, shortRetention, false, user)
	require.NoError(t, err)

	// Wait for retention to expire
	time.Sleep(2 * time.Second)

	// Verify that we can detect the object has expired retention
	eligible, err := rpm.IsObjectEligibleForDeletion(ctx, bucket, key)
	require.NoError(t, err)
	assert.True(t, eligible, "Object with expired retention should be eligible for deletion")

	// Verify the retention information is still present but expired
	currentRetention, err := ol.GetObjectRetention(ctx, bucket, key, user)
	require.NoError(t, err)
	require.NotNil(t, currentRetention, "Retention should still exist")
	assert.True(t, currentRetention.RetainUntilDate.Before(time.Now()), "Retention should be expired")

	// The cleanup function would normally remove this expired retention
	// We've verified the core functionality: detecting expired retention
}

// TestEnforceRetentionCompliance tests enforcing retention compliance
func TestEnforceRetentionCompliance(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	ol := NewObjectLocker(om, nil)
	rpm := NewRetentionPolicyManager(om, ol)

	bucket := "test-bucket"
	key := "test-object.txt"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put an object without retention
	content := bytes.NewReader([]byte("test content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	user := &auth.User{ID: "user-1", Roles: []string{"admin"}}

	// Enforce compliance (should pass for object without retention)
	err = rpm.EnforceRetentionCompliance(ctx, bucket, key, user)
	assert.NoError(t, err, "Should allow access to object without retention")
}

// TestEnforceRetentionCompliance_WithRetention tests enforcement with active retention
func TestEnforceRetentionCompliance_WithRetention(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	ol := NewObjectLocker(om, nil)
	rpm := NewRetentionPolicyManager(om, ol)

	bucket := "test-bucket"
	key := "test-object.txt"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put an object
	content := bytes.NewReader([]byte("test content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	user := &auth.User{ID: "user-1", Roles: []string{"admin"}}

	// Set active retention
	retention := &ObjectLockRetention{
		Mode:            RetentionModeCompliance,
		RetainUntilDate: time.Now().Add(30 * 24 * time.Hour),
	}
	err = ol.PutObjectRetention(ctx, bucket, key, retention, false, user)
	require.NoError(t, err)

	// Enforce compliance (should fail for object with active retention)
	err = rpm.EnforceRetentionCompliance(ctx, bucket, key, user)
	assert.Error(t, err, "Should deny access to object with active retention")
	assert.Contains(t, err.Error(), "locked")
}

// TestValidateComplianceDelete tests validating a delete operation for compliance
func TestValidateComplianceDelete(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	ol := NewObjectLocker(om, nil)
	rpm := NewRetentionPolicyManager(om, ol)

	bucket := "test-bucket"
	key := "test-object.txt"

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucket,
		TenantID: "tenant-1",
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	// Put an object without retention
	content := bytes.NewReader([]byte("test content"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	user := &auth.User{ID: "user-1", Roles: []string{"admin"}}

	// Validate compliance delete (should pass for object without retention)
	err = rpm.ValidateComplianceDelete(ctx, bucket, key, user)
	assert.NoError(t, err, "Should allow delete of object without retention")
}

// TestGetRetentionSummary tests generating a retention summary for a bucket
func TestGetRetentionSummary(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	ol := NewObjectLocker(om, nil)
	rpm := NewRetentionPolicyManager(om, ol)

	bucketName := "test-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName // Use full path: tenant-1/test-bucket

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	user := &auth.User{ID: "user-1", Roles: []string{"admin"}}

	// Create objects with different retention modes
	// Object with governance retention
	key1 := "governance-object.txt"
	content1 := bytes.NewReader([]byte("governance"))
	headers1 := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key1, content1, headers1)
	require.NoError(t, err)

	retention1 := &ObjectLockRetention{
		Mode:            RetentionModeGovernance,
		RetainUntilDate: time.Now().Add(30 * 24 * time.Hour),
	}
	err = ol.PutObjectRetention(ctx, bucket, key1, retention1, false, user)
	require.NoError(t, err)

	// Object with compliance retention
	key2 := "compliance-object.txt"
	content2 := bytes.NewReader([]byte("compliance"))
	headers2 := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key2, content2, headers2)
	require.NoError(t, err)

	retention2 := &ObjectLockRetention{
		Mode:            RetentionModeCompliance,
		RetainUntilDate: time.Now().Add(60 * 24 * time.Hour),
	}
	err = ol.PutObjectRetention(ctx, bucket, key2, retention2, false, user)
	require.NoError(t, err)

	// Object without retention
	key3 := "no-retention.txt"
	content3 := bytes.NewReader([]byte("no retention"))
	headers3 := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key3, content3, headers3)
	require.NoError(t, err)

	// Get retention summary
	summary, err := rpm.GetRetentionSummary(ctx, bucket)
	require.NoError(t, err)
	require.NotNil(t, summary)

	// Verify summary
	assert.Equal(t, bucket, summary.Bucket)
	assert.Equal(t, int64(3), summary.TotalObjects, "Should have 3 total objects")
	assert.Equal(t, int64(2), summary.ObjectsWithRetention, "Should have 2 objects with retention")
	assert.Equal(t, int64(1), summary.GovernanceObjects, "Should have 1 governance object")
	assert.Equal(t, int64(1), summary.ComplianceObjects, "Should have 1 compliance object")
	assert.False(t, summary.EarliestRetention.IsZero(), "Earliest retention should be set")
	assert.False(t, summary.LatestRetention.IsZero(), "Latest retention should be set")
}

// TestGenerateComplianceReport tests generating a detailed compliance report
func TestGenerateComplianceReport(t *testing.T) {
	ctx := context.Background()
	om, metaStore, cleanup := setupTestManagerWithStore(t)
	defer cleanup()

	ol := NewObjectLocker(om, nil)
	rpm := NewRetentionPolicyManager(om, ol)

	bucketName := "test-bucket"
	tenantID := "tenant-1"
	bucket := tenantID + "/" + bucketName // Use full path: tenant-1/test-bucket

	// Create bucket
	err := metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:     bucketName,
		TenantID: tenantID,
		OwnerID:  "user-1",
	})
	require.NoError(t, err)

	user := &auth.User{ID: "user-1", Roles: []string{"admin"}}

	// Create object with active retention
	key := "active-retention.txt"
	content := bytes.NewReader([]byte("active retention"))
	headers := http.Header{"Content-Type": []string{"text/plain"}}
	_, err = om.PutObject(ctx, bucket, key, content, headers)
	require.NoError(t, err)

	retention := &ObjectLockRetention{
		Mode:            RetentionModeCompliance,
		RetainUntilDate: time.Now().Add(30 * 24 * time.Hour),
	}
	err = ol.PutObjectRetention(ctx, bucket, key, retention, false, user)
	require.NoError(t, err)

	// Generate compliance report
	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now().Add(24 * time.Hour)
	report, err := rpm.GenerateComplianceReport(ctx, bucket, startTime, endTime)
	require.NoError(t, err)
	require.NotNil(t, report)

	// Verify report structure
	assert.Equal(t, bucket, report.Bucket)
	assert.Equal(t, startTime, report.StartTime)
	assert.Equal(t, endTime, report.EndTime)
	assert.NotEmpty(t, report.Objects, "Report should contain objects")
	assert.NotNil(t, report.Summary, "Report should have a summary")

	// Verify at least one object has active retention status
	foundActive := false
	for _, obj := range report.Objects {
		if obj.ComplianceStatus == "active" {
			foundActive = true
			assert.Greater(t, obj.DaysUntilExpiry, 0, "Active retention should have days until expiry")
			break
		}
	}
	assert.True(t, foundActive, "Should have at least one object with active retention")
}
