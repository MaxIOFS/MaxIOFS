package object

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

func TestWriteQuotaReservationsRelease(t *testing.T) {
	for _, countOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "bytes", true: "objects"}[countOnly], func(t *testing.T) {
			m, _, s := setupManagerWithConfigKey(t)
			q := &metadata.BucketQuota{MaxSizeBytes: 10}
			if countOnly {
				q = &metadata.BucketQuota{MaxObjectCount: 1}
			}
			require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "quota", Quota: q}))
			release, err := m.reserveWriteQuota(t.Context(), "quota", 8, nil, false)
			require.NoError(t, err)
			_, err = m.reserveWriteQuota(t.Context(), "quota", 8, nil, false)
			require.ErrorIs(t, err, ErrBucketQuotaExceeded)
			release()
			release, err = m.reserveWriteQuota(t.Context(), "quota", 8, nil, false)
			require.NoError(t, err)
			release()
			require.Empty(t, m.pendingQuotas)
			require.Empty(t, m.pendingTenants)
		})
	}
}

func TestWriteQuotaFailureRemainsRetryable(t *testing.T) {
	for _, multipart := range []bool{false, true} {
		t.Run(map[bool]string{false: "put", true: "multipart"}[multipart], func(t *testing.T) {
			m, s := setupAccountingManager(t)
			require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "quota", Quota: &metadata.BucketQuota{MaxSizeBytes: 10, MaxObjectCount: 1}}))
			var id string
			var parts []Part
			if multipart {
				id, parts = stageOnePartUpload(t, m, "quota", "key", "12345678")
			}
			write := func() error {
				if multipart {
					_, err := m.CompleteMultipartUpload(t.Context(), id, parts)
					return err
				}
				_, err := m.PutObject(t.Context(), "quota", "key", strings.NewReader("12345678"), http.Header{})
				return err
			}
			m.metadataStore = &failingPutStore{Store: s, failPuts: true}
			require.Error(t, write())
			require.Empty(t, m.pendingQuotas)
			m.metadataStore = s
			require.NoError(t, write())
			require.Empty(t, m.pendingQuotas)
		})
	}
}

type writeLockCommitStore struct {
	metadata.Store
	until time.Time
}

func (s writeLockCommitStore) PutObjectVersion(ctx context.Context, obj *metadata.ObjectMetadata, version *metadata.ObjectVersion) error {
	if obj.Retention == nil || !obj.Retention.RetainUntilDate.Equal(s.until) || !obj.LegalHold {
		return errors.New("protection missing at first commit")
	}
	return s.Store.PutObjectVersion(ctx, obj, version)
}

func TestWriteExplicitProtectionAtFirstCommit(t *testing.T) {
	m, _, s := setupManagerWithConfigKey(t)
	days := 30
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "locked", Versioning: &metadata.VersioningMetadata{Status: "Enabled"}, ObjectLock: &metadata.ObjectLockMetadata{Enabled: true, Rule: &metadata.ObjectLockRuleMetadata{DefaultRetention: &metadata.RetentionMetadata{Mode: "COMPLIANCE", Days: &days}}}}))
	until := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	m.metadataStore = writeLockCommitStore{Store: s, until: until}
	h := http.Header{}
	h.Set("x-amz-object-lock-mode", "COMPLIANCE")
	h.Set("x-amz-object-lock-retain-until-date", until.Format(time.RFC3339))
	h.Set("x-amz-object-lock-legal-hold", "ON")
	o, err := m.PutObject(t.Context(), "locked", "key", strings.NewReader("protected"), h)
	require.NoError(t, err)
	require.Equal(t, until, o.Retention.RetainUntilDate)
	_, err = m.DeleteObject(t.Context(), "locked", "key", false, o.VersionID)
	require.ErrorIs(t, err, ErrObjectUnderLegalHold)
}

func TestDeleteBucketLookupFailurePreservesObject(t *testing.T) {
	m, _, s := setupManagerWithConfigKey(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "bucket"}))
	_, err := m.PutObject(t.Context(), "bucket", "key", strings.NewReader("original"), http.Header{})
	require.NoError(t, err)
	m.metadataStore = &auditBucketReadFailure{Store: s}
	_, err = m.DeleteObject(t.Context(), "bucket", "key", false)
	require.Error(t, err)
	m.metadataStore = s
	_, r, err := m.GetObject(t.Context(), "bucket", "key")
	require.NoError(t, err)
	defer r.Close()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "original", string(data))
}

// Quota is accounted per bucket: a full bucket does not affect another one,
// whether global or owned by a tenant. Bucket names are global, as in S3.
func TestWriteQuotaIsAccountedPerBucket(t *testing.T) {
	m, s := setupAccountingManager(t)
	paths := []string{"shared", "tenant-a/shared-a", "tenant-b/shared-b"}
	for i, path := range paths {
		tenant, name := m.parseBucketPath(path)
		require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{
			Name: name, TenantID: tenant, Quota: &metadata.BucketQuota{MaxSizeBytes: 2, MaxObjectCount: 1},
		}))
		body := strings.Repeat(string(rune('a'+i)), 2)
		_, err := m.PutObject(t.Context(), path, "key", strings.NewReader(body), http.Header{})
		require.NoError(t, err)
	}
	for i, path := range paths {
		_, data := readWholeObject(t, m, path, "key")
		require.Equal(t, strings.Repeat(string(rune('a'+i)), 2), data)
		_, err := m.PutObject(t.Context(), path, "extra", strings.NewReader("x"), http.Header{})
		require.ErrorIs(t, err, ErrBucketQuotaExceeded)
		tenant, name := m.parseBucketPath(path)
		b, err := s.GetBucket(t.Context(), tenant, name)
		require.NoError(t, err)
		require.Equal(t, int64(2), b.TotalSize)
		require.Equal(t, int64(1), b.ObjectCount)
	}
}
