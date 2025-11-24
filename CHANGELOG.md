# Changelog

All notable changes to MaxIOFS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.2-beta] - 2025-11-24

### 🎯 Major Feature Release: Real-Time Notifications & Dynamic Security Settings

This release introduces **Server-Sent Events (SSE) for real-time push notifications** and **dynamic security configuration** that allows administrators to adjust rate limits and security thresholds without server restarts. Additionally, multiple critical bugs were fixed improving overall system stability.

### Added

#### 🔔 **Real-Time Push Notifications (SSE)**

- **Server-Side Events (SSE) System**:
  - Complete SSE notification hub for real-time push notifications to admins
  - NotificationHub manages SSE client connections with automatic cleanup
  - Asynchronous notification broadcasting to all connected admin clients
  - Per-user notification filtering (global admins see all, tenant admins see only their tenant)
  - Connection tracking with client registration/unregistration
  - Graceful handling of client disconnections
  - **Files**: `internal/server/sse_notifications.go` (new file, ~400 lines)

- **User Locked Notifications**:
  - Automatic SSE notifications when users are locked due to failed login attempts
  - Notification includes: user ID, username, tenant ID, timestamp
  - Callback mechanism in auth manager triggers notification on account lockout
  - Properly integrated with existing account lockout system
  - **Files**: `internal/auth/manager.go` (SetUserLockedCallback), `internal/server/server.go` (callback setup)

- **Frontend SSE Integration**:
  - Custom React hook `useNotifications()` for SSE connection management
  - Automatic token detection with periodic checking (every 1 second)
  - SSE connection using fetch API with ReadableStream parsing
  - Proper buffer management for incomplete SSE messages
  - Read/unread state tracking for notifications
  - localStorage persistence (survives page reloads)
  - Limited to last 3 notifications to prevent UI clutter
  - **Files**: `web/frontend/src/hooks/useNotifications.ts` (new file, ~205 lines)

- **Topbar Notification UI**:
  - Bell icon in topbar with unread count badge
  - Dropdown showing last 3 notifications
  - Visual indicators for read/unread state (dot badge on unread)
  - Click notification to navigate to users page and mark as read
  - "Mark all as read" functionality
  - Individual notification clearing
  - Connection status indicator (connected/disconnected)
  - Responsive design with dark mode support
  - **Files**: `web/frontend/src/components/layout/AppLayout.tsx` (notification dropdown section)

- **SSE API Endpoint**:
  - `GET /api/v1/notifications/stream` - SSE endpoint for real-time notifications
  - Requires JWT authentication (Bearer token)
  - Sends "connected" event on successful connection
  - Keeps connection alive with periodic events
  - Proper HTTP headers for SSE (text/event-stream, no-cache)
  - Flusher interface implementation for streaming responses
  - **Files**: `internal/server/console_api.go` (handleNotificationStream)

#### ⚙️ **Dynamic Security Configuration**

- **Configurable Rate Limiting**:
  - `security.ratelimit_login_per_minute` - IP-based rate limiting (default: 5 attempts/minute)
  - Previously hardcoded at 5 attempts per 60 seconds
  - Now dynamically configurable via settings API
  - Changes take effect immediately when settings updated
  - Separate from account lockout mechanism (different use cases)
  - **Files**: `internal/auth/manager.go` (SetSettingsManager method)

- **Configurable Account Lockout**:
  - `security.max_failed_attempts` - Account lockout threshold (default: 5 attempts)
  - `security.lockout_duration` - Lockout duration in seconds (default: 900 = 15 minutes)
  - Previously hardcoded in RecordFailedLogin and LockAccount methods
  - Now read from settings manager dynamically
  - No server restart required to change thresholds
  - **Files**: `internal/auth/manager.go` (RecordFailedLogin, LockAccount methods)

- **Settings Manager Integration**:
  - Added SettingsManager interface to auth manager
  - SetSettingsManager() method for dependency injection
  - Rate limiter recreated on settings manager initialization
  - Proper defaults when settings unavailable
  - Logging of configured values on startup
  - **Files**: `internal/auth/manager.go`, `internal/server/server.go`

### Fixed

#### 🐛 **Critical Bug Fixes**

- **Rate Limiter Double-Counting Bug**:
  - **Issue**: Login blocking occurred at 3 failed attempts instead of configured 5
  - **Root Cause**: `AllowLogin()` was incrementing counter, then `RecordFailedAttempt()` incremented it again = double counting
  - **Solution**: Changed `AllowLogin()` to only CHECK limit (RLock) without incrementing
  - **Impact**: HIGH - Users were being blocked prematurely
  - **Files**: `internal/auth/rate_limiter.go` (lines 41-69)

- **Failed Attempts Counter Not Resetting**:
  - **Issue**: After account lockout, `failed_login_attempts` continued incrementing (5, 6, 7, etc.)
  - **Root Cause**: `LockAccount()` SQL UPDATE didn't reset the counter to 0
  - **Solution**: Added `failed_login_attempts = 0` to UPDATE statement
  - **Impact**: MEDIUM - Counter accumulation could cause confusion
  - **Files**: `internal/auth/sqlite.go` (lines 659-669)

- **Security Page Not Showing Locked Users**:
  - **Issue**: Locked users count always showed 0 even when users were locked
  - **Root Cause**: Frontend used snake_case `locked_until` but API returns camelCase `lockedUntil`
  - **Solution**: Changed filter to use correct camelCase field name
  - **Impact**: MEDIUM - Admins couldn't see locked user count
  - **Files**: `web/frontend/src/pages/security/index.tsx` (line 102)

- **SSE Callback Not Executing**:
  - **Issue**: User locked callback never triggered, `callback_set: false` in logs
  - **Root Cause**: Type assertion failing silently - `UserLockedCallback` type alias didn't match `func(*User)` in interface
  - **Solution**: Added `SetUserLockedCallback` to Manager interface, changed struct field type to `func(*User)` directly
  - **Impact**: HIGH - SSE notifications completely non-functional
  - **Files**: `internal/auth/manager.go` (interface method + struct field type change)

- **Frontend Not Connecting to SSE**:
  - **Issue**: Hook showed `token exists: false` even after login, SSE never connected
  - **Root Cause**: useEffect ran once on mount (before login), never re-executed
  - **Solution**: Added `setInterval` to check token every 1 second + added `token` to useEffect dependency
  - **Impact**: HIGH - SSE notifications never worked for logged-in users
  - **Files**: `web/frontend/src/hooks/useNotifications.ts` (lines 31-44, dependency array line 166)

- **Wrong Token localStorage Key**:
  - **Issue**: Hook checked `'token'` but found nothing
  - **Root Cause**: App stores token as `'auth_token'` not `'token'`
  - **Solution**: Changed `localStorage.getItem('token')` to `localStorage.getItem('auth_token')`
  - **Impact**: HIGH - Token never detected, SSE never connected
  - **Files**: `web/frontend/src/hooks/useNotifications.ts` (line 33)

- **Streaming Unsupported Error**:
  - **Issue**: SSE endpoint returned 500 error "Streaming unsupported"
  - **Root Cause**: `metricsResponseWriter` middleware didn't implement `http.Flusher` interface
  - **Solution**: Added `Flush()` method to `metricsResponseWriter` that delegates to underlying writer
  - **Impact**: HIGH - SSE completely broken until fixed
  - **Files**: `internal/server/console_api.go` (lines 89-105)

### Enhanced

#### 🔒 **Security Improvements**

- **Separation of Rate Limiting Mechanisms**:
  - IP-based rate limiting: Prevents brute force from single IP (typically set higher, e.g., 15)
  - Account lockout: Protects individual accounts (typically set lower, e.g., 5)
  - Different thresholds address different security concerns
  - Especially important for users behind proxies (multiple users share same IP)

- **Real-Time Security Notifications**:
  - Administrators immediately notified when accounts are locked
  - No polling required - push-based notifications
  - Tenant admins only see their tenant's security events
  - Global admins see all security events across all tenants

- **Audit Trail for Security Settings**:
  - All security setting changes logged in audit log
  - Rate limit configuration changes tracked
  - Lockout duration changes tracked
  - Historical record of who changed what and when

### Technical Details

#### **New Files Created**

1. `internal/server/sse_notifications.go` (~400 lines)
   - NotificationHub implementation
   - sseClient connection management
   - Notification struct and filtering logic
   - handleNotificationStream HTTP handler

2. `web/frontend/src/hooks/useNotifications.ts` (~205 lines)
   - useNotifications React hook
   - SSE connection with ReadableStream
   - Token detection with periodic checking
   - Notification state management (read/unread, localStorage)

#### **Files Modified**

1. `internal/auth/rate_limiter.go` (lines 41-69)
   - `AllowLogin()` now only checks limit without incrementing

2. `internal/auth/sqlite.go` (lines 659-669)
   - `LockAccount()` resets `failed_login_attempts = 0`

3. `internal/auth/manager.go` (multiple sections)
   - Added `settingsManager` field and `SettingsManager` interface
   - Added `SetSettingsManager()` method with rate limiter recreation
   - Changed `userLockedCallback` field type to `func(*User)`
   - Updated `RecordFailedLogin()` to read settings dynamically
   - Updated `LockAccount()` to read lockout duration from settings

4. `internal/server/server.go` (lines 110-121, 213-234)
   - Connected settings manager to auth manager
   - Set up user locked callback for SSE notifications
   - Added notification hub initialization

5. `internal/server/console_api.go` (lines 89-105, 240)
   - Added `Flush()` method to `metricsResponseWriter`
   - Registered `/notifications/stream` SSE endpoint

6. `web/frontend/src/components/layout/AppLayout.tsx` (notification section)
   - Replaced polling with `useNotifications` hook
   - Updated UI to show unread count and read/unread state
   - Added mark as read functionality

7. `web/frontend/src/pages/security/index.tsx` (line 102)
   - Fixed `locked_until` to `lockedUntil` (camelCase)

#### **SSE Message Format**

```
data: {"type":"user_locked","message":"User john has been locked due to failed login attempts","data":{"userId":"user-123","username":"john","tenantId":"tenant-abc"},"timestamp":1732435200,"tenantId":"tenant-abc"}

```

#### **Settings Configuration Example**

```yaml
# These values are now in the database (system_settings table)
# Can be changed via Web Console without restart

security:
  ratelimit_login_per_minute: 15  # Higher for users behind proxies
  max_failed_attempts: 5          # Account lockout threshold
  lockout_duration: 900           # 15 minutes in seconds
```

### Deployment

#### **Upgrading from v0.4.2-beta**

**Zero downtime upgrade - Fully backward compatible:**

1. Stop MaxIOFS server
2. Replace binary with v0.4.3-beta
3. Start MaxIOFS server
4. SSE notifications automatically available
5. Security settings remain at defaults
6. Adjust security settings via Web Console if needed
7. No configuration changes required

**New Features Available**:
- Real-time notifications appear in topbar bell icon (admin users only)
- Security settings configurable at `/settings` page (global admin only)
- Rate limits and lockout thresholds adjustable without restart

### Breaking Changes

**None** - This release is fully backward compatible with v0.4.2-beta

### Performance Impact

**SSE Notifications**:
- Minimal overhead: ~1KB per connected client
- Idle connections kept alive with periodic keepalive
- Automatic cleanup of disconnected clients
- No performance impact on S3 operations

**Dynamic Settings**:
- Settings read from database on each check (cached in auth manager)
- Negligible performance impact (<1ms per auth operation)
- No additional database queries for normal operations

### Security Considerations

**SSE Notifications**:
- Requires JWT authentication (same as Console API)
- Tenant isolation enforced (tenant admins see only their tenant)
- No sensitive data in notifications (only user ID, username, event type)
- Connection automatically closed on token expiration

**Dynamic Security Settings**:
- Only global admins can modify security settings
- All changes logged in audit log with user ID and timestamp
- Changes take effect immediately (no restart required)
- Invalid values rejected with validation errors

### Validated with Web Console

**All features tested on November 24, 2025:**

**SSE Notifications**:
- ✅ User locked notification appears in topbar
- ✅ Unread count badge shows correct number
- ✅ Click notification navigates to users page
- ✅ Mark as read functionality working
- ✅ Mark all as read functionality working
- ✅ Notification persists across page reloads (localStorage)
- ✅ Limited to last 3 notifications as designed
- ✅ Connection status indicator accurate
- ✅ Dark mode UI consistent and readable

**Dynamic Security Settings**:
- ✅ Settings page shows all security settings
- ✅ Rate limit changes apply immediately
- ✅ Account lockout thresholds adjustable
- ✅ Lockout duration configurable
- ✅ Changes persist across server restarts
- ✅ Audit log captures all setting changes

**Bug Fixes Verified**:
- ✅ Rate limiter now blocks at 5 attempts (not 3)
- ✅ Failed attempts counter resets after lockout
- ✅ Security page shows locked user count correctly
- ✅ SSE notifications work immediately after login
- ✅ All SSE-related bugs resolved

### What's Next (v0.5.0)

Planned features for future releases:
- ⏳ Advanced notification types (quota warnings, system events)
- ⏳ Email notifications via SMTP
- ⏳ Webhook notifications for custom integrations
- ⏳ Notification preferences per user
- ⏳ Performance profiling and optimization
- ⏳ CI/CD pipeline (GitHub Actions)

---

## [0.4.2-beta] - 2025-11-23

### 🎯 Major Feature Release: S3 Compatibility Improvements & Bucket Notifications

This release focuses on **AWS S3 compatibility** improvements, implementing **global bucket uniqueness** (AWS S3 standard), **S3-compatible URLs** without tenant prefixes, and **bucket notifications (webhooks)** for object events. These changes significantly improve compatibility with standard S3 clients and tools.

### Added

#### 🌐 **Global Bucket Uniqueness - AWS S3 Compatible**

- **Global Bucket Name Validation**:
  - Bucket names are now globally unique across all tenants (matching AWS S3 behavior)
  - Prevents bucket name conflicts between different tenants
  - Validation layer added during bucket creation
  - Scans all existing buckets to enforce uniqueness
  - Returns proper S3 error (BucketAlreadyExists) when name is taken
  - **Files**: `internal/metadata/badger.go` (lines 154-170)

- **Backward Compatible Implementation**:
  - Database schema unchanged (preserves existing data)
  - Bucket keys remain as `bucket:{tenantID}:{name}` in BadgerDB
  - No data migration required
  - Existing buckets continue to work without modification
  - **Impact**: Zero downtime upgrade path

- **Automatic Tenant Resolution**:
  - New `GetBucketByName()` function for global bucket lookup
  - Backend automatically resolves bucket's tenant from bucket name
  - Transparent to S3 clients (no API changes)
  - Efficient metadata scanning with early exit on match
  - **Files**: `internal/metadata/badger.go` (lines 327-357), `internal/metadata/store.go` (lines 40-41)

#### 🔗 **S3-Compatible URLs - Standard URL Format**

- **Presigned URLs Without Tenant Prefix**:
  - Presigned URLs now use standard S3 format: `/bucket/object?signature=...`
  - Previously: `/tenant-id/bucket/object?signature=...` (non-standard)
  - Better compatibility with S3 clients and CDNs
  - Simplified URL structure
  - **Files**: `internal/presigned/generator.go` (line 75)

- **Share URLs Without Tenant Prefix**:
  - Share URLs follow same standard format: `/bucket/object`
  - Consistent URL format across all sharing mechanisms
  - Database-backed share system continues to persist URLs
  - **Files**: `internal/server/console_api.go` (lines 1324-1345)

- **Automatic Path Resolution**:
  - Updated `getBucketPath()` to resolve tenant automatically
  - First tries authenticated user's tenant
  - Falls back to bucket lookup for presigned URLs
  - Returns proper tenant-scoped path for storage backend
  - **Files**: `pkg/s3compat/handler.go` (lines 2101-2117)

#### 📢 **Bucket Notifications (Webhooks) - AWS S3 Compatible**

- **Event Notification System**:
  - AWS S3 compatible event format (EventVersion 2.1)
  - Supported event types:
    - `s3:ObjectCreated:*` (Put, Post, Copy, CompleteMultipartUpload)
    - `s3:ObjectRemoved:*` (Delete, DeleteMarkerCreated)
    - `s3:ObjectRestored:Post`
  - Wildcard event matching (e.g., `s3:ObjectCreated:*` matches all create events)
  - **Files**: Notification system implementation across multiple files

- **Webhook Delivery with Retry**:
  - HTTP POST to configured webhook endpoints
  - Retry mechanism: 3 attempts with 2-second delay
  - Custom HTTP headers support per notification rule
  - Timeout handling and error logging
  - Graceful failure (doesn't block S3 operations)

- **Per-Rule Filtering**:
  - Prefix filters (e.g., trigger only for objects in `logs/` folder)
  - Suffix filters (e.g., trigger only for `.jpg` files)
  - Combine prefix and suffix for precise targeting
  - Filter validation and sanitization

- **Web Console Integration**:
  - Tab-based bucket settings UI
  - "Notifications" tab for webhook management
  - Add/Edit/Delete notification rules via modal
  - Enable/disable rules without deletion
  - Visual configuration interface (no JSON/XML editing required)
  - Real-time validation and error feedback

- **Storage and Performance**:
  - Configuration stored in BadgerDB
  - In-memory caching for fast lookup
  - Multi-tenant support with global admin access
  - Full audit logging for all configuration changes

### Enhanced

#### 🔧 **Frontend Improvements**

- **Presigned URL Modal State Management**:
  - Fixed bug where modal showed previous object's URL when switching objects
  - Added `key={selectedObjectKey}` prop to force React component remount
  - Added `useEffect` hook to reset state when object/bucket changes
  - Improved user experience when generating multiple presigned URLs
  - **Files**: `web/frontend/src/components/PresignedURLModal.tsx` (lines 30-36), `web/frontend/src/pages/buckets/[bucket]/index.tsx` (line 1443)

- **React Component Lifecycle**:
  - Proper cleanup of component state on unmount
  - Better handling of prop changes
  - Prevents stale data display in modals

#### 📊 **Multi-Tenancy Architecture Updates**

- **Documentation Updates**:
  - Updated MULTI_TENANCY.md with global uniqueness explanation
  - Added examples showing naming conventions to avoid conflicts
  - Clarified tenant isolation (data isolation maintained despite global names)
  - Updated database schema documentation

### Fixed

- **Presigned URL Modal Bug**:
  - Issue: When generating presigned URL for object A, then opening modal for object B, it showed object A's URL
  - Root cause: React state not resetting when switching between objects
  - Solution: Force component remount with `key` prop + `useEffect` cleanup
  - **Impact**: Medium - Confusing UX when sharing multiple objects

### Technical Details

#### **Database Schema Changes**

**None** - Fully backward compatible:
```
Before (v0.4.1-beta):
bucket:tenant-abc123:my-bucket  → {metadata}
bucket:tenant-xyz789:my-bucket  → {metadata} ✅ Allowed (duplicate names)

After (v0.4.2-beta):
bucket:tenant-abc123:my-bucket  → {metadata}
bucket:tenant-xyz789:my-bucket  → ❌ Rejected (global uniqueness enforced)
bucket:tenant-xyz789:xyz-bucket → {metadata} ✅ Allowed (unique name)

Storage format unchanged - validation layer added
```

#### **New Functions Added**

1. **`GetBucketByName(ctx, name)`** - Global bucket lookup
   ```go
   // internal/metadata/badger.go (lines 327-357)
   func (s *BadgerStore) GetBucketByName(ctx context.Context, name string) (*BucketMetadata, error) {
       // Scans all buckets (prefix "bucket:")
       // Returns first match by bucket name
       // Used for tenant resolution in presigned URLs
   }
   ```

2. **Updated `getBucketPath()`** - Automatic tenant resolution
   ```go
   // pkg/s3compat/handler.go (lines 2101-2117)
   func (h *Handler) getBucketPath(r *http.Request, bucketName string) string {
       // Try authenticated user's tenant first
       // Fall back to GetBucketByName() for presigned URLs
       // Returns: "tenant-id/bucket" or "bucket" (for global)
   }
   ```

#### **Files Modified for S3 Compatibility**

1. `internal/metadata/badger.go` - Global uniqueness validation + GetBucketByName()
2. `internal/metadata/store.go` - Interface definition for GetBucketByName()
3. `internal/presigned/generator.go` - Removed tenant prefix from URLs
4. `pkg/s3compat/handler.go` - Automatic tenant resolution + interface update
5. `internal/server/console_api.go` - Share URLs without tenant prefix
6. `web/frontend/src/components/PresignedURLModal.tsx` - State management fix
7. `web/frontend/src/pages/buckets/[bucket]/index.tsx` - Key prop for modal

#### **Files Modified for Bucket Notifications**

- Multiple files across backend for notification system implementation
- Frontend bucket settings UI with notifications tab
- BadgerDB integration for notification configuration storage

### Deployment

#### **Upgrading from v0.4.1-beta**

**Zero downtime upgrade - Fully backward compatible:**

1. Stop MaxIOFS server
2. Replace binary with v0.4.2-beta
3. Start MaxIOFS server
4. All existing buckets remain accessible
5. New buckets will enforce global uniqueness
6. Presigned URLs automatically use new format
7. No configuration changes required

**Behavior Changes:**
- **Bucket Creation**: Global uniqueness now enforced
  - Tenant A creates "backups" → ✅ Success
  - Tenant B creates "backups" → ❌ BucketAlreadyExists error
  - **Recommendation**: Use tenant-prefixed names (e.g., "acme-backups", "xyz-backups")
- **Presigned URLs**: URLs no longer contain `/tenant-id/` prefix
  - Old format still works for existing URLs (backward compatible)
  - New URLs follow standard S3 format
- **Share URLs**: Follow same format as presigned URLs

#### **Configuration for Bucket Notifications**

No additional configuration required - notifications are configured per-bucket via Web Console:

1. Navigate to Bucket Settings → Notifications tab
2. Click "Add Notification Rule"
3. Configure:
   - Event types (ObjectCreated, ObjectRemoved, ObjectRestored)
   - Webhook URL (HTTP/HTTPS endpoint)
   - Prefix/suffix filters (optional)
   - Custom headers (optional)
4. Save configuration
5. Events automatically trigger webhook deliveries

### Breaking Changes

**None** - This release is fully backward compatible with v0.4.1-beta

**Non-Breaking Behavior Changes:**
- Bucket names now globally unique (prevents future conflicts)
- URL format standardized (improves S3 compatibility)
- Both changes align with AWS S3 standards

### Security Considerations

#### **Bucket Enumeration**

- Global bucket uniqueness allows bucket name enumeration
- Mitigation: Use non-obvious bucket names (avoid common names like "backups")
- Tenant isolation still enforced (users can't see/access other tenants' buckets)

#### **Webhook Security**

- Webhooks send HTTP POST to configured URLs
- **Important**: Validate webhook sources in receiving application
- Consider using HTTPS endpoints for webhook URLs
- Custom headers can include authentication tokens
- Failed deliveries logged in audit logs

### Validated with AWS CLI

**All operations tested on November 23, 2025:**

**Bucket Creation with Global Uniqueness**:
- ✅ Tenant A: `aws s3 mb s3://test-bucket` → Success
- ✅ Tenant B: `aws s3 mb s3://test-bucket` → BucketAlreadyExists error
- ✅ Tenant B: `aws s3 mb s3://test-bucket-2` → Success (different name)

**Presigned URLs**:
- ✅ Generated via Console UI
- ✅ URLs work in browser (no authentication required)
- ✅ URLs expire correctly after configured time
- ✅ Standard S3 format: `http://endpoint/bucket/object?signature=...`

**Share URLs**:
- ✅ Created via Console UI
- ✅ Persist in database across restarts
- ✅ Revocable via Console UI
- ✅ Standard S3 format: `http://endpoint/bucket/object`

**S3 Operations with Global Buckets**:
- ✅ All S3 operations work correctly
- ✅ Multi-tenant isolation maintained
- ✅ No performance regression
- ✅ Automatic tenant resolution transparent to clients

### Performance Impact

**Global Uniqueness Validation**:
- Adds bucket metadata scan during CreateBucket
- Performance: O(n) where n = total bucket count
- Impact: Minimal for typical deployments (<1000 buckets)
- Optimization: Early exit on first match

**Presigned URL Generation**:
- Additional `GetBucketByName()` lookup for presigned URLs
- Cached in typical S3 client usage patterns
- Impact: <10ms per request

**Webhook Delivery**:
- Asynchronous (doesn't block S3 operations)
- Retry logic runs in background goroutine
- Impact: Zero on S3 API performance

### What's Next (v0.5.0)

Planned features for future releases:
- ⏳ Performance profiling and optimization
- ⏳ CI/CD pipeline (GitHub Actions)
- ⏳ Encryption key rotation with dual-key support
- ⏳ Per-tenant encryption keys for multi-tenancy isolation
- ⏳ HSM integration for production key management
- ⏳ Official Docker images on Docker Hub

---

## [0.4.1-beta] - 2025-11-18

### 🎯 Major Feature Release: Server-Side Encryption (SSE) with Persistent Keys

This release introduces **comprehensive server-side encryption at rest** with AES-256-CTR streaming encryption, persistent master key storage, and flexible encryption control. The system supports unlimited file sizes with constant memory usage, mixed encrypted/unencrypted object coexistence, and full Web Console integration.

### Added

#### 🔐 **Complete Server-Side Encryption System (SSE)**

- **Persistent Master Key Storage**:
  - Master key stored in `config.yaml` (64 hexadecimal characters = 32 bytes for AES-256)
  - Key survives server restarts - no more data loss on reboot
  - Key loaded at startup regardless of `enable_encryption` setting
  - Automatic validation: key length (64 chars), format (hex), conversion (hex→bytes)
  - Critical security warnings in configuration documentation
  - **Files**: `internal/object/manager.go` (lines 125-165), `config.example.yaml` (lines 213-245)

- **Streaming Encryption (AES-256-CTR)**:
  - Counter mode (CTR) encryption with constant memory usage (~32KB buffers)
  - Supports files of ANY size (tested: 1KB to 100MB+)
  - Zero performance impact - tested at 150+ MiB/s for 100MB files
  - 256-bit encryption strength (industry standard)
  - Automatic initialization vector (IV) generation per object
  - **Files**: `internal/encryption/stream.go`, `internal/encryption/manager.go`

- **Flexible Dual-Level Encryption Control**:
  - **Server-Level Control**: Global `enable_encryption` flag in config.yaml
    - `true`: New objects CAN be encrypted (if bucket also enabled)
    - `false`: New objects will NOT be encrypted
  - **Bucket-Level Control**: Per-bucket encryption setting in Web Console
    - Users choose encryption when creating buckets
    - Setting stored in bucket metadata (`Encryption.Rules[0].ApplyServerSideEncryptionByDefault`)
  - **Decision Logic**: Encrypt ONLY if BOTH server AND bucket encryption enabled
  - **Files**: `internal/object/manager.go` (lines 410-463, 1768-1851)

- **Automatic Decryption on GetObject**:
  - Objects tagged with `encrypted=true` in metadata automatically decrypted
  - Decryption happens ALWAYS if master key exists (regardless of `enable_encryption` setting)
  - Allows disabling encryption for NEW uploads while keeping OLD encrypted files accessible
  - Transparent to S3 clients - no API changes required
  - **Files**: `internal/object/manager.go` (lines 300-331)

- **Backward Compatibility**:
  - Mixed encrypted/unencrypted objects coexist in same bucket
  - Non-encrypted objects returned as-is (no performance penalty)
  - Metadata-based detection (`encrypted=true` flag)
  - Seamless migration path from unencrypted to encrypted deployments

- **Web Console Integration**:
  - **Server Encryption Status Query**: Frontend queries `/api/v1/config` to check if server has encryption enabled
  - **Conditional Bucket Encryption UI**:
    - Checkbox enabled if server has `encryption_key` configured
    - Checkbox disabled with warning message if no master key
    - Warning: "Server Encryption Disabled - To enable encryption, configure encryption_key in config.yaml and restart"
  - **Visual Indicators**: Alert icons and amber warning boxes when encryption unavailable
  - **Files**: `web/frontend/src/pages/buckets/create.tsx` (lines 89-96, 660-710)

#### 📋 **Configuration Management Improvements**

- **Separate Configuration Storage**:
  - System configuration now stored in SQLite database (`system_settings` table)
  - Configuration persists across server restarts
  - Web Console settings (session timeout, max failed attempts, object lock defaults, etc.) saved to database
  - Migration from file-based config to database-backed config
  - Settings manager with categorized configuration (Security, Storage, ObjectLock, System)
  - **Files**: `internal/settings/manager.go`, `internal/settings/types.go`
  - **Commit**: `ed6c416` - "Separate config file and now mos config are stored on DB"

- **Enhanced Configuration Documentation**:
  - `config.example.yaml` updated with comprehensive encryption documentation
  - Critical security warnings about key backup and data loss
  - Step-by-step setup instructions (generate key, configure, restart)
  - Behavior explanation for `enable_encryption` flag
  - Examples for all encryption scenarios
  - **File**: `config.example.yaml` (lines 213-245)

#### 🎨 **UI/UX Improvements**

- **Unified Card Design Across All Pages**:
  - Consistent card styling throughout Web Console
  - All dashboard cards, metrics cards, and info cards use same design system
  - Improved visual hierarchy and spacing
  - **Commit**: `bba08a2` - "Fix cards in all pages, now use the same style"

- **Settings Page Enhancements**:
  - Fixed bugs in bucket settings display
  - Improved encryption configuration UI
  - Better visual feedback for enabled/disabled features
  - **Commit**: `3062a7e` - "Fix settings page"

#### 📊 **Metrics System Migration**

- **BadgerDB for Metrics Historical Storage**:
  - Migrated metrics historical data from in-memory to BadgerDB
  - Metrics snapshots now persist across server restarts
  - Historical metrics data preserved for dashboard charts and analytics
  - Improved metrics page performance and accuracy
  - Fixed range calculation bugs in metrics dashboard
  - **Files**: `internal/metrics/manager.go`
  - **Commits**: `9be6ce9` - "Now all metrics are stored in BadgerDB", `ca63e0d` - "Fix bug in metrics page in ranges"

### Fixed

#### 🐛 **Critical Security & Permission Fixes**

- **Tenant Menu Visibility Bug**:
  - Fixed bug where normal users could see Tenant management menu (should be global admin only)
  - Proper role-based access control enforced in frontend
  - **Commit**: `e1097c1` - "Fix bug normal users see Tenant menu"

- **Global Admin Privilege Escalation**:
  - Fixed critical bug where global users could access admin privileges incorrectly
  - Proper permission checks now enforced throughout backend
  - **Commit**: `4263a39` - "Fix bug global users now can't access to admin priviledges"

- **Password Change Detection**:
  - Fixed bug where backend didn't detect when user was changing their own vs another user's password
  - Proper validation now distinguishes between self-password-change and admin-changing-user-password
  - **Commit**: `9d7165c` - "Fix bug backend now detect when user un changing other or their password"

- **Frontend Permission Checks**:
  - Fixed permission validation in frontend components
  - Users now see only operations they're authorized to perform
  - **Commit**: `3062a7e` - "Fix permission on frontend"

#### 🔧 **Data Integrity & Validation Fixes**

- **Non-Existent Bucket Upload Prevention**:
  - Fixed critical bug where users could upload files to buckets that don't exist
  - Proper bucket existence validation before object upload
  - Returns proper S3 error code (NoSuchBucket) when bucket not found
  - **Commit**: `3062a7e` - "Fix bug users can upload file to no existent bucket"

- **Small Object Encryption**:
  - Fixed encryption handling for objects smaller than buffer size
  - Objects <32KB now properly encrypted with streaming logic
  - **Commit**: `3062a7e` - "Added encryption to objects smaller"

### Enhanced

#### 🔒 **Security Enhancements**

- **Encryption Key Management**:
  - Master key validation on startup (prevents invalid key formats)
  - Fatal error if key is malformed (prevents silent data corruption)
  - Hex string to byte array conversion with error handling
  - Key stored securely in encryption service's KeyManager
  - **Impact**: Prevents accidental data corruption from misconfigured keys

- **Configuration Security**:
  - Critical warnings about NEVER committing keys to version control
  - Backup recommendations (password managers, vaults, encrypted storage)
  - Data loss warnings prominently displayed in documentation
  - **File**: `config.example.yaml` (lines 213-245)

#### ⚡ **Performance & Architecture**

- **Streaming Encryption Performance**:
  - Constant memory usage regardless of file size (tested: 1KB to 100MB+)
  - No performance regression on unencrypted objects (0% overhead)
  - Encryption overhead: <5% CPU for 100MB files at 150+ MiB/s
  - Parallel PutObject operations maintain high throughput

- **Metadata-Based Encryption Detection**:
  - Fast detection via object metadata (no file reading required)
  - `encrypted=true` flag set during PutObject/CompleteMultipartUpload
  - O(1) lookup for encryption status

- **Build System Improvements**:
  - Release artifacts no longer compressed unnecessarily
  - Faster distribution of binaries
  - **Commit**: `ddfafe8` - "Releases are not compressed"

### Technical Details

#### **New Encryption Implementation Flow**

**Master Key Loading (Startup)**:
```go
// internal/object/manager.go (lines 125-165)
if config.EncryptionKey != "" {
    // Validate key length (64 hex chars = 32 bytes)
    if len(config.EncryptionKey) != 64 {
        logrus.Fatalf("Invalid encryption_key length: got %d characters, expected 64")
    }

    // Convert hex to bytes
    keyBytes := make([]byte, 32)
    _, err := fmt.Sscanf(config.EncryptionKey, "%64x", &keyBytes)
    if err != nil {
        logrus.WithError(err).Fatal("Invalid encryption_key format")
    }

    // Store master key
    encryptionService.GetKeyManager().StoreKey("default", keyBytes)

    // Log encryption status
    if config.EnableEncryption {
        logrus.Info("✅ Encryption enabled: New objects will be encrypted")
    } else {
        logrus.Info("⚠️ Encryption disabled for new objects (existing encrypted objects remain accessible)")
    }
}
```

**PutObject Conditional Encryption**:
```go
// internal/object/manager.go (lines 410-463)
shouldEncrypt := false
if om.config.EnableEncryption {
    bucketInfo, err := om.metadataStore.GetBucket(ctx, tenantID, bucketName)
    if err == nil && bucketInfo != nil && bucketInfo.Encryption != nil {
        if len(bucketInfo.Encryption.Rules) > 0 {
            sseConfig := bucketInfo.Encryption.Rules[0].ApplyServerSideEncryptionByDefault
            if sseConfig != nil && sseConfig.SSEAlgorithm != "" {
                shouldEncrypt = true
            }
        }
    }
}

if shouldEncrypt {
    // Encrypt stream before storing
    pipeReader, pipeWriter := io.Pipe()
    encryptionMeta := &encryption.EncryptionMetadata{Algorithm: "AES-256-GCM"}

    go func() {
        defer pipeWriter.Close()
        om.encryptionService.EncryptStream(body, pipeWriter, encryptionMeta)
    }()

    // Store encrypted stream
    storageMetadata["encrypted"] = "true"
    om.storageBackend.PutObject(ctx, pipeReader, ...)
} else {
    // Store unencrypted
    om.storageBackend.PutObject(ctx, body, ...)
}
```

**GetObject Automatic Decryption**:
```go
// internal/object/manager.go (lines 300-331)
isEncrypted := storageMetadata["encrypted"] == "true"

if isEncrypted {
    // Decrypt stream on-the-fly
    pipeReader, pipeWriter := io.Pipe()
    encryptionMeta := &encryption.EncryptionMetadata{Algorithm: "AES-256-GCM"}

    go func() {
        defer pipeWriter.Close()
        defer encryptedReader.Close()
        om.encryptionService.DecryptStream(encryptedReader, pipeWriter, encryptionMeta)
    }()

    return object, pipeReader, nil
} else {
    // Return unencrypted stream as-is
    return object, encryptedReader, nil
}
```

#### **Files Modified for Encryption**

1. `internal/object/manager.go` - Core encryption logic (lines 125-165, 300-331, 410-463, 1768-1851)
2. `internal/bucket/manager_badger.go` - Removed default encryption config (line 88)
3. `web/frontend/src/pages/buckets/create.tsx` - Frontend encryption UI (lines 89-96, 660-710)
4. `config.example.yaml` - Encryption documentation (lines 213-245)
5. `internal/config/config.go` - Added `EncryptionKey` field to Config struct
6. `internal/encryption/stream.go` - Streaming encryption implementation (NEW)
7. `internal/encryption/manager.go` - Encryption service and key manager (NEW)

#### **Configuration Format**

```yaml
# config.yaml
storage:
  # Enable/disable encryption for NEW object uploads
  # NOTE: Existing encrypted objects ALWAYS remain accessible if encryption_key is set
  enable_encryption: true

  # Master Encryption Key (AES-256)
  # ⚠️ CRITICAL: Must be EXACTLY 64 hexadecimal characters (32 bytes)
  # Generate with: openssl rand -hex 32
  # ⚠️ DATA LOSS WARNING: If you LOSE this key, encrypted data is PERMANENTLY UNRECOVERABLE
  encryption_key: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
```

### Deployment

**Enabling Encryption** (New Installations):
1. Generate master key: `openssl rand -hex 32`
2. Add to `config.yaml`: `encryption_key: "<64-char-hex-string>"`
3. Set `enable_encryption: true` (optional - default: false)
4. Start MaxIOFS
5. Create buckets with encryption enabled via Web Console

**Upgrading from v0.4.0-beta**:
- Existing unencrypted objects remain accessible
- Add `encryption_key` to config.yaml to enable encryption
- New buckets can choose encryption on/off
- No data migration required
- Mixed encrypted/unencrypted objects supported

**Disabling Encryption for New Uploads** (While Keeping Old Files Accessible):
1. Set `enable_encryption: false` in config.yaml
2. Keep `encryption_key` configured (DO NOT remove!)
3. Restart MaxIOFS
4. New uploads will NOT be encrypted
5. Old encrypted objects still decrypt automatically

### Breaking Changes

None - This release is fully backward compatible with v0.4.0-beta

### Upgrade Notes

- Encryption is **disabled by default** (must explicitly configure `encryption_key` and set `enable_encryption: true`)
- Server can run with only `--data-dir` flag (no config.yaml required)
- If no `encryption_key` is configured, bucket encryption checkbox is disabled in Web Console
- Existing unencrypted objects remain unencrypted (no automatic re-encryption)
- To enable encryption: Add `encryption_key` to config.yaml, restart server, create new buckets with encryption enabled
- **CRITICAL**: Backup your `encryption_key` securely - losing it means PERMANENT data loss for encrypted objects

### Security Considerations

**Encryption Key Backup**:
- ✅ Store master key in password manager (1Password, LastPass, Bitwarden)
- ✅ Use encrypted vault or HSM for production deployments
- ✅ NEVER commit `config.yaml` with real keys to version control
- ✅ Consider key rotation strategy for long-term deployments (currently manual)

**Data at Rest**:
- ✅ AES-256-CTR encryption meets industry standards (NIST, FIPS 140-2)
- ✅ Unique IV per object prevents pattern analysis
- ✅ Streaming encryption prevents memory exhaustion attacks
- ✅ Master key never stored in metadata or logs

**Access Control**:
- ✅ Encryption transparent to authorized S3 clients
- ✅ Unauthorized access to filesystem gets encrypted data (useless without key)
- ✅ Decryption only possible with valid master key

### Known Limitations

- **Key Rotation**: Manual process - changing key makes old objects unreadable (planned for v0.5.0)
- **Encryption Algorithm**: Fixed to AES-256-CTR (no configurable cipher selection)
- **Key Management**: Single master key (no per-tenant or per-bucket keys)
- **Metadata Encryption**: Object metadata stored unencrypted (only object data encrypted)

### Performance Benchmarks

**Encryption Performance** (Tested on Windows 11, Go 1.21):
- **1MB file**: ~200 MiB/s encryption, ~210 MiB/s decryption
- **10MB file**: ~180 MiB/s encryption, ~190 MiB/s decryption
- **100MB file**: ~150 MiB/s encryption, ~160 MiB/s decryption
- **Memory usage**: Constant ~32KB buffer (streaming encryption)
- **CPU overhead**: <5% for encryption/decryption operations

**S3 Compatibility**: 98%+ ✅
- All encryption operations transparent to S3 clients
- No API changes required
- Works with AWS CLI, SDKs, and third-party tools

### What's Next (v0.5.0)

Planned features for future releases:
- ⏳ Automatic key rotation with dual-key support
- ⏳ Per-tenant encryption keys for multi-tenancy isolation
- ⏳ Hardware Security Module (HSM) integration
- ⏳ Metadata encryption (currently only object data encrypted)
- ⏳ Encryption algorithm selection (ChaCha20-Poly1305, AES-GCM)
- ⏳ Compliance reporting (encryption coverage, key usage)

---

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
