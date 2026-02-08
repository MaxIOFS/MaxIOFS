# Changelog

All notable changes to MaxIOFS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

### Current Status: BETA (v0.8.0-beta)

**Completed Core Features:**
- ✅ All S3 core operations validated with AWS CLI (100% compatible)
- ✅ Multi-node cluster support with real object replication
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

### Completed Features (v0.1.0 - v0.8.0-beta)

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
