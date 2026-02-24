# MaxIOFS - Development Roadmap

**Version**: 0.9.2-beta
**Last Updated**: February 24, 2026
**Status**: Beta - S3 Core 100% Compatible

## 📊 Project Status

| Metric | Value | Notes |
|--------|-------|-------|
| S3 Core API | 100% | All standard S3 operations |
| Backend Coverage | ~75% | At practical ceiling — see details below |
| Frontend Coverage | 100% | Complete |
| Production Ready | Testing | Target: Q4 2026 |

### Backend Test Coverage Reality (February 7, 2026)

| Module | Coverage | Notes |
|--------|----------|-------|
| internal/metadata | 87.4% | Remaining ~13% are Pebble internal error branches (WAL failures, I/O errors) — not simulable in unit tests |
| internal/object | 77.3% | Remaining gaps: `NewManager` init (47.8%), `GetObject` encryption/range branches (53.7%), `cleanupEmptyDirectories` (34.6%), `deleteSpecificVersion` blocked by Windows file-locking bug |
| cmd/maxiofs | 71.4% | `main()` is 0% (entrypoint, expected), `runServer` at 87.5% |
| internal/server | 66.1% | `Start/startAPIServer/startConsoleServer/shutdown` are 0% (HTTP server lifecycle, not unit-testable). Cluster handlers (30-55%) require real remote nodes. Migration/replication handlers need live infrastructure |
| internal/replication | 19.0% | CRUD rule management tested. `s3client.go`, `worker.go`, `adapter.go` are all 0% — they operate against real remote S3 endpoints and cannot be unit-tested without full network infrastructure |

**Conclusion**: All testable business logic has been covered. The remaining uncovered code falls into categories that cannot be meaningfully unit-tested: server lifecycle, remote node communication, filesystem-level operations, and low-level database error branches. Reaching 90%+ would require integration test infrastructure (multi-node cluster, remote S3 endpoints) which is outside the scope of unit testing.

---

## 🔴 CRITICAL (Security Hardening)

### 1. JWT Signature Verification (CRITICAL) ✅
- [x] `parseBasicToken()` now verifies HMAC-SHA256 signature before trusting payload using `hmac.Equal()` for constant-time comparison
- **File**: `internal/auth/manager.go:1100-1132`
- **Tests**: `TestValidateJWT_ForgedSignature` (4 cases), `TestValidateJWT_TamperedPayload` — all pass

### 2. CORS Wildcard Removal (CRITICAL) ✅
- [x] Replaced hardcoded `Access-Control-Allow-Origin: *` with proper `middleware.CORSWithConfig()` using origin validation
- **File**: `internal/server/console_api.go`
- **Tests**: Existing CORS middleware tests cover origin validation (disallowed origins, wildcards, custom validators)

### 3. Rate Limiting IP Spoofing (CRITICAL) ✅
- [x] `IPKeyExtractor` now only trusts `X-Forwarded-For`/`X-Real-IP` when request comes from a trusted proxy. Added `TrustedProxies`, `stripPort()`, `isTrustedProxy()`.
- **File**: `internal/middleware/ratelimit.go:248-301`
- **Tests**: `TestIPKeyExtractor` (7 cases), `TestStripPort` (7 cases), `TestIsTrustedProxy` (2 cases) — all pass

---

## 🟠 HIGH (Stability & Robustness)

### 4. Default Password Change Notification ✅
- [x] Backend returns `default_password: true` in login response when admin/admin is used
- [x] Frontend shows persistent security warning in notification bell with amber icon, links to user profile
- [x] Warning clears automatically when password is changed via `APIClient.changePassword()`
- **Files**: `internal/server/console_api.go`, `web/frontend/src/lib/api.ts`, `web/frontend/src/components/layout/AppLayout.tsx`

### 5. Goroutine Leak in Decryption Pipeline ✅
- [x] Added context cancellation monitoring — when caller abandons the reader, `ctx.Done()` triggers `pipeWriter.CloseWithError()`, unblocking the goroutine
- **File**: `internal/object/manager.go:318-340`

### 6. Unbounded Map Growth in Replication Manager ✅
- [x] `DeleteRule()` now cleans up `ruleLocks` entry for deleted rules
- [x] `processScheduledRules()` now cleans up `lastSync` entries for rules no longer in the database
- **File**: `internal/replication/manager.go`

### 7. Race Condition in Cluster Cache ✅ (False Positive)
- [x] Verified: all `c.entries` accesses are correctly protected with `sync.RWMutex` — `RLock` for reads, `Lock` for writes
- **File**: `internal/cluster/cache.go` — no changes needed

### 8. Unchecked `crypto/rand.Read` Error ✅
- [x] Added error check — falls back to timestamp-only version ID if `crypto/rand` fails
- **File**: `internal/object/manager.go:207`

### 9. Array Bounds Check in S3 Signature Parsing ✅ (False Positive)
- [x] Verified: all array accesses in `parseS3SignatureV4` and `parseS3SignatureV2` already have proper bounds checks (`len(credParts) >= 2`, `len(kv) != 2`, `len(parts) != 2`)
- **File**: `internal/auth/manager.go:1189-1212` — no changes needed

---

## 🟡 MEDIUM PRIORITY

### Code Quality
- [x] HTTP response body not always closed via `defer` immediately after assignment — Verified: all `resp.Body.Close()` are properly deferred (false positive)
- [x] Audit logging errors silently ignored in 12 locations in `console_api.go` — Added `logAuditEvent()` helper that logs warnings on failure, migrated all 12 call sites
- [x] Temp file handle leak potential in `internal/object/manager.go:368-383` — Added `defer tempFile.Close()` immediately after creation to ensure cleanup on panic
- [x] Tag index deletion error ignored in `internal/metadata/pebble_objects.go` — Now returns error on failed batch delete to prevent inconsistent state
- [x] Path traversal with URL-encoded `%2e%2e%2f` — Verified safe: Go's `net/http` decodes URL-encoded paths before handlers, `strings.Contains(path, "..")` catches decoded traversal, and `filepath.Join` normalizes as defense-in-depth

---

## 🟠 HIGH — Cluster Resilience: Stale Node & Network Partition (v0.9.2-beta)

Two distinct failure scenarios require proper handling. Both involve a node that was isolated
from the cluster for a period and then reconnects. The current sync system has no conflict
resolution and simply overwrites — which causes entity resurrection and data loss.

---

### Scenario A — Node offline (clean shutdown or crash, no client traffic)

The cluster is the sole source of truth. The returning node just needs to pull the current
authoritative state and discard anything that was deleted during its absence.

### Scenario B — Network partition (node isolated but alive, serving clients)

Both sides diverge independently. Neither is authoritative. Requires bidirectional merge with
conflict resolution. This is the harder and more dangerous case.

```
Detection key:
  last_local_write_at ≤ last_seen_at_shutdown  →  Scenario A (offline, no local writes)
  last_local_write_at >  last_seen_at_shutdown  →  Scenario B (partition, had local writes)
```

---

### Phase 1 — Schema & Detection  [x] ✅

**Files**: `internal/cluster/schema.go`

- [x] Column `is_stale BOOLEAN NOT NULL DEFAULT 0` on `cluster_nodes`
- [x] Column `last_local_write_at TIMESTAMP` on `cluster_nodes`
- [x] `applyStaleNodeMigration()` adds both columns to existing databases

---

### Phase 2 — Stale Detection in Health Checker  [x] ✅

**File**: `internal/cluster/health.go`

- [x] `CheckNodeHealth()` marks `is_stale = true` when node reconnects after `> StalenessThreshold` (7 days)
- [x] `checkAndMarkStale()` helper
- [x] `touchLocalWriteAt()` helper in `internal/server/cluster_write_tracking.go`

---

### Phase 3 — LWW (Last-Write-Wins) in all entity upsert handlers  [x] ✅

**File**: `internal/server/cluster_object_handlers.go`, `internal/server/cluster_tenant_handlers.go`

- [x] Tenants — LWW on `updated_at` (TIMESTAMP type)
- [x] Users — LWW on `updated_at` (int64 Unix seconds)
- [x] IDP Providers — LWW on `updated_at` (int64 Unix seconds)
- [x] Group Mappings — LWW on `updated_at` (int64 Unix seconds)
- [x] Access Keys — implicit LWW via `created_at` in stale reconciler snapshot comparison (no `updated_at` column in schema); `INSERT OR REPLACE` only executes for entities absent from the peer
- [x] Bucket Permissions — same as access keys (`granted_at` used as timestamp proxy)

---

### Phase 4 — Tombstone vs Entity timestamp comparison  [x] ✅

**Files**: `internal/cluster/deletion_log.go`, `internal/server/cluster_object_handlers.go`, `internal/server/cluster_tenant_handlers.go`

- [x] `EntityIsNewerThanTombstone()` in `deletion_log.go` — supports Tenant, User, IDPProvider, GroupMapping (full `updated_at`); returns false for AccessKey/BucketPermission (no `updated_at` — tombstone always wins)
- [x] `handleReceiveTenantDeleteSync` — Phase 4 check present
- [x] `handleReceiveUserDeleteSync` — Phase 4 check present
- [x] `handleReceiveIDPProviderDeleteSync` — Phase 4 check present
- [x] `handleReceiveGroupMappingDeleteSync` — Phase 4 check present
- [x] `handleReceiveAccessKeyDeleteSync` — Phase 4 check present (always false; tombstone wins by design)
- [x] `handleReceiveBucketPermissionDeleteSync` — Phase 4 check present (always false; tombstone wins by design)
- [x] `handleReceiveDeletionLogSync` — Phase 4 check in bulk tombstone application loop

---

### Phase 5 — State Snapshot Endpoint  [x] ✅

**Files**: `internal/cluster/snapshot.go`, `internal/server/cluster_snapshot_handler.go`

- [x] `GET /api/internal/cluster/state-snapshot` — HMAC-authenticated, returns `StateSnapshot`
- [x] `BuildLocalSnapshot()` queries all 6 entity types + deletion log
- [x] `fetchRemoteSnapshot()` in `stale_reconciler.go` as the client-side caller

---

### Phase 6 — Stale Reconciler  [x] ✅

**File**: `internal/cluster/stale_reconciler.go`

- [x] `ModeOffline` / `ModePartition` detection via `last_local_write_at`
- [x] `reconcileWithPeer()` — fetches remote snapshot, pushes locally-newer entities (ModePartition), syncs tombstones bidirectionally (both modes)
- [x] `pushNewerEntities()` — all 6 entity types via `newerStamps()` + per-entity push methods
- [x] `pushTombstonesToPeer()` + `applyRemoteTombstones()` — bidirectional tombstone sync
- [x] `clearStaleFlag()` — resets `is_stale=0` and `last_local_write_at=NULL` on completion

---

### Phase 7 — Integration into Server Startup  [x] ✅

**File**: `internal/server/server.go`

- [x] `staleReconciler *cluster.StaleReconciler` field on `Server` struct (line 72)
- [x] `NewStaleReconciler()` called in server initialization (line 351)
- [x] `staleReconciler.Reconcile(ctx)` called at startup when cluster is enabled (line 506)

---

### Phase 8 — Track `last_local_write_at`  [x] ✅

**File**: `internal/server/cluster_write_tracking.go`, `internal/server/console_api.go`, `internal/server/console_idp.go`

- [x] `touchLocalWriteAt(ctx)` helper — updates `last_local_write_at` on local node row
- [x] Called in all 10 entity write handlers in `console_api.go` (createTenant, updateTenant, createUser, updateUser, createAccessKey, grantBucketPermission, revokeBucketPermission, ...)
- [x] Called in all 6 IDP/group-mapping handlers in `console_idp.go` (createIDP, updateIDP, deleteIDP, createGroupMapping, updateGroupMapping, deleteGroupMapping)

---

### Phase 9 — Tests  [x] ✅

**File**: `internal/cluster/stale_reconciler_test.go` — 25 tests, all passing

- [x] `TestNewerStamps` (6 sub-tests) — pure function: absent/newer included, equal/older skipped, mixed batch, empty input
- [x] `TestBuildStampIndex` — index built correctly for all 6 entity types
- [x] `TestBuildLocalSnapshot_Empty` — empty DB produces empty snapshot
- [x] `TestBuildLocalSnapshot_WithEntities` — all 6 entity types + tombstones appear in snapshot with correct timestamps
- [x] `TestEntityIsNewerThanTombstone` (11 sub-tests) — tenant/user/IDP/group-mapping: older=false, newer=true; access-key/bucket-permission: always false; entity-not-found=false
- [x] `TestDetectMode` (2 sub-tests) — NULL last_local_write_at → ModeOffline; set → ModePartition
- [x] `TestApplyRemoteTombstones` (4 sub-tests) — new tombstone recorded; equal/newer local tombstone skipped; entity newer than tombstone skipped (LWW); mixed batch filters correctly
- [x] `TestReconcile_SkipsWhenNotStale` — node not stale → returns nil immediately
- [x] `TestReconcile_SkipsWhenNoPeers` — no peers → stale flag remains set
- [x] `TestReconcile_ClearsStaleFlag` — is_stale=0 and last_local_write_at=NULL after reconciliation
- [x] `TestReconcile_ModeOffline_FetchesSnapshot` — state-snapshot endpoint called on peer
- [x] `TestReconcile_ModeOffline_DoesNotPushEntities` — no entity sync endpoints called in ModeOffline
- [x] `TestReconcile_ModePartition_PushesLocallyNewerEntities` — user-sync called when local is newer
- [x] `TestReconcile_ModePartition_SkipsRemoteNewerEntities` — no push when remote is strictly newer (LWW)
- [x] `TestReconcile_PushesLocalTombstonesToPeer` — local tombstones sent to peer via deletion-log-sync
- [x] `TestReconcile_AppliesRemoteTombstonesLocally` — remote tombstones recorded in local deletion log

---

### Summary of files to create / modify

| File | Action |
|---|---|
| `internal/cluster/schema.go` | Add `is_stale`, `last_local_write_at` columns |
| `internal/cluster/migration.go` | New migration for the 2 new columns |
| `internal/cluster/health.go` | Detect stale on reconnect, set `is_stale = true` |
| `internal/cluster/manager.go` | `SetNodeStale`, `IsNodeStale`, `UpdateLastLocalWriteAt`, `GetLocalNodeLastLocalWriteAt` |
| `internal/cluster/deletion_log.go` | Tombstone timestamp comparison before applying |
| `internal/cluster/stale_reconciler.go` | **New** — full reconciliation logic |
| `internal/cluster/stale_reconciler_test.go` | **New** — all unit tests |
| `internal/server/cluster_handlers.go` | New `GET /api/internal/cluster/state-snapshot` endpoint |
| `internal/server/server.go` | Startup: check stale → run reconciler before sync managers |
| `internal/server/console_api.go` | All entity handlers call `UpdateLastLocalWriteAt` |

---

## 🟡 PENDING — v0.9.2-beta

### 1. Maintenance Mode Enforcement ✅

- [x] S3 middleware: PUT/POST/DELETE blocked with 503 + XML error; GET/HEAD pass through
- [x] Console API middleware: mutating requests blocked with 503 JSON `MAINTENANCE_MODE`; exempt: `/auth/`, `/health`, `/settings`, `/api/internal/`, `/notifications`
- [x] Frontend amber banner in AppLayout, reactive without page reload via `queryClient.invalidateQueries(['serverConfig'])`
- [x] `handleGetServerConfig` includes `maintenanceMode: bool`
- **Files**: `internal/middleware/maintenance.go`, `internal/server/console_api.go`, `internal/server/server.go`, `web/frontend/src/components/layout/AppLayout.tsx`

---

### 2. Disk Space Threshold Alerts ✅

- [x] Settings: `system.disk_warning_threshold` (80%), `system.disk_critical_threshold` (90%)
- [x] `internal/server/disk_alerts.go`: goroutine every 5 min, `diskAlertState` deduplication
- [x] SSE to global admins + email to all active global admin accounts with email
- [x] SMTP: `internal/email/sender.go`, `email.*` settings category, test email endpoint `POST /settings/email/test`
- [x] Frontend Email tab in Settings with Test Email button
- **Files**: `internal/email/sender.go`, `internal/server/disk_alerts.go`, `internal/settings/manager.go`, `web/frontend/src/pages/settings/index.tsx`

---

### 3. Tenant Quota Warning Notifications ✅

- [x] Callback `SetStorageQuotaAlertCallback` added to auth Manager interface + `authManager` struct
- [x] `IncrementTenantStorage` fires callback asynchronously after every successful increment
- [x] `internal/server/quota_alerts.go`: `quotaAlertTracker` with per-tenant `sync.Map` deduplication
- [x] SSE to tenant admins + global admins; email to both groups
- [x] Tenants with `MaxStorageBytes = 0` (unlimited) skipped
- [x] Frontend: storage bar thresholds aligned to 80% (amber) / 90% (red) with inline label
- **Files**: `internal/auth/manager.go`, `internal/server/quota_alerts.go`, `internal/server/server.go`, `web/frontend/src/pages/tenants/index.tsx`

---

### 4. Object Integrity Verification (MEDIUM)

**Status**: MD5 is computed at write time and stored as ETag in Pebble. Never re-verified after storage.

- [ ] `VerifyObjectIntegrity(ctx, bucketPath, objectKey) error` in `internal/object/manager.go`: reads the object file from disk, computes MD5, compares with stored ETag — returns error on mismatch
- [ ] Background scrubber goroutine (`startIntegrityScrubber`): runs once every 24 hours, iterates all objects via `ListObjects`, calls `VerifyObjectIntegrity` for each, logs corrupted objects as `logrus.Error` and records an audit event (`EventTypeDataCorruption`)
- [ ] New audit event type `EventTypeDataCorruption` with fields: bucket, object key, expected ETag, detected ETag, file path
- [ ] Admin endpoint `POST /buckets/{bucket}/verify-integrity` — triggers an on-demand scan for a specific bucket, returns count of objects checked and list of corrupted objects found
- [ ] Skip objects with empty ETag (delete markers, multipart in-progress)
- [ ] Skip encrypted objects where ETag is of the unencrypted content (verify using `original-etag` from storage metadata)

---

### 5. Operational Documentation (LOW)

**Status**: Technical docs exist (`ARCHITECTURE.md`, `CLUSTER.md`, `SECURITY.md`). No operator runbook.

- [ ] `docs/OPERATIONS.md` — runbook for production operators:
  - What to do when a cluster node goes down
  - How to safely remove a node from the cluster
  - How to recover from a Pebble crash (WAL recovery is automatic, but document the indicators)
  - How to interpret audit logs for security incidents
  - Recommended monitoring alerts for Prometheus/Grafana
  - Disk space management (what to do when approaching capacity)

---

## ✅ COMPLETED

### v0.9.2-beta (February 2026)
- ✅ Maintenance Mode enforcement — S3 + Console API middleware, reactive frontend banner
- ✅ Disk space alerts — SSE + SMTP email to global admins when disk crosses 80%/90%; test email endpoint
- ✅ Tenant quota warnings — SSE + email on 80%/90% threshold crossing; per-tenant deduplication; colored storage bar in UI
- ✅ Replaced BadgerDB with Pebble (CockroachDB's LSM-tree engine) for all S3 object/bucket metadata — crash-safe WAL eliminates MANIFEST corruption on unclean shutdown
- ✅ Transparent auto-migration: `MigrateFromBadgerIfNeeded()` detects `metadata/KEYREGISTRY`, migrates all keys to Pebble in batches, renames directories atomically — no user intervention
- ✅ Decoupled ACL, bucket, object, metrics, notifications from BadgerDB via `metadata.RawKVStore` interface
- ✅ Multipart TTL replaced with hourly cleanup goroutine (Pebble has no native TTL)
- ✅ All test files updated to use `PebbleStore`; migration corrected existing under-reported counters
- ✅ Cluster: `StateSnapshot` endpoint + `StaleReconciler` (LWW conflict resolution on reconnect)
- ✅ Cluster: Write tracking (`last_local_write_at`) for accurate partition detection
- ✅ Cluster: Phase 3 LWW complete for all 6 entity upsert handlers (tenants, users, IDP providers, group mappings use `updated_at`; access keys and bucket permissions use `created_at`/`granted_at` as timestamp proxy in snapshot comparison)
- ✅ Cluster: Phase 4 `EntityIsNewerThanTombstone` check added to all 6 delete handlers — `handleReceiveAccessKeyDeleteSync` and `handleReceiveBucketPermissionDeleteSync` were the last two missing
- ✅ Cluster: Phase 9 stale reconciler tests — 25 tests covering pure functions (newerStamps, buildStampIndex), DB logic (BuildLocalSnapshot, EntityIsNewerThanTombstone, detectMode, applyRemoteTombstones), and full HTTP integration (Reconcile with mock peer server)
- ✅ Bucket metrics under-reported under concurrent load — replaced `UpdateBucketMetrics` OCC retry loop (5 attempts) with per-bucket `sync.Mutex` via `sync.Map`. Resolves VEEAM 4.2 GB stored / 2.21 GB shown discrepancy.
- ✅ `RecalculateBucketStats` tenant prefix fix — was scanning `obj:bucketName:` instead of `obj:tenantID/bucketName:key`, always returning 0 for tenant buckets.
- ✅ Admin endpoint `POST /buckets/{bucket}/recalculate-stats` — full Pebble scan to resync counters on demand.
- ✅ Background stats reconciler — goroutine that runs `RecalculateBucketStats` for all buckets every 15 minutes, 2-minute initial delay, clean shutdown on context cancellation.
- ✅ Frontend dynamic refresh — `refetchInterval: 30000` on dashboard, buckets listing, and bucket detail pages so stats update without navigation.
- ✅ Removed `TestHandleTestLogOutput` — called non-existent `server.handleTestLogOutput`, caused compilation failure.

### v0.9.1-beta (February 2026)
- ✅ Tombstone-based cluster deletion sync — new `cluster_deletion_log` table, `DeletionLogSyncManager`, tombstone checks in all upsert handlers
- ✅ IDP provider and group mapping cluster sync with automatic synchronization
- ✅ Delete-sync endpoints for all 6 entity types (users, tenants, access keys, bucket permissions, IDP providers, group mappings)
- ✅ 36 new cluster sync tests (deletion log, IDP provider sync, group mapping sync)

### v0.8.0-beta (February 2026)
- ✅ Object Filters & Advanced Search (content-type, size range, date range, tags) — new `/objects/search` endpoint + frontend filter panel
- ✅ Version check notification badge for global admins (proxied through backend)
- ✅ Dark mode toggle fixed — now uses ThemeContext, persists to user profile
- ✅ CI/CD test fix — `TestCPUStats_ConsistentData` frequency variance threshold increased for virtualized environments

### v0.7.0-beta (January-February 2026)
- ✅ Bucket Inventory System
- ✅ Database Migration System
- ✅ Performance Profiling & Benchmarking
- ✅ Cluster Production Hardening (rate limiting, circuit breakers, metrics)
- ✅ ListBuckets cross-node aggregation (fixed UX blocker)
- ✅ Cluster-aware quota enforcement (fixed security vulnerability)
- ✅ Backend test coverage expansion — reached practical ceiling (metadata 87.4%, object 77.3%, server 66.1%, cmd 71.4%)

### v0.6.x
- ✅ Cluster Management System
- ✅ Performance Metrics & Dashboards
- ✅ Prometheus/Grafana Integration
- ✅ Bucket Replication

### v0.5.0 and earlier
- ✅ Core S3 Operations (PutObject, GetObject, DeleteObject, ListObjects)
- ✅ Bucket Versioning & Lifecycle Policies
- ✅ Object Lock & Retention (COMPLIANCE/GOVERNANCE)
- ✅ Server-side Encryption (AES-256-CTR)
- ✅ Multi-tenancy with quotas
- ✅ Two-Factor Authentication
- ✅ Bucket Policies, ACLs, CORS, Tags

---

## 📝 References

- Changelog: [CHANGELOG.md](CHANGELOG.md)
- API Documentation: [docs/API.md](docs/API.md)
- Cluster Guide: [docs/CLUSTER.md](docs/CLUSTER.md)
- Performance: [docs/PERFORMANCE.md](docs/PERFORMANCE.md)
