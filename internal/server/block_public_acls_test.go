package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The console refuses a public ACL on a bucket whose Block Public Access
// blocks public ACLs, for the bucket and its objects.
func TestConsoleBlockPublicAcls(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	const tenantID = "t-blocked-acls"
	const b = "console-blocked-acls"
	require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{ID: tenantID, Name: tenantID, Status: "active"}))
	require.NoError(t, server.bucketManager.CreateBucket(ctx, tenantID, b, ""))
	require.NoError(t, server.bucketManager.SetPublicAccessBlock(ctx, tenantID, b, &bucket.PublicAccessBlock{BlockPublicAcls: true}))
	_, err := server.objectManager.PutObject(ctx, tenantID+"/"+b, "o", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)

	putBucketACL := func(canned string) *httptest.ResponseRecorder {
		req := createAuthenticatedRequest("PUT", "/api/v1/buckets/"+b+"/acl", nil, tenantID, "user-1", false)
		req.Header.Set("x-amz-acl", canned)
		req = mux.SetURLVars(req, map[string]string{"bucket": b})
		rr := httptest.NewRecorder()
		server.handlePutBucketACL(rr, req)
		return rr
	}
	putObjectACL := func(canned string) *httptest.ResponseRecorder {
		req := createAuthenticatedRequest("PUT", "/api/v1/buckets/"+b+"/objects/o/acl", nil, tenantID, "user-1", false)
		req.Header.Set("x-amz-acl", canned)
		req = mux.SetURLVars(req, map[string]string{"bucket": b, "object": "o"})
		rr := httptest.NewRecorder()
		server.handlePutObjectACL(rr, req)
		return rr
	}

	t.Run("bucket", func(t *testing.T) {
		rr := putBucketACL("public-read")
		assert.Equal(t, http.StatusForbidden, rr.Code)
		assert.Contains(t, rr.Body.String(), "Block public ACLs")
		got, err := server.bucketManager.GetBucketACL(ctx, tenantID, b)
		require.NoError(t, err)
		for _, g := range got.(*acl.ACL).Grants {
			assert.NotEqual(t, acl.GroupAllUsers, g.Grantee.URI, "a refused ACL is not stored")
		}
		assert.Equal(t, http.StatusOK, putBucketACL("private").Code)
	})

	t.Run("object", func(t *testing.T) {
		assert.Equal(t, http.StatusForbidden, putObjectACL("authenticated-read").Code)
		got, err := server.objectManager.GetObjectACL(ctx, tenantID+"/"+b, "o")
		require.NoError(t, err)
		for _, g := range got.Grants {
			assert.NotEqual(t, acl.GroupAuthenticatedUsers, g.Grantee.URI, "a refused ACL is not stored")
		}
		assert.Equal(t, http.StatusOK, putObjectACL("private").Code)
	})
}
