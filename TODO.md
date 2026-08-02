# MaxIOFS - Development Roadmap

**Last Updated**: August 2, 2026

> This file tracks ONLY work in progress and pending work. Completed work lives in [CHANGELOG.md](CHANGELOG.md).

---

## 🔵 In Progress — IAM/STS (implemented, validation pending)

MaxIOFS speaks both AWS identity protocols on `POST /` of the S3 endpoint:
**STS** issues short-lived credentials (`ASIA` keys, server-enforced expiry,
revocable, cluster-replicated) and **IAM** manages identities, credentials,
managed and inline policies, and roles. Details in [CHANGELOG.md](CHANGELOG.md);
the model is described in [docs/SECURITY.md](docs/SECURITY.md) and the wire
protocol in [docs/API.md](docs/API.md).

**The design rule that governs both**: a policy attached to a pre-existing user
can only *restrict* — the normal pipeline still decides and the policy
intersects with it. Only identities created through the IAM API are governed by
their policies as a grant. Nothing that already worked changes behaviour.

### Before this can ship

- [ ] Manual end-to-end check with a real S3 client (AWS CLI / SDK) using temporary credentials, with and without a session policy — the automated round-trip signs with the server's own canonicalisation, which does not prove SDK interoperability
- [ ] Manual check of the AWS STS surface with a real SDK (`aws sts get-session-token --endpoint-url <s3-endpoint>`, then `aws sts assume-role --role-arn ...`) — this is what the payload-hash middleware exists for, and only a real signer exercises it
- [ ] Manual check of the AWS IAM surface with `aws iam --endpoint-url <s3-endpoint>`: create-user, create-access-key, put-user-policy, attach-user-policy, create-policy-version — then verify the created credential can do exactly what its policy says and nothing else
- [ ] Veeam interop: confirm it discovers the endpoints from `system.xml` (`IAMSTS=true`) and completes its create-user → put-user-policy → create-access-key flow
- [ ] Multi-node check: issue on node A, use on node B; revoke on A, confirm B rejects; create a policy on A and confirm it applies on B; delete it on A and confirm it does not come back
- [ ] Federation against a real directory / identity provider: the LDAP bind and the OAuth userinfo call are the two paths automated tests cannot reach

### Still open on the IAM work

- [ ] `maxiofs:SuperAdmin` and `maxiofs:TenantAdmin` exist in the permission catalogue but are not enforced: the code still decides by `role == "admin" && TenantID == ""`. A tenant administrator must be assignable only by a super administrator.
- [ ] A managed policy whose `default_version_id` points at a version row that no longer exists returns an empty document. Reject or repair it rather than serving nothing.
- [ ] `TestDeleteBucket_NotFound` in `pkg/s3compat` — the harness creates its user only in the request context, with no row in `users`.

### Known limitations (deliberate)

- No `Condition` support in any policy kind — documents that use one are rejected at write time rather than silently evaluated without it.
- Policy evaluation covers S3 actions. There are no `iam:*` action-level policies: the IAM surface as a whole is gated by the `iam:manage` capability.
- POST form uploads reject temporary credentials outright (fail-closed).
- Session policies cannot be edited: revoke and re-issue.
- LDAP federation does not just-in-time provision users, matching console login.
- Federation endpoints are disabled by default (`security.sts_federation_enabled`).
- IAM-created identities never get console access; they exist to hold S3 credentials.

> **No new migrations for further IAM/STS work.** All of its schema lives in
> `migration18_v160_IAMSTS`; extend that function instead of adding migration 19,
> 20, … One migration per feature, not one per increment. Valid until v1.6.0
> ships, after which the function is frozen and any further schema change needs
> its own migration.

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
