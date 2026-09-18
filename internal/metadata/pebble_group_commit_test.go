package metadata

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/vfs"
	dto "github.com/prometheus/client_model/go"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type walSyncGate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	err     error
}

type gatedWALFS struct {
	vfs.FS
	gate atomic.Pointer[walSyncGate]
}

func (f *gatedWALFS) Create(name string, category vfs.DiskWriteCategory) (vfs.File, error) {
	file, err := f.FS.Create(name, category)
	if err == nil && strings.HasSuffix(name, ".log") {
		return &gatedWALFile{File: file, fs: f}, nil
	}
	return file, err
}

type gatedWALFile struct {
	vfs.File
	fs *gatedWALFS
}

func (f *gatedWALFile) Sync() error {
	if gate := f.fs.gate.Load(); gate != nil {
		gate.once.Do(func() { close(gate.entered) })
		<-gate.release
		if gate.err != nil {
			return gate.err
		}
	}
	return f.File.Sync()
}

func (f *gatedWALFile) SyncData() error { return f.Sync() }

func TestDurableWritesReleaseBucketBeforeWALSync(t *testing.T) {
	for _, kind := range []string{"object", "version", "part"} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fail=%t", kind, fail), func(t *testing.T) {
				if fail && os.Getenv("MAXIOFS_TEST_WAL_FAILURE") == "" {
					cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestDurableWritesReleaseBucketBeforeWALSync$/^"+kind+"$/^fail=true$", "-test.timeout=20s")
					cmd.Env = append(os.Environ(), "MAXIOFS_TEST_WAL_FAILURE=1")
					output, err := cmd.CombinedOutput()
					require.Error(t, err, "Pebble must fail-stop on a WAL sync failure")
					require.Contains(t, string(output), "fatal commit error: injected WAL sync failure")
					return
				}
				fs := &gatedWALFS{FS: vfs.NewMem()}
				db, err := pebble.Open("db", &pebble.Options{FS: fs})
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				logger := logrus.New()
				logger.SetOutput(io.Discard)
				s := &PebbleStore{db: db, logger: logger}
				require.NoError(t, s.CreateBucket(t.Context(), &BucketMetadata{Name: "bucket"}))
				require.NoError(t, s.CreateMultipartUpload(t.Context(), &MultipartUploadMetadata{UploadID: "upload", Bucket: "bucket", Key: "key"}))
				gate := &walSyncGate{entered: make(chan struct{}), release: make(chan struct{})}
				if fail {
					gate.err = errors.New("injected WAL sync failure")
				}
				fs.gate.Store(gate)
				var once sync.Once
				release := func() { once.Do(func() { close(gate.release) }) }
				var wg sync.WaitGroup
				t.Cleanup(func() { release(); wg.Wait(); fs.gate.Store(nil) })
				results := make(chan error, 2)
				write := func(n int) {
					wg.Add(1)
					go func() {
						defer wg.Done()
						obj := &ObjectMetadata{Bucket: "bucket", Key: fmt.Sprint(n), Size: 32, ETag: "etag"}
						var err error
						switch kind {
						case "object":
							err = s.PutObject(t.Context(), obj)
						case "version":
							err = s.PutObjectVersion(t.Context(), obj, &ObjectVersion{VersionID: "v1", IsLatest: true})
						case "part":
							err = s.PutPart(t.Context(), &PartMetadata{UploadID: "upload", PartNumber: n, Size: 32, ETag: "etag"})
						}
						results <- err
					}()
				}
				write(1)
				select {
				case <-gate.entered:
				case <-time.After(5 * time.Second):
					t.Fatal("WAL sync did not start")
				}
				write(2)
				require.Eventually(t, func() bool {
					if kind == "part" {
						_, err := s.GetPart(t.Context(), "upload", 2)
						return err == nil
					}
					_, err := s.GetObject(t.Context(), "bucket", "2")
					return err == nil
				}, 5*time.Second, time.Millisecond, "another write in the same bucket must publish while sync waits")
				select {
				case err := <-results:
					t.Fatalf("write returned before WAL sync: %v", err)
				default:
				}
				if kind != "part" {
					require.ErrorIs(t, s.DeleteBucketIfEmpty(t.Context(), "", "bucket"), ErrBucketNotEmpty)
				}
				release()
				for range 2 {
					select {
					case err := <-results:
						if fail {
							require.ErrorIs(t, err, gate.err)
						} else {
							require.NoError(t, err)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("write did not finish after WAL sync")
					}
				}
			})
		}
	}
}

func walSyncCount(b *testing.B, s *PebbleStore) uint64 {
	b.Helper()
	var m dto.Metric
	if err := s.db.Metrics().LogWriter.FsyncLatency.Write(&m); err != nil {
		b.Fatal(err)
	}
	return m.GetHistogram().GetSampleCount()
}

func BenchmarkDurableMetadata(b *testing.B) {
	for _, kind := range []string{"object", "version", "part"} {
		b.Run(kind, func(b *testing.B) {
			logger := logrus.New()
			logger.SetOutput(io.Discard)
			s, err := NewPebbleStore(PebbleOptions{DataDir: b.TempDir(), Logger: logger, WALSyncInterval: -1})
			if err != nil {
				b.Fatal(err)
			}
			defer s.Close()
			if err := s.CreateBucket(b.Context(), &BucketMetadata{Name: "bench"}); err != nil {
				b.Fatal(err)
			}
			if err := s.CreateMultipartUpload(b.Context(), &MultipartUploadMetadata{UploadID: "upload", Bucket: "bench", Key: "key"}); err != nil {
				b.Fatal(err)
			}
			before := walSyncCount(b, s)
			var next atomic.Int64
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					n := next.Add(1)
					obj := &ObjectMetadata{Bucket: "bench", Key: fmt.Sprint(n), Size: 32, ETag: "etag"}
					var err error
					switch kind {
					case "object":
						err = s.PutObject(b.Context(), obj)
					case "version":
						err = s.PutObjectVersion(b.Context(), obj, &ObjectVersion{VersionID: "v1", IsLatest: true})
					case "part":
						err = s.PutPart(b.Context(), &PartMetadata{UploadID: "upload", PartNumber: int(n), Size: 32, ETag: "etag"})
					}
					if err != nil {
						b.Error(err)
						return
					}
				}
			})
			b.StopTimer()
			b.ReportMetric(float64(walSyncCount(b, s)-before)/float64(b.N), "WAL-sync/op")
		})
	}
}
