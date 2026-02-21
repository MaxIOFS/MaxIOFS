# Changelog

All notable changes to MaxIOFS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.9.1-beta] - 2026-02-19

### Added
- **External syslog targets** — multiple external logging targets (syslog and HTTP) stored in SQLite with full CRUD API. Replaces the single-target legacy `logging.syslog_*` / `logging.http_*` settings with an N-target system supporting independent configuration per target.
  - New `logging_targets` table (migration 11, v0.9.2) with indexes on type and enabled status
  - `TargetStore` CRUD in `internal/logging/store.go` with validation, unique name constraint, and automatic migration of legacy settings
  - 7 new console API endpoints: `GET/POST /logs/targets`, `GET/PUT/DELETE /logs/targets/{id}`, `POST /logs/targets/{id}/test`, `POST /logs/targets/test` (test without saving)
  - Frontend `LoggingTargets` component integrated in Settings → Logging with create/edit modal, test connection, delete confirmation, and TLS indicator
- **Syslog TLS and RFC 5424** — `SyslogOutput` rewritten with TCP+TLS support (mTLS, custom CA, skip-verify) and RFC 5424 structured data format alongside RFC 3164
- **Lock-free log dispatch** — `DispatchHook` uses `atomic.Pointer` for the outputs snapshot, making `Fire()` completely lock-free and eliminating a deadlock where `Reconfigure()` (write lock) triggered logrus hooks that needed a read lock

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
- BadgerDB for metrics historical storage
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

### Current Status: BETA (v0.9.1-beta)

**Completed Core Features:**
- ✅ All S3 core operations validated with AWS CLI (100% compatible)
- ✅ Multi-node cluster support with real object replication
- ✅ Tombstone-based cluster deletion sync (all 6 entity types)
- ✅ LDAP/AD and OAuth2/OIDC identity provider system with SSO
- ✅ Object Filters & Advanced Search
- ✅ Production monitoring (Prometheus, Grafana, performance metrics)
- ✅ Server-side encryption (AES-256-CTR)
- ✅ Bucket policy enforcement (AWS S3-compatible evaluation engine)
- ✅ Presigned URL signature validation (V4 and V2)
- ✅ Audit logging and compliance features
- ✅ Two-Factor Authentication (2FA)

**Current Metrics:**
- Backend Test Coverage: ~75% (at practical ceiling)
- Frontend Test Coverage: 100%
- Performance: P95 <10ms (Linux production)

**Path to v1.0.0 Stable:**
See [TODO.md](TODO.md) for detailed roadmap and requirements.

---

## Version History

### Completed Features (v0.1.0 - v0.9.0-beta)

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
- ✅ Compression support (gzip) - pkg/compression with streaming support
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
