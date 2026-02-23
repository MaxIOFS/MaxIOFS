package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
)

// BadgerHistoryStore manages historical metrics storage.
// Despite its name it now works with any metadata.RawKVStore implementation
// (both BadgerStore and PebbleStore).
type BadgerHistoryStore struct {
	kvStore       metadata.RawKVStore
	retentionDays int
}

// NewBadgerHistoryStore creates a history store backed by any RawKVStore.
func NewBadgerHistoryStore(store interface{}, retentionDays int) (*BadgerHistoryStore, error) {
	if retentionDays == 0 {
		retentionDays = 365
	}

	kvStore, ok := store.(metadata.RawKVStore)
	if !ok {
		logrus.WithField("store_type", fmt.Sprintf("%T", store)).
			Error("Failed to cast store to metadata.RawKVStore")
		return nil, fmt.Errorf("store must implement metadata.RawKVStore, got %T", store)
	}

	logrus.Info("Successfully created metrics history store")
	return &BadgerHistoryStore{
		kvStore:       kvStore,
		retentionDays: retentionDays,
	}, nil
}

// Key formats:
//   - Snapshots:  "metrics:snapshot:{type}:{unix_timestamp}"
//   - Aggregates: "metrics:aggregate:{type}:{hour_unix_timestamp}"
//   - Latest:     "metrics:latest:{type}"

func (b *BadgerHistoryStore) snapshotKey(metricType string, timestamp time.Time) string {
	return fmt.Sprintf("metrics:snapshot:%s:%d", metricType, timestamp.Unix())
}

func (b *BadgerHistoryStore) aggregateKey(metricType string, hour time.Time) string {
	hourTimestamp := hour.Truncate(time.Hour).Unix()
	return fmt.Sprintf("metrics:aggregate:%s:%d", metricType, hourTimestamp)
}

func (b *BadgerHistoryStore) latestKey(metricType string) string {
	return fmt.Sprintf("metrics:latest:%s", metricType)
}

func (b *BadgerHistoryStore) snapshotPrefix(metricType string) string {
	return fmt.Sprintf("metrics:snapshot:%s:", metricType)
}

func (b *BadgerHistoryStore) aggregatePrefix(metricType string) string {
	return fmt.Sprintf("metrics:aggregate:%s:", metricType)
}

// SaveSnapshot saves a metric snapshot atomically (snapshot key + latest key).
func (b *BadgerHistoryStore) SaveSnapshot(metricType string, data map[string]interface{}) error {
	now := time.Now()
	snapshot := MetricSnapshot{Timestamp: now, Type: metricType, Data: data}

	dataJSON, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	snapKey := b.snapshotKey(metricType, now)
	latKey := b.latestKey(metricType)

	return b.kvStore.RawBatch(context.Background(), map[string][]byte{
		snapKey: dataJSON,
		latKey:  dataJSON,
	}, nil)
}

// GetSnapshots retrieves snapshots within a time range [start, end].
func (b *BadgerHistoryStore) GetSnapshots(metricType string, start, end time.Time) ([]MetricSnapshot, error) {
	prefix := b.snapshotPrefix(metricType)
	startKey := b.snapshotKey(metricType, start)
	endKey := b.snapshotKey(metricType, end)

	var snapshots []MetricSnapshot
	ctx := context.Background()

	err := b.kvStore.RawScan(ctx, prefix, startKey, func(key string, val []byte) bool {
		if key > endKey {
			return false // stop
		}
		var snapshot MetricSnapshot
		if err := json.Unmarshal(val, &snapshot); err != nil {
			logrus.WithError(err).Error("Failed to unmarshal snapshot")
			return true // continue despite error
		}
		snapshots = append(snapshots, snapshot)
		return true
	})

	return snapshots, err
}

// GetLatestSnapshot gets the most recent snapshot for a metric type.
func (b *BadgerHistoryStore) GetLatestSnapshot(metricType string) (*MetricSnapshot, error) {
	data, err := b.kvStore.GetRaw(context.Background(), b.latestKey(metricType))
	if err == metadata.ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var snapshot MetricSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// AggregateHourlyMetrics aggregates old snapshots into hourly summaries.
func (b *BadgerHistoryStore) AggregateHourlyMetrics() error {
	cutoffTime := time.Now().Add(-7 * 24 * time.Hour)
	types := []string{"system", "storage", "s3", "performance"}
	ctx := context.Background()

	for _, metricType := range types {
		// Pass 1: collect old snapshots grouped by hour
		hourlyData := make(map[int64][]MetricSnapshot)

		err := b.kvStore.RawScan(ctx, b.snapshotPrefix(metricType), "", func(_ string, val []byte) bool {
			var snapshot MetricSnapshot
			if err := json.Unmarshal(val, &snapshot); err != nil {
				logrus.WithError(err).Error("Failed to unmarshal snapshot")
				return true
			}
			if snapshot.Timestamp.After(cutoffTime) {
				return true
			}
			hourTimestamp := snapshot.Timestamp.Truncate(time.Hour).Unix()
			hourlyData[hourTimestamp] = append(hourlyData[hourTimestamp], snapshot)
			return true
		})
		if err != nil {
			logrus.WithError(err).Errorf("Failed to read snapshots for aggregation: %s", metricType)
			continue
		}

		if len(hourlyData) == 0 {
			continue
		}

		// Pass 2: build batch (save aggregates + delete old snapshots)
		sets := make(map[string][]byte)
		var deletes []string

		for hourTimestamp, snapshots := range hourlyData {
			if len(snapshots) == 0 {
				continue
			}
			dataPoints := make([]map[string]interface{}, len(snapshots))
			for i, s := range snapshots {
				dataPoints[i] = s.Data
			}
			aggregate := MetricSnapshot{
				Timestamp: time.Unix(hourTimestamp, 0),
				Type:      metricType,
				Data:      b.aggregateDataPoints(dataPoints),
			}
			aggregateJSON, err := json.Marshal(aggregate)
			if err != nil {
				logrus.WithError(err).Error("Failed to marshal aggregate")
				continue
			}
			sets[b.aggregateKey(metricType, time.Unix(hourTimestamp, 0))] = aggregateJSON
			for _, snapshot := range snapshots {
				deletes = append(deletes, b.snapshotKey(metricType, snapshot.Timestamp))
			}
		}

		if err := b.kvStore.RawBatch(ctx, sets, deletes); err != nil {
			logrus.WithError(err).Error("Failed to save aggregates")
		}
	}
	return nil
}

// aggregateDataPoints computes averages of numeric fields across data points.
func (b *BadgerHistoryStore) aggregateDataPoints(dataPoints []map[string]interface{}) map[string]interface{} {
	if len(dataPoints) == 0 {
		return map[string]interface{}{}
	}

	result := make(map[string]interface{})
	counts := make(map[string]int)
	sums := make(map[string]float64)

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
				result[key] = value
			}
		}
	}
	for key, sum := range sums {
		if counts[key] > 0 {
			result[key] = sum / float64(counts[key])
		}
	}
	return result
}

// GetAggregatedSnapshots retrieves hourly aggregate snapshots in [start, end].
func (b *BadgerHistoryStore) GetAggregatedSnapshots(metricType string, start, end time.Time) ([]MetricSnapshot, error) {
	prefix := b.aggregatePrefix(metricType)
	startKey := b.aggregateKey(metricType, start)
	endKey := b.aggregateKey(metricType, end)

	var snapshots []MetricSnapshot
	err := b.kvStore.RawScan(context.Background(), prefix, startKey, func(key string, val []byte) bool {
		if key > endKey {
			return false
		}
		var snapshot MetricSnapshot
		if err := json.Unmarshal(val, &snapshot); err != nil {
			logrus.WithError(err).Error("Failed to unmarshal aggregate")
			return true
		}
		snapshots = append(snapshots, snapshot)
		return true
	})
	return snapshots, err
}

// CleanupOldMetrics removes metrics older than the retention period.
func (b *BadgerHistoryStore) CleanupOldMetrics() error {
	cutoffTime := time.Now().Add(-time.Duration(b.retentionDays) * 24 * time.Hour)
	types := []string{"system", "storage", "s3", "performance"}
	ctx := context.Background()

	for _, metricType := range types {
		// Collect snapshot keys older than cutoff
		cutoffKey := b.snapshotKey(metricType, cutoffTime)
		var snapDeletes []string
		_ = b.kvStore.RawScan(ctx, b.snapshotPrefix(metricType), "", func(key string, _ []byte) bool {
			if key >= cutoffKey {
				return false
			}
			snapDeletes = append(snapDeletes, key)
			return true
		})
		if len(snapDeletes) > 0 {
			if err := b.kvStore.RawBatch(ctx, nil, snapDeletes); err != nil {
				logrus.WithError(err).Errorf("Failed to delete old snapshots for %s", metricType)
			}
		}

		// Collect aggregate keys older than cutoff
		aggCutoffKey := b.aggregateKey(metricType, cutoffTime)
		var aggDeletes []string
		_ = b.kvStore.RawScan(ctx, b.aggregatePrefix(metricType), "", func(key string, _ []byte) bool {
			if key >= aggCutoffKey {
				return false
			}
			aggDeletes = append(aggDeletes, key)
			return true
		})
		if len(aggDeletes) > 0 {
			if err := b.kvStore.RawBatch(ctx, nil, aggDeletes); err != nil {
				logrus.WithError(err).Errorf("Failed to delete old aggregates for %s", metricType)
			}
		}
	}

	// Ask the engine to reclaim space (no-op for Pebble)
	return b.kvStore.RawGC()
}

// GetSnapshotsIntelligent returns snapshots from the appropriate tier (raw or aggregate).
func (b *BadgerHistoryStore) GetSnapshotsIntelligent(metricType string, start, end time.Time) ([]MetricSnapshot, error) {
	sevenDaysAgo := time.Now().Add(-7 * 24 * time.Hour)
	var allSnapshots []MetricSnapshot

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

// Close is a no-op; the underlying store is managed by the server.
func (b *BadgerHistoryStore) Close() error { return nil }

// GetStats returns statistics about the stored metrics history.
func (b *BadgerHistoryStore) GetStats() (map[string]interface{}, error) {
	ctx := context.Background()
	types := []string{"system", "storage", "s3", "performance"}

	totalSnapshots := 0
	totalAggregates := 0
	var oldestSnapshot time.Time
	var newestSnapshot time.Time

	for _, metricType := range types {
		prefix := b.snapshotPrefix(metricType)
		_ = b.kvStore.RawScan(ctx, prefix, "", func(key string, _ []byte) bool {
			totalSnapshots++
			timestampStr := key[len(prefix):]
			if ts, err := strconv.ParseInt(timestampStr, 10, 64); err == nil {
				t := time.Unix(ts, 0)
				if oldestSnapshot.IsZero() || t.Before(oldestSnapshot) {
					oldestSnapshot = t
				}
				if newestSnapshot.IsZero() || t.After(newestSnapshot) {
					newestSnapshot = t
				}
			}
			return true
		})

		_ = b.kvStore.RawScan(ctx, b.aggregatePrefix(metricType), "", func(_ string, _ []byte) bool {
			totalAggregates++
			return true
		})
	}

	return map[string]interface{}{
		"snapshot_count":   totalSnapshots,
		"aggregate_count":  totalAggregates,
		"oldest_snapshot":  oldestSnapshot,
		"newest_snapshot":  newestSnapshot,
	}, nil
}
