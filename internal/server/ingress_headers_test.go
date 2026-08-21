package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/stretchr/testify/assert"
)

// A client can send any header it likes. The internal cluster headers say
// "another node already handled the routing for this", so a request that has
// not proved it comes from a peer must not arrive at a handler still carrying
// them — otherwise it runs on a node that does not own the bucket.
func TestStripInternalClusterHeaders_RemovesEveryOne(t *testing.T) {
	forged := map[string]string{
		"X-MaxIOFS-Proxied":          "true",
		"X-MaxIOFS-Proxy-Node":       "node-1",
		"X-MaxIOFS-Node-ID":          "node-1",
		"X-MaxIOFS-Node-Hmac":        "deadbeef",
		"X-MaxIOFS-Body-SHA256":      "UNSIGNED-PAYLOAD",
		"X-MaxIOFS-Timestamp":        "1700000000",
		"X-MaxIOFS-Nonce":            "n",
		"X-MaxIOFS-Signature":        "s",
		"X-MaxIOFS-Forwarded-User":   "admin",
		"X-MaxIOFS-Forwarded-Tenant": "",
		"X-MaxIOFS-Forwarded-Roles":  "admin",
	}

	req := httptest.NewRequest(http.MethodPut, "/bucket/key", nil)
	for k, v := range forged {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 ...")

	cluster.StripInternalClusterHeaders(req)

	for k := range forged {
		assert.Empty(t, req.Header.Get(k), "%s survived and would be trusted downstream", k)
	}
	assert.NotEmpty(t, req.Header.Get("Authorization"), "the client's own headers stay")
}
