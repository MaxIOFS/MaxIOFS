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
	assert.NotEmpty(t, aws.ToString(got.ChecksumCRC32), "a whole read is sent the checksum and checks it")
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
