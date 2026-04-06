package cluster

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

const (
	SyncJobRunning = "running"
	SyncJobDone    = "done"
	SyncJobFailed  = "failed"

	syncPageSize      = 500
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
// replication factor increases or a new node joins the cluster.
// One goroutine per target node runs concurrently; progress is checkpointed in
// the cluster SQLite DB so that a crash resumes from the last saved position.
type HASyncWorker struct {
	objMgr    object.Manager
	bucketMgr bucket.Manager
	mgr       *Manager

	mu      sync.Mutex
	running map[string]context.CancelFunc // nodeID → cancel func
}

// NewHASyncWorker creates a worker.  Call Start once at server startup, then
// call Trigger whenever the replication factor changes.
func NewHASyncWorker(objMgr object.Manager, bucketMgr bucket.Manager, mgr *Manager) *HASyncWorker {
	return &HASyncWorker{
		objMgr:    objMgr,
		bucketMgr: bucketMgr,
		mgr:       mgr,
		running:   make(map[string]context.CancelFunc),
	}
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

// Trigger inspects the current replication factor and starts a sync job for every
// healthy non-local node that does not yet have a completed sync.
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

	replicas := 0
	for _, n := range healthy {
		if n.ID == localID {
			continue
		}
		if replicas >= factor-1 {
			break
		}
		replicas++

		w.mu.Lock()
		_, alreadyRunning := w.running[n.ID]
		w.mu.Unlock()
		if alreadyRunning {
			continue
		}

		// Skip if this node already has a completed sync.
		var existingStatus string
		scanErr := w.mgr.db.QueryRowContext(ctx,
			`SELECT status FROM ha_sync_jobs WHERE target_node_id = ? ORDER BY id DESC LIMIT 1`, n.ID,
		).Scan(&existingStatus)
		if scanErr == nil && existingStatus == SyncJobDone {
			continue
		}

		result, insertErr := w.mgr.db.ExecContext(ctx,
			`INSERT INTO ha_sync_jobs
			 (target_node_id, status, objects_synced, last_checkpoint_bucket, last_checkpoint_key, started_at)
			 VALUES (?, ?, 0, '', '', ?)`,
			n.ID, SyncJobRunning, time.Now())
		if insertErr != nil {
			logrus.WithError(insertErr).WithField("node_id", n.ID).Warn("HASyncWorker: failed to create job")
			continue
		}
		jobID, _ := result.LastInsertId()

		node := n // capture for goroutine
		logrus.WithFields(logrus.Fields{
			"job_id": jobID, "node_id": n.ID,
		}).Info("HASyncWorker: starting initial sync")
		w.startJob(ctx, jobID, node, "", "")
	}
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

func (w *HASyncWorker) startJob(ctx context.Context, jobID int64, node *Node, startBucket, startKey string) {
	jobCtx, cancel := context.WithCancel(ctx)

	w.mu.Lock()
	w.running[node.ID] = cancel
	w.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			w.mu.Lock()
			delete(w.running, node.ID)
			w.mu.Unlock()
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
	}()
}

func (w *HASyncWorker) runSync(ctx context.Context, jobID int64, node *Node, startBucket, startKey string) error {
	localID, err := w.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return fmt.Errorf("get local node ID: %w", err)
	}
	client := NewProxyClient(w.mgr.GetTLSConfig())

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

	var synced int64
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
			result, listErr := w.objMgr.ListObjects(ctx, bp, "", "", marker, syncPageSize)
			if listErr != nil {
				logrus.WithError(listErr).WithField("bucket", bp).
					Warn("HASyncWorker: list objects error, skipping bucket")
				break
			}

			for _, obj := range result.Objects {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if putErr := w.syncObject(ctx, client, node, localID, bp, obj.Key); putErr != nil {
					logrus.WithError(putErr).WithFields(logrus.Fields{
						"bucket": bp, "key": obj.Key, "node_id": node.ID,
					}).Warn("HASyncWorker: object sync failed, skipping")
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

			if !result.IsTruncated {
				break
			}
			marker = result.NextMarker
		}
	}

	// Write final progress (checkpoint cleared — sync is done).
	w.mgr.db.ExecContext(context.Background(), //nolint:errcheck
		`UPDATE ha_sync_jobs
		 SET objects_synced=?, last_checkpoint_bucket='', last_checkpoint_key=''
		 WHERE id=?`,
		synced, jobID)

	return nil
}

func (w *HASyncWorker) syncObject(
	ctx context.Context,
	client *ProxyClient,
	node *Node,
	localID, bucketPath, key string,
) error {
	obj, reader, err := w.objMgr.GetObject(ctx, bucketPath, key)
	if err != nil {
		return err
	}
	defer reader.Close()

	url := fmt.Sprintf("%s/api/internal/ha/objects/%s", node.Endpoint, key)
	req, err := client.CreateAuthenticatedRequest(ctx, "PUT", url, reader, localID, node.NodeToken)
	if err != nil {
		return err
	}

	req.Header.Set("X-MaxIOFS-HA-Replica", "true")
	req.Header.Set(HABucketHeader, bucketPath)
	req.Header.Set("Content-Type", obj.ContentType)
	if obj.ContentDisposition != "" {
		req.Header.Set("Content-Disposition", obj.ContentDisposition)
	}
	if obj.ContentEncoding != "" {
		req.Header.Set("Content-Encoding", obj.ContentEncoding)
	}
	if obj.CacheControl != "" {
		req.Header.Set("Cache-Control", obj.CacheControl)
	}
	if obj.ContentLanguage != "" {
		req.Header.Set("Content-Language", obj.ContentLanguage)
	}
	if obj.StorageClass != "" {
		req.Header.Set("x-amz-storage-class", obj.StorageClass)
	}
	for k, v := range obj.Metadata {
		req.Header.Set("x-amz-meta-"+k, v)
	}
	req.ContentLength = obj.Size

	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("replica returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// bucketPath returns the canonical bucket path used by object.Manager:
// "tenantID/bucketName" when a tenant exists, or just "bucketName".
func bucketPath(b bucket.Bucket) string {
	if b.TenantID != "" {
		return b.TenantID + "/" + b.Name
	}
	return b.Name
}

// httpHeadersFromObject builds the HTTP headers that represent the full
// metadata of obj.  Shared by the sync worker and the fanout helpers so the
// replica always receives a complete, consistent set of headers.
func httpHeadersFromObject(obj *object.Object) http.Header {
	h := http.Header{}
	h.Set("Content-Type", obj.ContentType)
	if obj.ContentDisposition != "" {
		h.Set("Content-Disposition", obj.ContentDisposition)
	}
	if obj.ContentEncoding != "" {
		h.Set("Content-Encoding", obj.ContentEncoding)
	}
	if obj.CacheControl != "" {
		h.Set("Cache-Control", obj.CacheControl)
	}
	if obj.ContentLanguage != "" {
		h.Set("Content-Language", obj.ContentLanguage)
	}
	if obj.StorageClass != "" {
		h.Set("x-amz-storage-class", obj.StorageClass)
	}
	for k, v := range obj.Metadata {
		h.Set("x-amz-meta-"+k, v)
	}
	return h
}
