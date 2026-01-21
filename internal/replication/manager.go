package replication

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

// Manager handles replication operations
type Manager struct {
	db              *sql.DB
	config          ReplicationConfig
	workers         []*Worker
	queue           chan *QueueItem
	stopChan        chan struct{}
	wg              sync.WaitGroup
	mu              sync.RWMutex
	running         bool
	objectAdapter   ObjectAdapter
	objectManager   ObjectManager
	bucketLister    BucketLister
	ruleLocks       map[string]*sync.Mutex // Locks per rule to prevent concurrent syncs
	locksMu         sync.RWMutex           // Protects ruleLocks map
	s3ClientFactory S3ClientFactory        // Factory for creating S3 clients (for testing)
}

// ObjectAdapter provides methods to interact with objects
type ObjectAdapter interface {
	CopyObject(ctx context.Context, sourceBucket, sourceKey, destBucket, destKey, tenantID string) (int64, error)
	DeleteObject(ctx context.Context, bucket, key, tenantID string) error
	GetObjectMetadata(ctx context.Context, bucket, key, tenantID string) (map[string]string, error)
}

// BucketLister provides methods to list objects in a bucket
type BucketLister interface {
	ListObjects(ctx context.Context, tenantID, bucket, prefix string, maxKeys int) ([]string, error)
}

// NewManager creates a new replication manager
func NewManager(db *sql.DB, config ReplicationConfig, objectAdapter ObjectAdapter, objectManager ObjectManager, bucketLister BucketLister) (*Manager, error) {
	if err := InitSchema(db); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	if config.WorkerCount == 0 {
		config.WorkerCount = 5
	}
	if config.QueueSize == 0 {
		config.QueueSize = 1000
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryInterval == 0 {
		config.RetryInterval = 5 * time.Minute
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 24 * time.Hour
	}
	if config.RetentionDays == 0 {
		config.RetentionDays = 30
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}

	// Default S3 client factory (uses real AWS SDK)
	defaultS3Factory := func(endpoint, region, accessKey, secretKey string) S3Client {
		return NewS3RemoteClient(endpoint, region, accessKey, secretKey)
	}

	return &Manager{
		db:              db,
		config:          config,
		queue:           make(chan *QueueItem, config.QueueSize),
		stopChan:        make(chan struct{}),
		objectAdapter:   objectAdapter,
		objectManager:   objectManager,
		bucketLister:    bucketLister,
		ruleLocks:       make(map[string]*sync.Mutex),
		s3ClientFactory: defaultS3Factory,
	}, nil
}

// NewManagerWithS3Factory creates a new replication manager with custom S3 client factory (for testing)
func NewManagerWithS3Factory(db *sql.DB, config ReplicationConfig, objectAdapter ObjectAdapter, objectManager ObjectManager, bucketLister BucketLister, s3Factory S3ClientFactory) (*Manager, error) {
	manager, err := NewManager(db, config, objectAdapter, objectManager, bucketLister)
	if err != nil {
		return nil, err
	}
	manager.s3ClientFactory = s3Factory
	return manager, nil
}

// Start starts the replication manager and workers
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("manager already running")
	}

	// Start workers
	m.workers = make([]*Worker, m.config.WorkerCount)
	for i := 0; i < m.config.WorkerCount; i++ {
		worker := NewWorkerWithS3Factory(i, m.queue, m.db, m.objectAdapter, m.objectManager, m.s3ClientFactory)
		m.workers[i] = worker
		m.wg.Add(1)
		go func(w *Worker) {
			defer m.wg.Done()
			w.Start(ctx, m.stopChan)
		}(worker)
	}

	// Start cleanup routine
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.cleanupRoutine(ctx)
	}()

	// Start queue loader
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.queueLoader(ctx)
	}()

	// Start rule scheduler
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.ruleScheduler(ctx)
	}()

	m.running = true
	return nil
}

// Stop stops the replication manager
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return nil
	}

	close(m.stopChan)
	m.wg.Wait()
	close(m.queue)
	m.running = false
	return nil
}

// CreateRule creates a new replication rule
func (m *Manager) CreateRule(ctx context.Context, rule *ReplicationRule) error {
	if rule.ID == "" {
		rule.ID = uuid.New().String()
	}
	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()

	query := `
		INSERT INTO replication_rules (
			id, tenant_id, source_bucket, destination_endpoint, destination_bucket,
			destination_access_key, destination_secret_key, destination_region, prefix, enabled,
			priority, mode, schedule_interval, conflict_resolution, replicate_deletes,
			replicate_metadata, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := m.db.ExecContext(ctx, query,
		rule.ID, rule.TenantID, rule.SourceBucket, rule.DestinationEndpoint, rule.DestinationBucket,
		rule.DestinationAccessKey, rule.DestinationSecretKey, rule.DestinationRegion, rule.Prefix, rule.Enabled,
		rule.Priority, rule.Mode, rule.ScheduleInterval, rule.ConflictResolution, rule.ReplicateDeletes,
		rule.ReplicateMetadata, rule.CreatedAt, rule.UpdatedAt,
	)
	return err
}

// GetRule retrieves a replication rule by ID
func (m *Manager) GetRule(ctx context.Context, ruleID string) (*ReplicationRule, error) {
	query := `
		SELECT id, tenant_id, source_bucket, destination_endpoint, destination_bucket,
			   destination_access_key, destination_secret_key, destination_region, prefix, enabled,
			   priority, mode, schedule_interval, conflict_resolution, replicate_deletes,
			   replicate_metadata, created_at, updated_at
		FROM replication_rules WHERE id = ?
	`

	rule := &ReplicationRule{}
	err := m.db.QueryRowContext(ctx, query, ruleID).Scan(
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

// ListRules lists all replication rules for a tenant
func (m *Manager) ListRules(ctx context.Context, tenantID string) ([]*ReplicationRule, error) {
	query := `
		SELECT id, tenant_id, source_bucket, destination_endpoint, destination_bucket,
			   destination_access_key, destination_secret_key, destination_region, prefix, enabled,
			   priority, mode, schedule_interval, conflict_resolution, replicate_deletes,
			   replicate_metadata, created_at, updated_at
		FROM replication_rules
		WHERE tenant_id = ?
		ORDER BY priority DESC, created_at ASC
	`

	rows, err := m.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*ReplicationRule
	for rows.Next() {
		rule := &ReplicationRule{}
		err := rows.Scan(
			&rule.ID, &rule.TenantID, &rule.SourceBucket, &rule.DestinationEndpoint, &rule.DestinationBucket,
			&rule.DestinationAccessKey, &rule.DestinationSecretKey, &rule.DestinationRegion, &rule.Prefix, &rule.Enabled,
			&rule.Priority, &rule.Mode, &rule.ScheduleInterval, &rule.ConflictResolution, &rule.ReplicateDeletes,
			&rule.ReplicateMetadata, &rule.CreatedAt, &rule.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

// GetRulesForBucket returns all replication rules for a specific bucket
func (m *Manager) GetRulesForBucket(ctx context.Context, bucketName string) ([]*ReplicationRule, error) {
	query := `
		SELECT id, tenant_id, source_bucket, destination_endpoint, destination_bucket,
			   destination_access_key, destination_secret_key, destination_region, prefix, enabled,
			   priority, mode, schedule_interval, conflict_resolution, replicate_deletes,
			   replicate_metadata, created_at, updated_at
		FROM replication_rules
		WHERE source_bucket = ?
		ORDER BY priority DESC, created_at ASC
	`

	rows, err := m.db.QueryContext(ctx, query, bucketName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*ReplicationRule
	for rows.Next() {
		rule := &ReplicationRule{}
		err := rows.Scan(
			&rule.ID, &rule.TenantID, &rule.SourceBucket, &rule.DestinationEndpoint, &rule.DestinationBucket,
			&rule.DestinationAccessKey, &rule.DestinationSecretKey, &rule.DestinationRegion, &rule.Prefix, &rule.Enabled,
			&rule.Priority, &rule.Mode, &rule.ScheduleInterval, &rule.ConflictResolution, &rule.ReplicateDeletes,
			&rule.ReplicateMetadata, &rule.CreatedAt, &rule.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

// UpdateRule updates an existing replication rule
func (m *Manager) UpdateRule(ctx context.Context, rule *ReplicationRule) error {
	rule.UpdatedAt = time.Now()

	query := `
		UPDATE replication_rules SET
			destination_endpoint = ?, destination_bucket = ?, destination_access_key = ?, destination_secret_key = ?,
			destination_region = ?, prefix = ?, enabled = ?, priority = ?, mode = ?, schedule_interval = ?,
			conflict_resolution = ?, replicate_deletes = ?, replicate_metadata = ?,
			updated_at = ?
		WHERE id = ? AND tenant_id = ?
	`

	result, err := m.db.ExecContext(ctx, query,
		rule.DestinationEndpoint, rule.DestinationBucket, rule.DestinationAccessKey, rule.DestinationSecretKey,
		rule.DestinationRegion, rule.Prefix, rule.Enabled, rule.Priority, rule.Mode, rule.ScheduleInterval,
		rule.ConflictResolution, rule.ReplicateDeletes, rule.ReplicateMetadata,
		rule.UpdatedAt, rule.ID, rule.TenantID,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("rule not found")
	}
	return nil
}

// DeleteRule deletes a replication rule
func (m *Manager) DeleteRule(ctx context.Context, tenantID, ruleID string) error {
	query := `DELETE FROM replication_rules WHERE id = ? AND tenant_id = ?`
	result, err := m.db.ExecContext(ctx, query, ruleID, tenantID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("rule not found")
	}
	return nil
}

// QueueObject queues an object for replication
func (m *Manager) QueueObject(ctx context.Context, tenantID, bucket, objectKey, action string) error {
	// Find matching rules
	rules, err := m.findMatchingRules(ctx, tenantID, bucket, objectKey)
	if err != nil {
		return err
	}

	// Queue object for each matching rule
	for _, rule := range rules {
		item := &QueueItem{
			RuleID:     rule.ID,
			TenantID:   tenantID,
			Bucket:     bucket,
			ObjectKey:  objectKey,
			Action:     action,
			Status:     StatusPending,
			MaxRetries: m.config.MaxRetries,
			ScheduledAt: time.Now(),
		}

		query := `
			INSERT INTO replication_queue (
				rule_id, tenant_id, bucket, object_key, action,
				status, max_retries, scheduled_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`
		_, err := m.db.ExecContext(ctx, query,
			item.RuleID, item.TenantID, item.Bucket, item.ObjectKey, item.Action,
			item.Status, item.MaxRetries, item.ScheduledAt,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// GetMetrics retrieves replication metrics for a rule
func (m *Manager) GetMetrics(ctx context.Context, ruleID string) (*ReplicationMetrics, error) {
	query := `
		SELECT
			COUNT(*) as total,
			SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) as pending,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) as completed,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed,
			SUM(bytes_replicated) as bytes
		FROM replication_queue
		WHERE rule_id = ?
	`

	metrics := &ReplicationMetrics{RuleID: ruleID}
	err := m.db.QueryRowContext(ctx, query, ruleID).Scan(
		&metrics.TotalObjects,
		&metrics.PendingObjects,
		&metrics.CompletedObjects,
		&metrics.FailedObjects,
		&metrics.BytesReplicated,
	)
	return metrics, err
}

// findMatchingRules finds replication rules that match the object
func (m *Manager) findMatchingRules(ctx context.Context, tenantID, bucket, objectKey string) ([]*ReplicationRule, error) {
	query := `
		SELECT id, tenant_id, source_bucket, destination_endpoint, destination_bucket,
			   destination_access_key, destination_secret_key, destination_region, prefix, enabled,
			   priority, mode, schedule_interval, conflict_resolution, replicate_deletes,
			   replicate_metadata, created_at, updated_at
		FROM replication_rules
		WHERE tenant_id = ? AND source_bucket = ? AND enabled = 1
		ORDER BY priority DESC
	`

	rows, err := m.db.QueryContext(ctx, query, tenantID, bucket)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*ReplicationRule
	for rows.Next() {
		rule := &ReplicationRule{}
		err := rows.Scan(
			&rule.ID, &rule.TenantID, &rule.SourceBucket, &rule.DestinationEndpoint, &rule.DestinationBucket,
			&rule.DestinationAccessKey, &rule.DestinationSecretKey, &rule.DestinationRegion, &rule.Prefix, &rule.Enabled,
			&rule.Priority, &rule.Mode, &rule.ScheduleInterval, &rule.ConflictResolution, &rule.ReplicateDeletes,
			&rule.ReplicateMetadata, &rule.CreatedAt, &rule.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		// Check prefix match
		if rule.Prefix == "" || matchesPrefix(objectKey, rule.Prefix) {
			rules = append(rules, rule)
		}
	}
	return rules, rows.Err()
}

// queueLoader periodically loads pending items from database to queue
func (m *Manager) queueLoader(ctx context.Context) {
	// Load pending items immediately on startup
	m.loadPendingItems(ctx)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.loadPendingItems(ctx)
		}
	}
}

// loadPendingItems loads pending items from database
func (m *Manager) loadPendingItems(ctx context.Context) {
	query := `
		SELECT id, rule_id, tenant_id, bucket, object_key, version_id,
			   action, status, attempts, max_retries, last_error,
			   scheduled_at, bytes_replicated
		FROM replication_queue
		WHERE status = 'pending' OR (status = 'failed' AND attempts < max_retries)
		ORDER BY scheduled_at ASC
		LIMIT ?
	`

	rows, err := m.db.QueryContext(ctx, query, m.config.BatchSize)
	if err != nil {
		logrus.WithError(err).Error("Failed to query pending items from database")
		return
	}
	defer rows.Close()

	itemCount := 0
	for rows.Next() {
		item := &QueueItem{}
		err := rows.Scan(
			&item.ID, &item.RuleID, &item.TenantID, &item.Bucket, &item.ObjectKey,
			&item.VersionID, &item.Action, &item.Status, &item.Attempts,
			&item.MaxRetries, &item.LastError, &item.ScheduledAt, &item.BytesReplicated,
		)
		if err != nil {
			logrus.WithError(err).Error("Failed to scan queue item")
			continue
		}

		itemCount++

		// Try to queue item (non-blocking)
		select {
		case m.queue <- item:
			// Successfully queued
		default:
			logrus.Warn("Replication queue is full, item will be retried later")
		}
	}

	if itemCount > 0 {
		logrus.WithField("count", itemCount).Debug("Loaded pending replication items")
	}
}

// cleanupRoutine periodically cleans up old completed items
func (m *Manager) cleanupRoutine(ctx context.Context) {
	ticker := time.NewTicker(m.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.cleanup(ctx)
		}
	}
}

// ruleScheduler periodically checks and executes scheduled replication rules
func (m *Manager) ruleScheduler(ctx context.Context) {
	// Check every minute for scheduled rules
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Track last sync time for each rule
	lastSync := make(map[string]time.Time)

	logrus.Info("Replication rule scheduler started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.processScheduledRules(ctx, lastSync)
		}
	}
}

// processScheduledRules checks and processes rules that need to be synced
func (m *Manager) processScheduledRules(ctx context.Context, lastSync map[string]time.Time) {
	// Get all enabled scheduled rules
	query := `
		SELECT id, tenant_id, source_bucket, schedule_interval, mode
		FROM replication_rules
		WHERE enabled = 1 AND mode = 'scheduled' AND schedule_interval > 0
	`

	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		logrus.WithError(err).Error("Failed to query scheduled rules")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var ruleID, tenantID, sourceBucket, mode string
		var scheduleInterval int

		err := rows.Scan(&ruleID, &tenantID, &sourceBucket, &scheduleInterval, &mode)
		if err != nil {
			logrus.WithError(err).Error("Failed to scan rule")
			continue
		}

		// Check if it's time to sync this rule
		lastSyncTime, exists := lastSync[ruleID]
		now := time.Now()

		if !exists || now.Sub(lastSyncTime) >= time.Duration(scheduleInterval)*time.Minute {
			logrus.WithFields(logrus.Fields{
				"rule_id":  ruleID,
				"bucket":   sourceBucket,
				"interval": scheduleInterval,
			}).Info("Triggering scheduled sync")

			// Trigger sync in a goroutine to not block the scheduler
			go func(rID string) {
				count, err := m.SyncRule(ctx, rID)
				if err != nil {
					logrus.WithFields(logrus.Fields{
						"rule_id": rID,
					}).WithError(err).Error("Scheduled sync failed")
					return
				}

				logrus.WithFields(logrus.Fields{
					"rule_id": rID,
					"objects": count,
				}).Info("Scheduled sync completed")

				// Update last sync time
				lastSync[rID] = time.Now()
			}(ruleID)
		}
	}
}

// cleanup removes old completed/failed items
func (m *Manager) cleanup(ctx context.Context) {
	cutoff := time.Now().AddDate(0, 0, -m.config.RetentionDays)

	query := `
		DELETE FROM replication_queue
		WHERE (status = 'completed' OR status = 'failed')
		AND completed_at < ?
	`
	m.db.ExecContext(ctx, query, cutoff)
}

// SyncBucket synchronizes all objects in a bucket according to a replication rule
func (m *Manager) SyncBucket(ctx context.Context, ruleID string) (int, error) {
	// Get the rule
	rule, err := m.GetRule(ctx, ruleID)
	if err != nil {
		return 0, fmt.Errorf("failed to get rule: %w", err)
	}
	if rule == nil {
		return 0, fmt.Errorf("rule not found: %s", ruleID)
	}
	if !rule.Enabled {
		return 0, fmt.Errorf("rule is disabled: %s", ruleID)
	}

	// List all objects in the source bucket
	objects, err := m.bucketLister.ListObjects(ctx, rule.TenantID, rule.SourceBucket, rule.Prefix, 10000)
	if err != nil {
		return 0, fmt.Errorf("failed to list objects: %w", err)
	}

	// Queue each object for replication
	queuedCount := 0
	for _, objectKey := range objects {
		// Check if object matches prefix filter
		if !matchesPrefix(objectKey, rule.Prefix) {
			continue
		}

		// Queue the object
		err := m.QueueObject(ctx, rule.TenantID, rule.SourceBucket, objectKey, "PUT")
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"rule_id": ruleID,
				"object":  objectKey,
			}).WithError(err).Warn("Failed to queue object for replication")
			continue
		}
		queuedCount++
	}

	logrus.WithFields(logrus.Fields{
		"rule_id": ruleID,
		"bucket":  rule.SourceBucket,
		"queued":  queuedCount,
		"total":   len(objects),
	}).Info("Bucket sync completed")

	return queuedCount, nil
}

// SyncRule triggers a sync for a specific replication rule
func (m *Manager) SyncRule(ctx context.Context, ruleID string) (int, error) {
	// Try to acquire lock for this rule
	if !m.tryLockRule(ruleID) {
		return 0, fmt.Errorf("sync already in progress for rule: %s", ruleID)
	}
	defer m.unlockRule(ruleID)

	logrus.WithField("rule_id", ruleID).Info("Starting rule sync")

	// Perform the sync
	count, err := m.SyncBucket(ctx, ruleID)
	if err != nil {
		return 0, fmt.Errorf("sync failed: %w", err)
	}

	// Update last sync timestamp in rule (optional enhancement)
	// This would require adding last_sync_at field to the rules table

	logrus.WithFields(logrus.Fields{
		"rule_id": ruleID,
		"objects": count,
	}).Info("Rule sync completed successfully")

	return count, nil
}

// tryLockRule attempts to acquire a lock for a rule
func (m *Manager) tryLockRule(ruleID string) bool {
	m.locksMu.Lock()
	defer m.locksMu.Unlock()

	// Check if lock exists for this rule
	lock, exists := m.ruleLocks[ruleID]
	if !exists {
		// Create new lock for this rule
		lock = &sync.Mutex{}
		m.ruleLocks[ruleID] = lock
	}

	// Try to acquire the lock (non-blocking)
	return lock.TryLock()
}

// unlockRule releases the lock for a rule
func (m *Manager) unlockRule(ruleID string) {
	m.locksMu.RLock()
	lock, exists := m.ruleLocks[ruleID]
	m.locksMu.RUnlock()

	if exists {
		lock.Unlock()
	}
}

// matchesPrefix checks if objectKey matches the prefix
func matchesPrefix(objectKey, prefix string) bool {
	if prefix == "" {
		return true
	}
	return len(objectKey) >= len(prefix) && objectKey[:len(prefix)] == prefix
}

// MarshalJSON for ReplicationRule
func (r *ReplicationRule) MarshalJSON() ([]byte, error) {
	type Alias ReplicationRule
	return json.Marshal(&struct {
		*Alias
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}{
		Alias:     (*Alias)(r),
		CreatedAt: r.CreatedAt.Format(time.RFC3339),
		UpdatedAt: r.UpdatedAt.Format(time.RFC3339),
	})
}
