package server

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyendpoints "github.com/aws/smithy-go/endpoints"
	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/presigned"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sdkClient is an S3 client of s with the SDK's defaults: it sends a
// checksum with each write and checks the one each read gives.
func sdkClient(t *testing.T, s *Server) *s3.Client {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Unix()
	require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: "sdk-admin", Username: "sdk-admin",
		Status: auth.UserStatusActive, Roles: []string{auth.RoleAdmin}, CreatedAt: now, UpdatedAt: now}))
	key, err := s.authManager.GenerateAccessKey(ctx, "sdk-admin")
	require.NoError(t, err)
	api := httptest.NewServer(s.httpServer.Handler)
	t.Cleanup(api.Close)
	return s3.New(s3.Options{
		BaseEndpoint:     aws.String(api.URL),
		Region:           "us-east-1",
		UsePathStyle:     true,
		Credentials:      credentials.NewStaticCredentialsProvider(key.AccessKeyID, key.SecretAccessKey, ""),
		RetryMaxAttempts: 1,
	})
}

// A browser's preflight reaches the bucket's CORS rules through every
// middleware of the S3 API: the origin they allow is named back, another is
// refused and named nothing.
func TestABrowsersPreflightIsAnsweredByTheBucketsCORS(t *testing.T) {
	s := newClusterTestNode(t)
	c := sdkClient(t, s)
	ctx := context.Background()
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("web")})
	require.NoError(t, err)
	_, err = c.PutBucketCors(ctx, &s3.PutBucketCorsInput{Bucket: aws.String("web"), CORSConfiguration: &types.CORSConfiguration{
		CORSRules: []types.CORSRule{{AllowedOrigins: []string{"https://app.example"}, AllowedMethods: []string{"PUT", "GET"}, AllowedHeaders: []string{"*"}}},
	}})
	require.NoError(t, err)
	api := httptest.NewServer(s.httpServer.Handler)
	defer api.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	preflight := func(origin string) *http.Response {
		req, _ := http.NewRequest(http.MethodOptions, api.URL+"/web/upload.bin", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "PUT")
		req.Header.Set("Access-Control-Request-Headers", "authorization, x-amz-date")
		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		return resp
	}

	ok := preflight("https://app.example")
	assert.Equal(t, http.StatusOK, ok.StatusCode)
	assert.Equal(t, "https://app.example", ok.Header.Get("Access-Control-Allow-Origin"))
	assert.Contains(t, ok.Header.Get("Access-Control-Allow-Methods"), "PUT")
	assert.Equal(t, "authorization, x-amz-date", ok.Header.Get("Access-Control-Allow-Headers"))

	other := preflight("https://evil.example")
	assert.Equal(t, http.StatusForbidden, other.StatusCode)
	assert.Empty(t, other.Header.Get("Access-Control-Allow-Origin"))
	assert.Empty(t, other.Header.Get("Access-Control-Allow-Methods"), "nothing is named to another origin")
}

// Object Ownership as AWS has it: a new bucket has its ACLs off, which refuses
// any ACL but the owner's and lets no ACL decide access; a bucket created with
// them on takes ACLs; a bucket without a setting keeps them.
func TestObjectOwnershipTurnsACLsOffAsAWSDoes(t *testing.T) {
	s := newClusterTestNode(t)
	c := sdkClient(t, s)
	ctx := context.Background()
	codeOf := func(err error) string {
		t.Helper()
		var apiErr interface{ ErrorCode() string }
		require.ErrorAs(t, err, &apiErr)
		return apiErr.ErrorCode()
	}

	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("enforced")})
	require.NoError(t, err)
	got, err := c.GetBucketOwnershipControls(ctx, &s3.GetBucketOwnershipControlsInput{Bucket: aws.String("enforced")})
	require.NoError(t, err)
	assert.Equal(t, types.ObjectOwnershipBucketOwnerEnforced, got.OwnershipControls.Rules[0].ObjectOwnership, "a new bucket")
	_, err = c.PutBucketAcl(ctx, &s3.PutBucketAclInput{Bucket: aws.String("enforced"), ACL: types.BucketCannedACLPublicRead})
	assert.Equal(t, "AccessControlListNotSupported", codeOf(err))
	_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("enforced"), Key: aws.String("k"), Body: strings.NewReader("x"), ACL: types.ObjectCannedACLPublicRead})
	assert.Equal(t, "AccessControlListNotSupported", codeOf(err))
	_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("enforced"), Key: aws.String("k"), Body: strings.NewReader("x"), ACL: types.ObjectCannedACLBucketOwnerFullControl})
	require.NoError(t, err, "the owner's ACL is taken")
	_, err = c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("enforced-public"), ACL: types.BucketCannedACLPublicRead})
	assert.Equal(t, "InvalidBucketAclWithObjectOwnership", codeOf(err))

	// Created with ACLs on, its objects can be public, until the ACLs are off.
	_, err = c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("writer"), ObjectOwnership: types.ObjectOwnershipObjectWriter})
	require.NoError(t, err)
	_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("writer"), Key: aws.String("k"), Body: strings.NewReader("x"), ACL: types.ObjectCannedACLPublicRead})
	require.NoError(t, err)
	api := httptest.NewServer(s.httpServer.Handler)
	defer api.Close()
	anonymous := func() int {
		resp, err := http.Get(api.URL + "/writer/k")
		require.NoError(t, err)
		resp.Body.Close()
		return resp.StatusCode
	}
	assert.Equal(t, http.StatusOK, anonymous(), "a public object, ACLs on")
	_, err = c.PutBucketOwnershipControls(ctx, &s3.PutBucketOwnershipControlsInput{Bucket: aws.String("writer"),
		OwnershipControls: &types.OwnershipControls{Rules: []types.OwnershipControlsRule{{ObjectOwnership: types.ObjectOwnershipBucketOwnerEnforced}}}})
	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, anonymous(), "ACLs off decide nothing")
	acl, err := c.GetObjectAcl(ctx, &s3.GetObjectAclInput{Bucket: aws.String("writer"), Key: aws.String("k")})
	require.NoError(t, err)
	require.Len(t, acl.Grants, 1, "ACLs off: the owner holds everything")
	assert.Equal(t, types.PermissionFullControl, acl.Grants[0].Permission)

	// A bucket ACL granting others keeps the ACLs on; no setting answers 404.
	_, err = c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("open"), ObjectOwnership: types.ObjectOwnershipObjectWriter, ACL: types.BucketCannedACLPublicRead})
	require.NoError(t, err)
	_, err = c.PutBucketOwnershipControls(ctx, &s3.PutBucketOwnershipControlsInput{Bucket: aws.String("open"),
		OwnershipControls: &types.OwnershipControls{Rules: []types.OwnershipControlsRule{{ObjectOwnership: types.ObjectOwnershipBucketOwnerEnforced}}}})
	assert.Equal(t, "InvalidBucketAclWithObjectOwnership", codeOf(err))
	_, err = c.DeleteBucketOwnershipControls(ctx, &s3.DeleteBucketOwnershipControlsInput{Bucket: aws.String("open")})
	require.NoError(t, err)
	_, err = c.GetBucketOwnershipControls(ctx, &s3.GetBucketOwnershipControlsInput{Bucket: aws.String("open")})
	assert.Equal(t, "OwnershipControlsNotFoundError", codeOf(err))
}

// A request without credentials about a bucket that exists in no tenant is
// told so (404 NoSuchBucket), as AWS tells it; one about a bucket that exists
// is still refused, and creating a bucket too.
func TestAnAnonymousRequestToABucketThatDoesNotExist(t *testing.T) {
	s := newClusterTestNode(t)
	c := sdkClient(t, s)
	ctx := context.Background()
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("closed")})
	require.NoError(t, err)
	_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("closed"), Key: aws.String("k"), Body: strings.NewReader("x")})
	require.NoError(t, err)
	api := httptest.NewServer(s.httpServer.Handler)
	defer api.Close()
	send := func(method, path string) (int, string) {
		req, _ := http.NewRequest(method, api.URL+path, nil)
		req.Header.Set("User-Agent", "aws-cli/2.0")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		status, body := send(method, "/no-such-bucket/k")
		assert.Equal(t, http.StatusNotFound, status, method)
		assert.Contains(t, body, "<Code>NoSuchBucket</Code>", method)
	}
	status, _ := send(http.MethodGet, "/closed/k")
	assert.Equal(t, http.StatusForbidden, status, "a bucket that exists, private")
	status, _ = send(http.MethodPut, "/no-such-bucket")
	assert.Equal(t, http.StatusForbidden, status, "creating a bucket without credentials")
}

// x-amz-expected-bucket-owner refuses a request on a bucket of another owner
// (403), as AWS does; the bucket's owner passes.
func TestARequestExpectingAnotherBucketOwnerIsRefused(t *testing.T) {
	s := newClusterTestNode(t)
	c := sdkClient(t, s)
	ctx := context.Background()
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("owned")})
	require.NoError(t, err)
	_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("owned"), Key: aws.String("k"), Body: strings.NewReader("x")})
	require.NoError(t, err)

	_, err = c.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String("owned"), ExpectedBucketOwner: aws.String("sdk-admin")})
	require.NoError(t, err, "the bucket's owner")
	got, err := c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("owned"), Key: aws.String("k"), ExpectedBucketOwner: aws.String("sdk-admin")})
	assert.Equal(t, "x", readAll(t, got, err))

	for what, call := range map[string]func() error{
		"list": func() error {
			_, err := c.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String("owned"), ExpectedBucketOwner: aws.String("someone-else")})
			return err
		},
		"get": func() error {
			_, err := c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("owned"), Key: aws.String("k"), ExpectedBucketOwner: aws.String("someone-else")})
			return err
		},
	} {
		var apiErr interface{ ErrorCode() string }
		require.ErrorAs(t, call(), &apiErr, what)
		assert.Equal(t, "AccessDenied", apiErr.ErrorCode(), what)
	}
}

// The policy status says whether the bucket's policy makes it public, as AWS
// defines it; a bucket without a policy has none.
func TestThePolicyStatusSaysWhetherTheBucketIsPublic(t *testing.T) {
	s := newClusterTestNode(t)
	c := sdkClient(t, s)
	ctx := context.Background()
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("status")})
	require.NoError(t, err)
	_, err = c.GetBucketPolicyStatus(ctx, &s3.GetBucketPolicyStatusInput{Bucket: aws.String("status")})
	var apiErr interface{ ErrorCode() string }
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "NoSuchBucketPolicy", apiErr.ErrorCode())

	isPublic := func(policy string) bool {
		t.Helper()
		_, err := c.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{Bucket: aws.String("status"), Policy: aws.String(policy)})
		require.NoError(t, err)
		out, err := c.GetBucketPolicyStatus(ctx, &s3.GetBucketPolicyStatusInput{Bucket: aws.String("status")})
		require.NoError(t, err)
		return aws.ToBool(out.PolicyStatus.IsPublic)
	}
	assert.True(t, isPublic(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::status/*"}]}`))
	assert.False(t, isPublic(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam:::user/sdk-admin"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::status/*"}]}`))
}

// A key that is not UTF-8 is no key, as in AWS: writing or reading it is
// refused with InvalidURI.
func TestAKeyThatIsNotUTF8IsRefused(t *testing.T) {
	s := newClusterTestNode(t)
	c := sdkClient(t, s)
	ctx := context.Background()
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("utf8")})
	require.NoError(t, err)
	refused := func(err error, what string) {
		t.Helper()
		var apiErr interface{ ErrorCode() string }
		require.Error(t, err, what)
		require.ErrorAs(t, err, &apiErr, what)
		assert.Equal(t, "InvalidURI", apiErr.ErrorCode(), what)
	}
	key := aws.String("bad-\xff\xfe-key")
	_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("utf8"), Key: key, Body: strings.NewReader("x")})
	refused(err, "PUT")
	_, err = c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("utf8"), Key: key})
	refused(err, "GET")
}

// readAll is the body of a read whose headers are checked too.
func readAll(t *testing.T, out *s3.GetObjectOutput, err error) string {
	t.Helper()
	require.NoError(t, err)
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	require.NoError(t, err)
	return string(data)
}

// A range of an object written with a checksum is sent without it: an SDK that
// checks the checksum it is sent against the bytes, as botocore does, would
// refuse them.
func TestAnSDKReadsARangeOfAnObjectWrittenWithAChecksum(t *testing.T) {
	s := newClusterTestNode(t)
	c := sdkClient(t, s)
	ctx := context.Background()
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("ranges")})
	require.NoError(t, err)
	_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("ranges"), Key: aws.String("k"),
		Body: bytes.NewReader([]byte("0123456789abcdef")), ChecksumAlgorithm: types.ChecksumAlgorithmCrc32})
	require.NoError(t, err)

	got, err := c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("ranges"), Key: aws.String("k"), Range: aws.String("bytes=2-5")})
	require.NoError(t, err)
	assert.Empty(t, aws.ToString(got.ChecksumCRC32))
	assert.Equal(t, "2345", readAll(t, got, nil))
	got, err = c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("ranges"), Key: aws.String("k")})
	assert.Equal(t, "0123456789abcdef", readAll(t, got, err))
	assert.Empty(t, aws.ToString(got.ChecksumCRC32), "a read that does not ask for the checksum")
	got, err = c.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("ranges"), Key: aws.String("k"),
		ChecksumMode: types.ChecksumModeEnabled})
	assert.Equal(t, "0123456789abcdef", readAll(t, got, err))
	assert.NotEmpty(t, aws.ToString(got.ChecksumCRC32), "a whole read asking for it is sent the checksum and checks it")
}

// A key is the bytes it is, as in S3: a key with an empty, a "." or a ".."
// segment, or one that starts with "/", is not another key. A key too long is
// refused as the client's error.
func TestAKeyIsTheBytesItIs(t *testing.T) {
	s := newClusterTestNode(t)
	c := sdkClient(t, s)
	ctx := context.Background()
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("keys")})
	require.NoError(t, err)
	keys := []string{"a//double", "a/double", "a/./dot", "a/dot", "trailing//", "/leading", "leading", "a/../b", "b", "../up", ".."}
	for _, key := range keys {
		_, err := c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("keys"), Key: aws.String(key),
			Body: strings.NewReader("data of " + key)})
		require.NoError(t, err, key)
	}
	for _, key := range keys {
		assert.Equal(t, "data of "+key, getBody(t, c, "keys", key), key)
	}
	listed, err := c.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String("keys")})
	require.NoError(t, err)
	var names []string
	for _, o := range listed.Contents {
		names = append(names, aws.ToString(o.Key))
	}
	assert.ElementsMatch(t, keys, names)

	_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("keys"), Key: aws.String(strings.Repeat("k", 1025)), Body: strings.NewReader("x")})
	assert.Equal(t, http.StatusBadRequest, s3Status(err), "%v", err)
	assert.ErrorContains(t, err, "KeyTooLongError")
}

// A key with an empty segment is kept and read on every node, in whole
// copies or in shards: it is not another key between nodes.
func TestAKeyIsTheBytesItIsOnEveryNode(t *testing.T) {
	for _, setup := range []struct {
		name  string
		build func(t *testing.T) *haCluster
	}{
		{"copies", func(t *testing.T) *haCluster {
			c := newHACluster(t, 2, 2)
			require.NoError(t, c.nodes[0].bucketManager.CreateBucket(context.Background(), "", "keys", routingAdminID))
			return c
		}},
		{"shards", func(t *testing.T) *haCluster { return newParityCluster(t, 3, "keys") }},
	} {
		t.Run(setup.name, func(t *testing.T) {
			c := setup.build(t)
			ctx := context.Background()
			keys := []string{"a//double", "a/double", "trailing//", "con espacios.txt", "ñandú/acción.txt", "emoji-😀.txt",
				"plus+sign", "percent%20literal", "hash#tag", "question?mark", "amp&eq=x", "colon:semi;comma,",
				"trailing-space ", "tilde~star*", "brackets[1](2){3}", "quote'apos", "/leading", "a/../b", "../up"}
			for _, key := range keys {
				_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("keys"), Key: aws.String(key),
					Body: bytes.NewReader(append(payload(300<<10), key...))})
				require.NoError(t, err, key)
			}
			for _, n := range c.nodes {
				for _, key := range keys {
					assert.True(t, bytes.Equal(append(payload(300<<10), key...), getBytes(t, n.s3, "keys", key)), "%s on %s", key, n.id)
				}
			}
		})
	}
}

// The console reaches a key with an empty segment: a delete of a//b deletes
// it, not a/b.
func TestTheConsoleReachesAKeyWithAnEmptySegment(t *testing.T) {
	s := newClusterTestNode(t)
	c := sdkClient(t, s)
	ctx := context.Background()
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("keys")})
	require.NoError(t, err)
	for _, key := range []string{"a//double", "a/double"} {
		_, err := c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("keys"), Key: aws.String(key), Body: strings.NewReader(key)})
		require.NoError(t, err)
	}
	admin := &auth.User{ID: "console-admin", Username: "console-admin", Status: auth.UserStatusActive,
		Roles: []string{auth.RoleAdmin}, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
	require.NoError(t, s.authManager.CreateUser(ctx, admin))
	token, err := s.authManager.GenerateJWT(ctx, admin)
	require.NoError(t, err)
	console := httptest.NewServer(s.consoleServer.Handler)
	t.Cleanup(console.Close)

	req, err := http.NewRequest(http.MethodDelete, console.URL+"/api/v1/buckets/keys/objects/a%2F%2Fdouble", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	require.Less(t, resp.StatusCode, 300, "the delete is answered, not redirected")

	_, err = c.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String("keys"), Key: aws.String("a//double")})
	assert.Equal(t, http.StatusNotFound, s3Status(err), "a//double is deleted")
	_, err = c.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String("keys"), Key: aws.String("a/double")})
	assert.NoError(t, err, "a/double is kept")
}

// A URL the console presigns reads its object whatever the key: the key is
// sent and signed escaped, as S3 escapes it.
func TestAPresignedURLReadsAnyKey(t *testing.T) {
	s := newClusterTestNode(t)
	c := sdkClient(t, s)
	ctx := context.Background()
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("keys")})
	require.NoError(t, err)
	creds, err := c.Options().Credentials.Retrieve(ctx)
	require.NoError(t, err)
	for _, key := range []string{"plain.txt", "con espacios.txt", "plus+sign", "percent%20literal", "question?mark", "hash#tag",
		"ñandú/acción.txt", "/leading", "a//double", "amp&eq=x", "tilde~star*"} {
		_, err := c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("keys"), Key: aws.String(key), Body: strings.NewReader("data of " + key)})
		require.NoError(t, err, key)
		signed, err := presigned.GeneratePresignedURL(presigned.PresignedURLParams{
			Endpoint: aws.ToString(c.Options().BaseEndpoint), Bucket: "keys", Key: key,
			AccessKeyID: creds.AccessKeyID, SecretAccessKey: creds.SecretAccessKey, Method: http.MethodGet, ExpiresIn: 300, Region: "us-east-1",
		})
		require.NoError(t, err, key)
		resp, err := http.Get(signed)
		require.NoError(t, err, key)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode, "%s: %s", key, body)
		assert.Equal(t, "data of "+key, string(body), key)
	}
}

// The console reaches a key named as one of its routes, its "/" sent as %2F:
// x/tags is read and deleted, not the tags of x, and search is an object.
func TestTheConsoleReachesAKeyNamedAsARoute(t *testing.T) {
	s := newClusterTestNode(t)
	c := sdkClient(t, s)
	ctx := context.Background()
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("keys")})
	require.NoError(t, err)
	routed := []string{"search"}
	for _, route := range []string{"versions", "tags", "acl", "legal-hold", "share"} {
		routed = append(routed, "x/"+route)
	}
	for _, key := range append([]string{"x"}, routed...) {
		_, err := c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("keys"), Key: aws.String(key), Body: strings.NewReader("data of " + key)})
		require.NoError(t, err)
	}
	admin := &auth.User{ID: "console-admin", Username: "console-admin", Status: auth.UserStatusActive,
		Roles: []string{auth.RoleAdmin}, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
	require.NoError(t, s.authManager.CreateUser(ctx, admin))
	token, err := s.authManager.GenerateJWT(ctx, admin)
	require.NoError(t, err)
	console := httptest.NewServer(s.consoleServer.Handler)
	t.Cleanup(console.Close)
	var header http.Header
	call := func(method, path string) (int, string) {
		req, err := http.NewRequest(method, console.URL+"/api/v1/buckets/keys/"+path, nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		header = resp.Header
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	status, body := call(http.MethodGet, "objects/x")
	assert.Equal(t, "data of x", body)
	head, err := c.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String("keys"), Key: aws.String("x")})
	require.NoError(t, err)
	assert.Equal(t, aws.ToString(head.ETag), header.Get("ETag"), "quoted, as the S3 API sends it")
	status, body = call(http.MethodGet, "search")
	assert.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, `"key":"search"`)
	for _, key := range routed {
		status, body := call(http.MethodGet, "objects/"+url.PathEscape(key))
		assert.Equal(t, http.StatusOK, status, key)
		assert.Equal(t, "data of "+key, body, key)
		status, _ = call(http.MethodDelete, "objects/"+url.PathEscape(key))
		assert.Less(t, status, 300, key)
		_, err := c.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String("keys"), Key: aws.String(key)})
		assert.Equal(t, http.StatusNotFound, s3Status(err), "%s is deleted", key)
	}
	assert.Equal(t, "data of x", getBody(t, c, "keys", "x"))
}

// bucketHost is an endpoint at the host of the bucket, "{bucket}.{host}", one
// with periods too: the SDK's own resolver sends those path-style.
type bucketHost struct{ host string }

func (b bucketHost) ResolveEndpoint(_ context.Context, p s3.EndpointParameters) (smithyendpoints.Endpoint, error) {
	u := url.URL{Scheme: "http", Host: b.host}
	if bucket := aws.ToString(p.Bucket); bucket != "" {
		u.Host = bucket + "." + b.host
	}
	return smithyendpoints.Endpoint{URI: u}, nil
}

// hostedClient is c sending each request to the host of its bucket, as
// "{bucket}.{s3Host}", all of them dialed to the test server. hosts are the
// hosts it dialed.
func hostedClient(c *s3.Client, s3Host string) (client *s3.Client, hosts *[]string) {
	opts := c.Options()
	api := strings.TrimPrefix(aws.ToString(opts.BaseEndpoint), "http://")
	_, port, _ := net.SplitHostPort(api)
	hosts = &[]string{}
	var mu sync.Mutex
	opts.BaseEndpoint = nil
	opts.EndpointResolverV2 = bucketHost{net.JoinHostPort(s3Host, port)}
	opts.HTTPClient = &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			mu.Lock()
			*hosts = append(*hosts, addr)
			mu.Unlock()
			return (&net.Dialer{}).DialContext(ctx, network, api)
		},
	}}
	return s3.New(opts), hosts
}

// A bucket is named as S3 names it: periods and two hyphens in a row are
// taken, two periods in a row are not. A bucket with periods is reached by its
// host too, on the S3 API and as a website, with either host under the other:
// the longer one is the one a request is for.
func TestABucketIsNamedAsS3NamesIt(t *testing.T) {
	for _, hosts := range []struct{ s3, web string }{
		{"s3.test", "web.test"},
		{"s3.web.test", "web.test"},
		{"s3.test", "web.s3.test"},
	} {
		t.Run(hosts.s3+" and "+hosts.web, func(t *testing.T) {
			s := newClusterTestNodeWith(t, func(c *config.Config) {
				c.PublicAPIURL = "http://" + hosts.s3 + ":8080"
				c.WebsiteHostname = hosts.web
			})
			c := sdkClient(t, s)
			ctx := context.Background()
			for _, name := range []string{"my.dotted.bucket", "double--dash"} {
				_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(name)})
				require.NoError(t, err, name)
			}
			_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("my..bucket")})
			assert.Equal(t, http.StatusBadRequest, s3Status(err), "%v", err)
			assert.ErrorContains(t, err, "InvalidBucketName")

			hosted, dialed := hostedClient(c, hosts.s3)
			_, err = hosted.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("my.dotted.bucket"), Key: aws.String("k"),
				Body: strings.NewReader("by its host")})
			require.NoError(t, err)
			assert.Equal(t, "by its host", getBody(t, hosted, "my.dotted.bucket", "k"))
			require.NotEmpty(t, *dialed)
			assert.True(t, strings.HasPrefix((*dialed)[0], "my.dotted.bucket."+hosts.s3+":"), "%v", *dialed)

			_, err = c.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("my.dotted.bucket"), Key: aws.String("index.html"),
				Body: strings.NewReader("a website")})
			require.NoError(t, err)
			require.NoError(t, s.bucketManager.SetBucketACL(ctx, "", "my.dotted.bucket", &acl.ACL{
				Owner:  acl.Owner{ID: "sdk-admin"},
				Grants: []acl.Grant{{Grantee: acl.Grantee{Type: acl.GranteeTypeGroup, URI: acl.GroupAllUsers}, Permission: acl.PermissionRead}},
			}))
			require.NoError(t, s.bucketManager.SetWebsite(ctx, "", "my.dotted.bucket", &bucket.WebsiteConfig{IndexDocument: "index.html"}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = "my.dotted.bucket." + hosts.web
			w := httptest.NewRecorder()
			s.httpServer.Handler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Equal(t, "a website", w.Body.String())
		})
	}
}
