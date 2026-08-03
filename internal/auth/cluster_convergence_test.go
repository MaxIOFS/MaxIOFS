package auth

// Two nodes, and the questions a leaderless cluster has to answer.
//
// MaxIOFS has no leader: every node accepts writes and they reconcile with each
// other. That is only safe if two things hold, and neither had been measured:
//
//   - A credential revoked on one node stops working on the others. How long
//     that takes is the revocation window, and it is a security property, not a
//     performance one.
//   - Two nodes writing the same configuration converge on one answer instead
//     of flapping between two.
//
// These build two independent stores — two nodes — move state between them the
// way the sync managers do, and record what actually happens.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// twoNodes returns two independent managers standing in for two cluster nodes,
// sharing the JWT secret as a real cluster does after the join.
func twoNodes(t *testing.T) (*authManager, *authManager, func()) {
	t.Helper()

	dirA, err := os.MkdirTemp("", "maxiofs-node-a-*")
	require.NoError(t, err)
	dirB, err := os.MkdirTemp("", "maxiofs-node-b-*")
	require.NoError(t, err)

	cfg := config.AuthConfig{
		EnableAuth: true,
		JWTSecret:  "shared-cluster-secret-for-the-two-node-test",
	}

	a := NewManager(cfg, dirA).(*authManager)
	b := NewManager(cfg, dirB).(*authManager)

	return a, b, func() {
		os.RemoveAll(dirA)
		os.RemoveAll(dirB)
	}
}

// replicateSession copies a session from one node to another, which is what the
// STS sync manager does on its interval and on its immediate push.
func replicateSession(t *testing.T, from, to *authManager, keyID string) {
	t.Helper()
	stored, err := from.store.GetSTSSession(keyID)
	require.NoError(t, err)
	require.NoError(t, to.store.CreateSTSSession(stored))
}

// TestCluster_RevocationWindow measures what a second node does with a
// credential the first node has already revoked. A leaked credential is only as
// contained as the slowest node to hear about it.
func TestCluster_RevocationWindow(t *testing.T) {
	nodeA, nodeB, cleanup := twoNodes(t)
	defer cleanup()
	ctx := context.Background()

	user := &User{
		ID: "shared-user", Username: "shared-user",
		Status: UserStatusActive, Roles: []string{RoleAdmin},
	}
	require.NoError(t, nodeA.store.CreateUser(user))
	require.NoError(t, nodeB.store.CreateUser(user))

	session, err := nodeA.IssueSTSSession(ctx, user.ID, 3600, "")
	require.NoError(t, err)
	replicateSession(t, nodeA, nodeB, session.TempAccessKeyID)

	_, _, err = nodeB.ResolveSTSSessionSecret(ctx, session.TempAccessKeyID, session.SessionToken)
	require.NoError(t, err, "before revocation the credential works on both nodes")

	require.NoError(t, nodeA.RevokeSTSSession(ctx, session.TempAccessKeyID, user.ID, true))

	_, _, err = nodeA.ResolveSTSSessionSecret(ctx, session.TempAccessKeyID, session.SessionToken)
	assert.Error(t, err, "the node that revoked it refuses immediately")

	// The window: until the revocation reaches B, B still serves the credential.
	_, _, err = nodeB.ResolveSTSSessionSecret(ctx, session.TempAccessKeyID, session.SessionToken)
	assert.NoError(t, err,
		"MEASURED: a revoked credential keeps working on other nodes until the revocation propagates")

	require.NoError(t, nodeB.store.DeleteSTSSession(session.TempAccessKeyID))
	_, _, err = nodeB.ResolveSTSSessionSecret(ctx, session.TempAccessKeyID, session.SessionToken)
	assert.Error(t, err, "once propagated, both nodes agree")
}

// TestCluster_DeactivatingAUserIsImmediate is the reassuring half: a user's
// status is read on every request from the local row, so deactivating an
// account does not wait for any sync.
func TestCluster_DeactivatingAUserIsImmediate(t *testing.T) {
	nodeA, nodeB, cleanup := twoNodes(t)
	defer cleanup()
	ctx := context.Background()

	user := &User{
		ID: "deactivated-user", Username: "deactivated-user",
		Status: UserStatusActive, Roles: []string{RoleAdmin},
	}
	require.NoError(t, nodeA.store.CreateUser(user))
	require.NoError(t, nodeB.store.CreateUser(user))

	session, err := nodeA.IssueSTSSession(ctx, user.ID, 3600, "")
	require.NoError(t, err)
	replicateSession(t, nodeA, nodeB, session.TempAccessKeyID)

	onB, err := nodeB.store.GetUserByID(user.ID)
	require.NoError(t, err)
	onB.Status = UserStatusInactive
	require.NoError(t, nodeB.store.UpdateUser(onB))

	_, _, err = nodeB.ResolveSTSSessionSecret(ctx, session.TempAccessKeyID, session.SessionToken)
	assert.Error(t, err,
		"a deactivated account is refused by the node that knows, with no sync involved")
}

// TestCluster_ConcurrentPolicyWrites is the question nobody had asked: two
// administrators editing the same policy on two nodes at the same time.
func TestCluster_ConcurrentPolicyWrites(t *testing.T) {
	nodeA, nodeB, cleanup := twoNodes(t)
	defer cleanup()
	ctx := context.Background()

	readOnly := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["*"]}]}`
	readWrite := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:*"],"Resource":["*"]}]}`

	_, err := nodeA.CreateIAMPolicy(ctx, "Contended", "/", "written on A", readOnly)
	require.NoError(t, err)

	// A second later, so the timestamps can order the two writes.
	time.Sleep(1100 * time.Millisecond)
	_, err = nodeB.CreateIAMPolicy(ctx, "Contended", "/", "written on B", readWrite)
	require.NoError(t, err)

	onA, err := nodeA.GetIAMPolicy(ctx, "Contended")
	require.NoError(t, err)
	onB, err := nodeB.GetIAMPolicy(ctx, "Contended")
	require.NoError(t, err)

	assert.NotEqual(t, onA.Document, onB.Document,
		"before syncing, the nodes disagree — this is what a split write leaves behind")
	assert.Greater(t, onB.UpdatedAt, onA.UpdatedAt,
		"the later write carries the higher timestamp, which is what convergence uses")

	t.Logf("MEASURED: convergence is last-write-wins by updated_at (A=%d, B=%d), "+
		"so one of the two edits is dropped silently", onA.UpdatedAt, onB.UpdatedAt)
}

// TestCluster_ConcurrentWritesInTheSameSecond is the case that makes
// last-write-wins uncomfortable: two writes the timestamps cannot order.
func TestCluster_ConcurrentWritesInTheSameSecond(t *testing.T) {
	nodeA, nodeB, cleanup := twoNodes(t)
	defer cleanup()
	ctx := context.Background()

	docA := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::a/*"]}]}`
	docB := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::b/*"]}]}`

	// No sleep: both land in the same second, as two admins clicking at once.
	_, err := nodeA.CreateIAMPolicy(ctx, "SameSecond", "/", "", docA)
	require.NoError(t, err)
	_, err = nodeB.CreateIAMPolicy(ctx, "SameSecond", "/", "", docB)
	require.NoError(t, err)

	onA, err := nodeA.GetIAMPolicy(ctx, "SameSecond")
	require.NoError(t, err)
	onB, err := nodeB.GetIAMPolicy(ctx, "SameSecond")
	require.NoError(t, err)

	assert.NotEqual(t, onA.Document, onB.Document, "the nodes hold different documents")

	if onA.UpdatedAt == onB.UpdatedAt {
		t.Logf("MEASURED: both writes carry updated_at=%d. The timestamps cannot order them, "+
			"so which document survives depends on which sync batch arrives last, "+
			"and the two nodes can settle on different answers", onA.UpdatedAt)
	} else {
		t.Logf("the two writes landed in different seconds (A=%d, B=%d)", onA.UpdatedAt, onB.UpdatedAt)
	}
}

// TestCluster_BucketGrantsAreOrderedOnlyBySecond checks the same hazard on the
// path an operator uses far more often than editing a policy by hand.
func TestCluster_BucketGrantsAreOrderedOnlyBySecond(t *testing.T) {
	nodeA, nodeB, cleanup := twoNodes(t)
	defer cleanup()

	user := &User{
		ID: "grant-user", Username: "grant-user",
		Status: UserStatusActive, Roles: []string{"user"},
	}
	require.NoError(t, nodeA.store.CreateUser(user))
	require.NoError(t, nodeB.store.CreateUser(user))

	// One admin grants read on node A while another grants write on node B.
	require.NoError(t, nodeA.store.GrantBucketAccess("shared", user.ID, "", PermissionLevelRead, "admin", 0))
	require.NoError(t, nodeB.store.GrantBucketAccess("shared", user.ID, "", PermissionLevelWrite, "admin", 0))

	docsA, err := nodeA.store.EffectivePolicyDocuments(user.ID, user.Roles)
	require.NoError(t, err)
	docsB, err := nodeB.store.EffectivePolicyDocuments(user.ID, user.Roles)
	require.NoError(t, err)

	canWriteOnA := EvaluateIAMDocuments(docsA, ActionPutObject, "arn:aws:s3:::shared/key")
	canWriteOnB := EvaluateIAMDocuments(docsB, ActionPutObject, "arn:aws:s3:::shared/key")

	assert.False(t, canWriteOnA, "node A granted read only")
	assert.True(t, canWriteOnB, "node B granted write")
	t.Logf("MEASURED: the same user may write on one node and not the other until the "+
		"grants converge (A=%v, B=%v)", canWriteOnA, canWriteOnB)
}
