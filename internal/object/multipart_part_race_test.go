package object

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pausedPartReadBackend struct {
	storage.Backend
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (b *pausedPartReadBackend) GetPart(ctx context.Context, uploadID string, partNumber int) (io.ReadCloser, map[string]string, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.resume
	return b.Backend.GetPart(ctx, uploadID, partNumber)
}

// The completion validates each part's ETag and then opens the part files.
// A part replaced in between would be assembled under an ETag describing the
// bytes it no longer holds.
func TestUploadPartCannotReplaceAPartBeingAssembled(t *testing.T) {
	ctx := context.Background()
	om, _, metaStore := setupManagerWithConfigKey(t)

	bucketName := "mpu-race-bucket"
	key := "victim.txt"
	require.NoError(t, metaStore.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:    bucketName,
		OwnerID: "user-1",
	}))

	upload, err := om.CreateMultipartUpload(ctx, bucketName, key, http.Header{"Content-Type": []string{"text/plain"}})
	require.NoError(t, err)
	part, err := om.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader([]byte("original part")))
	require.NoError(t, err)

	paused := &pausedPartReadBackend{Backend: om.storage, entered: make(chan struct{}), resume: make(chan struct{})}
	om.storage = paused

	type completion struct {
		obj *Object
		err error
	}
	completed := make(chan completion, 1)
	go func() {
		obj, err := om.CompleteMultipartUpload(ctx, upload.UploadID, []Part{*part})
		completed <- completion{obj, err}
	}()
	<-paused.entered

	tampered := make(chan struct{})
	go func() {
		defer close(tampered)
		_, _ = om.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader([]byte("tampered part, longer")))
	}()

	select {
	case <-tampered:
		t.Fatal("a part was replaced while the completion was assembling it")
	case <-time.After(200 * time.Millisecond):
	}

	close(paused.resume)
	result := <-completed
	require.NoError(t, result.err)
	<-tampered

	_, reader, err := om.GetObject(ctx, bucketName, key)
	require.NoError(t, err)
	defer reader.Close() //nolint:errcheck
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.Equal(t, "original part", string(data), "the assembled object must hold the bytes whose ETag was validated")
	assert.Equal(t, int64(len("original part")), result.obj.Size)
}
