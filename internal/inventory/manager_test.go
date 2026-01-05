package inventory

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	// Create the tables
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS bucket_inventory_configs (
			id TEXT PRIMARY KEY,
			bucket_name TEXT NOT NULL,
			tenant_id TEXT,
			enabled BOOLEAN DEFAULT 1,
			frequency TEXT NOT NULL CHECK(frequency IN ('daily', 'weekly')),
			format TEXT NOT NULL CHECK(format IN ('csv', 'json')),
			destination_bucket TEXT NOT NULL,
			destination_prefix TEXT DEFAULT '',
			included_fields TEXT NOT NULL,
			schedule_time TEXT NOT NULL,
			last_run_at INTEGER,
			next_run_at INTEGER,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(bucket_name, tenant_id)
		)
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS bucket_inventory_reports (
			id TEXT PRIMARY KEY,
			config_id TEXT NOT NULL,
			bucket_name TEXT NOT NULL,
			report_path TEXT NOT NULL,
			object_count INTEGER DEFAULT 0,
			total_size INTEGER DEFAULT 0,
			status TEXT NOT NULL CHECK(status IN ('pending', 'completed', 'failed')),
			started_at INTEGER,
			completed_at INTEGER,
			error_message TEXT,
			created_at INTEGER NOT NULL
		)
	`)
	require.NoError(t, err)

	return db
}

func TestCreateConfig(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)
	ctx := context.Background()

	config := &InventoryConfig{
		BucketName:        "test-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest-bucket",
		DestinationPrefix: "inventory/",
		IncludedFields:    []string{"bucket_name", "object_key", "size"},
		ScheduleTime:      "02:00",
	}

	err := manager.CreateConfig(ctx, config)
	require.NoError(t, err)
	assert.NotEmpty(t, config.ID)
	assert.NotZero(t, config.CreatedAt)
	assert.NotZero(t, config.UpdatedAt)
}

func TestGetConfig(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)
	ctx := context.Background()

	// Create a config first
	config := &InventoryConfig{
		BucketName:        "test-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "weekly",
		Format:            "json",
		DestinationBucket: "dest-bucket",
		IncludedFields:    []string{"bucket_name", "object_key"},
		ScheduleTime:      "03:00",
	}

	err := manager.CreateConfig(ctx, config)
	require.NoError(t, err)

	// Retrieve it
	retrieved, err := manager.GetConfig(ctx, "test-bucket", "tenant1")
	require.NoError(t, err)
	assert.Equal(t, config.BucketName, retrieved.BucketName)
	assert.Equal(t, config.TenantID, retrieved.TenantID)
	assert.Equal(t, config.Frequency, retrieved.Frequency)
	assert.Equal(t, config.Format, retrieved.Format)
	assert.Equal(t, config.DestinationBucket, retrieved.DestinationBucket)
}

func TestGetConfigNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)
	ctx := context.Background()

	_, err := manager.GetConfig(ctx, "nonexistent", "tenant1")
	assert.Error(t, err)
}

func TestUpdateConfig(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)
	ctx := context.Background()

	// Create a config
	config := &InventoryConfig{
		BucketName:        "test-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest-bucket",
		IncludedFields:    []string{"bucket_name"},
		ScheduleTime:      "01:00",
	}
	err := manager.CreateConfig(ctx, config)
	require.NoError(t, err)

	// Update it
	config.Frequency = "weekly"
	config.Format = "json"
	config.DestinationBucket = "new-dest-bucket"

	err = manager.UpdateConfig(ctx, config)
	require.NoError(t, err)

	// Verify update
	updated, err := manager.GetConfig(ctx, "test-bucket", "tenant1")
	require.NoError(t, err)
	assert.Equal(t, "weekly", updated.Frequency)
	assert.Equal(t, "json", updated.Format)
	assert.Equal(t, "new-dest-bucket", updated.DestinationBucket)
}

func TestDeleteConfig(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)
	ctx := context.Background()

	// Create a config
	config := &InventoryConfig{
		BucketName:        "test-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest-bucket",
		IncludedFields:    []string{"bucket_name"},
		ScheduleTime:      "01:00",
	}
	err := manager.CreateConfig(ctx, config)
	require.NoError(t, err)

	// Delete it
	err = manager.DeleteConfig(ctx, "test-bucket", "tenant1")
	require.NoError(t, err)

	// Verify deletion
	_, err = manager.GetConfig(ctx, "test-bucket", "tenant1")
	assert.Error(t, err)
}

func TestListReadyConfigs(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)
	ctx := context.Background()

	now := time.Now().Unix()
	past := now - 3600 // 1 hour ago
	future := now + 3600 // 1 hour from now

	// Create config that should be ready (next_run_at in past)
	config1 := &InventoryConfig{
		BucketName:        "bucket1",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest",
		IncludedFields:    []string{"bucket_name"},
		ScheduleTime:      "01:00",
	}
	err := manager.CreateConfig(ctx, config1)
	require.NoError(t, err)
	// Update to set next_run_at in the past
	config1.NextRunAt = &past
	err = manager.UpdateConfig(ctx, config1)
	require.NoError(t, err)

	// Create config that should NOT be ready (next_run_at in future)
	config2 := &InventoryConfig{
		BucketName:        "bucket2",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest",
		IncludedFields:    []string{"bucket_name"},
		ScheduleTime:      "01:00",
	}
	err = manager.CreateConfig(ctx, config2)
	require.NoError(t, err)
	// Update to set next_run_at in the future
	config2.NextRunAt = &future
	err = manager.UpdateConfig(ctx, config2)
	require.NoError(t, err)

	// Create disabled config (should NOT be ready)
	config3 := &InventoryConfig{
		BucketName:        "bucket3",
		TenantID:          "tenant1",
		Enabled:           false,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest",
		IncludedFields:    []string{"bucket_name"},
		ScheduleTime:      "01:00",
	}
	err = manager.CreateConfig(ctx, config3)
	require.NoError(t, err)
	// Update to set next_run_at in the past (but it's disabled so won't be ready)
	config3.NextRunAt = &past
	err = manager.UpdateConfig(ctx, config3)
	require.NoError(t, err)

	// List ready configs
	ready, err := manager.ListReadyConfigs(ctx)
	require.NoError(t, err)
	assert.Len(t, ready, 1)
	assert.Equal(t, "bucket1", ready[0].BucketName)
}

func TestCreateReport(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)
	ctx := context.Background()

	// Create a config first
	config := &InventoryConfig{
		BucketName:        "test-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest",
		IncludedFields:    []string{"bucket_name"},
		ScheduleTime:      "01:00",
	}
	err := manager.CreateConfig(ctx, config)
	require.NoError(t, err)

	// Create a report
	now := time.Now().Unix()
	report := &InventoryReport{
		ConfigID:    config.ID,
		BucketName:  "test-bucket",
		ReportPath:  "inventory/report.csv",
		ObjectCount: 100,
		TotalSize:   1024000,
		Status:      "completed",
		StartedAt:   &now,
		CompletedAt: &now,
	}

	err = manager.CreateReport(ctx, report)
	require.NoError(t, err)
	assert.NotEmpty(t, report.ID)
	assert.NotZero(t, report.CreatedAt)
}

func TestListReports(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)
	ctx := context.Background()

	// Create a config
	config := &InventoryConfig{
		BucketName:        "test-bucket",
		TenantID:          "tenant1",
		Enabled:           true,
		Frequency:         "daily",
		Format:            "csv",
		DestinationBucket: "dest",
		IncludedFields:    []string{"bucket_name"},
		ScheduleTime:      "01:00",
	}
	err := manager.CreateConfig(ctx, config)
	require.NoError(t, err)

	// Create multiple reports
	for i := 0; i < 5; i++ {
		now := time.Now().Unix()
		report := &InventoryReport{
			ConfigID:    config.ID,
			BucketName:  "test-bucket",
			ReportPath:  "inventory/report.csv",
			ObjectCount: int64(i * 10),
			TotalSize:   int64(i * 1024),
			Status:      "completed",
			StartedAt:   &now,
			CompletedAt: &now,
		}
		err = manager.CreateReport(ctx, report)
		require.NoError(t, err)
	}

	// List reports with pagination
	reports, err := manager.ListReports(ctx, "test-bucket", "tenant1", 3, 0)
	require.NoError(t, err)
	assert.Len(t, reports, 3)

	// List with offset
	reports, err = manager.ListReports(ctx, "test-bucket", "tenant1", 3, 3)
	require.NoError(t, err)
	assert.Len(t, reports, 2)
}

func TestCalculateNextRunTime(t *testing.T) {
	tests := []struct {
		name         string
		frequency    string
		scheduleTime string
		lastRun      *int64
		wantErr      bool
	}{
		{
			name:         "daily with no previous run",
			frequency:    "daily",
			scheduleTime: "02:00",
			lastRun:      nil,
			wantErr:      false,
		},
		{
			name:         "weekly with no previous run",
			frequency:    "weekly",
			scheduleTime: "03:00",
			lastRun:      nil,
			wantErr:      false,
		},
		{
			name:         "daily with previous run",
			frequency:    "daily",
			scheduleTime: "02:00",
			lastRun:      func() *int64 { t := time.Now().Add(-25 * time.Hour).Unix(); return &t }(),
			wantErr:      false,
		},
		{
			name:         "invalid frequency",
			frequency:    "monthly",
			scheduleTime: "02:00",
			lastRun:      nil,
			wantErr:      true,
		},
		{
			name:         "invalid time format",
			frequency:    "daily",
			scheduleTime: "25:00",
			lastRun:      nil,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextRun, err := CalculateNextRunTime(tt.frequency, tt.scheduleTime, tt.lastRun)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Greater(t, nextRun, time.Now().Unix())
			}
		})
	}
}

func TestValidateIncludedFields(t *testing.T) {
	tests := []struct {
		name   string
		fields []string
		want   bool
	}{
		{
			name:   "valid fields",
			fields: []string{"bucket_name", "object_key", "size"},
			want:   true,
		},
		{
			name:   "invalid field",
			fields: []string{"bucket_name", "invalid_field"},
			want:   false,
		},
		{
			name:   "empty list",
			fields: []string{},
			want:   true, // Empty is valid, will use defaults
		},
		{
			name:   "all valid fields",
			fields: []string{"bucket_name", "object_key", "version_id", "is_latest", "size", "last_modified", "etag", "storage_class", "is_multipart_uploaded", "encryption_status", "replication_status", "object_acl"},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateIncludedFields(tt.fields)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultIncludedFields(t *testing.T) {
	fields := DefaultIncludedFields()
	assert.NotEmpty(t, fields)
	assert.Contains(t, fields, "bucket_name")
	assert.Contains(t, fields, "object_key")
	assert.Contains(t, fields, "size")
}
