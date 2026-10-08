package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Removing a member runs what is set for it, once: a removal received again,
// or of a node that is not a member, runs nothing.
func TestARemovedMemberIsAnnouncedOnce(t *testing.T) {
	ctx := context.Background()
	db := setupDeadNodeReconcilerDB(t)
	enableCluster(t, db)
	mgr := newTestManager(t, db)
	insertNode(t, db, "gone", "gone", HealthStatusHealthy, nil)
	var removed []string
	mgr.OnNodeRemoved(func(id string) { removed = append(removed, id) })

	at := time.Now().Unix()
	require.NoError(t, mgr.RemoveNodeAt(ctx, "gone", at, "local-node"))
	require.NoError(t, mgr.RemoveNodeAt(ctx, "gone", at, "other-node"))
	require.NoError(t, mgr.RemoveNodeAt(ctx, "never-a-member", at, "other-node"))
	assert.Equal(t, []string{"gone"}, removed)
}
