package cluster

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// A write that misses quorum is undone on the local node even when it set
// COMPLIANCE retention and a legal hold: the client was told it failed.
func TestHAPutQuorumFailureUndoesAProtectedVersion(t *testing.T) {
	root := t.TempDir()
	backend, err := storage.NewFilesystemBackend(storage.Config{Root: root})
	require.NoError(t, err)
	store, err := metadata.NewPebbleStore(metadata.PebbleOptions{
		DataDir: filepath.Join(root, "metadata"),
		Logger:  logrus.StandardLogger(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	local := object.NewManager(backend, store, config.StorageConfig{
		Backend: "filesystem", Root: root, EncryptionKey: strings.Repeat("ab", 32),
	})
	ctx := context.Background()
	require.NoError(t, store.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:       "worm",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
		ObjectLock: &metadata.ObjectLockMetadata{Enabled: true},
	}))

	db, cleanup := setupQuorumTestDB(t)
	t.Cleanup(cleanup)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(peer.Close)
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	_, err = mgr.InitializeCluster(ctx, "local-node", "us-east-1", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 2))
	node := &Node{Name: "peer", Endpoint: peer.URL, NodeToken: "t", Region: "us-east-1", Priority: 100, Metadata: "{}"}
	require.NoError(t, mgr.AddNode(ctx, node))
	_, err = db.ExecContext(ctx, `UPDATE cluster_nodes SET health_status = ? WHERE id = ?`, HealthStatusHealthy, node.ID)
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set("x-amz-object-lock-mode", object.RetentionModeCompliance)
	headers.Set("x-amz-object-lock-retain-until-date", time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339))
	headers.Set("x-amz-object-lock-legal-hold", object.LegalHoldStatusOn)
	_, err = NewHAObjectManager(local, mgr).PutObject(ctx, "worm", "k", strings.NewReader("data"), headers)
	require.ErrorIs(t, err, ErrClusterDegraded)

	versions, err := store.GetObjectVersions(ctx, "worm", "k")
	if err == nil {
		require.Empty(t, versions, "the failed write left a protected version on the local node")
	} else {
		require.ErrorIs(t, err, metadata.ErrObjectNotFound)
	}
}
