package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
)

// Background encryption worker: converts pre-existing plaintext objects
// (written by deployments that ran without an encryption key) to envelope
// encryption. New writes are always encrypted, so the worker only ever has
// to catch up on old data.
//
// It is the same component that will re-wrap DEKs on KEK rotation later —
// it walks every bucket/object and acts on each object's state (today:
// plaintext → convert; encrypted → skip).
//
// Behaviour:
//   - Load-aware: before each page it checks CPU/RAM via systemMetrics and
//     backs off while the node is busy.
//   - Checkpointed: progress (bucket + marker + counters) is persisted in
//     Pebble after every page, so a restart resumes where it left off.
//   - Single-flight: only one pass runs at a time.
//   - Paced: a small sleep between objects (plus the batch checkpoint)
//     keeps IO impact low, mirroring the integrity scrubber.

const (
	encWorkerStateKey = "encryption_worker:state"

	encWorkerInitialDelay = 2 * time.Minute
	encWorkerRescanEvery  = 24 * time.Hour
	encWorkerPageSize     = 200
	encWorkerLoadWait     = 30 * time.Second
	encWorkerMaxCPU       = 60.0 // percent
	encWorkerMaxMemory    = 85.0 // percent
)

// encryptionWorkerState is the persisted progress/checkpoint of the worker.
type encryptionWorkerState struct {
	Status        string `json:"status"` // idle | running | waiting_load | done
	CurrentBucket string `json:"currentBucket,omitempty"`
	Marker        string `json:"marker,omitempty"`
	BucketsDone   int    `json:"bucketsDone"`
	BucketsTotal  int    `json:"bucketsTotal"`
	Converted     int64  `json:"converted"`
	Skipped       int64  `json:"skipped"`
	Failed        int64  `json:"failed"`
	LastRunStart  int64  `json:"lastRunStart,omitempty"`
	LastRunEnd    int64  `json:"lastRunEnd,omitempty"`
	LastError     string `json:"lastError,omitempty"`
	UpdatedAt     int64  `json:"updatedAt"`
}

// objectEncryptor is the object-manager capability the worker needs. The HA
// wrapper promotes it through embedding, so the conversion always runs on the
// local node without cluster fanout (each node converts its own replicas).
type objectEncryptor interface {
	EncryptExistingObject(ctx context.Context, bucket, key string) (converted, skipped int, err error)
}

// startEncryptionWorker launches the background conversion goroutine. The
// first pass starts shortly after boot (so startup isn't burdened); after a
// completed pass it re-scans daily to catch strays.
func (s *Server) startEncryptionWorker(ctx context.Context) {
	if _, ok := s.objectManager.(objectEncryptor); !ok {
		logrus.Warn("Encryption worker: object manager does not support conversion, worker disabled")
		return
	}
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(encWorkerInitialDelay):
		}
		s.runEncryptionPass(ctx)

		ticker := time.NewTicker(encWorkerRescanEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runEncryptionPass(ctx)
			}
		}
	}()
}

// runEncryptionPass walks every bucket and converts plaintext objects.
func (s *Server) runEncryptionPass(ctx context.Context) {
	// Single-flight guard.
	if !s.encWorkerRunning.CompareAndSwap(false, true) {
		return
	}
	defer s.encWorkerRunning.Store(false)

	encryptor := s.objectManager.(objectEncryptor)

	state := s.loadEncryptionWorkerState(ctx)
	resumeBucket := ""
	resumeMarker := ""
	if state.Status == "running" || state.Status == "waiting_load" {
		// Previous pass was interrupted (restart) — resume from its checkpoint.
		resumeBucket = state.CurrentBucket
		resumeMarker = state.Marker
		logrus.WithFields(logrus.Fields{"bucket": resumeBucket, "marker": resumeMarker}).
			Info("Encryption worker: resuming interrupted pass")
	} else {
		state = &encryptionWorkerState{LastRunStart: time.Now().Unix()}
	}
	state.Status = "running"
	state.LastError = ""

	allBuckets, err := s.metadataStore.ListBuckets(ctx, "")
	if err != nil {
		logrus.WithError(err).Error("Encryption worker: failed to list buckets")
		state.Status = "idle"
		state.LastError = err.Error()
		s.saveEncryptionWorkerState(ctx, state)
		return
	}
	state.BucketsTotal = len(allBuckets)
	s.saveEncryptionWorkerState(ctx, state)

	// A checkpoint pointing at a bucket that no longer exists (deleted between
	// runs) must not be honoured: skipUntilResume would never flip off and the
	// whole pass would silently skip every bucket. Fall back to a full scan.
	if resumeBucket != "" {
		found := false
		for _, bkt := range allBuckets {
			bp := bkt.Name
			if bkt.TenantID != "" {
				bp = bkt.TenantID + "/" + bkt.Name
			}
			if bp == resumeBucket {
				found = true
				break
			}
		}
		if !found {
			logrus.WithField("bucket", resumeBucket).
				Warn("Encryption worker: checkpoint bucket no longer exists — restarting a full pass")
			resumeBucket = ""
			resumeMarker = ""
		}
	}

	logrus.WithField("buckets", len(allBuckets)).Info("Encryption worker: pass started")
	started := time.Now()
	skipUntilResume := resumeBucket != ""

	for i, bkt := range allBuckets {
		if ctx.Err() != nil {
			s.saveEncryptionWorkerState(ctx, state)
			return
		}

		bucketPath := bkt.Name
		if bkt.TenantID != "" {
			bucketPath = bkt.TenantID + "/" + bkt.Name
		}

		if skipUntilResume {
			if bucketPath != resumeBucket {
				state.BucketsDone = i + 1
				continue
			}
			skipUntilResume = false
		}

		state.CurrentBucket = bucketPath
		marker := ""
		if bucketPath == resumeBucket {
			marker = resumeMarker
		}

		for {
			// Back off while the node is busy.
			if !s.waitForLowLoad(ctx, state) {
				return // context cancelled
			}

			result, err := s.objectManager.ListObjects(ctx, bucketPath, "", "", marker, encWorkerPageSize)
			if err != nil {
				logrus.WithError(err).WithField("bucket", bucketPath).
					Error("Encryption worker: failed to list objects")
				state.LastError = err.Error()
				break
			}

			for _, obj := range result.Objects {
				if ctx.Err() != nil {
					s.saveEncryptionWorkerState(ctx, state)
					return
				}
				converted, _, cErr := encryptor.EncryptExistingObject(ctx, bucketPath, obj.Key)
				if cErr != nil {
					state.Failed++
					state.LastError = cErr.Error()
					logrus.WithError(cErr).WithFields(logrus.Fields{
						"bucket": bucketPath, "key": obj.Key,
					}).Error("Encryption worker: conversion failed")
					continue
				}
				if converted > 0 {
					state.Converted += int64(converted)
				} else {
					state.Skipped++
				}
				// Gentle pacing between objects (same spirit as the scrubber).
				time.Sleep(10 * time.Millisecond)
			}

			// Checkpoint after each page.
			state.Marker = result.NextMarker
			s.saveEncryptionWorkerState(ctx, state)

			if !result.IsTruncated || result.NextMarker == "" {
				break
			}
			marker = result.NextMarker
		}

		state.BucketsDone = i + 1
		state.Marker = ""
		s.saveEncryptionWorkerState(ctx, state)
	}

	state.Status = "done"
	state.CurrentBucket = ""
	state.Marker = ""
	state.LastRunEnd = time.Now().Unix()
	s.saveEncryptionWorkerState(ctx, state)

	logrus.WithFields(logrus.Fields{
		"duration":  time.Since(started).String(),
		"converted": state.Converted,
		"skipped":   state.Skipped,
		"failed":    state.Failed,
	}).Info("Encryption worker: pass complete")
}

// waitForLowLoad blocks until CPU and memory are under the thresholds.
// Returns false only when the context is cancelled.
func (s *Server) waitForLowLoad(ctx context.Context, state *encryptionWorkerState) bool {
	for {
		if s.systemMetrics == nil {
			return true
		}
		cpuOK := true
		if cpu, err := s.systemMetrics.GetCPUUsage(); err == nil && cpu > encWorkerMaxCPU {
			cpuOK = false
		}
		memOK := true
		if mem, err := s.systemMetrics.GetMemoryUsage(); err == nil && mem.UsedPercent > encWorkerMaxMemory {
			memOK = false
		}
		if cpuOK && memOK {
			if state.Status == "waiting_load" {
				state.Status = "running"
				s.saveEncryptionWorkerState(ctx, state)
			}
			return true
		}

		if state.Status != "waiting_load" {
			state.Status = "waiting_load"
			s.saveEncryptionWorkerState(ctx, state)
			logrus.Debug("Encryption worker: node busy, backing off")
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(encWorkerLoadWait):
		}
	}
}

// loadEncryptionWorkerState reads the persisted state (fresh zero state when absent).
func (s *Server) loadEncryptionWorkerState(ctx context.Context) *encryptionWorkerState {
	state := &encryptionWorkerState{Status: "idle"}
	kv, ok := s.metadataStore.(metadata.RawKVStore)
	if !ok {
		return state
	}
	data, err := kv.GetRaw(ctx, encWorkerStateKey)
	if err != nil {
		return state
	}
	if err := json.Unmarshal(data, state); err != nil {
		return &encryptionWorkerState{Status: "idle"}
	}
	return state
}

// saveEncryptionWorkerState persists the state/checkpoint.
func (s *Server) saveEncryptionWorkerState(ctx context.Context, state *encryptionWorkerState) {
	kv, ok := s.metadataStore.(metadata.RawKVStore)
	if !ok {
		return
	}
	state.UpdatedAt = time.Now().Unix()
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	if err := kv.PutRaw(ctx, encWorkerStateKey, data); err != nil {
		logrus.WithError(err).Debug("Encryption worker: failed to persist state")
	}
}

// handleEncryptionWorkerStatus returns the worker's persisted progress.
// GET /api/v1/settings/encryption/worker-status  (global admin only)
func (s *Server) handleEncryptionWorkerStatus(w http.ResponseWriter, r *http.Request) {
	if user := s.requireGlobalAdmin(w, r); user == nil {
		return
	}
	s.writeJSON(w, s.loadEncryptionWorkerState(r.Context()))
}

// handleEncryptionWorkerRun starts a pass immediately (instead of waiting for
// the daily re-scan) — e.g. right after upgrading an existing deployment.
// POST /api/v1/settings/encryption/worker-run  (global admin only)
func (s *Server) handleEncryptionWorkerRun(w http.ResponseWriter, r *http.Request) {
	user := s.requireGlobalAdmin(w, r)
	if user == nil {
		return
	}
	if s.encWorkerRunning.Load() {
		s.writeJSON(w, map[string]interface{}{"started": false, "reason": "already running"})
		return
	}

	logrus.WithField("user", user.Username).Info("Encryption worker: manual pass requested")
	bg := s.serverCtx
	if bg == nil {
		bg = context.Background()
	}
	go s.runEncryptionPass(bg)
	s.writeJSON(w, map[string]interface{}{"started": true})
}
