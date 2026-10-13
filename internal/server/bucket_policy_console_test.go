package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The console decides with the bucket policy as the S3 API does.
func TestConsoleBucketPolicy(t *testing.T) {
	server := getSharedServer()
	ctx := context.Background()
	const tenantID = "t-console-policy"
	require.NoError(t, server.authManager.CreateTenant(ctx, &auth.Tenant{ID: tenantID, Name: tenantID, Status: "active"}))

	setPolicy := func(tenant, name, policy string) {
		require.NoError(t, server.bucketManager.CreateBucket(ctx, tenant, name, ""))
		path := name
		if tenant != "" {
			path = tenant + "/" + name
		}
		_, err := server.objectManager.PutObject(ctx, path, "o", strings.NewReader("data"), http.Header{})
		require.NoError(t, err)
		var p bucket.Policy
		require.NoError(t, json.Unmarshal([]byte(policy), &p))
		require.NoError(t, server.bucketManager.SetBucketPolicy(ctx, tenant, name, &p))
	}
	readACL := func(req *http.Request, name string) int {
		req = mux.SetURLVars(req, map[string]string{"bucket": name, "object": "o"})
		rr := httptest.NewRecorder()
		server.handleGetObjectACL(rr, req)
		return rr.Code
	}

	t.Run("a Deny wins over the user's permissions", func(t *testing.T) {
		setPolicy(tenantID, "console-deny", `{"Version":"2012-10-17","Statement":[
			{"Effect":"Deny","Principal":"*","Action":"s3:GetObjectAcl","Resource":"arn:aws:s3:::console-deny/*"}]}`)
		req := createAuthenticatedRequest("GET", "/api/v1/buckets/console-deny/objects/o/acl", nil, tenantID, "user-1", false)
		assert.Equal(t, http.StatusForbidden, readACL(req, "console-deny"))
	})

	t.Run("an Allow grants a user without permissions", func(t *testing.T) {
		setPolicy(tenantID, "console-allow", `{"Version":"2012-10-17","Statement":[
			{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam:::user/reader"},"Action":"s3:GetObjectAcl","Resource":"arn:aws:s3:::console-allow/*"}]}`)
		reader := &auth.User{ID: "reader-id", Username: "reader", TenantID: tenantID}
		req := httptest.NewRequest("GET", "/api/v1/buckets/console-allow/objects/o/acl", nil)
		rctx := context.WithValue(req.Context(), "user", reader)
		rctx = auth.WithPolicySet(rctx, &auth.PolicySet{UserID: reader.ID, TenantID: tenantID})
		assert.Equal(t, http.StatusOK, readACL(req.WithContext(rctx), "console-allow"))
	})

	t.Run("a Deny applies to the global administrator", func(t *testing.T) {
		setPolicy("", "console-deny-global", `{"Version":"2012-10-17","Statement":[
			{"Effect":"Deny","Principal":"*","Action":"s3:GetObjectAcl","Resource":"arn:aws:s3:::console-deny-global/*"}]}`)
		req := createAuthenticatedRequest("GET", "/api/v1/buckets/console-deny-global/objects/o/acl", nil, "", "global-admin", true)
		assert.Equal(t, http.StatusForbidden, readACL(req, "console-deny-global"))
	})

	t.Run("a Deny of every action covers the bucket quota", func(t *testing.T) {
		setPolicy(tenantID, "console-deny-quota", `{"Version":"2012-10-17","Statement":[
			{"Effect":"Deny","Principal":"*","Action":"*","Resource":"arn:aws:s3:::console-deny-quota"}]}`)
		setPolicy(tenantID, "console-quota", `{"Version":"2012-10-17","Statement":[
			{"Effect":"Deny","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::console-quota/*"}]}`)
		readQuota := func(name string) int {
			quotaReader := &auth.User{ID: "quota-reader", Username: "quota-reader", TenantID: tenantID}
			req := httptest.NewRequest("GET", "/api/v1/buckets/"+name+"/quota", nil)
			rctx := context.WithValue(req.Context(), "user", quotaReader)
			rctx = auth.WithPolicySet(rctx, &auth.PolicySet{UserID: quotaReader.ID, TenantID: tenantID, Documents: []string{
				`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"maxiofs:GetBucketQuota","Resource":"*"}]}`,
			}})
			req = mux.SetURLVars(req.WithContext(rctx), map[string]string{"bucket": name})
			rr := httptest.NewRecorder()
			server.handleGetBucketQuota(rr, req)
			return rr.Code
		}
		assert.Equal(t, http.StatusOK, readQuota("console-quota"), "the user may read a quota")
		assert.Equal(t, http.StatusForbidden, readQuota("console-deny-quota"))
	})

	t.Run("saving validates the policy and applies BlockPublicPolicy", func(t *testing.T) {
		const name = "console-put-policy"
		require.NoError(t, server.bucketManager.CreateBucket(ctx, tenantID, name, ""))
		require.NoError(t, server.bucketManager.SetPublicAccessBlock(ctx, tenantID, name, &bucket.PublicAccessBlock{BlockPublicPolicy: true}))
		put := func(policy string) int {
			req := createAuthenticatedRequest("PUT", "/api/v1/buckets/"+name+"/policy", strings.NewReader(policy), tenantID, "user-1", false)
			req = mux.SetURLVars(req, map[string]string{"bucket": name})
			rr := httptest.NewRecorder()
			server.handlePutBucketPolicy(rr, req)
			return rr.Code
		}
		assert.Equal(t, http.StatusBadRequest, put(`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","NotPrincipal":{"AWS":"x"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::console-put-policy/*"}]}`))
		assert.Equal(t, http.StatusForbidden, put(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::console-put-policy/*"}]}`))
		assert.Equal(t, http.StatusOK, put(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam:::user/reader"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::console-put-policy/*"}]}`))
	})
}
