package object

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/maxiofs/maxiofs/pkg/encryption"
	"github.com/stretchr/testify/require"
)

type streamFailureBackend struct {
	storage.Backend
	mode   string
	err    error
	cancel context.CancelFunc
	puts   int
}

func (b *streamFailureBackend) Put(ctx context.Context, ref storage.ObjectRef, r io.Reader, meta map[string]string) error {
	b.puts++
	if b.mode == "early-put" && b.puts == 1 {
		return b.err
	}
	return b.Backend.Put(ctx, ref, r, meta)
}

func (b *streamFailureBackend) GetPart(ctx context.Context, id string, n int) (io.ReadCloser, map[string]string, error) {
	if b.mode == "open" && n == 2 {
		return nil, nil, b.err
	}
	r, meta, err := b.Backend.GetPart(ctx, id, n)
	if err != nil || n != 1 {
		return r, meta, err
	}
	if b.mode == "short" {
		return struct {
			io.Reader
			io.Closer
		}{io.LimitReader(r, 1), r}, meta, nil
	}
	return &streamFailureReader{ReadCloser: r, backend: b}, meta, nil
}

type streamFailureReader struct {
	io.ReadCloser
	backend *streamFailureBackend
}

func (r *streamFailureReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if r.backend.mode == "read" {
		return n, r.backend.err
	}
	if r.backend.mode == "cancel" {
		r.backend.cancel()
	}
	return n, err
}

func (r *streamFailureReader) Close() error {
	err := r.ReadCloser.Close()
	if r.backend.mode == "close" {
		return errors.Join(err, r.backend.err)
	}
	return err
}

type streamFailureStore struct {
	metadata.Store
	mode string
	err  error
}

func (s *streamFailureStore) PutObject(ctx context.Context, o *metadata.ObjectMetadata) error {
	if s.mode == "commit" {
		return s.err
	}
	return s.Store.PutObject(ctx, o)
}

func (s *streamFailureStore) GetObject(ctx context.Context, bucket, key string, version ...string) (*metadata.ObjectMetadata, error) {
	if s.mode == "lookup" {
		return nil, s.err
	}
	return s.Store.GetObject(ctx, bucket, key, version...)
}

type streamTrackingEncryptor struct {
	encryption.Encryptor
	done chan struct{}
	err  error
}

func (e *streamTrackingEncryptor) EncryptStream(src io.Reader, dst io.Writer, key []byte) (*encryption.EncryptionMetadata, error) {
	defer close(e.done)
	if e.err != nil {
		_, _ = src.Read(make([]byte, 1))
		return nil, e.err
	}
	return e.Encryptor.EncryptStream(src, dst, key)
}

func TestMultipartStreamFailuresKeepPreviousObject(t *testing.T) {
	for _, mode := range []string{"open", "read", "short", "close", "cancel", "early-put", "encrypt", "commit", "lookup"} {
		t.Run(mode, func(t *testing.T) {
			m, b, s := setupManagerWithConfigKey(t)
			require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "stream"}))
			old, err := m.PutObject(t.Context(), "stream", "key", strings.NewReader("previous object"), http.Header{})
			require.NoError(t, err)
			upload, err := m.CreateMultipartUpload(t.Context(), "stream", "key", http.Header{})
			require.NoError(t, err)
			var parts []Part
			for _, body := range []string{"first part", "second part"} {
				p, err := m.UploadPart(t.Context(), upload.UploadID, len(parts)+1, strings.NewReader(body))
				require.NoError(t, err)
				parts = append(parts, *p)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			injected := errors.New("injected stream failure")
			backend := &streamFailureBackend{Backend: b, mode: mode, err: injected, cancel: cancel}
			probe := &stageProbe{Backend: backend, root: m.config.Root}
			m.storage = probe
			m.metadataStore = &streamFailureStore{Store: s, mode: mode, err: injected}
			enc := &streamTrackingEncryptor{Encryptor: m.encryptor, done: make(chan struct{})}
			if mode == "encrypt" {
				enc.err = injected
			}
			m.encryptor = enc
			_, err = m.CompleteMultipartUpload(ctx, upload.UploadID, parts)
			require.Error(t, err)
			switch mode {
			case "cancel":
				require.ErrorIs(t, err, context.Canceled)
			case "short":
				require.Contains(t, err.Error(), "multipart size mismatch")
			default:
				require.ErrorIs(t, err, injected)
			}
			if mode != "lookup" {
				select {
				case <-enc.done:
				default:
					t.Fatal("completion returned before encryption stopped")
				}
			}
			require.Zero(t, probe.openParts.Load())
			require.LessOrEqual(t, probe.peakParts.Load(), int64(1))
			m.storage, m.metadataStore, m.encryptor = b, s, enc.Encryptor
			obj, data := readWholeObject(t, m, "stream", "key")
			require.Equal(t, "previous object", data)
			require.Equal(t, old.ETag, obj.ETag)
			require.Equal(t, old.Size, obj.Size)
			retained, err := m.ListParts(t.Context(), upload.UploadID)
			require.NoError(t, err)
			require.Len(t, retained, 2)
			requireNoRetainedBackup(t, m)
			_, err = m.CompleteMultipartUpload(t.Context(), upload.UploadID, parts)
			require.NoError(t, err, "the failed attempt must remain retryable")
			_, data = readWholeObject(t, m, "stream", "key")
			require.Equal(t, "first partsecond part", data)
		})
	}
}

func TestMultipartStreamEmptyPart(t *testing.T) {
	m, _, s := setupManagerWithConfigKey(t)
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "empty"}))
	id, parts := stageOnePartUpload(t, m, "empty", "key", "")
	obj, err := m.CompleteMultipartUpload(t.Context(), id, parts)
	require.NoError(t, err)
	require.Zero(t, obj.Size)
	_, data := readWholeObject(t, m, "empty", "key")
	require.Empty(t, data)
}
