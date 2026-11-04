package object

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strconv"

	"github.com/maxiofs/maxiofs/internal/storage"
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

// storeCustomerEncryptedObject encrypts and stores the object in tempPath
// with a fresh DEK wrapped with the customer's key, and returns the wrap and
// its nonce. Its ETag, set as the sidecar's original-etag before storage
// writes the sidecar, is the MD5 of the bytes stored: the MD5 of its content
// is kept nowhere, as in S3.
func (om *objectManager) storeCustomerEncryptedObject(ctx context.Context, ref storage.ObjectRef, tempPath string, storageMetadata map[string]string, originalSize int64, key []byte) (string, string, error) {
	dek, err := om.encryptor.GenerateKey()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate DEK: %w", err)
	}
	wrapped, nonce, err := wrapCustomerDEK(dek, key)
	if err != nil {
		return "", "", fmt.Errorf("failed to wrap DEK with the customer key: %w", err)
	}
	storageMetadata["original-size"] = strconv.FormatInt(originalSize, 10)
	storageMetadata["encrypted"] = "true"
	storageMetadata["x-amz-server-side-encryption-algorithm"] = "AES-256-GCM-STREAM"
	storageMetadata[sidecarCustomerAlgorithm] = CustomerAlgorithm
	storageMetadata[sidecarCustomerDEK] = wrapped
	storageMetadata[sidecarCustomerDEKIV] = nonce

	plain, err := os.Open(tempPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to open temp file for encryption: %w", err)
	}
	defer plain.Close()
	pr, pw := io.Pipe()
	defer pr.Close()
	go func() {
		_, err := om.encryptor.EncryptStream(plain, pw, dek)
		pw.CloseWithError(err)
	}()
	stored := &etagAtEnd{r: pr, hash: md5.New(), meta: storageMetadata}
	if err := om.storage.Put(ctx, ref, stored, storageMetadata); err != nil {
		return "", "", fmt.Errorf("failed to store object: %w", err)
	}
	return wrapped, nonce, nil
}

// etagAtEnd hashes the bytes read through it and, at their end, sets their
// MD5 as meta's original-etag.
type etagAtEnd struct {
	r    io.Reader
	hash hash.Hash
	meta map[string]string
}

func (e *etagAtEnd) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	e.hash.Write(p[:n])
	if err == io.EOF {
		e.meta["original-etag"] = hex.EncodeToString(e.hash.Sum(nil))
	}
	return n, err
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
