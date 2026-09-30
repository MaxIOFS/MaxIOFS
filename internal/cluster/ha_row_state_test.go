package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/db/migrations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rowsNode is a node's database, with every table and the foreign keys on as
// a server opens it, and its rows.
func rowsNode(t *testing.T) (*sql.DB, *RowStates) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rows-test-*")
	require.NoError(t, err)
	db, err := sql.Open("sqlite", filepath.Join(dir, "maxiofs.db")+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)")
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		os.RemoveAll(dir)
	})
	require.NoError(t, migrations.NewMigrationManager(db, nil).Migrate())
	require.NoError(t, InitSchema(db))
	return db, NewRowStates(NewManager(db, "", ""))
}

func execRows(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err, query)
}

const (
	insertInventoryConfig = `INSERT INTO bucket_inventory_configs (id, bucket_name, tenant_id, frequency, format,
		destination_bucket, included_fields, schedule_time, created_at, updated_at)
		VALUES (?, ?, ?, 'daily', 'csv', ?, '[]', '00:00', 1, 1)`
	insertInventoryReport = `INSERT INTO bucket_inventory_reports (id, config_id, bucket_name, report_path, status, created_at)
		VALUES (?, ?, ?, '', 'completed', 1)`
	insertRule = `INSERT INTO replication_rules (id, tenant_id, source_bucket, destination_endpoint, destination_bucket,
		destination_access_key, destination_secret_key, destination_region) VALUES (?, '', ?, 'http://dest', 'd', 'ak', 'sk', ?)`
	insertShare = `INSERT INTO shares (id, bucket_name, object_key, tenant_id, access_key_id, secret_key, share_token,
		expires_at, created_at, created_by) VALUES (?, ?, ?, '', 'ak', 'sk', ?, ?, 1, 'u')`
)

// rowOf reads a row as a node sends it.
func rowOf(t *testing.T, db *sql.DB, table, id string, version int64) *RowState {
	t.Helper()
	row, err := readRow(context.Background(), db, table, id)
	require.NoError(t, err)
	require.NotNil(t, row, "%s/%s", table, id)
	return &RowState{Table: table, ID: id, Version: version, Row: row}
}

func deletionOf(table, id string, version int64) *RowState {
	return &RowState{Table: table, ID: id, Version: version, Deleted: true}
}

func storeRows(t *testing.T, r *RowStates, states ...*RowState) []string {
	t.Helper()
	refused, err := r.Apply(context.Background(), states)
	require.NoError(t, err)
	return refused
}

func holds(t *testing.T, db *sql.DB, table, id string) bool {
	t.Helper()
	row, err := readRow(context.Background(), db, table, id)
	require.NoError(t, err)
	return row != nil
}

func column(t *testing.T, db *sql.DB, table, id, name string) string {
	t.Helper()
	var v sql.NullString
	err := db.QueryRow(fmt.Sprintf(`SELECT %s FROM %s WHERE id = ?`, name, table), id).Scan(&v)
	require.NoError(t, err)
	return v.String
}

func versionOf(t *testing.T, db *sql.DB, table, id string) rowVersion {
	t.Helper()
	v, err := readRowVersion(context.Background(), db, table, id)
	require.NoError(t, err)
	return v
}

// Two versions of a row are ordered by version; at the same version a
// deletion wins, then the greater content. A row a node does not hold, and
// whose deletion it did not record, is taken at any version.
func TestRowVersionsOrder(t *testing.T) {
	x := map[string]RowValue{"id": {Type: "text", Text: "r"}, "v": {Type: "text", Text: "x"}}
	y := map[string]RowValue{"id": {Type: "text", Text: "r"}, "v": {Type: "text", Text: "y"}}
	greater, lesser := x, y
	if rowDigest(y) > rowDigest(x) {
		greater, lesser = y, x
	}
	present := func(v int64, row map[string]RowValue) *RowState { return &RowState{Version: v, Row: row} }
	deleted := func(v int64) *RowState { return &RowState{Version: v, Deleted: true} }
	for _, c := range []struct {
		name  string
		st    *RowState
		local rowVersion
		row   map[string]RowValue
		want  bool
	}{
		{"unknown row, first version", present(0, x), rowVersion{}, nil, true},
		{"unknown row, deletion", deleted(5), rowVersion{}, nil, true},
		{"row dropped here without a record of it", present(3, x), rowVersion{version: 9}, nil, true},
		{"earlier than a deletion", present(9, x), rowVersion{version: 10, deleted: true}, nil, false},
		{"at the time of a deletion", present(10, x), rowVersion{version: 10, deleted: true}, nil, false},
		{"after a deletion", present(11, x), rowVersion{version: 10, deleted: true}, nil, true},
		{"deletion already recorded", deleted(10), rowVersion{version: 10, deleted: true}, nil, false},
		{"earlier deletion", deleted(9), rowVersion{version: 10, deleted: true}, nil, false},
		{"earlier version", present(9, x), rowVersion{version: 10}, y, false},
		{"later version", present(11, lesser), rowVersion{version: 10}, greater, true},
		{"deletion at the same version", deleted(10), rowVersion{version: 10}, x, true},
		{"earlier deletion of a held row", deleted(9), rowVersion{version: 10}, x, false},
		{"same version, greater content", present(10, greater), rowVersion{version: 10}, lesser, true},
		{"same version, lesser content", present(10, lesser), rowVersion{version: 10}, greater, false},
		{"same version, same content", present(10, x), rowVersion{version: 10}, x, false},
		{"unversioned row, greater content", present(0, greater), rowVersion{}, lesser, true},
		{"unversioned row, lesser content", present(0, lesser), rowVersion{}, greater, false},
	} {
		assert.Equal(t, c.want, newer(c.st, c.local, c.row), c.name)
	}
}

// A row replaces the one a node holds only when it is newer; a deletion
// removes it and keeps an older version from bringing it back.
func TestRowsAreStoredWhenNewer(t *testing.T) {
	src, _ := rowsNode(t)
	dst, rows := rowsNode(t)
	execRows(t, src, insertRule, "rule", "b", "first")
	first := rowOf(t, src, "replication_rules", "rule", 10)
	execRows(t, src, `UPDATE replication_rules SET destination_region = 'second' WHERE id = 'rule'`)
	second := rowOf(t, src, "replication_rules", "rule", 20)

	storeRows(t, rows, first)
	assert.Equal(t, "first", column(t, dst, "replication_rules", "rule", "destination_region"))
	assert.Equal(t, rowVersion{version: 10}, versionOf(t, dst, "replication_rules", "rule"))
	storeRows(t, rows, second)
	storeRows(t, rows, first)
	assert.Equal(t, "second", column(t, dst, "replication_rules", "rule", "destination_region"))
	assert.Equal(t, rowVersion{version: 20}, versionOf(t, dst, "replication_rules", "rule"))

	storeRows(t, rows, deletionOf("replication_rules", "rule", 15))
	assert.True(t, holds(t, dst, "replication_rules", "rule"), "an earlier deletion")
	storeRows(t, rows, deletionOf("replication_rules", "rule", 30))
	assert.False(t, holds(t, dst, "replication_rules", "rule"))
	assert.Equal(t, rowVersion{version: 30, deleted: true}, versionOf(t, dst, "replication_rules", "rule"))
	storeRows(t, rows, second)
	assert.False(t, holds(t, dst, "replication_rules", "rule"), "a version older than the deletion")
	second.Version = 40
	storeRows(t, rows, second)
	assert.True(t, holds(t, dst, "replication_rules", "rule"), "a version after the deletion")
	assert.Equal(t, rowVersion{version: 40}, versionOf(t, dst, "replication_rules", "rule"))
}

// A row is changed in place: the rows that reference it stay. A deletion takes
// them with it, as the table does.
func TestAStoredRowKeepsTheRowsThatReferenceIt(t *testing.T) {
	src, _ := rowsNode(t)
	dst, rows := rowsNode(t)
	execRows(t, src, insertInventoryConfig, "inv", "b", nil, "d1")
	execRows(t, src, insertRule, "rule", "b", "")
	execRows(t, dst, insertInventoryConfig, "inv", "b", nil, "d0")
	execRows(t, dst, insertInventoryReport, "rep", "inv", "b")
	execRows(t, dst, insertRule, "rule", "b", "")
	execRows(t, dst, `INSERT INTO replication_queue (rule_id, tenant_id, bucket, object_key, action) VALUES ('rule', '', 'b', 'k', 'PUT')`)

	storeRows(t, rows, rowOf(t, src, "bucket_inventory_configs", "inv", 5), rowOf(t, src, "replication_rules", "rule", 5))
	assert.Equal(t, "d1", column(t, dst, "bucket_inventory_configs", "inv", "destination_bucket"))
	assert.True(t, holds(t, dst, "bucket_inventory_reports", "rep"), "the report of a changed configuration stays")
	var queued int
	require.NoError(t, dst.QueryRow(`SELECT COUNT(*) FROM replication_queue WHERE rule_id = 'rule'`).Scan(&queued))
	assert.Equal(t, 1, queued, "the queue of a changed rule stays")

	storeRows(t, rows, deletionOf("bucket_inventory_configs", "inv", 6))
	assert.False(t, holds(t, dst, "bucket_inventory_reports", "rep"))
}

// A report is stored after its configuration; one whose configuration was
// deleted is not, and one whose configuration never arrived is refused.
func TestAReportFollowsItsConfiguration(t *testing.T) {
	src, _ := rowsNode(t)
	dst, rows := rowsNode(t)
	execRows(t, src, insertInventoryConfig, "inv", "b", nil, "d")
	execRows(t, src, insertInventoryReport, "rep", "inv", "b")
	report := rowOf(t, src, "bucket_inventory_reports", "rep", 5)

	refused := storeRows(t, rows, report)
	require.Len(t, refused, 1)
	assert.Contains(t, refused[0], "bucket_inventory_reports/rep")
	assert.Contains(t, refused[0], "FOREIGN KEY")
	assert.False(t, holds(t, dst, "bucket_inventory_reports", "rep"))

	assert.Empty(t, storeRows(t, rows, rowOf(t, src, "bucket_inventory_configs", "inv", 1), report))
	assert.True(t, holds(t, dst, "bucket_inventory_reports", "rep"))

	storeRows(t, rows, deletionOf("bucket_inventory_configs", "inv", 7))
	report.Version = 8
	assert.Empty(t, storeRows(t, rows, report), "a report of a deleted configuration is dropped, not refused")
	assert.False(t, holds(t, dst, "bucket_inventory_reports", "rep"))
}

// Two rows that may not coexist, created on two nodes, end as one on both: the
// later, or at the same version the one of the greater id. The loser's
// deletion is recorded, so it goes to the nodes that still hold it.
func TestRowsThatMayNotCoexistConvergeOnOne(t *testing.T) {
	src, _ := rowsNode(t)
	execRows(t, src, insertShare, "s1", "b", "k", "tok-1", nil)
	execRows(t, src, insertShare, "s0", "b", "other", "tok-0", nil)
	s1 := rowOf(t, src, "shares", "s1", 10)
	execRows(t, src, `DELETE FROM shares WHERE id = 's1'`)
	execRows(t, src, insertShare, "s2", "b", "k", "tok-2", nil)
	s2 := rowOf(t, src, "shares", "s2", 20)
	sameToken := rowOf(t, src, "shares", "s0", 30)
	sameToken.Row["share_token"] = RowValue{Type: "text", Text: "tok-2"}

	dst, rows := rowsNode(t)
	storeRows(t, rows, s1)
	storeRows(t, rows, s2)
	assert.False(t, holds(t, dst, "shares", "s1"))
	assert.True(t, holds(t, dst, "shares", "s2"))
	assert.Equal(t, rowVersion{version: 20, deleted: true}, versionOf(t, dst, "shares", "s1"))
	storeRows(t, rows, s1)
	assert.False(t, holds(t, dst, "shares", "s1"), "the loser does not come back")

	storeRows(t, rows, sameToken)
	assert.True(t, holds(t, dst, "shares", "s0"), "a later row wins over one of the same token")
	assert.False(t, holds(t, dst, "shares", "s2"))

	for _, order := range [][]*RowState{{s1, s2}, {s2, s1}} {
		dst, rows := rowsNode(t)
		a, b := *order[0], *order[1]
		a.Version, b.Version = 50, 50
		storeRows(t, rows, &a, &b)
		assert.True(t, holds(t, dst, "shares", "s2"), "the greater id wins at the same version")
		assert.False(t, holds(t, dst, "shares", "s1"))
	}
}

// Inventory configurations of a bucket without a tenant never conflict, as in
// the table; those of a tenant's bucket do.
func TestInventoryConfigurationsConflictAsTheTableSays(t *testing.T) {
	src, _ := rowsNode(t)
	dst, rows := rowsNode(t)
	execRows(t, src, `INSERT INTO tenants (id, name, display_name, created_at, updated_at) VALUES ('t1', 't1', 't1', 1, 1)`)
	execRows(t, dst, `INSERT INTO tenants (id, name, display_name, created_at, updated_at) VALUES ('t1', 't1', 't1', 1, 1)`)
	for _, id := range []string{"g1", "g2"} {
		execRows(t, src, insertInventoryConfig, id, "global", nil, "d")
	}
	execRows(t, src, insertInventoryConfig, "t-a", "tb", "t1", "d")
	ta := rowOf(t, src, "bucket_inventory_configs", "t-a", 5)
	execRows(t, src, `DELETE FROM bucket_inventory_configs WHERE id = 't-a'`)
	execRows(t, src, insertInventoryConfig, "t-b", "tb", "t1", "d")
	tb := rowOf(t, src, "bucket_inventory_configs", "t-b", 6)

	storeRows(t, rows, rowOf(t, src, "bucket_inventory_configs", "g1", 1), rowOf(t, src, "bucket_inventory_configs", "g2", 1), ta, tb)
	assert.True(t, holds(t, dst, "bucket_inventory_configs", "g1"))
	assert.True(t, holds(t, dst, "bucket_inventory_configs", "g2"))
	assert.False(t, holds(t, dst, "bucket_inventory_configs", "t-a"))
	assert.True(t, holds(t, dst, "bucket_inventory_configs", "t-b"))
}

// A row this node cannot store is refused with its reason; the rest of the
// delivery is stored.
func TestRowsThisNodeCannotStoreAreRefused(t *testing.T) {
	src, _ := rowsNode(t)
	dst, rows := rowsNode(t)
	execRows(t, src, insertRule, "ok", "b", "")
	execRows(t, src, insertInventoryConfig, "no-tenant", "b", nil, "d")
	execRows(t, src, insertInventoryConfig, "bad-check", "c", nil, "d")

	orphan := rowOf(t, src, "bucket_inventory_configs", "no-tenant", 1)
	orphan.Row["tenant_id"] = RowValue{Type: "text", Text: "missing-tenant"}
	badCheck := rowOf(t, src, "bucket_inventory_configs", "bad-check", 1)
	badCheck.Row["frequency"] = RowValue{Type: "text", Text: "hourly"}
	unknownColumn := rowOf(t, src, "replication_rules", "ok", 1)
	unknownColumn.ID, unknownColumn.Row = "u", copyRow(unknownColumn.Row)
	unknownColumn.Row["id"] = RowValue{Type: "text", Text: "u"}
	unknownColumn.Row["no_such_column"] = RowValue{Type: "int", Int: 1}
	otherID := rowOf(t, src, "replication_rules", "ok", 1)
	otherID.ID = "not-the-row"
	badType := rowOf(t, src, "replication_rules", "ok", 1)
	badType.ID, badType.Row = "t", copyRow(badType.Row)
	badType.Row["id"] = RowValue{Type: "text", Text: "t"}
	badType.Row["priority"] = RowValue{Type: "complex"}

	refused := storeRows(t, rows,
		&RowState{Table: "users", ID: "u1", Row: map[string]RowValue{"id": {Type: "text", Text: "u1"}}},
		&RowState{Table: "shares", Deleted: true},
		nil,
		orphan, badCheck, unknownColumn, otherID, badType,
		rowOf(t, src, "replication_rules", "ok", 1),
	)
	assert.Len(t, refused, 8, "%v", refused)
	assert.True(t, holds(t, dst, "replication_rules", "ok"))
	for _, id := range []string{"no-tenant", "bad-check"} {
		assert.False(t, holds(t, dst, "bucket_inventory_configs", id))
	}
	for _, id := range []string{"u", "t", "not-the-row"} {
		assert.False(t, holds(t, dst, "replication_rules", id))
		assert.Equal(t, rowVersion{}, versionOf(t, dst, "replication_rules", id))
	}
}

func copyRow(row map[string]RowValue) map[string]RowValue {
	out := make(map[string]RowValue, len(row))
	for k, v := range row {
		out[k] = v
	}
	return out
}

// An expired share, which every node drops on its own, is not stored.
func TestExpiredSharesAreNotStored(t *testing.T) {
	src, _ := rowsNode(t)
	dst, rows := rowsNode(t)
	execRows(t, src, insertShare, "old", "b", "k1", "tok-old", time.Now().Add(-time.Minute).Unix())
	execRows(t, src, insertShare, "new", "b", "k2", "tok-new", time.Now().Add(time.Hour).Unix())
	assert.Empty(t, storeRows(t, rows, rowOf(t, src, "shares", "old", 1), rowOf(t, src, "shares", "new", 1)))
	assert.False(t, holds(t, dst, "shares", "old"))
	assert.True(t, holds(t, dst, "shares", "new"))
}

// A change is given a version after any the node holds of the row, even with
// the clock behind it; a row deleted since it changed is not sent.
func TestAChangeIsGivenALaterVersion(t *testing.T) {
	db, rows := rowsNode(t)
	ctx := context.Background()
	t0 := time.Now().UnixNano()
	execRows(t, db, insertRule, "rule", "b", "")
	rules, _ := lookupReplicatedTable("replication_rules")

	st, err := rows.record(ctx, rules, "rule", false)
	require.NoError(t, err)
	require.NotNil(t, st)
	assert.GreaterOrEqual(t, st.Version, t0)
	assert.Equal(t, "rule", st.Row["id"].Text)
	assert.Equal(t, rowVersion{version: st.Version}, versionOf(t, db, "replication_rules", "rule"))

	ahead := time.Now().Add(time.Hour).UnixNano()
	require.NoError(t, writeRowVersion(ctx, db, "replication_rules", "rule", rowVersion{version: ahead}))
	st, err = rows.record(ctx, rules, "rule", false)
	require.NoError(t, err)
	assert.Equal(t, ahead+1, st.Version)

	execRows(t, db, `DELETE FROM replication_rules WHERE id = 'rule'`)
	st, err = rows.record(ctx, rules, "rule", false)
	require.NoError(t, err)
	assert.Nil(t, st, "the row is gone: its deletion is sent instead")
	assert.Equal(t, rowVersion{version: ahead + 1}, versionOf(t, db, "replication_rules", "rule"))
	st, err = rows.record(ctx, rules, "rule", true)
	require.NoError(t, err)
	require.NotNil(t, st)
	assert.True(t, st.Deleted)
	assert.Nil(t, st.Row)
	assert.Equal(t, rowVersion{version: ahead + 2, deleted: true}, versionOf(t, db, "replication_rules", "rule"))
}

// A synchronization sends every row, configurations before their reports,
// with the version the node holds, then every recorded deletion, a batch at a
// time. Expired shares and deletions of rows held again are not sent.
func TestSyncSendsEveryRow(t *testing.T) {
	db, rows := rowsNode(t)
	ctx := context.Background()
	execRows(t, db, insertInventoryConfig, "inv", "b", nil, "d")
	execRows(t, db, insertInventoryReport, "rep", "inv", "b")
	execRows(t, db, insertRule, "rule", "b", "")
	for i := 0; i < 150; i++ {
		execRows(t, db, insertShare, fmt.Sprintf("s%03d", i), "b", fmt.Sprintf("k%03d", i), fmt.Sprintf("tok-%03d", i), nil)
	}
	execRows(t, db, insertShare, "expired", "b", "gone", "tok-gone", time.Now().Add(-time.Minute).Unix())
	require.NoError(t, writeRowVersion(ctx, db, "bucket_inventory_configs", "inv", rowVersion{version: 77}))
	require.NoError(t, writeRowVersion(ctx, db, "shares", "deleted-share", rowVersion{version: 88, deleted: true}))
	require.NoError(t, writeRowVersion(ctx, db, "replication_rules", "rule", rowVersion{version: 99, deleted: true}))
	require.NoError(t, writeRowVersion(ctx, db, "users", "someone", rowVersion{version: 5, deleted: true}))
	// A row this node no longer holds without a recorded deletion, as one a
	// bucket migration moved to another node.
	require.NoError(t, writeRowVersion(ctx, db, "shares", "moved-away", rowVersion{version: 66}))

	var mu sync.Mutex
	var batches [][]*RowState
	status := http.StatusOK
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/internal/cluster/ha/row-states", r.URL.Path)
		var batch RowStateBatch
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&batch))
		mu.Lock()
		batches = append(batches, batch.Rows)
		answer := status
		mu.Unlock()
		if answer != http.StatusOK {
			http.Error(w, "no", answer)
			return
		}
		json.NewEncoder(w).Encode(RowStateResult{Refused: []string{"shares/s000: refused"}}) //nolint:errcheck
	}))
	t.Cleanup(peer.Close)
	node := &Node{ID: "peer", Endpoint: peer.URL, NodeToken: "token"}
	client := NewProxyClient(nil)

	require.NoError(t, rows.syncNode(ctx, client, node, "local"), "refused rows are logged")
	mu.Lock()
	require.Len(t, batches, 2)
	assert.Len(t, batches[0], rowBatchSize)
	var sent []*RowState
	for _, b := range batches {
		sent = append(sent, b...)
	}
	require.Len(t, sent, 3+150+1)
	index := map[string]int{}
	for i, st := range sent {
		index[st.Table+"/"+st.ID] = i
	}
	assert.Less(t, index["bucket_inventory_configs/inv"], index["bucket_inventory_reports/rep"])
	assert.EqualValues(t, 77, sent[index["bucket_inventory_configs/inv"]].Version)
	assert.EqualValues(t, 0, sent[index["shares/s042"]].Version)
	assert.Equal(t, "k042", sent[index["shares/s042"]].Row["object_key"].Text)
	assert.NotContains(t, index, "shares/expired")
	assert.NotContains(t, index, "users/someone")
	assert.NotContains(t, index, "shares/moved-away", "a row not held is not sent as deleted")
	rule := sent[index["replication_rules/rule"]]
	assert.False(t, rule.Deleted, "a row held again is sent, not its deletion")
	deletion := sent[len(sent)-1]
	assert.Equal(t, RowState{Table: "shares", ID: "deleted-share", Version: 88, Deleted: true}, *deletion)
	mu.Unlock()

	answer := func(code int) {
		mu.Lock()
		defer mu.Unlock()
		batches, status = nil, code
	}
	answer(http.StatusBadRequest)
	assert.NoError(t, rows.syncNode(ctx, client, node, "local"), "a refused delivery is logged")
	answer(http.StatusInternalServerError)
	assert.Error(t, rows.syncNode(ctx, client, node, "local"))
	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, batches, 1, "the synchronization stops at a failed delivery")
}

// Deletions are forgotten with the deletion log, and so are the versions of
// rows a node dropped without recording a deletion.
func TestRowVersionsAreForgotten(t *testing.T) {
	db, _ := rowsNode(t)
	ctx := context.Background()
	old := time.Now().Add(-8 * 24 * time.Hour).UnixNano()
	recent := time.Now().UnixNano()
	execRows(t, db, insertRule, "held", "b", "")
	for _, rec := range []rowVersionRecord{
		{"shares", "old-deletion", rowVersion{version: old, deleted: true}},
		{"shares", "recent-deletion", rowVersion{version: recent, deleted: true}},
		{"shares", "dropped", rowVersion{version: recent}},
		{"replication_rules", "held", rowVersion{version: old}},
	} {
		require.NoError(t, writeRowVersion(ctx, db, rec.table, rec.id, rec.rowVersion))
	}

	count, err := cleanupRowVersions(ctx, db, 7*24*time.Hour)
	require.NoError(t, err)
	assert.EqualValues(t, 2, count)
	count, err = cleanupRowVersions(ctx, db, 7*24*time.Hour)
	require.NoError(t, err)
	assert.Zero(t, count)
	records, err := readRowVersions(ctx, db)
	require.NoError(t, err)
	var kept []string
	for _, rec := range records {
		kept = append(kept, rec.table+"/"+rec.id)
	}
	assert.Equal(t, []string{"replication_rules/held", "shares/recent-deletion"}, kept)

	bare, cleanup := setupTestDB(t)
	defer cleanup()
	_, err = cleanupRowVersions(ctx, bare, time.Hour)
	assert.NoError(t, err, "a database without the tables has nothing to forget")

	require.NoError(t, writeRowVersion(ctx, db, "shares", "forgotten-by-the-loop", rowVersion{version: old, deleted: true}))
	loop, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		RunDeletionLogCleanup(loop, db, 10*time.Millisecond, 7*24*time.Hour)
		close(done)
	}()
	require.Eventually(t, func() bool {
		v, err := readRowVersion(ctx, db, "shares", "forgotten-by-the-loop")
		return err == nil && v == rowVersion{}
	}, 5*time.Second, 10*time.Millisecond, "the deletion log's cleanup forgets them")
	stop()
	<-done
}
