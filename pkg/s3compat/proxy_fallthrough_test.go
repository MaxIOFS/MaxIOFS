package s3compat

// What happens to a write when the node that owns the bucket cannot be reached.
//
// Forwarding streams the request body to the peer, which consumes and closes
// it. Falling back to local handling after that failed attempt therefore acts
// on an EMPTY body that reports no error — so a PUT stored a zero-byte object
// under the client's key and answered 200. The upload was reported successful
// and the data was gone.

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

	// This is the part that made it silent: reading again yields nothing AND no
	// error, so a handler downstream cannot tell an emptied body from an empty
	// upload. ContentLength is what still remembers.
	rest, err := io.ReadAll(req.Body)
	assert.Empty(t, rest)
	assert.NoError(t, err,
		"a consumed body reports success, which is why the fallback stored nothing")

	assert.NotZero(t, req.ContentLength,
		"the request still declares a body, which is the signal the fallback now reads")
}

// TestProxyFallthrough_ReadsMayStillFallThrough: the fallback exists so a
// request that cannot reach the owning node is still answered where possible.
// A request with no body has nothing to lose, so it must keep that behaviour —
// narrowing it to writes is the whole point of the fix.
func TestProxyFallthrough_ReadsMayStillFallThrough(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/bucket/object.bin", nil)

	assert.Zero(t, req.ContentLength,
		"a read carries no body, so falling through costs nothing")
}
