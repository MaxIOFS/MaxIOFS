package cluster

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockBucketManagerForLocation is a mock implementation of bucket.Manager for location tests
type MockBucketManagerForLocation struct {
	mu             sync.RWMutex
	buckets        map[string]*bucket.Bucket // key: "tenantID/bucketName"
	err            error                      // error to return on operations
	getErr         error                      // error to return on GetBucketInfo
	updateErr      error                      // error to return on UpdateBucket
	callCount      map[string]int             // track method call counts
}

func NewMockBucketManagerForLocation() *MockBucketManagerForLocation {
	return &MockBucketManagerForLocation{
		buckets:   make(map[string]*bucket.Bucket),
		callCount: make(map[string]int),
	}
}

func (m *MockBucketManagerForLocation) getBucketKey(tenantID, bucketName string) string {
	return fmt.Sprintf("%s/%s", tenantID, bucketName)
}

func (m *MockBucketManagerForLocation) GetBucketInfo(ctx context.Context, tenantID, bucketName string) (*bucket.Bucket, error) {
	m.mu.Lock()
	m.callCount["GetBucketInfo"]++
	m.mu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.err != nil {
		return nil, m.err
	}

	key := m.getBucketKey(tenantID, bucketName)
	bkt, exists := m.buckets[key]
	if !exists {
		return nil, errors.New("bucket not found")
	}

	// Return a copy to avoid race conditions
	bucketCopy := *bkt
	if bkt.Metadata != nil {
		bucketCopy.Metadata = make(map[string]string)
		for k, v := range bkt.Metadata {
			bucketCopy.Metadata[k] = v
		}
	}
	return &bucketCopy, nil
}

func (m *MockBucketManagerForLocation) UpdateBucket(ctx context.Context, tenantID, bucketName string, bkt *bucket.Bucket) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount["UpdateBucket"]++

	if m.updateErr != nil {
		return m.updateErr
	}
	if m.err != nil {
		return m.err
	}

	key := m.getBucketKey(tenantID, bucketName)
	if _, exists := m.buckets[key]; !exists {
		return errors.New("bucket not found")
	}

	// Store a copy to avoid race conditions
	bucketCopy := *bkt
	if bkt.Metadata != nil {
		bucketCopy.Metadata = make(map[string]string)
		for k, v := range bkt.Metadata {
			bucketCopy.Metadata[k] = v
		}
	}
	m.buckets[key] = &bucketCopy
	return nil
}

func (m *MockBucketManagerForLocation) CreateBucket(ctx context.Context, tenantID, bucketName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.getBucketKey(tenantID, bucketName)
	m.buckets[key] = &bucket.Bucket{
		Name:     bucketName,
		TenantID: tenantID,
		Metadata: make(map[string]string),
	}
	return nil
}

// Implement other required methods as no-ops for this test
func (m *MockBucketManagerForLocation) DeleteBucket(ctx context.Context, tenantID, name string) error {
	return nil
}
func (m *MockBucketManagerForLocation) ForceDeleteBucket(ctx context.Context, tenantID, name string) error {
	return nil
}
func (m *MockBucketManagerForLocation) ListBuckets(ctx context.Context, tenantID string) ([]bucket.Bucket, error) {
	return nil, nil
}
func (m *MockBucketManagerForLocation) BucketExists(ctx context.Context, tenantID, name string) (bool, error) {
	return false, nil
}
func (m *MockBucketManagerForLocation) GetBucketPolicy(ctx context.Context, tenantID, name string) (*bucket.Policy, error) {
	return nil, nil
}
func (m *MockBucketManagerForLocation) SetBucketPolicy(ctx context.Context, tenantID, name string, policy *bucket.Policy) error {
	return nil
}
func (m *MockBucketManagerForLocation) DeleteBucketPolicy(ctx context.Context, tenantID, name string) error {
	return nil
}
func (m *MockBucketManagerForLocation) GetVersioning(ctx context.Context, tenantID, name string) (*bucket.VersioningConfig, error) {
	return nil, nil
}
func (m *MockBucketManagerForLocation) SetVersioning(ctx context.Context, tenantID, name string, config *bucket.VersioningConfig) error {
	return nil
}
func (m *MockBucketManagerForLocation) GetLifecycle(ctx context.Context, tenantID, name string) (*bucket.LifecycleConfig, error) {
	return nil, nil
}
func (m *MockBucketManagerForLocation) SetLifecycle(ctx context.Context, tenantID, name string, config *bucket.LifecycleConfig) error {
	return nil
}
func (m *MockBucketManagerForLocation) DeleteLifecycle(ctx context.Context, tenantID, name string) error {
	return nil
}
func (m *MockBucketManagerForLocation) SetBucketTags(ctx context.Context, tenantID, name string, tags map[string]string) error {
	return nil
}
func (m *MockBucketManagerForLocation) GetCORS(ctx context.Context, tenantID, name string) (*bucket.CORSConfig, error) {
	return nil, nil
}
func (m *MockBucketManagerForLocation) SetCORS(ctx context.Context, tenantID, name string, config *bucket.CORSConfig) error {
	return nil
}
func (m *MockBucketManagerForLocation) DeleteCORS(ctx context.Context, tenantID, name string) error {
	return nil
}
func (m *MockBucketManagerForLocation) GetObjectLockConfig(ctx context.Context, tenantID, name string) (*bucket.ObjectLockConfig, error) {
	return nil, nil
}
func (m *MockBucketManagerForLocation) SetObjectLockConfig(ctx context.Context, tenantID, name string, config *bucket.ObjectLockConfig) error {
	return nil
}
func (m *MockBucketManagerForLocation) GetBucketACL(ctx context.Context, tenantID, name string) (interface{}, error) {
	return nil, nil
}
func (m *MockBucketManagerForLocation) SetBucketACL(ctx context.Context, tenantID, name string, acl interface{}) error {
	return nil
}
func (m *MockBucketManagerForLocation) GetACLManager() interface{} {
	return nil
}
func (m *MockBucketManagerForLocation) IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error {
	return nil
}
func (m *MockBucketManagerForLocation) DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error {
	return nil
}
func (m *MockBucketManagerForLocation) RecalculateMetrics(ctx context.Context, tenantID, name string) error {
	return nil
}
func (m *MockBucketManagerForLocation) IsReady() bool {
	return true
}

// Test helper to create a BucketLocationManager
func createTestBucketLocationManager(bucketMgr *MockBucketManagerForLocation, localNodeID string) *BucketLocationManager {
	cache := NewBucketLocationCache(5 * time.Minute)
	return NewBucketLocationManager(bucketMgr, cache, localNodeID)
}

func TestNewBucketLocationManager(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	cache := NewBucketLocationCache(5 * time.Minute)
	localNodeID := "node-1"

	blm := NewBucketLocationManager(bucketMgr, cache, localNodeID)

	assert.NotNil(t, blm)
	assert.Equal(t, localNodeID, blm.localNodeID)
	assert.NotNil(t, blm.cache)
	assert.NotNil(t, blm.bucketManager)
	assert.NotNil(t, blm.log)
}

func TestGetBucketLocation_FromCache(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	blm := createTestBucketLocationManager(bucketMgr, "node-1")

	// Pre-populate cache
	bucketName := "test-bucket"
	expectedNodeID := "node-2"
	blm.cache.Set(bucketName, expectedNodeID)

	ctx := context.Background()
	nodeID, err := blm.GetBucketLocation(ctx, "tenant-1", bucketName)

	require.NoError(t, err)
	assert.Equal(t, expectedNodeID, nodeID)
}

func TestGetBucketLocation_FromMetadata(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	blm := createTestBucketLocationManager(bucketMgr, "node-1")

	// Create bucket with location metadata
	tenantID := "tenant-1"
	bucketName := "test-bucket"
	expectedNodeID := "node-2"

	bucketMgr.CreateBucket(context.Background(), tenantID, bucketName)
	bkt, _ := bucketMgr.GetBucketInfo(context.Background(), tenantID, bucketName)
	bkt.Metadata[MetadataKeyLocation] = expectedNodeID
	bucketMgr.UpdateBucket(context.Background(), tenantID, bucketName, bkt)

	ctx := context.Background()
	nodeID, err := blm.GetBucketLocation(ctx, tenantID, bucketName)

	require.NoError(t, err)
	assert.Equal(t, expectedNodeID, nodeID)

	// Verify it was cached
	cachedNodeID := blm.cache.Get(bucketName)
	assert.Equal(t, expectedNodeID, cachedNodeID)
}

func TestGetBucketLocation_NoMetadata_DefaultsToLocalNode(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	localNodeID := "node-1"
	blm := createTestBucketLocationManager(bucketMgr, localNodeID)

	// Create bucket without location metadata
	tenantID := "tenant-1"
	bucketName := "test-bucket"
	bucketMgr.CreateBucket(context.Background(), tenantID, bucketName)

	ctx := context.Background()
	nodeID, err := blm.GetBucketLocation(ctx, tenantID, bucketName)

	require.NoError(t, err)
	assert.Equal(t, localNodeID, nodeID) // Should default to local node

	// Verify it was cached
	cachedNodeID := blm.cache.Get(bucketName)
	assert.Equal(t, localNodeID, cachedNodeID)
}

func TestGetBucketLocation_BucketNotFound(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	blm := createTestBucketLocationManager(bucketMgr, "node-1")

	ctx := context.Background()
	_, err := blm.GetBucketLocation(ctx, "tenant-1", "non-existent-bucket")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get bucket")
}

func TestGetBucketLocation_ManagerError(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	blm := createTestBucketLocationManager(bucketMgr, "node-1")

	// Set error on bucket manager
	bucketMgr.err = errors.New("database connection failed")

	ctx := context.Background()
	_, err := blm.GetBucketLocation(ctx, "tenant-1", "test-bucket")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get bucket")
	assert.Contains(t, err.Error(), "database connection failed")
}

func TestSetBucketLocation_Success(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	blm := createTestBucketLocationManager(bucketMgr, "node-1")

	// Create bucket with initial location
	tenantID := "tenant-1"
	bucketName := "test-bucket"
	bucketMgr.CreateBucket(context.Background(), tenantID, bucketName)
	bkt, _ := bucketMgr.GetBucketInfo(context.Background(), tenantID, bucketName)
	bkt.Metadata[MetadataKeyLocation] = "node-1"
	bucketMgr.UpdateBucket(context.Background(), tenantID, bucketName, bkt)

	// Pre-populate cache
	blm.cache.Set(bucketName, "node-1")

	// Update location
	newNodeID := "node-2"
	ctx := context.Background()
	err := blm.SetBucketLocation(ctx, tenantID, bucketName, newNodeID)

	require.NoError(t, err)

	// Verify metadata was updated
	updatedBkt, _ := bucketMgr.GetBucketInfo(context.Background(), tenantID, bucketName)
	assert.Equal(t, newNodeID, updatedBkt.Metadata[MetadataKeyLocation])

	// Verify cache was invalidated
	cachedNodeID := blm.cache.Get(bucketName)
	assert.Empty(t, cachedNodeID)
}

func TestSetBucketLocation_BucketWithoutMetadata(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	blm := createTestBucketLocationManager(bucketMgr, "node-1")

	// Create bucket without metadata map
	tenantID := "tenant-1"
	bucketName := "test-bucket"
	bucketMgr.CreateBucket(context.Background(), tenantID, bucketName)
	bkt, _ := bucketMgr.GetBucketInfo(context.Background(), tenantID, bucketName)
	bkt.Metadata = nil // Explicitly set to nil
	bucketMgr.UpdateBucket(context.Background(), tenantID, bucketName, bkt)

	// Set location
	newNodeID := "node-2"
	ctx := context.Background()
	err := blm.SetBucketLocation(ctx, tenantID, bucketName, newNodeID)

	require.NoError(t, err)

	// Verify metadata was created and updated
	updatedBkt, _ := bucketMgr.GetBucketInfo(context.Background(), tenantID, bucketName)
	assert.NotNil(t, updatedBkt.Metadata)
	assert.Equal(t, newNodeID, updatedBkt.Metadata[MetadataKeyLocation])
}

func TestSetBucketLocation_BucketNotFound(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	blm := createTestBucketLocationManager(bucketMgr, "node-1")

	ctx := context.Background()
	err := blm.SetBucketLocation(ctx, "tenant-1", "non-existent-bucket", "node-2")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get bucket")
}

func TestSetBucketLocation_UpdateError(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	blm := createTestBucketLocationManager(bucketMgr, "node-1")

	// Create bucket
	tenantID := "tenant-1"
	bucketName := "test-bucket"
	bucketMgr.CreateBucket(context.Background(), tenantID, bucketName)

	// Get the bucket first to populate metadata
	bkt, _ := bucketMgr.GetBucketInfo(context.Background(), tenantID, bucketName)
	bkt.Metadata[MetadataKeyLocation] = "node-1"
	bucketMgr.UpdateBucket(context.Background(), tenantID, bucketName, bkt)

	// Now set error on UpdateBucket only - GetBucketInfo will succeed
	bucketMgr.updateErr = errors.New("database write failed")

	ctx := context.Background()
	err := blm.SetBucketLocation(ctx, tenantID, bucketName, "node-2")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update bucket metadata")
}

func TestInitializeBucketLocation_NewBucket(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	blm := createTestBucketLocationManager(bucketMgr, "node-1")

	// Create bucket without location
	tenantID := "tenant-1"
	bucketName := "test-bucket"
	bucketMgr.CreateBucket(context.Background(), tenantID, bucketName)

	// Initialize location
	expectedNodeID := "node-2"
	ctx := context.Background()
	err := blm.InitializeBucketLocation(ctx, tenantID, bucketName, expectedNodeID)

	require.NoError(t, err)

	// Verify metadata was set
	bkt, _ := bucketMgr.GetBucketInfo(context.Background(), tenantID, bucketName)
	assert.Equal(t, expectedNodeID, bkt.Metadata[MetadataKeyLocation])

	// Verify it was cached
	cachedNodeID := blm.cache.Get(bucketName)
	assert.Equal(t, expectedNodeID, cachedNodeID)
}

func TestInitializeBucketLocation_AlreadyInitialized(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	blm := createTestBucketLocationManager(bucketMgr, "node-1")

	// Create bucket with existing location
	tenantID := "tenant-1"
	bucketName := "test-bucket"
	existingNodeID := "node-2"
	bucketMgr.CreateBucket(context.Background(), tenantID, bucketName)
	bkt, _ := bucketMgr.GetBucketInfo(context.Background(), tenantID, bucketName)
	bkt.Metadata[MetadataKeyLocation] = existingNodeID
	bucketMgr.UpdateBucket(context.Background(), tenantID, bucketName, bkt)

	// Try to initialize with different node
	ctx := context.Background()
	err := blm.InitializeBucketLocation(ctx, tenantID, bucketName, "node-3")

	require.NoError(t, err) // Should succeed but not override

	// Verify original location was preserved
	bkt, _ = bucketMgr.GetBucketInfo(context.Background(), tenantID, bucketName)
	assert.Equal(t, existingNodeID, bkt.Metadata[MetadataKeyLocation])
}

func TestInitializeBucketLocation_BucketNotFound(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	blm := createTestBucketLocationManager(bucketMgr, "node-1")

	ctx := context.Background()
	err := blm.InitializeBucketLocation(ctx, "tenant-1", "non-existent-bucket", "node-2")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get bucket")
}

func TestInitializeBucketLocation_UpdateError(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	blm := createTestBucketLocationManager(bucketMgr, "node-1")

	// Create bucket
	tenantID := "tenant-1"
	bucketName := "test-bucket"
	bucketMgr.CreateBucket(context.Background(), tenantID, bucketName)

	// Set error on UpdateBucket only - GetBucketInfo will succeed
	bucketMgr.updateErr = errors.New("database write failed")

	ctx := context.Background()
	err := blm.InitializeBucketLocation(ctx, tenantID, bucketName, "node-2")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize bucket location")
}

func TestBucketLocation_InvalidateCache(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	blm := createTestBucketLocationManager(bucketMgr, "node-1")

	// Pre-populate cache
	bucketName := "test-bucket"
	blm.cache.Set(bucketName, "node-2")

	// Verify cache has the entry
	assert.NotEmpty(t, blm.cache.Get(bucketName))

	// Invalidate cache
	blm.InvalidateCache(bucketName)

	// Verify cache was cleared
	assert.Empty(t, blm.cache.Get(bucketName))
}

func TestBucketLocationManager_ConcurrentAccess(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	blm := createTestBucketLocationManager(bucketMgr, "node-1")

	// Create multiple buckets
	tenantID := "tenant-1"
	for i := 0; i < 10; i++ {
		bucketName := fmt.Sprintf("bucket-%d", i)
		bucketMgr.CreateBucket(context.Background(), tenantID, bucketName)
		bkt, _ := bucketMgr.GetBucketInfo(context.Background(), tenantID, bucketName)
		bkt.Metadata[MetadataKeyLocation] = fmt.Sprintf("node-%d", i%3)
		bucketMgr.UpdateBucket(context.Background(), tenantID, bucketName, bkt)
	}

	// Concurrently access bucket locations
	var wg sync.WaitGroup
	numGoroutines := 20
	ctx := context.Background()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			bucketName := fmt.Sprintf("bucket-%d", id%10)

			// Get location
			nodeID, err := blm.GetBucketLocation(ctx, tenantID, bucketName)
			assert.NoError(t, err)
			assert.NotEmpty(t, nodeID)

			// Set location
			newNodeID := fmt.Sprintf("node-%d", id%5)
			err = blm.SetBucketLocation(ctx, tenantID, bucketName, newNodeID)
			assert.NoError(t, err)

			// Invalidate cache
			blm.InvalidateCache(bucketName)

			// Get location again
			nodeID, err = blm.GetBucketLocation(ctx, tenantID, bucketName)
			assert.NoError(t, err)
			assert.NotEmpty(t, nodeID)
		}(i)
	}

	wg.Wait()
}

func TestBucketLocationManager_CacheExpiration(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()

	// Create manager with very short TTL
	cache := NewBucketLocationCache(100 * time.Millisecond)
	blm := &BucketLocationManager{
		bucketManager: bucketMgr,
		cache:         cache,
		localNodeID:   "node-1",
		log:           logrus.WithField("component", "bucket-location-manager"),
	}

	// Create bucket with location
	tenantID := "tenant-1"
	bucketName := "test-bucket"
	expectedNodeID := "node-2"
	bucketMgr.CreateBucket(context.Background(), tenantID, bucketName)
	bkt, _ := bucketMgr.GetBucketInfo(context.Background(), tenantID, bucketName)
	bkt.Metadata[MetadataKeyLocation] = expectedNodeID
	bucketMgr.UpdateBucket(context.Background(), tenantID, bucketName, bkt)

	ctx := context.Background()

	// First access - should fetch from metadata and cache
	nodeID, err := blm.GetBucketLocation(ctx, tenantID, bucketName)
	require.NoError(t, err)
	assert.Equal(t, expectedNodeID, nodeID)

	// Second access - should fetch from cache
	nodeID, err = blm.GetBucketLocation(ctx, tenantID, bucketName)
	require.NoError(t, err)
	assert.Equal(t, expectedNodeID, nodeID)

	// Wait for cache to expire
	time.Sleep(150 * time.Millisecond)

	// Third access - cache should be expired, fetch from metadata again
	nodeID, err = blm.GetBucketLocation(ctx, tenantID, bucketName)
	require.NoError(t, err)
	assert.Equal(t, expectedNodeID, nodeID)
}

func TestBucketLocationManager_MetadataUpdateAfterCache(t *testing.T) {
	bucketMgr := NewMockBucketManagerForLocation()
	blm := createTestBucketLocationManager(bucketMgr, "node-1")

	// Create bucket with initial location
	tenantID := "tenant-1"
	bucketName := "test-bucket"
	initialNodeID := "node-2"
	bucketMgr.CreateBucket(context.Background(), tenantID, bucketName)
	bkt, _ := bucketMgr.GetBucketInfo(context.Background(), tenantID, bucketName)
	bkt.Metadata[MetadataKeyLocation] = initialNodeID
	bucketMgr.UpdateBucket(context.Background(), tenantID, bucketName, bkt)

	ctx := context.Background()

	// First access - caches the location
	nodeID, err := blm.GetBucketLocation(ctx, tenantID, bucketName)
	require.NoError(t, err)
	assert.Equal(t, initialNodeID, nodeID)

	// Update location directly in bucket manager (simulating external update)
	newNodeID := "node-3"
	bkt, _ = bucketMgr.GetBucketInfo(context.Background(), tenantID, bucketName)
	bkt.Metadata[MetadataKeyLocation] = newNodeID
	bucketMgr.UpdateBucket(context.Background(), tenantID, bucketName, bkt)

	// Second access - should still return cached (stale) value
	nodeID, err = blm.GetBucketLocation(ctx, tenantID, bucketName)
	require.NoError(t, err)
	assert.Equal(t, initialNodeID, nodeID) // Still cached

	// Invalidate cache
	blm.InvalidateCache(bucketName)

	// Third access - should fetch new value
	nodeID, err = blm.GetBucketLocation(ctx, tenantID, bucketName)
	require.NoError(t, err)
	assert.Equal(t, newNodeID, nodeID)
}
