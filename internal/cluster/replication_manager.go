package cluster

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// ClusterReplicationManager manages cluster bucket replication
type ClusterReplicationManager struct {
	db                *sql.DB
	clusterManager    *Manager
	tenantSyncManager *TenantSyncManager
	proxyClient       *ProxyClient
	workers           []*ClusterReplicationWorker
	workerCount       int
	queueChan         chan *ClusterReplicationQueueItem
	stopChan          chan struct{}
	wg                sync.WaitGroup
	log               *logrus.Entry
}

// NewClusterReplicationManager creates a new cluster replication manager
func NewClusterReplicationManager(db *sql.DB, clusterManager *Manager, tenantSyncManager *TenantSyncManager) *ClusterReplicationManager {
	// Get worker count from config
	workerCountStr, err := GetGlobalConfig(context.Background(), db, "replication_worker_count")
	if err != nil {
		logrus.WithError(err).Warn("Failed to get replication worker count, using default 5")
		workerCountStr = "5"
	}

	workerCount, err := strconv.Atoi(workerCountStr)
	if err != nil {
		logrus.WithError(err).Warn("Invalid replication worker count, using default 5")
		workerCount = 5
	}

	return &ClusterReplicationManager{
		db:                db,
		clusterManager:    clusterManager,
		tenantSyncManager: tenantSyncManager,
		proxyClient:       NewProxyClient(),
		workerCount:       workerCount,
		queueChan:         make(chan *ClusterReplicationQueueItem, 1000),
		stopChan:          make(chan struct{}),
		log:               logrus.WithField("component", "cluster-replication"),
	}
}

// Start starts the cluster replication manager
func (m *ClusterReplicationManager) Start(ctx context.Context) {
	if !m.clusterManager.IsClusterEnabled() {
		m.log.Info("Cluster replication disabled (cluster not enabled)")
		return
	}

	m.log.WithField("worker_count", m.workerCount).Info("Starting cluster replication manager")

	// Start workers
	for i := 0; i < m.workerCount; i++ {
		worker := NewClusterReplicationWorker(i, m.db, m.clusterManager, m.proxyClient, m.queueChan)
		m.workers = append(m.workers, worker)
		m.wg.Add(1)
		go func(w *ClusterReplicationWorker) {
			defer m.wg.Done()
			w.Start(ctx)
		}(worker)
	}

	// Start scheduler (checks sync intervals and queues objects)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.schedulerLoop(ctx)
	}()

	// Start queue loader (loads pending items from database)
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.queueLoaderLoop(ctx)
	}()

	m.log.Info("Cluster replication manager started")
}

// schedulerLoop checks replication rules and queues objects for replication
func (m *ClusterReplicationManager) schedulerLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	m.log.Debug("Scheduler loop started")

	for {
		select {
		case <-ctx.Done():
			m.log.Info("Scheduler loop stopped")
			return
		case <-m.stopChan:
			m.log.Info("Scheduler loop stopped")
			return
		case <-ticker.C:
			m.checkReplicationRules(ctx)
		}
	}
}

// checkReplicationRules checks all enabled replication rules and queues objects if needed
func (m *ClusterReplicationManager) checkReplicationRules(ctx context.Context) {
	rules, err := m.getEnabledRules(ctx)
	if err != nil {
		m.log.WithError(err).Error("Failed to get enabled replication rules")
		return
	}

	for _, rule := range rules {
		// Check if sync interval has elapsed
		if rule.LastSyncAt != nil {
			elapsed := time.Since(*rule.LastSyncAt)
			if elapsed < time.Duration(rule.SyncIntervalSeconds)*time.Second {
				continue // Not time to sync yet
			}
		}

		m.log.WithFields(logrus.Fields{
			"rule_id":       rule.ID,
			"source_bucket": rule.SourceBucket,
			"dest_node":     rule.DestinationNodeID,
		}).Debug("Queueing objects for replication")

		// Queue all objects in the bucket for replication
		if err := m.queueBucketObjects(ctx, rule); err != nil {
			m.log.WithError(err).WithField("rule_id", rule.ID).Error("Failed to queue bucket objects")
			continue
		}

		// Update last_sync_at
		if err := m.updateRuleLastSync(ctx, rule.ID); err != nil {
			m.log.WithError(err).WithField("rule_id", rule.ID).Warn("Failed to update rule last_sync_at")
		}
	}
}

// queueLoaderLoop loads pending queue items from database
func (m *ClusterReplicationManager) queueLoaderLoop(ctx context.Context) {
	intervalStr, err := GetGlobalConfig(ctx, m.db, "queue_check_interval_seconds")
	if err != nil {
		m.log.WithError(err).Warn("Failed to get queue check interval, using default 10s")
		intervalStr = "10"
	}

	interval, err := strconv.Atoi(intervalStr)
	if err != nil {
		m.log.WithError(err).Warn("Invalid queue check interval, using default 10s")
		interval = 10
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	m.log.WithField("interval_seconds", interval).Debug("Queue loader loop started")

	for {
		select {
		case <-ctx.Done():
			m.log.Info("Queue loader loop stopped")
			return
		case <-m.stopChan:
			m.log.Info("Queue loader loop stopped")
			return
		case <-ticker.C:
			m.loadPendingQueueItems(ctx)
		}
	}
}

// loadPendingQueueItems loads pending queue items and sends them to workers
func (m *ClusterReplicationManager) loadPendingQueueItems(ctx context.Context) {
	items, err := m.getPendingQueueItems(ctx, 100) // Load up to 100 items
	if err != nil {
		m.log.WithError(err).Error("Failed to load pending queue items")
		return
	}

	if len(items) == 0 {
		return
	}

	m.log.WithField("item_count", len(items)).Debug("Loading pending queue items")

	for _, item := range items {
		select {
		case m.queueChan <- item:
			// Successfully queued
		case <-ctx.Done():
			return
		default:
			// Queue is full, stop loading
			m.log.Warn("Replication queue is full")
			return
		}
	}
}

// getEnabledRules retrieves all enabled replication rules
func (m *ClusterReplicationManager) getEnabledRules(ctx context.Context) ([]*ClusterReplicationRule, error) {
	query := `
		SELECT id, tenant_id, source_bucket, destination_node_id, destination_bucket,
		       sync_interval_seconds, enabled, replicate_deletes, replicate_metadata,
		       prefix, priority, last_sync_at, last_error, objects_replicated,
		       bytes_replicated, created_at, updated_at
		FROM cluster_bucket_replication
		WHERE enabled = 1
		ORDER BY priority DESC, created_at ASC
	`

	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*ClusterReplicationRule
	for rows.Next() {
		rule := &ClusterReplicationRule{}
		var enabled, replicateDeletes, replicateMetadata int
		var lastSyncAt sql.NullTime

		err := rows.Scan(
			&rule.ID,
			&rule.TenantID,
			&rule.SourceBucket,
			&rule.DestinationNodeID,
			&rule.DestinationBucket,
			&rule.SyncIntervalSeconds,
			&enabled,
			&replicateDeletes,
			&replicateMetadata,
			&rule.Prefix,
			&rule.Priority,
			&lastSyncAt,
			&rule.LastError,
			&rule.ObjectsReplicated,
			&rule.BytesReplicated,
			&rule.CreatedAt,
			&rule.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		rule.Enabled = enabled == 1
		rule.ReplicateDeletes = replicateDeletes == 1
		rule.ReplicateMetadata = replicateMetadata == 1
		if lastSyncAt.Valid {
			rule.LastSyncAt = &lastSyncAt.Time
		}

		rules = append(rules, rule)
	}

	return rules, rows.Err()
}

// queueBucketObjects queues all objects in a bucket for replication
func (m *ClusterReplicationManager) queueBucketObjects(ctx context.Context, rule *ClusterReplicationRule) error {
	// Query all objects in the bucket
	query := `
		SELECT key, size, etag, version_id, created_at
		FROM objects
		WHERE bucket = ? AND tenant_id = ? AND deleted_at IS NULL
	`
	if rule.Prefix != "" {
		query += ` AND key LIKE ?`
	}

	var rows *sql.Rows
	var err error

	if rule.Prefix != "" {
		rows, err = m.db.QueryContext(ctx, query, rule.SourceBucket, rule.TenantID, rule.Prefix+"%")
	} else {
		rows, err = m.db.QueryContext(ctx, query, rule.SourceBucket, rule.TenantID)
	}

	if err != nil {
		return fmt.Errorf("failed to query objects: %w", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var key, etag, versionID string
		var size int64
		var createdAt time.Time

		if err := rows.Scan(&key, &size, &etag, &versionID, &createdAt); err != nil {
			m.log.WithError(err).Warn("Failed to scan object")
			continue
		}

		// Create queue item
		item := &ClusterReplicationQueueItem{
			ID:                uuid.New().String(),
			ReplicationRuleID: rule.ID,
			TenantID:          rule.TenantID,
			SourceBucket:      rule.SourceBucket,
			ObjectKey:         key,
			DestinationNodeID: rule.DestinationNodeID,
			DestinationBucket: rule.DestinationBucket,
			Operation:         "PUT",
			Status:            "pending",
			Attempts:          0,
			MaxAttempts:       3,
			Priority:          rule.Priority,
			CreatedAt:         time.Now(),
			UpdatedAt:         time.Now(),
		}

		// Insert into queue
		if err := m.insertQueueItem(ctx, item); err != nil {
			m.log.WithError(err).WithField("object_key", key).Warn("Failed to insert queue item")
			continue
		}

		count++
	}

	m.log.WithFields(logrus.Fields{
		"rule_id":      rule.ID,
		"object_count": count,
	}).Debug("Queued objects for replication")

	return rows.Err()
}

// insertQueueItem inserts a queue item into the database
func (m *ClusterReplicationManager) insertQueueItem(ctx context.Context, item *ClusterReplicationQueueItem) error {
	// Check if item already exists and is not completed
	var existingStatus string
	err := m.db.QueryRowContext(ctx, `
		SELECT status FROM cluster_replication_queue
		WHERE replication_rule_id = ? AND object_key = ? AND status IN ('pending', 'processing')
	`, item.ReplicationRuleID, item.ObjectKey).Scan(&existingStatus)

	if err == nil {
		// Item already queued, skip
		return nil
	}

	_, err = m.db.ExecContext(ctx, `
		INSERT INTO cluster_replication_queue (
			id, replication_rule_id, tenant_id, source_bucket, object_key,
			destination_node_id, destination_bucket, operation, status,
			attempts, max_attempts, priority, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		item.ID,
		item.ReplicationRuleID,
		item.TenantID,
		item.SourceBucket,
		item.ObjectKey,
		item.DestinationNodeID,
		item.DestinationBucket,
		item.Operation,
		item.Status,
		item.Attempts,
		item.MaxAttempts,
		item.Priority,
		item.CreatedAt,
		item.UpdatedAt,
	)

	return err
}

// getPendingQueueItems retrieves pending queue items
func (m *ClusterReplicationManager) getPendingQueueItems(ctx context.Context, limit int) ([]*ClusterReplicationQueueItem, error) {
	query := `
		SELECT id, replication_rule_id, tenant_id, source_bucket, object_key,
		       destination_node_id, destination_bucket, operation, status,
		       attempts, max_attempts, priority, created_at, updated_at
		FROM cluster_replication_queue
		WHERE status = 'pending' AND attempts < max_attempts
		ORDER BY priority DESC, created_at ASC
		LIMIT ?
	`

	rows, err := m.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*ClusterReplicationQueueItem
	for rows.Next() {
		item := &ClusterReplicationQueueItem{}
		err := rows.Scan(
			&item.ID,
			&item.ReplicationRuleID,
			&item.TenantID,
			&item.SourceBucket,
			&item.ObjectKey,
			&item.DestinationNodeID,
			&item.DestinationBucket,
			&item.Operation,
			&item.Status,
			&item.Attempts,
			&item.MaxAttempts,
			&item.Priority,
			&item.CreatedAt,
			&item.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		items = append(items, item)
	}

	return items, rows.Err()
}

// updateRuleLastSync updates the last_sync_at timestamp for a rule
func (m *ClusterReplicationManager) updateRuleLastSync(ctx context.Context, ruleID string) error {
	_, err := m.db.ExecContext(ctx, `
		UPDATE cluster_bucket_replication
		SET last_sync_at = ?, updated_at = ?
		WHERE id = ?
	`, time.Now(), time.Now(), ruleID)
	return err
}

// Stop stops the cluster replication manager
func (m *ClusterReplicationManager) Stop() {
	m.log.Info("Stopping cluster replication manager")
	close(m.stopChan)
	close(m.queueChan)
	m.wg.Wait()
	m.log.Info("Cluster replication manager stopped")
}

// CreateReplicationRule creates a new cluster replication rule
func (m *ClusterReplicationManager) CreateReplicationRule(ctx context.Context, rule *ClusterReplicationRule) error {
	if rule.ID == "" {
		rule.ID = uuid.New().String()
	}

	now := time.Now()
	rule.CreatedAt = now
	rule.UpdatedAt = now

	// Validate sync interval
	minIntervalStr, err := GetGlobalConfig(ctx, m.db, "min_sync_interval_seconds")
	if err == nil {
		minInterval, _ := strconv.Atoi(minIntervalStr)
		if rule.SyncIntervalSeconds < minInterval {
			return fmt.Errorf("sync interval must be at least %d seconds", minInterval)
		}
	}

	_, err = m.db.ExecContext(ctx, `
		INSERT INTO cluster_bucket_replication (
			id, tenant_id, source_bucket, destination_node_id, destination_bucket,
			sync_interval_seconds, enabled, replicate_deletes, replicate_metadata,
			prefix, priority, objects_replicated, bytes_replicated, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rule.ID,
		rule.TenantID,
		rule.SourceBucket,
		rule.DestinationNodeID,
		rule.DestinationBucket,
		rule.SyncIntervalSeconds,
		boolToInt(rule.Enabled),
		boolToInt(rule.ReplicateDeletes),
		boolToInt(rule.ReplicateMetadata),
		rule.Prefix,
		rule.Priority,
		0, // objects_replicated
		0, // bytes_replicated
		rule.CreatedAt,
		rule.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create replication rule: %w", err)
	}

	m.log.WithFields(logrus.Fields{
		"rule_id":       rule.ID,
		"source_bucket": rule.SourceBucket,
		"dest_node":     rule.DestinationNodeID,
	}).Info("Created cluster replication rule")

	return nil
}

// boolToInt converts bool to int for SQLite
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
