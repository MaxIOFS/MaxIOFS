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
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// MigrationStatus represents the status of a bucket migration
type MigrationStatus string

const (
	MigrationStatusPending    MigrationStatus = "pending"
	MigrationStatusInProgress MigrationStatus = "in_progress"
	// MigrationStatusCommitting: the copy is complete and verified and the
	// bucket is being handed over. From here the move is finished, never
	// undone, also after a restart.
	MigrationStatusCommitting MigrationStatus = "committing"
	MigrationStatusCompleted  MigrationStatus = "completed"
	MigrationStatusFailed     MigrationStatus = "failed"
	MigrationStatusCancelled  MigrationStatus = "cancelled"
)

// MigrationJob represents a bucket migration job
type MigrationJob struct {
	ID              int64           `json:"id"`
	BucketName      string          `json:"bucket_name"`
	SourceNodeID    string          `json:"source_node_id"`
	TargetNodeID    string          `json:"target_node_id"`
	Status          MigrationStatus `json:"status"`
	ObjectsTotal    int64           `json:"objects_total"`
	ObjectsMigrated int64           `json:"objects_migrated"`
	BytesTotal      int64           `json:"bytes_total"`
	BytesMigrated   int64           `json:"bytes_migrated"`
	DeleteSource    bool            `json:"delete_source"`
	VerifyData      bool            `json:"verify_data"`
	StartedAt       *time.Time      `json:"started_at"`
	CompletedAt     *time.Time      `json:"completed_at"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	ErrorMessage    string          `json:"error_message,omitempty"`
}

// CreateMigrationJob creates a new migration job in the database
func (cm *Manager) CreateMigrationJob(ctx context.Context, job *MigrationJob) error {
	query := `
		INSERT INTO cluster_migrations (
			bucket_name, source_node_id, target_node_id, status,
			objects_total, bytes_total, delete_source, verify_data, started_at,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`

	deleteSource := 0
	if job.DeleteSource {
		deleteSource = 1
	}
	verifyData := 1
	if !job.VerifyData {
		verifyData = 0
	}

	result, err := cm.db.ExecContext(ctx, query,
		job.BucketName,
		job.SourceNodeID,
		job.TargetNodeID,
		job.Status,
		job.ObjectsTotal,
		job.BytesTotal,
		deleteSource,
		verifyData,
		job.StartedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create migration job: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get migration job ID: %w", err)
	}

	job.ID = id
	logrus.WithFields(logrus.Fields{
		"migration_id": id,
		"bucket":       job.BucketName,
		"source":       job.SourceNodeID,
		"target":       job.TargetNodeID,
	}).Info("Migration job created")

	return nil
}

// UpdateMigrationJob updates an existing migration job
func (cm *Manager) UpdateMigrationJob(ctx context.Context, job *MigrationJob) error {
	query := `
		UPDATE cluster_migrations
		SET status = ?,
		    objects_total = ?,
		    objects_migrated = ?,
		    bytes_total = ?,
		    bytes_migrated = ?,
		    started_at = ?,
		    completed_at = ?,
		    error_message = ?,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`

	_, err := cm.db.ExecContext(ctx, query,
		job.Status,
		job.ObjectsTotal,
		job.ObjectsMigrated,
		job.BytesTotal,
		job.BytesMigrated,
		job.StartedAt,
		job.CompletedAt,
		job.ErrorMessage,
		job.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update migration job: %w", err)
	}

	return nil
}

const migrationJobColumns = `id, bucket_name, source_node_id, target_node_id, status,
	objects_total, objects_migrated, bytes_total, bytes_migrated,
	delete_source, verify_data, started_at, completed_at,
	created_at, updated_at, error_message`

func scanMigrationJob(row interface{ Scan(...any) error }) (*MigrationJob, error) {
	job := &MigrationJob{}
	var deleteSource, verifyData int
	var startedAt, completedAt sql.NullTime
	if err := row.Scan(
		&job.ID,
		&job.BucketName,
		&job.SourceNodeID,
		&job.TargetNodeID,
		&job.Status,
		&job.ObjectsTotal,
		&job.ObjectsMigrated,
		&job.BytesTotal,
		&job.BytesMigrated,
		&deleteSource,
		&verifyData,
		&startedAt,
		&completedAt,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.ErrorMessage,
	); err != nil {
		return nil, err
	}
	job.DeleteSource = deleteSource == 1
	job.VerifyData = verifyData == 1
	if startedAt.Valid {
		job.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		job.CompletedAt = &completedAt.Time
	}
	return job, nil
}

func (cm *Manager) queryMigrationJobs(ctx context.Context, where string, args ...any) ([]*MigrationJob, error) {
	rows, err := cm.db.QueryContext(ctx, `SELECT `+migrationJobColumns+` FROM cluster_migrations `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list migration jobs: %w", err)
	}
	defer rows.Close()

	var jobs []*MigrationJob
	for rows.Next() {
		job, err := scanMigrationJob(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan migration job: %w", err)
		}
		jobs = append(jobs, job)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating migration jobs: %w", err)
	}
	return jobs, nil
}

// GetMigrationJob retrieves a migration job by ID
func (cm *Manager) GetMigrationJob(ctx context.Context, id int64) (*MigrationJob, error) {
	job, err := scanMigrationJob(cm.db.QueryRowContext(ctx,
		`SELECT `+migrationJobColumns+` FROM cluster_migrations WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("migration job not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get migration job: %w", err)
	}
	return job, nil
}

// ListMigrationJobs retrieves all migration jobs
func (cm *Manager) ListMigrationJobs(ctx context.Context) ([]*MigrationJob, error) {
	return cm.queryMigrationJobs(ctx, `ORDER BY created_at DESC LIMIT 100`)
}

// GetMigrationJobsByBucket retrieves migration jobs for a specific bucket
func (cm *Manager) GetMigrationJobsByBucket(ctx context.Context, bucketName string) ([]*MigrationJob, error) {
	return cm.queryMigrationJobs(ctx, `WHERE bucket_name = ? ORDER BY created_at DESC`, bucketName)
}

// unfinishedMigrationJobs returns the jobs a restart left running.
func (cm *Manager) unfinishedMigrationJobs(ctx context.Context) ([]*MigrationJob, error) {
	return cm.queryMigrationJobs(ctx, `WHERE status IN (?, ?, ?) ORDER BY id`,
		MigrationStatusPending, MigrationStatusInProgress, MigrationStatusCommitting)
}

// migrationPageSize is how many keys are copied and then verified together.
const migrationPageSize = 1000

// errNodeGone: the node a migration talks to has left the cluster.
var errNodeGone = errors.New("the node is no longer in the cluster")

// migrationStepError is a step the target of a migration refused.
type migrationStepError struct {
	status  int
	message string
}

func (e *migrationStepError) Error() string {
	return fmt.Sprintf("the target answered %d: %s", e.status, e.message)
}

func stepStatus(err error) int {
	var stepErr *migrationStepError
	if errors.As(err, &stepErr) {
		return stepErr.status
	}
	return 0
}

// tenantUsage is the tenant storage a bucket that leaves the node stops using.
type tenantUsage interface {
	DecrementTenantStorage(ctx context.Context, tenantID string, bytes int64) error
}

// BucketMigrator moves buckets from this node to others, in a cluster whose
// buckets each live on one node. A bucket moves whole: every version and
// delete marker with its metadata, tags, ACL and lock state, the bucket's
// configuration and ACL, and its rows in the node's database. Its writes are
// held from the start of the copy until the move is committed or undone;
// reads go on.
type BucketMigrator struct {
	mgr     *Manager
	objects object.Manager
	store   metadata.Store
	buckets bucketRemover
	acls    acl.Manager
	usage   tenantUsage
	gate    *BucketWriteGate
	retry   time.Duration

	mu      sync.Mutex
	ctx     context.Context
	running map[string]bool
	wg      sync.WaitGroup
}

// NewBucketMigrator returns the source side of migrations on this node. usage
// may be nil.
func NewBucketMigrator(mgr *Manager, objects object.Manager, store metadata.Store, buckets bucketRemover, acls acl.Manager, usage tenantUsage, gate *BucketWriteGate) *BucketMigrator {
	return &BucketMigrator{
		mgr:     mgr,
		objects: objects,
		store:   store,
		buckets: buckets,
		acls:    acls,
		usage:   usage,
		gate:    gate,
		retry:   30 * time.Second,
		running: make(map[string]bool),
	}
}

// SetRetryInterval sets how long a step that failed waits before it is tried
// again: removing the copy of a failed migration, or handing a bucket over.
func (bm *BucketMigrator) SetRetryInterval(d time.Duration) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	bm.retry = d
}

func (bm *BucketMigrator) retryInterval() time.Duration {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return bm.retry
}

// Start takes over the migrations a restart interrupted, and must run before
// the node serves requests: a move being committed has its bucket's writes
// held again and is finished; one still copying is undone. ctx bounds every
// migration of this node.
func (bm *BucketMigrator) Start(ctx context.Context) {
	bm.mu.Lock()
	bm.ctx = ctx
	bm.mu.Unlock()

	jobs, err := bm.mgr.unfinishedMigrationJobs(ctx)
	if err != nil {
		logrus.WithError(err).Error("Could not read the bucket migrations a restart interrupted")
		return
	}
	for _, job := range jobs {
		if !bm.reserve(job.BucketName) {
			continue
		}
		if job.Status == MigrationStatusCommitting {
			_ = bm.gate.Freeze(ctx, job.BucketName)
			bm.launch(job, bm.finish)
			continue
		}
		bm.launch(job, func(job *MigrationJob) {
			bm.fail(job, errors.New("interrupted by a restart of the node"))
		})
	}
}

// Wait returns once no migration runs on this node.
func (bm *BucketMigrator) Wait() {
	bm.wg.Wait()
}

func (bm *BucketMigrator) lifetime() context.Context {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return bm.ctx
}

func (bm *BucketMigrator) reserve(bucketName string) bool {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	if bm.running[bucketName] {
		return false
	}
	bm.running[bucketName] = true
	return true
}

func (bm *BucketMigrator) release(bucketName string) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	delete(bm.running, bucketName)
}

// launch runs step for a job whose bucket is reserved, and releases it after.
func (bm *BucketMigrator) launch(job *MigrationJob, step func(*MigrationJob)) {
	bm.wg.Add(1)
	go func() {
		defer bm.wg.Done()
		defer bm.release(job.BucketName)
		step(job)
	}()
}

// Migrate starts moving bucketName from this node to targetNodeID and returns
// the job that follows it.
func (bm *BucketMigrator) Migrate(ctx context.Context, bucketName, targetNodeID string) (*MigrationJob, error) {
	if bm.lifetime() == nil {
		return nil, fmt.Errorf("bucket migrations are not running on this node")
	}
	if !bm.mgr.IsClusterEnabled() {
		return nil, fmt.Errorf("%w: the cluster is not enabled", ErrMigrationInvalid)
	}
	factor, err := bm.mgr.GetReplicationFactor(ctx)
	if err != nil {
		return nil, err
	}
	if factor > 1 {
		return nil, fmt.Errorf("%w: with a replication factor of %d every node holds every bucket", ErrMigrationInvalid, factor)
	}
	b, err := bm.store.GetBucketByName(ctx, bucketName)
	if errors.Is(err, metadata.ErrBucketNotFound) || (err == nil && b.Moving()) {
		return nil, fmt.Errorf("%w: bucket %s does not live on this node", ErrMigrationNotFound, bucketName)
	}
	if err != nil {
		return nil, err
	}
	localID, err := bm.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return nil, err
	}
	if targetNodeID == localID {
		return nil, fmt.Errorf("%w: bucket %s already lives on this node", ErrMigrationInvalid, bucketName)
	}
	target, err := bm.mgr.GetNode(ctx, targetNodeID)
	if errors.Is(err, ErrNodeNotFound) {
		return nil, fmt.Errorf("%w: node %s is not in the cluster", ErrMigrationInvalid, targetNodeID)
	}
	if err != nil {
		return nil, err
	}
	if target.HealthStatus != HealthStatusHealthy {
		return nil, fmt.Errorf("%w: node %s is %s", ErrMigrationConflict, target.Name, target.HealthStatus)
	}
	if !bm.reserve(bucketName) {
		return nil, fmt.Errorf("%w: bucket %s is already being migrated", ErrMigrationConflict, bucketName)
	}
	if err := bm.refuseOpenUploads(ctx, b); err != nil {
		bm.release(bucketName)
		return nil, err
	}

	now := time.Now()
	job := &MigrationJob{
		BucketName:   bucketName,
		SourceNodeID: localID,
		TargetNodeID: targetNodeID,
		Status:       MigrationStatusInProgress,
		ObjectsTotal: b.ObjectCount,
		BytesTotal:   b.TotalSize,
		DeleteSource: true,
		VerifyData:   true,
		StartedAt:    &now,
	}
	if err := bm.mgr.CreateMigrationJob(ctx, job); err != nil {
		bm.release(bucketName)
		return nil, err
	}
	bm.launch(job, bm.run)
	return job, nil
}

// refuseOpenUploads: an upload in progress holds parts that exist only here
// and cannot be completed once the bucket moves.
func (bm *BucketMigrator) refuseOpenUploads(ctx context.Context, b *metadata.BucketMetadata) error {
	uploads, err := bm.objects.ListMultipartUploads(ctx, pathOf(b))
	if err != nil {
		return err
	}
	if len(uploads) > 0 {
		return fmt.Errorf("%w: bucket %s has %d multipart uploads in progress; complete or abort them first",
			ErrMigrationConflict, b.Name, len(uploads))
	}
	return nil
}

// run holds the bucket's writes, copies it and hands it over.
func (bm *BucketMigrator) run(job *MigrationJob) {
	ctx := bm.lifetime()
	if err := bm.gate.Freeze(ctx, job.BucketName); err != nil {
		bm.fail(job, err)
		return
	}
	if err := bm.copyBucket(ctx, job); err != nil {
		bm.fail(job, err)
		return
	}
	bm.finish(job)
}

// copyBucket fills the copy on the target and checks it, a page of keys at a
// time.
func (bm *BucketMigrator) copyBucket(ctx context.Context, job *MigrationJob) error {
	b, err := bm.store.GetBucketByName(ctx, job.BucketName)
	if err != nil {
		return err
	}
	if err := bm.refuseOpenUploads(ctx, b); err != nil {
		return err
	}
	target, localID, err := bm.peer(ctx, job.TargetNodeID)
	if err != nil {
		return err
	}
	bucketACL, err := bm.acls.GetBucketACL(ctx, b.TenantID, b.Name)
	if err != nil {
		return fmt.Errorf("read the bucket ACL: %w", err)
	}
	stage := MigrationStage{JobID: job.ID, SourceNodeID: localID, Bucket: b, ACL: bucketACL}
	if err := bm.call(ctx, target, localID, "stage", stage, nil); err != nil {
		return fmt.Errorf("prepare the copy on %s: %w", target.Name, err)
	}

	path := pathOf(b)
	client := NewProxyClient(bm.mgr.GetTLSConfig())
	marker := ""
	for {
		entries, next, err := bm.store.ListObjects(ctx, path, "", marker, migrationPageSize)
		if err != nil {
			return fmt.Errorf("list the keys: %w", err)
		}
		keys := make([]string, 0, len(entries))
		for _, e := range entries {
			sent, err := sendKeyVersions(ctx, client, bm.objects, target, localID, path, e.Key)
			if err != nil {
				return fmt.Errorf("copy %s: %w", e.Key, err)
			}
			keys = append(keys, e.Key)
			job.BytesMigrated += sent
			if e.ETag != "" {
				job.ObjectsMigrated++
			}
		}
		if err := bm.verify(ctx, target, localID, path, keys); err != nil {
			return err
		}
		if err := bm.mgr.UpdateMigrationJob(ctx, job); err != nil {
			logrus.WithError(err).WithField("migration_id", job.ID).Warn("Could not record the migration's progress")
		}
		if next == "" {
			return nil
		}
		marker = next
	}
}

// verify checks that the target holds keys as this node does.
func (bm *BucketMigrator) verify(ctx context.Context, target *Node, localID, path string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	var theirs map[string][]VersionManifest
	if err := bm.call(ctx, target, localID, "manifest", MigrationManifestRequest{Bucket: path, Keys: keys}, &theirs); err != nil {
		return fmt.Errorf("read the copy back from %s: %w", target.Name, err)
	}
	for _, key := range keys {
		ours, err := KeyManifest(ctx, bm.objects, path, key)
		if err != nil {
			return fmt.Errorf("describe %s: %w", key, err)
		}
		if !slices.Equal(ours, theirs[key]) {
			return fmt.Errorf("the copy of %s on %s differs from this node's", key, target.Name)
		}
	}
	return nil
}

// finish hands the bucket over. Once the job is marked committing the move is
// completed, retrying until it is, and never undone.
func (bm *BucketMigrator) finish(job *MigrationJob) {
	ctx := bm.lifetime()
	if job.Status != MigrationStatusCommitting {
		job.Status = MigrationStatusCommitting
		if err := bm.mgr.UpdateMigrationJob(ctx, job); err != nil {
			bm.fail(job, fmt.Errorf("record the hand-over: %w", err))
			return
		}
	}
	for {
		err := bm.commit(ctx, job)
		if err == nil {
			break
		}
		if stepStatus(err) == http.StatusNotFound || errors.Is(err, errNodeGone) {
			// The copy is not on the target any more. Nothing was handed over
			// while this node still holds the bucket in full.
			if b, gErr := bm.store.GetBucketByName(ctx, job.BucketName); gErr == nil && !b.Moving() {
				bm.fail(job, err)
				return
			}
		}
		logrus.WithError(err).WithFields(logrus.Fields{
			"migration_id": job.ID,
			"bucket":       job.BucketName,
		}).Error("Could not hand the migrated bucket over; retrying")
		if !sleepContext(ctx, bm.retryInterval()) {
			return
		}
	}

	bm.gate.Thaw(job.BucketName)
	now := time.Now()
	job.Status = MigrationStatusCompleted
	job.CompletedAt = &now
	job.ErrorMessage = ""
	if err := bm.mgr.UpdateMigrationJob(context.WithoutCancel(ctx), job); err != nil {
		logrus.WithError(err).WithField("migration_id", job.ID).Warn("Could not record the migration as completed")
	}
	logrus.WithFields(logrus.Fields{
		"migration_id": job.ID,
		"bucket":       job.BucketName,
		"target":       job.TargetNodeID,
	}).Info("Bucket migrated")
}

// commit makes the target's copy the bucket, then removes it from here: first
// hidden, so no request is answered from it, then deleted with its rows and
// ACL. Each step can be repeated.
func (bm *BucketMigrator) commit(ctx context.Context, job *MigrationJob) error {
	b, err := bm.store.GetBucketByName(ctx, job.BucketName)
	if errors.Is(err, metadata.ErrBucketNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	target, localID, err := bm.peer(ctx, job.TargetNodeID)
	if err != nil {
		return err
	}
	rows, err := readBucketRows(ctx, bm.mgr.db, b.Name)
	if err != nil {
		return err
	}
	commit := MigrationCommit{JobID: job.ID, SourceNodeID: localID, Bucket: b.Name, Rows: rows}
	if err := bm.call(ctx, target, localID, "commit", commit, nil); err != nil {
		return fmt.Errorf("commit the copy on %s: %w", target.Name, err)
	}

	if !b.Moving() {
		hidden := *b
		hidden.Metadata = make(map[string]string, len(b.Metadata)+1)
		for k, v := range b.Metadata {
			hidden.Metadata[k] = v
		}
		hidden.Metadata[metadata.BucketMovingKey] = outgoingMarker(target.ID, job.ID)
		if err := bm.store.UpdateBucket(ctx, &hidden); err != nil {
			return err
		}
		if bm.usage != nil && b.TenantID != "" && b.TotalSize > 0 {
			if err := bm.usage.DecrementTenantStorage(ctx, b.TenantID, b.TotalSize); err != nil {
				logrus.WithError(err).WithField("tenant_id", b.TenantID).Warn("Could not release the migrated bucket's storage from the tenant on this node")
			}
		}
	}
	if err := deleteBucketRows(ctx, bm.mgr.db, b.Name); err != nil {
		return err
	}
	if err := bm.acls.DeleteBucketACL(ctx, b.TenantID, b.Name); err != nil {
		return err
	}
	if err := bm.buckets.ForceDeleteBucket(ctx, b.TenantID, b.Name); err != nil && !errors.Is(err, bucket.ErrBucketNotFound) {
		return err
	}
	return nil
}

// fail undoes a migration that did not reach its hand-over: the bucket takes
// writes again at once, and the copy on the target is removed, retrying until
// the target answers or leaves the cluster.
func (bm *BucketMigrator) fail(job *MigrationJob, cause error) {
	ctx := bm.lifetime()
	bm.gate.Thaw(job.BucketName)
	job.ErrorMessage = cause.Error()
	logrus.WithError(cause).WithFields(logrus.Fields{
		"migration_id": job.ID,
		"bucket":       job.BucketName,
		"target":       job.TargetNodeID,
	}).Error("Bucket migration failed; the bucket stays on this node")

	for {
		err := bm.abort(ctx, job)
		if err == nil || errors.Is(err, errNodeGone) || stepStatus(err) == http.StatusConflict {
			break
		}
		logrus.WithError(err).WithField("migration_id", job.ID).Warn("Could not remove the copy on the target; retrying")
		if !sleepContext(ctx, bm.retryInterval()) {
			// A restart finds the job unfinished and removes the copy then.
			if uErr := bm.mgr.UpdateMigrationJob(context.WithoutCancel(ctx), job); uErr != nil {
				logrus.WithError(uErr).WithField("migration_id", job.ID).Warn("Could not record the migration's failure")
			}
			return
		}
	}

	now := time.Now()
	job.Status = MigrationStatusFailed
	job.CompletedAt = &now
	if err := bm.mgr.UpdateMigrationJob(context.WithoutCancel(ctx), job); err != nil {
		logrus.WithError(err).WithField("migration_id", job.ID).Warn("Could not record the migration's failure")
	}
}

func (bm *BucketMigrator) abort(ctx context.Context, job *MigrationJob) error {
	target, localID, err := bm.peer(ctx, job.TargetNodeID)
	if err != nil {
		return err
	}
	return bm.call(ctx, target, localID, "abort",
		MigrationAbort{JobID: job.ID, SourceNodeID: localID, Bucket: job.BucketName}, nil)
}

func (bm *BucketMigrator) peer(ctx context.Context, nodeID string) (*Node, string, error) {
	node, err := bm.mgr.GetNode(ctx, nodeID)
	if errors.Is(err, ErrNodeNotFound) {
		return nil, "", errNodeGone
	}
	if err != nil {
		return nil, "", err
	}
	localID, err := bm.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return nil, "", err
	}
	return node, localID, nil
}

// call runs one step of a migration on the target. The body is signed by its
// digest, so a large one is not limited to what a signed request buffers.
func (bm *BucketMigrator) call(ctx context.Context, target *Node, localID, step string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	client := NewProxyClient(bm.mgr.GetTLSConfig())
	url := strings.TrimRight(target.Endpoint, "/") + "/api/internal/cluster/migration/" + step
	req, err := client.CreateAuthenticatedRequestWithDigest(ctx, http.MethodPost, url, bytes.NewReader(body),
		localID, target.NodeToken, "sha256:"+hex.EncodeToString(sum[:]))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.ContentLength = int64(len(body))
	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &migrationStepError{status: resp.StatusCode, message: strings.TrimSpace(string(msg))}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// sleepContext waits for d and reports whether ctx is still live.
func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
