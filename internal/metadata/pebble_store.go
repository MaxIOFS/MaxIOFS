package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/bloom"
	"github.com/sirupsen/logrus"
)

// PebbleStore implements the Store interface using Pebble (CockroachDB's LSM engine).
// Pebble's WAL is used for crash-safe metadata persistence.
//
// Durability model: hot-path writes (object puts, parts, metrics) commit with
// NoSync and a background loop fsyncs the WAL once per WALSyncInterval, so a
// hard kill loses at most that window. Low-frequency destructive operations
// (object/bucket deletes, multipart complete/abort) commit with Sync — losing
// a delete tombstone would leave a ghost listing entry pointing at a removed
// file, which no fallback can serve.
type PebbleStore struct {
	db               *pebble.DB
	ready            atomic.Bool
	logger           *logrus.Logger
	bucketMetricsMu  sync.Map   // map[string]*sync.Mutex — one per bucket key
	bucketMutationMu sync.Map   // map[string]*sync.Mutex — serializes object writes with bucket deletion
	deletedBuckets   sync.Map   // map[string]struct{} — buckets deleted during this process lifetime
	bucketCreateMu   sync.Mutex // serializes bucket creation for global uniqueness check
	stopCh           chan struct{}
	dbPath           string
	walDirty         atomic.Bool // unsynced NoSync writes since the last WAL fsync
	walSyncWG        sync.WaitGroup
	wasCleanShutdown bool
}

// PebbleOptions contains configuration options for PebbleStore
type PebbleOptions struct {
	DataDir     string
	Logger      *logrus.Logger
	CacheSizeMB int // Block cache size in MB (default 256)
	// WALSyncInterval is how often the background loop fsyncs the WAL,
	// bounding metadata loss on a hard kill. 0 uses the 1s default; a
	// negative value disables the loop (tests).
	WALSyncInterval time.Duration
}

// defaultWALSyncInterval bounds hard-kill metadata loss to ~1s at the cost of
// at most one fsync per second — the "everysec" model.
const defaultWALSyncInterval = time.Second

// cleanShutdownSentinelFile marks that the store was closed cleanly. It is
// written by Close after the DB closes and consumed (removed) on open; if a
// pre-existing store opens without it, the previous process died hard and the
// server should reconcile metadata against the on-disk object tree.
const cleanShutdownSentinelFile = "CLEAN_SHUTDOWN"

// NewPebbleStore creates a new Pebble-backed metadata store
func NewPebbleStore(opts PebbleOptions) (*PebbleStore, error) {
	if opts.Logger == nil {
		opts.Logger = logrus.New()
	}

	dbPath := filepath.Join(opts.DataDir, "metadata")
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create metadata directory: %w", err)
	}

	cacheSizeMB := opts.CacheSizeMB
	if cacheSizeMB <= 0 {
		cacheSizeMB = 256
	}
	cache := pebble.NewCache(int64(cacheSizeMB) << 20)
	defer cache.Unref()

	pebbleOpts := &pebble.Options{
		Cache:                       cache,
		WALBytesPerSync:             512 << 10,                         // background-flush WAL pages so the periodic fsync stays cheap
		MemTableSize:                64 << 20,                          // 64 MB per memtable
		MemTableStopWritesThreshold: 12,                                // allow more memtables before stalling writes
		L0CompactionThreshold:       4,                                 // compact L0 sooner (default 4)
		L0StopWritesThreshold:       12,                                // allow more L0 files before stalling
		CompactionConcurrencyRange:  func() (int, int) { return 2, 4 }, // 2–4 parallel compactions
		Levels: [7]pebble.LevelOptions{
			// L0: no bloom filter (range scans dominate at L0)
			{BlockSize: 32 << 10},
			// L1–L6: bloom filters speed up point lookups
			{BlockSize: 32 << 10, FilterPolicy: bloom.FilterPolicy(10)},
			{BlockSize: 32 << 10, FilterPolicy: bloom.FilterPolicy(10)},
			{BlockSize: 32 << 10, FilterPolicy: bloom.FilterPolicy(10)},
			{BlockSize: 32 << 10, FilterPolicy: bloom.FilterPolicy(10)},
			{BlockSize: 32 << 10, FilterPolicy: bloom.FilterPolicy(10)},
			{BlockSize: 32 << 10, FilterPolicy: bloom.FilterPolicy(10)},
		},
		Logger: &pebbleLogger{logger: opts.Logger},
	}

	// Clean-shutdown detection, decided BEFORE opening: a store existed here
	// (our v2 format sentinel is written on every open, so it reliably marks
	// a pre-existing store — Pebble itself has no CURRENT file) but the
	// sentinel its Close should have written is missing → the previous
	// process died hard. A fresh directory counts as clean (nothing to
	// reconcile). The sentinel is consumed so a future crash cannot read a
	// stale one.
	_, formatErr := os.Stat(filepath.Join(dbPath, PebbleV2SentinelFile))
	storeExisted := formatErr == nil
	shutdownSentinel := filepath.Join(dbPath, cleanShutdownSentinelFile)
	_, sentinelErr := os.Stat(shutdownSentinel)
	wasClean := !storeExisted || sentinelErr == nil
	_ = os.Remove(shutdownSentinel)

	db, err := pebble.Open(dbPath, pebbleOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to open pebble db: %w", err)
	}

	// Write sentinel so MigrateFromPebbleV1IfNeeded can identify this as v2 on future starts.
	sentinelPath := filepath.Join(dbPath, PebbleV2SentinelFile)
	if _, statErr := os.Stat(sentinelPath); os.IsNotExist(statErr) {
		_ = os.WriteFile(sentinelPath, []byte("v2\n"), 0644)
	}

	store := &PebbleStore{
		db:               db,
		logger:           opts.Logger,
		stopCh:           make(chan struct{}),
		dbPath:           dbPath,
		wasCleanShutdown: wasClean,
	}
	store.ready.Store(true)

	// Start TTL cleanup goroutine for multipart uploads.
	go store.runMultipartCleanup()

	// Start the periodic WAL fsync loop.
	walSyncInterval := opts.WALSyncInterval
	if walSyncInterval == 0 {
		walSyncInterval = defaultWALSyncInterval
	}
	if walSyncInterval > 0 {
		store.walSyncWG.Add(1)
		go store.runWALSyncLoop(walSyncInterval)
	}

	opts.Logger.WithField("path", dbPath).Info("Pebble metadata store initialized")
	return store, nil
}

// WasCleanShutdown reports whether the previous process closed this store
// cleanly (or the store is brand new). False means the server should
// reconcile metadata against the on-disk object tree.
func (s *PebbleStore) WasCleanShutdown() bool {
	return s.wasCleanShutdown
}

// runWALSyncLoop fsyncs the WAL once per interval while there are unsynced
// writes. Pebble's group commit makes the empty LogData record below durable
// together with everything committed before it.
func (s *PebbleStore) runWALSyncLoop(interval time.Duration) {
	defer s.walSyncWG.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	failing := false
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			if !s.walDirty.Swap(false) {
				continue
			}
			if err := s.db.LogData(nil, pebble.Sync); err != nil {
				s.walDirty.Store(true) // retry next tick
				if !failing {
					s.logger.WithError(err).Warn("Periodic WAL sync failed; retrying every interval")
					failing = true
				}
			} else if failing {
				s.logger.Info("Periodic WAL sync recovered")
				failing = false
			}
		}
	}
}

// setNoSync / commitNoSync are the hot-path write helpers:
// they commit without fsync and flag the WAL dirty so the periodic sync loop
// makes the write durable within one interval. The dirty flag is set AFTER
// the write lands in the WAL — a concurrent tick between the two at worst
// syncs once more than needed, never misses the write.
func (s *PebbleStore) setNoSync(key, value []byte) error {
	err := s.db.Set(key, value, pebble.NoSync)
	if err == nil {
		s.walDirty.Store(true)
	}
	return err
}

func (s *PebbleStore) commitNoSync(batch *pebble.Batch) error {
	err := batch.Commit(pebble.NoSync)
	if err == nil {
		s.walDirty.Store(true)
	}
	return err
}

// ==================== Key Helpers ====================

// prefixEnd returns the exclusive upper bound for a prefix scan in Pebble.
// It increments the last byte of the prefix; returns nil if all bytes overflow.
func prefixEnd(prefix []byte) []byte {
	end := make([]byte, len(prefix))
	copy(end, prefix)
	for i := len(end) - 1; i >= 0; i-- {
		end[i]++
		if end[i] != 0 {
			return end[:i+1]
		}
	}
	return nil // all bytes overflowed — no upper bound
}

// pebbleGet reads a single key from Pebble and returns a safe copy of the value.
func (s *PebbleStore) pebbleGet(key []byte) ([]byte, error) {
	val, closer, err := s.db.Get(key)
	if err != nil {
		return nil, err
	}
	data := make([]byte, len(val))
	copy(data, val)
	_ = closer.Close()
	return data, nil
}

// pebbleIter creates a prefix-bounded iterator over [lower, prefixEnd(lower)).
func (s *PebbleStore) pebbleIter(lower []byte) (*pebble.Iterator, error) {
	upper := prefixEnd(lower)
	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: lower,
		UpperBound: upper,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	return iter, nil
}

// ==================== Bucket Operations ====================

// CreateBucket creates a new bucket with global name uniqueness enforced.
func (s *PebbleStore) CreateBucket(ctx context.Context, bucket *BucketMetadata) error {
	if bucket == nil {
		return fmt.Errorf("bucket metadata cannot be nil")
	}

	s.bucketCreateMu.Lock()
	defer s.bucketCreateMu.Unlock()

	key := bucketKey(bucket.TenantID, bucket.Name)

	// Check if this exact tenant+name already exists
	if _, closer, err := s.db.Get(key); err == nil {
		_ = closer.Close()
		return ErrBucketAlreadyExists
	} else if err != pebble.ErrNotFound {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	}

	// Check global uniqueness — bucket names must be unique across all tenants
	prefix := []byte("bucket:")
	iter, err := s.pebbleIter(prefix)
	if err != nil {
		return err
	}
	defer iter.Close() //nolint:errcheck

	for iter.First(); iter.Valid(); iter.Next() {
		var existing BucketMetadata
		if jsonErr := json.Unmarshal(iter.Value(), &existing); jsonErr == nil {
			if existing.Name == bucket.Name {
				return ErrBucketAlreadyExists
			}
		}
	}
	if err := iter.Error(); err != nil {
		return fmt.Errorf("failed during bucket uniqueness check: %w", err)
	}

	now := time.Now()
	if bucket.CreatedAt.IsZero() {
		bucket.CreatedAt = now
	}
	bucket.UpdatedAt = now

	data, err := json.Marshal(bucket)
	if err != nil {
		return fmt.Errorf("failed to marshal bucket: %w", err)
	}

	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store bucket: %w", err)
	}
	s.deletedBuckets.Delete(bucketPathForMutation(bucket.TenantID, bucket.Name))

	s.logger.WithFields(logrus.Fields{
		"bucket":    bucket.Name,
		"tenant_id": bucket.TenantID,
	}).Debug("Bucket created in Pebble metadata store")

	return nil
}

// GetBucket retrieves bucket metadata by tenant and name.
func (s *PebbleStore) GetBucket(ctx context.Context, tenantID, name string) (*BucketMetadata, error) {
	key := bucketKey(tenantID, name)
	data, err := s.pebbleGet(key)
	if err == pebble.ErrNotFound {
		return nil, ErrBucketNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get bucket: %w", err)
	}

	var bucket BucketMetadata
	if err := json.Unmarshal(data, &bucket); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bucket: %w", err)
	}
	return &bucket, nil
}

// UpdateBucket updates an existing bucket's metadata.
func (s *PebbleStore) UpdateBucket(ctx context.Context, bucket *BucketMetadata) error {
	if bucket == nil {
		return fmt.Errorf("bucket metadata cannot be nil")
	}

	key := bucketKey(bucket.TenantID, bucket.Name)
	if _, closer, err := s.db.Get(key); err == pebble.ErrNotFound {
		return ErrBucketNotFound
	} else if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	} else {
		_ = closer.Close()
	}

	bucket.UpdatedAt = time.Now()
	data, err := json.Marshal(bucket)
	if err != nil {
		return fmt.Errorf("failed to marshal bucket: %w", err)
	}
	if err := s.db.Set(key, data, pebble.Sync); err != nil {
		return err
	}
	s.deletedBuckets.Delete(bucketPathForMutation(bucket.TenantID, bucket.Name))
	return nil
}

// DeleteBucket deletes a bucket from the store.
func (s *PebbleStore) DeleteBucket(ctx context.Context, tenantID, name string) error {
	key := bucketKey(tenantID, name)
	bucketPath := bucketPathForMutation(tenantID, name)

	mu := s.getBucketMutationMutex(bucketPath)
	mu.Lock()
	defer mu.Unlock()

	if _, closer, err := s.db.Get(key); err == pebble.ErrNotFound {
		return ErrBucketNotFound
	} else if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	} else {
		_ = closer.Close()
	}

	if err := s.db.Delete(key, pebble.Sync); err != nil {
		return fmt.Errorf("failed to delete bucket: %w", err)
	}
	s.deletedBuckets.Store(bucketPath, struct{}{})

	s.logger.WithFields(logrus.Fields{
		"bucket":    name,
		"tenant_id": tenantID,
	}).Debug("Bucket deleted from Pebble metadata store")

	return nil
}

// DeleteBucketIfEmpty deletes the bucket only if it has no object or version metadata.
// Returns ErrBucketNotFound if the bucket does not exist, ErrBucketNotEmpty if objects remain.
func (s *PebbleStore) DeleteBucketIfEmpty(ctx context.Context, tenantID, name string) error {
	key := bucketKey(tenantID, name)
	bucketPath := bucketPathForMutation(tenantID, name)

	mu := s.getBucketMutationMutex(bucketPath)
	mu.Lock()
	defer mu.Unlock()

	if _, closer, err := s.db.Get(key); err == pebble.ErrNotFound {
		return ErrBucketNotFound
	} else if err != nil {
		return fmt.Errorf("failed to check bucket existence: %w", err)
	} else {
		_ = closer.Close()
	}

	objPrefix := []byte(fmt.Sprintf("obj:%s:", bucketPath))
	objIter, err := s.pebbleIter(objPrefix)
	if err != nil {
		return fmt.Errorf("failed to create object iterator: %w", err)
	}
	hasObjects := objIter.First()
	_ = objIter.Close()
	if hasObjects {
		return ErrBucketNotEmpty
	}

	verPrefix := []byte(fmt.Sprintf("version:%s:", bucketPath))
	verIter, err := s.pebbleIter(verPrefix)
	if err != nil {
		return fmt.Errorf("failed to create version iterator: %w", err)
	}
	hasVersions := verIter.First()
	_ = verIter.Close()
	if hasVersions {
		return ErrBucketNotEmpty
	}

	if err := s.db.Delete(key, pebble.Sync); err != nil {
		return fmt.Errorf("failed to delete bucket: %w", err)
	}
	s.deletedBuckets.Store(bucketPath, struct{}{})

	s.logger.WithFields(logrus.Fields{
		"bucket":    name,
		"tenant_id": tenantID,
	}).Debug("Bucket deleted from Pebble metadata store")

	return nil
}

// ListBuckets lists all buckets for a tenant (empty tenantID = all tenants).
func (s *PebbleStore) ListBuckets(ctx context.Context, tenantID string) ([]*BucketMetadata, error) {
	var lower []byte
	if tenantID == "" {
		lower = []byte("bucket:")
	} else {
		lower = bucketListPrefix(tenantID)
	}

	iter, err := s.pebbleIter(lower)
	if err != nil {
		return nil, err
	}
	defer iter.Close() //nolint:errcheck

	var buckets []*BucketMetadata
	for iter.First(); iter.Valid(); iter.Next() {
		var bucket BucketMetadata
		if err := json.Unmarshal(iter.Value(), &bucket); err != nil {
			s.logger.WithError(err).Warn("Failed to unmarshal bucket metadata")
			continue
		}
		buckets = append(buckets, &bucket)
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("failed during bucket list: %w", err)
	}
	return buckets, nil
}

// GetBucketByName finds a bucket by name across all tenants.
func (s *PebbleStore) GetBucketByName(ctx context.Context, name string) (*BucketMetadata, error) {
	iter, err := s.pebbleIter([]byte("bucket:"))
	if err != nil {
		return nil, err
	}
	defer iter.Close() //nolint:errcheck

	for iter.First(); iter.Valid(); iter.Next() {
		var bucket BucketMetadata
		if err := json.Unmarshal(iter.Value(), &bucket); err == nil {
			if bucket.Name == name {
				return &bucket, nil
			}
		}
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("failed during bucket scan: %w", err)
	}
	return nil, ErrBucketNotFound
}

// BucketExists checks if a bucket exists.
func (s *PebbleStore) BucketExists(ctx context.Context, tenantID, name string) (bool, error) {
	key := bucketKey(tenantID, name)
	if _, closer, err := s.db.Get(key); err == pebble.ErrNotFound {
		return false, nil
	} else if err != nil {
		return false, err
	} else {
		_ = closer.Close()
		return true, nil
	}
}

// getBucketMetricsMutex returns a per-bucket mutex to serialise concurrent metric updates.
func (s *PebbleStore) getBucketMetricsMutex(key []byte) *sync.Mutex {
	keyStr := string(key)
	mu, _ := s.bucketMetricsMu.LoadOrStore(keyStr, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// getBucketMutationMutex serializes bucket deletion with object metadata writes.
// Pebble batches make each write atomic, but they do not make a prefix scan plus
// delete atomic against concurrent writers in the same process.
func (s *PebbleStore) getBucketMutationMutex(bucket string) *sync.Mutex {
	mu, _ := s.bucketMutationMu.LoadOrStore(bucket, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

func bucketPathForMutation(tenantID, name string) string {
	if tenantID == "" {
		return name
	}
	return tenantID + "/" + name
}

func (s *PebbleStore) rejectWriteToDeletedBucket(bucket string) error {
	if _, deleted := s.deletedBuckets.Load(bucket); deleted {
		return ErrBucketNotFound
	}
	return nil
}

// UpdateBucketMetrics atomically updates bucket object count and total size.
func (s *PebbleStore) UpdateBucketMetrics(ctx context.Context, tenantID, bucketName string, objectCountDelta, sizeDelta int64) error {
	key := bucketKey(tenantID, bucketName)
	mu := s.getBucketMetricsMutex(key)
	mu.Lock()
	defer mu.Unlock()

	data, err := s.pebbleGet(key)
	if err == pebble.ErrNotFound {
		return ErrBucketNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to get bucket: %w", err)
	}

	var bucket BucketMetadata
	if err := json.Unmarshal(data, &bucket); err != nil {
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

	newData, err := json.Marshal(&bucket)
	if err != nil {
		return fmt.Errorf("failed to marshal bucket: %w", err)
	}
	return s.setNoSync(key, newData)
}

// GetBucketStats retrieves cached statistics for a bucket.
func (s *PebbleStore) GetBucketStats(ctx context.Context, tenantID, bucketName string) (int64, int64, error) {
	bucket, err := s.GetBucket(ctx, tenantID, bucketName)
	if err != nil {
		return 0, 0, err
	}
	return bucket.ObjectCount, bucket.TotalSize, nil
}

// RecalculateBucketStats scans all objects to recompute bucket statistics.
func (s *PebbleStore) RecalculateBucketStats(ctx context.Context, tenantID, bucketName string) error {
	fullBucketPath := bucketName
	if tenantID != "" {
		fullBucketPath = tenantID + "/" + bucketName
	}

	lower := objectListPrefix(fullBucketPath)
	iter, err := s.pebbleIter(lower)
	if err != nil {
		return err
	}

	var objectCount, totalSize int64
	for iter.First(); iter.Valid(); iter.Next() {
		var obj ObjectMetadata
		if err := json.Unmarshal(iter.Value(), &obj); err != nil {
			continue
		}
		// Skip delete markers (ETag="" Size=0): they indicate the object is
		// logically deleted in a versioned bucket and should not be counted.
		if obj.ETag == "" && obj.Size == 0 {
			continue
		}
		objectCount++
		totalSize += obj.Size
	}
	iterErr := iter.Error()
	_ = iter.Close()
	if iterErr != nil {
		return fmt.Errorf("failed during bucket scan: %w", iterErr)
	}

	key := bucketKey(tenantID, bucketName)
	mu := s.getBucketMetricsMutex(key)
	mu.Lock()
	defer mu.Unlock()

	data, err := s.pebbleGet(key)
	if err != nil {
		return err
	}

	var bucket BucketMetadata
	if err := json.Unmarshal(data, &bucket); err != nil {
		return err
	}

	bucket.ObjectCount = objectCount
	bucket.TotalSize = totalSize
	bucket.UpdatedAt = time.Now()

	newData, err := json.Marshal(&bucket)
	if err != nil {
		return err
	}
	return s.setNoSync(key, newData)
}

// ==================== Lifecycle ====================

// Close shuts down the Pebble store gracefully.
func (s *PebbleStore) Close() error {
	s.ready.Store(false)
	close(s.stopCh)
	s.walSyncWG.Wait()
	s.logger.Info("Closing Pebble metadata store")
	if s.walDirty.Swap(false) {
		if err := s.db.LogData(nil, pebble.Sync); err != nil {
			s.logger.WithError(err).Warn("Final WAL sync on close failed")
		}
	}
	err := s.db.Close()
	if err == nil {
		// Mark the shutdown clean so the next open skips reconciliation.
		sentinel := filepath.Join(s.dbPath, cleanShutdownSentinelFile)
		if wErr := os.WriteFile(sentinel, []byte("clean\n"), 0644); wErr != nil {
			s.logger.WithError(wErr).Warn("Failed to write clean-shutdown sentinel")
		}
	}
	return err
}

// IsReady returns true when the store is ready to serve requests.
func (s *PebbleStore) IsReady() bool {
	return s.ready.Load()
}

// Compact triggers a manual compaction of the entire keyspace.
func (s *PebbleStore) Compact(ctx context.Context) error {
	s.logger.Info("Starting Pebble manual compaction")
	return s.db.Compact(ctx, []byte{0x00}, []byte{0xFF}, true)
}

// Backup creates a Pebble checkpoint (hard-linked snapshot) at the given path.
func (s *PebbleStore) Backup(ctx context.Context, path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid backup path: %w", err)
	}
	s.logger.WithField("path", absPath).Info("Creating Pebble checkpoint")
	return s.db.Checkpoint(absPath)
}

// ==================== Raw Key-Value Operations ====================
// These methods provide direct key-value access for subsystems such as cluster sync.

var ErrPebbleNotFound = ErrNotFound // alias so callers need not change

// GetRaw retrieves a raw value by key.
func (s *PebbleStore) GetRaw(ctx context.Context, key string) ([]byte, error) {
	data, err := s.pebbleGet([]byte(key))
	if err == pebble.ErrNotFound {
		return nil, ErrNotFound
	}
	return data, err
}

// PutRaw stores a raw value.
func (s *PebbleStore) PutRaw(ctx context.Context, key string, value []byte) error {
	return s.setNoSync([]byte(key), value)
}

// DeleteRaw deletes a raw key. Deletes are rare and synced — a lost raw
// tombstone (e.g. an ACL removal) would silently resurrect on a hard kill.
func (s *PebbleStore) DeleteRaw(ctx context.Context, key string) error {
	err := s.db.Delete([]byte(key), pebble.Sync)
	if err == pebble.ErrNotFound {
		return ErrNotFound
	}
	return err
}

// ==================== Logger adapter ====================

// pebbleLogger adapts logrus to pebble's Logger interface (Infof + Errorf + Fatalf).
type pebbleLogger struct {
	logger *logrus.Logger
}

func (l *pebbleLogger) Infof(format string, args ...interface{}) {
	l.logger.Debugf("[Pebble] "+format, args...)
}

func (l *pebbleLogger) Errorf(format string, args ...interface{}) {
	l.logger.Errorf("[Pebble] "+format, args...)
}

func (l *pebbleLogger) Fatalf(format string, args ...interface{}) {
	l.logger.Fatalf("[Pebble] "+format, args...)
}

// ==================== RawKVStore implementation ====================

// RawBatch applies writes and deletes atomically via a Pebble batch.
func (s *PebbleStore) RawBatch(ctx context.Context, sets map[string][]byte, deletes []string) error {
	batch := s.db.NewBatch()
	defer batch.Close() //nolint:errcheck

	for k, v := range sets {
		if err := batch.Set([]byte(k), v, nil); err != nil {
			return fmt.Errorf("batch set %q: %w", k, err)
		}
	}
	for _, k := range deletes {
		if err := batch.Delete([]byte(k), nil); err != nil {
			return fmt.Errorf("batch delete %q: %w", k, err)
		}
	}
	return s.commitNoSync(batch)
}

// RawScan iterates keys with the given prefix starting from startKey.
// fn receives copies; returning false stops the scan.
func (s *PebbleStore) RawScan(ctx context.Context, prefix, startKey string, fn func(key string, val []byte) bool) error {
	lower := []byte(prefix)
	upper := prefixEnd(lower)

	var seekKey []byte
	if startKey != "" && startKey >= prefix {
		seekKey = []byte(startKey)
	} else {
		seekKey = lower
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: lower,
		UpperBound: upper,
	})
	if err != nil {
		return fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close() //nolint:errcheck

	for valid := iter.SeekGE(seekKey); valid; valid = iter.Next() {
		keyCopy := string(iter.Key())
		val := iter.Value()
		valCopy := make([]byte, len(val))
		copy(valCopy, val)
		if !fn(keyCopy, valCopy) {
			break
		}
	}
	return iter.Error()
}

// RawGC is a no-op for Pebble (it auto-compacts).
func (s *PebbleStore) RawGC() error { return nil }

// compile-time interface checks
var _ Store = (*PebbleStore)(nil)
var _ RawKVStore = (*PebbleStore)(nil)
var _ io.Closer = (*PebbleStore)(nil)
