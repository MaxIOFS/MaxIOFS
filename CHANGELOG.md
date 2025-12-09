# Changelog

All notable changes to MaxIOFS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added - Cluster Bucket Replication System (Phase 3.3)
- **Complete HA Replication Between MaxIOFS Nodes** - Production-ready cluster replication system
  - **HMAC Authentication**: Inter-node authentication using HMAC-SHA256 signatures with `node_token`
  - **No Credentials Required**: Nodes authenticate with cluster tokens, not S3 access keys
  - **Automatic Tenant Synchronization**: Continuous tenant sync between all cluster nodes every 30 seconds
  - **Encryption Handling**: Automatic decrypt-on-source, re-encrypt-on-destination (transparent to user)
  - **Configurable Sync Intervals**: Minimum 10 seconds for real-time HA, up to hours/days for backups
  - **Self-Replication Prevention**: Nodes cannot replicate to themselves (validation in frontend + backend)
  - **Bulk Node-to-Node Replication**: Configure all buckets at once between cluster nodes
  - **Database Schema**: 5 new tables (`cluster_bucket_replication`, `cluster_replication_queue`, etc.)
  - **Backend Components**:
    - `internal/cluster/replication_schema.go` - Database schema (5 tables)
    - `internal/cluster/replication_manager.go` - Core replication manager
    - `internal/cluster/replication_worker.go` - Background worker processes
    - `internal/cluster/tenant_sync.go` - Automatic tenant synchronization
    - `internal/middleware/cluster_auth.go` - HMAC authentication middleware
    - `internal/server/cluster_replication_handlers.go` - Console API (CRUD operations)
    - `internal/server/cluster_tenant_handlers.go` - Tenant sync API
    - `internal/server/cluster_object_handlers.go` - Internal object replication API
  - **Frontend Components**:
    - `web/frontend/src/pages/cluster/BucketReplication.tsx` - Bucket replication UI (no credentials)
    - `web/frontend/src/pages/cluster/Nodes.tsx` - Bulk replication configuration
    - `web/frontend/src/lib/api.ts` - 5 new cluster replication API methods
    - `web/frontend/src/types/index.ts` - 5 new TypeScript interfaces for cluster replication
  - **Console API Endpoints**: 5 new REST endpoints
    - `POST /api/console/cluster/replication` - Create replication rule
    - `GET /api/console/cluster/replication` - List replication rules
    - `PUT /api/console/cluster/replication/:id` - Update rule
    - `DELETE /api/console/cluster/replication/:id` - Delete rule
    - `POST /api/console/cluster/replication/bulk` - Bulk node-to-node replication
  - **Complete Separation from User Replication**: Different tables, endpoints, authentication
  - **All Tests Passing**: 526+ backend tests, frontend build successful
  - **✅ PHASE 3.3 100% COMPLETE**

### Added - Cluster Dashboard UI (Phase 3)
- **Complete Web Console for Cluster Management** - Full-featured cluster management interface
  - **Cluster Page**: New `/cluster` route with comprehensive cluster management UI
  - **Navigation Integration**: "Cluster" menu item with Server icon (global admin only access)
  - **TypeScript Types**: 14 interfaces and 1 type for complete cluster entity definitions
  - **API Client**: 13 cluster management methods integrated into frontend API client
  - **Cluster Status Overview**: Real-time dashboard showing:
    - Total nodes, healthy/degraded/unavailable node counts
    - Total buckets, replicated buckets, local buckets statistics
    - Last updated timestamp
  - **Nodes Management Table**: Interactive table displaying all cluster nodes with:
    - Health status indicators (color-coded: green=healthy, yellow=degraded, red=unavailable)
    - Network latency in milliseconds
    - Storage capacity (used/total with progress bar)
    - Bucket count per node
    - Node priority for routing preferences
    - Last seen timestamp
  - **Initialize Cluster Dialog**: Create new cluster with:
    - Node name and region configuration
    - Automatic cluster token generation
    - Token display with copy-to-clipboard functionality
    - Instructions for joining other nodes
  - **Add Node Dialog**: Join existing cluster or add remote nodes with:
    - Endpoint URL configuration
    - Node token authentication
    - Node name, region, and priority settings
    - Form validation and error handling
  - **Edit Node Dialog**: Update existing node configuration:
    - Modify name, priority, region
    - Update metadata (JSON format)
    - Cannot edit endpoint or token (security)
  - **Cluster Operations**: Complete CRUD functionality
    - Remove nodes from cluster with confirmation
    - Manual health check trigger per node
    - Refresh cluster status and nodes list
    - Graceful error handling and loading states
  - **Frontend Build**: Successfully integrated with zero compilation errors

### Added - Multi-Node Cluster Management (Phase 2)
- **Complete Cluster Infrastructure** - Full implementation of multi-node cluster support with High Availability
  - **Cluster Manager**: Complete CRUD operations for cluster nodes, health monitoring, and configuration
  - **Smart Router with Failover**: Intelligent request routing to healthy nodes with automatic failover
  - **Bucket Location Cache**: 5-minute TTL cache for bucket-to-node mappings (5ms latency for cache hits vs 50ms for misses)
  - **Internal Proxy Mode**: Any node can receive any S3 request and proxy internally to the correct node
  - **Health Checker**: Background worker checking all nodes every 30 seconds with latency tracking
  - **SQLite Persistence**: 3 tables (cluster_config, cluster_nodes, cluster_health_history) for cluster state
  - **Console API Endpoints**: 13 REST endpoints for cluster management (initialize, join, nodes CRUD, health, cache)
  - **Flexible Replication**: Manual, user-controlled bucket replication (not automatic by region)
  - **High Availability Support**: External load balancer integration (HAProxy/nginx) for VIP management
  - **Interface Adapters**: Clean integration with existing bucket and replication managers
  - **Server Integration**: Cluster manager and router fully integrated into server.go lifecycle
  - **Graceful Shutdown**: Proper cleanup of cluster resources on server stop
  - **No Breaking Changes**: Cluster is opt-in, existing single-node deployments unaffected

### Changed - Bucket Replication Improvements
- **Complete Replication Implementation** - Full working implementation with real S3 transfers
  - **AWS SDK v2 Integration**: Real S3 client using AWS SDK for Go v2 with custom endpoint support
  - **Working Transfers**: Objects are now actually transferred from local storage to remote S3 servers
  - **Automatic Scheduler**: Background scheduler checks rules every minute and triggers syncs based on `schedule_interval`
  - **Concurrency Protection**: Per-rule mutex locks prevent overlapping syncs of the same bucket
  - **Manual Sync Trigger**: New POST endpoint `/api/v1/buckets/{bucket}/replication/rules/{ruleId}/sync`
  - **Frontend "Sync Now" Button**: UI button added to manually trigger replication syncs
  - **Object Manager Integration**: Proper adapters for reading objects from local storage
  - **Bucket Lister Integration**: Adapter for listing all objects in a bucket
  - **All Tests Passing**: 350+ backend tests passing, frontend build successful

## [0.5.0-beta] - 2025-12-04

### Added - Bucket Replication
- **Bucket Replication System** - Complete implementation of S3-compatible bucket replication
  - **S3 Protocol-Level Replication**: Uses standard S3 API for cross-bucket replication
  - **Flexible Destination Configuration**:
    - Endpoint URL (supports AWS S3, MinIO, or other MaxIOFS instances)
    - Destination bucket name
    - S3 access key and secret key for authentication
    - Optional region parameter
  - **Multiple Replication Modes**:
    - **Realtime**: Immediate replication on object changes
    - **Scheduled**: Periodic batch replication with configurable intervals (minutes)
    - **Batch**: Manual replication on demand
  - **Conflict Resolution Strategies**: Last Write Wins, Version-Based, Primary Wins
  - **Queue-Based Processing**: Async worker pools with configurable concurrency
  - **Retry Mechanism**: Exponential backoff for failed replications with max retries
  - **Selective Replication**: Prefix filters, delete replication toggle, metadata replication
  - **Priority System**: Rule ordering for multiple replication targets
  - **Comprehensive Metrics**: Pending, completed, failed objects, bytes replicated
  - **SQLite Persistence**: Rules, queue items, and replication status tracking
  - **Web Console Integration**: Visual rule management in bucket settings page
  - **Console API**: Full CRUD endpoints for replication rule management
  - **Audit Logging**: All replication operations logged for compliance
  - **Test Coverage**: 23 automated tests covering CRUD, queueing, processing (100% pass rate)

### Added - Logging System
- **Production-Ready Logging Infrastructure** - Complete logging system for operations and debugging
  - **Multiple Output Targets**: Console, file, HTTP endpoints, syslog
  - **Log Levels**: DEBUG, INFO, WARN, ERROR with configurable filtering
  - **Structured Logging**: JSON format with contextual fields (tenant, user, bucket, object)
  - **HTTP Output**: Batch delivery with retry mechanism and authentication
  - **Syslog Integration**: Remote logging to syslog servers (UDP/TCP)
  - **Performance Optimized**: Async buffering and batch processing
  - **Test Coverage**: 26 tests covering all output types and configurations (100% pass rate)

### Added - User Experience & Theming
- **Theme System** - User-customizable interface themes with multiple modes
  - **Theme Options**: System (auto), Dark, Light modes
  - **User Preferences**: Per-user theme selection with persistence
  - **Settings Page**: Dedicated user settings UI for theme management
  - **Persistent Storage**: Theme preference saved across sessions

### Added - CI/CD & Build System
- **Nightly Build Pipeline** - Automated nightly builds with comprehensive testing
  - **Automated Testing**: Frontend and backend tests run on every nightly build
  - **Multi-Architecture**: Builds for linux-amd64, linux-arm64, darwin-amd64, darwin-arm64, windows-amd64
  - **Debian Packages**: Automated .deb package generation for amd64 and arm64
  - **S3 Distribution**: Automated upload to S3 bucket with version tracking
  - **Smart Caching**: Commit-based build skipping to save resources
  - **Test Reports**: Automated test execution with pass/fail reporting

### Added - Testing Infrastructure
- **ACL Module Test Suite** - 25 tests covering S3-compatible canned ACLs, permissions validation, bucket/object ACL operations, and multi-tenant isolation (77.0% coverage)
- **Middleware Module Test Suite** - 30 test functions covering logging, rate limiting, CORS, and verbose logging middleware (87.4% coverage)
- **Lifecycle Module Test Suite** - 12 tests for lifecycle worker, policy processing, noncurrent version expiration, and delete marker cleanup (67.9% coverage)
- **Storage Module Test Suite** - 40 tests for filesystem operations and metadata management (79.1% coverage)
- **Metadata Module Test Suite** - Expanded to 30 tests covering object versioning and metadata operations (52.4% coverage)
- **Bucket Module Test Suite** - 47 tests for bucket validation, CRUD operations, configurations, and metrics (49.8% coverage)
- **Object Module Test Suite** - Expanded with 83 additional tests for retention policies, object lock, and type adapters (48.4% coverage)
- **Logging System Test Suite** - 26 tests covering manager configuration, HTTP output, and syslog integration (100% pass rate)
- **S3 API Test Expansion** - Coverage improved from 16.6% to 30.9% with tests for versioning, batch delete, copy, range requests, and list versions

### Fixed
- **Frontend Session Management** - Fixed unexpected logouts during active sessions caused by background React Query polling triggering 401 errors. Implemented intelligent error handling with consecutive error tracking
- **VEEAM SOSAPI Capacity** - Fixed capacity reporting to respect tenant quotas instead of reporting full disk capacity
- **ListObjectVersions** - Fixed empty results for non-versioned buckets by correcting object key prefix
- **Console API Test Coverage** - Expanded from 4.4% to 12.7% with 19 new tests for 2FA, tenant management, access keys, and user operations

### Changed
- **Backend Test Coverage** - Improved to ~53% with 504 tests (100% pass rate)
- **Frontend Test Coverage** - Maintained at 100% with 64 tests
- **Version**: Updated from 0.4.2-beta to 0.5.0-beta
- **Features Complete**: Improved from ~95% to ~96%

### Added
- **Frontend Testing Infrastructure** - Complete test suite with 64 tests using Vitest and React Testing Library
- Test coverage for Login, Dashboard, Buckets, and Users pages
- Custom test utilities with provider wrappers and API mocks

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
- Web Console with Next.js frontend
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

### Current Status: BETA (v0.5.0-beta)

**Completed:**
- ✅ All S3 core operations validated with AWS CLI
- ✅ Comprehensive testing (core features)
- ✅ Visual UI for bucket configurations
- ✅ Console API fully functional
- ✅ Multipart upload validated
- ✅ Zero critical bugs in core functionality
- ✅ Warp stress testing completed

**In Progress:**
- 🔄 80%+ backend test coverage (current: ~53%)
- 🔄 Comprehensive API documentation
- 🔄 Security review and audit
- 🔄 Complete user documentation

### Upgrade Path to Stable (v1.0.0)

Requirements:
- [ ] Security audit by third party
- [ ] 90%+ test coverage
- [ ] 6+ months of real-world usage
- [ ] Performance validated at scale
- [ ] Complete feature set documented
- [ ] All critical bugs resolved

---

**Note**: This project is currently in BETA phase. Suitable for development, testing, and staging environments. Production use requires extensive testing. Always backup your data.
