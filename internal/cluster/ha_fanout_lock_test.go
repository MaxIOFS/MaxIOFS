package cluster

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lockedObjectManager serves one object for the fan-out re-read. It has no raw
// access, so the legacy transfer is used.
type lockedObjectManager struct {
	rollbackRecorderManager
	obj *object.Object
}

func (m *lockedObjectManager) GetObject(context.Context, string, string, ...string) (*object.Object, io.ReadCloser, error) {
	return m.obj, io.NopCloser(strings.NewReader("body")), nil
}

func (m *lockedObjectManager) GetObjectMetadata(context.Context, string, string, ...string) (*object.Object, error) {
	return m.obj, nil
}

// fanoutToRecorder replicates obj to one peer and returns the headers the peer
// received.
func fanoutToRecorder(t *testing.T, obj *object.Object) http.Header {
	db, cleanup := setupQuorumTestDB(t)
	t.Cleanup(cleanup)
	received := make(chan http.Header, 1)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		received <- r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(peer.Close)

	ctx := context.Background()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	_, err := mgr.InitializeCluster(ctx, "local-node", "us-east-1", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 2))
	node := &Node{Name: "peer", Endpoint: peer.URL, NodeToken: "t", Region: "us-east-1", Priority: 100, Metadata: "{}"}
	require.NoError(t, mgr.AddNode(ctx, node))
	_, err = db.ExecContext(ctx, `UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, HealthStatusHealthy, node.ID)
	require.NoError(t, err)

	h := &HAObjectManager{Manager: &lockedObjectManager{obj: obj}, mgr: mgr}
	p, placed := h.place(ctx)
	require.True(t, placed)
	require.NoError(t, h.replicate(ctx, p, "bucket", "key", obj.VersionID, time.Now()))
	select {
	case got := <-received:
		return got
	default:
		t.Fatal("the peer received no transfer")
		return nil
	}
}

func TestFanoutLegacyTransferCarriesObjectLock(t *testing.T) {
	until := time.Date(2031, 5, 6, 7, 8, 9, 123456789, time.FixedZone("UTC-3", -3*3600))
	got := fanoutToRecorder(t, &object.Object{
		Key: "key", Size: 4, VersionID: "v1",
		Retention: &object.RetentionConfig{Mode: object.RetentionModeCompliance, RetainUntilDate: until},
		LegalHold: &object.LegalHoldConfig{Status: object.LegalHoldStatusOn},
	})

	assert.Equal(t, "true", got.Get(HAObjectLockHeader))
	assert.Equal(t, object.RetentionModeCompliance, got.Get("x-amz-object-lock-mode"))
	sent, err := time.Parse(time.RFC3339, got.Get("x-amz-object-lock-retain-until-date"))
	require.NoError(t, err)
	assert.True(t, sent.Equal(until), "sent %v for %v", sent, until)
	assert.Equal(t, object.LegalHoldStatusOn, got.Get("x-amz-object-lock-legal-hold"))
}

// Without lock state the transfer still says the state is complete, and sends
// no lock header: an older replica refuses one on a bucket without Object Lock.
func TestFanoutLegacyTransferWithoutObjectLock(t *testing.T) {
	got := fanoutToRecorder(t, &object.Object{
		Key: "key", Size: 4,
		LegalHold: &object.LegalHoldConfig{Status: object.LegalHoldStatusOff},
	})

	assert.Equal(t, "true", got.Get(HAObjectLockHeader))
	for _, name := range []string{"x-amz-object-lock-mode", "x-amz-object-lock-retain-until-date", "x-amz-object-lock-legal-hold"} {
		assert.Empty(t, got.Get(name), name)
	}
}
