package cluster

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// With a replication factor above 1 every node also holds the rows a bucket
// keeps in the node's database: its inventory configurations and reports, its
// replication rules and its shares. A change is sent to every other node
// before the request returns; a node that misses it is sent every row with the
// buckets. Two versions of a row are ordered by the time of the change; at the
// same time a deletion wins, then the greater content.

// ErrRowStateRefused is a node's answer to a row it cannot store: an unknown
// table or column, or a row that breaks a constraint of the table there.
var ErrRowStateRefused = errors.New("the row cannot be stored on this node")

// rowBatchSize is how many rows a synchronization sends at a time.
const rowBatchSize = 100

// replicatedTable is a table whose rows every node holds, found by id.
type replicatedTable struct {
	name string
	// unique are the sets of columns, besides id, no two rows share.
	unique [][]string
	// parent is the table a row belongs to, through parentColumn: a row whose
	// parent was deleted is not stored.
	parent, parentColumn string
	// expires is the column, unix seconds, after which every node drops the
	// row on its own; such a row is neither sent nor stored.
	expires string
}

// replicatedTables, parents before the rows that reference them.
var replicatedTables = []replicatedTable{
	{name: "bucket_inventory_configs", unique: [][]string{{"bucket_name", "tenant_id"}}},
	{name: "bucket_inventory_reports", parent: "bucket_inventory_configs", parentColumn: "config_id"},
	{name: "replication_rules"},
	{name: "shares", unique: [][]string{{"bucket_name", "object_key", "tenant_id"}, {"share_token"}}, expires: "expires_at"},
}

func lookupReplicatedTable(name string) (replicatedTable, bool) {
	for _, t := range replicatedTables {
		if t.name == name {
			return t, true
		}
	}
	return replicatedTable{}, false
}

// ReplicatesTable reports whether every node holds the rows of the table.
func ReplicatesTable(name string) bool {
	_, ok := lookupReplicatedTable(name)
	return ok
}

func (t replicatedTable) expired(row map[string]RowValue, now time.Time) bool {
	if t.expires == "" {
		return false
	}
	v := row[t.expires]
	return v.Type == "int" && v.Int < now.Unix()
}

// RowState is what a node tells another about a row: its columns, or that it
// was deleted.
type RowState struct {
	Table string `json:"table"`
	ID    string `json:"id"`
	// Version is the time of the change, unix nanoseconds; 0 for a row not
	// changed since it was first held in a cluster.
	Version int64               `json:"version"`
	Deleted bool                `json:"deleted,omitempty"`
	Row     map[string]RowValue `json:"row,omitempty"`
}

// RowStateBatch is the body of a delivery of rows.
type RowStateBatch struct {
	Rows []*RowState `json:"rows"`
}

// RowStateResult is a node's answer to a delivery: the rows it refused, and
// why.
type RowStateResult struct {
	Refused []string `json:"refused,omitempty"`
}

type rowVersion struct {
	version int64
	deleted bool
}

// sqlConn is a database or a transaction.
type sqlConn interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// readRowVersion returns the version this node holds of a row: zero for one
// it has no record of.
func readRowVersion(ctx context.Context, q sqlConn, table, id string) (rowVersion, error) {
	var v rowVersion
	err := q.QueryRowContext(ctx, `SELECT version, deleted FROM ha_row_versions WHERE tbl = ? AND id = ?`, table, id).
		Scan(&v.version, &v.deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return rowVersion{}, nil
	}
	return v, err
}

func writeRowVersion(ctx context.Context, q sqlConn, table, id string, v rowVersion) error {
	_, err := q.ExecContext(ctx, `
		INSERT INTO ha_row_versions (tbl, id, version, deleted) VALUES (?, ?, ?, ?)
		ON CONFLICT(tbl, id) DO UPDATE SET version = excluded.version, deleted = excluded.deleted`,
		table, id, v.version, v.deleted)
	return err
}

// readRow returns the row of table with id, or nil.
func readRow(ctx context.Context, q sqlConn, table, id string) (map[string]RowValue, error) {
	rows, err := q.QueryContext(ctx, fmt.Sprintf(`SELECT * FROM %s WHERE id = ?`, table), id)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", table, err)
	}
	found, err := scanRows(rows, table, "")
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return found[0], nil
}

func queryIDs(ctx context.Context, q sqlConn, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RowStates sends this node's rows to the other nodes and stores theirs.
type RowStates struct {
	mgr *Manager
	// mu orders the version a change is given, and the row read with it,
	// against the rows stored from other nodes.
	mu sync.Mutex
}

// NewRowStates wires the rows of the replicated tables in mgr's database.
func NewRowStates(mgr *Manager) *RowStates {
	return &RowStates{mgr: mgr}
}

// Changed records a change this node made to a row, as the manager of its
// table reports it, and sends it to every other node before returning. A node
// that is not healthy, or does not take it, is recorded as having missed a
// write and is sent every row when it is caught up.
func (r *RowStates) Changed(ctx context.Context, table, id string, deleted bool) {
	if r == nil || !r.mgr.IsClusterEnabled() {
		return
	}
	// The change is stored: it is recorded even if the request is gone.
	ctx = context.WithoutCancel(ctx)
	log := logrus.WithFields(logrus.Fields{"table": table, "id": id})
	t, ok := lookupReplicatedTable(table)
	if !ok {
		log.Error("HA: a change reported for a table that is not replicated")
		return
	}
	st, err := r.record(ctx, t, id, deleted)
	if err != nil {
		log.WithError(err).Warn("HA: a changed row could not be read; the other nodes get it when they are next synchronized")
		if replicatesBuckets(ctx, r.mgr) {
			missedByAll(ctx, r.mgr)
		}
		return
	}
	if st == nil || !replicatesBuckets(ctx, r.mgr) {
		return
	}
	fanout(ctx, r.mgr, st.Table+"/"+st.ID, func(ctx context.Context, client *ProxyClient, n *Node, localID string) error {
		return r.deliver(ctx, client, n, localID, []*RowState{st})
	})
}

// record gives a change a version later than any this node holds of the row,
// and reads the row as it is sent: nil for a row deleted since, whose
// deletion is recorded on its own.
func (r *RowStates) record(ctx context.Context, t replicatedTable, id string, deleted bool) (*RowState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	db := r.mgr.db
	current, err := readRowVersion(ctx, db, t.name, id)
	if err != nil {
		return nil, err
	}
	st := &RowState{Table: t.name, ID: id, Version: time.Now().UnixNano(), Deleted: deleted}
	if st.Version <= current.version {
		st.Version = current.version + 1
	}
	if !deleted {
		if st.Row, err = readRow(ctx, db, t.name, id); err != nil || st.Row == nil {
			return nil, err
		}
	}
	if err := writeRowVersion(ctx, db, t.name, id, rowVersion{version: st.Version, deleted: deleted}); err != nil {
		return nil, err
	}
	return st, nil
}

// deliver sends states to node and logs the rows it refused, or the refusal of
// the whole delivery. Any other failure is returned.
func (r *RowStates) deliver(ctx context.Context, client *ProxyClient, node *Node, localID string, states []*RowState) error {
	refused, err := sendRowStates(ctx, client, node, localID, states)
	if errors.Is(err, errStateRefused) {
		logrus.WithError(err).WithField("node_id", node.ID).Error("HA: a node refused rows")
		return nil
	}
	for _, reason := range refused {
		logrus.WithFields(logrus.Fields{"node_id": node.ID, "row": reason}).Error("HA: a node refused a row")
	}
	return err
}

// syncNode sends node every row this node holds and every deletion it
// recorded, rowBatchSize at a time.
func (r *RowStates) syncNode(ctx context.Context, client *ProxyClient, node *Node, localID string) error {
	db := r.mgr.db
	// The versions are read before the rows: a row changed in between is sent
	// with its earlier version, and then with its own.
	records, err := readRowVersions(ctx, db)
	if err != nil {
		return fmt.Errorf("read row versions: %w", err)
	}
	versions := make(map[[2]string]rowVersion, len(records))
	for _, rec := range records {
		versions[[2]string{rec.table, rec.id}] = rec.rowVersion
	}
	batch := make([]*RowState, 0, rowBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := r.deliver(ctx, client, node, localID, batch)
		batch = batch[:0]
		return err
	}
	add := func(st *RowState) error {
		batch = append(batch, st)
		if len(batch) < rowBatchSize {
			return nil
		}
		return flush()
	}

	now := time.Now()
	held := map[[2]string]bool{}
	for _, t := range replicatedTables {
		rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT * FROM %s ORDER BY id`, t.name))
		if err != nil {
			return fmt.Errorf("read %s: %w", t.name, err)
		}
		all, err := scanRows(rows, t.name, "")
		if err != nil {
			return err
		}
		for _, row := range all {
			if t.expired(row, now) {
				continue
			}
			key := [2]string{t.name, row["id"].Text}
			held[key] = true
			if err := add(&RowState{Table: t.name, ID: key[1], Version: versions[key].version, Row: row}); err != nil {
				return err
			}
		}
	}
	for _, rec := range records {
		if !rec.deleted || held[[2]string{rec.table, rec.id}] || !ReplicatesTable(rec.table) {
			continue
		}
		if err := add(&RowState{Table: rec.table, ID: rec.id, Version: rec.version, Deleted: true}); err != nil {
			return err
		}
	}
	return flush()
}

type rowVersionRecord struct {
	table, id string
	rowVersion
}

func readRowVersions(ctx context.Context, db *sql.DB) ([]rowVersionRecord, error) {
	rows, err := db.QueryContext(ctx, `SELECT tbl, id, version, deleted FROM ha_row_versions ORDER BY tbl, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []rowVersionRecord
	for rows.Next() {
		var rec rowVersionRecord
		if err := rows.Scan(&rec.table, &rec.id, &rec.version, &rec.deleted); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Apply stores the rows another node sent, each only when it is newer than
// what this node holds, and returns the rows it refused and why. A failure to
// read or write the database is returned.
func (r *RowStates) Apply(ctx context.Context, states []*RowState) ([]string, error) {
	var refused []string
	for _, st := range states {
		if st == nil {
			refused = append(refused, "an empty row")
			continue
		}
		err := r.apply(ctx, st)
		if errors.Is(err, ErrRowStateRefused) {
			refused = append(refused, fmt.Sprintf("%s/%s: %v", st.Table, st.ID, err))
			continue
		}
		if err != nil {
			return refused, fmt.Errorf("%s/%s: %w", st.Table, st.ID, err)
		}
	}
	return refused, nil
}

func (r *RowStates) apply(ctx context.Context, st *RowState) error {
	t, ok := lookupReplicatedTable(st.Table)
	switch {
	case !ok:
		return fmt.Errorf("%w: the table is not replicated", ErrRowStateRefused)
	case st.ID == "":
		return fmt.Errorf("%w: no id", ErrRowStateRefused)
	case !st.Deleted && (st.Row["id"].Type != "text" || st.Row["id"].Text != st.ID):
		return fmt.Errorf("%w: the row has another id", ErrRowStateRefused)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	tx, err := r.mgr.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := storeRow(ctx, tx, t, st); err != nil {
		return err
	}
	return tx.Commit()
}

// storeRow stores st when it is newer than the row this node holds.
func storeRow(ctx context.Context, tx *sql.Tx, t replicatedTable, st *RowState) error {
	local, err := readRowVersion(ctx, tx, t.name, st.ID)
	if err != nil {
		return err
	}
	row, err := readRow(ctx, tx, t.name, st.ID)
	if err != nil {
		return err
	}
	if !newer(st, local, row) {
		return nil
	}
	if st.Deleted {
		return deleteRow(ctx, tx, t.name, st.ID, st.Version)
	}
	if t.expired(st.Row, time.Now()) {
		return nil
	}
	if t.parent != "" {
		if gone, err := parentDeleted(ctx, tx, t, st.Row); err != nil || gone {
			return err
		}
	}
	losers, wins, err := conflicts(ctx, tx, t, st)
	if err != nil || !wins {
		return err
	}
	for _, id := range losers {
		if err := deleteRow(ctx, tx, t.name, id, st.Version); err != nil {
			return err
		}
	}
	if err := upsertRow(ctx, tx, t.name, st.Row); err != nil {
		return err
	}
	return writeRowVersion(ctx, tx, t.name, st.ID, rowVersion{version: st.Version})
}

// newer reports whether st is later than what this node holds of the row: a
// later version; at the same version a deletion, then the greater content. A
// row this node does not hold, and whose deletion it did not record, is taken
// at any version.
func newer(st *RowState, local rowVersion, row map[string]RowValue) bool {
	switch {
	case row == nil && !local.deleted:
		return true
	case st.Version != local.version:
		return st.Version > local.version
	case st.Deleted || local.deleted:
		return st.Deleted && !local.deleted
	}
	return rowDigest(st.Row) > rowDigest(row)
}

func rowDigest(row map[string]RowValue) string {
	b, err := json.Marshal(row)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// deleteRow deletes a row and records the deletion at version.
func deleteRow(ctx context.Context, tx *sql.Tx, table, id string, version int64) error {
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = ?`, table), id); err != nil {
		return fmt.Errorf("delete %s row: %w", table, err)
	}
	return writeRowVersion(ctx, tx, table, id, rowVersion{version: version, deleted: true})
}

// parentDeleted reports whether this node recorded the deletion of the row's
// parent: such a row is not stored. A row whose parent never arrived breaks
// the table's foreign key and is refused.
func parentDeleted(ctx context.Context, tx *sql.Tx, t replicatedTable, row map[string]RowValue) (bool, error) {
	v, err := readRowVersion(ctx, tx, t.parent, row[t.parentColumn].Text)
	return v.deleted, err
}

// conflicts returns the rows of this node that share a unique set of columns
// with st and lose to it: those of an earlier version, at the same version of
// a smaller id. wins is false when one of them wins instead. As in the table,
// rows sharing a null never conflict.
func conflicts(ctx context.Context, tx *sql.Tx, t replicatedTable, st *RowState) (losers []string, wins bool, err error) {
	seen := map[string]bool{}
	for _, columns := range t.unique {
		where := make([]string, 0, len(columns))
		args := make([]any, 0, len(columns)+1)
		for _, c := range columns {
			v, ok := st.Row[c]
			if !ok {
				break
			}
			value, err := v.value()
			if err != nil {
				return nil, false, fmt.Errorf("%w: %s: %v", ErrRowStateRefused, c, err)
			}
			where = append(where, `"`+c+`" = ?`)
			args = append(args, value)
		}
		if len(where) < len(columns) {
			continue
		}
		args = append(args, st.ID)
		ids, err := queryIDs(ctx, tx, fmt.Sprintf(`SELECT id FROM %s WHERE %s AND id != ?`, t.name, strings.Join(where, " AND ")), args...)
		if err != nil {
			return nil, false, err
		}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			v, err := readRowVersion(ctx, tx, t.name, id)
			if err != nil {
				return nil, false, err
			}
			if v.version > st.Version || (v.version == st.Version && id > st.ID) {
				return nil, false, nil
			}
			losers = append(losers, id)
		}
	}
	return losers, true, nil
}

// upsertRow writes row over the one of its id. The row is updated in place:
// replacing it would delete the rows that reference it.
func upsertRow(ctx context.Context, tx *sql.Tx, table string, row map[string]RowValue) error {
	known, err := tableColumns(ctx, tx, table)
	if err != nil {
		return err
	}
	columns := make([]string, 0, len(row))
	for c := range row {
		columns = append(columns, c)
	}
	sort.Strings(columns)
	names := make([]string, len(columns))
	args := make([]any, len(columns))
	var updates []string
	for i, c := range columns {
		if !known[c] {
			return fmt.Errorf("%w: column %q does not exist on this node", ErrRowStateRefused, c)
		}
		value, err := row[c].value()
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrRowStateRefused, c, err)
		}
		names[i] = `"` + c + `"`
		args[i] = value
		if c != "id" {
			updates = append(updates, names[i]+" = excluded."+names[i])
		}
	}
	query := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s) ON CONFLICT(id) DO `, table,
		strings.Join(names, ", "), strings.TrimSuffix(strings.Repeat("?, ", len(names)), ", "))
	if len(updates) == 0 {
		query += `NOTHING`
	} else {
		query += `UPDATE SET ` + strings.Join(updates, ", ")
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		if strings.Contains(err.Error(), "constraint failed") {
			return fmt.Errorf("%w: %v", ErrRowStateRefused, err)
		}
		return fmt.Errorf("store %s row: %w", table, err)
	}
	return nil
}

// sendRowStates delivers states to node and returns the rows it refused.
func sendRowStates(ctx context.Context, client *ProxyClient, node *Node, localID string, states []*RowState) ([]string, error) {
	body, err := json.Marshal(RowStateBatch{Rows: states})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), metadataOpTimeout)
	defer cancel()
	url := fmt.Sprintf("%s/api/internal/cluster/ha/row-states", node.Endpoint)
	req, err := client.CreateAuthenticatedRequest(ctx, http.MethodPost, url, bytes.NewReader(body), localID, node.NodeToken)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode < 300:
		var result RowStateResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("read the answer: %w", err)
		}
		return result.Refused, nil
	case resp.StatusCode < 500:
		return nil, fmt.Errorf("%w: status %d", errStateRefused, resp.StatusCode)
	default:
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
}

// cleanupRowVersions forgets row deletions older than maxAge, as the deletion
// log does, unless a member of the cluster has yet to be sent them, and the
// versions of rows this node dropped without recording a deletion, as an
// expired share.
func cleanupRowVersions(ctx context.Context, db *sql.DB, maxAge time.Duration) (int64, error) {
	result, err := db.ExecContext(ctx, `DELETE FROM ha_row_versions WHERE deleted = 1 AND version < ?`,
		forgetBefore(ctx, db, maxAge))
	if err != nil {
		return 0, err
	}
	count, _ := result.RowsAffected()
	for _, t := range replicatedTables {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, t.name).Scan(&n); err != nil {
			return count, err
		}
		if n == 0 {
			continue
		}
		result, err := db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM ha_row_versions
			WHERE tbl = ? AND deleted = 0 AND NOT EXISTS (SELECT 1 FROM %s WHERE %s.id = ha_row_versions.id)`, t.name, t.name), t.name)
		if err != nil {
			return count, err
		}
		n64, _ := result.RowsAffected()
		count += n64
	}
	return count, nil
}
