package cluster

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// With a replication factor above 1 every node holds every bucket, with the
// same configuration and ACL. A change is sent to every other node before the
// request returns; a node that misses it is sent the state of every bucket when
// it is caught up, and so is a new replica and every peer at the start of an
// anti-entropy cycle. Two versions of a bucket are ordered by the time of the
// change (UpdatedAt); a deletion by the time it was recorded.

var (
	// ErrBucketNameTaken is a node's answer to a bucket whose name one of its
	// own buckets, in another tenant, already has.
	ErrBucketNameTaken = errors.New("another tenant holds a bucket of that name")
	// ErrInvalidBucketState is an answer to a message that names no bucket.
	ErrInvalidBucketState = errors.New("the message names no bucket")

	errStateRefused = errors.New("the node refused the change")
)

// BucketState is what a node tells another about a bucket: its configuration
// and ACL, or that it was deleted and when.
type BucketState struct {
	Bucket *metadata.BucketMetadata `json:"bucket,omitempty"`
	ACL    *acl.ACL                 `json:"acl,omitempty"`

	TenantID  string `json:"tenant_id,omitempty"`
	Name      string `json:"name,omitempty"`
	DeletedAt int64  `json:"deleted_at,omitempty"` // unix nanoseconds
}

func tenantBucketPath(tenantID, name string) string {
	if tenantID == "" {
		return name
	}
	return tenantID + "/" + name
}

// recordBucketTombstone records that the bucket at path was deleted at the
// given time, keeping the later of two deletions.
func recordBucketTombstone(ctx context.Context, db *sql.DB, path string, deletedAt int64) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO ha_bucket_tombstones (path, deleted_at) VALUES (?, ?)
		ON CONFLICT(path) DO UPDATE SET deleted_at = MAX(deleted_at, excluded.deleted_at)`,
		path, deletedAt)
	return err
}

// bucketTombstone returns when the bucket at path was deleted, or 0.
func bucketTombstone(ctx context.Context, db *sql.DB, path string) (int64, error) {
	var deletedAt int64
	err := db.QueryRowContext(ctx, `SELECT deleted_at FROM ha_bucket_tombstones WHERE path = ?`, path).Scan(&deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return deletedAt, err
}

// cleanupBucketTombstones forgets deletions older than maxAge, as the deletion
// log does.
func cleanupBucketTombstones(ctx context.Context, db *sql.DB, maxAge time.Duration) (int64, error) {
	result, err := db.ExecContext(ctx, `DELETE FROM ha_bucket_tombstones WHERE deleted_at < ?`,
		time.Now().Add(-maxAge).UnixNano())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type bucketTombstoneRow struct {
	path      string
	deletedAt int64
}

func listBucketTombstones(ctx context.Context, db *sql.DB) ([]bucketTombstoneRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT path, deleted_at FROM ha_bucket_tombstones ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []bucketTombstoneRow
	for rows.Next() {
		var t bucketTombstoneRow
		if err := rows.Scan(&t.path, &t.deletedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// splitBucketPath is the reverse of tenantBucketPath. Bucket names hold no
// slash.
func splitBucketPath(path string) (tenantID, name string) {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i], path[i+1:]
		}
	}
	return "", path
}

// bucketStateStore is what BucketStates reads a bucket from.
type bucketStateStore interface {
	GetBucket(ctx context.Context, tenantID, name string) (*metadata.BucketMetadata, error)
	ListBuckets(ctx context.Context, tenantID string) ([]*metadata.BucketMetadata, error)
	UpdateBucket(ctx context.Context, bucket *metadata.BucketMetadata) error
}

// BucketStates sends this node's buckets to the other nodes.
type BucketStates struct {
	mgr   *Manager
	store bucketStateStore
	acl   acl.Manager
	rows  *RowStates
}

// NewBucketStates wires the sender. aclMgr may be nil: the buckets are then
// sent without their ACL.
func NewBucketStates(mgr *Manager, store bucketStateStore, aclMgr acl.Manager) *BucketStates {
	return &BucketStates{mgr: mgr, store: store, acl: aclMgr}
}

// SetRowStates makes a synchronization of the buckets send the rows the
// buckets keep in the node's database after them.
func (b *BucketStates) SetRowStates(rows *RowStates) {
	b.rows = rows
}

// replicatesBuckets reports whether every node holds every bucket: in a
// cluster whose replication factor is above 1.
func replicatesBuckets(ctx context.Context, mgr *Manager) bool {
	if !mgr.IsClusterEnabled() {
		return false
	}
	factor, err := mgr.GetReplicationFactor(ctx)
	return err == nil && factor > 1
}

func (b *BucketStates) active(ctx context.Context) bool {
	return b != nil && replicatesBuckets(ctx, b.mgr)
}

// state reads a bucket as it is sent: nil for a bucket this node does not hold
// or holds only as a hidden copy.
func (b *BucketStates) state(ctx context.Context, meta *metadata.BucketMetadata) (*BucketState, error) {
	if meta.Moving() {
		return nil, nil
	}
	st := &BucketState{Bucket: meta}
	if b.acl != nil {
		a, err := b.acl.GetBucketACL(ctx, meta.TenantID, meta.Name)
		if err != nil {
			return nil, fmt.Errorf("read the ACL of bucket %s: %w", meta.Name, err)
		}
		st.ACL = a
	}
	return st, nil
}

// touch dates a change the bucket's configuration does not hold, as its ACL.
func (b *BucketStates) touch(ctx context.Context, tenantID, name string) error {
	meta, err := b.store.GetBucket(ctx, tenantID, name)
	if err != nil {
		return err
	}
	return b.store.UpdateBucket(ctx, meta)
}

// Publish sends the bucket's current state to every other node before
// returning. A node that is not healthy, or does not take it, is recorded as
// having missed a write and is sent every bucket when it is caught up.
func (b *BucketStates) Publish(ctx context.Context, tenantID, name string) {
	if !b.active(ctx) {
		return
	}
	meta, err := b.store.GetBucket(ctx, tenantID, name)
	if err != nil {
		logrus.WithError(err).WithField("bucket", name).Warn("HA: a changed bucket could not be read; the other nodes get it when they are next synchronized")
		missedByAll(ctx, b.mgr)
		return
	}
	st, err := b.state(ctx, meta)
	if err != nil || st == nil {
		if err != nil {
			logrus.WithError(err).WithField("bucket", name).Warn("HA: a changed bucket could not be read; the other nodes get it when they are next synchronized")
			missedByAll(ctx, b.mgr)
		}
		return
	}
	b.fanout(ctx, st)
}

// PublishDeletion records that the bucket was deleted now and tells every
// other node, as Publish does.
func (b *BucketStates) PublishDeletion(ctx context.Context, tenantID, name string) {
	if !b.active(ctx) {
		return
	}
	now := time.Now().UnixNano()
	if err := recordBucketTombstone(ctx, b.mgr.db, tenantBucketPath(tenantID, name), now); err != nil {
		logrus.WithError(err).WithField("bucket", name).Error("HA: failed to record a bucket deletion; the other nodes may keep the bucket")
	}
	b.fanout(ctx, &BucketState{TenantID: tenantID, Name: name, DeletedAt: now})
}

// missedByAll records that every other node that is not dead missed a write.
func missedByAll(ctx context.Context, mgr *Manager) {
	if localID, err := mgr.GetLocalNodeID(ctx); err == nil {
		nodes, err := mgr.ListNodes(ctx)
		if err != nil {
			return
		}
		var ids []string
		for _, n := range nodes {
			if n.ID != localID && n.HealthStatus != HealthStatusDead {
				ids = append(ids, n.ID)
			}
		}
		mgr.noteMissedWrites(ctx, localID, time.Now(), ids...)
	}
}

// fanout calls send for every other node that is not dead, at once, and
// returns when all have answered. A node that is not healthy, or for which
// send fails, is recorded as having missed a write.
func fanout(ctx context.Context, mgr *Manager, change string,
	send func(ctx context.Context, client *ProxyClient, n *Node, localID string) error) {
	localID, err := mgr.GetLocalNodeID(ctx)
	if err != nil {
		return
	}
	nodes, err := mgr.ListNodes(ctx)
	if err != nil {
		logrus.WithError(err).WithField("change", change).Error("HA: cannot list nodes; a change reaches them at the next synchronization")
		return
	}
	now := time.Now()
	client := NewProxyClient(mgr.GetTLSConfig())
	var wg sync.WaitGroup
	for _, n := range nodes {
		if n.ID == localID || n.HealthStatus == HealthStatusDead {
			continue
		}
		if n.HealthStatus != HealthStatusHealthy {
			mgr.noteMissedWrites(ctx, localID, now, n.ID)
			continue
		}
		wg.Add(1)
		go func(n *Node) {
			defer wg.Done()
			if err := send(ctx, client, n, localID); err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{"node_id": n.ID, "change": change}).
					Warn("HA: a node did not take a change; it gets it when it is caught up")
				mgr.noteMissedWrites(ctx, localID, now, n.ID)
			}
		}(n)
	}
	wg.Wait()
}

func (b *BucketStates) fanout(ctx context.Context, st *BucketState) {
	fanout(ctx, b.mgr, bucketStateName(st), func(ctx context.Context, client *ProxyClient, n *Node, localID string) error {
		return b.send(ctx, client, n, localID, st)
	})
}

// SyncNode sends node every bucket this node holds and every deletion it
// recorded for a bucket it no longer holds, then the rows the buckets keep in
// the node's database. A bucket or row the node refuses is logged; any other
// failure is returned.
func (b *BucketStates) SyncNode(ctx context.Context, client *ProxyClient, node *Node, localID string) error {
	if !b.active(ctx) {
		return nil
	}
	buckets, err := b.store.ListBuckets(ctx, "")
	if err != nil {
		return fmt.Errorf("list buckets: %w", err)
	}
	live := make(map[string]bool, len(buckets))
	for _, meta := range buckets {
		live[tenantBucketPath(meta.TenantID, meta.Name)] = true
		st, err := b.state(ctx, meta)
		if err != nil {
			return err
		}
		if st == nil {
			continue
		}
		if err := b.send(ctx, client, node, localID, st); err != nil {
			return err
		}
	}
	tombstones, err := listBucketTombstones(ctx, b.mgr.db)
	if err != nil {
		return fmt.Errorf("list bucket deletions: %w", err)
	}
	for _, t := range tombstones {
		// A deletion of a path that holds a bucket again is superseded.
		if live[t.path] {
			continue
		}
		tenantID, name := splitBucketPath(t.path)
		if err := b.send(ctx, client, node, localID, &BucketState{TenantID: tenantID, Name: name, DeletedAt: t.deletedAt}); err != nil {
			return err
		}
	}
	if b.rows != nil {
		return b.rows.syncNode(ctx, client, node, localID)
	}
	return nil
}

// SyncPeers sends every other node every bucket, as SyncNode does. A node that
// is not healthy or does not take them is recorded as having missed a write.
func (b *BucketStates) SyncPeers(ctx context.Context) {
	if !b.active(ctx) {
		return
	}
	localID, err := b.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return
	}
	nodes, err := b.mgr.ListNodes(ctx)
	if err != nil {
		logrus.WithError(err).Warn("HA: cannot list nodes to synchronize buckets")
		return
	}
	client := NewProxyClient(b.mgr.GetTLSConfig())
	for _, n := range nodes {
		if n.ID == localID || n.HealthStatus == HealthStatusDead {
			continue
		}
		if n.HealthStatus != HealthStatusHealthy {
			b.mgr.noteMissedWrites(ctx, localID, time.Now(), n.ID)
			continue
		}
		if err := b.SyncNode(ctx, client, n, localID); err != nil {
			logrus.WithError(err).WithField("node_id", n.ID).Warn("HA: buckets not synchronized with a node; retried when it is caught up")
			b.mgr.noteMissedWrites(ctx, localID, time.Now(), n.ID)
		}
	}
}

func (b *BucketStates) send(ctx context.Context, client *ProxyClient, node *Node, localID string, st *BucketState) error {
	err := sendBucketState(ctx, client, node, localID, st)
	if errors.Is(err, errStateRefused) {
		logrus.WithError(err).WithFields(logrus.Fields{"node_id": node.ID, "bucket": bucketStateName(st)}).
			Error("HA: a node refused a bucket")
		return nil
	}
	return err
}

func bucketStateName(st *BucketState) string {
	if st.Bucket != nil {
		return tenantBucketPath(st.Bucket.TenantID, st.Bucket.Name)
	}
	return tenantBucketPath(st.TenantID, st.Name)
}

// sendBucketState delivers one bucket to node.
func sendBucketState(ctx context.Context, client *ProxyClient, node *Node, localID string, st *BucketState) error {
	body, err := json.Marshal(st)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), metadataOpTimeout)
	defer cancel()
	url := fmt.Sprintf("%s/api/internal/cluster/ha/bucket-state", node.Endpoint)
	req, err := client.CreateAuthenticatedRequest(ctx, http.MethodPost, url, bytes.NewReader(body), localID, node.NodeToken)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode < 300:
		return nil
	case resp.StatusCode < 500:
		return fmt.Errorf("%w: status %d", errStateRefused, resp.StatusCode)
	default:
		return fmt.Errorf("status %d", resp.StatusCode)
	}
}

// retentionChecker reports whether a bucket holds data a retention or a legal
// hold protects.
type retentionChecker interface {
	HasActiveComplianceRetention(ctx context.Context, bucket string) (bool, error)
}

// BucketStateReceiver applies what another node reports about a bucket.
type BucketStateReceiver struct {
	store     metadata.Store
	backend   storage.Backend
	acl       acl.Manager
	remover   bucketRemover
	retention retentionChecker
	keys      keyLister
	db        *sql.DB
}

// NewBucketStateReceiver wires the receiving side. remover is this node's own
// bucket manager, which does not tell the other nodes.
func NewBucketStateReceiver(store metadata.Store, backend storage.Backend, aclMgr acl.Manager, remover bucketRemover,
	retention retentionChecker, keys keyLister, db *sql.DB) *BucketStateReceiver {
	return &BucketStateReceiver{store: store, backend: backend, acl: aclMgr, remover: remover,
		retention: retention, keys: keys, db: db}
}

// Apply stores what st reports when it is newer than what this node holds.
func (r *BucketStateReceiver) Apply(ctx context.Context, st *BucketState) error {
	switch {
	case st.Bucket != nil && st.Bucket.Name != "":
		return r.applyBucket(ctx, st)
	case st.Name != "" && st.DeletedAt > 0:
		return r.applyDeletion(ctx, st)
	}
	return ErrInvalidBucketState
}

func (r *BucketStateReceiver) applyBucket(ctx context.Context, st *BucketState) error {
	meta := st.Bucket
	path := tenantBucketPath(meta.TenantID, meta.Name)
	deletedAt, err := bucketTombstone(ctx, r.db, path)
	if err != nil {
		return err
	}
	if deletedAt >= meta.UpdatedAt.UnixNano() {
		return nil
	}
	_, _, err = bucket.ApplyReplicaEntry(ctx, r.store, r.backend, meta)
	if errors.Is(err, metadata.ErrBucketAlreadyExists) {
		return ErrBucketNameTaken
	}
	if err != nil {
		return err
	}
	// The ACL is written separately from the configuration: it is brought to
	// the version the node holds, including one stored before a failed write.
	if st.ACL == nil || r.acl == nil {
		return nil
	}
	local, err := r.store.GetBucket(ctx, meta.TenantID, meta.Name)
	if err != nil {
		return err
	}
	if !local.UpdatedAt.Equal(meta.UpdatedAt) {
		return nil
	}
	current, err := r.acl.GetBucketACL(ctx, meta.TenantID, meta.Name)
	if err == nil && reflect.DeepEqual(current, st.ACL) {
		return nil
	}
	return r.acl.SetBucketACL(ctx, meta.TenantID, meta.Name, st.ACL)
}

// applyDeletion removes this node's copy of a bucket deleted elsewhere, unless
// the copy changed after the deletion, holds writes made after it, or holds
// data under retention or a legal hold: such a copy is kept and logged.
func (r *BucketStateReceiver) applyDeletion(ctx context.Context, st *BucketState) error {
	path := tenantBucketPath(st.TenantID, st.Name)
	if err := recordBucketTombstone(ctx, r.db, path, st.DeletedAt); err != nil {
		return err
	}
	local, err := r.store.GetBucket(ctx, st.TenantID, st.Name)
	if errors.Is(err, metadata.ErrBucketNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	log := logrus.WithFields(logrus.Fields{"bucket": path, "deleted_at": time.Unix(0, st.DeletedAt)})
	if local.UpdatedAt.UnixNano() > st.DeletedAt {
		log.Warn("HA: a bucket deleted on another node changed here after the deletion; kept")
		return nil
	}
	if newer, err := r.writtenAfter(ctx, path, st.DeletedAt); err != nil {
		return err
	} else if newer {
		log.Warn("HA: a bucket deleted on another node holds objects written here after the deletion; kept")
		return nil
	}
	if locked, err := r.retention.HasActiveComplianceRetention(ctx, path); err != nil {
		return err
	} else if locked {
		log.Warn("HA: a bucket deleted on another node holds objects under retention or a legal hold here; kept")
		return nil
	}
	if err := r.remover.ForceDeleteBucket(ctx, st.TenantID, st.Name); err != nil && !errors.Is(err, bucket.ErrBucketNotFound) {
		return err
	}
	return nil
}

// writtenAfter reports whether a key of the bucket was last written after the
// given time. Object times are whole seconds: a write in the second of the
// deletion counts as made after it.
func (r *BucketStateReceiver) writtenAfter(ctx context.Context, path string, at int64) (bool, error) {
	deletedIn := time.Unix(0, at).Unix()
	marker := ""
	for {
		entries, next, err := r.keys.ListObjects(ctx, path, "", marker, syncPageSize)
		if err != nil {
			return false, err
		}
		for _, e := range entries {
			if e.LastModified.Unix() >= deletedIn {
				return true, nil
			}
		}
		if next == "" || len(entries) == 0 {
			return false, nil
		}
		marker = next
	}
}
