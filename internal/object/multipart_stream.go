package object

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"strconv"

	"github.com/maxiofs/maxiofs/internal/storage"
)

type multipartPartReader struct {
	ctx      context.Context
	backend  storage.Backend
	uploadID string
	parts    []Part
	next     int
	current  io.ReadCloser
	size     int64
	// decrypt gives the bytes sent of a part stored encrypted with the
	// upload's key.
	decrypt func(io.ReadCloser) (io.ReadCloser, error)
}

func (r *multipartPartReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		if r.current == nil {
			if r.next == len(r.parts) {
				return 0, io.EOF
			}
			part := r.parts[r.next]
			reader, partMeta, err := r.backend.GetPart(r.ctx, r.uploadID, part.PartNumber)
			if err != nil {
				return 0, fmt.Errorf("open part %d: %w", part.PartNumber, err)
			}
			if partMeta["encrypted"] == "true" {
				if r.decrypt == nil {
					reader.Close()
					return 0, fmt.Errorf("part %d is encrypted and the upload has no key", part.PartNumber)
				}
				if reader, err = r.decrypt(reader); err != nil {
					return 0, fmt.Errorf("decrypt part %d: %w", part.PartNumber, err)
				}
			}
			r.current = reader
			r.next++
		}
		n, err := r.current.Read(p)
		r.size += int64(n)
		if err == io.EOF {
			if closeErr := r.Close(); closeErr != nil {
				return n, closeErr
			}
			if n != 0 {
				return n, nil
			}
			continue
		}
		if err != nil {
			return n, fmt.Errorf("read part %d: %w", r.parts[r.next-1].PartNumber, err)
		}
		return n, nil
	}
}

func (r *multipartPartReader) Close() error {
	if r.current == nil {
		return nil
	}
	err := r.current.Close()
	r.current = nil
	if err != nil {
		return fmt.Errorf("close multipart part: %w", err)
	}
	return nil
}

// decryptedPart reads a part decrypted; closing it closes the part's file.
type decryptedPart struct {
	io.ReadCloser
	file io.Closer
}

func (d *decryptedPart) Close() error {
	return errors.Join(d.ReadCloser.Close(), d.file.Close())
}

type multipartCiphertextReader struct {
	*io.PipeReader
	done     <-chan struct{}
	hash     hash.Hash
	metadata map[string]string
	// keepETag: the sidecar keeps the multipart ETag; the content's MD5 is
	// not kept, as for an object with a customer key.
	keepETag bool
}

func (r *multipartCiphertextReader) Read(p []byte) (int, error) {
	n, err := r.PipeReader.Read(p)
	if err == io.EOF {
		<-r.done
		// Finalize on the consumer goroutine, before storage publishes the sidecar.
		if !r.keepETag {
			r.metadata["original-etag"] = hex.EncodeToString(r.hash.Sum(nil))
		}
	}
	return n, err
}

func (om *objectManager) storeEncryptedMultipartObject(ctx context.Context, ref storage.ObjectRef, parts []Part, uploadID string, multipart *MultipartUpload, originalSize int64, multipartETag string) error {
	partKey, err := om.uploadKey(multipart)
	if err != nil {
		return fmt.Errorf("failed to open the upload's key: %w", err)
	}
	// The sidecar holds storage fields only, as a PUT's does: user metadata and
	// upload state stay in the index.
	meta := uploadStorageMetadata(multipart.Metadata)
	customer := multipart.CustomerDEK != ""
	var dek []byte
	if customer {
		// The upload's key is the object's: the customer's key wraps it.
		if partKey == nil {
			return fmt.Errorf("an upload with a customer key has no key of its own")
		}
		dek = partKey
		meta[sidecarCustomerAlgorithm] = CustomerAlgorithm
		meta[sidecarCustomerDEK] = multipart.CustomerDEK
		meta[sidecarCustomerDEKIV] = multipart.CustomerDEKIV
	} else {
		var envelopeMeta map[string]string
		if dek, envelopeMeta, err = om.newEnvelope(); err != nil {
			return err
		}
		for k, v := range envelopeMeta {
			meta[k] = v
		}
		meta["x-amz-server-side-encryption"] = "AES256"
	}
	meta["content-type"] = multipart.Metadata["content-type"]
	if meta["content-type"] == "" {
		meta["content-type"] = "application/octet-stream"
	}
	meta["original-size"] = strconv.FormatInt(originalSize, 10)
	meta["original-etag"] = multipartETag
	meta["multipart-etag"] = multipartETag
	meta["encrypted"] = "true"
	meta["x-amz-server-side-encryption-algorithm"] = "AES-256-GCM-STREAM"

	source := &multipartPartReader{ctx: ctx, backend: om.storage, uploadID: uploadID, parts: parts}
	if partKey != nil {
		source.decrypt = func(part io.ReadCloser) (io.ReadCloser, error) {
			// The part's file is closed by the reader, which reports how.
			plain := om.decryptingWith(ctx, io.NopCloser(part), partKey, "AES-256-GCM-STREAM")
			return &decryptedPart{ReadCloser: plain, file: part}, nil
		}
	}
	pr, pw := io.Pipe()
	done := make(chan struct{})
	var producerErr error
	hasher := md5.New()
	stop := context.AfterFunc(ctx, func() { _ = pr.CloseWithError(ctx.Err()) })
	defer stop()
	defer func() {
		_ = pr.Close()
		<-done
	}()
	go func() {
		defer close(done)
		_, err := om.encryptor.EncryptStream(io.TeeReader(source, hasher), pw, dek)
		err = errors.Join(err, source.Close())
		if err == nil && source.size != originalSize {
			err = fmt.Errorf("multipart size mismatch: expected %d, read %d", originalSize, source.size)
		}
		if err == nil {
			err = ctx.Err()
		}
		producerErr = err
		_ = pw.CloseWithError(err)
	}()
	reader := &multipartCiphertextReader{PipeReader: pr, done: done, hash: hasher, metadata: meta, keepETag: customer}
	putErr := om.storage.Put(ctx, ref, reader, meta)
	_ = pr.Close()
	<-done
	if err := errors.Join(putErr, producerErr, ctx.Err()); err != nil {
		return fmt.Errorf("failed to store encrypted multipart object: %w", err)
	}
	return nil
}
