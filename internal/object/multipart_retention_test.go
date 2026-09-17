package object

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

func TestMultipartDefaultRetention(t *testing.T) {
	for _, period := range []string{"days", "years", "none"} {
		t.Run(period, func(t *testing.T) {
			om, _, meta := setupManagerWithConfigKey(t)
			ctx := t.Context()
			b := &metadata.BucketMetadata{Name: "locked", OwnerID: "owner", Versioning: &metadata.VersioningMetadata{Status: "Enabled"}}
			amount := 1
			if period != "none" {
				r := &metadata.RetentionMetadata{Mode: "COMPLIANCE"}
				if period == "days" {
					r.Days = &amount
				} else {
					r.Years = &amount
				}
				b.ObjectLock = &metadata.ObjectLockMetadata{Enabled: true, Rule: &metadata.ObjectLockRuleMetadata{DefaultRetention: r}}
			}
			require.NoError(t, meta.CreateBucket(ctx, b))
			u, err := om.CreateMultipartUpload(ctx, b.Name, "key", http.Header{})
			require.NoError(t, err)
			p, err := om.UploadPart(ctx, u.UploadID, 1, strings.NewReader("protected"))
			require.NoError(t, err)
			if period != "none" {
				om.metadataStore = retentionCommitStore{Store: meta}
			}
			before := time.Now()
			o, err := om.CompleteMultipartUpload(ctx, u.UploadID, []Part{*p})
			require.NoError(t, err)
			stored, err := meta.GetObject(ctx, b.Name, "key", o.VersionID)
			require.NoError(t, err)
			if period == "none" {
				require.Nil(t, stored.Retention)
				_, err = om.DeleteObject(ctx, b.Name, "key", false, o.VersionID)
				require.NoError(t, err)
				return
			}
			require.NotNil(t, stored.Retention)
			require.Equal(t, "COMPLIANCE", stored.Retention.Mode)
			minimum := before.AddDate(0, 0, 1)
			if period == "years" {
				minimum = before.AddDate(1, 0, 0)
			}
			require.False(t, stored.Retention.RetainUntilDate.Before(minimum))
			_, err = om.DeleteObject(ctx, b.Name, "key", true, o.VersionID)
			require.Error(t, err, "even governance bypass must not remove a COMPLIANCE version")
		})
	}
}

type retentionCommitStore struct{ metadata.Store }

func (s retentionCommitStore) PutObjectVersion(ctx context.Context, obj *metadata.ObjectMetadata, version *metadata.ObjectVersion) error {
	if obj.Retention == nil || !obj.Retention.RetainUntilDate.After(time.Now()) {
		return errors.New("version must carry active retention at its first commit")
	}
	return s.Store.PutObjectVersion(ctx, obj, version)
}

type unavailableRetentionStore struct{ metadata.Store }

func (s unavailableRetentionStore) GetBucket(context.Context, string, string) (*metadata.BucketMetadata, error) {
	return nil, errors.New("injected bucket lookup failure")
}

func TestMultipartRetentionLookupFailurePreservesUpload(t *testing.T) {
	om, backend, meta := setupManagerWithConfigKey(t)
	ctx := t.Context()
	require.NoError(t, meta.CreateBucket(ctx, &metadata.BucketMetadata{Name: "locked", OwnerID: "owner"}))
	u, err := om.CreateMultipartUpload(ctx, "locked", "key", http.Header{})
	require.NoError(t, err)
	p, err := om.UploadPart(ctx, u.UploadID, 1, strings.NewReader("pending"))
	require.NoError(t, err)
	om.metadataStore = unavailableRetentionStore{Store: meta}
	_, err = om.CompleteMultipartUpload(ctx, u.UploadID, []Part{*p})
	require.ErrorContains(t, err, "injected bucket lookup failure")
	_, err = meta.GetMultipartUpload(ctx, u.UploadID)
	require.NoError(t, err)
	exists, err := backend.PartExists(ctx, u.UploadID, 1)
	require.NoError(t, err)
	require.True(t, exists)
	_, err = meta.GetObject(ctx, "locked", "key")
	require.ErrorIs(t, err, metadata.ErrObjectNotFound)
}
