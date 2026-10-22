package s3compat

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An object written before versioning was enabled, or while it is suspended,
// is the null version: kept when a version replaces it, read and deleted by
// the ID null, listed, and replaced by a write or a delete while versioning
// is suspended.
func TestS3TheNullVersion(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	require.NoError(t, env.bucketManager.CreateBucket(t.Context(), env.tenantID, "nullv", env.userID))
	versioning := func(status string) {
		t.Helper()
		w := env.sendSigned("PUT", "/nullv?versioning", []byte("<VersioningConfiguration><Status>"+status+"</Status></VersioningConfiguration>"), nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	put := func(body string) string {
		t.Helper()
		w := env.sendSigned("PUT", "/nullv/k", []byte(body), nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return w.Header().Get("x-amz-version-id")
	}
	get := func(query string) (int, string) {
		w := env.sendSigned("GET", "/nullv/k"+query, nil, nil)
		return w.Code, w.Body.String()
	}

	put("before")
	versioning("Enabled")
	v1 := put("after")
	require.NotEmpty(t, v1)
	code, body := get("?versionId=null")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "before", body, "the object written before versioning is kept")
	assert.Equal(t, http.StatusOK, env.sendSigned("HEAD", "/nullv/k?versionId=null", nil, nil).Code)
	listed := env.sendSigned("GET", "/nullv?versions", nil, nil).Body.String()
	assert.Contains(t, listed, "<VersionId>null</VersionId><IsLatest>false</IsLatest>")
	assert.Contains(t, listed, "<VersionId>"+v1+"</VersionId><IsLatest>true</IsLatest>")

	versioning("Suspended")
	put("suspended")
	code, body = get("?versionId=null")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "suspended", body, "a write while suspended replaces the null version")
	listed = env.sendSigned("GET", "/nullv?versions", nil, nil).Body.String()
	assert.Contains(t, listed, "<VersionId>null</VersionId><IsLatest>true</IsLatest>")
	assert.Contains(t, listed, "<VersionId>"+v1+"</VersionId><IsLatest>false</IsLatest>")

	deleted := env.sendSigned("DELETE", "/nullv/k", nil, nil)
	require.Equal(t, http.StatusNoContent, deleted.Code, deleted.Body.String())
	assert.Equal(t, "null", deleted.Header().Get("x-amz-version-id"))
	assert.Equal(t, "true", deleted.Header().Get("x-amz-delete-marker"))
	code, _ = get("")
	assert.Equal(t, http.StatusNotFound, code)
	code, _ = get("?versionId=null")
	assert.Equal(t, http.StatusMethodNotAllowed, code, "the null version is the delete marker")

	require.Equal(t, http.StatusNoContent, env.sendSigned("DELETE", "/nullv/k?versionId=null", nil, nil).Code)
	code, body = get("")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "after", body, "the version before the marker is current again")
}
