package lifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
)

func daysAgo(days int) time.Time {
	return time.Now().AddDate(0, 0, -days)
}

// A version is noncurrent from when a later one was written: its days are
// counted from then, not from its own write.
func TestNoncurrentDaysCountFromTheNextVersion(t *testing.T) {
	objMgr := &mockObjectMgr{}
	store := &mockMetaStore{versions: []*metadata.ObjectVersion{
		{Key: "k", VersionID: "v2", ETag: "e2", LastModified: daysAgo(100)},
		{Key: "k", VersionID: "v3", ETag: "e3", LastModified: daysAgo(1), IsLatest: true},
		{Key: "k", VersionID: "v1", ETag: "e1", LastModified: daysAgo(200)},
	}}
	worker := NewWorker(&mockBucketMgr{}, objMgr, store)
	worker.processNoncurrentVersionExpiration(context.Background(), "b", bucket.LifecycleRule{
		ID: "r", Status: "Enabled",
		NoncurrentVersionExpiration: &bucket.NoncurrentVersionExpiration{NoncurrentDays: 30},
	})
	assert.Equal(t, []string{"v1"}, objMgr.deletedVersionIDs, "v2 has been noncurrent for a day")
}

// The newest noncurrent versions a rule keeps are not expired, however old;
// a delete marker is not one of them.
func TestTheNewestNoncurrentVersionsAreKept(t *testing.T) {
	objMgr := &mockObjectMgr{}
	store := &mockMetaStore{versions: []*metadata.ObjectVersion{
		{Key: "k", VersionID: "v1", ETag: "e1", LastModified: daysAgo(50)},
		{Key: "k", VersionID: "v2", ETag: "e2", LastModified: daysAgo(40)},
		{Key: "k", VersionID: "marker", LastModified: daysAgo(35)},
		{Key: "k", VersionID: "v3", ETag: "e3", LastModified: daysAgo(30)},
		{Key: "k", VersionID: "v4", ETag: "e4", LastModified: daysAgo(20), IsLatest: true},
	}}
	worker := NewWorker(&mockBucketMgr{}, objMgr, store)
	worker.processNoncurrentVersionExpiration(context.Background(), "b", bucket.LifecycleRule{
		ID: "r", Status: "Enabled",
		NoncurrentVersionExpiration: &bucket.NoncurrentVersionExpiration{NoncurrentDays: 1, NewerNoncurrentVersions: 2},
	})
	assert.ElementsMatch(t, []string{"marker", "v1"}, objMgr.deletedVersionIDs, "v3 and v2 are kept")
}

// A rule's filter limits the noncurrent versions it expires.
func TestTheNoncurrentVersionsExpiredAreThoseTheFilterTakes(t *testing.T) {
	objMgr := &mockObjectMgr{}
	store := &mockMetaStore{versions: []*metadata.ObjectVersion{
		{Key: "k", VersionID: "small", ETag: "e1", Size: 10, LastModified: daysAgo(50)},
		{Key: "k", VersionID: "large", ETag: "e2", Size: 1000, LastModified: daysAgo(40)},
		{Key: "k", VersionID: "current", ETag: "e3", Size: 10, LastModified: daysAgo(30), IsLatest: true},
	}}
	above := int64(100)
	worker := NewWorker(&mockBucketMgr{}, objMgr, store)
	worker.processNoncurrentVersionExpiration(context.Background(), "b", bucket.LifecycleRule{
		ID: "r", Status: "Enabled",
		Filter:                      bucket.LifecycleFilter{ObjectSizeGreaterThan: &above},
		NoncurrentVersionExpiration: &bucket.NoncurrentVersionExpiration{NoncurrentDays: 1},
	})
	assert.Equal(t, []string{"large"}, objMgr.deletedVersionIDs)
}

// Of two versions written in one second, as those an earlier release wrote
// without the time of each write, the current one is the newest.
func TestTheCurrentVersionIsTheNewestOfOneSecond(t *testing.T) {
	objMgr := &mockObjectMgr{}
	at := daysAgo(40)
	store := &mockMetaStore{versions: []*metadata.ObjectVersion{
		{Key: "k", VersionID: "old", ETag: "e1", LastModified: at},
		{Key: "k", VersionID: "current", ETag: "e2", LastModified: at, IsLatest: true},
	}}
	worker := NewWorker(&mockBucketMgr{}, objMgr, store)
	worker.processNoncurrentVersionExpiration(context.Background(), "b", bucket.LifecycleRule{
		ID: "r", Status: "Enabled",
		NoncurrentVersionExpiration: &bucket.NoncurrentVersionExpiration{NoncurrentDays: 30},
	})
	assert.Equal(t, []string{"old"}, objMgr.deletedVersionIDs)
}
