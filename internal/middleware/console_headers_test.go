package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConsoleHeadersAllowOnlyNamedInlineScripts: the console's CSP runs the
// inline scripts it names by digest, and no other.
func TestConsoleHeadersAllowOnlyNamedInlineScripts(t *testing.T) {
	rr := httptest.NewRecorder()
	ConsoleHeaders("'sha256-abc='")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).
		ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))

	var scriptSrc string
	for _, directive := range strings.Split(rr.Header().Get("Content-Security-Policy"), ";") {
		if d := strings.TrimSpace(directive); strings.HasPrefix(d, "script-src ") {
			scriptSrc = d
		}
	}
	require.NotEmpty(t, scriptSrc)
	assert.Equal(t, "script-src 'self' 'sha256-abc='", scriptSrc)
	assert.NotContains(t, rr.Header().Get("Content-Security-Policy"), "unsafe-eval")
}
