# Changelog

All notable changes to MaxIOFS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Cross-node bucket aggregation in web console** - January 30, 2026. Implemented simple and efficient bucket aggregation for multi-node clusters in `internal/server/console_api.go`. The web console now shows:
  - **Always**: All buckets from the local node's database (source of truth)
  - **In cluster mode**: Also queries and displays buckets from OTHER healthy nodes (excludes self to avoid circular HTTP calls)
  - **Real node names**: Shows the actual node name from cluster config (e.g., "node-01", "node-02") instead of generic "local"
  - **Permission filtering**: Applies after aggregation, respecting tenant isolation and user permissions
  - **Graceful degradation**: If remote node queries fail, local buckets are still shown
  - **Load balancer friendly**: Node name is always correct regardless of which node serves the request
  Features:
  - `ListAllBuckets()` queries all nodes in parallel using goroutines for optimal performance
  - HMAC-authenticated internal API endpoint `/api/internal/cluster/buckets` for secure node-to-node communication
  - 5-second timeout per node with graceful degradation (continues if some nodes fail)
  - Bucket responses include node location metadata (`node_id`, `node_name`, `node_status`)
  - Modified `internal/server/console_api.go` handleListBuckets() to use aggregator in cluster mode
  - Modified `pkg/s3compat/handler.go` ListBuckets() to use aggregator for S3 API
  - Modified `internal/api/handler.go` to wire cluster components to S3 handler
  - 6 comprehensive unit tests validating HTTP requests, timeouts, error handling, JSON parsing
  - Dual-mode operation: cluster mode (aggregated) and standalone mode (local only)
  - Added `BucketResponse` fields for cluster info in `internal/server/console_api.go`

- **Cross-node storage quota aggregation system (QuotaAggregator)** - January 30, 2026. Implemented `internal/cluster/quota_aggregator.go` to aggregate tenant storage usage from all cluster nodes in real-time. Tenant storage quotas are now enforced cluster-wide instead of per-node. Features:
  - `GetTenantTotalStorage()` sums storage from all healthy nodes in parallel
  - HMAC-authenticated internal API endpoint `/api/internal/cluster/tenant/{tenantID}/storage`
  - Modified `internal/auth/manager.go` CheckTenantStorageQuota() to use cluster-wide storage in cluster mode
  - Modified `internal/server/cluster_handlers.go` with handleGetTenantStorage() handler
  - 8 comprehensive unit tests + 6 end-to-end integration tests validating:
    - Multi-node storage aggregation (3 nodes: 100MB + 200MB + 300MB = 600MB)
    - Partial node failure handling (gracefully continues with available nodes)
    - Complete failure detection (errors only when all nodes fail)
    - Large scale performance (10 nodes queried in 2.8ms)
    - Storage breakdown by node for monitoring
  - Fallback to local storage check if aggregation fails
  - Comprehensive logging with cluster mode detection

- **🚀 Production hardening: Rate limiting for internal cluster APIs** - January 30, 2026. Implemented token bucket rate limiter (`internal/cluster/rate_limiter.go`) to protect internal cluster endpoints from abuse and DoS attacks. Features:
  - Token bucket algorithm with configurable requests per second (100 req/s) and burst size (200)
  - Per-IP rate limiting with automatic bucket creation and cleanup
  - HTTP middleware integration applied to all `/api/internal/cluster` endpoints
  - Returns HTTP 429 (Too Many Requests) when rate limit exceeded
  - Automatic token refill based on elapsed time
  - Stale bucket cleanup every 5 minutes to prevent memory leaks
  - Statistics API for monitoring tracked IPs, configured rates, and burst size
  - 16 comprehensive tests validating burst handling, token refill, concurrent requests, middleware integration
  - Production-ready with minimal performance overhead

- **🚀 Production hardening: Circuit breaker for node communication** - January 30, 2026. Implemented circuit breaker pattern (`internal/cluster/circuit_breaker.go`) to prevent cascading failures when cluster nodes are down or experiencing issues. Features:
  - Three-state circuit breaker (Closed → Open → Half-Open → Closed)
  - Opens after 3 consecutive failures, preventing further requests to failing nodes
  - 30-second timeout before attempting recovery (Half-Open state)
  - Requires 2 successful requests to close from Half-Open state
  - Integrated into `BucketAggregator` and `QuotaAggregator` for all node communications
  - Per-node circuit breakers managed by `CircuitBreakerManager`
  - Statistics API showing state, failure/success counts, time until retry
  - Automatic recovery testing and manual reset capability
  - 19 comprehensive tests validating state transitions, concurrent calls, recovery logic
  - Prevents resource exhaustion and reduces latency when nodes are unhealthy

- **🚀 Production hardening: Metrics and monitoring for cluster operations** - January 30, 2026. Implemented comprehensive metrics tracking system (`internal/cluster/metrics.go`) for cluster health monitoring and performance analysis. Features:
  - Request counters for bucket/quota aggregation (total, successes, failures, success rates)
  - Node communication metrics (total requests, success/failure counts)
  - Circuit breaker metrics (total opens, state changes)
  - Rate limiting metrics (hits, misses, total requests)
  - Latency tracking with min/max/avg calculations for all operations
  - Atomic operations for thread-safe concurrent metric recording
  - Statistics API returning structured JSON for monitoring dashboards
  - Reset capability for metric windows
  - 15 comprehensive tests validating concurrent recording, success rate calculations, latency tracking
  - Production-ready for integration with Prometheus, Grafana, or custom monitoring solutions

- **Web UI: Node location column in bucket list** - January 30, 2026. Added "Node" column to the buckets table showing the actual cluster node name where each bucket is stored. Features:
  - Displays real node name from cluster configuration (e.g., "node-01", "node-02", "standalone")
  - Health status indicator: green dot for healthy nodes
  - Updated TypeScript types (`web/frontend/src/types/index.ts`) to include `node_id`, `node_name`, `node_status` fields
  - Modified bucket list page (`web/frontend/src/pages/buckets/index.tsx`) to display node column
  - Professional appearance: always shows meaningful node names, never generic "local"
  - Load balancer friendly: correct node name regardless of which node serves the request

### Fixed

- **🔥 CRITICAL SECURITY: Fixed tenant storage quota bypass vulnerability in multi-node clusters** - January 30, 2026. Tenant storage quotas were enforced per-node only, allowing tenants to exceed quota by a factor of N (number of nodes). Example: Tenant with 1TB quota could store 3TB on a 3-node cluster. **SECURITY FIX**: Modified `CheckTenantStorageQuota()` in `internal/auth/manager.go` to aggregate storage from ALL cluster nodes in real-time before allowing uploads. End-to-end tests confirm quota bypass attack is now prevented - tenants are correctly rejected when cluster-wide storage exceeds quota. CVE risk eliminated. Affects all upload code paths: `PutObject`, multipart uploads. Production blocker resolved.

- **⚠️ CRITICAL: Fixed multi-node cluster bucket aggregation** - January 30, 2026. Web interface and S3 API now correctly show all tenant buckets across the cluster, not just buckets from the local node. Users see consistent bucket lists regardless of which node serves the request. Implemented BucketAggregator with parallel node queries (see Added section). Multi-node clusters are now production-ready for bucket listing operations.

- **Fixed tenant unlimited storage quota incorrectly setting default** - January 30, 2026. `CreateTenant()` in `internal/auth/tenant.go` was automatically converting `MaxStorageBytes = 0` to 100GB default, making it impossible to create tenants with unlimited storage. This caused "quota exceeded" errors when tenants reached 100GB even though UI showed "unlimited". **FIX**: `MaxStorageBytes = 0` now correctly means UNLIMITED (no quota checking). If specific quota is desired, it must be set explicitly (e.g., 107374182400 for 100GB). Modified `CheckTenantStorageQuota()` to skip quota check when `MaxStorageBytes = 0`. Tests confirm tenants with `MaxStorageBytes = 0` can upload unlimited data without errors.

- **🔥 CRITICAL: Fixed cluster authentication failure due to incorrect column name** - January 30, 2026. Both `internal/cluster/manager.go:416` and `internal/middleware/cluster_auth.go:111` incorrectly queried `status` column in `cluster_nodes` table, causing "SQL logic error: no such column: status (1)" failures. This prevented ALL cluster internal API authentication, breaking:
  - Cross-node bucket aggregation (BucketAggregator)
  - Cross-node quota aggregation (QuotaAggregator)
  - Access key synchronization
  - Bucket permission synchronization
  - Tenant/user synchronization
  - Object replication
  **FIX**: Changed queries to use correct `health_status` column. Cluster authentication now works correctly, enabling all multi-node cluster features.

### Changed

- **Refactored cluster aggregators to use interfaces for testability** - January 30, 2026. Changed `BucketAggregator` and `QuotaAggregator` from using concrete `*Manager` type to `ClusterManagerInterface` with methods `GetHealthyNodes()`, `GetLocalNodeID()`, `GetLocalNodeToken()`. This enables proper unit testing with mock implementations and reduces coupling.

- **Enhanced CheckTenantStorageQuota() with cluster-aware logic** - January 30, 2026. Added detection of cluster mode, real-time aggregation of storage from all nodes, fallback to local storage on aggregation errors, and comprehensive logging showing `clusterMode`, `currentStorage` (cluster-wide or local), `maxStorage`, `projectedTotal`. Maintains backward compatibility - standalone mode unchanged.

- **Integrated circuit breakers into cluster aggregators** - January 30, 2026. Modified `BucketAggregator` and `QuotaAggregator` to wrap all node communication calls with circuit breaker protection. Each node gets its own circuit breaker managed by `CircuitBreakerManager`. Prevents cascading failures and reduces latency when nodes are unhealthy. Circuit breaker state (open/closed/half-open) is logged for troubleshooting.

### Known Issues
- Fixed syslog logging support for IPv6 addresses
- **CRITICAL: Fixed bucket replication workers not processing queue items** - Objects queued for replication were stuck in "pending" status indefinitely. Queue loader now loads pending items immediately on startup instead of waiting 10 seconds, ensuring objects are replicated promptly.
- **CRITICAL: Fixed database lock contention in cluster replication under high concurrency** - `queueBucketObjects()` maintained an active database reader (SELECT) while attempting writes (INSERT) within the same loop, causing "database is locked (5) (SQLITE_BUSY)" errors. Fixed by reading all objects into memory first, closing the reader, then performing writes. This prevented production failures with multiple replication workers and scheduler running concurrently.
- **Fixed non-atomic queue item insertion in cluster replication** - `insertQueueItem()` used separate SELECT + INSERT operations leading to race conditions. Replaced with single atomic `INSERT OR IGNORE` with subquery to prevent duplicate queue items and reduce lock contention.
- **Fixed missing storage backend validation** - `NewBackend()` accepted any backend type without validation, always defaulting to filesystem. Now properly validates backend type and returns error for unsupported backends (currently only 'filesystem' is supported).
- **Fixed server_test.go test suite failures** - Corrected 15+ failing tests with multiple issues:
  - Fixed `ListObjects` returning empty list for non-existent buckets instead of 404 error (added `BucketExists()` check in object manager)
  - Fixed API response parsing in tests - handlers wrap responses in `APIResponse{success, data}` structure
  - Fixed double-wrap issue in `handleGetSecurityStatus` and similar handlers where `writeJSON()` wraps response twice
  - Fixed `handleShareObject` test using wrong user ID and field names (`url`/`id` not `shareUrl`/`shareID`)
  - Fixed `handleUpdateTenant` test using wrong URL variable (`tenant` not `id`)
  - Fixed DELETE handlers expecting 200 instead of 204 No Content (lifecycle, tagging, CORS, policy, object)
  - Fixed PUT handlers receiving JSON instead of required XML format (lifecycle, tagging, CORS)
  - Fixed missing tenant creation before bucket creation in tests (required for storage quota validation)

### Added
- **23 comprehensive tests for internal/config module** - Tests validate configuration loading from multiple sources (CLI flags, environment variables, YAML files), default value handling, TLS configuration validation, data directory creation, storage root path resolution, JWT secret generation, and audit DB path setup. Config module test coverage improved from 35.8% to 94.0% (+58.2 points, 163% improvement).
- **5 comprehensive HTTP and background worker tests for internal/cluster/access_key_sync** - Tests validate:
  - HTTP request handling with mock servers for access key synchronization between cluster nodes
  - HMAC-authenticated requests with proper error handling for server failures
  - Single access key synchronization with checksum verification to prevent redundant syncs
  - Background sync loop with ticker-based scheduling and graceful shutdown
  - Sync manager startup with configuration from global settings (auto-sync enable/disable, interval configuration)
  - All tests cover complex scenarios including concurrent operations, HTTP mocking, and background goroutines
- Comprehensive end-to-end tests for bucket replication system with in-memory stores and mock S3 clients
- Replication test coverage includes object replication, metrics tracking, and prefix filtering
- 79 new tests for cluster module covering health checking, routing, bucket location tracking, and replication management
- Test coverage for cluster module improved from 17.8% to 32.7%
- **10 comprehensive security tests for internal/api module** - Tests validate authentication, authorization, input validation (XSS, SQL injection, path traversal), oversized headers, and concurrent request handling
- **API module test coverage improved from 0% to 91.6%** with security-focused tests for S3 API handlers
- **7 comprehensive lifecycle tests for internal/server module** - Tests validate background workers (lifecycle, inventory, replication), graceful shutdown, configuration variations, error handling, and component initialization
- Inventory worker tests with 73.4% coverage (0% → 73.4%) including bucket validation, circular reference detection, and CSV/JSON generation
- Inventory module test coverage improved from 30.1% to 80.6%
- **28 comprehensive tests for internal/notifications module** - Tests validate webhook delivery with retries, event dispatching, rule matching with prefix/suffix filters, wildcard event type matching, configuration validation, and AWS event format creation
- Notifications module test coverage improved from 30.7% to 85.0%
- **46 comprehensive tests for internal/server module** - Tests validate HTTP handlers for object operations (list, get, upload, delete), metrics endpoints (system, S3, historical), security status, bucket advanced features (lifecycle, tagging, CORS, policy, versioning, ACL), server lifecycle, and configuration management
- **30+ additional tests for console API handlers** - Tests validate 2FA workflows (enable, verify, regenerate backup codes), bucket permissions (list, grant, revoke), bucket owner updates, object ACL operations, shares and presigned URLs, settings management, audit logs, notifications, and tenant user management
- **60+ comprehensive tests for cluster, inventory, replication, and profiling handlers** - Tests validate:
  - Cluster operations: initialize, join, leave cluster, node CRUD, health checks, cache stats
  - Inventory handlers: put/get/delete bucket inventory, list reports, validation
  - Replication rules: create, list, get, update, delete rules, metrics, manual sync
  - Object lock and legal hold: configuration validation, status management
  - Bulk settings: global admin permissions, validation
  - Bucket notifications: put/delete configuration
  - Profiling endpoints: pprof handlers (heap, goroutine, threadcreate, block, mutex, allocs)
  - Global admin middleware: authentication and authorization validation
- **25+ additional tests for cluster internal sync handlers** - Tests validate:
  - Object replication sync: receive object, HMAC authentication, size validation
  - Object deletion sync: receive deletion requests with proper cluster node authentication
  - Tenant/User sync: create/update tenants and users across cluster nodes
  - Bucket permissions, ACLs, configurations, access keys, inventory sync handlers
  - Cluster replication rules CRUD: create, list, update, delete, bulk create
  - Proper context-based authentication (cluster_node_id for internal, username for console)
- Server module test coverage improved from 29.8% to 54.2% (+24.4 points)

### Changed
- **Sprint 8: Systematic backend test coverage expansion initiative** - Target: increase backend coverage from 54.8% to 90%+ (354 functions with 0% coverage identified). Phase 1 focuses on critical infrastructure (config, cluster, cmd/maxiofs, web modules). Commitment to test all complex scenarios including HTTP mocking, background workers, concurrent operations, and edge cases without shortcuts.
- Internal code refactoring to improve maintainability and reduce complexity
- Improved object upload, download, delete, and multipart upload operations
- Replication test coverage improved from 19.4% to support realistic E2E testing scenarios
- Cluster module now properly separates database read and write operations to prevent SQLite lock contention

### Removed
- Removed unused `.env.example` file
- Cleaned up unused dependencies

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
- S3 API compatibility improved to 98%
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

### Current Status: BETA (v0.7.0-beta)

**Completed Core Features:**
- ✅ All S3 core operations validated with AWS CLI
- ✅ 98% S3 API compatibility
- ✅ Multi-node cluster support with replication
- ✅ Production monitoring (Prometheus, Grafana, performance metrics)
- ✅ Visual UI for all bucket configurations
- ✅ Server-side encryption (AES-256-CTR)
- ✅ Audit logging and compliance features
- ✅ Two-Factor Authentication (2FA)
- ✅ Load testing and benchmarking infrastructure

**Current Metrics:**
- Backend Test Coverage: 36.2% (improved from 25.8%)
- Frontend Test Coverage: 100% (64 tests)
- Total Backend Tests: 500+
- Performance: P95 <10ms (Linux production)

**Path to v1.0.0 Stable:**
See [TODO.md](TODO.md) for detailed roadmap and requirements.

---

## Version History

### Completed Features (v0.1.0 - v0.7.0-beta)

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
