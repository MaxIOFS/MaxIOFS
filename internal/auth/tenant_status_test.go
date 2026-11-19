package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A tenant set inactive stops every user of it, as deactivating each would:
// no password sign-in, no session already open, no S3 signature, no new key.
// Reactivating it gives them back. No tenant is always active; one that cannot
// be found is not.
func TestATenantSetInactiveStopsItsUsers(t *testing.T) {
	managerInterface, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)
	manager := managerInterface.(*authManager)
	ctx := context.Background()

	require.NoError(t, manager.CreateTenant(ctx, &Tenant{ID: "acme", Name: "acme", Status: TenantStatusActive}))
	user := &User{Username: "acme-user", Password: "password123", TenantID: "acme", Status: UserStatusActive, Roles: []string{RoleUser}}
	require.NoError(t, manager.CreateUser(ctx, user))
	key, err := manager.GenerateAccessKey(ctx, user.ID)
	require.NoError(t, err)
	token, err := manager.GenerateJWT(ctx, user)
	require.NoError(t, err)

	signV2 := func() error {
		req, _ := http.NewRequest("GET", "/bucket/object.txt", nil)
		req.Header.Set("Date", "Tue, 27 Mar 2007 19:36:42 +0000")
		mac := hmac.New(sha1.New, []byte(key.SecretAccessKey))
		mac.Write([]byte(manager.createStringToSignV2(req)))
		req.Header.Set("Authorization", fmt.Sprintf("AWS %s:%s", key.AccessKeyID, base64.StdEncoding.EncodeToString(mac.Sum(nil))))
		_, err := manager.ValidateS3SignatureV2(ctx, req)
		return err
	}
	setStatus := func(status string) {
		tenant, err := manager.GetTenant(ctx, "acme")
		require.NoError(t, err)
		tenant.Status = status
		require.NoError(t, manager.UpdateTenant(ctx, tenant))
	}
	acts := func(want bool, when string) {
		t.Helper()
		_, err := manager.ValidateConsoleCredentials(ctx, "acme-user", "password123")
		assert.Equal(t, want, err == nil, "password sign-in, "+when)
		_, err = manager.ValidateJWT(ctx, token)
		assert.Equal(t, want, err == nil, "a session already open, "+when)
		assert.Equal(t, want, signV2() == nil, "an S3 signature, "+when)
		_, err = manager.GenerateAccessKey(ctx, user.ID)
		assert.Equal(t, want, err == nil, "a new access key, "+when)
		assert.Equal(t, want, manager.AccountActive(ctx, user), "AccountActive, "+when)
	}

	acts(true, "its tenant active")
	setStatus(TenantStatusInactive)
	assert.False(t, manager.TenantActive(ctx, "acme"))
	acts(false, "its tenant inactive")
	setStatus(TenantStatusActive)
	acts(true, "its tenant active again")

	assert.True(t, manager.TenantActive(ctx, ""), "no tenant")
	assert.False(t, manager.TenantActive(ctx, "no-such-tenant"), "a tenant that cannot be found")
}
