package object

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/require"
)

type pausedReadBackend struct {
	storage.Backend
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (b *pausedReadBackend) Get(ctx context.Context, ref storage.ObjectRef) (io.ReadCloser, map[string]string, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.resume
	return b.Backend.Get(ctx, ref)
}

func TestReadSnapshotDuringOverwrite(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(map[bool]string{false: "decrypted", true: "raw"}[raw], func(t *testing.T) {
			om, backend, meta := setupManagerWithConfigKey(t)
			ctx := t.Context()
			require.NoError(t, meta.CreateBucket(ctx, &metadata.BucketMetadata{Name: "snapshot", OwnerID: "owner"}))
			old, err := om.PutObject(ctx, "snapshot", "key", strings.NewReader("old"), http.Header{})
			require.NoError(t, err)
			paused := &pausedReadBackend{Backend: backend, entered: make(chan struct{}), resume: make(chan struct{})}
			om.storage = paused
			type readResult struct {
				obj     *Object
				reader  io.ReadCloser
				sidecar map[string]string
				err     error
			}
			read := make(chan readResult, 1)
			go func() {
				if raw {
					r, sidecar, m, err := om.GetObjectRaw(ctx, "snapshot", "key", "")
					var obj *Object
					if m != nil {
						obj = fromMetadataObject(m)
					}
					read <- readResult{obj, r, sidecar, err}
				} else {
					o, r, err := om.GetObject(ctx, "snapshot", "key")
					read <- readResult{obj: o, reader: r, err: err}
				}
			}()
			<-paused.entered
			written := make(chan error, 1)
			go func() {
				_, err := om.PutObject(ctx, "snapshot", "key", strings.NewReader("new-and-longer"), http.Header{})
				written <- err
			}()
			var early bool
			select {
			case err := <-written:
				early = true
				require.NoError(t, err)
			case <-time.After(100 * time.Millisecond):
			}
			close(paused.resume)
			result := <-read
			require.NoError(t, result.err)
			defer result.reader.Close()
			if !early {
				select {
				case err := <-written:
					require.NoError(t, err)
				case <-time.After(10 * time.Second):
					t.Fatal("overwrite blocked by an open reader")
				}
			}
			content, err := io.ReadAll(result.reader)
			require.NoError(t, err)
			require.False(t, early, "writer committed between metadata lookup and opening the data")
			require.Equal(t, old.ETag, result.obj.ETag)
			if raw {
				require.Equal(t, old.ETag, result.sidecar["original-etag"])
			} else {
				require.Equal(t, "old", string(content))
				require.Equal(t, int64(len(content)), result.obj.Size)
			}
		})
	}
}
