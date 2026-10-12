package s3compat

import (
	"context"
	"encoding/json"
	"fmt"
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

// asTenantUser runs a request as a user of the environment's tenant whose
// only permissions are the actions given, on the bucket other-bucket.
func (env *s3TestEnv) asTenantUser(req *http.Request, user *auth.User, bucketName, key string, actions ...string) *http.Request {
	list, _ := json.Marshal(actions)
	doc := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":%s,"Resource":["arn:aws:s3:::other-bucket","arn:aws:s3:::other-bucket/*"]}]}`, list)
	if len(actions) == 0 {
		doc = `{"Version":"2012-10-17","Statement":[]}`
	}
	ctx := context.WithValue(req.Context(), "user", user)
	ctx = auth.WithPolicySet(ctx, &auth.PolicySet{UserID: user.ID, TenantID: user.TenantID, Documents: []string{doc}})
	return mux.SetURLVars(req.WithContext(ctx), map[string]string{"bucket": bucketName, "object": key})
}

// asOwnerWith runs a request as the environment's user holding actions on the
// bucket itself.
func (env *s3TestEnv) asOwnerWith(req *http.Request, bucketName, key string, actions ...string) *http.Request {
	user := &auth.User{ID: env.userID, Username: "testuser", TenantID: env.tenantID}
	list, _ := json.Marshal(actions)
	doc := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":%s,"Resource":["arn:aws:s3:::%s","arn:aws:s3:::%s/*"]}]}`,
		list, bucketName, bucketName)
	ctx := context.WithValue(req.Context(), "user", user)
	ctx = auth.WithPolicySet(ctx, &auth.PolicySet{UserID: user.ID, TenantID: user.TenantID, Documents: []string{doc}})
	return mux.SetURLVars(req.WithContext(ctx), map[string]string{"bucket": bucketName, "object": key})
}

func anonymousRequest(method, bucketName, key string) *http.Request {
	target := "/" + bucketName
	if key != "" {
		target += "/" + key
	}
	req := httptest.NewRequest(method, target, nil)
	return mux.SetURLVars(req, map[string]string{"bucket": bucketName, "object": key})
}

// policyBucket creates a bucket of the environment's tenant holding the object
// "o" and the bucket policy given.
func policyBucket(t *testing.T, env *s3TestEnv, name, policy string) {
	ctx := context.Background()
	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, name, env.userID))
	_, err := env.objectManager.PutObject(ctx, env.tenantID+"/"+name, "o", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	if policy != "" {
		var p bucket.Policy
		require.NoError(t, json.Unmarshal([]byte(policy), &p))
		require.NoError(t, env.bucketManager.SetBucketPolicy(ctx, env.tenantID, name, &p))
	}
}

func getObject(env *s3TestEnv, req *http.Request) int {
	w := httptest.NewRecorder()
	env.handler.GetObject(w, req)
	return w.Code
}

// A bucket policy Allow grants a user of the bucket's tenant whom no
// permission of their own grants, named by user ARN or by user ID.
func TestBucketPolicy_AllowGrantsAUserOfTheTenant(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	const b = "policy-allow"
	policyBucket(t, env, b, `{"Version":"2012-10-17","Statement":[
		{"Effect":"Allow","Principal":{"AWS":["arn:aws:iam:::user/reader"]},"Action":"s3:GetObject","Resource":"arn:aws:s3:::policy-allow/*"},
		{"Effect":"Allow","Principal":{"AWS":"user-by-id"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::policy-allow/*"}]}`)

	reader := &auth.User{ID: "reader-id", Username: "reader", TenantID: env.tenantID}
	byID := &auth.User{ID: "user-by-id", Username: "someone", TenantID: env.tenantID}
	stranger := &auth.User{ID: "stranger-id", Username: "stranger", TenantID: env.tenantID}

	assert.Equal(t, http.StatusOK, getObject(env, env.asTenantUser(httptest.NewRequest("GET", "/"+b+"/o", nil), reader, b, "o")))
	assert.Equal(t, http.StatusOK, getObject(env, env.asTenantUser(httptest.NewRequest("GET", "/"+b+"/o", nil), byID, b, "o")))
	assert.Equal(t, http.StatusForbidden, getObject(env, env.asTenantUser(httptest.NewRequest("GET", "/"+b+"/o", nil), stranger, b, "o")),
		"a user the policy does not name gets nothing from it")
}

// A bucket policy Deny wins over the permissions of the user.
func TestBucketPolicy_DenyWinsOverPermissions(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	const b = "policy-deny"
	policyBucket(t, env, b, `{"Version":"2012-10-17","Statement":[
		{"Effect":"Deny","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::policy-deny/*"}]}`)

	req := env.asOwnerWith(httptest.NewRequest("GET", "/"+b+"/o", nil), b, "o", "s3:GetObject")
	assert.Equal(t, http.StatusForbidden, getObject(env, req))

	w := httptest.NewRecorder()
	env.handler.HeadObject(w, env.asOwnerWith(httptest.NewRequest("HEAD", "/"+b+"/o", nil), b, "o", "s3:GetObject"))
	assert.Equal(t, http.StatusForbidden, w.Code)
}

// Principal "*" grants requests without credentials; a Deny refuses them even
// when the bucket ACL is public.
func TestBucketPolicy_RequestsWithoutCredentials(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	ctx := context.Background()

	const open = "policy-public"
	policyBucket(t, env, open, `{"Version":"2012-10-17","Statement":[
		{"Effect":"Allow","Principal":"*","Action":["s3:GetObject","s3:ListBucket"],"Resource":["arn:aws:s3:::policy-public","arn:aws:s3:::policy-public/*"]}]}`)
	env.handler.SetMetadataStore(env.metadataStore)

	assert.Equal(t, http.StatusOK, getObject(env, anonymousRequest("GET", open, "o")))
	w := httptest.NewRecorder()
	env.handler.HeadObject(w, anonymousRequest("HEAD", open, "o"))
	assert.Equal(t, http.StatusOK, w.Code)
	w = httptest.NewRecorder()
	env.handler.ListObjects(w, anonymousRequest("GET", open, ""))
	assert.Equal(t, http.StatusOK, w.Code)

	const denied = "policy-deny-public-acl"
	policyBucket(t, env, denied, `{"Version":"2012-10-17","Statement":[
		{"Effect":"Deny","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::policy-deny-public-acl/*"}]}`)
	require.NoError(t, env.bucketManager.SetBucketACL(ctx, env.tenantID, denied, &acl.ACL{
		Owner:  acl.Owner{ID: env.userID},
		Grants: []acl.Grant{{Grantee: acl.Grantee{Type: acl.GranteeTypeGroup, URI: acl.GroupAllUsers}, Permission: acl.PermissionRead}},
	}))
	assert.Equal(t, http.StatusForbidden, getObject(env, anonymousRequest("GET", denied, "o")),
		"a Deny in the bucket policy wins over a public ACL")
}

// Every request without credentials asks the bucket policy first: listing,
// HEAD of the bucket, writes and deletes.
func TestBucketPolicy_EveryRequestWithoutCredentials(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	env.handler.SetMetadataStore(env.metadataStore)
	const statement = `{"Effect":"%s","Principal":"*","Action":["s3:ListBucket","s3:PutObject","s3:DeleteObject"],"Resource":["arn:aws:s3:::%s","arn:aws:s3:::%s/*"]}`

	const open = "anon-open"
	policyBucket(t, env, open, `{"Version":"2012-10-17","Statement":[`+fmt.Sprintf(statement, "Allow", open, open)+`]}`)
	const closed = "anon-closed"
	policyBucket(t, env, closed, `{"Version":"2012-10-17","Statement":[`+fmt.Sprintf(statement, "Deny", closed, closed)+`]}`)
	require.NoError(t, env.bucketManager.SetBucketACL(context.Background(), env.tenantID, closed, &acl.ACL{
		Owner: acl.Owner{ID: env.userID},
		Grants: []acl.Grant{
			{Grantee: acl.Grantee{Type: acl.GranteeTypeGroup, URI: acl.GroupAllUsers}, Permission: acl.PermissionRead},
			{Grantee: acl.Grantee{Type: acl.GranteeTypeGroup, URI: acl.GroupAllUsers}, Permission: acl.PermissionWrite},
		},
	}))

	for _, tc := range []struct {
		name    string
		allowed int
		call    func(name string) int
	}{
		{"HeadBucket", http.StatusOK, func(name string) int {
			w := httptest.NewRecorder()
			env.handler.HeadBucket(w, anonymousRequest("HEAD", name, ""))
			return w.Code
		}},
		{"ListObjectsV2", http.StatusOK, func(name string) int {
			w := httptest.NewRecorder()
			env.handler.ListObjectsV2(w, anonymousRequest("GET", name, ""))
			return w.Code
		}},
		{"PutObject", http.StatusOK, func(name string) int {
			req := httptest.NewRequest("PUT", "/"+name+"/new", strings.NewReader("data"))
			w := httptest.NewRecorder()
			env.handler.PutObject(w, mux.SetURLVars(req, map[string]string{"bucket": name, "object": "new"}))
			return w.Code
		}},
		{"DeleteObject", http.StatusNoContent, func(name string) int {
			w := httptest.NewRecorder()
			env.handler.DeleteObject(w, anonymousRequest("DELETE", name, "o"))
			return w.Code
		}},
	} {
		assert.Equal(t, tc.allowed, tc.call(open), tc.name+": an Allow to everyone grants it")
		assert.Equal(t, http.StatusForbidden, tc.call(closed), tc.name+": a Deny wins over a public ACL")
	}
}

// Reading a named version asks for s3:GetObjectVersion.
func TestBucketPolicy_VersionReadsAskForGetObjectVersion(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	ctx := context.Background()
	env.handler.SetMetadataStore(env.metadataStore)
	const b = "anon-versions"
	policyBucket(t, env, b, `{"Version":"2012-10-17","Statement":[
		{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::anon-versions/*"}]}`)
	require.NoError(t, env.bucketManager.SetVersioning(ctx, env.tenantID, b, &bucket.VersioningConfig{Status: "Enabled"}))
	obj, err := env.objectManager.PutObject(ctx, env.tenantID+"/"+b, "v", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	require.NotEmpty(t, obj.VersionID)

	versioned := func() *http.Request {
		req := httptest.NewRequest("GET", "/"+b+"/v?versionId="+obj.VersionID, nil)
		return mux.SetURLVars(req, map[string]string{"bucket": b, "object": "v"})
	}
	assert.Equal(t, http.StatusOK, getObject(env, anonymousRequest("GET", b, "v")))
	assert.Equal(t, http.StatusForbidden, getObject(env, versioned()))

	var p bucket.Policy
	require.NoError(t, json.Unmarshal([]byte(`{"Version":"2012-10-17","Statement":[
		{"Effect":"Allow","Principal":"*","Action":"s3:GetObjectVersion","Resource":"arn:aws:s3:::anon-versions/*"}]}`), &p))
	require.NoError(t, env.bucketManager.SetBucketPolicy(ctx, env.tenantID, b, &p))
	assert.Equal(t, http.StatusOK, getObject(env, versioned()))
}

func withFacts(req *http.Request, facts bucket.RequestFacts) *http.Request {
	return req.WithContext(bucket.WithRequestFacts(req.Context(), facts))
}

// A policy that denies everything cannot deny its own management: the owner
// reads, replaces and removes it.
func TestBucketPolicy_DoesNotLockOutItsOwnManagement(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	const b = "policy-lockout"
	policyBucket(t, env, b, `{"Version":"2012-10-17","Statement":[
		{"Effect":"Deny","Principal":"*","Action":"s3:*","Resource":["arn:aws:s3:::policy-lockout","arn:aws:s3:::policy-lockout/*"]}]}`)
	actions := []string{"s3:GetObject", "s3:GetBucketPolicy", "s3:DeleteBucketPolicy"}

	assert.Equal(t, http.StatusForbidden, getObject(env, env.asOwnerWith(httptest.NewRequest("GET", "/"+b+"/o", nil), b, "o", actions...)))

	w := httptest.NewRecorder()
	env.handler.GetBucketPolicy(w, env.asOwnerWith(httptest.NewRequest("GET", "/"+b+"?policy", nil), b, "", actions...))
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	env.handler.DeleteBucketPolicy(w, env.asOwnerWith(httptest.NewRequest("DELETE", "/"+b+"?policy", nil), b, "", actions...))
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, http.StatusOK, getObject(env, env.asOwnerWith(httptest.NewRequest("GET", "/"+b+"/o", nil), b, "o", actions...)))
}

// A bucket policy grants nothing to a user of another tenant.
func TestBucketPolicy_StaysInsideTheTenant(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	const b = "policy-boundary"
	policyBucket(t, env, b, `{"Version":"2012-10-17","Statement":[
		{"Effect":"Allow","Principal":{"AWS":"outsider-id"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::policy-boundary/*"}]}`)

	outsider := &auth.User{ID: "outsider-id", Username: "outsider", TenantID: "another-tenant"}
	assert.Equal(t, http.StatusForbidden, getObject(env, env.asTenantUser(httptest.NewRequest("GET", "/"+b+"/o", nil), outsider, b, "o")))
}

// RestrictPublicBuckets voids what a public policy grants to requests without
// credentials; a policy limited to source addresses is not public.
func TestBucketPolicy_RestrictPublicBuckets(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	ctx := context.Background()
	restrict := &bucket.PublicAccessBlock{RestrictPublicBuckets: true}

	const public = "policy-restricted"
	policyBucket(t, env, public, `{"Version":"2012-10-17","Statement":[
		{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::policy-restricted/*"}]}`)
	require.NoError(t, env.bucketManager.SetPublicAccessBlock(ctx, env.tenantID, public, restrict))
	env.handler.SetMetadataStore(env.metadataStore)

	assert.Equal(t, http.StatusForbidden, getObject(env, anonymousRequest("GET", public, "o")))
	member := &auth.User{ID: "member-id", Username: "member", TenantID: env.tenantID}
	assert.Equal(t, http.StatusOK, getObject(env, env.asTenantUser(httptest.NewRequest("GET", "/"+public+"/o", nil), member, public, "o")),
		"a user of the bucket's tenant keeps what the policy grants")

	const office = "policy-office"
	policyBucket(t, env, office, `{"Version":"2012-10-17","Statement":[
		{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::policy-office/*",
		 "Condition":{"IpAddress":{"aws:SourceIp":"10.0.0.0/8"}}}]}`)
	require.NoError(t, env.bucketManager.SetPublicAccessBlock(ctx, env.tenantID, office, restrict))

	assert.Equal(t, http.StatusOK, getObject(env, withFacts(anonymousRequest("GET", office, "o"), bucket.RequestFacts{SourceIP: "10.1.2.3"})))
	assert.Equal(t, http.StatusForbidden, getObject(env, withFacts(anonymousRequest("GET", office, "o"), bucket.RequestFacts{SourceIP: "192.0.2.7"})))
}

// Conditions test the facts of the request.
func TestBucketPolicy_SecureTransportCondition(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	const b = "policy-tls"
	policyBucket(t, env, b, `{"Version":"2012-10-17","Statement":[
		{"Effect":"Deny","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::policy-tls/*",
		 "Condition":{"Bool":{"aws:SecureTransport":"false"}}}]}`)

	plain := env.asOwnerWith(httptest.NewRequest("GET", "/"+b+"/o", nil), b, "o", "s3:GetObject")
	assert.Equal(t, http.StatusForbidden, getObject(env, withFacts(plain, bucket.RequestFacts{})))
	secure := env.asOwnerWith(httptest.NewRequest("GET", "/"+b+"/o", nil), b, "o", "s3:GetObject")
	assert.Equal(t, http.StatusOK, getObject(env, withFacts(secure, bucket.RequestFacts{SecureTransport: true})))
}

func putPolicy(env *s3TestEnv, bucketName, policy string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("PUT", "/"+bucketName+"?policy", strings.NewReader(policy))
	w := httptest.NewRecorder()
	env.handler.PutBucketPolicy(w, env.asOwnerWith(req, bucketName, "", "s3:PutBucketPolicy"))
	return w
}

// A policy the evaluator would not enforce as written is refused.
func TestBucketPolicy_PutRefusesWhatIsNotEnforced(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	const b = "policy-validate"
	policyBucket(t, env, b, "")

	for name, policy := range map[string]string{
		"NotPrincipal": `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","NotPrincipal":{"AWS":"x"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::policy-validate/*"}]}`,
		"NotAction":    `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Principal":"*","NotAction":"s3:GetObject","Resource":"arn:aws:s3:::policy-validate/*"}]}`,
		"condition key": `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Principal":"*","Action":"s3:ListBucket","Resource":"arn:aws:s3:::policy-validate",
			"Condition":{"StringNotLike":{"s3:prefix":"home/*"}}}]}`,
		"other bucket":   `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::someone-else/*"}]}`,
		"role principal": `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Principal":{"AWS":"arn:aws:iam:::role/auditor"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::policy-validate/*"}]}`,
		"no version":     `{"Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::policy-validate/*"}]}`,
	} {
		w := putPolicy(env, b, policy)
		assert.Equal(t, http.StatusBadRequest, w.Code, name)
		assert.Contains(t, w.Body.String(), "MalformedPolicy", name)
	}
	_, err := env.bucketManager.GetBucketPolicy(context.Background(), env.tenantID, b)
	assert.ErrorIs(t, err, bucket.ErrPolicyNotFound, "a refused policy is not stored")

	w := putPolicy(env, b, `{"Version":"2012-10-17","Statement":{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam:::user/reader"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::policy-validate/*"}}`)
	assert.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
}

// BlockPublicPolicy refuses a public policy; one limited to source addresses
// is not public.
func TestBucketPolicy_BlockPublicPolicy(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	const b = "policy-blocked"
	policyBucket(t, env, b, "")
	require.NoError(t, env.bucketManager.SetPublicAccessBlock(context.Background(), env.tenantID, b, &bucket.PublicAccessBlock{BlockPublicPolicy: true}))
	public := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::%s/*"}]}`

	w := putPolicy(env, b, fmt.Sprintf(public, b))
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "AccessDenied")
	_, err := env.bucketManager.GetBucketPolicy(context.Background(), env.tenantID, b)
	assert.ErrorIs(t, err, bucket.ErrPolicyNotFound)

	w = putPolicy(env, b, `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::policy-blocked/*",
		"Condition":{"IpAddress":{"aws:SourceIp":["10.0.0.0/8","2001:db8::/32"]}}}]}`)
	assert.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	const open = "policy-unblocked"
	policyBucket(t, env, open, "")
	assert.Equal(t, http.StatusNoContent, putPolicy(env, open, fmt.Sprintf(public, open)).Code)
}
