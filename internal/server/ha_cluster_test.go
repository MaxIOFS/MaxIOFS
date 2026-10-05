package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// haNode is one complete node of an haCluster, with an S3 client of its own
// signed as an administrator every node knows. down makes its cluster port
// answer 503; s3Down does the same for its S3 API.
type haNode struct {
	*Server
	id     string
	s3     *s3.Client
	down   atomic.Bool
	s3Down atomic.Bool
}

// haCluster is n complete nodes of one cluster with a replication factor,
// each reaching the others on their cluster port and their S3 API.
type haCluster struct {
	nodes []*haNode
}

func newHACluster(t *testing.T, n, factor int) *haCluster {
	t.Helper()
	ctx := context.Background()
	c := &haCluster{}
	clusterURLs := make([]string, n)
	apiURLs := make([]string, n)
	for i := 0; i < n; i++ {
		node := &haNode{Server: newClusterTestNode(t)}
		routes := node.clusterServer.Handler
		cs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if node.down.Load() {
				http.Error(w, "down", http.StatusServiceUnavailable)
				return
			}
			routes.ServeHTTP(w, r)
		}))
		t.Cleanup(cs.Close)
		api := node.httpServer.Handler
		as := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if node.s3Down.Load() {
				http.Error(w, "down", http.StatusServiceUnavailable)
				return
			}
			api.ServeHTTP(w, r)
		}))
		t.Cleanup(as.Close)
		clusterURLs[i], apiURLs[i] = cs.URL, as.URL
		c.nodes = append(c.nodes, node)
	}

	var token string
	for i, node := range c.nodes {
		created, err := node.clusterManager.InitializeCluster(ctx, fmt.Sprintf("node-%d", i), "us-east-1", clusterURLs[i])
		require.NoError(t, err)
		if i == 0 {
			token = created
		}
		node.id, err = node.clusterManager.GetLocalNodeID(ctx)
		require.NoError(t, err)
		// The nodes of a cluster share its token.
		_, err = node.db.ExecContext(ctx, `UPDATE cluster_config SET cluster_token = ?`, token)
		require.NoError(t, err)
		_, err = node.db.ExecContext(ctx, `UPDATE cluster_nodes SET node_token = ?, api_url = ? WHERE id = ?`, token, apiURLs[i], node.id)
		require.NoError(t, err)
	}

	now := time.Now().Unix()
	for i, node := range c.nodes {
		for j, peer := range c.nodes {
			if i == j {
				continue
			}
			require.NoError(t, node.clusterManager.AddNode(ctx, &cluster.Node{ID: peer.id, Name: fmt.Sprintf("node-%d", j),
				Endpoint: clusterURLs[j], APIURL: apiURLs[j], NodeToken: token, Region: "us-east-1", Priority: 100, Metadata: "{}"}))
			_, err := node.db.ExecContext(ctx, `UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusHealthy, peer.id)
			require.NoError(t, err)
		}
		require.NoError(t, node.clusterManager.SetReplicationFactor(ctx, factor))
		// The nodes share the test machine's disk; how full it is must not
		// put them under storage pressure.
		require.NoError(t, cluster.SetGlobalConfig(ctx, node.db, "ha.storage_pressure_threshold_percent", "100"))
		require.NoError(t, cluster.SetGlobalConfig(ctx, node.db, "ha.storage_pressure_release_percent", "99"))

		require.NoError(t, node.authManager.CreateUser(ctx, &auth.User{ID: routingAdminID, Username: routingAdminID,
			Status: auth.UserStatusActive, Roles: []string{auth.RoleAdmin}, CreatedAt: now, UpdatedAt: now}))
		key, err := node.authManager.GenerateAccessKey(ctx, routingAdminID)
		require.NoError(t, err)
		node.s3 = s3.New(s3.Options{
			BaseEndpoint:               aws.String(apiURLs[i]),
			Region:                     "us-east-1",
			UsePathStyle:               true,
			Credentials:                credentials.NewStaticCredentialsProvider(key.AccessKeyID, key.SecretAccessKey, ""),
			RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
			ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
			RetryMaxAttempts:           1,
		})
	}
	return c
}

// s3Status is the HTTP status of an S3 error, or 0.
func s3Status(err error) int {
	var answered *awshttp.ResponseError
	if errors.As(err, &answered) {
		return answered.HTTPStatusCode()
	}
	return 0
}

// A multipart upload is made on the node it was started on, whichever node
// each of its requests reaches: parts, listings, completion and abort.
func TestAMultipartUploadWorksWhicheverNodeEachRequestReaches(t *testing.T) {
	c := newHACluster(t, 2, 2)
	a, b := c.nodes[0], c.nodes[1]
	ctx := context.Background()
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "", "uploads", routingAdminID))
	require.True(t, hasBucket(b.Server, "", "uploads"))
	bucket, key := aws.String("uploads"), aws.String("big")

	upload, err := a.s3.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: bucket, Key: key})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(*upload.UploadId, a.id+"."), "the upload is named after its node: %s", *upload.UploadId)
	first := bytes.Repeat([]byte("x"), 5<<20)
	p1, err := b.s3.UploadPart(ctx, &s3.UploadPartInput{Bucket: bucket, Key: key, UploadId: upload.UploadId,
		PartNumber: aws.Int32(1), Body: bytes.NewReader(first)})
	require.NoError(t, err)
	p2, err := a.s3.UploadPart(ctx, &s3.UploadPartInput{Bucket: bucket, Key: key, UploadId: upload.UploadId,
		PartNumber: aws.Int32(2), Body: strings.NewReader("tail")})
	require.NoError(t, err)

	parts, err := b.s3.ListParts(ctx, &s3.ListPartsInput{Bucket: bucket, Key: key, UploadId: upload.UploadId})
	require.NoError(t, err)
	assert.Len(t, parts.Parts, 2)
	uploads, err := b.s3.ListMultipartUploads(ctx, &s3.ListMultipartUploadsInput{Bucket: bucket})
	require.NoError(t, err)
	require.Len(t, uploads.Uploads, 1, "b lists the upload started on a")
	assert.Equal(t, *upload.UploadId, *uploads.Uploads[0].UploadId)

	_, err = b.s3.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: bucket, Key: key, UploadId: upload.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: []types.CompletedPart{
			{ETag: p1.ETag, PartNumber: aws.Int32(1)}, {ETag: p2.ETag, PartNumber: aws.Int32(2)}}}})
	require.NoError(t, err)
	for _, n := range c.nodes {
		assert.Equal(t, string(first)+"tail", readBody(t, n.Server, "uploads", "big"))
	}

	second, err := a.s3.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: bucket, Key: key})
	require.NoError(t, err)
	_, err = b.s3.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{Bucket: bucket, Key: key, UploadId: second.UploadId})
	require.NoError(t, err)
	_, err = a.objectManager.ListParts(ctx, *second.UploadId)
	assert.Error(t, err, "aborted on a")
}

// A request about an upload whose node does not answer is answered 503: the
// upload is there and the client retries. One whose node is no longer in the
// cluster is answered NoSuchUpload. An upload named after no node is made on
// the node it reaches.
func TestAMultipartUploadWhoseNodeIsNotThere(t *testing.T) {
	c := newHACluster(t, 2, 2)
	a, b := c.nodes[0], c.nodes[1]
	ctx := context.Background()
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "", "uploads", routingAdminID))
	bucket, key := aws.String("uploads"), aws.String("k")
	upload, err := a.s3.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: bucket, Key: key})
	require.NoError(t, err)
	part := func(client *s3.Client, uploadID *string) error {
		_, err := client.UploadPart(ctx, &s3.UploadPartInput{Bucket: bucket, Key: key, UploadId: uploadID,
			PartNumber: aws.Int32(1), Body: strings.NewReader("data")})
		return err
	}

	_, err = b.db.ExecContext(ctx, `UPDATE cluster_nodes SET api_url = 'http://127.0.0.1:1' WHERE id = ?`, a.id)
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, s3Status(part(b.s3, upload.UploadId)))

	_, err = b.db.ExecContext(ctx, `DELETE FROM cluster_nodes WHERE id = ?`, a.id)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, s3Status(part(b.s3, upload.UploadId)))

	unnamed, err := a.objectManager.(*cluster.HAObjectManager).Manager.CreateMultipartUpload(ctx, "uploads", "k", http.Header{})
	require.NoError(t, err)
	require.NotContains(t, unnamed.UploadID, ".")
	assert.NoError(t, part(a.s3, aws.String(unnamed.UploadID)))
}
