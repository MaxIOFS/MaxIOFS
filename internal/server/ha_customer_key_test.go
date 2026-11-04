package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/erasure"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var clusterCustomerKey = bytes.Repeat([]byte{0x3C}, object.CustomerKeySize)

// customerContent is n bytes a test finds in no stored shard or copy.
func customerContent(n int) []byte {
	return bytes.Repeat([]byte("CUSTOMER-PLAINTEXT-"), n/19+1)[:n]
}

func (n *haNode) putWithCustomerKey(t *testing.T, bucketName, key string, data []byte) *object.Object {
	t.Helper()
	obj, err := n.objectManager.PutObject(object.WithWriteKey(context.Background(), clusterCustomerKey),
		bucketName, key, bytes.NewReader(data), http.Header{})
	require.NoError(t, err)
	return obj
}

func (n *haNode) readWithCustomerKey(t *testing.T, bucketName, key string) []byte {
	t.Helper()
	_, body, err := n.objectManager.GetObject(object.WithReadKey(context.Background(), clusterCustomerKey), bucketName, key)
	require.NoError(t, err, "read on %s", n.id)
	defer body.Close()
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	return data
}

// A write with a customer key and parity is cut from the bytes it is stored
// as: every node reads it with the key, with a node down too, none without
// it, and no shard holds its content.
func TestACustomerKeyWriteWithParity(t *testing.T) {
	c := newParityCluster(t, 3, "ssec")
	data := customerContent(3*erasure.DefaultBlock + 70000)
	obj := c.nodes[0].putWithCustomerKey(t, "ssec", "k", data)

	locations := assertSharded(t, c, "ssec", "k", "", erasure.Layout{Data: 2, Parity: 1, Block: erasure.DefaultBlock})
	for _, n := range c.nodes {
		assert.True(t, bytes.Equal(data, n.readWithCustomerKey(t, "ssec", "k")), "read on %s", n.id)
		_, _, err := n.objectManager.GetObject(context.Background(), "ssec", "k")
		assert.ErrorIs(t, err, object.ErrCustomerKeyRequired, "read on %s without the key", n.id)
	}

	entry := c.nodes[1].entry(t, "ssec", "k")
	assert.Equal(t, obj.ETag, entry.ETag)
	for i, id := range locations {
		holder := c.byID(id)
		store, ok := cluster.ShardStoreOf(holder.objectManager)
		require.True(t, ok)
		info := object.ShardOf(obj, i, entry.LocationsGen)
		info.Layout = erasure.Layout{Data: entry.ECData, Parity: entry.ECParity, Block: entry.ECBlock}
		shard, err := store.OpenShard(context.Background(), info, 0)
		require.NoError(t, err)
		held, err := io.ReadAll(shard)
		shard.Close()
		require.NoError(t, err)
		assert.NotContains(t, string(held), "CUSTOMER-PLAINTEXT-", "the shard on %s opens to no content", id)
	}

	// A read without the key is refused before any shard is asked for.
	for _, n := range c.nodes {
		n.shardReads.Store(0)
	}
	_, _, err := c.nodes[0].objectManager.GetObject(context.Background(), "ssec", "k")
	assert.ErrorIs(t, err, object.ErrCustomerKeyRequired)
	for _, n := range c.nodes {
		assert.Zero(t, n.shardReads.Load(), "no shard read on %s", n.id)
	}

	for _, down := range locations {
		c.byID(down).down.Store(true)
		for _, n := range c.except(c.byID(down)) {
			assert.True(t, bytes.Equal(data, n.readWithCustomerKey(t, "ssec", "k")), "read on %s with %s down", n.id, down)
		}
		c.byID(down).down.Store(false)
	}
}

// A write with a customer key moves between nodes as stored: copied with a
// factor of 2, read on the node that holds no copy, cut in shards and joined
// again, all without the key.
func TestACustomerKeyWriteMovesAsStored(t *testing.T) {
	c := newHACluster(t, 3, 2)
	ctx := context.Background()
	require.NoError(t, c.nodes[0].bucketManager.CreateBucket(ctx, "", "ssec", routingAdminID))
	data := customerContent(400000)
	c.nodes[1].putWithCustomerKey(t, "ssec", "k", data)

	holders := 0
	for _, n := range c.nodes {
		if n.holds(t, "ssec", "k", "") {
			holders++
		}
		assert.True(t, bytes.Equal(data, n.readWithCustomerKey(t, "ssec", "k")), "read on %s", n.id)
	}
	assert.Equal(t, 2, holders, "two whole copies")

	for _, n := range c.nodes {
		require.NoError(t, n.clusterManager.SetParity(ctx, true))
	}
	c.repairAll(t)
	eventuallySharded(t, c, "ssec", "k", erasure.Layout{Data: 2, Parity: 1, Block: erasure.DefaultBlock})
	for _, n := range c.nodes {
		assert.True(t, bytes.Equal(data, n.readWithCustomerKey(t, "ssec", "k")), "read in shards on %s", n.id)
	}

	for _, n := range c.nodes {
		require.NoError(t, n.clusterManager.SetParity(ctx, false))
		require.NoError(t, n.clusterManager.SetReplicationFactor(ctx, 2))
	}
	c.repairAll(t)
	require.Eventually(t, func() bool {
		holders := 0
		for _, n := range c.nodes {
			e := n.entry(t, "ssec", "k")
			if e.ECData != 0 || len(e.Locations) != 2 || len(n.shardsHeld(t, "ssec", "k", "")) != 0 {
				return false
			}
			if n.holds(t, "ssec", "k", "") {
				holders++
			}
		}
		return holders == 2
	}, 20*time.Second, 100*time.Millisecond)
	for _, n := range c.nodes {
		assert.True(t, bytes.Equal(data, n.readWithCustomerKey(t, "ssec", "k")), "read joined on %s", n.id)
		_, _, err := n.objectManager.GetObject(ctx, "ssec", "k")
		assert.ErrorIs(t, err, object.ErrCustomerKeyRequired)
	}
}
