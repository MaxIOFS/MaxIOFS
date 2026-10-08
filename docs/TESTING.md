# Testing

**Version**: 1.7.0
**Last Updated**: September 27, 2026

---

## Overview

MaxIOFS has **323 Go test files**, **10 frontend test files** (Vitest), and **4 K6 performance scripts**. All Go tests run with the `-race` flag enabled by default. The project uses pure-Go SQLite (`modernc.org/sqlite`) and Pebble, so tests require no external dependencies — no Docker, no databases, no network services.

### Test Stack

| Layer | Tool | Assertion Library |
|-------|------|-------------------|
| Backend unit/integration | `go test` | `stretchr/testify` (require + assert) |
| Frontend unit | Vitest 4 + jsdom | `@testing-library/react` |
| Performance/load | K6 | Built-in K6 checks |
| Benchmarks | `go test -bench` | Standard `testing.B` |

---

## Running Tests

### Makefile Targets

```bash
# All tests with race detection and coverage
make test

# Unit tests only (skips long-running tests)
make test-unit

# Integration tests
make test-integration

# Benchmarks (storage + encryption)
make bench

# Benchmarks with CPU profiling
make bench-profile

# Lint (Go + frontend)
make lint

# Format
make fmt
```

### Go Commands

```bash
# All tests
go test -v -race -coverprofile=coverage.out ./...

# Specific package
go test -v -race ./internal/auth/...

# Specific test function
go test -v -race -run TestRateLimiter ./internal/auth/...

# Short mode (skips integration tests)
go test -v -race -short ./...

# Coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Frontend Tests

```bash
cd web/frontend

# Run all tests
npx vitest run

# Watch mode
npx vitest

# Coverage
npx vitest run --coverage
```

---

## Test Coverage by Package

### `internal/acl/` — 1 test file

| File | Description |
|------|-------------|
| `acl_test.go` | ACL parsing, canned ACL application, permission evaluation |

### `internal/api/` — 1 test file

| File | Description |
|------|-------------|
| `handler_security_test.go` | API handler security: auth bypass, injection, header validation |

### `internal/audit/` — 1 test file

| File | Description |
|------|-------------|
| `sqlite_test.go` | Audit log storage, querying, retention, SQLite operations |

### `internal/auth/` — 32 test files

| File | Description |
|------|-------------|
| `access_key_last_used_test.go` | An access key's last-used time skips a write for a second already stored and never moves backwards |
| `admin_identity_test.go` | Administrator status comes from the role or from an attached policy |
| `api_rate_limiter_test.go` | The S3 rate limit answers `SlowDown`; zero means no limit; disabled lets everything through |
| `auth_disabled_test.go` | With authentication disabled, console login and S3 credentials resolve to a user that exists |
| `auth_test.go` | Core authentication flows |
| `bootstrap_key_test.go` | The bootstrap key pair seeds the administrator's first S3 key once; ignored when a key exists, when unset, or when unusable |
| `cluster_convergence_test.go` | Two-node convergence: credential revocation window, immediate deactivation, concurrent policy and bucket-grant writes |
| `download_token_test.go` | A download token opens only the resource it names, is not a session, and is refused for a disabled user |
| `iam_console_test.go` | The console's permission screens load what a user's role and grants give; assignable roles exist and are healed on an old database |
| `iam_legacy_schema_test.go` | Databases that applied the IAM migration early: a NOT NULL trust policy column, a missing permission catalogue |
| `iam_policy_repair_test.go` | A policy whose default version is missing is repaired; one with no versions is an error |
| `iam_role_test.go` | IAM roles: trust policy parsing and evaluation, `AssumeRole` checks (session name, duration, tenant), role-session permissions, immediate revocation, ARN parsing |
| `iam_test.go` | IAM policies: parsing and size budget, evaluation (explicit `Deny` wins), version lifecycle, built-ins, inline policies, IAM user create/delete rules |
| `manager_jwt_secret_test.go` | JWT secret rotation and validation |
| `manager_s3sig_test.go` | S3 Signature V4 verification |
| `manager_simple_test.go` | Basic auth manager operations (CRUD users, keys) |
| `manager_tenant_test.go` | Tenant-scoped auth operations; storage usage does not touch `updated_at` |
| `permissions_screen_test.go` | Saving a user's permissions stores real grants without baking in inherited ones |
| `permissions_test.go` | Permission checks |
| `policy_bootstrap_test.go` | First-boot conversion to IAM policies: administrators and ordinary users keep console access; an unconverted database is healed; idempotent |
| `policy_equivalence_test.go` | The unified policy set reaches the same verdict as the legacy role, bucket-permission and capability model over every combination |
| `policy_orphan_test.go` | Deleting a user, group or tenant removes its policies; a reused identifier inherits nothing |
| `policy_set_test.go` | The policy engine enforces the account boundary; a super administrator reads across accounts and writes nowhere; an unstated owner is refused |
| `quota_cluster_test.go` | Cluster-wide quota enforcement |
| `rate_limiter_test.go` | Login rate limiting, account lockout |
| `s3_subresources_test.go` | S3 action resolution for bucket subresources, action precedence and trailing slashes |
| `s3auth_test.go` | S3 authentication flow end-to-end |
| `sts_policy_test.go` | STS session policies: parsing, evaluation, action/ARN resolution per request, enforcement on signed and presigned requests |
| `sts_test.go` | STS sessions: issued credentials, duration bounds, per-user cap, signature round trip, tampering, expiry, revocation, deactivated user, SigV2 refusal, sweeping |
| `tenant_admin_test.go` | Tenant administrators: role policies still apply, Object Lock managed for their own tenant, no unscoped full access |
| `totp_test.go` | TOTP 2FA enrollment, verification, recovery codes |
| `upgrade_test.go` | Upgrade from a pre-IAM deployment: every user keeps exactly their permissions, console screens load, a second boot changes nothing |

### `internal/bandwidth/` — 1 test file

| File | Description |
|------|-------------|
| `manager_test.go` | Per-tenant bandwidth limiter: none when unlimited, one shared limiter per tenant with in-place rate updates, measured throttling |

### `internal/bgwork/` — 1 test file

| File | Description |
|------|-------------|
| `worker_test.go` | `Stop` waits for the goroutine and is idempotent under concurrency; a spawn after `Stop` is refused; the zero value is usable |

### `internal/bucket/` — 6 test files

| File | Description |
|------|-------------|
| `bucket_name_uniqueness_test.go` | A bucket name already taken in another tenant is rejected |
| `create_entry_test.go` | Creating a bucket whose name has a removal pending finishes the removal first, a bucket from another node included; creating one that exists touches nothing |
| `delete_bucket_test.go` | Bucket deletion with object cleanup |
| `integration_test.go` | Bucket CRUD + versioning integration |
| `manager_test.go` | Bucket manager core operations |
| `policy_evaluation_test.go` | S3 bucket policy evaluation |

### `internal/cluster/` — 44 test files

| File | Description |
|------|-------------|
| `access_key_sync_test.go` | Cross-node access key synchronization |
| `anti_entropy_test.go` | HA anti-entropy worker and reconciliation; a node compares with every other healthy node |
| `bucket_aggregator_test.go` | Multi-node bucket list aggregation |
| `bucket_gate_test.go` | Freezing a bucket refuses new writes and returns once the writes under way end; a freeze given up leaves the bucket writable |
| `bucket_permission_sync_test.go` | Bucket permission replication |
| `cache_test.go` | Cluster cache invalidation |
| `circuit_breaker_test.go` | Circuit breaker for unhealthy nodes |
| `dead_node_reconciler_test.go` | Dead-node replica redistribution |
| `deletion_log_test.go` | Tombstone-based deletion sync: a deletion keeps its time and the later of two records; each node is sent each deletion once, in order, and again when it changes |
| `deletion_order_test.go` | A deletion is dated after the last change of what it removes; entities of every type and their deletions are ordered by time, the same second keeping the entity; an earlier release's deletion log is repaired once; the IAM payload carries deletion times |
| `deletion_retention_test.go` | A deletion older than the retention is kept until every member was sent it and no catch-up that replays it is pending; a removed node holds nothing back; bucket and row deletions are kept for a member that missed them; a catch-up is tracked until it ends; the deletion sequence never goes back, and a log numbered from its highest entry is sent whole again once |
| `group_mapping_sync_test.go` | IDP group mapping synchronization |
| `ha_bucket_state_test.go` | Bucket deletions are pruned with the deletion log, the later of two deletions winning |
| `ha_row_state_test.go` | Rows of the replicated tables, on the real schema with foreign keys: the order of versions, deletions and content; a stored row keeps the rows that reference it; a report follows its configuration; rows that may not coexist converge on one; refusals; expired shares; a change dated after any version held; a synchronization sends every row and deletion in batches; old versions are forgotten |
| `ha_fanout_lock_test.go` | The legacy (non-raw) HA transfer carries the object's retention and legal hold; without lock state it sends no lock header |
| `ha_metadata_queue_test.go` | A metadata change reaches a healthy replica before the request returns; it is queued for a replica that is down, fails or has changes waiting, never for one that refuses it; the queue is replayed in order, dropping refused changes and stopping at a failure; removed nodes leave no queue |
| `ha_mirror_test.go` | A factor of 2 keeps writing with its peer down and records the miss; a returning node is caught up with what changed since (lock state included), the deletes it missed (never a key it wrote after the delete), and retried when it fails or is down again. The initial sync copies every version oldest first with its ID, delete markers with their time, and lock state; in a suspended bucket, the current object without a version ID last, with its ETag and tags; a bucket it cannot list is left to a catch-up from the start. A copy pulled of a version deleted here is not stored; a key whose versions cannot be listed or sent is left unreconciled |
| `ha_placement_test.go` | A write whose copies fail is not sent to the nodes holding its entry and they miss it; a node that does not take an entry misses the write and is unavailable; the data is read from a node that serves this write of it, passing over one that serves another, and a seek reads from there; outside a cluster data held elsewhere is not found; a ciphertext copy made to name its node carries those locations; the room of a cluster holds each byte on factor different nodes and counts every node but the dead ones |
| `ha_quorum_test.go` | HA write quorum behavior |
| `ha_rollback_lock_test.go` | A write that misses HA quorum (factor 3, both peers failing) removes its local version even under COMPLIANCE retention and a legal hold |
| `ha_url_test.go` | Cluster HA endpoint URL handling |
| `health_test.go` | Node health checks (30s intervals) |
| `idp_provider_sync_test.go` | IDP provider configuration sync |
| `leader_test.go` | Leader lease granted when free, refused while held, a stale term is fenced |
| `manager_test.go` | Cluster manager core operations |
| `membership_test.go` | A node and its removal are ordered by time, the same second keeping the node; a removed node is not added back; edits and drains are stamped after the last change; a node dead by its probes is back when it answers and caught up from when it was marked dead, a drained one stays dead; a drain on another node is taken here and refused below the replication factor; joining takes the cluster's membership |
| `placement_repair_test.go` | Removing a member announces it once: a removal received again, or of a node that is not a member, does not |
| `multipart_routing_test.go` | A multipart upload is made on the node its ID names with a factor above 1; with a factor of 1, an ID naming no node, outside a cluster or on its own node, where the request is; a node gone from the cluster is reported; the other nodes' uploads are asked for with a factor above 1 only, a node that does not answer left out |
| `migration_rows_test.go` | A bucket's database rows move typed and once however often applied; generated IDs never replace the target's rows; rows naming another bucket, table or column are refused. The verification digest covers what a client reads of a version |
| `migration_test.go` | Migration job records |
| `proxy_bounds_test.go` | Every proxy client entry point is bounded (no unbounded read/timeout) |
| `proxy_client_test.go` | The proxy's HTTP client bounds reachability, not transfer size; keeps TLS and pooling |
| `proxy_content_length_test.go` | A proxied request carries `Content-Length` to the target node |
| `proxy_replay_test.go` | Cluster proxy auth refuses a replay, tampered requests, and another cluster's token |
| `proxy_sts_test.go` | An STS session is preserved across a cluster proxy hop |
| `proxy_test.go` | Request proxying to remote nodes |
| `quota_aggregator_test.go` | Cross-node quota aggregation |
| `quota_integration_test.go` | Quota enforcement in cluster |
| `router_test.go` | Request routing to bucket owner; a bucket found here is served here whatever location was cached, and one that left is not |
| `shared_state_test.go` | TLS state is race-free; the leader manager's `Stop` is idempotent; start doesn't replace the proxy client |
| `storage_pressure_test.go` | Storage-pressure health state |
| `tenant_sync_test.go` | Tenant sync across nodes, with timestamps as the tenants table stores them; usage is not part of the checksum |
| `user_sync_test.go` | User sync across nodes |
| `written_at_test.go` | Two writes of an object are ordered by second, then within one second by the time each was written; a write is ordered against a deletion of its second, and one without a write time counts as made after |

### `internal/clusterauth/` — 1 test file

| File | Description |
|------|-------------|
| `signature_test.go` | The inter-node request signature binds every request field; components cannot be confused; nonces are unique |

### `internal/config/` — 1 test file

| File | Description |
|------|-------------|
| `config_test.go` | Config loading (YAML, env vars, CLI flags), validation, defaults |

### `internal/db/` — 1 test file

| File | Description |
|------|-------------|
| `dsn_test.go` | The canonical SQLite DSN applies its pragmas |

### `internal/db/migrations/` — 1 test file

| File | Description |
|------|-------------|
| `migrations_test.go` | SQLite schema migrations, version tracking |

### `internal/encsecret/` — 1 test file

| File | Description |
|------|-------------|
| `store_test.go` | The secret is stored once (configured, fallback or generated) and kept across starts; adoption re-encrypts every stored credential and stores the secret in one transaction, a failure changes neither; a check writes nothing; fingerprints |

### `internal/idp/` — 6 test files

| File | Description |
|------|-------------|
| `crypto_test.go` | AES-256-GCM encryption for IDP secrets |
| `manager_test.go` | IDP provider CRUD, user authorization |
| `reencrypt_test.go` | Bind passwords and client secrets move to a new secret; the rest of the configuration is kept; what no secret decrypts is named and left |
| `store_test.go` | IDP persistent storage operations |
| `ldap/ldap_test.go` | LDAP bind, search, group resolution |
| `oauth/provider_test.go` | OAuth2/OIDC flow, token exchange, user mapping |

### `internal/inventory/` — 5 test files

| File | Description |
|------|-------------|
| `change_observer_test.go` | Every configuration and report written or deleted is reported, trimmed reports too; the worker runs only when its gate says |
| `generator_test.go` | S3 inventory report generation (CSV/JSON), page after page, stored through the object writer |
| `manager_test.go` | Inventory schedule management; a completed report keeps its path |
| `report_retention_test.go` | Only recent report history is kept |
| `worker_test.go` | Background inventory worker |

### `internal/kek/` — 2 test files

| File | Description |
|------|-------------|
| `bundle_test.go` | Recovery bundle export and decryption: round trip, wrong or short passphrase, garbage input, several KEK versions, download tracking |
| `store_test.go` | KEK bootstrap (database key wins over config, config seeds version 1, otherwise generated), lookup by version, cluster key creation and adoption, rotation, concurrent rotations |

### `internal/layout/` — 1 test file

| File | Description |
|------|-------------|
| `migrate_test.go` | Layout v1 → v2 migration: moves every object and version, idempotent, resumes after an interruption, dry run, refuses duplicate or tenant-like bucket names, reports inconsistencies, prunes the old tree once per directory, purges implicit folder objects |

### `internal/lifecycle/` — 4 test files

| File | Description |
|------|-------------|
| `expiration_gate_test.go` | A node the gate stops expires nothing and still aborts its own multipart uploads; the pass deletes through the manager set after construction |
| `expiration_test.go` | Lifecycle expiration behavior |
| `worker_test.go` | Lifecycle rule evaluation and object expiration |
| `write_gate_test.go` | A bucket whose writes are held is skipped; the others are processed inside the gate |

### `internal/logging/` — 5 test files

| File | Description |
|------|-------------|
| `helpers_test.go` | Logging helper functions |
| `http_test.go` | HTTP log target delivery with WaitGroup-tracked goroutines |
| `manager_test.go` | Log manager routing, multi-target dispatch |
| `store_test.go` | Log target persistence |
| `syslog_test.go` | Syslog target output |

### `internal/metadata/` — 18 test files

| File | Description |
|------|-------------|
| `bucket_concurrency_test.go` | A bucket configuration update does not lose concurrent metric increments |
| `bucket_replica_test.go` | A bucket from another node is stored with its times when newer, keeping this node's usage; an older or equal version changes nothing; a name another tenant holds is refused. Usage does not change `UpdatedAt` |
| `close_idempotent_test.go` | `Close` is idempotent |
| `delete_bucket_if_empty_test.go` | A bucket holding a folder-marker object is not empty |
| `lastmodified_test.go` | `PutObject` keeps a caller-provided `LastModified` and stamps the current time only when it is unset |
| `multipart_comprehensive_test.go` | Multipart upload metadata tracking |
| `objects_test.go` | Object metadata storage and retrieval |
| `pagination_property_test.go` | Every listing, delimited or not, returns every key and prefix exactly once across page boundaries |
| `pebble_test.go` | Pebble store: object CRUD and bucket deletion race coverage |
| `pebble_durability_test.go` | WAL sync loop, `CLEAN_SHUTDOWN` sentinel, unclean-shutdown recovery |
| `pebble_group_commit_test.go` | `PutObject`/`PutObjectVersion` release the bucket lock before the WAL-sync wait, so concurrent writers share one fsync |
| `pebble_group_commit_crash_test.go` | A process killed between the NoSync commit and the WAL sync leaves a rollback copy consistent with the index, not a phantom entry |
| `pebble_pagination_test.go` | Marker loops over listing, delimited listing and search lose nothing at several page sizes |
| `search_objects_test.go` | Object search by prefix, delimiter, pagination |
| `store_consistency_test.go` | Metadata consistency and counter reconciliation |
| `tags_comprehensive_test.go` | Object and bucket tag operations |
| `version_delete_test.go` | Deleting a version promotes the next one, removes the entry when none remain, and leaves the main entry alone for an older version |
| `versioning_test.go` | Version ID generation, version listing |

### `internal/metrics/` — 6 test files

| File | Description |
|------|-------------|
| `badger_history_test.go` | Metrics history stored in Pebble (via RawKVStore) |
| `collector_test.go` | Prometheus metrics collection |
| `history_test.go` | Metrics history aggregation |
| `manager_test.go` | Metrics manager lifecycle |
| `performance_test.go` | Performance metrics tracking |
| `system_metrics_test.go` | System resource metrics (CPU, memory, disk) |

### `internal/middleware/` — 4 test files

| File | Description |
|------|-------------|
| `body_digest_test.go` | A verified request body passes declared content through and rejects substituted, truncated, or unknown-algorithm content |
| `cluster_auth_test.go` | HMAC inter-node authentication middleware |
| `middleware_test.go` | Auth, CORS, rate limit middleware chain |
| `tracing_test.go` | Request tracing and request ID propagation |

### `internal/notifications/` — 2 test files

| File | Description |
|------|-------------|
| `helpers_test.go` | Notification helper functions |
| `manager_test.go` | SSE notification delivery, client management |

### `internal/object/` — 51 test files

| File | Description |
|------|-------------|
| `accounting_property_test.go` | Bucket size/count accounting agrees with the object set after any sequence of operations |
| `adapter_test.go` | Storage adapter abstraction |
| `backup_restore_test.go` | A retained overwrite/multipart backup survives a failed restore and is removed after a successful one |
| `delete_key_fidelity_test.go` | `DeleteObject` deletes only the key it was given |
| `encryption_migration_test.go` | Background worker converts plaintext to envelope encryption; multipart ETag preserved |
| `faultcheck_test.go` | Kills a subprocess mid-write (before/after data, before/after metadata) and checks what the next start recovers; also covers the retained-backup rollback path and the cost of an overwrite's backup copy |
| `folder_marker_test.go` | A folder marker and a same-named object coexist (layout v2) |
| `integration_test.go` | Full object lifecycle (put → get → delete) |
| `key_acls_test.go` | An object does not inherit the ACL of the one before it at the key; ACLs kept per key are moved into their objects once |
| `key_serialization_test.go` | Every key-mutating call serializes on the per-key lock |
| `manager_coverage_test.go` | Manager edge cases for coverage |
| `manager_critical_functions_test.go` | Critical path testing (copy, multipart complete) |
| `manager_envelope_test.go` | Envelope encryption roundtrip, legacy direct-encrypted objects, folder markers stay plaintext |
| `manager_final_coverage_test.go` | Final coverage gap tests |
| `manager_integrity_test.go` | `VerifyObjectIntegrity` / `VerifyBucketIntegrity` — folder markers, delete markers, multipart skip, ETag mismatch |
| `manager_internal_test.go` | Internal manager functions |
| `manager_low_coverage_test.go` | Low-coverage path tests |
| `manager_metadata_failure_test.go` | A failed metadata save after publishing bytes restores the previous object/backup |
| `manager_quota_overwrite_test.go` | A quota-rejected overwrite never touches the existing object |
| `manager_replicated_lastmodified_test.go` | A replica write keeps the primary's `LastModified` |
| `manager_versioning_acl_test.go` | Versioning + ACL interaction |
| `multipart_cleanup_test.go` | Abort/complete leave no upload directory behind; the record survives when parts can't be removed |
| `multipart_complete_rollback_test.go` | A multipart completion that fails after publishing the assembled object restores the previous one |
| `multipart_lock_test.go` | Independent parts upload concurrently; conflicting operations exclude each other |
| `multipart_part_race_test.go` | A part cannot be replaced while completion is assembling it |
| `multipart_part_replace_test.go` | Replacing an acknowledged part retains it until the new one commits, and restores it on failure |
| `multipart_race_test.go` | Concurrent multipart upload race conditions |
| `multipart_retention_test.go` | Default retention on multipart completion; a lookup failure preserves the upload |
| `multipart_stream_test.go` | A multipart completion failing at any stage of the stream (open, read, short part, close, cancel, encryption, commit, lookup) keeps the previous object, closes every part reader and stays retryable; empty parts complete |
| `multipart_validation_test.go` | Multipart validation and failure handling |
| `raw_replication_test.go` | Replica-side raw ciphertext write, plain and versioned |
| `raw_replication_rollback_test.go` | A failed raw replica overwrite restores the previous data and sidecar, including existing versions |
| `read_snapshot_test.go` | A reader keeps the generation it opened across a concurrent overwrite |
| `replica_metadata_test.go` | The entry of an object whose data other nodes hold answers HEAD and listings and names them to a GET, removes a file kept for the key, and is not replaced by an earlier entry; versions land by time; a node serves its own file of an entry naming it, and every file outside a cluster; a lost copy names the other holders; user metadata changes in the entry; an earlier copy, plain or ciphertext, does not replace a later write; integrity is not checked here for data held elsewhere; the locations of a write or a version change only for that write, here and on its current entry, and a node they no longer name removes its file; the newest locations of a write, or of an older version, are kept against older changes, entries and copies, plain or ciphertext, one number going to the larger list; a copy, plain or ciphertext, is stored only where the newest locations name this node |
| `replica_versions_test.go` | Versions copied from another node, raw or not, land in order whatever order they arrive in: an older copy or delete marker never replaces the latest, a repeated copy is not counted twice |
| `replica_copy_test.go` | A copy into a bucket whose versioning was suspended is stored as the version it copies, never replaces the current object (not even one of the same second), and a copied delete marker stays one; a copy carries its ETag, tags, ACL and restore state and is refused when its bytes do not match; changing tags or ACL keeps the restore status; a write while suspended keeps the bytes of the version it covers |
| `reconcile_serialization_test.go` | Reconcile takes the same per-key lock as normal operations and does not race a concurrent PUT |
| `review_part_crash_faultcheck_test.go` | An acknowledged multipart part survives a process kill during a replacement upload |
| `search_objects_test.go` | Object search and listing |
| `sidecar_loss_test.go` | A lost sidecar never serves ciphertext as if it were the plaintext object |
| `staging_cost_test.go` | Content passes of PUT staging and multipart completion, per path; the large-part probe is opt-in (see [PERFORMANCE.md](PERFORMANCE.md#put-staging-and-multipart-completion)) |
| `suspended_delete_test.go` | DELETE without a version ID in a suspended bucket adds a marker and keeps the versions from before; a current object without a version ID is removed; a legal hold refuses it; a copied marker removes only an older object |
| `storage_identity_test.go` | `GetObject` refuses to serve ciphertext whose sidecar records no verifiable identity |
| `versioned_prefix_test.go` | Versioned-bucket key collisions: shared prefix, case-only difference, folder marker |
| `versioning_delete_test.go` | Version-aware delete operations |
| `versioning_multipart_metrics_test.go` | Versioning, multipart, and metric counter interaction |
| `write_audit_test.go` | PUT applies the bucket default retention; a bucket read failure fails closed; concurrent writes respect the quota; a failed encryption-migration restore keeps its backup |
| `write_cost_test.go` | Write-path cost accounting (backend calls, bytes, metadata lookups) for new, overwrite, versioned and part writes; the disk workload is opt-in (see [PERFORMANCE.md](PERFORMANCE.md#write-path-cost-accounting)) |
| `write_quota_test.go` | Quota reservations: concurrent writers never over-admit and all fitting writers are admitted; a finished or refused write is not counted twice; a slow tenant check blocks nobody; freeing space is allowed over the limit; a versioned overwrite is charged in full |
| `write_rollback_test.go` | Undoing a write deletes the protected version it created; a client delete of that version is still refused. Multipart uploads keep the Object Lock headers of their creation, validated then; user metadata stays user metadata — it cannot set the lock state, the Content-Type, the canned ACL or sidecar fields — including for uploads created before the upgrade |
| `write_safety_test.go` | Reservations are released on success and failure; a failed write stays retryable; quota is accounted per bucket; Object Lock headers are applied at the first commit; a failed bucket lookup on delete preserves the object |

### `internal/presigned/` — 1 test file

| File | Description |
|------|-------------|
| `presigned_test.go` | Pre-signed URL generation and validation |

### `internal/recovery/` — 5 test files

| File | Description |
|------|-------------|
| `reconcile_test.go` | Reconcile rebuilds an index entry missing entirely from an object left on disk |
| `reconcile_repair_test.go` | Reconcile corrects an entry describing bytes an overwrite already replaced, from the sidecar's own identity |
| `recovery_test.go` | Offline `maxiofs recover` — rebuilds a fresh Pebble store from the object tree alone |
| `restore_keks_test.go` | Restoring KEKs from a recovery bundle into the auth database |
| `review_faultcheck_test.go` | Reconcile takes the same per-key lock as a live overwrite and does not race it; a same-size same-second overwrite is still detected |

### `internal/replication/` — 10 test files

| File | Description |
|------|-------------|
| `adapter_test.go` | `RealObjectAdapter` CopyObject, DeleteObject, GetObjectMetadata |
| `change_observer_test.go` | Every rule created, changed or deleted is reported; scheduled rules run only when the gate says |
| `credentials_test.go` | AES-256-GCM credential encryption round-trip, wrong key, legacy plaintext, `deriveCredentialKey` |
| `manager_sync_test.go` | `SyncBucket` (every page), `SyncRule` (lock contention), `processScheduledRules`, `cleanup` |
| `manager_test.go` | Replication rule management |
| `reencrypt_test.go` | Destination keys move to a new secret; the manager reads with the secret it holds; legacy plaintext and keys no secret decrypts are left |
| `replication_e2e_test.go` | End-to-end replication with `require.Eventually` |
| `s3client_test.go` | S3 replication client behavior |
| `status_retention_test.go` | Old replication status copies are dropped; failures are kept |
| `worker_test.go` | `processItem` (PUT/COPY/DELETE), `handleError`, `completeItem`, `updateReplicationStatus` |

### `internal/rollback/` — 2 test files

| File | Description |
|------|-------------|
| `undo_test.go` | Undoing an interrupted write: restores when the index still matches the retained copy, keeps a committed overwrite (including same-size and multipart ETag shapes), never resurrects a deleted object, resolves only the oldest of repeated retained copies, and is idempotent |
| `review_faultcheck_test.go` | The same decisions under an actual process kill instead of a simulated one |

### `internal/server/` — 50 test files

| File | Description |
|------|-------------|
| `bucket_aggregation_test.go` | Multi-node bucket aggregation API |
| `bucket_quota_handlers_test.go` | A bucket quota is authorized against its own tenant, not `?tenantId=` |
| `bucket_ownership_test.go` | A bucket a client deletes, forced or not, takes its shares, rules, inventory, permissions, policies, notification configuration and integrity scans: a share link of the old bucket opens nothing in a new one; a new bucket starts without what a former one left; the state of deleted buckets is dropped at start; with a factor of 2 on every node |
| `bucket_removal_sweep_test.go` | A pending bucket removal is finished at the next start; force-delete records the removal too; the directory of a bucket created since is kept |
| `bucket_routing_test.go` | Between two complete nodes, every S3 request (AWS SDK client) and console request about a bucket reaches the node that holds it; bucket names are unique across nodes; bucket permissions stay with the coordinator and find a bucket on another node |
| `capability_guard_test.go` | A global administrator's console S3 access stays read-only across tenants |
| `cluster_lists_test.go` | With every node holding every bucket, a bucket is listed once in the console and to S3 clients; the degraded reason another node sends is not taken; a read is served by the node that receives it |
| `cluster_migration_test.go` | Bucket migration between two complete nodes: every version and its state moved and verified with the bucket's configuration, ACL and rows; writes refused while it runs and writes under way waited for; failure, verification, uploads in progress, restarts during the copy and the hand-over, a live bucket on the target; hidden copies answer for nothing; stale locations are forgotten; the console endpoint |
| `cluster_raw_replication_test.go` | HA raw-ciphertext receive; rejects a KEK version the receiving node does not hold; a raw copy of a version deleted here, or of a write before a deletion of its key, is not stored |
| `cluster_replica_lock_test.go` | The HA replica receive keeps the primary's lock state as sent: no bucket default added, no date refused; an older primary's transfer gets the bucket default. A copy served to a peer carries the lock state. A metadata change that cannot apply answers 404, 400 or 409. A replicated delete marker older than the latest version leaves the key visible; a copy sent with headers keeps the time its write was made, an object and a delete marker alike; a copy, whole or entry only, of a write before this node deleted the key is not stored, one of the second of the deletion or later is; an entry names the nodes holding its data; a copy's locations and their number are kept when received and sent when served |
| `console_access_test.go` | Console access and capability checks |
| `console_api_test.go` | Console REST API endpoint tests |
| `console_idp_test.go` | Console IDP management endpoints |
| `console_proxy_test.go` | The console proxy client bounds reachability checks, not transfer size |
| `console_tenant_boundary_test.go` | A global administrator cannot change another tenant's bucket from the console |
| `coordinator_forward_test.go` | An already-forwarded cluster request is not forwarded again; a coordinator write requires cluster auth |
| `deletion_semantics_test.go` | A received deletion keeps its time; a re-created bucket keeps its owner's policy; a key written again reaches the node that missed it; synchronized copies and IAM deletions are ordered against deletions; local deletions are dated after the last change and record the IAM policies of deleted users, tenants and groups; a deletion from the log removes the copy of every kind of entity with what goes with it; a revoked session reaches the other node with its time |
| `download_token_test.go` | A download token is only valid on the two download routes it was minted for |
| `encryption_recovery_handlers_test.go` | Encryption recovery status and recovery-bundle download endpoints |
| `encryption_secret_test.go` | Stored credentials survive a restart with the default configuration; a node joining a cluster (real join) takes its encryption secret; the coordinator gives its secret and no other node can; the Nodes list shows a differing secret; the settings never show the JWT secret |
| `encryption_worker_test.go` | Background encryption pass, its manual-run endpoint, and shutdown tracking |
| `forced_password_change_test.go` | A user under a forced password change can replace it, lifting the obligation |
| `group_handlers_test.go` | A group member add is rejected across a different tenant scope; group delete removes policies atomically |
| `ha_cluster_test.go` | Complete nodes with their S3 APIs and AWS SDK clients: a multipart upload's parts, listings, completion and abort reach the node it was started on from any node; a node that does not answer gives `503`, one gone from the cluster `NoSuchUpload`; an upload named after no node is made where it is. Three nodes with a factor of 2: a write is held by its node and the node with the most free space and listed, read (a range alone) and answered for HEAD on every node; a read tries each holder and answers `503` with none; a delete reaches every node; an overwrite moves its data; every version is listed and read everywhere; tags set where the data is not; a node serves only the data it holds, and its console finds an object whose holders do not answer. A node that missed writes, or lost its data, is sent what it holds; an initial sync sends each node what it holds; a later write reaches a node not holding it as an entry. The copies of a removed or dead node are made again, a lower factor drops the extra copies, a higher one adds those it needs after the request is answered and the first sync's entry, arriving after, does not undo it, a full cycle repairs them, and a factor of 1 is refused. A node under storage pressure passes its copy (object, version, multipart) to one node, to the next when one does not take it, keeps it when none does or no other node holds the entry only, and finishes even when the client goes away; a copy whose answer was lost, after a move or a repair, is removed. Every node reports to Veeam the room of the cluster; with a factor of 1 Veeam sees what a tenant uses on every node |
| `ha_bucket_test.go` | Between two complete nodes with a replication factor of 2: every bucket change (configuration, ACL, deletion) reaches the other node in both directions with the same version; usage stays each node's; a node that was down is sent its buckets, then their objects; a new replica is sent the buckets first; deletions keep newer data and data under a legal hold; refusals; lifecycle expires on the coordinator only; a deletion removes a write made before it in its second. An initial sync that fails some objects leaves them to the catch-up and says how many; a joining node is sent the objects; a catch-up sends every version the node missed, once, and its copies keep the tags; a version or delete marker deleted on a node is not stored again by a copy |
| `ha_rows_test.go` | Between two complete nodes with a replication factor of 2: shares, replication rules, inventory configurations and reports reach the other node in both directions; a node that was down is caught up, rows from before the upgrade included; the coordinator's inventory report, its file and run reach the other node; a node that is not the coordinator writes no report; with a factor of 1 changes are dated and sent once it rises; an undated change is missed by every node |
| `iam_admin_test.go` | Tenant scope separates administrators; who a policy grants administration to |
| `iam_aws_api_test.go` | AWS IAM XML actions route to the IAM handler and require the IAM-manage capability |
| `iam_tenant_isolation_test.go` | A tenant cannot reach another tenant's IAM role; a global administrator reaches every tenant's |
| `ingress_headers_test.go` | Internal cluster headers are stripped from every inbound request |
| `interrupted_writes_test.go` | `Start` refuses to serve traffic when the interrupted-write rollback has unresolved failures |
| `kek_rotation_test.go` | KEK rotation endpoint and cluster KEK-sync receive |
| `kek_shutdown_test.go` | Shutdown waits for a rotating KEK checkpoint; stopping encryption workers cancels the periodic pass |
| `leader_gate_test.go` | Reads, sign-in and cluster traffic are never blocked by the leader gate; a bucket's settings and objects need no coordinator, its permissions do |
| `lifecycle_coverage_test.go` | Shutdown releases every lifecycle field; no stale entry survives it |
| `multipart_sweep_test.go` | The startup sweep discards parts whose upload record is already gone |
| `node_lifecycle_test.go` | A removed node is removed on every node and not added back, and leaves when told; a leaving node is removed from the others; a drain takes the node out of service on every node; a cluster joined at runtime replicates writes; outside a cluster objects are only stored; only a node without data joins a cluster, every kind of data refusing it with what it holds |
| `object_extra_handlers_status_test.go` | A console write refused by a quota answers 403; a failure to check the quota does not. A console upload's Object Lock headers need their own permission; a tenant without a storage limit can upload |
| `partition_test.go` | Two nodes cut off for longer than deletions are kept, both serving, converge: deleted objects, users, buckets and shares stay deleted, writes on either side reach the other, and another round of every synchronization brings nothing back; a bucket deleted on one side and written to on the other is kept on both with the later writes only, and sent to the other node at once; a rewrite in the second of its deletion, while a node is away, is kept on every node; a delete marker reaches the other node with the time it was written |
| `pending_password_change_test.go` | A pending forced password change leaves only the intended way out |
| `privilege_boundary_test.go` | `IsGlobalAdmin`/admin-role checks require no tenant and cover both role names |
| `profiling_access_test.go` | `/debug/pprof/*` is reachable by a global administrator and refused without a credential |
| `replication_adapter_test.go` | Replication reads and lists a tenant's bucket under its tenant, page after page |
| `route_ordering_test.go` | Route priority and matching order |
| `search_api_test.go` | Object search API endpoints |
| `server_test.go` | Server startup, shutdown, configuration |
| `sts_aws_api_test.go` | AWS STS XML actions: missing/unknown action, `GetSessionToken` signature and credential issuance |
| `sts_federation_test.go` | STS federation disabled by default; requires a provider ID; rejects a malformed body |
| `tenant_usage_test.go` | Tenant synchronisation keeps each node's usage; the bucket limit and the console count the tenant's buckets on every node; the statistics pass corrects the tenant's storage; a tenant deletion from another node keeps a tenant changed after it |
| `worker_shutdown_test.go` | `goWorker` shutdown waits for every tracked worker and refuses new ones once shutdown starts |

### `internal/settings/` — 1 test file

| File | Description |
|------|-------------|
| `manager_test.go` | Dynamic settings CRUD, defaults, validation |

### `internal/share/` — 3 test files

| File | Description |
|------|-------------|
| `change_observer_test.go` | Every share created or deleted is reported, expired ones are not; a share replacing an expired one keeps its own ID |
| `manager_test.go` | Pre-signed share link generation and access |
| `reencrypt_test.go` | Share keys move to a new secret; the store reads with the secret it holds; legacy plaintext and keys no secret decrypts are left |

### `internal/storage/` — 11 test files

| File | Description |
|------|-------------|
| `durability_test.go` | `Put` flushes data and sidecar, and survives a read-back |
| `filesystem_layout_test.go` | Layout v2 multipart upload directory: listed, discarded, ID cannot carry a path separator |
| `filesystem_list_test.go` | Listing returns every object in a bucket; empty on an absent bucket |
| `filesystem_staged_commit_test.go` | The staged-sidecar two-phase commit: normal put/overwrite, roll-forward after the data commit, roll-back when it never happened |
| `filesystem_test.go` | Filesystem storage operations (read, write, delete, stat) |
| `folder_conflict_test.go` | An object and a same-named folder marker coexist; keys differing only in case coexist; a key ending in `.metadata` is ordinary (layout v2) |
| `folder_delete_test.go` | Deleting a folder marker leaves the objects under it alone |
| `path_serialization_test.go` | Every path-mutating backend call serializes on the per-path lock |
| `read_snapshot_test.go` | An open object handle survives the path being replaced or deleted under it |
| `sidecar_identity_test.go` | `Put` and `SetMetadata` record and keep the object's own identity in the sidecar |
| `storage_bench_test.go` | Storage benchmarks (throughput, latency by file size) |

### `internal/transfer/` — 1 test file

| File | Description |
|------|-------------|
| `stall_test.go` | Stall watchdog for inter-node transfers: a slow but progressing transfer is not cut, a stalled one is; upload progress counts; closing the body releases the watch; caller cancellation still works |

### `pkg/encryption/` — 2 test files

| File | Description |
|------|-------------|
| `encryption_test.go` | AES-256-GCM encrypt/decrypt roundtrip (legacy CTR backward-compat) |
| `encryption_bench_test.go` | Encryption throughput benchmarks |

### `pkg/s3compat/` — 39 test files

| File | Description |
|------|-------------|
| `acl_cascade_test.go` | An ACL grant never overrules an explicit policy `Deny`; a granted user still reads |
| `acl_debug_test.go` | ACL compatibility debugging |
| `acl_security_test.go` | ACL security edge cases |
| `background_jobs_test.go` | A background job keeps the request's values but uses shutdown cancellation, not the request's |
| `batch_missing_bucket_test.go` | A batch delete on a missing bucket answers `NoSuchBucket` |
| `bucket_limit_test.go` | A tenant's bucket limit counts every bucket the tenant has, including those its users created through the S3 API |
| `bucket_listing_test.go` | `ListBuckets` shows exactly what the caller's policies grant, no more and no less |
| `checksum_test.go` | `x-amz-checksum-*` header validation (CRC32, CRC32C, SHA1, SHA256) |
| `conditional_headers_test.go` | `If-Match`/`If-None-Match` ETag comparison |
| `copy_source_test.go` | Copy source parsing, version IDs, and encoded source keys |
| `delete_bucket_compat_test.go` | Bucket deletion allows only implicit folder markers to remain |
| `folder_conflict_test.go` | A folder marker over a same-named object succeeds (layout v2) |
| `governance_bypass_test.go` | Object Lock governance bypass is decided by policy, refused anonymously, and on the delete path |
| `handler_coverage_test.go` | S3 handler comprehensive coverage |
| `iam_authorization_test.go` | An IAM read policy does not authorize a write or delete; version actions are resource-scoped |
| `inventory_test.go` | S3 inventory API compatibility |
| `multipart_faultcheck_test.go` | Concurrent part uploads to different/the same part number over real HTTP; abort during a disconnected upload |
| `multipart_object_lock_test.go` | `CreateMultipartUpload` refuses Object Lock headers it cannot honour with `InvalidRequest`; a canned ACL comes from `x-amz-acl` only, never from user metadata |
| `multipart_ownership_test.go` | A foreign multipart upload is refused; the owner's own upload keeps working |
| `multipart_pagination_test.go` | Multipart upload listing and parts pagination |
| `multipart_retention_test.go` | Default retention applied to an S3 multipart completion |
| `notifications_test.go` | Bucket notification webhook dispatch |
| `object_lock_permissions_test.go` | Object Lock headers on PUT, CreateMultipartUpload and CopyObject need `s3:PutObjectRetention` / `s3:PutObjectLegalHold`; the bucket default needs neither; a copy carries the retention it asked for |
| `objectlock_config_test.go` | Object Lock configuration needs its own permission; refused anonymously |
| `post_presigned_test.go` | POST presigned URL (HTML form upload + policy validation) |
| `quota_write_errors_test.go` | A tenant quota refusal inside a PUT and a bucket quota refusal on CopyObject answer 403 `QuotaExceeded`; a failure to check the quota is not a refusal |
| `presigned_signer_test.go` | Presigned-URL signer resolves a permanent key's owner and an STS principal, denies what it cannot resolve |
| `presigned_test.go` | S3 pre-signed request handling (SigV2 + SigV4) |
| `proxy_fallthrough_test.go` | A request whose body was already consumed is not handled locally; reads may still fall through |
| `s3_test.go` | S3 protocol compatibility tests; in a cluster, capacity.xml takes the usage of the bucket's tenant on every node and, with no quota, the room of the cluster |
| `stale_location_test.go` | A node told the bucket is no longer where it forwarded the request forgets that location and passes the retryable answer on |
| `select_streaming_test.go` | `SelectObjectContent` loaders don't scale memory with input size; schema can grow mid-stream |
| `select_test.go` | `SelectObjectContent` — event stream encoding, CSV/JSON loaders, SQL queries, batch flushing |
| `sts_compound_test.go` | Compound operations within one STS session |
| `tenant_boundary_test.go` | The tenant boundary holds in both directions; a super-admin crosses it read-only |
| `tenant_isolation_test.go` | Tenant membership is not itself a permission — another tenant, even a member, is refused |
| `version_lookup_cost_test.go` | `FindExactObjectVersion` reads only the versions of the key it was asked for |
| `version_permission_test.go` | A grant on the current object does not reach a version; a version grant still works |
| `versioning_pagination_test.go` | `ListObjectVersions` paging covers everything exactly once, with and without a delimiter |

### `cmd/maxiofs/` — 2 test files

| File | Description |
|------|-------------|
| `main_test.go` | CLI flags, version output, config loading, server bootstrap |
| `reconcile_test.go` | `maxiofs reconcile --storage-root` reconciles against a storage root outside `<data-dir>/objects` |

### `web/` — 1 test file

| File | Description |
|------|-------------|
| `embed_test.go` | Frontend static file embedding verification |

---

## Frontend Tests

Located in `web/frontend/src/__tests__/`, using **Vitest 4** with **jsdom** environment and **@testing-library/react**.

| File | Description |
|------|-------------|
| `api-token.test.ts` | JWT payload decoding for refresh scheduling |
| `Buckets.test.tsx` | Bucket list, creation, deletion UI |
| `AppLayout.test.tsx` | App shell layout and navigation behavior |
| `Dashboard.test.tsx` | Dashboard metrics rendering, storage distribution |
| `IdentityProviders.test.tsx` | IDP management UI flows |
| `LanguageContext.test.tsx` | Language context and locale switching |
| `Login.test.tsx` | Login form, OAuth redirect, 2FA prompt |
| `Modal.test.tsx` | Modal keeps focus inside the dialog, returns it to the opener, announces itself as a dialog |
| `Users.test.tsx` | User management CRUD UI |
| `useIdleTimer.test.ts` | The idle timer ignores background token refreshes and resets on real activity |

### Configuration

```typescript
// vitest.config.ts
{
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    css: true,
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json', 'html'],
    },
  },
}
```

### Running Frontend Tests

```bash
cd web/frontend
npx vitest run              # Run once
npx vitest                  # Watch mode
npx vitest run --coverage   # With coverage
```

---

## Performance Tests (K6)

Located in `tests/performance/`. Requires [K6](https://k6.io/docs/get-started/installation/) installed.

### Scripts

| Script | Description | VUs | Duration |
|--------|-------------|-----|----------|
| `upload_test.js` | Upload throughput | Ramp to 50 | 2 min |
| `download_test.js` | Download throughput | Sustained 100 | 3 min |
| `mixed_workload.js` | Mixed read/write/list | Spike 25→100 | Variable |
| `common.js` | Shared helpers (auth, bucket setup) | — | — |

### Running

```bash
# Set credentials
export S3_ENDPOINT=http://localhost:8080
export ACCESS_KEY=your-access-key
export SECRET_KEY=your-secret-key

# Individual tests
make perf-test-upload
make perf-test-download
make perf-test-mixed

# Quick smoke test (5 VUs, 30s)
make perf-test-quick

# Stress test (200 VUs, 5 min)
make perf-test-stress

# All tests sequentially (~15 min)
make perf-test-all

# Custom parameters
make perf-test-custom VUS=50 DURATION=2m SCRIPT=upload_test.js
```

---

## Benchmarks

Three packages have dedicated benchmarks:

### Storage Benchmarks

```bash
go test ./internal/storage -bench=. -benchmem -benchtime=3s
```

Tests filesystem throughput for various file sizes (1KB to 100MB), measuring write/read/delete latency.

### Encryption Benchmarks

```bash
go test ./pkg/encryption -bench=. -benchmem -benchtime=3s
```

Tests AES-256-GCM encryption/decryption throughput and memory allocation.

### Metadata Durability Benchmark

```bash
go test ./internal/metadata -run '^$' -bench BenchmarkDurableMetadata -benchtime=3s
```

Parallel object, version and part commits against Pebble. Reports `WAL-sync/op`: below 1 means
concurrent commits share a WAL sync (group commit).

### Profiling

```bash
# Generate CPU profiles
make bench-profile

# Analyze
go tool pprof bench-results/cpu-storage.prof
go tool pprof bench-results/cpu-encryption.prof
```

---

## Test Patterns

### Common Setup

Most tests follow the same pattern: create a temp directory, initialize SQLite + Pebble, and clean up.

```go
func TestSomething(t *testing.T) {
    // Create isolated temp directory (use os.MkdirTemp on Windows — Pebble holds
    // file handles briefly after Close(), so t.TempDir() may fail on cleanup)
    dir, _ := os.MkdirTemp("", "maxiofs-test-*")
    t.Cleanup(func() { os.RemoveAll(dir) }) // ignore error on Windows file locking

    // Initialize SQLite (no CGO needed - uses modernc.org/sqlite)
    db, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
    require.NoError(t, err)
    defer db.Close()

    // Initialize Pebble for metadata
    store, err := metadata.NewPebbleStore(metadata.PebbleOptions{
        DataDir: filepath.Join(dir, "metadata"),
        Logger:  logrus.New(),
    })
    require.NoError(t, err)
    defer store.Close()

    // Run test logic...
}
```

### Race Detection

All tests run with `-race` by default (`make test` and `make test-unit` both include `-race`). Tests for concurrent operations use goroutines with proper synchronization:

```go
func TestConcurrentAccess(t *testing.T) {
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            // concurrent operation
        }()
    }
    wg.Wait()
}
```

### Eventually Pattern

For asynchronous operations (replication, notifications), use `require.Eventually`:

```go
require.Eventually(t, func() bool {
    result, err := checkSomething()
    return err == nil && result.Ready
}, 5*time.Second, 100*time.Millisecond, "expected condition within 5s")
```

### Table-Driven Tests

Most tests use table-driven patterns:

```go
tests := []struct {
    name    string
    input   Input
    want    Output
    wantErr bool
}{
    {"valid input", Input{...}, Output{...}, false},
    {"missing field", Input{}, Output{}, true},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        got, err := DoSomething(tt.input)
        if tt.wantErr {
            require.Error(t, err)
            return
        }
        require.NoError(t, err)
        assert.Equal(t, tt.want, got)
    })
}
```

### Windows Compatibility

Pebble holds OS file handles briefly after `Close()` due to Windows file locking semantics. Tests use `os.MkdirTemp` instead of `t.TempDir()` and ignore cleanup errors:

```go
dir, _ := os.MkdirTemp("", "maxiofs-test-*")
t.Cleanup(func() { os.RemoveAll(dir) }) // ignore error — Pebble may still hold handles
```

---

## Test Distribution Summary

| Area | Files | Key Packages |
|------|-------|-------------|
| Object | 48 | CRUD, versioning, locking, retention, multipart, interrupted-write recovery, quota reservations, write cost |
| Server | 38 | Routes, console API, IDP endpoints, search, IAM/STS, interrupted-write startup gate, HA receive, bucket migration between two nodes |
| Cluster | 37 | Sync, replication, routing, HA quorum, rollback and catch-up, migration, health |
| S3 Compat | 38 | Protocol compliance, ACL, multipart, inventory, pre-signed, handlers |
| Auth | 32 | Users, keys, JWT, S3 sig, TOTP, rate limiting, IAM policies and roles, STS, upgrade conversion |
| Metadata | 17 | Pebble store, search, pagination, tags, versioning, multipart, durability, group commit |
| Storage | 11 | Filesystem, staged commit, layout v2, benchmarks |
| Replication | 8 | Manager, worker, S3 client, end-to-end, status retention |
| Metrics | 6 | Prometheus, history, system, performance |
| Bucket | 5 | CRUD, deletion, policy, integration, name uniqueness |
| IDP | 5 | LDAP, OAuth, crypto, storage |
| Recovery | 5 | Offline recover, reconcile (restore + repair) |
| Logging | 5 | HTTP targets, syslog, manager, persistence |
| Middleware | 4 | Auth, CORS, tracing, body digest |
| Inventory | 4 | Report generation, scheduling, retention |
| Rollback | 2 | Interrupted-write undo: restore, keep, resurrection guard |
| Encryption | 2 | AES-256-GCM, benchmarks |
| Other | 23 | ACL, API, audit, bandwidth, bgwork, cluster auth, config, DB DSN, migrations, KEK, layout, lifecycle, notifications, presigned, settings, share, transfer, cmd, embed |
| **Total Go** | **290** | |
| Frontend | 10 | Dashboard, Login, Users, Buckets, IDP, layout, i18n, token handling, modal focus, idle timer |
| Performance | 4 | Upload, download, mixed, common |
