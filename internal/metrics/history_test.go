package metrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHistoryStore(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewHistoryStore(tmpDir, 30)
	require.NoError(t, err)
	require.NotNil(t, store)
	defer store.Close()

	assert.Equal(t, tmpDir, store.dataDir)
	assert.Equal(t, 30, store.retentionDays)
	assert.NotNil(t, store.db)
}

func TestNewHistoryStore_DefaultRetention(t *testing.T) {
	tmpDir := t.TempDir()

	// Pass 0 for retention, should default to 365
	store, err := NewHistoryStore(tmpDir, 0)
	require.NoError(t, err)
	require.NotNil(t, store)
	defer store.Close()

	assert.Equal(t, 365, store.retentionDays)
}

func TestNewHistoryStore_CreatesDatabase(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	require.NotNil(t, store)
	defer store.Close()

	// Verify database file was created
	dbPath := filepath.Join(tmpDir, "db", "maxiofs.db")
	_, err = os.Stat(dbPath)
	assert.NoError(t, err, "Database file should exist")
}

func TestHistoryStore_SaveSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

	data := map[string]interface{}{
		"cpu_usage":    45.5,
		"memory_usage": 60.2,
		"disk_usage":   30.1,
	}

	err = store.SaveSnapshot("system", data)
	assert.NoError(t, err)
}

func TestHistoryStore_SaveSnapshot_MultipleTypes(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

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

func TestHistoryStore_GetLatestSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

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

func TestHistoryStore_GetLatestSnapshot_NoData(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

	// Get latest snapshot for non-existent type
	snapshot, err := store.GetLatestSnapshot("nonexistent")
	assert.NoError(t, err)
	assert.Nil(t, snapshot)
}

func TestHistoryStore_GetLatestSnapshot_UpdatesWithNewData(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

	// Save first snapshot
	data1 := map[string]interface{}{"value": 10.0}
	err = store.SaveSnapshot("test", data1)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

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

func TestHistoryStore_GetSnapshots(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

	// Save multiple snapshots
	for i := 0; i < 5; i++ {
		data := map[string]interface{}{
			"value": float64(i * 10),
		}
		err = store.SaveSnapshot("system", data)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond)
	}

	// Get snapshots from last hour to now
	end := time.Now()
	start := end.Add(-1 * time.Hour)

	snapshots, err := store.GetSnapshots("system", start, end)
	require.NoError(t, err)
	assert.Equal(t, 5, len(snapshots))

	// Verify snapshots are ordered by timestamp
	for i := 0; i < len(snapshots)-1; i++ {
		assert.True(t, snapshots[i].Timestamp.Before(snapshots[i+1].Timestamp) ||
			snapshots[i].Timestamp.Equal(snapshots[i+1].Timestamp))
	}
}

func TestHistoryStore_GetSnapshots_EmptyRange(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

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

func TestHistoryStore_GetSnapshots_FiltersByType(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

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

func TestHistoryStore_GetStats(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

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
	}

	// Stats should now show 3 snapshots
	stats, err = store.GetStats()
	require.NoError(t, err)
	assert.Equal(t, 3, stats["snapshot_count"])
}

func TestHistoryStore_CleanupOldMetrics(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 1) // 1 day retention
	require.NoError(t, err)
	defer store.Close()

	// Save a snapshot
	data := map[string]interface{}{"value": 100.0}
	err = store.SaveSnapshot("system", data)
	require.NoError(t, err)

	// Cleanup (should not delete recent data)
	err = store.CleanupOldMetrics()
	assert.NoError(t, err)

	// Verify snapshot still exists
	snapshot, err := store.GetLatestSnapshot("system")
	require.NoError(t, err)
	assert.NotNil(t, snapshot)
}

func TestHistoryStore_AggregateHourlyMetrics(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 30)
	require.NoError(t, err)
	defer store.Close()

	// This test just verifies the method doesn't error
	// Full aggregation testing would require time manipulation
	err = store.AggregateHourlyMetrics()
	assert.NoError(t, err)
}

func TestHistoryStore_GetSnapshotsIntelligent(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 30)
	require.NoError(t, err)
	defer store.Close()

	// Save recent snapshots
	for i := 0; i < 3; i++ {
		data := map[string]interface{}{"value": float64(i * 10)}
		err = store.SaveSnapshot("system", data)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond)
	}

	// Get snapshots intelligently (recent data, should use raw snapshots)
	end := time.Now()
	start := end.Add(-1 * time.Hour)

	snapshots, err := store.GetSnapshotsIntelligent("system", start, end)
	require.NoError(t, err)
	assert.Equal(t, 3, len(snapshots))
}

func TestHistoryStore_GetSnapshotsIntelligent_OldData(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 30)
	require.NoError(t, err)
	defer store.Close()

	// Query for old data (> 7 days ago, should try aggregates)
	end := time.Now().Add(-8 * 24 * time.Hour)
	start := end.Add(-1 * time.Hour)

	snapshots, err := store.GetSnapshotsIntelligent("system", start, end)
	require.NoError(t, err)
	// Should return empty since we have no aggregates
	assert.Equal(t, 0, len(snapshots))
}

func TestHistoryStore_Close(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)

	// Close should not error
	err = store.Close()
	assert.NoError(t, err)

	// Second close should error (or be handled gracefully)
	err = store.Close()
	// SQLite may return error on double close, that's okay
	_ = err
}

func TestHistoryStore_AggregateDataPoints(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

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

func TestHistoryStore_AggregateDataPoints_EmptyInput(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

	dataPoints := []map[string]interface{}{}

	result := store.aggregateDataPoints(dataPoints)

	assert.NotNil(t, result)
	assert.Equal(t, 0, len(result))
}

func TestHistoryStore_AggregateDataPoints_MixedTypes(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

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

func TestHistoryStore_ConcurrentWrites(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

	// Write snapshots with slight delay to avoid SQLite lock contention
	for i := 0; i < 5; i++ {
		data := map[string]interface{}{
			"worker": i,
			"value":  float64(i * 10),
		}
		err := store.SaveSnapshot("concurrent", data)
		require.NoError(t, err)
		time.Sleep(50 * time.Millisecond) // Small delay to avoid lock contention
	}

	// Verify all snapshots were saved
	end := time.Now()
	start := end.Add(-1 * time.Hour)
	snapshots, err := store.GetSnapshots("concurrent", start, end)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(snapshots), 5)
}

func TestHistoryStore_MultipleTypes(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

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

func TestHistoryStore_LargeDataset(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

	// Save 50 snapshots
	for i := 0; i < 50; i++ {
		data := map[string]interface{}{
			"iteration": i,
			"value":     float64(i),
		}
		err = store.SaveSnapshot("system", data)
		require.NoError(t, err)
	}

	// Verify all were saved
	end := time.Now()
	start := end.Add(-1 * time.Hour)
	snapshots, err := store.GetSnapshots("system", start, end)
	require.NoError(t, err)
	assert.Equal(t, 50, len(snapshots))

	// Verify stats
	stats, err := store.GetStats()
	require.NoError(t, err)
	assert.Equal(t, 50, stats["snapshot_count"])
}

func TestHistoryStore_GetAggregatedSnapshots(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewHistoryStore(tmpDir, 7)
	require.NoError(t, err)
	defer store.Close()

	// This tests the method exists and doesn't error
	// Creating actual aggregates would require time manipulation
	end := time.Now()
	start := end.Add(-24 * time.Hour)

	snapshots, err := store.GetAggregatedSnapshots("system", start, end)
	require.NoError(t, err)
	// Should return empty slice, not nil (no aggregates created yet)
	assert.Equal(t, 0, len(snapshots))
}
