package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVerboseLoggingRedactsSecretHeaders: the debug dump of a request's
// headers gives the SSE-C keys and the session token redacted.
func TestVerboseLoggingRedactsSecretHeaders(t *testing.T) {
	var logged bytes.Buffer
	out, level := logrus.StandardLogger().Out, logrus.GetLevel()
	logrus.SetOutput(&logged)
	logrus.SetLevel(logrus.DebugLevel)
	defer func() {
		logrus.SetOutput(out)
		logrus.SetLevel(level)
	}()

	req := httptest.NewRequest("GET", "/bucket/key", nil)
	req.Header.Set("X-Amz-Server-Side-Encryption-Customer-Key", "c2VjcmV0LWtleQ==")
	req.Header.Set("X-Amz-Copy-Source-Server-Side-Encryption-Customer-Key", "c291cmNlLWtleQ==")
	req.Header.Set("X-Amz-Security-Token", "session-token-value")
	req.Header.Set("X-Amz-Date", "20261104T000000Z")
	VerboseLogging()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(httptest.NewRecorder(), req)

	require.Contains(t, logged.String(), "X-Amz-Date: 20261104T000000Z", "headers are dumped")
	for _, secret := range []string{"c2VjcmV0LWtleQ==", "c291cmNlLWtleQ==", "session-token-value"} {
		assert.NotContains(t, logged.String(), secret)
	}
	assert.Contains(t, logged.String(), "[redacted]")
}
