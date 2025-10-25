# Changelog

All notable changes to MaxIOFS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

### Upgrade Path to Beta (v0.3.0-beta)

To reach beta status, the following must be completed:
- [ ] 80%+ backend test coverage
- [ ] Comprehensive API documentation
- [ ] All S3 operations validated with AWS CLI
- [ ] Security review and audit
- [ ] User documentation complete
- [x] Warp stress testing completed

### Upgrade Path to Stable (v1.0.0)

To reach stable status, the following must be completed:
- [ ] Security audit by third party
- [ ] 90%+ test coverage
- [ ] 6+ months of real-world usage
- [ ] Performance validated at scale
- [ ] Complete feature set documented
- [ ] All critical bugs resolved

---

**Note**: This project is currently in ALPHA phase. Use for testing and development only.
