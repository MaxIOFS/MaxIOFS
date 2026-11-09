package object

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/maxiofs/maxiofs/internal/storage"
)

// uploadSeal is the key an upload is stored with. It is chosen before the
// content is received, so the content reaches the disk encrypted.
type uploadSeal struct {
	dek []byte // nil: stored as sent, as a folder marker is
	// envelope is what the sidecar keeps of a key the server's key wraps.
	envelope map[string]string
	// customerDEK and customerDEKIV are the key the client's key wraps.
	customerDEK, customerDEKIV string
}

// sealFor chooses the key an upload is stored with: one the client's key
// wraps, none for a folder marker, or one the server's key wraps.
func (om *objectManager) sealFor(customerKey []byte, folderMarker bool) (uploadSeal, error) {
	switch {
	case customerKey != nil:
		dek, err := om.encryptor.GenerateKey()
		if err != nil {
			return uploadSeal{}, fmt.Errorf("failed to generate DEK: %w", err)
		}
		wrapped, nonce, err := wrapCustomerDEK(dek, customerKey)
		if err != nil {
			return uploadSeal{}, fmt.Errorf("failed to wrap DEK with the customer key: %w", err)
		}
		return uploadSeal{dek: dek, customerDEK: wrapped, customerDEKIV: nonce}, nil
	case folderMarker:
		return uploadSeal{}, nil
	default:
		dek, envelope, err := om.newEnvelope()
		if err != nil {
			return uploadSeal{}, err
		}
		return uploadSeal{dek: dek, envelope: envelope}, nil
	}
}

// spool writes what src gives to dst, encrypted with dek unless it is nil, as
// it is received, and reports how many bytes src gave. A source that fails,
// io.ErrUnexpectedEOF included, fails the spool with its own error.
func (om *objectManager) spool(dst io.Writer, src io.Reader, dek []byte) (int64, error) {
	received := &receivedBytes{r: src}
	out := bufio.NewWriterSize(dst, 256<<10)
	var err error
	if dek == nil {
		_, err = io.Copy(out, received)
	} else {
		_, err = om.encryptor.EncryptStream(received, out, dek)
	}
	if received.err != nil {
		return received.n, received.err
	}
	if err == nil {
		err = out.Flush()
	}
	return received.n, err
}

// receivedBytes counts what r gives and keeps the first error it fails with.
type receivedBytes struct {
	r   io.Reader
	n   int64
	err error
}

func (c *receivedBytes) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if err != nil && err != io.EOF && c.err == nil {
		c.err = err
	}
	return n, err
}

// setEnvelopeFields records in storageMetadata an object stored encrypted with
// a key the server's key wraps: its size and ETag are those of its content.
func setEnvelopeFields(storageMetadata map[string]string, originalSize int64, originalETag string, envelope map[string]string) {
	storageMetadata["original-size"] = strconv.FormatInt(originalSize, 10)
	storageMetadata["original-etag"] = originalETag
	storageMetadata["encrypted"] = "true"
	storageMetadata["x-amz-server-side-encryption"] = "AES256"
	storageMetadata["x-amz-server-side-encryption-algorithm"] = "AES-256-GCM-STREAM"
	for k, v := range envelope {
		storageMetadata[k] = v
	}
}

// storeSealedObject stores the content tempPath holds encrypted with the key
// envelope wraps, as it was received.
func (om *objectManager) storeSealedObject(ctx context.Context, ref storage.ObjectRef, tempPath string, storageMetadata map[string]string, originalSize int64, originalETag string, envelope map[string]string) error {
	setEnvelopeFields(storageMetadata, originalSize, originalETag, envelope)
	sealed, err := os.Open(tempPath)
	if err != nil {
		return fmt.Errorf("failed to open temp file: %w", err)
	}
	defer sealed.Close()
	if err := om.storage.Put(ctx, ref, sealed, storageMetadata); err != nil {
		return fmt.Errorf("failed to store object: %w", err)
	}
	return nil
}
