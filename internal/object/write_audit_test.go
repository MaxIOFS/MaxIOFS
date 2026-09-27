package object

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/rollback"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/require"
)

func TestWriteAuditPutDefaultRetention(t *testing.T) {
	m, _, s := setupManagerWithConfigKey(t)
	days := 30
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{
		Name: "audit", Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
		ObjectLock: &metadata.ObjectLockMetadata{Enabled: true, Rule: &metadata.ObjectLockRuleMetadata{
			DefaultRetention: &metadata.RetentionMetadata{Mode: RetentionModeCompliance, Days: &days},
		}},
	}))
	m.metadataStore = retentionCommitStore{Store: s}
	o, err := m.PutObject(t.Context(), "audit", "key", strings.NewReader("protected"), http.Header{})
	require.NoError(t, err)
	require.NotNil(t, o.Retention)
	_, deleteErr := m.DeleteObject(t.Context(), "audit", "key", false, o.VersionID)
	t.Logf("retention=%s; delete error=%v", o.Retention.RetainUntilDate, deleteErr)
	require.Error(t, deleteErr, "a just-written COMPLIANCE version must not be deletable")
}

type auditBucketReadFailure struct {
	metadata.Store
	calls int
}

func (s *auditBucketReadFailure) GetBucket(ctx context.Context, tenant, name string) (*metadata.BucketMetadata, error) {
	s.calls++
	if s.calls == 1 {
		return nil, errors.New("injected bucket read failure")
	}
	return s.Store.GetBucket(ctx, tenant, name)
}

func TestWriteAuditBucketReadFailsClosed(t *testing.T) {
	m, _, s := setupManagerWithConfigKey(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{
		Name: "audit", Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
	}))
	m.metadataStore = &auditBucketReadFailure{Store: s}
	o, err := m.PutObject(t.Context(), "audit", "key", strings.NewReader("versioned"), http.Header{})
	if o != nil {
		t.Logf("returned version ID %q", o.VersionID)
	}
	require.Error(t, err, "a bucket lookup failure must not silently disable versioning")
}

type auditPublishBarrier struct {
	storage.Backend
	entered chan struct{}
	resume  chan struct{}
}

func (b *auditPublishBarrier) Put(ctx context.Context, ref storage.ObjectRef, r io.Reader, meta map[string]string) error {
	b.entered <- struct{}{}
	<-b.resume
	return b.Backend.Put(ctx, ref, r, meta)
}

func TestWriteAuditConcurrentQuota(t *testing.T) {
	m, s := setupAccountingManager(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{
		Name: "audit", Quota: &metadata.BucketQuota{MaxSizeBytes: 10, MaxObjectCount: 1},
	}))
	b := &auditPublishBarrier{Backend: m.storage, entered: make(chan struct{}, 2), resume: make(chan struct{})}
	m.storage = b
	done := make(chan error, 2)
	for _, key := range []string{"first", "second"} {
		go func() {
			_, err := m.PutObject(t.Context(), "audit", key, strings.NewReader("12345678"), http.Header{})
			done <- err
		}()
	}
	arrivals := 0
	waiting := true
	var results []error
	for waiting && arrivals < 2 {
		select {
		case <-b.entered:
			arrivals++
		case err := <-done:
			results = append(results, err)
			waiting = false
		case <-time.After(5 * time.Second):
			waiting = false
		}
	}
	close(b.resume)
	var successes int
	for len(results) < 2 {
		results = append(results, <-done)
	}
	for _, err := range results {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, ErrBucketQuotaExceeded)
		}
	}
	require.Equal(t, 1, successes)
	current, err := s.GetBucket(t.Context(), "", "audit")
	require.NoError(t, err)
	t.Logf("successful writes=%d size=%d count=%d", successes, current.TotalSize, current.ObjectCount)
	require.LessOrEqual(t, current.TotalSize, int64(10), "concurrent writes must respect the bucket quota")
	require.LessOrEqual(t, current.ObjectCount, int64(1))
}

type auditCorruptMigration struct {
	storage.Backend
	writes int
}

func (b *auditCorruptMigration) Put(ctx context.Context, ref storage.ObjectRef, r io.Reader, meta map[string]string) error {
	b.writes++
	if b.writes > 1 {
		return errors.New("injected restore failure")
	}
	if _, err := io.Copy(io.Discard, r); err != nil {
		return err
	}
	return b.Backend.Put(ctx, ref, bytes.NewReader([]byte("corrupt ciphertext")), meta)
}

func TestWriteAuditMigrationRetainsFailedRestore(t *testing.T) {
	m, backend, s := setupManagerWithConfigKey(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "audit"}))
	putPlaintextObject(t, m, s, "audit", "key", []byte("irreplaceable original"))
	b := &auditCorruptMigration{Backend: backend}
	m.storage = b
	_, _, err := m.EncryptExistingObject(t.Context(), "audit", "key")
	require.Error(t, err)
	require.Equal(t, 2, b.writes, "verification must attempt restoration")
	files, err := filepath.Glob(filepath.Join(m.config.Root, rollback.ObjectPrefix+"*"+rollback.ManifestSuffix))
	require.NoError(t, err)
	require.Len(t, files, 1, "failed restoration must retain a recoverable copy")
	m.storage = backend
	report, err := rollback.Undo(t.Context(), m.config.Root, backend, s, nil)
	require.NoError(t, err)
	require.Empty(t, report.Failures)
	require.Equal(t, 1, report.ObjectsRestored)
	_, data := readWholeObject(t, m, "audit", "key")
	require.Equal(t, "irreplaceable original", data)
	requireNoRetainedBackup(t, m)
}
