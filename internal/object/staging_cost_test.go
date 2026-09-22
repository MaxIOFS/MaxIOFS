package object

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/maxiofs/maxiofs/pkg/encryption"
	"github.com/stretchr/testify/require"
)

type stageWrite struct {
	Encrypted   bool  `json:"encrypted"`
	Bytes       int64 `json:"bytes"`
	StagedBytes int64 `json:"staged_bytes_at_put"`
}

type stageProbe struct {
	storage.Backend
	root                                     string
	writes                                   []stageWrite
	partBytes, plaintextBytes, previousBytes atomic.Int64
	openParts, peakParts                     atomic.Int64
}

type stageEncryptor struct {
	encryption.Encryptor
	read atomic.Int64
}

func (e *stageEncryptor) EncryptStream(src io.Reader, dst io.Writer, key []byte) (*encryption.EncryptionMetadata, error) {
	return e.Encryptor.EncryptStream(&costReader{src, &e.read}, dst, key)
}

func (p *stageProbe) Put(ctx context.Context, ref storage.ObjectRef, r io.Reader, meta map[string]string) error {
	entry := stageWrite{Encrypted: meta["encrypted"] == "true"}
	files, err := os.ReadDir(p.root)
	if err != nil {
		return err
	}
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "maxiofs-upload-") || strings.HasPrefix(f.Name(), "maxiofs-multipart-") {
			info, err := f.Info()
			if err != nil {
				return err
			}
			entry.StagedBytes += info.Size()
		}
	}
	var count atomic.Int64
	err = p.Backend.Put(ctx, ref, &costReader{r, &count}, meta)
	entry.Bytes = count.Load()
	p.writes = append(p.writes, entry)
	return err
}

func (p *stageProbe) Get(ctx context.Context, ref storage.ObjectRef) (io.ReadCloser, map[string]string, error) {
	r, meta, err := p.Backend.Get(ctx, ref)
	if err != nil {
		return r, meta, err
	}
	count := &p.plaintextBytes
	if meta["encrypted"] == "true" {
		count = &p.previousBytes
	}
	return &costReadCloser{&costReader{r, count}, r}, meta, nil
}

type stagePartReader struct {
	*costReadCloser
	open *atomic.Int64
}

func (r *stagePartReader) Close() error {
	err := r.costReadCloser.Close()
	r.open.Add(-1)
	return err
}

func (p *stageProbe) GetPart(ctx context.Context, id string, n int) (io.ReadCloser, map[string]string, error) {
	r, meta, err := p.Backend.GetPart(ctx, id, n)
	if err != nil {
		return r, meta, err
	}
	open := p.openParts.Add(1)
	for peak := p.peakParts.Load(); open > peak; peak = p.peakParts.Load() {
		if p.peakParts.CompareAndSwap(peak, open) {
			break
		}
	}
	return &stagePartReader{&costReadCloser{&costReader{r, &p.partBytes}, r}, &p.openParts}, meta, nil
}

func runStagingCost(t *testing.T, multipart, overwrite bool, partCount, partSize int) {
	t.Helper()
	m, s := openFaultManager(t, t.TempDir())
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	require.NoError(t, s.CreateBucket(t.Context(), &metadata.BucketMetadata{Name: "cost"}))
	require.NoError(t, m.storage.CreateBucket(t.Context(), "cost"))
	total := partCount * partSize
	if overwrite {
		_, err := m.PutObject(t.Context(), "cost", "key", bytes.NewReader(bytes.Repeat([]byte{0x31}, total)), http.Header{})
		require.NoError(t, err)
	}
	var parts []Part
	var uploadID string
	var payload []byte
	var partDigests []byte
	if multipart {
		u, err := m.CreateMultipartUpload(t.Context(), "cost", "key", http.Header{"X-Amz-Meta-Test": {"staging"}})
		require.NoError(t, err)
		uploadID = u.UploadID
		for i := range partCount {
			data := bytes.Repeat([]byte{byte(i + 1)}, partSize)
			p, err := m.UploadPart(t.Context(), uploadID, i+1, bytes.NewReader(data))
			require.NoError(t, err)
			parts = append(parts, *p)
			payload = append(payload, data...)
			digest := md5.Sum(data)
			partDigests = append(partDigests, digest[:]...)
		}
	} else {
		payload = bytes.Repeat([]byte{0x72}, total)
	}
	backend := m.storage
	probe := &stageProbe{Backend: backend, root: m.config.Root}
	m.storage = probe
	enc := &stageEncryptor{Encryptor: m.encryptor}
	m.encryptor = enc
	counts := &writeCostCounters{}
	m.metadataStore = &costStore{s, counts}
	var obj *Object
	var err error
	if multipart {
		obj, err = m.CompleteMultipartUpload(t.Context(), uploadID, parts)
	} else {
		obj, err = m.PutObject(t.Context(), "cost", "key", bytes.NewReader(payload), http.Header{})
	}
	require.NoError(t, err)
	m.storage = backend
	m.metadataStore = s
	m.encryptor = enc.Encryptor
	report, err := json.Marshal(map[string]any{
		"multipart": multipart, "overwrite": overwrite, "parts": partCount, "payload_bytes": total,
		"backend_writes": probe.writes, "part_read_bytes": probe.partBytes.Load(),
		"assembled_plaintext_read_bytes": probe.plaintextBytes.Load(), "previous_object_read_bytes": probe.previousBytes.Load(),
		"peak_open_part_readers":       probe.peakParts.Load(),
		"encryption_source_read_bytes": enc.read.Load(), "part_metadata_lookups": counts.partReads.Load(),
	})
	require.NoError(t, err)
	t.Logf("STAGING_COST %s", report)
	require.Equal(t, int64(total), enc.read.Load())
	require.NotEmpty(t, probe.writes)
	require.Zero(t, probe.openParts.Load())
	_, reader, err := m.GetObject(t.Context(), "cost", "key")
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, reader.Close())
	require.NoError(t, err)
	require.Equal(t, payload, data)
	require.Equal(t, int64(total), obj.Size)
	if multipart {
		require.Len(t, probe.writes, 1, "completion must publish only encrypted data")
		require.True(t, probe.writes[0].Encrypted)
		require.Zero(t, probe.writes[0].StagedBytes, "completion must not stage plaintext")
		require.Zero(t, probe.plaintextBytes.Load())
		require.Equal(t, int64(total), probe.partBytes.Load())
		require.Equal(t, int64(1), probe.peakParts.Load())
		digest := md5.Sum(partDigests)
		require.Equal(t, fmt.Sprintf("%s-%d", hex.EncodeToString(digest[:]), partCount), obj.ETag)
		require.Equal(t, "staging", obj.Metadata["test"])
		_, err := s.GetMultipartUpload(t.Context(), uploadID)
		require.ErrorIs(t, err, metadata.ErrUploadNotFound)
		for i := range partCount {
			exists, err := backend.PartExists(t.Context(), uploadID, i+1)
			require.NoError(t, err)
			require.False(t, exists)
		}
	} else {
		digest := md5.Sum(payload)
		require.Equal(t, hex.EncodeToString(digest[:]), obj.ETag)
	}
	stored, err := backend.GetMetadata(t.Context(), storage.ObjectRef{Bucket: "cost", Key: "key"})
	require.NoError(t, err)
	require.Equal(t, "true", stored["encrypted"])
	digest := md5.Sum(payload)
	require.Equal(t, hex.EncodeToString(digest[:]), stored["original-etag"])
	files, err := os.ReadDir(m.config.Root)
	require.NoError(t, err)
	for _, f := range files {
		require.False(t, strings.HasPrefix(f.Name(), "maxiofs-"), "temporary file left: %s", filepath.Join(m.config.Root, f.Name()))
	}
}

func TestStagingPassAccounting(t *testing.T) {
	for _, multipart := range []bool{false, true} {
		for _, overwrite := range []bool{false, true} {
			t.Run(fmt.Sprintf("multipart=%t/overwrite=%t", multipart, overwrite), func(t *testing.T) {
				runStagingCost(t, multipart, overwrite, 1, 4096)
			})
		}
	}
}

func TestMultipartPassCost(t *testing.T) {
	if os.Getenv("MAXIOFS_WRITE_COST") != "1" {
		t.Skip("set MAXIOFS_WRITE_COST=1 to run the disk workload")
	}
	for _, parts := range []int{2, 8} {
		for _, overwrite := range []bool{false, true} {
			t.Run(fmt.Sprintf("parts=%d/overwrite=%t", parts, overwrite), func(t *testing.T) {
				runStagingCost(t, true, overwrite, parts, 5<<20)
			})
		}
	}
}
