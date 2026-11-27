package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createBucketAs is the console's answer to a global administrator creating a
// bucket with settings.
func createBucketAs(t *testing.T, s *Server, name, settings string) *httptest.ResponseRecorder {
	t.Helper()
	admin, err := s.authManager.GetUser(context.Background(), "admin")
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	s.handleCreateBucket(rec, consoleRequestWith(admin, fmt.Sprintf(`{"name":%q%s}`, name, settings), "s3:*"))
	return rec
}

// A bucket is created with Object Lock and a default retention only as AWS
// takes one: GOVERNANCE or COMPLIANCE for a positive number of days or of
// years, not both. A bucket refused is not created.
func TestABucketIsCreatedOnlyWithARetentionAWSTakes(t *testing.T) {
	s := newClusterTestNode(t)
	lock := func(retention string) string {
		return `,"versioning":{"Status":"Enabled"},"objectLock":{"enabled":true` + retention + `}`
	}
	for name, retention := range map[string]string{
		"lock-gov-days":   `,"mode":"GOVERNANCE","days":2`,
		"lock-comp-years": `,"mode":"COMPLIANCE","years":1`,
		"lock-no-rule":    ``,
	} {
		require.Equal(t, http.StatusOK, createBucketAs(t, s, name, lock(retention)).Code, name)
	}
	locked, err := s.bucketManager.GetBucketInfo(context.Background(), "", "lock-comp-years")
	require.NoError(t, err)
	require.NotNil(t, locked.ObjectLock.Rule)
	assert.Equal(t, "COMPLIANCE", locked.ObjectLock.Rule.DefaultRetention.Mode)
	require.NotNil(t, locked.ObjectLock.Rule.DefaultRetention.Years)
	assert.Equal(t, 1, *locked.ObjectLock.Rule.DefaultRetention.Years)

	for name, retention := range map[string]string{
		"bad-mode":         `,"mode":"FOO","days":1`,
		"bad-negative":     `,"mode":"GOVERNANCE","days":-1`,
		"bad-both":         `,"mode":"GOVERNANCE","days":1,"years":1`,
		"bad-no-mode":      `,"days":1`,
		"bad-no-period":    `,"mode":"GOVERNANCE"`,
		"bad-lowercase":    `,"mode":"governance","days":1`,
		"bad-years-negati": `,"mode":"COMPLIANCE","years":-2`,
	} {
		assert.Equal(t, http.StatusBadRequest, createBucketAs(t, s, name, lock(retention)).Code, name)
		_, err := s.bucketManager.GetBucketInfo(context.Background(), "", name)
		assert.Error(t, err, "%s is not created", name)
	}
}

// failingSettings is a bucket manager that cannot store a bucket's settings.
type failingSettings struct {
	bucket.Manager
}

func (failingSettings) UpdateBucket(context.Context, string, string, *bucket.Bucket) error {
	return errors.New("the settings could not be stored")
}

// A bucket whose settings cannot be stored is not left without them: it is
// not created, and creating it again works once they can be.
func TestABucketIsNotLeftWithoutItsSettings(t *testing.T) {
	s := newClusterTestNode(t)
	working := s.bucketManager
	s.bucketManager = failingSettings{working}
	settings := `,"versioning":{"Status":"Enabled"},"objectLock":{"enabled":true,"mode":"COMPLIANCE","days":30}`

	assert.Equal(t, http.StatusInternalServerError, createBucketAs(t, s, "worm", settings).Code)
	_, err := working.GetBucketInfo(context.Background(), "", "worm")
	assert.Error(t, err, "no bucket without its Object Lock")

	s.bucketManager = working
	require.Equal(t, http.StatusOK, createBucketAs(t, s, "worm", settings).Code)
	created, err := working.GetBucketInfo(context.Background(), "", "worm")
	require.NoError(t, err)
	require.NotNil(t, created.ObjectLock)
	assert.True(t, created.ObjectLock.ObjectLockEnabled)
}

// createAs is the console's answer to user, allowed every S3 action, creating
// a bucket with body.
func createAs(s *Server, user *auth.User, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.handleCreateBucket(rec, consoleRequestWith(user, body, "s3:*"))
	return rec
}

// A bucket belongs to who creates it: the tenant of its administrator, the
// user of a tenant or of none; a global administrator gives it to the tenant
// or the user it names. A tenant holds no more buckets than it may.
func TestABucketBelongsToWhoCreatesIt(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: "acme", Name: "acme", Status: "active", MaxBuckets: 2}))
	require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: "beta", Name: "beta", Status: "active"}))
	admin, err := s.authManager.GetUser(ctx, "admin")
	require.NoError(t, err)
	tenantAdmin := consoleUserOf(t, s, "acme-admin", "acme", auth.RoleTenantAdmin)
	member := consoleUserOf(t, s, "acme-user", "acme", auth.RoleUser)
	consoleUserOf(t, s, "beta-user", "beta", auth.RoleUser)
	loner := consoleUserOf(t, s, "loner", "", auth.RoleUser)
	type owner struct{ id, kind string }
	owned := func(tenantID, name string) owner {
		t.Helper()
		info, err := s.bucketManager.GetBucketInfo(ctx, tenantID, name)
		require.NoError(t, err, "%s/%s", tenantID, name)
		return owner{info.OwnerID, info.OwnerType}
	}
	for _, tc := range []struct {
		user           *auth.User
		body           string
		tenant, bucket string
		want           owner
	}{
		{tenantAdmin, `{"name":"by-tenant-admin"}`, "acme", "by-tenant-admin", owner{"acme", "tenant"}},
		{member, `{"name":"by-member"}`, "acme", "by-member", owner{"acme-user", "user"}},
		{loner, `{"name":"by-loner"}`, "", "by-loner", owner{"loner", "user"}},
		{admin, `{"name":"for-beta","ownerType":"tenant","ownerId":"beta"}`, "beta", "for-beta", owner{"beta", "tenant"}},
		{admin, `{"name":"for-beta-user","ownerType":"user","ownerId":"beta-user"}`, "beta", "for-beta-user", owner{"beta-user", "user"}},
		{admin, `{"name":"by-admin"}`, "", "by-admin", owner{admin.ID, "user"}},
	} {
		require.Equal(t, http.StatusOK, createAs(s, tc.user, tc.body).Code, tc.body)
		assert.Equal(t, tc.want, owned(tc.tenant, tc.bucket), tc.body)
	}

	rec := createAs(s, member, `{"name":"one-too-many"}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, "acme holds 2 buckets")
	assert.Contains(t, rec.Body.String(), "(2/2)")
	assert.Equal(t, http.StatusForbidden, createAs(s, admin, `{"name":"for-full-acme","ownerType":"tenant","ownerId":"acme"}`).Code)
	for body, status := range map[string]int{
		`{"name":"no-tenant","ownerType":"tenant"}`:                    http.StatusBadRequest,
		`{"name":"gone-tenant","ownerType":"tenant","ownerId":"gone"}`: http.StatusBadRequest,
		`{"name":"gone-user","ownerType":"user","ownerId":"ghost"}`:    http.StatusBadRequest,
		`{"name":"odd-owner","ownerType":"bogus","ownerId":"beta"}`:    http.StatusBadRequest,
	} {
		assert.Equal(t, status, createAs(s, admin, body).Code, body)
	}
	for _, name := range []string{"one-too-many", "no-tenant", "gone-tenant", "gone-user", "odd-owner"} {
		for _, tenantID := range []string{"", "acme", "beta"} {
			_, err := s.bucketManager.GetBucketInfo(ctx, tenantID, name)
			assert.Error(t, err, "%s is not created", name)
		}
	}
}

// A bucket is created with the settings asked, and otherwise with the
// encryption every bucket has, its ACLs off and the versioning the
// installation sets by default.
func TestABucketIsCreatedWithTheSettingsAsked(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	admin, err := s.authManager.GetUser(ctx, "admin")
	require.NoError(t, err)

	rec := createAs(s, admin, `{"name":"plain"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	var answer struct {
		Data map[string]string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer))
	assert.Equal(t, map[string]string{"name": "plain"}, answer.Data)
	plain, err := s.bucketManager.GetBucketInfo(ctx, "", "plain")
	require.NoError(t, err)
	require.NotNil(t, plain.Encryption)
	assert.Equal(t, "AES256", plain.Encryption.Type)
	assert.Nil(t, plain.ObjectLock)
	assert.True(t, plain.Versioning == nil || plain.Versioning.Status != "Enabled")
	ownership, err := s.bucketManager.GetOwnershipControls(ctx, "", "plain")
	require.NoError(t, err)
	assert.Equal(t, bucket.ObjectOwnershipBucketOwnerEnforced, ownership.ObjectOwnership)

	require.Equal(t, http.StatusOK, createAs(s, admin, `{"name":"dressed","region":"eu-west-1","tags":{"team":"ops"},`+
		`"objectOwnership":"ObjectWriter","publicAccessBlock":{"blockPublicAcls":true},`+
		`"lifecycle":{"Rules":[{"ID":"x","Status":"Enabled","Expiration":{"Days":7}}]}}`).Code)
	dressed, err := s.bucketManager.GetBucketInfo(ctx, "", "dressed")
	require.NoError(t, err)
	assert.Equal(t, "eu-west-1", dressed.Region)
	assert.Equal(t, map[string]string{"team": "ops"}, dressed.Tags)
	require.NotNil(t, dressed.PublicAccessBlock)
	assert.True(t, dressed.PublicAccessBlock.BlockPublicAcls)
	assert.False(t, dressed.PublicAccessBlock.BlockPublicPolicy)
	require.NotNil(t, dressed.Lifecycle)
	require.Len(t, dressed.Lifecycle.Rules, 1)
	ownership, err = s.bucketManager.GetOwnershipControls(ctx, "", "dressed")
	require.NoError(t, err)
	assert.Equal(t, bucket.ObjectOwnershipObjectWriter, ownership.ObjectOwnership)

	require.NoError(t, s.settingsManager.Set("storage.default_bucket_versioning", "true"))
	time.Sleep(1100 * time.Millisecond) // settings are read through a cache of one second
	require.Equal(t, http.StatusOK, createAs(s, admin, `{"name":"versioned"}`).Code)
	versioned, err := s.bucketManager.GetBucketInfo(ctx, "", "versioned")
	require.NoError(t, err)
	require.NotNil(t, versioned.Versioning)
	assert.Equal(t, "Enabled", versioned.Versioning.Status)
	require.Equal(t, http.StatusOK, createAs(s, admin, `{"name":"unversioned","versioning":{"Status":"Suspended"}}`).Code)
	unversioned, err := s.bucketManager.GetBucketInfo(ctx, "", "unversioned")
	require.NoError(t, err)
	assert.Equal(t, "Suspended", unversioned.Versioning.Status, "what is asked wins over the default")

	for body, status := range map[string]int{
		`not json`:                              http.StatusBadRequest,
		`{}`:                                    http.StatusBadRequest,
		`{"name":"plain"}`:                      http.StatusConflict,
		`{"name":"x","objectOwnership":"Nope"}`: http.StatusBadRequest,
		`{"name":"lock-unversioned","versioning":{"Status":"Suspended"},"objectLock":{"enabled":true}}`: http.StatusBadRequest,
	} {
		assert.Equal(t, status, createAs(s, admin, body).Code, body)
	}
	rec = httptest.NewRecorder()
	s.handleCreateBucket(rec, httptest.NewRequest(http.MethodPost, "/api/v1/buckets", nil))
	assert.Equal(t, http.StatusBadRequest, rec.Code, "an empty body")
}
