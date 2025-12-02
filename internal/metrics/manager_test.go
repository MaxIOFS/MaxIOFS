package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Path:     "/metrics",
		Interval: 10,
	}

	manager := NewManager(cfg)
	require.NotNil(t, manager)

	// Manager is not started yet, so it's not healthy
	assert.False(t, manager.IsHealthy())
}

func TestNewManager_Disabled(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable: false,
	}

	manager := NewManager(cfg)
	require.NotNil(t, manager)

	// Disabled manager should be noop
	_, ok := manager.(*noopManager)
	assert.True(t, ok, "disabled manager should be noopManager")
}

func TestRecordHTTPRequest(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	// Record successful request
	manager.RecordHTTPRequest("GET", "/api/v1/buckets", "200", 100*time.Millisecond)

	// Verify counters updated
	assert.Greater(t, manager.totalRequests, uint64(0))
	assert.Equal(t, manager.totalErrors, uint64(0))
}

func TestRecordHTTPRequest_Error(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	// Record error request
	manager.RecordHTTPRequest("GET", "/api/v1/buckets", "500", 100*time.Millisecond)

	// Verify error counter updated
	assert.Greater(t, manager.totalErrors, uint64(0))
}

func TestRecordHTTPRequestSize(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	// Should not panic
	manager.RecordHTTPRequestSize("POST", "/api/v1/objects", 1024)
}

func TestRecordHTTPResponseSize(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	// Should not panic
	manager.RecordHTTPResponseSize("GET", "/api/v1/objects", 2048)
}

func TestRecordS3Operation(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	// Record successful operation
	manager.RecordS3Operation("PutObject", "test-bucket", true, 50*time.Millisecond)

	// Record failed operation
	manager.RecordS3Operation("GetObject", "test-bucket", false, 25*time.Millisecond)
}

func TestRecordS3Error(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	manager.RecordS3Error("PutObject", "test-bucket", "NoSuchBucket")
}

func TestRecordStorageOperation(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	manager.RecordStorageOperation("write", true, 10*time.Millisecond)
	manager.RecordStorageOperation("read", false, 5*time.Millisecond)
}

func TestUpdateStorageUsage(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	manager.UpdateStorageUsage("test-bucket", 100, 1024*1024)
}

func TestRecordObjectOperation(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	manager.RecordObjectOperation("put", "test-bucket", 1024, 10*time.Millisecond)
}

func TestRecordAuthAttempt(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	manager.RecordAuthAttempt("jwt", true)
	manager.RecordAuthAttempt("s3v4", false)
}

func TestRecordAuthFailure(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	manager.RecordAuthFailure("jwt", "invalid_token")
}

func TestUpdateSystemMetrics(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	manager.UpdateSystemMetrics(50.5, 75.2, 60.0)
}

func TestRecordSystemEvent(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	details := map[string]string{
		"type":    "startup",
		"version": "0.4.2",
	}
	manager.RecordSystemEvent("server_started", details)
}

func TestUpdateBucketMetrics(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	manager.UpdateBucketMetrics("test-bucket", 50, 1024*1024*10)
}

func TestRecordBucketOperation(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	manager.RecordBucketOperation("create", "test-bucket", true)
	manager.RecordBucketOperation("delete", "test-bucket", false)
}

func TestRecordObjectLockOperation(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	manager.RecordObjectLockOperation("set_retention", "test-bucket", true)
}

func TestUpdateRetentionMetrics(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	manager.UpdateRetentionMetrics("test-bucket", 10, 5)
}

func TestRecordBackgroundTask(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	manager.RecordBackgroundTask("lifecycle_cleanup", 2*time.Second, true)
}

func TestUpdateCacheMetrics(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	manager.UpdateCacheMetrics(0.85, 1024*1024*50)
}

func TestGetMetricsHandler(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	handler := manager.GetMetricsHandler()
	assert.NotNil(t, handler)
}

func TestGetMetricsSnapshot(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	snapshot, err := manager.GetMetricsSnapshot()
	require.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.Contains(t, snapshot, "timestamp")
	assert.Contains(t, snapshot, "namespace")
}

func TestGetS3MetricsSnapshot(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	// Record some metrics first
	manager.RecordHTTPRequest("GET", "/bucket/key", "200", 100*time.Millisecond)

	snapshot, err := manager.GetS3MetricsSnapshot()
	require.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.Contains(t, snapshot, "totalRequests")
	assert.Contains(t, snapshot, "totalErrors")
	assert.Contains(t, snapshot, "avgLatency")
	assert.Contains(t, snapshot, "requestsPerSec")
}

func TestIsHealthy(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	// Before starting
	assert.False(t, manager.IsHealthy())

	// After starting
	ctx := context.Background()
	err := manager.Start(ctx)
	require.NoError(t, err)
	assert.True(t, manager.IsHealthy())

	// After stopping
	err = manager.Stop()
	require.NoError(t, err)
	assert.False(t, manager.IsHealthy())
}

func TestStartStop(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	ctx := context.Background()

	// Start
	err := manager.Start(ctx)
	require.NoError(t, err)

	// Try to start again (should error)
	err = manager.Start(ctx)
	assert.Error(t, err)

	// Stop
	err = manager.Stop()
	require.NoError(t, err)

	// Try to stop again (should error)
	err = manager.Stop()
	assert.Error(t, err)
}

func TestMiddleware(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	middleware := manager.Middleware()
	assert.NotNil(t, middleware)
}

func TestReset(t *testing.T) {
	cfg := config.MetricsConfig{
		Enable:   true,
		Interval: 10,
	}

	manager := NewManager(cfg).(*metricsManager)
	require.NotNil(t, manager)

	err := manager.Reset()
	assert.NoError(t, err)
}

func TestNoopManager(t *testing.T) {
	noop := &noopManager{}

	// All methods should not panic
	noop.RecordHTTPRequest("GET", "/", "200", 0)
	noop.RecordHTTPRequestSize("GET", "/", 0)
	noop.RecordHTTPResponseSize("GET", "/", 0)
	noop.RecordS3Operation("PutObject", "bucket", true, 0)
	noop.RecordS3Error("PutObject", "bucket", "error")
	noop.RecordStorageOperation("write", true, 0)
	noop.UpdateStorageUsage("bucket", 0, 0)
	noop.RecordObjectOperation("put", "bucket", 0, 0)
	noop.RecordAuthAttempt("jwt", true)
	noop.RecordAuthFailure("jwt", "reason")
	noop.UpdateSystemMetrics(0, 0, 0)
	noop.RecordSystemEvent("event", nil)
	noop.UpdateBucketMetrics("bucket", 0, 0)
	noop.RecordBucketOperation("create", "bucket", true)
	noop.RecordObjectLockOperation("set", "bucket", true)
	noop.UpdateRetentionMetrics("bucket", 0, 0)
	noop.RecordBackgroundTask("task", 0, true)
	noop.UpdateCacheMetrics(0, 0)

	assert.NotNil(t, noop.GetMetricsHandler())
	assert.True(t, noop.IsHealthy())
	assert.NoError(t, noop.Reset())
	assert.NoError(t, noop.Start(context.Background()))
	assert.NoError(t, noop.Stop())

	_, err := noop.GetMetricsSnapshot()
	assert.Error(t, err)

	snapshot, err := noop.GetS3MetricsSnapshot()
	assert.NoError(t, err)
	assert.NotNil(t, snapshot)

	_, err = noop.GetHistoricalMetrics("system", time.Now(), time.Now())
	assert.Error(t, err)

	_, err = noop.GetHistoryStats()
	assert.Error(t, err)

	middleware := noop.Middleware()
	assert.NotNil(t, middleware)
}
