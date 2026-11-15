package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// consoleRequestWith is a console request of user whose policies allow only
// actions, as the console's middleware leaves it.
func consoleRequestWith(user *auth.User, body string, actions ...string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/buckets", strings.NewReader(body))
	quoted := make([]string, len(actions))
	for i, a := range actions {
		quoted[i] = fmt.Sprintf("%q", a)
	}
	ctx := context.WithValue(req.Context(), "user", user)
	ctx = auth.WithPolicySet(ctx, &auth.PolicySet{UserID: user.ID, TenantID: user.TenantID, Actions: actions,
		Documents: []string{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":[` +
			strings.Join(quoted, ",") + `],"Resource":"*"}]}`}})
	return req.WithContext(ctx)
}

// A bucket is created through the console with the settings its creator may
// set: each one asks the permission of its own S3 call. What a new bucket has
// anyway asks nothing, so the form as it comes creates a bucket with only
// s3:CreateBucket.
func TestConsoleBucketCreationAsksThePermissionOfEachSetting(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: "acme", Name: "acme", Status: "active"}))
	user := &auth.User{ID: "limited", Username: "limited", TenantID: "acme", Status: auth.UserStatusActive}

	create := func(name, settings string, actions ...string) int {
		body := fmt.Sprintf(`{"name":%q%s}`, name, settings)
		rec := httptest.NewRecorder()
		s.handleCreateBucket(rec, consoleRequestWith(user, body, actions...))
		return rec.Code
	}
	only := []string{auth.ActionCreateBucket, auth.ActionListBucket}
	form := `,"encryption":{"type":"AES256"},"publicAccessBlock":{"blockPublicAcls":true,"ignorePublicAcls":true,"blockPublicPolicy":true,"restrictPublicBuckets":true}`

	assert.Equal(t, http.StatusOK, create("plain", form, only...), "the form as it comes")
	refused := map[string]string{
		"object lock":        `,"versioning":{"Status":"Enabled"},"objectLock":{"enabled":true,"mode":"COMPLIANCE","days":3650}`,
		"versioning":         `,"versioning":{"Status":"Enabled"}`,
		"public access":      `,"publicAccessBlock":{"blockPublicAcls":false,"ignorePublicAcls":true,"blockPublicPolicy":true,"restrictPublicBuckets":true}`,
		"lifecycle":          `,"lifecycle":{"Rules":[{"ID":"x","Status":"Enabled","Expiration":{"Days":1}}]}`,
		"another encryption": `,"encryption":{"type":"aws:kms"}`,
	}
	for name, settings := range refused {
		assert.Equal(t, http.StatusForbidden, create("refused-"+strings.ReplaceAll(name, " ", "-"), settings, only...), name)
	}
	_, err := s.bucketManager.GetBucketInfo(ctx, "acme", "refused-object-lock")
	assert.Error(t, err, "a refused bucket is not created")

	assert.Equal(t, http.StatusForbidden, create("half", refused["object lock"],
		auth.ActionCreateBucket, auth.ActionPutBucketVersioning), "versioning alone does not enable Object Lock")
	assert.Equal(t, http.StatusOK, create("locked", refused["object lock"],
		auth.ActionCreateBucket, auth.ActionPutBucketVersioning, auth.ActionPutBucketObjectLockConfiguration))
	locked, err := s.bucketManager.GetBucketInfo(ctx, "acme", "locked")
	require.NoError(t, err)
	require.NotNil(t, locked.ObjectLock)
	assert.True(t, locked.ObjectLock.ObjectLockEnabled)
}

// The S3 CreateBucket with Object Lock asks the permissions of the Object Lock
// configuration and of versioning, as in AWS.
func TestS3BucketCreationWithObjectLockAsksItsPermissions(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	now := time.Now().Unix()
	require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: "maker", Username: "maker",
		Status: auth.UserStatusActive, CreatedAt: now, UpdatedAt: now}))
	store := s.authManager.(interface {
		SetUserPermissions(string, *auth.UserPermissions) error
	})
	require.NoError(t, store.SetUserPermissions("maker", &auth.UserPermissions{
		Global: []string{auth.ActionCreateBucket, auth.ActionListBucket}, Buckets: []auth.BucketGrant{}}))
	key, err := s.authManager.GenerateAccessKey(ctx, "maker")
	require.NoError(t, err)
	api := httptest.NewServer(s.httpServer.Handler)
	t.Cleanup(api.Close)
	c := s3.New(s3.Options{BaseEndpoint: aws.String(api.URL), Region: "us-east-1", UsePathStyle: true, RetryMaxAttempts: 1,
		Credentials: credentials.NewStaticCredentialsProvider(key.AccessKeyID, key.SecretAccessKey, "")})

	_, err = c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("locked"), ObjectLockEnabledForBucket: aws.Bool(true)})
	assert.Equal(t, http.StatusForbidden, s3Status(err), "with Object Lock: %v", err)
	_, err = s.bucketManager.GetBucketInfo(ctx, "", "locked")
	assert.Error(t, err, "the refused bucket is not created")

	_, err = c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("plain")})
	assert.NoError(t, err, "without Object Lock")
}

// A bucket a global administrator creates for a tenant, or for a user of one,
// is created in that tenant: it counts against the tenant's buckets and its
// users reach it.
func TestABucketCreatedForATenantIsInTheTenant(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	require.NoError(t, s.authManager.CreateTenant(ctx, &auth.Tenant{ID: "acme", Name: "acme", Status: "active", MaxBuckets: 2}))
	now := time.Now().Unix()
	require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: "ana", Username: "ana", TenantID: "acme",
		Status: auth.UserStatusActive, CreatedAt: now, UpdatedAt: now}))

	create := func(body string) int {
		req := createAuthenticatedRequest(http.MethodPost, "/api/v1/buckets", strings.NewReader(body), "", "root", true)
		rec := httptest.NewRecorder()
		s.handleCreateBucket(rec, req)
		return rec.Code
	}

	require.Equal(t, http.StatusOK, create(`{"name":"for-acme","ownerType":"tenant","ownerId":"acme"}`))
	b, err := s.bucketManager.GetBucketInfo(ctx, "acme", "for-acme")
	require.NoError(t, err, "the bucket is in the tenant")
	assert.Equal(t, "acme", b.TenantID)
	assert.Equal(t, "tenant", b.OwnerType)
	_, err = s.bucketManager.GetBucketInfo(ctx, "", "for-acme")
	assert.Error(t, err, "and not in the global namespace")

	require.Equal(t, http.StatusOK, create(`{"name":"for-ana","ownerType":"user","ownerId":"ana"}`))
	b, err = s.bucketManager.GetBucketInfo(ctx, "acme", "for-ana")
	require.NoError(t, err, "a bucket for a user of the tenant is in the tenant")
	assert.Equal(t, "ana", b.OwnerID)

	assert.Equal(t, http.StatusForbidden, create(`{"name":"third","ownerType":"tenant","ownerId":"acme"}`),
		"the tenant's limit of 2 buckets counts them")
	assert.Equal(t, http.StatusBadRequest, create(`{"name":"nowhere","ownerType":"tenant","ownerId":"missing"}`))
	assert.Equal(t, http.StatusOK, create(`{"name":"global-one"}`), "without an owner, the global namespace")
	_, err = s.bucketManager.GetBucketInfo(ctx, "", "global-one")
	assert.NoError(t, err)
}
