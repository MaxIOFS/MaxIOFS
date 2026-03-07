# Changelog

All notable changes to MaxIOFS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

## [Unreleased]

### Fixed
- **Refresh token never saved after 2FA login** — `verify2FA` in `api.ts` mapped the backend's `token` field but silently discarded the `refresh_token` field returned by `POST /auth/2fa/verify`. As a result, any session started through a 2FA challenge had no refresh token stored, so `TokenManager` could not renew it; the session terminated 15 minutes after login (access token TTL) with no warning. `LoginResponse.refreshToken` is now populated from `response.data.refresh_token` and forwarded to `tokenManager.setTokens()`. (`web/frontend/src/lib/api.ts`)
- **Refresh token never saved after OAuth/SSO login** — `oauth-complete.tsx` only read the `token` URL query parameter set by the OAuth callback redirect, ignoring the `refresh_token` parameter that the backend was already including. Sessions started via OAuth/SSO therefore lacked a refresh token and expired after 15 minutes. The page now reads `refresh_token`, stores it in `localStorage`, and persists it as a `Secure; SameSite=Strict` cookie alongside the access token. (`web/frontend/src/pages/auth/oauth-complete.tsx`)
- **Audit logs export only fetched the current page** — `exportToCSV` used `filteredLogs` (the records already loaded for display, capped to the current page). For datasets larger than one page, the exported CSV silently omitted all other records. The function now loops through pages 1…N via `APIClient.getAuditLogs({ page, pageSize: 100 })`, concatenating each chunk, and stops when a chunk contains fewer than 100 records. The backend page-size cap (100) is preserved; no backend changes were made. (`web/frontend/src/pages/audit-logs/index.tsx`)
- **Audit logs stats cards showed counts for current page only** — the success/failed counters above the table were derived by calling `getAuditLogs` with `pageSize: 1000` (silently capped to 100 by the backend) and then manually counting matching records in the returned array. On datasets with more than 100 records the counts were wrong. Replaced with two lightweight queries (`pageSize: 1`) filtered by `status: 'success'` and `status: 'failed'` respectively; the displayed value is read from the `total` field of each response, which the backend always computes from the full result set. (`web/frontend/src/pages/audit-logs/index.tsx`)
- **CSV export: timestamp column split by comma** — `formatTimestamp` used `toLocaleString()`, which in most locales produces a string such as `"3/5/2026, 2:30:00 PM"`. The embedded comma caused CSV parsers to split the timestamp across two columns, corrupting every subsequent column in the row. Replaced with a manual formatter producing `YYYY-MM-DD HH:MM:SS` (zero-padded, 24-hour, no comma). The same formatter is used for timestamps displayed in the table. (`web/frontend/src/pages/audit-logs/index.tsx`)

### Security
- **Frontend: auth cookies missing `Secure` and `SameSite=Strict`** — `auth_token` and `refresh_token` cookies set by `TokenManager.setTokens()` and `oauth-complete.tsx` lacked both the `Secure` attribute (cookie transmitted over plain HTTP) and `SameSite=Strict` (CSRF risk). Both attributes are now set on every cookie write and deletion. (`web/frontend/src/lib/api.ts`, `web/frontend/src/pages/auth/oauth-complete.tsx`)
- **Frontend: incomplete `sanitizeHtml()` allowed XSS via JAVASCRIPT: and data: URIs** — the previous implementation only blocked lowercase `javascript:` and did not handle `JAVASCRIPT:`, mixed-case variants, `data:` URIs, or HTML-entity-encoded schemes (e.g. `&#106;avascript:`). Rewrote the function to normalize encoded characters, collapse whitespace, and run a case-insensitive regex against the full set of dangerous schemes before allowing the value through. (`web/frontend/src/lib/modals.tsx`)
- **Frontend: server-supplied values interpolated into HTML templates without escaping** — several pages built `innerHTML` strings by directly concatenating API response values (bucket names, user names, configuration fields) without sanitization, creating a stored XSS vector if any of those values contained `<`, `>`, `"`, `'`, or `&`. Added `escapeHtml()` to `src/lib/utils.ts` and applied it to all affected interpolation sites. (`web/frontend/src/lib/utils.ts`, `web/frontend/src/pages/buckets/[bucket]/index.tsx`, `web/frontend/src/pages/buckets/create.tsx`, `web/frontend/src/pages/users/[user]/index.tsx`)
- **Frontend: debug `console.log` statements leaked API response data** — two files contained `console.log` calls that printed full API responses (including user data and settings) to the browser console in production builds, visible to anyone with DevTools access. Both statements removed. (`web/frontend/src/pages/buckets/[bucket]/settings.tsx`, `web/frontend/src/pages/users/[user]/index.tsx`)
- **SSRF prevention in replication destination endpoint** — `CreateRule` and `UpdateRule` now validate `DestinationEndpoint` via `validateReplicationEndpoint`: must be an `http`/`https` URL and must not resolve to a loopback (127.0.0.0/8, ::1), private (10/8, 172.16/12, 192.168/16), link-local (169.254/16, fe80::/10), ULA (fc00::/7), unspecified (0.0.0.0/8), or cloud metadata (169.254.169.254) address. `NewS3RemoteClient` now injects an SSRF-blocking `*http.Client` into the AWS SDK config: the custom `DialContext` performs DNS resolution before connecting and rejects any resolved IP in the above ranges; HTTP redirects are also forbidden. This prevents an admin with replication-rule write access from using the replication worker as an SSRF proxy to reach internal services or cloud metadata endpoints. (`internal/replication/s3client.go`, `internal/replication/manager.go`)
- **Missing authorization on cluster management endpoints** — `handleInitializeCluster`, `handleJoinCluster`, `handleLeaveCluster`, `handleAddClusterNode`, `handleUpdateClusterNode`, and `handleRemoveClusterNode` were accessible to any authenticated user; only `handleGetClusterToken` had a global-admin guard. All six mutating cluster handlers now require `isGlobalAdmin`. Additionally, `handleAddClusterNode` validates the caller-supplied `endpoint` field (must be `http`/`https` with a non-empty host; the cloud metadata address `169.254.169.254` is explicitly rejected) before any outbound HTTP connection is made. RFC-1918 private addresses remain permitted so that LAN cluster deployments continue to work. (`internal/server/cluster_handlers.go`)
- **[CRITICAL] CA private key no longer transmitted during cluster join** — `handleValidateClusterToken` now omits `ca_key` from its JSON response. Instead, the joining node generates its own ECDSA P-256 key pair locally (via `GenerateKeyAndCSR`) and submits a CSR to the new `POST /api/internal/cluster/sign-csr` endpoint. The existing node signs the CSR with its local CA key and returns only the signed certificate (`node_cert`). The CA private key never leaves the originating node. (`internal/cluster/tls.go`, `internal/cluster/manager.go`, `internal/server/cluster_handlers.go`, `internal/server/server.go`)
- **Proxy loop prevention** — `ProxyRequest` now rejects requests that already carry `X-MaxIOFS-Proxied: true` with an immediate error, preventing infinite forwarding loops (node A → B → A → ...). (`internal/cluster/proxy.go`)
- **Replication destination secret keys encrypted at rest** — `CreateRule` and `UpdateRule` now encrypt `destination_secret_key` with AES-256-GCM before writing to SQLite (stored as `enc1:<base64>`). All read paths (`GetRule`, `ListRules`, `GetRulesForBucket`, `Worker.getRule`) decrypt transparently. Legacy plaintext values remain readable until the rule is next updated (zero-migration-script upgrade path). The encryption key is derived from `auth.secret_key` (or `auth.jwt_secret` as fallback). (`internal/replication/credentials.go` [new], `internal/replication/manager.go`, `internal/replication/worker.go`, `internal/replication/types.go`, `internal/server/server.go`)
- **Cryptographically secure nonces for cluster HMAC signatures** — `generateNonce()` in `ClusterAuthMiddleware` and `SignClusterRequest` in `ProxyClient` both previously used `time.Now().UnixNano()` (deterministic, guessable). Both now use 16 random bytes from `crypto/rand`, making nonces effectively unguessable. (`internal/middleware/cluster_auth.go`, `internal/cluster/proxy.go`)

### Added
- **Static website hosting (`?website` + serving middleware)** — `GET/PUT/DELETE /?website` now fully persist the `WebsiteConfiguration` (IndexDocument, ErrorDocument, RoutingRules) in the Pebble metadata store, replacing the previous no-op stubs. A new HTTP middleware (`websiteServingMiddleware`) intercepts requests whose `Host` header matches `{bucket}.{website_hostname}` and serves objects directly as HTML/web assets — no S3 authentication required. Path logic mirrors AWS S3: directory paths append the `IndexDocument.Suffix`, missing objects fall back to the `ErrorDocument.Key` with a `404` status code, and prefix-based `RoutingRules` trigger HTTP redirects. Enable the feature by setting `website_hostname` in `config.yaml` (e.g. `s3-website.example.com`) and pointing a wildcard DNS record to the MaxIOFS server. (`internal/metadata/types.go`, `internal/bucket/types.go`, `internal/bucket/interface.go`, `internal/bucket/adapter.go`, `internal/bucket/manager_impl.go`, `pkg/s3compat/bucket_ops.go`, `internal/server/server.go`, `internal/config/config.go`, `config.example.yaml`)
- **`POST /api/v1/auth/refresh`** — new public endpoint that exchanges a valid refresh token for a fresh token pair (`access_token` + `refresh_token` + `expires_in`). Enables the frontend to silently renew the session before the 15-min access token expires. The endpoint is exempt from authentication middleware (no `Authorization` header required). (`internal/server/console_api.go`)
- **`GET /api/v1/buckets/{bucket}/encryption`** — retorna la configuración SSE del bucket más los flags `serverEncryptionEnabled` y `serverEncryptionKeyConfigured` para que el frontend muestre advertencias si la llave maestra no está activa. (`internal/server/console_api.go`)
- **`PUT /api/v1/buckets/{bucket}/encryption`** — configura cifrado AES-256 o aws:kms a nivel de bucket. **Rechaza la petición (400) si `enable_encryption` no está activo en `config.yaml`**, evitando que se almacene metadata de cifrado sin la llave maestra permanente que lo respalde (de lo contrario los objetos quedarían corruptos tras un reinicio). (`internal/server/console_api.go`)
- **`DELETE /api/v1/buckets/{bucket}/encryption`** — elimina la configuración SSE del bucket, volviendo al comportamiento del servidor. (`internal/server/console_api.go`)
- **`GET /api/v1/buckets/{bucket}/object-lock`** — retorna el estado completo de Object Lock (enabled + regla de retención por defecto). Antes sólo existía el PUT. (`internal/server/console_api.go`)
- **`GET /api/v1/buckets/{bucket}/public-access-block`** — retorna la configuración de Public Access Block (los 4 flags). (`internal/server/console_api.go`)
- **`PUT /api/v1/buckets/{bucket}/public-access-block`** — actualiza los 4 flags de bloqueo de acceso público; persiste en metadata via `UpdateBucket`. (`internal/server/console_api.go`)
- **`DELETE /api/v1/buckets/{bucket}/public-access-block`** — elimina la configuración de Public Access Block, dejando todos los flags a false (acceso abierto). (`internal/server/console_api.go`)
- **`ListObjectsV2` handler** (`GET /{bucket}?list-type=2`) — dedicated V2 handler with proper `ContinuationToken`/`NextContinuationToken`, `StartAfter`, `KeyCount`, and `fetch-owner` parameter support. AWS CLI v2, AWS SDK v3 (Go/Python/JavaScript), and all modern clients that default to `list-type=2` now paginate correctly. (`pkg/s3compat/handler.go`, `internal/api/handler.go`)
- **`encoding-type=url` support** in `ListObjects` V1/V2 and `ListObjectVersions` — URL-encodes `Key`, `Prefix`, `Delimiter`, `Marker`, and `NextMarker` fields when the `?encoding-type=url` query parameter is present. Invalid values return `400 InvalidArgument`. (`pkg/s3compat/handler.go`, `pkg/s3compat/versioning.go`)
- **`x-amz-metadata-directive` in `CopyObject`** — `COPY` (default) preserves source metadata; `REPLACE` uses request headers (`Content-Type`, `x-amz-meta-*`) as new metadata, enabling ETL-style copy-and-relabel in one request. (`pkg/s3compat/object_ops.go`)
- **Copy-source conditional headers in `CopyObject`** — `x-amz-copy-source-if-match`, `x-amz-copy-source-if-none-match`, `x-amz-copy-source-if-modified-since`, `x-amz-copy-source-if-unmodified-since`. Returns `412 PreconditionFailed` when conditions are not met. (`pkg/s3compat/object_ops.go`)
- **`If-Modified-Since` and `If-Unmodified-Since`** conditional headers in `GetObject` and `HeadObject` — returns `304 Not Modified` or `412 Precondition Failed` per RFC 7232; date conditions evaluated after ETag per spec precedence. Used by `aws s3 sync`, Veeam backup verification, and download managers to skip unchanged objects. (`pkg/s3compat/handler.go`)
- **`StorageClass` persistence** — `x-amz-storage-class` header value is stored during `PutObject` and `CreateMultipartUpload` and returned correctly in all listing operations (`ListObjects` V1/V2, `ListObjectVersions`, `ListParts`, `ListMultipartUploads`). Defaults to `STANDARD` when not set. (`internal/object/manager.go`, `pkg/s3compat/handler.go`, `pkg/s3compat/versioning.go`, `pkg/s3compat/multipart.go`)
- **`BucketAlreadyOwnedByYou` error code** — `CreateBucket` now fetches the existing bucket owner and distinguishes between a bucket already owned by the requesting user (`BucketAlreadyOwnedByYou`, 409) and one owned by a different user (`BucketAlreadyExists`, 409), matching AWS S3 semantics. (`pkg/s3compat/handler.go`)
- **`X-Amz-Request-Id` and `X-Amz-Id-2` headers on auth-failure responses** — `writeS3Error()` in the auth middleware generates and sets both tracing headers before `WriteHeader` on every error response (401, 403, 5xx). Absent these headers, Veeam and other enterprise S3 clients may reject the endpoint as non-compliant during connection validation. (`internal/auth/manager.go`)

### Changed
- **`writeXMLResponse` prepends XML declaration** — all successful XML responses (`ListBuckets`, `ListObjects`, `GetBucketVersioning`, `CompleteMultipartUpload`, etc.) now include `<?xml version="1.0" encoding="UTF-8"?>`. Strict XML parsers (some Java SDK versions, XML validators) no longer fail on missing declaration. (`pkg/s3compat/handler.go`)
- **`ListBucketResult` and `ListBucketResultV2` carry S3 namespace** — both structs now use the `http://s3.amazonaws.com/doc/2006-03-01/` XML namespace, rendering as `<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">` per the AWS S3 spec. (`pkg/s3compat/handler.go`)
- **`GetBucketLocation` now returns a proper XML document** — replaced raw string `<LocationConstraint>` with a `LocationConstraintResponse` struct carrying the `http://s3.amazonaws.com/doc/2006-03-01/` namespace and rendered via `writeXMLResponse` (includes XML declaration). (`pkg/s3compat/handler.go`)
- **`CompleteMultipartUpload` `<Location>` field is now an absolute URL** — mirrors the request style (virtual-hosted or path-style) and uses `publicAPIURL` as fallback, instead of the previous relative path `/bucketname/key`. (`pkg/s3compat/multipart.go`, `pkg/s3compat/handler.go`)
- **`CreateBucket` sets `Location` response header** — adds `Location: /{bucketName}` on HTTP 200 success, as required by the AWS S3 specification and expected by AWS SDK v2's `CreateBucketCommand`. (`pkg/s3compat/handler.go`)
- **`max-keys` capped at 1000** — `ListObjects` V1 and V2 return `400 InvalidArgument` for `max-keys` values greater than 1000, matching AWS S3 behavior. Non-numeric or negative values also return `InvalidArgument`. (`pkg/s3compat/handler.go`)
- **SigV2 string-to-sign includes canonical AMZ headers** — `createStringToSignV2()` now collects all `x-amz-*` request headers, lowercases names, sorts them, and appends as `name:value\n` lines. When `X-Amz-Date` is present the `Date` line in the string-to-sign is set to empty (AWS SigV2 spec). Fixes SigV2 validation for any client that includes `x-amz-*` headers. (`internal/auth/manager.go`)
- **Object Lock bucket-level configuration is now mutable** — removed non-standard restrictions: changing the default retention mode (e.g., `GOVERNANCE` → `COMPLIANCE`) and decreasing the default retention period are now permitted, matching AWS S3 behavior. `ObjectLockEnabled` remains immutable once set to `true`. (`pkg/s3compat/handler.go`)
- **`DeleteObjects` response now includes delete marker fields** — when deleting from a versioning-enabled bucket without a `VersionId`, the `<Deleted>` entry now includes `<DeleteMarker>true</DeleteMarker>` and `<DeleteMarkerVersionId>...</DeleteMarkerVersionId>`. Required by Veeam and backup tools to track created delete markers for later cleanup. (`pkg/s3compat/batch.go`)
- **ETag comparison normalized in conditional header validation** — replaced inline `strings.Trim(etag, "\"")` with a `normalizeETag()` helper that strips surrounding double-quotes from both the stored ETag and the request header value before comparison. Prevents false `412 Precondition Failed` responses when the stored ETag already includes surrounding quotes. (`pkg/s3compat/handler.go`)
- **Presigned URL handling fully implemented** — removed the `PresignedOperation` stub route that returned `501 Not Implemented`. `HandlePresignedRequest` now correctly reads `vars["object"]` (was `vars["key"]`), resolves and injects the authenticated user into the request context via `GetAccessKey` → `GetUser` → `context.WithValue`, then delegates to the appropriate downstream handler (`GetObject`, `PutObject`, `DeleteObject`, `HeadObject`). V4 (`?X-Amz-Algorithm=`) and V2 (`?AWSAccessKeyId=`) presigned routes are registered before the basic object routes in the router to ensure correct precedence. (`pkg/s3compat/presigned.go`, `internal/api/handler.go`)
- **`HEAD` error responses contain no body** — `writeError()` now early-returns after setting the HTTP status code when the request method is `HEAD`, per RFC 7231 §3.3. Previously a full XML body was written, which could cause strict HTTP/1.1 clients to block waiting for a body that should not exist. (`pkg/s3compat/handler.go`)
- **`security.session_timeout` now controls JWT expiry** — `GenerateJWT` reads the setting from the database on every login instead of using a hardcoded 24 h value. Changes take effect for new logins immediately without a restart. (`internal/auth/manager.go`)
- **`security.require_2fa_admin` now enforced at login** — when set to `true`, admin and globalAdmin users that have not enabled 2FA receive `403 Forbidden` during `POST /api/v1/auth/login`. (`internal/server/console_api.go`)
- **`security.ratelimit_enabled` and `security.ratelimit_login_per_minute` hot-reload** — `CheckRateLimit` reads both settings on every login attempt. Rate limiting can be disabled at runtime, and the per-minute limit update takes effect without a restart. (`internal/auth/manager.go`, `internal/auth/rate_limiter.go`)
- **`security.ratelimit_api_per_second` now enforces per-user S3 API rate limiting** — a new `APIRateLimiter` (token-bucket, one bucket per `Authorization` key) is mounted as middleware on the S3 router **after** auth and before request processing. Returns `429 Too Many Requests` with `Retry-After: 1` on excess. (`internal/auth/api_rate_limiter.go`, `internal/server/server.go`)
- **`audit.enabled`, `audit.log_s3_operations`, and `audit.log_console_operations` now gating** — `Manager.LogEvent` checks all three settings before writing each record. S3-category events are identified by `ResourceType == "object"`; everything else is treated as a console/admin event. (`internal/audit/manager.go`)
- **`audit.retention_days` reads from database instead of YAML** — the daily retention-cleanup goroutine re-reads the setting on every pass. The YAML value (`audit.retention_days`) is kept as a fallback for the first run before the database is available. (`internal/audit/manager.go`)
- **`storage.default_bucket_versioning` applied on bucket creation** — when the request does not include a `versioning` field and the setting is `true`, versioning is automatically set to `Enabled` for the new bucket. (`internal/server/console_api.go`)
- **`storage.default_object_lock_days` used when client omits retention period** — if Object Lock is enabled in the request but neither `days` nor `years` is specified, the setting value is used. A `400` is returned only if the setting is also zero. (`internal/server/console_api.go`)
- **`metrics.enabled` controls the Prometheus `/metrics` endpoint at runtime** — the endpoint handler now checks the DB setting on every request; returning `404` when disabled so scrape targets fail instead of silently receiving stale data. The YAML `metrics.enable` still controls whether the endpoint is registered at all. (`internal/server/server.go`)
- **`metrics.collection_interval` hot-reload** — the background collection loop re-reads the setting after every tick and calls `ticker.Reset()` when the value differs from the previous interval. (`internal/metrics/manager.go`)
- **`system.max_upload_size_mb` enforced on S3 API uploads** — a new middleware wraps each `PUT` and `POST` request body with `http.MaxBytesReader` using the value from the setting (default 5 120 MB / 5 GB) and reads it on every request for hot-reload support. (`internal/server/server.go`)

### Security
- **SSRF prevention in webhook notifications** — `validateConfiguration` now applies a static check that blocks webhook URLs pointing to `localhost`, loopback hostnames, and literal private/link-local IP addresses. Additionally, `NewManager` configures the shared HTTP client with a custom `DialContext` that resolves each hostname before connecting and rejects any resolved IP in loopback (`127.0.0.0/8`, `::1`), unspecified, link-local (including `169.254.0.0/16` AWS/GCP metadata), or private (RFC-1918 / RFC-4193) ranges. HTTP redirects are also forbidden to prevent redirect-based bypass. (`internal/notifications/manager.go`)
- **SMTP header injection prevented** — `buildMessage` now strips `\r` and `\n` from `from`, `to` entries, and `subject` via a `sanitizeHeader()` helper before writing them into message headers. Without this, a caller-controlled subject of `foo\r\nBcc: attacker@example.com` could inject arbitrary headers. (`internal/email/sender.go`)
- **Timing-safe cluster token comparison** — all four handlers that validate the shared `cluster_token` (`handleValidateClusterToken`, `handleSignCSR`, `handleRegisterNode`, `handleGetClusterNodesInternal`) now compare secrets with `hmac.Equal` instead of `!=`, eliminating a timing side-channel. (`internal/server/cluster_handlers.go`)
- **Cluster token removed from URL query parameter** — `handleGetClusterNodesInternal` previously accepted the secret via `?cluster_token=…`, which caused it to appear in HTTP access logs. The token is now passed via `Authorization: Bearer <token>`. Client and tests updated accordingly. (`internal/server/cluster_handlers.go`, `internal/cluster/manager.go`, `internal/server/server_test.go`)
- **`max-uploads` and `max-parts` capped at 1000** — `ListMultipartUploads` and `ListParts` accepted arbitrarily large values (e.g. `?max-uploads=99999999`), causing the server to fetch and hold an unbounded number of records in memory before responding. Both parameters are now capped at 1000, matching the AWS S3 specification, and non-positive values return `400 InvalidArgument`. (`pkg/s3compat/multipart.go`)
- **OAuth CSRF token compared with constant-time equality** — `handleOAuthCallback` previously used `cookie.Value != csrfToken` (non-timing-safe) to validate the anti-CSRF state cookie, creating a theoretical timing oracle for brute-forcing the 16-byte token. Now uses `crypto/subtle.ConstantTimeCompare`. (`internal/server/console_idp.go`)
- **`oauth_state` cookie gains `Secure` flag when served over HTTPS** — the anti-CSRF cookie set by `startOAuthFlow` now includes `Secure: true` whenever the request arrived over TLS directly (`r.TLS != nil`) or via a trusted reverse proxy (`X-Forwarded-Proto: https`), preventing transmission over plaintext HTTP. New helper `isHTTPS(r)` added. (`internal/server/console_idp.go`)
- **Last-admin self-lock protection** — `DELETE /api/v1/users/{id}` and `PUT /api/v1/users/{id}` (role demotion) now refuse to leave the system with zero global admins. Before deleting or demoting a global-admin account, both handlers call `countGlobalAdmins()` and return `409 Conflict` with message *"Cannot delete/remove the last global admin. Assign another admin first."* when there is only one. Tenant-scoped admins and regular users are unaffected. Covered by `TestLastAdminGuard` (4 sub-tests: block delete, block demotion, allow delete when 2nd admin exists, allow demotion when 2nd admin exists). (`internal/server/console_api.go`, `internal/server/console_idp.go`, `internal/server/server_test.go`)
- **Auth bypass via suffix matching in public paths** — the middleware that whitelists un-authenticated routes used `strings.HasSuffix(urlPath, pub)` against the full path. Any registered route whose path *ended* with a public suffix (e.g. `GET /api/v1/cluster/nodes/{nodeId}/health`) would bypass JWT validation. Fixed by extracting the path segment relative to `/api/v1` and matching with exact equality for plain paths and `strings.HasPrefix` for prefix patterns (OAuth routes). (`internal/server/console_api.go`)
- **X-Forwarded-For IP spoofing for rate-limit bypass** — `getClientIP()` unconditionally trusted the `X-Forwarded-For` and `X-Real-IP` headers, allowing any external client to forge an arbitrary source IP and bypass the per-IP login rate limiter (5 req/min). Implemented trusted-proxy validation: headers are now honoured **only** when the direct `RemoteAddr` peer is within a private/loopback network (`127.0.0.0/8`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `::1/128`, `fc00::/7`) or in the `trusted_proxies` list from `config.yaml`. Direct connections from public IPs have their forwarded-for headers silently ignored. The `trusted_proxies` key already existed in `config.example.yaml` and `config.go`; this change wires it to the actual IP-resolution logic. All 11 `getClientIP` call sites in `console_api.go` and `console_idp.go` updated, and `TestGetClientIP` extended with untrusted-peer and explicit-CIDR scenarios. (`internal/server/console_api.go`, `internal/server/console_idp.go`, `internal/server/server_test.go`)
- **Maintenance mode bypass via `strings.Contains`** — the maintenance mode middleware checked exempt paths using `strings.Contains(r.URL.Path, prefix)`, meaning a bucket named `"health"` or `"settings"` would bypass write-blocking because its object paths contain those substrings. Replaced with the same relPath extraction approach as the auth bypass fix: paths are matched as `HasPrefix` on the segment relative to `/api/v1`, and `/api/internal/` is matched against the full path. (`internal/server/console_api.go`)
- **Short-lived access tokens + sliding-window refresh tokens** — login responses previously issued a single long-lived JWT (default 24h). Any stolen token was valid until expiry with no revocation mechanism. Replaced with a two-token model: a short-lived **access token** (`security.access_token_lifetime`, default 900 s / 15 min) used for all API calls, and a longer-lived **refresh token** (`security.session_timeout`, default 86400 s) used only at `POST /auth/refresh`. Each refresh call issues a brand-new pair (sliding window), so the session stays alive while the user is active and closes automatically when idle for more than `session_timeout` seconds. To enforce a strict 15-min inactivity timeout, set both `access_token_lifetime = 900` and `session_timeout = 900` in the admin settings panel. The `token` field in login responses is preserved for backward compatibility alongside the new `access_token` / `refresh_token` / `expires_in` fields. All three token-issuance points updated: password login, 2FA verification, and OAuth callback. (`internal/auth/manager.go`, `internal/auth/types.go`, `internal/settings/manager.go`, `internal/server/console_api.go`, `internal/server/console_idp.go`)
- **Privilege escalation in user creation** — any authenticated user (regardless of role) could call `POST /api/v1/users` and create a new account with `"roles": ["admin"]`. A missing admin guard allowed the tenant-isolation checks to become the only barrier, which only blocked lateral tenant access, not vertical privilege escalation. Fixed by rejecting non-admin callers with `403 Forbidden` before any further validation. (`internal/server/console_api.go`)
- **JWT implementation replaced with `golang-jwt/jwt/v5`** — the custom HMAC-HS256 token implementation (`createBasicToken`/`parseBasicToken`) carrying an explicit "MVP, not production" comment has been replaced with the widely-audited [`golang-jwt/jwt`](https://github.com/golang-jwt/jwt) library. Token format is now standard RFC 7519 JWT; `JWTClaims` embeds `jwt.RegisteredClaims` and expiry/signature validation is fully delegated to the library. Existing sessions are invalidated after upgrade (one-time, users must log in again). (`internal/auth/manager.go`, `internal/auth/types.go`)
- **Minimum password length raised from 6 to 8 characters, and password policy settings now enforced** — `security.password_min_length`, `security.password_require_uppercase`, `security.password_require_numbers`, and `security.password_require_special` were defined in the settings manager and shown in the UI but never read during validation; all checks were hardcoded to `≥ 6`. Added `validatePasswordPolicy()` helper that reads the four settings at call time and applies them. Changes to these settings in the admin panel now take effect immediately without a restart. Applies to both password changes (`PUT /api/v1/users/{user}/password`) and new local user creation (`POST /api/v1/users`). (`internal/server/console_api.go`)
- **13 settings visible in the UI had no backend effect** — a full audit of all settings keys found that `security.session_timeout`, `security.require_2fa_admin`, `security.ratelimit_enabled`, `security.ratelimit_api_per_second`, `audit.enabled`, `audit.retention_days`, `audit.log_s3_operations`, `audit.log_console_operations`, `storage.default_bucket_versioning`, `storage.default_object_lock_days`, `metrics.enabled`, `metrics.collection_interval`, and `system.max_upload_size_mb` were stored in SQLite and shown in the admin panel but never consulted at runtime. All 13 are now wired to real behavior (see `### Changed` below). (`internal/auth/manager.go`, `internal/audit/manager.go`, `internal/metrics/manager.go`, `internal/server/console_api.go`, `internal/server/server.go`)

- **Presigned URL signature comparison made timing-safe** — `ValidatePresignedURL` previously compared the provided `X-Amz-Signature` against the expected HMAC-SHA256 hex using `strings.ToLower(provided) != expected` (a timing oracle). Replaced with `hmac.Equal([]byte(...), []byte(...))` for constant-time comparison, preventing HMAC forgery via timing side-channel. (`internal/presigned/validator.go`)
- **Share `secret_key` encrypted at rest** — the `shares` SQLite table stored the S3 `secret_key` used for per-object shared URLs in plaintext. An attacker with read access to the database file could harvest all active share credentials and make arbitrary S3 requests on behalf of any user. Share `secret_key` values are now encrypted with AES-256-GCM (same `enc1:<base64>` scheme as replication credentials) when written and decrypted transparently when read. The encryption key is derived from `auth.secret_key` (or `auth.jwt_secret` as fallback). Legacy plaintext values are returned as-is until the share is next updated (zero-migration upgrade path). New `encryptShareCredential`/`decryptShareCredential` functions scoped to the share subsystem to avoid key reuse with the replication key derivation. (`internal/share/credentials.go` [new], `internal/share/sqlite.go`, `internal/share/manager.go`, `internal/server/server.go`)
- **S3 SigV4 and SigV2 signature verification made timing-safe** — `verifyS3SignatureV4` and `verifyS3SignatureV2` in the auth manager both compared calculated vs. provided HMAC signatures using `==` on plain strings, leaking timing information that could be exploited to forge S3 signatures. Both now use `hmac.Equal([]byte(calculated), []byte(provided))`. (`internal/auth/manager.go`)
- **Received and calculated signatures removed from INFO logs** — `verifyS3SignatureV4` logged both the received signature and the calculated signature at `INFO` level (`"received_signature"`, `"calculated_signature"`), filling access logs with HMAC material that could aid offline forgery. The log entry is now at `Debug` level and only retains non-sensitive fields (`access_key`, `signed_headers`, `date`, `region`, `service`). (`internal/auth/manager.go`)
- **CORS wildcard `*` no longer allows credentialed cross-origin requests** — `CORSWithConfig` emitted `Access-Control-Allow-Credentials: true` unconditionally when `AllowCredentials` was `true` in the config, including when the allowed origins list contained `*`. An operator setting `MAXIOFS_ALLOWED_ORIGINS=*` would cause the middleware to reflect any request origin and attach `Allow-Credentials: true`, enabling any web page to make authenticated cross-origin requests to the console API with the user's cookies/tokens. `Allow-Credentials: true` is now suppressed whenever the config contains a wildcard origin. (`internal/middleware/cors.go`)
- **SSRF prevention in log HTTP forwarder** — `NewHTTPOutput` used a plain `http.Client` with no dial restrictions, allowing an admin to configure a log target pointing at internal metadata endpoints, cluster nodes, or RFC-1918 services. The client now uses a custom `DialContext` that resolves hostnames before connecting and rejects any resolved IP in loopback, private (RFC-1918/RFC-4193), link-local, unspecified, and AWS/GCP metadata (`169.254.0.0/16`) ranges. HTTP redirects are also forbidden to prevent redirect-based bypass. `validateLogURL` now also rejects any URL with a non-`http/https` scheme (e.g. `file://`). (`internal/logging/http.go`, `internal/logging/manager.go`)
- **`Content-Disposition` header injection via object key** — the download handler set `Content-Disposition: attachment; filename="<objectKey>"` using `filepath.Base(objectKey)` without any sanitization. An object key containing `\r\n` characters could inject arbitrary HTTP response headers. A new `sanitizeFilename()` helper strips `\r`, `\n`, `"`, and `\` from the filename before embedding it in the header. (`internal/server/console_api.go`)
- **Object streaming encryption replaced AES-CTR with AES-256-GCM** — `EncryptStream` in `pkg/encryption` (struct `aesGCMEncryptor`) was named for GCM but used AES-CTR, which provides no authentication or integrity protection. A malicious actor with write access to the storage backend could silently modify encrypted object ciphertext; decryption would succeed and return corrupted plaintext with no error. Replaced with a chunked AES-256-GCM scheme: the stream is split into 64 KB blocks, each independently sealed with GCM and a per-chunk nonce (baseNonce XOR chunkIndex). On-disk format is now `[12-byte base nonce][4-byte len + ciphertext+tag] × N`. Legacy AES-CTR objects (algorithm tag `"AES-256-CTR"`) are transparently decrypted via a backward-compat path; new objects are marked `"AES-256-GCM-STREAM"`. (`pkg/encryption/encryption.go`, `internal/object/manager.go`)
- **Path traversal defense-in-depth in filesystem storage backend** — `validatePath` checked for `..` and leading `/` but did not check for leading `\` (Windows volume-relative absolute paths) and performed no canonicalization after `filepath.Join`. On a Windows deployment, a key starting with `\` (e.g., `\Windows\System32\file`) would bypass the prefix check and `filepath.Join(root, "\\Windows\\...")` would resolve to an absolute OS path outside the data directory. Additionally, the `List()` method never called `validatePath` at all, so a crafted prefix containing `..` would cause `filepath.Walk` to traverse directories outside the storage root and return their contents. Fixed: (1) leading `\` now rejected in `validatePath`; (2) post-join canonical path check added (`filepath.Clean(fullPath)` must begin with `filepath.Clean(rootPath) + separator`); (3) `List()` now calls `validatePath` for non-empty prefixes. (`internal/storage/filesystem.go`)

### Fixed
- **`x-amz-acl` silently discarded on `PutObject`** — the `x-amz-acl` header was accepted but never applied; `SetObjectACL` was never called after the object was stored. Fixed by calling the new `applyObjectCannedACLHeader` helper after a successful `PutObject`. (`pkg/s3compat/handler.go`)
- **`x-amz-acl` silently discarded on `CompleteMultipartUpload`** — same bug for multipart uploads. The `x-amz-acl` header passed to `CreateMultipartUpload` is now read from the stored upload metadata before the goroutine launches (it is deleted during completion), and applied after the goroutine succeeds. (`pkg/s3compat/multipart.go`)
- **`x-amz-acl` silently discarded on `CopyObject`** — `aws s3api copy-object --acl public-read` had no effect. Fixed with the same pattern as `PutObject`. (`pkg/s3compat/object_ops.go`)
- **`?versionId=xxx` in `x-amz-copy-source` not parsed** — when copying a specific version (`x-amz-copy-source: bucket/key?versionId=ABC123`), the `?versionId=ABC123` suffix was included literally in the source key, causing a `NoSuchKey` error. It is now stripped and passed as a positional version ID to `GetObject`. (`pkg/s3compat/object_ops.go`)
- **Hardcoded owner `"maxiofs"` in `PutBucketACL` and `PutObjectACL`** — canned ACL paths used literal `"maxiofs"` / `"MaxIOFS"` as the owner instead of the authenticated user. Fixed to resolve the real user from `auth.GetUserFromContext`. (`pkg/s3compat/bucket_ops.go`, `pkg/s3compat/object_ops.go`)
- **Missing bucket sub-resource routes** — `?notification`, `?website`, `?accelerate`, `?requestPayment`, `?encryption`, `?replication`, and `?logging` routes were not registered; requests fell through to `ListObjects`, returning a confusing 200 XML body. Added 17 new handler methods returning AWS-spec-compliant responses (`NotificationConfiguration`, `AccelerateConfiguration`, `RequestPaymentConfiguration`, etc.). (`pkg/s3compat/bucket_ops.go`, `internal/api/handler.go`)
- **`PutBucketLifecycle` silently ignores `<Filter><Prefix>`** — modern clients (aws-cli v2, SDKv2, Terraform AWS provider) send the prefix inside `<Filter><Prefix>…</Prefix></Filter>` rather than the legacy top-level `<Prefix>`. The handler only read the old-style element, silently losing the prefix. Now prefers `rule.Filter.Prefix` and falls back to `rule.Prefix` for backward compatibility. (`pkg/s3compat/bucket_ops.go`)
- **`ListMultipartUploads` reports spurious `IsTruncated=true`** — the condition `len(uploads) > len(filteredUploads)` was true whenever any upload was skipped by marker filtering, even if `maxUploads` was never reached. `IsTruncated` is now set only when the loop actually hits the `maxUploads` limit. (`pkg/s3compat/multipart.go`)

### Tests
- `TestS3ListObjectsV2` (11 sub-tests) — pagination, `ContinuationToken`, `StartAfter`, `KeyCount`, `fetch-owner=false`
- `TestS3HeadErrorNoBody` (4 sub-tests) — verified no body on HEAD 404/403 errors
- `TestCompleteMultipartUploadLocation` (3 sub-tests) — absolute URL in `<Location>` for path-style and virtual-hosted-style
- `TestWriteXMLResponseDeclaration` (4 sub-tests) — XML declaration present in all major XML responses
- `TestS3CreateBucketLocationHeader` (3 sub-tests) — `Location: /bucketname` header on 200 response
- `TestS3CopyObjectMetadataDirective` (3 sub-tests) + `TestS3CopyObjectConditionals` (8 sub-tests)
- `TestS3ConditionalDateHeaders` (9 sub-tests) — `If-Modified-Since`/`If-Unmodified-Since` for GetObject and HeadObject
- `TestS3ListObjectsMaxKeys` (8 sub-tests) — `max-keys` > 1000 → `InvalidArgument`
- `TestObjectLockModeAndPeriodMutability` (6 sub-tests) — mode switch and period decrease accepted
- `TestS3CreateBucketObjectLockEnabled` (4 sub-tests) — `x-amz-bucket-object-lock-enabled` persisted on bucket creation
- `TestS3DeleteObjectsDeleteMarkers` (3 sub-tests) — versioned and non-versioned batch deletes
- `TestS3EncodingTypeURL` (7 sub-tests) — keys with `&`, `<`, `>`, spaces, non-ASCII
- `TestS3ListObjectsV2Namespace` — `<ListBucketResult xmlns="...">` rendered correctly
- `TestS3CreateBucketConflict` (2 sub-tests) — `BucketAlreadyOwnedByYou` vs `BucketAlreadyExists`
- `TestCreateStringToSignV2CanonicalAmzHeaders` (4 sub-tests) — `x-amz-*` headers in SigV2 string-to-sign
- `TestS3StorageClass` + `TestS3MultipartStorageClass` — storage class stored and returned in listings
- `TestPresignedGetRoutesFallsThrough` — V4 and V2 presigned routes matched before basic handlers
- `TestNormalizeETag` (5 cases) + `TestS3ETagConditionalHeaders` (5 sub-tests) — ETag quote normalization
- `TestWriteS3ErrorIncludesAmzHeaders` (3 sub-tests: Unauthorized, Forbidden, Internal) — `X-Amz-Request-Id` and `X-Amz-Id-2` present on auth errors

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
