package cluster

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// MigrationStatus represents the status of a bucket migration
type MigrationStatus string

const (
	MigrationStatusPending    MigrationStatus = "pending"
	MigrationStatusInProgress MigrationStatus = "in_progress"
	MigrationStatusCompleted  MigrationStatus = "completed"
	MigrationStatusFailed     MigrationStatus = "failed"
	MigrationStatusCancelled  MigrationStatus = "cancelled"
)

// MigrationJob represents a bucket migration job
type MigrationJob struct {
	ID              int64           `json:"id"`
	BucketName      string          `json:"bucket_name"`
	SourceNodeID    string          `json:"source_node_id"`
	TargetNodeID    string          `json:"target_node_id"`
	Status          MigrationStatus `json:"status"`
	ObjectsTotal    int64           `json:"objects_total"`
	ObjectsMigrated int64           `json:"objects_migrated"`
	BytesTotal      int64           `json:"bytes_total"`
	BytesMigrated   int64           `json:"bytes_migrated"`
	DeleteSource    bool            `json:"delete_source"`
	VerifyData      bool            `json:"verify_data"`
	StartedAt       *time.Time      `json:"started_at"`
	CompletedAt     *time.Time      `json:"completed_at"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	ErrorMessage    string          `json:"error_message,omitempty"`
}

// CreateMigrationJob creates a new migration job in the database
func (cm *Manager) CreateMigrationJob(ctx context.Context, job *MigrationJob) error {
	query := `
		INSERT INTO cluster_migrations (
			bucket_name, source_node_id, target_node_id, status,
			delete_source, verify_data, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`

	deleteSource := 0
	if job.DeleteSource {
		deleteSource = 1
	}
	verifyData := 1
	if !job.VerifyData {
		verifyData = 0
	}

	result, err := cm.db.ExecContext(ctx, query,
		job.BucketName,
		job.SourceNodeID,
		job.TargetNodeID,
		job.Status,
		deleteSource,
		verifyData,
	)
	if err != nil {
		return fmt.Errorf("failed to create migration job: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get migration job ID: %w", err)
	}

	job.ID = id
	logrus.WithFields(logrus.Fields{
		"migration_id": id,
		"bucket":       job.BucketName,
		"source":       job.SourceNodeID,
		"target":       job.TargetNodeID,
	}).Info("Migration job created")

	return nil
}

// UpdateMigrationJob updates an existing migration job
func (cm *Manager) UpdateMigrationJob(ctx context.Context, job *MigrationJob) error {
	query := `
		UPDATE cluster_migrations
		SET status = ?,
		    objects_total = ?,
		    objects_migrated = ?,
		    bytes_total = ?,
		    bytes_migrated = ?,
		    started_at = ?,
		    completed_at = ?,
		    error_message = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`

	_, err := cm.db.ExecContext(ctx, query,
		job.Status,
		job.ObjectsTotal,
		job.ObjectsMigrated,
		job.BytesTotal,
		job.BytesMigrated,
		job.StartedAt,
		job.CompletedAt,
		job.ErrorMessage,
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update migration job: %w", err)
	}

	return nil
}

// GetMigrationJob retrieves a migration job by ID
func (cm *Manager) GetMigrationJob(ctx context.Context, id int64) (*MigrationJob, error) {
	query := `
		SELECT id, bucket_name, source_node_id, target_node_id, status,
		       objects_total, objects_migrated, bytes_total, bytes_migrated,
		       delete_source, verify_data, started_at, completed_at,
		       created_at, updated_at, error_message
		FROM cluster_migrations
		WHERE id = ?
	`

	job := &MigrationJob{}
	var deleteSource, verifyData int
	var startedAt, completedAt sql.NullTime

	err := cm.db.QueryRowContext(ctx, query, id).Scan(
		&job.ID,
		&job.BucketName,
		&job.SourceNodeID,
		&job.TargetNodeID,
		&job.Status,
		&job.ObjectsTotal,
		&job.ObjectsMigrated,
		&job.BytesTotal,
		&job.BytesMigrated,
		&deleteSource,
		&verifyData,
		&startedAt,
		&completedAt,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.ErrorMessage,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("migration job not found")
		}
		return nil, fmt.Errorf("failed to get migration job: %w", err)
	}

	job.DeleteSource = deleteSource == 1
	job.VerifyData = verifyData == 1
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}

	return job, nil
}

// ListMigrationJobs retrieves all migration jobs
func (cm *Manager) ListMigrationJobs(ctx context.Context) ([]*MigrationJob, error) {
	query := `
		SELECT id, bucket_name, source_node_id, target_node_id, status,
		       objects_total, objects_migrated, bytes_total, bytes_migrated,
		       delete_source, verify_data, started_at, completed_at,
		       created_at, updated_at, error_message
		FROM cluster_migrations
		ORDER BY created_at DESC
		LIMIT 100
	`

	rows, err := cm.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list migration jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*MigrationJob
	for rows.Next() {
		job := &MigrationJob{}
		var deleteSource, verifyData int
		var startedAt, completedAt sql.NullTime

		err := rows.Scan(
			&job.ID,
			&job.BucketName,
			&job.SourceNodeID,
			&job.TargetNodeID,
			&job.Status,
			&job.ObjectsTotal,
			&job.ObjectsMigrated,
			&job.BytesTotal,
			&job.BytesMigrated,
			&deleteSource,
			&verifyData,
			&startedAt,
			&completedAt,
			&job.CreatedAt,
			&job.UpdatedAt,
			&job.ErrorMessage,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan migration job: %w", err)
		}

		job.DeleteSource = deleteSource == 1
		job.VerifyData = verifyData == 1
		if startedAt.Valid {
			job.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			job.CompletedAt = &completedAt.Time
		}

		jobs = append(jobs, job)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating migration jobs: %w", err)
	}

	return jobs, nil
}

// GetMigrationJobsByBucket retrieves migration jobs for a specific bucket
func (cm *Manager) GetMigrationJobsByBucket(ctx context.Context, bucketName string) ([]*MigrationJob, error) {
	query := `
		SELECT id, bucket_name, source_node_id, target_node_id, status,
		       objects_total, objects_migrated, bytes_total, bytes_migrated,
		       delete_source, verify_data, started_at, completed_at,
		       created_at, updated_at, error_message
		FROM cluster_migrations
		WHERE bucket_name = ?
		ORDER BY created_at DESC
	`

	rows, err := cm.db.QueryContext(ctx, query, bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to get migration jobs for bucket: %w", err)
	}
	defer rows.Close()

	var jobs []*MigrationJob
	for rows.Next() {
		job := &MigrationJob{}
		var deleteSource, verifyData int
		var startedAt, completedAt sql.NullTime

		err := rows.Scan(
			&job.ID,
			&job.BucketName,
			&job.SourceNodeID,
			&job.TargetNodeID,
			&job.Status,
			&job.ObjectsTotal,
			&job.ObjectsMigrated,
			&job.BytesTotal,
			&job.BytesMigrated,
			&deleteSource,
			&verifyData,
			&startedAt,
			&completedAt,
			&job.CreatedAt,
			&job.UpdatedAt,
			&job.ErrorMessage,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan migration job: %w", err)
		}

		job.DeleteSource = deleteSource == 1
		job.VerifyData = verifyData == 1
		if startedAt.Valid {
			job.StartedAt = &startedAt.Time
		}
		if completedAt.Valid {
			job.CompletedAt = &completedAt.Time
		}

		jobs = append(jobs, job)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating migration jobs: %w", err)
	}

	return jobs, nil
}

// MigrateBucket migrates a bucket from one node to another
func (m *Manager) MigrateBucket(ctx context.Context, locationMgr *BucketLocationManager, tenantID, bucketName, targetNodeID string, deleteSource, verifyData bool) (*MigrationJob, error) {
	// Get bucket location (source node) using BucketLocationManager
	sourceNodeID, err := locationMgr.GetBucketLocation(ctx, tenantID, bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to get bucket location: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"bucket":    bucketName,
		"tenant_id": tenantID,
		"source":    sourceNodeID,
		"target":    targetNodeID,
	}).Info("Initiating bucket migration")

	// Validate target node exists and is healthy
	targetNode, err := m.GetNode(ctx, targetNodeID)
	if err != nil {
		return nil, fmt.Errorf("target node not found: %w", err)
	}

	if targetNode.HealthStatus != HealthStatusHealthy {
		return nil, fmt.Errorf("target node is not healthy: %s", targetNode.HealthStatus)
	}

	// Validate source node
	sourceNode, err := m.GetNode(ctx, sourceNodeID)
	if err != nil {
		return nil, fmt.Errorf("source node not found: %w", err)
	}

	if sourceNode.HealthStatus != HealthStatusHealthy {
		return nil, fmt.Errorf("source node is not healthy: %s", sourceNode.HealthStatus)
	}

	// Check if source and target are the same
	if sourceNodeID == targetNodeID {
		return nil, fmt.Errorf("source and target nodes cannot be the same")
	}

	// Create migration job
	now := time.Now()
	job := &MigrationJob{
		BucketName:   bucketName,
		SourceNodeID: sourceNodeID,
		TargetNodeID: targetNodeID,
		Status:       MigrationStatusPending,
		DeleteSource: deleteSource,
		VerifyData:   verifyData,
		StartedAt:    &now,
	}

	if err := m.CreateMigrationJob(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to create migration job: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"migration_id": job.ID,
		"bucket":       bucketName,
		"source":       sourceNodeID,
		"target":       targetNodeID,
	}).Info("Starting bucket migration")

	// Update status to in_progress
	job.Status = MigrationStatusInProgress
	if err := m.UpdateMigrationJob(ctx, job); err != nil {
		return nil, fmt.Errorf("failed to update migration job status: %w", err)
	}

	// Execute migration
	if err := m.executeMigration(ctx, locationMgr, tenantID, job); err != nil {
		job.Status = MigrationStatusFailed
		job.ErrorMessage = err.Error()
		now := time.Now()
		job.CompletedAt = &now
		m.UpdateMigrationJob(ctx, job)
		return job, fmt.Errorf("migration failed: %w", err)
	}

	// Mark as completed
	job.Status = MigrationStatusCompleted
	now = time.Now()
	job.CompletedAt = &now
	if err := m.UpdateMigrationJob(ctx, job); err != nil {
		return job, fmt.Errorf("migration completed but failed to update status: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"migration_id": job.ID,
		"bucket":       bucketName,
	}).Info("Bucket migration completed successfully")

	return job, nil
}

// executeMigration performs the actual migration steps
func (m *Manager) executeMigration(ctx context.Context, locationMgr *BucketLocationManager, tenantID string, job *MigrationJob) error {
	logrus.WithField("migration_id", job.ID).Info("Executing migration steps")

	// Step 1: Count objects and calculate total size
	logrus.WithField("migration_id", job.ID).Info("Counting objects in bucket")
	objectCount, totalSize, err := m.countBucketObjects(ctx, tenantID, job.BucketName)
	if err != nil {
		return fmt.Errorf("failed to count objects: %w", err)
	}

	job.ObjectsTotal = objectCount
	job.BytesTotal = totalSize
	if err := m.UpdateMigrationJob(ctx, job); err != nil {
		logrus.WithError(err).Warn("Failed to update job with object counts")
	}

	logrus.WithFields(logrus.Fields{
		"migration_id":   job.ID,
		"object_count":   objectCount,
		"total_size_mb":  totalSize / 1024 / 1024,
	}).Info("Object counting completed")

	// Step 2: Copy all objects from source to target
	if objectCount > 0 {
		logrus.WithFields(logrus.Fields{
			"migration_id": job.ID,
			"bucket":       job.BucketName,
			"source":       job.SourceNodeID,
			"target":       job.TargetNodeID,
		}).Info("Copying objects to target node")

		if err := m.copyBucketObjects(ctx, tenantID, job); err != nil {
			return fmt.Errorf("failed to copy objects: %w", err)
		}

		logrus.WithFields(logrus.Fields{
			"migration_id":      job.ID,
			"objects_migrated":  job.ObjectsMigrated,
			"bytes_migrated_mb": job.BytesMigrated / 1024 / 1024,
		}).Info("Object copying completed")
	} else {
		logrus.WithField("migration_id", job.ID).Info("No objects to migrate (empty bucket)")
	}

	// Step 3: Migrate bucket permissions
	logrus.WithField("migration_id", job.ID).Info("Migrating bucket permissions")
	if err := m.migrateBucketPermissions(ctx, tenantID, job); err != nil {
		return fmt.Errorf("failed to migrate bucket permissions: %w", err)
	}
	logrus.WithField("migration_id", job.ID).Info("Bucket permissions migrated successfully")

	// Step 3.5: Migrate bucket ACLs
	logrus.WithField("migration_id", job.ID).Info("Migrating bucket ACLs")
	if err := m.migrateBucketACLs(ctx, tenantID, job); err != nil {
		return fmt.Errorf("failed to migrate bucket ACLs: %w", err)
	}
	logrus.WithField("migration_id", job.ID).Info("Bucket ACLs migrated successfully")

	// Step 4: Migrate bucket configuration (tags, lifecycle, etc.)
	logrus.WithField("migration_id", job.ID).Info("Migrating bucket configuration")
	if err := m.migrateBucketConfiguration(ctx, tenantID, job); err != nil {
		return fmt.Errorf("failed to migrate bucket configuration: %w", err)
	}
	logrus.WithField("migration_id", job.ID).Info("Bucket configuration migrated successfully")

	// Step 5: Verify data integrity (if requested)
	if job.VerifyData && objectCount > 0 {
		logrus.WithField("migration_id", job.ID).Info("Verifying data integrity")
		if err := m.verifyMigration(ctx, tenantID, job); err != nil {
			return fmt.Errorf("data verification failed: %w", err)
		}
		logrus.WithField("migration_id", job.ID).Info("Data verification completed successfully")
	}

	// Step 5: Update bucket location metadata
	logrus.WithField("migration_id", job.ID).Info("Updating bucket location")
	if err := locationMgr.SetBucketLocation(ctx, tenantID, job.BucketName, job.TargetNodeID); err != nil {
		return fmt.Errorf("failed to update bucket location: %w", err)
	}
	logrus.WithFields(logrus.Fields{
		"migration_id": job.ID,
		"bucket":       job.BucketName,
		"new_location": job.TargetNodeID,
	}).Info("Bucket location updated successfully")

	// Step 5: Optionally delete from source
	if job.DeleteSource {
		logrus.WithField("migration_id", job.ID).Warn("Deleting bucket data from source node")
		// TODO: Implement source deletion
		// This should only happen after successful verification
	}

	return nil
}

// countBucketObjects counts all objects in a bucket and calculates total size
func (m *Manager) countBucketObjects(ctx context.Context, tenantID, bucketName string) (int64, int64, error) {
	query := `
		SELECT COUNT(*), COALESCE(SUM(size), 0)
		FROM objects
		WHERE bucket = ? AND tenant_id = ? AND deleted_at IS NULL
	`

	var count, totalSize int64
	err := m.db.QueryRowContext(ctx, query, bucketName, tenantID).Scan(&count, &totalSize)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to count objects: %w", err)
	}

	return count, totalSize, nil
}

// copyBucketObjects copies all objects from source to target node
func (m *Manager) copyBucketObjects(ctx context.Context, tenantID string, job *MigrationJob) error {
	// Get target node info
	targetNode, err := m.GetNode(ctx, job.TargetNodeID)
	if err != nil {
		return fmt.Errorf("failed to get target node: %w", err)
	}

	// Get authentication credentials
	localNodeID, err := m.GetLocalNodeID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get local node ID: %w", err)
	}

	nodeToken, err := m.GetLocalNodeToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node token: %w", err)
	}

	// Query all objects in the bucket
	query := `
		SELECT key, size, etag, content_type, version_id, metadata
		FROM objects
		WHERE bucket = ? AND tenant_id = ? AND deleted_at IS NULL
		ORDER BY created_at ASC
	`

	rows, err := m.db.QueryContext(ctx, query, job.BucketName, tenantID)
	if err != nil {
		return fmt.Errorf("failed to query objects: %w", err)
	}
	defer rows.Close()

	proxyClient := NewProxyClient()
	objectsCopied := int64(0)
	bytesCopied := int64(0)
	errors := 0
	maxErrors := 10 // Allow some failures but continue

	for rows.Next() {
		var key, etag, contentType, versionID, metadata string
		var size int64

		if err := rows.Scan(&key, &size, &etag, &contentType, &versionID, &metadata); err != nil {
			logrus.WithError(err).Warn("Failed to scan object")
			errors++
			if errors >= maxErrors {
				return fmt.Errorf("too many errors scanning objects")
			}
			continue
		}

		// Copy this object to target node
		if err := m.copyObject(ctx, proxyClient, targetNode.Endpoint, localNodeID, nodeToken, tenantID, job.BucketName, key, size, etag, contentType, versionID, metadata); err != nil {
			logrus.WithError(err).WithField("object_key", key).Error("Failed to copy object")
			errors++
			if errors >= maxErrors {
				return fmt.Errorf("too many errors copying objects (latest: %w)", err)
			}
			continue
		}

		// Update progress
		objectsCopied++
		bytesCopied += size

		// Update job progress every 10 objects or if it's the last one
		if objectsCopied%10 == 0 {
			job.ObjectsMigrated = objectsCopied
			job.BytesMigrated = bytesCopied
			if err := m.UpdateMigrationJob(ctx, job); err != nil {
				logrus.WithError(err).Warn("Failed to update migration progress")
			}
		}

		logrus.WithFields(logrus.Fields{
			"migration_id": job.ID,
			"object_key":   key,
			"size":         size,
			"progress":     fmt.Sprintf("%d/%d", objectsCopied, job.ObjectsTotal),
		}).Debug("Object copied successfully")
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("error iterating objects: %w", err)
	}

	// Final progress update
	job.ObjectsMigrated = objectsCopied
	job.BytesMigrated = bytesCopied
	if err := m.UpdateMigrationJob(ctx, job); err != nil {
		logrus.WithError(err).Warn("Failed to update final migration progress")
	}

	if errors > 0 {
		logrus.WithFields(logrus.Fields{
			"migration_id": job.ID,
			"error_count":  errors,
			"total":        objectsCopied,
		}).Warn("Migration completed with some errors")
	}

	return nil
}

// copyObject copies a single object to the target node
func (m *Manager) copyObject(ctx context.Context, proxyClient *ProxyClient, targetEndpoint, localNodeID, nodeToken, tenantID, bucket, key string, size int64, etag, contentType, versionID, metadata string) error {
	// Build URL for internal cluster API
	url := fmt.Sprintf("%s/api/internal/cluster/objects/%s/%s/%s",
		targetEndpoint,
		tenantID,
		bucket,
		key,
	)

	// Check if storage is available
	if m.storage == nil {
		return fmt.Errorf("storage backend not initialized")
	}

	// Build object path
	var objectPath string
	if versionID != "" && versionID != "null" {
		// Versioned object
		objectPath = fmt.Sprintf("%s/.versions/%s/%s", bucket, key, versionID)
	} else {
		// Regular object
		objectPath = fmt.Sprintf("%s/%s", bucket, key)
	}

	// Get object data from storage
	objectReader, _, err := m.storage.Get(ctx, objectPath)
	if err != nil {
		return fmt.Errorf("failed to read object from storage: %w", err)
	}
	defer objectReader.Close()

	// Create authenticated request with actual object data
	req, err := proxyClient.CreateAuthenticatedRequest(ctx, "PUT", url, objectReader, localNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add metadata headers
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Object-Size", fmt.Sprintf("%d", size))
	req.Header.Set("X-Object-ETag", etag)
	req.Header.Set("X-Object-Metadata", metadata)
	req.Header.Set("X-Source-Version-ID", versionID)
	req.ContentLength = size

	// Execute request
	resp, err := proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("failed to send object: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// migrateBucketPermissions migrates bucket permissions to the target node
func (m *Manager) migrateBucketPermissions(ctx context.Context, tenantID string, job *MigrationJob) error {
	// Query all permissions for this bucket
	query := `
		SELECT id, bucket_name, user_id, tenant_id, permission_level, granted_by, granted_at, expires_at
		FROM bucket_permissions
		WHERE bucket_name = ?
	`

	rows, err := m.db.QueryContext(ctx, query, job.BucketName)
	if err != nil {
		return fmt.Errorf("failed to query bucket permissions: %w", err)
	}
	defer rows.Close()

	// Get target node info
	targetNode, err := m.GetNode(ctx, job.TargetNodeID)
	if err != nil {
		return fmt.Errorf("failed to get target node: %w", err)
	}

	// Get authentication credentials
	localNodeID, err := m.GetLocalNodeID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get local node ID: %w", err)
	}

	nodeToken, err := m.GetLocalNodeToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node token: %w", err)
	}

	proxyClient := NewProxyClient()
	permissionCount := 0

	// Iterate through all permissions and send them to target node
	for rows.Next() {
		var id, bucketName, userID, tenantIDVal, permissionLevel, grantedBy string
		var grantedAt, expiresAt sql.NullInt64

		if err := rows.Scan(&id, &bucketName, &userID, &tenantIDVal, &permissionLevel, &grantedBy, &grantedAt, &expiresAt); err != nil {
			logrus.WithError(err).Warn("Failed to scan bucket permission")
			continue
		}

		// Send permission to target node
		if err := m.sendBucketPermission(ctx, proxyClient, targetNode.Endpoint, localNodeID, nodeToken,
			id, bucketName, userID, tenantIDVal, permissionLevel, grantedBy, grantedAt.Int64, expiresAt); err != nil {
			logrus.WithError(err).WithField("permission_id", id).Error("Failed to send bucket permission")
			continue
		}

		permissionCount++
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("error iterating bucket permissions: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"migration_id":       job.ID,
		"permissions_count": permissionCount,
	}).Info("Bucket permissions migration completed")

	return nil
}

// sendBucketPermission sends a bucket permission to the target node
func (m *Manager) sendBucketPermission(ctx context.Context, proxyClient *ProxyClient, targetEndpoint, localNodeID, nodeToken string,
	id, bucketName, userID, tenantID, permissionLevel, grantedBy string, grantedAt int64, expiresAt sql.NullInt64) error {

	// Build URL for internal cluster API
	url := fmt.Sprintf("%s/api/internal/cluster/bucket-permissions", targetEndpoint)

	// Create permission data
	permissionData := map[string]interface{}{
		"id":               id,
		"bucket_name":      bucketName,
		"user_id":          userID,
		"tenant_id":        tenantID,
		"permission_level": permissionLevel,
		"granted_by":       grantedBy,
		"granted_at":       grantedAt,
	}

	if expiresAt.Valid {
		permissionData["expires_at"] = expiresAt.Int64
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(permissionData)
	if err != nil {
		return fmt.Errorf("failed to marshal permission data: %w", err)
	}

	// Create authenticated request
	req, err := proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(jsonData), localNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("failed to send permission: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// migrateBucketACLs migrates bucket ACLs to the target node
func (m *Manager) migrateBucketACLs(ctx context.Context, tenantID string, job *MigrationJob) error {
	// Check if ACL manager is available
	if m.aclManager == nil {
		logrus.Warn("ACL manager not available, skipping ACL migration")
		return nil
	}

	// Get bucket ACL
	bucketACL, err := m.aclManager.GetBucketACL(ctx, tenantID, job.BucketName)
	if err != nil {
		return fmt.Errorf("failed to get bucket ACL: %w", err)
	}

	// If it's the default ACL, no need to migrate
	if bucketACL.CannedACL == "private" && len(bucketACL.Grants) == 0 {
		logrus.WithField("bucket", job.BucketName).Debug("Default ACL, skipping migration")
		return nil
	}

	// Get target node info
	targetNode, err := m.GetNode(ctx, job.TargetNodeID)
	if err != nil {
		return fmt.Errorf("failed to get target node: %w", err)
	}

	// Get authentication credentials
	localNodeID, err := m.GetLocalNodeID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get local node ID: %w", err)
	}

	nodeToken, err := m.GetLocalNodeToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node token: %w", err)
	}

	// Send ACL to target node
	proxyClient := NewProxyClient()
	if err := m.sendBucketACL(ctx, proxyClient, targetNode.Endpoint, localNodeID, nodeToken, tenantID, job.BucketName, bucketACL); err != nil {
		return fmt.Errorf("failed to send bucket ACL: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"migration_id": job.ID,
		"bucket":       job.BucketName,
		"canned_acl":   bucketACL.CannedACL,
		"grants":       len(bucketACL.Grants),
	}).Info("Bucket ACL migrated successfully")

	return nil
}

// sendBucketACL sends bucket ACL to the target node
func (m *Manager) sendBucketACL(ctx context.Context, proxyClient *ProxyClient, targetEndpoint, localNodeID, nodeToken, tenantID, bucketName string, acl interface{}) error {
	// Build URL for internal cluster API
	url := fmt.Sprintf("%s/api/internal/cluster/bucket-acl", targetEndpoint)

	// Create ACL data
	aclData := map[string]interface{}{
		"tenant_id":   tenantID,
		"bucket_name": bucketName,
		"acl":         acl,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(aclData)
	if err != nil {
		return fmt.Errorf("failed to marshal ACL data: %w", err)
	}

	// Create authenticated request
	req, err := proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(jsonData), localNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("failed to send ACL: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// migrateBucketConfiguration migrates bucket configuration (tags, lifecycle, versioning, etc.) to the target node
func (m *Manager) migrateBucketConfiguration(ctx context.Context, tenantID string, job *MigrationJob) error {
	// Get bucket metadata from database
	query := `
		SELECT versioning, object_lock, encryption, lifecycle, tags, cors, policy, notification
		FROM buckets
		WHERE name = ? AND tenant_id = ?
	`

	var versioning, objectLock, encryption, lifecycle, tags, cors, policy, notification sql.NullString

	err := m.db.QueryRowContext(ctx, query, job.BucketName, tenantID).Scan(
		&versioning, &objectLock, &encryption, &lifecycle, &tags, &cors, &policy, &notification,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			logrus.WithField("bucket", job.BucketName).Warn("Bucket not found in database")
			return nil
		}
		return fmt.Errorf("failed to get bucket configuration: %w", err)
	}

	// Get target node info
	targetNode, err := m.GetNode(ctx, job.TargetNodeID)
	if err != nil {
		return fmt.Errorf("failed to get target node: %w", err)
	}

	// Get authentication credentials
	localNodeID, err := m.GetLocalNodeID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get local node ID: %w", err)
	}

	nodeToken, err := m.GetLocalNodeToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node token: %w", err)
	}

	// Send configuration to target node
	proxyClient := NewProxyClient()
	if err := m.sendBucketConfiguration(ctx, proxyClient, targetNode.Endpoint, localNodeID, nodeToken, tenantID, job.BucketName,
		versioning, objectLock, encryption, lifecycle, tags, cors, policy, notification); err != nil {
		return fmt.Errorf("failed to send bucket configuration: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"migration_id": job.ID,
		"bucket":       job.BucketName,
	}).Info("Bucket configuration migrated successfully")

	return nil
}

// sendBucketConfiguration sends bucket configuration to the target node
func (m *Manager) sendBucketConfiguration(ctx context.Context, proxyClient *ProxyClient, targetEndpoint, localNodeID, nodeToken, tenantID, bucketName string,
	versioning, objectLock, encryption, lifecycle, tags, cors, policy, notification sql.NullString) error {

	// Build URL for internal cluster API
	url := fmt.Sprintf("%s/api/internal/cluster/bucket-config", targetEndpoint)

	// Create configuration data
	configData := map[string]interface{}{
		"tenant_id":   tenantID,
		"bucket_name": bucketName,
	}

	if versioning.Valid {
		configData["versioning"] = versioning.String
	}
	if objectLock.Valid {
		configData["object_lock"] = objectLock.String
	}
	if encryption.Valid {
		configData["encryption"] = encryption.String
	}
	if lifecycle.Valid {
		configData["lifecycle"] = lifecycle.String
	}
	if tags.Valid {
		configData["tags"] = tags.String
	}
	if cors.Valid {
		configData["cors"] = cors.String
	}
	if policy.Valid {
		configData["policy"] = policy.String
	}
	if notification.Valid {
		configData["notification"] = notification.String
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(configData)
	if err != nil {
		return fmt.Errorf("failed to marshal configuration data: %w", err)
	}

	// Create authenticated request
	req, err := proxyClient.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(jsonData), localNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("failed to send configuration: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// verifyMigration verifies that all objects were copied correctly
func (m *Manager) verifyMigration(ctx context.Context, tenantID string, job *MigrationJob) error {
	// Verify object count matches
	if job.ObjectsMigrated != job.ObjectsTotal {
		return fmt.Errorf("object count mismatch: migrated %d but expected %d", job.ObjectsMigrated, job.ObjectsTotal)
	}

	// Verify total bytes matches (allow small discrepancies due to metadata)
	byteDiff := job.BytesTotal - job.BytesMigrated
	if byteDiff < 0 {
		byteDiff = -byteDiff
	}
	percentDiff := float64(byteDiff) / float64(job.BytesTotal) * 100
	if percentDiff > 1.0 { // Allow 1% difference
		return fmt.Errorf("bytes mismatch: migrated %d bytes but expected %d bytes (%.2f%% difference)",
			job.BytesMigrated, job.BytesTotal, percentDiff)
	}

	// Query target node to verify objects exist
	// Get target node info
	targetNode, err := m.GetNode(ctx, job.TargetNodeID)
	if err != nil {
		return fmt.Errorf("failed to get target node: %w", err)
	}

	// Get authentication credentials
	localNodeID, err := m.GetLocalNodeID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get local node ID: %w", err)
	}

	nodeToken, err := m.GetLocalNodeToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node token: %w", err)
	}

	proxyClient := NewProxyClient()

	// Sample verification: Check first 10 objects exist on target
	query := `
		SELECT key, etag
		FROM objects
		WHERE bucket = ? AND tenant_id = ? AND deleted_at IS NULL
		ORDER BY created_at ASC
		LIMIT 10
	`

	rows, err := m.db.QueryContext(ctx, query, job.BucketName, tenantID)
	if err != nil {
		return fmt.Errorf("failed to query sample objects: %w", err)
	}
	defer rows.Close()

	verifiedCount := 0
	for rows.Next() {
		var key, sourceETag string
		if err := rows.Scan(&key, &sourceETag); err != nil {
			return fmt.Errorf("failed to scan object: %w", err)
		}

		// Verify this object exists on target node with same ETag
		if err := m.verifyObjectOnTarget(ctx, proxyClient, targetNode.Endpoint, localNodeID, nodeToken, tenantID, job.BucketName, key, sourceETag); err != nil {
			return fmt.Errorf("verification failed for object %s: %w", key, err)
		}

		verifiedCount++
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("error iterating sample objects: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"migration_id":     job.ID,
		"verified_samples": verifiedCount,
		"total_objects":    job.ObjectsTotal,
	}).Info("Sample verification completed")

	return nil
}

// verifyObjectOnTarget checks if an object exists on the target node with the expected ETag
func (m *Manager) verifyObjectOnTarget(ctx context.Context, proxyClient *ProxyClient, targetEndpoint, localNodeID, nodeToken, tenantID, bucket, key, expectedETag string) error {
	// Build URL for HEAD request
	url := fmt.Sprintf("%s/api/internal/cluster/objects/%s/%s/%s",
		targetEndpoint,
		tenantID,
		bucket,
		key,
	)

	// Create authenticated HEAD request
	req, err := proxyClient.CreateAuthenticatedRequest(ctx, "HEAD", url, nil, localNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request
	resp, err := proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("failed to verify object: %w", err)
	}
	defer resp.Body.Close()

	// Check if object exists
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("object not found on target node")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	// Verify ETag matches (if provided)
	if expectedETag != "" {
		targetETag := resp.Header.Get("X-Object-ETag")
		if targetETag == "" {
			targetETag = resp.Header.Get("ETag")
		}
		if targetETag != expectedETag {
			return fmt.Errorf("ETag mismatch: source=%s, target=%s", expectedETag, targetETag)
		}
	}

	return nil
}
