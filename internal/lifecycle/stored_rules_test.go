package lifecycle

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storedRulesWorker is a worker over real stores holding bucket "lifecycle",
// with the rules given stored in it.
func storedRulesWorker(t *testing.T, rules ...bucket.LifecycleRule) (*Worker, object.Manager) {
	t.Helper()
	dir, err := os.MkdirTemp("", "maxiofs-lifecycle-test-*")
	require.NoError(t, err)
	backend, err := storage.NewFilesystemBackend(storage.Config{Root: dir})
	require.NoError(t, err)
	store, err := metadata.NewPebbleStore(metadata.PebbleOptions{DataDir: filepath.Join(dir, "metadata"), Logger: logrus.StandardLogger()})
	require.NoError(t, err)
	t.Cleanup(func() {
		store.Close()
		os.RemoveAll(dir)
	})
	buckets := bucket.NewManager(backend, store)
	objects := object.NewManager(backend, store, config.StorageConfig{Backend: "filesystem", Root: dir})
	ctx := context.Background()
	require.NoError(t, buckets.CreateBucket(ctx, "", "lifecycle", ""))
	require.NoError(t, buckets.SetLifecycle(ctx, "", "lifecycle", &bucket.LifecycleConfig{Rules: rules}))
	return NewWorker(buckets, objects, store), objects
}

func putObject(t *testing.T, objects object.Manager, key string, size int, tags map[string]string) {
	t.Helper()
	ctx := context.Background()
	_, err := objects.PutObject(ctx, "lifecycle", key, bytes.NewReader(make([]byte, size)), http.Header{})
	require.NoError(t, err)
	if len(tags) > 0 {
		set := &object.TagSet{}
		for k, v := range tags {
			set.Tags = append(set.Tags, object.Tag{Key: k, Value: v})
		}
		require.NoError(t, objects.SetObjectTagging(ctx, "lifecycle", key, set))
	}
}

func kept(t *testing.T, objects object.Manager, key string) bool {
	t.Helper()
	_, err := objects.GetObjectMetadata(context.Background(), "lifecycle", key)
	return err == nil
}

// A rule stored with only the removal of expired delete markers expires no
// object.
func TestARuleRemovingDeleteMarkersOnlyExpiresNoObject(t *testing.T) {
	yes := true
	worker, objects := storedRulesWorker(t, bucket.LifecycleRule{
		ID: "markers", Status: "Enabled",
		Expiration: &bucket.LifecycleExpiration{ExpiredObjectDeleteMarker: &yes},
	})
	putObject(t, objects, "k", 4, nil)
	require.NoError(t, worker.processLifecyclePolicies(context.Background()))
	assert.True(t, kept(t, objects, "k"))
}

// An expiration on a date expires nothing before that date.
func TestAnExpirationOnADateWaitsForIt(t *testing.T) {
	next := time.Now().UTC().AddDate(1, 0, 0)
	worker, objects := storedRulesWorker(t, bucket.LifecycleRule{
		ID: "until", Status: "Enabled",
		Expiration: &bucket.LifecycleExpiration{Date: &next},
	})
	putObject(t, objects, "k", 4, nil)
	require.NoError(t, worker.processLifecyclePolicies(context.Background()))
	assert.True(t, kept(t, objects, "k"), "the date has not come")

	worker.now = func() time.Time { return next.AddDate(0, 0, 1) }
	require.NoError(t, worker.processLifecyclePolicies(context.Background()))
	assert.False(t, kept(t, objects, "k"), "the date has come")
}

// A rule's filter by tags and size limits its expiration to the objects that
// carry every tag and are within the size.
func TestARuleExpiresOnlyWhatItsFilterTakes(t *testing.T) {
	day := 1
	ten, hundred := int64(10), int64(100)
	worker, objects := storedRulesWorker(t, bucket.LifecycleRule{
		ID: "temp", Status: "Enabled",
		Filter: bucket.LifecycleFilter{
			Prefix:                "logs/",
			Tags:                  []bucket.Tag{{Key: "kind", Value: "temp"}, {Key: "team", Value: "ops"}},
			ObjectSizeGreaterThan: &ten,
			ObjectSizeLessThan:    &hundred,
		},
		Expiration: &bucket.LifecycleExpiration{Days: &day},
	})
	putObject(t, objects, "logs/taken", 50, map[string]string{"kind": "temp", "team": "ops", "other": "x"})
	putObject(t, objects, "logs/one-tag", 50, map[string]string{"kind": "temp"})
	putObject(t, objects, "logs/another-value", 50, map[string]string{"kind": "keep", "team": "ops"})
	putObject(t, objects, "logs/small", 10, map[string]string{"kind": "temp", "team": "ops"})
	putObject(t, objects, "logs/large", 100, map[string]string{"kind": "temp", "team": "ops"})
	putObject(t, objects, "other/taken", 50, map[string]string{"kind": "temp", "team": "ops"})

	worker.now = func() time.Time { return time.Now().AddDate(0, 0, 3) }
	require.NoError(t, worker.processLifecyclePolicies(context.Background()))
	assert.False(t, kept(t, objects, "logs/taken"))
	for _, key := range []string{"logs/one-tag", "logs/another-value", "logs/small", "logs/large", "other/taken"} {
		assert.True(t, kept(t, objects, key), key)
	}
}
