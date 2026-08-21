package cluster

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testClusterToken = "cluster-token"

func signedProxyRequest(t *testing.T, method, target, body string) *http.Request {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	r.Header.Set("X-MaxIOFS-Proxied", "true")
	AddClusterProxyHeaders(r, "node-1", testClusterToken, "admin", "", "admin")
	return r
}

// A forwarded request carries the caller's identity, so accepting the same one
// twice means a captured mutation can be applied again.
func TestClusterProxyAuth_RefusesAReplay(t *testing.T) {
	req := signedProxyRequest(t, http.MethodDelete, "/bucket/key", "")

	_, _, _, ok := ValidateClusterProxyAuth(req, testClusterToken)
	require.True(t, ok, "the freshly signed request should be accepted")

	_, _, _, ok = ValidateClusterProxyAuth(req, testClusterToken)
	assert.False(t, ok, "the same signed request was accepted a second time")
}

// Every signed component must be bound: changing one has to invalidate the
// signature rather than silently pass.
func TestClusterProxyAuth_RejectsTamperedRequests(t *testing.T) {
	tamper := map[string]func(*http.Request){
		"the query string": func(r *http.Request) { r.URL.RawQuery = "versionId=other" },
		"the path":         func(r *http.Request) { r.URL.Path = "/bucket/other-key" },
		"the method":       func(r *http.Request) { r.Method = http.MethodDelete },
		"the user":         func(r *http.Request) { r.Header.Set("X-MaxIOFS-Forwarded-User", "root") },
		"the tenant":       func(r *http.Request) { r.Header.Set("X-MaxIOFS-Forwarded-Tenant", "other") },
		"the roles":        func(r *http.Request) { r.Header.Set("X-MaxIOFS-Forwarded-Roles", "admin,root") },
		"the body digest":  func(r *http.Request) { r.Header.Set(clusterBodySHA256Header, "md5:forged") },
		"the nonce":        func(r *http.Request) { r.Header.Set("X-MaxIOFS-Nonce", "another-nonce") },
	}

	for name, apply := range tamper {
		t.Run(name, func(t *testing.T) {
			req := signedProxyRequest(t, http.MethodPut, "/bucket/key?versionId=v1", "payload")
			apply(req)

			_, _, _, ok := ValidateClusterProxyAuth(req, testClusterToken)
			assert.False(t, ok, "changing %s did not invalidate the signature", name)
		})
	}
}

func TestClusterProxyAuth_RejectsAnotherClustersToken(t *testing.T) {
	req := signedProxyRequest(t, http.MethodGet, "/bucket/key", "")

	_, _, _, ok := ValidateClusterProxyAuth(req, "a-different-cluster-token")
	assert.False(t, ok, "a request signed with another token was accepted")
}
