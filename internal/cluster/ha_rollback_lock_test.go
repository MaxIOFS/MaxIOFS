package cluster

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/stretchr/testify/require"
)

// A write that misses quorum is undone on the local node even when it set
// COMPLIANCE retention and a legal hold: the client was told it failed. A
// factor of 3 needs one of its two peers; both fail here.
func TestHAPutQuorumFailureUndoesAProtectedVersion(t *testing.T) {
	local := newLocalNode(t)
	ctx := context.Background()
	require.NoError(t, local.store.CreateBucket(ctx, &metadata.BucketMetadata{
		Name:       "worm",
		Versioning: &metadata.VersioningMetadata{Status: "Enabled"},
		ObjectLock: &metadata.ObjectLockMetadata{Enabled: true},
	}))
	hits := 0
	mgr, _, _ := newClusterWithPeers(t, 3, failingPeer(t, &hits), failingPeer(t, &hits))

	headers := http.Header{}
	headers.Set("x-amz-object-lock-mode", object.RetentionModeCompliance)
	headers.Set("x-amz-object-lock-retain-until-date", time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339))
	headers.Set("x-amz-object-lock-legal-hold", object.LegalHoldStatusOn)
	_, err := NewHAObjectManager(local.objects, mgr).PutObject(ctx, "worm", "k", strings.NewReader("data"), headers)
	require.ErrorIs(t, err, ErrClusterDegraded)
	require.Equal(t, 2, hits)

	versions, err := local.store.GetObjectVersions(ctx, "worm", "k")
	if err == nil {
		require.Empty(t, versions, "the failed write left a protected version on the local node")
	} else {
		require.ErrorIs(t, err, metadata.ErrObjectNotFound)
	}
}
