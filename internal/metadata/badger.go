package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/sirupsen/logrus"
)

// BadgerStore implements the Store interface using BadgerDB
type BadgerStore struct {
	db              *badger.DB
	ready           atomic.Bool
	logger          *logrus.Logger
	bucketMetricsMu sync.Map // map[string]*sync.Mutex — one mutex per bucket key, eliminates BadgerDB OCC conflicts
}

// BadgerOptions contains configuration options for BadgerStore
type BadgerOptions struct {
	DataDir           string
	SyncWrites        bool // If true, every write is synced to disk (slower but safer)
	CompactionEnabled bool // Enable automatic compaction
	Logger            *logrus.Logger
}

// NewBadgerStore creates a new BadgerDB-backed metadata store
func NewBadgerStore(opts BadgerOptions) (*BadgerStore, error) {
	if opts.Logger == nil {
		opts.Logger = logrus.New()
	}

	// Ensure data directory exists
	dbPath := filepath.Join(opts.DataDir, "metadata")

	// Configure BadgerDB options
	badgerOpts := badger.DefaultOptions(dbPath).
		WithLogger(newBadgerLogger(opts.Logger)).
		WithSyncWrites(opts.SyncWrites).
		WithIndexCacheSize(100 << 20). // 100MB index cache
		WithBlockCacheSize(256 << 20). // 256MB block cache
		WithNumVersionsToKeep(1)       // Keep only latest version (we manage versions separately)

	// Open BadgerDB
	db, err := badger.Open(badgerOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger db: %w", err)
	}

	store := &BadgerStore{
		db:     db,
		logger: opts.Logger,
	}

	store.ready.Store(true)

	// Start garbage collection if compaction is enabled
	if opts.CompactionEnabled {
		go store.runGC()
	}

	opts.Logger.WithField("path", dbPath).Info("BadgerDB metadata store initialized")

	return store, nil
}

// DB returns the underlying BadgerDB instance
// This is useful for advanced operations like metrics storage
func (s *BadgerStore) DB() *badger.DB {
	return s.db
}

// ==================== Key Naming Scheme ====================
// This defines how we structure keys in BadgerDB for efficient lookups

func bucketKey(tenantID, name string) []byte {
	return []byte(fmt.Sprintf("bucket:%s:%s", tenantID, name))
}

func bucketListPrefix(tenantID string) []byte {
	return []byte(fmt.Sprintf("bucket:%s:", tenantID))
}

func objectKey(bucket, key string) []byte {
	return []byte(fmt.Sprintf("obj:%s:%s", bucket, key))
}

func objectVersionKey(bucket, key, versionID string) []byte {
	return []byte(fmt.Sprintf("version:%s:%s:%s", bucket, key, versionID))
}

func objectListPrefix(bucket string) []byte {
	return []byte(fmt.Sprintf("obj:%s:", bucket))
}

func objectPrefixKey(bucket, prefix string) []byte {
	return []byte(fmt.Sprintf("obj:%s:%s", bucket, prefix))
}

func multipartUploadKey(uploadID string) []byte {
	return []byte(fmt.Sprintf("multipart:%s", uploadID))
}

func multipartListPrefix(bucket string) []byte {
	return []byte(fmt.Sprintf("multipart_idx:%s:", bucket))
}

func multipartIndexKey(bucket, uploadID string) []byte {
	return []byte(fmt.Sprintf("multipart_idx:%s:%s", bucket, uploadID))
}

func partKey(uploadID string, partNumber int) []byte {
	return []byte(fmt.Sprintf("part:%s:%05d", uploadID, partNumber))
}

func partListPrefix(uploadID string) []byte {
	return []byte(fmt.Sprintf("part:%s:", uploadID))
}

func tagIndexKey(bucket, tagKey, tagValue, objectKey string) []byte {
	return []byte(fmt.Sprintf("tag_idx:%s:%s:%s:%s", bucket, tagKey, tagValue, objectKey))
}

func tagIndexPrefix(bucket, tagKey, tagValue string) []byte {
	return []byte(fmt.Sprintf("tag_idx:%s:%s:%s:", bucket, tagKey, tagValue))
}

// ==================== Bucket Operations ====================

// CreateBucket creates a new bucket
func (s *BadgerStore) CreateBucket(ctx context.Context, bucket *BucketMetadata) error {
	if bucket == nil {
		return fmt.Errorf("bucket metadata cannot be nil")
	}

	key := bucketKey(bucket.TenantID, bucket.Name)

	return s.db.Update(func(txn *badger.Txn) error {
		// Check if bucket already exists in this tenant
		_, err := txn.Get(key)
		if err == nil {
			return ErrBucketAlreadyExists
		}
		if err != badger.ErrKeyNotFound {
			return fmt.Errorf("failed to check bucket existence: %w", err)
		}

		// Check for global uniqueness - bucket names must be unique across all tenants
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("bucket:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			var existingBucket BucketMetadata
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &existingBucket)
			}); err == nil {
				if existingBucket.Name == bucket.Name {
					return ErrBucketAlreadyExists
				}
			}
		}

		// Set timestamps
		now := time.Now()
		if bucket.CreatedAt.IsZero() {
			bucket.CreatedAt = now
		}
		bucket.UpdatedAt = now

		// Marshal and store
		data, err := json.Marshal(bucket)
		if err != nil {
			return fmt.Errorf("failed to marshal bucket metadata: %w", err)
		}

		if err := txn.Set(key, data); err != nil {
			return fmt.Errorf("failed to store bucket: %w", err)
		}

		s.logger.WithFields(logrus.Fields{
			"bucket":    bucket.Name,
			"tenant_id": bucket.TenantID,
		}).Debug("Bucket created in metadata store")

		return nil
	})
}

// GetBucket retrieves bucket metadata
func (s *BadgerStore) GetBucket(ctx context.Context, tenantID, name string) (*BucketMetadata, error) {
	var bucket BucketMetadata

	err := s.db.View(func(txn *badger.Txn) error {
		key := bucketKey(tenantID, name)

		item, err := txn.Get(key)
		if err == badger.ErrKeyNotFound {
			return ErrBucketNotFound
		}
		if err != nil {
			return fmt.Errorf("failed to get bucket: %w", err)
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &bucket)
		})
	})

	if err != nil {
		return nil, err
	}

	return &bucket, nil
}

// UpdateBucket updates an existing bucket's metadata
func (s *BadgerStore) UpdateBucket(ctx context.Context, bucket *BucketMetadata) error {
	if bucket == nil {
		return fmt.Errorf("bucket metadata cannot be nil")
	}

	key := bucketKey(bucket.TenantID, bucket.Name)

	return s.db.Update(func(txn *badger.Txn) error {
		// Check if bucket exists
		_, err := txn.Get(key)
		if err == badger.ErrKeyNotFound {
			return ErrBucketNotFound
		}
		if err != nil {
			return fmt.Errorf("failed to check bucket existence: %w", err)
		}

		// Update timestamp
		bucket.UpdatedAt = time.Now()

		// Marshal and store
		data, err := json.Marshal(bucket)
		if err != nil {
			return fmt.Errorf("failed to marshal bucket metadata: %w", err)
		}

		return txn.Set(key, data)
	})
}

// DeleteBucket deletes a bucket
func (s *BadgerStore) DeleteBucket(ctx context.Context, tenantID, name string) error {
	key := bucketKey(tenantID, name)

	return s.db.Update(func(txn *badger.Txn) error {
		// Check if bucket exists
		_, err := txn.Get(key)
		if err == badger.ErrKeyNotFound {
			return ErrBucketNotFound
		}
		if err != nil {
			return fmt.Errorf("failed to check bucket existence: %w", err)
		}

		// Delete the bucket
		if err := txn.Delete(key); err != nil {
			return fmt.Errorf("failed to delete bucket: %w", err)
		}

		s.logger.WithFields(logrus.Fields{
			"bucket":    name,
			"tenant_id": tenantID,
		}).Debug("Bucket deleted from metadata store")

		return nil
	})
}

// ListBuckets lists all buckets for a tenant
func (s *BadgerStore) ListBuckets(ctx context.Context, tenantID string) ([]*BucketMetadata, error) {
	var buckets []*BucketMetadata

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions

		// If tenantID is empty (global admin), list ALL buckets
		// Otherwise, list only buckets for specific tenant
		if tenantID == "" {
			opts.Prefix = []byte("bucket:")
		} else {
			opts.Prefix = bucketListPrefix(tenantID)
		}

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()

			var bucket BucketMetadata
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &bucket)
			})
			if err != nil {
				s.logger.WithError(err).Warn("Failed to unmarshal bucket metadata")
				continue
			}

			buckets = append(buckets, &bucket)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return buckets, nil
}

// GetBucketByName finds a bucket by name across all tenants
// Since bucket names are globally unique, this returns the first match
func (s *BadgerStore) GetBucketByName(ctx context.Context, name string) (*BucketMetadata, error) {
	var result *BucketMetadata

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = []byte("bucket:")
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			var bucket BucketMetadata
			if err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &bucket)
			}); err == nil {
				if bucket.Name == name {
					result = &bucket
					return nil // Found it, stop iteration
				}
			}
		}
		return ErrBucketNotFound
	})

	if err != nil {
		return nil, err
	}
	return result, nil
}

// BucketExists checks if a bucket exists
func (s *BadgerStore) BucketExists(ctx context.Context, tenantID, name string) (bool, error) {
	key := bucketKey(tenantID, name)

	err := s.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get(key)
		return err
	})

	if err == badger.ErrKeyNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

// getBucketMetricsMutex returns a per-bucket mutex for serializing metric updates.
// Using a Go-level mutex instead of BadgerDB's OCC eliminates ErrConflict entirely,
// making metric updates safe under any level of concurrency (100 VEEAM clients, etc.).
func (s *BadgerStore) getBucketMetricsMutex(key []byte) *sync.Mutex {
	keyStr := string(key)
	mu, _ := s.bucketMetricsMu.LoadOrStore(keyStr, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// UpdateBucketMetrics atomically updates bucket metrics.
// Uses a per-bucket Go mutex to serialise concurrent updates, eliminating
// BadgerDB OCC conflicts regardless of how many parallel S3 clients write to
// the same bucket.
func (s *BadgerStore) UpdateBucketMetrics(ctx context.Context, tenantID, bucketName string, objectCountDelta, sizeDelta int64) error {
	key := bucketKey(tenantID, bucketName)

	// Acquire the per-bucket mutex. Only one goroutine at a time can update
	// this bucket's counters — no BadgerDB transaction conflicts possible.
	mu := s.getBucketMetricsMutex(key)
	mu.Lock()
	defer mu.Unlock()

	return s.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err == badger.ErrKeyNotFound {
			return ErrBucketNotFound
		}
		if err != nil {
			return fmt.Errorf("failed to get bucket: %w", err)
		}

		var bucket BucketMetadata
		if err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &bucket)
		}); err != nil {
			return fmt.Errorf("failed to unmarshal bucket: %w", err)
		}

		bucket.ObjectCount += objectCountDelta
		bucket.TotalSize += sizeDelta
		bucket.UpdatedAt = time.Now()

		if bucket.ObjectCount < 0 {
			bucket.ObjectCount = 0
		}
		if bucket.TotalSize < 0 {
			bucket.TotalSize = 0
		}

		data, err := json.Marshal(&bucket)
		if err != nil {
			return fmt.Errorf("failed to marshal bucket: %w", err)
		}

		return txn.Set(key, data)
	})
}

// GetBucketStats retrieves bucket statistics
func (s *BadgerStore) GetBucketStats(ctx context.Context, tenantID, bucketName string) (objectCount, totalSize int64, err error) {
	bucket, err := s.GetBucket(ctx, tenantID, bucketName)
	if err != nil {
		return 0, 0, err
	}

	return bucket.ObjectCount, bucket.TotalSize, nil
}

// RecalculateBucketStats recalculates bucket statistics by scanning all objects
func (s *BadgerStore) RecalculateBucketStats(ctx context.Context, tenantID, bucketName string) error {
	var objectCount int64
	var totalSize int64

	// Count all objects in the bucket
	// Build the full bucket path as used in object keys: "tenantID/bucketName" or "bucketName"
	fullBucketPath := bucketName
	if tenantID != "" {
		fullBucketPath = tenantID + "/" + bucketName
	}

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = objectListPrefix(fullBucketPath)

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()

			var obj ObjectMetadata
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &obj)
			})
			if err != nil {
				continue
			}

			objectCount++
			totalSize += obj.Size
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Update bucket with recalculated metrics
	key := bucketKey(tenantID, bucketName)
	return s.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		var bucket BucketMetadata
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &bucket)
		})
		if err != nil {
			return err
		}

		bucket.ObjectCount = objectCount
		bucket.TotalSize = totalSize
		bucket.UpdatedAt = time.Now()

		data, err := json.Marshal(&bucket)
		if err != nil {
			return err
		}

		return txn.Set(key, data)
	})
}

// ==================== Lifecycle ====================

// Close closes the BadgerDB instance
func (s *BadgerStore) Close() error {
	s.ready.Store(false)
	s.logger.Info("Closing BadgerDB metadata store")
	return s.db.Close()
}

// IsReady returns true if the store is ready
func (s *BadgerStore) IsReady() bool {
	return s.ready.Load()
}

// Compact runs garbage collection and compaction
func (s *BadgerStore) Compact(ctx context.Context) error {
	s.logger.Info("Starting BadgerDB compaction")
	return s.db.RunValueLogGC(0.5) // Rewrite if 50% of space can be reclaimed
}

// Backup creates a backup of the database
func (s *BadgerStore) Backup(ctx context.Context, path string) error {
	s.logger.WithField("path", path).Info("Creating backup")

	file, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid backup path: %w", err)
	}

	// Create backup file
	f, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("failed to create backup file: %w", err)
	}
	defer f.Close()

	// Perform backup
	_, err = s.db.Backup(f, 0)
	return err
}

// runGC runs garbage collection periodically
func (s *BadgerStore) runGC() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		if !s.ready.Load() {
			return
		}

		err := s.db.RunValueLogGC(0.5)
		if err != nil && err != badger.ErrNoRewrite {
			s.logger.WithError(err).Warn("Failed to run GC")
		}
	}
}

// ==================== Helper Functions ====================

// badgerLogger adapts logrus to BadgerDB's logger interface
type badgerLogger struct {
	logger *logrus.Logger
}

func newBadgerLogger(logger *logrus.Logger) *badgerLogger {
	return &badgerLogger{logger: logger}
}

func (l *badgerLogger) Errorf(format string, args ...interface{}) {
	l.logger.Errorf("[BadgerDB] "+format, args...)
}

func (l *badgerLogger) Warningf(format string, args ...interface{}) {
	l.logger.Warnf("[BadgerDB] "+format, args...)
}

func (l *badgerLogger) Infof(format string, args ...interface{}) {
	l.logger.Debugf("[BadgerDB] "+format, args...)
}

func (l *badgerLogger) Debugf(format string, args ...interface{}) {
	l.logger.Tracef("[BadgerDB] "+format, args...)
}

// extractObjectKeyFromKey extracts the object key from a BadgerDB key
func extractObjectKeyFromKey(key string) string {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

// ============================================================================
// Generic Key-Value Operations (for storing custom configurations)
// ============================================================================

var ErrNotFound = errors.New("key not found")

// GetRaw retrieves a raw value from BadgerDB
func (s *BadgerStore) GetRaw(ctx context.Context, key string) ([]byte, error) {
	var value []byte
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return ErrNotFound
			}
			return err
		}

		value, err = item.ValueCopy(nil)
		return err
	})

	if err != nil {
		return nil, err
	}
	return value, nil
}

// PutRaw stores a raw value in BadgerDB
func (s *BadgerStore) PutRaw(ctx context.Context, key string, value []byte) error {
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), value)
	})
}

// DeleteRaw deletes a key from BadgerDB
func (s *BadgerStore) DeleteRaw(ctx context.Context, key string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		err := txn.Delete([]byte(key))
		if err == badger.ErrKeyNotFound {
			return ErrNotFound
		}
		return err
	})
}
