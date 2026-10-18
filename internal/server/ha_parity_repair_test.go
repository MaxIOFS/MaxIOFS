package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/erasure"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repairAll runs the repair of the copies and shards on every node.
func (c *haCluster) repairAll(t *testing.T) {
	t.Helper()
	for _, n := range c.nodes {
		n.haSyncWorker.StartRepair(context.Background())
	}
}

// eventuallySharded waits until every node names the shards of a write cut by
// layout and their holders hold them.
func eventuallySharded(t *testing.T, c *haCluster, bucketName, key string, layout erasure.Layout) []string {
	t.Helper()
	require.Eventually(t, func() bool {
		var locations []string
		for _, n := range c.nodes {
			e, err := n.metadataStore.GetObject(context.Background(), bucketName, key)
			if err != nil || e.ECData != layout.Data || e.ECParity != layout.Parity {
				return false
			}
			if locations == nil {
				locations = e.Locations
			}
			if !slices.Equal(locations, e.Locations) {
				return false
			}
		}
		for i, id := range locations {
			holder := c.byID(id)
			if holder == nil || !slices.Equal([]int{i}, holder.shardsHeld(t, bucketName, key, "")) {
				return false
			}
		}
		return true
	}, 20*time.Second, 100*time.Millisecond)
	return assertSharded(t, c, bucketName, key, "", layout)
}

// Turning parity on cuts the writes kept in whole copies in shards; turning it
// off joins them in whole copies again.
func TestTurningParityOnAndOffCutsAndJoinsTheWrites(t *testing.T) {
	c := newHACluster(t, 3, 2)
	ctx := context.Background()
	require.NoError(t, c.nodes[0].bucketManager.CreateBucket(ctx, "", "turn", routingAdminID))
	data := payload(400000)
	_, err := c.nodes[1].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("turn"), Key: aws.String("k"), Body: bytes.NewReader(data)})
	require.NoError(t, err)
	require.Len(t, c.nodes[0].entry(t, "turn", "k").Locations, 2)

	for _, n := range c.nodes {
		require.NoError(t, n.clusterManager.SetParity(ctx, true))
	}
	c.repairAll(t)
	eventuallySharded(t, c, "turn", "k", erasure.Layout{Data: 2, Parity: 1, Block: erasure.DefaultBlock})
	for _, n := range c.nodes {
		assert.True(t, bytes.Equal(data, getBytes(t, n.s3, "turn", "k")))
	}

	for _, n := range c.nodes {
		require.NoError(t, n.clusterManager.SetParity(ctx, false))
		require.NoError(t, n.clusterManager.SetReplicationFactor(ctx, 2))
	}
	c.repairAll(t)
	require.Eventually(t, func() bool {
		holders := 0
		for _, n := range c.nodes {
			e := n.entry(t, "turn", "k")
			if e.ECData != 0 || len(e.Locations) != 2 || len(n.shardsHeld(t, "turn", "k", "")) != 0 {
				return false
			}
			if n.holds(t, "turn", "k", "") {
				holders++
			}
		}
		return holders == 2
	}, 20*time.Second, 100*time.Millisecond)
	for _, n := range c.nodes {
		assert.True(t, bytes.Equal(data, getBytes(t, n.s3, "turn", "k")))
	}
}

// fourWithASpare is four nodes with parity and a write cut over three of
// them, the fourth drained while it was made (it took the entry only), then
// taking data again.
func fourWithASpare(t *testing.T) (c *haCluster, spare *haNode, locations []string, data []byte) {
	t.Helper()
	c = newParityCluster(t, 4, "spare")
	spare = c.nodes[3]
	setDrained := func(drained int) {
		for _, n := range c.except(spare) {
			_, err := n.db.Exec(`UPDATE cluster_nodes SET drained = ? WHERE id = ?`, drained, spare.id)
			require.NoError(t, err)
		}
	}
	setDrained(1)
	data = payload(500000)
	_, err := c.nodes[0].s3.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String("spare"), Key: aws.String("k"), Body: bytes.NewReader(data)})
	require.NoError(t, err)
	setDrained(0)
	locations = c.nodes[0].entry(t, "spare", "k").Locations
	require.Len(t, locations, 3)
	require.NotContains(t, locations, spare.id)
	return c, spare, locations, data
}

// The shard of a removed node is made again from the others on a node that
// holds none of the write, at the same index.
func TestTheShardOfARemovedNodeIsMadeAgain(t *testing.T) {
	c, spare, locations, data := fourWithASpare(t)
	ctx := context.Background()
	gone := c.byID(locations[1])
	for _, n := range c.except(gone) {
		require.NoError(t, n.clusterManager.RemoveNode(ctx, gone.id))
	}
	rest := &haCluster{nodes: c.except(gone)}
	rest.repairAll(t)
	moved := eventuallySharded(t, rest, "spare", "k", erasure.Layout{Data: 2, Parity: 1, Block: erasure.DefaultBlock})
	assert.Equal(t, []string{locations[0], spare.id, locations[2]}, moved)

	rest.byID(locations[0]).down.Store(true)
	assert.True(t, bytes.Equal(data, getBytes(t, spare.s3, "spare", "k")), "read with the rebuilt shard")
}

// A drained node's shard is moved to a node holding none of the write before
// the drained node leaves service.
func TestADrainedNodesShardIsMoved(t *testing.T) {
	c, spare, locations, data := fourWithASpare(t)
	ctx := context.Background()
	drained := c.byID(locations[2])
	w := httptest.NewRecorder()
	drained.handleDrainClusterNode(w, asGlobalAdmin(httptest.NewRequest(http.MethodPost, "/api/v1/cluster/nodes/"+drained.id+"/drain", nil),
		map[string]string{"nodeId": drained.id}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Eventually(t, func() bool {
		for _, n := range c.nodes {
			n.deadNodeReconciler.ApplyDrains(ctx)
		}
		return drained.clusterManager.LocalDrainDone(ctx)
	}, 20*time.Second, 100*time.Millisecond, "the drained node ends empty")
	for _, n := range c.nodes {
		require.Eventually(t, func() bool {
			return slices.Equal([]string{locations[0], locations[1], spare.id}, n.entry(t, "spare", "k").Locations)
		}, 20*time.Second, 100*time.Millisecond)
	}
	assert.Equal(t, []int{2}, spare.shardsHeld(t, "spare", "k", ""))
	assert.Empty(t, drained.shardsHeld(t, "spare", "k", ""))
	c.byID(locations[0]).down.Store(true)
	assert.True(t, bytes.Equal(data, getBytes(t, spare.s3, "spare", "k")))
}

// A fifth node holding data raises the parity of the writes cut over four:
// they are cut again with two parity shards, and read with two nodes down.
func TestAFifthNodeRaisesTheParity(t *testing.T) {
	c := newParityCluster(t, 5, "grow")
	ctx := context.Background()
	fifth := c.nodes[4]
	// Drained, it holds no data and is sent every entry.
	for _, n := range c.except(fifth) {
		_, err := n.db.Exec(`UPDATE cluster_nodes SET drained = 1 WHERE id = ?`, fifth.id)
		require.NoError(t, err)
	}
	data := payload(600000)
	_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("grow"), Key: aws.String("k"), Body: bytes.NewReader(data)})
	require.NoError(t, err)
	before := c.nodes[0].entry(t, "grow", "k")
	require.Equal(t, [2]int{3, 1}, [2]int{before.ECData, before.ECParity})

	for _, n := range c.except(fifth) {
		_, err := n.db.Exec(`UPDATE cluster_nodes SET drained = 0 WHERE id = ?`, fifth.id)
		require.NoError(t, err)
	}
	c.repairAll(t)
	locations := eventuallySharded(t, c, "grow", "k", erasure.Layout{Data: 3, Parity: 2, Block: erasure.DefaultBlock})
	for i, id := range before.Locations {
		assert.NotEqual(t, id, locations[i], "no node keeps the index it held: shard %d", i)
	}
	c.byID(locations[0]).down.Store(true)
	c.byID(locations[3]).down.Store(true)
	assert.True(t, bytes.Equal(data, getBytes(t, c.byID(locations[4]).s3, "grow", "k")))
}

// A shard a node lost is made again by the first of the write's other holders
// when it compares the key with that node.
func TestALostShardIsMadeAgain(t *testing.T) {
	c := newParityCluster(t, 3, "lost")
	ctx := context.Background()
	data := payload(300000)
	_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("lost"), Key: aws.String("k"), Body: bytes.NewReader(data)})
	require.NoError(t, err)
	locations := assertSharded(t, c, "lost", "k", "", erasure.Layout{Data: 2, Parity: 1, Block: erasure.DefaultBlock})
	loser := c.byID(locations[1])
	require.NoError(t, loser.storageBackend.Delete(ctx, storage.ObjectRef{Bucket: "lost", Key: "k"}.ShardOf(1)))
	require.Empty(t, loser.shardsHeld(t, "lost", "k", ""))

	for _, n := range c.except(loser) {
		startScrubber(t, n.Server)
		n.antiEntropyScrubber.CatchUp(loser.id, time.Unix(1, 0))
	}
	require.Eventually(t, func() bool {
		return slices.Equal([]int{1}, loser.shardsHeld(t, "lost", "k", ""))
	}, 20*time.Second, 100*time.Millisecond)
	c.byID(locations[0]).down.Store(true)
	assert.True(t, bytes.Equal(data, getBytes(t, loser.s3, "lost", "k")))
}

// A node that did not take its shard during a write is given it when it is
// caught up.
func TestAShardMissedDuringAWriteIsMadeOnCatchUp(t *testing.T) {
	c := newParityCluster(t, 5, "missed")
	ctx := context.Background()
	missing := c.nodes[3]
	missing.refuseShards.Store(true)
	_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("missed"), Key: aws.String("k"), Body: bytes.NewReader(payload(200000))})
	require.NoError(t, err)
	index := slices.Index(c.nodes[0].entry(t, "missed", "k").Locations, missing.id)
	require.GreaterOrEqual(t, index, 0)
	missing.refuseShards.Store(false)

	for _, n := range c.except(missing) {
		_, err := n.db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusHealthy, missing.id)
		require.NoError(t, err)
		startScrubber(t, n.Server)
		n.antiEntropyScrubber.CatchUp(missing.id, time.Unix(1, 0))
	}
	require.Eventually(t, func() bool {
		return slices.Equal([]int{index}, missing.shardsHeld(t, "missed", "k", ""))
	}, 20*time.Second, 100*time.Millisecond)
}

// A node that leaves the cluster drops the writes cut in shards, which its
// shard alone cannot serve, and keeps those it holds whole.
func TestANodeThatLeavesDropsItsShards(t *testing.T) {
	c := newParityCluster(t, 3, "leave")
	ctx := context.Background()
	leaving := c.nodes[2]
	_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("leave"), Key: aws.String("cut"), Body: bytes.NewReader(payload(100000))})
	require.NoError(t, err)
	require.NotEmpty(t, leaving.shardsHeld(t, "leave", "cut", ""))

	w := httptest.NewRecorder()
	leaving.handleLeaveCluster(w, asGlobalAdmin(httptest.NewRequest(http.MethodPost, "/api/v1/cluster/leave", nil), nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Eventually(t, func() bool {
		_, err := leaving.metadataStore.GetObject(ctx, "leave", "cut")
		return err != nil && len(leaving.shardsHeld(t, "leave", "cut", "")) == 0
	}, 15*time.Second, 50*time.Millisecond)
}

// A drained node's shard is copied from it: it moves with another holder
// down, when the others are too few to make it again.
func TestADrainedNodesShardIsCopiedWithAnotherNodeDown(t *testing.T) {
	c, spare, locations, data := fourWithASpare(t)
	ctx := context.Background()
	drained, away := c.byID(locations[2]), c.byID(locations[1])
	away.down.Store(true)
	w := httptest.NewRecorder()
	drained.handleDrainClusterNode(w, asGlobalAdmin(httptest.NewRequest(http.MethodPost, "/api/v1/cluster/nodes/"+drained.id+"/drain", nil),
		map[string]string{"nodeId": drained.id}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Eventually(t, func() bool {
		for _, n := range c.except(away) {
			n.deadNodeReconciler.ApplyDrains(ctx)
		}
		return slices.Equal([]int{2}, spare.shardsHeld(t, "spare", "k", ""))
	}, 20*time.Second, 100*time.Millisecond)
	away.down.Store(false)
	c.byID(locations[0]).down.Store(true)
	require.Eventually(t, func() bool {
		return slices.Equal([]string{locations[0], locations[1], spare.id}, spare.entry(t, "spare", "k").Locations)
	}, 20*time.Second, 100*time.Millisecond)
	assert.True(t, bytes.Equal(data, getBytes(t, spare.s3, "spare", "k")))
	layout := erasure.Layout{Data: 2, Parity: 1, Block: erasure.DefaultBlock}
	progress := drained.haSyncWorker.DrainProgress()
	require.NotNil(t, progress)
	assert.EqualValues(t, layout.ShardSize(int64(len(data))), progress.TotalBytes, "the drained node held one shard")
}

// One node makes a lost shard again, whichever nodes compare the key.
func TestALostShardIsMadeByOneNode(t *testing.T) {
	c := newParityCluster(t, 4, "lost")
	ctx := context.Background()
	_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("lost"), Key: aws.String("k"), Body: bytes.NewReader(payload(300000))})
	require.NoError(t, err)
	locations := c.nodes[0].entry(t, "lost", "k").Locations
	loser := c.byID(locations[0])
	require.NoError(t, loser.storageBackend.Delete(ctx, storage.ObjectRef{Bucket: "lost", Key: "k"}.ShardOf(0)))
	taken := loser.shards.Load()
	others := c.except(loser)
	for _, n := range others {
		startScrubber(t, n.Server)
	}
	before := make([]int, len(others))
	for i, n := range others {
		before[i] = n.scrubRuns(t)
		n.antiEntropyScrubber.CatchUp(loser.id, time.Unix(1, 0))
	}
	require.Eventually(t, func() bool {
		for i, n := range others {
			if n.scrubRuns(t) <= before[i] {
				return false
			}
		}
		return true
	}, 20*time.Second, 100*time.Millisecond)
	assert.Equal(t, []int{0}, loser.shardsHeld(t, "lost", "k", ""))
	assert.EqualValues(t, 1, loser.shards.Load()-taken, "made once")
}

// A node that took neither the shard nor the entry of a write is sent the
// entry and given the shard in one catch-up.
func TestANodeThatMissedAWriteInShardsIsSentBoth(t *testing.T) {
	c := newParityCluster(t, 5, "missed")
	ctx := context.Background()
	missing := c.nodes[3]
	missing.refuseShards.Store(true)
	missing.refuseEntries.Store(true)
	_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("missed"), Key: aws.String("k"), Body: bytes.NewReader(payload(200000))})
	require.NoError(t, err)
	index := slices.Index(c.nodes[0].entry(t, "missed", "k").Locations, missing.id)
	require.GreaterOrEqual(t, index, 0)
	_, err = missing.metadataStore.GetObject(ctx, "missed", "k")
	require.Error(t, err)
	missing.refuseShards.Store(false)
	missing.refuseEntries.Store(false)

	for _, n := range c.except(missing) {
		_, err := n.db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusHealthy, missing.id)
		require.NoError(t, err)
		startScrubber(t, n.Server)
		n.antiEntropyScrubber.CatchUp(missing.id, time.Unix(1, 0))
	}
	require.Eventually(t, func() bool {
		_, err := missing.metadataStore.GetObject(ctx, "missed", "k")
		return err == nil && slices.Equal([]int{index}, missing.shardsHeld(t, "missed", "k", ""))
	}, 20*time.Second, 100*time.Millisecond)
}

// A node holding an earlier write of a key takes the entry of a later write
// cut in shards from a node that has it, in its own anti-entropy.
func TestANodeTakesTheEntryOfALaterWriteInShards(t *testing.T) {
	c := newParityCluster(t, 5, "later")
	ctx := context.Background()
	behind := c.nodes[3]
	_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("later"), Key: aws.String("k"), Body: bytes.NewReader(payload(1000))})
	require.NoError(t, err)
	time.Sleep(1100 * time.Millisecond)
	behind.refuseShards.Store(true)
	behind.refuseEntries.Store(true)
	_, err = c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("later"), Key: aws.String("k"), Body: bytes.NewReader(payload(2000))})
	require.NoError(t, err)
	want := c.nodes[0].entry(t, "later", "k").ETag
	require.NotEqual(t, want, behind.entry(t, "later", "k").ETag)
	behind.refuseShards.Store(false)
	behind.refuseEntries.Store(false)
	for _, n := range c.except(behind) {
		_, err := behind.db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusHealthy, n.id)
		require.NoError(t, err)
	}

	startScrubber(t, behind.Server)
	before := behind.scrubRuns(t)
	behind.antiEntropyScrubber.CatchUp(c.nodes[0].id, time.Unix(1, 0))
	require.Eventually(t, func() bool { return behind.scrubRuns(t) > before }, 20*time.Second, 100*time.Millisecond)
	assert.Equal(t, want, behind.entry(t, "later", "k").ETag)
}
