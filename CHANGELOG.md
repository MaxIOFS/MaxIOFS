# Changelog

All notable changes to MaxIOFS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- **Veeam: SOSAPI `system.xml` had literal quote characters in `ProtocolVersion` and `ModelName`** — the values were set with backtick strings containing JSON-style quotes (`` `"1.0"` `` and `` `"MaxIOFS"` ``), producing `<ProtocolVersion>"1.0"</ProtocolVersion>` with embedded double-quote characters in the XML body. Veeam failed to parse the capabilities document correctly and fell back to treating MaxIOFS as a generic S3 storage without SOSAPI support, causing multi-bucket mode to remain enabled when it should have been automatically disabled. Fixed by using plain string literals `"1.0"` and `"MaxIOFS"`. (`pkg/s3compat/sosapi.go`)
- **Veeam / S3: `GetBucketLocation` returned `"us-east-1"` instead of empty string** — the AWS S3 spec requires that buckets in the default region return an empty `LocationConstraint`, not the string `"us-east-1"`. Returning the explicit region name caused Veeam (and other S3 clients such as MinIO-compatible tools) to treat the storage as a named-region deployment and enable multi-bucket mode automatically. MinIO follows the spec and returns an empty string, which is why Veeam disables multi-bucket with MinIO but not with MaxIOFS. Fixed to return `""`. (`pkg/s3compat/handler.go`)
- **Veeam / S3: `PutObjectLockConfiguration` nil pointer panic when `<Rule>` element is absent** — if a client sent a `PutObjectLockConfiguration` request without a `<Rule>/<DefaultRetention>` element (e.g., a bare `<ObjectLockEnabled>Enabled</ObjectLockEnabled>` body), the handler accessed `newConfig.Rule.DefaultRetention.Mode` directly, triggering a nil pointer dereference that was caught by the deferred panic handler and returned HTTP 500. Veeam interpreted this as "default retention is not supported". Added an explicit nil check: if `Rule` or `DefaultRetention` is absent the handler now returns `MalformedXML` 400 with a clear message. (`pkg/s3compat/handler.go`)
- **S3 Object Lock: `PutObjectLockConfiguration` returned wrong error code when Object Lock is not enabled on bucket** — when called on a bucket that was not created with Object Lock enabled, the handler returned `ObjectLockConfigurationNotFoundError` (the same 404 code used by `GetObjectLockConfiguration`). The AWS S3 spec requires `InvalidBucketState` with HTTP 409 Conflict for this case. Fixed to return `InvalidBucketState` 409; also added `InvalidBucketState` to the `writeError` switch so it maps to the correct HTTP status. (`pkg/s3compat/handler.go`)
- **Metrics: real-time throughput cards (requests/s, bytes/s, objects/s) always showed zero** — `RecordThroughput` was defined but never called anywhere; the three counters (`requestCount`, `bytesProcessed`, `objectsProcessed`) were always 0 so `CalculateThroughput()` returned zero for all three values every 5-second cycle. Fixed by calling `RecordThroughput` inside `TracingMiddleware` after every request: bytes = `r.ContentLength` (upload body) + response bytes written (tracked by extending `responseWriter` with a `bytesWritten` counter); objects = 1 for PutObject / GetObject / DeleteObject / CopyObject / MultipartUpload operations. Latency percentile cards were unaffected. (`internal/middleware/tracing.go`)
- **Cluster: duplicate buckets in list when same bucket is replicated across nodes** — when a bucket was mirrored/replicated on multiple cluster nodes, both S3 API `ListBuckets` and the web console showed one entry per node (e.g. two entries for the same logical bucket). Users could mistake a replica for a duplicate and delete it. `BucketAggregator.ListAllBuckets` now deduplicates by `(TenantID, Name)` after aggregating all nodes; one canonical entry per logical bucket is returned, preferring the local node. The console `handleListBuckets` in cluster mode uses `bucketAggregator.ListAllBuckets`, so S3 clients and the web console share the same deduplicated list. (`internal/cluster/bucket_aggregator.go`, `internal/server/console_api.go`, `internal/cluster/bucket_aggregator_test.go`)
- **Cluster / quotas: tenant storage must not double-count replicated buckets** — when a bucket is replicated across nodes, the same data exists on multiple nodes; tenant storage usage must be counted once, not per replica. (Phase 1: list dedup done; quota aggregation to exclude replica size is planned.)
- **Static website: error document not persisted when set** — configuring only the index document did not persist correctly; setting both index and error document appeared to work but the error document field showed empty when revisiting the config. The `errorDocument` field is now optional; when provided it is persisted. (`internal/metadata/types.go`, `internal/server/website_handlers.go`, `web/frontend/src/pages/buckets/[bucket]/settings.tsx`, `web/frontend/src/lib/api.ts`)
- **Static website: 404 when accessing website endpoint for bucket without website config** — accessing the website-style URL for a bucket that has no static website hosting configured now returns **403 Access Denied** (S3-style XML) instead of 404, so unconfigured website endpoints do not leak bucket existence. (`internal/server/server.go`)
- **Clean URL shares (non-presigned) returning 403** — shared object URLs that do not use presigned query parameters (clean links stored in DB) were failing with 403. Share URLs are now generated without `tenant_id` in the path (bucket names are globally unique). `GetShareByObject` when `tenantID` is empty (clean URL) searches by `bucket_name` and `object_key` only. (`internal/server/console_api.go`, `internal/share/sqlite.go`)
- **TestAccessKeySyncManager_Start "enabled auto sync" flakiness on CI** — setup (schema init, DB inserts) used a context with timeout, causing "context deadline exceeded" on slow CI. Setup now uses `context.Background()`; only the `Start()` call uses `context.WithTimeout` to assert non-blocking behavior. (`internal/cluster/access_key_sync_test.go`)
- **Refresh token never saved after 2FA login** — `verify2FA` in `api.ts` discarded the `refresh_token` returned by `POST /auth/2fa/verify`; sessions after 2FA expired after 15 minutes. `LoginResponse.refreshToken` is now populated and forwarded to `tokenManager.setTokens()`. (`web/frontend/src/lib/api.ts`)
- **Refresh token never saved after OAuth/SSO login** — `oauth-complete.tsx` only read `token` from the URL, ignoring `refresh_token`; OAuth/SSO sessions expired after 15 minutes. The page now reads and stores `refresh_token`. (`web/frontend/src/pages/auth/oauth-complete.tsx`)
- **Audit logs export only fetched the current page** — `exportToCSV` used `filteredLogs` (capped to current page). Export now loops through all pages via the API. (`web/frontend/src/pages/audit-logs/index.tsx`)
- **Audit logs stats cards showed counts for current page only** — success/failed counters were derived from the current page. Replaced with queries that use the `total` field from the API. (`web/frontend/src/pages/audit-logs/index.tsx`)
- **CSV export: timestamp column split by comma** — `formatTimestamp` used `toLocaleString()` (e.g. `"3/5/2026, 2:30:00 PM"`), so the comma split the timestamp into two columns. Replaced with `YYYY-MM-DD HH:MM:SS` format. (`web/frontend/src/pages/audit-logs/index.tsx`)
- **Clean share (URL limpia) 403 — S3 API** — unauthenticated GET to `/{bucket}/{object}` (clean share link, no presigned params) now resolves the share via `GetShareByObject` with empty tenant and, when found, serves the object instead of returning 403. Bucket read permission uses an `allowedByShare` flag so access is granted whenever the share was found (including global buckets where `shareTenantID` is empty; previously the check `shareTenantID != ""` wrongly denied those). (`pkg/s3compat/handler.go`; lookup by bucket+object when tenant empty in `internal/share/sqlite.go`.)
- **Website 403 HostId** — when the static website endpoint returns 403 Access Denied (bucket without website config or bucket not found), the response `<HostId>` and header `X-Amz-Id-2` now use the request host (`r.Host`) instead of a static value. (`internal/server/server.go`)
- **S3: `X-Amz-Id-2` header sent as hex instead of base64** — MaxIOFS generated the host ID as a 64-character lowercase hex string (`hex.EncodeToString`), while AWS S3 and MinIO use base64-encoded binary (48 random bytes → 64-char base64). Some S3 clients validate or log this header; using non-standard encoding could cause parsing issues. Changed to `base64.StdEncoding.EncodeToString` with 48 bytes of random data. (`internal/middleware/s3headers.go`, `pkg/s3compat/handler.go`)
- **S3: responses missing `Content-Length` header** — `writeXMLResponse` called `w.WriteHeader(statusCode)` before the response body was known, causing Go's HTTP layer to fall back to chunked transfer encoding (no `Content-Length`). AWS S3 and MinIO always send `Content-Length`. Fixed by buffering the full XML body first, setting `Content-Length` from the buffer size, then writing the header and body. (`pkg/s3compat/handler.go`)
- **Object Lock: default retention rule now optional at bucket creation** — the console API and frontend previously required a retention mode (GOVERNANCE/COMPLIANCE) and a period (days/years) whenever Object Lock was enabled, making it impossible to create a bucket with Object Lock enabled but no bucket-level default retention. Veeam B&R and other clients that manage retention per-object reject buckets with a pre-configured default retention rule ("unable to use backup immutability: the default retention is not supported"). Fixed: mode and period are now optional — a bucket can be created with Object Lock enabled and no default retention rule. If either mode or period is provided, both must be specified (consistency validation remains). The bucket-level default retention was never required by the AWS S3 spec; it is a convenience feature that is incompatible with clients that set retention per-object. (`internal/server/console_api.go`, `web/frontend/src/pages/buckets/create.tsx`)
- **Object Lock: `PutObjectLockConfiguration` with no `<Rule>` element now clears the bucket-level default retention** — AWS S3 allows sending a `PutObjectLockConfiguration` request with only `<ObjectLockEnabled>Enabled</ObjectLockEnabled>` and no `<Rule>` element to remove the bucket-level default retention policy without disabling Object Lock or affecting per-object locks. Previously MaxIOFS returned HTTP 400 `MalformedXML` for any request missing `<Rule>/<DefaultRetention>`, preventing administrators from clearing a pre-existing default retention rule on a bucket. Fixed: when `<Rule>` is absent the handler now clears `ObjectLock.Rule` and persists the updated configuration. Existing per-object lock metadata is unaffected. (`pkg/s3compat/handler.go`)
- **Veeam: "unable to use backup immutability: the default retention is not supported" — root cause: versioning `Suspended` on Object Lock bucket** — Object Lock requires versioning to be permanently `Enabled`; AWS S3 does not allow suspending versioning once Object Lock is active. If the bucket's versioning status was `Suspended` (or unset) while Object Lock was enabled, `GetBucketVersioning` returned `Suspended`, causing Veeam to reject the bucket as incompatible. Fixed in two places: (1) `GetBucketVersioning` now overrides `Suspended`/unset to `Enabled` when Object Lock is active, matching AWS S3 behavior where Object Lock permanently locks versioning on; (2) `PutBucketVersioning` now returns `InvalidBucketState` (409) when attempting to suspend versioning on an Object Lock bucket. (`pkg/s3compat/handler.go`)
- **S3: `HeadBucket` missing `x-amz-bucket-object-lock-enabled` header** — AWS S3 and MinIO include this header in the `HeadBucket` response when the bucket was created with Object Lock enabled. Veeam B&R uses it to determine whether the bucket supports immutability before proceeding with Object Lock configuration. The handler only called `BucketExists` (boolean check) and never exposed the Object Lock status. Changed to `GetBucketInfo` to retrieve full bucket metadata and set `x-amz-bucket-object-lock-enabled: true` when Object Lock is enabled. Also moved the Veeam `HeadBucket` debug log to after all headers are set (previously it logged at the start of the handler before the bucket lookup). (`pkg/s3compat/handler.go`)

---

## [1.0.0-rc1] - 2026-03-07

### Security
- **CRITICAL: AES-256-GCM replaces AES-256-CTR for object encryption** — AES-CTR provides confidentiality but no authentication: an attacker with write access to the storage backend could silently flip bits in ciphertext and the server would accept and decrypt the corrupted data (malleable ciphertext). Replaced with AES-256-GCM in 64 KB authenticated chunks. Each chunk has an independent nonce and authentication tag; any tampered chunk is detected and rejected on read. A backward-compatible legacy path decrypts existing AES-CTR objects transparently on first access; re-encryption happens lazily. New objects are always written with GCM.
- **CRITICAL: Cluster CA private key no longer transmitted over the network** — during cluster join, the CA private key was included in the HTTP response body, meaning any MITM or compromised node could steal it and sign arbitrary node certificates. Reworked to a CSR-based flow: the joining node generates its own ECDSA P-256 key pair locally, sends only a CSR to the `POST /sign-csr` endpoint, and the CA node signs it without ever transmitting the CA private key. The CA key now never leaves the originating node.
- **HIGH: 6 cluster management handlers missing `isGlobalAdmin` authorization check** — `handleAddClusterNode`, `handleRemoveClusterNode`, `handleInitializeCluster`, `handleJoinCluster`, `handleLeaveCluster`, and `handleGetClusterToken` accepted any authenticated request. Any tenant user or non-admin could initialize, join, or dismantle the cluster. All 6 handlers now enforce `isGlobalAdmin` before proceeding. Additionally, `handleAddClusterNode` had a partial SSRF: the caller-supplied node endpoint URL was fetched without validation. Added `validateClusterNodeEndpoint()` with SSRF-blocking dial restrictions.
- **HIGH: SSRF via notification webhook URLs** — the webhook notification system accepted arbitrary URLs from the database and made outbound HTTP requests without validation. An admin who set a webhook URL to `http://169.254.169.254/latest/meta-data/` (AWS metadata) or any internal service could exfiltrate instance credentials or probe the internal network. Added `ssrfBlockingDialer()` that blocks RFC 1918 private ranges, loopback, link-local (169.254.x.x), and IPv6 equivalents.
- **HIGH: SSRF via external HTTP logging targets** — same pattern as webhooks: user-configured HTTP log destinations were fetched without SSRF protection. Applied the same `ssrfBlockingDialer()` to the HTTP logging output.
- **HIGH: Path traversal via backslash on Windows** — `filesystem.go` sanitized `/` separators but not `\`, allowing object keys like `..\..\etc\passwd` to escape the data directory on Windows hosts. Added explicit `\` blocking, post-join canonical path validation with `filepath.Clean`, and `validatePath` in `List()`.
- **HIGH: Replication credentials stored in plaintext** — target bucket secret keys were stored unencrypted in the replication SQLite database. Any user with read access to the database file could extract valid AWS-compatible credentials. Now encrypted with AES-256-GCM using the server's `SecretKey`; stored with an `enc1:` prefix. Existing plaintext entries are read and re-encrypted on first access.
- **HIGH: HMAC nonce derived from `time.Now().UnixNano()` (predictable)** — inter-node cluster authentication used `time.Now().UnixNano()` as a nonce, making it predictable within a known time window and potentially replayable. Replaced with 16 bytes from `crypto/rand`.
- **HIGH: Infinite cluster proxy loop** — when node A proxied a request to node B, B could proxy it back to A (A→B→A→…) if routing logic disagreed. Added `X-MaxIOFS-Proxied: true` header; nodes reject any incoming request already carrying this header with 508 Loop Detected.
- **MEDIUM: OAuth callback missing CSRF state validation** — the Google OAuth callback accepted any `code` parameter without verifying the `state` value against the session, making it vulnerable to CSRF login attacks. Added `state` parameter generation, storage, and strict validation on callback.
- **MEDIUM: CORS origin allowlist replaced wildcard** — `Access-Control-Allow-Origin: *` was set unconditionally on all responses, allowing any website to make credentialed cross-origin requests to the console API. Replaced with `CORSWithConfig` using an explicit origin allowlist derived from `server.public_console_url`.
- **MEDIUM: DoS via multipart upload without part count limit** — a client could open a multipart upload and submit an unlimited number of parts (each consuming server memory and disk), eventually exhausting resources. Added a 10,000-part cap per upload (matching the S3 spec) and `partNumber` validation (must be 1–10,000).
- **MEDIUM: SSRF via replication destination endpoint** — user-configured replication target URLs were passed directly to the AWS SDK HTTP client without validation. Added `validateReplicationEndpoint()` and `ssrfBlockingReplicationDialer()` injected into the SDK's transport.
- **Frontend MEDIUM: Auth cookies missing `Secure` and `SameSite` flags** — JWT auth cookies were set without `Secure` (transmittable over plain HTTP) or `SameSite=Strict` (susceptible to CSRF). Added both flags to all `document.cookie` write and clear operations in `api.ts` and `oauth-complete.tsx`.
- **Frontend MEDIUM: Incomplete HTML sanitizer** — `sanitizeHtml()` in `modals.tsx` only blocked lowercase `javascript:`; missed `JAVASCRIPT:`, `data:` URIs, whitespace padding (`javascript :`) and HTML-entity variants. Rewrote sanitizer using the browser's own HTML parser for entity decoding, strict URL attribute inspection via `getAttribute()`, and an `http/https/mailto/#/` allowlist. Also removes `<meta>` and `<base>` tags.
- **Frontend MEDIUM: XSS via server values interpolated into HTML templates** — `shareData.url`, object keys, and bucket names returned by the server were interpolated directly into HTML template literals passed as `html:` to `ModalManager.fire()`. If the server were compromised or values contained HTML metacharacters, this would execute arbitrary scripts. Added `escapeHtml()` to `src/lib/utils.ts` and applied at every dynamic interpolation site.
- **Frontend LOW: Debug `console.log` statements in production** — `settings.tsx` and `users/[user]/index.tsx` left debug log calls that exposed ACL/policy API responses and internal user IDs in the browser DevTools console. Removed all non-error debug statements.

### Changed
- **Frontend: code splitting with `React.lazy`** — 18 pages now load on demand; main bundle reduced from **1,003 kB → 550 kB** (gzip: 252 kB → 170 kB, −33%). The recharts library (383 kB) only loads when navigating to the Metrics page.
- **Frontend: accessibility improvements** — `aria-current="page"` on all active nav links; `aria-label` and `aria-expanded` on all icon-only buttons (mobile menu, dark mode toggle, language selector, notifications bell); `aria-expanded` on the expandable Users submenu.
- **Debian/RPM packages: service auto-restart on upgrade** — `prerm` now records whether the service was running (flag file `/tmp/maxiofs-service-was-running`) before stopping it; `postinst` restarts it after upgrade if the flag is present. Services manually stopped before upgrading are not auto-started. RPM `%post` runs `try-restart` when `$1 -gt 1`.
- **Makefile: `VERSION_CLEAN` strips `-rc*` suffix** — `v1.0.0-rc1` now produces `1.0.0` for RPM/tarball naming, matching the existing `-beta`/`-alpha` stripping behaviour.

### Fixed
- **Mobile sidebar stays open after navigation** — clicking a nav link on small screens navigated to the page but left the sidebar overlay open; the user had to tap outside to dismiss it. All nav `<Link>` elements in `SidebarNav` now call `onClose()` on click.

### Tests
- `LanguageContext.test.tsx` — 8 tests covering default language, localStorage persistence, `setLanguage` state update, `i18n.changeLanguage` call, `initialLanguage` prop, round-trip switch, and `useLanguage` outside-provider error.
- `AppLayout.test.tsx` — 12 tests covering nav visibility by role (global admin / tenant admin / regular user), maintenance banner on/off, notification badge count, and default-password warning contribution to badge total.

---

## [1.0.0-beta] - 2026-03-02

### Added
- **Disk and quota alert auto-resolution** — condition-based SSE notifications now automatically disappear from the admin panel when the triggering condition clears, without requiring manual dismissal.
  - `disk_alerts.go`: when disk usage drops below the warning threshold after having been in warning or critical state, a `disk_resolved` SSE notification is broadcast to all connected global admins. The frontend removes any `disk_warning` or `disk_critical` notification from the panel on receipt.
  - `quota_alerts.go`: same pattern for tenant storage quota — `quota_resolved` is emitted (with `tenantId`) when usage drops below the warning level. Additionally fixed a bug where the per-tenant alert level tracker was only updated on escalation, preventing de-escalation from being detected across check cycles.
  - Frontend (`useNotifications.ts`): handles `disk_resolved` and `quota_resolved` events by filtering the matching condition notifications out of state and `localStorage`. All other notification types (e.g. `user_locked`, `data_corruption`) are unaffected — they remain as point-in-time events.
- **Audit log entry for automatic account locks** — when a user is locked automatically after exceeding the configured max failed login attempts, the event is now recorded in the audit log as `EventTypeUserBlocked` with `reason: "max_failed_attempts"`, `failed_attempts`, and `duration_minutes` in the details. Previously only manually-triggered locks (from the admin UI) were audited; auto-locks only fired the SSE callback with no persistent record.

### Changed
- **Metadata engine: BadgerDB → Pebble** — Replaced BadgerDB with `github.com/cockroachdb/pebble` (CockroachDB's LSM-tree engine) for all S3 object/bucket metadata storage. Pebble uses a crash-safe WAL (write-ahead log) that survives unclean shutdowns — the root cause of recurring BadgerDB MANIFEST corruption on power loss or process kill. Pebble is pure-Go (no CGO), zero external dependencies, same paradigm (LSM-tree), same on-disk key format preserved byte-for-byte.
  - **Transparent auto-migration**: on first startup after upgrade, if `metadata/KEYREGISTRY` is detected (BadgerDB-exclusive file), all keys are read from BadgerDB and written to Pebble in batches. Directories are atomically renamed (`metadata/` → `metadata_badger_backup/`, `metadata_pebble/` → `metadata/`). Users see no interruption. Migration ran successfully in production, additionally correcting previously under-reported object counts and sizes that had diverged in BadgerDB.
  - **Decoupled all packages from BadgerDB**: ACL, bucket, object, metrics, and notifications packages no longer import `github.com/dgraph-io/badger/v4` directly. A new `metadata.RawKVStore` interface (`GetRaw`, `PutRaw`, `DeleteRaw`, `RawBatch`, `RawScan`, `RawGC`) is implemented by PebbleStore and consumed by ACL and metrics history.
  - **TTL replacement**: Pebble has no native TTL. Multipart upload cleanup (previously 7-day TTL in BadgerDB) is replaced by an hourly goroutine that scans multipart entries and removes those older than 7 days.
  - BadgerDB source files (`badger.go`, `badger_objects.go`, `badger_multipart.go`, `badger_rawkv.go`) are retained solely to support the one-time migration path and may be removed in a future release.
- **Frontend i18n: translation files split** — all translation strings for `es`, `en`, `pt`, `de`, `fr` are now split into per-page JSON files under `src/locales/` instead of one monolithic file per language. Reduces bundle size and makes adding new languages easier.
- **AppLayout refactored** — navigation sidebar and top bar extracted into `SidebarNav` and `TopBar` components. `AppLayout` is now a thin shell that composes them.
- **Docs updated** — `ARCHITECTURE.md`, `CONFIGURATION.md`, `DEPLOYMENT.md`, `OPERATIONS.md`, `SECURITY.md`, `CLUSTER.md`, `API.md`, `TESTING.md` revised to reflect Pebble engine, new features, and removed compression support.
- **Compression removed** — LZ4/Snappy object compression was removed. It provided minimal benefit for already-compressed content (images, videos, ISOs) while adding latency and complexity.
- **i18n: Cluster, metrics, bucket, and settings pages fully translated** — New `locales/en/cluster.json` and `locales/es/cluster.json` covering all four cluster management pages (Nodes, Overview, Migrations, BucketReplication). `metrics.json`, `buckets.json`, `settings.json`, and `about.json` received complete translation coverage for en/es. OAuth completion page (`oauth-complete.tsx`) and `LoggingTargets` settings component now use `useTranslation` hooks. All hardcoded English strings on these pages are replaced with i18n-aware keys.
- **Frontend: `TIME_RANGES` extracted to shared module** — `TimeRange` type and `TIME_RANGES` array moved from `TimeRangeSelector.tsx` to a standalone `src/components/charts/timeRanges.ts` module, making the canonical time range definitions importable by other components without pulling in the React selector component.
- **Frontend: code splitting with `React.lazy`** — all non-critical pages are now loaded on demand via dynamic imports instead of being bundled into a single JS file. Login, Dashboard, and OAuth callback remain in the main bundle (critical first-load paths); the remaining 18 pages (Buckets, Users, Cluster, Metrics, Settings, etc.) each become their own chunk fetched only when the user navigates to them. A `<Suspense>` spinner in `AppLayout`'s content area shows while a chunk loads, keeping the sidebar and topbar immediately visible. Main bundle reduced from **1,003 kB → 550 kB** (gzip: 252 kB → 170 kB, −33%). The heavy charting library (recharts, 383 kB) only loads when navigating to the Metrics page.

### Added
- **Object Integrity Verification** — detects silent data corruption (bad sectors, filesystem errors, write bugs) by re-reading object bytes from disk and comparing the computed MD5 against the stored ETag.
  - **`POST /buckets/{bucket}/verify-integrity`** — on-demand verification endpoint (global admin only). Accepts `prefix`, `marker` and `maxKeys` query params (default 1000, max 5000) for paginated scanning. Returns a `BucketIntegrityReport` with totals (`checked`, `ok`, `corrupted`, `skipped`, `errors`) and a per-object `issues` list for any corrupted/missing objects. Response includes a `nextMarker` for pagination.
  - **Background integrity scrubber** — `startIntegrityScrubber(ctx)` goroutine runs a full sweep of every tenant and bucket every **24 hours** (first run after the initial tick, not on startup). Paginates in batches of 500 objects with a 10 ms yield between pages to avoid saturating disk IO.
  - **Corruption alerting** — when a corrupted or missing object is found (by either the scrubber or the endpoint), three actions fire automatically:
    - Audit event of type `data_corruption` is logged with bucket, key, stored ETag, computed ETag, and expected/actual size.
    - SSE notification (`type: "data_corruption"`) pushed to all connected admin sessions.
    - Email alert sent to all active global admins that have an email address, including the full bucket/key path and ETag mismatch details. Reuses the existing `buildEmailSender()` and `email.*` settings.
  - **Multipart-safe**: objects with composite ETags (format `<md5>-<N>`) are automatically skipped with status `skipped` — their ETag cannot be verified with a simple content hash.
  - **Encryption-transparent**: verification calls `GetObject()` which handles AES-256-CTR decryption before hashing, so encrypted objects are verified against their original plaintext MD5.
  - New `internal/object/integrity.go`: `IntegrityStatus` type, `IntegrityResult` and `BucketIntegrityReport` structs, `VerifyObjectIntegrity` and `VerifyBucketIntegrity` methods on `objectManager`.
  - New `internal/server/integrity_scrubber.go` and `internal/server/object_integrity_handler.go`.
  - `Manager` interface extended with `VerifyObjectIntegrity` and `VerifyBucketIntegrity`.
  - New audit constants: `EventTypeDataIntegrityCheck`, `EventTypeDataCorruption`, `ActionVerifyIntegrity`.
- **Maintenance Mode enforcement** — the existing `system.maintenance_mode` setting now actively blocks write operations across the server when enabled.
  - **S3 API middleware** (`internal/middleware/maintenance.go`): PUT, POST, and DELETE requests receive `HTTP 503 ServiceUnavailable` with an XML `<Error><Code>ServiceUnavailable</Code>` body and a `Retry-After: 3600` header. GET, HEAD, and OPTIONS are always allowed (clients can still read their data).
  - **Console API middleware**: any mutating request returns `HTTP 503` with a JSON body `{"error": "...", "code": "MAINTENANCE_MODE"}`. Exempt paths that always pass through: `/auth/`, `/health`, `/settings`, `/api/internal/`, `/notifications` (ensures admins can disable maintenance mode without getting locked out).
  - **Frontend amber banner** (`AppLayout.tsx`): visible across every page immediately when maintenance mode is active; disappears when disabled — no page reload required. Achieved by adding `refetchInterval: 30000` to the `serverConfig` query and calling `queryClient.invalidateQueries(['serverConfig'])` in the settings save handler so all tabs reflect the change within seconds.
  - `handleGetServerConfig` now includes `maintenanceMode: bool` in its response payload.
- **SMTP email notifications** — new `email` settings category with full SMTP support for system alerts.
  - New `internal/email` package (`sender.go`): `Sender` struct supporting three explicit TLS modes: `none` (plain SMTP, port 25), `starttls` (STARTTLS upgrade, port 587), `ssl` (implicit TLS, port 465). Optional `skip_tls_verify` for self-signed certificates.
  - 8 new settings under `email.*` category: `email.enabled`, `email.smtp_host`, `email.smtp_port`, `email.smtp_user`, `email.smtp_password`, `email.from_address`, `email.tls_mode`, `email.skip_tls_verify`.
  - **Test email endpoint** `POST /api/v1/settings/email/test` — sends a test message to the requesting admin's own account email address to verify SMTP configuration without triggering a real alert.
  - **Frontend Email tab** in System Settings: all 7 settings rendered with a dedicated "Send Test Email" button showing inline success/error feedback. Password field for `email.smtp_password` renders as `type="password"`.
- **Tenant quota warning notifications** — after every successful object upload (`PutObject`), the server checks whether the tenant's storage usage has crossed the configured warning (80%) or critical (90%) threshold. Fires only on level escalation — not on repeated checks at the same level.
  - Callback wired from `authManager.IncrementTenantStorage` (covers both regular PutObject and multipart complete) → `server.checkQuotaAlert()`.
  - SSE notification delivered to all connected admins of the affected tenant (`TenantID` set) and to global admins.
  - Email sent to all active tenant admins and global admins with email addresses.
  - Deduplication via `quotaAlertTracker` (`sync.Map[tenantID → alertLevel]`), one entry per tenant.
  - Thresholds reuse `system.disk_warning_threshold` and `system.disk_critical_threshold` settings.
  - Tenants with `MaxStorageBytes = 0` (unlimited quota) are skipped entirely.
  - **Frontend** (`pages/tenants/index.tsx`): storage bar thresholds aligned to 80% (amber) / 90% (red) with an inline "80% — Warning" / "90% — Critical" label below the bar when threshold is reached.
- **Disk space alerts** — background monitor that fires SSE notifications and emails when disk usage crosses configurable thresholds.
  - `internal/server/disk_alerts.go`: `startDiskAlertMonitor(ctx)` goroutine checks disk usage every **5 minutes** using the existing `systemMetrics.GetDiskUsage()`. Alert deduplication: only fires on level *escalation* (none→warning, none/warning→critical), never re-alerts at the same level on repeated checks.
  - Two new threshold settings in the `system` category: `system.disk_warning_threshold` (default 80%) and `system.disk_critical_threshold` (default 90%).
  - On threshold crossing: SSE notification pushed to all connected global admin sessions + email sent to every active global admin user that has an email address on their account.
  - Email body includes used/total/free GB breakdown and a reminder of how to adjust thresholds in settings.
- **Cluster: State Snapshot system** — nodes can expose their full local entity state (tenants, users, buckets, access keys, IDPs, group mappings) as a snapshot for LWW (Last-Write-Wins) comparison via `GET /api/internal/cluster/state-snapshot`.
- **Cluster: Stale Reconciler** — when a node reconnects after a partition or extended downtime, it fetches a healthy peer's snapshot, compares `updated_at` timestamps across all entity types, and pushes locally-newer entities before clearing the stale flag. Supports two modes: `ModeOffline` (node was unreachable, no local writes) and `ModePartition` (node was isolated but served clients).
- **Cluster: Write tracking** — new `last_local_write_at` column tracks the most recent client-driven write per node, enabling correct partition detection on reconnect.
- **Admin endpoint `POST /buckets/{bucket}/recalculate-stats`** — allows administrators to resync a bucket's counters (`ObjectCount`, `TotalSize`) by scanning all objects present in the metadata store. Useful for correcting metrics that diverged due to system restarts or updates lost under concurrent load. Requires admin role; global admins can pass `?tenantId=` to target a specific tenant's bucket. Returns the recalculated values in the response.
- **Background stats reconciler** (`startStatsReconciler`) — a goroutine started automatically at server startup that recalculates `ObjectCount` and `TotalSize` for every bucket across all tenants every **15 minutes**. Iterates all buckets via `ListBuckets(ctx, "")` and calls `RecalculateBucketStats` for each one. Starts after an initial 2-minute delay to allow the server to be fully ready. Exits cleanly on context cancellation. Errors on individual buckets are logged as warnings and do not interrupt the rest of the pass. Acts as a continuous safety net independent of the manual admin endpoint.
- **Non-blocking bulk delete with background progress bar** — selecting multiple objects and clicking Delete no longer blocks the UI. The modal closes immediately, selection clears, and a fixed bottom-right `BackgroundTaskBar` component shows real-time per-item progress using 8 concurrent workers. Displays a success/failure summary on completion and auto-dismisses.
- **Background task progress bar** — `BackgroundTaskBar` UI component shows long-running server-side operations (scrub, migration, bulk delete) in the console without blocking navigation. Displays per-item progress, success/failure summary, and auto-dismisses on completion.
- **Sidebar and TopBar extracted from AppLayout** — `SidebarNav` and `TopBar` are now standalone components, making `AppLayout` significantly smaller and easier to maintain.
- **Maintenance banner** — `MaintenanceBanner` component displayed at the top of the console when the server is in maintenance mode.
- **`AdjustBucketSize` metric operation** — new `bucket.Manager` method that updates `TotalSize` without touching `ObjectCount`, used when overwriting existing objects so the object count is not inflated.
- **Audit: Object operation event logging** — Console API now emits audit events for all object-level operations performed through the web console: `object_uploaded`, `object_downloaded`, `object_deleted`, and `object_shared`. Each record includes bucket, object key, size, content type, ETag, and share ID as applicable. New audit constants: `EventTypeObjectUploaded`, `EventTypeObjectDeleted`, `EventTypeObjectDownloaded`, `EventTypeObjectShared`, `ResourceTypeObject`, `ActionUpload`, `ActionDownload`, `ActionShare`.
- **Audit: Structured forwarding to external log targets** — `audit.Manager.LogEvent` now emits at `Info` level (previously `Debug`) tagged with `"audit": true` and all structured fields (user, tenant, resource type/ID/name, IP address, user-agent, details map) so every audit event is automatically forwarded to configured external syslog/HTTP logging targets via `DispatchHook`. Previously only sparse `Debug` lines were emitted, meaning external log targets received no useful audit data.
- **Cluster: Storage stats per bucket in cluster view** — `GET /api/internal/cluster/buckets` now includes `object_count` and `total_size` fields per bucket, enabling storage usage overview directly in the cluster management UI.
- **Metrics: Immediate startup snapshot** — `metricsCollectionLoop` now captures a metrics snapshot immediately on startup (previously data only appeared after the first full tick interval elapsed) and captures a final snapshot on graceful shutdown. Charts now show data from the first moment after server start with no leading gap.

### Fixed
- **Empty folders not displayed in object browser** — folder marker objects (keys ending in `/`, stored with metadata flag `x-maxiofs-implicit-folder=true`) were unconditionally skipped in `ListObjects` and `SearchObjects`. When a folder contained no objects, there were no child-key paths to derive the common prefix from, so the folder was completely invisible in the frontend. The fix: when a delimiter is present (hierarchical listing mode), folder markers are no longer skipped — they fall through to the common-prefix extraction block and are added to `commonPrefixesMap` like any other object whose key contains the delimiter after the prefix. Flat listings (`delimiter=""`) continue to omit implicit folder markers as before. A guard prevents the folder's own key from appearing as a child of itself when listing with the folder as the prefix.
- **Integrity scanner erroring on folder and delete-marker objects** — `VerifyBucketIntegrity` calls `metadataStore.ListObjects` directly (bypassing the object-manager filters) and therefore received folder markers and delete markers alongside real data objects. `VerifyObjectIntegrity` would then try to open a content file for a folder key (which has no backing file on storage) and report it as `IntegrityMissing` or `IntegrityError`. Fixed by adding two early-exit checks at the top of `VerifyObjectIntegrity`: (1) any key ending with `/` is returned as `IntegritySkipped` with reason `"folder marker: not a data object"`; (2) any object whose stored ETag is empty (delete marker tombstone) is returned as `IntegritySkipped` with reason `"delete marker: not a data object"`. Both types are counted in the `skipped` total so the report counters remain consistent (`checked = ok + corrupted + skipped + errors`).
- **Integrity scan results persisted and surfaced in UI** — previously, opening the bucket integrity modal always showed "Ready to scan" regardless of whether the background scrubber had already run. Scan results are now persisted per-bucket in Pebble (`integrity_scans:{bucketPath}` key) as a JSON array of the last **10** scan records (newest first). The scrubber accumulates per-bucket totals across all paginated pages and writes one record on completion. On-demand manual scans post their accumulated result via `POST /buckets/{bucket}/integrity-status` when finished. The modal fetches the history via `GET /buckets/{bucket}/integrity-status` on open and immediately shows the last known state (including which source triggered it — `"manual"` or `"scrubber"`). Old single-record format is read with backward-compat fallback.
- **Integrity check modal permissions** — "Start Verification", "Scan again" and "Retry" action buttons are hidden for tenant admins (view-only access to scan history). Regular users no longer see the "Check Integrity" button in the bucket list at all, preventing unnecessary 403 requests to the backend. New `canRunScan` prop on `BucketIntegrityModal` (default `false`); passed as `canRunScan={isGlobalAdmin}` from the bucket list.
- **Integrity scan rate limit** — a global admin could repeatedly trigger `POST /buckets/{bucket}/verify-integrity` back-to-back, causing unbounded disk I/O. Manual scans are now rate-limited to **once per hour** per bucket. The limit is enforced server-side: when `marker` is empty (start of a new scan), the handler checks the timestamp of the most recent manual scan from the persisted history. If less than 60 minutes have elapsed, it returns `HTTP 429` with a message indicating the remaining wait time. Continuation pages (`marker != ""`) are always allowed. The frontend surfaces the limit proactively: the modal computes the next-available time from history and shows a live countdown with the "Start Verification" / "Scan again" button disabled until the cooldown expires.
- **SMTP TLS mode ambiguity** — `email.use_tls: false` (previous default) connected to the SMTP server and then opportunistically upgraded to STARTTLS whenever the server advertised it, making it impossible to force a truly unencrypted connection (e.g. internal Postfix relay or Milter setup that does not support TLS). The setting has been replaced with an explicit three-way `email.tls_mode` selector: `none` (plain TCP, no TLS at all — default), `starttls` (connect plain, upgrade with STARTTLS; fails if server does not advertise it), `ssl` (implicit TLS from the first byte, port 465). An additional `email.skip_tls_verify` boolean allows bypassing certificate validation for self-signed certificates on internal mail servers. Existing `email.use_tls` entries are removed from the database on startup as a deprecated setting.
- **Virtual-hosted-style S3 requests returning bucket list instead of objects** — when a client used virtual-hosted-style addressing (bucket name in the subdomain, e.g. `mybucket.s3.example.com`), every request to `/` or any object path was incorrectly routed to the root handler and returned a `ListAllMyBucketsResult` instead of the expected bucket operation. This broke compatibility with WinSCP, CyberDuck, CloudBerry, and other clients that default to virtual-hosted-style. Fixed by adding an HTTP middleware (`virtualHostedStyleMiddleware`) that intercepts requests before Gorilla Mux routing: if the `Host` header matches `{bucket}.{configured-s3-host}`, the URL path is transparently rewritten from `/...` to `/{bucket}/...` so all existing path-based routes handle it correctly. Both path-style and virtual-hosted-style now work simultaneously without any configuration change.
- **Metadata corruption on unclean shutdown** — Pebble's WAL guarantees database integrity after process kill, power loss, or OOM kill. No more `MANIFEST` corruption requiring manual recovery.
- **Object/size metrics corrected post-migration** — the BadgerDB → Pebble migration recounted all existing objects and sizes, fixing counters that had diverged due to BadgerDB transaction conflicts under concurrent load.
- **Bucket metrics under-reported under concurrent load (VEEAM / multiple S3 clients)** — `UpdateBucketMetrics` used BadgerDB's Optimistic Concurrency Control (OCC) with only 5 retry attempts. Under high concurrency (VEEAM Backup with multiple parallel upload threads, 10 or 100 simultaneous S3 clients), `ErrConflict` exhausted retries and silently discarded metric updates, causing the web interface to show significantly less storage than was actually stored. The retry loop has been replaced with a per-bucket `sync.Mutex` via `sync.Map`: serialization now occurs at the Go level, making `ErrConflict` impossible by construction. Metric updates are now fully reliable under any concurrency level.
- **`RecalculateBucketStats` ignored tenant prefix** — the function scanned `obj:bucketName:` but objects in tenant buckets are stored as `obj:tenantID/bucketName:key`. For any tenant bucket, recalculation always returned 0 objects and 0 bytes, silently resetting counters. Now builds the full path (`tenantID/bucketName` for tenant buckets, or just `bucketName` for global buckets) before scanning. Global buckets (no tenant) are unaffected.
- **Frontend: dynamic bucket stats refresh** — React Query `refetchInterval: 30000` added to bucket-related queries on three pages so the UI updates automatically every 30 seconds without requiring navigation or page reload:
  - Dashboard (`pages/index.tsx`) — `['buckets']` query
  - Buckets listing (`pages/buckets/index.tsx`) — `['buckets']` query
  - Bucket detail (`pages/buckets/[bucket]/index.tsx`) — `['bucket', name, tenant]` query
  - All three also set `refetchOnWindowFocus: false` to avoid redundant fetches on tab switch, consistent with the existing metrics page behavior.
- **Regular users locked out of their own profile** — `handleGetUser`, `handleUpdateUser`, `handleListAccessKeys`, `handleCreateAccessKey`, and `handleDeleteAccessKey` blocked all non-admin users unconditionally, including self-access. Added `isSelf` exception: any authenticated user can view and update their own profile (email only) and manage their own access keys. Roles, status, and tenantId remain admin-only fields. Tenant-scope cross-check applies only when an admin accesses another user's data.
- **Dashboard system metrics inaccessible to global non-admin users** — `/metrics/system` was restricted to global admins only. Any user without a `tenantID` (global user, regardless of admin role) can now call it, allowing the dashboard to show disk usage and free space. Tenant users remain blocked and see only their tenant storage usage. Frontend `systemMetrics` query guard changed from `enabled: isGlobalAdmin` to `enabled: !isTenantUser`.
- **User role label hardcoded as "Global Admin" in header** — the subtitle under the username showed "Global Admin" for every user without a tenantId, regardless of actual role. Now shows "Global Admin" for users with the admin role and "Global User" for regular global users.
- **Backend 4xx responses logged as warnings** — `writeError` called `logrus.Warn("API error")` for every error response including expected 403/404 access-control denials, filling logs with noise. Now only 5xx server errors are logged as warnings.
- **SSE notification stream attempted by non-admin users** — `useNotifications` hook unconditionally connected to `/notifications/stream` (admin-only endpoint) for all authenticated users on login, generating repeated 403 errors. Hook now accepts an `enabled` parameter; `AppLayout` passes `isAdminUser` so non-admins never open the SSE connection.
- **Bucket permissions modal allowing cross-scope grants** — the "Grant Permission" form listed all users in the system regardless of bucket scope, making it possible to grant a tenant user access to a global bucket or a global user access to a tenant bucket, and to grant entire-tenant access to buckets. Permissions are now strictly scope-isolated:
  - **Global bucket** → selectable users are limited to global users only (no `tenantId`). No tenant-wide grants.
  - **Tenant bucket** → selectable users are limited to users belonging to exactly that tenant. No global users, no cross-tenant grants.
  - Admin users (global or tenant) are excluded from the selector in all cases — they already have full access by role and do not need explicit grants.
  - The "Target Type" selector (User / Tenant) has been removed; all grants are now individual-user only.
  - A scope badge and contextual notice in the form make the restriction visible to the admin.
- **User deletion leaving orphan bucket permissions** — deleting a user did not remove their bucket access grants, leaving stale entries that referenced a non-existent user ID. `authManager.DeleteUser` now calls `store.ListUserBucketPermissions` and revokes every grant before removing the user record. Individual revocation failures are logged as warnings and do not block the deletion.
- **About page updated to v1.0.0-beta** — "New in" section reflects the changes of this release.
- **Large file multipart upload — 5 cascading bugs fixed** (`f10b774`, resolves [#5](https://github.com/MaxIOFS/MaxIOFS/issues/5)):
  1. *Error type mismatch*: HTTP handler checked `ErrUploadNotFound` but the object manager returns `ErrInvalidUploadID`; fell through to a generic 500 instead of 404.
  2. *`os.Rename` race on Windows*: two concurrent `CompleteMultipartUpload` requests for the same `uploadID` raced to rename the temp file to `objectPath`. On Windows, `os.Open` does not set `FILE_SHARE_DELETE`, causing `ERROR_SHARING_VIOLATION`. Fixed with per-`uploadID` deduplication (`completionFuture`).
  3. *Excessive I/O (3× full-file read/write)*: `calculateMultipartHash` re-read the entire combined object to compute an MD5 already computed by `storage.Put`; `storeUnencryptedMultipartObject` then re-read and re-wrote the same file just to update the `.metadata` sidecar. For a 6 GB file this added ~60 s of unnecessary I/O. Fixed: use `storage.GetMetadata` + `storage.SetMetadata` on the sidecar only.
  4. *`http.Flusher` not propagating through metrics middleware*: `metrics.responseWriterWrapper` had no `Flush()` method, so `w.(http.Flusher)` always returned `ok=false` — the immediate `200 OK` was never flushed to the client. Same fix applied to tracing, logging, and verbose-logging wrappers.
  5. *Background goroutine cancelled on client disconnect*: the combine goroutine used `r.Context()`, which Go cancels when the client disconnects. Metadata operations then failed, leaving multipart state partially cleaned up; retries got `NoSuchUpload`. Fixed: goroutine now uses `context.WithoutCancel(r.Context())`.
- **Bucket object count inflation on overwrite** — `PutObject` and `CompleteMultipartUpload` incorrectly called `IncrementObjectCount` when replacing an existing object, inflating `ObjectCount`. Now uses `AdjustBucketSize` (size delta only, count unchanged) for overwrites and additional versions.
- **Bulk delete** — fixed parsing of `Delete` XML body; objects with keys containing special characters were silently skipped.
- **Frontend upload chunking** — large file uploads from the console web UI now correctly chunk and track upload progress; the upload API client was not splitting streams above the part-size boundary.
- **Integrity check button** — the "Run Integrity Check" button on the buckets page was missing its click handler after a recent refactor. Re-wired to the new `object_integrity_handler`.
- **Auth: S3 SigV4 host in virtual-hosted mode** — signature validation now strips the bucket prefix from the `Host` header before verifying the canonical request, matching AWS SDK behaviour.
- **Server HTTP timeouts** — removed global `ReadTimeout` and `WriteTimeout` (were 30 s, broke large uploads/downloads). Replaced with `ReadHeaderTimeout: 30s` only, leaving body transfer unlimited.
- **Settings page: email tab** — added SMTP configuration fields, password input, and "Send Test Email" button to the settings UI.
- **`refetch` on settings and user pages** — values now refresh automatically after save; users no longer need to reload the page.
- **Audit SQLite `SQLITE_BUSY` under concurrent load** — `SQLiteStore` previously wrote each audit event synchronously through a connection pool (up to 25 connections). Under concurrent request load, multiple goroutines raced to write simultaneously, producing `SQLITE_BUSY`/`SQLITE_LOCKED` errors and silently dropping audit events. Rewritten with a single writer goroutine: events are dispatched asynchronously to a buffered channel (4096 capacity), batched up to 128 per SQLite transaction, and flushed every 100 ms. WAL mode (`PRAGMA journal_mode=WAL`) enabled. Connection pool reduced to 1 connection. Audit events are no longer dropped under any realistic concurrency level.
- **Metrics chart false spikes from stale query data** — the metrics page always appended a synthetic "current" data point to chart series using the latest `systemMetrics` query value. When that query returned stale cached data older than the last stored snapshot, the virtual point showed lower values, producing a visible downward spike at the right edge of the chart. The "current" point is now only appended when the last stored snapshot is older than 30 seconds.

### Removed
- **Test `TestHandleTestLogOutput`** — called `server.handleTestLogOutput` which does not exist on `*Server`, causing a compilation error. The handler was never implemented.

### Tests
- `TestRecalculateBucketStats_GlobalBucket` — new test covering the global bucket path (no tenant). Objects are stored as `obj:bucketName:key` without a tenant prefix; verifies that recalculation scans the correct prefix.
- `TestRecalculateBucketStats_Success` and `TestRecalculateBucketStats/Recalculate_bucket_stats` updated — the `Bucket` field of test objects now uses the full `tenantID/bucketName` path matching production behavior, confirming the prefix fix works end-to-end.
- `pkg/s3compat` `mockAuthManager` updated with `SetStorageQuotaAlertCallback` stub — added after `auth.Manager` interface gained this method, fixing a build failure that blocked the entire test package from compiling.

---

## [0.9.1-beta] - 2026-02-22

### Added
- **Inter-node TLS encryption** — all cluster communication is now automatically encrypted with TLS using auto-generated internal certificates. No configuration needed.
  - On cluster initialization, an internal CA (ECDSA P-256, 10-year validity) and node certificate (1-year validity) are generated automatically
  - Joining nodes receive the CA cert+key and generate their own node certificate signed by the cluster CA
  - All inter-node HTTP clients (health checks, sync managers, replication, proxy, migrations) use the cluster TLS config
  - Background certificate auto-renewal: monthly check, auto-renews node certs expiring within 30 days with hot-swap via `tls.Config.GetCertificate` callback — no restart needed
  - CA expiry warning logged when CA certificate is within 1 year of expiring
  - New `internal/cluster/tls.go`: `GenerateCA()`, `GenerateNodeCert()`, `BuildClusterTLSConfig()`, `ParseCertKeyPEM()`, `IsCertExpiringSoon()`
  - DB migration: 4 new columns on `cluster_config` (`ca_cert`, `ca_key`, `node_cert`, `node_key`)
  - `NewProxyClient()` now accepts optional `*tls.Config` parameter for TLS-aware proxying
  - Initial join handshake uses `InsecureSkipVerify` (remote node not yet in cluster); all subsequent communication uses strict CA validation
- **External syslog targets** — multiple external logging targets (syslog and HTTP) stored in SQLite with full CRUD API. Replaces the single-target legacy `logging.syslog_*` / `logging.http_*` settings with an N-target system supporting independent configuration per target.
  - New `logging_targets` table (migration 11, v1.0.0) with indexes on type and enabled status
  - `TargetStore` CRUD in `internal/logging/store.go` with validation, unique name constraint, and automatic migration of legacy settings
  - 7 new console API endpoints: `GET/POST /logs/targets`, `GET/PUT/DELETE /logs/targets/{id}`, `POST /logs/targets/{id}/test`, `POST /logs/targets/test` (test without saving)
  - Frontend `LoggingTargets` component integrated in Settings → Logging with create/edit modal, test connection, delete confirmation, and TLS indicator
- **Syslog TLS and RFC 5424** — `SyslogOutput` rewritten with TCP+TLS support (mTLS, custom CA, skip-verify) and RFC 5424 structured data format alongside RFC 3164
- **Lock-free log dispatch** — `DispatchHook` uses `atomic.Pointer` for the outputs snapshot, making `Fire()` completely lock-free and eliminating a deadlock where `Reconfigure()` (write lock) triggered logrus hooks that needed a read lock
- **Cluster: Join Cluster UI** — standalone nodes now show a "Join Existing Cluster" button alongside "Initialize Cluster". The join form prompts for the existing cluster node's console URL and cluster token. The backend `POST /cluster/join` endpoint was already implemented but had no frontend UI.
- **Cluster: Add Node with credentials** — the "Add Node" flow now accepts the remote node's console URL and admin credentials instead of a mysterious "node token". The local node authenticates to the remote node, verifies it is in standalone mode, and triggers the join automatically. Replaces the previous manual token-based workflow.
- **Cluster: Token display modal** — replaced the plain `alert()` shown after cluster initialization with a proper modal featuring a copy-to-clipboard button and an amber warning to save the token.
- **Cluster: Local node label** — the nodes table now shows "(This node)" next to the local node's name, and hides the delete button to prevent accidental self-removal.
- **Cluster: View cluster token** — new `GET /api/v1/cluster/token` endpoint (global admin only) and "Cluster Token" button in the cluster overview header. Previously the token was only shown once during initialization and could never be retrieved again.

### Removed
- **Legacy syslog/HTTP runtime code** — removed ~150 lines of dead code that never executed after `InitTargetStore` was introduced: `reconfigureLegacyOutputs()`, `configureLegacySyslog()`, `configureLegacyHTTP()`, `TestOutput()`, `testLegacySyslog()`, `testLegacyHTTP()`, the `/logs/test` API endpoint, and three legacy-only error sentinels. The one-time migration path (`MigrateFromSettings`) that converts old `logging.syslog_*`/`logging.http_*` keys into new `logging_targets` rows on first upgrade is preserved.

### Fixed
- **IDP tenant isolation** — Tenant admins could see global IDPs and IDPs from other tenants. `ListProviders` SQL query included `OR tenant_id IS NULL`; `handleGetIDP`, `handleUpdateIDP`, and `handleDeleteIDP` had a `TenantID != ""` bypass that granted access to global IDPs. All handlers now enforce strict tenant scoping: tenant admins can only list, view, update, and delete IDPs belonging to their own tenant.
- **IDP tenant column always showed "Global"** — `IdentityProvider` TypeScript type used `tenant_id` (snake_case) but backend JSON tag is `tenantId` (camelCase), so the field was always `undefined`. Fixed the type and all references (page, modal, tests). IDP list now resolves tenant IDs to display names via a tenants query.
- **No tenant selector when creating/editing IDPs** — Global admins had no way to assign an IDP to a specific tenant from the UI. Added a tenant dropdown to `CreateIDPModal` (only visible to global admins) with "Global (all tenants)" as default, populated from the tenants API. `handleSubmit` now includes `tenantId` in the payload; edit mode pre-populates the current value.
- **User handlers tenant isolation** — `handleUpdateUser` had no auth or tenant check: any authenticated user could modify any user's roles, status, email, or tenant assignment. `handleDeleteUser` had no auth or tenant check: could delete users from any tenant including global admins. Both handlers now require admin role and enforce strict tenant ownership for non-global admins. Only global admins can change tenant assignment.
- **Access key handlers tenant isolation** — `handleListAccessKeys`, `handleCreateAccessKey`, and `handleDeleteAccessKey` had no auth or tenant validation. A tenant admin could list, create, or revoke access keys for users in other tenants, including global admins. All three handlers now require admin role and verify that the target user belongs to the requester's tenant.
- **Bucket permission handlers missing auth** — `handleListBucketPermissions`, `handleGrantBucketPermission`, and `handleRevokeBucketPermission` had no authentication or authorization checks. Any authenticated user could list, grant, or revoke permissions on any bucket across all tenants. All three handlers now require admin role and verify bucket ownership via `GetBucketInfo` for tenant admins.
- **handleGetUser cross-tenant data leak** — Any authenticated user could retrieve details of users from other tenants by ID. Now requires admin role; tenant admins can only view users in their own tenant.
- **handleGetTenant cross-tenant data leak** — Any authenticated user could retrieve configuration details of any tenant by ID. Now requires admin role; tenant admins can only view their own tenant.
- **handleListTenantUsers cross-tenant data leak** — Any authenticated user could list all users of any tenant. Now requires admin role; tenant admins can only list users in their own tenant.
- **handleDeleteBucket tenant override bypass** — Accepted `tenantId` query parameter from any user without validating global admin status. A tenant admin could delete buckets from other tenants by passing their `tenantId`. Now only global admins can override tenant via query parameter.
- **handleListBucketShares / handleDeleteShare tenant override bypass** — Accepted `tenantId` query parameter without global admin validation. Now only global admins can override tenant context via query parameter.
- **Cluster: self-deletion allowed** — `handleRemoveClusterNode` had no validation preventing a node from removing itself from the cluster, leaving it in a broken state. Now returns 400 if the target node ID matches the local node ID, directing the user to use "Leave Cluster" instead.
- **Cluster: Add Node accepted already-clustered nodes** — `handleAddClusterNode` did not check if the remote node was already part of a cluster. Now queries the remote node's `/cluster/config` before joining and returns 409 Conflict if `is_cluster_enabled` is true.
- **Cluster overview bucket counts hardcoded to 0** — `GetClusterStatus` had a `// TODO` that set `TotalBuckets`, `ReplicatedBuckets`, and `LocalBuckets` to 0. The handler now queries `ListBuckets` and `GetRulesForBucket` to compute real values from local storage.

### Verified
- **Veeam Backup & Replication compatibility** — fully tested and operational with Veeam including S3 connection, backup jobs, and Instant Recovery workflows

---

## [0.9.0-beta] - 2026-02-17

### Security
- **CRITICAL**: JWT tokens were signed with `auth.secret_key` (`SecretKey`) instead of `auth.jwt_secret` (`JWTSecret`). `SecretKey` is meant for S3 default credentials, not JWT signing. If `secret_key` was not configured (the common case), JWTs were signed with an empty string. Fixed `parseBasicToken()` and `createBasicToken()` to use `JWTSecret`, which auto-generates a 32-char random string on startup when not explicitly configured.
- **CRITICAL**: JWT signature verification — `parseBasicToken()` now verifies HMAC-SHA256 signature with `hmac.Equal()` constant-time comparison before trusting payload
- **CRITICAL**: CORS wildcard removal — replaced hardcoded `Access-Control-Allow-Origin: *` with proper origin validation via `middleware.CORSWithConfig()`
- **CRITICAL**: Rate limiting IP spoofing — `IPKeyExtractor` now auto-trusts RFC 1918 private networks (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, ::1, fc00::/7) and only honors `X-Forwarded-For`/`X-Real-IP` from trusted proxies. Supports explicit CIDR ranges for public proxies (Cloudflare, AWS ALB)
- LDAP bind passwords and OAuth client secrets encrypted at rest with AES-256-GCM using config secret key
- LDAP search filter injection protection via `ldap.EscapeFilter()` (RFC 4515)
- OAuth CSRF protection with random state parameter + secure cookie validation on callback
- External users (LDAP/OAuth) have no password hash — `ValidateConsoleCredentials` rejects them with "Use SSO login"

### Added
- **Identity Provider System** — extensible framework for external authentication with LDAP/AD and OAuth2/OIDC support
  - `internal/idp/` package: Provider interface, factory registry, SQLite store, AES-256-GCM crypto, manager with CRUD/auth/import/sync
  - `internal/idp/ldap/` package: LDAP client (connect, bind, search, browse) with TLS/StartTLS support and AD attribute mapping
  - `internal/idp/oauth/` package: OAuth2 provider with Google and Microsoft presets, configurable claim mapping
  - Database migration 9: `identity_providers`, `idp_group_mappings` tables, `auth_provider` and `external_id` columns on `users`
- **IDP Console API** — 20+ new endpoints for identity provider management
  - CRUD: `GET/POST/PUT/DELETE /api/v1/identity-providers`, `POST /api/v1/identity-providers/{id}/test`
  - LDAP browse: `POST .../search-users`, `POST .../search-groups`, `POST .../group-members`, `POST .../import-users`
  - Group mappings: `GET/POST/PUT/DELETE .../group-mappings`, `POST .../group-mappings/{id}/sync`, `POST .../sync`
  - OAuth flow: `GET /api/v1/auth/oauth/{id}/login`, `GET /api/v1/auth/oauth/callback`, `GET /api/v1/auth/oauth/providers`
- **Login flow routing** — `handleLogin()` now routes by `auth_provider`: local (bcrypt), `ldap:*` (LDAP bind), `oauth:*` (rejects with SSO message)
- **Frontend: Identity Providers page** — full CRUD with type selector (LDAP/OAuth), config forms, test connection, status badges
- **Frontend: LDAP Browser** — search LDAP directory users, select and bulk import with role assignment
- **Frontend: Group Mapping Manager** — map external groups to MaxIOFS roles, manual and auto sync, sync status tracking
- **Frontend: OAuth login buttons** — login page shows SSO buttons for active OAuth providers (Google/Microsoft icons)
- **Frontend: OAuth complete page** — token receiver for OAuth callback redirect flow
- **Frontend: Auth provider badge** — user list shows Local/LDAP/SSO badge per user via `IDPStatusBadge` component
- **Frontend: Identity Providers sidebar entry** — added under Users submenu, visible to admins only
- **OAuth SSO auto-provisioning** — users auto-created on first SSO login if they belong to a mapped group; role resolved from group mappings (admin > user > readonly)
- **SSO one-button-per-type login** — login page shows one "Sign in with Google" / "Sign in with Microsoft" button regardless of how many providers are configured; user enters email first, backend resolves the correct provider
- **SSO start endpoint** — `POST /api/v1/auth/oauth/start` accepts email + preset, finds correct provider across tenants, redirects to OAuth provider with `login_hint`
- **SSO hint on login** — when a user types an email address in the password login form, backend returns `sso_hint: true` to highlight the SSO buttons
- **Cross-provider user lookup** — OAuth callback searches ALL active OAuth providers for existing users, not just the one used for authentication; enables multi-tenant SSO routing by email domain
- **Redirect URI auto-generation** — OAuth providers auto-fill `redirect_uri` from `PublicConsoleURL` if left empty; frontend shows the computed callback URL with copy hint
- **SSO user creation from Users page** — Create User modal includes an "Authentication" dropdown listing active OAuth providers; password field hidden for SSO users; email auto-populated from username
- **Email auto-sync for SSO users** — on each SSO login, if the user's email field is empty, it's populated from the OAuth provider profile; manual SSO user creation also auto-fills email from username
- **SSO documentation** — comprehensive `docs/SSO.md` covering setup guide (Google, Microsoft), authorization model (group-based + individual), multi-tenant configuration, login page behavior, error reference
- **Public version endpoint** — `GET /api/v1/version` returns server version without authentication; login page fetches version dynamically instead of relying on auth-required `/config` endpoint
- Default password change notification — backend returns `default_password: true` on login with admin/admin, frontend shows persistent amber security warning in notification bell linking to user profile
- `trusted_proxies` configuration option in `config.yaml` for public proxy IP/CIDR ranges
- **Tombstone-based cluster deletion sync** — solves the "entity resurrection" bug where deleted entities reappear after bidirectional sync
  - New `cluster_deletion_log` table (migration 10) stores tombstone records for all deleted entities
  - `RecordDeletion()`, `ListDeletions()`, `HasDeletion()`, `CleanupOldDeletions()` helper functions in `internal/cluster/deletion_log.go`
  - `DeletionLogSyncManager` pushes all tombstones to other cluster nodes every 30 seconds
  - Tombstones are authoritative: upsert handlers check for tombstones before accepting synced entities, preventing resurrection
  - All 6 entity types covered: users, tenants, access keys, bucket permissions, IDP providers, group mappings
  - Automatic tombstone cleanup after 7 days (configurable) via background goroutine
  - New internal cluster endpoint: `POST /api/internal/cluster/deletion-log-sync`
- **Cluster sync for IDP providers and group mappings** — identity providers and their group mappings now sync automatically between cluster nodes (same pattern as users/tenants/access keys)
- **Delete-sync endpoints for all entity types** — 6 new internal cluster endpoints for propagating deletions between nodes
- **JWT secret persistence across restarts** — auto-generated JWT secrets are now persisted to the `system_settings` DB table (key: `jwt_secret`, non-editable via UI). On restart, the persisted value is loaded instead of generating a new random secret, preserving all active user sessions. Priority order: explicit config/env var > persisted DB value > auto-generated (saved to DB).
- **Cluster JWT secret synchronization** — when a node joins a cluster, it automatically fetches the JWT secret from the existing node via the HMAC-authenticated `GET /api/internal/cluster/jwt-secret` endpoint. This ensures all cluster nodes share the same JWT signing key, so users authenticated on one node are valid on all nodes without re-login.
  - New `FetchJWTSecretFromNode()` method on cluster manager
  - New `SetJWTSecret()` method on auth manager for runtime secret updates
  - New `JWTSecretAutoGenerated` flag on `AuthConfig` to distinguish explicit vs auto-generated secrets
  - `handleJoinCluster` now fetches and applies the cluster JWT secret after successful join

### Fixed
- **CRITICAL**: Cluster deletion sync race condition — deleted entities no longer "resurrect" when another node pushes them back during its sync cycle. Previous `reconcileDeletions()` approach only worked if the deleting node was the origin node; replaced with tombstone-based architecture that works regardless of which node performs the delete
- **CRITICAL**: XSS via `dangerouslySetInnerHTML` in modal renderer — added `sanitizeHtml()` that strips script/iframe/embed tags and event handler attributes
- Goroutine leak in decryption pipeline — added context cancellation monitoring to unblock `pipeWriter` when caller abandons the reader
- Unbounded map growth in replication manager — `DeleteRule()` now cleans up `ruleLocks`, `processScheduledRules()` prunes stale `lastSync` entries
- Unchecked `crypto/rand.Read` error — added fallback to timestamp-only version ID on failure
- Toast notifications rendering in light theme when dark mode active — added missing CSS variable shades (success-900, error-900, warning-900)
- Notification badge count not clearing reactively after password change — converted to `useState` with custom event dispatch
- Audit logging errors silently ignored in 12 locations — added `logAuditEvent()` helper that logs warnings on failure
- Temp file handle leak on panic in `PutObject` — added `defer tempFile.Close()` immediately after creation
- Tag index deletion error ignored in `SetObjectTags` — `txn.Delete()` error now checked and propagated
- Cluster proxy request body consumed before forwarding — buffered with `io.ReadAll` + `bytes.NewReader` to prevent empty body on retry
- Storage delete error silently ignored on quota rollback in `PutObject` — now logs error to identify orphaned files
- Added React Error Boundary around protected routes to catch render crashes with recovery UI
- Duplicate username creation returned 500 with raw SQLite constraint error — now returns 409 Conflict with clear `"username 'x' already exists"` message and frontend shows "Duplicate entry" dialog
- Duplicate tenant name creation had the same raw SQLite error — now returns 409 Conflict with `"tenant name 'x' already exists"`
- **Login page UI** — removed visible background rectangles on "or continue with" divider and input hover states that clashed with the card design; divider now uses flex+gap layout instead of absolute-positioned text with opaque background
- **OAuth redirect method** — changed `startOAuthFlow` from `307 Temporary Redirect` to `302 Found`; 307 preserves the POST method and body, causing the form data (`email`, `preset`) to be forwarded to Google/Microsoft which reject unknown parameters
- **OAuth test connection response** — `handleTestIDPConnection` success response now wrapped in `APIResponse` format; frontend was reading `response.data.data` which was undefined without the wrapper
- **OAuth group sync username** — group sync handlers now use `member.Email` as username when provider type is `oauth2` instead of the LDAP-style username
- **Docker env vars silently ignored** — `MAXIOFS_JWT_SECRET`, `MAXIOFS_ENABLE_AUTH`, and `MAXIOFS_ENABLE_METRICS` in `docker-compose.yaml` did not match Viper's `MAXIOFS_` prefix + nested key convention. Fixed to `MAXIOFS_AUTH_JWT_SECRET`, `MAXIOFS_AUTH_ENABLE_AUTH`, and `MAXIOFS_METRICS_ENABLE` respectively. All 3 cluster nodes affected.
- Added cluster note to `config.example.yaml`: `jwt_secret` MUST be identical across all nodes
- Updated `DOCKER.md` and `docker/README.md` with correct env var names
- **Session invalidation on server restart** — previously, restarting the server generated a new random JWT secret, invalidating all existing sessions. Now the secret is persisted and reused.

### Removed
- Dead code: `GetPresignedURL` handler in `pkg/s3compat/presigned.go` — unused endpoint with hardcoded test credentials, never registered in any router
- Dead code: `CopyObjects`, `ExecuteBatchOperation`, `copyObject`, `parseJSONBody`, `writeJSONResponse` and related types (`CopyObjectsRequest`, `CopySource`, `CopyObjectsResult`, `CopySuccess`, `CopyFailure`, `BatchOperation`) from `pkg/s3compat/batch.go` — stub implementations never wired to routes
- Dead code: 13 associated test functions for the above removed code in `handler_coverage_test.go`
- Dead code: 10 unused utility functions from `web/frontend/src/lib/utils.ts` (`truncateText`, `generateRandomId`, `isValidEmail`, `getFileExtension`, `getMimeTypeIcon`, `calculateProgress`, `formatSpeed`, `formatDuration`, `debounce`, `throttle`)
- Dead code: `DarkModeToggle` component — duplicate of ThemeContext-based toggle already in use

### Changed
- Replaced `fmt.Println` with `logrus.Warn` in `internal/share/sqlite.go` for table migration logging

### Tests
- Deletion log: RecordDeletion (idempotency, multi-type), ListDeletions (filtering), HasDeletion, CleanupOldDeletions (TTL), DeletionLogSyncManager (new, stop, checksum, sync-to-node, server error), StartDeletionLogCleanup (12 tests)
- IDP provider sync: New, ListLocalProviders, ComputeChecksum, NeedsSynchronization, UpdateSyncStatus, Stop, SendProviderToNode, SyncProviderToNode, SyncLoop, Start, SendDeletionToNode (12 tests)
- Group mapping sync: New, ListLocalMappings, ComputeChecksum, NeedsSynchronization, UpdateSyncStatus, Stop, SendMappingToNode, SyncMappingToNode, SyncLoop, Start, SendDeletionToNode (12 tests)
- IDP crypto: encrypt/decrypt roundtrip, empty string, wrong key, tampered ciphertext, unique nonces, key derivation (9 tests)
- IDP store: SQLite CRUD for providers and group mappings, tenant filtering, cascade delete, unique constraint, sync time (17 tests)
- IDP manager: create/get/update/delete with encryption, masking, cache invalidation, group mapping CRUD (13 tests)
- Frontend IDP: page rendering, search filtering, delete confirmation, test connection, permission check, badge component (11 tests)
- OAuth provider: NewProvider with Google/Microsoft/custom presets, TestConnection field validation, GetAuthURL with/without login_hint, fetchUserInfo with mock HTTP (standard claims, custom claims, groups, missing email, errors), ExchangeCode error handling, ApplyGooglePreset (24 sub-tests)
- LDAP provider: EscapeFilter with LDAP injection prevention (8 cases), EntryToExternalUser with default/custom attribute mapping and fallback chain (uid→sAMAccountName→cn), getUserAttributes default vs custom, connection error handling for all provider methods, unsupported OAuth methods (14 tests)
- Server IDP handlers: resolveRoleFromMappings role priority (admin>user>readonly, 10 scenarios), CRUD handler auth/validation/404 checks, group mapping handlers, OAuth callback CSRF validation and state parsing, handleOAuthStart JSON/form data, handleListOAuthProviders preset deduplication, sync handlers, helper functions (getAuthUser, isAdmin, isGlobalAdmin) (55+ sub-tests)
- Server: `TestHandleReceiveIDPProviderSync`, `TestHandleReceiveIDPProviderDeleteSync`, `TestHandleReceiveGroupMappingSync`, `TestHandleReceiveGroupMappingDeleteSync`, `TestHandleReceiveDeletionLogSync` — 5 new handler tests (19 sub-tests) covering auth rejection, JSON validation, create/update/delete flows, and edge cases
- Auth: `TestJWTSecretPersistence_AutoGenerated`, `TestJWTSecretPersistence_ExplicitOverridesDB`, `TestJWTSecretPersistence_SetJWTSecret`, `TestJWTSecretPersistence_EmptyDB`, `TestJWTSecretPersistence_SystemSettingsTable` — 5 new tests covering JWT secret DB persistence, priority resolution, runtime updates, and system_settings row properties
- Config: `TestValidate_JWTSecretAutoGeneratedFlag` — 3 sub-tests verifying the auto-generated flag behavior (auto-generated sets flag, explicit does not, auth disabled does not)

### Verified (No Changes Needed)
- Race condition in cluster cache — all `c.entries` accesses already properly protected with `sync.RWMutex`
- Array bounds in S3 signature parsing — `parseS3SignatureV4` and `parseS3SignatureV2` already have proper bounds checks
- Path traversal with URL-encoded `%2e%2e%2f` — Go's `net/http` decodes before handlers, `validatePath` catches `..`, `filepath.Join` normalizes as defense-in-depth
- HTTP response body leak — all `resp.Body.Close()` already properly deferred

---

## [0.8.0-beta] - 2026-02-07

### Added
- Object Filters & Advanced Search — new `GET /api/v1/buckets/{bucket}/objects/search` endpoint with server-side filtering by content-type (prefix match), size range, date range, and tags (AND semantics)
- Frontend filter panel with content-type checkboxes, size range with unit selector, date range inputs, and dynamic tag management
- Object Lock default retention storage — bucket-level default retention configuration completing the Object Lock feature set
- Bucket policy enforcement — complete AWS S3-compatible policy evaluation engine (Default Deny → Explicit Allow → Explicit Deny)
- Presigned URL signature validation — AWS Signature V4 and V2 validation preventing unauthorized access and parameter tampering
- Cluster join functionality — multi-step join protocol for dynamic cluster expansion
- Cross-node bucket aggregation — web console and S3 API show buckets from all cluster nodes
- Cross-node storage quota aggregation — tenant quotas enforced cluster-wide instead of per-node
- Production hardening: rate limiting (token bucket, 100 req/s), circuit breakers (3-state), and cluster metrics
- Node location column in bucket list UI with health status indicators
- Comprehensive test coverage expansion across all modules (metadata 87.4%, object 77.3%, server 66.1%, cmd 71.4%)
- Version check notification — global admins see a badge in the sidebar indicating if a new release is available (fetches from maxiofs.com/version.json), clickable to downloads page

### Fixed
- Dark mode toggle now uses ThemeContext instead of a separate localStorage-based system, eliminating UI freeze on theme switch and persisting the preference to the user's profile via API
- CRITICAL: Cluster replication now replicates real object data (was metadata-only)
- CRITICAL: Tenant storage quota bypass vulnerability in multi-node clusters
- CRITICAL: ListBuckets returns empty in standalone mode when cluster enabled
- CRITICAL: Cluster authentication failure due to incorrect column name (`status` → `health_status`)
- Tenant unlimited storage quota incorrectly defaulting to 100GB
- Bucket replication workers not processing queue items on startup
- Database lock contention in cluster replication under high concurrency
- AWS SDK v2 endpoint configuration migrated from deprecated resolver
- Multiple server test suite failures (15+ tests corrected)
- Dead code cleanup — removed unused functions identified by staticcheck

### Changed
- CreateBucket now uses actual creator's user ID as owner (AWS S3 compatible, was hardcoded "maxiofs")
- Cluster aggregators refactored to use interfaces for testability
- CheckTenantStorageQuota enhanced with cluster-aware logic and fallback
- Circuit breakers integrated into cluster aggregators for all node communications

## [0.7.0-beta] - 2026-01-16

### Added
- Performance benchmarking suite with `make bench` command for storage and encryption operations
- RPM package generation for RHEL/CentOS/Fedora distributions (AMD64 and ARM64)
- Database migration system with automatic schema upgrades on application startup
- AWS-compatible access key format (AKIA prefix, 20-character IDs) for better S3 tool compatibility
- Bucket inventory reports with automated daily/weekly generation in CSV or JSON format
- Cluster bucket migration feature to move buckets between nodes with progress tracking
- Automatic access key synchronization across cluster nodes
- Automatic bucket permission synchronization across cluster nodes
- Comprehensive bucket migration including objects, metadata, permissions, ACLs, and configuration
- Bucket inventory UI in bucket settings with schedule configuration and report history
- Real-time performance dashboards with specialized metrics views (Overview, System, Storage, API, Performance)
- Enhanced Prometheus metrics with 9 new performance indicators (P50/P95/P99 latencies, throughput, success rates)
- Comprehensive alerting rules for performance degradation and SLO violations in Prometheus
- Production pprof profiling endpoints with global admin authentication (fixed security)
- K6 load testing suite with upload, download, and mixed workload scenarios (10,000+ operations)
- Performance documentation with Windows vs Linux baseline analysis

### Changed
- Access keys now use AWS-compatible format (existing keys continue to work)
- Test coverage improved from 25.8% to 36.2%

## [0.6.2-beta] - 2026-01-01

### Added
- API root endpoint (GET /api/v1/) for API discovery and endpoint listing
- MIT License file

### Fixed
- CRITICAL: Debian package upgrades now preserve config.yaml to prevent encryption key loss and data corruption
- Console API documentation corrected to show proper /api/v1/ prefix for all endpoints
- AWS Signature V4 authorization header parsing for S3 compatibility
- Timestamp validation now works correctly across all timezones
- S3 ARN generation for bucket root listings

### Changed
- Metrics dashboard redesigned with 5 specialized tabs (Overview, System, Storage, API & Requests, Performance)
- Historical data filtering with time range selector (real-time to 1 year)
- Improved UI consistency across all pages with standardized metric cards and table styling
- Auth module test coverage increased from 30.2% to 47.1%

### Removed
- SweetAlert2 dependency replaced with custom modal components (reduced bundle size by 65KB)

## [0.6.1-beta] - 2025-12-24

### Changed
- Build requirements updated to Node.js 24+ and Go 1.25+ for latest security patches
- Frontend dependencies upgraded to Tailwind CSS v4 (10x faster build) and Vitest v4 (59% faster tests)
- Docker Compose reorganized with profiles for monitoring and cluster setups (74% file size reduction)
- Frontend test performance improved from 21.7s to 9.0s
- S3 API test coverage increased from 30.9% to 45.7%

### Fixed
- Documentation corrected from "Next.js" to "React" references throughout
- Modal backdrop opacity for Tailwind CSS v4 compatibility
- S3 operation tracking in tracing middleware (PUT/GET/DELETE now properly tracked)
- Success rate percentage display bug (was showing 10000% instead of 100%)

### Added
- K6 load testing suite with upload, download, and mixed workload tests
- Prometheus metrics integration with 9 new performance metrics
- Grafana unified dashboard with 14 panels for monitoring
- Performance baselines established (Linux production: p95 <10ms for all operations)
- Prometheus alert rules for performance degradation and SLO violations
- Docker profiles for conditional deployment (monitoring, cluster, full stack)

### Removed
- Legacy Next.js server code (nextjs.go - unused 118 lines)

## [0.6.0-beta] - 2025-12-09

### Added
- Cluster bucket replication system with HMAC authentication between nodes
- Automatic tenant synchronization across cluster nodes every 30 seconds
- Cluster management UI with node health monitoring and status dashboard
- Bucket location cache with automatic failover to healthy nodes
- Manual "Sync Now" button for triggering bucket replication on demand
- Bulk node-to-node replication configuration
- Smart request routing with automatic failover to healthy cluster nodes

### Changed
- Bucket replication now uses AWS SDK v2 for real S3 transfers
- Background scheduler automatically syncs buckets based on configured intervals

## [0.5.0-beta] - 2025-12-04

### Added
- Bucket replication system with realtime, scheduled, and batch modes to AWS S3, MinIO, or other MaxIOFS instances
- Production logging infrastructure with console, file, HTTP, and syslog output targets
- User-customizable themes (System, Dark, Light) with persistent preferences
- Nightly build pipeline with multi-architecture support (linux/darwin/windows, amd64/arm64)
- Frontend testing infrastructure with 64 tests using Vitest and React Testing Library
- Expanded test coverage across ACL, middleware, lifecycle, storage, metadata, bucket, and object modules

### Fixed
- Frontend session management bugs causing unexpected logouts and page reload issues
- VEEAM SOSAPI capacity reporting now respects tenant quotas
- ListObjectVersions returning empty results for non-versioned buckets

### Changed
- Backend test coverage improved to 53% with 531 passing tests
- S3 API test coverage increased from 16.6% to 30.9%

## [0.4.2-beta] - 2025-11-24

### Added
- **Real-Time Push Notifications (SSE)** - Server-Sent Events system for admin notifications with automatic user locked alerts
- **Dynamic Security Configuration** - Configurable rate limiting and account lockout thresholds without server restart
- **Global Bucket Uniqueness** - AWS S3 compatible global bucket naming across all tenants
- **S3-Compatible URLs** - Standard S3 URL format without tenant prefix for presigned and share URLs
- **Bucket Notifications (Webhooks)** - AWS S3 compatible event notifications with HTTP POST delivery and retry mechanism

### Fixed
- Rate limiter double-counting bug causing premature blocking
- Failed attempts counter not resetting after account lockout
- Security page not showing locked users count
- SSE callback execution issues and frontend connection problems
- Streaming support for SSE endpoint

### Changed
- Presigned URLs and share URLs now use standard S3 path format
- Automatic tenant resolution in S3 API calls

## [0.4.1-beta] - 2025-11-18

### Added
- **Server-Side Encryption (SSE)** - AES-256-CTR streaming encryption with persistent master key storage
- Dual-level encryption control (server and bucket level)
- Flexible mixed encrypted/unencrypted object coexistence
- Configuration management migrated to SQLite database
- Pebble (LSM-tree engine) for metrics historical storage (migrated from initial BadgerDB in v1.0.0)
- Visual encryption status indicators in Web Console

### Fixed
- Tenant menu visibility for non-admin users
- Global admin privilege escalation vulnerability
- Password change detection in backend
- Non-existent bucket upload prevention
- Small object encryption handling

### Changed
- Master key stored in config.yaml with validation on startup
- Automatic decryption on GetObject for encrypted objects
- Settings now persist across server restarts in database

## [0.4.0-beta] - 2025-11-15

### Added
- **Complete Audit Logging System** - SQLite-based audit log with 20+ event types
- RESTful API endpoints for audit log management with advanced filtering
- Professional frontend UI with filtering, search, CSV export, and responsive design
- Automatic retention policy with configurable days (default: 90 days)
- Comprehensive unit tests with 100% core functionality coverage

### Changed
- Audit logs stored separately from metadata with indexed searches
- Multi-tenancy support in audit logs (tenant admins see only their tenant)

## [0.3.2-beta] - 2025-11-10

### Added
- **Two-Factor Authentication (2FA)** - TOTP-based 2FA with QR code generation and backup codes
- **Prometheus & Grafana Monitoring** - Metrics endpoint with pre-built dashboards
- **Docker Support** - Complete containerization with Docker Compose setup
- Bucket pagination and responsive frontend design
- Configurable object lock retention days per bucket

### Fixed
- **Critical: Versioned Bucket Deletion** - ListObjectVersions now properly shows delete markers
- **HTTP Conditional Requests** - Implemented If-Match and If-None-Match headers for efficient caching
- S3 API tenant quota handling
- ESLint warnings across frontend

### Changed
- S3 API compatibility improved to 100%
- All dependencies upgraded to latest versions

## [0.3.1-beta] - 2025-11-05

### Added
- Debian package support with installation scripts
- ARM64 architecture support for cross-platform builds
- Session management with idle timer and automatic expiration

### Fixed
- Object deletion issues and metadata cleanup
- Object Lock GOVERNANCE mode enforcement
- Session timeout configuration application
- URL redirects for reverse proxy deployments with base path
- Build system for Debian and ARM64 compilation

## [0.3.0-beta] - 2025-10-28

### Added
- **Bucket Tagging Visual UI** - Key-value interface without XML editing
- **CORS Visual Editor** - Dual-mode interface with form-based configuration
- **Complete Bucket Policy** - Full PUT/GET/DELETE operations with JSON validation
- Enhanced Policy UI with 4 pre-built templates
- Object versioning with delete markers
- Lifecycle policy improvements

### Fixed
- Bucket policy JSON parsing with UTF-8 BOM handling
- Policy fields now accept both string and array formats
- Lifecycle form loading correct values from backend
- CORS endpoints using correct API client
- Data integrity for delete markers and version management

### Changed
- Beta status achieved with all core S3 operations validated
- All AWS CLI commands fully supported

## [0.2.5-alpha] - 2025-10-25

### Added
- CopyObject S3 API with metadata preservation and cross-bucket support
- UploadPartCopy for multipart operations on files >5MB
- Modern login page design with dark mode support

### Fixed
- CopyObject routing and source format parsing
- Binary file corruption during copy operations

## [0.2.4-alpha] - 2025-10-19

### Added
- Comprehensive stress testing with MinIO Warp (7000+ objects)
- BadgerDB transaction retry logic for concurrent operations
- Metadata-first deletion strategy

### Fixed
- BadgerDB transaction conflicts
- Bulk delete operations handling up to 1000 objects per request

## [0.2.3-alpha] - 2025-10-13

### Added
- Complete S3 API implementation (40+ operations)
- Web Console with dark mode support
- Dashboard with real-time metrics
- Multi-tenancy with resource isolation
- Bucket management features
- Security audit page

### Changed
- Migrated from SQLite to BadgerDB for object metadata

## [0.2.0-dev] - 2025-10

### Initial Release
- Basic S3-compatible API
- Web Console with React frontend
- SQLite for metadata storage
- Filesystem storage backend
- Multi-tenancy foundation
- User and access key management

---

## Versioning Strategy

MaxIOFS follows semantic versioning:
- **0.x.x-alpha**: Alpha releases - Feature development
- **0.x.x-beta**: Beta releases - Feature complete, testing phase
- **0.x.x-rc**: Release candidates - Production-ready testing
- **1.x.x**: Stable releases - Production-ready

### Current Status: RELEASE CANDIDATE (v1.0.0-rc1)

**Completed Core Features:**
- ✅ All S3 core operations validated with AWS CLI (100% compatible)
- ✅ Multi-node cluster support with real object replication
- ✅ Tombstone-based cluster deletion sync (all 6 entity types)
- ✅ LDAP/AD and OAuth2/OIDC identity provider system with SSO
- ✅ Object Filters & Advanced Search
- ✅ Production monitoring (Prometheus, Grafana, performance metrics)
- ✅ Server-side encryption (AES-256-GCM authenticated encryption)
- ✅ Bucket policy enforcement (AWS S3-compatible evaluation engine)
- ✅ Presigned URL signature validation (V4 and V2)
- ✅ Audit logging and compliance features
- ✅ Two-Factor Authentication (2FA)
- ✅ Comprehensive security audit — 169 files reviewed, 24 vulnerabilities fixed

**Current Metrics:**
- S3 API Compatibility: ~99% (full audit completed March 2026 — 20 issues identified and resolved)
- Backend Test Coverage: ~75% (at practical ceiling)
- Frontend Test Coverage: 100%
- Performance: P95 <10ms (Linux production)

**Path to v1.0.0 Stable:**
See [TODO.md](TODO.md) for detailed roadmap and requirements.

---

## Version History

### Completed Features (v0.1.0 - v1.0.0-rc1)

**v1.0.0-rc1 (March 2026)** - Comprehensive Security Audit, AES-256-GCM Encryption, CSR-based Cluster TLS, SSRF Hardening
- ✅ 169-file security audit — 2 CRITICAL, 8 HIGH, 5 MEDIUM, 1 LOW vulnerabilities identified and fixed
- ✅ AES-256-GCM authenticated encryption (replaces AES-CTR, backward-compatible)
- ✅ CSR-based cluster join — CA private key never transmitted over the network
- ✅ SSRF protection on webhooks, HTTP logging targets, and replication endpoints
- ✅ Frontend bundle reduced 45% via React.lazy code splitting

**v1.0.0-beta (February–March 2026)** - Pebble Metadata Engine, S3 Compatibility Audit, Cluster Snapshot Reconciliation & Veeam Compatibility
- ✅ Full S3 compatibility audit — 20 issues identified and resolved (ListObjectsV2, encoding-type, presigned URLs, ETag normalization, XML namespaces, SigV2 canonical headers, StorageClass, conditional headers, and more)
- ✅ Replaced BadgerDB with Pebble (crash-safe WAL, no CGO) for all S3 metadata
- ✅ Transparent auto-migration from BadgerDB on first startup (no manual steps)
- ✅ Cluster state snapshot endpoint and stale reconciler (LWW partition handling)
- ✅ Automatic inter-node TLS encryption with auto-generated internal CA
- ✅ Certificate auto-renewal with hot-swap (no restart needed)
- ✅ Veeam Backup & Replication fully tested (connection, backup, Instant Recovery)

**v0.9.0-beta (February 2026)** - Identity Providers, SSO & Cluster Deletion Sync
- ✅ LDAP/Active Directory and OAuth2/OIDC identity provider system
- ✅ SSO login flow (Google, Microsoft presets) with auto-provisioning
- ✅ IDP management UI (CRUD, LDAP browser, group mappings, test connection)
- ✅ Tombstone-based cluster deletion sync (prevents entity resurrection)
- ✅ Cluster sync for all 6 entity types with delete propagation
- ✅ 3 critical security fixes (JWT verification, CORS, rate limiting IP spoofing)
- ✅ XSS fix, dead code cleanup, 190+ new tests

**v0.8.0-beta (February 2026)** - Object Search, Security Fixes & Production Hardening
- ✅ Object Filters & Advanced Search (content-type, size, date, tags)
- ✅ Bucket policy enforcement and presigned URL signature validation
- ✅ Cluster replication with real object data
- ✅ Cross-node quota enforcement and bucket aggregation
- ✅ Production hardening (rate limiting, circuit breakers, cluster metrics)

**v0.6.0-beta (December 2025)** - Multi-Node Cluster & Replication
- ✅ Multi-node cluster support with intelligent routing
- ✅ Node-to-node HMAC-authenticated replication
- ✅ Automatic failover and health monitoring
- ✅ Bucket location caching for performance
- ✅ Cluster management web console

**v0.5.0-beta (December 2025)** - S3-Compatible Replication
- ✅ S3-compatible bucket replication (AWS S3, MinIO, MaxIOFS)
- ✅ Real-time, scheduled, and batch replication modes
- ✅ Queue-based async processing
- ✅ Production-ready logging system

**v0.4.2-beta (November 2025)** - Notifications & Security
- ✅ Bucket notifications (webhooks)
- ✅ Dynamic security configuration
- ✅ Real-time push notifications (SSE)
- ✅ Global bucket uniqueness

**v0.4.1-beta (November 2025)** - Encryption at Rest
- ✅ Server-side encryption (AES-256-CTR)
- ✅ SQLite-based configuration management
- ✅ Visual encryption indicators

**v0.4.0-beta (November 2025)** - Audit & Compliance
- ✅ Complete audit logging system (20+ event types)
- ✅ CSV export and filtering
- ✅ Automatic retention policies

**v0.3.2-beta (November 2025)** - Security & Monitoring
- ✅ Two-Factor Authentication (2FA/TOTP)
- ✅ Prometheus & Grafana integration
- ✅ Docker support with Compose
- ✅ Object Lock (COMPLIANCE/GOVERNANCE)

**v0.3.0-beta (October 2025)** - Advanced Features Already Implemented
- ✅ Object immutability (Object Lock GOVERNANCE/COMPLIANCE modes)
- ✅ Advanced RBAC with custom bucket policies (JSON-based S3-compatible policies)
- ✅ Tenant resource quotas (MaxStorageBytes, MaxBuckets, MaxAccessKeys)
- ✅ Multi-region replication (cluster replication + S3 replication to external endpoints)
- ✅ Parallel multipart upload (fully functional multipart API)
- ✅ Complete ACL system (canned ACLs + custom grants)

**See version history above for complete feature details**

---

## Future Development

For upcoming features, roadmap, and development plans, see [TODO.md](TODO.md).

**Quick Links:**
- [Current Sprint & Priorities](TODO.md#-current-sprint)
- [Feature Roadmap](TODO.md#-high-priority)
- [Test Coverage Goals](TODO.md#test-coverage-expansion)
- [Completed Features](TODO.md#-completed-features)

---

**Note**: This project is currently in BETA phase. Suitable for development, testing, and staging environments. Production use requires extensive testing. Always backup your data.
