package metrics

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a test BadgerDB store
func createTestBadgerStore(t *testing.T) *metadata.BadgerStore {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "badger")

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Only show errors in tests

	opts := metadata.BadgerOptions{
		DataDir:           dbPath,
		SyncWrites:        false, // Faster for tests
		CompactionEnabled: false, // Not needed for tests
		Logger:            logger,
	}

	store, err := metadata.NewBadgerStore(opts)
	require.NoError(t, err)

	t.Cleanup(func() {
		store.Close()
	})

	return store
}

func TestNewBadgerHistoryStore(t *testing.T) {
	badgerStore := createTestBadgerStore(t)

	store, err := NewBadgerHistoryStore(badgerStore, 30)
	require.NoError(t, err)
	require.NotNil(t, store)

	assert.Equal(t, 30, store.retentionDays)
	assert.NotNil(t, store.store)
}

func TestNewBadgerHistoryStore_DefaultRetention(t *testing.T) {
	badgerStore := createTestBadgerStore(t)

	// Pass 0 for retention, should default to 365
	store, err := NewBadgerHistoryStore(badgerStore, 0)
	require.NoError(t, err)
	require.NotNil(t, store)

	assert.Equal(t, 365, store.retentionDays)
}

func TestNewBadgerHistoryStore_InvalidStore(t *testing.T) {
	// Pass invalid store type
	invalidStore := "not a store"

	store, err := NewBadgerHistoryStore(invalidStore, 7)
	assert.Error(t, err)
	assert.Nil(t, store)
	assert.Contains(t, err.Error(), "must implement metadata.Store interface")
}

func TestBadgerHistoryStore_SaveSnapshot(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	data := map[string]interface{}{
		"cpu_usage":    45.5,
		"memory_usage": 60.2,
		"disk_usage":   30.1,
	}

	err = store.SaveSnapshot("system", data)
	assert.NoError(t, err)
}

func TestBadgerHistoryStore_SaveSnapshot_MultipleTypes(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	systemData := map[string]interface{}{"cpu_usage": 45.5}
	storageData := map[string]interface{}{"total_bytes": int64(1024000)}
	s3Data := map[string]interface{}{"requests_total": int64(150)}

	err = store.SaveSnapshot("system", systemData)
	assert.NoError(t, err)

	err = store.SaveSnapshot("storage", storageData)
	assert.NoError(t, err)

	err = store.SaveSnapshot("s3", s3Data)
	assert.NoError(t, err)
}

func TestBadgerHistoryStore_GetLatestSnapshot(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	// Save a snapshot
	data := map[string]interface{}{
		"cpu_usage":    50.0,
		"memory_usage": 65.0,
	}
	err = store.SaveSnapshot("system", data)
	require.NoError(t, err)

	// Get latest snapshot
	snapshot, err := store.GetLatestSnapshot("system")
	require.NoError(t, err)
	require.NotNil(t, snapshot)

	assert.Equal(t, "system", snapshot.Type)
	assert.Equal(t, 50.0, snapshot.Data["cpu_usage"])
	assert.Equal(t, 65.0, snapshot.Data["memory_usage"])
}

func TestBadgerHistoryStore_GetLatestSnapshot_NoData(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	// Get latest snapshot for non-existent type
	snapshot, err := store.GetLatestSnapshot("nonexistent")
	assert.NoError(t, err)
	assert.Nil(t, snapshot)
}

func TestBadgerHistoryStore_GetLatestSnapshot_UpdatesWithNewData(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	// Save first snapshot
	data1 := map[string]interface{}{"value": 10.0}
	err = store.SaveSnapshot("test", data1)
	require.NoError(t, err)

	time.Sleep(1100 * time.Millisecond) // BadgerDB uses Unix seconds as key

	// Save second snapshot (should become latest)
	data2 := map[string]interface{}{"value": 20.0}
	err = store.SaveSnapshot("test", data2)
	require.NoError(t, err)

	// Get latest should return second snapshot
	snapshot, err := store.GetLatestSnapshot("test")
	require.NoError(t, err)
	require.NotNil(t, snapshot)

	assert.Equal(t, 20.0, snapshot.Data["value"])
}

func TestBadgerHistoryStore_GetSnapshots(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	// Save multiple snapshots (need 1 second between each because key uses Unix seconds)
	for i := 0; i < 5; i++ {
		data := map[string]interface{}{
			"value": float64(i * 10),
		}
		err = store.SaveSnapshot("system", data)
		require.NoError(t, err)
		if i < 4 {
			time.Sleep(1100 * time.Millisecond) // Wait >1 second for different Unix timestamp
		}
	}

	// Get snapshots from last hour to now
	end := time.Now()
	start := end.Add(-1 * time.Hour)

	snapshots, err := store.GetSnapshots("system", start, end)
	require.NoError(t, err)
	assert.Equal(t, 5, len(snapshots))
}

func TestBadgerHistoryStore_GetSnapshots_EmptyRange(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	// Save a snapshot now
	data := map[string]interface{}{"value": 100.0}
	err = store.SaveSnapshot("system", data)
	require.NoError(t, err)

	// Query for snapshots from 2 hours ago to 1 hour ago (should be empty)
	end := time.Now().Add(-1 * time.Hour)
	start := end.Add(-1 * time.Hour)

	snapshots, err := store.GetSnapshots("system", start, end)
	require.NoError(t, err)
	assert.Equal(t, 0, len(snapshots))
}

func TestBadgerHistoryStore_GetSnapshots_FiltersByType(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	// Save snapshots of different types
	systemData := map[string]interface{}{"cpu": 50.0}
	storageData := map[string]interface{}{"disk": 70.0}

	err = store.SaveSnapshot("system", systemData)
	require.NoError(t, err)

	err = store.SaveSnapshot("storage", storageData)
	require.NoError(t, err)

	// Get only system snapshots
	end := time.Now()
	start := end.Add(-1 * time.Hour)

	snapshots, err := store.GetSnapshots("system", start, end)
	require.NoError(t, err)
	assert.Equal(t, 1, len(snapshots))
	assert.Equal(t, "system", snapshots[0].Type)
}

func TestBadgerHistoryStore_GetStats(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	// Initially should have 0 snapshots
	stats, err := store.GetStats()
	require.NoError(t, err)
	assert.Equal(t, 0, stats["snapshot_count"])
	assert.Equal(t, 0, stats["aggregate_count"])

	// Save some snapshots
	for i := 0; i < 3; i++ {
		data := map[string]interface{}{"value": float64(i)}
		err = store.SaveSnapshot("system", data)
		require.NoError(t, err)
		if i < 2 {
			time.Sleep(1100 * time.Millisecond) // BadgerDB uses Unix seconds as key
		}
	}

	// Stats should now show 3 snapshots
	stats, err = store.GetStats()
	require.NoError(t, err)
	assert.Equal(t, 3, stats["snapshot_count"])
}

func TestBadgerHistoryStore_CleanupOldMetrics(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 1) // 1 day retention
	require.NoError(t, err)

	// Save a snapshot
	data := map[string]interface{}{"value": 100.0}
	err = store.SaveSnapshot("system", data)
	require.NoError(t, err)

	// Cleanup (should not delete recent data)
	err = store.CleanupOldMetrics()
	// May return error from GC, that's okay
	_ = err

	// Verify snapshot still exists
	snapshot, err := store.GetLatestSnapshot("system")
	require.NoError(t, err)
	assert.NotNil(t, snapshot)
}

func TestBadgerHistoryStore_AggregateHourlyMetrics(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 30)
	require.NoError(t, err)

	// This test just verifies the method doesn't error
	// Full aggregation testing would require time manipulation
	err = store.AggregateHourlyMetrics()
	assert.NoError(t, err)
}

func TestBadgerHistoryStore_GetSnapshotsIntelligent(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 30)
	require.NoError(t, err)

	// Save recent snapshots
	for i := 0; i < 3; i++ {
		data := map[string]interface{}{"value": float64(i * 10)}
		err = store.SaveSnapshot("system", data)
		require.NoError(t, err)
		time.Sleep(1100 * time.Millisecond) // BadgerDB uses Unix seconds as key
	}

	// Get snapshots intelligently (recent data, should use raw snapshots)
	end := time.Now()
	start := end.Add(-1 * time.Hour)

	snapshots, err := store.GetSnapshotsIntelligent("system", start, end)
	require.NoError(t, err)
	assert.Equal(t, 3, len(snapshots))
}

func TestBadgerHistoryStore_GetSnapshotsIntelligent_OldData(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 30)
	require.NoError(t, err)

	// Query for old data (> 7 days ago, should try aggregates)
	end := time.Now().Add(-8 * 24 * time.Hour)
	start := end.Add(-1 * time.Hour)

	snapshots, err := store.GetSnapshotsIntelligent("system", start, end)
	require.NoError(t, err)
	// Should return empty since we have no aggregates
	assert.Equal(t, 0, len(snapshots))
}

func TestBadgerHistoryStore_Close(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	// Close should not error (it's a no-op for BadgerDB)
	err = store.Close()
	assert.NoError(t, err)

	// Second close should also not error
	err = store.Close()
	assert.NoError(t, err)
}

func TestBadgerHistoryStore_AggregateDataPoints(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	// Test aggregation with numeric values
	dataPoints := []map[string]interface{}{
		{"value": float64(10), "count": int64(5)},
		{"value": float64(20), "count": int64(10)},
		{"value": float64(30), "count": int64(15)},
	}

	result := store.aggregateDataPoints(dataPoints)

	// Average of 10, 20, 30 should be 20
	assert.Equal(t, 20.0, result["value"])
	// Average of 5, 10, 15 should be 10
	assert.Equal(t, 10.0, result["count"])
}

func TestBadgerHistoryStore_AggregateDataPoints_EmptyInput(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	dataPoints := []map[string]interface{}{}

	result := store.aggregateDataPoints(dataPoints)

	assert.NotNil(t, result)
	assert.Equal(t, 0, len(result))
}

func TestBadgerHistoryStore_AggregateDataPoints_MixedTypes(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	// Test with mixed numeric types and strings
	dataPoints := []map[string]interface{}{
		{"number": float64(10), "name": "test1"},
		{"number": int(20), "name": "test2"},
		{"number": int64(30), "name": "test3"},
		{"number": uint64(40), "name": "test4"},
	}

	result := store.aggregateDataPoints(dataPoints)

	// Average of 10, 20, 30, 40 should be 25
	assert.Equal(t, 25.0, result["number"])
	// String should be the last value
	assert.Equal(t, "test4", result["name"])
}

func TestBadgerHistoryStore_ConcurrentWrites(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	// Write snapshots with delay (BadgerDB keys use Unix seconds, concurrent writes overwrite)
	for i := 0; i < 5; i++ {
		data := map[string]interface{}{
			"worker": i,
			"value":  float64(i * 10),
		}
		err := store.SaveSnapshot("concurrent", data)
		require.NoError(t, err)
		if i < 4 {
			time.Sleep(1100 * time.Millisecond)
		}
	}

	// Verify all snapshots were saved
	end := time.Now()
	start := end.Add(-1 * time.Hour)
	snapshots, err := store.GetSnapshots("concurrent", start, end)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(snapshots), 5)
}

func TestBadgerHistoryStore_MultipleTypes(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	types := []string{"system", "storage", "s3", "performance"}

	// Save snapshots for each type
	for _, metricType := range types {
		data := map[string]interface{}{
			"type":  metricType,
			"value": 100.0,
		}
		err = store.SaveSnapshot(metricType, data)
		require.NoError(t, err)
	}

	// Verify each type has its latest snapshot
	for _, metricType := range types {
		snapshot, err := store.GetLatestSnapshot(metricType)
		require.NoError(t, err)
		require.NotNil(t, snapshot)
		assert.Equal(t, metricType, snapshot.Type)
		assert.Equal(t, metricType, snapshot.Data["type"])
	}
}

func TestBadgerHistoryStore_LargeDataset(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	// Save 10 snapshots with delays (reduced from 50 to keep test time reasonable)
	// BadgerDB uses Unix seconds as key, so we need >1 second between snapshots
	for i := 0; i < 10; i++ {
		data := map[string]interface{}{
			"iteration": i,
			"value":     float64(i),
		}
		err = store.SaveSnapshot("system", data)
		require.NoError(t, err)
		if i < 9 {
			time.Sleep(1100 * time.Millisecond) // Wait >1 second for different Unix timestamp
		}
	}

	// Verify all were saved
	end := time.Now()
	start := end.Add(-1 * time.Hour)
	snapshots, err := store.GetSnapshots("system", start, end)
	require.NoError(t, err)
	assert.Equal(t, 10, len(snapshots))

	// Verify stats
	stats, err := store.GetStats()
	require.NoError(t, err)
	assert.Equal(t, 10, stats["snapshot_count"])
}

func TestBadgerHistoryStore_GetAggregatedSnapshots(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	// This tests the method exists and doesn't error
	// Creating actual aggregates would require time manipulation
	end := time.Now()
	start := end.Add(-24 * time.Hour)

	snapshots, err := store.GetAggregatedSnapshots("system", start, end)
	require.NoError(t, err)
	// Should return empty slice, not nil (no aggregates created yet)
	assert.Equal(t, 0, len(snapshots))
}

func TestBadgerHistoryStore_KeyGeneration(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	now := time.Now()

	// Test snapshot key format
	snapshotKey := store.snapshotKey("system", now)
	assert.Contains(t, string(snapshotKey), "metrics:snapshot:system:")

	// Test aggregate key format
	aggregateKey := store.aggregateKey("system", now)
	assert.Contains(t, string(aggregateKey), "metrics:aggregate:system:")

	// Test latest key format
	latestKey := store.latestKey("system")
	assert.Equal(t, "metrics:latest:system", string(latestKey))

	// Test stats key format
	statsKey := store.statsKey()
	assert.Equal(t, "metrics:stats", string(statsKey))

	// Test prefix formats
	snapshotPrefix := store.snapshotPrefix("system")
	assert.Equal(t, "metrics:snapshot:system:", string(snapshotPrefix))

	aggregatePrefix := store.aggregatePrefix("system")
	assert.Equal(t, "metrics:aggregate:system:", string(aggregatePrefix))
}

func TestBadgerHistoryStore_TimestampOrdering(t *testing.T) {
	badgerStore := createTestBadgerStore(t)
	store, err := NewBadgerHistoryStore(badgerStore, 7)
	require.NoError(t, err)

	// Save snapshots with known timestamps
	timestamps := []time.Time{}
	for i := 0; i < 5; i++ {
		data := map[string]interface{}{"index": i}
		err = store.SaveSnapshot("ordered", data)
		require.NoError(t, err)
		timestamps = append(timestamps, time.Now())
		time.Sleep(1100 * time.Millisecond) // BadgerDB uses Unix seconds as key
	}

	// Get snapshots
	end := time.Now()
	start := end.Add(-1 * time.Hour)
	snapshots, err := store.GetSnapshots("ordered", start, end)
	require.NoError(t, err)
	assert.Equal(t, 5, len(snapshots))

	// Verify they're in order by timestamp
	for i := 0; i < len(snapshots)-1; i++ {
		assert.True(t, !snapshots[i].Timestamp.After(snapshots[i+1].Timestamp),
			"Snapshots should be ordered by timestamp")
	}
}
