package s3compat

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/share"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noShareManager holds no share.
type noShareManager struct{}

func (noShareManager) GetShareByObject(ctx context.Context, bucketName, objectKey, tenantID string) (interface{}, error) {
	return nil, share.ErrShareNotFound
}

// anonymousGet sends a GetObject without credentials, of a version when one is
// given.
func anonymousGet(h *Handler, bucketName, key string, versionID ...string) *httptest.ResponseRecorder {
	target := "/" + bucketName + "/" + key
	if len(versionID) > 0 {
		target += "?versionId=" + versionID[0]
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": bucketName, "object": key})
	rec := httptest.NewRecorder()
	h.GetObject(rec, req)
	return rec
}

func everyoneReads() []acl.Grant {
	return []acl.Grant{{
		Grantee:    acl.Grantee{Type: acl.GranteeTypeGroup, URI: acl.GroupAllUsers},
		Permission: acl.PermissionRead,
	}}
}

// publicObjectACL is an object ACL granting READ to everyone.
func publicObjectACL(owner string) *object.ACL {
	return &object.ACL{
		Owner: object.Owner{ID: owner},
		Grants: []object.Grant{{
			Grantee:    object.Grantee{Type: string(acl.GranteeTypeGroup), URI: acl.GroupAllUsers},
			Permission: string(acl.PermissionRead),
		}},
	}
}

// putAnonymousTestObject creates a global bucket holding one object; the
// bucket's ACL grants READ to everyone when bucketPublic, the object's when
// objectPublic.
func putAnonymousTestObject(t *testing.T, env *aclTestEnv, bucketName string, bucketPublic, objectPublic bool) {
	ctx := context.Background()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, "", bucketName, env.userID))
	if bucketPublic {
		require.NoError(t, env.bucketManager.SetBucketACL(ctx, "", bucketName, &acl.ACL{
			Owner: acl.Owner{ID: env.userID}, Grants: everyoneReads(),
		}))
	}
	_, err := env.objectManager.PutObject(ctx, bucketName, "09.png", strings.NewReader("avatar"), http.Header{})
	require.NoError(t, err)
	if objectPublic {
		require.NoError(t, env.objectManager.SetObjectACL(ctx, bucketName, "09.png", publicObjectACL(env.userID)))
	}
}

// As in AWS, READ on a bucket lists it and opens none of its objects: a read
// without credentials takes READ on the object (issue #9: without a share).
func TestAnonymousGetObject_TheObjectsACLDecides(t *testing.T) {
	for name, tc := range map[string]struct {
		bucketPublic, objectPublic bool
		want                       int
	}{
		"a public object in a private bucket":      {false, true, http.StatusOK},
		"a private object in a public-read bucket": {true, false, http.StatusForbidden},
		"a public object in a public-read bucket":  {true, true, http.StatusOK},
		"a private object in a private bucket":     {false, false, http.StatusForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			env := setupACLTestEnvironment(t)
			defer env.cleanup()
			env.handler.SetShareManager(noShareManager{})
			putAnonymousTestObject(t, env, "avatars", tc.bucketPublic, tc.objectPublic)

			rec := anonymousGet(env.handler, "avatars", "09.png")

			require.Equal(t, tc.want, rec.Code, rec.Body.String())
			if tc.want == http.StatusOK {
				body, _ := io.ReadAll(rec.Body)
				assert.Equal(t, "avatar", string(body))
			}
		})
	}
}

// A key that does not exist is told to whoever may list the bucket (404) and
// refused to the rest (403), as in AWS.
func TestAnonymousGetObject_AMissingKey(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()
	env.handler.SetShareManager(noShareManager{})
	putAnonymousTestObject(t, env, "listable", true, false)
	putAnonymousTestObject(t, env, "closed", false, false)

	assert.Equal(t, http.StatusNotFound, anonymousGet(env.handler, "listable", "missing.png").Code)
	assert.Equal(t, http.StatusForbidden, anonymousGet(env.handler, "closed", "missing.png").Code)
}

// The ACL read is the one of the version read.
func TestAnonymousGetObject_TheVersionsACLDecides(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()
	env.handler.SetShareManager(noShareManager{})
	ctx := context.Background()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, "", "versions", env.userID))
	require.NoError(t, env.bucketManager.SetVersioning(ctx, "", "versions", &bucket.VersioningConfig{Status: "Enabled"}))
	first, err := env.objectManager.PutObject(ctx, "versions", "k", strings.NewReader("first"), http.Header{})
	require.NoError(t, err)
	_, err = env.objectManager.PutObject(ctx, "versions", "k", strings.NewReader("second"), http.Header{})
	require.NoError(t, err)
	require.NoError(t, env.objectManager.SetObjectACL(ctx, "versions", "k", publicObjectACL(env.userID), first.VersionID))

	rec := anonymousGet(env.handler, "versions", "k", first.VersionID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body, _ := io.ReadAll(rec.Body)
	assert.Equal(t, "first", string(body))
	assert.Equal(t, http.StatusForbidden, anonymousGet(env.handler, "versions", "k").Code, "the current version is private")
}

// IgnorePublicAcls voids the public ACL of the object. RestrictPublicBuckets
// voids public bucket policies, not ACLs, as in AWS S3.
func TestAnonymousGetObject_PublicAccessBlockAndPublicRead(t *testing.T) {
	for name, tc := range map[string]struct {
		pab  *bucket.PublicAccessBlock
		want int
	}{
		"IgnorePublicAcls":      {&bucket.PublicAccessBlock{IgnorePublicAcls: true}, http.StatusForbidden},
		"RestrictPublicBuckets": {&bucket.PublicAccessBlock{RestrictPublicBuckets: true}, http.StatusOK},
	} {
		t.Run(name, func(t *testing.T) {
			env := setupACLTestEnvironment(t)
			defer env.cleanup()
			env.handler.SetShareManager(noShareManager{})
			putAnonymousTestObject(t, env, "avatars", false, true)
			require.NoError(t, env.bucketManager.SetPublicAccessBlock(context.Background(), "", "avatars", tc.pab))

			rec := anonymousGet(env.handler, "avatars", "09.png")

			assert.Equal(t, tc.want, rec.Code)
		})
	}
}

// putSharedTestObject creates a bucket of the environment's tenant holding one
// object its user may read, and shares every object as that user.
func putSharedTestObject(t *testing.T, env *aclTestEnv, bucketName string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, env.userID))
	_, err := env.objectManager.PutObject(ctx, env.tenantID+"/"+bucketName, "09.png", strings.NewReader("avatar"), http.Header{})
	require.NoError(t, err)
	grantExactly(t, env.authManager, env.userID, bucketName, auth.ActionGetObject)
	env.handler.SetShareManager(&mockShareManagerFull{createdBy: env.userID, tenantID: env.tenantID})
}

func TestAnonymousGetObject_ShareGrantsPrivateObject(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()
	putSharedTestObject(t, env, "private")

	rec := anonymousGet(env.handler, "private", "09.png")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body, _ := io.ReadAll(rec.Body)
	assert.Equal(t, "avatar", string(body))
}
