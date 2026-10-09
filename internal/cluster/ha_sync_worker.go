package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/maxiofs/maxiofs/internal/bgwork"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

const (
	SyncJobRunning = "running"
	SyncJobDone    = "done"
	SyncJobFailed  = "failed"

	syncPageSize        = 500
	syncCheckpointEvery = 100
)

// SyncJobStatus represents the persisted state of one initial sync job.
type SyncJobStatus struct {
	ID                   int64      `json:"id"`
	TargetNodeID         string     `json:"target_node_id"`
	Status               string     `json:"status"`
	ObjectsSynced        int64      `json:"objects_synced"`
	LastCheckpointBucket string     `json:"last_checkpoint_bucket"`
	LastCheckpointKey    string     `json:"last_checkpoint_key"`
	StartedAt            time.Time  `json:"started_at"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	ErrorMessage         string     `json:"error_message,omitempty"`
}

// HASyncWorker copies all existing objects to new replica nodes whenever the
type HASyncWorker struct {
	objMgr    object.Manager
	bucketMgr bucket.Manager
	mgr       *Manager
	keys      keyLister
	buckets   *BucketStates

	bgwork.Worker
	mu      sync.Mutex
	running map[string]context.CancelFunc // nodeID → cancel func
	// repairing is set while a repair of the copies runs.
	repairing atomic.Bool
}

// Stop cancels the in-flight syncs and waits for them. Their final status write
// goes to the same database the shutdown is about to close, so returning before
// they finish means writing into a closed handle.
func (w *HASyncWorker) Stop() {
	w.mu.Lock()
	for _, cancel := range w.running {
		cancel()
	}
	w.mu.Unlock()
	w.Worker.Stop()
}

// keyLister lists a bucket's keys from the index, including keys whose latest
// version is a delete marker.
type keyLister interface {
	ListObjects(ctx context.Context, bucket, prefix, marker string, maxKeys int) ([]*metadata.ObjectMetadata, string, error)
}

// NewHASyncWorker creates a worker.  Call Start once at server startup, then
// call Trigger whenever the replication factor changes. keys is the metadata
// index the sync walks.
func NewHASyncWorker(objMgr object.Manager, bucketMgr bucket.Manager, mgr *Manager, keys keyLister) *HASyncWorker {
	return &HASyncWorker{
		objMgr:    objMgr,
		bucketMgr: bucketMgr,
		mgr:       mgr,
		keys:      keys,
		running:   make(map[string]context.CancelFunc),
	}
}

// SetBucketStates makes a sync send the new replica every bucket before the
// objects that go into them.
func (w *HASyncWorker) SetBucketStates(b *BucketStates) {
	w.buckets = b
}

// Start resumes any sync jobs that were still running when the server last stopped.
// Must be called once at startup, before serving requests.
func (w *HASyncWorker) Start(ctx context.Context) {
	rows, err := w.mgr.db.QueryContext(ctx,
		`SELECT id, target_node_id, last_checkpoint_bucket, last_checkpoint_key
		 FROM ha_sync_jobs WHERE status = ?`, SyncJobRunning)
	if err != nil {
		logrus.WithError(err).Warn("HASyncWorker: failed to query in-progress jobs")
		return
	}
	defer rows.Close()

	type pending struct {
		id     int64
		nodeID string
		bucket string
		key    string
	}
	var jobs []pending
	for rows.Next() {
		var j pending
		if err := rows.Scan(&j.id, &j.nodeID, &j.bucket, &j.key); err == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()

	for _, j := range jobs {
		node, err := w.mgr.GetNode(ctx, j.nodeID)
		if err != nil {
			logrus.WithField("node_id", j.nodeID).Warn("HASyncWorker: node not found on resume, skipping")
			continue
		}
		logrus.WithFields(logrus.Fields{
			"job_id": j.id, "node_id": j.nodeID,
		}).Info("HASyncWorker: resuming sync job")
		w.startJob(ctx, j.id, node, j.bucket, j.key)
	}
}

// Trigger starts, when the replication factor is above 1, a sync job for every
// healthy node that has not had one completed: it is sent every entry, and the
// data it is to hold. It repairs the copies once those jobs end, so that every
// node holds the entries whose copies change (see RepairPlacement).
// Safe to call multiple times; already-running jobs are skipped.
func (w *HASyncWorker) Trigger(ctx context.Context) {
	if !w.mgr.IsClusterEnabled() {
		return
	}
	factor, err := w.mgr.GetReplicationFactor(ctx)
	if err != nil || factor <= 1 {
		return
	}
	localID, err := w.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return
	}
	healthy, err := w.mgr.GetHealthyNodes(ctx)
	if err != nil {
		return
	}

	var jobs []<-chan struct{}
	for _, n := range healthy {
		if n.ID == localID {
			continue
		}

		w.mu.Lock()
		_, alreadyRunning := w.running[n.ID]
		skipBecauseDone := false
		if !alreadyRunning {
			var existingStatus string
			if scanErr := w.mgr.db.QueryRowContext(ctx,
				`SELECT status FROM ha_sync_jobs WHERE target_node_id = ? ORDER BY id DESC LIMIT 1`, n.ID,
			).Scan(&existingStatus); scanErr == nil && existingStatus == SyncJobDone {
				skipBecauseDone = true
			} else {
				// Reserve the slot; startJob will overwrite with the real cancel.
				w.running[n.ID] = func() {}
			}
		}
		w.mu.Unlock()
		if alreadyRunning || skipBecauseDone {
			continue
		}

		result, insertErr := w.mgr.db.ExecContext(ctx,
			`INSERT INTO ha_sync_jobs
			 (target_node_id, status, objects_synced, last_checkpoint_bucket, last_checkpoint_key, started_at)
			 VALUES (?, ?, 0, '', '', ?)`,
			n.ID, SyncJobRunning, time.Now())
		if insertErr != nil {
			logrus.WithError(insertErr).WithField("node_id", n.ID).Warn("HASyncWorker: failed to create job")
			w.mu.Lock()
			delete(w.running, n.ID)
			w.mu.Unlock()
			continue
		}
		jobID, _ := result.LastInsertId()

		node := n // capture for goroutine
		logrus.WithFields(logrus.Fields{
			"job_id": jobID, "node_id": n.ID,
		}).Info("HASyncWorker: starting initial sync")
		jobs = append(jobs, w.startJob(ctx, jobID, node, "", ""))
	}
	if len(jobs) == 0 {
		w.StartRepair(ctx)
		return
	}
	w.Spawn(func() {
		for _, done := range jobs {
			<-done
		}
		w.StartRepair(ctx)
	})
}

// FactorRaised runs on every node when the replication factor rises from 1.
// Each bucket was on one node, which alone holds the data of its objects:
// they are given this node as their location before its buckets and entries
// are sent to the other nodes; the copies are made after (see Trigger).
func (w *HASyncWorker) FactorRaised(ctx context.Context) {
	if err := w.claimWrites(ctx); err != nil {
		logrus.WithError(err).Warn("HASyncWorker: some writes held here name no node; the repair finds their holders")
	}
	w.Trigger(ctx)
}

// DropEntriesHeldElsewhere removes, once this node has left the cluster, its
// entries of the writes whose data the nodes of the cluster hold, which it no
// longer reaches; the writes it holds stay. localID is the ID it had in the
// cluster.
func (w *HASyncWorker) DropEntriesHeldElsewhere(ctx context.Context, localID string) error {
	writer, ok := ReplicaWriter(w.objMgr)
	if !ok {
		return nil
	}
	return w.eachPage(ctx, func(ctx context.Context, bucket string, keys []string) error {
		var errs error
		for _, key := range keys {
			writes, err := keyWrites(ctx, w.objMgr, bucket, key)
			if err != nil {
				errs = errors.Join(errs, err)
				continue
			}
			for _, v := range writes {
				if v.IsDeleteMarker || len(v.Locations) == 0 || slices.Contains(v.Locations, localID) {
					continue
				}
				errs = errors.Join(errs, writer.DropEntry(ctx, bucket, key, v.VersionID))
			}
		}
		return errs
	})
}

// claimWrites gives this node as the location of every write whose data it
// holds and that names no node.
func (w *HASyncWorker) claimWrites(ctx context.Context) error {
	writer, ok := ReplicaWriter(w.objMgr)
	if !ok {
		return nil
	}
	localID, err := w.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return err
	}
	return w.eachPage(ctx, func(ctx context.Context, bucket string, keys []string) error {
		var errs error
		for _, key := range keys {
			writes, err := keyWrites(ctx, w.objMgr, bucket, key)
			if err != nil {
				errs = errors.Join(errs, err)
				continue
			}
			for _, v := range writes {
				if !unplaced(v) {
					continue
				}
				held, err := writer.HoldsData(ctx, bucket, key, v.VersionID)
				if err != nil || !held {
					errs = errors.Join(errs, err)
					continue
				}
				errs = errors.Join(errs, writer.SetLocations(ctx, bucket, key, locationsChange(v, []string{localID}, 1)))
			}
		}
		return errs
	})
}

// GetSyncJobs returns all sync jobs ordered newest first (for status display).
func (w *HASyncWorker) GetSyncJobs(ctx context.Context) ([]SyncJobStatus, error) {
	rows, err := w.mgr.db.QueryContext(ctx,
		`SELECT id, target_node_id, status, objects_synced,
		        last_checkpoint_bucket, last_checkpoint_key,
		        started_at, completed_at, error_message
		 FROM ha_sync_jobs ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []SyncJobStatus
	for rows.Next() {
		var j SyncJobStatus
		var completedAt sql.NullTime
		var errMsg sql.NullString
		if err := rows.Scan(
			&j.ID, &j.TargetNodeID, &j.Status, &j.ObjectsSynced,
			&j.LastCheckpointBucket, &j.LastCheckpointKey,
			&j.StartedAt, &completedAt, &errMsg,
		); err != nil {
			continue
		}
		if completedAt.Valid {
			j.CompletedAt = &completedAt.Time
		}
		if errMsg.Valid {
			j.ErrorMessage = errMsg.String
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// startJob runs a sync job; the channel it returns is closed when the job
// ends.
func (w *HASyncWorker) startJob(ctx context.Context, jobID int64, node *Node, startBucket, startKey string) <-chan struct{} {
	jobCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	w.mu.Lock()
	w.running[node.ID] = cancel
	w.mu.Unlock()

	started := w.Spawn(func() {
		defer func() {
			cancel()
			w.mu.Lock()
			delete(w.running, node.ID)
			w.mu.Unlock()
			close(done)
		}()

		syncErr := w.runSync(jobCtx, jobID, node, startBucket, startKey)
		now := time.Now()

		if syncErr != nil && jobCtx.Err() == nil {
			// Real failure (not context cancellation).
			logrus.WithError(syncErr).WithField("node_id", node.ID).Error("HASyncWorker: sync failed")
			w.mgr.db.ExecContext(context.Background(), //nolint:errcheck
				`UPDATE ha_sync_jobs SET status=?, completed_at=?, error_message=? WHERE id=?`,
				SyncJobFailed, now, syncErr.Error(), jobID)
		} else if syncErr == nil {
			logrus.WithFields(logrus.Fields{
				"job_id": jobID, "node_id": node.ID,
			}).Info("HASyncWorker: initial sync completed successfully")
			w.mgr.db.ExecContext(context.Background(), //nolint:errcheck
				`UPDATE ha_sync_jobs SET status=?, completed_at=? WHERE id=?`,
				SyncJobDone, now, jobID)
		}
		// If jobCtx.Err() != nil the server is shutting down — leave status as
		// "running" so that Start() resumes from the last checkpoint on next boot.
	})

	if !started {
		// Shutting down: leave the job "running" so the next boot resumes it.
		cancel()
		w.mu.Lock()
		delete(w.running, node.ID)
		w.mu.Unlock()
		close(done)
	}
	return done
}

func (w *HASyncWorker) runSync(ctx context.Context, jobID int64, node *Node, startBucket, startKey string) error {
	localID, err := w.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return fmt.Errorf("get local node ID: %w", err)
	}
	client := NewProxyClient(w.mgr.GetTLSConfig())

	if err := w.buckets.SyncNode(ctx, client, node, localID); err != nil {
		return fmt.Errorf("send buckets: %w", err)
	}

	// List every bucket across all tenants.
	buckets, err := w.bucketMgr.ListBuckets(ctx, "")
	if err != nil {
		return fmt.Errorf("list buckets: %w", err)
	}

	// Find the bucket index to resume from.
	startIdx := 0
	if startBucket != "" {
		for i, b := range buckets {
			if bucketPath(b) == startBucket {
				startIdx = i
				break
			}
		}
	}

	// What could not be sent is left to the catch-up: the node is recorded as
	// having missed the writes since the oldest of it.
	var synced, skipped int64
	var oldestSkipped time.Time
	skip := func(modified time.Time) {
		skipped++
		if oldestSkipped.IsZero() || modified.Before(oldestSkipped) {
			oldestSkipped = modified
		}
	}
	for i := startIdx; i < len(buckets); i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		bp := bucketPath(buckets[i])

		marker := ""
		if i == startIdx && startKey != "" {
			marker = startKey
		}

		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			entries, nextMarker, listErr := w.keys.ListObjects(ctx, bp, "", marker, syncPageSize)
			if listErr != nil {
				logrus.WithError(listErr).WithField("bucket", bp).
					Warn("HASyncWorker: list objects error, skipping bucket")
				skip(time.Unix(1, 0))
				break
			}

			for _, obj := range entries {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if putErr := w.syncKey(ctx, client, node, localID, bp, obj.Key); putErr != nil {
					logrus.WithError(putErr).WithFields(logrus.Fields{
						"bucket": bp, "key": obj.Key, "node_id": node.ID,
					}).Warn("HASyncWorker: object sync failed, skipping")
					skip(obj.LastModified)
					continue
				}
				synced++
				if synced%syncCheckpointEvery == 0 {
					w.mgr.db.ExecContext(ctx, //nolint:errcheck
						`UPDATE ha_sync_jobs
						 SET objects_synced=?, last_checkpoint_bucket=?, last_checkpoint_key=?
						 WHERE id=?`,
						synced, bp, obj.Key, jobID)
				}
			}

			if nextMarker == "" {
				break
			}
			marker = nextMarker
		}
	}

	note := ""
	if skipped > 0 {
		w.mgr.noteMissedWrites(ctx, localID, oldestSkipped, node.ID)
		note = fmt.Sprintf("%d object(s) not sent; the catch-up sends them", skipped)
	}
	// Write final progress (checkpoint cleared — sync is done).
	w.mgr.db.ExecContext(context.Background(), //nolint:errcheck
		`UPDATE ha_sync_jobs
		 SET objects_synced=?, last_checkpoint_bucket='', last_checkpoint_key='', error_message=?
		 WHERE id=?`,
		synced, note, jobID)

	return nil
}

// syncKey copies every version of key to node.
func (w *HASyncWorker) syncKey(ctx context.Context, client *ProxyClient, node *Node, localID, bucket, key string) error {
	_, err := sendKeyVersions(ctx, client, w.objMgr, node, localID, bucket, key)
	return err
}

// sendKeyVersions copies every version of key to node, oldest first, so that
// the latest stays the latest there: stored versions with their IDs, lock
// state and attributes, delete markers as delete markers with their IDs and
// times, then the current object when it has no version ID — a bucket never
// versioned, or a write while versioning was suspended. It returns the bytes
// of data sent.
func sendKeyVersions(ctx context.Context, client *ProxyClient, objects object.Manager, node *Node, localID, bucket, key string) (int64, error) {
	versions, err := keyVersions(ctx, objects, bucket, key)
	if err != nil {
		return 0, err
	}
	sent, err := sendVersions(ctx, client, objects, node, localID, bucket, key, versions)
	if err != nil {
		return sent, err
	}
	current, err := objects.GetObjectMetadata(ctx, bucket, key)
	switch {
	case errors.Is(err, object.ErrObjectNotFound):
		return sent, nil
	case err != nil:
		return sent, err
	case current.VersionID == "":
		data, err := sendCopy(ctx, client, objects, node, localID, bucket, key, object.ObjectVersion{Object: *current})
		if err != nil {
			return sent, err
		}
		if data {
			sent += current.Size
		}
	}
	return sent, nil
}

// keyVersions lists the versions and delete markers of key, oldest first.
func keyVersions(ctx context.Context, objects object.Manager, bucket, key string) ([]object.ObjectVersion, error) {
	versions, err := objects.GetObjectVersions(ctx, bucket, key)
	if err != nil && !errors.Is(err, object.ErrObjectNotFound) {
		return nil, err
	}
	sort.SliceStable(versions, func(i, j int) bool {
		a, b := versions[i].LastModified.Unix(), versions[j].LastModified.Unix()
		if a != b {
			return a < b
		}
		return versions[i].VersionID < versions[j].VersionID
	})
	return versions, nil
}

// KeyVersionIDs lists the IDs of the versions and delete markers of key.
func KeyVersionIDs(ctx context.Context, objects object.Manager, bucket, key string) ([]string, error) {
	versions, err := keyVersions(ctx, objects, bucket, key)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(versions))
	for i, v := range versions {
		ids[i] = v.VersionID
	}
	return ids, nil
}

// sendVersions copies versions and delete markers of key to node, in order,
// and returns the bytes sent.
func sendVersions(ctx context.Context, client *ProxyClient, objects object.Manager, node *Node, localID, bucket, key string, versions []object.ObjectVersion) (int64, error) {
	var sent int64
	for _, v := range versions {
		data, err := sendCopy(ctx, client, objects, node, localID, bucket, key, v)
		if err != nil {
			return sent, fmt.Errorf("version %s: %w", v.VersionID, err)
		}
		if data {
			sent += v.Size
		}
	}
	return sent, nil
}

// sendCopy sends node its copy of one version of key: a delete marker as a
// delete; the data when this node and node both hold it, or when the version
// names no node; the entry otherwise, and a node that is to hold the data is
// sent it by one that holds it. It reports whether the data was sent.
func sendCopy(ctx context.Context, client *ProxyClient, objects object.Manager, node *Node, localID, bucket, key string, v object.ObjectVersion) (bool, error) {
	switch {
	case v.IsDeleteMarker:
		return false, sendHADelete(ctx, client, node, localID, bucket, key, "", v.VersionID, v.LastModified)
	case len(v.Locations) == 0 || (slices.Contains(v.Locations, node.ID) && slices.Contains(v.Locations, localID)):
		return true, sendObjectVersion(ctx, client, objects, node, localID, bucket, key, v.VersionID)
	}
	return false, sendEntryOf(ctx, client, objects, node, localID, bucket, key, v.VersionID)
}

// sendEntryOf sends node this node's entry of one version of key, or of its
// current object when versionID is empty.
func sendEntryOf(ctx context.Context, client *ProxyClient, objects object.Manager, node *Node, localID, bucket, key, versionID string) error {
	writer, ok := ReplicaWriter(objects)
	if !ok {
		return fmt.Errorf("this node keeps no entries to send")
	}
	entry, err := writer.ObjectEntry(ctx, bucket, key, versionID)
	if err != nil {
		return err
	}
	body, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return sendObjectEntry(ctx, client, node, localID, bucket, body)
}

// ReplicaWriter is the manager that reads and stores this node's entries,
// below the HA layer.
func ReplicaWriter(objects object.Manager) (object.ReplicaMetadataWriter, bool) {
	if ha, ok := objects.(*HAObjectManager); ok {
		objects = ha.Manager
	}
	writer, ok := objects.(object.ReplicaMetadataWriter)
	return writer, ok
}

// bucketPath returns the canonical bucket path used by object.Manager:
// "tenantID/bucketName" when a tenant exists, or just "bucketName".
func bucketPath(b bucket.Bucket) string {
	if b.TenantID != "" {
		return b.TenantID + "/" + b.Name
	}
	return b.Name
}
