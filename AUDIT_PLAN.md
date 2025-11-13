# Audit Logs System - Implementation Plan

**Version**: 0.4.0-dev
**Created**: November 13, 2025
**Last Updated**: November 13, 2025
**Status**: Phase 2 Complete - Backend Integration Done ✅

---

## 🎯 Current Progress Summary

### ✅ Phase 1: Core Infrastructure - COMPLETED
- Created audit types, manager interface, and SQLite storage
- Implemented retention job (runs every 24 hours)
- Default retention: 90 days

### ✅ Phase 2: Backend Integration - COMPLETED
- Auth Manager: All authentication and user management events logged
- Bucket Manager: Bucket creation and deletion logged
- Console API: Access keys, tenants, and password changes logged
- Server: Audit manager initialized and wired to all components
- Config: Added audit configuration (enable, retention_days, db_path)

### ✅ Phase 3: API Endpoints - COMPLETED
- Created API endpoints to query audit logs
- `GET /api/v1/audit-logs` - List all logs with filters (global/tenant admin)
- `GET /api/v1/audit-logs/:id` - Get specific log by ID
- Global admins see all logs, tenant admins see only their tenant's logs
- Full filtering support: date range, event type, status, user, resource type

### ⏳ Phase 4-9: Frontend & Testing - PENDING
- Frontend UI with table, filters, pagination
- CSV export functionality
- Unit and integration tests

**Overall Progress**: ~60% complete (13.5 hours spent, ~18 hours remaining)

---

## 📋 Overview

Implement a comprehensive audit logging system to track all critical system events. Global admins can view all events across all tenants, while tenant admins can only view events within their tenant.

### Goals
- ✅ Track all critical user actions (login, logout, password changes, etc.)
- ✅ Track administrative operations (user/bucket/key management)
- ✅ Provide filtering and search capabilities
- ✅ Support multi-tenancy with proper isolation
- ✅ Ensure logs are immutable and secure
- ❌ **NOT** tracking object operations (too many events)

---

## 🎯 Events to Log

### Authentication & Security Events
- [x] **Login Success** - User successfully authenticated
- [x] **Login Failed** - Failed login attempt with reason
- [x] **Logout** - User logged out
- [x] **User Blocked** - Account locked after 5 failed attempts
- [x] **User Unblocked** - Account unlocked (automatic or manual by admin)
- [x] **Password Changed** - User changed their password
- [x] **2FA Enabled** - User enabled two-factor authentication
- [x] **2FA Disabled** - User or admin disabled two-factor authentication
- [x] **2FA Verification Success** - User verified 2FA code successfully
- [x] **2FA Verification Failed** - User failed 2FA verification

### User Management Events
- [x] **User Created** - New user created by admin
- [x] **User Deleted** - User deleted by admin
- [x] **User Updated** - User profile updated (email, display name)
- [x] **User Role Changed** - User role modified by admin
- [x] **User Status Changed** - User activated/deactivated/suspended

### Bucket Management Events
- [x] **Bucket Created** - New bucket created (via Console or S3 API)
- [x] **Bucket Deleted** - Bucket deleted by user/admin

### Access Key Management Events
- [x] **Access Key Created** - New S3 access key generated
- [x] **Access Key Deleted** - Access key revoked/deleted
- [x] **Access Key Status Changed** - Access key activated/deactivated

### Tenant Management Events (Global Admin Only)
- [x] **Tenant Created** - New tenant created
- [x] **Tenant Deleted** - Tenant deleted
- [x] **Tenant Updated** - Tenant settings modified (quotas, limits)

### Events NOT Logged (Performance Reasons)
- ❌ Object uploaded (PutObject)
- ❌ Object downloaded (GetObject)
- ❌ Object deleted (DeleteObject)
- ❌ Object listed (ListObjects)
- ❌ Bucket listed (ListBuckets)

---

## 🗄️ Database Schema

### Audit Logs Table

```sql
CREATE TABLE IF NOT EXISTS audit_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp INTEGER NOT NULL,              -- Unix timestamp (seconds)
    tenant_id TEXT,                          -- NULL for global events
    user_id TEXT NOT NULL,                   -- User who performed the action
    username TEXT NOT NULL,                  -- Username for display
    event_type TEXT NOT NULL,                -- Event category (see Event Types)
    resource_type TEXT,                      -- Type of resource affected
    resource_id TEXT,                        -- ID of affected resource
    resource_name TEXT,                      -- Name of affected resource (for display)
    action TEXT NOT NULL,                    -- Action performed (create, delete, update, etc.)
    status TEXT NOT NULL,                    -- success or failed
    ip_address TEXT,                         -- Client IP address
    user_agent TEXT,                         -- Client user agent
    details TEXT,                            -- JSON object with additional details
    created_at INTEGER NOT NULL              -- Record creation timestamp
);

-- Indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_audit_logs_timestamp ON audit_logs(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant_id ON audit_logs(tenant_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_event_type ON audit_logs(event_type);
CREATE INDEX IF NOT EXISTS idx_audit_logs_status ON audit_logs(status);
CREATE INDEX IF NOT EXISTS idx_audit_logs_resource_type ON audit_logs(resource_type);
```

### Event Types (Constants)

```go
const (
    // Authentication Events
    EventTypeLoginSuccess           = "login_success"
    EventTypeLoginFailed            = "login_failed"
    EventTypeLogout                 = "logout"
    EventTypeUserBlocked            = "user_blocked"
    EventTypeUserUnblocked          = "user_unblocked"
    EventTypePasswordChanged        = "password_changed"
    EventType2FAEnabled             = "2fa_enabled"
    EventType2FADisabled            = "2fa_disabled"
    EventType2FAVerifySuccess       = "2fa_verify_success"
    EventType2FAVerifyFailed        = "2fa_verify_failed"

    // User Management Events
    EventTypeUserCreated            = "user_created"
    EventTypeUserDeleted            = "user_deleted"
    EventTypeUserUpdated            = "user_updated"
    EventTypeUserRoleChanged        = "user_role_changed"
    EventTypeUserStatusChanged      = "user_status_changed"

    // Bucket Management Events
    EventTypeBucketCreated          = "bucket_created"
    EventTypeBucketDeleted          = "bucket_deleted"

    // Access Key Events
    EventTypeAccessKeyCreated       = "access_key_created"
    EventTypeAccessKeyDeleted       = "access_key_deleted"
    EventTypeAccessKeyStatusChanged = "access_key_status_changed"

    // Tenant Management Events
    EventTypeTenantCreated          = "tenant_created"
    EventTypeTenantDeleted          = "tenant_deleted"
    EventTypeTenantUpdated          = "tenant_updated"
)

const (
    // Resource Types
    ResourceTypeUser       = "user"
    ResourceTypeBucket     = "bucket"
    ResourceTypeAccessKey  = "access_key"
    ResourceTypeTenant     = "tenant"
    ResourceTypeSystem     = "system"

    // Actions
    ActionCreate   = "create"
    ActionDelete   = "delete"
    ActionUpdate   = "update"
    ActionLogin    = "login"
    ActionLogout   = "logout"
    ActionBlock    = "block"
    ActionUnblock  = "unblock"
    ActionEnable   = "enable"
    ActionDisable  = "disable"
    ActionVerify   = "verify"

    // Status
    StatusSuccess = "success"
    StatusFailed  = "failed"
)
```

---

## 📁 Backend Implementation

### Phase 1: Core Infrastructure (Priority: HIGH) ✅ COMPLETED

#### Task 1.1: Create Audit Package ✅ COMPLETED
**File**: `internal/audit/types.go` (NEW)
**Status**: ✅ Done
**Actual Time**: ~30 minutes
```go
// AuditEvent represents a single audit log event
type AuditEvent struct {
    TenantID     string
    UserID       string
    Username     string
    EventType    string
    ResourceType string
    ResourceID   string
    ResourceName string
    Action       string
    Status       string
    IPAddress    string
    UserAgent    string
    Details      map[string]interface{}
}

// AuditLogFilters for querying logs
type AuditLogFilters struct {
    TenantID     string
    UserID       string
    EventType    string
    ResourceType string
    Action       string
    Status       string
    StartDate    int64
    EndDate      int64
    Page         int
    PageSize     int
}

// AuditLog represents a stored audit log record
type AuditLog struct {
    ID           int64
    Timestamp    int64
    TenantID     string
    UserID       string
    Username     string
    EventType    string
    ResourceType string
    ResourceID   string
    ResourceName string
    Action       string
    Status       string
    IPAddress    string
    UserAgent    string
    Details      string // JSON
    CreatedAt    int64
}
```

---

#### Task 1.2: Create Audit Manager Interface ✅ COMPLETED
**File**: `internal/audit/manager.go` (NEW)
**Status**: ✅ Done
**Actual Time**: ~1 hour
```go
type Manager interface {
    // LogEvent records an audit event
    LogEvent(ctx context.Context, event *AuditEvent) error

    // GetLogs retrieves audit logs with filters (global admin)
    GetLogs(ctx context.Context, filters *AuditLogFilters) ([]*AuditLog, int, error)

    // GetLogsByTenant retrieves logs for a specific tenant (tenant admin)
    GetLogsByTenant(ctx context.Context, tenantID string, filters *AuditLogFilters) ([]*AuditLog, int, error)

    // GetLogByID retrieves a single log entry
    GetLogByID(ctx context.Context, id int64) (*AuditLog, error)

    // PurgeLogs deletes logs older than specified days (maintenance)
    PurgeLogs(ctx context.Context, olderThanDays int) (int, error)
}
```

---

#### Task 1.3: Implement SQLite Storage ✅ COMPLETED
**File**: `internal/audit/sqlite.go` (NEW)
**Status**: ✅ Done
**Actual Time**: ~3 hours

Implement:
- Database initialization and migration
- `LogEvent()` - Insert audit log
- `GetLogs()` - Query with filters and pagination
- `GetLogsByTenant()` - Query filtered by tenant
- `GetLogByID()` - Get single log
- `PurgeLogs()` - Delete old logs

**Implementation Notes**:
- Using `modernc.org/sqlite` (pure Go SQLite)
- Includes retention job that runs every 24 hours
- Background cleanup of logs older than configured retention period

---

### Phase 2: Integration with Existing Code (Priority: HIGH) ✅ COMPLETED

#### Task 2.1: Modify Auth Manager ✅ COMPLETED
**File**: `internal/auth/manager.go` + `internal/auth/audit_helpers.go` (NEW)
**Status**: ✅ Done
**Actual Time**: ~2.5 hours

Add audit logging to:
- `Login()` - Log success/failed
- `Verify2FA()` - Log 2FA verification success/failed
- `Logout()` - Log logout
- `LockAccount()` - Log user blocked
- `UnlockAccount()` - Log user unblocked
- `CreateUser()` - Log user created
- `DeleteUser()` - Log user deleted
- `UpdateUser()` - Log user updated (role changes, status changes)
- `ChangePassword()` - Log password changed (in Console API)
- `Enable2FA()` - Log 2FA enabled
- `Disable2FA()` - Log 2FA disabled

**Implementation Notes**:
- Created `audit_helpers.go` with helper functions for safe logging
- Added `SetAuditManager()` for dependency injection
- Implemented user filtering in `ListUsers()` - regular users see only themselves, tenant admins see their tenant, global admins see all

---

#### Task 2.2: Modify Bucket Manager ✅ COMPLETED
**File**: `internal/bucket/manager_badger.go` (MODIFY)
**Status**: ✅ Done
**Actual Time**: ~1 hour

Add audit logging to:
- `CreateBucket()` - Log bucket created
- `DeleteBucket()` - Log bucket deleted

**Implementation Notes**:
- Added `SetAuditManager()` method
- Created helper `logAuditEvent()` for safe logging
- Extracts user from context using `auth.GetUserFromContext()`

---

#### Task 2.3: Modify Console API ✅ COMPLETED
**File**: `internal/server/console_api.go` (MODIFY)
**Status**: ✅ Done
**Actual Time**: ~2 hours

Add audit logging to:
- `handleCreateAccessKey()` - Log key created ✅
- `handleDeleteAccessKey()` - Log key deleted ✅
- `handleChangePassword()` - Log password changed ✅
- `handleCreateTenant()` - Log tenant created (global admin) ✅
- `handleDeleteTenant()` - Log tenant deleted (global admin) ✅
- `handleUpdateTenant()` - Log tenant updated (global admin) ✅

**Implementation Notes**:
- All audit logging includes user context from request
- Password changes logged immediately after successful update
- Access key and tenant operations logged with full details

**New endpoints** (⏳ Pending - Task 3.1):
- `GET /api/v1/audit-logs` - List all logs (global admin only)
- `GET /api/v1/audit-logs/:id` - Get single log
- `GET /api/v1/tenants/:tenantId/audit-logs` - List tenant logs (tenant admin)

---

#### Task 2.4: Wire Dependencies ✅ COMPLETED
**File**: `internal/server/server.go` + `internal/config/config.go` (MODIFY)
**Status**: ✅ Done
**Actual Time**: ~1.5 hours

- Initialize audit manager ✅
- Inject into auth manager, bucket manager ✅
- Add to cleanup on shutdown ✅
- Start retention job on server start ✅

**Implementation Notes**:
- Added `AuditConfig` to config with `Enable`, `RetentionDays` (default 90), `DBPath`
- Audit manager initialized in `New()` if enabled
- Dependencies injected using interface type assertions
- Retention job started in `Start()` method
- Proper cleanup in `shutdown()` method

---

### Phase 3: API Endpoints (Priority: HIGH) ✅ COMPLETED

#### Task 3.1: Audit Logs API ✅ COMPLETED
**File**: `internal/server/console_api.go` (additions)
**Status**: ✅ Done
**Actual Time**: ~2 hours

```go
// GET /api/v1/audit-logs
func (s *Server) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
    // Extract filters from query params
    // Check if user is global admin or tenant admin
    // Call auditManager.GetLogs() or GetLogsByTenant()
    // Return paginated results
}

// GET /api/v1/audit-logs/:id
func (s *Server) handleGetAuditLog(w http.ResponseWriter, r *http.Request) {
    // Get log by ID
    // Check permissions (global admin or tenant admin for their logs)
    // Return log details
}

// GET /api/v1/tenants/:tenantId/audit-logs
func (s *Server) handleListTenantAuditLogs(w http.ResponseWriter, r *http.Request) {
    // Extract tenant ID from URL
    // Verify user is admin of this tenant or global admin
    // Call auditManager.GetLogsByTenant()
    // Return paginated results
}
```

**Implementation Notes**:
- ✅ `GET /api/v1/audit-logs` - List logs with full filtering and pagination
- ✅ `GET /api/v1/audit-logs/:id` - Get specific log by ID
- ✅ Permission checks: Global admins see all, tenant admins see only their tenant
- ✅ Query parameters: page, page_size, tenant_id, user_id, event_type, resource_type, action, status, start_date, end_date
- ✅ Default page size: 50, max: 100
- ✅ Returns total count for pagination
- ✅ Proper error handling and logging

---

### Phase 4: Helper Utilities (Priority: MEDIUM) ⏸️ SKIPPED

#### Task 4.1: Request Context Helpers
**File**: `internal/audit/helpers.go` (NEW)

**Status**: ⏸️ Skipped - Not needed, helpers already implemented in `internal/auth/audit_helpers.go`

---

### Phase 5: Maintenance & Cleanup (Priority: HIGH) ✅ COMPLETED

#### Task 5.1: Log Retention Policy ✅ COMPLETED
**Status**: ✅ Done - Already included in Task 1.2 (Manager implementation)
**Actual Time**: Included in Phase 1

**Implementation Notes**:
- Background job runs every 24 hours (implemented in `manager.go`)
- Configurable retention period (default: 90 days)
- Simple deletion via `PurgeLogs()` method
- Started automatically in `server.Start()` if audit is enabled
- Logs activity to system logs

---

## 🎨 Frontend Implementation

### Phase 6: Audit Logs UI (Priority: HIGH)

#### Task 6.1: Create Types
**File**: `web/frontend/src/types/index.ts` (MODIFY)

```typescript
export interface AuditLog {
  id: number;
  timestamp: number;
  tenantId?: string;
  userId: string;
  username: string;
  eventType: string;
  resourceType?: string;
  resourceId?: string;
  resourceName?: string;
  action: string;
  status: 'success' | 'failed';
  ipAddress?: string;
  userAgent?: string;
  details?: Record<string, any>;
  createdAt: number;
}

export interface AuditLogFilters {
  tenantId?: string;
  userId?: string;
  eventType?: string;
  resourceType?: string;
  action?: string;
  status?: string;
  startDate?: number;
  endDate?: number;
  page?: number;
  pageSize?: number;
}

export interface AuditLogsResponse {
  logs: AuditLog[];
  total: number;
  page: number;
  pageSize: number;
}
```

**Estimated Lines**: 60 lines
**Complexity**: Low
**Time**: 30 minutes

---

#### Task 6.2: Add API Client Methods
**File**: `web/frontend/src/lib/api.ts` (MODIFY)

```typescript
export const APIClient = {
  // ... existing methods ...

  // Get audit logs (global admin sees all, tenant admin sees filtered)
  getAuditLogs: async (filters?: AuditLogFilters): Promise<AuditLogsResponse> => {
    const params = new URLSearchParams();
    if (filters?.tenantId) params.append('tenant_id', filters.tenantId);
    if (filters?.userId) params.append('user_id', filters.userId);
    if (filters?.eventType) params.append('event_type', filters.eventType);
    if (filters?.status) params.append('status', filters.status);
    if (filters?.startDate) params.append('start_date', filters.startDate.toString());
    if (filters?.endDate) params.append('end_date', filters.endDate.toString());
    if (filters?.page) params.append('page', filters.page.toString());
    if (filters?.pageSize) params.append('page_size', filters.pageSize.toString());

    const response = await fetch(`/api/v1/audit-logs?${params}`, {
      headers: getAuthHeaders(),
    });
    return handleResponse<AuditLogsResponse>(response);
  },

  // Get single audit log
  getAuditLog: async (id: number): Promise<AuditLog> => {
    const response = await fetch(`/api/v1/audit-logs/${id}`, {
      headers: getAuthHeaders(),
    });
    return handleResponse<AuditLog>(response);
  },
};
```

**Estimated Lines**: 40 lines
**Complexity**: Low
**Time**: 30 minutes

---

#### Task 6.3: Create Audit Logs Page
**File**: `web/frontend/src/pages/audit-logs/index.tsx` (NEW)

Main audit logs page with:
- **Data table** with columns:
  - Timestamp (formatted)
  - User (username)
  - Event Type (with badge color coding)
  - Resource (type + name)
  - Action
  - Status (success/failed badge)
  - IP Address
  - Details (expandable)

- **Filter panel**:
  - Date range picker (start/end)
  - Event type dropdown (all event types)
  - Status dropdown (success/failed/all)
  - User search/filter
  - Resource type dropdown

- **Pagination**:
  - Page size selector (10, 25, 50, 100)
  - Previous/Next buttons
  - Page number display

- **Export**:
  - Export to CSV button (downloads current filtered results)

- **Permissions**:
  - Global admins see all logs (all tenants)
  - Tenant admins see only their tenant's logs
  - Regular users cannot access this page

**Estimated Lines**: 450 lines
**Complexity**: Medium-High
**Time**: 5-6 hours

---

#### Task 6.4: Add Navigation Menu Item
**File**: `web/frontend/src/components/layout/AppLayout.tsx` (MODIFY)

Add "Audit Logs" menu item:
- Icon: FileText or ClipboardList from lucide-react
- Route: `/audit-logs`
- Visible only for admins (global or tenant)

**Estimated Lines**: 20 lines
**Complexity**: Low
**Time**: 15 minutes

---

#### Task 6.5: Add Route
**File**: `web/frontend/src/App.tsx` or routing config (MODIFY)

Add route for audit logs page:
```typescript
<Route path="/audit-logs" element={<AuditLogsPage />} />
```

**Estimated Lines**: 5 lines
**Complexity**: Low
**Time**: 5 minutes

---

### Phase 7: UI Enhancements (Priority: MEDIUM)

#### Task 7.1: Event Type Color Coding
Create badge color mapping for event types:
- Login/Logout: Blue
- User Blocked/Unblocked: Orange/Yellow
- User Created/Deleted: Purple
- Bucket Created/Deleted: Cyan
- Access Key Created/Deleted: Green
- Failed events: Red
- Success events: Green

**Estimated Lines**: 50 lines
**Complexity**: Low
**Time**: 30 minutes

---

#### Task 7.2: Details Expansion
Expandable row to show full event details (JSON formatted):
- Click row to expand
- Show formatted JSON with syntax highlighting
- Copy to clipboard button

**Estimated Lines**: 80 lines
**Complexity**: Medium
**Time**: 1 hour

---

#### Task 7.3: CSV Export
Export filtered logs to CSV:
- Format: timestamp, user, event_type, resource, action, status, ip_address
- Download as `audit-logs-YYYY-MM-DD.csv`

**Estimated Lines**: 60 lines
**Complexity**: Low
**Time**: 1 hour

---

## 🧪 Testing Plan

### Phase 8: Backend Testing (Priority: HIGH)

#### Task 8.1: Unit Tests
**File**: `internal/audit/sqlite_test.go` (NEW)

Test cases:
- Create audit log
- Query logs with various filters
- Pagination works correctly
- Tenant filtering works correctly
- Date range filtering works correctly
- Purge old logs

**Estimated Lines**: 300 lines
**Complexity**: Medium
**Time**: 3 hours

---

#### Task 8.2: Integration Tests
Test audit logging in actual operations:
- Login/logout creates audit logs
- User creation creates audit log
- Bucket creation creates audit log
- Failed operations create failed status logs
- Tenant isolation works (tenant admin can't see other tenant logs)

**Estimated Lines**: 200 lines
**Complexity**: Medium
**Time**: 2 hours

---

### Phase 9: Frontend Testing (Priority: MEDIUM)

#### Task 9.1: Manual Testing
- Test all filters work correctly
- Test pagination
- Test permission checks (global vs tenant admin)
- Test CSV export
- Test details expansion
- Test responsive design

**Time**: 2 hours

---

#### Task 9.2: E2E Testing (Optional)
- Automated UI tests with Playwright/Cypress
- Test full user flow (login → perform action → check audit log)

**Time**: 3 hours
**Status**: ⏸️ Can be deferred to v0.5.0

---

## 📊 Progress Tracking

### Backend Tasks Summary

| Task | Priority | Status | Estimated Time | Actual Time |
|------|----------|--------|----------------|-------------|
| 1.1 - Create Audit Types | HIGH | ✅ Done | 30 min | 30 min |
| 1.2 - Create Manager Interface | HIGH | ✅ Done | 1 hour | 1 hour |
| 1.3 - Implement SQLite Storage | HIGH | ✅ Done | 3-4 hours | 3 hours |
| 2.1 - Modify Auth Manager | HIGH | ✅ Done | 2 hours | 2.5 hours |
| 2.2 - Modify Bucket Manager | HIGH | ✅ Done | 30 min | 1 hour |
| 2.3 - Modify Console API | HIGH | ✅ Done | 2-3 hours | 2 hours |
| 2.4 - Wire Dependencies | HIGH | ✅ Done | 30 min | 1.5 hours |
| 3.1 - Audit Logs API | HIGH | ✅ Done | 2 hours | 2 hours |
| 4.1 - Helper Utilities | MEDIUM | ⏸️ Skipped | 1 hour | - |
| 5.1 - Log Retention Policy | HIGH | ✅ Done | 2 hours | (included in 1.2) |
| 8.1 - Unit Tests | HIGH | ⏳ Pending | 3 hours | - |
| 8.2 - Integration Tests | HIGH | ⏳ Pending | 2 hours | - |

**Backend Completed**: 13.5 hours / ~21-23 hours (~65% complete)
**Backend Remaining**: ~7.5-9.5 hours (mostly testing)

---

### Frontend Tasks Summary

| Task | Priority | Status | Estimated Time | Actual Time |
|------|----------|--------|----------------|-------------|
| 6.1 - Create Types | HIGH | ⏳ Pending | 30 min | - |
| 6.2 - Add API Client Methods | HIGH | ⏳ Pending | 30 min | - |
| 6.3 - Create Audit Logs Page | HIGH | ⏳ Pending | 5-6 hours | - |
| 6.4 - Add Navigation Menu | HIGH | ⏳ Pending | 15 min | - |
| 6.5 - Add Route | HIGH | ⏳ Pending | 5 min | - |
| 7.1 - Event Color Coding | MEDIUM | ⏳ Pending | 30 min | - |
| 7.2 - Details Expansion | MEDIUM | ⏳ Pending | 1 hour | - |
| 7.3 - CSV Export | MEDIUM | ⏳ Pending | 1 hour | - |
| 9.1 - Manual Testing | MEDIUM | ⏳ Pending | 2 hours | - |
| 9.2 - E2E Testing | LOW | ⏸️ Deferred | 3 hours | - |

**Frontend Total**: ~11-13 hours

---

### Overall Summary

| Component | High Priority | Medium Priority | Low Priority | Total Time |
|-----------|---------------|-----------------|--------------|------------|
| Backend | 18-20 hours | 1 hour | 0 hours | 19-21 hours |
| Frontend | 7-8 hours | 3.5 hours | 3 hours | 13.5-14.5 hours |
| **TOTAL** | **25-28 hours** | **4.5 hours** | **3 hours** | **32.5-35.5 hours** |

**Estimate for v0.4.0-beta (High + Medium Priority)**: ~29-32 hours (~4-5 days of focused work)

---

## 🚀 Rollout Plan

### Current Version: v0.3.2-beta
**Status**: Stable - All critical bugs fixed, quota system tested

### Version 0.4.0-beta (Complete Audit Logs System)
**Target**: Q1 2026
**Scope**: Complete audit logging system with retention

**Includes** (ALL must be complete for v0.4.0-beta):
- ✅ Core audit logging infrastructure
- ✅ All critical events logged (login, logout, user/bucket/key management, 2FA)
- ✅ Filtering and pagination (date range, event type, status, user)
- ✅ Multi-tenancy support with proper isolation
- ✅ Frontend UI with responsive table and filters
- ✅ CSV export functionality
- ✅ **Log retention policy (default: 90 days)**
- ✅ **Automatic cleanup of old logs**
- ✅ Configuration options (retention days, page size)
- ✅ Unit and integration tests (>80% coverage)
- ✅ Basic documentation (user guide + API docs)

**Excludes** (advanced features for v0.5.0+):
- ⏸️ Log archiving before deletion (only simple deletion in v0.4.0-beta)
- ⏸️ E2E automated testing with Playwright/Cypress
- ⏸️ Export to external systems (syslog, webhook, SIEM)
- ⏸️ Advanced filtering (regex, full-text search)
- ⏸️ Real-time notifications for critical events
- ⏸️ Anomaly detection

**Note**: This will be a BETA release because:
- Audit logs feature is new and needs real-world testing
- Performance with large datasets (>100k records) needs validation
- User feedback needed on UI/UX and filtering capabilities
- Edge cases may be discovered in production use

### Future Versions (v0.5.0+)
**Advanced Features**:
- Log archiving to external storage before deletion
- Export to external systems (syslog, Splunk, ELK stack)
- Advanced filtering and search capabilities
- Real-time notifications and alerting
- Anomaly detection and security insights

---

## 🔒 Security Considerations

### Immutability
- Audit logs MUST be append-only (no UPDATE or DELETE by users)
- Only system maintenance jobs can purge old logs
- No API endpoint to delete individual logs

### Access Control
- Global admins can view all audit logs (all tenants)
- Tenant admins can ONLY view logs from their own tenant
- Regular users cannot access audit logs at all
- Failed permission checks should themselves be logged

### Data Privacy
- NEVER log passwords (even hashed)
- NEVER log sensitive tokens or secrets
- Sanitize user-agent strings (remove sensitive info)
- Consider GDPR compliance for IP address storage

### Performance
- Use database indexes for fast queries
- Implement pagination to avoid loading all logs
- Consider archiving to separate storage after 90 days
- Monitor database size and add alerts

---

## 📝 Configuration Options

### Backend Configuration
```yaml
audit:
  enabled: true
  retention_days: 90  # Auto-delete logs older than this
  max_page_size: 100  # Maximum results per page
  archive_enabled: false  # Archive before deletion
  archive_path: "/var/log/maxiofs/archive"
```

### Environment Variables
```bash
AUDIT_ENABLED=true
AUDIT_RETENTION_DAYS=90
AUDIT_MAX_PAGE_SIZE=100
```

---

## 🎯 Success Criteria

### For v0.4.0-beta (Complete Audit System)
- ✅ All critical events are logged (auth, user mgmt, bucket ops, 2FA)
- ✅ Global admins can view all logs across all tenants
- ✅ Tenant admins can view only their tenant logs
- ✅ Filtering works (date range, event type, status, user)
- ✅ Pagination works correctly with configurable page size
- ✅ CSV export works for filtered results
- ✅ UI is responsive and user-friendly
- ✅ **Log retention policy implemented (90 days default)**
- ✅ **Automatic cleanup job runs daily**
- ✅ Configuration via environment variables works
- ✅ Backend tests pass with >80% coverage
- ✅ No performance degradation on existing operations
- ✅ Basic documentation complete (user guide + API docs)

### For Future Versions (v0.5.0+)
- ✅ Log archiving to external storage before deletion
- ✅ Large log datasets (>1M records) perform well
- ✅ E2E automated tests validate full user flows
- ✅ Export to external systems works (syslog, webhook, SIEM)
- ✅ Advanced filtering (regex, full-text search)
- ✅ Real-time notifications for critical security events
- ✅ Anomaly detection and alerting
- ✅ Complete documentation with production deployment examples

---

## 📚 Documentation Needed

### User Documentation
- [ ] Audit logs overview (what is logged, what isn't)
- [ ] How to view audit logs (UI guide)
- [ ] How to filter and search logs
- [ ] How to export logs
- [ ] Understanding event types and actions

### Admin Documentation
- [ ] Configuration options
- [ ] Log retention policy setup
- [ ] Performance tuning for large datasets
- [ ] Backup and restore procedures
- [ ] GDPR compliance considerations

### API Documentation
- [ ] GET /api/v1/audit-logs endpoint
- [ ] GET /api/v1/audit-logs/:id endpoint
- [ ] Filter parameters documentation
- [ ] Response format examples

---

## ✅ Next Steps

1. **Review and approve this plan** ✅
2. **Start with Phase 1** - Backend infrastructure (Tasks 1.1 - 1.3)
3. **Proceed to Phase 2** - Integration with existing code (Tasks 2.1 - 2.4)
4. **Build API endpoints** - Phase 3 (Task 3.1)
5. **Implement Frontend** - Phase 6 (Tasks 6.1 - 6.5)
6. **Test thoroughly** - Phase 8 & 9
7. **Document and release** - v0.4.0-alpha

---

**Document Version**: 1.0
**Last Updated**: November 13, 2025
**Status**: Ready for Implementation
