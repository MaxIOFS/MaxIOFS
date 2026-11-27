package inventory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A report runs at the time last set: a schedule changed before the first
// report moves the next one, as one changed after it does.
func TestAScheduleChangedBeforeTheFirstReportMovesIt(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	manager := NewManager(db)
	ctx := context.Background()

	config := newTestConfig("c1", "b1", "t1")
	config.ScheduleTime = "02:00"
	require.NoError(t, manager.CreateConfig(ctx, config))
	stored, err := manager.GetConfig(ctx, "b1", "t1")
	require.NoError(t, err)
	require.NotNil(t, stored.NextRunAt)
	assert.Equal(t, 2, time.Unix(*stored.NextRunAt, 0).Hour())

	stored.ScheduleTime = "14:30"
	require.NoError(t, manager.UpdateConfig(ctx, stored))
	moved, err := manager.GetConfig(ctx, "b1", "t1")
	require.NoError(t, err)
	require.NotNil(t, moved.NextRunAt)
	next := time.Unix(*moved.NextRunAt, 0)
	assert.Equal(t, 14, next.Hour(), "the next report runs at the time last set")
	assert.Equal(t, 30, next.Minute())

	moved.ScheduleTime = "25:99"
	assert.Error(t, manager.UpdateConfig(ctx, moved), "a time that is none")
}
