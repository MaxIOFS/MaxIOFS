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
| internal/server | 66.1% | `Start/startAPIServer/startConsoleServer/Shutdown` are 0% (HTTP server lifecycle, not unit-testable). Cluster handlers (30–55%) require real remote nodes |
| internal/replication | 67.8% | CRUD, worker, credentials, adapter, sync, scheduler all tested. Remaining: `e2e_test` integration flows, `s3client` remote calls requiring live S3 endpoint |

**Conclusion**: All testable business logic is covered. Remaining uncovered code falls into categories that cannot be meaningfully unit-tested: server lifecycle, remote node communication, live S3 endpoints, and encryption pipeline internals.

---

## 🔵 v1.3.0 — Quorum Write / HA Cluster (Synchronous Replication)

**Goal**: if a node goes down, buckets on that node remain accessible. Today the bucket is simply unavailable if its primary node dies.

**Decision taken (April 3, 2026)**: implement synchronous quorum writes (N replicas, majority must confirm before returning 200 to the client). Erasure coding ruled out — too large a rewrite, insufficient ROI at current scale.

### What already exists (do NOT re-implement)
- `Router.GetBucketReplicas()` — resolves replica nodes for a bucket (`internal/cluster/router.go:78`)
- `Router.GetHealthyNodeForBucket()` — already falls back to replicas on reads if primary is down (`internal/cluster/router.go:123`)
- `ProxyClient` — already sends authenticated HTTP requests to any cluster node (`internal/cluster/proxy.go`)
- `stale_reconciler.go` — already reconciles diverged state when a node rejoins
- Circuit breaker, health checks, inter-node TLS — all working

### Work items

#### 1. Remove cluster-level async replication — START HERE
The `ClusterReplicationManager` / `ClusterReplicationWorker` system is incomplete and never delivered automatic failover. The HA quorum write covers its intended purpose properly. Nobody is using it.

Files to delete:
- `internal/cluster/replication_manager.go`, `replication_worker.go`, `replication_schema.go`, `replication_types.go`
- `internal/server/cluster_replication_handlers.go`
- 4 routes in `internal/server/console_api.go` (`/cluster/replication`, `/cluster/replication/{id}`, `/cluster/replication/bulk`)
- `clusterReplicationMgr` field and wiring in `internal/server/server.go`
- `web/frontend/src/pages/cluster/BucketReplication.tsx`
- 5 API functions + types in `web/frontend/src/lib/api.ts` and `src/types/index.ts`
- Tests: `replication_manager_test.go`, `replication_worker_test.go`, `replication_integration_test.go`
- **Do NOT delete the DB migration** — tables become unused but harmless; migrations already applied to production cannot be removed.

#### 2. Write fanout on PutObject / DeleteObject / CompleteMultipart (~2 weeks)
- In `internal/object/manager.go`: after writing locally, fan out to all configured replica nodes in parallel via `ProxyClient`.
- Wait for `ceil(N/2)` confirmations (quorum). Quorum reached but some replicas failed → mark stale, reconciler catches up. Quorum unreachable → return error to client.
- Introduce `WriteQuorum` field in bucket config (default: majority). Allow `1` as opt-out (async, current behavior).
- Files: `internal/object/manager.go`, `internal/cluster/router.go`, `internal/cluster/proxy.go`

#### 3. Metadata fanout (~1 week)
- Pebble is per-node today. On `PutObject`/`DeleteObject`, replicate metadata writes to replica nodes via a new internal endpoint `POST /api/internal/metadata/sync`.
- Endpoint accepts a batch of key-value pairs, applies them directly to local Pebble (cluster node token auth, not S3 auth).
- Files: `internal/metadata/pebble_store.go`, `internal/server/console_api.go`, `internal/cluster/proxy.go`

#### 4. Replica configuration per bucket (~3 days)
- Add `ReplicaCount int` and `ReplicaNodes []string` to bucket metadata (`internal/metadata/types.go`, `internal/bucket/types.go`).
- Console API: `PUT /buckets/{bucket}/replicas` — cluster manager auto-selects healthiest nodes if `ReplicaNodes` is empty.
- Frontend: "Replication" tab in bucket settings — replica count, which nodes, health per replica.
- Files: `internal/metadata/types.go`, `internal/bucket/`, `internal/server/console_api.go`, `web/frontend/src/pages/buckets/[bucket]/settings.tsx`

#### 5. Storage-aware replica placement (~2 days)
- Must be done before item 4 goes to production — placing replicas blindly without storage checks defeats the HA guarantee.
- Reject nodes where `freeSpace < bucketSize * 1.2` (20% headroom). Alert operator when a replica node approaches capacity.
- If a write would push a replica node over 90% disk usage: write still allowed (quorum wins) but replica flagged `StoragePressure` with a console alert.
- Add `AvailableBytes` / `TotalBytes` to node health payload (`internal/cluster/health.go`, `internal/cluster/types.go`).
- Nodes do not need to be identical — only sufficient free space matters.
- Files: `internal/cluster/health.go`, `internal/cluster/types.go`, `internal/cluster/manager.go`

#### 6. Stale replica catch-up on rejoin (~2 days)
- Extend `stale_reconciler.go`: when a node rejoins, compare its object list against the primary and pull missing/outdated objects.
- Use existing `deletion_log.go` to replay deletes that happened during downtime.
- The reconciler already has the skeleton — needs the "pull missing objects" direction added (currently only pushes).

#### 7. Read load balancing (~1 day)
- `Router.RouteRequest()` currently always routes to primary. Distribute reads round-robin across primary + healthy replicas.
- Useful for Veeam verify jobs which produce heavy read bursts.

### Storage math

With replication factor R across N nodes of size S:

| Factor | Usable (3×500 GB) | Node failures tolerated |
|--------|-------------------|------------------------|
| 1 (no HA) | 1 500 GB | 0 |
| 2 (recommended) | 750 GB | 1 |
| 3 (max HA) | 500 GB | 2 |

Factor 2 is the recommended default: 50% efficiency, tolerates 1 node failure, quorum = 2 of 3 nodes.

### Consistency model
- **AP (availability over consistency)**: write succeeds if quorum is reached, even if some replicas lag. Correct for Veeam backup workloads.
- Strict CP (all nodes must confirm) ruled out — write latency becomes unacceptable with any network hiccup.

### Upgrade path
- Single-node deployments: no change, `ReplicaCount = 1` is the default.
- Existing cluster deployments: opt-in per bucket. Enabling replicas triggers an initial sync from primary to replica nodes.

### Estimated effort
- Total: **4–5 weeks** focused engineering
- Items 2+3 are the critical path; items 4–7 can ship incrementally
- Start with item 1 (removal) so the codebase is clean before new code is added

---

## 📝 References

- Changelog: [CHANGELOG.md](CHANGELOG.md)
- API Documentation: [docs/API.md](docs/API.md)
- Cluster Guide: [docs/CLUSTER.md](docs/CLUSTER.md)
- Performance: [docs/PERFORMANCE.md](docs/PERFORMANCE.md)
