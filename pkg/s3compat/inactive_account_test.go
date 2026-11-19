package s3compat

import (
	"context"
	"net/http"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setTenantStatus sets the status of the environment's tenant.
func setTenantStatus(t *testing.T, env *aclTestEnv, status string) {
	t.Helper()
	tenant, err := env.authManager.GetTenant(context.Background(), "test-tenant")
	require.NoError(t, err)
	tenant.Status = status
	require.NoError(t, env.authManager.UpdateTenant(context.Background(), tenant))
}

// setUserStatus sets the status of the environment's user.
func setUserStatus(t *testing.T, env *aclTestEnv, status string) {
	t.Helper()
	user, err := env.authManager.GetUser(context.Background(), env.userID)
	require.NoError(t, err)
	user.Status = status
	require.NoError(t, env.authManager.UpdateUser(context.Background(), user))
}

// Nothing of an inactive tenant is served anonymously, whatever its buckets
// allow; reactivating it serves them again.
func TestAnInactiveTenantPublishesNoBucket(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	ctx := context.Background()
	policyBucket(t, env, "tenant-public", `{"Version":"2012-10-17","Statement":[
		{"Effect":"Allow","Principal":"*","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::tenant-public/*"]}]}`)
	env.handler.SetMetadataStore(env.metadataStore)
	setStatus := func(status string) {
		tenant, err := env.authManager.GetTenant(ctx, env.tenantID)
		require.NoError(t, err)
		tenant.Status = status
		require.NoError(t, env.authManager.UpdateTenant(ctx, tenant))
	}

	require.Equal(t, http.StatusOK, getObject(env, anonymousRequest("GET", "tenant-public", "o")), "a public bucket")
	setStatus(auth.TenantStatusInactive)
	assert.Equal(t, http.StatusForbidden, getObject(env, anonymousRequest("GET", "tenant-public", "o")), "its tenant inactive")
	setStatus(auth.TenantStatusActive)
	assert.Equal(t, http.StatusOK, getObject(env, anonymousRequest("GET", "tenant-public", "o")), "its tenant active again")
}

// A share is served while who made it may act: not once it is inactive or its
// tenant is, again once they are active.
func TestAShareStopsWithItsOwner(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()
	env.handler.SetShareManager(&mockShareManagerFull{createdBy: env.userID})
	putAnonymousTestObject(t, env, "private", false)

	require.Equal(t, http.StatusOK, anonymousGet(env.handler, "private", "09.png").Code, "a share")
	setUserStatus(t, env, auth.UserStatusInactive)
	assert.Equal(t, http.StatusForbidden, anonymousGet(env.handler, "private", "09.png").Code, "its owner inactive")
	setUserStatus(t, env, auth.UserStatusActive)
	assert.Equal(t, http.StatusOK, anonymousGet(env.handler, "private", "09.png").Code, "its owner active again")
	setTenantStatus(t, env, auth.TenantStatusInactive)
	assert.Equal(t, http.StatusForbidden, anonymousGet(env.handler, "private", "09.png").Code, "its owner's tenant inactive")
	setTenantStatus(t, env, auth.TenantStatusActive)
	assert.Equal(t, http.StatusOK, anonymousGet(env.handler, "private", "09.png").Code, "its owner's tenant active again")

	env.handler.SetShareManager(&mockShareManagerFull{createdBy: "a-user-deleted-since"})
	assert.Equal(t, http.StatusForbidden, anonymousGet(env.handler, "private", "09.png").Code, "an owner that is gone")
}
