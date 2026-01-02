package cluster

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func setupMigrationTestDB(t *testing.T) (*sql.DB, func()) {
	db, cleanup := setupTestDB(t)

	// Create a test tenant and bucket for migration tests
	ctx := context.Background()

	// Insert test tenant
	_, err := db.ExecContext(ctx, `
		INSERT INTO tenants (id, name, created_at, updated_at)
		VALUES ('test-tenant', 'Test Tenant', ?, ?)
	`, time.Now().Unix(), time.Now().Unix())
	if err != nil {
		cleanup()
		t.Fatalf("Failed to insert test tenant: %v", err)
	}

	// Insert test bucket
	_, err = db.ExecContext(ctx, `
		INSERT INTO buckets (name, tenant_id, created_at, updated_at)
		VALUES ('test-bucket', 'test-tenant', ?, ?)
	`, time.Now().Unix(), time.Now().Unix())
	if err != nil {
		cleanup()
		t.Fatalf("Failed to insert test bucket: %v", err)
	}

	// Insert some test objects
	for i := 0; i < 10; i++ {
		_, err = db.ExecContext(ctx, `
			INSERT INTO objects (bucket, key, tenant_id, size, etag, created_at, updated_at)
			VALUES ('test-bucket', ?, 'test-tenant', 1024, 'test-etag', ?, ?)
		`, "object-"+string(rune(i)), time.Now().Unix(), time.Now().Unix())
		if err != nil {
			cleanup()
			t.Fatalf("Failed to insert test object %d: %v", i, err)
		}
	}

	return db, cleanup
}

func TestCreateMigration(t *testing.T) {
	db, cleanup := setupMigrationTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Initialize cluster
	_, err := manager.InitializeCluster(ctx, "source-node", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	// Add target node
	targetNodeID, err := manager.AddNode(ctx, AddNodeRequest{
		Name:     "target-node",
		Endpoint: "http://target:8080",
		Region:   "us-west-1",
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("Failed to add target node: %v", err)
	}

	// Get local node ID
	config, err := manager.GetConfig(ctx)
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	// Create migration
	job, err := manager.CreateMigration(ctx, "test-tenant", "test-bucket", config.NodeID, targetNodeID, true, false)
	if err != nil {
		t.Fatalf("Failed to create migration: %v", err)
	}

	// Verify migration was created
	if job.ID == 0 {
		t.Error("Expected non-zero migration ID")
	}

	if job.BucketName != "test-bucket" {
		t.Errorf("Expected bucket name 'test-bucket', got '%s'", job.BucketName)
	}

	if job.Status != "pending" {
		t.Errorf("Expected status 'pending', got '%s'", job.Status)
	}

	if job.SourceNodeID != config.NodeID {
		t.Errorf("Expected source node ID '%s', got '%s'", config.NodeID, job.SourceNodeID)
	}

	if job.TargetNodeID != targetNodeID {
		t.Errorf("Expected target node ID '%s', got '%s'", targetNodeID, job.TargetNodeID)
	}

	if !job.VerifyData {
		t.Error("Expected verify_data to be true")
	}

	if job.DeleteSource {
		t.Error("Expected delete_source to be false")
	}
}

func TestGetMigration(t *testing.T) {
	db, cleanup := setupMigrationTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Initialize cluster and add target node
	_, err := manager.InitializeCluster(ctx, "source-node", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	config, err := manager.GetConfig(ctx)
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	targetNodeID, err := manager.AddNode(ctx, AddNodeRequest{
		Name:     "target-node",
		Endpoint: "http://target:8080",
		Region:   "us-west-1",
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("Failed to add target node: %v", err)
	}

	// Create migration
	created, err := manager.CreateMigration(ctx, "test-tenant", "test-bucket", config.NodeID, targetNodeID, true, false)
	if err != nil {
		t.Fatalf("Failed to create migration: %v", err)
	}

	// Get migration
	retrieved, err := manager.GetMigration(ctx, "test-tenant", created.ID)
	if err != nil {
		t.Fatalf("Failed to get migration: %v", err)
	}

	// Verify fields match
	if retrieved.ID != created.ID {
		t.Errorf("Expected ID %d, got %d", created.ID, retrieved.ID)
	}

	if retrieved.BucketName != created.BucketName {
		t.Errorf("Expected bucket name '%s', got '%s'", created.BucketName, retrieved.BucketName)
	}

	if retrieved.Status != created.Status {
		t.Errorf("Expected status '%s', got '%s'", created.Status, retrieved.Status)
	}
}

func TestGetMigration_NotFound(t *testing.T) {
	db, cleanup := setupMigrationTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Try to get non-existent migration
	_, err := manager.GetMigration(ctx, "test-tenant", 999)
	if err == nil {
		t.Error("Expected error when getting non-existent migration")
	}
}

func TestListMigrations(t *testing.T) {
	db, cleanup := setupMigrationTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Initialize cluster and add target node
	_, err := manager.InitializeCluster(ctx, "source-node", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	config, err := manager.GetConfig(ctx)
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	targetNodeID, err := manager.AddNode(ctx, AddNodeRequest{
		Name:     "target-node",
		Endpoint: "http://target:8080",
		Region:   "us-west-1",
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("Failed to add target node: %v", err)
	}

	// Create multiple migrations
	_, err = manager.CreateMigration(ctx, "test-tenant", "test-bucket", config.NodeID, targetNodeID, true, false)
	if err != nil {
		t.Fatalf("Failed to create migration 1: %v", err)
	}

	_, err = manager.CreateMigration(ctx, "test-tenant", "test-bucket", config.NodeID, targetNodeID, true, false)
	if err != nil {
		t.Fatalf("Failed to create migration 2: %v", err)
	}

	// List all migrations
	migrations, err := manager.ListMigrations(ctx, "test-tenant", "")
	if err != nil {
		t.Fatalf("Failed to list migrations: %v", err)
	}

	if len(migrations) != 2 {
		t.Errorf("Expected 2 migrations, got %d", len(migrations))
	}

	// List migrations for specific bucket
	migrations, err = manager.ListMigrations(ctx, "test-tenant", "test-bucket")
	if err != nil {
		t.Fatalf("Failed to list migrations for bucket: %v", err)
	}

	if len(migrations) != 2 {
		t.Errorf("Expected 2 migrations for test-bucket, got %d", len(migrations))
	}

	// List migrations for non-existent bucket
	migrations, err = manager.ListMigrations(ctx, "test-tenant", "non-existent-bucket")
	if err != nil {
		t.Fatalf("Failed to list migrations for non-existent bucket: %v", err)
	}

	if len(migrations) != 0 {
		t.Errorf("Expected 0 migrations for non-existent bucket, got %d", len(migrations))
	}
}

func TestUpdateMigrationStatus(t *testing.T) {
	db, cleanup := setupMigrationTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Initialize cluster and add target node
	_, err := manager.InitializeCluster(ctx, "source-node", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	config, err := manager.GetConfig(ctx)
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	targetNodeID, err := manager.AddNode(ctx, AddNodeRequest{
		Name:     "target-node",
		Endpoint: "http://target:8080",
		Region:   "us-west-1",
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("Failed to add target node: %v", err)
	}

	// Create migration
	job, err := manager.CreateMigration(ctx, "test-tenant", "test-bucket", config.NodeID, targetNodeID, true, false)
	if err != nil {
		t.Fatalf("Failed to create migration: %v", err)
	}

	// Update status to in_progress
	err = manager.UpdateMigrationStatus(ctx, job.ID, "in_progress", nil)
	if err != nil {
		t.Fatalf("Failed to update migration status: %v", err)
	}

	// Verify status was updated
	updated, err := manager.GetMigration(ctx, "test-tenant", job.ID)
	if err != nil {
		t.Fatalf("Failed to get updated migration: %v", err)
	}

	if updated.Status != "in_progress" {
		t.Errorf("Expected status 'in_progress', got '%s'", updated.Status)
	}

	// Update status to completed
	err = manager.UpdateMigrationStatus(ctx, job.ID, "completed", nil)
	if err != nil {
		t.Fatalf("Failed to update migration status to completed: %v", err)
	}

	completed, err := manager.GetMigration(ctx, "test-tenant", job.ID)
	if err != nil {
		t.Fatalf("Failed to get completed migration: %v", err)
	}

	if completed.Status != "completed" {
		t.Errorf("Expected status 'completed', got '%s'", completed.Status)
	}

	if completed.CompletedAt == nil {
		t.Error("Expected completed_at to be set")
	}
}

func TestUpdateMigrationStatus_WithError(t *testing.T) {
	db, cleanup := setupMigrationTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Initialize cluster and add target node
	_, err := manager.InitializeCluster(ctx, "source-node", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	config, err := manager.GetConfig(ctx)
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	targetNodeID, err := manager.AddNode(ctx, AddNodeRequest{
		Name:     "target-node",
		Endpoint: "http://target:8080",
		Region:   "us-west-1",
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("Failed to add target node: %v", err)
	}

	// Create migration
	job, err := manager.CreateMigration(ctx, "test-tenant", "test-bucket", config.NodeID, targetNodeID, true, false)
	if err != nil {
		t.Fatalf("Failed to create migration: %v", err)
	}

	// Update status to failed with error message
	errorMsg := "Network timeout"
	err = manager.UpdateMigrationStatus(ctx, job.ID, "failed", &errorMsg)
	if err != nil {
		t.Fatalf("Failed to update migration status with error: %v", err)
	}

	// Verify status and error message were updated
	failed, err := manager.GetMigration(ctx, "test-tenant", job.ID)
	if err != nil {
		t.Fatalf("Failed to get failed migration: %v", err)
	}

	if failed.Status != "failed" {
		t.Errorf("Expected status 'failed', got '%s'", failed.Status)
	}

	if failed.ErrorMessage == nil {
		t.Fatal("Expected error message to be set")
	}

	if *failed.ErrorMessage != errorMsg {
		t.Errorf("Expected error message '%s', got '%s'", errorMsg, *failed.ErrorMessage)
	}
}

func TestCountBucketObjects(t *testing.T) {
	db, cleanup := setupMigrationTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Count objects in test bucket (we inserted 10 objects of 1024 bytes each)
	count, totalSize, err := manager.countBucketObjects(ctx, "test-tenant", "test-bucket")
	if err != nil {
		t.Fatalf("Failed to count bucket objects: %v", err)
	}

	if count != 10 {
		t.Errorf("Expected 10 objects, got %d", count)
	}

	expectedSize := int64(10 * 1024) // 10 objects * 1024 bytes
	if totalSize != expectedSize {
		t.Errorf("Expected total size %d, got %d", expectedSize, totalSize)
	}
}

func TestCountBucketObjects_EmptyBucket(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Insert test tenant and empty bucket
	_, err := db.ExecContext(ctx, `
		INSERT INTO tenants (id, name, created_at, updated_at)
		VALUES ('test-tenant', 'Test Tenant', ?, ?)
	`, time.Now().Unix(), time.Now().Unix())
	if err != nil {
		t.Fatalf("Failed to insert test tenant: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO buckets (name, tenant_id, created_at, updated_at)
		VALUES ('empty-bucket', 'test-tenant', ?, ?)
	`, time.Now().Unix(), time.Now().Unix())
	if err != nil {
		t.Fatalf("Failed to insert empty bucket: %v", err)
	}

	// Count objects in empty bucket
	count, totalSize, err := manager.countBucketObjects(ctx, "test-tenant", "empty-bucket")
	if err != nil {
		t.Fatalf("Failed to count empty bucket objects: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected 0 objects, got %d", count)
	}

	if totalSize != 0 {
		t.Errorf("Expected total size 0, got %d", totalSize)
	}
}

func TestUpdateMigrationProgress(t *testing.T) {
	db, cleanup := setupMigrationTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Initialize cluster and add target node
	_, err := manager.InitializeCluster(ctx, "source-node", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	config, err := manager.GetConfig(ctx)
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	targetNodeID, err := manager.AddNode(ctx, AddNodeRequest{
		Name:     "target-node",
		Endpoint: "http://target:8080",
		Region:   "us-west-1",
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("Failed to add target node: %v", err)
	}

	// Create migration
	job, err := manager.CreateMigration(ctx, "test-tenant", "test-bucket", config.NodeID, targetNodeID, true, false)
	if err != nil {
		t.Fatalf("Failed to create migration: %v", err)
	}

	// Update progress
	err = manager.UpdateMigrationProgress(ctx, job.ID, 5, 5120)
	if err != nil {
		t.Fatalf("Failed to update migration progress: %v", err)
	}

	// Verify progress was updated
	updated, err := manager.GetMigration(ctx, "test-tenant", job.ID)
	if err != nil {
		t.Fatalf("Failed to get updated migration: %v", err)
	}

	if updated.ObjectsMigrated != 5 {
		t.Errorf("Expected 5 objects migrated, got %d", updated.ObjectsMigrated)
	}

	if updated.BytesMigrated != 5120 {
		t.Errorf("Expected 5120 bytes migrated, got %d", updated.BytesMigrated)
	}

	// Update progress again
	err = manager.UpdateMigrationProgress(ctx, job.ID, 10, 10240)
	if err != nil {
		t.Fatalf("Failed to update migration progress second time: %v", err)
	}

	// Verify progress was updated
	updated2, err := manager.GetMigration(ctx, "test-tenant", job.ID)
	if err != nil {
		t.Fatalf("Failed to get updated migration second time: %v", err)
	}

	if updated2.ObjectsMigrated != 10 {
		t.Errorf("Expected 10 objects migrated, got %d", updated2.ObjectsMigrated)
	}

	if updated2.BytesMigrated != 10240 {
		t.Errorf("Expected 10240 bytes migrated, got %d", updated2.BytesMigrated)
	}
}

func TestMigrationJobFields(t *testing.T) {
	db, cleanup := setupMigrationTestDB(t)
	defer cleanup()

	manager := NewManager(db, "http://localhost:8080")
	ctx := context.Background()

	// Initialize cluster and add target node
	_, err := manager.InitializeCluster(ctx, "source-node", "us-east-1")
	if err != nil {
		t.Fatalf("Failed to initialize cluster: %v", err)
	}

	config, err := manager.GetConfig(ctx)
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}

	targetNodeID, err := manager.AddNode(ctx, AddNodeRequest{
		Name:     "target-node",
		Endpoint: "http://target:8080",
		Region:   "us-west-1",
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("Failed to add target node: %v", err)
	}

	// Create migration with specific settings
	job, err := manager.CreateMigration(ctx, "test-tenant", "test-bucket", config.NodeID, targetNodeID, false, true)
	if err != nil {
		t.Fatalf("Failed to create migration: %v", err)
	}

	// Verify all fields
	if job.TenantID != "test-tenant" {
		t.Errorf("Expected tenant ID 'test-tenant', got '%s'", job.TenantID)
	}

	if job.VerifyData {
		t.Error("Expected verify_data to be false")
	}

	if !job.DeleteSource {
		t.Error("Expected delete_source to be true")
	}

	if job.CreatedAt.IsZero() {
		t.Error("Expected created_at to be set")
	}

	if job.UpdatedAt.IsZero() {
		t.Error("Expected updated_at to be set")
	}

	if job.StartedAt != nil {
		t.Error("Expected started_at to be nil for pending migration")
	}

	if job.CompletedAt != nil {
		t.Error("Expected completed_at to be nil for pending migration")
	}

	if job.ErrorMessage != nil {
		t.Error("Expected error_message to be nil")
	}
}
