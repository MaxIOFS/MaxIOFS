package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
)

// A bucket whose writes are held is left alone; the others are processed, each
// inside the gate for as long as its pass runs.
func TestLifecycleSkipsABucketWhoseWritesAreHeld(t *testing.T) {
	days := 1
	rules := &bucket.LifecycleConfig{Rules: []bucket.LifecycleRule{{
		ID: "expire", Status: "Enabled", Expiration: &bucket.LifecycleExpiration{Days: &days},
	}}}
	objMgr := &mockObjectMgr{listResult: &object.ListObjectsResult{Objects: []object.Object{
		{Key: "old", LastModified: time.Now().AddDate(0, 0, -2)},
	}}}
	bucketMgr := &mockBucketMgr{
		buckets:   []bucket.Bucket{{Name: "held"}, {Name: "free"}},
		getBucket: &bucket.Bucket{Lifecycle: rules},
	}
	worker := NewWorker(bucketMgr, objMgr, &mockMetaStore{})

	inside := map[string]int{}
	var deletesInside int
	worker.SetWriteGate(func(name string) (func(), bool) {
		if name == "held" {
			return nil, false
		}
		inside[name]++
		before := objMgr.deleteCount
		return func() {
			inside[name]--
			deletesInside += objMgr.deleteCount - before
		}, true
	})
	worker.processLifecyclePolicies(context.Background())

	assert.Equal(t, 1, objMgr.deleteCount, "only the bucket that takes writes is expired")
	assert.Equal(t, 1, deletesInside, "the expiration ran inside the gate")
	assert.Equal(t, map[string]int{"free": 0}, inside, "the pass left the gate")
}
