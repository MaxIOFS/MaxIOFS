package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cacheTestSigner(t *testing.T) (*authManager, *AccessKey, *User) {
	t.Helper()
	managerInterface, tmpDir := setupTestAuthManager(t)
	t.Cleanup(func() { cleanupTestAuthManager(t, tmpDir) })
	manager := managerInterface.(*authManager)
	ctx := context.Background()
	require.NoError(t, manager.store.CreateTenant(&Tenant{ID: "tenant-c", Name: "tenant-c", Status: TenantStatusActive,
		MaxAccessKeys: 10, MaxStorageBytes: 1 << 30, MaxBuckets: 10}))
	user := &User{ID: "cache-user", Username: "cache-user", Status: UserStatusActive, Roles: []string{"user"}, TenantID: "tenant-c"}
	require.NoError(t, manager.store.CreateUser(user))
	key, err := manager.GenerateAccessKey(ctx, user.ID)
	require.NoError(t, err)
	return manager, key, user
}

func validateSigned(manager *authManager, t *testing.T, key *AccessKey) error {
	t.Helper()
	req := signedV4TestRequest(t, manager, key.AccessKeyID, key.SecretAccessKey, time.Now().UTC().Format("20060102T150405Z"))
	_, err := manager.ValidateS3SignatureV4(context.Background(), req)
	return err
}

// What a signed request reads is kept, and a change to it shows in the next
// request, made through SQL as a peer's synchronization makes it.
func TestAChangeToTheAuthTablesShowsInTheNextRequest(t *testing.T) {
	manager, key, _ := cacheTestSigner(t)
	db := manager.store.db
	change := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(query, args...)
		require.NoError(t, err)
	}
	require.NoError(t, validateSigned(manager, t, key))
	require.NoError(t, validateSigned(manager, t, key), "from the cache")

	change(`UPDATE access_keys SET status = 'inactive' WHERE access_key_id = ?`, key.AccessKeyID)
	assert.ErrorIs(t, validateSigned(manager, t, key), ErrInvalidCredentials, "a key revoked")
	change(`UPDATE access_keys SET status = 'active' WHERE access_key_id = ?`, key.AccessKeyID)
	require.NoError(t, validateSigned(manager, t, key))

	change(`UPDATE users SET status = ? WHERE id = 'cache-user'`, UserStatusSuspended)
	assert.ErrorIs(t, validateSigned(manager, t, key), ErrAccessDenied, "a user suspended")
	change(`UPDATE users SET status = ? WHERE id = 'cache-user'`, UserStatusActive)
	require.NoError(t, validateSigned(manager, t, key))

	change(`UPDATE tenants SET status = 'inactive' WHERE id = 'tenant-c'`)
	assert.ErrorIs(t, validateSigned(manager, t, key), ErrAccessDenied, "a tenant set inactive")
	change(`UPDATE tenants SET status = 'active' WHERE id = 'tenant-c'`)
	require.NoError(t, validateSigned(manager, t, key))

	change(`DELETE FROM access_keys WHERE access_key_id = ?`, key.AccessKeyID)
	assert.ErrorIs(t, validateSigned(manager, t, key), ErrInvalidCredentials, "a key deleted")
}

// A user's permissions are kept, and follow a change of their policies.
func TestPermissionsFollowAChangeOfTheirPolicies(t *testing.T) {
	manager, _, user := cacheTestSigner(t)
	ctx := context.Background()
	const resource = "arn:aws:s3:::cached/k"
	allowed := func() bool {
		t.Helper()
		set, err := manager.ResolvePolicySet(ctx, user)
		require.NoError(t, err)
		return EvaluateIAMDocuments(set.Documents, ActionGetObject, resource)
	}
	require.False(t, allowed())
	require.NoError(t, manager.store.PutIAMInlinePolicy(IAMTargetUser, user.ID, "read",
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::cached/*"]}]}`))
	assert.True(t, allowed())
	_, err := manager.store.db.Exec(`DELETE FROM iam_inline_policies WHERE target_id = ?`, user.ID)
	require.NoError(t, err)
	assert.False(t, allowed())
}

// A set or a user given to one caller is its own: a session policy set on it,
// or a role changed, reaches no other.
func TestWhatTheCacheGivesIsTheCallersOwn(t *testing.T) {
	manager, key, user := cacheTestSigner(t)
	ctx := context.Background()
	_, err := manager.ResolvePolicySet(ctx, user)
	require.NoError(t, err)
	_, _, _, err = manager.signerOf(ctx, key.AccessKeyID)
	require.NoError(t, err)

	first, err := manager.ResolvePolicySet(ctx, user)
	require.NoError(t, err)
	first.SessionPolicy = &Policy{}
	first.Actions = append(first.Actions, "s3:Everything")
	second, err := manager.ResolvePolicySet(ctx, user)
	require.NoError(t, err)
	assert.Nil(t, second.SessionPolicy)
	assert.NotContains(t, second.Actions, "s3:Everything")

	_, signer, _, err := manager.signerOf(ctx, key.AccessKeyID)
	require.NoError(t, err)
	signer.Roles[0] = RoleAdmin
	_, again, _, err := manager.signerOf(ctx, key.AccessKeyID)
	require.NoError(t, err)
	assert.Equal(t, []string{"user"}, again.Roles)
}

// What requests write themselves, a key's last use and a tenant's usage, does
// not count as a change: the cache is not emptied on every request.
func TestRequestsDoNotCountAsAChange(t *testing.T) {
	manager, key, _ := cacheTestSigner(t)
	before, err := manager.store.AuthGeneration()
	require.NoError(t, err)
	require.NoError(t, manager.store.UpdateAccessKeyLastUsed(key.AccessKeyID, time.Now().Unix()+3600))
	require.NoError(t, manager.store.IncrementTenantStorage("tenant-c", 100))
	after, err := manager.store.AuthGeneration()
	require.NoError(t, err)
	assert.Equal(t, before, after)

	_, err = manager.store.db.Exec(`UPDATE tenants SET status = 'active' WHERE id = 'tenant-c'`)
	require.NoError(t, err)
	changed, err := manager.store.AuthGeneration()
	require.NoError(t, err)
	assert.Greater(t, changed, after)
}

// A request reads the count once: it is authorized by the auth tables as they
// were when it started, and the next request by what changed since.
func TestARequestIsAuthorizedAsTheTablesWereWhenItStarted(t *testing.T) {
	manager, _, user := cacheTestSigner(t)
	allowed := func(ctx context.Context) bool {
		t.Helper()
		set, err := manager.ResolvePolicySet(ctx, user)
		require.NoError(t, err)
		return EvaluateIAMDocuments(set.Documents, ActionGetObject, "arn:aws:s3:::cached/k")
	}
	request := withRequestGeneration(context.Background())
	require.False(t, allowed(request))
	require.NoError(t, manager.store.PutIAMInlinePolicy(IAMTargetUser, user.ID, "read",
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::cached/*"]}]}`))
	assert.False(t, allowed(request), "the same request")
	assert.True(t, allowed(withRequestGeneration(context.Background())), "the next request")
}

// A tenant whose status cannot be read denies that request only: the failure
// is not kept as an inactive tenant.
func TestATenantThatCouldNotBeReadIsNotKeptInactive(t *testing.T) {
	manager, key, _ := cacheTestSigner(t)
	db := manager.store.db
	_, err := db.Exec(`ALTER TABLE tenants RENAME TO tenants_away`)
	require.NoError(t, err)
	assert.ErrorIs(t, validateSigned(manager, t, key), ErrAccessDenied, "the tenant cannot be read")
	_, err = db.Exec(`ALTER TABLE tenants_away RENAME TO tenants`)
	require.NoError(t, err)
	assert.NoError(t, validateSigned(manager, t, key))
}
