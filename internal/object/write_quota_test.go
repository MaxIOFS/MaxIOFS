package object

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

// blockingTenantAuth holds every tenant quota check until released — the shape
// of a cluster-wide check waiting on a slow peer — and reports what each check
// was asked to add.
type blockingTenantAuth struct {
	seen    chan int64
	release chan struct{}
}

func (a *blockingTenantAuth) IncrementTenantStorage(context.Context, string, int64) error { return nil }
func (a *blockingTenantAuth) DecrementTenantStorage(context.Context, string, int64) error { return nil }
func (a *blockingTenantAuth) CheckTenantStorageQuota(_ context.Context, _ string, add int64) error {
	a.seen <- add
	<-a.release
	return nil
}

// A tenant quota check waiting on the cluster holds up nobody: not another
// bucket, not another write of the same tenant — and that second check still
// counts the first write's reservation.
func TestWriteQuotaSlowTenantCheckBlocksNobody(t *testing.T) {
	m, _, s := setupManagerWithConfigKey(t)
	ctx := t.Context()
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{Name: "slowtenantdata", TenantID: "slow"}))
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{Name: "globaldata"}))
	auth := &blockingTenantAuth{seen: make(chan int64, 2), release: make(chan struct{})}
	var releaseOnce sync.Once
	releaseChecks := func() { releaseOnce.Do(func() { close(auth.release) }) }
	m.SetAuthManager(auth)

	// Writes run in the background; on any failure they must finish before
	// the store is closed, or they panic on it and hide the result.
	var inflight sync.WaitGroup
	t.Cleanup(func() { releaseChecks(); inflight.Wait() })
	putAsync := func(bucket, key, body string) chan error {
		inflight.Add(1)
		done := make(chan error, 1)
		go func() {
			defer inflight.Done()
			_, err := m.PutObject(context.Background(), bucket, key, strings.NewReader(body), http.Header{})
			done <- err
		}()
		return done
	}

	first := putAsync("slow/slowtenantdata", "a", "xxxx")
	select {
	case add := <-auth.seen:
		require.Equal(t, int64(4), add)
	case <-time.After(5 * time.Second):
		t.Fatal("the first tenant check never started")
	}

	select {
	case err := <-putAsync("globaldata", "k", "y"):
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("a write to an unrelated bucket waited on another tenant's quota check")
	}

	second := putAsync("slow/slowtenantdata", "b", "zz")
	select {
	case add := <-auth.seen:
		require.Equal(t, int64(6), add, "the second check must count the first write's reservation")
	case <-time.After(5 * time.Second):
		t.Fatal("a write of the same tenant waited on another write's quota check")
	}

	releaseChecks()
	require.NoError(t, <-first)
	require.NoError(t, <-second)
}

// slowBucketReads widens the window between reserving and checking, so an
// unguarded check-then-reserve fails every run instead of by luck.
type slowBucketReads struct{ metadata.Store }

func (s slowBucketReads) GetBucket(ctx context.Context, tenant, name string) (*metadata.BucketMetadata, error) {
	time.Sleep(20 * time.Millisecond)
	return s.Store.GetBucket(ctx, tenant, name)
}

func TestWriteQuotaConcurrentWritersNeverOverAdmit(t *testing.T) {
	m, s := setupAccountingManager(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{
		Name: "w", Quota: &metadata.BucketQuota{MaxObjectCount: 3},
	}))
	m.metadataStore = slowBucketReads{Store: s}
	var admitted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := m.PutObject(context.Background(), "w", fmt.Sprintf("k%02d", i), strings.NewReader("abc"), http.Header{}); err == nil {
				admitted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	require.Equal(t, int64(3), admitted.Load(), "writers were admitted against the same free space")

	m.quotaMu.Lock()
	defer m.quotaMu.Unlock()
	require.Empty(t, m.pendingQuotas, "reservations leaked")
	require.Empty(t, m.pendingTenants, "tenant reservations leaked")
}

// Over the limit, nothing that frees space or keeps it is refused: deletes,
// delete markers and overwrites that do not grow the object.
func TestWriteQuotaOverLimitStillAllowsFreeingSpace(t *testing.T) {
	for _, versioned := range []bool{false, true} {
		t.Run(fmt.Sprintf("versioned=%v", versioned), func(t *testing.T) {
			m, s := setupAccountingManager(t)
			b := &metadata.BucketMetadata{Name: "full", Quota: &metadata.BucketQuota{MaxSizeBytes: 10, MaxObjectCount: 1}}
			if versioned {
				b.Versioning = &metadata.VersioningMetadata{Status: "Enabled"}
			}
			require.NoError(t, s.CreateBucket(t.Context(), b))

			// Past both limits: 20 bytes, 2 objects.
			over := WithBypassQuotaEnforcement(t.Context())
			_, err := m.PutObject(over, "full", "a", strings.NewReader("0123456789"), http.Header{})
			require.NoError(t, err)
			second, err := m.PutObject(over, "full", "b", strings.NewReader("0123456789"), http.Header{})
			require.NoError(t, err)

			_, err = m.PutObject(t.Context(), "full", "c", strings.NewReader("x"), http.Header{})
			require.ErrorIs(t, err, ErrBucketQuotaExceeded, "a write that grows usage is refused")

			if !versioned {
				_, err = m.PutObject(t.Context(), "full", "a", strings.NewReader("abc"), http.Header{})
				require.NoError(t, err, "an overwrite that shrinks the object is allowed over the limit")
			}
			_, err = m.DeleteObject(t.Context(), "full", "b", false)
			require.NoError(t, err, "a delete is allowed over the limit")
			if versioned {
				_, err = m.DeleteObject(t.Context(), "full", "b", false, second.VersionID)
				require.NoError(t, err, "deleting a version is allowed over the limit")
			}
		})
	}
}

// A versioned overwrite keeps the version it replaces, so it is charged its
// full size — not the difference, as an in-place overwrite is.
func TestWriteQuotaVersionedOverwriteIsChargedInFull(t *testing.T) {
	m, s := setupAccountingManager(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{
		Name: "vq", Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
		Quota: &metadata.BucketQuota{MaxSizeBytes: 10},
	}))
	_, err := m.PutObject(t.Context(), "vq", "k", strings.NewReader("123456"), http.Header{})
	require.NoError(t, err)
	_, err = m.PutObject(t.Context(), "vq", "k", strings.NewReader("abcdef"), http.Header{})
	require.ErrorIs(t, err, ErrBucketQuotaExceeded, "a second version would take the bucket to 12 bytes")
}

// fullTenant refuses any growth: the tenant is already past its quota.
type fullTenant struct{}

func (fullTenant) IncrementTenantStorage(context.Context, string, int64) error { return nil }
func (fullTenant) DecrementTenantStorage(context.Context, string, int64) error { return nil }
func (fullTenant) CheckTenantStorageQuota(context.Context, string, int64) error {
	return errors.New("tenant over quota")
}

func TestWriteQuotaTenantOverLimitStillAllowsFreeingSpace(t *testing.T) {
	m, s := setupAccountingManager(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "tenantfull", TenantID: "t1"}))
	m.SetAuthManager(fullTenant{})
	_, err := m.PutObject(WithBypassQuotaEnforcement(t.Context()), "t1/tenantfull", "a", strings.NewReader("0123456789"), http.Header{})
	require.NoError(t, err)

	_, err = m.PutObject(t.Context(), "t1/tenantfull", "c", strings.NewReader("x"), http.Header{})
	require.Error(t, err, "a write that grows usage is refused")
	_, err = m.PutObject(t.Context(), "t1/tenantfull", "a", strings.NewReader("abc"), http.Header{})
	require.NoError(t, err, "an overwrite that shrinks the object is allowed over the limit")
	_, err = m.DeleteObject(t.Context(), "t1/tenantfull", "a", false)
	require.NoError(t, err, "a delete is allowed over the limit")
}

type quotaMarker struct{}

// pauseMarkedRead pauses the first bucket read of a call carrying quotaMarker:
// that write holds its room but has not read the committed usage yet.
type pauseMarkedRead struct {
	metadata.Store
	once             sync.Once
	entered, proceed chan struct{}
}

func newPauseMarkedRead(s metadata.Store) *pauseMarkedRead {
	return &pauseMarkedRead{Store: s, entered: make(chan struct{}), proceed: make(chan struct{})}
}

func (s *pauseMarkedRead) GetBucket(ctx context.Context, tenant, name string) (*metadata.BucketMetadata, error) {
	if ctx.Value(quotaMarker{}) != nil {
		s.once.Do(func() {
			close(s.entered)
			<-s.proceed
		})
	}
	return s.Store.GetBucket(ctx, tenant, name)
}

func waitClosed(t *testing.T, c chan struct{}, what string) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(5 * time.Second):
		t.Fatal(what)
	}
}

// Limit 10. A holds 4 and B holds 6; A finishes while B reads the committed
// usage. 4 + 6 fits: B is admitted.
func TestWriteQuotaFinishedWriteIsNotCountedTwice(t *testing.T) {
	m, s := setupAccountingManager(t)
	ctx := context.Background()
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{Name: "twice", Quota: &metadata.BucketQuota{MaxSizeBytes: 10}}))
	releaseA, err := m.reserveWriteQuota(ctx, "twice", 4, nil, false)
	require.NoError(t, err)

	reads := newPauseMarkedRead(s)
	m.metadataStore = reads
	done := make(chan error, 1)
	go func() {
		release, err := m.reserveWriteQuota(context.WithValue(ctx, quotaMarker{}, true), "twice", 6, nil, false)
		if err == nil {
			release()
		}
		done <- err
	}()
	waitClosed(t, reads.entered, "B never read the committed usage")
	releaseA()
	require.NoError(t, m.bucketManager.IncrementObjectCount(ctx, "", "twice", 4))
	close(reads.proceed)
	require.NoError(t, <-done, "4 committed + 6 in flight fits a limit of 10")
}

type bucketUsage interface {
	IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
	DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
	AdjustBucketSize(ctx context.Context, tenantID, name string, sizeDelta int64) error
}

// pauseAfterUsage pauses the first write right after its usage is committed.
type pauseAfterUsage struct {
	bucketUsage
	once             sync.Once
	entered, proceed chan struct{}
}

func (p *pauseAfterUsage) IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error {
	err := p.bucketUsage.IncrementObjectCount(ctx, tenantID, name, sizeBytes)
	p.once.Do(func() {
		close(p.entered)
		<-p.proceed
	})
	return err
}

// A write whose usage is committed no longer holds room: a check made at that
// moment does not count it twice.
func TestWriteQuotaCommittedWriteHoldsNothing(t *testing.T) {
	for _, multipart := range []bool{false, true} {
		t.Run(map[bool]string{false: "put", true: "multipart"}[multipart], func(t *testing.T) {
			m, s := setupAccountingManager(t)
			ctx := context.Background()
			require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{Name: "done", Quota: &metadata.BucketQuota{MaxSizeBytes: 10}}))
			var id string
			var parts []Part
			if multipart {
				id, parts = stageOnePartUpload(t, m, "done", "a", "1234")
			}
			usage := &pauseAfterUsage{bucketUsage: m.bucketManager, entered: make(chan struct{}), proceed: make(chan struct{})}
			m.bucketManager = usage
			done := make(chan error, 1)
			go func() {
				var err error
				if multipart {
					_, err = m.CompleteMultipartUpload(ctx, id, parts)
				} else {
					_, err = m.PutObject(ctx, "done", "a", strings.NewReader("1234"), http.Header{})
				}
				done <- err
			}()
			waitClosed(t, usage.entered, "the first write never committed its usage")
			release, err := m.reserveWriteQuota(ctx, "done", 6, nil, false)
			close(usage.proceed)
			require.NoError(t, <-done)
			require.NoError(t, err, "4 committed + 6 fits a limit of 10")
			release()
		})
	}
}

// A write ahead that ends up refused does not take a fitting write with it.
func TestWriteQuotaRefusedWriteAheadDoesNotRefuseOthers(t *testing.T) {
	m, s := setupAccountingManager(t)
	ctx := context.Background()
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{Name: "ahead", Quota: &metadata.BucketQuota{MaxSizeBytes: 10}}))
	_, err := m.PutObject(ctx, "ahead", "base", strings.NewReader("12345"), http.Header{})
	require.NoError(t, err)

	reads := newPauseMarkedRead(s)
	m.metadataStore = reads
	big := make(chan error, 1)
	go func() {
		_, err := m.reserveWriteQuota(context.WithValue(ctx, quotaMarker{}, true), "ahead", 8, nil, false)
		big <- err
	}()
	waitClosed(t, reads.entered, "the 8-byte write never started its check")

	small := make(chan error, 1)
	go func() {
		release, err := m.reserveWriteQuota(ctx, "ahead", 3, nil, false)
		if err == nil {
			release()
		}
		small <- err
	}()
	select {
	case err := <-small:
		t.Fatalf("the 3-byte write was decided before the write ahead of it: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(reads.proceed)
	require.ErrorIs(t, <-big, ErrBucketQuotaExceeded, "5 committed + 8 exceeds 10")
	require.NoError(t, <-small, "5 committed + 3 fits 10")
}

// memTenant is a tenant quota kept in memory. The first check of a call
// carrying quotaMarker pauses before reading the usage.
type memTenant struct {
	mu               sync.Mutex
	used, limit      int64
	once             sync.Once
	entered, proceed chan struct{}
}

func (a *memTenant) IncrementTenantStorage(_ context.Context, _ string, n int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.used += n
	return nil
}

func (a *memTenant) DecrementTenantStorage(_ context.Context, _ string, n int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.used -= n
	return nil
}

func (a *memTenant) CheckTenantStorageQuota(ctx context.Context, _ string, add int64) error {
	if ctx.Value(quotaMarker{}) != nil {
		a.once.Do(func() {
			close(a.entered)
			<-a.proceed
		})
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.used+add > a.limit {
		return errors.New("tenant over quota")
	}
	return nil
}

// The tenant check counts a finished write once as well.
func TestWriteQuotaFinishedWriteIsNotCountedTwiceByTenant(t *testing.T) {
	m, s := setupAccountingManager(t)
	ctx := context.Background()
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{Name: "tq", TenantID: "t1"}))
	tenant := &memTenant{limit: 10, entered: make(chan struct{}), proceed: make(chan struct{})}
	m.SetAuthManager(tenant)
	releaseA, err := m.reserveWriteQuota(ctx, "t1/tq", 4, nil, false)
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		release, err := m.reserveWriteQuota(context.WithValue(ctx, quotaMarker{}, true), "t1/tq", 6, nil, false)
		if err == nil {
			release()
		}
		done <- err
	}()
	waitClosed(t, tenant.entered, "B never reached the tenant check")
	releaseA()
	require.NoError(t, tenant.IncrementTenantStorage(ctx, "t1", 4))
	close(tenant.proceed)
	require.NoError(t, <-done, "4 committed + 6 in flight fits a tenant limit of 10")
}

// As many concurrent writers as the quota allows, started a few milliseconds
// apart so that some finish while others read the committed usage: none is
// refused.
func TestWriteQuotaConcurrentWritersThatFitAreAllAdmitted(t *testing.T) {
	m, s := setupAccountingManager(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{
		Name: "fit", Quota: &metadata.BucketQuota{MaxObjectCount: 16, MaxSizeBytes: 48},
	}))
	m.metadataStore = slowBucketReads{Store: s}
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := m.PutObject(context.Background(), "fit", fmt.Sprintf("k%02d", i), strings.NewReader("abc"), http.Header{})
			errs <- err
		}(i)
		time.Sleep(5 * time.Millisecond)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err, "16 objects of 3 bytes fit 16 objects and 48 bytes")
	}
}
