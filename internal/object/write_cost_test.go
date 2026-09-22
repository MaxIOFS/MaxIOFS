package object

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/rollback"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/require"
)

type writeCostCounters struct {
	backendReads, backendWrites, backendChecks, sidecarReads           atomic.Int64
	backupReadBytes, backendWriteBytes                                 atomic.Int64
	bucketReads, objectReads, partReads, uploadReads, commits, metrics atomic.Int64
}

type costReader struct {
	io.Reader
	count *atomic.Int64
}

func (r *costReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.count.Add(int64(n))
	return n, err
}

func (r *costReader) WriteTo(w io.Writer) (int64, error) {
	// Preserve os.File's copy fast path when counting backup bytes.
	if wt, ok := r.Reader.(io.WriterTo); ok {
		n, err := wt.WriteTo(w)
		r.count.Add(n)
		return n, err
	}
	return io.Copy(w, struct{ io.Reader }{r})
}

type costReadCloser struct {
	*costReader
	io.Closer
}

type costBackend struct {
	storage.Backend
	c *writeCostCounters
}

func (b *costBackend) Put(ctx context.Context, ref storage.ObjectRef, r io.Reader, m map[string]string) error {
	b.c.backendWrites.Add(1)
	return b.Backend.Put(ctx, ref, &costReader{r, &b.c.backendWriteBytes}, m)
}

func (b *costBackend) Get(ctx context.Context, ref storage.ObjectRef) (io.ReadCloser, map[string]string, error) {
	b.c.backendReads.Add(1)
	r, m, err := b.Backend.Get(ctx, ref)
	if err != nil {
		return r, m, err
	}
	return &costReadCloser{&costReader{r, &b.c.backupReadBytes}, r}, m, nil
}

func (b *costBackend) Exists(ctx context.Context, ref storage.ObjectRef) (bool, error) {
	b.c.backendChecks.Add(1)
	return b.Backend.Exists(ctx, ref)
}

func (b *costBackend) GetMetadata(ctx context.Context, ref storage.ObjectRef) (map[string]string, error) {
	b.c.sidecarReads.Add(1)
	return b.Backend.GetMetadata(ctx, ref)
}

func (b *costBackend) PutPart(ctx context.Context, id string, n int, r io.Reader, m map[string]string) error {
	b.c.backendWrites.Add(1)
	return b.Backend.PutPart(ctx, id, n, &costReader{r, &b.c.backendWriteBytes}, m)
}

func (b *costBackend) GetPart(ctx context.Context, id string, n int) (io.ReadCloser, map[string]string, error) {
	b.c.backendReads.Add(1)
	r, m, err := b.Backend.GetPart(ctx, id, n)
	if err != nil {
		return r, m, err
	}
	return &costReadCloser{&costReader{r, &b.c.backupReadBytes}, r}, m, nil
}

func (b *costBackend) PartMetadata(ctx context.Context, id string, n int) (map[string]string, error) {
	b.c.sidecarReads.Add(1)
	return b.Backend.PartMetadata(ctx, id, n)
}

type costStore struct {
	metadata.Store
	c *writeCostCounters
}

func (s *costStore) GetBucket(ctx context.Context, tenant, name string) (*metadata.BucketMetadata, error) {
	s.c.bucketReads.Add(1)
	return s.Store.GetBucket(ctx, tenant, name)
}

func (s *costStore) GetObject(ctx context.Context, bucket, key string, version ...string) (*metadata.ObjectMetadata, error) {
	s.c.objectReads.Add(1)
	return s.Store.GetObject(ctx, bucket, key, version...)
}

func (s *costStore) GetPart(ctx context.Context, id string, n int) (*metadata.PartMetadata, error) {
	s.c.partReads.Add(1)
	return s.Store.GetPart(ctx, id, n)
}

func (s *costStore) GetMultipartUpload(ctx context.Context, id string) (*metadata.MultipartUploadMetadata, error) {
	s.c.uploadReads.Add(1)
	return s.Store.GetMultipartUpload(ctx, id)
}

func (s *costStore) PutObject(ctx context.Context, obj *metadata.ObjectMetadata) error {
	s.c.commits.Add(1)
	return s.Store.PutObject(ctx, obj)
}

func (s *costStore) PutObjectVersion(ctx context.Context, obj *metadata.ObjectMetadata, v *metadata.ObjectVersion) error {
	s.c.commits.Add(1)
	return s.Store.PutObjectVersion(ctx, obj, v)
}

func (s *costStore) PutPart(ctx context.Context, p *metadata.PartMetadata) error {
	s.c.commits.Add(1)
	return s.Store.PutPart(ctx, p)
}

func (s *costStore) UpdateBucketMetrics(ctx context.Context, tenant, name string, count, size int64) error {
	s.c.metrics.Add(1)
	return s.Store.UpdateBucketMetrics(ctx, tenant, name, count, size)
}

func runWriteCost(t *testing.T, mode string, size, clients, operations int) {
	t.Helper()
	root := t.TempDir()
	if parent := os.Getenv("MAXIOFS_COST_DIR"); parent != "" {
		var err error
		root, err = os.MkdirTemp(parent, "maxiofs-write-cost-")
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, os.RemoveAll(root)) })
	}
	objectsRoot := filepath.Join(root, "objects")
	b, err := storage.NewFilesystemBackend(storage.Config{Root: objectsRoot})
	require.NoError(t, err)
	s, err := metadata.NewPebbleStore(metadata.PebbleOptions{DataDir: root, CacheSizeMB: 8})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	bm := &metadata.BucketMetadata{Name: "cost", OwnerID: "owner"}
	if mode == "versioned" {
		bm.Versioning = &metadata.VersioningMetadata{Status: "Enabled"}
	}
	require.NoError(t, s.CreateBucket(t.Context(), bm))
	require.NoError(t, b.CreateBucket(t.Context(), "cost"))
	m := NewManager(b, s, config.StorageConfig{Root: objectsRoot, EncryptionKey: envelopeTestKey}).(*objectManager)
	m.SetBucketManager(bucket.NewManager(b, s))
	part := strings.HasPrefix(mode, "part-")
	replace := mode == "overwrite" || mode == "part-replace" || mode == "versioned"
	uploadID := ""
	if part {
		u, err := m.CreateMultipartUpload(t.Context(), "cost", "multipart", http.Header{})
		require.NoError(t, err)
		uploadID = u.UploadID
	}
	old := bytes.Repeat([]byte{0x31}, size)
	if replace {
		for i := range clients {
			if part {
				_, err = m.UploadPart(t.Context(), uploadID, i+1, bytes.NewReader(old))
			} else {
				_, err = m.PutObject(t.Context(), "cost", fmt.Sprint(i), bytes.NewReader(old), http.Header{})
			}
			require.NoError(t, err)
		}
	}
	c := &writeCostCounters{}
	backend, store := &costBackend{b, c}, &costStore{s, c}
	m.storage, m.metadataStore = backend, store
	m.SetBucketManager(bucket.NewManager(backend, store))
	payload := bytes.Repeat([]byte{0x72}, size)
	latencies := make([]time.Duration, operations)
	errors := make([]error, operations)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for client := range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := client; i < operations; i += clients {
				key := i
				if replace {
					key = client
				}
				t0 := time.Now()
				if part {
					_, errors[i] = m.UploadPart(t.Context(), uploadID, key+1, bytes.NewReader(payload))
				} else {
					_, errors[i] = m.PutObject(t.Context(), "cost", fmt.Sprint(key), bytes.NewReader(payload), http.Header{})
				}
				latencies[i] = time.Since(t0)
			}
		}()
	}
	t0 := time.Now()
	close(start)
	wg.Wait()
	elapsed := time.Since(t0)
	for _, err := range errors {
		require.NoError(t, err)
	}
	denom := float64(operations)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	report := map[string]any{
		"mode": mode, "bytes": size, "clients": clients, "operations": operations,
		"os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version(), "gomaxprocs": runtime.GOMAXPROCS(0),
		"local_ops_per_second":         denom / elapsed.Seconds(),
		"local_p50_ms":                 float64(latencies[(operations-1)/2]) / float64(time.Millisecond),
		"local_p95_ms":                 float64(latencies[(operations*95+99)/100-1]) / float64(time.Millisecond),
		"backend_get_per_op":           float64(c.backendReads.Load()) / denom,
		"backend_put_per_op":           float64(c.backendWrites.Load()) / denom,
		"backend_exists_per_op":        float64(c.backendChecks.Load()) / denom,
		"backend_metadata_per_op":      float64(c.sidecarReads.Load()) / denom,
		"backup_read_bytes_per_op":     float64(c.backupReadBytes.Load()) / denom,
		"backend_write_bytes_per_op":   float64(c.backendWriteBytes.Load()) / denom,
		"metadata_bucket_get_per_op":   float64(c.bucketReads.Load()) / denom,
		"metadata_object_get_per_op":   float64(c.objectReads.Load()) / denom,
		"metadata_part_get_per_op":     float64(c.partReads.Load()) / denom,
		"metadata_upload_get_per_op":   float64(c.uploadReads.Load()) / denom,
		"metadata_commit_per_op":       float64(c.commits.Load()) / denom,
		"bucket_metrics_update_per_op": float64(c.metrics.Load()) / denom,
	}
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	t.Logf("WRITE_COST %s", encoded)
	require.Equal(t, int64(operations), c.backendWrites.Load())
	require.Equal(t, int64(operations), c.commits.Load())
	if mode == "overwrite" || mode == "part-replace" {
		require.GreaterOrEqual(t, c.backupReadBytes.Load(), int64(size)*int64(operations))
	} else {
		require.Zero(t, c.backupReadBytes.Load())
	}
	// Validation is outside both the counters and the timed interval.
	m.storage, m.metadataStore = b, s
	keys := operations
	if replace {
		keys = clients
	}
	for i := range keys {
		var r io.ReadCloser
		if part {
			r, _, err = b.GetPart(t.Context(), uploadID, i+1)
		} else {
			_, r, err = m.GetObject(t.Context(), "cost", fmt.Sprint(i))
		}
		require.NoError(t, err)
		data, readErr := io.ReadAll(r)
		closeErr := r.Close()
		require.NoError(t, readErr)
		require.NoError(t, closeErr)
		require.Equal(t, payload, data)
	}
	files, err := os.ReadDir(objectsRoot)
	require.NoError(t, err)
	for _, f := range files {
		require.False(t, strings.HasPrefix(f.Name(), rollback.ObjectPrefix) || strings.HasPrefix(f.Name(), rollback.PartPrefix), "leftover backup: %s", f.Name())
	}
}

func TestWriteCostAccounting(t *testing.T) {
	for _, mode := range []string{"new", "overwrite", "versioned", "part-new", "part-replace"} {
		t.Run(mode, func(t *testing.T) { runWriteCost(t, mode, 4096, 2, 4) })
	}
}

func TestWriteCostCustomDirectory(t *testing.T) {
	parent := t.TempDir()
	t.Setenv("MAXIOFS_COST_DIR", parent)
	t.Run("workload", func(t *testing.T) { runWriteCost(t, "overwrite", 4096, 2, 4) })
	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	require.Empty(t, entries, "only the test-owned directory should have been removed")
}

func TestWritePathCost(t *testing.T) {
	if os.Getenv("MAXIOFS_WRITE_COST") != "1" {
		t.Skip("set MAXIOFS_WRITE_COST=1 to run the disk workload")
	}
	operations := 64
	if value := os.Getenv("MAXIOFS_COST_OPS"); value != "" {
		n, err := strconv.Atoi(value)
		require.NoError(t, err)
		require.GreaterOrEqual(t, n, 32)
		require.LessOrEqual(t, n, 10000)
		operations = n
	}
	for _, mode := range []string{"new", "overwrite", "versioned", "part-new", "part-replace"} {
		for _, size := range []int{4096, 1 << 20} {
			for _, clients := range []int{1, 8, 32} {
				t.Run(fmt.Sprintf("%s/bytes=%d/clients=%d", mode, size, clients), func(t *testing.T) { runWriteCost(t, mode, size, clients, operations) })
			}
		}
	}
}
