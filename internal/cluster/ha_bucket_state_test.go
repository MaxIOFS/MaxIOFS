package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A bucket deletion is kept as long as the deletion log keeps its entries, the
// later of two deletions of a path winning; the cleanup that prunes the log
// prunes it too.
func TestBucketDeletionsAreForgottenWithTheDeletionLog(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	require.NoError(t, InitSchema(db))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	old := time.Now().Add(-8 * 24 * time.Hour).UnixNano()
	recent := time.Now().UnixNano()
	require.NoError(t, recordBucketTombstone(ctx, db, "t/old", old))
	require.NoError(t, recordBucketTombstone(ctx, db, "recent", recent))
	require.NoError(t, recordBucketTombstone(ctx, db, "recent", old))
	at, err := bucketTombstone(ctx, db, "recent")
	require.NoError(t, err)
	assert.Equal(t, recent, at, "an earlier deletion does not replace a later one")

	done := make(chan struct{})
	go func() {
		RunDeletionLogCleanup(ctx, db, 10*time.Millisecond, 7*24*time.Hour)
		close(done)
	}()
	require.Eventually(t, func() bool {
		at, err := bucketTombstone(ctx, db, "t/old")
		return err == nil && at == 0
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	<-done
	at, err = bucketTombstone(context.Background(), db, "recent")
	require.NoError(t, err)
	assert.Equal(t, recent, at)
}

func TestSplitBucketPath(t *testing.T) {
	for path, want := range map[string][2]string{
		"b":        {"", "b"},
		"t/b":      {"t", "b"},
		"tenant/b": {"tenant", "b"},
	} {
		tenantID, name := splitBucketPath(path)
		assert.Equal(t, want, [2]string{tenantID, name}, path)
		assert.Equal(t, path, tenantBucketPath(tenantID, name))
	}
}
