package s3compat

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A batch delete against a bucket that does not exist answers NoSuchBucket for
// the request, as S3 does — not a per-key error nor a claimed success.
func TestBatchDeleteMissingBucket(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	body := []byte(`<Delete><Object><Key>a.txt</Key></Object><Object><Key>b.txt</Key></Object></Delete>`)
	req, w := env.makeS3Request("POST", "/nonexistent-batch-bucket?delete", body)
	env.router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "NoSuchBucket")
}
