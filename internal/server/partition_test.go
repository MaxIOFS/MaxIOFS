package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func holdsObject(s *Server, bucketName, key string) bool {
	_, err := s.metadataStore.GetObject(context.Background(), bucketName, key)
	return err == nil
}

func holdsUser(s *Server, id string) bool {
	_, err := s.authManager.GetUser(context.Background(), id)
	return err == nil
}

// Two nodes cut off from each other for longer than deletions are kept, both
// serving clients: each takes the writes made on the other side, and what one
// side deleted is deleted on the other, not brought back.
func TestALongPartitionConverges(t *testing.T) {
	p := newHAPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	both := []*Server{p.a, p.b}

	// What both sides held before the partition.
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "kept", "admin"))
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "dropped", "admin"))
	for _, k := range []string{"x", "stay"} {
		_, err := p.a.objectManager.PutObject(ctx, "kept", k, strings.NewReader(k), http.Header{})
		require.NoError(t, err)
	}
	require.True(t, holdsObject(p.b, "kept", "x"))
	share, err := p.a.shareManager.CreateShare(ctx, "kept", "stay", "", "AKID", "secret", "admin", nil)
	require.NoError(t, err)
	require.Equal(t, 1, rowCount(t, p.b, "shares", share.ID))
	now := time.Now().Unix()
	for _, s := range both {
		require.NoError(t, s.authManager.CreateUser(ctx, &auth.User{ID: "juan", Username: "juan", Status: auth.UserStatusActive,
			Roles: []string{auth.RoleUser}, CreatedAt: now, UpdatedAt: now}))
	}

	time.Sleep(1100 * time.Millisecond) // what follows comes after all of it
	p.aDown.Store(true)
	p.bDown.Store(true)

	// a deletes, b writes.
	_, err = p.a.objectManager.DeleteObject(ctx, "kept", "x", false)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	p.a.handleDeleteUser(w, asGlobalAdmin(httptest.NewRequest(http.MethodDelete, "/api/v1/users/juan", nil), map[string]string{"user": "juan"}))
	require.Less(t, w.Code, 300, w.Body.String())
	require.NoError(t, p.a.bucketManager.DeleteBucket(ctx, "", "dropped"))
	require.NoError(t, p.a.shareManager.DeleteShare(ctx, share.ID))

	_, err = p.b.objectManager.PutObject(ctx, "kept", "y", strings.NewReader("y"), http.Header{})
	require.NoError(t, err)
	require.NoError(t, p.b.authManager.CreateUser(ctx, &auth.User{ID: "pedro", Username: "pedro", Status: auth.UserStatusActive,
		Roles: []string{auth.RoleUser}, CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}))

	// The partition outlasts the time deletions are kept: every deletion is
	// older than that.
	for _, s := range both {
		cluster.ForgetOldDeletions(ctx, s.db, -time.Hour)
	}

	// Back together: each finds the other healthy and catches it up. The
	// health checks and synchronizations run as the server runs them, again
	// until they succeed.
	p.aDown.Store(false)
	p.bDown.Store(false)
	for _, s := range both {
		s.antiEntropyScrubber.Start(ctx)
	}
	cycle := func() {
		_, _ = p.a.clusterManager.CheckNodeHealth(ctx, p.bID)
		_, _ = p.b.clusterManager.CheckNodeHealth(ctx, p.aID)
		for _, s := range both {
			s.deletionLogSyncMgr.SyncNow(ctx)
			s.userSyncMgr.TriggerSync(ctx)
		}
	}

	// What each node must hold, and lacks.
	diverged := func() []string {
		var off []string
		for _, s := range both {
			name := "a"
			if s == p.b {
				name = "b"
			}
			for what, ok := range map[string]bool{
				"has the deleted object":        holdsObject(s, "kept", "x"),
				"has the deleted user":          holdsUser(s, "juan"),
				"has the deleted bucket":        hasBucket(s, "", "dropped"),
				"has the deleted share":         rowCount(t, s, "shares", share.ID) != 0,
				"lacks the object written on b": !holdsObject(s, "kept", "y"),
				"lacks the user created on b":   !holdsUser(s, "pedro"),
				"lacks what nobody touched":     !holdsObject(s, "kept", "stay"),
			} {
				if ok {
					off = append(off, name+" "+what)
				}
			}
		}
		return off
	}
	require.Eventually(t, func() bool {
		cycle()
		return len(diverged()) == 0
	}, 30*time.Second, time.Second, "the nodes do not converge")

	// Another round of every synchronization, a full comparison of the objects
	// included, brings nothing back.
	for _, s := range both {
		s.userSyncMgr.TriggerSync(ctx)
		s.globalConfigSyncMgr.SyncNow(ctx)
	}
	p.a.antiEntropyScrubber.CatchUp(p.bID, time.Unix(1, 0))
	p.b.antiEntropyScrubber.CatchUp(p.aID, time.Unix(1, 0))
	assert.Never(t, func() bool { return len(diverged()) > 0 }, 3*time.Second, 100*time.Millisecond, "%v", diverged())

	// Every catch-up ended: nothing is held back for either node any longer.
	assert.Eventually(t, func() bool {
		cycle()
		return !hasRow(t, p.a, `SELECT COUNT(*) FROM cluster_nodes WHERE replica_catchup_since IS NOT NULL OR replica_missed_since IS NOT NULL`) &&
			!hasRow(t, p.b, `SELECT COUNT(*) FROM cluster_nodes WHERE replica_catchup_since IS NOT NULL OR replica_missed_since IS NOT NULL`)
	}, 30*time.Second, time.Second)
}

// A bucket deleted on one side of a partition and written to on the other is
// kept on every node with what was written after its deletion, and without
// what it held before.
func TestABucketDeletedOnOneSideAndWrittenOnTheOther(t *testing.T) {
	p := newHAPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "contested", "admin"))
	_, err := p.a.objectManager.PutObject(ctx, "contested", "old", strings.NewReader("old"), http.Header{})
	require.NoError(t, err)
	require.True(t, holdsObject(p.b, "contested", "old"))

	time.Sleep(1100 * time.Millisecond)
	p.aDown.Store(true)
	p.bDown.Store(true)
	require.NoError(t, p.a.bucketManager.ForceDeleteBucket(ctx, "", "contested"))
	time.Sleep(1100 * time.Millisecond) // b writes after the deletion
	_, err = p.b.objectManager.PutObject(ctx, "contested", "late", strings.NewReader("late"), http.Header{})
	require.NoError(t, err)

	p.aDown.Store(false)
	p.bDown.Store(false)
	for _, s := range []*Server{p.a, p.b} {
		s.antiEntropyScrubber.Start(ctx)
	}

	// The health checks run as the periodic checker runs them; a catch-up that
	// arrives before the other side's is retried at the next one.
	assert.Eventually(t, func() bool {
		_, _ = p.a.clusterManager.CheckNodeHealth(ctx, p.bID)
		_, _ = p.b.clusterManager.CheckNodeHealth(ctx, p.aID)
		return hasBucket(p.a, "", "contested") && holdsObject(p.a, "contested", "late") &&
			hasBucket(p.b, "", "contested") && holdsObject(p.b, "contested", "late") &&
			!holdsObject(p.b, "contested", "old") && !holdsObject(p.a, "contested", "old")
	}, 20*time.Second, 50*time.Millisecond, "a: bucket %v late %v old %v; b: bucket %v late %v old %v",
		hasBucket(p.a, "", "contested"), holdsObject(p.a, "contested", "late"), holdsObject(p.a, "contested", "old"),
		hasBucket(p.b, "", "contested"), holdsObject(p.b, "contested", "late"), holdsObject(p.b, "contested", "old"))
}

// A bucket kept for what was written to it after another node deleted it is
// sent to the other nodes at once, as a bucket created then.
func TestABucketKeptForLaterWritesIsSentAtOnce(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "kept-later", "admin"))
	require.True(t, hasBucket(p.b, "", "kept-later"))
	p.bDown.Store(true)
	require.NoError(t, p.a.bucketManager.DeleteBucket(ctx, "", "kept-later"))
	p.bDown.Store(false)
	deletedAt := time.Now().UnixNano()
	time.Sleep(1100 * time.Millisecond)
	_, err := p.b.objectManager.(*cluster.HAObjectManager).Manager.PutObject(ctx, "kept-later", "late", strings.NewReader("late"), http.Header{})
	require.NoError(t, err)

	require.NoError(t, p.b.bucketStateReceiver.Apply(ctx, &cluster.BucketState{Name: "kept-later", DeletedAt: deletedAt}))
	assert.True(t, hasBucket(p.b, "", "kept-later"))
	assert.True(t, hasBucket(p.a, "", "kept-later"), "sent without waiting for a catch-up")
}

// An object written, deleted and written again within one second while a
// node is away keeps the last write on every node. The writes' ETags order
// them the other way ("final" before "draft"), so a tie decided by ETag would
// keep the first.
func TestARewriteInTheSecondOfItsDeletionIsKept(t *testing.T) {
	p := newHAPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "quick", "admin"))
	var first, last time.Time
	for attempt := 0; ; attempt++ {
		require.Less(t, attempt, 20, "could not fit the writes in one second")
		p.bDown.Store(false)
		markHealthy(t, p.a, p.bID)
		obj, err := p.a.objectManager.PutObject(ctx, "quick", "k", strings.NewReader("draft"), http.Header{})
		require.NoError(t, err)
		first = obj.LastModified
		p.bDown.Store(true)
		_, err = p.a.objectManager.DeleteObject(ctx, "quick", "k", false)
		require.NoError(t, err)
		obj, err = p.a.objectManager.PutObject(ctx, "quick", "k", strings.NewReader("final"), http.Header{})
		require.NoError(t, err)
		last = obj.LastModified
		if first.Unix() == last.Unix() {
			break
		}
	}
	require.Equal(t, "draft", readBody(t, p.b, "quick", "k"), "b has the first write only")

	p.bDown.Store(false)
	p.a.antiEntropyScrubber.Start(ctx)
	assert.Eventually(t, func() bool {
		_, _ = p.a.clusterManager.CheckNodeHealth(ctx, p.bID)
		return holdsObject(p.b, "quick", "k") && readBody(t, p.b, "quick", "k") == "final"
	}, 20*time.Second, time.Second, "b takes the last write")
	p.a.antiEntropyScrubber.CatchUp(p.bID, time.Unix(1, 0))
	assert.Never(t, func() bool {
		return !holdsObject(p.a, "quick", "k") || readBody(t, p.a, "quick", "k") != "final"
	}, 5*time.Second, 200*time.Millisecond, "a full comparison keeps the last write on a")
}

// A delete marker reaches the other node with the time it was written.
func TestADeleteMarkerReachesTheOtherNodeWithItsWriteTime(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "marked", "admin"))
	require.NoError(t, p.a.bucketManager.SetVersioning(ctx, "", "marked", &bucket.VersioningConfig{Status: "Enabled"}))
	_, err := p.a.objectManager.PutObject(ctx, "marked", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	markerID, err := p.a.objectManager.DeleteObject(ctx, "marked", "k", false)
	require.NoError(t, err)
	writtenAt := func(s *Server) int64 {
		t.Helper()
		versions, err := s.metadataStore.GetObjectVersions(ctx, "marked", "k")
		require.NoError(t, err)
		for _, v := range versions {
			if v.VersionID == markerID {
				return v.WrittenAt
			}
		}
		return -1
	}
	require.Positive(t, writtenAt(p.a))
	assert.Equal(t, writtenAt(p.a), writtenAt(p.b))
}
