package middleware

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"strings"
)

// ClusterBodyDigestHeader declares the digest of a streamed body, in the form
// "md5:<hex>" or "sha256:<hex>". A sender that already knows the digest of what
// it is about to stream — a replica reading an object off its own disk — can
// put it here instead of declaring the body unsigned. The value is covered by
// the request signature, so the receiver can verify the bytes as they arrive.
const ClusterBodyDigestHeader = "X-MaxIOFS-Body-Digest"

func newDigestHasher(algorithm string) (hash.Hash, error) {
	switch algorithm {
	case "md5":
		return md5.New(), nil
	case "sha256":
		return sha256.New(), nil
	default:
		return nil, fmt.Errorf("unsupported body digest algorithm %q", algorithm)
	}
}

// verifyingBody streams the body through a hash and fails the final Read when
// the bytes do not match what the sender signed. Failing at EOF rather than up
// front is what lets a large object stream: the handler writes to its staging
// path and the error aborts the commit.
type verifyingBody struct {
	inner    io.ReadCloser
	hasher   hash.Hash
	expected string
	checked  bool
}

func newVerifyingBody(inner io.ReadCloser, declared string) (io.ReadCloser, error) {
	algorithm, want, found := strings.Cut(declared, ":")
	if !found || want == "" {
		return nil, fmt.Errorf("malformed body digest %q", declared)
	}
	hasher, err := newDigestHasher(algorithm)
	if err != nil {
		return nil, err
	}
	return &verifyingBody{inner: inner, hasher: hasher, expected: strings.ToLower(want)}, nil
}

func (v *verifyingBody) Read(p []byte) (int, error) {
	n, err := v.inner.Read(p)
	if n > 0 {
		v.hasher.Write(p[:n])
	}
	if err == io.EOF && !v.checked {
		v.checked = true
		if got := hex.EncodeToString(v.hasher.Sum(nil)); got != v.expected {
			return n, fmt.Errorf("cluster body digest mismatch: declared %s, received %s", v.expected, got)
		}
	}
	return n, err
}

func (v *verifyingBody) Close() error { return v.inner.Close() }
