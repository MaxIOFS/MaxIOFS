package s3compat

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Replication is not configured through the S3 API: a PUT is answered
// NotImplemented and names where replication rules are set, and a GET finds no
// configuration.
func TestBucketReplicationThroughS3PointsToTheConsole(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	send := func(method, path string, body []byte) (int, string) {
		t.Helper()
		req, w := env.makeS3Request(method, path, body)
		env.router.ServeHTTP(w, req)
		return w.Code, w.Body.String()
	}
	status, body := send("PUT", "/replicated", nil)
	require.Equal(t, http.StatusOK, status, body)

	status, body = send("PUT", "/replicated?replication", []byte(`<ReplicationConfiguration><Role>arn:aws:iam::1:role/r</Role></ReplicationConfiguration>`))
	assert.Equal(t, http.StatusNotImplemented, status, body)
	assert.Contains(t, body, "<Code>NotImplemented</Code>")
	assert.Contains(t, body, "/api/v1/buckets/{bucket}/replication/rules")

	status, body = send("GET", "/replicated?replication", nil)
	assert.Equal(t, http.StatusNotFound, status, body)
	assert.Contains(t, body, "<Code>ReplicationConfigurationNotFoundError</Code>")
}
