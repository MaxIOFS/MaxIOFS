package cluster

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A node sent a write it already holds gets its entry, not its data again: a
// sync running beside the copies of a raised factor sent each copy back to the
// node it came from.
func TestSyncSendsNoDataANodeHolds(t *testing.T) {
	local := newLocalNode(t)
	ctx := context.Background()
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{Name: "held"}))
	obj, err := local.objects.PutObject(ctx, "held", "k", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)

	var mu sync.Mutex
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		requests = append(requests, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	_, _, nodes := newClusterWithPeers(t, 2, srv)
	peer := nodes[0]

	writer, ok := ReplicaWriter(local.objects)
	require.True(t, ok)
	v := object.ObjectVersion{Object: *obj}
	require.NoError(t, writer.SetLocations(ctx, "held", "k", locationsChange(v, []string{"local", peer.ID}, 1)))

	sent := func(held *ChecksumEntry) []string {
		mu.Lock()
		requests = nil
		mu.Unlock()
		_, err := sendKeyVersions(ctx, NewProxyClient(nil), local.objects, peer, "local", "held", "k", held)
		require.NoError(t, err)
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), requests...)
	}
	isData := func(r string) bool { return strings.HasPrefix(r, "PUT ") && strings.Contains(r, "/ha/objects/") }
	isEntry := func(r string) bool { return strings.HasSuffix(r, "/ha/object-entry") }

	holds := &ChecksumEntry{Key: "k", Found: true, HoldsCurrent: true, ETag: obj.ETag,
		LastModified: obj.LastModified.Unix(), WrittenAt: obj.WrittenAt}
	got := sent(holds)
	assert.Condition(t, func() bool { return len(got) == 1 && isEntry(got[0]) }, "the node holding it is sent the entry: %v", got)

	got = sent(nil)
	assert.Condition(t, func() bool { return len(got) == 1 && isData(got[0]) }, "without knowing, the data is sent: %v", got)

	other := *holds
	other.ETag = "another"
	got = sent(&other)
	assert.Condition(t, func() bool { return len(got) == 1 && isData(got[0]) }, "a node holding another write is sent the data: %v", got)
}
