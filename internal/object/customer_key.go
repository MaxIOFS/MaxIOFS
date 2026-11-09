package object

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/maxiofs/maxiofs/pkg/encryption"
)

// SSE-C: an object encrypted with a key its client sends with each request.
// The object's data is encrypted with a DEK of its own, as every object's is;
// that DEK is wrapped with the client's key, not with the KEK, and the wrap is
// kept in the object's entry and sidecar. MaxIOFS keeps no copy of the key.

// CustomerAlgorithm is the one algorithm SSE-C takes.
const CustomerAlgorithm = "AES256"

// CustomerKeySize is the size of an SSE-C key: 256 bits.
const CustomerKeySize = 32

// Sidecar fields of an object encrypted with a customer key.
const (
	sidecarCustomerAlgorithm = storage.MetadataCustomerAlgorithmField
	sidecarCustomerDEK       = storage.MetadataCustomerDEKField
	sidecarCustomerDEKIV     = storage.MetadataCustomerDEKIVField
)

var (
	// ErrCustomerKeyRequired: the object is encrypted with a customer key the
	// request does not give.
	ErrCustomerKeyRequired = errors.New("the object is encrypted with a customer key the request does not give")
	// ErrCustomerKeyMismatch: the request gives another key than the one the
	// object was written with.
	ErrCustomerKeyMismatch = errors.New("the customer key is not the one the object was written with")
	// ErrCustomerKeyNotApplicable: the request gives a customer key for an
	// object that is not encrypted with one.
	ErrCustomerKeyNotApplicable = errors.New("the object is not encrypted with a customer key")
)

type readKeyCtx struct{}
type writeKeyCtx struct{}

// WithReadKey carries the customer key reads in ctx decrypt with.
func WithReadKey(ctx context.Context, key []byte) context.Context {
	if key == nil {
		return ctx
	}
	return context.WithValue(ctx, readKeyCtx{}, key)
}

// WithWriteKey carries the customer key writes in ctx encrypt with.
func WithWriteKey(ctx context.Context, key []byte) context.Context {
	if key == nil {
		return ctx
	}
	return context.WithValue(ctx, writeKeyCtx{}, key)
}

func readKeyFrom(ctx context.Context) []byte {
	key, _ := ctx.Value(readKeyCtx{}).([]byte)
	return key
}

func writeKeyFrom(ctx context.Context) []byte {
	key, _ := ctx.Value(writeKeyCtx{}).([]byte)
	return key
}

// CustomerEncrypted reports whether o is encrypted with a customer key.
func (o *Object) CustomerEncrypted() bool {
	return o != nil && o.CustomerDEK != ""
}

// CheckCustomerKey reports whether key opens o: ErrCustomerKeyRequired when o
// is encrypted with a customer key and key is nil, ErrCustomerKeyMismatch for
// another key, ErrCustomerKeyNotApplicable for a key given for an object that
// is not encrypted with one.
func CheckCustomerKey(o *Object, key []byte) error {
	if !o.CustomerEncrypted() {
		if key != nil {
			return ErrCustomerKeyNotApplicable
		}
		return nil
	}
	_, err := unwrapCustomerDEK(o.CustomerDEK, o.CustomerDEKIV, key)
	return err
}

// wrapCustomerDEK wraps dek with key: the wrap and its nonce, in hex.
func wrapCustomerDEK(dek, key []byte) (string, string, error) {
	gcm, err := customerGCM(key)
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", fmt.Errorf("failed to make a nonce: %w", err)
	}
	return hex.EncodeToString(gcm.Seal(nil, nonce, dek, nil)), hex.EncodeToString(nonce), nil
}

// unwrapCustomerDEK opens the DEK wrapped with the customer key: nil key is
// ErrCustomerKeyRequired, a key that does not open it ErrCustomerKeyMismatch.
func unwrapCustomerDEK(wrappedHex, nonceHex string, key []byte) ([]byte, error) {
	if key == nil {
		return nil, ErrCustomerKeyRequired
	}
	wrapped, err := hex.DecodeString(wrappedHex)
	if err != nil {
		return nil, fmt.Errorf("corrupt customer DEK: %w", err)
	}
	nonce, err := hex.DecodeString(nonceHex)
	if err != nil {
		return nil, fmt.Errorf("corrupt customer DEK nonce: %w", err)
	}
	gcm, err := customerGCM(key)
	if err != nil {
		return nil, ErrCustomerKeyMismatch
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("corrupt customer DEK nonce: %d bytes", len(nonce))
	}
	dek, err := gcm.Open(nil, nonce, wrapped, nil)
	if err != nil {
		return nil, ErrCustomerKeyMismatch
	}
	return dek, nil
}

func customerGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != CustomerKeySize {
		return nil, fmt.Errorf("a customer key is %d bytes, not %d", CustomerKeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// customerSidecar reports whether a sidecar is of an object encrypted with a
// customer key.
func customerSidecar(sidecar map[string]string) bool {
	return sidecar[sidecarCustomerDEK] != ""
}

// storeCustomerSealedObject stores the content tempPath holds encrypted with
// the key the customer's key wraps in sealed, as it was received. Its ETag is
// storedETag, the MD5 of the bytes stored: the MD5 of its content is kept
// nowhere, as in S3.
func (om *objectManager) storeCustomerSealedObject(ctx context.Context, ref storage.ObjectRef, tempPath string, storageMetadata map[string]string, originalSize int64, sealed uploadSeal, storedETag string) error {
	storageMetadata["original-size"] = strconv.FormatInt(originalSize, 10)
	storageMetadata["original-etag"] = storedETag
	storageMetadata["encrypted"] = "true"
	storageMetadata["x-amz-server-side-encryption-algorithm"] = "AES-256-GCM-STREAM"
	storageMetadata[sidecarCustomerAlgorithm] = CustomerAlgorithm
	storageMetadata[sidecarCustomerDEK] = sealed.customerDEK
	storageMetadata[sidecarCustomerDEKIV] = sealed.customerDEKIV

	stored, err := os.Open(tempPath)
	if err != nil {
		return fmt.Errorf("failed to open temp file: %w", err)
	}
	defer stored.Close()
	if err := om.storage.Put(ctx, ref, stored, storageMetadata); err != nil {
		return fmt.Errorf("failed to store object: %w", err)
	}
	return nil
}

// dataKeyFor is the DEK of the stored object sidecar describes: unwrapped
// with the customer key reads in ctx give for an object encrypted with one,
// with the KEK otherwise.
func (om *objectManager) dataKeyFor(ctx context.Context, sidecar map[string]string) ([]byte, error) {
	if customerSidecar(sidecar) {
		return unwrapCustomerDEK(sidecar[sidecarCustomerDEK], sidecar[sidecarCustomerDEKIV], readKeyFrom(ctx))
	}
	return om.decryptionKeyFor(sidecar)
}

// newUploadKey gives mu a DEK of its own for its parts, wrapped with the KEK,
// and also with the customer key writes in ctx give, if any: the object the
// upload completes is encrypted with it.
func (om *objectManager) newUploadKey(ctx context.Context, mu *MultipartUpload) error {
	dek, envelope, err := om.newEnvelope()
	if err != nil {
		return err
	}
	mu.PartDEK, mu.PartDEKIV = envelope["wrapped-dek"], envelope["wrapped-dek-iv"]
	if mu.PartKEKVersion, err = strconv.Atoi(envelope["kek-version"]); err != nil {
		return fmt.Errorf("corrupt KEK version: %w", err)
	}
	if key := writeKeyFrom(ctx); key != nil {
		if mu.CustomerDEK, mu.CustomerDEKIV, err = wrapCustomerDEK(dek, key); err != nil {
			return fmt.Errorf("failed to wrap DEK with the customer key: %w", err)
		}
	}
	return nil
}

// uploadKey is the DEK mu's parts are encrypted with; nil for an upload
// started before parts were encrypted, whose parts are stored as sent.
func (om *objectManager) uploadKey(mu *MultipartUpload) ([]byte, error) {
	if mu.PartDEK == "" {
		return nil, nil
	}
	return om.decryptionKeyFor(map[string]string{
		"wrapped-dek":    mu.PartDEK,
		"wrapped-dek-iv": mu.PartDEKIV,
		"kek-version":    strconv.Itoa(mu.PartKEKVersion),
	})
}

// checkUploadKey reports whether key is the one mu was started with:
// ErrCustomerKeyRequired, ErrCustomerKeyMismatch or, for a key given to an
// upload started without one, ErrCustomerKeyNotApplicable.
func checkUploadKey(mu *MultipartUpload, key []byte) error {
	if mu.CustomerDEK == "" {
		if key != nil {
			return ErrCustomerKeyNotApplicable
		}
		return nil
	}
	_, err := unwrapCustomerDEK(mu.CustomerDEK, mu.CustomerDEKIV, key)
	return err
}

// countingMD5 hashes and counts the bytes read through it.
type countingMD5 struct {
	r    io.Reader
	hash hash.Hash
	n    int64
}

func (c *countingMD5) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.hash.Write(p[:n])
	c.n += int64(n)
	return n, err
}

// encrypting gives the bytes of src encrypted with dek, as EncryptStream
// writes them; closing it stops the encryption.
func (om *objectManager) encrypting(src io.Reader, dek []byte) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		_, err := om.encryptor.EncryptStream(src, pw, dek)
		pw.CloseWithError(err)
	}()
	return pr
}

// storedSize is the size n bytes are stored as, encrypted by EncryptStream:
// a 12-byte nonce, then each chunk of up to 64 KiB with a 4-byte length and
// a 16-byte tag.
func storedSize(n int64) int64 {
	const chunk = 64 * 1024
	return 12 + n + (n+chunk-1)/chunk*20
}

// CutSize is the number of bytes of o's write that parity cuts in shards and
// a whole copy carries between nodes: its content, or, for an object
// encrypted with a customer key, which the server cannot decrypt, the bytes
// it is stored as.
func CutSize(o *Object) int64 {
	if o.CustomerEncrypted() {
		return storedSize(o.Size)
	}
	return o.Size
}

// DecryptCustomer gives the content of o, encrypted with a customer key,
// from stored, the bytes it is stored as; the key reads in ctx give opens
// it. It closes stored.
func DecryptCustomer(ctx context.Context, stored io.ReadCloser, o *Object) (io.ReadCloser, error) {
	dek, err := unwrapCustomerDEK(o.CustomerDEK, o.CustomerDEKIV, readKeyFrom(ctx))
	if err != nil {
		stored.Close()
		return nil, err
	}
	return decryptingWith(ctx, encryption.NewAESGCMEncryptor(nil), stored, dek, "AES-256-GCM-STREAM"), nil
}

// StoredCopy is what a whole copy of o, encrypted with a customer key, is
// stored as with the bytes it is stored as: its sidecar and its entry.
func StoredCopy(o *Object) (map[string]string, *metadata.ObjectMetadata) {
	sidecar := map[string]string{
		"encrypted":                              "true",
		"x-amz-server-side-encryption-algorithm": "AES-256-GCM-STREAM",
		sidecarCustomerAlgorithm:                 CustomerAlgorithm,
		sidecarCustomerDEK:                       o.CustomerDEK,
		sidecarCustomerDEKIV:                     o.CustomerDEKIV,
		"original-size":                          strconv.FormatInt(o.Size, 10),
		"original-etag":                          o.ETag,
		"content-type":                           o.ContentType,
	}
	if strings.Contains(o.ETag, "-") {
		sidecar["multipart-etag"] = o.ETag
	}
	if o.StorageClass != "" {
		sidecar["storage-class"] = o.StorageClass
	}
	stored := http.Header{}
	o.SetStoredHeaders(stored)
	for _, name := range StoredHeaders {
		if v := stored.Get(name); v != "" {
			sidecar[strings.ToLower(name)] = v
		}
	}
	return sidecar, toMetadataObject(o)
}
