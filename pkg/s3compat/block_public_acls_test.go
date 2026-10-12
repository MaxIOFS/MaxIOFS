package s3compat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const allUsersReadACL = `<AccessControlPolicy xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` +
	`<Owner><ID>owner</ID></Owner><AccessControlList><Grant>` +
	`<Grantee xsi:type="Group"><URI>http://acs.amazonaws.com/groups/global/AllUsers</URI></Grantee>` +
	`<Permission>READ</Permission></Grant></AccessControlList></AccessControlPolicy>`

var publicACLActions = []string{"s3:GetObject", "s3:PutObject", "s3:PutBucketAcl", "s3:PutObjectAcl"}

// newPABBucket creates a bucket of the user, with Block Public Access when pab
// is given.
func newPABBucket(t *testing.T, env *s3TestEnv, name string, pab *bucket.PublicAccessBlock) {
	ctx := context.Background()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, name, env.userID))
	if pab != nil {
		require.NoError(t, env.bucketManager.SetPublicAccessBlock(ctx, env.tenantID, name, pab))
	}
}

func grantsPublic(uris ...string) bool {
	for _, uri := range uris {
		if uri == acl.GroupAllUsers || uri == acl.GroupAuthenticatedUsers {
			return true
		}
	}
	return false
}

func (env *s3TestEnv) bucketACLIsPublic(t *testing.T, name string) bool {
	got, err := env.bucketManager.GetBucketACL(context.Background(), env.tenantID, name)
	require.NoError(t, err)
	var uris []string
	for _, g := range got.(*acl.ACL).Grants {
		uris = append(uris, g.Grantee.URI)
	}
	return grantsPublic(uris...)
}

func (env *s3TestEnv) objectACLIsPublic(t *testing.T, name, key string) bool {
	got, err := env.objectManager.GetObjectACL(context.Background(), env.tenantID+"/"+name, key)
	require.NoError(t, err)
	var uris []string
	for _, g := range got.Grants {
		uris = append(uris, g.Grantee.URI)
	}
	return grantsPublic(uris...)
}

// BlockPublicAcls refuses a PutBucketAcl that grants to everyone or to every
// authenticated user, as in AWS S3; the ACL stays as it was.
func TestBlockPublicAcls_PutBucketACL(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	const b = "blocked-acls"
	newPABBucket(t, env, b, &bucket.PublicAccessBlock{BlockPublicAcls: true})
	put := func(name, canned, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", "/"+name+"?acl", strings.NewReader(body))
		if canned != "" {
			req.Header.Set("x-amz-acl", canned)
		}
		w := httptest.NewRecorder()
		env.handler.PutBucketACL(w, env.asUserWith(req, name, "", publicACLActions...))
		return w
	}

	for _, canned := range []string{"public-read", "public-read-write", "authenticated-read"} {
		w := put(b, canned, "")
		assert.Equal(t, http.StatusForbidden, w.Code, canned)
		assert.Contains(t, w.Body.String(), "AccessDenied", canned)
	}
	assert.Equal(t, http.StatusForbidden, put(b, "", allUsersReadACL).Code, "an XML ACL granting to AllUsers")
	assert.False(t, env.bucketACLIsPublic(t, b), "a refused ACL is not stored")

	assert.Equal(t, http.StatusOK, put(b, "private", "").Code, "a private ACL is allowed")

	const open = "open-acls"
	newPABBucket(t, env, open, &bucket.PublicAccessBlock{IgnorePublicAcls: true})
	w := put(open, "public-read", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, env.bucketACLIsPublic(t, open), "only BlockPublicAcls refuses a public ACL")
}

func TestBlockPublicAcls_PutObjectACL(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	const b = "blocked-object-acls"
	newPABBucket(t, env, b, &bucket.PublicAccessBlock{BlockPublicAcls: true})
	_, err := env.objectManager.PutObject(context.Background(), env.tenantID+"/"+b, "o", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	put := func(canned, body string) int {
		req := httptest.NewRequest("PUT", "/"+b+"/o?acl", strings.NewReader(body))
		if canned != "" {
			req.Header.Set("x-amz-acl", canned)
		}
		w := httptest.NewRecorder()
		env.handler.PutObjectACL(w, env.asUserWith(req, b, "o", publicACLActions...))
		return w.Code
	}

	assert.Equal(t, http.StatusForbidden, put("public-read", ""))
	assert.Equal(t, http.StatusForbidden, put("authenticated-read", ""))
	assert.Equal(t, http.StatusForbidden, put("", allUsersReadACL))
	assert.False(t, env.objectACLIsPublic(t, b, "o"), "a refused ACL is not stored")
	assert.Equal(t, http.StatusOK, put("bucket-owner-full-control", ""))
}

// A write carrying a public x-amz-acl is refused before anything is stored.
func TestBlockPublicAcls_WriteWithPublicACLHeader(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	ctx := context.Background()
	const b = "blocked-writes"
	newPABBucket(t, env, b, &bucket.PublicAccessBlock{BlockPublicAcls: true})
	bucketPath := env.tenantID + "/" + b
	putObject := func(name, key, canned string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", "/"+name+"/"+key, strings.NewReader("data"))
		req.Header.Set("x-amz-acl", canned)
		w := httptest.NewRecorder()
		env.handler.PutObject(w, env.asUserWith(req, name, key, publicACLActions...))
		return w
	}

	t.Run("put", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, putObject(b, "pub", "public-read").Code)
		_, err := env.objectManager.GetObjectMetadata(ctx, bucketPath, "pub")
		assert.Error(t, err, "a refused write stores nothing")

		w := putObject(b, "priv", "private")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})

	t.Run("copy", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/"+b+"/copy", nil)
		req.Header.Set("x-amz-copy-source", "/"+b+"/priv")
		req.Header.Set("x-amz-acl", "public-read")
		w := httptest.NewRecorder()
		env.handler.CopyObject(w, env.asUserWith(req, b, "copy", publicACLActions...))
		assert.Equal(t, http.StatusForbidden, w.Code)
		_, err := env.objectManager.GetObjectMetadata(ctx, bucketPath, "copy")
		assert.Error(t, err, "a refused copy stores nothing")
	})

	t.Run("create multipart upload", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/"+b+"/mp?uploads", nil)
		req.Header.Set("x-amz-acl", "public-read-write")
		w := httptest.NewRecorder()
		env.handler.CreateMultipartUpload(w, env.asUserWith(req, b, "mp", publicACLActions...))
		assert.Equal(t, http.StatusForbidden, w.Code)
		uploads, err := env.objectManager.ListMultipartUploads(ctx, bucketPath)
		require.NoError(t, err)
		assert.Empty(t, uploads, "a refused upload is not created")
	})

	t.Run("bucket without BlockPublicAcls", func(t *testing.T) {
		const open = "open-writes"
		newPABBucket(t, env, open, nil)
		w := putObject(open, "pub", "public-read")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})
}
