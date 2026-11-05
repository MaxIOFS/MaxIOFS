package server

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConsolePageScriptsMatchTheCSP: every inline script of the console's
// page is the one the Content-Security-Policy names by digest.
func TestConsolePageScriptsMatchTheCSP(t *testing.T) {
	for _, consoleURL := range []string{"http://127.0.0.1:8081", "https://maxiofs.example/ui"} {
		s := &Server{config: &config.Config{PublicConsoleURL: consoleURL}}
		handler, err := s.setupEmbeddedFrontend()
		require.NoError(t, err)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))

		inline := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindAllStringSubmatch(rr.Body.String(), -1)
		require.Len(t, inline, 1, "the page has one inline script")
		digest := sha256.Sum256([]byte(inline[0][1]))
		assert.Equal(t, "'sha256-"+base64.StdEncoding.EncodeToString(digest[:])+"'", s.consoleScriptHash, consoleURL)
	}
}
