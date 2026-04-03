# MaxIOFS - Development Roadmap

**Version**: 1.2.0
**Last Updated**: April 2, 2026
**Status**: Stable

## 📊 Project Status

| Metric | Value | Notes |
|--------|-------|-------|
| S3 Core API | ~99% | Full compatibility audit completed — 20 issues identified and resolved (March 2026) |
| Backend Tests | 3,850+ | At practical ceiling — see details below |
| Frontend Tests | 95+ | |
| Production Ready | ✅ Stable | v1.2.0 released April 2, 2026 |

### Backend Test Coverage Reality

| Module | Coverage | Notes |
|--------|----------|-------|
| internal/metadata | 87.4% | Remaining ~13% are Pebble internal error branches (WAL failures, I/O errors) — not simulable in unit tests |
| internal/object | 73.6% | Remaining gaps: `NewManager` init (42.9%), `GetObject` encryption/range branches (45.5%), multipart helpers `stagePlaintextToTemp`/`storeUnencryptedMultipartObject`/`calculateMultipartHash` (0% — not exercisable without real encryption pipeline) |
| cmd/maxiofs | 71.4% | `main()` is 0% (entrypoint, expected), `runServer` at 87.5% |
| internal/server | 66.1% | `Start/startAPIServer/startConsoleServer/Shutdown` are 0% (HTTP server lifecycle, not unit-testable). Cluster handlers (30–55%) require real remote nodes |
| internal/replication | 67.8% | CRUD, worker, credentials, adapter, sync, scheduler all tested. Remaining: `e2e_test` integration flows, `s3client` remote calls requiring live S3 endpoint |

**Conclusion**: All testable business logic is covered. Remaining uncovered code falls into categories that cannot be meaningfully unit-tested: server lifecycle, remote node communication, live S3 endpoints, and encryption pipeline internals.

---

## 🚧 S3 API Completeness — Pending Features

Identified gaps vs the full S3 API spec, ordered by practical impact.
Handler pattern: register in `internal/api/handler.go`, implement in `pkg/s3compat/`.

---

### 🔴 High Priority — Real Functional Gaps

#### ~~1. `RestoreObject` — `POST /{bucket}/{object}?restore`~~ ✅ Done
Implemented. Parses `<RestoreRequest>` XML, returns 409 if restore already in progress, marks
`RestoreStatus="restored"` with `RestoreExpiresAt = now + Days` in object metadata, and returns 200.
`HeadObject`/`GetObject` return `x-amz-restore: ongoing-request="false", expiry-date="..."` when set.
(`internal/api/handler.go`, `pkg/s3compat/handler.go`, `internal/object/manager.go`,
`internal/metadata/types.go`, `internal/object/adapter.go`)

---

#### ~~2. `OwnershipControls` — `GET/PUT/DELETE /{bucket}?ownershipControls`~~ ✅ Done
Implemented. GET returns `BucketOwnerEnforced` as default when no config is set; PUT validates and
persists the `ObjectOwnership` value (`BucketOwnerEnforced`, `BucketOwnerPreferred`, `ObjectWriter`);
DELETE clears the config. Fixes AWS SDK v2 `OwnershipControlsNotFoundError` on bucket creation and ACL
uploads.
(`internal/api/handler.go`, `pkg/s3compat/bucket_ops.go`, `internal/bucket/manager_impl.go`,
`internal/bucket/interface.go`, `internal/bucket/types.go`, `internal/metadata/types.go`)

---

#### ~~3. Checksum support (`x-amz-checksum-*`)~~ ✅ Already implemented
Validation, storage, and response headers are fully implemented in `internal/object/manager.go:439-549`
and `pkg/s3compat/handler.go`. Supports CRC32, CRC32C, SHA1, SHA256. No action needed.

---

### 🟡 Medium Priority — Analytics & Advanced Features

#### ~~4. `SelectObjectContent` — `POST /{bucket}/{object}?select&select-type=2`~~ ✅ Done
Implemented. Loads CSV or JSON Lines input into an in-memory SQLite database, executes the SQL
expression (full SQL: SELECT, WHERE, GROUP BY, ORDER BY, aggregate functions), and streams results
back using the Amazon Event Stream binary protocol (Records → Stats → End events with CRC32
checksums). Supports CSV and JSON Lines input; CSV and JSON output; custom field delimiters;
FileHeaderInfo (USE/NONE/IGNORE). Batch-flushes every 1 000 rows.
(`pkg/s3compat/select.go`, `internal/api/handler.go`)

#### ~~5. `BucketInventory` — `GET/PUT/DELETE /{bucket}?inventory&id=`, `GET /{bucket}?inventory`~~ ✅ Done
Implemented. The existing inventory execution engine (`internal/inventory/`) was console-only.
Added `GetConfigByID`, `ListConfigsByBucket`, `UpsertConfigByID`, `DeleteConfigByID` methods to
`inventory.Manager`. New `pkg/s3compat/inventory.go` provides all four S3-compatible handlers with
full XML wire format (InventoryConfiguration, Destination.S3BucketDestination, Schedule, OptionalFields,
IncludedObjectVersions). Routes registered in `internal/api/handler.go` with id-specific routes before
the list route to ensure correct gorilla/mux matching.
(`pkg/s3compat/inventory.go`, `internal/inventory/manager.go`, `internal/api/handler.go`, `pkg/s3compat/handler.go`)

---

### 🟢 Low Priority — Stub Completeness

- ~~**Bucket Notifications** (`?notification`)~~ ✅ Done — real webhook delivery implemented.
- ~~**Bucket Logging** (`?logging`)~~ ✅ Done — real log delivery to target bucket implemented.
- ~~**Transfer Acceleration** (`?accelerate`)~~ ✅ Closed as no-op — GET returns empty `<AccelerateConfiguration/>`, PUT accepts and discards config. Meaningless for self-hosted deployments with no CDN edge nodes. Clients that configure it see no difference in behavior.
- ~~**GetObjectTorrent** (`GET /{bucket}/{object}?torrent`)~~ ✅ Closed as not implemented — returns `501 NotImplemented`. BitTorrent manifests have no practical use case in self-hosted object storage.

---

## ✅ Completed

- [x] `docs/OPERATIONS.md` — production operations runbook
- [x] Docker multi-arch images (trixie-slim, Go 1.26, wget, runuser, no-cache)
- [x] Fix `cleanupEmptyDirectories` path bug
- [x] Fix stale "account locked" notification on fresh installations
- [x] Test coverage: `internal/replication` 19% → 67.8% (worker, credentials, adapter, sync, scheduler)
- [x] Test coverage: `internal/object` integrity tests (VerifyObjectIntegrity, VerifyBucketIntegrity)
- [x] S3 `RestoreObject` — `POST /{bucket}/{object}?restore` with metadata tracking and `x-amz-restore` response header
- [x] S3 `OwnershipControls` — `GET/PUT/DELETE /{bucket}?ownershipControls` with `BucketOwnerEnforced` default
- [x] S3 `SelectObjectContent` — `POST /{bucket}/{object}?select` with SQLite in-memory SQL engine, event-stream protocol
- [x] S3 `BucketInventory` — `GET/PUT/DELETE /{bucket}?inventory&id=` + `GET /{bucket}?inventory`, full S3 XML wire format, multiple configs per bucket
- [x] S3 `BucketNotifications` — `GET/PUT/DELETE /{bucket}?notification` with real async webhook delivery, event filtering (prefix/suffix), SSRF protection
- [x] S3 `BucketLogging` — `GET/PUT /{bucket}?logging` with real async log delivery to target bucket in AWS S3 access log format, 100-entry/5-min flush
- [x] S3 `Transfer Acceleration` — closed as documented no-op (GET/PUT return 200, no actual acceleration)
- [x] S3 `GetObjectTorrent` — closed as `501 NotImplemented`

---

---

## 🔵 v1.3.0 — Quorum Write / HA Cluster (Synchronous Replication)

**Goal**: if a node goes down, buckets on that node remain accessible. Today the bucket is simply unavailable if its primary node dies.

**Decision taken (April 3, 2026)**: implement synchronous quorum writes (N replicas, majority must confirm before returning 200 to the client). Erasure coding ruled out — too large a rewrite, insufficient ROI at current scale.

### What already exists (do NOT re-implement)
- `Router.GetBucketReplicas()` — resolves replica nodes for a bucket via replication rules (`internal/cluster/router.go:78`)
- `Router.GetHealthyNodeForBucket()` — already falls back to replicas on reads if primary is down (`internal/cluster/router.go:123`)
- `ProxyClient` — already sends authenticated HTTP requests to any cluster node (`internal/cluster/proxy.go`)
- `stale_reconciler.go` — already reconciles diverged state when a node rejoins
- Circuit breaker, health checks, inter-node TLS — all working

### What needs to be built

#### 1. Write fanout on PutObject / DeleteObject / CompleteMultipart (~2 weeks)
- In `internal/object/manager.go` (`PutObject`, `deletePermanently`, `CompleteMultipartUpload`): after writing locally, fan out the same write to all configured replica nodes in parallel via `ProxyClient`.
- Wait for `ceil(N/2)` confirmations (quorum). If quorum is reached but some replicas failed, mark them as stale (the reconciler handles catch-up). If quorum cannot be reached, return error to client.
- Introduce a `WriteQuorum` field in the cluster node/bucket config (default: majority). Allow `1` (async, current behavior) as opt-out.
- Files to modify: `internal/object/manager.go`, `internal/cluster/router.go`, `internal/cluster/proxy.go`

#### 2. Metadata fanout (~1 week)
- Pebble is per-node today. On `PutObject`/`DeleteObject`, after writing metadata locally, replicate the metadata write to replica nodes via a new internal cluster endpoint `POST /api/internal/metadata/sync`.
- The endpoint accepts a batch of key-value pairs and applies them to the local Pebble store directly (bypassing S3 auth — use existing cluster node token auth).
- Files: `internal/metadata/pebble_store.go`, `internal/server/console_api.go` (register internal route), `internal/cluster/proxy.go`

#### 3. Replica configuration per bucket (~3 days)
- Add `ReplicaCount int` and `ReplicaNodes []string` to bucket metadata (`internal/metadata/types.go`, `internal/bucket/types.go`).
- Console API: expose `PUT /buckets/{bucket}/replicas` to configure replicas; cluster manager auto-selects healthiest nodes if `ReplicaNodes` is empty.
- Frontend: add "Replication" tab to bucket settings showing replica count, which nodes hold replicas, and health status per replica.
- Files: `internal/metadata/types.go`, `internal/bucket/`, `internal/server/console_api.go`, `web/frontend/src/pages/buckets/[bucket]/settings.tsx`

#### 4. Stale replica catch-up on rejoin (~2 days)
- Extend `stale_reconciler.go`: when a node rejoins after downtime, compare its object list against the primary and pull missing/outdated objects.
- Use existing `deletion_log.go` to replay deletes that happened while the node was down.
- The reconciler already has the skeleton — needs the "pull missing objects" direction added (currently only pushes).

#### 5. Read load balancing (~1 day)
- `Router.RouteRequest()` currently always goes to primary. With replicas available and healthy, distribute reads round-robin across primary + replicas.
- Useful for Veeam verify jobs which do heavy read bursts.

### The three replication systems — how they coexist

MaxIOFS has three distinct replication mechanisms, each serving a different purpose. None replaces the others.

| System | Config location | Mode | Failover | Purpose |
|--------|----------------|------|----------|---------|
| **Bucket-level replication** (bucket settings UI) | Per-bucket settings | Async | Manual | Replicate to external S3 (AWS, MinIO, another MaxIOFS). Like S3 CRR. |
| **Cluster replication** (`cluster_bucket_replication` table) | Cluster admin UI | Async, queued, with retries | Manual (admin re-points clients) | Replicate between nodes of the same cluster. DR copy — if node A dies, admin can point clients to node B manually. |
| **HA Quorum Write** (this feature, v1.3.0) | Per-bucket, "Replicas" setting | Synchronous | **Automatic, transparent** | Same bucket lives on N nodes. Write confirmed only after quorum. Client never sees the failure. |

**Cluster replication (existing) is NOT a mirror or HA system.** It is a scheduled async copy: `source_bucket` on the local node → `destination_bucket` on `destination_node_id`, every `sync_interval_seconds`. If the source node dies, the destination bucket exists independently but there is no automatic failover. The admin must manually redirect clients.

**Quorum Write HA adds automatic failover** on top. The cluster replication remains useful as a secondary DR layer or for intentional named copies between nodes.

### Storage math — usable capacity with N nodes
With replication factor R across N nodes of size S each:
- Raw capacity = N × S
- Usable capacity = (N × S) / R

Example — 3 nodes × 500 GB:

| Replication factor | Usable space | Node failures tolerated |
|--------------------|-------------|------------------------|
| 1 (no HA) | 1 500 GB | 0 |
| 2 (recommended) | 750 GB | 1 |
| 3 (max HA) | 500 GB | 2 |

Factor 2 is the recommended default: 50% storage efficiency, tolerates 1 node failure, quorum = 2/3 nodes must confirm each write.

### Consistency model decision
- **Prefer availability** (AP): write succeeds if quorum is reached, even if some replicas are behind. Correct for Veeam backup workloads.
- Do **not** implement strict CP (all-or-nothing across all nodes) — write latency becomes unacceptable with >2 nodes and any network hiccup.

### Upgrade path for existing deployments
- Existing single-node deployments: no change, `ReplicaCount = 1` is the default, behavior identical to today.
- Existing cluster deployments: opt-in per bucket. No automatic migration of data — enabling replicas on a bucket triggers an initial sync from primary to replica nodes.

#### 6. Storage-aware replica placement (~2 days)
- For HA to hold, a replica node must have enough free space to store a full copy of the bucket.
- **Nodes do not need to be identical**, but the system must refuse to place a replica on a node that cannot hold the data.
- When selecting replica nodes (item 3 above), the cluster manager must:
  1. Query each candidate node for available disk space via the existing health/metrics endpoint.
  2. Compare against the current bucket size (`BucketStats.TotalSize`).
  3. Reject nodes where `freeSpace < bucketSize * 1.2` (20% headroom).
  4. Emit a warning in the console if a replica node's free space falls below 1.2x the source bucket size after initial placement (ongoing monitoring).
- Add `AvailableBytes` and `TotalBytes` to the node health payload (`internal/cluster/health.go`) — already reports disk metrics via Prometheus, just needs to be included in the cluster node info struct (`internal/cluster/types.go`).
- **Write path constraint**: if a write would cause a replica node to exceed 90% disk usage, the write is still allowed (quorum wins) but the replica is flagged as `StoragePressure` and the console shows an alert to the operator.
- Files: `internal/cluster/health.go`, `internal/cluster/types.go`, `internal/cluster/manager.go` (replica selection logic)

#### 7. Remove cluster-level async replication (`cluster_bucket_replication`) — same milestone
The existing `ClusterReplicationManager` / `ClusterReplicationWorker` system is incomplete (the handler writes directly to SQLite bypassing the manager — there is a `// TODO: Use ClusterReplicationManager.CreateReplicationRule() when integrated` comment in `cluster_replication_handlers.go`). It never delivered automatic failover. The HA quorum write system covers its intended use case properly.

**Delete in v1.3.0, not before** (do it alongside the HA implementation so there is no gap where neither system works):
- `internal/cluster/replication_manager.go`, `replication_worker.go`, `replication_schema.go`, `replication_types.go`
- `internal/server/cluster_replication_handlers.go`
- 4 routes in `internal/server/console_api.go` (`/cluster/replication`, `/cluster/replication/{id}`, `/cluster/replication/bulk`)
- `clusterReplicationMgr` field and wiring in `internal/server/server.go`
- `web/frontend/src/pages/cluster/BucketReplication.tsx`
- 5 API functions + types in `web/frontend/src/lib/api.ts` and `src/types/index.ts`
- Tests: `replication_manager_test.go`, `replication_worker_test.go`, `replication_integration_test.go`
- **Do NOT delete the DB migration** — migrations already applied to production databases cannot be removed. The tables become unused but harmless.

### Estimated effort
- Total: **4–5 weeks** focused engineering
- Items 1+2 are the critical path; items 3–7 can ship incrementally
- Item 6 (storage-aware placement) must be done before item 3 goes to production — placing replicas blindly without storage checks defeats the HA guarantee
- Item 7 (removal) should be the first commit of the milestone so the codebase is clean before new code is added

---

## 📝 References

- Changelog: [CHANGELOG.md](CHANGELOG.md)
- API Documentation: [docs/API.md](docs/API.md)
- Cluster Guide: [docs/CLUSTER.md](docs/CLUSTER.md)
- Performance: [docs/PERFORMANCE.md](docs/PERFORMANCE.md)
