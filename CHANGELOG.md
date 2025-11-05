# Changelog

All notable changes to MaxIOFS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
