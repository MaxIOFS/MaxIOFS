package replication

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every rule created, changed or deleted is reported once stored; a write
// that failed or changed nothing is not.
func TestRuleChangesAreReported(t *testing.T) {
	m, _ := setupTestManager(t)
	var seen []string
	m.SetChangeObserver(func(_ context.Context, table, id string, deleted bool) {
		seen = append(seen, fmt.Sprintf("%s/%s/%v", table, id, deleted))
	})
	ctx := context.Background()
	rule := &ReplicationRule{ID: "r1", TenantID: "t1", SourceBucket: "b", DestinationEndpoint: "https://s3.example.com",
		DestinationBucket: "d", DestinationAccessKey: "ak", DestinationSecretKey: "sk", Enabled: true, Mode: ModeRealTime}

	require.NoError(t, m.CreateRule(ctx, rule))
	require.Error(t, m.CreateRule(ctx, rule), "the id is taken")
	rule.Prefix = "p/"
	require.NoError(t, m.UpdateRule(ctx, rule))
	missing := *rule
	missing.ID = "missing"
	require.Error(t, m.UpdateRule(ctx, &missing))
	require.Error(t, m.DeleteRule(ctx, "another-tenant", "r1"))
	require.NoError(t, m.DeleteRule(ctx, "t1", "r1"))
	require.Error(t, m.DeleteRule(ctx, "t1", "r1"))

	assert.Equal(t, []string{RulesTable + "/r1/false", RulesTable + "/r1/false", RulesTable + "/r1/true"}, seen)
}

// A node that does not run the jobs of every bucket leaves the scheduled
// rules to the one that does.
func TestScheduledRulesRunOnlyWhenTheGateSays(t *testing.T) {
	lister := &MockBucketLister{
		ListObjectsFunc: func(_ context.Context, _, _, _ string, _ int) ([]string, error) {
			return []string{"obj.txt"}, nil
		},
	}
	m := setupSyncManager(t, lister)
	ctx := context.Background()
	rule := insertScheduledRule(t, m, "gated", 1)
	lastSync := map[string]time.Time{}

	asked := 0
	m.SetScheduleGate(func() bool { asked++; return false })
	m.scheduledPass(ctx, lastSync)
	assert.Equal(t, 1, asked)
	assert.Empty(t, lastSync)

	m.SetScheduleGate(func() bool { return true })
	m.scheduledPass(ctx, lastSync)
	assert.Contains(t, lastSync, rule.ID)
	require.Eventually(t, func() bool { return queueCount(t, m, rule.ID) == 1 }, 10*time.Second, 20*time.Millisecond)
}
