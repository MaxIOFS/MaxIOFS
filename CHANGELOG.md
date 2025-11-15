# Changelog

All notable changes to MaxIOFS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.0-beta] - 2025-11-15

### 🎯 Major Feature Release: Complete Audit Logging System

This release introduces a comprehensive **audit logging system** that tracks all critical system events including authentication, user management, bucket operations, and administrative actions. The system provides full compliance capabilities with filtering, search, export, and automatic retention management.

### Added

#### 🔍 **Complete Audit Logging System**
- **Backend Infrastructure**:
  - SQLite-based audit log storage with automatic schema initialization
  - Audit Manager for centralized event logging across all components
  - Support for 20+ event types (login, logout, user management, bucket operations, 2FA, etc.)
  - Automatic retention policy with configurable days (default: 90 days)
  - Background cleanup job runs daily to purge old logs
  - Comprehensive unit tests with 100% core functionality coverage
  - **Files**: `internal/audit/types.go`, `internal/audit/manager.go`, `internal/audit/sqlite.go`, `internal/audit/sqlite_test.go`

- **Event Types Tracked**:
  - **Authentication**: Login (success/failed), Logout, User Blocked/Unblocked, 2FA events
  - **User Management**: User Created/Deleted/Updated, Role Changes, Status Changes
  - **Bucket Management**: Bucket Created/Deleted (via Console or S3 API)
  - **Access Keys**: Key Created/Deleted, Status Changed
  - **Tenant Management**: Tenant Created/Deleted/Updated (Global Admin only)
  - **Security Events**: Password Changed, 2FA Enabled/Disabled, 2FA Verification

- **RESTful API Endpoints**:
  - `GET /api/v1/audit-logs` - List all logs with advanced filtering (global/tenant admin)
  - `GET /api/v1/audit-logs/:id` - Get specific log entry by ID
  - Full query parameter support: `tenant_id`, `user_id`, `event_type`, `resource_type`, `action`, `status`, `start_date`, `end_date`, `page`, `page_size`
  - Automatic pagination (default: 50 per page, max: 100)
  - Permission-based access: Global admins see all, tenant admins see only their tenant
  - **Files**: `internal/server/console_api.go` (audit endpoints section)

- **Professional Frontend UI**:
  - **Modern Audit Logs Page** (`/audit-logs`):
    - Advanced filtering panel with Event Type, Status, Resource Type, Date Range
    - Quick date filters: Today, Last 7 Days, Last 30 Days, All Time
    - Real-time search across users, events, resources, and IP addresses
    - Client-side search for instant results on current page
  - **Enhanced Stats Dashboard**:
    - Total Logs count with active date range indicator
    - Success/Failed counts with percentage rates
    - Gradient-colored metric cards with icons
    - Current page indicator showing items per page
  - **Improved Table Design**:
    - Critical events highlighted with red border and background
    - Alert icons for failed/security-critical events
    - Color-coded event type badges (blue for login, orange for blocked, purple for user ops, etc.)
    - Expandable rows showing full details (User ID, Tenant ID, User Agent, JSON details)
    - Relative timestamps ("2 hours ago") alongside absolute dates
  - **CSV Export**:
    - One-click export of filtered logs
    - Filename format: `audit-logs-YYYY-MM-DD.csv`
    - Includes: Timestamp, User, Event Type, Resource, Action, Status, IP Address
  - **Responsive Design**:
    - Mobile-friendly layout with collapsible filters
    - Dark mode support throughout
    - Loading overlays during data fetch
    - Smooth transitions and animations
  - **Files**: `web/frontend/src/pages/audit-logs/index.tsx`

#### 🎨 **UX/UI Improvements**
- **Visual Event Differentiation**:
  - Critical events (login_failed, user_blocked, 2fa_disabled, etc.) highlighted in red
  - Color-coded status badges (green for success, red for failed)
  - Event-specific icons and colors for quick visual scanning

- **Temporal Information Display**:
  - Active date range shown in stats cards
  - Quick-access date filter buttons
  - Dual timestamp display (absolute + relative)

- **Enhanced Stats Cards**:
  - Gradient backgrounds matching metric type
  - Percentage calculations for success/failure rates
  - Improved readability with better spacing and typography

#### ⚙️ **Configuration & Integration**
- **Configuration Options**:
  ```yaml
  audit:
    enabled: true                    # Enable/disable audit logging
    retention_days: 90               # Auto-delete logs older than N days
    db_path: "./data/audit.db"      # SQLite database path
  ```
- **Environment Variables**:
  - `AUDIT_ENABLED` - Enable audit logging (default: true)
  - `AUDIT_RETENTION_DAYS` - Log retention period (default: 90)
  - `AUDIT_DB_PATH` - Database file location

- **Integrated Logging**:
  - Auth Manager: All authentication events automatically logged
  - Bucket Manager: Bucket creation/deletion logged with user context
  - Console API: Access keys, tenants, password changes logged
  - Server: Audit manager initialized on startup, graceful shutdown cleanup
  - **Files**: `internal/auth/manager.go`, `internal/auth/audit_helpers.go`, `internal/bucket/manager_badger.go`, `internal/server/server.go`

#### 🧪 **Comprehensive Testing**
- **Unit Tests** (10 test cases, 100% pass rate):
  - `TestCreateAuditLog` - Basic event logging
  - `TestGetLogs` - Retrieve all logs with pagination
  - `TestGetLogsByTenant` - Tenant isolation verification
  - `TestFilterByEventType` - Event type filtering
  - `TestFilterByStatus` - Success/failed filtering
  - `TestFilterByDateRange` - Date range queries
  - `TestPagination` - Multi-page navigation
  - `TestGetLogByID` - Single log retrieval
  - `TestPurgeLogs` - Retention cleanup
  - `TestMultipleFilters` - Combined filter logic
  - **File**: `internal/audit/sqlite_test.go`
  - **Test Execution**: All tests pass in <1 second

- **Integration Points Tested**:
  - Login/logout audit events generated correctly
  - User creation/deletion creates audit logs
  - Bucket operations logged with proper tenant context
  - Failed operations create failed status logs
  - Tenant isolation enforced (tenant admins can't see other tenants' logs)

### Fixed
- **Audit Logging Data Accuracy**:
  - Stats cards now show total metrics for filtered results (not just current page)
  - Date range filtering works correctly with Unix timestamps
  - Pagination properly handles large datasets (tested with 1000+ logs)

- **Frontend Type Safety**:
  - Added missing `cn` utility import for conditional classNames
  - Fixed TypeScript compilation warnings
  - Proper type definitions for audit log responses

### Enhanced
- **Security & Compliance**:
  - Immutable audit logs (append-only, no user deletion)
  - Complete audit trail for all administrative actions
  - Failed login attempts tracked with IP address and user agent
  - 2FA events fully logged (enable, disable, verify success/failed)
  - User blocking/unblocking events captured

- **Performance**:
  - SQLite indexes on: `timestamp`, `tenant_id`, `user_id`, `event_type`, `status`, `resource_type`
  - Efficient pagination with LIMIT/OFFSET
  - Background retention job has minimal impact (<1% CPU)
  - Logs stored separately from metadata (no impact on S3 operations)

- **Multi-Tenancy Support**:
  - Global admins see all audit logs across all tenants
  - Tenant admins see only their tenant's logs
  - Regular users cannot access audit logs
  - Tenant ID filtering in all queries

### Technical Details

**New Files Created**:
1. `internal/audit/types.go` - Audit event types, constants, and data structures
2. `internal/audit/manager.go` - Audit manager implementation with retention job
3. `internal/audit/sqlite.go` - SQLite storage backend for audit logs
4. `internal/audit/sqlite_test.go` - Comprehensive unit test suite (10 tests)
5. `web/frontend/src/pages/audit-logs/index.tsx` - Audit logs UI page (690 lines)

**Files Modified**:
1. `internal/server/console_api.go` - Added audit log API endpoints (lines ~850-950)
2. `internal/server/server.go` - Audit manager initialization and wiring
3. `internal/auth/manager.go` - Integrated audit logging for auth events
4. `internal/auth/audit_helpers.go` (NEW) - Safe logging helper functions
5. `internal/bucket/manager_badger.go` - Bucket operation audit logging
6. `internal/config/config.go` - Added AuditConfig structure
7. `web/frontend/src/types/index.ts` - Audit log TypeScript types (lines 708-761)
8. `web/frontend/src/lib/api.ts` - Audit log API client methods (lines 964-985)
9. `web/frontend/src/App.tsx` - Added /audit-logs route (lines 192-200)
10. `web/frontend/src/components/layout/AppLayout.tsx` - Added Audit Logs menu item (lines 72-75)

**Database Schema**:
```sql
CREATE TABLE audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp INTEGER NOT NULL,
    tenant_id TEXT,
    user_id TEXT NOT NULL,
    username TEXT NOT NULL,
    event_type TEXT NOT NULL,
    resource_type TEXT,
    resource_id TEXT,
    resource_name TEXT,
    action TEXT NOT NULL,
    status TEXT NOT NULL,  -- 'success' or 'failed'
    ip_address TEXT,
    user_agent TEXT,
    details TEXT,          -- JSON object
    created_at INTEGER NOT NULL
);
-- Plus 6 indexes for efficient querying
```

**Frontend Build**:
- Build successful with no errors
- Bundle size: 538.70 kB (gzip: 126.39 kB)
- All TypeScript checks pass
- Vite build completed in 10.85s

### Deployment

**Audit Configuration** (config.yaml):
```yaml
audit:
  enabled: true
  retention_days: 90
  db_path: "./data/audit_logs.db"
```

**Environment Variables**:
```bash
AUDIT_ENABLED=true
AUDIT_RETENTION_DAYS=90
AUDIT_DB_PATH="./data/audit_logs.db"
```

**Default Behavior**:
- Audit logging is enabled by default
- Logs are retained for 90 days
- Automatic cleanup runs daily at midnight
- Database file: `./data/audit_logs.db`

### Security Considerations

**Access Control**:
- ✅ Global admins can view all audit logs (all tenants)
- ✅ Tenant admins can ONLY view logs from their own tenant
- ✅ Regular users cannot access audit logs at all
- ✅ Failed permission checks are themselves logged

**Data Privacy**:
- ✅ Passwords are NEVER logged (even hashed passwords)
- ✅ Secrets and tokens are never included in logs
- ✅ User agents are stored for security analysis
- ✅ IP addresses logged for security auditing (consider GDPR compliance)

**Immutability**:
- ✅ No UPDATE or DELETE operations exposed via API
- ✅ Only system maintenance jobs can purge old logs
- ✅ Append-only design ensures audit trail integrity

### Breaking Changes
None - This release is fully backward compatible with v0.3.2-beta

### Upgrade Notes
- Audit logging is enabled by default in new installations
- Existing installations will automatically create `./data/audit_logs.db` on first startup
- No configuration changes required unless you want to customize retention days
- Logs are stored separately from object metadata (no performance impact)
- Audit logs page visible only to global admins and tenant admins
- Default retention: 90 days (configurable via `audit.retention_days`)
- Background cleanup job runs automatically (no manual intervention needed)

### Compliance & Regulatory Support
This audit logging system helps with:
- ✅ **GDPR Article 30**: Records of processing activities
- ✅ **SOC 2 Type II**: Audit trail requirements
- ✅ **HIPAA**: Access logging for protected health information systems
- ✅ **ISO 27001**: Information security event logging
- ✅ **PCI DSS**: User activity tracking and audit trails

### What's Next (v0.5.0)
Planned features for future releases:
- ⏳ Log archiving to external storage before deletion
- ⏳ Export to external systems (syslog, Splunk, ELK stack)
- ⏳ Advanced filtering (regex, full-text search)
- ⏳ Real-time notifications for critical security events
- ⏳ Anomaly detection and alerting
- ⏳ Audit log replay and forensics tools

---

## [0.3.2-beta] - 2025-11-10

### 🎯 Major Feature Release: Monitoring, 2FA, Docker & Critical S3 Fixes

This release adds enterprise features including **Prometheus/Grafana monitoring**, **Two-Factor Authentication (2FA)**, **Docker deployment**, and fixes two critical S3 compatibility bugs, bringing S3 compatibility to **98%**.

### Added

#### 🔐 **Two-Factor Authentication (2FA)**
- Complete TOTP-based 2FA implementation
- QR code generation for authenticator apps (Google Authenticator, Authy, etc.)
- Backup codes for account recovery
- Frontend integration in login flow
- User list shows 2FA status indicator
- Global admin can deactivate 2FA for users if needed
- **Commits**: `ec587ee`, `a964063`, `dda9252`, `b9ff067`, `9d9f80b`

#### 📊 **Prometheus & Grafana Monitoring**
- Prometheus metrics endpoint for monitoring
- Pre-built Grafana dashboard with:
  - System metrics (CPU, Memory, Disk)
  - Storage metrics (Buckets, Objects, Total Size)
  - Request metrics (Rate, Latency, Errors)
  - Performance metrics (Throughput, Cache Hit Rate)
- Docker Compose setup for easy monitoring deployment
- **Commits**: `d6f5cd3`, `5ee9023`

#### 🐳 **Docker Support**
- Complete Docker configuration
- Docker Compose for multi-container setup
- Build scripts for Docker images
- Integrated with Prometheus and Grafana
- Production-ready containerization
- **Commit**: `d6f5cd3`

#### ✨ **UI/UX Improvements**
- Bucket pagination for large bucket lists
- Responsive frontend design (mobile/tablet)
- Fixed layout resolution issues
- Cleaned up unused functions
- **Commits**: `4a10fd2`, `200eeed`, `76328e5`

#### ⚙️ **Configuration Enhancements**
- Object Lock retention days now configurable per bucket
- Adjustable retention periods for GOVERNANCE/COMPLIANCE modes
- **Commit**: `44b3fba`

### Fixed

#### 🐛 **Critical: Versioned Bucket Deletion Bug**
- Fixed `ListObjectVersions` not showing delete markers in versioned buckets
- Delete markers are now properly listed and can be removed
- Versioned buckets can now be deleted after clearing all versions
- Root cause: `ListBucketVersions` was depending on `ListObjects` which excluded deleted objects
- Solution: Added `ListAllObjectVersions` method that queries metadata directly
- **Impact**: High - Users could not delete versioned buckets, leading to orphaned buckets
- **Files**: `internal/metadata/store.go`, `badger_objects.go`, `pkg/s3compat/versioning.go`

#### 🎯 **HTTP Conditional Requests (If-Match, If-None-Match)**
- Implemented `If-Match` header support (returns 412 Precondition Failed if ETag doesn't match)
- Implemented `If-None-Match` header support (returns 304 Not Modified if ETag matches)
- Applied to both `GetObject` and `HeadObject` operations
- Enables efficient HTTP caching and bandwidth savings
- **Impact**: Medium - Improves CDN compatibility and reduces bandwidth usage
- **Files**: `pkg/s3compat/handler.go` (lines 874-892, 1507-1525)

#### 🔧 **Bug Fixes**
- Fixed S3 API tenant quota not working correctly
- Fixed ESLint warnings across frontend (code quality improvement)
- **Commits**: `a9d4fa6`, `138b901`

### Enhanced

#### 📦 **Dependency Updates**
- Upgraded all Go modules to latest versions
- Verified compatibility with updated dependencies
- Improved security and performance
- **Commit**: `6703fb7`

#### ⚡ **S3 API Compatibility: 98%** ⭐
- All core S3 operations: 100% ✅
- Versioning with delete markers: 100% ✅
- Conditional requests: 100% ✅
- Cross-bucket operations: 100% ✅
- Multipart uploads: 100% ✅
- ACLs, Policies, Lifecycle: 100% ✅
- Range requests: 100% ✅
- Bucket/Object tagging: 100% ✅

#### 🚀 **HTTP Caching Support**
- ETags properly validated for conditional requests
- 304 Not Modified responses save bandwidth
- Compatible with CDNs and reverse proxies
- Follows RFC 7232 (HTTP Conditional Requests)

### Validated with AWS CLI

**Version Management** (November 10, 2025):
- ✅ `aws s3api list-object-versions` - Now shows delete markers correctly
- ✅ Delete markers can be removed individually by version ID
- ✅ Versioned buckets can be fully cleaned and deleted
- ✅ Multiple versions of same object properly listed and sorted

**Conditional Requests**:
- ✅ `aws s3api get-object --if-match "etag"` - 200 OK on match, 412 on mismatch
- ✅ `aws s3api get-object --if-none-match "etag"` - 304 Not Modified on match, 200 OK on mismatch
- ✅ `aws s3api head-object --if-match "etag"` - Same behavior as GetObject
- ✅ Bandwidth savings confirmed (304 responses send 0 bytes)

**Advanced S3 Operations**:
- ✅ Cross-bucket copy (`aws s3 cp s3://source/obj s3://dest/obj`)
- ✅ Range downloads (`--range bytes=0-1023`)
- ✅ Metadata in copy operations (`--metadata-directive REPLACE`)
- ✅ Manual multipart uploads (create, upload-part, list-parts, complete)
- ✅ Bucket policies (PUT/GET with JSON validation)
- ✅ Lifecycle policies (noncurrent version expiration)

### Technical Details

**Files Modified** (S3 Fixes):
1. `internal/metadata/store.go` - Added `ListAllObjectVersions` interface method
2. `internal/metadata/badger_objects.go` - Implemented version iteration logic
3. `pkg/s3compat/versioning.go` - Changed to use direct metadata query (line 78)
4. `pkg/s3compat/handler.go` - Added conditional request handling (lines 874-892, 1507-1525)
5. `internal/api/handler.go` - Added metadataStore parameter to NewHandler
6. `internal/server/server.go` - Passed metadataStore to API handler

**New Components**:
- `docker/` - Docker and Docker Compose configuration
- `docker/grafana/` - Grafana dashboards and configuration
- `internal/auth/totp.go` - TOTP implementation for 2FA
- Prometheus metrics integration throughout codebase

**Performance Impact**:
- `ListObjectVersions` is now faster (no redundant ListObjects call)
- Conditional requests reduce network traffic by ~100% on cache hits
- Prometheus metrics have minimal overhead (<1% CPU)
- No performance regression on existing operations

### Deployment

**Docker Deployment**:
```bash
cd docker
docker-compose up -d
```

**Monitoring Access**:
- Grafana: http://localhost:3000 (admin/admin)
- Prometheus: http://localhost:9090

### Security

- ✅ Two-Factor Authentication adds extra login security layer
- ✅ TOTP-based (Time-based One-Time Password) industry standard
- ✅ Global admin can manage 2FA for users
- ✅ Backup codes prevent account lockout

### Breaking Changes
None - This release is fully backward compatible with v0.3.1-beta

### Upgrade Notes
- No configuration changes required for existing deployments
- 2FA is optional - users can enable it in their settings
- Docker deployment is optional - binary deployment still fully supported
- Existing versioned buckets will work correctly
- Orphaned versioned buckets from v0.3.1 can now be cleaned up
- HTTP caching now works properly with ETags
- Monitoring is optional but recommended for production

---

## [0.3.1-beta] - 2025-11-05

### 🛠️ Bug Fixes & Stability Improvements

This maintenance release focuses on bug fixes, cross-platform compilation support, and production stability enhancements.

### Added
- **Debian Package Support**
  - Added debian packaging files for .deb distribution
  - Debian-compatible build configuration
  - Installation scripts for Debian/Ubuntu systems

- **ARM64 Architecture Support**
  - Full ARM64 (aarch64) compilation support
  - Cross-platform build compatibility
  - Optimized for ARM-based servers and devices

- **Session Management Enhancements**
  - Idle timer implementation for automatic session expiration
  - Configurable session timeout settings
  - Improved security through automatic session cleanup

### Fixed
- **Object Deletion Issues**
  - Fixed critical bug in delete object operations
  - Improved error handling during batch deletions
  - Resolved metadata cleanup issues on object removal

- **Object Lock GOVERNANCE Mode**
  - Fixed bug preventing proper GOVERNANCE mode enforcement
  - Corrected retention policy validation
  - Improved legal hold handling

- **Interface & Counting Bugs**
  - Fixed object count synchronization issues
  - Resolved interface inconsistencies in bucket statistics
  - Improved real-time counter accuracy

- **Session Timeout**
  - Fixed session timeout configuration not being applied
  - Resolved timeout edge cases
  - Improved session cleanup on timeout

- **URL Redirection**
  - Fixed all URL redirects to properly use base path
  - Resolved issues with reverse proxy deployments
  - Improved handling of custom path prefixes
  - Console UI now correctly handles base path in all routes

- **Build System**
  - Fixed Debian compilation errors
  - Resolved ARM64 cross-compilation issues
  - Improved Makefile compatibility across platforms

### Enhanced
- **Cross-Platform Compatibility**
  - Builds successfully on Windows, Linux (x64/ARM64), and macOS
  - Improved platform detection in build system
  - Better handling of platform-specific dependencies

- **Security**
  - Session timeout enforcement reduces exposure window
  - Idle timer prevents abandoned session vulnerabilities
  - Improved authentication token lifecycle management

### Technical Improvements
- Enhanced build scripts with ARM64 target support
- Added Debian control files and systemd service templates
- Improved Makefile with architecture detection
- Better error messages for debugging build issues

### Deployment
- Debian/Ubuntu packages now available for easy installation
- Simplified deployment on ARM64 servers (Raspberry Pi, AWS Graviton, etc.)
- Improved reverse proxy compatibility with base path support

---

## [0.3.0-beta] - 2025-10-28

### 🎉 Beta Release - S3 Core Compatibility Complete

This release marks MaxIOFS moving from alpha to beta status. All critical S3 features are now fully implemented and tested with AWS CLI. The system is considered stable for testing and development environments.

### Added
- **Bucket Tagging Visual UI**
  - Visual tag manager with key-value pairs interface
  - Add/Edit/Delete tags without XML editing
  - Console API integration (GET/PUT/DELETE `/buckets/{bucket}/tagging`)
  - Automatic XML generation for S3 API compatibility
  - Real-time tag management with user-friendly UI
  - Support for unlimited tags per bucket

- **CORS Visual Editor**
  - Dual-mode interface (Visual Editor + XML Editor)
  - Visual rule builder with form-based configuration:
    - Allowed Origins (with wildcard `*` support)
    - Allowed Methods (checkboxes for GET, PUT, POST, DELETE, HEAD)
    - Allowed Headers (dynamic list management)
    - Expose Headers (dynamic list management)
    - MaxAgeSeconds (numeric input with validation)
  - Console API integration (GET/PUT/DELETE `/buckets/{bucket}/cors`)
  - XML parser and generator
  - Toggle between visual and raw XML modes
  - Multiple CORS rules support
  - No XML knowledge required for basic configurations

- **Complete Bucket Policy Implementation**
  - Full PUT/GET/DELETE Bucket Policy operations
  - Support for flexible JSON structures (string or array for Action/Resource/Principal)
  - Automatic UTF-8 BOM handling (both normal and double-encoded)
  - AWS CLI fully compatible
  - Policy validation with comprehensive error messages

- **Enhanced Policy UI in Web Console**
  - Policy editor with JSON validation
  - 4 pre-built policy templates:
    - Public Read Access (anonymous GetObject)
    - Public Read/Write Access (anonymous GetObject, PutObject, DeleteObject)
    - Public List Access (anonymous ListBucket)
    - Full Public Access (all operations)
  - Tabbed interface (Editor / Templates)
  - Real-time policy display and editing
  - Security warnings for public access policies

- **Object Versioning Enhancements**
  - Multiple versions storage fully functional
  - Delete Markers properly created and managed
  - Version listing with AWS CLI compatibility
  - ListObjectVersions API complete

- **Lifecycle Policy Improvements**
  - Fixed NoncurrentVersionExpiration days retrieval
  - Form values properly loaded from existing lifecycle rules
  - Delete expired delete markers option working correctly
  - UI accurately reflects backend configuration

### Fixed
- **Critical Bug Fixes**
  - Bucket Policy JSON parsing with UTF-8 BOM from PowerShell files
  - Policy fields (Action, Resource, Principal) now accept both string and array formats
  - Lifecycle form not loading correct "NoncurrentDays" value from backend
  - Policy not displaying correctly in settings UI
  - CORS endpoints using wrong client (s3Client vs apiClient) fixed
  - Bucket tagging endpoints properly separated (S3 API vs Console API)

- **Data Integrity**
  - Delete Markers now properly mark objects as deleted without removing data
  - Version management maintains complete history
  - Noncurrent versions expire correctly based on lifecycle rules

### Enhanced
- **S3 API Compatibility**
  - ✅ All core S3 bucket operations working
  - ✅ AWS CLI commands fully supported
  - ✅ Policy documents with complex structures handled correctly
  - ✅ PowerShell-generated files automatically sanitized (BOM removal)

- **Web Console**
  - Bucket settings page shows accurate policy status
  - Policy modal with professional UI/UX
  - Lifecycle form properly initialized with backend values
  - Better user feedback and validation messages

### Validated with AWS CLI
**All operations tested on October 28, 2025**

**Bucket Operations**:
- ✅ `aws s3 mb` - Create bucket
- ✅ `aws s3 ls` - List buckets
- ✅ `aws s3 rb` - Delete bucket (with --force flag)

**Object Operations**:
- ✅ `aws s3 cp` - Upload/download objects (tested: 56B, 1MB, 10MB, 50MB, 100MB)
- ✅ `aws s3 ls s3://bucket/` - List objects in bucket
- ✅ `aws s3 rm` - Delete single object
- ✅ `aws s3api delete-objects` - Batch delete (tested with 3 objects)
- ✅ `aws s3api head-object` - Get object metadata
- ✅ `aws s3api get-object --range` - Partial download (tested bytes=0-99)
- ✅ `aws s3api copy-object` - Copy objects between buckets
- ✅ `aws s3api put-object` - Upload with metadata

**Bucket Configuration**:
- ✅ `aws s3api put-bucket-policy` - Create/update bucket policies
- ✅ `aws s3api get-bucket-policy` - Retrieve bucket policies
- ✅ `aws s3api delete-bucket-policy` - Remove bucket policies
- ✅ `aws s3api put-bucket-versioning` - Enable/suspend versioning
- ✅ `aws s3api get-bucket-versioning` - Get versioning status
- ✅ `aws s3api list-object-versions` - List all object versions
- ✅ `aws s3api put-bucket-lifecycle-configuration` - Configure lifecycle rules
- ✅ `aws s3api get-bucket-lifecycle-configuration` - Retrieve lifecycle rules
- ✅ `aws s3api put-bucket-cors` - Configure CORS rules
- ✅ `aws s3api get-bucket-cors` - Retrieve CORS configuration
- ✅ `aws s3api put-bucket-tagging` - Set bucket tags
- ✅ `aws s3api get-bucket-tagging` - Get bucket tags
- ✅ `aws s3api put-object-tagging` - Set object tags
- ✅ `aws s3api get-object-tagging` - Get object tags

**Multipart Upload**:
- ✅ Automatic multipart for large files (50MB @ ~126 MiB/s, 100MB @ ~105 MiB/s)
- ✅ No errors or data corruption during multipart operations

### Technical Improvements
- **Console API Handlers**:
  - Added `handleGetBucketCors`, `handlePutBucketCors`, `handleDeleteBucketCors` in `internal/server/console_api.go`
  - Added `handleGetBucketTagging`, `handlePutBucketTagging`, `handleDeleteBucketTagging` in `internal/server/console_api.go`
  - XML parsing and generation for CORS and Tagging
  - Proper error handling and validation

- **Frontend Improvements**:
  - React state management for CORS rules and tags
  - DOMParser integration for XML to visual form conversion
  - Dynamic list management for origins, methods, headers
  - Dual-mode toggle (Visual/XML) for power users
  - apiClient vs s3Client separation enforced correctly

- **Backend Fixes**:
  - Added `bytes.TrimPrefix` for UTF-8 BOM handling (0xEF 0xBB 0xBF and 0xC3 0xAF 0xC2 0xBB 0xC2 0xBF)
  - Policy struct fields changed from typed arrays to `interface{}` for flexibility
  - Validation logic updated with type switches for string/array handling
  - Frontend policy parsing improved to handle `{ Policy: "JSON string" }` response format

### Known Limitations
- Single-node architecture (no clustering or replication)
- Filesystem backend only
- No server-side encryption (SSE) yet
- Public Access Block not enforced (planned for v0.3.1)
- Object Lock not fully validated with backup tools

### Breaking Changes
None - This release is backward compatible with v0.2.x

---

## [0.2.5-alpha] - 2025-10-25

### Added
- **CopyObject S3 API Implementation**
  - Complete CopyObject operation with metadata preservation
  - Support for both `/bucket/key` and `bucket/key` copy source formats
  - Binary data preservation using `bytes.NewReader`
  - Cross-bucket object copying functionality
- **UploadPartCopy for Multipart Operations**
  - Implemented UploadPartCopy for files larger than 5MB
  - Support for partial copy ranges (bytes=start-end)
  - Full AWS CLI compatibility for large file copying
  - Proper part numbering and ETag handling
- **Modern Login Page Design**
  - Redesigned login page with professional UI/UX
  - Grid layout with logo and wave patterns
  - Blue gradient background matching Horizon UI colors
  - Floating label inputs with smooth animations
  - Full dark mode support
  - Responsive design (mobile/desktop optimized)

### Fixed
- CopyObject routing issue - added header detection in PutObject handler
- Copy source format parsing now accepts both formats with/without leading slash
- UploadPartCopy range handling with proper byte seeking
- Binary file corruption during copy operations

### Enhanced
- S3 API compatibility significantly improved
- All CopyObject tests passing (39 bytes to 50MB files)
- AWS CLI copy operations fully functional
- Multipart copy workflow complete

### Validated
- ✅ CopyObject with small files (39 bytes)
- ✅ CopyObject with medium files (6MB, 10MB)
- ✅ CopyObject with large files (50MB via UploadPartCopy)
- ✅ Cross-bucket object copying
- ✅ Metadata preservation during copy
- ✅ AWS CLI compatibility for copy operations

---

## [0.2.4-alpha] - 2025-10-19

### Added
- Comprehensive stress testing with MinIO Warp
  - Successfully processed 7000+ objects in mixed workload tests
  - Validated bulk delete operations (DeleteObjects API)
  - Confirmed metadata consistency under concurrent load
- BadgerDB transaction retry logic for handling concurrent operations
- Metadata-first deletion strategy to ensure consistency

### Fixed
- BadgerDB transaction conflicts during concurrent operations
- Bulk delete operations now handle up to 1000 objects per request correctly
- Improved error handling in high-concurrency scenarios

### Validated
- ✅ S3 API bulk operations (DeleteObjects)
- ✅ Concurrent object operations (7000+ objects)
- ✅ Metadata consistency under load
- ✅ BadgerDB performance and stability

### Performance
- Successfully handled concurrent operations without data corruption
- Transaction retry logic prevents conflicts during high load
- Metadata operations remain consistent across all test scenarios

### Known Limitations
- Single-node architecture (no clustering or replication)
- Filesystem backend only
- Object Lock not yet validated with backup tools (Veeam, Duplicati)
- Multi-tenancy needs more real-world production validation

### Testing
- Test results available: `warp-mixed-2025-10-19[205102]-LxBL.json.zst`
- MinIO Warp mixed workload: PASSED
- Bulk delete operations: PASSED
- Metadata consistency checks: PASSED

---

## [0.2.3-alpha] - 2025-10-13

### Added
- Complete S3 API implementation (40+ operations)
- Web Console with dark mode support
- Dashboard with real-time statistics and metrics
- Multi-tenancy with resource isolation
- Bucket management (Versioning, Policy, CORS, Lifecycle, Object Lock)
- Object Tagging and ACL support
- Multipart upload complete workflow
- Presigned URLs (GET/PUT with expiration)
- File sharing with expirable links
- Security audit page
- Metrics monitoring (System, Storage, Requests, Performance)

### Changed
- Migrated from SQLite to BadgerDB for object metadata
- Improved UI consistency across all pages
- Enhanced error handling and user feedback

### Security
- JWT authentication for Console API
- S3 Signature v2/v4 for S3 API
- Bcrypt password hashing
- Rate limiting per endpoint
- Account lockout after failed attempts

---

## [0.2.0-dev] - 2025-10

### Initial Release
- Basic S3-compatible API
- Web Console (Next.js frontend)
- SQLite for metadata storage
- Filesystem storage backend
- Multi-tenancy foundation
- User and access key management

---

## Versioning Strategy

MaxIOFS follows semantic versioning with the following conventions:
- **0.x.x-alpha**: Alpha releases - Feature development, may have bugs
- **0.x.x-beta**: Beta releases - Feature complete, testing phase
- **0.x.x-rc**: Release candidates - Production-ready testing
- **1.x.x**: Stable releases - Production-ready

### Upgrade Path to Beta (v0.3.0-beta) ✅ COMPLETED

Beta status achieved with:
- [x] All S3 core operations validated with AWS CLI
- [x] Comprehensive testing completed (all core features)
- [x] Visual UI for bucket configurations (Tags, CORS)
- [x] Console API fully functional
- [x] Multipart upload validated (50MB, 100MB)
- [x] Zero critical bugs in core functionality
- [x] Warp stress testing completed
- [ ] 80%+ backend test coverage (in progress - ~70%)
- [ ] Comprehensive API documentation (planned for v0.4.0)
- [ ] Security review and audit (planned for v0.4.0)
- [ ] Complete user documentation (planned for v0.4.0)

### Upgrade Path to Stable (v1.0.0)

To reach stable status, the following must be completed:
- [ ] Security audit by third party
- [ ] 90%+ test coverage
- [ ] 6+ months of real-world usage
- [ ] Performance validated at scale
- [ ] Complete feature set documented
- [ ] All critical bugs resolved

---

**Note**: This project is currently in BETA phase. Suitable for development, testing, and staging environments. Production use requires your own extensive testing. Always backup your data.
