package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/erasure"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keepClass makes every node of c keep class.
func (c *haCluster) keepClass(t *testing.T, class cluster.StorageClass) {
	t.Helper()
	for _, n := range c.nodes {
		require.NoError(t, n.clusterManager.SetStorageClass(context.Background(), class))
	}
}

// putInClass puts node in class on every node of c.
func (c *haCluster) putInClass(t *testing.T, node *haNode, class string) {
	t.Helper()
	for _, n := range c.nodes {
		require.NoError(t, n.clusterManager.SetNodeStorageClass(context.Background(), node.id, class))
	}
}

// heldBy are the IDs of nodes, sorted.
func heldBy(nodes ...*haNode) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.id
	}
	slices.Sort(ids)
	return ids
}

// holdersOf are the nodes every node of c names for a key's write, sorted;
// they must all name the same.
func (c *haCluster) holdersOf(t *testing.T, bucketName, key string) []string {
	t.Helper()
	var holders []string
	for i, n := range c.nodes {
		e := n.entry(t, bucketName, key)
		named := slices.Sorted(slices.Values(e.Locations))
		if i == 0 {
			holders = named
		}
		assert.Equal(t, holders, named, "node %s", n.id)
	}
	return holders
}

// eventuallyHeldBy waits until every node names holders for a key's write and
// only they hold its data.
func (c *haCluster) eventuallyHeldBy(t *testing.T, bucketName, key string, holders ...*haNode) {
	t.Helper()
	want := heldBy(holders...)
	require.Eventually(t, func() bool {
		for _, n := range c.nodes {
			e, err := n.metadataStore.GetObject(context.Background(), bucketName, key)
			if err != nil || !slices.Equal(want, slices.Sorted(slices.Values(e.Locations))) {
				return false
			}
			if n.holds(t, bucketName, key, "") != slices.Contains(want, n.id) {
				return false
			}
		}
		return true
	}, 20*time.Second, 100*time.Millisecond)
}

func putClassed(t *testing.T, n *haNode, bucketName, key, class string, data []byte) {
	t.Helper()
	in := &s3.PutObjectInput{Bucket: aws.String(bucketName), Key: aws.String(key), Body: bytes.NewReader(data)}
	if class != "" {
		in.StorageClass = types.StorageClass(class)
	}
	_, err := n.s3.PutObject(context.Background(), in)
	require.NoError(t, err)
}

func classOnHead(t *testing.T, n *haNode, bucketName, key string) string {
	t.Helper()
	out, err := n.s3.HeadObject(context.Background(), &s3.HeadObjectInput{Bucket: aws.String(bucketName), Key: aws.String(key)})
	require.NoError(t, err)
	return string(out.StorageClass)
}

// glacierCluster is four nodes keeping STANDARD in two copies on the first
// two and GLACIER in two copies on the others, with a bucket. Every node sees
// those of STANDARD with more free space.
func glacierCluster(t *testing.T, bucketName string) *haCluster {
	t.Helper()
	c := newHACluster(t, 4, 2)
	c.keepClass(t, cluster.StorageClass{Name: "GLACIER", Factor: 2})
	c.putInClass(t, c.nodes[2], "GLACIER")
	c.putInClass(t, c.nodes[3], "GLACIER")
	for _, n := range c.nodes {
		for i, peer := range c.nodes {
			n.setFree(t, peer, int64(900-200*(i/2)))
		}
	}
	require.NoError(t, c.nodes[0].bucketManager.CreateBucket(context.Background(), "", bucketName, routingAdminID))
	return c
}

// The data of a write of a class is held by the nodes of the class, whichever
// node takes it, by a PUT, a multipart upload or a copy; every node serves it
// and tells its class. A write of no class, or of a class the cluster does
// not keep, is held by the nodes of STANDARD.
func TestTheWritesOfAClassAreHeldByItsNodes(t *testing.T) {
	c := glacierCluster(t, "classes")
	n0, n1, n2, n3 := c.nodes[0], c.nodes[1], c.nodes[2], c.nodes[3]
	ctx := context.Background()

	putClassed(t, n0, "classes", "cold", "GLACIER", []byte("cold data"))
	c.eventuallyHeldBy(t, "classes", "cold", n2, n3)
	for _, n := range c.nodes {
		assert.Equal(t, "cold data", getBody(t, n.s3, "classes", "cold"), "read on %s", n.id)
		assert.Equal(t, "GLACIER", classOnHead(t, n, "classes", "cold"))
	}

	putClassed(t, n2, "classes", "warm", "", []byte("warm data"))
	c.eventuallyHeldBy(t, "classes", "warm", n0, n1)
	putClassed(t, n3, "classes", "ia", "STANDARD_IA", []byte("ia data"))
	c.eventuallyHeldBy(t, "classes", "ia", n0, n1)
	assert.Equal(t, "STANDARD_IA", classOnHead(t, n2, "classes", "ia"), "the class is kept as the write's")

	upload, err := n1.s3.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{Bucket: aws.String("classes"), Key: aws.String("mp"),
		StorageClass: types.StorageClassGlacier})
	require.NoError(t, err)
	part, err := n1.s3.UploadPart(ctx, &s3.UploadPartInput{Bucket: aws.String("classes"), Key: aws.String("mp"), UploadId: upload.UploadId,
		PartNumber: aws.Int32(1), Body: strings.NewReader("parts")})
	require.NoError(t, err)
	_, err = n1.s3.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{Bucket: aws.String("classes"), Key: aws.String("mp"),
		UploadId: upload.UploadId, MultipartUpload: &types.CompletedMultipartUpload{Parts: []types.CompletedPart{
			{ETag: part.ETag, PartNumber: aws.Int32(1)}}}})
	require.NoError(t, err)
	c.eventuallyHeldBy(t, "classes", "mp", n2, n3)
	assert.Equal(t, "parts", getBody(t, n0.s3, "classes", "mp"))

	_, err = n0.s3.CopyObject(ctx, &s3.CopyObjectInput{Bucket: aws.String("classes"), Key: aws.String("copied"),
		CopySource: aws.String("classes/warm"), StorageClass: types.StorageClassGlacier})
	require.NoError(t, err)
	c.eventuallyHeldBy(t, "classes", "copied", n2, n3)
	assert.Equal(t, "GLACIER", classOnHead(t, n1, "classes", "copied"))
	_, err = n0.s3.CopyObject(ctx, &s3.CopyObjectInput{Bucket: aws.String("classes"), Key: aws.String("copied-back"),
		CopySource: aws.String("classes/cold")})
	require.NoError(t, err)
	c.eventuallyHeldBy(t, "classes", "copied-back", n0, n1)
	assert.Equal(t, "STANDARD", classOnHead(t, n1, "classes", "copied-back"))
}

// A class with fewer nodes than its protection needs keeps its writes on
// nodes of other classes as well; once it has the nodes, the copies are
// moved to them.
func TestAClassWithTooFewNodesKeepsItsWritesElsewhere(t *testing.T) {
	c := newHACluster(t, 3, 1)
	n0, n1, n2 := c.nodes[0], c.nodes[1], c.nodes[2]
	c.keepClass(t, cluster.StorageClass{Name: "GLACIER", Factor: 2})
	c.putInClass(t, n2, "GLACIER")
	require.NoError(t, n0.bucketManager.CreateBucket(context.Background(), "", "few", routingAdminID))

	putClassed(t, n0, "few", "cold", "GLACIER", []byte("cold"))
	holders := c.holdersOf(t, "few", "cold")
	assert.Len(t, holders, 2, "two copies")
	assert.Contains(t, holders, n2.id)
	assert.Contains(t, holders, n0.id, "the copy taken stays where no node of the class takes it")
	copiesTaken := func() int32 { return n0.copies.Load() + n1.copies.Load() + n2.copies.Load() }
	copies := copiesTaken()
	for _, n := range c.nodes {
		require.NoError(t, n.haSyncWorker.RepairPlacement(context.Background()))
	}
	assert.Equal(t, holders, c.holdersOf(t, "few", "cold"), "a repair keeps the copy out of the class while the class has no other node")
	assert.Equal(t, copies, copiesTaken(), "and makes no other")

	c.putInClass(t, n1, "GLACIER")
	c.repairAll(t)
	c.eventuallyHeldBy(t, "few", "cold", n1, n2)
	assert.Equal(t, "cold", getBody(t, n0.s3, "few", "cold"))
}

// setLifecycle stores the rules of a bucket through the first node.
func (c *haCluster) setLifecycle(t *testing.T, bucketName string, rules ...bucket.LifecycleRule) {
	t.Helper()
	require.NoError(t, c.nodes[0].bucketManager.SetLifecycle(context.Background(), "", bucketName, &bucket.LifecycleConfig{Rules: rules}))
	require.Eventually(t, func() bool {
		for _, n := range c.nodes {
			if got, err := n.bucketManager.GetLifecycle(context.Background(), "", bucketName); err != nil || len(got.Rules) != len(rules) {
				return false
			}
		}
		return true
	}, 10*time.Second, 50*time.Millisecond)
}

// runLifecycle runs a pass of the lifecycle rules on every node.
func (c *haCluster) runLifecycle(t *testing.T) {
	t.Helper()
	for _, n := range c.nodes {
		require.NoError(t, n.lifecycleWorker.RunOnce(context.Background()))
	}
}

// A lifecycle transition moves a write to the nodes of its new class: copied
// there, then dropped from the others; every node tells its class and serves
// it. One node moves it.
func TestALifecycleTransitionMovesAWriteToTheNodesOfItsClass(t *testing.T) {
	c := glacierCluster(t, "aging")
	n0, n1, n2, n3 := c.nodes[0], c.nodes[1], c.nodes[2], c.nodes[3]
	putClassed(t, n0, "aging", "k", "", []byte("aging data"))
	c.eventuallyHeldBy(t, "aging", "k", n0, n1)
	before := n0.entry(t, "aging", "k").LocationsGen
	copies := n2.copies.Load() + n3.copies.Load()

	entry := n0.entry(t, "aging", "k")
	change := object.ClassChange{ETag: entry.ETag, LastModified: entry.LastModified.Unix(), WrittenAt: entry.WrittenAt, StorageClass: "GLACIER"}
	another := change
	another.ETag = "another"
	for _, n := range c.nodes {
		moved, err := n.objectManager.(*cluster.HAObjectManager).ChangeStorageClass(context.Background(), "aging", "k", another)
		require.NoError(t, err)
		assert.False(t, moved, "another write of the key, on %s", n.id)
	}
	moves := 0
	for _, n := range c.except(n0, n1) {
		moved, err := n.objectManager.(*cluster.HAObjectManager).ChangeStorageClass(context.Background(), "aging", "k", change)
		require.NoError(t, err)
		if moved {
			moves++
		}
	}
	assert.Zero(t, moves, "a node not holding the write does not move it")
	assert.Equal(t, before, n0.entry(t, "aging", "k").LocationsGen)

	zero := 0
	c.setLifecycle(t, "aging", bucket.LifecycleRule{ID: "cold", Status: "Enabled",
		Transitions: []bucket.LifecycleTransition{{Days: &zero, StorageClass: "GLACIER"}}})
	c.runLifecycle(t)
	c.eventuallyHeldBy(t, "aging", "k", n2, n3)
	assert.EqualValues(t, 2, n2.copies.Load()+n3.copies.Load()-copies, "one copy to each node of the class")
	for _, n := range c.nodes {
		e := n.entry(t, "aging", "k")
		assert.Equal(t, "GLACIER", e.StorageClass, "node %s", n.id)
		assert.Greater(t, e.LocationsGen, before)
		assert.Equal(t, "aging data", getBody(t, n.s3, "aging", "k"))
	}

	c.runLifecycle(t)
	assert.EqualValues(t, 2, n2.copies.Load()+n3.copies.Load()-copies, "a write of the class is moved no more")
	moved := n0.entry(t, "aging", "k").LocationsGen
	for _, n := range c.nodes {
		again, err := n.objectManager.(*cluster.HAObjectManager).ChangeStorageClass(context.Background(), "aging", "k", change)
		require.NoError(t, err)
		assert.False(t, again, "already of the class, on %s", n.id)
	}
	assert.Equal(t, moved, n0.entry(t, "aging", "k").LocationsGen)
}

// A transition to a class kept with parity cuts the write in shards over the
// nodes of the class.
func TestATransitionToAClassWithParityCutsTheWrite(t *testing.T) {
	c := newHACluster(t, 5, 2)
	n0, n1 := c.nodes[0], c.nodes[1]
	c.keepClass(t, cluster.StorageClass{Name: "DEEP_ARCHIVE", Parity: true})
	for _, n := range c.nodes[2:] {
		c.putInClass(t, n, "DEEP_ARCHIVE")
	}
	require.NoError(t, n0.bucketManager.CreateBucket(context.Background(), "", "deep", routingAdminID))
	data := payload(600 << 10)
	putClassed(t, n0, "deep", "k", "", data)
	c.eventuallyHeldBy(t, "deep", "k", n0, n1)

	zero := 0
	c.setLifecycle(t, "deep", bucket.LifecycleRule{ID: "deep", Status: "Enabled",
		Transitions: []bucket.LifecycleTransition{{Days: &zero, StorageClass: "DEEP_ARCHIVE"}}})
	c.runLifecycle(t)
	layout := erasure.Layout{Data: 2, Parity: 1, Block: erasure.DefaultBlock}
	locations := eventuallySharded(t, c, "deep", "k", layout)
	assert.ElementsMatch(t, heldBy(c.nodes[2:]...), locations)
	for _, n := range c.nodes {
		assert.True(t, bytes.Equal(data, getBytes(t, n.s3, "deep", "k")), "read on %s", n.id)
	}

	putClassed(t, n1, "deep", "direct", "DEEP_ARCHIVE", data)
	assert.ElementsMatch(t, heldBy(c.nodes[2:]...), assertSharded(t, c, "deep", "direct", "", layout))
}

// A node put in another class moves the data of its old class off to the
// nodes of that class, and takes that of its new one.
func TestANodeMovedToAnotherClassMovesItsDataOff(t *testing.T) {
	c := newHACluster(t, 4, 2)
	n0, n1, n2, n3 := c.nodes[0], c.nodes[1], c.nodes[2], c.nodes[3]
	require.NoError(t, n0.bucketManager.CreateBucket(context.Background(), "", "moving", routingAdminID))
	n3.setFree(t, n3, 900)
	putClassed(t, n3, "moving", "k", "", []byte("moving data"))
	require.Contains(t, c.holdersOf(t, "moving", "k"), n3.id)

	c.keepClass(t, cluster.StorageClass{Name: "GLACIER", Factor: 1})
	c.putInClass(t, n3, "GLACIER")
	c.repairAll(t)
	require.Eventually(t, func() bool {
		holders := slices.Sorted(slices.Values(n0.entry(t, "moving", "k").Locations))
		return len(holders) == 2 && !slices.Contains(holders, n3.id) && !n3.holds(t, "moving", "k", "")
	}, 20*time.Second, 100*time.Millisecond)
	for _, id := range c.holdersOf(t, "moving", "k") {
		assert.True(t, c.byID(id).holds(t, "moving", "k", ""))
	}
	putClassed(t, n1, "moving", "cold", "GLACIER", []byte("cold"))
	c.eventuallyHeldBy(t, "moving", "cold", n3)
	_ = n2
}

// A class deleted puts its nodes back in STANDARD; its objects keep their
// class and are served, and its new writes are kept as STANDARD's.
func TestADeletedClassKeepsItsObjects(t *testing.T) {
	c := glacierCluster(t, "deleted")
	n0, n2, n3 := c.nodes[0], c.nodes[2], c.nodes[3]
	putClassed(t, n0, "deleted", "cold", "GLACIER", []byte("cold"))
	c.eventuallyHeldBy(t, "deleted", "cold", n2, n3)
	for _, n := range c.nodes {
		require.NoError(t, n.clusterManager.DeleteStorageClass(context.Background(), "GLACIER"))
	}
	c.repairAll(t)
	for _, n := range c.nodes {
		assert.Equal(t, "cold", getBody(t, n.s3, "deleted", "cold"))
		assert.Equal(t, "GLACIER", classOnHead(t, n, "deleted", "cold"))
	}
	putClassed(t, n0, "deleted", "later", "GLACIER", []byte("later"))
	assert.Len(t, c.holdersOf(t, "deleted", "later"), 2)
}

func consoleCall(t *testing.T, n *haNode, handler func(http.ResponseWriter, *http.Request), method, path string, vars map[string]string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		require.NoError(t, err)
	}
	req := asGlobalAdmin(httptest.NewRequest(method, path, bytes.NewReader(payload)), vars)
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

// The console keeps the classes of a cluster and puts its nodes in them: it
// shows each class with its protection, nodes and room, and the classes that
// can be added; STANDARD's protection is set on the HA page, and a node does
// not leave a class that would be left without its protection.
func TestTheConsoleKeepsStorageClasses(t *testing.T) {
	c := newHACluster(t, 3, 2)
	n0, n2 := c.nodes[0], c.nodes[2]
	set := func(name string, body any) *httptest.ResponseRecorder {
		return consoleCall(t, n0, n0.handleSetStorageClass, "PUT", "/api/v1/cluster/storage-classes/"+name, map[string]string{"name": name}, body)
	}
	assert.Equal(t, http.StatusOK, set("GLACIER", map[string]any{"factor": 1}).Code)
	rr := set("STANDARD", map[string]any{"factor": 1})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "HA page")
	assert.Equal(t, http.StatusBadRequest, set("COLD", map[string]any{"factor": 1}).Code)
	assert.Equal(t, http.StatusBadRequest, set("GLACIER", map[string]any{"factor": 5}).Code)

	move := func(node *haNode, class string) *httptest.ResponseRecorder {
		return consoleCall(t, n0, n0.handleSetNodeStorageClass, "PUT", "/api/v1/cluster/nodes/"+node.id+"/storage-class",
			map[string]string{"nodeId": node.id}, map[string]any{"storage_class": class})
	}
	assert.Equal(t, http.StatusOK, move(n2, "GLACIER").Code)
	assert.Equal(t, http.StatusConflict, move(c.nodes[1], "GLACIER").Code, "STANDARD keeps two copies")
	assert.Equal(t, http.StatusBadRequest, move(c.nodes[1], "DEEP_ARCHIVE").Code)

	rr = consoleCall(t, n0, n0.handleGetStorageClasses, "GET", "/api/v1/cluster/storage-classes", nil, nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var got struct {
		Data struct {
			Classes []cluster.StorageClassState `json:"classes"`
			Nodes   []struct {
				ID           string `json:"id"`
				StorageClass string `json:"storage_class"`
			} `json:"nodes"`
			Definable []string `json:"definable"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Len(t, got.Data.Classes, 2)
	assert.Equal(t, "STANDARD", got.Data.Classes[0].Name)
	assert.Equal(t, 2, got.Data.Classes[0].Factor)
	assert.Equal(t, 2, got.Data.Classes[0].Nodes)
	assert.Equal(t, "GLACIER", got.Data.Classes[1].Name)
	assert.Equal(t, 1, got.Data.Classes[1].Nodes)
	for _, n := range got.Data.Nodes {
		want := "STANDARD"
		if n.ID == n2.id {
			want = "GLACIER"
		}
		assert.Equal(t, want, n.StorageClass)
	}
	assert.NotContains(t, got.Data.Definable, "GLACIER")
	assert.Contains(t, got.Data.Definable, "DEEP_ARCHIVE")

	rr = consoleCall(t, n0, n0.handleGetClusterHA, "GET", "/api/v1/cluster/ha", nil, nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var ha struct {
		Data struct {
			StandardNodes  int `json:"standard_nodes"`
			StorageClasses int `json:"storage_classes"`
			Nodes          []struct {
				ID           string `json:"id"`
				StorageClass string `json:"storage_class"`
			} `json:"nodes"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &ha))
	assert.Equal(t, 2, ha.Data.StandardNodes)
	assert.Equal(t, 2, ha.Data.StorageClasses)
	for _, n := range ha.Data.Nodes {
		want := "STANDARD"
		if n.ID == n2.id {
			want = "GLACIER"
		}
		assert.Equal(t, want, n.StorageClass)
	}

	rr = consoleCall(t, n0, n0.handleDeleteStorageClass, "DELETE", "/api/v1/cluster/storage-classes/GLACIER", map[string]string{"name": "GLACIER"}, nil)
	assert.Equal(t, http.StatusOK, rr.Code)
	node, err := n0.clusterManager.GetNode(context.Background(), n2.id)
	require.NoError(t, err)
	assert.Equal(t, "", node.StorageClass, "back in STANDARD")
	rr = consoleCall(t, n0, n0.handleDeleteStorageClass, "DELETE", "/api/v1/cluster/storage-classes/GLACIER", map[string]string{"name": "GLACIER"}, nil)
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

// A delete of a write of a class takes the quorum of that class: with the
// other nodes down it is refused, and the write is kept, where one of
// STANDARD is made.
func TestADeleteTakesTheQuorumOfItsClass(t *testing.T) {
	c := newHACluster(t, 4, 1)
	n0 := c.nodes[0]
	ctx := context.Background()
	c.keepClass(t, cluster.StorageClass{Name: "GLACIER", Factor: 3})
	c.putInClass(t, c.nodes[2], "GLACIER")
	c.putInClass(t, c.nodes[3], "GLACIER")
	require.NoError(t, n0.bucketManager.CreateBucket(ctx, "", "quorum", routingAdminID))
	putClassed(t, n0, "quorum", "cold", "GLACIER", []byte("cold"))
	putClassed(t, n0, "quorum", "warm", "", []byte("warm"))
	for _, peer := range c.nodes[1:] {
		peer.down.Store(true)
		_, err := n0.db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusUnavailable, peer.id)
		require.NoError(t, err)
	}
	_, err := n0.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String("quorum"), Key: aws.String("cold")})
	assert.Equal(t, http.StatusServiceUnavailable, s3Status(err))
	_, err = n0.metadataStore.GetObject(ctx, "quorum", "cold")
	assert.NoError(t, err, "the write is kept")
	_, err = n0.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String("quorum"), Key: aws.String("warm")})
	assert.NoError(t, err)
}

// The HA page sets the protection of STANDARD by the nodes it has: those of
// other classes do not count.
func TestTheProtectionOfStandardIsSetOnItsNodes(t *testing.T) {
	c := newHACluster(t, 4, 1)
	n0 := c.nodes[0]
	c.keepClass(t, cluster.StorageClass{Name: "GLACIER", Factor: 1})
	c.putInClass(t, c.nodes[2], "GLACIER")
	c.putInClass(t, c.nodes[3], "GLACIER")
	for _, n := range c.nodes {
		n0.setFree(t, n, 1000)
	}
	rr := consoleCall(t, n0, n0.handleSetClusterHA, "PUT", "/api/v1/cluster/ha", nil, map[string]any{"factor": 3})
	assert.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
	assert.Contains(t, rr.Body.String(), "cluster has 2")
	rr = consoleCall(t, n0, n0.handleSetClusterHA, "PUT", "/api/v1/cluster/ha", nil, map[string]any{"factor": 2})
	assert.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
}

// A write of a class that loses a node with no other node in its class is
// copied to a node of another class: it keeps its two copies.
func TestAWriteOfAClassThatLosesANodeIsCopiedElsewhere(t *testing.T) {
	c := glacierCluster(t, "lost")
	n0, n2, n3 := c.nodes[0], c.nodes[2], c.nodes[3]
	ctx := context.Background()
	putClassed(t, n0, "lost", "cold", "GLACIER", []byte("cold data"))
	c.eventuallyHeldBy(t, "lost", "cold", n2, n3)

	n3.down.Store(true)
	for _, n := range c.nodes {
		_, err := n.db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusDead, n3.id)
		require.NoError(t, err)
	}
	for _, n := range c.except(n3) {
		require.NoError(t, n.haSyncWorker.RepairPlacement(ctx))
	}
	holders := slices.Sorted(slices.Values(n0.entry(t, "lost", "cold").Locations))
	require.Len(t, holders, 2)
	assert.Contains(t, holders, n2.id)
	assert.NotContains(t, holders, n3.id)
	for _, id := range holders {
		assert.True(t, c.byID(id).holds(t, "lost", "cold", ""), "node %s", id)
	}
	assert.Equal(t, "cold data", getBody(t, n0.s3, "lost", "cold"))
}

// A repair that trims the copies of a write keeps the one on a node of its
// class first, however little room that node has.
func TestARepairKeepsTheCopyOfTheClassFirst(t *testing.T) {
	c := newHACluster(t, 3, 1)
	n0, n1, n2 := c.nodes[0], c.nodes[1], c.nodes[2]
	ctx := context.Background()
	for _, n := range c.nodes {
		n.setFree(t, n0, 900)
		n.setFree(t, n1, 800)
		n.setFree(t, n2, 100)
	}
	c.keepClass(t, cluster.StorageClass{Name: "GLACIER", Factor: 2})
	require.NoError(t, n0.bucketManager.CreateBucket(ctx, "", "trim", routingAdminID))
	putClassed(t, n0, "trim", "cold", "GLACIER", []byte("cold data"))
	require.Equal(t, heldBy(n0, n1), c.holdersOf(t, "trim", "cold"), "the class has no node yet")

	c.putInClass(t, n2, "GLACIER")
	for _, n := range c.nodes {
		require.NoError(t, n.haSyncWorker.RepairPlacement(ctx))
	}
	holders := c.holdersOf(t, "trim", "cold")
	require.Len(t, holders, 2)
	assert.Contains(t, holders, n2.id, "the node of the class keeps its copy")
	for _, id := range holders {
		assert.True(t, c.byID(id).holds(t, "trim", "cold", ""), "node %s", id)
	}
}

// A write that names no node, held by more nodes than its class keeps, is
// given those of its class: no copy is made.
func TestAWriteNamingNoNodeIsGivenTheHoldersOfItsClass(t *testing.T) {
	c := newHACluster(t, 3, 1)
	ctx := context.Background()
	first := firstOf(c.nodes...)
	others := c.except(first)
	c.keepClass(t, cluster.StorageClass{Name: "GLACIER", Factor: 2})
	c.putInClass(t, others[0], "GLACIER")
	c.putInClass(t, others[1], "GLACIER")
	require.NoError(t, first.bucketManager.CreateBucket(ctx, "", "old", routingAdminID))
	at := time.Now().Truncate(time.Second)
	for _, n := range c.nodes {
		copyCtx := object.WithReplicatedWrittenAt(object.WithReplicatedLastModified(object.WithReplicaCopy(ctx), at), at.UnixNano())
		_, err := n.objectManager.(*cluster.HAObjectManager).Manager.PutObject(copyCtx, "old", "cold", strings.NewReader("cold"),
			http.Header{"X-Amz-Storage-Class": {"GLACIER"}})
		require.NoError(t, err)
	}
	require.Empty(t, first.entry(t, "old", "cold").Locations)

	require.NoError(t, first.haSyncWorker.RepairPlacement(ctx))
	assert.Equal(t, heldBy(others...), slices.Sorted(slices.Values(first.entry(t, "old", "cold").Locations)))
	for _, n := range c.nodes {
		assert.Zero(t, n.copies.Load(), "node %s", n.id)
	}
	assert.Equal(t, "cold", getBody(t, first.s3, "old", "cold"))
}

// A shard a write of a class with parity leaves on a node of another class,
// while a node of its class is down, is moved to that node once it is back.
func TestAShardKeptOutOfItsClassIsMovedToIt(t *testing.T) {
	c := newHACluster(t, 5, 1)
	n0, n4 := c.nodes[0], c.nodes[4]
	ctx := context.Background()
	c.keepClass(t, cluster.StorageClass{Name: "DEEP_ARCHIVE", Parity: true})
	for _, n := range c.nodes[2:] {
		c.putInClass(t, n, "DEEP_ARCHIVE")
	}
	require.NoError(t, n0.bucketManager.CreateBucket(ctx, "", "deep", routingAdminID))
	n4.down.Store(true)
	for _, n := range c.nodes {
		_, err := n.db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusUnavailable, n4.id)
		require.NoError(t, err)
	}
	data := payload(300 << 10)
	putClassed(t, n0, "deep", "k", "DEEP_ARCHIVE", data)
	layout := erasure.Layout{Data: 2, Parity: 1, Block: erasure.DefaultBlock}
	up := &haCluster{nodes: c.except(n4)}
	locations := assertSharded(t, up, "deep", "k", "", layout)
	require.Contains(t, locations, n0.id, "a node of another class takes the shard of the node down")
	shards := c.nodes[1].shards.Load()
	for _, n := range c.except(n4) {
		require.NoError(t, n.haSyncWorker.RepairPlacement(ctx))
	}
	assert.Equal(t, locations, assertSharded(t, up, "deep", "k", "", layout), "the shard stays while no node of the class takes it")
	assert.Equal(t, shards, c.nodes[1].shards.Load(), "it is not moved to another node out of the class")

	n4.down.Store(false)
	for _, n := range c.nodes {
		_, err := n.db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusHealthy, n4.id)
		require.NoError(t, err)
	}
	// The catch-up of the node back gives it the entry it missed.
	from, _ := cluster.ReplicaWriter(n0.objectManager)
	entry, err := from.ObjectEntry(ctx, "deep", "k", "")
	require.NoError(t, err)
	to, _ := cluster.ReplicaWriter(n4.objectManager)
	require.NoError(t, to.PutReplicaMetadata(ctx, entry))
	for _, n := range c.nodes {
		require.NoError(t, n.haSyncWorker.RepairPlacement(ctx))
	}
	locations = eventuallySharded(t, c, "deep", "k", layout)
	assert.ElementsMatch(t, heldBy(c.nodes[2:]...), locations)
	assert.True(t, bytes.Equal(data, getBytes(t, n0.s3, "deep", "k")))
}

// A write cut in shards moved by a transition keeps its shards until it is
// held by the nodes of its new class: kept there in whole copies, it is read
// from every node.
func TestATransitionOfAWriteInShardsJoinsItOnTheNodesOfItsClass(t *testing.T) {
	c := newHACluster(t, 5, 2)
	ctx := context.Background()
	for _, n := range c.nodes {
		require.NoError(t, n.clusterManager.SetParity(ctx, true))
	}
	c.keepClass(t, cluster.StorageClass{Name: "DEEP_ARCHIVE", Factor: 2})
	c.putInClass(t, c.nodes[3], "DEEP_ARCHIVE")
	c.putInClass(t, c.nodes[4], "DEEP_ARCHIVE")
	n0 := c.nodes[0]
	require.NoError(t, n0.bucketManager.CreateBucket(ctx, "", "shards", routingAdminID))
	data := payload(400 << 10)
	putClassed(t, n0, "shards", "k", "", data)
	layout := erasure.Layout{Data: 2, Parity: 1, Block: erasure.DefaultBlock}
	assert.ElementsMatch(t, heldBy(c.nodes[:3]...), assertSharded(t, c, "shards", "k", "", layout))

	zero := 0
	c.setLifecycle(t, "shards", bucket.LifecycleRule{ID: "deep", Status: "Enabled",
		Transitions: []bucket.LifecycleTransition{{Days: &zero, StorageClass: "DEEP_ARCHIVE"}}})
	c.runLifecycle(t)
	c.eventuallyHeldBy(t, "shards", "k", c.nodes[3], c.nodes[4])
	for _, n := range c.nodes {
		e := n.entry(t, "shards", "k")
		assert.Zero(t, e.ECData, "whole copies on %s", n.id)
		assert.Equal(t, "DEEP_ARCHIVE", e.StorageClass)
		assert.Empty(t, n.shardsHeld(t, "shards", "k", ""), "no shard left on %s", n.id)
		assert.True(t, bytes.Equal(data, getBytes(t, n.s3, "shards", "k")), "read on %s", n.id)
	}
}
