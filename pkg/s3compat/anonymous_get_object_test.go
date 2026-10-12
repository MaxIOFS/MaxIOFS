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
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/share"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noShareManager holds no share.
type noShareManager struct{}

func (noShareManager) GetShareByObject(ctx context.Context, bucketName, objectKey, tenantID string) (interface{}, error) {
	return nil, share.ErrShareNotFound
}

// anonymousGet sends a GetObject without credentials.
func anonymousGet(h *Handler, bucketName, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/"+bucketName+"/"+key, nil)
	req = mux.SetURLVars(req, map[string]string{"bucket": bucketName, "object": key})
	rec := httptest.NewRecorder()
	h.GetObject(rec, req)
	return rec
}

// putAnonymousTestObject creates a global bucket holding one object, readable
// by everyone when public.
func putAnonymousTestObject(t *testing.T, env *aclTestEnv, bucketName string, public bool) {
	ctx := context.Background()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, "", bucketName, env.userID))
	if public {
		require.NoError(t, env.bucketManager.SetBucketACL(ctx, "", bucketName, &acl.ACL{
			Owner: acl.Owner{ID: env.userID},
			Grants: []acl.Grant{{
				Grantee:    acl.Grantee{Type: acl.GranteeTypeGroup, URI: acl.GroupAllUsers},
				Permission: acl.PermissionRead,
			}},
		}))
	}
	_, err := env.objectManager.PutObject(ctx, bucketName, "09.png", strings.NewReader("avatar"), http.Header{})
	require.NoError(t, err)
}

// Issue #9: a public-read bucket is readable without a share.
func TestAnonymousGetObject_PublicReadBucketWithoutShare(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()
	env.handler.SetShareManager(noShareManager{})
	putAnonymousTestObject(t, env, "avatars", true)

	rec := anonymousGet(env.handler, "avatars", "09.png")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body, _ := io.ReadAll(rec.Body)
	assert.Equal(t, "avatar", string(body))
}

// IgnorePublicAcls voids the public ACL. RestrictPublicBuckets voids public
// bucket policies, not ACLs, as in AWS S3.
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
			putAnonymousTestObject(t, env, "avatars", true)
			require.NoError(t, env.bucketManager.SetPublicAccessBlock(context.Background(), "", "avatars", tc.pab))

			rec := anonymousGet(env.handler, "avatars", "09.png")

			assert.Equal(t, tc.want, rec.Code)
		})
	}
}

func TestAnonymousGetObject_PrivateBucketWithoutShareDenied(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()
	env.handler.SetShareManager(noShareManager{})
	putAnonymousTestObject(t, env, "private", false)

	rec := anonymousGet(env.handler, "private", "09.png")

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAnonymousGetObject_ShareGrantsPrivateObject(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()
	env.handler.SetShareManager(&mockShareManagerFull{})
	putAnonymousTestObject(t, env, "private", false)

	rec := anonymousGet(env.handler, "private", "09.png")

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body, _ := io.ReadAll(rec.Body)
	assert.Equal(t, "avatar", string(body))
}
