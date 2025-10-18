package metrics

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
	_ "modernc.org/sqlite"
)

// MetricSnapshot represents a point-in-time snapshot of all metrics
type MetricSnapshot struct {
	ID        int64                  `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	Type      string                 `json:"type"` // "system", "storage", "s3", "performance"
	Data      map[string]interface{} `json:"data"`
}

// HistoryStore manages historical metrics storage
type HistoryStore struct {
	db            *sql.DB
	dataDir       string
	retentionDays int
}

// NewHistoryStore creates a new history store
func NewHistoryStore(dataDir string, retentionDays int) (*HistoryStore, error) {
	if retentionDays == 0 {
		retentionDays = 365 // Default: 1 year
	}

	// Create metrics database
	dbPath := filepath.Join(dataDir, "db", "maxiofs.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open metrics database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	store := &HistoryStore{
		db:            db,
		dataDir:       dataDir,
		retentionDays: retentionDays,
	}

	// Initialize schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return store, nil
}

// initSchema creates the database schema
func (h *HistoryStore) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS metric_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		type TEXT NOT NULL,
		data TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_metric_snapshots_timestamp ON metric_snapshots(timestamp);
	CREATE INDEX IF NOT EXISTS idx_metric_snapshots_type ON metric_snapshots(type);
	CREATE INDEX IF NOT EXISTS idx_metric_snapshots_type_timestamp ON metric_snapshots(type, timestamp);

	-- Aggregated metrics table for long-term storage (hourly aggregates)
	CREATE TABLE IF NOT EXISTS metric_aggregates (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		hour DATETIME NOT NULL,
		type TEXT NOT NULL,
		data TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_metric_aggregates_hour ON metric_aggregates(hour);
	CREATE INDEX IF NOT EXISTS idx_metric_aggregates_type ON metric_aggregates(type);
	CREATE INDEX IF NOT EXISTS idx_metric_aggregates_type_hour ON metric_aggregates(type, hour);
	`

	_, err := h.db.Exec(schema)
	return err
}

// SaveSnapshot saves a metric snapshot
func (h *HistoryStore) SaveSnapshot(metricType string, data map[string]interface{}) error {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	_, err = h.db.Exec(
		"INSERT INTO metric_snapshots (timestamp, type, data) VALUES (?, ?, ?)",
		time.Now(),
		metricType,
		string(dataJSON),
	)

	return err
}

// GetSnapshots retrieves snapshots within a time range
func (h *HistoryStore) GetSnapshots(metricType string, start, end time.Time) ([]MetricSnapshot, error) {
	rows, err := h.db.Query(
		`SELECT id, timestamp, type, data FROM metric_snapshots
		 WHERE type = ? AND timestamp >= ? AND timestamp <= ?
		 ORDER BY timestamp ASC`,
		metricType,
		start,
		end,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []MetricSnapshot
	for rows.Next() {
		var snapshot MetricSnapshot
		var dataJSON string

		if err := rows.Scan(&snapshot.ID, &snapshot.Timestamp, &snapshot.Type, &dataJSON); err != nil {
			logrus.WithError(err).Error("Failed to scan snapshot row")
			continue
		}

		if err := json.Unmarshal([]byte(dataJSON), &snapshot.Data); err != nil {
			logrus.WithError(err).Error("Failed to unmarshal snapshot data")
			continue
		}

		snapshots = append(snapshots, snapshot)
	}

	return snapshots, rows.Err()
}

// GetLatestSnapshot gets the most recent snapshot for a metric type
func (h *HistoryStore) GetLatestSnapshot(metricType string) (*MetricSnapshot, error) {
	var snapshot MetricSnapshot
	var dataJSON string

	err := h.db.QueryRow(
		`SELECT id, timestamp, type, data FROM metric_snapshots
		 WHERE type = ?
		 ORDER BY timestamp DESC
		 LIMIT 1`,
		metricType,
	).Scan(&snapshot.ID, &snapshot.Timestamp, &snapshot.Type, &dataJSON)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if err := json.Unmarshal([]byte(dataJSON), &snapshot.Data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot data: %w", err)
	}

	return &snapshot, nil
}

// AggregateHourlyMetrics aggregates snapshots into hourly summaries
func (h *HistoryStore) AggregateHourlyMetrics() error {
	// Find the oldest non-aggregated snapshot older than 7 days
	cutoffTime := time.Now().Add(-7 * 24 * time.Hour)

	// Get all snapshot types
	types := []string{"system", "storage", "s3", "performance"}

	for _, metricType := range types {
		// Get snapshots in 1-hour buckets
		rows, err := h.db.Query(
			`SELECT
				datetime(timestamp, 'start of hour') as hour,
				data
			FROM metric_snapshots
			WHERE type = ? AND timestamp < ?
			ORDER BY hour`,
			metricType,
			cutoffTime,
		)
		if err != nil {
			logrus.WithError(err).Errorf("Failed to query snapshots for aggregation: %s", metricType)
			continue
		}

		// Group by hour and calculate averages
		hourlyData := make(map[string][]map[string]interface{})
		for rows.Next() {
			var hour string
			var dataJSON string

			if err := rows.Scan(&hour, &dataJSON); err != nil {
				logrus.WithError(err).Error("Failed to scan row for aggregation")
				continue
			}

			var data map[string]interface{}
			if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
				logrus.WithError(err).Error("Failed to unmarshal data for aggregation")
				continue
			}

			hourlyData[hour] = append(hourlyData[hour], data)
		}
		rows.Close()

		// Save aggregates and delete raw snapshots
		for hour, dataPoints := range hourlyData {
			// Calculate averages for numeric fields
			aggregated := h.aggregateDataPoints(dataPoints)

			// Save aggregate
			aggregatedJSON, err := json.Marshal(aggregated)
			if err != nil {
				logrus.WithError(err).Error("Failed to marshal aggregated data")
				continue
			}

			hourTime, _ := time.Parse("2006-01-02 15:04:05", hour)
			_, err = h.db.Exec(
				"INSERT OR REPLACE INTO metric_aggregates (hour, type, data) VALUES (?, ?, ?)",
				hourTime,
				metricType,
				string(aggregatedJSON),
			)
			if err != nil {
				logrus.WithError(err).Error("Failed to insert aggregate")
			}
		}

		// Delete old snapshots that have been aggregated
		_, err = h.db.Exec(
			"DELETE FROM metric_snapshots WHERE type = ? AND timestamp < ?",
			metricType,
			cutoffTime,
		)
		if err != nil {
			logrus.WithError(err).Error("Failed to delete old snapshots")
		}
	}

	return nil
}

// aggregateDataPoints calculates average values for numeric fields
func (h *HistoryStore) aggregateDataPoints(dataPoints []map[string]interface{}) map[string]interface{} {
	if len(dataPoints) == 0 {
		return map[string]interface{}{}
	}

	result := make(map[string]interface{})
	counts := make(map[string]int)
	sums := make(map[string]float64)

	// Sum all numeric values
	for _, dp := range dataPoints {
		for key, value := range dp {
			switch v := value.(type) {
			case float64:
				sums[key] += v
				counts[key]++
			case int:
				sums[key] += float64(v)
				counts[key]++
			case int64:
				sums[key] += float64(v)
				counts[key]++
			case uint64:
				sums[key] += float64(v)
				counts[key]++
			default:
				// For non-numeric values, use the last value
				result[key] = value
			}
		}
	}

	// Calculate averages
	for key, sum := range sums {
		if counts[key] > 0 {
			result[key] = sum / float64(counts[key])
		}
	}

	return result
}

// GetAggregatedSnapshots retrieves aggregated hourly snapshots
func (h *HistoryStore) GetAggregatedSnapshots(metricType string, start, end time.Time) ([]MetricSnapshot, error) {
	rows, err := h.db.Query(
		`SELECT id, hour as timestamp, type, data FROM metric_aggregates
		 WHERE type = ? AND hour >= ? AND hour <= ?
		 ORDER BY hour ASC`,
		metricType,
		start,
		end,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []MetricSnapshot
	for rows.Next() {
		var snapshot MetricSnapshot
		var dataJSON string

		if err := rows.Scan(&snapshot.ID, &snapshot.Timestamp, &snapshot.Type, &dataJSON); err != nil {
			logrus.WithError(err).Error("Failed to scan aggregate row")
			continue
		}

		if err := json.Unmarshal([]byte(dataJSON), &snapshot.Data); err != nil {
			logrus.WithError(err).Error("Failed to unmarshal aggregate data")
			continue
		}

		snapshots = append(snapshots, snapshot)
	}

	return snapshots, rows.Err()
}

// CleanupOldMetrics removes metrics older than retention period
func (h *HistoryStore) CleanupOldMetrics() error {
	cutoffTime := time.Now().Add(-time.Duration(h.retentionDays) * 24 * time.Hour)

	// Delete old snapshots
	_, err := h.db.Exec("DELETE FROM metric_snapshots WHERE timestamp < ?", cutoffTime)
	if err != nil {
		return fmt.Errorf("failed to delete old snapshots: %w", err)
	}

	// Delete old aggregates
	_, err = h.db.Exec("DELETE FROM metric_aggregates WHERE hour < ?", cutoffTime)
	if err != nil {
		return fmt.Errorf("failed to delete old aggregates: %w", err)
	}

	// Run VACUUM to reclaim space
	_, err = h.db.Exec("VACUUM")
	if err != nil {
		logrus.WithError(err).Warn("Failed to vacuum database")
	}

	return nil
}

// GetSnapshotsIntelligent retrieves snapshots, using aggregates for older data
func (h *HistoryStore) GetSnapshotsIntelligent(metricType string, start, end time.Time) ([]MetricSnapshot, error) {
	sevenDaysAgo := time.Now().Add(-7 * 24 * time.Hour)

	var allSnapshots []MetricSnapshot

	// If requesting data older than 7 days, use aggregates
	if start.Before(sevenDaysAgo) {
		aggregateEnd := end
		if end.After(sevenDaysAgo) {
			aggregateEnd = sevenDaysAgo
		}

		aggregates, err := h.GetAggregatedSnapshots(metricType, start, aggregateEnd)
		if err != nil {
			logrus.WithError(err).Error("Failed to get aggregated snapshots")
		} else {
			allSnapshots = append(allSnapshots, aggregates...)
		}
	}

	// If requesting recent data (last 7 days), use raw snapshots
	if end.After(sevenDaysAgo) {
		recentStart := start
		if start.Before(sevenDaysAgo) {
			recentStart = sevenDaysAgo
		}

		recent, err := h.GetSnapshots(metricType, recentStart, end)
		if err != nil {
			logrus.WithError(err).Error("Failed to get recent snapshots")
		} else {
			allSnapshots = append(allSnapshots, recent...)
		}
	}

	return allSnapshots, nil
}

// Close closes the database connection
func (h *HistoryStore) Close() error {
	return h.db.Close()
}

// GetStats returns statistics about the metrics history
func (h *HistoryStore) GetStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Count snapshots
	var snapshotCount int
	err := h.db.QueryRow("SELECT COUNT(*) FROM metric_snapshots").Scan(&snapshotCount)
	if err != nil {
		return nil, err
	}
	stats["snapshot_count"] = snapshotCount

	// Count aggregates
	var aggregateCount int
	err = h.db.QueryRow("SELECT COUNT(*) FROM metric_aggregates").Scan(&aggregateCount)
	if err != nil {
		return nil, err
	}
	stats["aggregate_count"] = aggregateCount

	// Get oldest snapshot
	var oldestSnapshot time.Time
	err = h.db.QueryRow("SELECT MIN(timestamp) FROM metric_snapshots").Scan(&oldestSnapshot)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	stats["oldest_snapshot"] = oldestSnapshot

	// Get newest snapshot
	var newestSnapshot time.Time
	err = h.db.QueryRow("SELECT MAX(timestamp) FROM metric_snapshots").Scan(&newestSnapshot)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	stats["newest_snapshot"] = newestSnapshot

	return stats, nil
}
