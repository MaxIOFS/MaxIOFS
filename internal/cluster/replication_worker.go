package cluster

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// ClusterReplicationWorker processes cluster replication queue items
type ClusterReplicationWorker struct {
	id             int
	db             *sql.DB
	clusterManager *Manager
	proxyClient    *ProxyClient
	queueChan      chan *ClusterReplicationQueueItem
	log            *logrus.Entry
}

// NewClusterReplicationWorker creates a new replication worker
func NewClusterReplicationWorker(id int, db *sql.DB, clusterManager *Manager, proxyClient *ProxyClient, queueChan chan *ClusterReplicationQueueItem) *ClusterReplicationWorker {
	return &ClusterReplicationWorker{
		id:             id,
		db:             db,
		clusterManager: clusterManager,
		proxyClient:    proxyClient,
		queueChan:      queueChan,
		log:            logrus.WithField("component", fmt.Sprintf("replication-worker-%d", id)),
	}
}

// Start starts the worker
func (w *ClusterReplicationWorker) Start(ctx context.Context) {
	w.log.Info("Replication worker started")

	for {
		select {
		case <-ctx.Done():
			w.log.Info("Replication worker stopped")
			return
		case item, ok := <-w.queueChan:
			if !ok {
				w.log.Info("Queue channel closed, worker stopping")
				return
			}
			w.processItem(ctx, item)
		}
	}
}

// processItem processes a single replication queue item
func (w *ClusterReplicationWorker) processItem(ctx context.Context, item *ClusterReplicationQueueItem) {
	w.log.WithFields(logrus.Fields{
		"item_id":    item.ID,
		"bucket":     item.SourceBucket,
		"object_key": item.ObjectKey,
		"operation":  item.Operation,
		"dest_node":  item.DestinationNodeID,
	}).Debug("Processing replication item")

	// Mark as processing
	if err := w.updateItemStatus(ctx, item.ID, "processing", ""); err != nil {
		w.log.WithError(err).Error("Failed to update item status to processing")
		return
	}

	// Process based on operation type
	var err error
	switch item.Operation {
	case "PUT":
		err = w.replicateObject(ctx, item)
	case "DELETE":
		err = w.replicateDelete(ctx, item)
	default:
		err = fmt.Errorf("unknown operation: %s", item.Operation)
	}

	// Update item status based on result
	if err != nil {
		w.log.WithError(err).WithField("item_id", item.ID).Error("Replication failed")

		// Increment attempts
		newAttempts := item.Attempts + 1
		if newAttempts >= item.MaxAttempts {
			// Max attempts reached, mark as failed
			w.updateItemStatus(ctx, item.ID, "failed", err.Error())
		} else {
			// Retry later
			w.updateItemRetry(ctx, item.ID, newAttempts, err.Error())
		}
	} else {
		// Mark as completed
		w.updateItemCompleted(ctx, item.ID)

		// Update replication statistics
		w.updateReplicationStats(ctx, item.ReplicationRuleID)

		w.log.WithFields(logrus.Fields{
			"item_id":    item.ID,
			"object_key": item.ObjectKey,
		}).Info("Object replicated successfully")
	}
}

// replicateObject replicates an object to the destination node
func (w *ClusterReplicationWorker) replicateObject(ctx context.Context, item *ClusterReplicationQueueItem) error {
	// Get object metadata and data from local storage
	// NOTE: In a real implementation, this would call ObjectManager.GetObject()
	// which automatically decrypts the object. For now, we'll query the database.

	var size int64
	var etag, contentType, versionID string
	var metadata string

	err := w.db.QueryRowContext(ctx, `
		SELECT size, etag, content_type, version_id, metadata
		FROM objects
		WHERE bucket = ? AND key = ? AND tenant_id = ? AND deleted_at IS NULL
		ORDER BY created_at DESC LIMIT 1
	`, item.SourceBucket, item.ObjectKey, item.TenantID).Scan(&size, &etag, &contentType, &versionID, &metadata)

	if err == sql.ErrNoRows {
		return fmt.Errorf("object not found: %s/%s", item.SourceBucket, item.ObjectKey)
	}
	if err != nil {
		return fmt.Errorf("failed to get object metadata: %w", err)
	}

	// TODO: Get actual object data via ObjectManager.GetObject()
	// For now, we'll send metadata only as placeholder
	// In real implementation:
	// reader, size, contentType, metadata, err := objectManager.GetObject(ctx, item.TenantID, item.SourceBucket, item.ObjectKey)
	// The GetObject call will automatically decrypt the object

	// Get destination node info
	node, err := w.clusterManager.GetNode(ctx, item.DestinationNodeID)
	if err != nil {
		return fmt.Errorf("failed to get destination node: %w", err)
	}

	// Get authentication credentials
	localNodeID, err := w.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get local node ID: %w", err)
	}

	nodeToken, err := w.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node token: %w", err)
	}

	// Build URL for internal cluster API
	url := fmt.Sprintf("%s/api/internal/cluster/objects/%s/%s/%s",
		node.Endpoint,
		item.TenantID,
		item.DestinationBucket,
		item.ObjectKey,
	)

	// Create placeholder body (in real implementation, this would be the actual object data)
	body := bytes.NewReader([]byte{})

	// Create authenticated request
	req, err := w.proxyClient.CreateAuthenticatedRequest(ctx, "PUT", url, body, localNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Add metadata headers
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Object-Size", fmt.Sprintf("%d", size))
	req.Header.Set("X-Object-ETag", etag)
	req.Header.Set("X-Object-Metadata", metadata)
	req.Header.Set("X-Source-Version-ID", versionID)

	// Execute request
	resp, err := w.proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("failed to send object: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Update replication status
	if err := w.updateReplicationStatus(ctx, item, etag, versionID, size); err != nil {
		w.log.WithError(err).Warn("Failed to update replication status")
	}

	return nil
}

// replicateDelete replicates a delete operation to the destination node
func (w *ClusterReplicationWorker) replicateDelete(ctx context.Context, item *ClusterReplicationQueueItem) error {
	// Get destination node info
	node, err := w.clusterManager.GetNode(ctx, item.DestinationNodeID)
	if err != nil {
		return fmt.Errorf("failed to get destination node: %w", err)
	}

	// Get authentication credentials
	localNodeID, err := w.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		return fmt.Errorf("failed to get local node ID: %w", err)
	}

	nodeToken, err := w.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get node token: %w", err)
	}

	// Build URL for internal cluster API
	url := fmt.Sprintf("%s/api/internal/cluster/objects/%s/%s/%s",
		node.Endpoint,
		item.TenantID,
		item.DestinationBucket,
		item.ObjectKey,
	)

	// Create authenticated DELETE request
	req, err := w.proxyClient.CreateAuthenticatedRequest(ctx, "DELETE", url, nil, localNodeID, nodeToken)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request
	resp, err := w.proxyClient.DoAuthenticatedRequest(req)
	if err != nil {
		return fmt.Errorf("failed to send delete: %w", err)
	}
	defer resp.Body.Close()

	// Check response (404 is OK - object already deleted)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// updateItemStatus updates the status of a queue item
func (w *ClusterReplicationWorker) updateItemStatus(ctx context.Context, itemID, status, lastError string) error {
	_, err := w.db.ExecContext(ctx, `
		UPDATE cluster_replication_queue
		SET status = ?, last_error = ?, updated_at = ?
		WHERE id = ?
	`, status, lastError, time.Now(), itemID)
	return err
}

// updateItemRetry updates a queue item for retry
func (w *ClusterReplicationWorker) updateItemRetry(ctx context.Context, itemID string, attempts int, lastError string) error {
	_, err := w.db.ExecContext(ctx, `
		UPDATE cluster_replication_queue
		SET status = 'pending', attempts = ?, last_error = ?, updated_at = ?
		WHERE id = ?
	`, attempts, lastError, time.Now(), itemID)
	return err
}

// updateItemCompleted marks a queue item as completed
func (w *ClusterReplicationWorker) updateItemCompleted(ctx context.Context, itemID string) error {
	now := time.Now()
	_, err := w.db.ExecContext(ctx, `
		UPDATE cluster_replication_queue
		SET status = 'completed', completed_at = ?, updated_at = ?
		WHERE id = ?
	`, now, now, itemID)
	return err
}

// updateReplicationStatus updates or creates a replication status record
func (w *ClusterReplicationWorker) updateReplicationStatus(ctx context.Context, item *ClusterReplicationQueueItem, sourceETag, sourceVersionID string, sourceSize int64) error {
	now := time.Now()
	id := fmt.Sprintf("%s-%s-%d", item.ReplicationRuleID, item.ObjectKey, now.UnixNano())

	_, err := w.db.ExecContext(ctx, `
		INSERT INTO cluster_replication_status (
			id, replication_rule_id, tenant_id, source_bucket, object_key,
			destination_node_id, destination_bucket, source_version_id, source_etag,
			source_size, status, last_sync_at, replicated_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'replicated', ?, ?, ?, ?)
		ON CONFLICT(replication_rule_id, object_key) DO UPDATE SET
			source_version_id = ?,
			source_etag = ?,
			source_size = ?,
			status = 'replicated',
			last_sync_at = ?,
			replicated_at = ?,
			updated_at = ?
	`,
		id,
		item.ReplicationRuleID,
		item.TenantID,
		item.SourceBucket,
		item.ObjectKey,
		item.DestinationNodeID,
		item.DestinationBucket,
		sourceVersionID,
		sourceETag,
		sourceSize,
		now,
		now,
		now,
		now,
		sourceVersionID,
		sourceETag,
		sourceSize,
		now,
		now,
		now,
	)

	return err
}

// updateReplicationStats updates replication statistics for a rule
func (w *ClusterReplicationWorker) updateReplicationStats(ctx context.Context, ruleID string) error {
	// Increment objects_replicated counter
	_, err := w.db.ExecContext(ctx, `
		UPDATE cluster_bucket_replication
		SET objects_replicated = objects_replicated + 1, updated_at = ?
		WHERE id = ?
	`, time.Now(), ruleID)
	return err
}
