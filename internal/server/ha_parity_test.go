package server

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/erasure"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newParityCluster is n nodes with parity and a bucket created on the first.
func newParityCluster(t *testing.T, n int, bucketName string) *haCluster {
	t.Helper()
	c := newHACluster(t, n, 1)
	ctx := context.Background()
	for _, node := range c.nodes {
		require.NoError(t, node.clusterManager.SetParity(ctx, true))
	}
	require.NoError(t, c.nodes[0].bucketManager.CreateBucket(ctx, "", bucketName, routingAdminID))
	return c
}

// shardsHeld are the indexes of the shards n stores of a key's version.
func (n *haNode) shardsHeld(t *testing.T, bucketName, key, versionID string) []int {
	t.Helper()
	indexes, err := n.storageBackend.Shards(context.Background(), storage.ObjectRef{Bucket: bucketName, Key: key, VersionID: versionID})
	require.NoError(t, err)
	return indexes
}

func payload(n int) []byte {
	data := make([]byte, n)
	rand.New(rand.NewSource(int64(n))).Read(data)
	return data
}

func getBytes(t *testing.T, client *s3.Client, bucketName, key string, opts ...func(*s3.GetObjectInput)) []byte {
	t.Helper()
	return []byte(getBody(t, client, bucketName, key, opts...))
}

// assertSharded checks that every node holds the entry of a write cut by
// layout and that each node it names holds its shard, no whole copy anywhere.
func assertSharded(t *testing.T, c *haCluster, bucketName, key, versionID string, layout erasure.Layout) []string {
	t.Helper()
	var locations []string
	for _, n := range c.nodes {
		var e *metadata.ObjectMetadata
		var err error
		if versionID != "" {
			e, err = n.metadataStore.GetObject(context.Background(), bucketName, key, versionID)
		} else {
			e, err = n.metadataStore.GetObject(context.Background(), bucketName, key)
		}
		require.NoError(t, err)
		assert.Equal(t, [3]int{layout.Data, layout.Parity, layout.Block}, [3]int{e.ECData, e.ECParity, e.ECBlock}, "node %s", n.id)
		if locations == nil {
			locations = e.Locations
		}
		assert.Equal(t, locations, e.Locations, "every node names the same shard holders")
		assert.False(t, n.holds(t, bucketName, key, versionID), "no whole copy on %s", n.id)
	}
	require.Len(t, locations, layout.Width())
	assert.Len(t, slices.Compact(slices.Sorted(slices.Values(locations))), layout.Width(), "each shard on its own node")
	for i, id := range locations {
		assert.Equal(t, []int{i}, c.byID(id).shardsHeld(t, bucketName, key, versionID), "node %s holds shard %d", id, i)
	}
	for _, n := range c.nodes {
		if !slices.Contains(locations, n.id) {
			assert.Empty(t, n.shardsHeld(t, bucketName, key, versionID))
		}
	}
	return locations
}

// A write with parity on three nodes is cut in two data shards and one of
// parity, one on each node; every node lists it and serves it, a range alone.
func TestAWriteWithParityIsCutOverTheNodes(t *testing.T) {
	c := newParityCluster(t, 3, "parity")
	a := c.nodes[0]
	ctx := context.Background()
	data := payload(3*erasure.DefaultBlock + 1234)
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("parity"), Key: aws.String("k"), Body: bytes.NewReader(data)})
	require.NoError(t, err)

	layout := erasure.Layout{Data: 2, Parity: 1, Block: erasure.DefaultBlock}
	assertSharded(t, c, "parity", "k", "", layout)
	for _, n := range c.nodes {
		assert.True(t, bytes.Equal(data, getBytes(t, n.s3, "parity", "k")), "read on %s", n.id)
		head, err := n.s3.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String("parity"), Key: aws.String("k")})
		require.NoError(t, err)
		assert.EqualValues(t, len(data), *head.ContentLength)
		listed, err := n.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String("parity")})
		require.NoError(t, err)
		assert.Len(t, listed.Contents, 1)
	}
	part := getBytes(t, c.nodes[2].s3, "parity", "k", func(in *s3.GetObjectInput) { in.Range = aws.String("bytes=600000-600099") })
	assert.Equal(t, data[600000:600100], part)
	tail := getBytes(t, c.nodes[1].s3, "parity", "k", func(in *s3.GetObjectInput) { in.Range = aws.String("bytes=-10") })
	assert.Equal(t, data[len(data)-10:], tail)
}

// A write with parity is read with any one node down, and not with two.
func TestAWriteWithParityIsReadWithANodeDown(t *testing.T) {
	c := newParityCluster(t, 3, "parity")
	ctx := context.Background()
	data := payload(700000)
	_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("parity"), Key: aws.String("k"), Body: bytes.NewReader(data)})
	require.NoError(t, err)
	locations := assertSharded(t, c, "parity", "k", "", erasure.Layout{Data: 2, Parity: 1, Block: erasure.DefaultBlock})

	for _, down := range locations {
		c.byID(down).down.Store(true)
		for _, n := range c.except(c.byID(down)) {
			assert.True(t, bytes.Equal(data, getBytes(t, n.s3, "parity", "k")), "read on %s with %s down", n.id, down)
		}
		c.byID(down).down.Store(false)
	}

	reader := c.byID(locations[2])
	c.byID(locations[0]).down.Store(true)
	c.byID(locations[1]).down.Store(true)
	_, err = reader.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("parity"), Key: aws.String("k")})
	assert.Equal(t, http.StatusServiceUnavailable, s3Status(err))
	_, err = reader.s3.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String("parity"), Key: aws.String("k")})
	assert.NoError(t, err, "the entry still answers")
}

// From five nodes a write with parity has two parity shards and is read with
// any two nodes down.
func TestAWriteWithParityOnFiveNodesSurvivesTwo(t *testing.T) {
	c := newParityCluster(t, 5, "parity")
	ctx := context.Background()
	data := payload(900000)
	_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("parity"), Key: aws.String("k"), Body: bytes.NewReader(data)})
	require.NoError(t, err)
	locations := assertSharded(t, c, "parity", "k", "", erasure.Layout{Data: 3, Parity: 2, Block: erasure.DefaultBlock})

	c.byID(locations[0]).down.Store(true)
	c.byID(locations[2]).down.Store(true)
	assert.True(t, bytes.Equal(data, getBytes(t, c.byID(locations[4]).s3, "parity", "k")))
}

// A node that does not take its shard leaves the write in the other shards
// when they are one more than the data shards; with fewer, the write is kept
// in whole copies and the shards taken go.
func TestAShardNotTakenDuringAWriteWithParity(t *testing.T) {
	c := newParityCluster(t, 5, "parity")
	ctx := context.Background()
	refusing := c.nodes[3]
	refusing.refuseShards.Store(true)
	data := payload(300000)
	_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("parity"), Key: aws.String("four"), Body: bytes.NewReader(data)})
	require.NoError(t, err)
	e := c.nodes[1].entry(t, "parity", "four")
	assert.Equal(t, 3, e.ECData)
	assert.Contains(t, e.Locations, refusing.id, "its shard is made again later")
	assert.Empty(t, refusing.shardsHeld(t, "parity", "four", ""))
	for _, n := range c.nodes {
		assert.True(t, bytes.Equal(data, getBytes(t, n.s3, "parity", "four")))
	}

	three := newParityCluster(t, 3, "parity")
	three.nodes[2].refuseShards.Store(true)
	_, err = three.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("parity"), Key: aws.String("copies"), Body: bytes.NewReader(data)})
	require.NoError(t, err)
	for _, n := range three.nodes {
		e := n.entry(t, "parity", "copies")
		assert.Zero(t, e.ECData, "whole copies on %s", n.id)
		assert.Len(t, e.Locations, 2)
		assert.Empty(t, n.shardsHeld(t, "parity", "copies", ""), "no shard stays on %s", n.id)
		assert.True(t, bytes.Equal(data, getBytes(t, n.s3, "parity", "copies")))
	}
}

// With a node down a write with parity on three nodes is kept in two whole
// copies; with two down it is refused.
func TestAWriteWithParityWithANodeDown(t *testing.T) {
	c := newParityCluster(t, 3, "parity")
	a, b, far := c.nodes[0], c.nodes[1], c.nodes[2]
	ctx := context.Background()
	_, err := a.db.ExecContext(ctx, `UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusUnavailable, far.id)
	require.NoError(t, err)
	far.down.Store(true)
	_, err = a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("parity"), Key: aws.String("k"), Body: strings.NewReader("written while a node is down")})
	require.NoError(t, err)
	for _, n := range []*haNode{a, b} {
		e := n.entry(t, "parity", "k")
		assert.Zero(t, e.ECData)
		assert.ElementsMatch(t, []string{a.id, b.id}, e.Locations)
		assert.True(t, n.holds(t, "parity", "k", ""))
	}

	_, err = a.db.ExecContext(ctx, `UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusUnavailable, b.id)
	require.NoError(t, err)
	_, err = a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("parity"), Key: aws.String("refused"), Body: strings.NewReader("x")})
	assert.Equal(t, http.StatusServiceUnavailable, s3Status(err))
}

// An empty object with parity is kept in as many whole copies as the shards'
// parity plus one.
func TestAnEmptyObjectWithParityIsKeptInCopies(t *testing.T) {
	c := newParityCluster(t, 3, "parity")
	ctx := context.Background()
	_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("parity"), Key: aws.String("empty"), Body: strings.NewReader("")})
	require.NoError(t, err)
	e := c.nodes[1].entry(t, "parity", "empty")
	assert.Zero(t, e.ECData)
	assert.Len(t, e.Locations, 2)
	for _, n := range c.nodes {
		assert.Empty(t, getBody(t, n.s3, "parity", "empty"))
	}
}

// A multipart upload with parity is cut in shards once completed; the
// versions of a key are each cut, and each is read from any node; a delete
// removes the shards from every node.
func TestMultipartVersionsAndDeletesWithParity(t *testing.T) {
	c := newParityCluster(t, 3, "parity")
	a, b := c.nodes[0], c.nodes[1]
	ctx := context.Background()
	layout := erasure.Layout{Data: 2, Parity: 1, Block: erasure.DefaultBlock}

	first := payload(5 << 20)
	upload, err := b.s3.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String("parity"), Key: aws.String("mp")})
	require.NoError(t, err)
	p1, err := a.s3.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String("parity"), Key: aws.String("mp"), UploadId: upload.UploadId,
		PartNumber: aws.Int32(1), Body: bytes.NewReader(first)})
	require.NoError(t, err)
	p2, err := b.s3.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String("parity"), Key: aws.String("mp"), UploadId: upload.UploadId,
		PartNumber: aws.Int32(2), Body: strings.NewReader("tail")})
	require.NoError(t, err)
	_, err = a.s3.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: aws.String("parity"), Key: aws.String("mp"),
		UploadId: upload.UploadId, MultipartUpload: &types.CompletedMultipartUpload{Parts: []types.CompletedPart{
			{ETag: p1.ETag, PartNumber: aws.Int32(1)}, {ETag: p2.ETag, PartNumber: aws.Int32(2)}}}})
	require.NoError(t, err)
	assertSharded(t, c, "parity", "mp", "", layout)
	for _, n := range c.nodes {
		assert.True(t, bytes.Equal(append(slices.Clone(first), "tail"...), getBytes(t, n.s3, "parity", "mp")))
	}

	require.NoError(t, a.bucketManager.SetVersioning(ctx, "", "parity", &bucket.VersioningConfig{Status: "Enabled"}))
	one, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("parity"), Key: aws.String("v"), Body: strings.NewReader("version one")})
	require.NoError(t, err)
	two, err := b.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("parity"), Key: aws.String("v"), Body: strings.NewReader("version two")})
	require.NoError(t, err)
	assertSharded(t, c, "parity", "v", *one.VersionId, layout)
	assertSharded(t, c, "parity", "v", *two.VersionId, layout)
	for _, n := range c.nodes {
		assert.Equal(t, "version one", getBody(t, n.s3, "parity", "v", func(in *s3.GetObjectInput) { in.VersionId = one.VersionId }))
		assert.Equal(t, "version two", getBody(t, n.s3, "parity", "v"))
	}

	_, err = b.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String("parity"), Key: aws.String("v"), VersionId: one.VersionId})
	require.NoError(t, err)
	_, err = a.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String("parity"), Key: aws.String("mp")})
	require.NoError(t, err)
	for _, n := range c.nodes {
		assert.Empty(t, n.shardsHeld(t, "parity", "v", *one.VersionId), "on %s", n.id)
		assert.NotEmpty(t, n.shardsHeld(t, "parity", "v", *two.VersionId), "on %s", n.id)
	}
	// The key without a version was deleted before versioning had a marker
	// for it: a marker hides it, its shards stay with its null version.
	_, err = a.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("parity"), Key: aws.String("mp")})
	assert.Equal(t, http.StatusNotFound, s3Status(err))
}

// An overwrite of a key without a version moves its shards to the nodes of
// the later write and removes those of the earlier one.
func TestAnOverwriteWithParityReplacesTheShards(t *testing.T) {
	c := newParityCluster(t, 4, "parity")
	ctx := context.Background()
	_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("parity"), Key: aws.String("k"), Body: bytes.NewReader(payload(100000))})
	require.NoError(t, err)
	later := payload(200000)
	_, err = c.nodes[3].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("parity"), Key: aws.String("k"), Body: bytes.NewReader(later)})
	require.NoError(t, err)
	assertSharded(t, c, "parity", "k", "", erasure.Layout{Data: 3, Parity: 1, Block: erasure.DefaultBlock})
	for _, n := range c.nodes {
		got := getBytes(t, n.s3, "parity", "k")
		assert.True(t, bytes.Equal(later, got))
	}
}

// A write with parity that no other node takes, shards nor copies, is refused
// and not kept on the node that took it.
func TestAWriteWithParityNoOtherNodeTakesIsRefused(t *testing.T) {
	c := newParityCluster(t, 3, "parity")
	a := c.nodes[0]
	ctx := context.Background()
	for _, n := range c.except(a) {
		n.down.Store(true)
	}
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("parity"), Key: aws.String("k"), Body: strings.NewReader("data")})
	assert.Equal(t, http.StatusServiceUnavailable, s3Status(err))

	_, err = a.db.ExecContext(ctx, `UPDATE cluster_nodes SET health_status = ? WHERE id != ?`, cluster.HealthStatusUnavailable, a.id)
	require.NoError(t, err)
	_, err = a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("parity"), Key: aws.String("never"), Body: strings.NewReader("data")})
	assert.Equal(t, http.StatusServiceUnavailable, s3Status(err))
	_, err = a.metadataStore.GetObject(ctx, "parity", "never")
	assert.ErrorIs(t, err, metadata.ErrObjectNotFound, "refused before it is written")
}

// From five nodes a delete with parity needs another node to confirm it.
func TestADeleteWithParityOnFiveNodesNeedsAnotherNode(t *testing.T) {
	c := newParityCluster(t, 5, "parity")
	a := c.nodes[0]
	ctx := context.Background()
	_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("parity"), Key: aws.String("k"), Body: strings.NewReader("data")})
	require.NoError(t, err)
	for _, n := range c.except(a) {
		n.down.Store(true)
	}
	_, err = a.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String("parity"), Key: aws.String("k")})
	assert.Equal(t, http.StatusServiceUnavailable, s3Status(err))
}

// The console turns parity on from three healthy nodes and shows the layout
// of a write, the room it leaves and the nodes it survives; a factor turns it
// off.
func TestTheConsoleTurnsParityOn(t *testing.T) {
	two := newHACluster(t, 2, 2)
	set := func(n *haNode, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		n.handleSetClusterHA(w, asGlobalAdmin(httptest.NewRequest(http.MethodPut, "/api/v1/cluster/ha", strings.NewReader(body)), nil))
		return w
	}
	w := set(two.nodes[0], `{"parity":true}`)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	c := newHACluster(t, 3, 2)
	a := c.nodes[0]
	ctx := context.Background()
	for _, n := range c.nodes {
		a.setFree(t, n, 1000)
	}
	w = set(a, `{"parity":true}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	p, err := a.clusterManager.GetProtection(ctx)
	require.NoError(t, err)
	assert.True(t, p.Parity)

	get := func() map[string]any {
		w := httptest.NewRecorder()
		a.handleGetClusterHA(w, asGlobalAdmin(httptest.NewRequest(http.MethodGet, "/api/v1/cluster/ha", nil), nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var out struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out.Data
	}
	out := get()
	assert.Equal(t, true, out["parity"])
	assert.EqualValues(t, 2, out["data_shards"])
	assert.EqualValues(t, 1, out["parity_shards"])
	assert.EqualValues(t, 1, out["tolerated_failures"])
	total := out["total_bytes"].(float64)
	assert.InDelta(t, total*2/3, out["usable_bytes"].(float64), 1)

	// The status read the nodes' disks: they are made to have room again.
	for _, n := range c.nodes {
		a.setFree(t, n, 1000)
	}
	w = set(a, `{"factor":3}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	p, err = a.clusterManager.GetProtection(ctx)
	require.NoError(t, err)
	assert.Equal(t, cluster.Protection{Factor: 3}, p)
	out = get()
	assert.Equal(t, false, out["parity"])
	assert.EqualValues(t, 2, out["tolerated_failures"])
}

// With parity every node reports to Veeam the room of the cluster for data
// cut in shards, each shard on its own node.
func TestEveryNodeReportsTheRoomOfTheClusterWithParity(t *testing.T) {
	c := newParityCluster(t, 3, "veeam")
	for _, n := range c.nodes {
		for i, peer := range c.nodes {
			n.setFree(t, peer, []int64{800, 600, 100}[i])
		}
	}
	for _, n := range c.nodes {
		capacity, available := n.sosapiCapacity(t, "veeam")
		assert.EqualValues(t, 2000, capacity)
		assert.EqualValues(t, 200, available)
	}
}
