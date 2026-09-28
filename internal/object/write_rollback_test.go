package object

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/require"
)

// Undoing a write deletes the version it created under the retention or legal
// hold that write set. A client delete still cannot, nor can the undo touch
// another version.
func TestWriteRollbackDeletesTheProtectedVersionItCreated(t *testing.T) {
	until := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	for name, lock := range map[string]map[string]string{
		"compliance": {"x-amz-object-lock-mode": RetentionModeCompliance, "x-amz-object-lock-retain-until-date": until},
		"legal hold": {"x-amz-object-lock-legal-hold": LegalHoldStatusOn},
	} {
		t.Run(name, func(t *testing.T) {
			m, _, s := setupManagerWithConfigKey(t)
			ctx := t.Context()
			require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{
				Name:       "worm",
				Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
				ObjectLock: &metadata.ObjectLockMetadata{Enabled: true},
			}))
			headers := http.Header{}
			for k, v := range lock {
				headers.Set(k, v)
			}
			older, err := m.PutObject(ctx, "worm", "k", strings.NewReader("older"), headers.Clone())
			require.NoError(t, err)
			written, err := m.PutObject(ctx, "worm", "k", strings.NewReader("written"), headers.Clone())
			require.NoError(t, err)

			_, err = m.DeleteObject(ctx, "worm", "k", true, written.VersionID)
			require.Error(t, err, "a client delete, even bypassing governance, is refused")

			_, err = m.DeleteObject(WithWriteRollback(ctx), "worm", "k", true, written.VersionID)
			require.NoError(t, err)
			_, _, err = m.GetObject(ctx, "worm", "k", written.VersionID)
			require.ErrorIs(t, err, ErrObjectNotFound)

			obj, reader, err := m.GetObject(ctx, "worm", "k", older.VersionID)
			require.NoError(t, err)
			body, err := io.ReadAll(reader)
			require.NoError(t, reader.Close())
			require.NoError(t, err)
			require.Equal(t, "older", string(body))
			require.Equal(t, older.VersionID, obj.VersionID)
		})
	}
}

// Object-lock headers sent when a multipart upload is created are validated
// then, as a PUT's are, and applied to the object it completes into.
func TestMultipartUploadKeepsItsObjectLock(t *testing.T) {
	m, _, s := setupManagerWithConfigKey(t)
	ctx := t.Context()
	days := 1
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:       "mpworm",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
		ObjectLock: &metadata.ObjectLockMetadata{Enabled: true, Rule: &metadata.ObjectLockRuleMetadata{
			DefaultRetention: &metadata.RetentionMetadata{Mode: RetentionModeGovernance, Days: &days},
		}},
	}))
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{Name: "mpplain"}))
	complete := func(t *testing.T, bucket, key string, h http.Header) (*Object, error) {
		t.Helper()
		upload, err := m.CreateMultipartUpload(ctx, bucket, key, h)
		if err != nil {
			return nil, err
		}
		part, err := m.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("part"))
		require.NoError(t, err)
		return m.CompleteMultipartUpload(ctx, upload.UploadID, []Part{*part})
	}

	t.Run("explicit retention and legal hold", func(t *testing.T) {
		until := time.Now().Add(72*time.Hour + 123456789).UTC()
		h := http.Header{}
		h.Set("x-amz-object-lock-mode", RetentionModeCompliance)
		h.Set("x-amz-object-lock-retain-until-date", until.Format(time.RFC3339Nano))
		h.Set("x-amz-object-lock-legal-hold", LegalHoldStatusOn)
		h.Set("x-amz-meta-owner", "backup")
		obj, err := complete(t, "mpworm", "explicit", h)
		require.NoError(t, err)
		require.NotNil(t, obj.Retention)
		require.Equal(t, RetentionModeCompliance, obj.Retention.Mode)
		require.True(t, obj.Retention.RetainUntilDate.Equal(until), "stored %v for %v", obj.Retention.RetainUntilDate, until)
		require.Equal(t, LegalHoldStatusOn, obj.LegalHold.Status)
		require.Equal(t, map[string]string{"owner": "backup"}, obj.Metadata, "lock state is not user metadata")

		_, err = m.DeleteObject(ctx, "mpworm", "explicit", true, obj.VersionID)
		require.Error(t, err, "the completed version is protected")
	})

	t.Run("legal hold only: the bucket default retention applies", func(t *testing.T) {
		h := http.Header{}
		h.Set("x-amz-object-lock-legal-hold", LegalHoldStatusOn)
		obj, err := complete(t, "mpworm", "hold-only", h)
		require.NoError(t, err)
		require.NotNil(t, obj.Retention)
		require.Equal(t, RetentionModeGovernance, obj.Retention.Mode)
		require.Equal(t, LegalHoldStatusOn, obj.LegalHold.Status)
	})

	t.Run("user metadata cannot set the lock state", func(t *testing.T) {
		h := http.Header{}
		h.Set("x-amz-meta-x-amz-object-lock-legal-hold", LegalHoldStatusOn)
		_, err := complete(t, "mpplain", "sneaky", h)
		require.NoError(t, err)
		stored, err := m.GetObjectMetadata(ctx, "mpplain", "sneaky")
		require.NoError(t, err)
		require.Nil(t, stored.Retention)
		require.NotEqual(t, LegalHoldStatusOn, stored.LegalHold.Status)
	})

	t.Run("refused at creation", func(t *testing.T) {
		past := http.Header{}
		past.Set("x-amz-object-lock-mode", RetentionModeGovernance)
		past.Set("x-amz-object-lock-retain-until-date", time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
		_, err := complete(t, "mpworm", "past", past)
		require.ErrorIs(t, err, ErrRetentionDateInPast)

		hold := http.Header{}
		hold.Set("x-amz-object-lock-legal-hold", LegalHoldStatusOn)
		_, err = complete(t, "mpplain", "nolock", hold)
		require.ErrorIs(t, err, ErrNoRetentionConfiguration)
	})
}

// User metadata of a multipart upload cannot stand for its storage fields,
// its canned ACL or the sidecar's fields: it stays user metadata.
func TestMultipartUserMetadataStaysUserMetadata(t *testing.T) {
	m, _, s := setupManagerWithConfigKey(t)
	ctx := t.Context()
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{Name: "mpmeta"}))
	h := http.Header{}
	h.Set("Content-Type", "text/plain")
	h.Set("x-amz-meta-content-type", "fake/type")
	h.Set("x-amz-meta-x-amz-acl", "public-read")
	h.Set("x-amz-meta-last_modified", "1")
	h.Set("x-amz-meta-owner", "backup")
	upload, err := m.CreateMultipartUpload(ctx, "mpmeta", "k", h)
	require.NoError(t, err)
	require.Empty(t, UploadCannedACL(upload.Metadata), "user metadata is not a canned ACL")
	part, err := m.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("part"))
	require.NoError(t, err)
	obj, err := m.CompleteMultipartUpload(ctx, upload.UploadID, []Part{*part})
	require.NoError(t, err)

	require.Equal(t, "text/plain", obj.ContentType)
	require.Equal(t, map[string]string{
		"content-type": "fake/type", "x-amz-acl": "public-read", "last_modified": "1", "owner": "backup",
	}, obj.Metadata)
	sidecar, err := m.storage.GetMetadata(ctx, m.objectRef("mpmeta", "k"))
	require.NoError(t, err)
	for k := range sidecar {
		require.False(t, strings.HasPrefix(k, "x-amz-meta-") || strings.Contains(k, ":") || k == "owner",
			"user metadata and upload state stay out of the sidecar, found %q", k)
	}
	require.NotEqual(t, "1", sidecar["last_modified"])
	require.Equal(t, "text/plain", sidecar["content-type"])
}

// An upload created before user metadata kept its prefix still completes with
// its user metadata; its old canned-ACL key is neither user metadata nor ACL.
func TestMultipartUploadCreatedBeforeTheUpgrade(t *testing.T) {
	m, _, s := setupManagerWithConfigKey(t)
	ctx := t.Context()
	require.NoError(t, s.CreateBucket(ctx, &metadata.BucketMetadata{Name: "mplegacy"}))
	upload, err := m.CreateMultipartUpload(ctx, "mplegacy", "k", http.Header{})
	require.NoError(t, err)
	record, err := s.GetMultipartUpload(ctx, upload.UploadID)
	require.NoError(t, err)
	record.Metadata = map[string]string{"content-type": "text/plain", "owner": "backup", "x-amz-acl": "public-read"}
	require.NoError(t, s.CreateMultipartUpload(ctx, record))

	require.Empty(t, UploadCannedACL(record.Metadata))
	part, err := m.UploadPart(ctx, upload.UploadID, 1, strings.NewReader("part"))
	require.NoError(t, err)
	obj, err := m.CompleteMultipartUpload(ctx, upload.UploadID, []Part{*part})
	require.NoError(t, err)
	require.Equal(t, "text/plain", obj.ContentType)
	require.Equal(t, map[string]string{"owner": "backup"}, obj.Metadata)
}
