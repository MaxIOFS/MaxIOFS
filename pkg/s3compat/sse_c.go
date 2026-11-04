package s3compat

import (
	"crypto/md5"
	"encoding/base64"
	"errors"
	"net/http"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/object"
)

// SSE-C: the client sends the key of an object with each request about it.

const (
	sseCAlgorithmHeader = "x-amz-server-side-encryption-customer-algorithm"
	sseCKeyHeader       = "x-amz-server-side-encryption-customer-key"
	sseCKeyMD5Header    = "x-amz-server-side-encryption-customer-key-MD5"
	// copySourcePrefix names the key of a copy's source.
	copySourcePrefix = "x-amz-copy-source-"
)

// customerKey is the SSE-C key a request gives under prefix: "" for the object
// it names, copySourcePrefix for the source of a copy. It is nil when the
// request gives none. A request that gives one wrongly, or not over HTTPS, is
// answered, and ok is false.
func (h *Handler) customerKey(w http.ResponseWriter, r *http.Request, prefix, resource string) (key []byte, ok bool) {
	algorithm := r.Header.Get(prefix + sseCAlgorithmHeader)
	encoded := r.Header.Get(prefix + sseCKeyHeader)
	digest := r.Header.Get(prefix + sseCKeyMD5Header)
	if algorithm == "" && encoded == "" && digest == "" {
		return nil, true
	}
	fail := func(code, message string) ([]byte, bool) {
		h.writeError(w, code, message, resource, r)
		return nil, false
	}
	if !bucket.RequestFactsFrom(r.Context()).SecureTransport {
		return fail("InvalidRequest", "Requests specifying Server Side Encryption with Customer provided keys must be made over a secure connection.")
	}
	if algorithm == "" {
		return fail("InvalidArgument", "Requests specifying Server Side Encryption with Customer provided keys must provide a valid encryption algorithm.")
	}
	if algorithm != object.CustomerAlgorithm {
		return fail("InvalidEncryptionAlgorithmError", "The encryption request you specified is not valid. The valid value is AES256.")
	}
	if encoded == "" {
		return fail("InvalidArgument", "Requests specifying Server Side Encryption with Customer provided keys must provide an appropriate secret key.")
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != object.CustomerKeySize {
		return fail("InvalidArgument", "The secret key was invalid for the specified algorithm.")
	}
	if digest == "" {
		return fail("InvalidArgument", "Requests specifying Server Side Encryption with Customer provided keys must provide the client calculated MD5 of the secret key.")
	}
	sum := md5.Sum(key)
	if digest != base64.StdEncoding.EncodeToString(sum[:]) {
		return fail("InvalidArgument", "The calculated MD5 hash of the key did not match the hash that was provided.")
	}
	if prefix == "" && r.Header.Get("x-amz-server-side-encryption") != "" {
		return fail("InvalidArgument", "Server Side Encryption with Customer provided key is incompatible with the encryption method specified")
	}
	return key, true
}

// writeCustomerKey is customerKey for a write to bucketName, which refuses
// SSE-C unless its encryption configuration allows it, as in AWS since April
// 2026: 403 AccessDenied.
func (h *Handler) writeCustomerKey(w http.ResponseWriter, r *http.Request, bucketName, resource string) ([]byte, bool) {
	key, ok := h.customerKey(w, r, "", resource)
	if !ok || key == nil {
		return key, ok
	}
	cfg, err := h.bucketManager.GetEncryption(r.Context(), h.resolveBucketTenantID(r, bucketName), bucketName)
	if err != nil && !errors.Is(err, bucket.ErrEncryptionNotFound) {
		if errors.Is(err, bucket.ErrBucketNotFound) {
			h.writeError(w, "NoSuchBucket", "The specified bucket does not exist", bucketName, r)
			return nil, false
		}
		h.writeError(w, "InternalError", err.Error(), resource, r)
		return nil, false
	}
	if !cfg.AllowsCustomerKeys() {
		h.writeError(w, "AccessDenied", "Server Side Encryption with Customer provided keys is blocked for this bucket.", resource, r)
		return nil, false
	}
	return key, true
}

// setCustomerKeyHeaders answers a request about an object encrypted with key
// with the headers S3 gives it.
func setCustomerKeyHeaders(header http.Header, key []byte) {
	if key == nil {
		return
	}
	sum := md5.Sum(key)
	header.Set(sseCAlgorithmHeader, object.CustomerAlgorithm)
	header.Set(sseCKeyMD5Header, base64.StdEncoding.EncodeToString(sum[:]))
}

// writeCustomerKeyError answers a request whose customer key does not open
// the object it names, as S3 does; it reports whether err is such an error.
func (h *Handler) writeCustomerKeyError(w http.ResponseWriter, r *http.Request, resource string, err error) bool {
	switch {
	case errors.Is(err, object.ErrCustomerKeyRequired):
		h.writeError(w, "InvalidRequest", "The object was stored using a form of Server Side Encryption. The correct parameters must be provided to retrieve the object.", resource, r)
	case errors.Is(err, object.ErrCustomerKeyMismatch):
		h.writeError(w, "AccessDenied", "The provided encryption parameters did not match the ones used originally.", resource, r)
	case errors.Is(err, object.ErrCustomerKeyNotApplicable):
		h.writeError(w, "InvalidRequest", "The encryption parameters are not applicable to this object.", resource, r)
	default:
		return false
	}
	return true
}
