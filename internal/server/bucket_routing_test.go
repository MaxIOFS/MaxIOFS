package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const routingAdminID = "routing-admin"

// newRoutingCluster is a migration cluster whose target serves its S3 API to
// the source, and an S3 client of the source signed as a global administrator
// both nodes know.
func newRoutingCluster(t *testing.T) (*migrationCluster, *s3.Client) {
	t.Helper()
	c := newMigrationCluster(t)
	ctx := context.Background()

	targetS3 := httptest.NewServer(c.target.httpServer.Handler)
	t.Cleanup(targetS3.Close)
	_, err := c.source.db.ExecContext(ctx, `UPDATE cluster_nodes SET api_url = ? WHERE id = ?`, targetS3.URL, c.targetID)
	require.NoError(t, err)
	// The target checks a forwarded request against the cluster token.
	_, err = c.target.db.ExecContext(ctx,
		`INSERT INTO cluster_config (node_id, node_name, cluster_token, region) VALUES (?, 'target', ?, 'us-east-1')`,
		c.targetID, c.token)
	require.NoError(t, err)

	now := time.Now().Unix()
	for _, s := range []*Server{c.source, c.target} {
		require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: routingAdminID, Username: routingAdminID,
			Status: auth.UserStatusActive, Roles: []string{auth.RoleAdmin}, CreatedAt: now, UpdatedAt: now}))
	}
	key, err := c.source.authManager.GenerateAccessKey(ctx, routingAdminID)
	require.NoError(t, err)

	sourceS3 := httptest.NewServer(c.source.httpServer.Handler)
	t.Cleanup(sourceS3.Close)
	client := s3.New(s3.Options{
		BaseEndpoint:               aws.String(sourceS3.URL),
		Region:                     "us-east-1",
		UsePathStyle:               true,
		Credentials:                credentials.NewStaticCredentialsProvider(key.AccessKeyID, key.SecretAccessKey, ""),
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
		RetryMaxAttempts:           1,
	})
	return c, client
}

// Every S3 request about a bucket goes to the node that holds it, whatever the
// operation.
func TestS3RequestsGoToTheBucketsNode(t *testing.T) {
	c, client := newRoutingCluster(t)
	ctx := context.Background()
	require.NoError(t, c.target.bucketManager.CreateBucket(ctx, "", "far", routingAdminID))
	_, err := c.target.objectManager.PutObject(ctx, "far", "a", strings.NewReader("alpha"), http.Header{})
	require.NoError(t, err)
	bucket := aws.String("far")

	_, err = client.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{Bucket: bucket,
		Tagging: &types.Tagging{TagSet: []types.Tag{{Key: aws.String("team"), Value: aws.String("blue")}}}})
	require.NoError(t, err)
	info, err := c.target.bucketManager.GetBucketInfo(ctx, "", "far")
	require.NoError(t, err)
	assert.Equal(t, "blue", info.Tags["team"])

	_, err = client.PutObjectTagging(ctx, &s3.PutObjectTaggingInput{Bucket: bucket, Key: aws.String("a"),
		Tagging: &types.Tagging{TagSet: []types.Tag{{Key: aws.String("kind"), Value: aws.String("doc")}}}})
	require.NoError(t, err)
	tags, err := c.target.objectManager.GetObjectTagging(ctx, "far", "a")
	require.NoError(t, err)
	assert.Equal(t, []object.Tag{{Key: "kind", Value: "doc"}}, tags.Tags)

	upload, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: bucket, Key: aws.String("mp")})
	require.NoError(t, err)
	part, err := client.UploadPart(ctx, &s3.UploadPartInput{Bucket: bucket, Key: aws.String("mp"), UploadId: upload.UploadId,
		PartNumber: aws.Int32(1), Body: strings.NewReader("multi")})
	require.NoError(t, err)
	_, err = client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: bucket, Key: aws.String("mp"),
		UploadId: upload.UploadId, MultipartUpload: &types.CompletedMultipartUpload{
			Parts: []types.CompletedPart{{ETag: part.ETag, PartNumber: aws.Int32(1)}}}})
	require.NoError(t, err)
	assert.Equal(t, "multi", readBody(t, c.target, "far", "mp"))

	_, err = client.CopyObject(ctx, &s3.CopyObjectInput{Bucket: bucket, Key: aws.String("b"), CopySource: aws.String("far/a")})
	require.NoError(t, err)
	assert.Equal(t, "alpha", readBody(t, c.target, "far", "b"))

	versions, err := client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: bucket})
	require.NoError(t, err)
	assert.Len(t, versions.Versions, 3)

	_, err = client.DeleteObjects(ctx, &s3.DeleteObjectsInput{Bucket: bucket, Delete: &types.Delete{
		Objects: []types.ObjectIdentifier{{Key: aws.String("a")}, {Key: aws.String("b")}, {Key: aws.String("mp")}}}})
	require.NoError(t, err)
	listed, err := c.target.objectManager.ListObjects(ctx, "far", "", "", "", 10)
	require.NoError(t, err)
	assert.Empty(t, listed.Objects)

	_, err = client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: bucket})
	require.NoError(t, err)
	_, err = c.target.metadataStore.GetBucketByName(ctx, "far")
	assert.ErrorIs(t, err, metadata.ErrBucketNotFound)
}

// A bucket name another node holds is taken: creating it is answered by that
// node, and no second bucket is made here.
func TestS3CreateBucketSeesTheWholeCluster(t *testing.T) {
	c, client := newRoutingCluster(t)
	ctx := context.Background()
	require.NoError(t, c.target.bucketManager.CreateBucket(ctx, "", "taken", routingAdminID))

	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("taken")})
	var answered *awshttp.ResponseError
	require.ErrorAs(t, err, &answered)
	assert.Equal(t, http.StatusConflict, answered.HTTPStatusCode())
	_, err = c.source.metadataStore.GetBucketByName(ctx, "taken")
	assert.ErrorIs(t, err, metadata.ErrBucketNotFound)

	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String("free")})
	require.NoError(t, err)
	_, err = c.source.metadataStore.GetBucketByName(ctx, "free")
	assert.NoError(t, err)
}

// consoleNode serves a forwarded console request with a node's own routes and
// records what it was sent.
type consoleNode struct {
	node *Server
	mu   sync.Mutex
	sent []string
}

func (n *consoleNode) RoundTrip(r *http.Request) (*http.Response, error) {
	n.mu.Lock()
	n.sent = append(n.sent, r.Method+" "+r.URL.Path)
	n.mu.Unlock()
	w := httptest.NewRecorder()
	n.node.consoleRouter.ServeHTTP(w, r)
	return w.Result(), nil
}

func (n *consoleNode) requests() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.sent...)
}

// Every console request about a bucket's own state goes to the node that holds
// the bucket, whatever the operation. Its permissions are cluster
// configuration and go to the coordinator; a bucket held here is served here.
func TestConsoleBucketRequestsGoToTheBucketsNode(t *testing.T) {
	c := newMigrationCluster(t)
	ctx := context.Background()
	require.NoError(t, c.target.bucketManager.CreateBucket(ctx, "", "far", "admin"))
	require.NoError(t, c.target.bucketManager.CreateBucket(ctx, "", "far-empty", "admin"))
	_, err := c.target.objectManager.PutObject(ctx, "far", "a", strings.NewReader("alpha"), http.Header{})
	require.NoError(t, err)
	require.NoError(t, c.source.bucketManager.CreateBucket(ctx, "", "near", "admin"))
	target := &consoleNode{node: c.target}
	previous := consoleProxyClient.Transport
	consoleProxyClient.Transport = target
	t.Cleanup(func() { consoleProxyClient.Transport = previous })
	token := getAdminToken(t, c.source)
	send := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		c.source.consoleRouter.ServeHTTP(w, req)
		return w
	}

	w := send(http.MethodPut, "/api/v1/buckets/far/versioning", `{"status":"Enabled"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	versioning, err := c.target.bucketManager.GetVersioning(ctx, "", "far")
	require.NoError(t, err)
	assert.Equal(t, "Enabled", versioning.Status)

	w = send(http.MethodPut, "/api/v1/buckets/far/objects/a/tags", `{"tags":[{"key":"kind","value":"doc"}]}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	tags, err := c.target.objectManager.GetObjectTagging(ctx, "far", "a")
	require.NoError(t, err)
	assert.Equal(t, []object.Tag{{Key: "kind", Value: "doc"}}, tags.Tags)

	w = send(http.MethodDelete, "/api/v1/buckets/far-empty", "")
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	_, err = c.target.metadataStore.GetBucketByName(ctx, "far-empty")
	assert.ErrorIs(t, err, metadata.ErrBucketNotFound)

	w = send(http.MethodGet, "/api/v1/buckets/near/versioning", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// No coordinator is elected in this cluster.
	w = send(http.MethodPost, "/api/v1/buckets/far/permissions", `{"userId":"someone","permissionLevel":"read"}`)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())

	assert.Equal(t, []string{
		"PUT /api/v1/buckets/far/versioning",
		"PUT /api/v1/buckets/far/objects/a/tags",
		"DELETE /api/v1/buckets/far-empty",
	}, target.requests())
}

// A bucket's permissions are granted where the coordinator runs, which finds
// the bucket on whichever node holds it, under the tenant it belongs to.
func TestBucketPermissionsFindTheBucketOnAnotherNode(t *testing.T) {
	c := newMigrationCluster(t)
	ctx := context.Background()
	now := time.Now().Unix()
	require.NoError(t, c.target.authManager.CreateTenant(ctx, &auth.Tenant{ID: "t-far", Name: "t-far", Status: "active"}))
	require.NoError(t, c.target.bucketManager.CreateBucket(ctx, "", "far", "admin"))
	require.NoError(t, c.target.bucketManager.CreateBucket(ctx, "t-far", "far-tenant", "u"))
	require.NoError(t, c.source.authManager.CreateUser(ctx, &auth.User{ID: "grantee", Username: "grantee",
		Status: auth.UserStatusActive, Roles: []string{auth.RoleUser}, CreatedAt: now, UpdatedAt: now}))
	grant := func(bucketName, bucketTenant string) int {
		path := "/api/v1/buckets/" + bucketName + "/permissions"
		if bucketTenant != "" {
			path += "?bucketTenantId=" + bucketTenant
		}
		req := createAuthenticatedRequest(http.MethodPost, path,
			strings.NewReader(`{"userId":"grantee","permissionLevel":"read"}`), "", "global-admin", true)
		req = mux.SetURLVars(req, map[string]string{"bucket": bucketName})
		w := httptest.NewRecorder()
		c.source.handleGrantBucketPermission(w, req)
		return w.Code
	}

	assert.Equal(t, http.StatusOK, grant("far", ""))
	assert.Equal(t, http.StatusNotFound, grant("far-tenant", ""), "the bucket is the tenant's, not a global one")
	assert.Equal(t, http.StatusOK, grant("far-tenant", "t-far"))
	assert.Equal(t, http.StatusNotFound, grant("nowhere", ""))
}

// Bucket names are unique in the cluster: the console refuses one another node
// holds.
func TestConsoleCreateBucketSeesTheWholeCluster(t *testing.T) {
	c := newMigrationCluster(t)
	ctx := context.Background()
	require.NoError(t, c.target.bucketManager.CreateBucket(ctx, "", "taken", "admin"))
	create := func(name string) *httptest.ResponseRecorder {
		req := createAuthenticatedRequest(http.MethodPost, "/api/v1/buckets",
			strings.NewReader(`{"name":"`+name+`"}`), "", "global-admin", true)
		w := httptest.NewRecorder()
		c.source.handleCreateBucket(w, req)
		return w
	}

	w := create("taken")
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	_, err := c.source.metadataStore.GetBucketByName(ctx, "taken")
	assert.ErrorIs(t, err, metadata.ErrBucketNotFound)

	w = create("free")
	assert.Less(t, w.Code, 300, w.Body.String())
	_, err = c.source.metadataStore.GetBucketByName(ctx, "free")
	assert.NoError(t, err)
}
