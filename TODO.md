# MaxIOFS - Development Roadmap

**Version**: 1.4.0
**Last Updated**: May 3, 2026
**Status**: Stable

> Completed work is in [CHANGELOG.md](CHANGELOG.md). This file tracks only pending work.

## 📊 Project Status

| Metric | Value | Notes |
|--------|-------|-------|
| S3 Core API | ~99% | Full compatibility audit completed — 20 issues identified and resolved (March 2026) |
| Backend Tests | 3,850+ | At practical ceiling — see details below |
| Frontend Tests | 95+ | |
| Production Ready | ✅ Stable | v1.4.0 released May 3, 2026 |

### Backend Test Coverage Reality

| Module | Coverage | Notes |
|--------|----------|-------|
| internal/metadata | 87.4% | Remaining ~13% are Pebble internal error branches (WAL failures, I/O errors) — not simulable in unit tests |
| internal/object | 73.6% | Remaining gaps: `NewManager` init (42.9%), `GetObject` encryption/range branches (45.5%), multipart helpers `stagePlaintextToTemp`/`storeUnencryptedMultipartObject`/`calculateMultipartHash` (0% — not exercisable without real encryption pipeline) |
| cmd/maxiofs | 71.4% | `main()` is 0% (entrypoint, expected), `runServer` at 87.5% |
| internal/server | 66.1% | `Start/startAPIServer/startConsoleServer/Shutdown` are 0% (HTTP server lifecycle, not unit-testable). Cluster handlers (30-55%) require real remote nodes |
| internal/replication | 67.8% | CRUD, worker, credentials, adapter, sync, scheduler all tested. Remaining: `e2e_test` integration flows, `s3client` remote calls requiring live S3 endpoint |

**Conclusion**: All testable business logic is covered. Remaining uncovered code falls into categories that cannot be meaningfully unit-tested: server lifecycle, remote node communication, live S3 endpoints, and encryption pipeline internals.

---

## 🔴 v1.4.1 — Audit Findings (May 17, 2026)

Findings from a full code audit performed on v1.4.0. Items are sorted by severity and are independent — each can be fixed and shipped individually. The BadgerDB removal (deprecation of legacy migration code) is also scheduled for this release.

---

### 🔴 Critical — General Backend

#### A1. Replication queue race condition — duplicate work
- **File**: `internal/replication/manager.go:528-598` (`loadPendingItems`)
- **Issue**: when the in-memory queue is full, a claimed item is released back to `pending`. The next loader tick re-claims and re-enqueues it, causing the same object to be replicated twice to the destination.
- **Fix**: instead of reverting to `pending` when the queue is full, keep the item as `retrying` and retry the enqueue on the next tick. Never release an active claim back to `pending`.
- [x] Done

#### A2. Orphaned combined file on `CompleteMultipartUpload` metadata failure
- **File**: `internal/object/manager.go:2065-2075` (`doCompleteMultipartUpload`)
- **Issue**: if `PutObjectVersion()` or `PutObject()` fails after the parts have been physically combined, the combined file remains on disk indefinitely with no metadata record — an unrecoverable storage leak. (Quota is incremented only inside `updateMetricsAndCleanupMultipart` which is called after the metadata write, so quota is not double-counted, but the file still wastes disk space.)
- **Fix**: delete `objectPath` via `storage.Delete` before returning the error when either metadata write fails.
- [x] Done

---

### 🟠 High — General Backend

#### A3. Quota decrement applied even when metadata delete fails
- **File**: `internal/object/manager.go:830-889` (`deleteSpecificVersion`)
- **Issue**: `storage.Delete` errors were swallowed (Warn-only); the physical file could remain on disk while bucket size metrics were still decremented as if it were gone, making reported bucket size smaller than actual disk usage. (Note: `DeleteObjectVersion` is called first; if it fails the function returns before touching the file or metrics — the bug is in the physical-delete failure path, not vice versa.)
- **Fix**: introduce `physicalDeleteOK` flag; pass `freedBytes = 0` to `DecrementObjectCount` when the physical delete failed so the bucket size counter is not decremented until the orphaned file is cleaned up.
- [x] Done

#### A4. TOCTOU in `DeleteBucket` — orphaned objects
- **File**: `internal/bucket/manager_impl.go:172-180`
- **Issue**: between `isBucketEmpty()` and the actual delete, another thread can call `PutObject`. The bucket is deleted leaving objects with metadata but no parent bucket.
- **Fix**: move the empty check inside the store's delete transaction — check + delete as a single atomic operation.
- [x] Done

#### A5. Broken atomicity in `FilesystemBackend.Put()` — data without metadata
- **File**: `internal/storage/filesystem.go:144-159`
- **Issue**: the data file is renamed first, then the metadata file. If the metadata rename fails (disk full, permission change), the object exists on disk but reads return `ErrObjectNotFound`. Silent corruption.
- **Fix**: prepare metadata to a temp file, rename metadata first, then rename data. Or use a commit marker.
- [x] Done

#### A6. Lifecycle without pagination — objects never expired in large buckets
- **File**: `internal/lifecycle/worker.go:284`
- **Issue**: `processObjectExpiration()` calls `ListObjects(maxKeys=10000)` without pagination. Buckets with more than 10,000 objects only expire the first 10,000. Objects beyond the first page are never cleaned up.
- **Fix**: implement a pagination loop with marker until `IsTruncated=false`.
- [x] Done

#### A7. Replication credentials decrypted without validation
- **File**: `internal/replication/manager.go:228-266`
- **Issue**: if the encryption key changes or is corrupted, the decryption result is garbage but is used anyway. Replication fails silently with no alert to the operator.
- **Fix**: validate the decrypt result (non-empty, minimum length). Return `ErrDecryptionFailed` and log at `Error` level if validation fails.
- [x] Done

#### A8. Race in visible object count in `updateBucketMetricsAfterPut`
- **File**: `internal/object/manager.go:2645-2705`
- **Issue**: the fallback that re-queries `GetObjectVersions()` when `existingObjBeforeSave` is nil runs after the save; the previous state may have changed, producing incorrect bucket metrics.
- **Fix**: always capture the previous state before the save and pass it explicitly. Remove the fallback path.
- [x] Done

#### A9. `computeMultipartETag` silently accepts invalid part ETags
- **File**: `internal/object/manager.go:2798-2819`
- **Issue**: if a part ETag is not valid hex, it hashes the string instead of returning an error. Masks data corruption and violates S3 compliance.
- **Fix**: return an error if a part ETag is not a valid 32-character MD5 hex string.
- [x] Done

#### A10. HA replica quota not updated locally
- **File**: `internal/cluster/ha_object_manager.go`
- **Issue**: the primary increments quota locally; replicas store bytes without updating their local quota. If a replica becomes primary, its reported quota is lower than actual stored bytes.
- **Fix**: replicas must update quota metrics (but must NOT enforce the quota limit — that is the primary's responsibility).
- [x] Done

#### A11. Deleting a delete marker in lifecycle may create another marker
- **File**: `internal/lifecycle/worker.go:218-232`
- **Issue**: if `DeleteObject()` with the versionID of a delete marker creates another marker instead of removing it, lifecycle cleanup becomes an infinite loop. No test covers this semantic.
- **Fix**: verify that deleting a delete marker removes it permanently. Add an idempotency test.
- [x] Done

#### A12. `CreateBucket` rollback does not revert the ACL
- **File**: `internal/bucket/manager_impl.go:112-125`
- **Issue**: if the storage marker creation fails, bucket metadata is rolled back but the ACL already created is not. The bucket is left in an inconsistent state with an orphaned ACL.
- **Fix**: if the storage marker fails, also delete the ACL created in the same rollback step.
- [x] Done

---

### 🟡 Medium — General Backend

#### A13. Decryption goroutine not cancelled when reader is abandoned
- **File**: `internal/object/manager.go:417-445`
- **Issue**: if the client disconnects mid-stream, the decryption goroutine continues consuming CPU until the end of the object.
- **Fix**: use `context.WithCancel` and cancel explicitly when the pipe reader is closed without consuming the full stream.
- [x] Done (already fixed in code — inner goroutine watches ctx.Done() and calls pipeWriter.CloseWithError; io.Pipe mechanism also unblocks on pipeReader.Close())

#### A14. Lifecycle prefix filter not validated or applied consistently
- **File**: `internal/lifecycle/worker.go:115-193`
- **Issue**: `rule.Filter.Prefix` is not validated before use and is not applied consistently between noncurrent and current version cleanup paths.
- **Fix**: validate the prefix is non-nil at the start of processing; apply the same filter across all lifecycle action paths.
- [x] Done

#### A15. `CompleteMultipartUpload` leaves `ContentType` empty
- **File**: `internal/object/manager.go:2048`
- **Issue**: the final object's `ContentType` is taken from `multipart.Metadata["content-type"]`. If absent, the object is stored without a Content-Type instead of defaulting to `application/octet-stream`.
- **Fix**: `if contentType == "" { contentType = "application/octet-stream" }`.
- [x] Done

#### A16. Panic if S3 client factory returns nil in replication worker
- **File**: `internal/replication/worker.go`
- **Issue**: without a nil check before using the client, any construction error causes a panic in the worker.
- **Fix**: add nil check after building the S3 client; return a specific error if construction fails.
- [x] Done

#### A17. Audit log failures silenced
- **File**: `internal/bucket/manager_impl.go:52` and others
- **Issue**: audit logging failures are discarded with `_ = auditManager.LogEvent(...)`. In compliance environments this is a correctness issue.
- **Fix**: log audit failures at `Warn` level without blocking the main operation.
- [x] Done

---

### 🔴 Critical — HA Subsystem

#### H1. Quorum factor=2 off-by-one — no replica confirmation required
- **File**: `internal/cluster/ha_object_manager.go:196`
- **Issue**: `neededReplicas = ceil(2/2) - 1 = 0`. With factor=2, a write can succeed on the local node alone with no error to the client. The best-effort behavior is not documented as intentional in the code.
- **Fix**: explicitly document that factor=2 is best-effort, or change the formula to `(factor+1)/2` without subtracting 1 to require at least one replica confirmation with factor=2.
- **Resolution**: documented as intentional — requiring 1 peer confirmation with only 2 nodes would block all writes whenever the single peer is unreachable, which is worse than best-effort. Added a detailed comment with quorum table in `replicaTargets`.
- [x] Done

#### H2. Race in stale mode detection (offline vs partition)
- **File**: `internal/cluster/stale_reconciler.go:174-187`
- **Issue**: `detectMode` reads `last_local_write_at` without a lock while writes may be in progress. A node can be classified as offline when it was actually in a partition, causing its local writes to be skipped during reconciliation.
- **Fix**: use a query with FOR UPDATE, or add a sequence number to define the write window before reconciliation begins.
- [x] Done

#### H3. No retry when all peers are unreachable in stale reconciler
- **File**: `internal/cluster/stale_reconciler.go:121-124`
- **Issue**: if no peer responds when `Reconcile` runs, it returns nil without clearing the `is_stale` flag. The node stays stale indefinitely and never rejoins the write path without manual intervention.
- **Fix**: return an error and implement a bounded retry loop with exponential backoff.
- [x] Done

#### H4. `countNonDeadNodes` is not atomic — last-survivor protection violation
- **File**: `internal/cluster/dead_node_reconciler.go:318-325`
- **Issue**: two threads can both pass the last-survivor check simultaneously and both mark their respective nodes as dead, leaving the cluster below the replication factor.
- **Fix**: wrap count + check + update in a single serializable SQLite transaction.
- [x] Done

#### H5. Proxy HMAC does not include the request body
- **File**: `internal/cluster/proxy.go:240`
- **Issue**: the HMAC payload only covers method+path+timestamp+nonce. An attacker with internal network access can intercept a PUT, replace the body, and reuse the same signature. Enables arbitrary data writes to replicas.
- **Fix**: include `SHA256(body)` in the HMAC payload: `"METHOD\nPATH\nTIMESTAMP\nNONCE\nBODY_HASH"`.
- [x] Done

---

### 🟠 High — HA Subsystem

#### H6. Metadata fanout is fire-and-forget with no confirmation
- **File**: `internal/cluster/ha_object_manager.go:362-412`
- **Issue**: tagging, ACL, retention, and legal hold are fanned out to replicas without waiting for an ACK. An immediate read on another replica may return stale metadata.
- **Fix**: add a confirmation queue for metadata ops, or at minimum a timeout + retry with a divergence log entry.
- [x] Done

#### H7. `countNonDeadNodes` counts UNAVAILABLE nodes as alive
- **File**: `internal/cluster/dead_node_reconciler.go:321`
- **Issue**: the query counts all nodes where `health_status != dead`, including UNAVAILABLE and DEGRADED. There may be fewer useful nodes than the check indicates, blocking the last-survivor protection when the reconciler should be allowed to act.
- **Fix**: count only nodes with `health_status = healthy` for last-survivor protection.
- [x] Done

---

### 🟡 Medium — HA Subsystem

#### H8. Anti-entropy TOCTOU — object deleted between checksum-batch and pull
- **File**: `internal/cluster/anti_entropy.go:680-684`
- **Issue**: if the peer deletes an object between the checksum batch and the pull, the scrubber returns nil silently. The same divergence is re-detected on every subsequent cycle without ever being resolved.
- **Fix**: on 404 during the pull, record as "divergence resolved remotely" and continue; do not silently swallow it.
- [x] Done

#### H9. LWW tie-breaking with equal timestamps and different ETags — never resolved
- **File**: `internal/cluster/anti_entropy.go:488-496`
- **Issue**: `classifyDivergence` returns `actNone` when local and peer have the same `LastModified` but different ETags. The divergence is detected but never fixed.
- **Fix**: use a deterministic secondary key (e.g., lexicographic node ID) to break ties and decide which version wins.
- [x] Done

#### H10. SSE event emitted even when node was already dead (0 rows affected)
- **File**: `internal/cluster/dead_node_reconciler.go:287`
- **Issue**: the UPDATE includes `AND health_status != dead` but does not check `RowsAffected`. If the node was already dead, the UPDATE is a no-op but the SSE transition event is still emitted.
- **Fix**: check `RowsAffected > 0` before emitting the transition event.
- [x] Done

#### H11. Storage pressure SSE callback called inline inside health check
- **File**: `internal/cluster/health.go:146-164`
- **Issue**: if the callback blocks or panics, the health check goroutine hangs.
- **Fix**: emit events through a buffered channel so the health check never blocks on external callbacks.
- [x] Done

#### H12. `EntityIsNewerThanTombstone` check is not transactional
- **File**: `internal/cluster/stale_reconciler.go:385-389`
- **Issue**: between the check and `RecordDeletion`, another thread can delete the entity locally. The check passes but the entity no longer exists, leaving state inconsistent.
- **Fix**: use a delete-only-if-exists pattern with the verification inside the same transaction.
- [ ] Deferred — requires adding `*sql.Tx` variants of EntityIsNewerThanTombstone and RecordDeletion; impact is limited to a one-cycle delay before the next sync resolves the state. Will revisit in v1.4.2.

#### H13. HASyncWorker can run two concurrent syncs for the same node
- **File**: `internal/cluster/ha_sync_worker.go:132-137`
- **Issue**: the lock is released before checking the DB for an existing job. Two threads can both pass the check and run parallel syncs for the same node.
- **Fix**: hold the lock through the DB existence check.
- [x] Done

#### H14. HMAC timestamp window of ±5 minutes is too wide
- **File**: `internal/cluster/proxy.go:348-358`
- **Issue**: an attacker on the internal network can capture and replay a signed request for up to 5 minutes.
- **Fix**: reduce to 30–60 seconds for inter-node internal traffic.
- [x] Done

---

### 🌐 New Languages — Frontend i18n

The i18n system uses `react-i18next` with 20 namespaces. Currently supports English (`en`) and Spanish (`es`). Add French, German, Italian, and Portuguese.

#### I1. Create translation files — French (`fr`)
- Copy all 20 files from `locales/en/` to `locales/fr/` and translate.
- Approx. ~2,200 lines of strings.
- [x] Done

#### I2. Create translation files — German (`de`)
- Copy all 20 files from `locales/en/` to `locales/de/` and translate.
- [x] Done

#### I3. Create translation files — Italian (`it`)
- Copy all 20 files from `locales/en/` to `locales/it/` and translate.
- [x] Done

#### I4. Create translation files — Portuguese (`pt`)
- Copy all 20 files from `locales/en/` to `locales/pt/` and translate.
- [x] Done

#### I5. Register the 4 new languages in `i18n.ts`
- **File**: `web/frontend/src/i18n.ts`
- Add imports for 19 namespaces × 4 languages.
- Add 4 new blocks to `resources: {}`.
- Extend the `getSavedLanguage()` type guard to `'en' | 'es' | 'fr' | 'de' | 'it' | 'pt'`.
- [x] Done

#### I6. Add the 4 languages to the language selector in Preferences
- **File**: `web/frontend/src/components/preferences/UserPreferences.tsx`, `TopBar.tsx`, `AppLayout.tsx`, `LanguageContext.tsx`, `useAuth.ts`
- Added DE 🇩🇪 and PT 🇧🇷 options alongside FR 🇫🇷 and IT 🇮🇹.
- Added `languageGerman` / `languagePortuguese` / `german` / `portuguese` keys to all 6 locale files.
- [x] Done

---

### 🗑️ Technical Debt — BadgerDB Removal

#### D1. Delete BadgerDB files from `internal/metadata/`
- Delete: `badger.go`, `badger_objects.go`, `badger_rawkv.go`, `badger_multipart.go`, `badger_test.go`, `badger_comprehensive_test.go`
- Edit `store_consistency_test.go`: remove the BadgerStore variant from the `setupStoreVariants` loop.
- Edit `pebble_test.go`: remove `TestMigrateFromBadger`, `TestMigrateIdempotent`, and the `badger "github.com/dgraph-io/badger/v4"` import.
- Edit `server.go`: remove the call to `MigrateFromBadgerIfNeeded` and the stale BadgerDB comment at line ~661.
- Note: **do NOT touch** `internal/metrics/badger_history.go` — it does not import BadgerDB; it uses `metadata.RawKVStore` (Pebble).
- [ ] Open

#### D2. Remove `github.com/dgraph-io/badger/v4` from `go.mod`
- After D1, run `go mod tidy` to drop the direct dependency and its transitives (ristretto, etc.).
- [ ] Open

---

## 🔵 v1.3.0 — HA Cluster: Durability fixes + operations

**Goal**: close the gaps in the existing N-way replication so the cluster actually tolerates node failures without silent data loss, and add the operational primitives (dead-node redistribution, drain, storage pressure) needed to run it in production.

**Decision (April 20, 2026)**: the previous v1.3.0 plan ("N copies of every object") was implemented partially via `HAObjectManager` but the critical guarantees were never closed — `fanoutPut` returns 200 to the client based on the local write alone, `collectAndLog` only logs quorum misses, there is no read fallback, and no anti-entropy. v1.3.0 now ships those fixes on the existing replication model. Erasure coding moves to v1.4.0 as a separate, larger effort to address the disk-overhead problem (3× with `factor=3`).

---

### Reality check — what is already built

`internal/cluster/ha_object_manager.go` exists and wraps `object.Manager`. PUT/DELETE/CompleteMultipart fan out to `factor-1` healthy nodes. Metadata-only operations (tagging, ACL, retention, legal hold, restore status) fan out via `POST /api/internal/ha/metadata-op`. `HASyncWorker` resumes initial sync jobs from Pebble checkpoints. Read load balancing exists in `manager.go:SelectReadNode`.

What is **missing** vs. the original v1.3.0 plan:

| Original item | Current state | Gap |
|---|---|---|
| C — Write quorum | `collectAndLog` only logs (`ha_object_manager.go:251`) | Client gets 200 with only local write done; if local node dies before fanout, data is lost silently |
| F — Read fallback | `SelectReadNode` returns one node, no retry | If the chosen replica errors, the client sees the error even though the object exists elsewhere |
| G — Stale catch-up | Only runs on factor-change/new-node | A node that was down comes back stale forever until next factor change |
| (new) Anti-entropy | Not implemented | Bit rot and silent drift accumulate undetected |
| (new) Dead-node redistribution | ✅ Implemented (`dead_node_reconciler.go`) | — |

---

### Work items

#### 1. Write quorum — make it actually synchronous ✅ (April 20, 2026)

`fanoutPut`/`fanoutDelete` are now synchronous. Quorum threshold = `ceil(factor/2)` confirmations (local counts as 1, so replica confirmations needed = `ceil(factor/2)-1`).

Behavioral changes:
- New `cluster.ErrClusterDegraded` error, mapped by S3 handler to 503 + `Retry-After: 30` (PutObject, DeleteObject, CompleteMultipartUpload).
- New `Manager.ClusterCanAcceptWrites(ctx)` early-rejects writes when factor>1 and not enough healthy non-local nodes are present (saves a local write+rollback cycle).
- After fanout, `collectAndCheckQuorum` returns `ErrClusterDegraded` when successes < needed; PUT/CompleteMultipartUpload then roll the local write back via `Manager.DeleteObject` with `WithHARollbackContext(ctx)` so the rollback delete is not itself fanned out.
- DELETE on quorum failure does **not** rollback (delete is a tombstone — anti-entropy item 3 will reconcile); client sees 503 and retries.
- factor=2 special-case: needed replica confirmations = 0, so factor=2 keeps best-effort 2nd-copy semantics. Strict 2-copy is achieved by picking factor=3.

Files touched: `internal/cluster/manager.go` (ErrClusterDegraded, ClusterCanAcceptWrites), `internal/cluster/ha_object_manager.go` (synchronous fanout, rollback context, collectAndCheckQuorum), `pkg/s3compat/handler.go` + `pkg/s3compat/multipart.go` (503 mapping). Tests in `internal/cluster/ha_quorum_test.go`.

#### 2. Read fallback with ordered retry ✅ (April 20, 2026)

New `Manager.SelectReadNodes` returns an ordered list of ready replicas sorted by `latency_ms` asc → `priority` asc → `name`, then rotated by `readCounter % N` to preserve round-robin balance while giving the caller a deterministic retry path. The old `SelectReadNode` is kept as a deprecated thin wrapper.

New `Manager.TryProxyRead(ctx, w, r, node) (served bool, err error)` peeks the replica's response status before writing anything to `w`:
- 2xx / 3xx / non-404 client errors (401/403/412/416) → stream to `w`, `served=true`. Caller stops.
- 404 → close response, `served=false`. Object not synced on this replica yet — try next. Node stays Healthy.
- 5xx or transport failure → close response, `served=false`, node flipped to `Unavailable` via new `markNodeUnavailable` helper.

S3 `GetObject` handler (`pkg/s3compat/handler.go`) now iterates the candidate list and falls through to the local read on full miss. Mid-stream failures (200 then connection death) surface as truncated responses — by then bytes are committed.

Files touched: `internal/cluster/manager.go` (SelectReadNodes, TryProxyRead, markNodeUnavailable), `pkg/s3compat/handler.go` (interface + GetObject loop), `pkg/s3compat/handler_coverage_test.go` (mock). Tests in `internal/cluster/ha_read_test.go` (8 cases: ordering, rotation, factor=1/disabled/no-replicas; 2xx/404/5xx/403/transport-failure for TryProxyRead).

#### 3. Anti-entropy scrubber ✅

Implemented in `internal/cluster/anti_entropy.go` as `AntiEntropyScrubber`. One goroutine per node, scheduler with 5-60 min jittered first run then `ha.scrub_interval_hours` (default 24h) between cycles. Each cycle scans **all** buckets in randomized order so divergences across the entire keyspace are detected within one interval rather than over months.

Per batch (default 500 keys), the scrubber calls `POST /api/internal/ha/checksum-batch` on every healthy peer and reconciles via LWW:
- **Peer missing → push** (re-uses existing `PUT /api/internal/ha/objects/{key}` endpoint with `WithHAReplicaContext`).
- **ETags differ, local newer → push.**
- **ETags differ, peer newer → pull** (GET from peer, local PUT under replica context).
- **Multipart objects** (ETag has `<md5>-N` suffix) skip ETag compare and rely on existence + size + 1s mtime tolerance to avoid expensive whole-file recompute.
- **Same timestamp + different ETag** is logged but not auto-fixed (rare; manual triage).

Throttled to `ha.scrub_rate_limit` (default 50 obj/sec) via `time.Sleep` between compares, no extra dep. Crash-safe checkpoint is JSON-serialized into Pebble at key `ha:scrub:checkpoint` after every batch — a restart resumes the same cycle from the same `(bucket_idx, last_key)`. New `ha_scrub_runs` SQLite table records the last 30 cycles (insert at start, update progress each batch, prune oldest on completion). New global config keys (`ha.scrub_enabled`, `ha.scrub_interval_hours`, `ha.scrub_rate_limit`, `ha.scrub_batch_size`) are seeded with defaults in `cluster_global_config`.

Status surfaces via `GET /cluster/ha/scrub-status` (global admin only): last 10 runs + in-progress checkpoint snapshot.

Files: `internal/cluster/anti_entropy.go` (new), `internal/cluster/sync_schema.go` (table + config defaults), `internal/server/cluster_object_handlers.go` (`handleHAChecksumBatch`), `internal/server/cluster_ha_handlers.go` (`handleGetHAScrubStatus`), `internal/server/server.go` (wire + start + route registration), `internal/server/console_api.go` (status route). Tests in `internal/cluster/anti_entropy_test.go` (20 cases: classifyDivergence push/pull/tie/multipart, multipart ETag detection, checkpoint save/load/delete/JSON round-trip, config defaults vs overrides, runCycle no-ops on disabled/factor=1, ListRecentRuns ordering, pruneRuns retention, urlEscapeBucket).

#### 4. Dead-node redistribution (~3 days) ✅

`HealthStatusDead` is a new terminal state added alongside `unknown / healthy / unavailable`. `cluster_nodes.unavailable_since` (new TIMESTAMP column, applied via idempotent `applyDeadNodeMigration`) records the start of a continuous outage; `markNodeUnavailable` and `CheckNodeHealth` use `COALESCE(unavailable_since, ?)` so the timestamp is preserved across repeated probes and cleared on the first healthy transition. Once the gap exceeds `ha.dead_node_threshold_hours` (default 24h, live-reloadable from `cluster_global_config`), the new `internal/cluster/dead_node_reconciler.go` flips the node to `dead` and calls `HASyncWorker.Trigger()` — because HA replication is symmetric (every healthy node holds every bucket), the existing initial-sync catch-up is the redistribution mechanism, no per-bucket replica reassignment needed. Loop runs every `ha.redistribution_check_interval_minutes` (default 5m) with a 30s jittered first pass and `ticker.Reset` on config change; kill switch is `ha.redistribution_enabled`.

Last-survivor protection: if marking a node dead would drop the count of non-dead nodes below the replication factor, the reconciler refuses and writes the reason into `ha.cluster_degraded_reason`, which is exposed via `GET /cluster/ha/degraded-state` and broadcast over SSE (`cluster_degraded` / `cluster_degraded_resolved` events). Admin short-circuit: `POST /cluster/nodes/{id}/drain` with optional `{"reason"}` body — rejects the local node so the responding server doesn't flip itself to dead mid-call. SSE bridge lives in `internal/server/dead_node_events.go` (decouples cluster from server via `EventEmitter` callback).

Files: `internal/cluster/types.go` (HealthStatusDead + UnavailableSince), `internal/cluster/schema.go` (migration), `internal/cluster/sync_schema.go` (4 new global config keys), `internal/cluster/manager.go` (4 SELECTs + markNodeUnavailable), `internal/cluster/health.go` (transition handling), `internal/cluster/dead_node_reconciler.go` (new), `internal/server/dead_node_events.go` (new SSE bridge), `internal/server/cluster_ha_handlers.go` (drain + degraded-state handlers), `internal/server/server.go` (wiring + Start), `internal/server/console_api.go` (route registration). Tests in `internal/cluster/dead_node_reconciler_test.go` (13 cases: cluster-disabled no-op, kill-switch, mark-dead-past-threshold + sync trigger, skip before threshold, last-survivor protection, degraded-resolved transition, drain success, drain-already-dead, unavailable_since preservation, dead-node skip in markNodeUnavailable, threshold defaults/overrides, check-interval override, ClusterDegradedReason round-trip).

#### 5. Storage-pressure feedback loop (~2 days) ✅

New node-level health state `HealthStatusStoragePressure` lives between `healthy` and `degraded`. The existing health checker (`/health` endpoint already returns `capacity_total` / `capacity_used`) computes `usage% = used/total*100` per probe. Two new live-reloadable global config keys drive the transition: `ha.storage_pressure_threshold_percent` (default 90) flips a healthy node to `storage_pressure`; `ha.storage_pressure_release_percent` (default 85) restores it. Hysteresis is sticky in `CheckNodeHealth`: while in `storage_pressure`, the node only returns to `healthy` once usage drops below release. A misconfiguration where `release ≥ threshold` is auto-clamped to `threshold-5` so the loop is never disabled.

Read vs write split: writes use `GetHealthyNodes` (strict `=healthy` filter), so `replicaTargets` and the dead-node reconciler's non-dead count both naturally exclude SP nodes from new-write target selection. Reads via `GetReadyReplicaNodes` were extended to `IN (healthy, storage_pressure)` — SP nodes still hold valid data and must keep serving reads. SP transitions never override `dead`, `unavailable`, or `degraded` (high latency); the branch only runs for reachable, low-latency nodes.

SSE: new events `node_storage_pressure` and `node_storage_pressure_resolved` carry `usage_percent` + `threshold_percent`. Wired via `cluster.StoragePressureEmitter` callback set by `Manager.SetStoragePressureEmitter`, with the SSE bridge in `internal/server/storage_pressure_events.go` (mirrors `dead_node_events.go`, decoupling cluster from server). Emission fires only when the transition crosses the SP boundary (alert one-shot on entry, resolved one-shot on exit).

Files: `internal/cluster/types.go` (HealthStatusStoragePressure constant), `internal/cluster/sync_schema.go` (2 config defaults), `internal/cluster/manager.go` (emitter field/setter, StoragePressureEvent type, GetReadyReplicaNodes filter), `internal/cluster/health.go` (loadStoragePressureThresholds + CheckNodeHealth state machine + emit), `internal/server/storage_pressure_events.go` (new SSE bridge), `internal/server/server.go` (wires emitter). Tests in `internal/cluster/storage_pressure_test.go` (10 cases: threshold defaults/overrides, inverted-config clamp, cross-threshold flip + emit, hysteresis sticky between release and threshold, resolved emission, dead-node skip, unreachable skip, GetReadyReplicaNodes includes SP, GetHealthyNodes excludes SP).

#### 6. Frontend — HA admin page polish (~2 days) ✅

`HealthBadge` in `HA.tsx` now renders all six node states (`healthy`, `storage_pressure`, `degraded`, `unavailable`, `dead`, `unknown`) with distinct colors and Lucide icons (Gauge for storage_pressure, Skull for dead). Backend status is authoritative — the existing 80%-usage row tint is kept only as a quick visual hint, but the badge itself follows what the cluster reports.

Three new admin surfaces wired through React Query polling:
- **Cluster degraded banner** (red, top of page, polled every 10 s) reads `GET /cluster/ha/degraded-state` and shows the reason set by the dead-node reconciler when last-survivor protection refuses to mark a node dead. Falls back to a generic message when the backend reason is empty.
- **Drain control per node** in the storage table: a `PowerOff` button that calls `POST /cluster/nodes/{id}/drain` after a confirm modal. Disabled with tooltips for the local node (the backend would otherwise flip the responding server to dead mid-call) and for already-dead nodes. Success toast + invalidates the HA, sync-jobs, and degraded-state queries.
- **Anti-entropy scrubber section** (new `ScrubberSection` component): when `current` is non-null, shows progress bar (`current_bucket_idx/total`), objects compared, divergences found, divergences fixed, buckets scanned. When idle, shows last completed run with status, completed-at, and the same metrics. Polled every 15 s via `GET /cluster/ha/scrub-status`.

Backend addition: `/cluster/ha` now returns `local_node_id` so the frontend can identify the local row without a second round-trip. API client (`api.ts`) extended with `getClusterDegradedState`, `getHAScrubStatus`, `drainClusterNode`, plus `dead` and `storage_pressure` added to the health-status type. i18n keys (en + es) for all new surfaces (`statusDead`, `statusStoragePressure`, `clusterDegradedTitle`, `drainNode*`, `scrubber*`).

Files: `web/frontend/src/pages/cluster/HA.tsx`, `web/frontend/src/lib/api.ts`, `web/frontend/src/locales/{en,es}/cluster.json`, `internal/server/cluster_ha_handlers.go` (`local_node_id` in response). Verified clean: `go build ./...`, `npx tsc -b`, JSON parse of both locale files, full cluster Go suite (151 s) green.

---

### Estimated effort
- Total: ~2.5 weeks focused engineering
- Critical path: 1 (write quorum) → 2 (read fallback) → 3 (anti-entropy)
- 4, 5, 6 can ship in parallel after item 3

### Consistency model (unchanged)
- AP (availability over consistency): write succeeds if quorum is reached, even if some replicas lag.
- Strict CP ruled out — write latency becomes unacceptable with any network hiccup.

### Upgrade path
- Single-node: no change. `factor` defaults to 1.
- Existing cluster on `factor > 1`: behavior changes for writes — they now block on quorum. Latency for PUT goes up by one inter-node RTT. Operators should be informed in the release notes.

---

## 🟣 v1.4.0 — Erasure Coding (replace N-way replication for large objects)

**Goal**: cut disk overhead from `N×` (N-way replication) to `~1.5×` while preserving the same failure tolerance. Today a 1 GB object with `factor=3` consumes 3 GB cluster-wide; with EC `4+2` it consumes 1.5 GB and tolerates the same 2 node failures.

**Decision (April 20, 2026)**: erasure coding deserves its own release. It changes the on-disk layout, the metadata schema, and the read/write paths. v1.3.0 must ship first to give us the durability primitives (quorum, read fallback, anti-entropy) that EC depends on — without them, EC just multiplies the existing data-loss windows across more shards.

---

### Storage model

Reed-Solomon `K + M`:
- `K` data shards: the object split into K equal parts.
- `M` parity shards: computed from the K data shards.
- Object reconstructible from **any K of the K+M shards**.
- Tolerates loss of `M` nodes simultaneously.
- Disk overhead: `(K+M)/K`.

| Scheme | Nodes needed | Overhead | Tolerates |
|--------|--------------|----------|-----------|
| `4+2` | 6 | 1.5× | 2 nodes |
| `6+3` | 9 | 1.5× | 3 nodes |
| `8+4` | 12 | 1.5× | 4 nodes |

For comparison, current `factor=3` replication is 3× overhead and tolerates 2 nodes — EC `4+2` is the same tolerance at half the disk cost.

**Hybrid model**: small objects (< `ec.min_object_size`, default 1 MB) keep using N-way replication. Reed-Solomon has fixed per-object overhead (shard headers, metadata) that dominates for small files. MinIO does the same.

---

### Work items

#### 1. EC config + library integration (~3 days)

- New cluster global config: `ec.enabled`, `ec.data_shards` (K, default 4), `ec.parity_shards` (M, default 2), `ec.min_object_size` (default 1 MB).
- Validate at config-set time: `K + M ≤ healthy_node_count`.
- Add dependency: `github.com/klauspost/reedsolomon` (the canonical Go EC library, well-maintained, used by MinIO, SeaweedFS, etc.).
- Files: `internal/cluster/sync_schema.go`, `internal/cluster/manager.go`, `go.mod`.

#### 2. EC writer (~1 week)

New module `internal/storage/ec/writer.go`:

- Buffer the input stream into chunks (configurable, default 4 MB per stripe).
- For each stripe: split into K data shards, compute M parity shards via reedsolomon library, send each shard to a different cluster node in parallel.
- Same quorum semantics as v1.3.0 item 1: client gets 200 only when all `K+M` shards are written. Tolerate up to `M` failures (we still have K to reconstruct), but mark failed nodes `stale` for repair.
- Anti-loop: same `X-MaxIOFS-HA-Replica` header pattern.

Edge cases:
- Object size not a multiple of stripe size: pad the last stripe, store the original size in metadata so reads truncate correctly.
- Multipart upload: each part goes through its own EC encoding. Part metadata records the shard layout per part.
- Concurrent writes to the same key: same versioning rules as today, but each version is its own EC layout.

Files: `internal/storage/ec/writer.go` (new), `internal/object/manager.go` (route to EC for `size >= ec.min_object_size`), `internal/server/cluster_object_handlers.go` (shard receiver endpoint).

#### 3. EC reader (~1 week)

New module `internal/storage/ec/reader.go`:

- Read object metadata to learn the shard layout `[(NodeID, ShardIdx, Size)]`.
- Request K shards in parallel (try data shards first; fall back to parity shards if any data shard node is unavailable).
- Reconstruct the original stream via `reedsolomon.Reconstruct`.
- Streaming: produce output as soon as the first K shards arrive — don't buffer the whole object.

Edge cases:
- More than M nodes down → object unrecoverable, return 503 with which shards are missing (admin needs to know what to repair).
- Partial shard corruption (checksum mismatch on a shard): treat that shard as missing, fall back to another.
- Range requests: compute which stripes are needed, fetch only those shards. Saves bandwidth on large objects with small ranges.

Files: `internal/storage/ec/reader.go` (new), `internal/object/manager.go`.

#### 4. EC metadata in Pebble (~3 days)

Extend object metadata to store EC layout. New fields on `metadata.Object`:

```go
EncodingType  string  // "replication" | "ec"
ECDataShards  int     // K
ECParityShards int    // M
ECStripeSize  int     // bytes per stripe
ECShards      []ECShardLocation  // per-shard: NodeID, ShardIdx, Checksum
```

Existing replicated objects keep `EncodingType = "replication"` and the new fields stay zero-valued. Reader picks the path based on `EncodingType`.

Files: `internal/metadata/types.go`, `internal/metadata/pebble_objects.go`, `internal/object/adapter.go`.

#### 5. EC-aware anti-entropy and repair (~3 days)

Extend the v1.3.0 scrubber to also check shard health:

- For EC objects, check each shard's existence and checksum on its assigned node.
- If a shard is missing or corrupted: read K healthy shards, reconstruct the missing/bad one, write it to a healthy node (the original or a new one if the original is dead).
- If `M` shards are missing simultaneously, the object is on the edge of unrecoverable — escalate to a critical SSE alert immediately.

Files: `internal/cluster/anti_entropy.go`.

#### 6. Migration: replication → EC (~1 week)

Background worker that converts existing replicated objects (size ≥ `ec.min_object_size`) to EC layout:

- Reads the object once from any replica.
- Writes new EC shards to K+M nodes via the new EC writer.
- Updates Pebble metadata atomically (`EncodingType` flips from `replication` → `ec`, `ECShards` populated).
- Deletes the old replica copies only after the EC layout is verified readable.
- Crash-safe: checkpoint last-migrated key in Pebble.
- Throttled and pausable from the admin UI.

Reverse migration (EC → replication) supported for the same case the user wants to roll back. Same worker, opposite direction.

Files: `internal/cluster/ec_migration_worker.go` (new), `internal/server/cluster_ha_handlers.go`.

#### 7. Frontend — EC controls (~3 days)

`web/frontend/src/pages/cluster/HA.tsx`:

- New section "Storage encoding" with Replication / Erasure Coding toggle.
- K and M sliders, with live disk-overhead and tolerance preview.
- Migration progress bar (per-bucket: how many objects migrated).
- Per-object inspector: show shard layout for debugging.

---

### Estimated effort
- Total: ~4 weeks focused engineering
- Critical path: 1 → 2+3 (writer/reader in parallel) → 4 → 5
- 6 (migration) and 7 (UI) ship after the core path is stable

### Consistency model
- Same as v1.3.0: AP, quorum-based.
- EC writes require all K+M shards to be acked or the write fails — there is no "EC quorum" partial-write mode (you cannot reconstruct without K shards, period).

### Upgrade path
- v1.4.0 ships with `ec.enabled = false` by default. Existing deployments behave like v1.3.0.
- Admin enables EC → migration worker starts converting objects in background. Cluster stays operational throughout.
- Rollback: set `ec.enabled = false` and run reverse migration.

---

## 🔵 v1.3.0 — Cluster improvements: event-driven config sync

#### 8. Event-driven config sync -- eliminate polling lag between nodes (~1 week)

**Problem today**: all sync managers (tenant, user, access key, bucket permission, IDP, group mapping) use a polling timer (default 30s). If a user is created on node A, node B rejects their requests for up to 30 seconds.

With HA quorum write this is critical: a client can be routed to any node at any time, so a 30s auth blackout after any config change is unacceptable.

**Solution**: every write to auth/config data immediately fans out to all healthy nodes in background before returning 200. The polling loop stays as a reconciliation safety net for nodes that were down.

Changes per operation:
- `POST/PUT/DELETE /users/{id}` -> `UserSyncManager.SyncUserNow(ctx, userID)` -- **highest priority** for S3 access keys
- `POST/PUT/DELETE /tenants/{id}` -> `TenantSyncManager.SyncTenantNow`
- `POST/DELETE /access-keys/{id}` -> `AccessKeySyncManager.SyncKeyNow` -- S3 auth breaks without this
- Bucket permission changes -> `BucketPermissionSyncManager.SyncPermissionNow`
- IDP provider changes -> `IDPProviderSyncManager.SyncProviderNow`
- Group mapping changes -> `GroupMappingSyncManager.SyncMappingNow`

Each `SyncXNow` fans out in parallel goroutines, logs failures as warnings, does NOT block the original request. Polling interval raised from 30s to 5 minutes once event-driven sync is in place.

Files: all 6 `*_sync.go` files (add `SyncXNow`), all handler files that mutate users/tenants/keys/permissions/IDPs/mappings.

---

## 📝 References

- Changelog: [CHANGELOG.md](CHANGELOG.md)
- API Documentation: [docs/API.md](docs/API.md)
- Cluster Guide: [docs/CLUSTER.md](docs/CLUSTER.md)
- Performance: [docs/PERFORMANCE.md](docs/PERFORMANCE.md)
