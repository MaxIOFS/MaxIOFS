# MaxIOFS - Development Roadmap

**Version**: 1.4.2 (+ unreleased work on `main`)
**Last Updated**: July 9, 2026
**Status**: Stable — unreleased batch pending a version bump (next: likely v1.5.0)

> Completed work lives in [CHANGELOG.md](CHANGELOG.md). This file tracks only pending / planned work.

## 🔖 TODO — Release v1.5.0

There is unreleased work on `main` (see [CHANGELOG.md](CHANGELOG.md) `[Unreleased]`). To cut the release: bump the version everywhere (Makefile, `cmd/maxiofs/main.go`, `web/frontend/package.json`, `debian/`, `rpm/`, `docker-compose.yaml`, `docs/`, About page), then tag. Review README/docs for the new encryption + recovery features while cutting it.

## 📊 Project Status

| Metric | Value | Notes |
|--------|-------|-------|
| S3 Core API | ~99% | Full compatibility audit completed — 20 issues identified and resolved (March 2026) |
| Backend Tests | 3,900+ | At practical ceiling |
| Frontend Tests | 106+ | |
| Production Ready | ✅ Stable | v1.4.2 release-ready (June 30, 2026) |

---

## ⚪ Backlog — IAM/STS (temporary credentials)

Not implemented (SOSAPI reports `IAMSTS: false`). Emits short-lived credentials (access key + secret + session token, with expiry + scoped permissions) without exposing permanent keys. Use cases: temporary third-party access, apps needing ephemeral creds, identity federation (OAuth/LDAP → temporary S3 creds). Scope/design TBD. Related: the RBAC permission system is still a stub (SEC-03 startup warning) — fine-grained permissions would land together with this.

---

## 🔐 Backlog — Encryption: SSE-C / SSE-KMS (Phase 5)

The envelope system (KEK in DB, per-object DEK, worker, rotation, ciphertext HA replication, recovery bundle + `maxiofs recover`) shipped in the v1.5.0 batch. What remains is real per-request key support on top of it:

- **SSE-C**: the KEK is the customer key from the request headers (over TLS); store only the key MD5 + the wrapped DEK — the server never persists the customer key.
- **SSE-KMS**: the KEK lives in an external KMS via a pluggable provider (Vault Transit / AWS KMS).

Context that still applies:
- The reader stays multi-format — (1) plaintext → as-is; (2) legacy direct-encrypted → KEK-v1; (3) envelope → unwrap DEK. None can be dropped or existing data is lost.
- Old KEK versions are kept forever by design (tiny, included in the bundle, deleting one could orphan sidecar-only files that reference it).
- Cluster known limitation: a node that already holds a conflicting KEK version number (e.g. ex-member of another cluster) cannot join without recovery.

### Smaller encryption/recovery follow-ups

- **`maxiofs recover` checkpoint/resume**: the current implementation is a single pass (safe: output store is fresh and non-empty output is refused, so a crash = delete the partial out-db and re-run). For multi-million-object deployments a checkpoint would avoid restarting the walk.
- **Recovery-bundle stronger variants** (optional): recovery-key **escrow** (wrap the KEK with a separately-held break-glass key) and **Shamir** split (N shares, K to reconstruct).
- **Admin restore endpoint** (optional): upload a bundle through the console for the "fresh install after disaster" flow — must replace the freshly-generated KEK before any new objects are written. Today this is covered by `maxiofs recover` offline.

---

## 🔧 Follow-up — Pebble durability on hard kill

**Hard-kill loses the last seconds of Pebble metadata writes** (`batch.Commit(pebble.NoSync)`): an object PUT moments before a crash keeps its data file + sidecar but loses its Pebble entry (it still serves via the sidecar fallback, but doesn't appear in listings). Graceful shutdown is fine. Options: periodic WAL sync / sync-on-N-writes, or a startup scan that reconciles sidecars → Pebble (the walk logic now exists in `internal/recovery`).

---

## 🔧 Follow-up — Minor items deliberately deferred (July 2026 bug-hunt)

Two low-impact items from the full-project review were left as-is on purpose:

- **Bandwidth throttling spends tokens after the read** (`internal/bandwidth`): each 32 KB chunk is delivered, then waited for — transfers can run up to one chunk ahead of budget. Cosmetic smoothing only.
- **Quota delta computed outside the per-key lock** (`object.PutObject`): two concurrent overwrites of the same key can compute the same delta, causing transient metrics drift. Fixing it would hold the sharded per-key lock across encryption + disk IO, serialising unrelated keys that share a shard — not worth it for a metrics-only drift.

---

## 🟣 Planned — Erasure Coding (replace N-way replication for large objects)

**Goal**: cut disk overhead from `N×` (N-way replication) to `~1.5×` while preserving the same failure tolerance. Today a 1 GB object with `factor=3` consumes 3 GB cluster-wide; with EC `4+2` it consumes 1.5 GB and tolerates the same 2 node failures.

**Rationale**: erasure coding deserves its own release. It changes the on-disk layout, the metadata schema, and the read/write paths. The HA durability primitives shipped earlier (quorum, read fallback, anti-entropy) are prerequisites — without them, EC just multiplies the existing data-loss windows across more shards.

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

**Encryption interaction**: EC shards carry ciphertext (objects are envelope-encrypted before sharding), so the shard distribution path reuses the raw-transfer machinery from ciphertext HA replication.

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
- Same quorum semantics as the current write path: client gets 200 only when all `K+M` shards are written. Tolerate up to `M` failures (we still have K to reconstruct), but mark failed nodes `stale` for repair.
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
ECParityShards int     // M
ECStripeSize  int     // bytes per stripe
ECShards      []ECShardLocation  // per-shard: NodeID, ShardIdx, Checksum
```

Existing replicated objects keep `EncodingType = "replication"` and the new fields stay zero-valued. Reader picks the path based on `EncodingType`.

Files: `internal/metadata/types.go`, `internal/metadata/pebble_objects.go`, `internal/object/adapter.go`.

#### 5. EC-aware anti-entropy and repair (~3 days)

Extend the existing scrubber to also check shard health:

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
- Same as the current cluster: AP, quorum-based.
- EC writes require all K+M shards to be acked or the write fails — there is no "EC quorum" partial-write mode (you cannot reconstruct without K shards, period).

### Upgrade path
- Ships with `ec.enabled = false` by default. Existing deployments are unaffected.
- Admin enables EC → migration worker starts converting objects in background. Cluster stays operational throughout.
- Rollback: set `ec.enabled = false` and run reverse migration.

---

## 📝 References

- Changelog: [CHANGELOG.md](CHANGELOG.md)
- API Documentation: [docs/API.md](docs/API.md)
- Cluster Guide: [docs/CLUSTER.md](docs/CLUSTER.md)
- Performance: [docs/PERFORMANCE.md](docs/PERFORMANCE.md)
