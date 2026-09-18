package object

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/recovery"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/require"
)

const faultOld = "original acknowledged object"
const faultNew = "replacement object with a different size"

type faultPutBackend struct {
	storage.Backend
	before, after func()
	err           error
}

func (b *faultPutBackend) Put(ctx context.Context, ref storage.ObjectRef, r io.Reader, m map[string]string) error {
	if b.before != nil {
		b.before()
	}
	if err := b.Backend.Put(ctx, ref, r, m); err != nil {
		return err
	}
	if b.after != nil {
		b.after()
	}
	return b.err
}

type faultCommitStore struct {
	metadata.Store
	before, after func()
}

func (s *faultCommitStore) PutObject(ctx context.Context, o *metadata.ObjectMetadata) error {
	if s.before != nil {
		s.before()
	}
	if err := s.Store.PutObject(ctx, o); err != nil {
		return err
	}
	if s.after != nil {
		s.after()
	}
	return nil
}

func openFaultManager(t *testing.T, root string) (*objectManager, *metadata.PebbleStore) {
	t.Helper()
	b, err := storage.NewFilesystemBackend(storage.Config{Root: filepath.Join(root, "objects")})
	require.NoError(t, err)
	s, err := metadata.NewPebbleStore(metadata.PebbleOptions{DataDir: root, CacheSizeMB: 8})
	require.NoError(t, err)
	m := NewManager(b, s, config.StorageConfig{Root: filepath.Join(root, "objects"), EncryptionKey: envelopeTestKey}).(*objectManager)
	return m, s
}

func TestFaultCrashChild(t *testing.T) {
	root := os.Getenv("MAXIOFS_FAULT_CHILD_ROOT")
	if root == "" {
		t.Skip("subprocess helper")
	}
	m, s := openFaultManager(t, root)
	defer s.Close()
	pause := func() {
		require.NoError(t, os.WriteFile(filepath.Join(root, "ready"), []byte("ready"), 0600))
		for {
			time.Sleep(time.Second)
		}
	}
	switch os.Getenv("MAXIOFS_FAULT_PHASE") {
	case "before-data":
		m.storage = &faultPutBackend{Backend: m.storage, before: pause}
	case "after-data":
		m.storage = &faultPutBackend{Backend: m.storage, after: pause}
	case "before-metadata":
		m.metadataStore = &faultCommitStore{Store: s, before: pause}
	case "after-metadata":
		m.metadataStore = &faultCommitStore{Store: s, after: pause}
	default:
		t.Fatal("unknown phase")
	}
	if os.Getenv("MAXIOFS_FAULT_MULTIPART") == "true" {
		u, err := m.CreateMultipartUpload(t.Context(), "fault", "key", http.Header{})
		require.NoError(t, err)
		p, err := m.UploadPart(t.Context(), u.UploadID, 1, strings.NewReader(faultNew))
		require.NoError(t, err)
		_, err = m.CompleteMultipartUpload(t.Context(), u.UploadID, []Part{*p})
		require.NoError(t, err)
	} else {
		_, err := m.PutObject(t.Context(), "fault", "key", strings.NewReader(faultNew), http.Header{})
		require.NoError(t, err)
	}
	t.Fatal("did not reach fault boundary")
}

func TestFaultKilledOverwriteRecovery(t *testing.T) {
	for _, multipart := range []bool{false, true} {
		for _, phase := range []string{"before-data", "after-data", "before-metadata", "after-metadata"} {
			t.Run(fmt.Sprintf("multipart=%v/%s", multipart, phase), func(t *testing.T) {
				root := t.TempDir()
				m, s := openFaultManager(t, root)
				require.NoError(t, m.storage.CreateBucket(t.Context(), "fault"))
				require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "fault", OwnerID: "owner"}))
				old, err := m.PutObject(t.Context(), "fault", "key", strings.NewReader(faultOld), http.Header{})
				require.NoError(t, err)
				require.NoError(t, s.Close())
				cmd := exec.Command(os.Args[0], "-test.run=^TestFaultCrashChild$", "-test.timeout=60s")
				cmd.Env = append(os.Environ(), "MAXIOFS_FAULT_CHILD_ROOT="+root, "MAXIOFS_FAULT_PHASE="+phase, fmt.Sprintf("MAXIOFS_FAULT_MULTIPART=%v", multipart))
				var output bytes.Buffer
				cmd.Stdout, cmd.Stderr = &output, &output
				require.NoError(t, cmd.Start())
				t.Cleanup(func() {
					if cmd.ProcessState == nil {
						_ = cmd.Process.Kill()
						_ = cmd.Wait()
					}
				})
				deadline := time.Now().Add(30 * time.Second)
				for {
					if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
						break
					}
					if time.Now().After(deadline) {
						_ = cmd.Process.Kill()
						_ = cmd.Wait()
						t.Fatalf("child never reached boundary: %s", output.String())
					}
					time.Sleep(10 * time.Millisecond)
				}
				require.NoError(t, cmd.Process.Kill())
				require.Error(t, cmd.Wait())
				m, s = openFaultManager(t, root)
				defer s.Close()
				require.False(t, s.WasCleanShutdown())
				report, err := recovery.Reconcile(t.Context(), root, s, nil)
				require.NoError(t, err)
				require.Equal(t, 1, report.Buckets)
				require.Empty(t, report.Failures)
				o, r, err := m.GetObject(t.Context(), "fault", "key")
				require.NoError(t, err)
				defer r.Close()
				data, err := io.ReadAll(r)
				require.NoError(t, err)
				require.Contains(t, []string{faultOld, faultNew}, string(data))
				require.Equal(t, int64(len(data)), o.Size, "metadata and recovered bytes disagree")
				etag := fmt.Sprintf("%x", md5.Sum(data))
				if multipart && string(data) == faultNew {
					sum := md5.Sum(data)
					etag = fmt.Sprintf("%x-1", md5.Sum(sum[:]))
				}
				require.Equal(t, etag, o.ETag)
				if phase == "before-data" {
					require.Equal(t, old.ETag, o.ETag)
				}
			})
		}
	}
}

func TestFaultMultipartPostPublishIOError(t *testing.T) {
	m, b, s := setupManagerWithConfigKey(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "fault", OwnerID: "owner"}))
	_, err := m.PutObject(t.Context(), "fault", "key", strings.NewReader(faultOld), http.Header{})
	require.NoError(t, err)
	u, err := m.CreateMultipartUpload(t.Context(), "fault", "key", http.Header{})
	require.NoError(t, err)
	p, err := m.UploadPart(t.Context(), u.UploadID, 1, strings.NewReader(faultNew))
	require.NoError(t, err)
	m.storage = &faultPutBackend{Backend: b, err: syscall.ENOSPC}
	_, err = m.CompleteMultipartUpload(t.Context(), u.UploadID, []Part{*p})
	require.ErrorIs(t, err, syscall.ENOSPC)
	m.storage = b
	_, r, err := m.GetObject(t.Context(), "fault", "key")
	require.NoError(t, err)
	defer r.Close()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, faultOld, string(data))
}

func TestFaultBackupCreationFailure(t *testing.T) {
	m, _, s := setupManagerWithConfigKey(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "fault", OwnerID: "owner"}))
	_, err := m.PutObject(t.Context(), "fault", "key", strings.NewReader(faultOld), http.Header{})
	require.NoError(t, err)
	m.config.Root = filepath.Join(t.TempDir(), "missing")
	_, err = m.PutObject(t.Context(), "fault", "key", strings.NewReader(faultNew), http.Header{})
	require.Error(t, err)
	_, r, err := m.GetObject(t.Context(), "fault", "key")
	require.NoError(t, err)
	defer r.Close()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, faultOld, string(data))
}

type faultBackupReader struct {
	io.ReadCloser
	blockManifest func()
}

func (r *faultBackupReader) Read(p []byte) (int, error) {
	if r.blockManifest != nil {
		r.blockManifest()
		r.blockManifest = nil
	}
	return r.ReadCloser.Read(p)
}

type faultBackupBackend struct {
	storage.Backend
	blockManifest func()
}

func (b *faultBackupBackend) Get(ctx context.Context, ref storage.ObjectRef) (io.ReadCloser, map[string]string, error) {
	r, m, err := b.Backend.Get(ctx, ref)
	if err != nil {
		return nil, nil, err
	}
	return &faultBackupReader{ReadCloser: r, blockManifest: b.blockManifest}, m, nil
}

func TestFaultManifestCreationFailure(t *testing.T) {
	m, b, s := setupManagerWithConfigKey(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "fault", OwnerID: "owner"}))
	_, err := m.PutObject(t.Context(), "fault", "key", strings.NewReader(faultOld), http.Header{})
	require.NoError(t, err)
	m.storage = &faultBackupBackend{Backend: b, blockManifest: func() {
		files, err := filepath.Glob(filepath.Join(m.config.Root, "maxiofs-mpu-backup-*"))
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.NoError(t, os.Mkdir(files[0]+".json", 0700))
	}}
	_, err = m.PutObject(t.Context(), "fault", "key", strings.NewReader(faultNew), http.Header{})
	require.ErrorContains(t, err, "manifest")
	m.storage = b
	_, r, err := m.GetObject(t.Context(), "fault", "key")
	require.NoError(t, err)
	defer r.Close()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, faultOld, string(data))
}

type faultPartStore struct{ metadata.Store }

func (s *faultPartStore) PutPart(context.Context, *metadata.PartMetadata) error {
	return syscall.ENOSPC
}

func TestFaultPartMetadataFailurePreservesPriorPart(t *testing.T) {
	m, b, s := setupManagerWithConfigKey(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "fault", OwnerID: "owner"}))
	u, err := m.CreateMultipartUpload(t.Context(), "fault", "key", http.Header{})
	require.NoError(t, err)
	old, err := m.UploadPart(t.Context(), u.UploadID, 1, strings.NewReader(faultOld))
	require.NoError(t, err)
	m.metadataStore = &faultPartStore{Store: s}
	_, err = m.UploadPart(t.Context(), u.UploadID, 1, strings.NewReader(faultNew))
	require.ErrorIs(t, err, syscall.ENOSPC)
	parts, err := m.ListParts(t.Context(), u.UploadID)
	require.NoError(t, err)
	require.Len(t, parts, 1)
	require.Equal(t, old.ETag, parts[0].ETag)
	r, _, err := b.GetPart(t.Context(), u.UploadID, 1)
	require.NoError(t, err, "ListParts must not claim an acknowledged part that is missing on disk")
	defer r.Close()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, faultOld, string(data))
}

func TestFaultOverwriteCost(t *testing.T) {
	if os.Getenv("MAXIOFS_BENCH_OVERWRITE") == "" {
		t.Skip("set MAXIOFS_BENCH_OVERWRITE=1 to measure overwrite cost")
	}
	m, b, s := setupManagerWithConfigKey(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "fault", OwnerID: "owner"}))
	data := bytes.Repeat([]byte("x"), 64<<20)
	var fresh, overwrite time.Duration
	var backupSize int64
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("key-%d", i)
		m.storage = b
		start := time.Now()
		_, err := m.PutObject(t.Context(), "fault", key, bytes.NewReader(data), http.Header{})
		fresh += time.Since(start)
		require.NoError(t, err)
		m.storage = &faultPutBackend{Backend: b, before: func() {
			files, err := filepath.Glob(filepath.Join(m.config.Root, "maxiofs-mpu-backup-*"))
			require.NoError(t, err)
			for _, path := range files {
				if strings.HasSuffix(path, ".json") {
					continue
				}
				info, err := os.Stat(path)
				require.NoError(t, err)
				backupSize = info.Size()
			}
		}}
		start = time.Now()
		_, err = m.PutObject(t.Context(), "fault", key, bytes.NewReader(data), http.Header{})
		overwrite += time.Since(start)
		require.NoError(t, err)
	}
	t.Logf("64 MiB, 3 samples: create mean=%s overwrite mean=%s ratio=%.2f retained backup bytes=%d", fresh/3, overwrite/3, float64(overwrite)/float64(fresh), backupSize)
	require.GreaterOrEqual(t, backupSize, int64(len(data)))
}
