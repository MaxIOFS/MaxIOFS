package object

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/require"
)

type failedRestoreBackend struct {
	storage.Backend
	calls, failAt int
	err           error
}

func (b *failedRestoreBackend) Put(ctx context.Context, ref storage.ObjectRef, r io.Reader, m map[string]string) error {
	b.calls++
	if b.calls == b.failAt {
		return b.err
	}
	return b.Backend.Put(ctx, ref, r, m)
}

func TestFailedRestoreKeepsRecoverableBackup(t *testing.T) {
	for _, multipart := range []bool{false, true} {
		t.Run(map[bool]string{false: "put", true: "multipart"}[multipart], func(t *testing.T) {
			om, backend, meta := setupManagerWithConfigKey(t)
			ctx := t.Context()
			require.NoError(t, meta.CreateBucket(ctx, &metadata.BucketMetadata{Name: "backup", OwnerID: "owner"}))
			old, err := om.PutObject(ctx, "backup", "key", strings.NewReader("original bytes"), http.Header{})
			require.NoError(t, err)
			u, err := om.CreateMultipartUpload(ctx, "backup", "key", http.Header{})
			require.NoError(t, err)
			p, err := om.UploadPart(ctx, u.UploadID, 1, strings.NewReader("replacement bytes"))
			require.NoError(t, err)
			restoreErr := errors.New("restore failed")
			failAt := 2
			if multipart {
				failAt = 3
			}
			om.storage = &failedRestoreBackend{Backend: backend, failAt: failAt, err: restoreErr}
			om.metadataStore = &failingPutStore{Store: meta, failPuts: true}
			if multipart {
				_, err = om.CompleteMultipartUpload(ctx, u.UploadID, []Part{*p})
			} else {
				_, err = om.PutObject(ctx, "backup", "key", strings.NewReader("replacement bytes"), http.Header{})
			}
			require.ErrorIs(t, err, restoreErr)
			manifests, err := filepath.Glob(filepath.Join(om.config.Root, "maxiofs-mpu-backup-*.json"))
			require.NoError(t, err)
			require.Len(t, manifests, 1)
			content, err := os.ReadFile(manifests[0])
			require.NoError(t, err)
			var manifest objectBackupManifest
			require.NoError(t, json.Unmarshal(content, &manifest))
			require.Equal(t, storage.ObjectRef{Bucket: "backup", Key: "key"}, manifest.Ref)
			require.NotEmpty(t, manifest.Metadata["wrapped-dek"])
			backup, err := os.Open(strings.TrimSuffix(manifests[0], ".json"))
			require.NoError(t, err)
			defer backup.Close()
			require.NoError(t, backend.Put(ctx, manifest.Ref, backup, manifest.Metadata))
			obj, r, err := om.GetObject(ctx, "backup", "key")
			require.NoError(t, err)
			defer r.Close()
			data, err := io.ReadAll(r)
			require.NoError(t, err)
			require.Equal(t, "original bytes", string(data))
			require.Equal(t, old.ETag, obj.ETag)
		})
	}
}

func TestSuccessfulRestoreRemovesBackup(t *testing.T) {
	om, _, meta := setupManagerWithConfigKey(t)
	ctx := t.Context()
	require.NoError(t, meta.CreateBucket(ctx, &metadata.BucketMetadata{Name: "backup", OwnerID: "owner"}))
	_, err := om.PutObject(ctx, "backup", "key", strings.NewReader("original"), http.Header{})
	require.NoError(t, err)
	om.metadataStore = &failingPutStore{Store: meta, failPuts: true}
	_, err = om.PutObject(ctx, "backup", "key", strings.NewReader("replacement"), http.Header{})
	require.Error(t, err)
	files, err := filepath.Glob(filepath.Join(om.config.Root, "maxiofs-mpu-backup-*"))
	require.NoError(t, err)
	require.Empty(t, files)
}
