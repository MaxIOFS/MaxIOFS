package s3compat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// grantExactly attaches a policy allowing precisely the listed actions on a
// bucket and its keys, and nothing else.
func grantExactly(t *testing.T, env *coverageTestEnv, userID, bucketName string, actions ...string) {
	t.Helper()

	quoted := ""
	for i, a := range actions {
		if i > 0 {
			quoted += ","
		}
		quoted += `"` + a + `"`
	}

	writer, ok := env.authManager.(interface {
		PutIAMInlinePolicy(ctx context.Context, targetType, targetID, name, document string) error
	})
	require.True(t, ok)
	require.NoError(t, writer.PutIAMInlinePolicy(context.Background(), auth.IAMTargetUser, userID, "exact",
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":[`+quoted+`],`+
			`"Resource":["arn:aws:s3:::`+bucketName+`","arn:aws:s3:::`+bucketName+`/*"]}]}`))
}

func TestVersionPermission_CurrentObjectGrantDoesNotReachAVersion(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "versioned", env.userID))

	const caller = "reader-of-current"
	grantExactly(t, env, caller, "versioned",
		auth.ActionGetObject, auth.ActionGetObjectTagging)

	user := &auth.User{ID: caller, TenantID: env.tenantID, Roles: []string{"user"}}

	// The current object: allowed, because that is exactly what was granted.
	assert.True(t, env.handler.requireObjectS3ActionOnVersion(
		httptest.NewRecorder(), requestForObject(user, "versioned", "doc.txt", ""),
		"versioned", "doc.txt", auth.ActionGetObjectTagging, ""),
		"the grant covers the current object")

	// The same operation naming a version: refused, because addressing versions
	// was never granted.
	assert.False(t, env.handler.requireObjectS3ActionOnVersion(
		httptest.NewRecorder(), requestForObject(user, "versioned", "doc.txt", "v-old"),
		"versioned", "doc.txt", auth.ActionGetObjectTagging, "v-old"),
		"reading a historical version requires being allowed to address versions")
}

func TestVersionPermission_AVersionGrantStillWorks(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "versioned2", env.userID))

	const caller = "reader-of-versions"
	grantExactly(t, env, caller, "versioned2",
		auth.ActionGetObject, auth.ActionGetObjectTagging, auth.ActionGetObjectVersion)

	user := &auth.User{ID: caller, TenantID: env.tenantID, Roles: []string{"user"}}

	assert.True(t, env.handler.requireObjectS3ActionOnVersion(
		httptest.NewRecorder(), requestForObject(user, "versioned2", "doc.txt", "v-old"),
		"versioned2", "doc.txt", auth.ActionGetObjectTagging, "v-old"),
		"granting the version permission is what makes it reachable")
}

// requestForObject builds a request carrying the caller and, optionally, a
// version in the query string.
func requestForObject(user *auth.User, bucketName, objectKey, versionID string) *http.Request {
	url := "/" + bucketName + "/" + objectKey
	if versionID != "" {
		url += "?versionId=" + versionID
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": bucketName, "object": objectKey})
	return req.WithContext(setUserInContext(req.Context(), user))
}
