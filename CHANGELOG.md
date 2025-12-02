# Changelog

All notable changes to MaxIOFS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased] - 2025-12-02

### Added
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
- **Backend Test Coverage** - Improved to 48.2% with 352 tests (100% pass rate)
- **Frontend Test Coverage** - Maintained at 100% with 64 tests

## [Previous Releases] - 2025-11-28

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

### Current Status: BETA (v0.4.2-beta)

**Completed:**
- ✅ All S3 core operations validated with AWS CLI
- ✅ Comprehensive testing (core features)
- ✅ Visual UI for bucket configurations
- ✅ Console API fully functional
- ✅ Multipart upload validated
- ✅ Zero critical bugs in core functionality
- ✅ Warp stress testing completed

**In Progress:**
- 🔄 80%+ backend test coverage (current: ~48%)
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
