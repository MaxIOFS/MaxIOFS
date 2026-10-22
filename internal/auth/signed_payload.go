package auth

import (
	"encoding/hex"
	"errors"
	"hash"
	"io"
)

// ErrContentSHA256Mismatch: a body whose SHA-256 is not the one the request
// was signed with.
var ErrContentSHA256Mismatch = errors.New("the body is not the one the request was signed with")

// isSHA256Hex reports an x-amz-content-sha256 that names the hash of the
// body, rather than an unsigned or streaming payload.
func isSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// signedPayloadBody fails the last Read of a body whose SHA-256 is not the
// one the request was signed with: the write it feeds stores nothing.
type signedPayloadBody struct {
	inner    io.ReadCloser
	hasher   hash.Hash
	expected string
	checked  bool
}

func (b *signedPayloadBody) Read(p []byte) (int, error) {
	n, err := b.inner.Read(p)
	if n > 0 {
		b.hasher.Write(p[:n])
	}
	if err == io.EOF && !b.checked {
		b.checked = true
		if hex.EncodeToString(b.hasher.Sum(nil)) != b.expected {
			return n, ErrContentSHA256Mismatch
		}
	}
	return n, err
}

func (b *signedPayloadBody) Close() error { return b.inner.Close() }
