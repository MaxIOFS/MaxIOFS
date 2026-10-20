package lifecycle

import (
	"bytes"
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func classOf(t *testing.T, objects object.Manager, key string, versionID ...string) string {
	t.Helper()
	obj, err := objects.GetObjectMetadata(context.Background(), "lifecycle", key, versionID...)
	require.NoError(t, err)
	return obj.StorageClass
}

func later(days int) func() time.Time {
	return func() time.Time { return time.Now().AddDate(0, 0, days) }
}

// A transition moves the objects its filter takes to the coldest class due,
// as days pass; another rule moves its own.
func TestATransitionMovesObjectsToAColderClass(t *testing.T) {
	thirty, ninety, one := 30, 90, 1
	worker, objects := storedRulesWorker(t, bucket.LifecycleRule{
		ID: "logs", Status: "Enabled",
		Filter: bucket.LifecycleFilter{Prefix: "logs/"},
		Transitions: []bucket.LifecycleTransition{
			{Days: &ninety, StorageClass: "GLACIER"},
			{Days: &thirty, StorageClass: "STANDARD_IA"},
		},
	}, bucket.LifecycleRule{
		ID: "tagged", Status: "Enabled",
		Filter:      bucket.LifecycleFilter{Prefix: "data/", Tags: []bucket.Tag{{Key: "cold", Value: "yes"}}},
		Transitions: []bucket.LifecycleTransition{{Days: &one, StorageClass: "DEEP_ARCHIVE"}},
	}, bucket.LifecycleRule{
		ID: "off", Status: "Disabled",
		Transitions: []bucket.LifecycleTransition{{Days: &one, StorageClass: "GLACIER_IR"}},
	})
	putObject(t, objects, "logs/a", 4, nil)
	putObject(t, objects, "data/cold", 4, map[string]string{"cold": "yes"})
	putObject(t, objects, "data/warm", 4, map[string]string{"cold": "no"})
	putObject(t, objects, "logs-not/b", 4, nil)
	ctx := context.Background()

	worker.now = later(10)
	require.NoError(t, worker.processLifecyclePolicies(ctx))
	assert.Equal(t, "STANDARD", classOf(t, objects, "logs/a"), "no transition is due")
	assert.Equal(t, "DEEP_ARCHIVE", classOf(t, objects, "data/cold"))
	assert.Equal(t, "STANDARD", classOf(t, objects, "data/warm"))
	assert.Equal(t, "STANDARD", classOf(t, objects, "logs-not/b"))

	worker.now = later(40)
	require.NoError(t, worker.processLifecyclePolicies(ctx))
	assert.Equal(t, "STANDARD_IA", classOf(t, objects, "logs/a"))

	worker.now = later(100)
	require.NoError(t, worker.processLifecyclePolicies(ctx))
	assert.Equal(t, "GLACIER", classOf(t, objects, "logs/a"))
	assert.Equal(t, "STANDARD", classOf(t, objects, "logs-not/b"))
}

// A transition on a date moves the objects written before it once it has
// come.
func TestATransitionOnADateWaitsForIt(t *testing.T) {
	on := time.Now().UTC().AddDate(0, 0, 5)
	worker, objects := storedRulesWorker(t, bucket.LifecycleRule{
		ID: "on", Status: "Enabled",
		Transitions: []bucket.LifecycleTransition{{Date: &on, StorageClass: "GLACIER"}},
	})
	putObject(t, objects, "k", 4, nil)
	ctx := context.Background()
	worker.now = later(1)
	require.NoError(t, worker.processLifecyclePolicies(ctx))
	assert.Equal(t, "STANDARD", classOf(t, objects, "k"))
	worker.now = later(6)
	require.NoError(t, worker.processLifecyclePolicies(ctx))
	assert.Equal(t, "GLACIER", classOf(t, objects, "k"))
}

// An object only moves to a colder class: one written in a colder one stays.
func TestATransitionMovesObjectsColderOnly(t *testing.T) {
	zero := 0
	worker, objects := storedRulesWorker(t, bucket.LifecycleRule{
		ID: "glacier", Status: "Enabled",
		Transitions: []bucket.LifecycleTransition{{Days: &zero, StorageClass: "GLACIER"}},
	})
	ctx := context.Background()
	_, err := objects.PutObject(ctx, "lifecycle", "deep", bytes.NewReader([]byte("data")), http.Header{"X-Amz-Storage-Class": {"DEEP_ARCHIVE"}})
	require.NoError(t, err)
	worker.now = later(1)
	require.NoError(t, worker.processLifecyclePolicies(ctx))
	assert.Equal(t, "DEEP_ARCHIVE", classOf(t, objects, "deep"))
}

// An object due to expire is not moved: the expiration wins, on the node that
// expires objects.
func TestAnExpirationDueWinsOverATransition(t *testing.T) {
	one := 1
	worker, objects := storedRulesWorker(t, bucket.LifecycleRule{
		ID: "move", Status: "Enabled",
		Transitions: []bucket.LifecycleTransition{{Days: &one, StorageClass: "GLACIER"}},
	}, bucket.LifecycleRule{
		ID: "expire", Status: "Enabled",
		Filter:     bucket.LifecycleFilter{Prefix: "tmp/"},
		Expiration: &bucket.LifecycleExpiration{Days: &one},
	})
	worker.SetExpirationGate(func() bool { return false })
	putObject(t, objects, "tmp/k", 4, nil)
	putObject(t, objects, "kept", 4, nil)
	worker.now = later(5)
	require.NoError(t, worker.processLifecyclePolicies(context.Background()))
	assert.Equal(t, "STANDARD", classOf(t, objects, "tmp/k"))
	assert.Equal(t, "GLACIER", classOf(t, objects, "kept"))
}

// Noncurrent versions move to a colder class the days after a later version
// replaced them, but for the newest ones the rule keeps; the current one
// stays.
func TestNoncurrentVersionsMoveToAColderClass(t *testing.T) {
	worker, objects := storedRulesWorker(t, bucket.LifecycleRule{
		ID: "old", Status: "Enabled",
		NoncurrentVersionTransitions: []bucket.NoncurrentVersionTransition{
			{NoncurrentDays: 10, NewerNoncurrentVersions: 1, StorageClass: "GLACIER"},
		},
	})
	ctx := context.Background()
	require.NoError(t, worker.bucketManager.SetVersioning(ctx, "", "lifecycle", &bucket.VersioningConfig{Status: "Enabled"}))
	var ids []string
	for range 3 {
		obj, err := objects.PutObject(ctx, "lifecycle", "k", bytes.NewReader([]byte("data")), http.Header{})
		require.NoError(t, err)
		ids = append(ids, obj.VersionID)
	}
	worker.now = later(5)
	require.NoError(t, worker.processLifecyclePolicies(ctx))
	assert.Equal(t, "STANDARD", classOf(t, objects, "k", ids[0]), "noncurrent for five days")

	worker.now = later(20)
	require.NoError(t, worker.processLifecyclePolicies(ctx))
	assert.Equal(t, "GLACIER", classOf(t, objects, "k", ids[0]))
	assert.Equal(t, "STANDARD", classOf(t, objects, "k", ids[1]), "the newest noncurrent version is kept")
	assert.Equal(t, "STANDARD", classOf(t, objects, "k", ids[2]), "the current one")
}

// A noncurrent version due to expire is not moved: the expiration wins.
func TestANoncurrentExpirationDueWinsOverATransition(t *testing.T) {
	worker, objects := storedRulesWorker(t, bucket.LifecycleRule{
		ID: "old", Status: "Enabled",
		NoncurrentVersionTransitions: []bucket.NoncurrentVersionTransition{{NoncurrentDays: 1, StorageClass: "GLACIER"}},
		NoncurrentVersionExpiration:  &bucket.NoncurrentVersionExpiration{NoncurrentDays: 1},
	})
	ctx := context.Background()
	worker.SetExpirationGate(func() bool { return false })
	require.NoError(t, worker.bucketManager.SetVersioning(ctx, "", "lifecycle", &bucket.VersioningConfig{Status: "Enabled"}))
	first, err := objects.PutObject(ctx, "lifecycle", "k", bytes.NewReader([]byte("one")), http.Header{})
	require.NoError(t, err)
	_, err = objects.PutObject(ctx, "lifecycle", "k", bytes.NewReader([]byte("two")), http.Header{})
	require.NoError(t, err)
	worker.now = later(5)
	require.NoError(t, worker.processLifecyclePolicies(ctx))
	assert.Equal(t, "STANDARD", classOf(t, objects, "k", first.VersionID))
}
