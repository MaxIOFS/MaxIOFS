# MaxIOFS - Development Roadmap

**Version**: 1.2.0
**Last Updated**: April 3, 2026
**Status**: Stable

> Completed work is in [CHANGELOG.md](CHANGELOG.md). This file tracks only pending work.

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
| internal/server | 66.1% | `Start/startAPIServer/startConsoleServer/Shutdown` are 0% (HTTP server lifecycle, not unit-testable). Cluster handlers (30-55%) require real remote nodes |
| internal/replication | 67.8% | CRUD, worker, credentials, adapter, sync, scheduler all tested. Remaining: `e2e_test` integration flows, `s3client` remote calls requiring live S3 endpoint |

**Conclusion**: All testable business logic is covered. Remaining uncovered code falls into categories that cannot be meaningfully unit-tested: server lifecycle, remote node communication, live S3 endpoints, and encryption pipeline internals.

---

## 🔵 v1.3.0 — HA Cluster: Quorum Write + Synchronous Replication

**Goal**: if a node goes down, all buckets remain accessible and writable. Today any bucket whose primary node dies becomes unavailable.

**Decision (April 5, 2026)**: cluster-level replication factor. Every object is written to N nodes simultaneously (synchronous quorum). No erasure coding — each replica node holds a complete copy of every object. Factor is defined once for the entire cluster and can be changed at any time.

---

### Storage model

The replication factor is a **single cluster-wide setting**, not per-bucket or per-node.

| Factor | Copies | Usable (3×500 GB cluster) | Tolerates |
|--------|--------|---------------------------|-----------|
| 1 | 1 | 1,500 GB | 0 nodes |
| 2 | 2 | 750 GB | 1 node |
| 3 | 3 | 500 GB | 2 nodes |

With 5 nodes and factor 3: data lives on 3 of the 5 nodes — cluster tolerates 2 simultaneous failures.

**Quota is always counted once** regardless of factor — replicas are invisible to quota calculations.

---

### Core architecture

**Factor is cluster-wide.** One setting, stored in cluster global config. All buckets on all nodes replicate according to this factor. Buckets have no replication configuration of their own.

**Nodes always join empty.** A new node entering the cluster must have no data. This prevents data inconsistency. After joining, the cluster distributes objects and metadata to the new node in background.

**Factor change is always validated first.** Before applying a new factor, the system calculates whether all target nodes have enough free space to hold the full dataset. If not, the change is rejected with a clear error — production is never affected.

**Space recalculation on node add/remove.** When the number of nodes changes, the cluster recalculates usable capacity and (if factor > 1) redistributes data to include or exclude the changed node.

**Bucket appears exactly once.** Each bucket has a `PrimaryNodeID` (the node where it was created) — internal tracking only. The `BucketAggregator` shows a bucket only from its primary. Replica nodes hold data silently. No duplicate listings, no duplicate quota.

**Any node can serve reads.** Once a replica reaches `ready` status it serves reads directly without proxy. The router distributes reads round-robin across primary + ready replicas. Reads for objects not yet synced fall back to a node that has them.

**Any node can accept writes.** The receiving node fans out to all replica nodes in parallel. Header `X-MaxIOFS-HA-Replica: true` prevents re-fanout (anti-loop). Quorum = `ceil(factor/2)` confirmations required before returning 200 to client. Partial failure marks failed nodes `stale`. Full quorum failure returns 503.

**Initial sync is visible and resumable.** When factor increases or a new node joins, background sync starts. Admin sees per-node progress in the cluster HA page. Cluster stays fully operational during sync. Progress checkpointed in Pebble — restart resumes from last checkpoint.

---

### Internal tracking data structures (not user-configurable)

```go
// BucketMetadata in internal/metadata/types.go — internal tracking only
type BucketHA struct {
    PrimaryNodeID string          // node that owns this bucket's listing and quota
    ReplicaNodes  []HAReplicaNode // current state of each replica
}

type HAReplicaNode struct {
    NodeID   string
    Status   string    // "syncing" | "ready" | "stale" | "pending_removal" | "storage_pressure"
    Progress int       // 0-100, only meaningful during "syncing"
    SyncedAt time.Time
}
```

The cluster replication factor is stored in cluster global config (key `ha.replication_factor`), not in bucket metadata.

---

### What already exists (do NOT re-implement)
- `ProxyClient` — sends authenticated HTTP requests to any cluster node (`internal/cluster/proxy.go`)
- `stale_reconciler.go` — reconciles diverged state when a node rejoins
- `GetGlobalConfig` / `SetGlobalConfig` — cluster-wide key/value store (`internal/cluster/sync_schema.go`)
- Circuit breaker, health checks, inter-node TLS
- `PUT /api/internal/cluster/objects/` — already streams objects between nodes (used by migration)

---

### Work items

#### ~~1. Remove cluster-level async replication~~ ✅ Done

#### A. Cluster HA types + configuration API (~2 days)

Internal tracking structs (already added to codebase):
- `internal/metadata/types.go` — `BucketHA` / `HAReplicaNode` structs + `HA *BucketHA` on `BucketMetadata` ✅
- `internal/bucket/interface.go` — `HA *metadata.BucketHA` on `Bucket` struct + status constants ✅
- `internal/bucket/adapter.go` — wire `HA` through `toMetadataBucket` / `fromMetadataBucket` ✅
- `internal/server/console_api.go` `handleCreateBucket` — assign `PrimaryNodeID = localNodeID` on creation ✅

Cluster-level HA config API (to implement):
- `GET /cluster/ha` — return current factor, usable capacity, per-node status
- `PUT /cluster/ha` — change factor (1/2/3); validate space on all nodes first, reject with clear error if insufficient; kick off background sync if factor increases
- `internal/cluster/manager.go` — `GetReplicationFactor()`, `SetReplicationFactor()` using `GetGlobalConfig`/`SetGlobalConfig`

Node join validation:
- `internal/cluster/manager.go` `JoinCluster()` — reject if joining node has any existing objects or metadata (must be empty)

#### B. Space validation before factor change (~1 day)

Before applying `PUT /cluster/ha`:
- Sum total data size across all buckets on primary nodes
- For each node that will receive new replicas: verify `freeSpace >= totalDataSize × 1.2` (20% headroom)
- Reject with HTTP 400 and per-node detail if any node fails the check
- If a write would push any replica over 90% disk: flag that node `storage_pressure` + SSE alert to admin
- Files: `internal/cluster/manager.go`, `internal/cluster/health.go`

#### C. Write fanout — PutObject / DeleteObject / CompleteMultipart (~2 weeks)

In `internal/object/manager.go`:
- After writing locally, read cluster replication factor and `bucket.HA.ReplicaNodes` filtered to `ready` + `syncing`
- Fan out in parallel via new `ProxyClient.FanoutWrite(ctx, nodes, req)` method
- Add header `X-MaxIOFS-HA-Replica: true` to all fanout requests
- Receiving node detects header: writes locally, does NOT fan out further (anti-loop)
- Quorum: wait for `ceil(factor/2)` confirmations. Partial failure: mark failed nodes `stale`. Full quorum failure: return 503
- Files: `internal/object/manager.go`, `internal/cluster/proxy.go`, `internal/api/handler.go`

#### D. Metadata fanout — Pebble sync (~1 week)

- New internal endpoint `POST /api/internal/ha/metadata/sync` — accepts `[]{ key, value }`, writes to local Pebble (cluster node token auth)
- After every `PutObject` / `DeleteObject` local Pebble write, fan out metadata batch to ready replicas in background goroutine (non-blocking)
- Files: `internal/metadata/pebble_store.go`, `internal/server/console_api.go`, `internal/cluster/proxy.go`

#### E. Initial sync worker (~2 days)

- Background goroutine triggered when factor increases or a new node joins
- Iterates all objects across all buckets, streams each to new replica nodes via `PUT /api/internal/cluster/objects/`
- Updates `HAReplicaNode.Progress` in bucket metadata as it progresses
- On completion: sets replica status to `ready` — router starts using it for reads immediately
- Crash-safe: checkpoints last synced object key in Pebble — restart resumes from checkpoint
- Files: `internal/server/ha_sync_worker.go` (new), `internal/server/server.go`

#### F. Read load balancing across replicas (~1 day)

- `Router.RouteRequest()`: if cluster factor > 1 and bucket has ready replicas, select node round-robin
- If object not yet present on selected replica (metadata check): fall back transparently to a node that has it
- Files: `internal/cluster/router.go`

#### G. Stale replica catch-up on node rejoin (~2 days)

- Extend `stale_reconciler.go`: on node rejoin, find all buckets where this node is a `stale` replica
- Re-trigger sync worker (item E) for missing/outdated objects only (delta sync, not full resync)
- Use `deletion_log.go` to replay deletes that happened during downtime
- On catch-up complete: replica status → `ready`
- Files: `internal/cluster/stale_reconciler.go`

#### H. Bucket aggregator — no duplicates, no double quota (~1 day)

- `internal/cluster/bucket_aggregator.go`: skip buckets where local node is NOT the `PrimaryNodeID`
- Quota aggregation: only sum bucket size from primary node — replicas do not contribute
- Files: `internal/cluster/bucket_aggregator.go`

#### I. Cluster HA admin page (~2 days)

Frontend: `web/frontend/src/pages/cluster/HA.tsx` — new page at `/cluster/ha`
- Current cluster replication factor display with selector (1 / 2 / 3)
- Live usable capacity estimate based on factor + cluster total storage
- Nodes that would be tolerated to fail with current factor
- Space validation warning before confirming a factor change
- Per-node sync status table: node name, status badge (ready/syncing/stale/storage_pressure), progress bar during sync, last synced timestamp
- Confirmation dialog for any factor change (especially downgrade)
- StoragePressure warning banner when any node exceeds 80% disk
- Link from cluster Overview page

---

### Two replication systems — how they coexist

| System | Config | Mode | Failover | Purpose |
|--------|--------|------|----------|---------|
| Bucket-level replication (bucket settings) | Per-bucket | Async | Manual | Replicate to external S3 (AWS, MinIO, another MaxIOFS) |
| HA Quorum Write (this feature) | Cluster-wide factor | Synchronous | Automatic | All buckets on N nodes, transparent failover |

---

### Consistency model
- AP (availability over consistency): write succeeds if quorum is reached, even if some replicas lag.
- Strict CP ruled out — write latency becomes unacceptable with any network hiccup.

### Upgrade path
- Single-node: no change. Factor defaults to 1. Behavior identical to today.
- Existing cluster: admin sets factor > 1. System validates space, then syncs all existing data in background. Cluster stays operational throughout.

### Estimated effort
- Total: 4–5 weeks focused engineering
- Critical path: A → C → D (types + config API, then write fanout, then metadata fanout)
- B, E, F, G, H, I can ship incrementally after C is working

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
