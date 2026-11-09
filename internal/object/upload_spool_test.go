package object

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An upload reaches the disk encrypted: its temp file never holds the content
// in clear, with the server's key or with a customer's, and what is read back
// is what was sent.
func TestAnUploadIsEncryptedBeforeItReachesTheDisk(t *testing.T) {
	om, store := newNodeManager(t, newKEKStore(t))
	ctx := context.Background()
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "spool", OwnerID: "u"}))
	content := bytes.Repeat([]byte("MAXIOFS-PLAINTEXT-MARKER-"), 200_000) // 5 MB

	for name, keys := range map[string]struct{ write, read context.Context }{
		"server key":   {ctx, ctx},
		"customer key": {WithWriteKey(ctx, customerKeyA), WithReadKey(ctx, customerKeyA)},
	} {
		t.Run(name, func(t *testing.T) {
			src := &watchedUpload{t: t, root: om.config.Root, r: bytes.NewReader(content)}
			_, err := om.PutObject(keys.write, "spool", name, src, http.Header{})
			require.NoError(t, err)
			assert.Positive(t, src.seen, "the temp file was read with content in it")

			_, body, err := om.GetObject(keys.read, "spool", name)
			require.NoError(t, err)
			got, err := io.ReadAll(body)
			body.Close()
			require.NoError(t, err)
			assert.True(t, bytes.Equal(content, got), "what is read back is what was sent")
		})
	}
}

// watchedUpload gives what r gives and, at each MiB, reads the upload temp
// files under root: none may hold the content in clear.
type watchedUpload struct {
	t           *testing.T
	root        string
	r           io.Reader
	given, next int64
	seen        int64
}

func (w *watchedUpload) Read(p []byte) (int, error) {
	if w.given >= w.next {
		w.next += 1 << 20
		files, _ := filepath.Glob(filepath.Join(w.root, "maxiofs-upload-*"))
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			w.seen += int64(len(data))
			assert.False(w.t, bytes.Contains(data, []byte("PLAINTEXT-MARKER")), "a temp file holds the content in clear")
		}
	}
	n, err := w.r.Read(p)
	w.given += int64(n)
	return n, err
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

// A body cut short fails its write and is not stored as what came of it: a PUT
// and a part of an upload alike.
func TestABodyCutShortIsNotStored(t *testing.T) {
	om, store := newNodeManager(t, newKEKStore(t))
	ctx := context.Background()
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "spool", OwnerID: "u"}))
	cut := func() io.Reader {
		return io.MultiReader(bytes.NewReader(bytes.Repeat([]byte("x"), 100_000)), failingReader{io.ErrUnexpectedEOF})
	}

	_, err := om.PutObject(ctx, "spool", "cut", cut(), http.Header{})
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	_, err = om.GetObjectMetadata(ctx, "spool", "cut")
	assert.ErrorIs(t, err, ErrObjectNotFound)

	upload, err := om.CreateMultipartUpload(ctx, "spool", "parts", http.Header{})
	require.NoError(t, err)
	_, err = om.UploadPart(ctx, upload.UploadID, 1, cut())
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	parts, err := om.ListParts(ctx, upload.UploadID)
	require.NoError(t, err)
	assert.Empty(t, parts, "a part cut short is not listed")
}
