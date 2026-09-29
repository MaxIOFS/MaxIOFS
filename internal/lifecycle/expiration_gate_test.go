package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
)

// With every node holding every bucket one node expires objects and the others
// receive its deletes: a node the gate stops expires nothing, current or
// noncurrent, and still aborts the multipart uploads started on it. The pass
// deletes through the manager set after the worker was built.
func TestLifecycleExpiresOnlyWhereTheGateAllows(t *testing.T) {
	ctx := context.Background()
	days := 1
	rules := &bucket.LifecycleConfig{Rules: []bucket.LifecycleRule{{
		ID: "all", Status: "Enabled",
		Expiration:                     &bucket.LifecycleExpiration{Days: &days},
		NoncurrentVersionExpiration:    &bucket.NoncurrentVersionExpiration{NoncurrentDays: 1},
		AbortIncompleteMultipartUpload: &bucket.LifecycleAbortIncompleteMultipartUpload{DaysAfterInitiation: 1},
	}}}
	old := time.Now().AddDate(0, 0, -3)
	newMgr := func() *mockObjectMgrWithMultipart {
		return &mockObjectMgrWithMultipart{
			mockObjectMgr: mockObjectMgr{listResult: &object.ListObjectsResult{Objects: []object.Object{{Key: "k", LastModified: old}}}},
			uploads:       []object.MultipartUpload{{UploadID: "stale", Initiated: old}},
		}
	}
	bucketMgr := &mockBucketMgr{buckets: []bucket.Bucket{{Name: "b"}}, getBucket: &bucket.Bucket{Lifecycle: rules}}
	store := &mockMetaStore{versions: []*metadata.ObjectVersion{
		{Key: "k", VersionID: "current", IsLatest: true, LastModified: old},
		{Key: "k", VersionID: "noncurrent", LastModified: old},
	}}

	built := newMgr()
	worker := NewWorker(bucketMgr, built, store)
	replicated := newMgr()
	worker.SetObjectManager(replicated)
	expires := false
	worker.SetExpirationGate(func() bool { return expires })

	worker.processLifecyclePolicies(ctx)
	assert.Zero(t, replicated.deleteCount, "a node the gate stops expires nothing")
	assert.Equal(t, []string{"stale"}, replicated.abortedIDs, "the uploads started on it are aborted")

	expires = true
	replicated.abortedIDs = nil
	worker.processLifecyclePolicies(ctx)
	assert.Equal(t, []string{"noncurrent"}, replicated.deletedVersionIDs)
	assert.Equal(t, 2, replicated.deleteCount, "the noncurrent version and the expired object")
	assert.Equal(t, []string{"stale"}, replicated.abortedIDs)
	assert.Zero(t, built.deleteCount+len(built.abortedIDs), "nothing goes through the manager the worker was built with")
	assert.NotNil(t, rules.Rules[0].Expiration, "the bucket's rules are left as they are")
	assert.NotNil(t, rules.Rules[0].NoncurrentVersionExpiration)
}
