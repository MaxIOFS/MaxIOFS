package object

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/rollback"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/require"
)

type rawRollbackBackend struct {
	storage.Backend
	mode   string
	calls  int
	cancel context.CancelFunc
	err    error
}

func (b *rawRollbackBackend) Put(ctx context.Context, ref storage.ObjectRef, r io.Reader, meta map[string]string) error {
	b.calls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.mode == "restore" && b.calls == 2 {
		return b.err
	}
	if err := b.Backend.Put(ctx, ref, r, meta); err != nil {
		return err
	}
	if b.calls == 1 {
		if b.mode == "cancel" {
			b.cancel()
		}
		if b.mode == "publish" || b.mode == "cancel" {
			return b.err
		}
	}
	return nil
}

func TestRawReplicaOverwriteRollback(t *testing.T) {
	for _, target := range []string{"plain", "latest-version", "historical-version"} {
		for _, failure := range []string{"metadata", "publish", "cancel", "restore", "lookup"} {
			t.Run(fmt.Sprintf("%s/%s", target, failure), func(t *testing.T) {
				m, backend, s := setupManagerWithConfigKey(t)
				ctx := t.Context()
				bucket := &metadata.BucketMetadata{Name: "raw"}
				if target != "plain" {
					bucket.Versioning = &metadata.VersioningMetadata{Status: "Enabled"}
				}
				require.NoError(t, s.CreateBucket(ctx, bucket))
				old, err := m.PutObject(ctx, "raw", "target", strings.NewReader("original bytes"), http.Header{})
				require.NoError(t, err)
				if target == "historical-version" {
					_, err := m.PutObject(ctx, "raw", "target", strings.NewReader("latest bytes"), http.Header{})
					require.NoError(t, err)
				}
				latest, err := s.GetObject(ctx, "raw", "target")
				require.NoError(t, err)
				_, err = m.PutObject(ctx, "raw", "source", strings.NewReader("replacement bytes, longer"), http.Header{})
				require.NoError(t, err)
				r, sidecar, incoming, err := m.GetObjectRaw(ctx, "raw", "source", "")
				require.NoError(t, err)
				data, err := io.ReadAll(r)
				require.NoError(t, r.Close())
				require.NoError(t, err)
				incoming.VersionID = old.VersionID
				requestCtx, cancel := context.WithCancel(ctx)
				defer cancel()
				injected := errors.New("injected raw write failure")
				b := &rawRollbackBackend{Backend: backend, mode: failure, cancel: cancel, err: injected}
				m.storage = b
				if failure == "metadata" || failure == "restore" {
					m.metadataStore = &failingPutStore{Store: s, failPuts: true}
				}
				if failure == "lookup" {
					m.metadataStore = &streamFailureStore{Store: s, mode: "lookup", err: injected}
				}
				err = m.PutObjectRaw(requestCtx, "raw", "target", bytes.NewReader(data), sidecar, incoming)
				require.Error(t, err)
				if failure != "metadata" {
					require.ErrorIs(t, err, injected)
				}
				require.Equal(t, "source", incoming.Key, "replication must not mutate caller metadata")
				if failure == "lookup" {
					require.Zero(t, b.calls)
				}
				m.storage, m.metadataStore = backend, s
				if failure == "restore" {
					report, err := rollback.Undo(ctx, m.config.Root, backend, s, nil)
					require.NoError(t, err)
					require.Empty(t, report.Failures)
					require.Equal(t, 1, report.ObjectsRestored)
				}
				obj, reader, err := m.GetObject(ctx, "raw", "target", old.VersionID)
				require.NoError(t, err)
				got, err := io.ReadAll(reader)
				require.NoError(t, reader.Close())
				require.NoError(t, err)
				require.Equal(t, "original bytes", string(got))
				require.Equal(t, old.ETag, obj.ETag)
				current, err := s.GetObject(ctx, "raw", "target")
				require.NoError(t, err)
				require.Equal(t, latest.VersionID, current.VersionID)
				require.Equal(t, latest.ETag, current.ETag)
				requireNoRetainedBackup(t, m)
				require.NoError(t, m.PutObjectRaw(ctx, "raw", "target", bytes.NewReader(data), sidecar, incoming))
				_, reader, err = m.GetObject(ctx, "raw", "target", old.VersionID)
				require.NoError(t, err)
				got, err = io.ReadAll(reader)
				require.NoError(t, reader.Close())
				require.NoError(t, err)
				require.Equal(t, "replacement bytes, longer", string(got))
				requireNoRetainedBackup(t, m)
			})
		}
	}
}
