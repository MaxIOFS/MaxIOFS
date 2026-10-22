package server

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (c *haCluster) versioning(t *testing.T, bucketName, status string) {
	t.Helper()
	for _, n := range c.nodes {
		require.NoError(t, n.bucketManager.SetVersioning(context.Background(), "", bucketName, &bucket.VersioningConfig{Status: status}))
	}
}

func nullVersion(in *s3.GetObjectInput) { in.VersionId = aws.String("null") }

// An object written before versioning was enabled is kept as the null version
// on every node when a version replaces it, and every node reads it, kept in
// whole copies or cut in shards.
func TestTheNullVersionIsKeptOnEveryNode(t *testing.T) {
	for _, setup := range []struct {
		name  string
		build func(t *testing.T) *haCluster
	}{
		{"copies", func(t *testing.T) *haCluster {
			c := newHACluster(t, 2, 2)
			require.NoError(t, c.nodes[0].bucketManager.CreateBucket(context.Background(), "", "nullv", routingAdminID))
			return c
		}},
		{"shards", func(t *testing.T) *haCluster { return newParityCluster(t, 3, "nullv") }},
	} {
		t.Run(setup.name, func(t *testing.T) {
			c := setup.build(t)
			ctx := context.Background()
			before, after := payload(300<<10), payload(200<<10)
			_, err := c.nodes[0].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("nullv"), Key: aws.String("k"), Body: bytes.NewReader(before)})
			require.NoError(t, err)
			c.versioning(t, "nullv", "Enabled")
			_, err = c.nodes[1].s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("nullv"), Key: aws.String("k"), Body: bytes.NewReader(after)})
			require.NoError(t, err)
			for _, n := range c.nodes {
				assert.True(t, bytes.Equal(before, getBytes(t, n.s3, "nullv", "k", nullVersion)), "the null version read on %s", n.id)
				assert.True(t, bytes.Equal(after, getBytes(t, n.s3, "nullv", "k")), "the current version read on %s", n.id)
			}
		})
	}
}

// A delete of the null version stops the copies of the one it deleted, not of
// one written after it: a node that missed the later one is sent it.
func TestANullVersionWrittenAfterADeleteOfOneIsCaughtUp(t *testing.T) {
	c := newHACluster(t, 2, 2)
	a, b := c.nodes[0], c.nodes[1]
	ctx := context.Background()
	require.NoError(t, a.bucketManager.CreateBucket(ctx, "", "nullv", routingAdminID))
	put := func(data string) {
		t.Helper()
		_, err := a.s3.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("nullv"), Key: aws.String("k"), Body: bytes.NewReader([]byte(data))})
		require.NoError(t, err)
	}
	put("first")
	c.versioning(t, "nullv", "Enabled")
	put("v1")
	_, err := a.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String("nullv"), Key: aws.String("k"), VersionId: aws.String("null")})
	require.NoError(t, err)
	time.Sleep(1100 * time.Millisecond)

	b.down.Store(true)
	c.versioning(t, "nullv", "Suspended")
	put("second")
	c.versioning(t, "nullv", "Enabled")
	put("v2")
	b.down.Store(false)
	_, err = a.db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusHealthy, b.id)
	require.NoError(t, err)
	startScrubber(t, a.Server)
	a.antiEntropyScrubber.CatchUp(b.id, time.Unix(1, 0))
	require.Eventually(t, func() bool {
		out, err := b.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("nullv"), Key: aws.String("k"), VersionId: aws.String("null")})
		if err != nil {
			return false
		}
		defer out.Body.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(out.Body)
		return buf.String() == "second"
	}, 20*time.Second, 100*time.Millisecond, "the null version written after the delete reaches the node that missed it")
	// The delete of the earlier null version is sent too, after the copies; it
	// leaves the later one.
	time.Sleep(2 * time.Second)
	out, err := b.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("nullv"), Key: aws.String("k"), VersionId: aws.String("null")})
	require.NoError(t, err, "the later null version is kept")
	out.Body.Close()
}
