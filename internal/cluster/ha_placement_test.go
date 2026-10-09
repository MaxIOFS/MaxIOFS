package cluster

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setFreeSpace(t *testing.T, db *sql.DB, nodeID string, free int64) {
	t.Helper()
	_, err := db.Exec(`UPDATE cluster_nodes SET capacity_total = 1000, capacity_used = ? WHERE id = ?`, 1000-free, nodeID)
	require.NoError(t, err)
}

func answering(t *testing.T, status int, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if hits != nil {
			hits.Add(1)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A write whose copies fail is not sent to the nodes that were to hold its
// entry; they are recorded as having missed it too.
func TestAWriteWithoutItsCopiesIsMissedByTheNodesHoldingItsEntry(t *testing.T) {
	ctx := context.Background()
	local := newLocalNode(t)
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	var entries atomic.Int32
	mgr, db, nodes := newClusterWithPeers(t, 3,
		answering(t, http.StatusServiceUnavailable, nil), answering(t, http.StatusServiceUnavailable, nil), answering(t, http.StatusNoContent, &entries))
	setFreeSpace(t, db, nodes[0].ID, 900)
	setFreeSpace(t, db, nodes[1].ID, 900)
	setFreeSpace(t, db, nodes[2].ID, 100)

	h := &HAObjectManager{Manager: local.objects, mgr: mgr}
	_, err := h.PutObject(ctx, "b", "k", strings.NewReader("data"), http.Header{})
	require.ErrorIs(t, err, ErrClusterDegraded)
	assert.Zero(t, entries.Load())
	assert.True(t, missedSince(t, db, nodes[2].ID).Valid)
}

// A node that does not take a write's entry is unavailable from then on and
// recorded as having missed the write.
func TestANodeThatDoesNotTakeAnEntryMissesTheWrite(t *testing.T) {
	ctx := context.Background()
	local := newLocalNode(t)
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "b"}))
	var copies, entries atomic.Int32
	mgr, db, nodes := newClusterWithPeers(t, 2,
		answering(t, http.StatusNoContent, &copies), answering(t, http.StatusInternalServerError, &entries))
	setFreeSpace(t, db, nodes[0].ID, 900)
	setFreeSpace(t, db, nodes[1].ID, 100)

	h := &HAObjectManager{Manager: local.objects, mgr: mgr}
	_, err := h.PutObject(ctx, "b", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	assert.EqualValues(t, 1, copies.Load(), "the node with more free space holds the data")
	assert.EqualValues(t, 1, entries.Load())
	assert.True(t, missedSince(t, db, nodes[1].ID).Valid)
	n, err := mgr.GetNode(ctx, nodes[1].ID)
	require.NoError(t, err)
	assert.Equal(t, HealthStatusUnavailable, n.HealthStatus)
}

// The data of an object is read from a node that serves this write of it,
// passing over one that serves another; a seek opens it again from there.
func TestTheDataIsReadFromANodeThatHoldsThisWrite(t *testing.T) {
	ctx := context.Background()
	content := "0123456789"
	stale := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(HAETagHeader, "other")
		_, _ = w.Write([]byte("stale data"))
	}))
	defer stale.Close()
	var mu sync.Mutex
	var ranges []string
	holder := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ranges = append(ranges, r.Header.Get("Range"))
		mu.Unlock()
		w.Header().Set(HAETagHeader, "good")
		offset := 0
		if rng := r.Header.Get("Range"); rng != "" {
			offset, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rng, "bytes="), "-"))
			w.WriteHeader(http.StatusPartialContent)
		}
		_, _ = w.Write([]byte(content[offset:]))
	}))
	defer holder.Close()
	mgr, _, nodes := newClusterWithPeers(t, 2, stale, holder)
	h := &HAObjectManager{mgr: mgr}
	locations := []string{nodes[0].ID, nodes[1].ID}

	r, err := h.openRemote(ctx, "b", "k", &object.Object{Key: "k", Size: 10, ETag: "good"}, locations)
	require.NoError(t, err)
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
	_, err = r.(io.Seeker).Seek(6, io.SeekStart)
	require.NoError(t, err)
	data, err = io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, "6789", string(data))
	require.NoError(t, r.Close())
	assert.Equal(t, []string{"", "bytes=6-"}, ranges)

	_, err = h.openRemote(ctx, "b", "k", &object.Object{Key: "k", Size: 10, ETag: "none serves"}, locations)
	assert.ErrorIs(t, err, object.ErrDataUnavailable)
}

// elsewhereManager holds entries whose data other nodes hold.
type elsewhereManager struct {
	object.Manager
}

func (elsewhereManager) GetObject(context.Context, string, string, ...string) (*object.Object, io.ReadCloser, error) {
	return nil, nil, &object.DataElsewhereError{Object: &object.Object{Key: "k"}, Locations: []string{"gone"}}
}

// Outside a cluster an object whose data is elsewhere is not found here.
func TestOutsideAClusterDataElsewhereIsNotFound(t *testing.T) {
	db := setupDeadNodeReconcilerDB(t)
	h := &HAObjectManager{Manager: elsewhereManager{}, mgr: newTestManager(t, db)}
	_, _, err := h.GetObject(context.Background(), "b", "k")
	assert.ErrorIs(t, err, object.ErrObjectNotFound)
}

// rawCopySource serves the ciphertext of one entry, as a node whose objects
// every node can decrypt.
type rawCopySource struct {
	object.Manager
	entry metadata.ObjectMetadata
}

func (r *rawCopySource) GetObjectRaw(context.Context, string, string, string) (io.ReadCloser, map[string]string, *metadata.ObjectMetadata, error) {
	entry := r.entry
	return io.NopCloser(strings.NewReader("ciphertext")), map[string]string{"size": "10"}, &entry, nil
}

func (r *rawCopySource) PutObjectRaw(context.Context, string, string, io.Reader, map[string]string, *metadata.ObjectMetadata) error {
	return nil
}

func (r *rawCopySource) CanReplicateRaw(map[string]string) bool { return true }

// A ciphertext copy made to name its node carries those locations and their
// number; any other copy carries those of its write.
func TestANamingRawCopyCarriesItsLocations(t *testing.T) {
	received := make(chan metadata.ObjectMetadata, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		var entry metadata.ObjectMetadata
		data, err := base64.StdEncoding.DecodeString(r.Header.Get(HARawObjectMetaHeader))
		if err == nil {
			err = json.Unmarshal(data, &entry)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		received <- entry
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	ctx := context.Background()
	source := &rawCopySource{entry: metadata.ObjectMetadata{Bucket: "b", Key: "k", ETag: "e", Locations: []string{"a", "b"}, LocationsGen: 1}}
	node := &Node{ID: "n", Endpoint: srv.URL}

	require.NoError(t, sendNamingCopy(ctx, NewProxyClient(nil), source, node, "a", "b", "k", "", 2, []string{"a", "n"}))
	sent := <-received
	assert.Equal(t, []string{"a", "n"}, sent.Locations)
	assert.EqualValues(t, 2, sent.LocationsGen)

	require.NoError(t, sendObjectVersion(ctx, NewProxyClient(nil), source, node, "a", "b", "k", ""))
	sent = <-received
	assert.Equal(t, []string{"a", "b"}, sent.Locations)
	assert.EqualValues(t, 1, sent.LocationsGen)
}

// The most data nodes hold with each byte on factor different nodes: their
// room divided by the factor, less when a node is too large for its share or
// there are fewer nodes than the factor.
func TestSpaceForCopies(t *testing.T) {
	for _, c := range []struct {
		room   []int64
		factor int
		want   int64
	}{
		{[]int64{10, 10, 10}, 2, 15},
		{[]int64{10, 10, 10}, 3, 10},
		{[]int64{1, 1, 10}, 2, 2},
		{[]int64{1, 10, 10}, 2, 10},
		{[]int64{10, 9, 1}, 3, 1},
		{[]int64{10, -1, 4}, 2, 4},
		{[]int64{10}, 2, 0},
		{[]int64{10}, 3, 0},
	} {
		assert.Equal(t, c.want, spaceForCopies(c.room, c.factor), "%v, factor %d", c.room, c.factor)
	}
}

// The room of a cluster counts every node but the dead ones, each byte once
// per copy; outside a cluster there is none.
func TestTheClusterSpace(t *testing.T) {
	ctx := context.Background()
	db := setupDeadNodeReconcilerDB(t)
	mgr := newTestManager(t, db)
	setReplicationFactor(t, db, 2)
	_, _, ok := mgr.Space(ctx)
	assert.False(t, ok, "outside a cluster")

	enableCluster(t, db)
	setReplicationFactor(t, db, 1)
	for id, status := range map[string]string{"a": HealthStatusHealthy, "b": HealthStatusUnavailable, "c": HealthStatusHealthy, "gone": HealthStatusDead} {
		insertNode(t, db, id, id, status, nil)
	}
	for id, used := range map[string]int64{"a": 200, "b": 400, "c": 900} {
		_, err := db.Exec(`UPDATE cluster_nodes SET capacity_total = 1000, capacity_used = ? WHERE id = ?`, used, id)
		require.NoError(t, err)
	}
	_, err := db.Exec(`UPDATE cluster_nodes SET capacity_total = 5000 WHERE id = 'gone'`)
	require.NoError(t, err)
	capacity, available, ok := mgr.Space(ctx)
	require.True(t, ok)
	assert.EqualValues(t, 3000, capacity)
	assert.EqualValues(t, 1500, available, "with a factor of 1, 800, 600 and 100 free")

	setReplicationFactor(t, db, 2)
	capacity, available, ok = mgr.Space(ctx)
	require.True(t, ok)
	assert.EqualValues(t, 1500, capacity)
	assert.EqualValues(t, 700, available, "800, 600 and 100 free: the 800 holds one copy of 700 at most")
}
