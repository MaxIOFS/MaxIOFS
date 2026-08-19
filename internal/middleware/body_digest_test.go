package middleware

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestVerifyingBody_PassesTheDeclaredContentThrough(t *testing.T) {
	payload := "the replicated bytes"
	body, err := newVerifyingBody(io.NopCloser(strings.NewReader(payload)), "md5:"+md5Hex(payload))
	require.NoError(t, err)

	got, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, payload, string(got))
}

// The point of the digest: bytes swapped in flight must not be storable.
func TestVerifyingBody_RejectsSubstitutedContent(t *testing.T) {
	declared := "md5:" + md5Hex("the replicated bytes")
	body, err := newVerifyingBody(io.NopCloser(strings.NewReader("attacker payload")), declared)
	require.NoError(t, err)

	_, err = io.ReadAll(body)
	require.Error(t, err, "a body that does not match its signed digest must fail the read")
	assert.Contains(t, err.Error(), "digest mismatch")
}

func TestVerifyingBody_RejectsTruncatedContent(t *testing.T) {
	declared := "md5:" + md5Hex("the replicated bytes")
	body, err := newVerifyingBody(io.NopCloser(strings.NewReader("the replicated")), declared)
	require.NoError(t, err)

	_, err = io.ReadAll(body)
	require.Error(t, err)
}

func TestVerifyingBody_RefusesAnUnknownAlgorithm(t *testing.T) {
	_, err := newVerifyingBody(io.NopCloser(strings.NewReader("x")), "crc32:deadbeef")
	require.Error(t, err)

	_, err = newVerifyingBody(io.NopCloser(strings.NewReader("x")), "no-separator")
	require.Error(t, err)
}
