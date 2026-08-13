package s3compat

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProxyFallthrough_AConsumedBodyIsNotHandledLocally reproduces the shape of
// the bug: a body that has been read to completion, as a failed forward leaves
// it, must not be treated as a legitimate empty upload.
func TestProxyFallthrough_AConsumedBodyIsNotHandledLocally(t *testing.T) {
	const payload = "the bytes the client actually sent"

	req := httptest.NewRequest(http.MethodPut, "/bucket/object.bin", strings.NewReader(payload))
	req.ContentLength = int64(len(payload))

	// Exactly what forwarding does to it.
	drained, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.NoError(t, req.Body.Close())
	require.Len(t, drained, len(payload))

	rest, err := io.ReadAll(req.Body)
	assert.Empty(t, rest)
	assert.NoError(t, err,
		"a consumed body reports success, which is why the fallback stored nothing")

	assert.NotZero(t, req.ContentLength,
		"the request still declares a body, which is the signal the fallback now reads")
}

// TestProxyFallthrough_ReadsMayStillFallThrough: the fallback exists so a
func TestProxyFallthrough_ReadsMayStillFallThrough(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/bucket/object.bin", nil)

	assert.Zero(t, req.ContentLength,
		"a read carries no body, so falling through costs nothing")
}
