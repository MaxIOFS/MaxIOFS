package server

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storage"
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
	// copies counts the copies of an object's data its cluster port takes.
	copies atomic.Int32
	// onCopy runs when its cluster port takes a copy, before the copy is made.
	onCopy atomic.Pointer[func()]
	// onEntry runs when its cluster port takes an entry, before it is stored.
	onEntry atomic.Pointer[func()]
	// copyAnswer, when set, is the status its cluster port answers a copy
	// with once the copy is made: the sender takes it as failed.
	copyAnswer atomic.Int32
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
			if r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/ha/objects/") {
				node.copies.Add(1)
				if fn := node.onCopy.Load(); fn != nil {
					(*fn)()
				}
				if code := node.copyAnswer.Load(); code != 0 {
					routes.ServeHTTP(httptest.NewRecorder(), r)
					http.Error(w, "answer lost", int(code))
					return
				}
			}
			if r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/ha/object-entry") {
				if fn := node.onEntry.Load(); fn != nil {
					(*fn)()
				}
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
		// A cluster that never changed its factor has a factor of 1 unset.
		if factor > 1 {
			require.NoError(t, node.clusterManager.SetReplicationFactor(ctx, factor))
		}
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

// holds reports whether n has the data of a key's version on its disk.
func (n *haNode) holds(t *testing.T, bucket, key, versionID string) bool {
	t.Helper()
	raw := n.objectManager.(*cluster.HAObjectManager).Manager.(object.RawObjectAccessor)
	reader, _, _, err := raw.GetObjectRaw(context.Background(), bucket, key, versionID)
	if err != nil {
		return false
	}
	reader.Close()
	return true
}

// setFree makes n see peer with that much free space.
func (n *haNode) setFree(t *testing.T, peer *haNode, free int64) {
	t.Helper()
	_, err := n.db.Exec(`UPDATE cluster_nodes SET capacity_total = 1000, capacity_used = ? WHERE id = ?`, 1000-free, peer.id)
	require.NoError(t, err)
}

func (n *haNode) entry(t *testing.T, bucket, key string) *metadata.ObjectMetadata {
	t.Helper()
	e, err := n.metadataStore.GetObject(context.Background(), bucket, key)
	require.NoError(t, err)
	return e
}

func getBody(t *testing.T, client *s3.Client, bucket, key string, opts ...func(*s3.GetObjectInput)) string {
	t.Helper()
	in := &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
	for _, o := range opts {
		o(in)
	}
	out, err := client.GetObject(context.Background(), in)
	require.NoError(t, err)
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	require.NoError(t, err)
	return string(data)
}

// placedCluster is three nodes with a factor of 2 and a bucket created on the
// first, which sees the third with more free space than the second.
func placedCluster(t *testing.T, bucketName string) (c *haCluster, a, b, far *haNode) {
	t.Helper()
	c = newHACluster(t, 3, 2)
	a, b, far = c.nodes[0], c.nodes[1], c.nodes[2]
	require.NoError(t, a.bucketManager.CreateBucket(context.Background(), "", bucketName, routingAdminID))
	a.setFree(t, b, 100)
	a.setFree(t, far, 900)
	return c, a, b, far
}

// A write is held by the node that took it and the nodes with the most free
// space, to the replication factor; every node holds its entry, lists it,
// answers HEAD and serves it, a range alone.
func TestAWriteIsHeldByTheFactorAndListedEverywhere(t *testing.T) {
	c, a, b, far := placedCluster(t, "placed")
	ctx := context.Background()

	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("hello world")})
	require.NoError(t, err)
	assert.True(t, a.holds(t, "placed", "k", ""))
	assert.True(t, far.holds(t, "placed", "k", ""), "the node with the most free space")
	assert.False(t, b.holds(t, "placed", "k", ""))
	for _, n := range c.nodes {
		assert.Equal(t, []string{a.id, far.id}, n.entry(t, "placed", "k").Locations)
		listed, err := n.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String("placed")})
		require.NoError(t, err)
		require.Len(t, listed.Contents, 1)
		head, err := n.s3.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String("placed"), Key: aws.String("k")})
		require.NoError(t, err)
		assert.EqualValues(t, 11, *head.ContentLength)
		assert.Equal(t, "hello world", getBody(t, n.s3, "placed", "k"))
	}
	assert.Equal(t, "world", getBody(t, b.s3, "placed", "k", func(in *s3.GetObjectInput) { in.Range = aws.String("bytes=6-10") }))

	upload, err := a.s3.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String("placed"), Key: aws.String("mp")})
	require.NoError(t, err)
	part, err := a.s3.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String("placed"), Key: aws.String("mp"), UploadId: upload.UploadId,
		PartNumber: aws.Int32(1), Body: strings.NewReader("parts")})
	require.NoError(t, err)
	_, err = a.s3.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: aws.String("placed"), Key: aws.String("mp"),
		UploadId: upload.UploadId, MultipartUpload: &types.CompletedMultipartUpload{Parts: []types.CompletedPart{{ETag: part.ETag, PartNumber: aws.Int32(1)}}}})
	require.NoError(t, err)
	assert.True(t, far.holds(t, "placed", "mp", ""))
	assert.False(t, b.holds(t, "placed", "mp", ""))
	assert.Equal(t, "parts", getBody(t, b.s3, "placed", "mp"))
}

// A node without the data reads it from the next node holding it when one
// does not answer; with none answering the read is answered 503, and HEAD
// still answers.
func TestAReadOfDataHeldElsewhereTriesEachHolder(t *testing.T) {
	_, a, b, far := placedCluster(t, "placed")
	ctx := context.Background()
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)

	far.down.Store(true)
	assert.Equal(t, "data", getBody(t, b.s3, "placed", "k"))
	a.down.Store(true)
	_, err = b.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("placed"), Key: aws.String("k")})
	assert.Equal(t, http.StatusServiceUnavailable, s3Status(err))
	_, err = b.s3.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String("placed"), Key: aws.String("k")})
	assert.NoError(t, err)
	_, err = b.s3.CopyObject(ctx, &s3.CopyObjectInput{Bucket: aws.String("placed"), Key: aws.String("copy"), CopySource: aws.String("placed/k")})
	assert.Equal(t, http.StatusServiceUnavailable, s3Status(err))
	w := httptest.NewRecorder()
	b.handleGetObject(w, asGlobalAdmin(httptest.NewRequest(http.MethodGet, "/api/v1/buckets/placed/objects/k", nil),
		map[string]string{"bucket": "placed", "object": "k"}))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
}

// A delete made on a node without the data removes the object from every
// node, its data from the nodes that held it.
func TestADeleteReachesEveryNode(t *testing.T) {
	c, a, b, _ := placedCluster(t, "placed")
	ctx := context.Background()
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)

	_, err = b.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String("placed"), Key: aws.String("k")})
	require.NoError(t, err)
	for _, n := range c.nodes {
		_, err := n.metadataStore.GetObject(ctx, "placed", "k")
		assert.ErrorIs(t, err, metadata.ErrObjectNotFound)
		assert.False(t, n.holds(t, "placed", "k", ""))
	}
}

// An overwrite made on another node is held where that write is placed: a
// node that held the earlier data and does not hold the new one removes it.
func TestAnOverwriteMovesItsData(t *testing.T) {
	c, a, b, far := placedCluster(t, "placed")
	ctx := context.Background()
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("first")})
	require.NoError(t, err)
	require.True(t, far.holds(t, "placed", "k", ""))

	b.setFree(t, a, 900)
	b.setFree(t, far, 100)
	_, err = b.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("second")})
	require.NoError(t, err)
	assert.True(t, b.holds(t, "placed", "k", ""))
	assert.True(t, a.holds(t, "placed", "k", ""))
	assert.False(t, far.holds(t, "placed", "k", ""), "the earlier data is removed")
	for _, n := range c.nodes {
		assert.Equal(t, []string{b.id, a.id}, n.entry(t, "placed", "k").Locations)
		assert.Equal(t, "second", getBody(t, n.s3, "placed", "k"))
	}
}

// The versions of a key are listed on every node and each is read from any.
func TestEveryVersionIsListedAndReadEverywhere(t *testing.T) {
	c, a, b, far := placedCluster(t, "versions")
	ctx := context.Background()
	require.NoError(t, a.bucketManager.SetVersioning(ctx, "", "versions", &bucket.VersioningConfig{Status: "Enabled"}))
	b.setFree(t, a, 100)
	b.setFree(t, far, 900)
	first, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("versions"), Key: aws.String("k"), Body: strings.NewReader("one")})
	require.NoError(t, err)
	second, err := b.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("versions"), Key: aws.String("k"), Body: strings.NewReader("two")})
	require.NoError(t, err)
	require.False(t, b.holds(t, "versions", "k", *first.VersionId))
	require.False(t, a.holds(t, "versions", "k", *second.VersionId))

	for _, n := range c.nodes {
		listed, err := n.s3.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{Bucket: aws.String("versions")})
		require.NoError(t, err)
		assert.Len(t, listed.Versions, 2)
		assert.Equal(t, "one", getBody(t, n.s3, "versions", "k", func(in *s3.GetObjectInput) { in.VersionId = first.VersionId }))
		assert.Equal(t, "two", getBody(t, n.s3, "versions", "k"))
		head, err := n.s3.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String("versions"), Key: aws.String("k"), VersionId: first.VersionId})
		require.NoError(t, err)
		assert.Equal(t, *first.VersionId, *head.VersionId)
		assert.EqualValues(t, 3, *head.ContentLength)
	}
}

// The tags of an object are set on a node without its data and read on all.
func TestTheTagsOfAnObjectWhoseDataIsElsewhere(t *testing.T) {
	c, a, b, _ := placedCluster(t, "placed")
	ctx := context.Background()
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)

	_, err = b.s3.PutObjectTagging(ctx, &s3.PutObjectTaggingInput{Bucket: aws.String("placed"), Key: aws.String("k"),
		Tagging: &types.Tagging{TagSet: []types.Tag{{Key: aws.String("team"), Value: aws.String("blue")}}}})
	require.NoError(t, err)
	for _, n := range c.nodes {
		tags, err := n.s3.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{Bucket: aws.String("placed"), Key: aws.String("k")})
		require.NoError(t, err)
		require.Len(t, tags.TagSet, 1)
		assert.Equal(t, "blue", *tags.TagSet[0].Value)
	}
}

// A node asked by another for data it does not hold answers 404 and does not
// read it from a third node. While no node holding the data answers, its
// console still finds the object to issue a download token or a presigned URL.
func TestANodeServesOnlyTheDataItHolds(t *testing.T) {
	_, a, b, far := placedCluster(t, "placed")
	ctx := context.Background()
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)

	req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/api/internal/cluster/ha/objects/k?bucket=placed", nil), map[string]string{"key": "k"})
	w := httptest.NewRecorder()
	b.handleHAGetObject(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	a.down.Store(true)
	far.down.Store(true)
	vars := map[string]string{"bucket": "placed", "object": "k"}
	w = httptest.NewRecorder()
	b.handleCreateDownloadToken(w, asGlobalAdmin(httptest.NewRequest(http.MethodPost, "/api/v1/buckets/placed/objects/k/download-token", nil), vars))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, err = b.authManager.GenerateAccessKey(ctx, "admin")
	require.NoError(t, err)
	w = httptest.NewRecorder()
	b.handleGeneratePresignedURL(w, asGlobalAdmin(httptest.NewRequest(http.MethodPost, "/api/v1/buckets/placed/objects/k/presigned-url", strings.NewReader("{}")), vars))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// scrubRuns is how many anti-entropy runs n completed.
func (n *haNode) scrubRuns(t *testing.T) int {
	t.Helper()
	runs, err := n.antiEntropyScrubber.ListRecentRuns(context.Background(), 10)
	require.NoError(t, err)
	done := 0
	for _, run := range runs {
		if run.Status == "done" {
			done++
		}
	}
	return done
}

// A node that missed writes is caught up with the data of those it is to hold
// and the entry of the others.
func TestANodeThatMissedWritesIsSentWhatItHolds(t *testing.T) {
	_, a, b, far := placedCluster(t, "placed")
	ctx := context.Background()
	a.setFree(t, b, 900)
	a.setFree(t, far, 100)
	b.down.Store(true)
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("its"), Body: strings.NewReader("held by b")})
	require.NoError(t, err)
	_, err = a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("other"), Body: strings.NewReader("held elsewhere")})
	require.NoError(t, err)
	require.Equal(t, []string{a.id, b.id}, a.entry(t, "placed", "its").Locations)
	require.Equal(t, []string{a.id, far.id}, a.entry(t, "placed", "other").Locations, "b was down")

	b.down.Store(false)
	startScrubber(t, a.Server)
	_, err = a.clusterManager.CheckNodeHealth(ctx, b.id)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, err := b.metadataStore.GetObject(ctx, "placed", "other")
		return b.holds(t, "placed", "its", "") && err == nil
	}, 15*time.Second, 50*time.Millisecond)
	assert.False(t, b.holds(t, "placed", "other", ""), "b is sent the entry only")
	assert.Equal(t, []string{a.id, far.id}, b.entry(t, "placed", "other").Locations)
	assert.Equal(t, "held elsewhere", getBody(t, b.s3, "placed", "other"))
}

// A node that is to hold data it lost is sent it by a node that holds it.
func TestAHolderThatLostItsDataIsSentItAgain(t *testing.T) {
	_, a, b, far := placedCluster(t, "placed")
	ctx := context.Background()
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "", "versions", routingAdminID))
	require.NoError(t, a.bucketManager.SetVersioning(ctx, "", "versions", &bucket.VersioningConfig{Status: "Enabled"}))
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	version, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("versions"), Key: aws.String("k"), Body: strings.NewReader("version")})
	require.NoError(t, err)

	startScrubber(t, a.Server)
	compare := func() {
		t.Helper()
		before := a.scrubRuns(t)
		a.antiEntropyScrubber.CatchUp(far.id, time.Unix(1, 0))
		require.Eventually(t, func() bool { return a.scrubRuns(t) > before }, 15*time.Second, 50*time.Millisecond)
	}
	copies := far.copies.Load()
	compare()
	assert.Equal(t, copies, far.copies.Load(), "data far holds is not sent again")

	require.NoError(t, far.storageBackend.Delete(ctx, storage.ObjectRef{Bucket: "placed", Key: "k"}))
	require.NoError(t, far.storageBackend.Delete(ctx, storage.ObjectRef{Bucket: "versions", Key: "k", VersionID: *version.VersionId}))
	require.False(t, far.holds(t, "placed", "k", ""))
	require.False(t, far.holds(t, "versions", "k", *version.VersionId))

	compare()
	assert.True(t, far.holds(t, "placed", "k", ""))
	assert.True(t, far.holds(t, "versions", "k", *version.VersionId))
	assert.Equal(t, "data", getBody(t, far.s3, "placed", "k"))
	assert.False(t, b.holds(t, "placed", "k", ""), "a node not to hold it is not sent it")
}

// The initial sync sends each node the entry of every object, and the data of
// those it is to hold.
func TestAnInitialSyncSendsEachNodeWhatItHolds(t *testing.T) {
	_, a, b, far := placedCluster(t, "placed")
	ctx := object.WithLocations(context.Background(), []string{a.id, b.id})
	local := a.objectManager.(*cluster.HAObjectManager).Manager
	_, err := local.PutObject(ctx, "placed", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)

	a.haSyncWorker.Trigger(context.Background())
	require.Eventually(t, func() bool {
		_, err := far.metadataStore.GetObject(context.Background(), "placed", "k")
		return b.holds(t, "placed", "k", "") && err == nil
	}, 15*time.Second, 50*time.Millisecond)
	assert.False(t, far.holds(t, "placed", "k", ""))
	assert.Equal(t, []string{a.id, b.id}, far.entry(t, "placed", "k").Locations)
}

// A node that compares a key with a node holding a later write of it, and is
// not to hold that write's data, takes its entry only.
func TestALaterWriteIsTakenAsAnEntryByANodeNotHoldingIt(t *testing.T) {
	_, a, b, far := placedCluster(t, "placed")
	ctx := context.Background()
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("first")})
	require.NoError(t, err)
	time.Sleep(1100 * time.Millisecond)
	later := object.WithLocations(ctx, []string{far.id, a.id})
	_, err = far.objectManager.(*cluster.HAObjectManager).Manager.PutObject(later, "placed", "k", strings.NewReader("second"), http.Header{})
	require.NoError(t, err)

	startScrubber(t, b.Server)
	before := b.scrubRuns(t)
	b.antiEntropyScrubber.CatchUp(far.id, time.Unix(1, 0))
	require.Eventually(t, func() bool { return b.scrubRuns(t) > before }, 15*time.Second, 50*time.Millisecond)
	assert.Equal(t, far.entry(t, "placed", "k").ETag, b.entry(t, "placed", "k").ETag)
	assert.False(t, b.holds(t, "placed", "k", ""))
	assert.Equal(t, "second", getBody(t, b.s3, "placed", "k"))

	// a is to hold it: it takes the data, with the nodes the entry names.
	startScrubber(t, a.Server)
	before = a.scrubRuns(t)
	a.antiEntropyScrubber.CatchUp(far.id, time.Unix(1, 0))
	require.Eventually(t, func() bool { return a.scrubRuns(t) > before }, 15*time.Second, 50*time.Millisecond)
	assert.True(t, a.holds(t, "placed", "k", ""))
	assert.Equal(t, []string{far.id, a.id}, a.entry(t, "placed", "k").Locations)
}

// firstOf is the node with the smallest ID among nodes: the one that repairs
// a write they hold.
func firstOf(nodes ...*haNode) *haNode {
	first := nodes[0]
	for _, n := range nodes[1:] {
		if n.id < first.id {
			first = n
		}
	}
	return first
}

func (c *haCluster) except(nodes ...*haNode) []*haNode {
	var rest []*haNode
	for _, n := range c.nodes {
		if !slices.Contains(nodes, n) {
			rest = append(rest, n)
		}
	}
	return rest
}

func (c *haCluster) byID(id string) *haNode {
	for _, n := range c.nodes {
		if n.id == id {
			return n
		}
	}
	return nil
}

// The copies a removed node held are made again on the healthy node with the
// most free space, and every node is told where they are.
func TestTheCopiesOfARemovedNodeAreMadeAgain(t *testing.T) {
	_, a, b, far := placedCluster(t, "placed")
	ctx := context.Background()
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	require.True(t, far.holds(t, "placed", "k", ""))

	require.NoError(t, a.clusterManager.RemoveNode(ctx, far.id))
	require.Eventually(t, func() bool {
		return b.holds(t, "placed", "k", "") && slices.Equal([]string{a.id, b.id}, b.entry(t, "placed", "k").Locations)
	}, 15*time.Second, 50*time.Millisecond)
	assert.Equal(t, []string{a.id, b.id}, a.entry(t, "placed", "k").Locations)
	assert.EqualValues(t, 2, a.entry(t, "placed", "k").LocationsGen)
	assert.EqualValues(t, 2, b.entry(t, "placed", "k").LocationsGen)
	assert.Equal(t, "data", getBody(t, b.s3, "placed", "k"))
}

// A lower replication factor keeps the copies of the nodes with the most free
// space; the others remove theirs.
func TestALowerFactorDropsTheExtraCopies(t *testing.T) {
	c := newHACluster(t, 3, 3)
	ctx := context.Background()
	require.NoError(t, c.nodes[0].bucketManager.CreateBucket(ctx, "", "placed", routingAdminID))
	_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	first := firstOf(c.nodes...)
	others := c.except(first)
	first.setFree(t, first, 900)
	first.setFree(t, others[0], 900)
	first.setFree(t, others[1], 700)

	// Set from the console of the node that repairs; the others repair too,
	// and leave the write to it.
	for _, n := range others {
		require.NoError(t, n.clusterManager.SetReplicationFactor(ctx, 2))
		n.setFree(t, n, 900)
		for _, peer := range c.except(n) {
			n.setFree(t, peer, 100)
		}
	}
	w := httptest.NewRecorder()
	first.handleSetClusterHA(w, asGlobalAdmin(httptest.NewRequest(http.MethodPut, "/api/v1/cluster/ha", strings.NewReader(`{"factor":2}`)), nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	for _, n := range others {
		n.haSyncWorker.StartRepair(ctx)
	}
	require.Eventually(t, func() bool { return !others[1].holds(t, "placed", "k", "") }, 15*time.Second, 50*time.Millisecond)
	for _, n := range c.nodes {
		require.Eventually(t, func() bool {
			return slices.Equal([]string{first.id, others[0].id}, n.entry(t, "placed", "k").Locations)
		}, 15*time.Second, 50*time.Millisecond)
		assert.Equal(t, "data", getBody(t, n.s3, "placed", "k"))
	}
	assert.True(t, others[0].holds(t, "placed", "k", ""))
}

// A higher replication factor set from the console copies every write to the
// nodes it needs once the first syncs end, and the work goes on once the
// request is answered. A lower one removes the copies it does not need, down
// to one copy with a factor of 1.
func TestAHigherFactorAddsTheCopiesItNeeds(t *testing.T) {
	c := newHACluster(t, 3, 2)
	ctx := context.Background()
	require.NoError(t, c.nodes[0].bucketManager.CreateBucket(ctx, "", "placed", routingAdminID))
	_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	locations := c.nodes[0].entry(t, "placed", "k").Locations
	require.Len(t, locations, 2)
	first := firstOf(c.byID(locations[0]), c.byID(locations[1]))
	missing := c.except(c.byID(locations[0]), c.byID(locations[1]))[0]
	// The entry the first sync sends is held a while: no copy is made before
	// the sync ends.
	var copiedDuringSync atomic.Bool
	released := make(chan struct{})
	var once sync.Once
	hold := func() {
		time.Sleep(500 * time.Millisecond)
		copiedDuringSync.Store(missing.holds(t, "placed", "k", ""))
		once.Do(func() { close(released) })
	}
	missing.onEntry.Store(&hold)
	for _, n := range c.nodes {
		first.setFree(t, n, 900)
		if n != first {
			require.NoError(t, n.clusterManager.SetReplicationFactor(ctx, 3))
		}
	}

	setFactor := func(factor int) *httptest.ResponseRecorder {
		reqCtx, cancel := context.WithCancel(ctx)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/cluster/ha", strings.NewReader(fmt.Sprintf(`{"factor":%d}`, factor))).WithContext(reqCtx)
		w := httptest.NewRecorder()
		first.handleSetClusterHA(w, asGlobalAdmin(req, nil))
		cancel()
		return w
	}
	w := setFactor(3)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Eventually(t, func() bool { return missing.holds(t, "placed", "k", "") }, 15*time.Second, 50*time.Millisecond)
	select {
	case <-released:
	case <-time.After(15 * time.Second):
		t.Fatal("the first sync did not send the entry")
	}
	assert.False(t, copiedDuringSync.Load(), "the copy is made once the sync ends")
	for _, n := range c.nodes {
		require.Eventually(t, func() bool { return len(n.entry(t, "placed", "k").Locations) == 3 }, 15*time.Second, 50*time.Millisecond)
	}
	require.Eventually(t, func() bool {
		jobs, err := first.haSyncWorker.GetSyncJobs(ctx)
		require.NoError(t, err)
		for _, j := range jobs {
			if j.Status != cluster.SyncJobDone {
				return false
			}
		}
		return len(jobs) > 0
	}, 15*time.Second, 50*time.Millisecond, "the sync jobs end after the request")

	w = setFactor(2)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Eventually(t, func() bool {
		var holders int
		for _, n := range c.nodes {
			if n.holds(t, "placed", "k", "") {
				holders++
			}
		}
		return holders == 2
	}, 15*time.Second, 50*time.Millisecond, "with the syncs done, the copies are repaired at once")

	w = setFactor(1)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Eventually(t, func() bool {
		var holders int
		for _, n := range c.nodes {
			if n.holds(t, "placed", "k", "") {
				holders++
			}
			if len(n.entry(t, "placed", "k").Locations) != 1 {
				return false
			}
		}
		return holders == 1
	}, 15*time.Second, 50*time.Millisecond, "a factor of 1 keeps one copy")
	for _, n := range c.nodes {
		assert.Equal(t, "data", getBody(t, n.s3, "placed", "k"))
	}
}

// Each full anti-entropy cycle repairs the copies: a write a dead node held is
// copied to a healthy node.
func TestAFullCycleRepairsTheCopies(t *testing.T) {
	_, a, b, far := placedCluster(t, "placed")
	ctx := context.Background()
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	_, err = a.db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusDead, far.id)
	require.NoError(t, err)

	startScrubber(t, a.Server)
	before := a.scrubRuns(t)
	a.antiEntropyScrubber.CatchUp(b.id, time.Now())
	require.Eventually(t, func() bool { return a.scrubRuns(t) > before }, 15*time.Second, 50*time.Millisecond)
	time.Sleep(500 * time.Millisecond)
	assert.False(t, b.holds(t, "placed", "k", ""), "a catch-up of what changed since a time repairs nothing")

	a.antiEntropyScrubber.CatchUp(b.id, time.Unix(1, 0))
	require.Eventually(t, func() bool {
		return b.holds(t, "placed", "k", "") && slices.Equal([]string{a.id, b.id}, a.entry(t, "placed", "k").Locations)
	}, 15*time.Second, 50*time.Millisecond)
}

// underPressure makes n see itself under storage pressure.
func (n *haNode) underPressure(t *testing.T) {
	t.Helper()
	n.seesUnderPressure(t, n)
}

// seesUnderPressure makes n see peer under storage pressure.
func (n *haNode) seesUnderPressure(t *testing.T, peer *haNode) {
	t.Helper()
	_, err := n.db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusStoragePressure, peer.id)
	require.NoError(t, err)
}

// A node under storage pressure passes its copy of a write it takes to the
// node with the most free space among those that hold the entry only: an
// object, a version and a multipart upload.
func TestANodeUnderStoragePressurePassesItsCopyOn(t *testing.T) {
	c, a, b, far := placedCluster(t, "full")
	ctx := context.Background()
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "", "fullv", routingAdminID))
	require.NoError(t, a.bucketManager.SetVersioning(ctx, "", "fullv", &bucket.VersioningConfig{Status: "Enabled"}))
	a.underPressure(t)

	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("full"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	version, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("fullv"), Key: aws.String("k"), Body: strings.NewReader("version")})
	require.NoError(t, err)
	upload, err := a.s3.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String("full"), Key: aws.String("mp")})
	require.NoError(t, err)
	part, err := a.s3.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String("full"), Key: aws.String("mp"), UploadId: upload.UploadId,
		PartNumber: aws.Int32(1), Body: strings.NewReader("parts")})
	require.NoError(t, err)
	_, err = a.s3.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: aws.String("full"), Key: aws.String("mp"), UploadId: upload.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: []types.CompletedPart{{ETag: part.ETag, PartNumber: aws.Int32(1)}}}})
	require.NoError(t, err)

	for _, w := range []struct{ bucket, key, versionID, body string }{
		{"full", "k", "", "data"}, {"fullv", "k", *version.VersionId, "version"}, {"full", "mp", "", "parts"},
	} {
		assert.False(t, a.holds(t, w.bucket, w.key, w.versionID), w.bucket+"/"+w.key)
		assert.True(t, far.holds(t, w.bucket, w.key, w.versionID), w.bucket+"/"+w.key)
		assert.True(t, b.holds(t, w.bucket, w.key, w.versionID), w.bucket+"/"+w.key)
		for _, n := range c.nodes {
			assert.Equal(t, []string{far.id, b.id}, n.entry(t, w.bucket, w.key).Locations, w.bucket+"/"+w.key)
			assert.EqualValues(t, 2, n.entry(t, w.bucket, w.key).LocationsGen, w.bucket+"/"+w.key)
			assert.Equal(t, w.body, getBody(t, n.s3, w.bucket, w.key))
		}
	}
}

// A node under storage pressure passes its copy to one node, the one with the
// most free space among those holding the entry only; to the next when one
// does not take it; and keeps it when none does.
func TestANodeUnderStoragePressureKeepsACopyNoNodeTakes(t *testing.T) {
	c := newHACluster(t, 4, 2)
	a, near, mid, far := c.nodes[0], c.nodes[1], c.nodes[2], c.nodes[3]
	ctx := context.Background()
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "", "full", routingAdminID))
	a.setFree(t, far, 900)
	a.setFree(t, mid, 500)
	a.setFree(t, near, 100)
	a.underPressure(t)

	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("full"), Key: aws.String("one"), Body: strings.NewReader("one")})
	require.NoError(t, err)
	assert.True(t, mid.holds(t, "full", "one", ""))
	assert.Zero(t, near.copies.Load(), "one node takes the copy")
	assert.Equal(t, []string{far.id, mid.id}, a.entry(t, "full", "one").Locations)

	mid.down.Store(true)
	_, err = a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("full"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	assert.False(t, a.holds(t, "full", "k", ""))
	assert.True(t, near.holds(t, "full", "k", ""))
	assert.Equal(t, []string{far.id, near.id}, a.entry(t, "full", "k").Locations)

	near.down.Store(true)
	_, err = a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("full"), Key: aws.String("j"), Body: strings.NewReader("kept")})
	require.NoError(t, err)
	assert.True(t, a.holds(t, "full", "j", ""))
	for _, n := range []*haNode{a, far} {
		assert.Equal(t, []string{a.id, far.id}, n.entry(t, "full", "j").Locations)
		assert.Equal(t, "kept", getBody(t, n.s3, "full", "j"))
	}
}

// A node under storage pressure passes its copy on even when the client goes
// away before the answer.
func TestACopyIsPassedOnWhenTheClientGoesAway(t *testing.T) {
	c, a, b, far := placedCluster(t, "full")
	a.underPressure(t)
	reqCtx, cancel := context.WithCancel(context.Background())
	gone := func() {
		cancel()
		time.Sleep(300 * time.Millisecond)
	}
	b.onCopy.Store(&gone)

	_, err := a.s3.PutObject(reqCtx, &s3.PutObjectInput{Bucket: aws.String("full"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.Error(t, err)
	require.Eventually(t, func() bool { return !a.holds(t, "full", "k", "") }, 15*time.Second, 50*time.Millisecond)
	for _, n := range c.nodes {
		require.Eventually(t, func() bool {
			return slices.Equal([]string{far.id, b.id}, n.entry(t, "full", "k").Locations)
		}, 15*time.Second, 50*time.Millisecond)
	}
	assert.True(t, b.holds(t, "full", "k", ""))
}

// A node under storage pressure with no node holding the entry only keeps its
// copy, and the locations are not changed.
func TestANodeUnderStoragePressureWithNoOtherNodeKeepsItsCopy(t *testing.T) {
	c := newHACluster(t, 2, 2)
	a, b := c.nodes[0], c.nodes[1]
	ctx := context.Background()
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "", "full", routingAdminID))
	a.underPressure(t)
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("full"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	assert.True(t, a.holds(t, "full", "k", ""))
	for _, n := range c.nodes {
		assert.Equal(t, []string{a.id, b.id}, n.entry(t, "full", "k").Locations)
		assert.Zero(t, n.entry(t, "full", "k").LocationsGen)
	}
}

// A copy a node took and whose answer was lost is removed there: the node under
// storage pressure keeps its own, and every node is told the holders with a
// higher number than the copy's.
func TestACopyWhoseAnswerIsLostIsRemovedAfterAMove(t *testing.T) {
	c, a, b, far := placedCluster(t, "full")
	ctx := context.Background()
	a.underPressure(t)
	b.copyAnswer.Store(http.StatusInternalServerError)
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("full"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	assert.True(t, a.holds(t, "full", "k", ""))
	assert.False(t, b.holds(t, "full", "k", ""), "the copy b took is removed")
	for _, n := range c.nodes {
		assert.Equal(t, []string{a.id, far.id}, n.entry(t, "full", "k").Locations)
		assert.EqualValues(t, 2, n.entry(t, "full", "k").LocationsGen)
	}
}

// A copy a node took for a repair and whose answer was lost is removed there:
// every node is told the holders with a higher number than the copy's.
func TestACopyWhoseAnswerIsLostIsRemovedAfterARepair(t *testing.T) {
	c, a, b, far := placedCluster(t, "placed")
	ctx := context.Background()
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	for _, n := range c.nodes {
		require.NoError(t, n.clusterManager.SetReplicationFactor(ctx, 3))
	}
	b.copyAnswer.Store(http.StatusInternalServerError)

	require.NoError(t, firstOf(a, far).haSyncWorker.RepairPlacement(ctx))
	assert.Equal(t, int32(1), b.copies.Load(), "b took the copy")
	assert.False(t, b.holds(t, "placed", "k", ""), "and removed it")
	for _, n := range c.nodes {
		assert.Equal(t, []string{a.id, far.id}, n.entry(t, "placed", "k").Locations)
		assert.EqualValues(t, 2, n.entry(t, "placed", "k").LocationsGen)
	}
}

// sosapiCapacity is the capacity.xml n answers for bucket.
func (n *haNode) sosapiCapacity(t *testing.T, bucket string) (capacity, available int64) {
	t.Helper()
	out, err := n.s3.GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String(bucket),
		Key: aws.String(".system-d26a9498-cb7c-4a87-a44a-8ae204f5ba6c/capacity.xml")})
	require.NoError(t, err)
	defer out.Body.Close()
	var info struct {
		Capacity  int64 `xml:"Capacity"`
		Available int64 `xml:"Available"`
	}
	require.NoError(t, xml.NewDecoder(out.Body).Decode(&info))
	return info.Capacity, info.Available
}

// With a factor above 1 every node reports to Veeam, for a bucket without a
// quota, the room of the cluster.
func TestEveryNodeReportsTheRoomOfTheCluster(t *testing.T) {
	c := newHACluster(t, 3, 2)
	ctx := context.Background()
	require.NoError(t, c.nodes[0].bucketManager.CreateBucket(ctx, "", "veeam", routingAdminID))
	for _, n := range c.nodes {
		for i, peer := range c.nodes {
			n.setFree(t, peer, []int64{800, 600, 100}[i])
		}
	}
	for _, n := range c.nodes {
		capacity, available := n.sosapiCapacity(t, "veeam")
		assert.EqualValues(t, 1500, capacity)
		assert.EqualValues(t, 700, available)
	}
}

// A bucket without a quota of its own reports to Veeam the quota of its tenant
// less what the tenant uses on every node.
func TestVeeamSeesWhatATenantUsesOnEveryNode(t *testing.T) {
	c := newHACluster(t, 2, 1)
	a, b := c.nodes[0], c.nodes[1]
	ctx := context.Background()
	for _, n := range c.nodes {
		require.NoError(t, n.authManager.CreateTenant(ctx, &auth.Tenant{ID: "spread", Name: "spread", DisplayName: "Spread",
			Status: "active", MaxStorageBytes: 1000}))
	}
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "spread", "here", routingAdminID))
	require.NoError(t, b.bucketManager.CreateBucket(ctx, "spread", "there", routingAdminID))
	_, err := a.objectManager.PutObject(ctx, "spread/here", "k", strings.NewReader(strings.Repeat("x", 100)), http.Header{})
	require.NoError(t, err)
	_, err = b.objectManager.PutObject(ctx, "spread/there", "k", strings.NewReader(strings.Repeat("x", 300)), http.Header{})
	require.NoError(t, err)

	capacity, available := a.sosapiCapacity(t, "here")
	assert.EqualValues(t, 1000, capacity)
	assert.EqualValues(t, 600, available)
}

// The usage of every bucket and of its tenant is the same on every node and
// counts each write once, whichever node takes it and whichever nodes hold its
// data; what is counted as the writes go is what the entries count.
func TestUsageIsTheSameOnEveryNode(t *testing.T) {
	c := newHACluster(t, 3, 2)
	a, b, far := c.nodes[0], c.nodes[1], c.nodes[2]
	ctx := context.Background()
	for _, n := range c.nodes {
		require.NoError(t, n.authManager.CreateTenant(ctx, &auth.Tenant{ID: "acme", Name: "acme", DisplayName: "Acme", Status: "active"}))
	}
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "acme", "plain", routingAdminID))
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "acme", "versions", routingAdminID))
	require.NoError(t, a.bucketManager.SetVersioning(ctx, "acme", "versions", &bucket.VersioningConfig{Status: "Enabled"}))
	a.setFree(t, b, 100)
	a.setFree(t, far, 900)
	put := func(n *haNode, bucket, key string, size int) *object.Object {
		t.Helper()
		obj, err := n.objectManager.PutObject(ctx, "acme/"+bucket, key, strings.NewReader(strings.Repeat("x", size)), http.Header{})
		require.NoError(t, err)
		return obj
	}

	put(a, "plain", "one", 100)
	put(b, "plain", "two", 200)
	put(far, "plain", "three", 300)
	put(b, "plain", "one", 150)
	_, err := far.objectManager.DeleteObject(ctx, "acme/plain", "two", false)
	require.NoError(t, err)
	upload, err := b.objectManager.CreateMultipartUpload(ctx, "acme/plain", "big", http.Header{})
	require.NoError(t, err)
	p1, err := b.objectManager.UploadPart(ctx, upload.UploadID, 1, bytes.NewReader(bytes.Repeat([]byte("x"), 5<<20)))
	require.NoError(t, err)
	p2, err := b.objectManager.UploadPart(ctx, upload.UploadID, 2, strings.NewReader("tail"))
	require.NoError(t, err)
	_, err = b.objectManager.CompleteMultipartUpload(ctx, upload.UploadID, []object.Part{*p1, *p2})
	require.NoError(t, err)
	a.underPressure(t)
	put(a, "plain", "moved", 40)

	first := put(a, "versions", "v", 10)
	put(b, "versions", "v", 20)
	_, err = far.objectManager.DeleteObject(ctx, "acme/versions", "v", false)
	require.NoError(t, err)
	_, err = a.objectManager.DeleteObject(ctx, "acme/versions", "v", false, first.VersionID)
	require.NoError(t, err)

	plainSize := int64(150 + 300 + 5<<20 + 4 + 40)
	check := func(when string) {
		t.Helper()
		for _, n := range c.nodes {
			plain, err := n.metadataStore.GetBucket(ctx, "acme", "plain")
			require.NoError(t, err)
			assert.EqualValues(t, 4, plain.ObjectCount, "objects, %s, node %s", when, n.id)
			assert.Equal(t, plainSize, plain.TotalSize, "size, %s, node %s", when, n.id)
			versions, err := n.metadataStore.GetBucket(ctx, "acme", "versions")
			require.NoError(t, err)
			assert.EqualValues(t, 0, versions.ObjectCount, "versioned objects, %s, node %s", when, n.id)
			assert.EqualValues(t, 20, versions.TotalSize, "versioned size, %s, node %s", when, n.id)
			tenant, err := n.authManager.GetTenant(ctx, "acme")
			require.NoError(t, err)
			assert.Equal(t, plainSize+20, tenant.CurrentStorageBytes, "tenant, %s, node %s", when, n.id)
		}
	}
	check("as the writes went")
	for _, n := range c.nodes {
		n.reconcileBucketStats(ctx)
	}
	check("counted from the entries")
}

// A factor raised from 1 on one node reaches every node: the copies the
// factor needs of the writes each node holds alone are made, one per write,
// and every node reads them.
func TestAFactorRaisedFromOneCopiesTheWritesOfEveryNode(t *testing.T) {
	c := newHACluster(t, 3, 1)
	a := c.nodes[0]
	ctx := context.Background()
	type write struct {
		owner                            *haNode
		bucket, key, versionID, contents string
	}
	var writes []write
	for i, n := range c.nodes {
		name := fmt.Sprintf("own-%d", i)
		require.NoError(t, n.bucketManager.CreateBucket(ctx, "", name, routingAdminID))
		obj, err := n.objectManager.PutObject(ctx, name, "k", strings.NewReader(name), http.Header{})
		require.NoError(t, err)
		writes = append(writes, write{n, name, "k", obj.VersionID, name})
	}
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "", "own-versions", routingAdminID))
	require.NoError(t, a.bucketManager.SetVersioning(ctx, "", "own-versions", &bucket.VersioningConfig{Status: "Enabled"}))
	for _, contents := range []string{"one", "two"} {
		obj, err := a.objectManager.PutObject(ctx, "own-versions", "v", strings.NewReader(contents), http.Header{})
		require.NoError(t, err)
		writes = append(writes, write{a, "own-versions", "v", obj.VersionID, contents})
	}

	w := httptest.NewRecorder()
	a.handleSetClusterHA(w, asGlobalAdmin(httptest.NewRequest(http.MethodPut, "/api/v1/cluster/ha", strings.NewReader(`{"factor":2}`)), nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	for _, wr := range writes {
		require.Eventually(t, func() bool {
			var holders int
			var locations []string
			for i, n := range c.nodes {
				if n.holds(t, wr.bucket, wr.key, wr.versionID) {
					holders++
				}
				e, err := n.metadataStore.GetObject(ctx, wr.bucket, wr.key, wr.versionID)
				if err != nil || (i > 0 && !slices.Equal(e.Locations, locations)) {
					return false
				}
				locations = e.Locations
			}
			return holders == 2 && len(locations) == 2 && slices.Contains(locations, wr.owner.id)
		}, 15*time.Second, 50*time.Millisecond, "%s/%s", wr.bucket, wr.key)
		for _, n := range c.nodes {
			assert.Equal(t, wr.contents, getBody(t, n.s3, wr.bucket, wr.key, func(in *s3.GetObjectInput) {
				if wr.versionID != "" {
					in.VersionId = aws.String(wr.versionID)
				}
			}))
		}
	}
	var copies int32
	for _, n := range c.nodes {
		copies += n.copies.Load()
	}
	assert.EqualValues(t, len(writes), copies, "one copy of each write is made")
}

// Writes that name no node, made before the cluster kept their locations, are
// given the nodes that hold them by the first of those nodes, trimmed to the
// factor, and a write held by fewer is copied; a node holding another write of
// the key does not count. Without the answer of every healthy node they wait
// for the next repair.
func TestWritesThatNameNoNodeAreGivenTheirHolders(t *testing.T) {
	c := newHACluster(t, 3, 2)
	ctx := context.Background()
	first := firstOf(c.nodes...)
	silent, third := c.except(first)[0], c.except(first)[1]
	require.NoError(t, c.nodes[0].bucketManager.CreateBucket(ctx, "", "old", routingAdminID))
	require.NoError(t, c.nodes[0].bucketManager.CreateBucket(ctx, "", "oldv", routingAdminID))
	require.NoError(t, c.nodes[0].bucketManager.SetVersioning(ctx, "", "oldv", &bucket.VersioningConfig{Status: "Enabled"}))
	first.setFree(t, silent, 100)
	first.setFree(t, third, 900)
	first.seesUnderPressure(t, third)
	at := time.Now().Truncate(time.Second)
	const versionID = "1000000000000000000.old"
	legacy := func(n *haNode, bucket, key, versionID, contents string, at time.Time) {
		t.Helper()
		copyCtx := object.WithReplicatedWrittenAt(object.WithReplicatedLastModified(object.WithReplicaCopy(ctx), at), at.UnixNano())
		if versionID != "" {
			copyCtx = object.WithReplicatedVersionID(copyCtx, versionID)
		}
		_, err := n.objectManager.(*cluster.HAObjectManager).Manager.PutObject(copyCtx, bucket, key, strings.NewReader(contents), http.Header{})
		require.NoError(t, err)
	}
	for _, n := range c.nodes {
		legacy(n, "old", "everywhere", "", "everywhere", at)
		legacy(n, "oldv", "v", versionID, "v", at)
	}
	legacy(first, "old", "diverged", "", "one write", at)
	legacy(silent, "old", "diverged", "", "one write", at)
	legacy(third, "old", "diverged", "", "another write", at.Add(time.Second))
	const loneVersion = "1000000000000000000.lone"
	legacy(third, "old", "lone", "", "lone", at)
	legacy(third, "oldv", "lonev", loneVersion, "lonev", at)
	for _, held := range []struct{ bucket, key, versionID string }{{"old", "lone", ""}, {"oldv", "lonev", loneVersion}} {
		entry, err := third.metadataStore.GetObject(ctx, held.bucket, held.key, held.versionID)
		require.NoError(t, err)
		for _, n := range []*haNode{first, silent} {
			writer, ok := cluster.ReplicaWriter(n.objectManager)
			require.True(t, ok)
			require.NoError(t, writer.PutReplicaMetadata(ctx, entry))
		}
	}

	silent.down.Store(true)
	assert.Error(t, first.haSyncWorker.RepairPlacement(ctx))
	assert.Empty(t, first.entry(t, "old", "everywhere").Locations, "without the answer of every healthy node")
	silent.down.Store(false)
	require.NoError(t, silent.haSyncWorker.RepairPlacement(ctx))
	assert.Empty(t, silent.entry(t, "old", "everywhere").Locations, "a holder that is not the first leaves the write to it")

	require.NoError(t, first.haSyncWorker.RepairPlacement(ctx))
	assert.Equal(t, []string{first.id, third.id}, first.entry(t, "old", "everywhere").Locations, "the holders with the most free space, one under storage pressure too")
	assert.Equal(t, []string{first.id, silent.id}, first.entry(t, "old", "diverged").Locations, "the node with another write does not hold this one")
	for _, n := range c.nodes {
		assert.Zero(t, n.copies.Load(), "the writes held by enough nodes are not copied")
	}

	require.NoError(t, third.haSyncWorker.RepairPlacement(ctx))
	for _, w := range []struct{ bucket, key, versionID string }{{"old", "everywhere", ""}, {"oldv", "v", versionID}, {"old", "lone", ""}, {"oldv", "lonev", loneVersion}} {
		locations := first.entry(t, w.bucket, w.key).Locations
		require.Len(t, locations, 2, w.key)
		var holders int
		for _, n := range c.nodes {
			assert.Equal(t, locations, n.entry(t, w.bucket, w.key).Locations, w.key)
			assert.Equal(t, w.key, getBody(t, n.s3, w.bucket, w.key))
			if n.holds(t, w.bucket, w.key, w.versionID) {
				holders++
			}
		}
		assert.Equal(t, 2, holders, w.key)
	}
	assert.Equal(t, first.id, first.entry(t, "oldv", "v").Locations[0], "adopted by the first of its holders")
	assert.Contains(t, first.entry(t, "old", "lone").Locations, third.id)
}

// With a factor of 1 a write is held by the node that takes it alone. Every
// node holds every bucket and the entry of the write, lists it and serves it;
// a delete made on any node removes it everywhere.
func TestAFactorOfOneKeepsOneCopyWhereTheWriteIsMade(t *testing.T) {
	c := newHACluster(t, 3, 1)
	a, b, far := c.nodes[0], c.nodes[1], c.nodes[2]
	ctx := context.Background()
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "", "single", routingAdminID))
	for _, n := range c.nodes {
		require.True(t, hasBucket(n.Server, "", "single"))
	}

	_, err := b.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("single"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	for i, n := range c.nodes {
		assert.Equal(t, n == b, n.holds(t, "single", "k", ""), "node %d", i)
		assert.Equal(t, []string{b.id}, n.entry(t, "single", "k").Locations, "node %d", i)
		assert.Equal(t, "data", getBody(t, n.s3, "single", "k"), "node %d", i)
		listed, err := n.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String("single")})
		require.NoError(t, err)
		require.Len(t, listed.Contents, 1, "node %d", i)
		assert.Equal(t, "k", *listed.Contents[0].Key)
		assert.Zero(t, n.copies.Load(), "node %d is sent no data", i)
	}

	_, err = far.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String("single"), Key: aws.String("k")})
	require.NoError(t, err)
	for i, n := range c.nodes {
		_, err := n.metadataStore.GetObject(ctx, "single", "k")
		assert.ErrorIs(t, err, metadata.ErrObjectNotFound, "node %d", i)
		assert.False(t, n.holds(t, "single", "k", ""), "node %d", i)
	}
}

// With a factor of 1 a node under storage pressure passes the write it takes
// to the node with the most free space; a slow node keeps it.
func TestAFactorOfOneMovesAWriteOffANodeUnderStoragePressure(t *testing.T) {
	c := newHACluster(t, 3, 1)
	a, b, far := c.nodes[0], c.nodes[1], c.nodes[2]
	ctx := context.Background()
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "", "full", routingAdminID))
	a.setFree(t, b, 100)
	a.setFree(t, far, 900)

	a.underPressure(t)
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("full"), Key: aws.String("moved"), Body: strings.NewReader("moved")})
	require.NoError(t, err)
	for i, n := range c.nodes {
		assert.Equal(t, n == far, n.holds(t, "full", "moved", ""), "node %d", i)
		assert.Equal(t, []string{far.id}, n.entry(t, "full", "moved").Locations, "node %d", i)
		assert.Equal(t, "moved", getBody(t, n.s3, "full", "moved"), "node %d", i)
	}

	_, err = a.db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusDegraded, a.id)
	require.NoError(t, err)
	_, err = a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("full"), Key: aws.String("kept"), Body: strings.NewReader("kept")})
	require.NoError(t, err)
	assert.True(t, a.holds(t, "full", "kept", ""))
	assert.Equal(t, []string{a.id}, b.entry(t, "full", "kept").Locations)
	assert.Equal(t, "kept", getBody(t, b.s3, "full", "kept"))
}

// A node of an earlier version places its writes before it sends anything:
// its first sync and its catch-up of another node send the entries without
// the data, and the other node reads the data from it.
func TestAnEarlierNodesSyncAndCatchUpSendNoData(t *testing.T) {
	for _, way := range []string{"sync", "catch-up"} {
		t.Run(way, func(t *testing.T) {
			c := newHACluster(t, 2, 1)
			a, b := c.nodes[0], c.nodes[1]
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			require.NoError(t, innerBuckets(a).CreateBucket(ctx, "", "old", routingAdminID))
			_, err := a.objectManager.(*cluster.HAObjectManager).Manager.PutObject(ctx, "old", "k", strings.NewReader("old"), http.Header{})
			require.NoError(t, err)

			if way == "sync" {
				a.haSyncWorker.Trigger(ctx)
			} else {
				a.antiEntropyScrubber.Start(ctx)
				a.antiEntropyScrubber.CatchUp(b.id, time.Unix(1, 0))
			}
			require.Eventually(t, func() bool {
				e, err := b.metadataStore.GetObject(ctx, "old", "k")
				return err == nil && slices.Equal(e.Locations, []string{a.id})
			}, 15*time.Second, 50*time.Millisecond)
			assert.False(t, b.holds(t, "old", "k", ""))
			assert.Zero(t, b.copies.Load(), "no data is sent")
			assert.Equal(t, "old", getBody(t, b.s3, "old", "k"))
		})
	}
}

// innerBuckets is the bucket manager of n below the HA layer: its changes
// reach no other node.
func innerBuckets(n *haNode) bucket.Manager {
	return n.bucketManager.(*clientBuckets).Manager.(*cluster.HABucketManager).Manager
}

// A cluster of factor 1 from a version that kept each bucket on one node: on
// start every node gives the writes it holds itself as their location, then
// sends the other nodes its buckets and entries without their data. Every
// node then lists and serves every write. A write whose data is not on the
// node is not given it. A node does this once.
func TestAClusterOfFactorOneFromAnEarlierVersionPlacesItsWrites(t *testing.T) {
	c := newHACluster(t, 3, 1)
	a := c.nodes[0]
	ctx := context.Background()
	type write struct {
		owner                            *haNode
		bucket, key, versionID, contents string
	}
	earlier := func(n *haNode, bucketName, key, contents string) string {
		t.Helper()
		obj, err := n.objectManager.(*cluster.HAObjectManager).Manager.PutObject(ctx, bucketName, key, strings.NewReader(contents), http.Header{})
		require.NoError(t, err)
		return obj.VersionID
	}
	var writes []write
	for i, n := range c.nodes {
		name := fmt.Sprintf("own-%d", i)
		require.NoError(t, innerBuckets(n).CreateBucket(ctx, "", name, routingAdminID))
		writes = append(writes, write{n, name, "k", earlier(n, name, "k", name), name})
	}
	inner := innerBuckets(a)
	require.NoError(t, inner.CreateBucket(ctx, "", "own-versions", routingAdminID))
	require.NoError(t, inner.SetVersioning(ctx, "", "own-versions", &bucket.VersioningConfig{Status: "Enabled"}))
	for _, contents := range []string{"one", "two"} {
		writes = append(writes, write{a, "own-versions", "v", earlier(a, "own-versions", "v", contents), contents})
	}
	writer, ok := cluster.ReplicaWriter(a.objectManager)
	require.True(t, ok)
	const lostVersion = "1000000000000000000.lost"
	require.NoError(t, writer.PutReplicaMetadata(ctx, &metadata.ObjectMetadata{Bucket: "own-versions", Key: "lost", VersionID: lostVersion,
		Size: 4, ETag: "0123456789abcdef0123456789abcdef", LastModified: time.Now()}))
	require.False(t, hasBucket(c.nodes[1].Server, "", "own-0"), "each bucket starts on its node alone")

	for _, n := range c.nodes {
		n.haSyncWorker.Start(ctx)
	}
	for _, wr := range writes {
		require.Eventually(t, func() bool {
			for _, n := range c.nodes {
				e, err := n.metadataStore.GetObject(ctx, wr.bucket, wr.key, wr.versionID)
				if err != nil || !slices.Equal(e.Locations, []string{wr.owner.id}) {
					return false
				}
			}
			return true
		}, 15*time.Second, 50*time.Millisecond, "%s/%s", wr.bucket, wr.key)
		for i, n := range c.nodes {
			assert.Equal(t, n == wr.owner, n.holds(t, wr.bucket, wr.key, wr.versionID), "%s/%s on node %d", wr.bucket, wr.key, i)
			assert.Equal(t, wr.contents, getBody(t, n.s3, wr.bucket, wr.key, func(in *s3.GetObjectInput) {
				if wr.versionID != "" {
					in.VersionId = aws.String(wr.versionID)
				}
			}), "%s/%s on node %d", wr.bucket, wr.key, i)
		}
	}
	for i, n := range c.nodes {
		assert.Zero(t, n.copies.Load(), "node %d is sent no data", i)
	}
	lostEntry, err := a.metadataStore.GetObject(ctx, "own-versions", "lost", lostVersion)
	require.NoError(t, err)
	assert.Empty(t, lostEntry.Locations, "a write whose data is not here is not given this node")

	earlier(a, "own-0", "later", "later")
	restarted := cluster.NewHASyncWorker(a.objectManager, a.bucketManager, a.clusterManager, a.metadataStore)
	require.NoError(t, restarted.PlaceWrites(ctx))
	assert.Empty(t, a.entry(t, "own-0", "later").Locations, "the writes are placed once")
}

// With a factor above 1, a node under storage pressure is given no new copy,
// even with the most free space, but is sent the entry of every write, its
// changes and its deletes.
func TestANodeUnderStoragePressureTakesEntriesButNoNewData(t *testing.T) {
	_, a, b, far := placedCluster(t, "placed")
	ctx := context.Background()
	a.seesUnderPressure(t, far)

	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	assert.False(t, far.holds(t, "placed", "k", ""))
	assert.True(t, b.holds(t, "placed", "k", ""))
	assert.Equal(t, []string{a.id, b.id}, far.entry(t, "placed", "k").Locations)

	_, err = a.s3.PutObjectTagging(ctx, &s3.PutObjectTaggingInput{Bucket: aws.String("placed"), Key: aws.String("k"),
		Tagging: &types.Tagging{TagSet: []types.Tag{{Key: aws.String("team"), Value: aws.String("blue")}}}})
	require.NoError(t, err)
	tags, err := far.objectManager.GetObjectTagging(ctx, "placed", "k")
	require.NoError(t, err)
	require.Len(t, tags.Tags, 1)
	assert.Equal(t, "blue", tags.Tags[0].Value)
	_, err = a.s3.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{Bucket: aws.String("placed"),
		Tagging: &types.Tagging{TagSet: []types.Tag{{Key: aws.String("env"), Value: aws.String("prod")}}}})
	require.NoError(t, err)
	placed, err := far.metadataStore.GetBucket(ctx, "", "placed")
	require.NoError(t, err)
	assert.Equal(t, "prod", placed.Tags["env"])

	_, err = a.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String("placed"), Key: aws.String("k")})
	require.NoError(t, err)
	_, err = far.metadataStore.GetObject(ctx, "placed", "k")
	assert.ErrorIs(t, err, metadata.ErrObjectNotFound)
}

// A node under storage pressure does not pass its copy to another node under
// pressure, and a repair makes no copy on one.
func TestNoNewCopyGoesToANodeUnderStoragePressure(t *testing.T) {
	_, a, b, far := placedCluster(t, "full")
	ctx := context.Background()
	a.underPressure(t)
	a.seesUnderPressure(t, b)
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("full"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	assert.True(t, a.holds(t, "full", "k", ""))
	assert.Equal(t, []string{a.id, far.id}, a.entry(t, "full", "k").Locations)

	require.NoError(t, a.clusterManager.RemoveNode(ctx, far.id))
	require.Eventually(t, func() bool {
		return slices.Equal([]string{a.id}, a.entry(t, "full", "k").Locations)
	}, 15*time.Second, 50*time.Millisecond)
	assert.Zero(t, b.copies.Load(), "b is under storage pressure")
}

// A node back under storage pressure is caught up: it is sent every bucket and
// the writes it missed.
func TestANodeBackUnderStoragePressureIsCaughtUp(t *testing.T) {
	_, a, _, far := placedCluster(t, "placed")
	ctx := context.Background()
	far.down.Store(true)
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("missed"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	far.down.Store(false)
	a.seesUnderPressure(t, far)
	require.NoError(t, a.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: "quiet", OwnerID: routingAdminID}))

	startScrubber(t, a.Server)
	a.antiEntropyScrubber.CatchUp(far.id, time.Unix(1, 0))
	require.Eventually(t, func() bool {
		_, err := far.metadataStore.GetObject(ctx, "placed", "missed")
		return err == nil && hasBucket(far.Server, "", "quiet")
	}, 15*time.Second, 50*time.Millisecond)
}

// A node that leaves the cluster drops the entries of the writes the other
// nodes hold, a version as well as an object, and keeps and serves those it
// holds.
func TestANodeThatLeavesKeepsOnlyTheWritesItHolds(t *testing.T) {
	_, a, b, far := placedCluster(t, "placed")
	ctx := context.Background()
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "", "placedv", routingAdminID))
	require.NoError(t, a.bucketManager.SetVersioning(ctx, "", "placedv", &bucket.VersioningConfig{Status: "Enabled"}))
	b.setFree(t, a, 900)
	b.setFree(t, far, 100)
	put := func(n *haNode, bucket, key, contents string) *s3.PutObjectOutput {
		t.Helper()
		out, err := n.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: strings.NewReader(contents)})
		require.NoError(t, err)
		return out
	}
	put(a, "placed", "kept", "kept")
	put(b, "placed", "gone", "gone")
	first := put(a, "placedv", "v", "one")
	put(b, "placedv", "v", "two")
	require.True(t, far.holds(t, "placed", "kept", ""))
	require.False(t, far.holds(t, "placed", "gone", ""))
	_, err := far.objectManager.(*cluster.HAObjectManager).Manager.PutObject(ctx, "placed", "legacy", strings.NewReader("legacy"), http.Header{})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	far.handleLeaveCluster(w, asGlobalAdmin(httptest.NewRequest(http.MethodPost, "/api/v1/cluster/leave", nil), nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Eventually(t, func() bool {
		_, err := far.metadataStore.GetObject(ctx, "placed", "gone")
		versions, verr := far.metadataStore.GetObjectVersions(ctx, "placedv", "v")
		return errors.Is(err, metadata.ErrObjectNotFound) && verr == nil && len(versions) == 1
	}, 15*time.Second, 50*time.Millisecond)

	assert.Equal(t, "kept", getBody(t, far.s3, "placed", "kept"))
	assert.Equal(t, "legacy", getBody(t, far.s3, "placed", "legacy"), "an entry naming no node is held where its file is")
	versions, err := far.metadataStore.GetObjectVersions(ctx, "placedv", "v")
	require.NoError(t, err)
	assert.Equal(t, *first.VersionId, versions[0].VersionID)
	assert.Equal(t, "one", getBody(t, far.s3, "placedv", "v"))
	placed, err := far.metadataStore.GetBucket(ctx, "", "placed")
	require.NoError(t, err)
	assert.EqualValues(t, 2, placed.ObjectCount)
	assert.EqualValues(t, 10, placed.TotalSize)
}

// A dead node that answers again is compared on every write: the locations
// changed while it was dead reach it, and the copy it held, made again on
// another node, is removed from it. A version as well as an object.
func TestADeadNodeThatComesBackTakesTheLocationsChangedMeanwhile(t *testing.T) {
	_, a, b, far := placedCluster(t, "placed")
	ctx := context.Background()
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "", "placedv", routingAdminID))
	require.NoError(t, a.bucketManager.SetVersioning(ctx, "", "placedv", &bucket.VersioningConfig{Status: "Enabled"}))
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placed"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	version, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("placedv"), Key: aws.String("v"), Body: strings.NewReader("version")})
	require.NoError(t, err)
	require.True(t, far.holds(t, "placed", "k", ""))
	require.True(t, far.holds(t, "placedv", "v", *version.VersionId))

	setStatus := func(status string) {
		t.Helper()
		for _, n := range []*haNode{a, b} {
			_, err := n.db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, status, far.id)
			require.NoError(t, err)
		}
	}
	setStatus(cluster.HealthStatusDead)
	require.NoError(t, a.haSyncWorker.RepairPlacement(ctx))
	require.Equal(t, []string{a.id, b.id}, a.entry(t, "placed", "k").Locations)
	require.Equal(t, []string{a.id, far.id}, far.entry(t, "placed", "k").Locations, "a dead node is told nothing")

	setStatus(cluster.HealthStatusHealthy)
	startScrubber(t, a.Server)
	runs := a.scrubRuns(t)
	a.antiEntropyScrubber.CatchUp(far.id, time.Unix(1, 0))
	require.Eventually(t, func() bool {
		v, err := far.metadataStore.GetObject(ctx, "placedv", "v", *version.VersionId)
		return err == nil && slices.Equal([]string{a.id, b.id}, v.Locations) &&
			slices.Equal([]string{a.id, b.id}, far.entry(t, "placed", "k").Locations)
	}, 15*time.Second, 50*time.Millisecond)
	assert.False(t, far.holds(t, "placed", "k", ""))
	assert.False(t, far.holds(t, "placedv", "v", *version.VersionId))
	assert.Equal(t, "data", getBody(t, far.s3, "placed", "k"))

	// Once the locations agree, a full cycle sends no entry.
	require.Eventually(t, func() bool { return a.scrubRuns(t) > runs }, 15*time.Second, 50*time.Millisecond)
	var entries atomic.Int32
	count := func() { entries.Add(1) }
	for _, n := range []*haNode{b, far} {
		n.onEntry.Store(&count)
	}
	runs = a.scrubRuns(t)
	a.antiEntropyScrubber.CatchUp(far.id, time.Unix(1, 0))
	require.Eventually(t, func() bool { return a.scrubRuns(t) > runs }, 15*time.Second, 50*time.Millisecond)
	assert.Zero(t, entries.Load(), "the locations agree")
}
