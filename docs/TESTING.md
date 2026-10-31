# Testing

**Version**: 1.8.0
**Last Updated**: October 13, 2026

---

## Overview

MaxIOFS has **379 Go test files**, **19 frontend test files** (Vitest), and **4 K6 performance scripts**. All Go tests run with the `-race` flag enabled by default. The project uses pure-Go SQLite (`modernc.org/sqlite`) and Pebble, so tests require no external dependencies — no Docker, no databases, no network services.

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

### `internal/api/` — 2 test files

| File | Description |
|------|-------------|
| `handler_security_test.go` | API handler security: auth bypass, injection, header validation |
| `route_names_test.go` | Every S3 route is named after its S3 operation, the name the metrics count requests by |

### `internal/audit/` — 2 test files

| File | Description |
|------|-------------|
| `sqlite_test.go` | Audit log storage, querying, retention, SQLite operations |
| `task_observer_test.go` | Each retention cleanup is told to the task observer |

### `internal/auth/` — 35 test files

| File | Description |
|------|-------------|
| `access_key_last_used_test.go` | An access key's last-used time skips a write for a second already stored and never moves backwards |
| `admin_identity_test.go` | Administrator status comes from the role or from an attached policy |
| `api_rate_limiter_test.go` | The S3 rate limit answers `SlowDown`; zero means no limit; disabled lets everything through |
| `auth_disabled_test.go` | With authentication disabled, console login and S3 credentials resolve to a user that exists |
| `auth_test.go` | Core authentication flows; only failed sign-ins count against an address |
| `console_session_test.go` | A session ends before its tokens expire and another of the user does not; a token from before sessions works until it expires; a provider's logout ends the sessions of its session or of its subject; a two-factor session keeps the provider's grant; another node's sessions never start an ended one again; ended sessions stay ended after a restart; a session ended during a renewal stays ended; in a cluster a session names the node that started it; a renewal in the same second reaches the other nodes; an older copy from another node never replaces a renewal |
| `bucket_policy_decision_test.go` | The bucket policy decides with the caller's policies: a Deny in either refuses, an Allow in either grants; the tenant boundary, a session policy and an unreadable document bound it |
| `bootstrap_key_test.go` | The bootstrap key pair seeds the administrator's first S3 key once; ignored when a key exists, when unset, or when unusable |
| `cluster_convergence_test.go` | Two-node convergence: credential revocation window, immediate deactivation, concurrent policy and bucket-grant writes |
| `download_token_test.go` | A download token opens only the resource it names, is not a session, and is refused for a disabled user |
| `email_test.go` | An email names one user, as its email or its username, in any case: creating or editing a user with another's is refused; users without an email, and users that shared one before, are left alone |
| `iam_console_test.go` | The console's permission screens load what a user's role and grants give; assignable roles exist and are healed on an old database |
| `iam_legacy_schema_test.go` | Databases that applied the IAM migration early: a NOT NULL trust policy column, a missing permission catalogue |
| `iam_policy_repair_test.go` | A policy whose default version is missing is repaired; one with no versions is an error |
| `iam_role_test.go` | IAM roles: trust policy parsing and evaluation, `AssumeRole` checks (session name, duration, tenant), role-session permissions, immediate revocation, ARN parsing |
| `iam_test.go` | IAM policies: parsing and size budget, evaluation (explicit `Deny` wins), version lifecycle, built-ins, inline policies, IAM user create/delete rules |
| `manager_jwt_secret_test.go` | JWT secret rotation and validation; a stored secret is encrypted with the current JWT secret, its key derived once per secret |
| `manager_s3sig_test.go` | S3 Signature V4 verification; the canonical headers are logged one by one |
| `manager_simple_test.go` | Basic auth manager operations (CRUD users, keys) |
| `manager_tenant_test.go` | Tenant-scoped auth operations; storage usage does not touch `updated_at` |
| `permissions_screen_test.go` | Saving a user's permissions stores real grants without baking in inherited ones |
| `permissions_test.go` | Permission checks |
| `policy_bootstrap_test.go` | First-boot conversion to IAM policies: administrators and ordinary users keep console access; an unconverted database is healed; idempotent |
| `policy_equivalence_test.go` | The unified policy set reaches the same verdict as the legacy role, bucket-permission and capability model over every combination |
| `policy_orphan_test.go` | Deleting a user, group or tenant removes its policies; a reused identifier inherits nothing |
| `policy_set_test.go` | The policy engine enforces the account boundary; a super administrator reads across accounts and writes nowhere; an unstated owner is refused; a refusal says why: the account, the bucket policy or the caller's policies |
| `rate_limiter_test.go` | Login rate limiting, account lockout |
| `s3_subresources_test.go` | S3 action resolution for bucket subresources, action precedence and trailing slashes |
| `s3auth_test.go` | S3 authentication flow end-to-end |
| `sigv4_header_values_test.go` | A header sent more than once is signed with its values joined by commas and a run of spaces in a value counts as one, as the AWS SDK's signer signs them |
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

### `internal/bucket/` — 9 test files

| File | Description |
|------|-------------|
| `bucket_name_uniqueness_test.go` | A bucket name already taken in another tenant is rejected |
| `create_entry_test.go` | Creating a bucket whose name has a removal pending finishes the removal first, a bucket from another node included; creating one that exists touches nothing |
| `delete_bucket_test.go` | Bucket deletion with object cleanup |
| `integration_test.go` | Bucket CRUD + versioning integration |
| `lifecycle_xml_test.go` | A lifecycle keeps every condition and action of its rules, stored and read back as XML: filters by prefix, tags and size, expirations by days, date or of delete markers, transitions by days and date, noncurrent transitions and the noncurrent versions kept; a lifecycle S3 would refuse is refused with S3's error; the rules sent set among the stored ones by ID; a prefix read back in the form it was given, a rule stored before as it was |
| `manager_test.go` | Bucket manager core operations; bucket names as S3 takes them |
| `policy_decision_test.go` | A bucket policy that cannot be read denies; a missing policy or bucket leaves the decision to the caller's permissions |
| `policy_evaluation_test.go` | S3 bucket policy evaluation |
| `policy_public_test.go` | A bucket policy is public as AWS defines it: an Allow naming everyone not limited to source ranges of /8 (IPv4) or /32 (IPv6) or narrower |

### `internal/cluster/` — 43 test files

| File | Description |
|------|-------------|
| `access_key_sync_test.go` | Cross-node access key synchronization |
| `anti_entropy_test.go` | HA anti-entropy worker and reconciliation; a node compares with every other healthy node |
| `bucket_permission_sync_test.go` | Bucket permission replication |
| `dead_node_reconciler_test.go` | Dead-node replica redistribution; a drain leaves the node in service and moves its copies, retried at every pass, and takes it out once it holds no data; a drain the others cannot hold is refused; a drained node that does not answer is out at once with a factor above 1 and waited for with a factor of 1; a node being drained does not count for the factor when another node would be marked dead or for the degraded state; the degraded state follows each change of a node's health and is computed when read, and a node out of a cluster is not degraded |
| `drain_progress_test.go` | The time left of a drain is the pace so far applied to what is left, unknown while nothing moved; a pass that finds more raises the total |
| `deletion_log_test.go` | Tombstone-based deletion sync: a deletion keeps its time and the later of two records; each node is sent each deletion once, in order, and again when it changes |
| `deletion_order_test.go` | A deletion is dated after the last change of what it removes; entities of every type and their deletions are ordered by time, the same second keeping the entity; an earlier release's deletion log is repaired once; the IAM payload carries deletion times |
| `deletion_retention_test.go` | A deletion older than the retention is kept until every member was sent it and no catch-up that replays it is pending; a removed node holds nothing back; bucket and row deletions are kept for a member that missed them; a catch-up is tracked until it ends; the deletion sequence never goes back, and a log numbered from its highest entry is sent whole again once |
| `group_mapping_sync_test.go` | IDP group mapping synchronization |
| `ha_bucket_state_test.go` | Bucket deletions are pruned with the deletion log, the later of two deletions winning |
| `ha_row_state_test.go` | Rows of the replicated tables, on the real schema with foreign keys: the order of versions, deletions and content; a stored row keeps the rows that reference it; a report follows its configuration; rows that may not coexist converge on one; refusals; expired shares; a change dated after any version held; a synchronization sends every row and deletion in batches; old versions are forgotten |
| `ha_fanout_lock_test.go` | The legacy (non-raw) HA transfer carries the object's retention and legal hold; without lock state it sends no lock header |
| `ha_metadata_queue_test.go` | A metadata change reaches a healthy replica before the request returns; it is queued for a replica that is down, fails or has changes waiting, never for one that refuses it; the queue is replayed in order, dropping refused changes and stopping at a failure; removed nodes leave no queue |
| `ha_mirror_test.go` | A factor of 2 keeps writing with its peer down and records the miss; a returning node is caught up with what changed since (lock state included), the deletes it missed (never a key it wrote after the delete), and retried when it fails or is down again. The initial sync copies every version oldest first with its ID, delete markers with their time, and lock state; in a suspended bucket, the current object without a version ID last, with its ETag and tags; a bucket it cannot list is left to a catch-up from the start. A copy pulled of a version deleted here is not stored; a key whose versions cannot be listed or sent is left unreconciled |
| `ha_placement_test.go` | A write whose copies fail is not sent to the nodes holding its entry and they miss it; a node that does not take an entry misses the write and is unavailable; the data is read from a node that serves this write of it, passing over one that serves another, and a seek reads from there; outside a cluster data held elsewhere is not found; a ciphertext copy made to name its node carries those locations; the room of a cluster holds each byte on factor different nodes and counts every node but the dead ones; a write, the new copies of a repair and the copies it keeps go to the least full nodes, of their counted room or of their disk |
| `ha_quorum_test.go` | HA write quorum behavior |
| `ha_rollback_lock_test.go` | A write that misses HA quorum (factor 3, both peers failing) removes its local version even under COMPLIANCE retention and a legal hold |
| `ha_url_test.go` | Cluster HA endpoint URL handling |
| `health_test.go` | Node health checks (30s intervals); the nodes are checked when the checker starts |
| `idp_provider_sync_test.go` | IDP provider configuration sync |
| `leader_test.go` | Leader lease granted when free, refused while held, a stale term is fenced; a node without the lease does not name itself the coordinator |
| `manager_test.go` | Cluster manager core operations |
| `membership_test.go` | A node and its removal are ordered by time, the same second keeping the node; a removed node is not added back; edits and drains are stamped after the last change; a node dead by its probes is back when it answers and compared on every write, a drained one stays dead; a drain on another node is taken here: the copies of the drained nodes are moved, another node is out of service once it says it holds no data, and a node does not take itself out; a drain is refused below the replication factor; joining takes the cluster's membership |
| `parity_protection_test.go` | Parity is off until turned on; its layout follows the nodes that hold data (one parity shard up to four, two from five, dead and drained nodes left out); a delete's quorum follows the parity; an empty write and a cluster too small for shards keep whole copies; room for data in shards; a write needs another node that takes data; a node is not marked dead nor drained when fewer than three would take data; degraded when fewer nodes take data than a write spans; the writes are repaired when the layout changes, once every node that holds data takes it |
| `placement_repair_test.go` | Removing a member announces it once: a removal received again, or of a node that is not a member, does not |
| `multipart_routing_test.go` | A multipart upload is made on the node its ID names, whatever the factor; with an ID naming no node, outside a cluster or on its own node, where the request is; a node gone from the cluster is reported; the other nodes' uploads are asked for, a node that does not answer left out |
| `rebalance_test.go` | A class within 10 points of its fill moves nothing; past it each node is brought to it by its own room; a node taking no data takes nothing; the draws of many writes add up to the plan, the same on every node; a write larger than what a node gives or takes stays; a class is spread only with every node in service, none drained, each room known |
| `proxy_bounds_test.go` | Every proxy client entry point is bounded (no unbounded read/timeout) |
| `proxy_client_test.go` | The proxy's HTTP client bounds reachability, not transfer size; keeps TLS and pooling |
| `proxy_content_length_test.go` | A proxied request carries `Content-Length` to the target node |
| `proxy_replay_test.go` | Cluster proxy auth refuses a replay, tampered requests, and another cluster's token |
| `proxy_sts_test.go` | An STS session is preserved across a cluster proxy hop |
| `proxy_test.go` | Request proxying to remote nodes |
| `shared_state_test.go` | TLS state is race-free; the leader manager's `Stop` is idempotent; start doesn't replace the proxy client |
| `mark_dead_busy_test.go` | A node is marked dead while another writer holds the database; marking a node already dead changes nothing and tells no one |
| `sync_held_data_test.go` | A node sent a write it already holds gets its entry, not its data again; without knowing, or holding another write, it gets the data |
| `sync_schema_test.go` | A default no one changed is dated at the epoch, a default seeded with the time of its seeding too; a changed setting keeps the time of its change |
| `storage_classes_test.go` | The classes a cluster keeps, set, changed and deleted, its nodes back in STANDARD; stored classes it cannot keep passed over; a node leaves its class only when the class keeps its protection, a node down or drained counting for none; each class has its own layout, quorum, write target and acceptance of writes; the room of the cluster is STANDARD's, each class's state with its room and tolerated failures; the writes are repaired when the nodes or protection of a class change; a class below its protection makes the cluster degraded by name; a drain keeps the nodes of the most demanding class; the class of a node travels with its row; the placement settings follow the classes |
| `storage_pressure_test.go` | Storage-pressure health state; a node under storage pressure stays in service, takes no new data and misses no write, and is caught up when it answers again under pressure; a slow node is in service and takes data, counts for writes, dead marking and the degraded state, a slow and full one is under pressure; a drained node's health check notes the end of its drain, and a later one clears it |
| `task_observer_test.go` | The cluster's background tasks are told to the observer set on the manager |
| `tenant_sync_test.go` | Tenant sync across nodes, with timestamps as the tenants table stores them; usage is not part of the checksum |
| `user_sync_test.go` | User sync across nodes, two-factor authentication with the user |
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

### `internal/erasure/` — 1 test file

| File | Description |
|------|-------------|
| `erasure_test.go` | The layout of a cluster of nodes; the size of a shard; an object read back with any parity's worth of shards missing, whatever its size against the stripes; too few shards refused; a shard failing mid-read replaced from its stripe; ranges and Seeks, a Seek while reading reopening the shards; each shard opened once in a sequential read; a missing shard rebuilt byte for byte without reading its stale bytes; a failed destination written no more; a short object refused; `Ready` reads the first stripe ahead |

### `internal/idp/` — 13 test files

| File | Description |
|------|-------------|
| `crypto_test.go` | AES-256-GCM encryption for IDP secrets |
| `manager_test.go` | IDP provider CRUD, user authorization |
| `provider_cache_test.go` | A provider changed or deleted through the database, as another node's change arrives, is rebuilt or refused instead of served from the cache |
| `reencrypt_test.go` | Bind passwords and client secrets move to a new secret; the rest of the configuration is kept; what no secret decrypts is named and left |
| `store_test.go` | IDP persistent storage operations |
| `ldap/ldap_test.go` | LDAP bind, search, group resolution |
| `ldap/directory_test.go` | A directory for the tests: simple binds, searches with a size limit, `noSuchObject` for a base that does not exist, and one that takes connections and never answers |
| `ldap/connection_test.go` | The connection test checks the connection, StartTLS, the service account, the users and the groups, names the step that fails and stops at a step the next ones need: a wrong or missing password, no TLS, a base that does not exist, a filter of nobody or broken, no username attribute, no group found, a directory not there |
| `ldap/provider_directory_test.go` | The connection test passes with more users than it reads; a search gives the first matches beyond its limit; a user signs in by DN or by name and is that user; a directory that does not answer, or is not there, is unavailable within the timeouts; a sign-in gives the user's groups found by member and its DN; a revalidation ends the sign-in of a user gone or disabled, and a directory that does not answer ends nothing |
| `oauth/provider_test.go` | OAuth2/OIDC flow, token exchange, user mapping; a configuration without a field a sign-in needs fails the connection test naming it |
| `oauth/connection_test.go` | The connection test checks the OpenID configuration and keys, the sign-in page for the client and redirect URI, the client secret by an invented code (`invalid_grant` takes it; `invalid_client` and `unauthorized_client` refuse it, in the header or the form) and the userinfo endpoint; a provider that does not answer, or is not there, fails each check within the timeout |
| `oauth/oidc_test.go` | An OpenID Connect sign-in keeps the subject and session of its checked ID token and the refresh token, and refuses an ID token for another client; a revalidation renews the grant and the groups, ends on a refused refresh or userinfo, and ends nothing for a failing provider or a refused client; a back-channel logout token is taken as the standard says |
| `oauth/provider_unavailable_test.go` | A provider that does not answer at its token or userinfo endpoint, or is not there, is unavailable within the timeout; one that refuses the code is not |

### `internal/inventory/` — 6 test files

| File | Description |
|------|-------------|
| `change_observer_test.go` | Every configuration and report written or deleted is reported, trimmed reports too; the worker runs only when its gate says |
| `generator_test.go` | S3 inventory report generation (CSV/JSON), page after page, stored through the object writer |
| `manager_test.go` | Inventory schedule management; a completed report keeps its path |
| `report_retention_test.go` | Only recent report history is kept |
| `task_observer_test.go` | Each inventory pass is told to the task observer; a node the gate holds back runs none |
| `worker_test.go` | Background inventory worker |

### `internal/kek/` — 2 test files

| File | Description |
|------|-------------|
| `bundle_test.go` | Recovery bundle export and decryption: round trip, wrong or short passphrase, garbage input, several KEK versions, download tracking |
| `store_test.go` | KEK bootstrap (database key wins over config, config seeds version 1, otherwise generated), lookup by version, cluster key creation and adoption, keys already held adopted without a write or a log line, keys of a node that missed a rotation leave the current key, rotation, concurrent rotations |

### `internal/layout/` — 1 test file

| File | Description |
|------|-------------|
| `migrate_test.go` | Layout v1 → v2 migration: moves every object and version, idempotent, resumes after an interruption, dry run, refuses duplicate or tenant-like bucket names, reports inconsistencies, prunes the old tree once per directory, purges implicit folder objects |

### `internal/lifecycle/` — 7 test files

| File | Description |
|------|-------------|
| `expiration_gate_test.go` | A node the gate stops expires nothing and still aborts its own multipart uploads; the pass deletes through the manager set after construction |
| `expiration_test.go` | Lifecycle expiration behavior |
| `noncurrent_test.go` | A version is noncurrent from when the next one was written; the newest noncurrent versions a rule keeps are not expired, a delete marker not one of them; the filter limits the noncurrent versions expired |
| `stored_rules_test.go` | Over real stores: a rule removing delete markers alone expires no object; an expiration on a date waits for it; a rule expires only the objects its filter takes, by prefix, every tag and size |
| `task_observer_test.go` | Each lifecycle pass is told to the task observer, failed when the buckets cannot be listed |
| `transition_test.go` | Over real stores: a transition moves the objects its filter takes to the coldest class due, as days pass, a disabled rule none; a transition on a date waits for it; an object only moves to a colder class; an expiration due wins over a transition; noncurrent versions move the days after a later version replaced them, but for the newest kept |
| `worker_test.go` | Lifecycle rule evaluation and object expiration |

### `internal/logging/` — 5 test files

| File | Description |
|------|-------------|
| `helpers_test.go` | Logging helper functions |
| `http_test.go` | HTTP log target delivery with WaitGroup-tracked goroutines |
| `manager_test.go` | Log manager routing, multi-target dispatch; JSON lines keep `&`, `<` and `>` |
| `store_test.go` | Log target persistence |
| `syslog_test.go` | Syslog target output |

### `internal/metadata/` — 19 test files

| File | Description |
|------|-------------|
| `bucket_concurrency_test.go` | A bucket configuration update does not lose concurrent metric increments |
| `bucket_replica_test.go` | A bucket from another node is stored with its times when newer, keeping this node's usage; an older or equal version changes nothing; a name another tenant holds is refused. Usage does not change `UpdatedAt`; a change here is later than the version it replaces, from a node whose clock is ahead too |
| `close_idempotent_test.go` | `Close` is idempotent |
| `delete_bucket_if_empty_test.go` | A bucket holding a folder-marker object is not empty |
| `finished_upload_test.go` | Finishing an upload removes it and its parts and keeps what answers it again, in one commit; what is kept goes after a day |
| `lastmodified_test.go` | `PutObject` keeps a caller-provided `LastModified` and stamps the current time only when it is unset |
| `multipart_comprehensive_test.go` | Multipart upload metadata tracking |
| `objects_test.go` | Object metadata storage and retrieval; a version kept latest beside a current null object is listed as no longer the latest |
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

### `internal/metrics/` — 5 test files

| File | Description |
|------|-------------|
| `badger_history_test.go` | Metrics history stored in Pebble (via RawKVStore) |
| `history_test.go` | Metrics history aggregation |
| `manager_test.go` | S3 requests counted by operation, not path; server errors (5xx) and refusals (4xx) apart, each by S3 code; a refused request names no bucket; an error total kept from before refusals were apart is not restored; bucket usage, cache and retention read and replaced; sign-ins and every recorder |
| `performance_test.go` | Performance metrics tracking |
| `system_metrics_test.go` | System resource metrics (CPU, memory, disk) |

### `internal/middleware/` — 4 test files

| File | Description |
|------|-------------|
| `body_digest_test.go` | A verified request body passes declared content through and rejects substituted, truncated, or unknown-algorithm content |
| `cluster_auth_test.go` | HMAC inter-node authentication middleware |
| `middleware_test.go` | Auth, CORS, rate limit middleware chain |
| `tracing_test.go` | Request tracing and request ID propagation; an operation's errors are its 5xx answers, not its refusals |

### `internal/notifications/` — 2 test files

| File | Description |
|------|-------------|
| `helpers_test.go` | Notification helper functions |
| `manager_test.go` | SSE notification delivery, client management |

### `internal/object/` — 56 test files

| File | Description |
|------|-------------|
| `accounting_property_test.go` | Bucket size/count accounting agrees with the object set after any sequence of operations, across versioning states too; a replaced null version gives its bytes back |
| `adapter_test.go` | Storage adapter abstraction |
| `backup_restore_test.go` | A retained overwrite/multipart backup survives a failed restore and is removed after a successful one |
| `delete_key_fidelity_test.go` | `DeleteObject` deletes only the key it was given |
| `encryption_migration_test.go` | Background worker converts plaintext to envelope encryption; multipart ETag preserved; a KEK rotation re-wraps the key of each shard held |
| `faultcheck_test.go` | Kills a subprocess mid-write (before/after data, before/after metadata) and checks what the next start recovers; also covers the retained-backup rollback path and the cost of an overwrite's backup copy |
| `folder_marker_test.go` | A folder marker and a same-named object coexist (layout v2) |
| `integration_test.go` | Full object lifecycle (put → get → delete) |
| `key_acls_test.go` | An object does not inherit the ACL of the one before it at the key; ACLs kept per key are moved into their objects once |
| `lock_sidecar_test.go` | The sidecar keeps a version's Object Lock state through multipart completion and a metadata update, which cannot change it; a raw copy keeps its entry's state; the encryption pass writes it for versions stored before |
| `key_serialization_test.go` | Every key-mutating call serializes on the per-key lock; a PUT still receiving its data holds up no read |
| `manager_coverage_test.go` | Manager edge cases for coverage; a retention in force only grows stricter |
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
| `multipart_finish_test.go` | A completion's `If-Match` and `If-None-Match` are decided under the key's lock, and a refused one leaves the upload; a finished upload is kept with its result and part list, and aborting it again does nothing; a completion is logged with its bucket, key, parts and version; it names its version before it waits for the key's lock, and the same completion sent again is told it |
| `multipart_lock_test.go` | Independent parts upload concurrently; conflicting operations exclude each other |
| `multipart_part_race_test.go` | A part cannot be replaced while completion is assembling it |
| `multipart_part_replace_test.go` | Replacing an acknowledged part retains it until the new one commits, and restores it on failure |
| `multipart_race_test.go` | Concurrent multipart upload race conditions |
| `multipart_retention_test.go` | Default retention on multipart completion; a lookup failure preserves the upload |
| `multipart_stream_test.go` | A multipart completion failing at any stage of the stream (open, read, short part, close, cancel, encryption, commit, lookup) keeps the previous object, closes every part reader and stays retryable; empty parts complete |
| `multipart_validation_test.go` | Multipart validation and failure handling |
| `null_version_test.go` | The object written without a version is kept as the null version when a version replaces it or a delete marker goes over it; a suspended write replaces it; it is deleted by its ID, current or not; a suspended delete is a null delete marker, and an older copy of one keeps a null version written after it |
| `raw_replication_test.go` | Replica-side raw ciphertext write, plain and versioned |
| `raw_replication_rollback_test.go` | A failed raw replica overwrite restores the previous data and sidecar, including existing versions |
| `read_snapshot_test.go` | A reader keeps the generation it opened across a concurrent overwrite |
| `replica_metadata_test.go` | The entry of an object whose data other nodes hold answers HEAD and listings and names them to a GET, removes a file kept for the key, and is not replaced by an earlier entry; versions land by time; a node serves its own file of an entry naming it, and every file outside a cluster; a lost copy names the other holders; user metadata changes in the entry; an earlier copy, plain or ciphertext, does not replace a later write; integrity is not checked here for data held elsewhere; the locations of a write or a version change only for that write, here and on its current entry, and a node they no longer name removes its file; the newest locations of a write, or of an older version, are kept against older changes, entries and copies, plain or ciphertext, one number going to the larger list; a copy, plain or ciphertext, is stored only where the newest locations name this node; a dropped entry is removed whatever its lock; a multipart upload completed over a write whose data other nodes hold replaces it |
| `replica_versions_test.go` | Versions copied from another node, raw or not, land in order whatever order they arrive in: an older copy or delete marker never replaces the latest, a repeated copy is not counted twice |
| `replica_copy_test.go` | A copy into a bucket whose versioning was suspended is stored as the version it copies, never replaces the current object (not even one of the same second), and a copied delete marker stays one; a copy carries its ETag, tags, ACL and restore state and is refused when its bytes do not match; changing tags or ACL keeps the restore status; a write while suspended keeps the bytes of the version it covers |
| `reconcile_serialization_test.go` | Reconcile takes the same per-key lock as normal operations and does not race a concurrent PUT |
| `review_part_crash_faultcheck_test.go` | An acknowledged multipart part survives a process kill during a replacement upload |
| `storage_class_test.go` | A write moves to another class as the write named, not another of its key nor a delete marker, its data where it was; a version moves alone; a change of placement carries the class and an entry of an older placement keeps the newer one's, a copy too; a multipart upload completes in the class it was started with; every version listed has its class |
| `shards_test.go` | A shard is stored encrypted beside its object's file and read back from any offset, only as the shard of its write and layout; an entry in shards is read from the nodes holding them, this node holding the shard it is named for; a shard follows the placement of its write (a staged one kept until its placement lands, one of an earlier write removed); deletes, overwrites, multipart completions and raw copies remove the shards of the writes they remove; a whole copy of a write in shards is not stored, or keeps the shard until its placement lands; the integrity check of a shard, a corrupt one removed |
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
| `write_quota_test.go` | Quota reservations: concurrent writers never over-admit and all fitting writers are admitted; a finished or refused write is not counted twice; a slow tenant check blocks nobody; freeing space is allowed over the limit; a versioned overwrite, and a suspended write over a kept version, are charged in full |
| `write_rollback_test.go` | Undoing a write deletes the protected version it created; a client delete of that version is still refused. Multipart uploads keep the Object Lock headers of their creation, validated then; user metadata stays user metadata — it cannot set the lock state, the Content-Type, the canned ACL or sidecar fields — including for uploads created before the upgrade |
| `write_safety_test.go` | Reservations are released on success and failure; a failed write stays retryable; quota is accounted per bucket; Object Lock headers are applied at the first commit; a failed bucket lookup on delete preserves the object |

### `internal/presigned/` — 1 test file

| File | Description |
|------|-------------|
| `presigned_test.go` | Pre-signed URL generation and validation |

### `internal/recovery/` — 7 test files

| File | Description |
|------|-------------|
| `lock_recovery_test.go` | A rebuilt store gets back the retention a write set, an extended retention, a legal hold on and one turned off |
| `reconcile_test.go` | Reconcile rebuilds an index entry missing entirely from an object left on disk; a shard is passed over by reconcile and the offline rebuild |
| `reconcile_repair_test.go` | Reconcile corrects an entry describing bytes an overwrite already replaced, from the sidecar's own identity |
| `recovery_test.go` | Offline `maxiofs recover` — rebuilds a fresh Pebble store from the object tree alone, with the headers each object kept |
| `restore_keks_test.go` | Restoring KEKs from a recovery bundle into the auth database |
| `resume_test.go` | A recovery stopped at any step, and stopped again as it continues, ends as one that never stopped; it is continued only with the same data directory, bundle and files |
| `review_faultcheck_test.go` | Reconcile takes the same per-key lock as a live overwrite and does not race it; a same-size same-second overwrite is still detected |

### `internal/replication/` — 11 test files

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
| `task_observer_test.go` | Each object a worker processes is told to the task observer, failed when it could not be replicated |
| `worker_test.go` | `processItem` (PUT/COPY/DELETE), `handleError`, `completeItem`, `updateReplicationStatus` |

### `internal/rollback/` — 2 test files

| File | Description |
|------|-------------|
| `undo_test.go` | Undoing an interrupted write: restores when the index still matches the retained copy, keeps a committed overwrite (including same-size and multipart ETag shapes), never resurrects a deleted object, resolves only the oldest of repeated retained copies, and is idempotent |
| `review_faultcheck_test.go` | The same decisions under an actual process kill instead of a simulated one |

### `internal/server/` — 68 test files

| File | Description |
|------|-------------|
| `account_unlock_test.go` | An administrator's unlock, served by the coordinator, unlocks the account on the node where failed sign-ins locked it; a change of a user reaches the other node at once, a new password made within the second the user was made with it |
| `block_public_acls_test.go` | The console refuses a public bucket or object ACL while the bucket's Block Public Access blocks public ACLs; a private one is set |
| `bucket_aggregation_test.go` | Multi-node bucket aggregation API |
| `bucket_policy_console_test.go` | The console decides with the bucket policy: a Deny wins over the user's permissions and over the global administrator, an Allow grants a user without permissions; saving validates the policy and applies `BlockPublicPolicy` |
| `bucket_quota_handlers_test.go` | A bucket quota is authorized against its own tenant, not `?tenantId=` |
| `bucket_ownership_test.go` | A bucket a client deletes, forced or not, takes its shares, rules, inventory, permissions, policies, notification configuration and integrity scans: a share link of the old bucket opens nothing in a new one; a new bucket starts without what a former one left; the state of deleted buckets is dropped at start; with a factor of 2 on every node |
| `bucket_removal_sweep_test.go` | A pending bucket removal is finished at the next start; force-delete records the removal too; the directory of a bucket created since is kept |
| `capability_guard_test.go` | A global administrator's console S3 access stays read-only across tenants |
| `cluster_nodes_helpers_test.go` | Helpers: a whole server on its own data directory, the data an object manager reads, a bucket copy hidden as a migration of an earlier version left it |
| `cluster_lists_test.go` | With every node holding every bucket, a bucket is listed once in the console and to S3 clients; the degraded reason another node sends is not taken; a read is served by the node that receives it; a node installed after a setting changed takes the change, and its defaults replace no change on the others |
| `cluster_raw_replication_test.go` | HA raw-ciphertext receive; rejects a KEK version the receiving node does not hold; a raw copy of a version deleted here, or of a write before a deletion of its key, is not stored |
| `cluster_replica_lock_test.go` | The HA replica receive keeps the primary's lock state as sent: no bucket default added, no date refused; an older primary's transfer gets the bucket default. A copy served to a peer carries the lock state. A metadata change that cannot apply answers 404, 400 or 409. A replicated delete marker older than the latest version leaves the key visible; a copy sent with headers keeps the time its write was made, an object and a delete marker alike; a copy, whole or entry only, of a write before this node deleted the key is not stored, one of the second of the deletion or later is; an entry names the nodes holding its data; a copy's locations and their number are kept when received and sent when served |
| `console_access_test.go` | Console access and capability checks |
| `console_api_test.go` | Console REST API endpoint tests |
| `console_idp_test.go` | Console IDP management endpoints; a provider made or changed is answered with its secrets masked; the connection test answers each check, and the first that failed as its message |
| `idp_isolation_test.go` | A provider signs in its own users only, by the console and by STS, and provisions with its own mappings; a local user's email is not provisioned; a tenant administrator reaches its tenant's providers and their mappings only; a tenant's provider maps, imports and syncs into its tenant, with the email, and not an email another user has; the console answers `409` |
| `idp_sessions_test.go` | A provider's session follows the provider at each renewal (role from the groups, kept through an outage, ended on a refused refresh at once); renewals of one session that arrive together keep it with a provider that rotates refresh tokens; in a cluster the node that started a session renews it, a node the session has not reached passes the renewal there, another node keeps it without the provider while that one does not answer or is seen down, and the coordinator renews the sessions of a removed node; a user out of its groups loses its session and a user an administrator made does not; a back-channel logout ends the session; a sign-out ends the session; an administrator marks a user as following its groups; sessions reach every node of a cluster |
| `idp_sign_in_test.go` | A user whose ID is not its name signs in; a sign-in its directory cannot answer is `503` and locks neither the account nor the address; one its SSO provider cannot answer goes back to the login page with `provider_unavailable` |
| `login_rate_limit_test.go` | Only failed sign-ins hold an address up; the security settings reach the sign-in of a server as it starts |
| `console_tenant_boundary_test.go` | A global administrator cannot change another tenant's bucket from the console |
| `coordinator_forward_test.go` | An already-forwarded cluster request is not forwarded again; a coordinator write requires cluster auth; the coordinator sees the user's address and TLS |
| `deletion_semantics_test.go` | A received deletion keeps its time; a re-created bucket keeps its owner's policy; a key written again reaches the node that missed it; synchronized copies and IAM deletions are ordered against deletions; local deletions are dated after the last change and record the IAM policies of deleted users, tenants and groups; a deletion from the log removes the copy of every kind of entity with what goes with it; a revoked session reaches the other node with its time |
| `download_token_test.go` | A download token is only valid on the two download routes it was minted for; a folder ZIP names no entry out of its folder (`..`, a leading `/`, a backslash, a drive letter) and names the archive by the folder or the bucket |
| `encryption_recovery_handlers_test.go` | Encryption recovery status and recovery-bundle download endpoints; in a cluster every node shows the download, and a rotation asks every node for a new bundle |
| `encryption_secret_test.go` | Stored credentials survive a restart with the default configuration; a node joining a cluster (real join) takes its encryption secret; the coordinator gives its secret and no other node can; the Nodes list shows a differing secret; the settings never show the JWT secret |
| `encryption_worker_test.go` | Background encryption pass, its manual-run endpoint, and shutdown tracking |
| `forced_password_change_test.go` | A user under a forced password change can replace it, lifting the obligation |
| `group_handlers_test.go` | A group member add is rejected across a different tenant scope; group delete removes policies atomically |
| `ha_cluster_test.go` | Complete nodes with their S3 APIs and AWS SDK clients: a multipart upload's parts, listings, completion and abort reach the node it was started on from any node; a node that does not answer gives `503`, one gone from the cluster `NoSuchUpload`; an upload named after no node is made where it is. Three nodes with a factor of 2: a write is held by its node and the least full node and listed, read (a range alone) and answered for HEAD on every node; a read tries each holder and answers `503` with none; a delete reaches every node; an overwrite moves its data; every version is listed and read everywhere; tags set where the data is not; a node serves only the data it holds, and its console finds an object whose holders do not answer. A node that missed writes, or lost its data, is sent what it holds; an initial sync sends each node what it holds; a later write reaches a node not holding it as an entry. The copies of a removed or dead node are made again, a lower factor drops the extra copies, a higher one adds those it needs after the request is answered and the first sync's entry, arriving after, does not undo it, a full cycle repairs them, and a factor of 1 keeps one copy. A node under storage pressure passes its copy (object, version, multipart) to one node, to the next when one does not take it, keeps it when none does or no other node holds the entry only, and finishes even when the client goes away; a copy whose answer was lost, after a move or a repair, is removed. Every node reports to Veeam the room of the cluster and what a tenant uses on every node. The usage of every bucket and tenant is the same on every node, as counted and as recounted from the entries. A factor raised from 1 on one node reaches every node, each object copied once. With a factor of 1 a write is held by the node that takes it alone, listed, read and deleted from every node; a node under storage pressure passes it to the least full node, a slow one keeps it. A cluster of factor 1 from a version that kept each bucket on one node places each node's writes once, at start, and sends every node the buckets and entries without their data; a first sync and a catch-up place them before sending; above a factor of 1 they are not placed; objects that name no node are given their holders by the first of them, a node with another write not counted, and wait while a node does not answer. A node under storage pressure takes entries, changes and deletes but no new copy, and is caught up; a node that leaves keeps only the writes it holds; a dead node that comes back takes the locations changed meanwhile, and a later cycle sends no entry; with a factor of 3 on four nodes a dead holder's copy is made on the fourth and dropped by the holder back; a node that joins is given its share of the copies, nothing moved while a node is out of service or once spread; the copies are spread by the room of each node; new writes follow the room the copy repair counted; a drained node's objects are moved with factors 1 and 2 and read from every node, it then answers no S3 request, its health check included, and the others take it out of service; a node being drained takes no copy and passes on the writes it takes; a drained node keeps the copy no node takes, is not empty while a pass fails, and a write it holds with another node keeps the other without a copy; a drained node counts what it holds and moved, and the other nodes take it from its health check and show it; the degraded banner follows a change of a node's health and shows the nodes as they are; a node that missed a write is sent its copy once; a retention change bypassing GOVERNANCE reaches every node, COMPLIANCE kept on each |
| `ha_null_version_test.go` | The null version is kept and read on every node, in whole copies and in shards; a null version written after a delete of one reaches a node that missed it, and the delete sent after it leaves it |
| `ha_object_headers_test.go` | An object keeps on every node, in whole copies or in shards, the headers it was written with and, written in parts, its tags and each of its parts; a copy served to a peer that catches up carries the headers |
| `ha_storage_class_test.go` | Storage classes between complete nodes with S3 clients: the writes of a class, by PUT, multipart upload or copy, are held by its nodes, a write of no class or of a class not kept by STANDARD's; a class with too few nodes keeps its writes elsewhere, then moves them to its nodes; a lifecycle transition moves a write to the nodes of its class, once, and to a class with parity cuts it in shards; a node put in another class moves its data off; a deleted class keeps its objects; the console keeps the classes and puts nodes in them, the HA page names each node's class; a delete takes the quorum of its class; the HA page sets STANDARD's protection by its nodes |
| `ha_parity_test.go` | Parity between complete nodes with S3 clients: a write cut over three nodes in 2+1 and read from any, a range alone; read with any one node down and not with two, and from a holder the reader holds out of service when the others are too few, HEAD still answering; five nodes cut in 3+2 and read with two down; a shard not taken leaves the write in the others, or in whole copies with the shards taken removed; a write with a node down kept in two whole copies, refused with two down and not kept; an empty object in copies; multipart, versions and deletes; an overwrite replaces the shards; a delete on five nodes needs another node; the console turns parity on from three nodes, shows the layout, room and tolerated failures, a factor turns it off; Veeam sees the room for shards; a write landing nowhere leaves no shard; a read passes over a node out of service; a shard of a deleted write or version is not stored |
| `ha_parity_repair_test.go` | Turning parity on cuts the copies in shards and off joins them; the shard of a removed node made again at the same index; a drained node's shard moved, copied from it with another node down; a fifth node raises the parity, no node keeping its index; a sixth node widens the writes; a dead node's write is cut over the others, and over every node when it is back; a node beyond the shards of the writes is given its share; a lost shard made again by one node; a node that missed a write's shard and entry sent both in one catch-up, also when the node that makes the shard sends the entry; a node out of a write's shards sent its entry alone; a node takes the entry of a later write in shards; a node that leaves drops its shards; a write whose shards no longer fit is cut again; a write that cannot be cut keeps its copies, the staged shards removed; a node that missed a change of a write's placement reads the write by the placement it takes from the others |
| `ha_bucket_test.go` | Between two complete nodes with a replication factor of 2: every bucket change (configuration, ACL, deletion) reaches the other node in both directions with the same version; usage stays each node's; a node that was down is sent its buckets, then their objects; a new replica is sent the buckets first; deletions keep newer data and data under a legal hold; refusals; lifecycle expires on the coordinator only; a deletion removes a write made before it in its second. An initial sync that fails some objects leaves them to the catch-up and says how many; a joining node is sent the objects; a catch-up sends every version the node missed, once, and its copies keep the tags; a version or delete marker deleted on a node is not stored again by a copy |
| `ha_rows_test.go` | Between two complete nodes with a replication factor of 2: shares, replication rules, inventory configurations and reports reach the other node in both directions; a node that was down is caught up, rows from before the upgrade included; the coordinator's inventory report, its file and run reach the other node; a node that is not the coordinator writes no report; with a factor of 1 changes are dated and sent too; an undated change is missed by every node |
| `iam_admin_test.go` | Tenant scope separates administrators; who a policy grants administration to |
| `iam_aws_api_test.go` | AWS IAM XML actions route to the IAM handler and require the IAM-manage capability |
| `iam_tenant_isolation_test.go` | A tenant cannot reach another tenant's IAM role; a global administrator reaches every tenant's |
| `ingress_headers_test.go` | Internal cluster headers are stripped from every inbound request |
| `interrupted_writes_test.go` | `Start` refuses to serve traffic when the interrupted-write rollback has unresolved failures |
| `kek_rotation_test.go` | KEK rotation endpoint and cluster KEK-sync receive |
| `kek_shutdown_test.go` | Shutdown waits for a rotating KEK checkpoint; stopping encryption workers cancels the periodic pass |
| `leader_gate_test.go` | Reads, sign-in (on the router's real paths) and cluster traffic are never blocked by the leader gate; two-factor settings are; a bucket's settings and objects need no coordinator, its permissions do |
| `lifecycle_coverage_test.go` | Shutdown releases every lifecycle field; no stale entry survives it |
| `multipart_sweep_test.go` | The startup sweep discards parts whose upload record is already gone |
| `node_lifecycle_test.go` | A removed node is removed on every node and not added back, and leaves when told; a leaving node is removed from the others; a drain reaches every node, which takes the node out of service once it says it holds no data; a cluster joined at runtime replicates writes; outside a cluster objects are only stored; only a node without data joins a cluster, every kind of data refusing it with what it holds; a node learned from another node's membership is checked at once; the list of nodes names the coordinator, and none while the nodes elect one |
| `oauth_code_any_node_test.go` | The code of an OAuth sign-in is exchanged on another node of the cluster, once |
| `oauth_two_factor_test.go` | An OAuth sign-in of a user with two-factor authentication: the exchanged code carries the second step's token, not a session, and the user's code signs it in; without 2FA the exchange gives the session |
| `object_extra_handlers_status_test.go` | A console write refused by a quota answers 403; a failure to check the quota does not. A console upload's Object Lock headers need their own permission; a tenant without a storage limit can upload |
| `partition_test.go` | Two nodes cut off for longer than deletions are kept, both serving, converge: deleted objects, users, buckets and shares stay deleted, writes on either side reach the other, and another round of every synchronization brings nothing back; a bucket deleted on one side and written to on the other is kept on both with the later writes only, and sent to the other node at once; a rewrite in the second of its deletion, while a node is away, is kept on every node; a delete marker reaches the other node with the time it was written |
| `pending_password_change_test.go` | A pending forced password change leaves only the intended way out |
| `privilege_boundary_test.go` | `IsGlobalAdmin`/admin-role checks require no tenant and cover both role names |
| `profiling_access_test.go` | `/debug/pprof/*` is reachable by a global administrator and refused without a credential |
| `prometheus_metrics_test.go` | Every metric family reaches Prometheus once the server does what it counts, through real S3, console and background traffic; no label carries an object key |
| `replication_adapter_test.go` | Replication reads and lists a tenant's bucket under its tenant, page after page |
| `repair_rerun_test.go` | A factor changed while a repair runs is acted on once that repair ends |
| `request_facts_test.go` | Bucket policy conditions see the client address and TLS; a trusted proxy's `X-Forwarded-For` and `X-Forwarded-Proto` count, a client's do not |
| `route_ordering_test.go` | Route priority and matching order |
| `s3_request_log_test.go` | A refused request (403) is logged as a warning with the user, the action, the resource and why; another error at Info with its code and message, without a refusal that did not answer it; a request gives the bytes it received and the version and ETag of its answer |
| `s3_sdk_test.go` | Through the S3 router with the AWS SDK's defaults: a range of an object written with a checksum is sent without it; `a//b`, `a/./b`, `/a` and `a/../b` are keys of their own, on every node in copies or shards and in the console; a key too long is a `400`; bucket names with periods and `--` are taken and reached by their host, on the S3 API and as a website, with either host under the other; a presigned URL reads any key; the console reaches a key named as one of its routes (`x/tags`, `search`) and sends a quoted ETag |
| `search_api_test.go` | Object search API endpoints |
| `server_test.go` | Server startup, shutdown, configuration; the console keeps the lifecycle rules it does not send |
| `sts_aws_api_test.go` | AWS STS XML actions: missing/unknown action, `GetSessionToken` signature and credential issuance |
| `sts_federation_test.go` | STS federation disabled by default; requires a provider ID; rejects a malformed body |
| `tenant_usage_test.go` | Tenant synchronisation keeps each node's usage; the bucket limit and the console count the tenant's buckets on every node; the statistics pass corrects the tenant's storage; an overwrite, smaller or larger, keeps the tenant's storage equal to its bucket's; a tenant deletion from another node keeps a tenant changed after it |
| `two_factor_login_test.go` | A two-factor code alone signs no one in; the first step's token is not a session and a session token is not one; a user disabled between the steps, the account locked after the configured number of wrong codes, the limit per address; the sign-in is audited; in a cluster a user's two-factor authentication reaches the other node with the user |
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

### `internal/storage/` — 12 test files

| File | Description |
|------|-------------|
| `durability_test.go` | `Put` flushes data and sidecar, and survives a read-back |
| `filesystem_layout_test.go` | Layout v2 multipart upload directory: listed, discarded, ID cannot carry a path separator |
| `filesystem_list_test.go` | Listing returns every object in a bucket; empty on an absent bucket |
| `filesystem_shard_test.go` | The shards of a write are files of their own beside its file, each with its index in its sidecar, found by the write's ref, listed, deleted alone |
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

### `pkg/s3compat/` — 48 test files

| File | Description |
|------|-------------|
| `acl_cascade_test.go` | An ACL grant never overrules an explicit policy `Deny`; a granted user still reads |
| `acl_debug_test.go` | ACL compatibility debugging |
| `acl_security_test.go` | ACL security edge cases |
| `anonymous_get_object_test.go` | An anonymous GET reads a public-read bucket without a share, is denied by Block Public Access or a private bucket, and a share still grants it |
| `background_jobs_test.go` | A background job keeps the request's values but uses shutdown cancellation, not the request's |
| `bandwidth_test.go` | A tenant's bandwidth cap paces a PUT, a multipart part, a GET, a range GET and a browser form upload |
| `batch_missing_bucket_test.go` | A batch delete on a missing bucket answers `NoSuchBucket` |
| `block_public_acls_test.go` | `BlockPublicAcls` refuses a public ACL in PutBucketAcl, PutObjectAcl (canned or XML) and in `x-amz-acl` on PutObject, CopyObject and CreateMultipartUpload; nothing is stored; private ACLs and buckets without the flag are unaffected |
| `bucket_policy_test.go` | Bucket policies decide with the caller's permissions: an Allow grants a user of the tenant by ARN or ID, a Deny wins, `"*"` grants and denies requests without credentials over the ACL; the tenant boundary holds; a policy cannot deny its own management; `RestrictPublicBuckets`, `BlockPublicPolicy`, conditions on source address and TLS; what the evaluator would not enforce is refused when saved |
| `bucket_replication_test.go` | PutBucketReplication answers `NotImplemented` and names where replication rules are set; GetBucketReplication finds no configuration |
| `bucket_limit_test.go` | A tenant's bucket limit counts every bucket the tenant has, including those its users created through the S3 API |
| `bucket_listing_test.go` | `ListBuckets` shows exactly what the caller's policies grant, no more and no less |
| `checksum_test.go` | `x-amz-checksum-*` header validation (CRC32, CRC32C, SHA1, SHA256); a write and a part are checked against the digests they carry (Content-MD5, a checksum in a header or an aws-chunked trailer, CRC64NVME, the signed payload hash) and nothing is stored on a mismatch; a checksum is kept and returned, not with a range; a body that only looks aws-chunked is stored as it is; a body in signed chunks is checked chunk by chunk and its trailers too (as AWS documents them and as minio-go writes them); an aws-chunked body keeps the content codings it names besides `aws-chunked` |
| `conditional_headers_test.go` | `If-Match`/`If-None-Match` ETag comparison; a PUT with `If-Match` of another ETag or of no object is refused, of its own replaces it; of concurrent writes of a new key with `If-None-Match: *` one is taken |
| `copy_source_test.go` | Copy source parsing, version IDs, and encoded source keys |
| `delete_bucket_compat_test.go` | Bucket deletion allows only implicit folder markers to remain |
| `folder_conflict_test.go` | A folder marker over a same-named object succeeds (layout v2) |
| `governance_bypass_test.go` | Object Lock governance bypass is decided by policy, refused anonymously, and on the delete path; a retention in force changes as S3 lets it: COMPLIANCE never made GOVERNANCE or shortened, GOVERNANCE shortened only bypassing it |
| `handler_coverage_test.go` | S3 handler comprehensive coverage |
| `iam_authorization_test.go` | An IAM read policy does not authorize a write or delete; version actions are resource-scoped |
| `inventory_test.go` | S3 inventory API compatibility |
| `multipart_faultcheck_test.go` | Concurrent part uploads to different/the same part number over real HTTP; abort during a disconnected upload |
| `multipart_finish_test.go` | A completion or an abort sent again once the upload is finished is answered as AWS answers it; a completion is held to `If-Match` and `If-None-Match` with `412` or `NoSuchKey` before anything is written; a quick completion is answered with its version or its error's status, a long one kept open with spaces and its version |
| `multipart_object_lock_test.go` | `CreateMultipartUpload` refuses Object Lock headers it cannot honour with `InvalidRequest`; a canned ACL comes from `x-amz-acl` only, never from user metadata |
| `multipart_ownership_test.go` | A foreign multipart upload is refused; the owner's own upload keeps working |
| `multipart_pagination_test.go` | Multipart upload listing and parts pagination |
| `multipart_retention_test.go` | Default retention applied to an S3 multipart completion |
| `null_version_test.go` | The null version through the S3 API: kept, read and listed with `versionId=null`, replaced while suspended, a suspended delete answered `x-amz-version-id: null`, deleted by its ID |
| `object_headers_test.go` | An object gives back the headers it was written with (`Expires` included) written whole, in parts or copied, and one written in parts the tags it was started with; a signed read is sent the headers its `response-*` parameters ask for, an anonymous one asking for them is refused; an ETag is sent quoted in every header and listing and kept without quotes; HEAD and GET name the storage class of every class but STANDARD |
| `notifications_test.go` | Bucket notification webhook dispatch |
| `object_lock_permissions_test.go` | Object Lock headers on PUT, CreateMultipartUpload and CopyObject need `s3:PutObjectRetention` / `s3:PutObjectLegalHold`; the bucket default needs neither; a copy carries the retention it asked for |
| `objectlock_config_test.go` | Object Lock configuration needs its own permission; refused anonymously; read with or without the S3 namespace, answered with it |
| `post_presigned_test.go` | POST presigned URL (HTML form upload + policy validation) |
| `quota_over_limit_test.go` | A bucket or tenant over its quota refuses writes that grow usage, allows deletes, batch deletes and shrinking overwrites, and admits a write that fits once under the limit |
| `quota_write_errors_test.go` | A tenant quota refusal inside a PUT and a bucket quota refusal on CopyObject answer 403 `QuotaExceeded`; a failure to check the quota is not a refusal |
| `presigned_signer_test.go` | Presigned-URL signer resolves a permanent key's owner and an STS principal, denies what it cannot resolve |
| `presigned_test.go` | S3 pre-signed request handling (SigV2 + SigV4) |
| `proxy_fallthrough_test.go` | A request whose body was already consumed is not handled locally; reads may still fall through |
| `request_errors_test.go` | A request S3 refuses is a `4xx` with S3's code and writes nothing: bucket names, a missing bucket, tags, storage classes, metadata over 2 KB, keys too long; a copy onto itself that changes nothing; a part other than the last under 5 MB; a wrong secret is `SignatureDoesNotMatch`; a read of a part is that part, parts of one size kept as one run |
| `s3_test.go` | S3 protocol compatibility tests; in a cluster, capacity.xml takes the usage of the bucket's tenant on every node and, with no quota, the room of the cluster; a delete removes only the key it names; the subresources of a missing bucket answer `NoSuchBucket`; a grantee is typed with `xsi:type` and a policy given back is taken as it is |
| `select_streaming_test.go` | `SelectObjectContent` loaders don't scale memory with input size; schema can grow mid-stream |
| `select_test.go` | `SelectObjectContent` — event stream encoding, CSV/JSON loaders, SQL queries, batch flushing |
| `sts_compound_test.go` | Compound operations within one STS session |
| `tenant_boundary_test.go` | The tenant boundary holds in both directions; a super-admin crosses it read-only; a refused upload notes who asked for what and why |
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
| `api-token.test.ts` | JWT payload decoding for refresh scheduling; an object URL escapes each segment of the key |
| `api-refresh.test.ts` | One session renewal at a time: the scheduled one waits for a renewal in flight |
| `timeTicks.test.ts` | Time axis ticks fall on round times, minutes in real time, local hours and midnights, and name the day or second where needed |
| `bucketPolicyRules.test.ts` | A bucket policy edited as rules: rules round-trip through the policy, resources follow the actions, earlier template policies are read, what the rules cannot say is shown read-only, rule problems and address ranges |
| `Buckets.test.tsx` | Bucket list, creation, deletion UI |
| `ClusterHA.test.tsx` | The HA page shows the layout of a write with parity, turns parity on and off, and needs three nodes for it; with other storage classes it counts the nodes of STANDARD and names each node's class |
| `ClusterNodes.test.tsx` | The Nodes page names the coordinator and the members, and says the nodes are electing one when none holds it |
| `lifecycleRules.test.ts` | The lifecycle of the bucket settings: noncurrent versions and lone delete markers expired, objects and noncurrent versions moved to a colder class, a move off or of no class left out, the moves of a stored rule read back; the rule of the console found by its ID among the others, its values or the defaults asked |
| `StorageClasses.test.tsx` | The storage classes page shows each class with how it keeps its data, warns of one short of nodes, adds a class, changes its protection, deletes it, moves a node to another class |
| `AppLayout.test.tsx` | App shell layout and navigation behavior |
| `Dashboard.test.tsx` | Dashboard metrics rendering, storage distribution |
| `IdentityProviders.test.tsx` | IDP management UI flows; the directory browser imports only the user ticked, with its DN and email; a group mapping is made with the group given; an OAuth provider with an issuer shows its back-channel logout URL and is created with the issuer; the connection test shows each step with how it went |
| `UserDetails.test.tsx` | An administrator saves whether an identity provider's user follows its groups; a local user is offered no such mark |
| `LanguageContext.test.tsx` | Language context and locale switching |
| `Login.test.tsx` | Login form, OAuth redirect, 2FA prompt |
| `OAuthComplete.test.tsx` | The OAuth completion page keeps the session the exchange gives, or asks for the second factor and sends it with the first step's token |
| `Modal.test.tsx` | Modal keeps focus inside the dialog, returns it to the opener, announces itself as a dialog; a success or error message given as HTML is laid out, sanitized |
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
| Object | 56 | CRUD, versioning, locking, retention, multipart, interrupted-write recovery, quota reservations, write cost |
| Server | 67 | Routes, console API, IDP endpoints, search, IAM/STS, interrupted-write startup gate, HA receive, object placement between complete nodes |
| Cluster | 43 | Sync, replication, routing, HA quorum, rollback and catch-up, health, parity, storage classes |
| S3 Compat | 48 | Protocol compliance, ACL, multipart, inventory, pre-signed, handlers, Object Lock, integrity |
| Auth | 34 | Users, keys, JWT, S3 sig, TOTP, rate limiting, IAM policies and roles, STS, upgrade conversion |
| Metadata | 19 | Pebble store, search, pagination, tags, versioning, multipart, durability, group commit |
| Storage | 12 | Filesystem, staged commit, layout v2, shards, benchmarks |
| Erasure | 1 | Reed-Solomon layouts, encoding, reads with shards missing, rebuild |
| Replication | 11 | Manager, worker, S3 client, end-to-end, status retention |
| Metrics | 5 | Prometheus, history, system, performance |
| Bucket | 9 | CRUD, deletion, policy, integration, name uniqueness, lifecycle XML |
| IDP | 11 | LDAP against a test directory, OAuth, crypto, storage |
| Recovery | 7 | Offline recover and its resumption, reconcile (restore + repair) |
| Logging | 5 | HTTP targets, syslog, manager, persistence |
| Middleware | 4 | Auth, CORS, tracing, body digest |
| Inventory | 6 | Report generation, scheduling, retention |
| Rollback | 2 | Interrupted-write undo: restore, keep, resurrection guard |
| Encryption | 2 | AES-256-GCM, benchmarks |
| Other | 33 | ACL, API, audit, bandwidth, bgwork, cluster auth, config, DB DSN, encryption secret, migrations, KEK, layout, lifecycle, notifications, presigned, settings, share, transfer, cmd, embed |
| **Total Go** | **375** | |
| Frontend | 19 | Dashboard, Login, Users, Buckets, IDP, user details, layout, i18n, token handling, session renewal, modal focus, idle timer, bucket policy rules, OAuth completion, HA page, nodes page, storage classes, lifecycle rules, time axis |
| Performance | 4 | Upload, download, mixed, common |
