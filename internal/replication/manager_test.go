package replication

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockObjectAdapter for testing
type MockObjectAdapter struct {
	CopyObjectFunc        func(ctx context.Context, sourceBucket, sourceKey, destBucket, destKey, tenantID string) (int64, error)
	DeleteObjectFunc      func(ctx context.Context, bucket, key, tenantID string) error
	GetObjectMetadataFunc func(ctx context.Context, bucket, key, tenantID string) (map[string]string, error)
}

func (m *MockObjectAdapter) CopyObject(ctx context.Context, sourceBucket, sourceKey, destBucket, destKey, tenantID string) (int64, error) {
	if m.CopyObjectFunc != nil {
		return m.CopyObjectFunc(ctx, sourceBucket, sourceKey, destBucket, destKey, tenantID)
	}
	return 1024, nil
}

func (m *MockObjectAdapter) DeleteObject(ctx context.Context, bucket, key, tenantID string) error {
	if m.DeleteObjectFunc != nil {
		return m.DeleteObjectFunc(ctx, bucket, key, tenantID)
	}
	return nil
}

func (m *MockObjectAdapter) GetObjectMetadata(ctx context.Context, bucket, key, tenantID string) (map[string]string, error) {
	if m.GetObjectMetadataFunc != nil {
		return m.GetObjectMetadataFunc(ctx, bucket, key, tenantID)
	}
	return map[string]string{}, nil
}

func setupTestDB(t *testing.T) *sql.DB {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_replication.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	return db
}

func setupTestManager(t *testing.T) (*Manager, *MockObjectAdapter) {
	db := setupTestDB(t)
	t.Cleanup(func() {
		db.Close()
	})
	adapter := &MockObjectAdapter{}
	config := ReplicationConfig{
		Enable:          true,
		WorkerCount:     2,
		QueueSize:       100,
		BatchSize:       10,
		RetryInterval:   5 * time.Minute,
		MaxRetries:      3,
		CleanupInterval: 1 * time.Hour,
		RetentionDays:   30,
	}
	manager, err := NewManager(db, config, adapter)
	require.NoError(t, err)
	return manager, adapter
}

func TestNewManager(t *testing.T) {
	manager, _ := setupTestManager(t)
	assert.NotNil(t, manager)
	assert.NotNil(t, manager.db)
	assert.NotNil(t, manager.queue)
	assert.Equal(t, 2, manager.config.WorkerCount)
	assert.Equal(t, 100, manager.config.QueueSize)
}

func TestNewManager_DefaultConfig(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	adapter := &MockObjectAdapter{}
	config := ReplicationConfig{}
	manager, err := NewManager(db, config, adapter)
	require.NoError(t, err)

	assert.Equal(t, 5, manager.config.WorkerCount)
	assert.Equal(t, 1000, manager.config.QueueSize)
	assert.Equal(t, 3, manager.config.MaxRetries)
	assert.Equal(t, 5*time.Minute, manager.config.RetryInterval)
	assert.Equal(t, 24*time.Hour, manager.config.CleanupInterval)
	assert.Equal(t, 30, manager.config.RetentionDays)
}

func TestCreateRule(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := context.Background()

	rule := &ReplicationRule{
		TenantID:           "tenant-1",
		SourceBucket:       "source-bucket",
		DestinationBucket:  "dest-bucket",
		DestinationRegion:  "us-east-1",
		Prefix:             "backups/",
		Enabled:            true,
		Priority:           10,
		Mode:               ModeRealTime,
		ConflictResolution: ConflictLWW,
		ReplicateDeletes:   true,
		ReplicateMetadata:  true,
	}

	err := manager.CreateRule(ctx, rule)
	require.NoError(t, err)
	assert.NotEmpty(t, rule.ID)
	assert.False(t, rule.CreatedAt.IsZero())
	assert.False(t, rule.UpdatedAt.IsZero())
}

func TestGetRule(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := context.Background()

	// Create rule
	rule := &ReplicationRule{
		TenantID:          "tenant-1",
		SourceBucket:      "source-bucket",
		DestinationBucket: "dest-bucket",
		Enabled:           true,
		Mode:              ModeRealTime,
	}
	err := manager.CreateRule(ctx, rule)
	require.NoError(t, err)

	// Get rule
	retrieved, err := manager.GetRule(ctx, rule.ID)
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, rule.ID, retrieved.ID)
	assert.Equal(t, "tenant-1", retrieved.TenantID)
	assert.Equal(t, "source-bucket", retrieved.SourceBucket)
	assert.Equal(t, "dest-bucket", retrieved.DestinationBucket)
}

func TestGetRule_NotFound(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := context.Background()

	retrieved, err := manager.GetRule(ctx, "non-existent-id")
	require.NoError(t, err)
	assert.Nil(t, retrieved)
}

func TestListRules(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := context.Background()

	// Create multiple rules
	rule1 := &ReplicationRule{
		TenantID:          "tenant-1",
		SourceBucket:      "bucket-1",
		DestinationBucket: "dest-1",
		Enabled:           true,
		Priority:          10,
		Mode:              ModeRealTime,
	}
	rule2 := &ReplicationRule{
		TenantID:          "tenant-1",
		SourceBucket:      "bucket-2",
		DestinationBucket: "dest-2",
		Enabled:           true,
		Priority:          5,
		Mode:              ModeScheduled,
	}

	err := manager.CreateRule(ctx, rule1)
	require.NoError(t, err)
	err = manager.CreateRule(ctx, rule2)
	require.NoError(t, err)

	// List rules
	rules, err := manager.ListRules(ctx, "tenant-1")
	require.NoError(t, err)
	assert.Len(t, rules, 2)
	// Should be ordered by priority DESC
	assert.Equal(t, 10, rules[0].Priority)
	assert.Equal(t, 5, rules[1].Priority)
}

func TestListRules_Empty(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := context.Background()

	rules, err := manager.ListRules(ctx, "tenant-1")
	require.NoError(t, err)
	assert.Empty(t, rules)
}

func TestUpdateRule(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := context.Background()

	// Create rule
	rule := &ReplicationRule{
		TenantID:          "tenant-1",
		SourceBucket:      "source-bucket",
		DestinationBucket: "dest-bucket",
		Enabled:           true,
		Priority:          5,
		Mode:              ModeRealTime,
	}
	err := manager.CreateRule(ctx, rule)
	require.NoError(t, err)

	// Update rule
	rule.DestinationBucket = "new-dest-bucket"
	rule.Priority = 20
	rule.Enabled = false
	err = manager.UpdateRule(ctx, rule)
	require.NoError(t, err)

	// Verify update
	updated, err := manager.GetRule(ctx, rule.ID)
	require.NoError(t, err)
	assert.Equal(t, "new-dest-bucket", updated.DestinationBucket)
	assert.Equal(t, 20, updated.Priority)
	assert.False(t, updated.Enabled)
}

func TestUpdateRule_NotFound(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := context.Background()

	rule := &ReplicationRule{
		ID:       "non-existent",
		TenantID: "tenant-1",
	}
	err := manager.UpdateRule(ctx, rule)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDeleteRule(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := context.Background()

	// Create rule
	rule := &ReplicationRule{
		TenantID:          "tenant-1",
		SourceBucket:      "source-bucket",
		DestinationBucket: "dest-bucket",
		Enabled:           true,
		Mode:              ModeRealTime,
	}
	err := manager.CreateRule(ctx, rule)
	require.NoError(t, err)

	// Delete rule
	err = manager.DeleteRule(ctx, "tenant-1", rule.ID)
	require.NoError(t, err)

	// Verify deletion
	retrieved, err := manager.GetRule(ctx, rule.ID)
	require.NoError(t, err)
	assert.Nil(t, retrieved)
}

func TestDeleteRule_NotFound(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := context.Background()

	err := manager.DeleteRule(ctx, "tenant-1", "non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestQueueObject(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := context.Background()

	// Create rule
	rule := &ReplicationRule{
		TenantID:          "tenant-1",
		SourceBucket:      "source-bucket",
		DestinationBucket: "dest-bucket",
		Enabled:           true,
		Mode:              ModeRealTime,
	}
	err := manager.CreateRule(ctx, rule)
	require.NoError(t, err)

	// Queue object
	err = manager.QueueObject(ctx, "tenant-1", "source-bucket", "file.txt", "PUT")
	require.NoError(t, err)

	// Verify queue item was created
	var count int
	err = manager.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM replication_queue").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestQueueObject_NoMatchingRules(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := context.Background()

	// Queue object without any rules
	err := manager.QueueObject(ctx, "tenant-1", "source-bucket", "file.txt", "PUT")
	require.NoError(t, err)

	// Verify no queue items created
	var count int
	err = manager.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM replication_queue").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestQueueObject_WithPrefix(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := context.Background()

	// Create rule with prefix
	rule := &ReplicationRule{
		TenantID:          "tenant-1",
		SourceBucket:      "source-bucket",
		DestinationBucket: "dest-bucket",
		Prefix:            "backups/",
		Enabled:           true,
		Mode:              ModeRealTime,
	}
	err := manager.CreateRule(ctx, rule)
	require.NoError(t, err)

	// Queue matching object
	err = manager.QueueObject(ctx, "tenant-1", "source-bucket", "backups/file.txt", "PUT")
	require.NoError(t, err)

	// Queue non-matching object
	err = manager.QueueObject(ctx, "tenant-1", "source-bucket", "other/file.txt", "PUT")
	require.NoError(t, err)

	// Verify only one queue item created
	var count int
	err = manager.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM replication_queue").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestGetMetrics(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := context.Background()

	// Create rule
	rule := &ReplicationRule{
		TenantID:          "tenant-1",
		SourceBucket:      "source-bucket",
		DestinationBucket: "dest-bucket",
		Enabled:           true,
		Mode:              ModeRealTime,
	}
	err := manager.CreateRule(ctx, rule)
	require.NoError(t, err)

	// Queue some objects
	err = manager.QueueObject(ctx, "tenant-1", "source-bucket", "file1.txt", "PUT")
	require.NoError(t, err)
	err = manager.QueueObject(ctx, "tenant-1", "source-bucket", "file2.txt", "PUT")
	require.NoError(t, err)

	// Get metrics
	metrics, err := manager.GetMetrics(ctx, rule.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), metrics.TotalObjects)
	assert.Equal(t, int64(2), metrics.PendingObjects)
	assert.Equal(t, int64(0), metrics.CompletedObjects)
	assert.Equal(t, int64(0), metrics.FailedObjects)
}

func TestMatchesPrefix(t *testing.T) {
	tests := []struct {
		objectKey string
		prefix    string
		expected  bool
	}{
		{"backups/file.txt", "backups/", true},
		{"backups/2023/file.txt", "backups/", true},
		{"other/file.txt", "backups/", false},
		{"file.txt", "backups/", false},
		{"file.txt", "", true},
		{"backups", "backups/", false},
	}

	for _, tt := range tests {
		t.Run(tt.objectKey+"_"+tt.prefix, func(t *testing.T) {
			result := matchesPrefix(tt.objectKey, tt.prefix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReplicationRule_MarshalJSON(t *testing.T) {
	rule := &ReplicationRule{
		ID:                "rule-1",
		TenantID:          "tenant-1",
		SourceBucket:      "source",
		DestinationBucket: "dest",
		Enabled:           true,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	data, err := rule.MarshalJSON()
	require.NoError(t, err)
	assert.NotEmpty(t, data)
	assert.Contains(t, string(data), "rule-1")
	assert.Contains(t, string(data), "tenant-1")
}

func TestReplicationConfig_Defaults(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	adapter := &MockObjectAdapter{}
	config := ReplicationConfig{}

	manager, err := NewManager(db, config, adapter)
	require.NoError(t, err)

	assert.Equal(t, 5, manager.config.WorkerCount)
	assert.Equal(t, 1000, manager.config.QueueSize)
	assert.Equal(t, 3, manager.config.MaxRetries)
}

func TestReplicationStatus_Types(t *testing.T) {
	assert.Equal(t, ReplicationStatus("pending"), StatusPending)
	assert.Equal(t, ReplicationStatus("in_progress"), StatusInProgress)
	assert.Equal(t, ReplicationStatus("completed"), StatusCompleted)
	assert.Equal(t, ReplicationStatus("failed"), StatusFailed)
	assert.Equal(t, ReplicationStatus("retrying"), StatusRetrying)
}

func TestReplicationMode_Types(t *testing.T) {
	assert.Equal(t, ReplicationMode("realtime"), ModeRealTime)
	assert.Equal(t, ReplicationMode("scheduled"), ModeScheduled)
	assert.Equal(t, ReplicationMode("batch"), ModeBatch)
}

func TestConflictResolution_Types(t *testing.T) {
	assert.Equal(t, ConflictResolution("last_write_wins"), ConflictLWW)
	assert.Equal(t, ConflictResolution("version_based"), ConflictVersionBased)
	assert.Equal(t, ConflictResolution("primary_wins"), ConflictPrimaryWins)
}

func TestCreateRule_WithAllFields(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := context.Background()

	rule := &ReplicationRule{
		ID:                 "custom-id",
		TenantID:           "tenant-1",
		SourceBucket:       "source-bucket",
		DestinationBucket:  "dest-bucket",
		DestinationRegion:  "us-west-2",
		DestinationTenant:  "tenant-2",
		Prefix:             "logs/",
		Enabled:            true,
		Priority:           15,
		Mode:               ModeScheduled,
		ConflictResolution: ConflictVersionBased,
		ReplicateDeletes:   false,
		ReplicateMetadata:  true,
	}

	err := manager.CreateRule(ctx, rule)
	require.NoError(t, err)

	// Verify all fields
	retrieved, err := manager.GetRule(ctx, "custom-id")
	require.NoError(t, err)
	assert.Equal(t, "custom-id", retrieved.ID)
	assert.Equal(t, "us-west-2", retrieved.DestinationRegion)
	assert.Equal(t, "tenant-2", retrieved.DestinationTenant)
	assert.Equal(t, "logs/", retrieved.Prefix)
	assert.Equal(t, 15, retrieved.Priority)
	assert.Equal(t, ModeScheduled, retrieved.Mode)
	assert.Equal(t, ConflictVersionBased, retrieved.ConflictResolution)
	assert.False(t, retrieved.ReplicateDeletes)
	assert.True(t, retrieved.ReplicateMetadata)
}

func TestQueueObject_MultipleRules(t *testing.T) {
	manager, _ := setupTestManager(t)
	ctx := context.Background()

	// Create two rules for the same bucket
	rule1 := &ReplicationRule{
		TenantID:          "tenant-1",
		SourceBucket:      "source-bucket",
		DestinationBucket: "dest-bucket-1",
		Enabled:           true,
		Priority:          10,
		Mode:              ModeRealTime,
	}
	rule2 := &ReplicationRule{
		TenantID:          "tenant-1",
		SourceBucket:      "source-bucket",
		DestinationBucket: "dest-bucket-2",
		Enabled:           true,
		Priority:          5,
		Mode:              ModeRealTime,
	}

	err := manager.CreateRule(ctx, rule1)
	require.NoError(t, err)
	err = manager.CreateRule(ctx, rule2)
	require.NoError(t, err)

	// Queue object
	err = manager.QueueObject(ctx, "tenant-1", "source-bucket", "file.txt", "PUT")
	require.NoError(t, err)

	// Verify two queue items created (one per rule)
	var count int
	err = manager.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM replication_queue").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}
