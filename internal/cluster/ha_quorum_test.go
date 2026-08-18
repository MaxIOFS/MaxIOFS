package cluster

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── ClusterCanAcceptWrites ─────────────────────────────────────────────────────

// setupQuorumTestDB returns a DB with both core and replication schemas so that
// SetReplicationFactor (which writes cluster_global_config) works.
func setupQuorumTestDB(t *testing.T) (*sql.DB, func()) {
	db, cleanup := setupTestDB(t)
	if err := InitReplicationSchema(db); err != nil {
		cleanup()
		t.Fatalf("InitReplicationSchema: %v", err)
	}
	return db, cleanup
}

func TestClusterCanAcceptWrites_ClusterDisabled(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")

	ok, err := mgr.ClusterCanAcceptWrites(context.Background())
	require.NoError(t, err)
	assert.True(t, ok, "single-node mode should always accept writes")
}

func TestClusterCanAcceptWrites_FactorOneAlwaysOK(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	ctx := context.Background()

	_, err := mgr.InitializeCluster(ctx, "local-node", "us-east-1", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 1))

	ok, err := mgr.ClusterCanAcceptWrites(ctx)
	require.NoError(t, err)
	assert.True(t, ok, "factor=1 needs no replicas")
}

func TestClusterCanAcceptWrites_FactorTwoRejectsWhenNoHealthyPeers(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	ctx := context.Background()

	_, err := mgr.InitializeCluster(ctx, "local-node", "us-east-1", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 2))

	ok, err := mgr.ClusterCanAcceptWrites(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "factor=2 is a mirror and needs one healthy replica")
}

func TestClusterCanAcceptWrites_FactorTwoAcceptsWithOneHealthyPeer(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	ctx := context.Background()

	_, err := mgr.InitializeCluster(ctx, "local-node", "us-east-1", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 2))

	peer := &Node{
		Name: "peer-up", Endpoint: "http://peer-up:8080", NodeToken: "t",
		Region: "us-east-1", Priority: 100, Metadata: "{}",
	}
	require.NoError(t, mgr.AddNode(ctx, peer))
	_, err = db.ExecContext(ctx,
		`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`,
		HealthStatusHealthy, peer.ID)
	require.NoError(t, err)

	ok, err := mgr.ClusterCanAcceptWrites(ctx)
	require.NoError(t, err)
	assert.True(t, ok, "factor=2 needs exactly one healthy replica")
}

func TestClusterCanAcceptWrites_FactorThreeAcceptsWithOneHealthyPeer(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	ctx := context.Background()

	_, err := mgr.InitializeCluster(ctx, "local-node", "us-east-1", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 3))

	peer := &Node{
		Name: "peer-up", Endpoint: "http://peer-up:8080", NodeToken: "t",
		Region: "us-east-1", Priority: 100, Metadata: "{}",
	}
	require.NoError(t, mgr.AddNode(ctx, peer))
	_, err = db.ExecContext(ctx,
		`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`,
		HealthStatusHealthy, peer.ID)
	require.NoError(t, err)

	ok, err := mgr.ClusterCanAcceptWrites(ctx)
	require.NoError(t, err)
	assert.True(t, ok, "factor=3 holds a majority with the local copy and one peer")
}

func TestClusterCanAcceptWrites_FactorThreeAcceptsWithTwoHealthyPeers(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	ctx := context.Background()

	_, err := mgr.InitializeCluster(ctx, "local-node", "us-east-1", "http://localhost:8082")
	require.NoError(t, err)
	require.NoError(t, mgr.SetReplicationFactor(ctx, 3))

	for _, name := range []string{"peer-up-1", "peer-up-2"} {
		peer := &Node{
			Name: name, Endpoint: "http://" + name + ":8080", NodeToken: "t",
			Region: "us-east-1", Priority: 100, Metadata: "{}",
		}
		require.NoError(t, mgr.AddNode(ctx, peer))
		_, err = db.ExecContext(ctx,
			`UPDATE cluster_nodes SET health_status = ? WHERE id = ?`,
			HealthStatusHealthy, peer.ID)
		require.NoError(t, err)
	}

	ok, err := mgr.ClusterCanAcceptWrites(ctx)
	require.NoError(t, err)
	assert.True(t, ok, "factor=3 accepts writes with both peers up")
}

func TestGlobalConfigSyncListParsesModerncTimestampWithOffset(t *testing.T) {
	db, cleanup := setupQuorumTestDB(t)
	defer cleanup()
	ctx := context.Background()
	rawUpdatedAt := "2026-08-18T12:23:59.7080149-03:00"
	expected, err := time.Parse(time.RFC3339Nano, rawUpdatedAt)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx,
		`INSERT INTO cluster_global_config (key, value, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"test.timestamp_offset", "2", rawUpdatedAt, rawUpdatedAt)
	require.NoError(t, err)

	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	syncMgr := NewGlobalConfigSyncManager(db, mgr)
	entries, err := syncMgr.listGlobalConfig(ctx)
	require.NoError(t, err)
	var found bool
	for _, entry := range entries {
		if entry.Key == "test.timestamp_offset" {
			found = true
			assert.Equal(t, expected.Unix(), entry.UpdatedAt)
		}
	}
	assert.True(t, found)
}

// ── collectAndCheckQuorum ──────────────────────────────────────────────────────

func TestCollectAndCheckQuorum_AllSuccess(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	h := &HAObjectManager{mgr: mgr}

	ch := make(chan fanoutResult, 2)
	ch <- fanoutResult{nodeID: "n1", err: nil}
	ch <- fanoutResult{nodeID: "n2", err: nil}

	err := h.collectAndCheckQuorum(context.Background(), ch, 2, 1, "PUT", "b", "k")
	assert.NoError(t, err)
}

func TestCollectAndCheckQuorum_QuorumMet(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	// Pre-seed the node so the UPDATE side-effect can find it.
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO cluster_nodes (id, name, endpoint, api_url, node_token, region, priority,
		 health_status, latency_ms, capacity_total, capacity_used, bucket_count, metadata, created_at, updated_at)
		 VALUES ('n2', 'peer2', 'http://x', 'http://x', 't', 'r', 1, ?, 0, 0, 0, 0, '{}', ?, ?)`,
		HealthStatusHealthy, time.Now(), time.Now())
	require.NoError(t, err)
	h := &HAObjectManager{mgr: mgr}

	ch := make(chan fanoutResult, 2)
	ch <- fanoutResult{nodeID: "n1", err: nil}
	ch <- fanoutResult{nodeID: "n2", err: errors.New("boom")}

	// 1 success >= needed=1 → no error, but n2 must be marked unavailable.
	err = h.collectAndCheckQuorum(context.Background(), ch, 2, 1, "PUT", "b", "k")
	assert.NoError(t, err)

	var status string
	require.NoError(t, db.QueryRow(`SELECT health_status FROM cluster_nodes WHERE id = 'n2'`).Scan(&status))
	assert.Equal(t, HealthStatusUnavailable, status, "failed peer must be marked unavailable")
}

func TestCollectAndCheckQuorum_QuorumMissed(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	mgr := NewManager(db, "http://localhost:8080", "http://localhost:8082")
	h := &HAObjectManager{mgr: mgr}

	ch := make(chan fanoutResult, 2)
	ch <- fanoutResult{nodeID: "n1", err: errors.New("boom1")}
	ch <- fanoutResult{nodeID: "n2", err: errors.New("boom2")}

	err := h.collectAndCheckQuorum(context.Background(), ch, 2, 1, "PUT", "b", "k")
	assert.ErrorIs(t, err, ErrClusterDegraded)
}

func TestRollbackLocalPutSkipsNonVersionedWrite(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	recorder := &rollbackRecorderManager{}
	h := &HAObjectManager{
		Manager: recorder,
		mgr:     NewManager(db, "http://localhost:8080", "http://localhost:8082"),
	}

	h.rollbackLocalPut(context.Background(), "bucket", "key", "", "PutObject")

	assert.False(t, recorder.deleted, "non-versioned rollback must not delete the current key")
}

func TestRollbackLocalPutDeletesOnlySpecificVersion(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	recorder := &rollbackRecorderManager{}
	h := &HAObjectManager{
		Manager: recorder,
		mgr:     NewManager(db, "http://localhost:8080", "http://localhost:8082"),
	}

	h.rollbackLocalPut(context.Background(), "bucket", "key", "v123", "PutObject")

	require.True(t, recorder.deleted)
	assert.Equal(t, []string{"v123"}, recorder.versionIDs)
}

type rollbackRecorderManager struct {
	deleted    bool
	versionIDs []string
}

func (m *rollbackRecorderManager) GetObject(context.Context, string, string, ...string) (*object.Object, io.ReadCloser, error) {
	return nil, nil, nil
}
func (m *rollbackRecorderManager) PutObject(context.Context, string, string, io.Reader, http.Header) (*object.Object, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) DeleteObject(_ context.Context, _, _ string, _ bool, versionID ...string) (string, error) {
	m.deleted = true
	m.versionIDs = append([]string(nil), versionID...)
	return "", nil
}
func (m *rollbackRecorderManager) ListObjects(context.Context, string, string, string, string, int) (*object.ListObjectsResult, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) SearchObjects(context.Context, string, string, string, string, int, *metadata.ObjectFilter) (*object.ListObjectsResult, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) GetObjectMetadata(context.Context, string, string) (*object.Object, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) UpdateObjectMetadata(context.Context, string, string, map[string]string) error {
	return nil
}
func (m *rollbackRecorderManager) GetObjectRetention(context.Context, string, string, ...string) (*object.RetentionConfig, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) SetObjectRetention(context.Context, string, string, *object.RetentionConfig, ...string) error {
	return nil
}
func (m *rollbackRecorderManager) GetObjectLegalHold(context.Context, string, string, ...string) (*object.LegalHoldConfig, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) SetObjectLegalHold(context.Context, string, string, *object.LegalHoldConfig, ...string) error {
	return nil
}
func (m *rollbackRecorderManager) SetRestoreStatus(context.Context, string, string, string, *time.Time, ...string) error {
	return nil
}
func (m *rollbackRecorderManager) GetObjectVersions(context.Context, string, string) ([]object.ObjectVersion, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) DeleteObjectVersion(context.Context, string, string, string) error {
	return nil
}
func (m *rollbackRecorderManager) GetObjectTagging(context.Context, string, string, ...string) (*object.TagSet, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) SetObjectTagging(context.Context, string, string, *object.TagSet, ...string) error {
	return nil
}
func (m *rollbackRecorderManager) DeleteObjectTagging(context.Context, string, string, ...string) error {
	return nil
}
func (m *rollbackRecorderManager) GetObjectACL(context.Context, string, string, ...string) (*object.ACL, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) SetObjectACL(context.Context, string, string, *object.ACL, ...string) error {
	return nil
}
func (m *rollbackRecorderManager) CreateMultipartUpload(context.Context, string, string, http.Header) (*object.MultipartUpload, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) UploadPart(context.Context, string, int, io.Reader) (*object.Part, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) ListParts(context.Context, string) ([]object.Part, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) CompleteMultipartUpload(context.Context, string, []object.Part) (*object.Object, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) AbortMultipartUpload(context.Context, string) error { return nil }
func (m *rollbackRecorderManager) ListMultipartUploads(context.Context, string) ([]object.MultipartUpload, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) VerifyObjectIntegrity(context.Context, string, string) (*object.IntegrityResult, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) VerifyBucketIntegrity(context.Context, string, string, string, int) (*object.BucketIntegrityReport, error) {
	return nil, nil
}
func (m *rollbackRecorderManager) HasActiveComplianceRetention(context.Context, string) (bool, error) {
	return false, nil
}
func (m *rollbackRecorderManager) IsReady() bool { return true }

// ── HA context markers ─────────────────────────────────────────────────────────

func TestHARollbackContext(t *testing.T) {
	ctx := context.Background()
	assert.False(t, isHARollback(ctx))
	assert.False(t, isHAReplica(ctx))

	rb := WithHARollbackContext(ctx)
	assert.True(t, isHARollback(rb))
	assert.False(t, isHAReplica(rb), "rollback marker must not also flag as replica")

	rep := WithHAReplicaContext(ctx)
	assert.True(t, isHAReplica(rep))
	assert.False(t, isHARollback(rep), "replica marker must not also flag as rollback")
}

func TestRequiredReplicaAcks_IsAMajorityCountingTheLocalCopy(t *testing.T) {
	cases := map[int]int{1: 0, 2: 1, 3: 1, 4: 2, 5: 2, 6: 3, 7: 3}
	for factor, want := range cases {
		assert.Equal(t, want, RequiredReplicaAcks(factor),
			"replication factor %d", factor)
	}
}
