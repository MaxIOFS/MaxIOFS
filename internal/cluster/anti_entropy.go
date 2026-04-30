package cluster

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// Pebble key under which the scrubber persists its mid-cycle checkpoint.
const scrubCheckpointKey = "ha:scrub:checkpoint"

// Defaults applied if cluster_global_config does not have a value yet.
const (
	defaultScrubIntervalHours = 24
	defaultScrubRateLimit     = 50
	defaultScrubBatchSize     = 500
	scrubRunsKeepRecent       = 30
)

// ScrubCheckpoint is the JSON blob written to Pebble between batches so that
// a server restart resumes mid-cycle from the same place.
type ScrubCheckpoint struct {
	CycleID          string    `json:"cycle_id"`
	StartedAt        time.Time `json:"started_at"`
	BucketOrder      []string  `json:"bucket_order"`
	CurrentBucketIdx int       `json:"current_bucket_idx"`
	LastKey          string    `json:"last_key"`
	BucketsScanned   int       `json:"buckets_scanned"`
	ObjectsCompared  int64     `json:"objects_compared"`
	DivergencesFound int64     `json:"divergences_found"`
	DivergencesFixed int64     `json:"divergences_fixed"`
	RunID            int64     `json:"run_id"`
}

// ScrubRun mirrors one row of the ha_scrub_runs table for status display.
type ScrubRun struct {
	ID               int64      `json:"id"`
	CycleID          string     `json:"cycle_id"`
	StartedAt        time.Time  `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	Status           string     `json:"status"`
	BucketsScanned   int        `json:"buckets_scanned"`
	ObjectsCompared  int64      `json:"objects_compared"`
	DivergencesFound int64      `json:"divergences_found"`
	DivergencesFixed int64      `json:"divergences_fixed"`
	ErrorMessage     string     `json:"error_message,omitempty"`
}

// AntiEntropyScrubber compares local objects with their peer replicas on a
// schedule and reconciles divergences via LWW.  One instance runs per node.
type AntiEntropyScrubber struct {
	objMgr    object.Manager
	bucketMgr bucket.Manager
	mgr       *Manager
	rawKV     metadata.RawKVStore

	mu        sync.Mutex
	cancel    context.CancelFunc
	currentCP *ScrubCheckpoint // in-memory snapshot for status endpoint
}

// NewAntiEntropyScrubber wires a scrubber.  rawKV is used for the persistent
// mid-cycle checkpoint; passing the underlying metadata store works.
func NewAntiEntropyScrubber(objMgr object.Manager, bucketMgr bucket.Manager, mgr *Manager, rawKV metadata.RawKVStore) *AntiEntropyScrubber {
	return &AntiEntropyScrubber{
		objMgr:    objMgr,
		bucketMgr: bucketMgr,
		mgr:       mgr,
		rawKV:     rawKV,
	}
}

// Start launches the background scheduler goroutine.  Returns immediately.
// Safe to call once at server startup; subsequent calls are no-ops.
func (s *AntiEntropyScrubber) Start(ctx context.Context) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	scrubCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()

	go s.run(scrubCtx)
}

// Stop terminates the scheduler.  In-flight cycle is cancelled cleanly; the
// current checkpoint stays in Pebble so the next Start resumes from there.
func (s *AntiEntropyScrubber) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}

// CurrentCheckpoint returns a snapshot of the in-progress cycle (nil when
// idle).  Used by the status endpoint.
func (s *AntiEntropyScrubber) CurrentCheckpoint() *ScrubCheckpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currentCP == nil {
		return nil
	}
	cp := *s.currentCP
	return &cp
}

// ListRecentRuns returns the last `limit` scrub runs newest first.
func (s *AntiEntropyScrubber) ListRecentRuns(ctx context.Context, limit int) ([]ScrubRun, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.mgr.db.QueryContext(ctx,
		`SELECT id, cycle_id, started_at, completed_at, status,
		        buckets_scanned, objects_compared, divergences_found,
		        divergences_fixed, error_message
		 FROM ha_scrub_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []ScrubRun
	for rows.Next() {
		var r ScrubRun
		var completedAt sql.NullTime
		var errMsg sql.NullString
		if err := rows.Scan(
			&r.ID, &r.CycleID, &r.StartedAt, &completedAt, &r.Status,
			&r.BucketsScanned, &r.ObjectsCompared, &r.DivergencesFound,
			&r.DivergencesFixed, &errMsg,
		); err != nil {
			continue
		}
		if completedAt.Valid {
			r.CompletedAt = &completedAt.Time
		}
		if errMsg.Valid {
			r.ErrorMessage = errMsg.String
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// ---------------------------------------------------------------------------
// Scheduler
// ---------------------------------------------------------------------------

func (s *AntiEntropyScrubber) run(ctx context.Context) {
	// Random jitter (5-60 min) before the first cycle so multi-node clusters
	// do not all start scanning at the same instant after a synchronized boot.
	jitter := time.Duration(5+rand.Intn(56)) * time.Minute
	logrus.WithField("first_run_in", jitter).Info("AntiEntropyScrubber: scheduled first cycle")

	// On boot, if a checkpoint exists immediately resume — the previous
	// process was killed mid-cycle and we should finish that cycle first.
	if cp := s.loadCheckpoint(ctx); cp != nil {
		logrus.WithFields(logrus.Fields{
			"cycle_id":         cp.CycleID,
			"resume_bucket":    safeIndex(cp.BucketOrder, cp.CurrentBucketIdx),
			"buckets_scanned":  cp.BucketsScanned,
			"objects_compared": cp.ObjectsCompared,
		}).Info("AntiEntropyScrubber: resuming interrupted cycle")
		s.runCycle(ctx, cp)
	}

	timer := time.NewTimer(jitter)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if !s.scrubEnabled(ctx) {
				logrus.Debug("AntiEntropyScrubber: disabled by config, skipping cycle")
			} else {
				s.runCycle(ctx, nil)
			}
			timer.Reset(s.cycleInterval(ctx))
		}
	}
}

// runCycle performs one full pass.  When `resume` is non-nil it picks up from
// that checkpoint; otherwise a brand-new cycle is started.
func (s *AntiEntropyScrubber) runCycle(ctx context.Context, resume *ScrubCheckpoint) {
	if !s.mgr.IsClusterEnabled() {
		return
	}
	factor, err := s.mgr.GetReplicationFactor(ctx)
	if err != nil || factor <= 1 {
		return
	}

	cp, runID, err := s.beginCycle(ctx, resume)
	if err != nil {
		logrus.WithError(err).Error("AntiEntropyScrubber: failed to begin cycle")
		return
	}

	s.mu.Lock()
	s.currentCP = cp
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.currentCP = nil
		s.mu.Unlock()
	}()

	cycleErr := s.executeCycle(ctx, cp)

	if cycleErr != nil && ctx.Err() == nil {
		// Real failure (not shutdown).  Mark the run failed, but leave the
		// checkpoint in place so the next interval retries.
		s.mgr.db.ExecContext(context.Background(), //nolint:errcheck
			`UPDATE ha_scrub_runs SET status='failed', completed_at=?, error_message=?,
			        buckets_scanned=?, objects_compared=?, divergences_found=?, divergences_fixed=?
			 WHERE id=?`,
			time.Now(), cycleErr.Error(),
			cp.BucketsScanned, cp.ObjectsCompared, cp.DivergencesFound, cp.DivergencesFixed,
			runID)
		logrus.WithError(cycleErr).Error("AntiEntropyScrubber: cycle failed")
		return
	}

	if ctx.Err() != nil {
		// Shutdown: leave run as 'running' and checkpoint intact so resume works.
		return
	}

	// Clean completion: clear checkpoint, mark run done, prune old run rows.
	s.deleteCheckpoint(ctx)
	s.mgr.db.ExecContext(context.Background(), //nolint:errcheck
		`UPDATE ha_scrub_runs SET status='done', completed_at=?,
		        buckets_scanned=?, objects_compared=?, divergences_found=?, divergences_fixed=?
		 WHERE id=?`,
		time.Now(),
		cp.BucketsScanned, cp.ObjectsCompared, cp.DivergencesFound, cp.DivergencesFixed,
		runID)
	s.pruneRuns(ctx)

	logrus.WithFields(logrus.Fields{
		"cycle_id":          cp.CycleID,
		"buckets_scanned":   cp.BucketsScanned,
		"objects_compared":  cp.ObjectsCompared,
		"divergences_found": cp.DivergencesFound,
		"divergences_fixed": cp.DivergencesFixed,
	}).Info("AntiEntropyScrubber: cycle completed")
}

// beginCycle returns the checkpoint to use and the ha_scrub_runs row id.
func (s *AntiEntropyScrubber) beginCycle(ctx context.Context, resume *ScrubCheckpoint) (*ScrubCheckpoint, int64, error) {
	if resume != nil {
		return resume, resume.RunID, nil
	}

	buckets, err := s.bucketMgr.ListBuckets(ctx, "")
	if err != nil {
		return nil, 0, fmt.Errorf("list buckets: %w", err)
	}
	order := make([]string, 0, len(buckets))
	for _, b := range buckets {
		order = append(order, bucketPath(b))
	}
	rand.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })

	cycleID := uuid.New().String()
	now := time.Now()
	res, err := s.mgr.db.ExecContext(ctx,
		`INSERT INTO ha_scrub_runs (cycle_id, started_at, status) VALUES (?, ?, 'running')`,
		cycleID, now)
	if err != nil {
		return nil, 0, fmt.Errorf("insert ha_scrub_runs: %w", err)
	}
	runID, _ := res.LastInsertId()

	cp := &ScrubCheckpoint{
		CycleID:     cycleID,
		StartedAt:   now,
		BucketOrder: order,
		RunID:       runID,
	}
	s.saveCheckpoint(ctx, cp)
	return cp, runID, nil
}

// executeCycle iterates buckets in checkpoint order, comparing each against
// every healthy peer.  Updates the checkpoint after every batch.
func (s *AntiEntropyScrubber) executeCycle(ctx context.Context, cp *ScrubCheckpoint) error {
	rateLimit := s.cycleRateLimit(ctx)
	perObjectDelay := time.Second / time.Duration(rateLimit)
	batchSize := s.cycleBatchSize(ctx)

	localID, err := s.mgr.GetLocalNodeID(ctx)
	if err != nil {
		return fmt.Errorf("get local node ID: %w", err)
	}
	client := NewProxyClient(s.mgr.GetTLSConfig())

	for ; cp.CurrentBucketIdx < len(cp.BucketOrder); cp.CurrentBucketIdx++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		bp := cp.BucketOrder[cp.CurrentBucketIdx]
		marker := cp.LastKey

		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			result, listErr := s.objMgr.ListObjects(ctx, bp, "", "", marker, batchSize)
			if listErr != nil {
				logrus.WithError(listErr).WithField("bucket", bp).
					Warn("AntiEntropyScrubber: list error, skipping bucket")
				break
			}
			if len(result.Objects) == 0 {
				break
			}

			peers, err := s.healthyPeers(ctx, localID)
			if err != nil || len(peers) == 0 {
				logrus.WithError(err).Warn("AntiEntropyScrubber: no healthy peers, aborting cycle")
				return fmt.Errorf("no healthy peers")
			}

			s.processBatch(ctx, client, peers, localID, bp, result.Objects, cp, perObjectDelay)

			marker = result.NextMarker
			cp.LastKey = marker
			s.saveCheckpoint(ctx, cp)
			s.persistRunProgress(ctx, cp)

			if !result.IsTruncated {
				break
			}
		}

		cp.BucketsScanned++
		cp.LastKey = ""
		s.saveCheckpoint(ctx, cp)
	}
	return nil
}

// processBatch compares one page of local objects against every peer and
// reconciles divergences.  Errors against individual peers/objects are logged
// but do not abort the cycle.
func (s *AntiEntropyScrubber) processBatch(
	ctx context.Context,
	client *ProxyClient,
	peers []*Node,
	localID, bucketPath string,
	localObjects []object.Object,
	cp *ScrubCheckpoint,
	perObjectDelay time.Duration,
) {
	keys := make([]string, 0, len(localObjects))
	localByKey := make(map[string]*object.Object, len(localObjects))
	for i := range localObjects {
		keys = append(keys, localObjects[i].Key)
		localByKey[localObjects[i].Key] = &localObjects[i]
	}

	for _, peer := range peers {
		if ctx.Err() != nil {
			return
		}
		entries, err := s.fetchPeerChecksums(ctx, client, peer, localID, bucketPath, keys)
		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"peer": peer.ID, "bucket": bucketPath,
			}).Warn("AntiEntropyScrubber: checksum-batch failed, skipping peer for this batch")
			continue
		}

		peerByKey := make(map[string]*ChecksumEntry, len(entries))
		for i := range entries {
			peerByKey[entries[i].Key] = &entries[i]
		}

		for _, key := range keys {
			if ctx.Err() != nil {
				return
			}
			cp.ObjectsCompared++

			local := localByKey[key]
			peerEntry := peerByKey[key]
			divergence, action := classifyDivergence(local, peerEntry)
			if divergence != divNone {
				cp.DivergencesFound++
				if s.applyAction(ctx, client, peer, localID, bucketPath, key, local, action) {
					cp.DivergencesFixed++
				}
			}

			if perObjectDelay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(perObjectDelay):
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Divergence classification + action
// ---------------------------------------------------------------------------

// ChecksumEntry is the per-key payload returned by /api/internal/ha/checksum-batch.
type ChecksumEntry struct {
	Key          string `json:"key"`
	Found        bool   `json:"found"`
	ETag         string `json:"etag,omitempty"`
	Size         int64  `json:"size,omitempty"`
	LastModified int64  `json:"last_modified,omitempty"` // unix seconds
}

type divergenceKind int

const (
	divNone divergenceKind = iota
	divPeerMissing
	divLocalNewer
	divPeerNewer
	divTieDifferentETag // same last_modified, different ETag — log only
)

type reconcileAction int

const (
	actNone reconcileAction = iota
	actPushToPeer
	actPullFromPeer
)

// classifyDivergence applies the LWW rules.  multipart objects (ETag has a
// "-N" suffix) skip checksum compare and rely on existence + size + 1s
// last_modified tolerance.
func classifyDivergence(local *object.Object, peer *ChecksumEntry) (divergenceKind, reconcileAction) {
	if local == nil {
		return divNone, actNone
	}
	if peer == nil || !peer.Found {
		return divPeerMissing, actPushToPeer
	}

	multipart := isMultipartETag(local.ETag) || isMultipartETag(peer.ETag)

	if multipart {
		// Existence + size + ~1s mtime tolerance.
		if local.Size != peer.Size || abs64(local.LastModified.Unix()-peer.LastModified) > 1 {
			return lwwClassify(local, peer)
		}
		return divNone, actNone
	}

	if local.ETag == peer.ETag {
		return divNone, actNone
	}
	return lwwClassify(local, peer)
}

func lwwClassify(local *object.Object, peer *ChecksumEntry) (divergenceKind, reconcileAction) {
	localTS := local.LastModified.Unix()
	if localTS > peer.LastModified {
		return divLocalNewer, actPushToPeer
	}
	if localTS < peer.LastModified {
		return divPeerNewer, actPullFromPeer
	}
	return divTieDifferentETag, actNone
}

func isMultipartETag(etag string) bool {
	// Multipart S3 ETags have the form "<md5>-<N>" with N >= 1.
	idx := strings.LastIndex(etag, "-")
	if idx <= 0 || idx == len(etag)-1 {
		return false
	}
	_, err := strconv.Atoi(etag[idx+1:])
	return err == nil
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// applyAction performs the reconciliation push or pull.  Returns true on
// success so the caller can increment divergences_fixed.
func (s *AntiEntropyScrubber) applyAction(
	ctx context.Context,
	client *ProxyClient,
	peer *Node,
	localID, bucketPath, key string,
	local *object.Object,
	action reconcileAction,
) bool {
	switch action {
	case actPushToPeer:
		if err := s.pushObjectToPeer(ctx, client, peer, localID, bucketPath, key); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"peer": peer.ID, "bucket": bucketPath, "key": key,
			}).Warn("AntiEntropyScrubber: push failed")
			return false
		}
		logrus.WithFields(logrus.Fields{
			"peer": peer.ID, "bucket": bucketPath, "key": key,
		}).Info("AntiEntropyScrubber: pushed local copy to peer")
		return true

	case actPullFromPeer:
		if err := s.pullObjectFromPeer(ctx, client, peer, localID, bucketPath, key); err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"peer": peer.ID, "bucket": bucketPath, "key": key,
			}).Warn("AntiEntropyScrubber: pull failed")
			return false
		}
		logrus.WithFields(logrus.Fields{
			"peer": peer.ID, "bucket": bucketPath, "key": key,
		}).Info("AntiEntropyScrubber: pulled newer copy from peer")
		return true

	default:
		// divTieDifferentETag — manual investigation required.
		logrus.WithFields(logrus.Fields{
			"peer": peer.ID, "bucket": bucketPath, "key": key,
			"local_etag": local.ETag, "local_mtime": local.LastModified.Unix(),
		}).Warn("AntiEntropyScrubber: divergence with identical timestamp, no auto-fix")
		return false
	}
}

// ---------------------------------------------------------------------------
// Peer I/O
// ---------------------------------------------------------------------------

// fetchPeerChecksums calls POST /api/internal/ha/checksum-batch on the peer.
func (s *AntiEntropyScrubber) fetchPeerChecksums(
	ctx context.Context,
	client *ProxyClient,
	peer *Node,
	localID, bucketPath string,
	keys []string,
) ([]ChecksumEntry, error) {
	body, err := json.Marshal(struct {
		Bucket string   `json:"bucket"`
		Keys   []string `json:"keys"`
	}{Bucket: bucketPath, Keys: keys})
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/internal/ha/checksum-batch", peer.Endpoint)
	req, err := client.CreateAuthenticatedRequest(ctx, "POST", url, bytes.NewReader(body), localID, peer.NodeToken)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("checksum-batch %d: %s", resp.StatusCode, string(b))
	}

	var out struct {
		Entries []ChecksumEntry `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return out.Entries, nil
}

// pushObjectToPeer streams the local copy of `key` to the peer's HA receive
// endpoint.  Same call shape as HASyncWorker.syncObject.
func (s *AntiEntropyScrubber) pushObjectToPeer(
	ctx context.Context,
	client *ProxyClient,
	peer *Node,
	localID, bucketPath, key string,
) error {
	obj, reader, err := s.objMgr.GetObject(ctx, bucketPath, key)
	if err != nil {
		return err
	}
	defer reader.Close()

	url := fmt.Sprintf("%s/api/internal/ha/objects/%s", peer.Endpoint, escapeHAObjectKey(key))
	req, err := client.CreateAuthenticatedRequest(ctx, "PUT", url, reader, localID, peer.NodeToken)
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
		return fmt.Errorf("peer returned %d", resp.StatusCode)
	}
	return nil
}

// pullObjectFromPeer fetches the peer's copy and stores it locally as a
// replica write (suppresses re-fanout via WithHAReplicaContext).
func (s *AntiEntropyScrubber) pullObjectFromPeer(
	ctx context.Context,
	client *ProxyClient,
	peer *Node,
	localID, bucketPath, key string,
) error {
	url := fmt.Sprintf("%s/api/internal/ha/objects/%s?bucket=%s", peer.Endpoint, escapeHAObjectKey(key), urlEscapeBucket(bucketPath))
	req, err := client.CreateAuthenticatedRequest(ctx, "GET", url, nil, localID, peer.NodeToken)
	if err != nil {
		return err
	}
	resp, err := client.DoAuthenticatedRequest(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// Peer deleted it after the checksum was taken — race; skip.
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET object returned %d: %s", resp.StatusCode, string(b))
	}

	repCtx := WithHAReplicaContext(ctx)
	_, err = s.objMgr.PutObject(repCtx, bucketPath, key, resp.Body, resp.Header.Clone())
	return err
}

// urlEscapeBucket escapes only `?` and `&` and `#` so the bucket query value
// survives bucket paths that contain a literal `/` (tenant/bucket form).
func urlEscapeBucket(b string) string {
	r := strings.NewReplacer("?", "%3F", "&", "%26", "#", "%23", " ", "%20")
	return r.Replace(b)
}

// ---------------------------------------------------------------------------
// Helpers: peers, config, checkpoint persistence
// ---------------------------------------------------------------------------

func (s *AntiEntropyScrubber) healthyPeers(ctx context.Context, localID string) ([]*Node, error) {
	healthy, err := s.mgr.GetHealthyNodes(ctx)
	if err != nil {
		return nil, err
	}
	peers := make([]*Node, 0, len(healthy))
	for _, n := range healthy {
		if n.ID == localID {
			continue
		}
		peers = append(peers, n)
	}
	return peers, nil
}

func (s *AntiEntropyScrubber) scrubEnabled(ctx context.Context) bool {
	v, err := GetGlobalConfig(ctx, s.mgr.db, "ha.scrub_enabled")
	if err != nil {
		return true
	}
	return v == "true"
}

func (s *AntiEntropyScrubber) cycleInterval(ctx context.Context) time.Duration {
	v, err := GetGlobalConfig(ctx, s.mgr.db, "ha.scrub_interval_hours")
	if err != nil {
		return time.Duration(defaultScrubIntervalHours) * time.Hour
	}
	hours, err := strconv.Atoi(v)
	if err != nil || hours <= 0 {
		return time.Duration(defaultScrubIntervalHours) * time.Hour
	}
	return time.Duration(hours) * time.Hour
}

func (s *AntiEntropyScrubber) cycleRateLimit(ctx context.Context) int {
	v, err := GetGlobalConfig(ctx, s.mgr.db, "ha.scrub_rate_limit")
	if err != nil {
		return defaultScrubRateLimit
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultScrubRateLimit
	}
	return n
}

func (s *AntiEntropyScrubber) cycleBatchSize(ctx context.Context) int {
	v, err := GetGlobalConfig(ctx, s.mgr.db, "ha.scrub_batch_size")
	if err != nil {
		return defaultScrubBatchSize
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultScrubBatchSize
	}
	return n
}

func (s *AntiEntropyScrubber) loadCheckpoint(ctx context.Context) *ScrubCheckpoint {
	if s.rawKV == nil {
		return nil
	}
	data, err := s.rawKV.GetRaw(ctx, scrubCheckpointKey)
	if err != nil || len(data) == 0 {
		return nil
	}
	var cp ScrubCheckpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil
	}
	return &cp
}

func (s *AntiEntropyScrubber) saveCheckpoint(ctx context.Context, cp *ScrubCheckpoint) {
	if s.rawKV == nil || cp == nil {
		return
	}
	data, err := json.Marshal(cp)
	if err != nil {
		return
	}
	_ = s.rawKV.PutRaw(ctx, scrubCheckpointKey, data)
}

func (s *AntiEntropyScrubber) deleteCheckpoint(ctx context.Context) {
	if s.rawKV == nil {
		return
	}
	_ = s.rawKV.DeleteRaw(ctx, scrubCheckpointKey)
}

func (s *AntiEntropyScrubber) persistRunProgress(ctx context.Context, cp *ScrubCheckpoint) {
	s.mgr.db.ExecContext(ctx, //nolint:errcheck
		`UPDATE ha_scrub_runs SET buckets_scanned=?, objects_compared=?,
		        divergences_found=?, divergences_fixed=? WHERE id=?`,
		cp.BucketsScanned, cp.ObjectsCompared, cp.DivergencesFound, cp.DivergencesFixed, cp.RunID)
}

func (s *AntiEntropyScrubber) pruneRuns(ctx context.Context) {
	s.mgr.db.ExecContext(ctx, //nolint:errcheck
		`DELETE FROM ha_scrub_runs WHERE id NOT IN (
			SELECT id FROM ha_scrub_runs ORDER BY id DESC LIMIT ?
		)`, scrubRunsKeepRecent)
}

func safeIndex(s []string, i int) string {
	if i < 0 || i >= len(s) {
		return ""
	}
	return s[i]
}
