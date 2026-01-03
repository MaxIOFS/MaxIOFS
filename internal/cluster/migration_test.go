package cluster

import (
	"context"
	"testing"
)

// TestMigrationJobCRUD tests basic CRUD operations for migration jobs
func TestMigrationJobCRUD(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Initialize cluster
	_, err := manager.InitializeCluster(ctx, "node-1", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	config, err := manager.GetConfig(ctx)
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	// Test 1: List migrations when empty
	jobs, err := manager.ListMigrationJobs(ctx)
	if err != nil {
		t.Fatalf("Failed to list migrations: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("Expected 0 migrations, got %d", len(jobs))
	}

	// Test 2: Create a migration job via CreateMigrationJob
	job := &MigrationJob{
		BucketName:      "test-bucket",
		SourceNodeID:    config.NodeID,
		TargetNodeID:    "target-node",
		Status:          MigrationStatusPending,
		ObjectsTotal:    100,
		ObjectsMigrated: 0,
		BytesTotal:      102400,
		BytesMigrated:   0,
		DeleteSource:    false,
		VerifyData:      true,
	}

	err = manager.CreateMigrationJob(ctx, job)
	if err != nil {
		t.Fatalf("Failed to create migration job: %v", err)
	}

	if job.ID == 0 {
		t.Error("Expected non-zero migration ID after creation")
	}

	// Test 3: Get the migration job
	retrieved, err := manager.GetMigrationJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("Failed to get migration job: %v", err)
	}

	if retrieved.BucketName != "test-bucket" {
		t.Errorf("Expected bucket 'test-bucket', got '%s'", retrieved.BucketName)
	}
	if retrieved.Status != MigrationStatusPending {
		t.Errorf("Expected status 'pending', got '%s'", retrieved.Status)
	}

	// Test 4: Update migration status and progress
	retrieved.Status = MigrationStatusInProgress
	retrieved.ObjectsMigrated = 50
	retrieved.BytesMigrated = 51200

	err = manager.UpdateMigrationJob(ctx, retrieved)
	if err != nil {
		t.Fatalf("Failed to update migration job: %v", err)
	}

	updated, err := manager.GetMigrationJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("Failed to get updated migration: %v", err)
	}
	if updated.Status != MigrationStatusInProgress {
		t.Errorf("Expected status 'in_progress', got '%s'", updated.Status)
	}
	if updated.ObjectsMigrated != 50 {
		t.Errorf("Expected 50 objects migrated, got %d", updated.ObjectsMigrated)
	}
	if updated.BytesMigrated != 51200 {
		t.Errorf("Expected 51200 bytes migrated, got %d", updated.BytesMigrated)
	}

	// Test 6: List migrations (should have 1)
	jobs, err = manager.ListMigrationJobs(ctx)
	if err != nil {
		t.Fatalf("Failed to list migrations: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("Expected 1 migration, got %d", len(jobs))
	}

	// Test 7: Get migrations by bucket
	bucketJobs, err := manager.GetMigrationJobsByBucket(ctx, "test-bucket")
	if err != nil {
		t.Fatalf("Failed to get migrations by bucket: %v", err)
	}
	if len(bucketJobs) != 1 {
		t.Errorf("Expected 1 migration for test-bucket, got %d", len(bucketJobs))
	}

	// Test 5: Mark as failed with error
	updated.Status = MigrationStatusFailed
	updated.ErrorMessage = "Test error"

	err = manager.UpdateMigrationJob(ctx, updated)
	if err != nil {
		t.Fatalf("Failed to update status with error: %v", err)
	}

	failed, err := manager.GetMigrationJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("Failed to get failed migration: %v", err)
	}
	if failed.Status != MigrationStatusFailed {
		t.Errorf("Expected status 'failed', got '%s'", failed.Status)
	}
	if failed.ErrorMessage != "Test error" {
		t.Errorf("Expected error 'Test error', got '%s'", failed.ErrorMessage)
	}
}

// TestGetMigrationJob_NotFound tests error handling for non-existent migration
func TestGetMigrationJob_NotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	_, err := manager.GetMigrationJob(ctx, 999)
	if err == nil {
		t.Error("Expected error when getting non-existent migration")
	}
}
