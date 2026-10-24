package s3compat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestObjectLockConfiguration_IsReadWithOrWithoutTheS3Namespace(t *testing.T) {
	env := setupCoverageTestEnvironment(t)
	defer env.cleanup()
	ctx := context.Background()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, "worm-ns", env.userID))
	require.NoError(t, env.bucketManager.SetObjectLockConfig(ctx, env.tenantID, "worm-ns",
		&bucket.ObjectLockConfig{ObjectLockEnabled: true}))
	tenantAdmin := &auth.User{ID: "tenant-admin", TenantID: env.tenantID, Roles: []string{auth.RoleTenantAdmin}}

	for days, namespace := range map[int]string{3: "", 5: objectLockNS} {
		w := putObjectLockConfig(env, tenantAdmin, "worm-ns",
			`<ObjectLockConfiguration`+namespace+`><ObjectLockEnabled>Enabled</ObjectLockEnabled>`+
				`<Rule><DefaultRetention><Mode>GOVERNANCE</Mode><Days>`+strconv.Itoa(days)+`</Days></DefaultRetention></Rule>`+
				`</ObjectLockConfiguration>`)
		require.Equal(t, http.StatusOK, w.Code, "namespace %q: %s", namespace, w.Body.String())
		config, err := env.bucketManager.GetObjectLockConfig(ctx, env.tenantID, "worm-ns")
		require.NoError(t, err)
		require.NotNil(t, config.Rule)
		require.NotNil(t, config.Rule.DefaultRetention.Days)
		assert.Equal(t, days, *config.Rule.DefaultRetention.Days, "namespace %q", namespace)
	}

	req := httptest.NewRequest(http.MethodGet, "/worm-ns?object-lock", nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": "worm-ns"})
	req = req.WithContext(setUserInContext(req.Context(), tenantAdmin))
	w := httptest.NewRecorder()
	env.handler.GetObjectLockConfiguration(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `<ObjectLockConfiguration`+objectLockNS+`>`)
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
