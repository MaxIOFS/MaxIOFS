package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/inventory"
	"github.com/maxiofs/maxiofs/internal/replication"
	"github.com/maxiofs/maxiofs/internal/share"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRule(id, bucket string) *replication.ReplicationRule {
	return &replication.ReplicationRule{ID: id, SourceBucket: bucket, DestinationEndpoint: "https://s3.example.com",
		DestinationBucket: "far", DestinationAccessKey: "ak", DestinationSecretKey: "rule-secret", Enabled: true,
		Mode: replication.ModeRealTime}
}

func testInventoryConfig(bucket, destination string) *inventory.InventoryConfig {
	return &inventory.InventoryConfig{BucketName: bucket, Enabled: true, Frequency: "daily", Format: "csv",
		DestinationBucket: destination, DestinationPrefix: "reports", IncludedFields: []string{"object_key"}, ScheduleTime: "02:00"}
}

func rowCount(t *testing.T, s *Server, table, id string) int {
	t.Helper()
	var n int
	require.NoError(t, s.db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE id = ?`, id).Scan(&n))
	return n
}

// A share, a replication rule and an inventory configuration with its
// reports, created, changed and deleted on either node, are the same on the
// other before the request returns.
func TestHARowChangesReachTheOtherNode(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "rows", "admin"))

	share, err := p.a.shareManager.CreateShare(ctx, "rows", "k", "", "AKID", "share-secret", "admin", nil)
	require.NoError(t, err)
	shareOnB, err := p.b.shareManager.GetShare(ctx, share.ID)
	require.NoError(t, err)
	assert.Equal(t, share.ShareToken, shareOnB.ShareToken)
	assert.Equal(t, "share-secret", shareOnB.SecretKey)

	require.NoError(t, p.a.replicationManager.CreateRule(ctx, testRule("rule-1", "rows")))
	ruleOnB, err := p.b.replicationManager.GetRule(ctx, "rule-1")
	require.NoError(t, err)
	require.NotNil(t, ruleOnB)
	assert.Equal(t, "rule-secret", ruleOnB.DestinationSecretKey)
	ruleOnB.Prefix = "from-b/"
	require.NoError(t, p.b.replicationManager.UpdateRule(ctx, ruleOnB))
	ruleOnA, err := p.a.replicationManager.GetRule(ctx, "rule-1")
	require.NoError(t, err)
	require.NotNil(t, ruleOnA)
	assert.Equal(t, "from-b/", ruleOnA.Prefix)

	cfg := testInventoryConfig("rows", "rows-dest")
	require.NoError(t, p.a.inventoryManager.CreateConfig(ctx, cfg))
	cfgOnB, err := p.b.inventoryManager.GetConfig(ctx, "rows", "")
	require.NoError(t, err)
	assert.Equal(t, cfg.ID, cfgOnB.ID)
	require.NoError(t, p.a.inventoryManager.CreateReport(ctx, &inventory.InventoryReport{ConfigID: cfg.ID, BucketName: "rows", Status: "completed"}))
	reports, err := p.b.inventoryManager.ListReports(ctx, "rows", "", 10, 0)
	require.NoError(t, err)
	assert.Len(t, reports, 1)

	require.NoError(t, p.a.inventoryManager.DeleteConfig(ctx, "rows", ""))
	_, err = p.b.inventoryManager.GetConfig(ctx, "rows", "")
	assert.Error(t, err)
	assert.Zero(t, rowCount(t, p.b, "bucket_inventory_reports", reports[0].ID))
	require.NoError(t, p.b.replicationManager.DeleteRule(ctx, "", "rule-1"))
	ruleOnA, err = p.a.replicationManager.GetRule(ctx, "rule-1")
	require.NoError(t, err)
	assert.Nil(t, ruleOnA)
	require.NoError(t, p.a.shareManager.DeleteShare(ctx, share.ID))
	_, err = p.b.shareManager.GetShare(ctx, share.ID)
	assert.Error(t, err)

	assert.Nil(t, missedSince(t, p.a, p.bID))
	assert.Nil(t, missedSince(t, p.b, p.aID))
}

// A node that missed row changes while down is caught up with them, and a row
// held from before rows were sent between nodes reaches the other node.
func TestHARowsMissedWhileDownAreCaughtUp(t *testing.T) {
	p := newHAPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "rows", "admin"))
	old, err := p.a.shareManager.CreateShare(ctx, "rows", "old", "", "AKID", "s", "admin", nil)
	require.NoError(t, err)
	rule := testRule("rule-1", "rows")
	require.NoError(t, p.a.replicationManager.CreateRule(ctx, rule))

	p.bDown.Store(true)
	fresh, err := p.a.shareManager.CreateShare(ctx, "rows", "fresh", "", "AKID", "s", "admin", nil)
	require.NoError(t, err)
	require.NoError(t, p.a.shareManager.DeleteShare(ctx, old.ID))
	rule.Prefix = "changed/"
	require.NoError(t, p.a.replicationManager.UpdateRule(ctx, rule))
	require.NotNil(t, missedSince(t, p.a, p.bID))
	_, err = p.b.db.ExecContext(ctx, `INSERT INTO replication_rules (id, tenant_id, source_bucket, destination_endpoint,
		destination_bucket, destination_access_key, destination_secret_key) VALUES ('from-before', '', 'rows', 'https://s3.example.com', 'far', 'ak', 'x')`)
	require.NoError(t, err)
	_, err = p.b.shareManager.GetShare(ctx, fresh.ID)
	require.Error(t, err)

	p.bDown.Store(false)
	p.a.antiEntropyScrubber.Start(ctx)
	p.b.antiEntropyScrubber.Start(ctx)
	require.Eventually(t, func() bool {
		_, freshErr := p.b.shareManager.GetShare(ctx, fresh.ID)
		ruleOnB, ruleErr := p.b.replicationManager.GetRule(ctx, rule.ID)
		return freshErr == nil && ruleErr == nil && ruleOnB != nil && ruleOnB.Prefix == "changed/" &&
			rowCount(t, p.b, "shares", old.ID) == 0 && rowCount(t, p.a, "replication_rules", "from-before") == 1
	}, 10*time.Second, 20*time.Millisecond)
}

// The coordinator writes a bucket's inventory report once; the report, its
// row and the configuration's run reach the other node.
func TestHAInventoryReportReachesEveryNode(t *testing.T) {
	p := newHAPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "inv-src", "admin"))
	require.NoError(t, p.a.bucketManager.CreateBucket(ctx, "", "inv-dest", "admin"))
	_, err := p.a.objectManager.PutObject(ctx, "inv-src", "listed-key", strings.NewReader("data"), http.Header{})
	require.NoError(t, err)
	cfg := testInventoryConfig("inv-src", "inv-dest")
	require.NoError(t, p.a.inventoryManager.CreateConfig(ctx, cfg))
	_, err = p.a.db.ExecContext(ctx, `UPDATE bucket_inventory_configs SET next_run_at = 1 WHERE id = ?`, cfg.ID)
	require.NoError(t, err)

	// The server stores reports through the object manager that sends its
	// writes to the other nodes when it starts in a cluster.
	p.a.inventoryWorker.SetObjectManager(cluster.NewHAObjectManager(p.a.objectManager, p.a.clusterManager))
	p.a.inventoryWorker.SetRunGate(func() bool { return true })
	p.a.inventoryWorker.Start(ctx, time.Hour)
	t.Cleanup(p.a.inventoryWorker.Stop)

	var report *inventory.InventoryReport
	require.Eventually(t, func() bool {
		reports, err := p.b.inventoryManager.ListReports(ctx, "inv-src", "", 10, 0)
		if err != nil || len(reports) == 0 || reports[0].Status != "completed" {
			return false
		}
		report = reports[0]
		cfgOnB, err := p.b.inventoryManager.GetConfig(ctx, "inv-src", "")
		return err == nil && cfgOnB.LastRunAt != nil
	}, 10*time.Second, 20*time.Millisecond, "the report and the run are recorded on every node")
	assert.Contains(t, readBody(t, p.b, "inv-dest", report.ReportPath), "listed-key")
	dest, err := p.a.bucketManager.GetBucketInfo(ctx, "", "inv-dest")
	require.NoError(t, err)
	assert.EqualValues(t, 1, dest.ObjectCount, "the report counts as an object of its bucket")
}

// With every node holding every bucket, a node that is not the coordinator
// writes no inventory report; a node outside such a cluster does.
func TestInventoryOnANodeThatIsNotTheCoordinator(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ready := func(s *Server) {
		t.Helper()
		require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "inv-src", "admin"))
		require.NoError(t, s.bucketManager.CreateBucket(ctx, "", "inv-dest", "admin"))
		cfg := testInventoryConfig("inv-src", "inv-dest")
		require.NoError(t, s.inventoryManager.CreateConfig(ctx, cfg))
		_, err := s.db.ExecContext(ctx, `UPDATE bucket_inventory_configs SET next_run_at = 1 WHERE id = ?`, cfg.ID)
		require.NoError(t, err)
		s.inventoryWorker.Start(ctx, 10*time.Millisecond)
		t.Cleanup(s.inventoryWorker.Stop)
	}
	reports := func(s *Server) int {
		list, err := s.inventoryManager.ListReports(ctx, "inv-src", "", 10, 0)
		require.NoError(t, err)
		return len(list)
	}

	standalone := newClusterTestNode(t)
	ready(standalone)
	require.Eventually(t, func() bool { return reports(standalone) > 0 }, 10*time.Second, 10*time.Millisecond)
	var versions int
	require.NoError(t, standalone.db.QueryRow(`SELECT COUNT(*) FROM ha_row_versions`).Scan(&versions))
	assert.Zero(t, versions, "a node outside a cluster dates no change")

	p := newHAPair(t)
	ready(p.a)
	time.Sleep(300 * time.Millisecond)
	assert.Zero(t, reports(p.a), "the coordinator writes it")
}

// Every node holds the rows of every bucket, whatever the factor: with a
// factor of 1 a change reaches the other node before the request returns.
func TestHARowsReachEveryNodeWhateverTheFactor(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	for _, s := range []*Server{p.a, p.b} {
		require.NoError(t, s.clusterManager.SetReplicationFactor(ctx, 1))
	}
	require.NoError(t, p.a.replicationManager.CreateRule(ctx, testRule("kept", "rows")))
	assert.Equal(t, 1, rowCount(t, p.b, "replication_rules", "kept"))
	assert.Equal(t, 1, rowCount(t, p.a, "ha_row_versions", "kept"), "the change is dated")
}

// A change that cannot be dated is not sent, and every other node is recorded
// as having missed it.
func TestHAUndatedRowChangeIsMissedByEveryNode(t *testing.T) {
	p := newHAPair(t)
	ctx := context.Background()
	_, err := p.a.db.ExecContext(ctx, `DROP TABLE ha_row_versions`)
	require.NoError(t, err)
	require.Nil(t, missedSince(t, p.a, p.bID))

	require.NoError(t, p.a.replicationManager.CreateRule(ctx, testRule("undated", "rows")))
	assert.NotNil(t, missedSince(t, p.a, p.bID))
	assert.Zero(t, rowCount(t, p.b, "replication_rules", "undated"))
}

// The tables the managers report changes of are the tables every node holds.
func TestManagersReportTheReplicatedTables(t *testing.T) {
	for _, table := range []string{share.SharesTable, replication.RulesTable, inventory.ConfigsTable, inventory.ReportsTable} {
		assert.True(t, cluster.ReplicatesTable(table), table)
	}
	assert.False(t, cluster.ReplicatesTable("users"))
}
