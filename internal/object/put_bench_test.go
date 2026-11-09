package object

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/kek"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// BenchmarkPutObject measures a PUT through the manager, encrypted as every
// object is: its rate and the most memory its objects held while it ran.
func BenchmarkPutObject(b *testing.B) {
	formerLevel := logrus.GetLevel()
	logrus.SetLevel(logrus.WarnLevel)
	defer logrus.SetLevel(formerLevel)
	for _, size := range []int{1 << 20, 64 << 20, 256 << 20} {
		b.Run(fmt.Sprintf("%dMiB", size>>20), func(b *testing.B) {
			om := benchManager(b)
			ctx := context.Background()
			block := make([]byte, 1<<20)
			_, _ = rand.Read(block)

			runtime.GC()
			base := heapNow()
			peak, stop := heapPeak()
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := om.PutObject(ctx, "bench", fmt.Sprintf("k%d", i), &cycle{block: block, left: int64(size)}, http.Header{}); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			stop()
			b.ReportMetric(float64(peak.Load()-base)/(1<<20), "peak-heap-MiB")
		})
	}
}

// cycle gives left bytes of block over and over, holding no more than block.
type cycle struct {
	block []byte
	off   int
	left  int64
}

func (c *cycle) Read(p []byte) (int, error) {
	if c.left == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n := copy(p, c.block[c.off:])
	c.off = (c.off + n) % len(c.block)
	c.left -= int64(n)
	return n, nil
}

func heapNow() int64 {
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(sample)
	return int64(sample[0].Value.Uint64())
}

// heapPeak samples the bytes held by heap objects until stop is called.
func heapPeak() (*atomic.Int64, func()) {
	var peak atomic.Int64
	done := make(chan struct{})
	sample := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	go func() {
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				metrics.Read(sample)
				if v := int64(sample[0].Value.Uint64()); v > peak.Load() {
					peak.Store(v)
				}
			}
		}
	}()
	return &peak, func() { close(done) }
}

func benchManager(b *testing.B) *objectManager {
	b.Helper()
	dir := b.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "kek.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE encryption_keys (version INTEGER PRIMARY KEY, key_hex TEXT NOT NULL,
		is_current INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL, cluster_shared INTEGER NOT NULL DEFAULT 0)`); err != nil {
		b.Fatal(err)
	}
	keys, err := kek.Bootstrap(db, "")
	if err != nil {
		b.Fatal(err)
	}
	root := filepath.Join(dir, "objects")
	backend, err := storage.NewFilesystemBackend(storage.Config{Root: root})
	if err != nil {
		b.Fatal(err)
	}
	logger := logrus.New()
	logger.SetLevel(logrus.WarnLevel)
	store, err := metadata.NewPebbleStore(metadata.PebbleOptions{DataDir: dir, Logger: logger})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { store.Close() })
	ctx := context.Background()
	if err := store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "bench", OwnerID: "admin"}); err != nil {
		b.Fatal(err)
	}
	if err := backend.CreateBucket(ctx, "bench"); err != nil {
		b.Fatal(err)
	}
	return NewManager(backend, store, config.StorageConfig{Backend: "filesystem", Root: root}, WithKEKProvider(keys)).(*objectManager)
}
