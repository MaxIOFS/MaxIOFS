package cluster

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRawKV is a tiny in-memory RawKVStore used to verify checkpoint persistence.
type fakeRawKV struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newFakeRawKV() *fakeRawKV {
	return &fakeRawKV{data: make(map[string][]byte)}
}

func (f *fakeRawKV) GetRaw(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.data[key]
	if !ok {
		return nil, metadata.ErrNotFound
	}
	out := make([]byte, len(v))
	copy(out, v)
	return out, nil
}

func (f *fakeRawKV) PutRaw(_ context.Context, key string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(value))
	copy(cp, value)
	f.data[key] = cp
	return nil
}

func (f *fakeRawKV) DeleteRaw(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.data[key]; !ok {
		return metadata.ErrNotFound
	}
	delete(f.data, key)
	return nil
}

func (f *fakeRawKV) RawBatch(_ context.Context, sets map[string][]byte, deletes []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, v := range sets {
		cp := make([]byte, len(v))
		copy(cp, v)
		f.data[k] = cp
	}
	for _, k := range deletes {
		delete(f.data, k)
	}
	return nil
}

func (f *fakeRawKV) RawScan(_ context.Context, prefix, _ string, fn func(key string, val []byte) bool) error {
	f.mu.Lock()
	snapshot := make(map[string][]byte, len(f.data))
	for k, v := range f.data {
		snapshot[k] = v
	}
	f.mu.Unlock()
	for k, v := range snapshot {
		if len(k) < len(prefix) || k[:len(prefix)] != prefix {
			continue
		}
		if !fn(k, v) {
			return nil
		}
	}
	return nil
}

func (f *fakeRawKV) RawGC() error { return nil }

// ── classifyDivergence ────────────────────────────────────────────────────────

func TestClassifyDivergence_PeerMissing(t *testing.T) {
	local := &object.Object{ETag: "abc", Size: 100, LastModified: time.Now()}
	div, act := classifyDivergence(local, nil)
	assert.Equal(t, divPeerMissing, div)
	assert.Equal(t, actPushToPeer, act)

	div2, act2 := classifyDivergence(local, &ChecksumEntry{Key: "k", Found: false})
	assert.Equal(t, divPeerMissing, div2)
	assert.Equal(t, actPushToPeer, act2)
}

func TestClassifyDivergence_Identical(t *testing.T) {
	now := time.Now()
	local := &object.Object{ETag: "abc", Size: 100, LastModified: now}
	peer := &ChecksumEntry{Key: "k", Found: true, ETag: "abc", Size: 100, LastModified: now.Unix()}
	div, act := classifyDivergence(local, peer)
	assert.Equal(t, divNone, div)
	assert.Equal(t, actNone, act)
}

func TestClassifyDivergence_LocalNewer_Push(t *testing.T) {
	local := &object.Object{ETag: "newer", Size: 200, LastModified: time.Unix(2000, 0)}
	peer := &ChecksumEntry{Key: "k", Found: true, ETag: "older", Size: 100, LastModified: 1000}
	div, act := classifyDivergence(local, peer)
	assert.Equal(t, divLocalNewer, div)
	assert.Equal(t, actPushToPeer, act)
}

func TestClassifyDivergence_PeerNewer_Pull(t *testing.T) {
	local := &object.Object{ETag: "older", Size: 100, LastModified: time.Unix(1000, 0)}
	peer := &ChecksumEntry{Key: "k", Found: true, ETag: "newer", Size: 200, LastModified: 2000}
	div, act := classifyDivergence(local, peer)
	assert.Equal(t, divPeerNewer, div)
	assert.Equal(t, actPullFromPeer, act)
}

func TestClassifyDivergence_TieDifferentETag_NoAutoFix(t *testing.T) {
	local := &object.Object{ETag: "left", Size: 100, LastModified: time.Unix(1000, 0)}
	peer := &ChecksumEntry{Key: "k", Found: true, ETag: "right", Size: 100, LastModified: 1000}
	div, act := classifyDivergence(local, peer)
	assert.Equal(t, divTieDifferentETag, div)
	assert.Equal(t, actNone, act)
}

func TestClassifyDivergence_Multipart_SizeAndMtimeMatch_NoFix(t *testing.T) {
	// Multipart ETag (md5-N) — size and mtime within 1s tolerance.
	local := &object.Object{ETag: "abc-5", Size: 1024, LastModified: time.Unix(1000, 0)}
	peer := &ChecksumEntry{Key: "k", Found: true, ETag: "different-5", Size: 1024, LastModified: 1001}
	div, act := classifyDivergence(local, peer)
	assert.Equal(t, divNone, div)
	assert.Equal(t, actNone, act)
}

func TestClassifyDivergence_Multipart_SizeDiffers_LWW(t *testing.T) {
	local := &object.Object{ETag: "abc-5", Size: 2048, LastModified: time.Unix(2000, 0)}
	peer := &ChecksumEntry{Key: "k", Found: true, ETag: "abc-5", Size: 1024, LastModified: 1000}
	div, act := classifyDivergence(local, peer)
	assert.Equal(t, divLocalNewer, div)
	assert.Equal(t, actPushToPeer, act)
}

func TestClassifyDivergence_Multipart_MtimeDiffersBeyondTolerance_LWW(t *testing.T) {
	local := &object.Object{ETag: "abc-3", Size: 1024, LastModified: time.Unix(1000, 0)}
	peer := &ChecksumEntry{Key: "k", Found: true, ETag: "abc-3", Size: 1024, LastModified: 5000}
	div, act := classifyDivergence(local, peer)
	assert.Equal(t, divPeerNewer, div)
	assert.Equal(t, actPullFromPeer, act)
}

// ── isMultipartETag ───────────────────────────────────────────────────────────

func TestIsMultipartETag(t *testing.T) {
	cases := []struct {
		etag     string
		expected bool
	}{
		{"abc-5", true},
		{"deadbeef-100", true},
		{"abc", false},
		{"abc-", false},
		{"-5", false},
		{"abc-foo", false},
		{"", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.expected, isMultipartETag(c.etag), "etag=%q", c.etag)
	}
}

// ── Checkpoint persistence ────────────────────────────────────────────────────

func TestScrubCheckpoint_SaveAndLoad(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	rawKV := newFakeRawKV()
	scrubber := NewAntiEntropyScrubber(nil, nil, mgr, rawKV)

	cp := &ScrubCheckpoint{
		CycleID:          "cycle-1",
		StartedAt:        time.Now().UTC().Truncate(time.Second),
		BucketOrder:      []string{"a", "tenant/b", "c"},
		CurrentBucketIdx: 1,
		LastKey:          "foo/bar.png",
		BucketsScanned:   1,
		ObjectsCompared:  500,
		DivergencesFound: 3,
		DivergencesFixed: 3,
		RunID:            42,
	}

	scrubber.saveCheckpoint(context.Background(), cp)

	loaded := scrubber.loadCheckpoint(context.Background())
	require.NotNil(t, loaded)
	assert.Equal(t, cp.CycleID, loaded.CycleID)
	assert.Equal(t, cp.BucketOrder, loaded.BucketOrder)
	assert.Equal(t, cp.CurrentBucketIdx, loaded.CurrentBucketIdx)
	assert.Equal(t, cp.LastKey, loaded.LastKey)
	assert.Equal(t, cp.ObjectsCompared, loaded.ObjectsCompared)
	assert.Equal(t, cp.RunID, loaded.RunID)

	scrubber.deleteCheckpoint(context.Background())
	assert.Nil(t, scrubber.loadCheckpoint(context.Background()))
}

func TestScrubCheckpoint_LoadEmpty(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	scrubber := NewAntiEntropyScrubber(nil, nil, mgr, newFakeRawKV())

	assert.Nil(t, scrubber.loadCheckpoint(context.Background()))
}

func TestScrubCheckpoint_NilRawKV(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	scrubber := NewAntiEntropyScrubber(nil, nil, mgr, nil)

	// All checkpoint operations must be no-ops when the store is nil.
	scrubber.saveCheckpoint(context.Background(), &ScrubCheckpoint{CycleID: "x"})
	assert.Nil(t, scrubber.loadCheckpoint(context.Background()))
	scrubber.deleteCheckpoint(context.Background())
}

// ── Config readers ────────────────────────────────────────────────────────────

func TestScrubConfig_DefaultsWhenMissing(t *testing.T) {
	db, cleanup := setupTestDB(t) // bare schema, no cluster_global_config
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	scrubber := NewAntiEntropyScrubber(nil, nil, mgr, nil)

	ctx := context.Background()
	assert.True(t, scrubber.scrubEnabled(ctx))
	assert.Equal(t, time.Duration(defaultScrubIntervalHours)*time.Hour, scrubber.cycleInterval(ctx))
	assert.Equal(t, defaultScrubRateLimit, scrubber.cycleRateLimit(ctx))
	assert.Equal(t, defaultScrubBatchSize, scrubber.cycleBatchSize(ctx))
}

func TestScrubConfig_HonorsOverrides(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	scrubber := NewAntiEntropyScrubber(nil, nil, mgr, nil)
	ctx := context.Background()

	require.NoError(t, SetGlobalConfig(ctx, db, "ha.scrub_enabled", "false"))
	require.NoError(t, SetGlobalConfig(ctx, db, "ha.scrub_interval_hours", "6"))
	require.NoError(t, SetGlobalConfig(ctx, db, "ha.scrub_rate_limit", "10"))
	require.NoError(t, SetGlobalConfig(ctx, db, "ha.scrub_batch_size", "200"))

	assert.False(t, scrubber.scrubEnabled(ctx))
	assert.Equal(t, 6*time.Hour, scrubber.cycleInterval(ctx))
	assert.Equal(t, 10, scrubber.cycleRateLimit(ctx))
	assert.Equal(t, 200, scrubber.cycleBatchSize(ctx))
}

// ── runCycle no-ops ──────────────────────────────────────────────────────────

func TestRunCycle_ClusterDisabled_NoOp(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	scrubber := NewAntiEntropyScrubber(nil, nil, mgr, newFakeRawKV())

	// Must NOT panic and must NOT insert into ha_scrub_runs (table doesn't exist
	// without InitReplicationSchema; the early return guards against that).
	scrubber.runCycle(context.Background(), nil)
}

func TestRunCycle_FactorOne_NoOp(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	ctx := context.Background()

	_, err := mgr.InitializeCluster(ctx, "local-node", "us-east-1", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 1))

	scrubber := NewAntiEntropyScrubber(nil, nil, mgr, newFakeRawKV())
	scrubber.runCycle(ctx, nil)

	// No row should have been inserted because the cycle exited before beginCycle.
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ha_scrub_runs`).Scan(&n))
	assert.Equal(t, 0, n)
}

// ── ListRecentRuns / pruneRuns ───────────────────────────────────────────────

func TestListRecentRuns_OrderAndLimit(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	scrubber := NewAntiEntropyScrubber(nil, nil, mgr, nil)
	ctx := context.Background()

	// Insert 5 runs.
	now := time.Now()
	for i := 0; i < 5; i++ {
		_, err := db.ExecContext(ctx,
			`INSERT INTO ha_scrub_runs (cycle_id, started_at, status) VALUES (?, ?, 'done')`,
			"cycle-"+string(rune('a'+i)), now.Add(time.Duration(i)*time.Minute))
		require.NoError(t, err)
	}

	runs, err := scrubber.ListRecentRuns(ctx, 3)
	require.NoError(t, err)
	require.Len(t, runs, 3)
	// Newest first.
	assert.Equal(t, "cycle-e", runs[0].CycleID)
	assert.Equal(t, "cycle-d", runs[1].CycleID)
	assert.Equal(t, "cycle-c", runs[2].CycleID)
}

func TestPruneRuns_KeepsRecent(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	scrubber := NewAntiEntropyScrubber(nil, nil, mgr, nil)
	ctx := context.Background()

	// Insert more than the retention limit.
	now := time.Now()
	for i := 0; i < scrubRunsKeepRecent+5; i++ {
		_, err := db.ExecContext(ctx,
			`INSERT INTO ha_scrub_runs (cycle_id, started_at, status) VALUES (?, ?, 'done')`,
			"cycle", now)
		require.NoError(t, err)
	}

	scrubber.pruneRuns(ctx)

	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ha_scrub_runs`).Scan(&n))
	assert.Equal(t, scrubRunsKeepRecent, n)
}

// ── ScrubCheckpoint JSON round-trip ──────────────────────────────────────────

func TestScrubCheckpoint_JSONRoundTrip(t *testing.T) {
	cp := ScrubCheckpoint{
		CycleID:          "abc",
		StartedAt:        time.Now().UTC().Truncate(time.Second),
		BucketOrder:      []string{"x", "y/z"},
		CurrentBucketIdx: 1,
		LastKey:          "deeply/nested/key.dat",
		BucketsScanned:   1,
		ObjectsCompared:  10,
		DivergencesFound: 2,
		DivergencesFixed: 1,
		RunID:            7,
	}
	data, err := json.Marshal(cp)
	require.NoError(t, err)

	var back ScrubCheckpoint
	require.NoError(t, json.Unmarshal(data, &back))
	assert.Equal(t, cp, back)
}

// ── urlEscapeBucket ───────────────────────────────────────────────────────────

func TestURLEscapeBucket(t *testing.T) {
	assert.Equal(t, "tenant/bucket", urlEscapeBucket("tenant/bucket"))
	assert.Equal(t, "a%3Fb", urlEscapeBucket("a?b"))
	assert.Equal(t, "a%26b", urlEscapeBucket("a&b"))
	assert.Equal(t, "a%23b", urlEscapeBucket("a#b"))
	assert.Equal(t, "a%20b", urlEscapeBucket("a b"))
}
