package inventory

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The worker writes one report an hour per bucket, so the table grows for as
// long as the deployment lives unless the history is bounded.
func TestCreateReport_KeepsOnlyTheRecentHistory(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	manager := NewManager(db)
	ctx := context.Background()

	const total = reportsKeptPerBucket + 15
	for i := 0; i < total; i++ {
		require.NoError(t, manager.CreateReport(ctx, &InventoryReport{
			ConfigID:   "config-1",
			BucketName: "kept",
			ReportPath: fmt.Sprintf("reports/%d.csv", i),
			Status:     "completed",
		}))
	}

	// A second bucket must not lose anything to the first one's trimming.
	require.NoError(t, manager.CreateReport(ctx, &InventoryReport{
		ConfigID: "config-2", BucketName: "other", ReportPath: "other.csv", Status: "completed",
	}))

	var kept int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM bucket_inventory_reports WHERE bucket_name = 'kept'`).Scan(&kept))
	assert.Equal(t, reportsKeptPerBucket, kept)

	survived := func(path string) bool {
		var n int
		require.NoError(t, db.QueryRow(
			`SELECT COUNT(*) FROM bucket_inventory_reports WHERE bucket_name = 'kept' AND report_path = ?`,
			path).Scan(&n))
		return n == 1
	}
	assert.True(t, survived(fmt.Sprintf("reports/%d.csv", total-1)), "the newest report must survive")
	assert.False(t, survived("reports/0.csv"), "the oldest must be gone")

	var others int
	require.NoError(t, db.QueryRow(
		`SELECT COUNT(*) FROM bucket_inventory_reports WHERE bucket_name = 'other'`).Scan(&others))
	assert.Equal(t, 1, others)
}
