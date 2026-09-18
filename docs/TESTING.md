# Testing

**Version**: 1.7.0
**Last Updated**: May 18, 2026

---

## Overview

MaxIOFS has **268 Go test files**, **10 frontend test files** (Vitest), and **4 K6 performance scripts**. All Go tests run with the `-race` flag enabled by default. The project uses pure-Go SQLite (`modernc.org/sqlite`) and Pebble, so tests require no external dependencies — no Docker, no databases, no network services.

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

### `internal/auth/` — 32 test files (table below is partial, not yet reconciled)

| File | Description |
|------|-------------|
| `auth_test.go` | Core authentication flows |
| `manager_jwt_secret_test.go` | JWT secret rotation and validation |
| `manager_s3sig_test.go` | S3 Signature V4 verification |
| `manager_simple_test.go` | Basic auth manager operations (CRUD users, keys) |
| `manager_tenant_test.go` | Tenant-scoped auth operations |
| `permissions_test.go` | Permission checks |
| `quota_cluster_test.go` | Cluster-wide quota enforcement |
| `rate_limiter_test.go` | Login rate limiting, account lockout |
| `s3auth_test.go` | S3 authentication flow end-to-end |
| `totp_test.go` | TOTP 2FA enrollment, verification, recovery codes |

`audit_helpers_test.go` no longer exists; the file was removed without updating this table. The
other 22 files in the package (IAM policy evaluation, STS, session policies, bootstrap key —
grown across several releases) are not catalogued here yet.

### `internal/bucket/` — 5 test files

| File | Description |
|------|-------------|
| `bucket_name_uniqueness_test.go` | A bucket name already taken in another tenant is rejected |
| `delete_bucket_test.go` | Bucket deletion with object cleanup |
| `integration_test.go` | Bucket CRUD + versioning integration |
| `manager_test.go` | Bucket manager core operations |
| `policy_evaluation_test.go` | S3 bucket policy evaluation |

### `internal/cluster/` — 33 test files

| File | Description |
|------|-------------|
| `access_key_sync_test.go` | Cross-node access key synchronization |
| `anti_entropy_test.go` | HA anti-entropy worker and reconciliation |
| `bucket_aggregator_test.go` | Multi-node bucket list aggregation |
| `bucket_location_test.go` | Bucket location cache and routing |
| `bucket_permission_sync_test.go` | Bucket permission replication |
| `cache_test.go` | Cluster cache invalidation |
| `circuit_breaker_test.go` | Circuit breaker for unhealthy nodes |
| `dead_node_reconciler_test.go` | Dead-node replica redistribution |
| `deletion_log_test.go` | Tombstone-based deletion sync |
| `group_mapping_sync_test.go` | IDP group mapping synchronization |
| `ha_quorum_test.go` | HA write quorum behavior |
| `ha_read_test.go` | HA read fallback and ordered retry |
| `ha_url_test.go` | Cluster HA endpoint URL handling |
| `health_test.go` | Node health checks (30s intervals) |
| `idp_provider_sync_test.go` | IDP provider configuration sync |
| `leader_test.go` | Leader lease granted when free, refused while held, a stale term is fenced |
| `manager_test.go` | Cluster manager core operations |
| `migration_integration_test.go` | Bucket migration end-to-end |
| `migration_test.go` | Migration unit operations |
| `proxy_bounds_test.go` | Every proxy client entry point is bounded (no unbounded read/timeout) |
| `proxy_client_test.go` | The proxy's HTTP client bounds reachability, not transfer size; keeps TLS and pooling |
| `proxy_content_length_test.go` | A proxied request carries `Content-Length` to the target node |
| `proxy_replay_test.go` | Cluster proxy auth refuses a replay, tampered requests, and another cluster's token |
| `proxy_sts_test.go` | An STS session is preserved across a cluster proxy hop |
| `proxy_test.go` | Request proxying to remote nodes |
| `quota_aggregator_test.go` | Cross-node quota aggregation |
| `quota_integration_test.go` | Quota enforcement in cluster |
| `router_test.go` | Request routing to bucket owner |
| `shared_state_test.go` | TLS state is race-free; the leader manager's `Stop` is idempotent; start doesn't replace the proxy client |
| `stale_reconciler_test.go` | Stale node reconciliation (offline/partition modes) |
| `storage_pressure_test.go` | Storage-pressure health state |
| `tenant_sync_test.go` | Tenant sync across nodes |
| `user_sync_test.go` | User sync across nodes |

### `internal/config/` — 1 test file

| File | Description |
|------|-------------|
| `config_test.go` | Config loading (YAML, env vars, CLI flags), validation, defaults |

### `internal/db/migrations/` — 1 test file

| File | Description |
|------|-------------|
| `migrations_test.go` | SQLite schema migrations, version tracking |

### `internal/idp/` — 5 test files

| File | Description |
|------|-------------|
| `crypto_test.go` | AES-256-GCM encryption for IDP secrets |
| `manager_test.go` | IDP provider CRUD, user authorization |
| `store_test.go` | IDP persistent storage operations |
| `ldap/ldap_test.go` | LDAP bind, search, group resolution |
| `oauth/provider_test.go` | OAuth2/OIDC flow, token exchange, user mapping |

### `internal/inventory/` — 4 test files

| File | Description |
|------|-------------|
| `generator_test.go` | S3 inventory report generation (CSV/Parquet) |
| `manager_test.go` | Inventory schedule management |
| `report_retention_test.go` | Only recent report history is kept |
| `worker_test.go` | Background inventory worker |

### `internal/lifecycle/` — 2 test files

| File | Description |
|------|-------------|
| `expiration_test.go` | Lifecycle expiration behavior |
| `worker_test.go` | Lifecycle rule evaluation and object expiration |

### `internal/logging/` — 5 test files

| File | Description |
|------|-------------|
| `helpers_test.go` | Logging helper functions |
| `http_test.go` | HTTP log target delivery with WaitGroup-tracked goroutines |
| `manager_test.go` | Log manager routing, multi-target dispatch |
| `store_test.go` | Log target persistence |
| `syslog_test.go` | Syslog target output |

### `internal/metadata/` — 17 test files

| File | Description |
|------|-------------|
| `multipart_comprehensive_test.go` | Multipart upload metadata tracking |
| `objects_test.go` | Object metadata storage and retrieval |
| `pebble_test.go` | Pebble store: object CRUD and bucket deletion race coverage |
| `pebble_durability_test.go` | WAL sync loop, `CLEAN_SHUTDOWN` sentinel, unclean-shutdown recovery |
| `pebble_group_commit_test.go` | `PutObject`/`PutObjectVersion` release the bucket lock before the WAL-sync wait, so concurrent writers share one fsync |
| `pebble_group_commit_crash_test.go` | A process killed between the NoSync commit and the WAL sync leaves a rollback copy consistent with the index, not a phantom entry |
| `search_objects_test.go` | Object search by prefix, delimiter, pagination |
| `store_consistency_test.go` | Metadata consistency and counter reconciliation |
| `tags_comprehensive_test.go` | Object and bucket tag operations |
| `versioning_test.go` | Version ID generation, version listing |

Not yet catalogued here: `bucket_concurrency_test.go`, `close_idempotent_test.go`,
`delete_bucket_if_empty_test.go`, `lastmodified_test.go`, `pagination_property_test.go`,
`pebble_pagination_test.go`, `version_delete_test.go`.

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

### `internal/object/` — 38 test files

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
| `multipart_validation_test.go` | Multipart validation and failure handling |
| `raw_replication_test.go` | Replica-side raw ciphertext write, plain and versioned |
| `read_snapshot_test.go` | A reader keeps the generation it opened across a concurrent overwrite |
| `reconcile_serialization_test.go` | Reconcile takes the same per-key lock as normal operations and does not race a concurrent PUT |
| `review_part_crash_faultcheck_test.go` | An acknowledged multipart part survives a process kill during a replacement upload |
| `search_objects_test.go` | Object search and listing |
| `sidecar_loss_test.go` | A lost sidecar never serves ciphertext as if it were the plaintext object |
| `storage_identity_test.go` | `GetObject` refuses to serve ciphertext whose sidecar records no verifiable identity |
| `versioned_prefix_test.go` | Versioned-bucket key collisions: shared prefix, case-only difference, folder marker |
| `versioning_delete_test.go` | Version-aware delete operations |
| `versioning_multipart_metrics_test.go` | Versioning, multipart, and metric counter interaction |

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

### `internal/replication/` — 8 test files

| File | Description |
|------|-------------|
| `adapter_test.go` | `RealObjectAdapter` CopyObject, DeleteObject, GetObjectMetadata |
| `credentials_test.go` | AES-256-GCM credential encryption round-trip, wrong key, legacy plaintext, `deriveCredentialKey` |
| `manager_sync_test.go` | `SyncBucket`, `SyncRule` (lock contention), `processScheduledRules`, `cleanup` |
| `manager_test.go` | Replication rule management |
| `replication_e2e_test.go` | End-to-end replication with `require.Eventually` |
| `s3client_test.go` | S3 replication client behavior |
| `status_retention_test.go` | Old replication status copies are dropped; failures are kept |
| `worker_test.go` | `processItem` (PUT/COPY/DELETE), `handleError`, `completeItem`, `updateReplicationStatus` |

### `internal/rollback/` — 2 test files

| File | Description |
|------|-------------|
| `undo_test.go` | Undoing an interrupted write: restores when the index still matches the retained copy, keeps a committed overwrite (including same-size and multipart ETag shapes), never resurrects a deleted object, resolves only the oldest of repeated retained copies, and is idempotent |
| `review_faultcheck_test.go` | The same decisions under an actual process kill instead of a simulated one |

### `internal/server/` — 35 test files

| File | Description |
|------|-------------|
| `bucket_aggregation_test.go` | Multi-node bucket aggregation API |
| `bucket_quota_handlers_test.go` | A bucket quota is authorized against its own tenant, not `?tenantId=` |
| `bucket_removal_sweep_test.go` | A pending bucket removal is finished at the next start; force-delete records the removal too |
| `capability_guard_test.go` | A global administrator's console S3 access stays read-only across tenants |
| `cluster_raw_replication_test.go` | HA raw-ciphertext receive; rejects a KEK version the receiving node does not hold |
| `console_access_test.go` | Console access and capability checks |
| `console_api_test.go` | Console REST API endpoint tests |
| `console_idp_test.go` | Console IDP management endpoints |
| `console_proxy_test.go` | The console proxy client bounds reachability checks, not transfer size |
| `console_tenant_boundary_test.go` | A global administrator cannot change another tenant's bucket from the console |
| `coordinator_forward_test.go` | An already-forwarded cluster request is not forwarded again; a coordinator write requires cluster auth |
| `download_token_test.go` | A download token is only valid on the two download routes it was minted for |
| `encryption_recovery_handlers_test.go` | Encryption recovery status and recovery-bundle download endpoints |
| `encryption_worker_test.go` | Background encryption pass, its manual-run endpoint, and shutdown tracking |
| `forced_password_change_test.go` | A user under a forced password change can replace it, lifting the obligation |
| `group_handlers_test.go` | A group member add is rejected across a different tenant scope; group delete removes policies atomically |
| `iam_admin_test.go` | Tenant scope separates administrators; who a policy grants administration to |
| `iam_aws_api_test.go` | AWS IAM XML actions route to the IAM handler and require the IAM-manage capability |
| `iam_tenant_isolation_test.go` | A tenant cannot reach another tenant's IAM role; a global administrator reaches every tenant's |
| `ingress_headers_test.go` | Internal cluster headers are stripped from every inbound request |
| `interrupted_writes_test.go` | `Start` refuses to serve traffic when the interrupted-write rollback has unresolved failures |
| `kek_rotation_test.go` | KEK rotation endpoint and cluster KEK-sync receive |
| `kek_shutdown_test.go` | Shutdown waits for a rotating KEK checkpoint; stopping encryption workers cancels the periodic pass |
| `leader_gate_test.go` | Reads, sign-in and cluster traffic are never blocked by the leader gate |
| `lifecycle_coverage_test.go` | Shutdown releases every lifecycle field; no stale entry survives it |
| `multipart_sweep_test.go` | The startup sweep discards parts whose upload record is already gone |
| `pending_password_change_test.go` | A pending forced password change leaves only the intended way out |
| `privilege_boundary_test.go` | `IsGlobalAdmin`/admin-role checks require no tenant and cover both role names |
| `profiling_access_test.go` | `/debug/pprof/*` is reachable by a global administrator and refused without a credential |
| `route_ordering_test.go` | Route priority and matching order |
| `search_api_test.go` | Object search API endpoints |
| `server_test.go` | Server startup, shutdown, configuration |
| `sts_aws_api_test.go` | AWS STS XML actions: missing/unknown action, `GetSessionToken` signature and credential issuance |
| `sts_federation_test.go` | STS federation disabled by default; requires a provider ID; rejects a malformed body |
| `worker_shutdown_test.go` | `goWorker` shutdown waits for every tracked worker and refuses new ones once shutdown starts |

### `internal/settings/` — 1 test file

| File | Description |
|------|-------------|
| `manager_test.go` | Dynamic settings CRUD, defaults, validation |

### `internal/share/` — 1 test file

| File | Description |
|------|-------------|
| `manager_test.go` | Pre-signed share link generation and access |

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

### `pkg/encryption/` — 2 test files

| File | Description |
|------|-------------|
| `encryption_test.go` | AES-256-GCM encrypt/decrypt roundtrip (legacy CTR backward-compat) |
| `encryption_bench_test.go` | Encryption throughput benchmarks |

### `pkg/s3compat/` — 33 test files

| File | Description |
|------|-------------|
| `acl_cascade_test.go` | An ACL grant never overrules an explicit policy `Deny`; a granted user still reads |
| `acl_debug_test.go` | ACL compatibility debugging |
| `acl_security_test.go` | ACL security edge cases |
| `background_jobs_test.go` | A background job keeps the request's values but uses shutdown cancellation, not the request's |
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
| `multipart_ownership_test.go` | A foreign multipart upload is refused; the owner's own upload keeps working |
| `multipart_pagination_test.go` | Multipart upload listing and parts pagination |
| `multipart_retention_test.go` | Default retention applied to an S3 multipart completion |
| `notifications_test.go` | Bucket notification webhook dispatch |
| `objectlock_config_test.go` | Object Lock configuration needs its own permission; refused anonymously |
| `post_presigned_test.go` | POST presigned URL (HTML form upload + policy validation) |
| `presigned_signer_test.go` | Presigned-URL signer resolves a permanent key's owner and an STS principal, denies what it cannot resolve |
| `presigned_test.go` | S3 pre-signed request handling (SigV2 + SigV4) |
| `proxy_fallthrough_test.go` | A request whose body was already consumed is not handled locally; reads may still fall through |
| `s3_test.go` | S3 protocol compatibility tests |
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
| `Users.test.tsx` | User management CRUD UI |

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

Two packages have dedicated benchmarks:

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
| Object | 38 | CRUD, versioning, locking, retention, multipart, interrupted-write recovery |
| Cluster | 33 | Sync, replication, routing, HA, migration, health |
| Auth | 32 | Users, keys, JWT, S3 sig, TOTP, rate limiting, IAM, STS *(table below is partial)* |
| S3 Compat | 33 | Protocol compliance, ACL, multipart, inventory, pre-signed, handlers |
| Server | 35 | Routes, console API, IDP endpoints, search, IAM/STS, interrupted-write startup gate |
| Metadata | 17 | Pebble store, search, tags, versioning, multipart, durability, group commit |
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
| Other | 23 | ACL, API, audit, bandwidth, bgwork, cluster auth, config, migrations, kek, layout, lifecycle, notifications, presigned, settings, share, transfer, cmd, embed |
| **Total Go** | **268** | |

Recovery and Rollback are new packages in 1.7.0. The full per-file breakdown for every area above
except Auth is in the sections below; Auth's is partial (10 of 32 files) — the rest predate this
release and are not yet catalogued in detail.
| Frontend | 8 | Dashboard, Login, Users, Buckets, IDP, layout, i18n, token handling |
| Performance | 4 | Upload, download, mixed, common |
