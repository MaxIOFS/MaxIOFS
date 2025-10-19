package metrics

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
)

// BadgerHistoryStore manages historical metrics storage using BadgerDB
type BadgerHistoryStore struct {
	store         metadata.Store
	retentionDays int
}

// NewBadgerHistoryStore creates a new BadgerDB-based history store
func NewBadgerHistoryStore(store interface{}, retentionDays int) (*BadgerHistoryStore, error) {
	if retentionDays == 0 {
		retentionDays = 365 // Default: 1 year
	}

	// Cast to metadata.Store
	mdStore, ok := store.(metadata.Store)
	if !ok {
		logrus.WithField("store_type", fmt.Sprintf("%T", store)).
			Error("Failed to cast store to metadata.Store interface")
		return nil, fmt.Errorf("store must implement metadata.Store interface, got %T", store)
	}

	logrus.Info("Successfully created BadgerDB history store")
	return &BadgerHistoryStore{
		store:         mdStore,
		retentionDays: retentionDays,
	}, nil
}

// Key formats for BadgerDB:
// - Snapshots: "metrics:snapshot:{type}:{unix_timestamp}"
// - Aggregates: "metrics:aggregate:{type}:{hour_unix_timestamp}"
// - Latest: "metrics:latest:{type}"
// - Stats: "metrics:stats"

func (b *BadgerHistoryStore) snapshotKey(metricType string, timestamp time.Time) []byte {
	return []byte(fmt.Sprintf("metrics:snapshot:%s:%d", metricType, timestamp.Unix()))
}

func (b *BadgerHistoryStore) aggregateKey(metricType string, hour time.Time) []byte {
	// Round to hour
	hourTimestamp := hour.Truncate(time.Hour).Unix()
	return []byte(fmt.Sprintf("metrics:aggregate:%s:%d", metricType, hourTimestamp))
}

func (b *BadgerHistoryStore) latestKey(metricType string) []byte {
	return []byte(fmt.Sprintf("metrics:latest:%s", metricType))
}

func (b *BadgerHistoryStore) snapshotPrefix(metricType string) []byte {
	return []byte(fmt.Sprintf("metrics:snapshot:%s:", metricType))
}

func (b *BadgerHistoryStore) aggregatePrefix(metricType string) []byte {
	return []byte(fmt.Sprintf("metrics:aggregate:%s:", metricType))
}

func (b *BadgerHistoryStore) statsKey() []byte {
	return []byte("metrics:stats")
}

// SaveSnapshot saves a metric snapshot
func (b *BadgerHistoryStore) SaveSnapshot(metricType string, data map[string]interface{}) error {
	now := time.Now()

	snapshot := MetricSnapshot{
		Timestamp: now,
		Type:      metricType,
		Data:      data,
	}

	dataJSON, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Use the underlying BadgerDB directly
	badgerStore, ok := b.store.(*metadata.BadgerStore)
	if !ok {
		logrus.Error("BadgerHistoryStore: store is not a BadgerStore")
		return fmt.Errorf("store is not a BadgerStore")
	}

	err = badgerStore.DB().Update(func(txn *badger.Txn) error {
		// Save snapshot with timestamp key
		key := b.snapshotKey(metricType, now)
		if err := txn.Set(key, dataJSON); err != nil {
			return err
		}

		// Update latest snapshot for quick access
		latestKey := b.latestKey(metricType)
		if err := txn.Set(latestKey, dataJSON); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}

	return nil
}

// GetSnapshots retrieves snapshots within a time range
func (b *BadgerHistoryStore) GetSnapshots(metricType string, start, end time.Time) ([]MetricSnapshot, error) {
	badgerStore, ok := b.store.(*metadata.BadgerStore)
	if !ok {
		return nil, fmt.Errorf("store is not a BadgerStore")
	}

	var snapshots []MetricSnapshot

	err := badgerStore.DB().View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = b.snapshotPrefix(metricType)
		it := txn.NewIterator(opts)
		defer it.Close()

		startKey := b.snapshotKey(metricType, start)
		endKey := b.snapshotKey(metricType, end)

		for it.Seek(startKey); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()

			// Stop if we've passed the end time
			if string(key) > string(endKey) {
				break
			}

			err := item.Value(func(val []byte) error {
				var snapshot MetricSnapshot
				if err := json.Unmarshal(val, &snapshot); err != nil {
					logrus.WithError(err).Error("Failed to unmarshal snapshot")
					return nil // Continue to next item
				}

				snapshots = append(snapshots, snapshot)
				return nil
			})

			if err != nil {
				return err
			}
		}

		return nil
	})

	return snapshots, err
}

// GetLatestSnapshot gets the most recent snapshot for a metric type
func (b *BadgerHistoryStore) GetLatestSnapshot(metricType string) (*MetricSnapshot, error) {
	badgerStore, ok := b.store.(*metadata.BadgerStore)
	if !ok {
		return nil, fmt.Errorf("store is not a BadgerStore")
	}

	var snapshot *MetricSnapshot

	err := badgerStore.DB().View(func(txn *badger.Txn) error {
		item, err := txn.Get(b.latestKey(metricType))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil // No snapshot found
			}
			return err
		}

		return item.Value(func(val []byte) error {
			snapshot = &MetricSnapshot{}
			return json.Unmarshal(val, snapshot)
		})
	})

	if err != nil {
		return nil, err
	}

	return snapshot, nil
}

// AggregateHourlyMetrics aggregates snapshots into hourly summaries
func (b *BadgerHistoryStore) AggregateHourlyMetrics() error {
	// Find snapshots older than 7 days
	cutoffTime := time.Now().Add(-7 * 24 * time.Hour)
	types := []string{"system", "storage", "s3", "performance"}

	badgerStore, ok := b.store.(*metadata.BadgerStore)
	if !ok {
		return fmt.Errorf("store is not a BadgerStore")
	}

	for _, metricType := range types {
		// Collect snapshots by hour
		hourlyData := make(map[int64][]MetricSnapshot)

		err := badgerStore.DB().View(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			opts.Prefix = b.snapshotPrefix(metricType)
			it := txn.NewIterator(opts)
			defer it.Close()

			for it.Rewind(); it.Valid(); it.Next() {
				item := it.Item()

				var snapshot MetricSnapshot
				err := item.Value(func(val []byte) error {
					return json.Unmarshal(val, &snapshot)
				})

				if err != nil {
					logrus.WithError(err).Error("Failed to unmarshal snapshot")
					continue
				}

				// Only aggregate old snapshots
				if snapshot.Timestamp.After(cutoffTime) {
					continue
				}

				// Group by hour
				hourTimestamp := snapshot.Timestamp.Truncate(time.Hour).Unix()
				hourlyData[hourTimestamp] = append(hourlyData[hourTimestamp], snapshot)
			}

			return nil
		})

		if err != nil {
			logrus.WithError(err).Errorf("Failed to read snapshots for aggregation: %s", metricType)
			continue
		}

		// Save aggregates and delete old snapshots
		err = badgerStore.DB().Update(func(txn *badger.Txn) error {
			for hourTimestamp, snapshots := range hourlyData {
				if len(snapshots) == 0 {
					continue
				}

				// Calculate aggregates
				dataPoints := make([]map[string]interface{}, len(snapshots))
				for i, s := range snapshots {
					dataPoints[i] = s.Data
				}
				aggregated := b.aggregateDataPoints(dataPoints)

				// Save aggregate
				aggregate := MetricSnapshot{
					Timestamp: time.Unix(hourTimestamp, 0),
					Type:      metricType,
					Data:      aggregated,
				}

				aggregateJSON, err := json.Marshal(aggregate)
				if err != nil {
					logrus.WithError(err).Error("Failed to marshal aggregate")
					continue
				}

				aggKey := b.aggregateKey(metricType, time.Unix(hourTimestamp, 0))
				if err := txn.Set(aggKey, aggregateJSON); err != nil {
					logrus.WithError(err).Error("Failed to save aggregate")
					continue
				}

				// Delete old snapshots for this hour
				for _, snapshot := range snapshots {
					snapKey := b.snapshotKey(metricType, snapshot.Timestamp)
					if err := txn.Delete(snapKey); err != nil {
						logrus.WithError(err).Warn("Failed to delete old snapshot")
					}
				}
			}

			return nil
		})

		if err != nil {
			logrus.WithError(err).Error("Failed to save aggregates")
		}
	}

	return nil
}

// aggregateDataPoints calculates average values for numeric fields
func (b *BadgerHistoryStore) aggregateDataPoints(dataPoints []map[string]interface{}) map[string]interface{} {
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
func (b *BadgerHistoryStore) GetAggregatedSnapshots(metricType string, start, end time.Time) ([]MetricSnapshot, error) {
	badgerStore, ok := b.store.(*metadata.BadgerStore)
	if !ok {
		return nil, fmt.Errorf("store is not a BadgerStore")
	}

	var snapshots []MetricSnapshot

	err := badgerStore.DB().View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = b.aggregatePrefix(metricType)
		it := txn.NewIterator(opts)
		defer it.Close()

		startKey := b.aggregateKey(metricType, start)
		endKey := b.aggregateKey(metricType, end)

		for it.Seek(startKey); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()

			// Stop if we've passed the end time
			if string(key) > string(endKey) {
				break
			}

			err := item.Value(func(val []byte) error {
				var snapshot MetricSnapshot
				if err := json.Unmarshal(val, &snapshot); err != nil {
					logrus.WithError(err).Error("Failed to unmarshal aggregate")
					return nil
				}

				snapshots = append(snapshots, snapshot)
				return nil
			})

			if err != nil {
				return err
			}
		}

		return nil
	})

	return snapshots, err
}

// CleanupOldMetrics removes metrics older than retention period
func (b *BadgerHistoryStore) CleanupOldMetrics() error {
	cutoffTime := time.Now().Add(-time.Duration(b.retentionDays) * 24 * time.Hour)
	types := []string{"system", "storage", "s3", "performance"}

	badgerStore, ok := b.store.(*metadata.BadgerStore)
	if !ok {
		return fmt.Errorf("store is not a BadgerStore")
	}

	for _, metricType := range types {
		// Delete old snapshots
		err := badgerStore.DB().Update(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			opts.Prefix = b.snapshotPrefix(metricType)
			it := txn.NewIterator(opts)
			defer it.Close()

			cutoffKey := b.snapshotKey(metricType, cutoffTime)

			for it.Rewind(); it.Valid(); it.Next() {
				item := it.Item()
				key := item.Key()

				// Only delete keys older than cutoff
				if string(key) >= string(cutoffKey) {
					break
				}

				if err := txn.Delete(key); err != nil {
					logrus.WithError(err).Warn("Failed to delete old snapshot")
				}
			}

			return nil
		})

		if err != nil {
			logrus.WithError(err).Errorf("Failed to cleanup snapshots for %s", metricType)
		}

		// Delete old aggregates
		err = badgerStore.DB().Update(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			opts.Prefix = b.aggregatePrefix(metricType)
			it := txn.NewIterator(opts)
			defer it.Close()

			cutoffKey := b.aggregateKey(metricType, cutoffTime)

			for it.Rewind(); it.Valid(); it.Next() {
				item := it.Item()
				key := item.Key()

				// Only delete keys older than cutoff
				if string(key) >= string(cutoffKey) {
					break
				}

				if err := txn.Delete(key); err != nil {
					logrus.WithError(err).Warn("Failed to delete old aggregate")
				}
			}

			return nil
		})

		if err != nil {
			logrus.WithError(err).Errorf("Failed to cleanup aggregates for %s", metricType)
		}
	}

	// Run BadgerDB GC to reclaim space
	return badgerStore.DB().RunValueLogGC(0.5)
}

// GetSnapshotsIntelligent retrieves snapshots, using aggregates for older data
func (b *BadgerHistoryStore) GetSnapshotsIntelligent(metricType string, start, end time.Time) ([]MetricSnapshot, error) {
	sevenDaysAgo := time.Now().Add(-7 * 24 * time.Hour)

	var allSnapshots []MetricSnapshot

	// If requesting data older than 7 days, use aggregates
	if start.Before(sevenDaysAgo) {
		aggregateEnd := end
		if end.After(sevenDaysAgo) {
			aggregateEnd = sevenDaysAgo
		}

		aggregates, err := b.GetAggregatedSnapshots(metricType, start, aggregateEnd)
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

		recent, err := b.GetSnapshots(metricType, recentStart, end)
		if err != nil {
			logrus.WithError(err).Error("Failed to get recent snapshots")
		} else {
			allSnapshots = append(allSnapshots, recent...)
		}
	}

	return allSnapshots, nil
}

// Close closes the store (no-op for BadgerDB as it's managed externally)
func (b *BadgerHistoryStore) Close() error {
	// BadgerDB is managed by the metadata store, don't close it here
	return nil
}

// GetStats returns statistics about the metrics history
func (b *BadgerHistoryStore) GetStats() (map[string]interface{}, error) {
	badgerStore, ok := b.store.(*metadata.BadgerStore)
	if !ok {
		return nil, fmt.Errorf("store is not a BadgerStore")
	}

	stats := make(map[string]interface{})
	types := []string{"system", "storage", "s3", "performance"}

	totalSnapshots := 0
	totalAggregates := 0
	var oldestSnapshot time.Time
	var newestSnapshot time.Time

	err := badgerStore.DB().View(func(txn *badger.Txn) error {
		for _, metricType := range types {
			// Count snapshots
			opts := badger.DefaultIteratorOptions
			opts.Prefix = b.snapshotPrefix(metricType)
			it := txn.NewIterator(opts)

			count := 0
			for it.Rewind(); it.Valid(); it.Next() {
				count++

				// Extract timestamp from key
				key := string(it.Item().Key())
				// Format: "metrics:snapshot:{type}:{timestamp}"
				parts := []byte(key)
				if len(parts) > 0 {
					// Parse timestamp from key
					timestampStr := key[len(b.snapshotPrefix(metricType)):]
					if ts, err := strconv.ParseInt(timestampStr, 10, 64); err == nil {
						t := time.Unix(ts, 0)
						if oldestSnapshot.IsZero() || t.Before(oldestSnapshot) {
							oldestSnapshot = t
						}
						if newestSnapshot.IsZero() || t.After(newestSnapshot) {
							newestSnapshot = t
						}
					}
				}
			}
			it.Close()
			totalSnapshots += count

			// Count aggregates
			opts.Prefix = b.aggregatePrefix(metricType)
			it = txn.NewIterator(opts)
			count = 0
			for it.Rewind(); it.Valid(); it.Next() {
				count++
			}
			it.Close()
			totalAggregates += count
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	stats["snapshot_count"] = totalSnapshots
	stats["aggregate_count"] = totalAggregates
	stats["oldest_snapshot"] = oldestSnapshot
	stats["newest_snapshot"] = newestSnapshot

	return stats, nil
}
