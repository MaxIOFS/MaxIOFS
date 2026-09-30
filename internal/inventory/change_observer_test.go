package inventory

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func watchChanges(m *Manager) *[]string {
	seen := &[]string{}
	m.SetChangeObserver(func(_ context.Context, table, id string, deleted bool) {
		*seen = append(*seen, fmt.Sprintf("%s/%s/%v", table, id, deleted))
	})
	return seen
}

func newTestConfig(id, bucket, tenant string) *InventoryConfig {
	return &InventoryConfig{ID: id, BucketName: bucket, TenantID: tenant, Enabled: true, Frequency: "daily",
		Format: "csv", DestinationBucket: "dest", IncludedFields: []string{"object_key"}, ScheduleTime: "02:00"}
}

// Every configuration and report written or deleted is reported once stored;
// a write that changed nothing is not.
func TestInventoryChangesAreReported(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	manager := NewManager(db)
	seen := watchChanges(manager)
	ctx := context.Background()
	cfg, rep := ConfigsTable+"/", ReportsTable+"/"

	c := newTestConfig("c1", "b1", "t1")
	require.NoError(t, manager.CreateConfig(ctx, c))
	require.NoError(t, manager.UpdateConfig(ctx, c))
	require.Error(t, manager.UpdateConfig(ctx, newTestConfig("missing", "b9", "t1")))
	require.NoError(t, manager.UpsertConfigByID(ctx, newTestConfig("c1", "b1", "t1")))
	require.NoError(t, manager.UpsertConfigByID(ctx, newTestConfig("c2", "b2", "t1")))

	r := &InventoryReport{ID: "r1", ConfigID: "c1", BucketName: "b1", Status: "pending"}
	require.NoError(t, manager.CreateReport(ctx, r))
	r.Status = "completed"
	require.NoError(t, manager.UpdateReport(ctx, r))
	require.Error(t, manager.UpdateReport(ctx, &InventoryReport{ID: "missing", Status: "failed"}))

	require.NoError(t, manager.DeleteConfigByID(ctx, "c2", "t1"))
	require.Error(t, manager.DeleteConfigByID(ctx, "c2", "t1"))
	require.NoError(t, manager.DeleteConfig(ctx, "b1", "t1"))
	require.Error(t, manager.DeleteConfig(ctx, "b1", "t1"))

	assert.Equal(t, []string{
		cfg + "c1/false", cfg + "c1/false", cfg + "c1/false", cfg + "c2/false",
		rep + "r1/false", rep + "r1/false",
		cfg + "c2/true", cfg + "c1/true",
	}, *seen)
}

// Deleting the configurations of a bucket without a tenant, which may hold
// several, reports each; those of other buckets stay.
func TestDeletingABucketsConfigurationsReportsEach(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	manager := NewManager(db)
	ctx := context.Background()
	for _, c := range []*InventoryConfig{newTestConfig("g1", "global", ""), newTestConfig("g2", "global", ""),
		newTestConfig("t1", "global", "tenant"), newTestConfig("o1", "other", "")} {
		require.NoError(t, manager.UpsertConfigByID(ctx, c))
	}
	seen := watchChanges(manager)

	require.NoError(t, manager.DeleteConfig(ctx, "global", ""))
	assert.ElementsMatch(t, []string{ConfigsTable + "/g1/true", ConfigsTable + "/g2/true"}, *seen)
	for id, want := range map[string]bool{"g1": false, "g2": false, "t1": true, "o1": true} {
		var n int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM bucket_inventory_configs WHERE id = ?`, id).Scan(&n))
		assert.Equal(t, want, n == 1, id)
	}
}

// The reports dropped to keep a bucket's history bounded are reported
// deleted, and only they.
func TestTrimmedReportsAreReportedDeleted(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	manager := NewManager(db)
	ctx := context.Background()
	for i := 0; i < reportsKeptPerBucket; i++ {
		require.NoError(t, manager.CreateReport(ctx, &InventoryReport{ID: fmt.Sprintf("r%02d", i), ConfigID: "c", BucketName: "b", Status: "completed"}))
	}
	seen := watchChanges(manager)

	require.NoError(t, manager.CreateReport(ctx, &InventoryReport{ID: "r20", ConfigID: "c", BucketName: "b", Status: "completed"}))
	require.NoError(t, manager.CreateReport(ctx, &InventoryReport{ID: "r21", ConfigID: "c", BucketName: "b", Status: "completed"}))
	assert.Equal(t, []string{
		ReportsTable + "/r20/false", ReportsTable + "/r00/true",
		ReportsTable + "/r21/false", ReportsTable + "/r01/true",
	}, *seen)
	var kept int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM bucket_inventory_reports WHERE id IN ('r00', 'r01')`).Scan(&kept))
	assert.Zero(t, kept)
}

// A node that does not run the jobs of every bucket leaves the ready
// configurations to the one that does.
func TestWorkerRunsOnlyWhenItsGateSays(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	manager := NewManager(db)
	ctx := context.Background()
	require.NoError(t, manager.CreateConfig(ctx, newTestConfig("ready", "src", "t1")))
	_, err := db.Exec(`UPDATE bucket_inventory_configs SET next_run_at = 1 WHERE id = 'ready'`)
	require.NoError(t, err)
	mockBucketMgr := new(MockBucketManager)
	worker := NewWorker(manager, mockBucketMgr, new(MockMetadataStore), new(MockObjectWriter))

	asked := 0
	worker.SetRunGate(func() bool { asked++; return false })
	worker.processInventories(ctx)
	assert.Equal(t, 1, asked)
	mockBucketMgr.AssertNotCalled(t, "GetBucketInfo", mock.Anything, mock.Anything, mock.Anything)

	mockBucketMgr.On("GetBucketInfo", ctx, "t1", "src").Return(nil, errors.New("gone")).Once()
	worker.SetRunGate(func() bool { return true })
	worker.processInventories(ctx)
	reports, err := manager.ListReports(ctx, "src", "t1", 10, 0)
	require.NoError(t, err)
	require.Len(t, reports, 1)
	assert.Equal(t, "failed", reports[0].Status)
	mockBucketMgr.AssertExpectations(t)
}
