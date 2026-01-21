package replication

import (
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// InMemoryObjectStore simulates object storage for testing replication
type InMemoryObjectStore struct {
	mu      sync.RWMutex
	objects map[string][]byte // key: "tenantID/bucket/key" -> value: content
}

func NewInMemoryObjectStore() *InMemoryObjectStore {
	return &InMemoryObjectStore{
		objects: make(map[string][]byte),
	}
}

func (s *InMemoryObjectStore) PutObject(tenantID, bucket, key string, content []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	objKey := fmt.Sprintf("%s/%s/%s", tenantID, bucket, key)
	s.objects[objKey] = content
}

func (s *InMemoryObjectStore) GetObject(tenantID, bucket, key string) ([]byte, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	objKey := fmt.Sprintf("%s/%s/%s", tenantID, bucket, key)
	content, exists := s.objects[objKey]
	return content, exists
}

func (s *InMemoryObjectStore) ListObjects(tenantID, bucket, prefix string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var keys []string
	searchPrefix := fmt.Sprintf("%s/%s/", tenantID, bucket)

	for objKey := range s.objects {
		if strings.HasPrefix(objKey, searchPrefix) {
			key := strings.TrimPrefix(objKey, searchPrefix)
			if prefix == "" || strings.HasPrefix(key, prefix) {
				keys = append(keys, key)
			}
		}
	}
	return keys
}

func (s *InMemoryObjectStore) DeleteObject(tenantID, bucket, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	objKey := fmt.Sprintf("%s/%s/%s", tenantID, bucket, key)
	delete(s.objects, objKey)
}

// TestObjectManager implements ObjectManager interface using in-memory storage
type TestObjectManager struct {
	store *InMemoryObjectStore
}

func NewTestObjectManager(store *InMemoryObjectStore) *TestObjectManager {
	return &TestObjectManager{store: store}
}

func (m *TestObjectManager) GetObject(ctx context.Context, tenantID, bucket, key string) (io.ReadCloser, int64, string, map[string]string, error) {
	content, exists := m.store.GetObject(tenantID, bucket, key)
	if !exists {
		return nil, 0, "", nil, fmt.Errorf("object not found")
	}

	reader := io.NopCloser(bytes.NewReader(content))
	return reader, int64(len(content)), "application/octet-stream", map[string]string{}, nil
}

func (m *TestObjectManager) GetObjectMetadata(ctx context.Context, tenantID, bucket, key string) (int64, string, map[string]string, error) {
	content, exists := m.store.GetObject(tenantID, bucket, key)
	if !exists {
		return 0, "", nil, fmt.Errorf("object not found")
	}

	return int64(len(content)), "application/octet-stream", map[string]string{}, nil
}

// TestBucketLister implements BucketLister interface using in-memory storage
type TestBucketLister struct {
	store *InMemoryObjectStore
}

func NewTestBucketLister(store *InMemoryObjectStore) *TestBucketLister {
	return &TestBucketLister{store: store}
}

func (l *TestBucketLister) ListObjects(ctx context.Context, tenantID, bucket, prefix string, maxKeys int) ([]string, error) {
	keys := l.store.ListObjects(tenantID, bucket, prefix)
	if len(keys) > maxKeys {
		keys = keys[:maxKeys]
	}
	return keys, nil
}

// TestReplicationAdapter simulates replication between two in-memory stores
type TestReplicationAdapter struct {
	sourceStore *InMemoryObjectStore
	destStore   *InMemoryObjectStore
}

func NewTestReplicationAdapter(sourceStore, destStore *InMemoryObjectStore) *TestReplicationAdapter {
	return &TestReplicationAdapter{
		sourceStore: sourceStore,
		destStore:   destStore,
	}
}

func (a *TestReplicationAdapter) CopyObject(ctx context.Context, sourceBucket, sourceKey, destBucket, destKey, tenantID string) (int64, error) {
	// Get from source store
	content, exists := a.sourceStore.GetObject(tenantID, sourceBucket, sourceKey)
	if !exists {
		return 0, fmt.Errorf("source object not found: %s/%s", sourceBucket, sourceKey)
	}

	// Put to destination store (simulating remote S3)
	a.destStore.PutObject(tenantID, destBucket, destKey, content)

	return int64(len(content)), nil
}

func (a *TestReplicationAdapter) DeleteObject(ctx context.Context, bucket, key, tenantID string) error {
	a.destStore.DeleteObject(tenantID, bucket, key)
	return nil
}

func (a *TestReplicationAdapter) GetObjectMetadata(ctx context.Context, bucket, key, tenantID string) (map[string]string, error) {
	_, exists := a.sourceStore.GetObject(tenantID, bucket, key)
	if !exists {
		return nil, fmt.Errorf("object not found")
	}
	return map[string]string{}, nil
}

// FailingReplicationAdapter simulates failures for retry testing
type FailingReplicationAdapter struct {
	sourceStore  *InMemoryObjectStore
	destStore    *InMemoryObjectStore
	attemptCount *int
	attemptMu    *sync.Mutex
	t            *testing.T
}

func (a *FailingReplicationAdapter) CopyObject(ctx context.Context, sourceBucket, sourceKey, destBucket, destKey, tenantID string) (int64, error) {
	a.attemptMu.Lock()
	*a.attemptCount++
	currentAttempt := *a.attemptCount
	a.attemptMu.Unlock()

	if currentAttempt <= 2 {
		a.t.Logf("Attempt %d: Simulating failure", currentAttempt)
		return 0, fmt.Errorf("simulated network error")
	}

	a.t.Logf("Attempt %d: Succeeding", currentAttempt)

	// Success: copy object
	content, exists := a.sourceStore.GetObject(tenantID, sourceBucket, sourceKey)
	if !exists {
		return 0, fmt.Errorf("source object not found")
	}

	a.destStore.PutObject(tenantID, destBucket, destKey, content)
	return int64(len(content)), nil
}

func (a *FailingReplicationAdapter) DeleteObject(ctx context.Context, bucket, key, tenantID string) error {
	return nil
}

func (a *FailingReplicationAdapter) GetObjectMetadata(ctx context.Context, bucket, key, tenantID string) (map[string]string, error) {
	return map[string]string{}, nil
}

// MockS3Client implements S3Client interface for testing
type MockS3Client struct {
	sourceStore      *InMemoryObjectStore
	destStore        *InMemoryObjectStore
	tenantID         string
	sourceBucket     string
	destinationBucket string
	t                *testing.T
}

func (m *MockS3Client) PutObject(ctx context.Context, bucket, key string, data io.Reader, size int64, contentType string, metadata map[string]string) error {
	m.t.Logf("MockS3Client.PutObject: bucket=%s, key=%s, size=%d", bucket, key, size)

	// Read all data
	content, err := io.ReadAll(data)
	if err != nil {
		return fmt.Errorf("failed to read data: %w", err)
	}

	// Write to destination store
	m.destStore.PutObject(m.tenantID, bucket, key, content)
	m.t.Logf("✅ MockS3Client: Successfully wrote object %s to destination store", key)

	return nil
}

func (m *MockS3Client) DeleteObject(ctx context.Context, bucket, key string) error {
	m.destStore.DeleteObject(m.tenantID, bucket, key)
	return nil
}

func (m *MockS3Client) HeadObject(ctx context.Context, bucket, key string) (map[string]string, int64, error) {
	content, exists := m.destStore.GetObject(m.tenantID, bucket, key)
	if !exists {
		return nil, 0, fmt.Errorf("object not found")
	}
	return map[string]string{}, int64(len(content)), nil
}

func (m *MockS3Client) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, int64, error) {
	content, exists := m.destStore.GetObject(m.tenantID, bucket, key)
	if !exists {
		return nil, 0, fmt.Errorf("object not found")
	}
	return io.NopCloser(bytes.NewReader(content)), int64(len(content)), nil
}

func (m *MockS3Client) CopyObject(ctx context.Context, sourceBucket, sourceKey, destBucket, destKey string) error {
	content, exists := m.destStore.GetObject(m.tenantID, sourceBucket, sourceKey)
	if !exists {
		return fmt.Errorf("source object not found")
	}
	m.destStore.PutObject(m.tenantID, destBucket, destKey, content)
	return nil
}

func (m *MockS3Client) ListObjects(ctx context.Context, bucket, prefix string, maxKeys int32) ([]types.Object, error) {
	keys := m.destStore.ListObjects(m.tenantID, bucket, prefix)
	objects := make([]types.Object, 0, len(keys))
	for _, key := range keys {
		objects = append(objects, types.Object{
			Key: &key,
		})
	}
	return objects, nil
}

func (m *MockS3Client) TestConnection(ctx context.Context) error {
	return nil
}

// TestReplicationEndToEnd_WithInMemoryStores tests complete replication flow
func TestReplicationEndToEnd_WithInMemoryStores(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Enable debug logging
	logrus.SetLevel(logrus.DebugLevel)

	ctx := context.Background()

	// Setup two in-memory stores (source and destination)
	sourceStore := NewInMemoryObjectStore()
	destStore := NewInMemoryObjectStore()

	tenantID := "test-tenant"
	sourceBucket := "source-bucket"
	destBucket := "dest-bucket"

	// Populate source bucket with objects
	testObjects := map[string][]byte{
		"file1.txt":     []byte("This is file 1 content"),
		"file2.txt":     []byte("This is file 2 content"),
		"dir/file3.txt": []byte("This is file 3 in directory"),
		"large.bin":     bytes.Repeat([]byte("X"), 10000), // 10KB
	}

	t.Log("Populating source bucket with test objects...")
	for key, content := range testObjects {
		sourceStore.PutObject(tenantID, sourceBucket, key, content)
	}

	// Setup replication manager
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "replication.db")
	replicationDB, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=10000&_synchronous=NORMAL&cache=shared")
	require.NoError(t, err)
	defer replicationDB.Close()

	// Set connection pool settings for better concurrency
	// Use only 1 connection to avoid lock contention in tests
	replicationDB.SetMaxOpenConns(1)
	replicationDB.SetMaxIdleConns(1)

	// Execute PRAGMA commands to ensure WAL mode is enabled
	_, err = replicationDB.Exec("PRAGMA journal_mode=WAL;")
	require.NoError(t, err)
	_, err = replicationDB.Exec("PRAGMA busy_timeout=10000;")
	require.NoError(t, err)

	config := ReplicationConfig{
		Enable:          true,
		WorkerCount:     2,
		QueueSize:       100,
		BatchSize:       10,
		RetryInterval:   2 * time.Second,
		MaxRetries:      3,
		CleanupInterval: 1 * time.Hour,
		RetentionDays:   30,
	}

	sourceObjectManager := NewTestObjectManager(sourceStore)
	adapter := NewTestReplicationAdapter(sourceStore, destStore)
	lister := NewTestBucketLister(sourceStore)

	// Create mock S3 client factory
	mockS3Factory := func(endpoint, region, accessKey, secretKey string) S3Client {
		return &MockS3Client{
			sourceStore:      sourceStore,
			destStore:        destStore,
			tenantID:         tenantID,
			sourceBucket:     sourceBucket,
			destinationBucket: destBucket,
			t:                t,
		}
	}

	manager, err := NewManagerWithS3Factory(replicationDB, config, adapter, sourceObjectManager, lister, mockS3Factory)
	require.NoError(t, err)

	// Create replication rule
	rule := &ReplicationRule{
		TenantID:             tenantID,
		SourceBucket:         sourceBucket,
		DestinationEndpoint:  "http://fake-destination:8080", // Not used in test adapter
		DestinationBucket:    destBucket,
		DestinationAccessKey: "fake-key",
		DestinationSecretKey: "fake-secret",
		DestinationRegion:    "us-east-1",
		Enabled:              true,
		Mode:                 ModeRealTime,
		ReplicateDeletes:     false,
		ReplicateMetadata:    true,
		ConflictResolution:   ConflictLWW,
	}

	err = manager.CreateRule(ctx, rule)
	require.NoError(t, err)
	t.Logf("✅ Created replication rule: %s", rule.ID)

	// Queue all objects for replication BEFORE starting manager
	t.Log("Queueing objects for replication...")
	for key := range testObjects {
		err := manager.QueueObject(ctx, tenantID, sourceBucket, key, "PUT")
		require.NoError(t, err)
	}

	// Start manager (starts workers and loads pending items) - keep running for all subtests
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	t.Run("Replicate all objects from source to destination", func(t *testing.T) {
		// Wait for workers to process queue
		t.Log("Waiting for replication to complete...")
		time.Sleep(5 * time.Second)

		// Verify all objects were replicated
		t.Log("Verifying replicated objects...")
		for key, expectedContent := range testObjects {
			destContent, exists := destStore.GetObject(tenantID, destBucket, key)
			require.True(t, exists, "Object %s should exist in destination", key)

			// Verify content matches
			assert.Equal(t, expectedContent, destContent, "Content mismatch for %s", key)

			// Verify MD5
			expectedMD5 := fmt.Sprintf("%x", md5.Sum(expectedContent))
			actualMD5 := fmt.Sprintf("%x", md5.Sum(destContent))
			assert.Equal(t, expectedMD5, actualMD5, "MD5 mismatch for %s", key)
		}

		t.Logf("✅ All %d objects successfully replicated", len(testObjects))
	})

	t.Run("Verify replication metrics", func(t *testing.T) {
		metrics, err := manager.GetMetrics(ctx, rule.ID)
		require.NoError(t, err)

		t.Logf("📊 Replication Metrics:")
		t.Logf("   Total Objects: %d", metrics.TotalObjects)
		t.Logf("   Completed: %d", metrics.CompletedObjects)
		t.Logf("   Failed: %d", metrics.FailedObjects)
		t.Logf("   Pending: %d", metrics.PendingObjects)
		t.Logf("   Bytes Replicated: %d", metrics.BytesReplicated)

		assert.Equal(t, int64(len(testObjects)), metrics.CompletedObjects, "Should have completed all objects")
		assert.Equal(t, int64(0), metrics.FailedObjects, "Should have no failed objects")
		assert.Greater(t, metrics.BytesReplicated, int64(0), "Should have replicated bytes")
	})

	t.Run("Test prefix filtering", func(t *testing.T) {
		// Create new rule with prefix filter
		filteredRule := &ReplicationRule{
			TenantID:             tenantID,
			SourceBucket:         sourceBucket,
			DestinationBucket:    "filtered-dest",
			DestinationEndpoint:  "http://fake",
			DestinationAccessKey: "fake",
			DestinationSecretKey: "fake",
			Enabled:              true,
			Mode:                 ModeRealTime,
			Prefix:               "dir/", // Only replicate objects in "dir/" prefix
		}

		err := manager.CreateRule(ctx, filteredRule)
		require.NoError(t, err)

		// Queue object matching prefix
		err = manager.QueueObject(ctx, tenantID, sourceBucket, "dir/file3.txt", "PUT")
		require.NoError(t, err)

		// Wait long enough for queueLoader to pick it up (runs every 10 seconds)
		t.Log("Waiting for replication...")
		time.Sleep(12 * time.Second)

		// Verify only the prefixed object was replicated
		content, exists := destStore.GetObject(tenantID, "filtered-dest", "dir/file3.txt")
		assert.True(t, exists, "Prefixed object should be replicated")
		assert.Equal(t, testObjects["dir/file3.txt"], content)

		t.Log("✅ Prefix filtering works correctly")
	})
}

// TestSchedulerTriggers tests that the scheduler properly triggers syncs at intervals
func TestSchedulerTriggers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping scheduler test in short mode")
	}

	ctx := context.Background()

	sourceStore := NewInMemoryObjectStore()
	destStore := NewInMemoryObjectStore()

	tenantID := "test-tenant"
	sourceBucket := "scheduled-source"
	destBucket := "scheduled-dest"

	// Add objects to source
	sourceStore.PutObject(tenantID, sourceBucket, "scheduled1.txt", []byte("content1"))
	sourceStore.PutObject(tenantID, sourceBucket, "scheduled2.txt", []byte("content2"))

	tmpDir := t.TempDir()
	replicationDB, err := sql.Open("sqlite", filepath.Join(tmpDir, "replication.db"))
	require.NoError(t, err)
	defer replicationDB.Close()

	config := ReplicationConfig{
		Enable:        true,
		WorkerCount:   2,
		QueueSize:     100,
		RetryInterval: 1 * time.Second,
		MaxRetries:    3,
	}

	sourceObjectManager := NewTestObjectManager(sourceStore)
	adapter := NewTestReplicationAdapter(sourceStore, destStore)
	lister := NewTestBucketLister(sourceStore)

	manager, err := NewManager(replicationDB, config, adapter, sourceObjectManager, lister)
	require.NoError(t, err)

	// Create scheduled rule (every 1 minute, but we'll manually trigger for testing)
	rule := &ReplicationRule{
		TenantID:             tenantID,
		SourceBucket:         sourceBucket,
		DestinationBucket:    destBucket,
		DestinationEndpoint:  "http://fake",
		DestinationAccessKey: "fake",
		DestinationSecretKey: "fake",
		Enabled:              true,
		Mode:                 ModeScheduled,
		ScheduleInterval:     1, // 1 minute
	}

	err = manager.CreateRule(ctx, rule)
	require.NoError(t, err)

	// Start manager
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	t.Log("Manually triggering sync...")
	count, err := manager.SyncRule(ctx, rule.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "Should queue 2 objects")

	// Wait for processing
	time.Sleep(3 * time.Second)

	// Verify objects were replicated
	content1, exists1 := destStore.GetObject(tenantID, destBucket, "scheduled1.txt")
	content2, exists2 := destStore.GetObject(tenantID, destBucket, "scheduled2.txt")

	assert.True(t, exists1, "scheduled1.txt should be replicated")
	assert.True(t, exists2, "scheduled2.txt should be replicated")
	assert.Equal(t, []byte("content1"), content1)
	assert.Equal(t, []byte("content2"), content2)

	t.Log("✅ Scheduler sync completed successfully")
}

// TestReplicationRetries tests retry logic when replication fails
func TestReplicationRetries(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping retry test in short mode")
	}

	ctx := context.Background()

	sourceStore := NewInMemoryObjectStore()
	destStore := NewInMemoryObjectStore()

	tenantID := "test-tenant"
	sourceBucket := "retry-source"

	// Add object to source
	sourceStore.PutObject(tenantID, sourceBucket, "retry-test.txt", []byte("test content"))

	tmpDir := t.TempDir()
	replicationDB, err := sql.Open("sqlite", filepath.Join(tmpDir, "replication.db"))
	require.NoError(t, err)
	defer replicationDB.Close()

	// Create adapter that fails first 2 times, then succeeds
	var attemptCount int
	var attemptMu sync.Mutex

	failingAdapter := &FailingReplicationAdapter{
		sourceStore:  sourceStore,
		destStore:    destStore,
		attemptCount: &attemptCount,
		attemptMu:    &attemptMu,
		t:            t,
	}

	config := ReplicationConfig{
		Enable:        true,
		WorkerCount:   1,
		QueueSize:     10,
		RetryInterval: 1 * time.Second,
		MaxRetries:    5,
	}

	sourceObjectManager := NewTestObjectManager(sourceStore)
	lister := NewTestBucketLister(sourceStore)

	manager, err := NewManager(replicationDB, config, failingAdapter, sourceObjectManager, lister)
	require.NoError(t, err)

	rule := &ReplicationRule{
		TenantID:             tenantID,
		SourceBucket:         sourceBucket,
		DestinationBucket:    "retry-dest",
		DestinationEndpoint:  "http://fake",
		DestinationAccessKey: "fake",
		DestinationSecretKey: "fake",
		Enabled:              true,
		Mode:                 ModeRealTime,
	}

	err = manager.CreateRule(ctx, rule)
	require.NoError(t, err)

	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	// Queue object
	err = manager.QueueObject(ctx, tenantID, sourceBucket, "retry-test.txt", "PUT")
	require.NoError(t, err)

	// Wait for retries and eventual success
	time.Sleep(6 * time.Second)

	attemptMu.Lock()
	finalAttempts := attemptCount
	attemptMu.Unlock()

	assert.Equal(t, 3, finalAttempts, "Should have attempted 3 times (2 failures + 1 success)")

	// Verify object was eventually replicated
	content, exists := destStore.GetObject(tenantID, "retry-dest", "retry-test.txt")
	assert.True(t, exists, "Object should eventually be replicated after retries")
	assert.Equal(t, []byte("test content"), content)

	t.Log("✅ Retry logic works correctly")
}
