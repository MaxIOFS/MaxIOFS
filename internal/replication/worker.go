package replication

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// S3ClientFactory creates an S3 client for a given endpoint
type S3ClientFactory func(endpoint, region, accessKey, secretKey string) S3Client

// Worker processes replication queue items
type Worker struct {
	id              int
	queue           <-chan *QueueItem
	db              *sql.DB
	objectAdapter   ObjectAdapter
	objectManager   ObjectManager
	s3ClientFactory S3ClientFactory
}

// NewWorker creates a new replication worker
func NewWorker(id int, queue <-chan *QueueItem, db *sql.DB, objectAdapter ObjectAdapter, objectManager ObjectManager) *Worker {
	return &Worker{
		id:            id,
		queue:         queue,
		db:            db,
		objectAdapter: objectAdapter,
		objectManager: objectManager,
		s3ClientFactory: func(endpoint, region, accessKey, secretKey string) S3Client {
			return NewS3RemoteClient(endpoint, region, accessKey, secretKey)
		},
	}
}

// NewWorkerWithS3Factory creates a new replication worker with custom S3 client factory (for testing)
func NewWorkerWithS3Factory(id int, queue <-chan *QueueItem, db *sql.DB, objectAdapter ObjectAdapter, objectManager ObjectManager, factory S3ClientFactory) *Worker {
	return &Worker{
		id:              id,
		queue:           queue,
		db:              db,
		objectAdapter:   objectAdapter,
		objectManager:   objectManager,
		s3ClientFactory: factory,
	}
}

// Start starts the worker
func (w *Worker) Start(ctx context.Context, stopChan <-chan struct{}) {
	logrus.WithFields(logrus.Fields{
		"worker_id": w.id,
	}).Info("Replication worker started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-stopChan:
			return
		case item, ok := <-w.queue:
			if !ok {
				return
			}
			w.processItem(ctx, item)
		}
	}
}

// processItem processes a single queue item
func (w *Worker) processItem(ctx context.Context, item *QueueItem) {
	logrus.WithFields(logrus.Fields{
		"worker_id": w.id,
		"item_id":   item.ID,
		"rule_id":   item.RuleID,
		"bucket":    item.Bucket,
		"key":       item.ObjectKey,
		"action":    item.Action,
	}).Debug("Processing replication item")

	// Update status to in_progress
	if err := w.updateItemStatus(ctx, item.ID, StatusInProgress, ""); err != nil {
		logrus.WithFields(logrus.Fields{
			"item_id": item.ID,
		}).WithError(err).Error("Failed to update item status")
		return
	}

	// Get rule
	rule, err := w.getRule(ctx, item.RuleID)
	if err != nil {
		w.handleError(ctx, item, fmt.Errorf("failed to get rule: %w", err))
		return
	}

	if rule == nil || !rule.Enabled {
		w.handleError(ctx, item, fmt.Errorf("rule not found or disabled"))
		return
	}

	// Process based on action
	var bytesReplicated int64
	switch item.Action {
	case "PUT", "COPY":
		bytesReplicated, err = w.replicateObject(ctx, rule, item)
	case "DELETE":
		if rule.ReplicateDeletes {
			err = w.replicateDelete(ctx, rule, item)
		} else {
			// Skip delete replication if not enabled
			w.completeItem(ctx, item, 0)
			return
		}
	default:
		err = fmt.Errorf("unknown action: %s", item.Action)
	}

	if err != nil {
		w.handleError(ctx, item, err)
		return
	}

	// Mark as completed
	w.completeItem(ctx, item, bytesReplicated)

	// Update replication status
	w.updateReplicationStatus(ctx, rule, item, StatusCompleted, "")
}

// replicateObject replicates an object
func (w *Worker) replicateObject(ctx context.Context, rule *ReplicationRule, item *QueueItem) (int64, error) {
	destKey := item.ObjectKey
	if rule.Prefix != "" && len(item.ObjectKey) >= len(rule.Prefix) {
		// Optionally strip prefix for destination
		destKey = item.ObjectKey
	}

	logrus.WithFields(logrus.Fields{
		"source_bucket": rule.SourceBucket,
		"source_key":    item.ObjectKey,
		"dest_endpoint": rule.DestinationEndpoint,
		"dest_bucket":   rule.DestinationBucket,
		"dest_key":      destKey,
	}).Info("Replicating object")

	// Create S3 client for remote destination
	region := rule.DestinationRegion
	if region == "" {
		region = "us-east-1" // Default region
	}

	s3Client := w.s3ClientFactory(
		rule.DestinationEndpoint,
		region,
		rule.DestinationAccessKey,
		rule.DestinationSecretKey,
	)

	// Get object from local storage
	reader, size, contentType, metadata, err := w.objectManager.GetObject(
		ctx,
		item.TenantID,
		rule.SourceBucket,
		item.ObjectKey,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to get source object from local storage: %w", err)
	}
	defer reader.Close()

	logrus.WithFields(logrus.Fields{
		"source_key":   item.ObjectKey,
		"size":         size,
		"content_type": contentType,
	}).Debug("Retrieved object from local storage")

	// Upload object to remote S3
	err = s3Client.PutObject(
		ctx,
		rule.DestinationBucket,
		destKey,
		reader,
		size,
		contentType,
		metadata,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to upload object to remote S3: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"bytes":         size,
		"dest_endpoint": rule.DestinationEndpoint,
		"dest_bucket":   rule.DestinationBucket,
		"dest_key":      destKey,
	}).Info("Object replicated successfully to remote S3")

	return size, nil
}

// replicateDelete replicates a delete operation
func (w *Worker) replicateDelete(ctx context.Context, rule *ReplicationRule, item *QueueItem) error {
	destKey := item.ObjectKey

	logrus.WithFields(logrus.Fields{
		"dest_endpoint": rule.DestinationEndpoint,
		"dest_bucket":   rule.DestinationBucket,
		"dest_key":      destKey,
	}).Info("Replicating delete")

	// Create S3 client for remote destination
	region := rule.DestinationRegion
	if region == "" {
		region = "us-east-1" // Default region
	}

	s3Client := w.s3ClientFactory(
		rule.DestinationEndpoint,
		region,
		rule.DestinationAccessKey,
		rule.DestinationSecretKey,
	)

	// Delete object from remote S3
	err := s3Client.DeleteObject(ctx, rule.DestinationBucket, destKey)
	if err != nil {
		return fmt.Errorf("failed to delete object from remote S3: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"dest_endpoint": rule.DestinationEndpoint,
		"dest_bucket":   rule.DestinationBucket,
		"dest_key":      destKey,
	}).Info("Delete replicated successfully to remote S3")

	return nil
}

// getRule retrieves a replication rule
func (w *Worker) getRule(ctx context.Context, ruleID string) (*ReplicationRule, error) {
	query := `
		SELECT id, tenant_id, source_bucket, destination_endpoint, destination_bucket,
			   destination_access_key, destination_secret_key, destination_region, prefix, enabled,
			   priority, mode, schedule_interval, conflict_resolution, replicate_deletes,
			   replicate_metadata, created_at, updated_at
		FROM replication_rules WHERE id = ?
	`

	rule := &ReplicationRule{}
	err := w.db.QueryRowContext(ctx, query, ruleID).Scan(
		&rule.ID, &rule.TenantID, &rule.SourceBucket, &rule.DestinationEndpoint, &rule.DestinationBucket,
		&rule.DestinationAccessKey, &rule.DestinationSecretKey, &rule.DestinationRegion, &rule.Prefix, &rule.Enabled,
		&rule.Priority, &rule.Mode, &rule.ScheduleInterval, &rule.ConflictResolution, &rule.ReplicateDeletes,
		&rule.ReplicateMetadata, &rule.CreatedAt, &rule.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return rule, err
}

// updateItemStatus updates the status of a queue item
func (w *Worker) updateItemStatus(ctx context.Context, itemID int64, status ReplicationStatus, errorMsg string) error {
	query := `
		UPDATE replication_queue
		SET status = ?, last_error = ?, attempts = attempts + 1, processed_at = ?
		WHERE id = ?
	`
	_, err := w.db.ExecContext(ctx, query, status, errorMsg, time.Now(), itemID)
	return err
}

// completeItem marks an item as completed
func (w *Worker) completeItem(ctx context.Context, item *QueueItem, bytesReplicated int64) {
	query := `
		UPDATE replication_queue
		SET status = ?, completed_at = ?, bytes_replicated = ?
		WHERE id = ?
	`
	_, err := w.db.ExecContext(ctx, query, StatusCompleted, time.Now(), bytesReplicated, item.ID)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"item_id": item.ID,
		}).WithError(err).Error("Failed to complete item")
	}
}

// handleError handles replication errors
func (w *Worker) handleError(ctx context.Context, item *QueueItem, err error) {
	logrus.WithFields(logrus.Fields{
		"item_id":  item.ID,
		"attempts": item.Attempts + 1,
	}).WithError(err).Error("Replication failed")

	item.Attempts++
	item.LastError = err.Error()

	var status ReplicationStatus
	if item.Attempts >= item.MaxRetries {
		status = StatusFailed
		logrus.WithFields(logrus.Fields{
			"item_id":     item.ID,
			"max_retries": item.MaxRetries,
		}).Warn("Max retries reached")
	} else {
		status = StatusPending // Will be retried
	}

	query := `
		UPDATE replication_queue
		SET status = ?, attempts = ?, last_error = ?, processed_at = ?
		WHERE id = ?
	`
	_, updateErr := w.db.ExecContext(ctx, query, status, item.Attempts, item.LastError, time.Now(), item.ID)
	if updateErr != nil {
		logrus.WithFields(logrus.Fields{
			"item_id": item.ID,
		}).WithError(updateErr).Error("Failed to update error status")
	}

	// Update replication status
	if item.Attempts >= item.MaxRetries {
		// Get rule for status update
		rule, err := w.getRule(ctx, item.RuleID)
		if err == nil && rule != nil {
			w.updateReplicationStatus(ctx, rule, item, StatusFailed, item.LastError)
		}
	}
}

// updateReplicationStatus updates the replication status record
func (w *Worker) updateReplicationStatus(ctx context.Context, rule *ReplicationRule, item *QueueItem, status ReplicationStatus, errorMsg string) {
	var replicatedAt *time.Time
	if status == StatusCompleted {
		now := time.Now()
		replicatedAt = &now
	}

	// Check if status record exists
	checkQuery := `
		SELECT id FROM replication_status
		WHERE rule_id = ? AND source_bucket = ? AND source_key = ? AND source_version_id = ?
	`
	var existingID int64
	err := w.db.QueryRowContext(ctx, checkQuery, rule.ID, rule.SourceBucket, item.ObjectKey, item.VersionID).Scan(&existingID)

	if err == sql.ErrNoRows {
		// Insert new status
		insertQuery := `
			INSERT INTO replication_status (
				rule_id, tenant_id, source_bucket, source_key, source_version_id,
				destination_bucket, destination_key, status, last_attempt, replicated_at, error_message
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`
		_, err = w.db.ExecContext(ctx, insertQuery,
			rule.ID, item.TenantID, rule.SourceBucket, item.ObjectKey, item.VersionID,
			rule.DestinationBucket, item.ObjectKey, status, time.Now(), replicatedAt, errorMsg,
		)
	} else if err == nil {
		// Update existing status
		updateQuery := `
			UPDATE replication_status
			SET status = ?, last_attempt = ?, replicated_at = ?, error_message = ?
			WHERE id = ?
		`
		_, err = w.db.ExecContext(ctx, updateQuery, status, time.Now(), replicatedAt, errorMsg, existingID)
	}

	if err != nil {
		logrus.WithFields(logrus.Fields{
			"rule_id": rule.ID,
			"key":     item.ObjectKey,
		}).WithError(err).Error("Failed to update replication status")
	}
}
