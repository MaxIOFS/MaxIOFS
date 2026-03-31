# MaxIOFS - Development Roadmap

**Version**: 1.1.0
**Last Updated**: March 29, 2026
**Status**: Stable

## 📊 Project Status

| Metric | Value | Notes |
|--------|-------|-------|
| S3 Core API | ~99% | Full compatibility audit completed — 20 issues identified and resolved (March 2026) |
| Backend Tests | 3,850+ | At practical ceiling — see details below |
| Frontend Tests | 95+ | |
| Production Ready | ✅ Stable | v1.1.0 released March 25, 2026 |

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

## 📝 References

- Changelog: [CHANGELOG.md](CHANGELOG.md)
- API Documentation: [docs/API.md](docs/API.md)
- Cluster Guide: [docs/CLUSTER.md](docs/CLUSTER.md)
- Performance: [docs/PERFORMANCE.md](docs/PERFORMANCE.md)
