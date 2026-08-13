package s3compat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const objectLockNS = ` xmlns="http://s3.amazonaws.com/doc/2006-03-01/"`

func putObjectLockConfig(env *coverageTestEnv, user *auth.User, bucketName, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/"+bucketName+"?object-lock", strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"bucket": bucketName})
	req = req.WithContext(setUserInContext(req.Context(), user))
	w := httptest.NewRecorder()
	env.handler.PutObjectLockConfiguration(w, req)
	return w
}

func TestObjectLockConfiguration_NeedsThePermission(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "worm", env.userID))
	require.NoError(t, env.bucketManager.SetObjectLockConfig(ctx, env.tenantID, "worm",
		&bucket.ObjectLockConfig{ObjectLockEnabled: true}))

	// A member of the bucket's tenant, holding no policy over it.
	reader := &auth.User{ID: "reader", TenantID: env.tenantID, Roles: []string{"readonly"}}

	w := putObjectLockConfig(env, reader, "worm",
		`<ObjectLockConfiguration`+objectLockNS+`><ObjectLockEnabled>Enabled</ObjectLockEnabled>`+
			`<Rule><DefaultRetention><Mode>COMPLIANCE</Mode><Days>3650</Days></DefaultRetention></Rule>`+
			`</ObjectLockConfiguration>`)

	assert.Equal(t, http.StatusForbidden, w.Code,
		"sharing the tenant is not permission to lock every future upload for ten years")

	config, err := env.bucketManager.GetObjectLockConfig(ctx, env.tenantID, "worm")
	require.NoError(t, err)
	assert.Nil(t, config.Rule, "and nothing was written")
}

func TestObjectLockConfiguration_AnonymousIsRefused(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "worm-anon", env.userID))

	req := httptest.NewRequest(http.MethodPut, "/worm-anon?object-lock",
		strings.NewReader(`<ObjectLockConfiguration`+objectLockNS+`></ObjectLockConfiguration>`))
	req = mux.SetURLVars(req, map[string]string{"bucket": "worm-anon"})
	w := httptest.NewRecorder()
	env.handler.PutObjectLockConfiguration(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}
