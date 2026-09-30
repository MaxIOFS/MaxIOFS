package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// haPair is two complete nodes of a cluster whose replication factor is 2,
// each able to reach the other. bDown makes b answer every request from a
// node with 503; bRefused counts those answers.
type haPair struct {
	a, b     *Server
	aID, bID string
	bDown    atomic.Bool
	bRefused atomic.Int32
}

func newHAPair(t *testing.T) *haPair {
	t.Helper()
	p := &haPair{a: newClusterTestNode(t), b: newClusterTestNode(t)}
	ctx := context.Background()
	tsA := httptest.NewServer(p.a.clusterServer.Handler)
	t.Cleanup(tsA.Close)
	bRoutes := p.b.clusterServer.Handler
	tsB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.bDown.Load() {
			p.bRefused.Add(1)
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		bRoutes.ServeHTTP(w, r)
	}))
	t.Cleanup(tsB.Close)

	token, err := p.a.clusterManager.InitializeCluster(ctx, "a", "us-east-1", tsA.URL)
	require.NoError(t, err)
	_, err = p.b.clusterManager.InitializeCluster(ctx, "b", "us-east-1", tsB.URL)
	require.NoError(t, err)
	p.aID, err = p.a.clusterManager.GetLocalNodeID(ctx)
	require.NoError(t, err)
	p.bID, err = p.b.clusterManager.GetLocalNodeID(ctx)
	require.NoError(t, err)
	// The nodes of a cluster share its token.
	_, err = p.b.db.ExecContext(ctx, `UPDATE cluster_config SET cluster_token = ?`, token)
	require.NoError(t, err)
	_, err = p.b.db.ExecContext(ctx, `UPDATE cluster_nodes SET node_token = ? WHERE id = ?`, token, p.bID)
	require.NoError(t, err)
	for _, link := range []struct {
		s    *Server
		peer *cluster.Node
	}{
		{p.a, &cluster.Node{ID: p.bID, Name: "b", Endpoint: tsB.URL, NodeToken: token, Region: "us-east-1", Priority: 100, Metadata: "{}"}},
		{p.b, &cluster.Node{ID: p.aID, Name: "a", Endpoint: tsA.URL, NodeToken: token, Region: "us-east-1", Priority: 100, Metadata: "{}"}},
	} {
		require.NoError(t, link.s.clusterManager.AddNode(ctx, link.peer))
		_, err := link.s.db.ExecContext(ctx, `UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusHealthy, link.peer.ID)
		require.NoError(t, err)
		require.NoError(t, link.s.clusterManager.SetReplicationFactor(ctx, 2))
	}
	return p
}

func bucketOn(t *testing.T, s *Server, tenantID, name string) *metadata.BucketMetadata {
	t.Helper()
	b, err := s.metadataStore.GetBucket(context.Background(), tenantID, name)
	require.NoError(t, err, "bucket %s", name)
	return b
}

func hasBucket(s *Server, tenantID, name string) bool {
	_, err := s.metadataStore.GetBucket(context.Background(), tenantID, name)
	return err == nil
}

// missedSince is when node, as s knows it, first missed a write, or nil.
func missedSince(t *testing.T, s *Server, nodeID string) *int64 {
	t.Helper()
	var since *int64
	require.NoError(t, s.db.QueryRow(`SELECT replica_missed_since FROM cluster_nodes WHERE id = ?`, nodeID).Scan(&since))
	return since
}

func clearMissed(t *testing.T, s *Server, nodeID string) {
	t.Helper()
	_, err := s.db.Exec(`UPDATE cluster_nodes SET replica_missed_since = NULL WHERE id = ?`, nodeID)
	require.NoError(t, err)
}

// A bucket created on one node exists on the other with the same times,
// configuration and ACL; each node counts its own usage. A change and a
// deletion follow, and a bucket created again right after its deletion exists
// again.
func TestHABucketChangesReachTheOtherNode(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	a := p.a.bucketManager
	require.NoError(t, a.CreateBucket(ctx, "", "shared", "admin"))
	require.NoError(t, a.SetVersioning(ctx, "", "shared", &bucket.VersioningConfig{Status: "Enabled"}))
	require.NoError(t, a.SetBucketTags(ctx, "", "shared", map[string]string{"team": "blue"}))
	granted := acl.CreateDefaultACL("another-owner", "Another")
	require.NoError(t, a.SetBucketACL(ctx, "", "shared", granted))

	onA, onB := bucketOn(t, p.a, "", "shared"), bucketOn(t, p.b, "", "shared")
	assert.True(t, onB.CreatedAt.Equal(onA.CreatedAt))
	assert.True(t, onB.UpdatedAt.Equal(onA.UpdatedAt))
	require.NotNil(t, onB.Versioning)
	assert.Equal(t, "Enabled", onB.Versioning.Status)
	assert.Equal(t, "blue", onB.Tags["team"])
	aclOnB, err := p.b.bucketManager.GetBucketACL(ctx, "", "shared")
	require.NoError(t, err)
	assert.Equal(t, granted, aclOnB)

	_, err = p.a.objectManager.PutObject(ctx, "shared", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	assert.True(t, bucketOn(t, p.a, "", "shared").UpdatedAt.Equal(onA.UpdatedAt), "a write is not a change to the bucket")
	require.NoError(t, a.SetQuota(ctx, "", "shared", &metadata.BucketQuota{MaxSizeBytes: 1 << 20}))
	onA, onB = bucketOn(t, p.a, "", "shared"), bucketOn(t, p.b, "", "shared")
	require.NotNil(t, onB.Quota)
	assert.EqualValues(t, 1<<20, onB.Quota.MaxSizeBytes)
	assert.EqualValues(t, 1, onA.ObjectCount)
	assert.Zero(t, onB.ObjectCount, "each node counts what it holds")
	assert.Zero(t, onB.TotalSize)

	require.NoError(t, a.CreateBucket(ctx, "", "brief", "admin"))
	require.True(t, hasBucket(p.b, "", "brief"))
	require.NoError(t, a.DeleteBucket(ctx, "", "brief"))
	assert.False(t, hasBucket(p.b, "", "brief"))
	require.NoError(t, a.CreateBucket(ctx, "", "brief", "admin"))
	assert.True(t, hasBucket(p.b, "", "brief"), "created again right after its deletion")

	require.NoError(t, a.ForceDeleteBucket(ctx, "", "shared"))
	assert.False(t, hasBucket(p.b, "", "shared"), "a forced deletion reaches the other node too")
}

// Every change to a bucket reaches the other node, which then holds the same
// version of it.
func TestHAEveryBucketChangeReachesTheOtherNode(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "every", "admin"))
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "every-logs", "admin"))
	days := 1
	var bucketInfo *bucket.Bucket
	for _, step := range []struct {
		name   string
		change func(m bucket.Manager) error
	}{
		{"policy", func(m bucket.Manager) error {
			return m.SetBucketPolicy(ctx, "", "every", &bucket.Policy{Version: "2012-10-17", Statement: []bucket.Statement{{
				Effect: "Allow", Principal: "*", Action: "s3:GetObject", Resource: "arn:aws:s3:::every/*"}}})
		}},
		{"policy removed", func(m bucket.Manager) error { return m.DeleteBucketPolicy(ctx, "", "every") }},
		{"versioning", func(m bucket.Manager) error {
			return m.SetVersioning(ctx, "", "every", &bucket.VersioningConfig{Status: "Enabled"})
		}},
		{"lifecycle", func(m bucket.Manager) error {
			return m.SetLifecycle(ctx, "", "every", &bucket.LifecycleConfig{Rules: []bucket.LifecycleRule{{
				ID: "r", Status: "Enabled", Expiration: &bucket.LifecycleExpiration{Days: &days}}}})
		}},
		{"lifecycle removed", func(m bucket.Manager) error { return m.DeleteLifecycle(ctx, "", "every") }},
		{"cors", func(m bucket.Manager) error {
			return m.SetCORS(ctx, "", "every", &bucket.CORSConfig{CORSRules: []bucket.CORSRule{{
				AllowedMethods: []string{"GET"}, AllowedOrigins: []string{"*"}}}})
		}},
		{"cors removed", func(m bucket.Manager) error { return m.DeleteCORS(ctx, "", "every") }},
		{"website", func(m bucket.Manager) error {
			return m.SetWebsite(ctx, "", "every", &bucket.WebsiteConfig{IndexDocument: "index.html"})
		}},
		{"website removed", func(m bucket.Manager) error { return m.DeleteWebsite(ctx, "", "every") }},
		{"encryption", func(m bucket.Manager) error {
			return m.SetEncryption(ctx, "", "every", &bucket.EncryptionConfig{Type: "AES256"})
		}},
		{"encryption removed", func(m bucket.Manager) error { return m.DeleteEncryption(ctx, "", "every") }},
		{"public access block", func(m bucket.Manager) error {
			return m.SetPublicAccessBlock(ctx, "", "every", &bucket.PublicAccessBlock{BlockPublicAcls: true})
		}},
		{"public access block removed", func(m bucket.Manager) error { return m.DeletePublicAccessBlock(ctx, "", "every") }},
		{"ownership controls", func(m bucket.Manager) error {
			return m.SetOwnershipControls(ctx, "", "every", &bucket.OwnershipControlsConfig{ObjectOwnership: "BucketOwnerEnforced"})
		}},
		{"ownership controls removed", func(m bucket.Manager) error { return m.DeleteOwnershipControls(ctx, "", "every") }},
		{"logging", func(m bucket.Manager) error {
			return m.SetLogging(ctx, "", "every", &bucket.LoggingConfig{TargetBucket: "every-logs", TargetPrefix: "logs/"})
		}},
		{"logging removed", func(m bucket.Manager) error { return m.DeleteLogging(ctx, "", "every") }},
		{"notification", func(m bucket.Manager) error {
			return m.SetNotification(ctx, "", "every", &bucket.NotificationConfig{})
		}},
		{"notification removed", func(m bucket.Manager) error { return m.DeleteNotification(ctx, "", "every") }},
		{"tags", func(m bucket.Manager) error { return m.SetBucketTags(ctx, "", "every", map[string]string{"k": "v"}) }},
		{"object lock", func(m bucket.Manager) error {
			return m.SetObjectLockConfig(ctx, "", "every", &bucket.ObjectLockConfig{ObjectLockEnabled: true})
		}},
		{"quota", func(m bucket.Manager) error {
			return m.SetQuota(ctx, "", "every", &metadata.BucketQuota{MaxObjectCount: 10})
		}},
		{"quota removed", func(m bucket.Manager) error { return m.DeleteQuota(ctx, "", "every") }},
		{"acl", func(m bucket.Manager) error {
			return m.SetBucketACL(ctx, "", "every", acl.CreateDefaultACL("someone", "Someone"))
		}},
		{"whole bucket", func(m bucket.Manager) error {
			var err error
			if bucketInfo, err = m.GetBucketInfo(ctx, "", "every"); err != nil {
				return err
			}
			bucketInfo.OwnerID = "new-owner"
			return m.UpdateBucket(ctx, "", "every", bucketInfo)
		}},
	} {
		before := bucketOn(t, p.a, "", "every").UpdatedAt
		require.NoError(t, step.change(p.a.bucketManager), step.name)
		onA, onB := bucketOn(t, p.a, "", "every"), bucketOn(t, p.b, "", "every")
		require.True(t, onA.UpdatedAt.After(before), "%s changes the bucket", step.name)
		assert.True(t, onB.UpdatedAt.Equal(onA.UpdatedAt), "%s reaches the other node", step.name)
	}
	assert.Equal(t, "new-owner", bucketOn(t, p.b, "", "every").OwnerID)
}

// A change made on either node reaches the other. A version older than the
// one a node holds does not replace it.
func TestHABucketChangesGoBothWays(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "both", "admin"))
	before := bucketOn(t, p.b, "", "both")

	require.NoError(t, p.b.bucketManager.SetVersioning(ctx, "", "both", &bucket.VersioningConfig{Status: "Enabled"}))
	onA := bucketOn(t, p.a, "", "both")
	require.NotNil(t, onA.Versioning)
	assert.Equal(t, "Enabled", onA.Versioning.Status)

	require.NoError(t, p.a.bucketStateReceiver.Apply(ctx, &cluster.BucketState{Bucket: before}))
	onA = bucketOn(t, p.a, "", "both")
	require.NotNil(t, onA.Versioning, "an older version does not replace a newer one")
	assert.Equal(t, "Enabled", onA.Versioning.Status)
}

// A node that misses bucket changes, known to be down or failing to take
// them, is recorded as having missed a write, and is sent every bucket and
// every deletion when the other node synchronizes it; so is a bucket created
// before buckets were replicated. A bucket the node refuses does not stop the
// others, and a hidden copy left by a migration is not sent.
func TestHABucketMissedWhileDownIsCaughtUp(t *testing.T) {
	p := newHAPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "gone", "admin"))
	require.True(t, hasBucket(p.b, "", "gone"))

	_, err := p.a.db.ExecContext(ctx, `UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusUnavailable, p.bID)
	require.NoError(t, err)
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "unsent", "admin"))
	require.False(t, hasBucket(p.b, "", "unsent"))
	require.NotNil(t, missedSince(t, p.a, p.bID), "a node known to be down is recorded as having missed the change")
	clearMissed(t, p.a, p.bID)
	_, err = p.a.db.ExecContext(ctx, `UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, cluster.HealthStatusHealthy, p.bID)
	require.NoError(t, err)

	p.bDown.Store(true)
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "late", "admin"))
	require.NoError(t, p.a.bucketManager.SetVersioning(ctx, "", "late", &bucket.VersioningConfig{Status: "Enabled"}))
	require.NoError(t, p.a.bucketManager.DeleteBucket(ctx, "", "gone"))
	require.False(t, hasBucket(p.b, "", "late"))
	require.True(t, hasBucket(p.b, "", "gone"))
	require.NotNil(t, missedSince(t, p.a, p.bID), "a node that fails to take the change is recorded as having missed it")

	p.bDown.Store(false)
	require.NoError(t, p.b.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: "older", OwnerID: "admin"}))
	require.NoError(t, p.b.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: "aaa-dup", TenantID: "t1", OwnerID: "admin"}))
	require.NoError(t, p.a.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: "aaa-dup", OwnerID: "admin"}))
	require.NoError(t, p.a.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: "hidden", OwnerID: "admin"}))
	hide(t, p.a, "hidden", "job-1")
	p.a.antiEntropyScrubber.Start(ctx)
	p.b.antiEntropyScrubber.Start(ctx)
	require.Eventually(t, func() bool {
		return hasBucket(p.b, "", "late") && hasBucket(p.b, "", "unsent") && !hasBucket(p.b, "", "gone") && hasBucket(p.a, "", "older")
	}, 10*time.Second, 20*time.Millisecond)
	late := bucketOn(t, p.b, "", "late")
	require.NotNil(t, late.Versioning)
	assert.Equal(t, "Enabled", late.Versioning.Status)
	assert.False(t, hasBucket(p.b, "", "aaa-dup"), "the name is another tenant's there")
	assert.False(t, hasBucket(p.b, "", "hidden"))
}

// A deletion reported by another node removes this node's copy with what it
// holds, unless the copy changed after the deletion, holds a write made after
// it, or holds data under a legal hold. A version older than the deletion does
// not bring the bucket back; a newer one does.
func TestHABucketDeletionKeepsNewerData(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	deleteAt := func(name string, at time.Time) {
		t.Helper()
		require.NoError(t, s.bucketStateReceiver.Apply(ctx, &cluster.BucketState{Name: name, DeletedAt: at.UnixNano()}))
	}
	put := func(bucketName string, h http.Header) {
		t.Helper()
		_, err := s.objectManager.PutObject(ctx, bucketName, "k", strings.NewReader("data"), h)
		require.NoError(t, err)
	}

	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "removed", "admin"))
	put("removed", http.Header{})
	deleteAt("removed", time.Now().Add(time.Second))
	assert.False(t, hasBucket(s, "", "removed"), "a copy holding only what the deletion covered is removed")

	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "written", "admin"))
	before := time.Now()
	put("written", http.Header{})
	deleteAt("written", before)
	assert.True(t, hasBucket(s, "", "written"), "a write in the second of the deletion or later is kept")

	require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "changed", "admin"))
	deleteAt("changed", bucketOn(t, s, "", "changed").UpdatedAt.Add(-time.Nanosecond))
	assert.True(t, hasBucket(s, "", "changed"), "a change made after the deletion is kept")

	require.NoError(t, s.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: "held", OwnerID: "admin",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"}, ObjectLock: &metadata.ObjectLockMetadata{Enabled: true}}))
	put("held", http.Header{"X-Amz-Object-Lock-Legal-Hold": {"ON"}})
	deleteAt("held", time.Now().Add(time.Second))
	assert.True(t, hasBucket(s, "", "held"), "data under a legal hold is kept")

	stale := bucketOn(t, s, "", "written")
	stale.Name = "removed"
	stale.UpdatedAt = time.Now().Add(-time.Hour)
	require.NoError(t, s.bucketStateReceiver.Apply(ctx, &cluster.BucketState{Bucket: stale}))
	assert.False(t, hasBucket(s, "", "removed"), "a version older than the deletion does not bring it back")
	fresh := *stale
	fresh.UpdatedAt = time.Now().Add(time.Minute)
	require.NoError(t, s.bucketStateReceiver.Apply(ctx, &cluster.BucketState{Bucket: &fresh}))
	assert.True(t, hasBucket(s, "", "removed"), "a newer version does")
}

// A bucket whose name another tenant holds on the node is refused, and so is a
// message that names no bucket.
func TestHABucketStateRefusals(t *testing.T) {
	s := newClusterTestNode(t)
	ctx := context.Background()
	require.NoError(t, s.metadataStore.CreateBucket(ctx, &metadata.BucketMetadata{Name: "dup", TenantID: "t1", OwnerID: "u"}))
	send := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/internal/cluster/ha/bucket-state", strings.NewReader(body))
		w := httptest.NewRecorder()
		s.handleHABucketState(w, req)
		return w.Code
	}

	assert.Equal(t, http.StatusConflict, send(`{"bucket":{"name":"dup","updated_at":"2026-01-01T00:00:00Z"}}`))
	assert.False(t, hasBucket(s, "", "dup"))
	assert.Equal(t, http.StatusBadRequest, send(`{}`))
	assert.Equal(t, http.StatusBadRequest, send(`{"bucket":{"updated_at":"2026-01-01T00:00:00Z"}}`), "a bucket has a name")
	assert.Equal(t, http.StatusBadRequest, send(`{"name":"dup"}`), "a deletion names its time")
	assert.Equal(t, http.StatusBadRequest, send(`not json`))
	assert.True(t, hasBucket(s, "t1", "dup"))
	assert.Equal(t, http.StatusNoContent, send(`{"bucket":{"name":"fresh","updated_at":"2026-01-01T00:00:00Z"}}`))
	assert.True(t, hasBucket(s, "", "fresh"))
}

type fakeReplication struct {
	enabled bool
	factor  int
	err     error
}

func (f fakeReplication) IsClusterEnabled() bool { return f.enabled }
func (f fakeReplication) GetReplicationFactor(context.Context) (int, error) {
	return f.factor, f.err
}

type fakeLeader bool

func (f fakeLeader) IsLeader() bool { return bool(f) }

// A node runs the jobs that act on every bucket outside a cluster whose
// buckets are on every node, and only as the coordinator inside one.
func TestClusterJobsRunOnTheCoordinator(t *testing.T) {
	for _, c := range []struct {
		name    string
		repl    fakeReplication
		leader  bool
		expires bool
	}{
		{"no cluster", fakeReplication{}, false, true},
		{"factor 1", fakeReplication{enabled: true, factor: 1}, false, true},
		{"factor 2, coordinator", fakeReplication{enabled: true, factor: 2}, true, true},
		{"factor 2, not coordinator", fakeReplication{enabled: true, factor: 2}, false, false},
		{"factor unknown", fakeReplication{enabled: true, factor: 1, err: errors.New("unreadable")}, true, false},
	} {
		assert.Equal(t, c.expires, runsClusterJobsHere(c.repl, fakeLeader(c.leader))(), c.name)
	}
}

// A node that was down when a bucket was created and written to is caught up
// with the bucket, then with its objects: the objects have a bucket to go into.
func TestHAObjectsFollowTheirBucket(t *testing.T) {
	p := newHAPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p.bDown.Store(true)
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "data", "admin"))
	_, err := p.a.objectManager.PutObject(ctx, "data", "k", strings.NewReader("payload"), http.Header{})
	require.NoError(t, err)
	require.False(t, hasBucket(p.b, "", "data"))
	clearMissed(t, p.a, p.bID)
	refused := p.bRefused.Load()
	p.a.antiEntropyScrubber.Start(ctx)
	require.Eventually(t, func() bool { return p.bRefused.Load() > refused }, 10*time.Second, 10*time.Millisecond,
		"the synchronization at start-up reaches the node while it is down")
	require.Eventually(t, func() bool { return missedSince(t, p.a, p.bID) != nil }, 10*time.Second, 10*time.Millisecond,
		"and records it as having missed a write")

	p.bDown.Store(false)
	p.a.antiEntropyScrubber.CatchUp(p.bID, time.Now().Add(-time.Minute))
	require.Eventually(t, func() bool {
		_, err := p.b.metadataStore.GetObject(ctx, "data", "k")
		return err == nil
	}, 10*time.Second, 20*time.Millisecond)
	assert.Equal(t, "payload", readBody(t, p.b, "data", "k"))
}

// A new replica is sent every bucket before the objects that go into them.
func TestHANewReplicaGetsTheBucketsFirst(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	p.bDown.Store(true)
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "seed", "admin"))
	_, err := p.a.objectManager.PutObject(ctx, "seed", "k", strings.NewReader("seeded"), http.Header{})
	require.NoError(t, err)

	p.bDown.Store(false)
	p.a.haSyncWorker.Trigger(ctx)
	require.Eventually(t, func() bool {
		_, err := p.b.metadataStore.GetObject(ctx, "seed", "k")
		return err == nil
	}, 10*time.Second, 20*time.Millisecond)
	assert.Equal(t, "seeded", readBody(t, p.b, "seed", "k"))
}

// With every node holding every bucket, a node that is not the coordinator
// leaves an expired object to it; a node outside such a cluster expires it.
func TestLifecycleOnANodeThatIsNotTheCoordinator(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	days := 1
	expiring := func(s *Server) {
		t.Helper()
		require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "expiring", "admin"))
		require.NoError(t, s.bucketManager.SetLifecycle(ctx, "", "expiring", &bucket.LifecycleConfig{Rules: []bucket.LifecycleRule{{
			ID: "expire", Status: "Enabled", Expiration: &bucket.LifecycleExpiration{Days: &days}}}}))
		old := object.WithReplicatedLastModified(object.WithReplicaCopy(ctx), time.Now().AddDate(0, 0, -3))
		_, err := s.objectManager.PutObject(old, "expiring", "old", strings.NewReader("data"), http.Header{})
		require.NoError(t, err)
		s.lifecycleWorker.Start(ctx, 10*time.Millisecond)
	}
	present := func(s *Server) bool {
		_, err := s.metadataStore.GetObject(ctx, "expiring", "old")
		return err == nil
	}

	standalone := newClusterTestNode(t)
	expiring(standalone)
	require.Eventually(t, func() bool { return !present(standalone) }, 10*time.Second, 10*time.Millisecond)

	p := newHAPair(t)
	expiring(p.a)
	time.Sleep(300 * time.Millisecond)
	assert.True(t, present(p.a), "the coordinator expires it")
}
