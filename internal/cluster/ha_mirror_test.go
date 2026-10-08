package cluster

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
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

type localNode struct {
	objects object.Manager
	buckets bucket.Manager
	store   metadata.Store
}

// newLocalNode is a real object manager over a temporary store.
func newLocalNode(t *testing.T) *localNode {
	t.Helper()
	root := t.TempDir()
	backend, err := storage.NewFilesystemBackend(storage.Config{Root: root})
	require.NoError(t, err)
	store, err := metadata.NewPebbleStore(metadata.PebbleOptions{
		DataDir: filepath.Join(root, "metadata"),
		Logger:  logrus.StandardLogger(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return &localNode{
		objects: object.NewManager(backend, store, config.StorageConfig{
			Backend: "filesystem", Root: root, EncryptionKey: strings.Repeat("ab", 32),
		}),
		buckets: bucket.NewManager(backend, store),
		store:   store,
	}
}

// newClusterWithPeers initialises a cluster of the given factor whose peers
// are the given servers, all healthy.
func newClusterWithPeers(t *testing.T, factor int, peers ...*httptest.Server) (*Manager, *sql.DB, []*Node) {
	t.Helper()
	db, cleanup := setupQuorumTestDB(t)
	t.Cleanup(cleanup)
	ctx := context.Background()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	_, err := mgr.InitializeCluster(ctx, "local-node", "us-east-1", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, factor))
	var nodes []*Node
	for i, p := range peers {
		n := &Node{Name: "peer-" + string(rune('a'+i)), Endpoint: p.URL, NodeToken: "t", Region: "us-east-1", Priority: 100, Metadata: "{}"}
		require.NoError(t, mgr.AddNode(ctx, n))
		_, err = db.ExecContext(ctx, `UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, HealthStatusHealthy, n.ID)
		require.NoError(t, err)
		nodes = append(nodes, n)
	}
	return mgr, db, nodes
}

func missedSince(t *testing.T, db *sql.DB, nodeID string) sql.NullInt64 {
	t.Helper()
	var since sql.NullInt64
	require.NoError(t, db.QueryRow(`SELECT replica_missed_since FROM cluster_nodes WHERE id = ?`, nodeID).Scan(&since))
	return since
}

func failingPeer(t *testing.T, hits *int) *httptest.Server {
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		*hits++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Two nodes with a factor of 2 are a mirror: with the peer down, writes go on
// and the peer is recorded as having missed the earliest of them.
func TestHAMirrorKeepsWritingWithItsPeerDown(t *testing.T) {
	local := newLocalNode(t)
	ctx := context.Background()
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "mirror"}))
	hits := 0
	mgr, db, nodes := newClusterWithPeers(t, 2, failingPeer(t, &hits))
	ha := NewHAObjectManager(local.objects, mgr)

	first, err := ha.PutObject(ctx, "mirror", "a", strings.NewReader("first"), http.Header{})
	require.NoError(t, err, "the write succeeds with the peer failing")
	assert.Equal(t, 1, hits)
	peer, err := mgr.GetNode(ctx, nodes[0].ID)
	require.NoError(t, err)
	assert.Equal(t, HealthStatusUnavailable, peer.HealthStatus)
	assert.Equal(t, first.LastModified.Unix(), missedSince(t, db, nodes[0].ID).Int64)

	time.Sleep(1100 * time.Millisecond)
	_, err = ha.PutObject(ctx, "mirror", "b", strings.NewReader("second"), http.Header{})
	require.NoError(t, err, "the write succeeds with the peer down")
	assert.Equal(t, 1, hits, "a peer known to be down is not tried")
	assert.Equal(t, first.LastModified.Unix(), missedSince(t, db, nodes[0].ID).Int64, "the earliest miss is kept")

	for _, key := range []string{"a", "b"} {
		_, reader, err := local.objects.GetObject(ctx, "mirror", key)
		require.NoError(t, err)
		reader.Close()
	}
}

// A factor of 3 still needs one of its two peers.
func TestHAFactorThreeNeedsAPeer(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	ctx := context.Background()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	_, err := mgr.InitializeCluster(ctx, "local-node", "us-east-1", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 3))
	ok, err := mgr.ClusterCanAcceptWrites(ctx)
	require.NoError(t, err)
	assert.False(t, ok)
}

// A health check that finds a node healthy hands its missed writes to the
// hook once, and clears them; an unhealthy check keeps them.
func TestHealthCheckHandsBackAMissedReplica(t *testing.T) {
	healthy := true
	var mu sync.Mutex
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ok := healthy
		mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer peer.Close()
	mgr, db, nodes := newClusterWithPeers(t, 2, peer)
	ctx := context.Background()
	type call struct {
		node  string
		since time.Time
	}
	var calls []call
	mgr.OnReplicaBack(func(nodeID string, since time.Time) { calls = append(calls, call{nodeID, since}) })

	_, err := mgr.CheckNodeHealth(ctx, nodes[0].ID)
	require.NoError(t, err)
	assert.Empty(t, calls, "nothing was missed")

	missed := time.Now().Add(-time.Hour).Unix()
	_, err = db.Exec(`UPDATE cluster_nodes SET replica_missed_since = ? WHERE id = ?`, missed, nodes[0].ID)
	require.NoError(t, err)
	mu.Lock()
	healthy = false
	mu.Unlock()
	_, err = mgr.CheckNodeHealth(ctx, nodes[0].ID)
	require.NoError(t, err)
	assert.Empty(t, calls, "the node is still down")
	assert.Equal(t, missed, missedSince(t, db, nodes[0].ID).Int64)

	mu.Lock()
	healthy = true
	mu.Unlock()
	_, err = mgr.CheckNodeHealth(ctx, nodes[0].ID)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, nodes[0].ID, calls[0].node)
	assert.Equal(t, missed, calls[0].since.Unix())
	assert.False(t, missedSince(t, db, nodes[0].ID).Valid, "the record is cleared")

	_, err = mgr.CheckNodeHealth(ctx, nodes[0].ID)
	require.NoError(t, err)
	assert.Len(t, calls, 1, "handed back once")
}

// catchUpPeer answers checksum-batch from what it holds, and records the
// objects pushed to it and the deletes sent to it.
type catchUpPeer struct {
	mu      sync.Mutex
	holds   map[string]ChecksumEntry // key → what the peer holds
	pushed  map[string]http.Header   // key → headers of the push
	deletes []http.Header
	fail    bool
}

func newCatchUpPeer(t *testing.T) (*catchUpPeer, *httptest.Server) {
	p := &catchUpPeer{holds: map[string]ChecksumEntry{}, pushed: map[string]http.Header{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/ha/checksum-batch"):
			var req struct {
				Keys []string `json:"keys"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			var out struct {
				Entries []ChecksumEntry `json:"entries"`
			}
			for _, k := range req.Keys {
				e, ok := p.holds[k]
				if !ok {
					e = ChecksumEntry{Key: k}
				}
				out.Entries = append(out.Entries, e)
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPut:
			_, _ = io.Copy(io.Discard, r.Body)
			key := r.URL.Path[strings.Index(r.URL.Path, "/ha/objects/")+len("/ha/objects/"):]
			p.pushed[key] = r.Header.Clone()
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete:
			h := r.Header.Clone()
			h.Set("X-Test-Key", r.URL.Path[strings.Index(r.URL.Path, "/ha/objects/")+len("/ha/objects/"):])
			p.deletes = append(p.deletes, h)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return p, srv
}

// A catch-up pushes what changed since the node went missing, lock state
// included, and nothing older, then sends the deletes it missed.
func TestCatchUpPushesOnlyWhatTheNodeMissed(t *testing.T) {
	local := newLocalNode(t)
	ctx := context.Background()
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:       "worm",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
		ObjectLock: &metadata.ObjectLockMetadata{Enabled: true},
	}))
	old := object.WithReplicatedLastModified(ctx, time.Now().Add(-2*time.Hour))
	_, err := local.objects.PutObject(old, "worm", "before", strings.NewReader("old"), http.Header{})
	require.NoError(t, err)
	until := time.Now().Add(24 * time.Hour).UTC()
	h := http.Header{}
	h.Set("x-amz-object-lock-mode", object.RetentionModeCompliance)
	h.Set("x-amz-object-lock-retain-until-date", until.Format(time.RFC3339))
	h.Set("x-amz-object-lock-legal-hold", object.LegalHoldStatusOn)
	_, err = local.objects.PutObject(ctx, "worm", "during", strings.NewReader("new"), h)
	require.NoError(t, err)
	_, err = local.objects.PutObject(old, "worm", "removed", strings.NewReader("gone"), http.Header{})
	require.NoError(t, err)
	_, err = local.objects.DeleteObject(ctx, "worm", "removed", false)
	require.NoError(t, err)

	peer, srv := newCatchUpPeer(t)
	peer.holds["removed"] = ChecksumEntry{Key: "removed", Found: true, LastModified: time.Now().Add(-2 * time.Hour).Unix()}
	mgr, db, nodes := newClusterWithPeers(t, 2, srv)
	require.NoError(t, RecordDeletion(ctx, db, EntityTypeObject, ObjectTombstoneID("worm", "removed"), "local", time.Now().Unix()))
	scrubber := NewAntiEntropyScrubber(local.objects, local.buckets, mgr, newFakeRawKV())
	scrubber.CatchUp(nodes[0].ID, time.Now().Add(-10*time.Minute))
	scrubber.runCatchUp(ctx)

	peer.mu.Lock()
	defer peer.mu.Unlock()
	require.Contains(t, peer.pushed, "during")
	assert.NotContains(t, peer.pushed, "before", "written before the node went missing")
	require.Len(t, peer.deletes, 1, "the delete made while the node was missing")
	assert.Equal(t, "removed", peer.deletes[0].Get("X-Test-Key"))
	pushed := peer.pushed["during"]
	assert.Equal(t, "true", pushed.Get(HAObjectLockHeader))
	assert.Equal(t, object.RetentionModeCompliance, pushed.Get("x-amz-object-lock-mode"))
	assert.Equal(t, object.LegalHoldStatusOn, pushed.Get("x-amz-object-lock-legal-hold"))
}

// A catch-up that cannot reach the node leaves the miss recorded, so the next
// health check retries it.
func TestCatchUpThatFailsIsRetried(t *testing.T) {
	local := newLocalNode(t)
	ctx := context.Background()
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "retry"}))
	_, err := local.objects.PutObject(ctx, "retry", "k", strings.NewReader("x"), http.Header{})
	require.NoError(t, err)
	peer, srv := newCatchUpPeer(t)
	peer.fail = true
	mgr, db, nodes := newClusterWithPeers(t, 2, srv)
	scrubber := NewAntiEntropyScrubber(local.objects, local.buckets, mgr, newFakeRawKV())
	since := time.Now().Add(-10 * time.Minute)
	scrubber.CatchUp(nodes[0].ID, since)
	scrubber.runCatchUp(ctx)
	assert.Equal(t, since.Unix(), missedSince(t, db, nodes[0].ID).Int64)
}

// Deletes the node missed are sent to it: a version delete always, a key
// delete only while the key is still deleted here and the node's copy is not
// newer than the delete.
func TestCatchUpReplaysMissedDeletes(t *testing.T) {
	local := newLocalNode(t)
	ctx := context.Background()
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: "vb", Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
	}))
	put := func(key string) {
		_, err := local.objects.PutObject(ctx, "vb", key, strings.NewReader(key), http.Header{})
		require.NoError(t, err)
	}
	del := func(key string) string {
		marker, err := local.objects.DeleteObject(ctx, "vb", key, false)
		require.NoError(t, err)
		return marker
	}
	put("gone")
	goneMarker := del("gone")
	put("newer-there")
	del("newer-there")
	put("alive")
	del("alive")
	put("alive")
	put("absent-there")
	del("absent-there")
	put("old")
	del("old")
	put("same-second")
	del("same-second")

	peer, srv := newCatchUpPeer(t)
	mgr, db, nodes := newClusterWithPeers(t, 2, srv)
	now := time.Now().Unix()
	tomb := func(entityType, id string, at int64) {
		require.NoError(t, RecordDeletion(ctx, db, entityType, id, "local", at))
	}
	tomb(EntityTypeObjectVersion, ObjectVersionTombstoneID("vb", "versioned", "v123"), now)
	for _, key := range []string{"gone", "newer-there", "alive", "absent-there", "same-second"} {
		tomb(EntityTypeObject, ObjectTombstoneID("vb", key), now)
	}
	tomb(EntityTypeObject, ObjectTombstoneID("vb", "old"), now-7200)
	peer.holds["gone"] = ChecksumEntry{Key: "gone", Found: true, LastModified: now - 60}
	peer.holds["newer-there"] = ChecksumEntry{Key: "newer-there", Found: true, LastModified: now + 60}
	peer.holds["alive"] = ChecksumEntry{Key: "alive", Found: true, LastModified: now - 60}
	peer.holds["old"] = ChecksumEntry{Key: "old", Found: true, LastModified: now - 9000}
	peer.holds["same-second"] = ChecksumEntry{Key: "same-second", Found: true, LastModified: now}

	scrubber := NewAntiEntropyScrubber(local.objects, local.buckets, mgr, newFakeRawKV())
	node, err := mgr.GetNode(ctx, nodes[0].ID)
	require.NoError(t, err)
	localID, err := mgr.GetLocalNodeID(ctx)
	require.NoError(t, err)
	require.NoError(t, scrubber.replayDeletes(ctx, NewProxyClient(nil), node, localID, time.Unix(now-600, 0)))

	peer.mu.Lock()
	defer peer.mu.Unlock()
	sent := map[string]http.Header{}
	for _, h := range peer.deletes {
		sent[h.Get("X-Test-Key")] = h
	}
	require.Contains(t, sent, "versioned")
	assert.Equal(t, "v123", sent["versioned"].Get(HAObjectVersionHeader))
	require.Contains(t, sent, "gone")
	assert.Equal(t, goneMarker, sent["gone"].Get(HADeleteMarkerVersionHeader), "the marker keeps its ID on the node")
	assert.NotContains(t, sent, "newer-there", "the node wrote it after the delete")
	assert.NotContains(t, sent, "alive", "written again here after the delete")
	assert.NotContains(t, sent, "absent-there", "the node does not hold it")
	assert.NotContains(t, sent, "old", "deleted before the node went missing")
	assert.NotContains(t, sent, "same-second", "the node wrote it in the second of the delete")
	assert.Len(t, sent, 2)
}

// A node that is down again when the catch-up starts keeps its record, so it
// is caught up when it is next back — even while another peer is healthy and
// the rest of the catch-up runs.
func TestCatchUpWaitsForANodeDownAgain(t *testing.T) {
	local := newLocalNode(t)
	ctx := context.Background()
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "again"}))
	_, err := local.objects.PutObject(ctx, "again", "k", strings.NewReader("x"), http.Header{})
	require.NoError(t, err)
	down, downSrv := newCatchUpPeer(t)
	up, upSrv := newCatchUpPeer(t)
	mgr, db, nodes := newClusterWithPeers(t, 3, downSrv, upSrv)
	_, err = db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, HealthStatusUnavailable, nodes[0].ID)
	require.NoError(t, err)
	scrubber := NewAntiEntropyScrubber(local.objects, local.buckets, mgr, newFakeRawKV())
	since := time.Now().Add(-10 * time.Minute)
	scrubber.CatchUp(nodes[0].ID, since)
	scrubber.CatchUp(nodes[1].ID, since)
	scrubber.runCatchUp(ctx)

	assert.Equal(t, since.Unix(), missedSince(t, db, nodes[0].ID).Int64)
	assert.False(t, missedSince(t, db, nodes[1].ID).Valid, "the healthy peer was caught up")
	down.mu.Lock()
	assert.Empty(t, down.pushed)
	down.mu.Unlock()
	up.mu.Lock()
	assert.Contains(t, up.pushed, "k")
	up.mu.Unlock()
}

// A write that skips a peer already known to be down records the miss.
func TestHAWriteSkippingADownPeerRecordsTheMiss(t *testing.T) {
	local := newLocalNode(t)
	ctx := context.Background()
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "skip"}))
	hits := 0
	mgr, db, nodes := newClusterWithPeers(t, 2, failingPeer(t, &hits))
	_, err := db.Exec(`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, HealthStatusDegraded, nodes[0].ID)
	require.NoError(t, err)

	before := time.Now().Unix()
	obj, err := NewHAObjectManager(local.objects, mgr).PutObject(ctx, "skip", "k", strings.NewReader("x"), http.Header{})
	require.NoError(t, err)
	assert.Zero(t, hits)
	// The miss is recorded before the write is made: a catch-up from then
	// covers it.
	missed := missedSince(t, db, nodes[0].ID)
	require.True(t, missed.Valid)
	assert.GreaterOrEqual(t, missed.Int64, before)
	assert.LessOrEqual(t, missed.Int64, obj.LastModified.Unix())
}

// The initial sync to a new replica carries the object's lock state.
func TestInitialSyncCarriesObjectLock(t *testing.T) {
	received := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		received <- r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	mgr, _, nodes := newClusterWithPeers(t, 2, srv)
	until := time.Now().Add(time.Hour).UTC()
	w := NewHASyncWorker(&lockedObjectManager{obj: &object.Object{
		Key: "k", Size: 4,
		Retention: &object.RetentionConfig{Mode: object.RetentionModeGovernance, RetainUntilDate: until},
		LegalHold: &object.LegalHoldConfig{Status: object.LegalHoldStatusOn},
	}}, nil, mgr, nil)
	require.NoError(t, w.syncKey(context.Background(), NewProxyClient(nil), nodes[0], "local", "bucket", "k"))
	got := <-received
	assert.Equal(t, "true", got.Get(HAObjectLockHeader))
	assert.Equal(t, object.RetentionModeGovernance, got.Get("x-amz-object-lock-mode"))
	assert.Equal(t, object.LegalHoldStatusOn, got.Get("x-amz-object-lock-legal-hold"))
}

// A copy pulled from a peer keeps the peer's lock state as sent, a date that
// passed in transit included.
func TestPulledCopyKeepsThePeersObjectLock(t *testing.T) {
	local := newLocalNode(t)
	ctx := context.Background()
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:       "worm",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
		ObjectLock: &metadata.ObjectLockMetadata{Enabled: true},
	}))
	passed := time.Now().Add(-time.Minute).UTC()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		SetHAObjectLock(w.Header(), &object.Object{
			Retention: &object.RetentionConfig{Mode: object.RetentionModeCompliance, RetainUntilDate: passed},
			LegalHold: &object.LegalHoldConfig{Status: object.LegalHoldStatusOn},
		})
		w.Header().Set(HAObjectVersionHeader, "1700000000.peer")
		_, _ = w.Write([]byte("peer copy"))
	}))
	defer srv.Close()
	mgr, _, nodes := newClusterWithPeers(t, 2, srv)
	scrubber := NewAntiEntropyScrubber(local.objects, local.buckets, mgr, newFakeRawKV())
	node, err := mgr.GetNode(ctx, nodes[0].ID)
	require.NoError(t, err)
	require.NoError(t, scrubber.pullObjectFromPeer(ctx, NewProxyClient(nil), node, "local", "worm", "k"))

	obj, reader, err := local.objects.GetObject(ctx, "worm", "k")
	require.NoError(t, err)
	reader.Close()
	require.NotNil(t, obj.Retention)
	assert.Equal(t, object.RetentionModeCompliance, obj.Retention.Mode)
	assert.True(t, obj.Retention.RetainUntilDate.Equal(passed), "stored %v for %v", obj.Retention.RetainUntilDate, passed)
	require.NotNil(t, obj.LegalHold)
	assert.Equal(t, object.LegalHoldStatusOn, obj.LegalHold.Status)
	assert.Equal(t, "1700000000.peer", obj.VersionID)
}

// A version this node deleted is not stored again when pulled from a peer that
// missed the delete.
func TestAPulledCopyOfAVersionDeletedHereIsNotStored(t *testing.T) {
	local := newLocalNode(t)
	ctx := context.Background()
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: "pulled", Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
	}))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(HAObjectVersionHeader, "1700000000.deleted")
		_, _ = w.Write([]byte("peer copy"))
	}))
	defer srv.Close()
	mgr, _, nodes := newClusterWithPeers(t, 2, srv)
	require.NoError(t, RecordDeletion(ctx, mgr.db, EntityTypeObjectVersion, ObjectVersionTombstoneID("pulled", "k", "1700000000.deleted"), "local", time.Now().Unix()))
	scrubber := NewAntiEntropyScrubber(local.objects, local.buckets, mgr, newFakeRawKV())
	require.NoError(t, scrubber.pullObjectFromPeer(ctx, NewProxyClient(nil), nodes[0], "local", "pulled", "k"))

	_, err := local.objects.GetObjectMetadata(ctx, "pulled", "k")
	assert.ErrorIs(t, err, object.ErrObjectNotFound)
}

// unlistedVersions is an object manager that cannot list versions.
type unlistedVersions struct {
	object.Manager
}

func (unlistedVersions) GetObjectVersions(context.Context, string, string) ([]object.ObjectVersion, error) {
	return nil, fmt.Errorf("unreadable")
}

// A key whose versions cannot be listed here, or that the peer fails to take,
// is left unreconciled, so that the catch-up is made again.
func TestVersionsNotComparedOrNotSentLeaveTheKeyUnreconciled(t *testing.T) {
	ctx := context.Background()
	local := newLocalNode(t)
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: "v", Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
	}))
	obj, err := local.objects.PutObject(ctx, "v", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(ChecksumResponse{Versions: true, Entries: []ChecksumEntry{{
				Key: "k", Found: true, ETag: obj.ETag, Size: obj.Size, LastModified: obj.LastModified.Unix()}}})
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		http.Error(w, "failing", http.StatusInternalServerError)
	}))
	defer srv.Close()
	mgr, _, nodes := newClusterWithPeers(t, 2, srv)
	listed, err := local.objects.ListObjects(ctx, "v", "", "", "", 10)
	require.NoError(t, err)

	for name, objects := range map[string]object.Manager{"not sent": local.objects, "not listed": unlistedVersions{local.objects}} {
		scrubber := NewAntiEntropyScrubber(objects, local.buckets, mgr, newFakeRawKV())
		cp := &ScrubCheckpoint{}
		scrubber.processBatch(ctx, NewProxyClient(nil), nodes, "local", "v", listed.Objects, cp, 0)
		assert.EqualValues(t, 1, cp.Unreconciled, name)
	}
}

// syncRequest is what a replica received from the initial sync.
type syncRequest struct {
	method, key, version, marker, markedAt string
}

// The initial sync to a new replica copies every version of every key, oldest
// first, with its ID — delete markers as delete markers, keys hidden by one
// included — and a key of a bucket without versions as its one object.
func TestInitialSyncCopiesEveryVersionOldestFirst(t *testing.T) {
	local := newLocalNode(t)
	ctx := context.Background()
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{
		Name: "vsync", Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
	}))
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "usync"}))
	put := func(bucket, key, body string) string {
		obj, err := local.objects.PutObject(ctx, bucket, key, strings.NewReader(body), http.Header{})
		require.NoError(t, err)
		time.Sleep(1100 * time.Millisecond)
		return obj.VersionID
	}
	del := func(bucket, key string) string {
		marker, err := local.objects.DeleteObject(ctx, bucket, key, false)
		require.NoError(t, err)
		time.Sleep(1100 * time.Millisecond)
		return marker
	}
	v1 := put("vsync", "k", "one")
	v2 := put("vsync", "k", "two")
	m1 := del("vsync", "k")
	v3 := put("vsync", "k", "three")
	g1 := put("vsync", "gone", "hidden")
	gm := del("vsync", "gone")
	put("usync", "u", "plain")

	var mu sync.Mutex
	var got []syncRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		key := r.URL.Path[strings.Index(r.URL.Path, "/ha/objects/")+len("/ha/objects/"):]
		mu.Lock()
		req := syncRequest{r.Method, r.Header.Get(HABucketHeader) + "/" + key,
			r.Header.Get(HAObjectVersionHeader), r.Header.Get(HADeleteMarkerVersionHeader), ""}
		if r.Method == http.MethodDelete {
			req.markedAt = r.Header.Get(HALastModifiedHeader)
		}
		got = append(got, req)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	mgr, _, nodes := newClusterWithPeers(t, 2, srv)
	w := NewHASyncWorker(local.objects, local.buckets, mgr, local.store)
	require.NoError(t, w.runSync(ctx, 0, nodes[0], "", ""))

	markedAt := func(key, marker string) string {
		versions, err := local.objects.GetObjectVersions(ctx, "vsync", key)
		require.NoError(t, err)
		for _, v := range versions {
			if v.VersionID == marker {
				return fmt.Sprintf("%d", v.LastModified.Unix())
			}
		}
		t.Fatalf("marker %s not found", marker)
		return ""
	}
	perKey := map[string][]syncRequest{}
	for _, r := range got {
		perKey[r.key] = append(perKey[r.key], r)
	}
	require.Equal(t, []syncRequest{
		{"PUT", "vsync/k", v1, "", ""}, {"PUT", "vsync/k", v2, "", ""},
		{"DELETE", "vsync/k", "", m1, markedAt("k", m1)}, {"PUT", "vsync/k", v3, "", ""},
	}, perKey["vsync/k"])
	require.Equal(t, []syncRequest{
		{"PUT", "vsync/gone", g1, "", ""}, {"DELETE", "vsync/gone", "", gm, markedAt("gone", gm)},
	}, perKey["vsync/gone"], "a key hidden by a delete marker keeps its versions")
	require.Equal(t, []syncRequest{{"PUT", "usync/u", "", "", ""}}, perKey["usync/u"])
}

// A bucket whose versioning was suspended keeps the versions from before and
// an object without a version ID written since: the sync sends the versions,
// then that object, with the attributes its headers cannot carry.
func TestInitialSyncCopiesTheObjectWrittenWhileSuspended(t *testing.T) {
	local := newLocalNode(t)
	ctx := context.Background()
	b := &metadata.BucketMetadata{Name: "ssync", Versioning: &metadata.VersioningMetadata{Status: "Enabled"}}
	require.NoError(t, local.store.CreateBucket(ctx, b))
	v1, err := local.objects.PutObject(ctx, "ssync", "k", strings.NewReader("before"), http.Header{})
	require.NoError(t, err)
	time.Sleep(1100 * time.Millisecond)
	b.Versioning.Status = "Suspended"
	require.NoError(t, local.store.UpdateBucket(ctx, b))
	current, err := local.objects.PutObject(ctx, "ssync", "k", strings.NewReader("since"), http.Header{})
	require.NoError(t, err)
	require.Empty(t, current.VersionID)
	require.NoError(t, local.objects.SetObjectTagging(ctx, "ssync", "k", &object.TagSet{Tags: []object.Tag{{Key: "kept", Value: "yes"}}}))

	var mu sync.Mutex
	var got []syncRequest
	var attributes string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		got = append(got, syncRequest{method: r.Method, version: r.Header.Get(HAObjectVersionHeader)})
		if r.Header.Get(HAObjectVersionHeader) == "" {
			attributes = r.Header.Get(HAObjectAttributesHeader)
		}
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	mgr, _, nodes := newClusterWithPeers(t, 2, srv)
	w := NewHASyncWorker(local.objects, local.buckets, mgr, local.store)
	require.NoError(t, w.runSync(ctx, 0, nodes[0], "", ""))

	require.Equal(t, []syncRequest{{method: "PUT", version: v1.VersionID}, {method: "PUT"}}, got)
	data, err := base64.StdEncoding.DecodeString(attributes)
	require.NoError(t, err)
	var sent object.ReplicatedAttributes
	require.NoError(t, json.Unmarshal(data, &sent))
	require.Equal(t, current.ETag, sent.ETag)
	require.Equal(t, []object.Tag{{Key: "kept", Value: "yes"}}, sent.Tags.Tags)
}

// unreadableBucket lists every bucket but one.
type unreadableBucket struct {
	keyLister
	bucket string
}

func (u unreadableBucket) ListObjects(ctx context.Context, bucket, prefix, marker string, maxKeys int) ([]*metadata.ObjectMetadata, string, error) {
	if bucket == u.bucket {
		return nil, "", fmt.Errorf("unreadable")
	}
	return u.keyLister.ListObjects(ctx, bucket, prefix, marker, maxKeys)
}

// A bucket the initial synchronization cannot list is left to the catch-up,
// which compares every object: the node is recorded as having missed writes
// since the start, whatever else it failed to send after.
func TestAnInitialSyncThatCannotListABucketLeavesItToTheCatchUp(t *testing.T) {
	ctx := context.Background()
	local := newLocalNode(t)
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "a-unreadable"}))
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b-readable"}))
	for _, o := range []struct{ bucket, key string }{{"a-unreadable", "k"}, {"b-readable", "k"}, {"b-readable", "refused"}} {
		_, err := local.objects.PutObject(ctx, o.bucket, o.key, strings.NewReader("data"), http.Header{})
		require.NoError(t, err)
	}
	var sent sync.Map
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if strings.HasSuffix(r.URL.Path, "/refused") {
			http.Error(w, "failing", http.StatusInternalServerError)
			return
		}
		sent.Store(r.Header.Get(HABucketHeader), true)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	mgr, _, nodes := newClusterWithPeers(t, 2, srv)

	w := NewHASyncWorker(local.objects, local.buckets, mgr, unreadableBucket{local.store, "a-unreadable"})
	require.NoError(t, w.runSync(ctx, 0, nodes[0], "", ""))
	_, readable := sent.Load("b-readable")
	assert.True(t, readable)
	var since sql.NullInt64
	require.NoError(t, mgr.db.QueryRow(`SELECT replica_missed_since FROM cluster_nodes WHERE id = ?`, nodes[0].ID).Scan(&since))
	require.True(t, since.Valid)
	assert.LessOrEqual(t, since.Int64, int64(1))
}

// A node that refuses a delete, because the object's lock keeps it there, is
// healthy and is not recorded as having missed it; one that fails is.
func TestARefusedDeleteIsNotAMissedWrite(t *testing.T) {
	for _, c := range []struct {
		status int
		missed bool
	}{{http.StatusConflict, false}, {http.StatusInternalServerError, true}} {
		local := newLocalNode(t)
		ctx := context.Background()
		require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "mirror"}))
		peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			if r.Method == http.MethodDelete {
				w.WriteHeader(c.status)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(peer.Close)
		mgr, db, nodes := newClusterWithPeers(t, 2, peer)
		ha := NewHAObjectManager(local.objects, mgr)
		_, err := ha.PutObject(ctx, "mirror", "k", strings.NewReader("data"), http.Header{})
		require.NoError(t, err)

		_, err = ha.DeleteObject(ctx, "mirror", "k", false)
		require.NoError(t, err)
		n, err := mgr.GetNode(ctx, nodes[0].ID)
		require.NoError(t, err)
		assert.Equal(t, c.missed, n.HealthStatus != HealthStatusHealthy, "status %d", c.status)
		assert.Equal(t, c.missed, n.UnavailableSince != nil, "status %d: the dead-node threshold counts from the failure", c.status)
		assert.Equal(t, c.missed, missedSince(t, db, nodes[0].ID).Valid, "status %d", c.status)
	}
}
