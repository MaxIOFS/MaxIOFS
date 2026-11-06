package s3compat

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lockedVersions is a bucket with Object Lock, driven through the S3 router.
type lockedVersions struct {
	t      *testing.T
	serve  func(method, path string, body []byte, headers map[string]string) (int, http.Header, string)
	bucket string
}

func newLockedVersions(t *testing.T, name string) *lockedVersions {
	t.Helper()
	env := setupCompleteS3Environment(t)
	t.Cleanup(env.cleanup)
	ctx := t.Context()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, name, env.userID))
	require.NoError(t, env.bucketManager.SetVersioning(ctx, env.tenantID, name, &bucket.VersioningConfig{Status: "Enabled"}))
	require.NoError(t, env.bucketManager.SetObjectLockConfig(ctx, env.tenantID, name, &bucket.ObjectLockConfig{ObjectLockEnabled: true}))
	return &lockedVersions{t: t, bucket: name, serve: func(method, path string, body []byte, headers map[string]string) (int, http.Header, string) {
		req, w := env.makeS3Request(method, path, body)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		env.router.ServeHTTP(w, req)
		return w.Code, w.Header(), w.Body.String()
	}}
}

// put writes a version of key with headers, and gives its ID.
func (l *lockedVersions) put(key string, headers map[string]string) string {
	l.t.Helper()
	status, header, body := l.serve("PUT", "/"+l.bucket+"/"+key, []byte("data"), headers)
	require.Equal(l.t, http.StatusOK, status, body)
	version := header.Get("x-amz-version-id")
	require.NotEmpty(l.t, version)
	return version
}

// batchDelete posts one version to DeleteObjects and gives the result.
func (l *lockedVersions) batchDelete(key, versionID string, headers map[string]string) DeleteObjectsResult {
	l.t.Helper()
	body := fmt.Sprintf(`<Delete><Object><Key>%s</Key><VersionId>%s</VersionId></Object></Delete>`, key, versionID)
	status, _, answer := l.serve("POST", "/"+l.bucket+"?delete", []byte(body), headers)
	require.Equal(l.t, http.StatusOK, status, answer)
	var result DeleteObjectsResult
	require.NoError(l.t, xml.Unmarshal([]byte(answer), &result))
	return result
}

func lockHeaders(mode string) map[string]string {
	return map[string]string{
		"x-amz-object-lock-mode":              mode,
		"x-amz-object-lock-retain-until-date": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
	}
}

// DeleteObjects refuses what Object Lock protects as a single delete refuses it:
// AccessDenied, which a client takes as an answer. An InternalError is retried.
func TestDeleteObjectsRefusesWhatObjectLockProtects(t *testing.T) {
	l := newLockedVersions(t, "batch-lock")
	for key, headers := range map[string]map[string]string{
		"compliance": lockHeaders("COMPLIANCE"),
		"governance": lockHeaders("GOVERNANCE"),
		"legal-hold": {"x-amz-object-lock-legal-hold": "ON"},
	} {
		result := l.batchDelete(key, l.put(key, headers), nil)
		require.Len(t, result.Errors, 1, key)
		assert.Equal(t, "AccessDenied", result.Errors[0].Code, key)
		assert.Empty(t, result.Deleted, key)
	}
}

// The bypass header works in a batch as in a single delete, for GOVERNANCE only.
func TestDeleteObjectsTakesTheGovernanceBypass(t *testing.T) {
	l := newLockedVersions(t, "batch-bypass")
	bypass := map[string]string{"x-amz-bypass-governance-retention": "true"}

	result := l.batchDelete("g", l.put("g", lockHeaders("GOVERNANCE")), bypass)
	assert.Empty(t, result.Errors)
	assert.Len(t, result.Deleted, 1)

	result = l.batchDelete("c", l.put("c", lockHeaders("COMPLIANCE")), bypass)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "AccessDenied", result.Errors[0].Code)
}

// A delete marker protects nothing: it takes no retention and no legal hold, or
// it could not be removed and would hide its object until the date.
func TestADeleteMarkerTakesNoObjectLock(t *testing.T) {
	l := newLockedVersions(t, "marker-lock")
	l.put("k", nil)
	status, header, _ := l.serve("DELETE", "/marker-lock/k", nil, nil)
	require.Equal(t, http.StatusNoContent, status)
	marker := header.Get("x-amz-version-id")
	require.NotEmpty(t, marker)

	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	status, _, body := l.serve("PUT", "/marker-lock/k?retention&versionId="+marker,
		[]byte(`<Retention><Mode>COMPLIANCE</Mode><RetainUntilDate>`+until+`</RetainUntilDate></Retention>`), nil)
	assert.Equal(t, http.StatusMethodNotAllowed, status, body)
	status, _, body = l.serve("PUT", "/marker-lock/k?legal-hold&versionId="+marker,
		[]byte(`<LegalHold><Status>ON</Status></LegalHold>`), nil)
	assert.Equal(t, http.StatusMethodNotAllowed, status, body)

	status, _, body = l.serve("DELETE", "/marker-lock/k?versionId="+marker, nil, nil)
	assert.Equal(t, http.StatusNoContent, status, body)
	status, _, body = l.serve("GET", "/marker-lock/k", nil, nil)
	assert.Equal(t, http.StatusOK, status, "the object is back: "+body)
}

// Only a bucket with Object Lock keeps a retention or a legal hold.
func TestRetentionNeedsObjectLockOnTheBucket(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "plain", env.userID))
	req, w := env.makeS3Request("PUT", "/plain/k", []byte("data"))
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	until := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	for path, body := range map[string]string{
		"/plain/k?retention":  `<Retention><Mode>COMPLIANCE</Mode><RetainUntilDate>` + until + `</RetainUntilDate></Retention>`,
		"/plain/k?legal-hold": `<LegalHold><Status>ON</Status></LegalHold>`,
	} {
		req, w := env.makeS3Request("PUT", path, []byte(body))
		env.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code, path)
		assert.True(t, strings.Contains(w.Body.String(), "Object Lock"), w.Body.String())
	}
}

// A walk over the pages of ListObjectVersions, with the markers each page gives,
// sees every version once and ends.
func TestListObjectVersionsPagesCoverTheBucket(t *testing.T) {
	l := newLockedVersions(t, "pages")
	want := map[string]bool{}
	for i := 0; i < 12; i++ {
		key := fmt.Sprintf("blocks/%02d.blk", i)
		want[key+"/"+l.put(key, nil)] = true
		want[key+"/"+l.put(key, nil)] = true
	}

	seen := map[string]bool{}
	query := "/pages?versions&max-keys=5"
	for pages := 0; ; pages++ {
		require.Less(t, pages, 20, "the pages never end")
		status, _, body := l.serve("GET", query, nil, nil)
		require.Equal(t, http.StatusOK, status, body)
		var page ListVersionsResult
		require.NoError(t, xml.Unmarshal([]byte(body), &page))
		assert.LessOrEqual(t, len(page.Versions), 5)
		for _, v := range page.Versions {
			id := v.Key + "/" + v.VersionId
			assert.False(t, seen[id], "listed twice: %s", id)
			seen[id] = true
		}
		if !page.IsTruncated {
			break
		}
		query = fmt.Sprintf("/pages?versions&max-keys=5&key-marker=%s&version-id-marker=%s", page.NextKeyMarker, page.NextVersionIdMarker)
	}
	assert.Equal(t, want, seen)
}
