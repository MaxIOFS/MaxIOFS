package replication

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setupWorkerDB(t *testing.T) *sql.DB {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "worker-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=10000&_synchronous=NORMAL")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() })

	require.NoError(t, InitSchema(db))
	return db
}

func insertTestRule(t *testing.T, db *sql.DB, rule *ReplicationRule) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO replication_rules (
			id, tenant_id, source_bucket, destination_endpoint, destination_bucket,
			destination_access_key, destination_secret_key, destination_region,
			prefix, enabled, priority, mode, schedule_interval, conflict_resolution,
			replicate_deletes, replicate_metadata, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rule.ID, rule.TenantID, rule.SourceBucket, rule.DestinationEndpoint, rule.DestinationBucket,
		rule.DestinationAccessKey, rule.DestinationSecretKey, rule.DestinationRegion,
		rule.Prefix, rule.Enabled, rule.Priority, rule.Mode, rule.ScheduleInterval,
		rule.ConflictResolution, rule.ReplicateDeletes, rule.ReplicateMetadata,
		time.Now(), time.Now(),
	)
	require.NoError(t, err)
}

func insertTestQueueItem(t *testing.T, db *sql.DB, item *QueueItem) *QueueItem {
	t.Helper()
	result, err := db.Exec(`
		INSERT INTO replication_queue (
			rule_id, tenant_id, bucket, object_key, version_id,
			action, status, attempts, max_retries, scheduled_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.RuleID, item.TenantID, item.Bucket, item.ObjectKey, item.VersionID,
		item.Action, StatusPending, item.Attempts, item.MaxRetries, time.Now(),
	)
	require.NoError(t, err)
	id, _ := result.LastInsertId()
	item.ID = id
	return item
}

func getQueueItemStatus(t *testing.T, db *sql.DB, itemID int64) (ReplicationStatus, int, string) {
	t.Helper()
	var status ReplicationStatus
	var attempts int
	var lastError sql.NullString
	err := db.QueryRow(
		`SELECT status, attempts, last_error FROM replication_queue WHERE id = ?`, itemID,
	).Scan(&status, &attempts, &lastError)
	require.NoError(t, err)
	return status, attempts, lastError.String
}

func defaultRule(id string) *ReplicationRule {
	return &ReplicationRule{
		ID:                   id,
		TenantID:             "tenant1",
		SourceBucket:         "src-bucket",
		DestinationEndpoint:  "http://remote-s3:9000",
		DestinationBucket:    "dst-bucket",
		DestinationAccessKey: "access-key",
		DestinationSecretKey: "secret-key",
		DestinationRegion:    "us-east-1",
		Enabled:              true,
		Mode:                 ModeRealTime,
		ReplicateDeletes:     true,
		ReplicateMetadata:    true,
		ConflictResolution:   ConflictLWW,
	}
}

// simpleS3Factory returns a MockS3Client backed by the given in-memory store.
func simpleS3Factory(store *InMemoryObjectStore, tenantID string, t *testing.T) S3ClientFactory {
	return func(endpoint, region, accessKey, secretKey string) S3Client {
		return &MockS3Client{
			destStore: store,
			tenantID:  tenantID,
			t:         t,
		}
	}
}

// failingS3Factory returns a factory whose client always errors on PutObject.
func failingS3Factory(t *testing.T) S3ClientFactory {
	return func(endpoint, region, accessKey, secretKey string) S3Client {
		return &alwaysFailS3Client{t: t}
	}
}

type alwaysFailS3Client struct{ t *testing.T }

func (f *alwaysFailS3Client) PutObject(_ context.Context, _, _ string, _ io.Reader, _ int64, _ string, _ map[string]string) error {
	return fmt.Errorf("S3 unavailable")
}
func (f *alwaysFailS3Client) DeleteObject(_ context.Context, _, _ string) error {
	return fmt.Errorf("S3 unavailable")
}
func (f *alwaysFailS3Client) HeadObject(_ context.Context, _, _ string) (map[string]string, int64, error) {
	return nil, 0, fmt.Errorf("S3 unavailable")
}
func (f *alwaysFailS3Client) GetObject(_ context.Context, _, _ string) (io.ReadCloser, int64, error) {
	return nil, 0, fmt.Errorf("S3 unavailable")
}
func (f *alwaysFailS3Client) CopyObject(_ context.Context, _, _, _, _ string) error {
	return fmt.Errorf("S3 unavailable")
}
func (f *alwaysFailS3Client) ListObjects(_ context.Context, _, _ string, _ int32) ([]types.Object, error) {
	return nil, fmt.Errorf("S3 unavailable")
}
func (f *alwaysFailS3Client) TestConnection(_ context.Context) error {
	return fmt.Errorf("S3 unavailable")
}

// ---------------------------------------------------------------------------
// processItem — PUT success
// ---------------------------------------------------------------------------

func TestWorker_ProcessItem_PUT_Success(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	rule := defaultRule("rule-put-ok")
	insertTestRule(t, db, rule)

	content := []byte("object content")
	sourceStore := NewInMemoryObjectStore()
	sourceStore.PutObject("tenant1", "src-bucket", "file.txt", content)
	destStore := NewInMemoryObjectStore()

	om := NewTestObjectManager(sourceStore)
	worker := NewWorkerWithS3Factory(1,
		make(chan *QueueItem),
		db,
		&MockObjectAdapter{},
		om,
		simpleS3Factory(destStore, "tenant1", t),
	)

	item := insertTestQueueItem(t, db, &QueueItem{
		RuleID:     rule.ID,
		TenantID:   "tenant1",
		Bucket:     "src-bucket",
		ObjectKey:  "file.txt",
		Action:     "PUT",
		MaxRetries: 3,
	})

	worker.processItem(ctx, item)

	status, _, _ := getQueueItemStatus(t, db, item.ID)
	assert.Equal(t, StatusCompleted, status)

	// Object should have landed in destination store
	got, exists := destStore.GetObject("tenant1", "dst-bucket", "file.txt")
	assert.True(t, exists)
	assert.Equal(t, content, got)
}

// ---------------------------------------------------------------------------
// processItem — COPY treated same as PUT
// ---------------------------------------------------------------------------

func TestWorker_ProcessItem_COPY_Success(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	rule := defaultRule("rule-copy-ok")
	insertTestRule(t, db, rule)

	content := []byte("copy content")
	sourceStore := NewInMemoryObjectStore()
	sourceStore.PutObject("tenant1", "src-bucket", "copy.txt", content)
	destStore := NewInMemoryObjectStore()

	om := NewTestObjectManager(sourceStore)
	worker := NewWorkerWithS3Factory(1,
		make(chan *QueueItem),
		db,
		&MockObjectAdapter{},
		om,
		simpleS3Factory(destStore, "tenant1", t),
	)

	item := insertTestQueueItem(t, db, &QueueItem{
		RuleID:     rule.ID,
		TenantID:   "tenant1",
		Bucket:     "src-bucket",
		ObjectKey:  "copy.txt",
		Action:     "COPY",
		MaxRetries: 3,
	})

	worker.processItem(ctx, item)

	status, _, _ := getQueueItemStatus(t, db, item.ID)
	assert.Equal(t, StatusCompleted, status)
}

// ---------------------------------------------------------------------------
// processItem — PUT fails because source object not found
// ---------------------------------------------------------------------------

func TestWorker_ProcessItem_PUT_SourceNotFound(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	rule := defaultRule("rule-put-nf")
	insertTestRule(t, db, rule)

	om := &MockObjectManager{
		GetObjectFunc: func(_ context.Context, _, _, _ string) (io.ReadCloser, int64, string, map[string]string, error) {
			return nil, 0, "", nil, fmt.Errorf("object not found")
		},
	}

	worker := NewWorkerWithS3Factory(1, make(chan *QueueItem), db, &MockObjectAdapter{}, om,
		simpleS3Factory(NewInMemoryObjectStore(), "tenant1", t))

	item := insertTestQueueItem(t, db, &QueueItem{
		RuleID:     rule.ID,
		TenantID:   "tenant1",
		Bucket:     "src-bucket",
		ObjectKey:  "missing.txt",
		Action:     "PUT",
		MaxRetries: 3,
	})

	worker.processItem(ctx, item)

	status, _, lastErr := getQueueItemStatus(t, db, item.ID)
	// First attempt fails → goes back to pending for retry
	assert.Equal(t, StatusPending, status)
	assert.Contains(t, lastErr, "failed to get source object")
}

// ---------------------------------------------------------------------------
// processItem — PUT fails because S3 upload fails
// ---------------------------------------------------------------------------

func TestWorker_ProcessItem_PUT_S3Error(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	rule := defaultRule("rule-put-s3err")
	insertTestRule(t, db, rule)

	sourceStore := NewInMemoryObjectStore()
	sourceStore.PutObject("tenant1", "src-bucket", "file.txt", []byte("data"))
	om := NewTestObjectManager(sourceStore)

	worker := NewWorkerWithS3Factory(1, make(chan *QueueItem), db, &MockObjectAdapter{}, om,
		failingS3Factory(t))

	item := insertTestQueueItem(t, db, &QueueItem{
		RuleID:     rule.ID,
		TenantID:   "tenant1",
		Bucket:     "src-bucket",
		ObjectKey:  "file.txt",
		Action:     "PUT",
		MaxRetries: 3,
	})

	worker.processItem(ctx, item)

	status, _, lastErr := getQueueItemStatus(t, db, item.ID)
	assert.Equal(t, StatusPending, status)
	assert.Contains(t, lastErr, "S3 unavailable")
}

// ---------------------------------------------------------------------------
// processItem — DELETE with ReplicateDeletes=true
// ---------------------------------------------------------------------------

func TestWorker_ProcessItem_DELETE_Enabled(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	rule := defaultRule("rule-del-enabled")
	rule.ReplicateDeletes = true
	insertTestRule(t, db, rule)

	destStore := NewInMemoryObjectStore()
	destStore.PutObject("tenant1", "dst-bucket", "to-delete.txt", []byte("exists"))

	worker := NewWorkerWithS3Factory(1, make(chan *QueueItem), db, &MockObjectAdapter{},
		&MockObjectManager{},
		simpleS3Factory(destStore, "tenant1", t))

	item := insertTestQueueItem(t, db, &QueueItem{
		RuleID:     rule.ID,
		TenantID:   "tenant1",
		Bucket:     "src-bucket",
		ObjectKey:  "to-delete.txt",
		Action:     "DELETE",
		MaxRetries: 3,
	})

	worker.processItem(ctx, item)

	status, _, _ := getQueueItemStatus(t, db, item.ID)
	assert.Equal(t, StatusCompleted, status)

	_, exists := destStore.GetObject("tenant1", "dst-bucket", "to-delete.txt")
	assert.False(t, exists, "object should have been deleted from destination")
}

// ---------------------------------------------------------------------------
// processItem — DELETE with ReplicateDeletes=false (skipped)
// ---------------------------------------------------------------------------

func TestWorker_ProcessItem_DELETE_Disabled(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	rule := defaultRule("rule-del-disabled")
	rule.ReplicateDeletes = false
	insertTestRule(t, db, rule)

	worker := NewWorkerWithS3Factory(1, make(chan *QueueItem), db, &MockObjectAdapter{},
		&MockObjectManager{}, failingS3Factory(t))

	item := insertTestQueueItem(t, db, &QueueItem{
		RuleID:     rule.ID,
		TenantID:   "tenant1",
		Bucket:     "src-bucket",
		ObjectKey:  "file.txt",
		Action:     "DELETE",
		MaxRetries: 3,
	})

	worker.processItem(ctx, item)

	// Should be completed (skipped, not actually sent to S3)
	status, _, _ := getQueueItemStatus(t, db, item.ID)
	assert.Equal(t, StatusCompleted, status)
}

// ---------------------------------------------------------------------------
// processItem — unknown action
// ---------------------------------------------------------------------------

func TestWorker_ProcessItem_UnknownAction(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	rule := defaultRule("rule-unknown-action")
	insertTestRule(t, db, rule)

	worker := NewWorkerWithS3Factory(1, make(chan *QueueItem), db, &MockObjectAdapter{},
		&MockObjectManager{}, failingS3Factory(t))

	item := insertTestQueueItem(t, db, &QueueItem{
		RuleID:     rule.ID,
		TenantID:   "tenant1",
		Bucket:     "src-bucket",
		ObjectKey:  "file.txt",
		Action:     "PATCH",
		MaxRetries: 3,
	})

	worker.processItem(ctx, item)

	status, _, lastErr := getQueueItemStatus(t, db, item.ID)
	assert.Equal(t, StatusPending, status)
	assert.Contains(t, lastErr, "unknown action")
}

// ---------------------------------------------------------------------------
// processItem — rule not found
// ---------------------------------------------------------------------------

func TestWorker_ProcessItem_RuleNotFound(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	worker := NewWorkerWithS3Factory(1, make(chan *QueueItem), db, &MockObjectAdapter{},
		&MockObjectManager{}, failingS3Factory(t))

	item := insertTestQueueItem(t, db, &QueueItem{
		RuleID:     "nonexistent-rule",
		TenantID:   "tenant1",
		Bucket:     "src-bucket",
		ObjectKey:  "file.txt",
		Action:     "PUT",
		MaxRetries: 3,
	})

	worker.processItem(ctx, item)

	status, _, lastErr := getQueueItemStatus(t, db, item.ID)
	assert.Equal(t, StatusPending, status)
	assert.Contains(t, lastErr, "rule not found or disabled")
}

// ---------------------------------------------------------------------------
// processItem — rule disabled
// ---------------------------------------------------------------------------

func TestWorker_ProcessItem_RuleDisabled(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	rule := defaultRule("rule-disabled")
	rule.Enabled = false
	insertTestRule(t, db, rule)

	worker := NewWorkerWithS3Factory(1, make(chan *QueueItem), db, &MockObjectAdapter{},
		&MockObjectManager{}, failingS3Factory(t))

	item := insertTestQueueItem(t, db, &QueueItem{
		RuleID:     rule.ID,
		TenantID:   "tenant1",
		Bucket:     "src-bucket",
		ObjectKey:  "file.txt",
		Action:     "PUT",
		MaxRetries: 3,
	})

	worker.processItem(ctx, item)

	status, _, lastErr := getQueueItemStatus(t, db, item.ID)
	assert.Equal(t, StatusPending, status)
	assert.Contains(t, lastErr, "rule not found or disabled")
}

// ---------------------------------------------------------------------------
// handleError — max retries exceeded → StatusFailed
// ---------------------------------------------------------------------------

func TestWorker_HandleError_MaxRetriesExceeded(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	rule := defaultRule("rule-maxretries")
	insertTestRule(t, db, rule)

	worker := NewWorkerWithS3Factory(1, make(chan *QueueItem), db, &MockObjectAdapter{},
		&MockObjectManager{}, failingS3Factory(t))

	item := insertTestQueueItem(t, db, &QueueItem{
		RuleID:     rule.ID,
		TenantID:   "tenant1",
		Bucket:     "src-bucket",
		ObjectKey:  "file.txt",
		Action:     "PUT",
		Attempts:   2, // already at max-1
		MaxRetries: 3,
	})

	worker.handleError(ctx, item, fmt.Errorf("terminal error"))

	status, attempts, lastErr := getQueueItemStatus(t, db, item.ID)
	assert.Equal(t, StatusFailed, status)
	assert.Equal(t, 3, attempts)
	assert.Contains(t, lastErr, "terminal error")
}

// ---------------------------------------------------------------------------
// handleError — below max retries → StatusPending (retry)
// ---------------------------------------------------------------------------

func TestWorker_HandleError_BelowMaxRetries(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	rule := defaultRule("rule-retry")
	insertTestRule(t, db, rule)

	worker := NewWorkerWithS3Factory(1, make(chan *QueueItem), db, &MockObjectAdapter{},
		&MockObjectManager{}, failingS3Factory(t))

	item := insertTestQueueItem(t, db, &QueueItem{
		RuleID:     rule.ID,
		TenantID:   "tenant1",
		Bucket:     "src-bucket",
		ObjectKey:  "file.txt",
		Action:     "PUT",
		Attempts:   0,
		MaxRetries: 3,
	})

	worker.handleError(ctx, item, fmt.Errorf("transient error"))

	status, attempts, _ := getQueueItemStatus(t, db, item.ID)
	assert.Equal(t, StatusPending, status)
	assert.Equal(t, 1, attempts)
}

// ---------------------------------------------------------------------------
// getRule — found and not found
// ---------------------------------------------------------------------------

func TestWorker_GetRule_Found(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	rule := defaultRule("rule-get-found")
	insertTestRule(t, db, rule)

	worker := NewWorkerWithS3Factory(1, make(chan *QueueItem), db, &MockObjectAdapter{},
		&MockObjectManager{}, failingS3Factory(t))

	got, err := worker.getRule(ctx, rule.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, rule.ID, got.ID)
	assert.Equal(t, rule.SourceBucket, got.SourceBucket)
	assert.Equal(t, rule.DestinationBucket, got.DestinationBucket)
	assert.True(t, got.Enabled)
}

func TestWorker_GetRule_NotFound(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	worker := NewWorkerWithS3Factory(1, make(chan *QueueItem), db, &MockObjectAdapter{},
		&MockObjectManager{}, failingS3Factory(t))

	got, err := worker.getRule(ctx, "does-not-exist")
	require.NoError(t, err)
	assert.Nil(t, got)
}

// ---------------------------------------------------------------------------
// completeItem — sets status=completed and bytes_replicated
// ---------------------------------------------------------------------------

func TestWorker_CompleteItem(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	rule := defaultRule("rule-complete")
	insertTestRule(t, db, rule)

	worker := NewWorkerWithS3Factory(1, make(chan *QueueItem), db, &MockObjectAdapter{},
		&MockObjectManager{}, failingS3Factory(t))

	item := insertTestQueueItem(t, db, &QueueItem{
		RuleID:     rule.ID,
		TenantID:   "tenant1",
		Bucket:     "src-bucket",
		ObjectKey:  "file.txt",
		Action:     "PUT",
		MaxRetries: 3,
	})

	worker.completeItem(ctx, item, 4096)

	var status ReplicationStatus
	var bytes int64
	err := db.QueryRow(
		`SELECT status, bytes_replicated FROM replication_queue WHERE id = ?`, item.ID,
	).Scan(&status, &bytes)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, status)
	assert.Equal(t, int64(4096), bytes)
}

// ---------------------------------------------------------------------------
// updateReplicationStatus — inserts new record, then updates it
// ---------------------------------------------------------------------------

func TestWorker_UpdateReplicationStatus_Insert(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	rule := defaultRule("rule-status-insert")
	insertTestRule(t, db, rule)

	worker := NewWorkerWithS3Factory(1, make(chan *QueueItem), db, &MockObjectAdapter{},
		&MockObjectManager{}, failingS3Factory(t))

	item := &QueueItem{
		RuleID:    rule.ID,
		TenantID:  "tenant1",
		Bucket:    "src-bucket",
		ObjectKey: "obj.txt",
	}

	worker.updateReplicationStatus(ctx, rule, item, StatusCompleted, "")

	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM replication_status WHERE rule_id = ? AND source_key = ?`,
		rule.ID, "obj.txt",
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	var status ReplicationStatus
	err = db.QueryRow(
		`SELECT status FROM replication_status WHERE rule_id = ? AND source_key = ?`,
		rule.ID, "obj.txt",
	).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, StatusCompleted, status)
}

func TestWorker_UpdateReplicationStatus_Update(t *testing.T) {
	db := setupWorkerDB(t)
	ctx := context.Background()

	rule := defaultRule("rule-status-update")
	insertTestRule(t, db, rule)

	worker := NewWorkerWithS3Factory(1, make(chan *QueueItem), db, &MockObjectAdapter{},
		&MockObjectManager{}, failingS3Factory(t))

	item := &QueueItem{
		RuleID:    rule.ID,
		TenantID:  "tenant1",
		Bucket:    "src-bucket",
		ObjectKey: "obj.txt",
	}

	// First call inserts
	worker.updateReplicationStatus(ctx, rule, item, StatusPending, "first attempt")

	// Second call should update, not insert a new row
	worker.updateReplicationStatus(ctx, rule, item, StatusFailed, "permanent failure")

	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM replication_status WHERE rule_id = ? AND source_key = ?`,
		rule.ID, "obj.txt",
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "should update the existing record, not insert a new one")

	var status ReplicationStatus
	var errMsg string
	err = db.QueryRow(
		`SELECT status, error_message FROM replication_status WHERE rule_id = ? AND source_key = ?`,
		rule.ID, "obj.txt",
	).Scan(&status, &errMsg)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, status)
	assert.Equal(t, "permanent failure", errMsg)
}
