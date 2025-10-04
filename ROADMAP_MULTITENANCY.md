# Multi-Tenancy Implementation Roadmap

## Overview
Implement multi-tenancy support to allow bucket isolation and access control per user/tenant.

## Current State
- Buckets are globally accessible to all authenticated users
- No ownership or permission model for buckets
- Access keys are user-specific but don't control bucket access

## Goal
- Each bucket has an owner (user or tenant)
- Users can only see/access buckets they own or have been granted access to
- Admin users can see all buckets
- Implement sharing mechanism for buckets

---

## Phase 1: Data Model & Schema Design

### 1.1 Database Schema Changes

**New Table: `tenants`**
```sql
CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    display_name TEXT,
    description TEXT,
    status TEXT DEFAULT 'active',
    metadata TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
```

**Update `users` table:**
```sql
ALTER TABLE users ADD COLUMN tenant_id TEXT;
```

**New Table: `bucket_permissions`**
```sql
CREATE TABLE IF NOT EXISTS bucket_permissions (
    id TEXT PRIMARY KEY,
    bucket_name TEXT NOT NULL,
    user_id TEXT,
    tenant_id TEXT,
    permission_level TEXT NOT NULL, -- 'read', 'write', 'admin'
    granted_by TEXT NOT NULL,
    granted_at INTEGER NOT NULL,
    expires_at INTEGER,
    UNIQUE(bucket_name, user_id),
    UNIQUE(bucket_name, tenant_id)
);
```

**Update bucket metadata:**
```json
{
  "name": "bucket-name",
  "owner_id": "user-id-or-tenant-id",
  "owner_type": "user|tenant",
  "created_at": "...",
  "is_public": false,
  "...": "existing fields"
}
```

### 1.2 Backend Types Update

**File: `internal/bucket/manager.go`**
```go
type Bucket struct {
    Name              string             `json:"name"`
    OwnerID           string             `json:"owner_id"`           // NEW
    OwnerType         string             `json:"owner_type"`         // NEW: "user" or "tenant"
    IsPublic          bool               `json:"is_public"`          // NEW
    CreatedAt         time.Time          `json:"created_at"`
    // ... existing fields
}
```

**File: `internal/auth/types.go`**
```go
type Tenant struct {
    ID          string            `json:"id"`
    Name        string            `json:"name"`
    DisplayName string            `json:"display_name"`
    Description string            `json:"description"`
    Status      string            `json:"status"`
    Metadata    map[string]string `json:"metadata,omitempty"`
    CreatedAt   int64             `json:"created_at"`
    UpdatedAt   int64             `json:"updated_at"`
}

type BucketPermission struct {
    ID              string `json:"id"`
    BucketName      string `json:"bucket_name"`
    UserID          string `json:"user_id,omitempty"`
    TenantID        string `json:"tenant_id,omitempty"`
    PermissionLevel string `json:"permission_level"` // read, write, admin
    GrantedBy       string `json:"granted_by"`
    GrantedAt       int64  `json:"granted_at"`
    ExpiresAt       int64  `json:"expires_at,omitempty"`
}
```

---

## Phase 2: Backend Implementation

### 2.1 Tenant Management

**File: `internal/auth/tenant_store.go`** (NEW)
- `CreateTenant(tenant *Tenant) error`
- `GetTenant(tenantID string) (*Tenant, error)`
- `GetTenantByName(name string) (*Tenant, error)`
- `ListTenants() ([]*Tenant, error)`
- `UpdateTenant(tenant *Tenant) error`
- `DeleteTenant(tenantID string) error`

**File: `internal/auth/manager.go`** (UPDATE)
- Add tenant management methods to Manager interface
- Update `CreateUser` to optionally assign to tenant

### 2.2 Bucket Ownership & Permissions

**File: `internal/bucket/permissions.go`** (NEW)
```go
type PermissionManager interface {
    GrantBucketAccess(bucketName, userID, tenantID, permissionLevel string) error
    RevokeBucketAccess(bucketName, userID, tenantID string) error
    CheckBucketAccess(bucketName, userID string) (bool, string, error)
    ListBucketPermissions(bucketName string) ([]*BucketPermission, error)
    ListUserBucketPermissions(userID string) ([]*BucketPermission, error)
}
```

**File: `internal/bucket/manager.go`** (UPDATE)
- Update `CreateBucket` to set owner_id and owner_type
- Update `ListBuckets` to filter by user permissions
- Add `GetBucketOwner(bucketName string) (string, string, error)`
- Add `TransferBucketOwnership(bucketName, newOwnerID, ownerType string) error`

### 2.3 Permission Filtering Logic

**File: `internal/bucket/filter.go`** (NEW)
```go
func FilterBucketsByPermissions(buckets []*Bucket, userID string, userRoles []string, pm PermissionManager) ([]*Bucket, error) {
    // Admin users see all buckets
    if containsRole(userRoles, "admin") {
        return buckets, nil
    }

    var filtered []*Bucket
    for _, bucket := range buckets {
        // Check if user owns the bucket
        if bucket.OwnerID == userID && bucket.OwnerType == "user" {
            filtered = append(filtered, bucket)
            continue
        }

        // Check if user has explicit permission
        hasAccess, _, err := pm.CheckBucketAccess(bucket.Name, userID)
        if err == nil && hasAccess {
            filtered = append(filtered, bucket)
        }
    }

    return filtered, nil
}
```

### 2.4 API Endpoints

**File: `internal/server/console_api.go`** (UPDATE)

**Tenant Management:**
- `POST /tenants` - Create tenant
- `GET /tenants` - List tenants
- `GET /tenants/{id}` - Get tenant details
- `PUT /tenants/{id}` - Update tenant
- `DELETE /tenants/{id}` - Delete tenant
- `GET /tenants/{id}/users` - List tenant users
- `POST /tenants/{id}/users/{userId}` - Add user to tenant

**Bucket Permissions:**
- `GET /buckets/{bucket}/permissions` - List bucket permissions
- `POST /buckets/{bucket}/permissions` - Grant access
- `DELETE /buckets/{bucket}/permissions/{permissionId}` - Revoke access
- `PUT /buckets/{bucket}/owner` - Transfer ownership

**Update existing endpoints:**
- `GET /buckets` - Add permission filtering
- `POST /buckets` - Set owner on creation
- `GET /users/{user}/buckets` - List user's accessible buckets

---

## Phase 3: Frontend Implementation

### 3.1 Tenant Management UI

**File: `web/frontend/src/app/tenants/page.tsx`** (NEW)
- List all tenants (admin only)
- Create new tenant
- Edit tenant details
- Delete tenant
- View tenant users

**File: `web/frontend/src/app/tenants/[tenant]/page.tsx`** (NEW)
- Tenant details
- List users in tenant
- List buckets owned by tenant
- Tenant statistics

### 3.2 Bucket Ownership UI

**File: `web/frontend/src/app/buckets/page.tsx`** (UPDATE)
- Show only accessible buckets (not all)
- Display owner information on each bucket card
- Filter: My Buckets / Shared with me / All (admin only)

**File: `web/frontend/src/app/buckets/[bucket]/page.tsx`** (UPDATE)
- Show bucket owner
- Show "Transfer Ownership" button (owner/admin only)
- Show "Share" button (owner/admin only)

**File: `web/frontend/src/app/buckets/[bucket]/permissions/page.tsx`** (NEW)
- List users/tenants with access
- Grant access to user/tenant
- Revoke access
- Change permission level

### 3.3 User Assignment to Tenant

**File: `web/frontend/src/app/users/[user]/page.tsx`** (UPDATE)
- Show tenant assignment
- Allow admin to change user's tenant

**File: `web/frontend/src/app/users/create/page.tsx`** (UPDATE)
- Add tenant selection dropdown

### 3.4 Types & API Client

**File: `web/frontend/src/types/index.ts`** (UPDATE)
```typescript
export interface Tenant {
  id: string;
  name: string;
  displayName: string;
  description?: string;
  status: 'active' | 'inactive';
  metadata?: Record<string, string>;
  createdAt: number;
  updatedAt: number;
}

export interface BucketPermission {
  id: string;
  bucketName: string;
  userId?: string;
  tenantId?: string;
  permissionLevel: 'read' | 'write' | 'admin';
  grantedBy: string;
  grantedAt: number;
  expiresAt?: number;
}

export interface Bucket {
  // ... existing fields
  ownerId: string;
  ownerType: 'user' | 'tenant';
  isPublic: boolean;
}

export interface User {
  // ... existing fields
  tenantId?: string;
}
```

**File: `web/frontend/src/lib/api.ts`** (UPDATE)
- Add tenant CRUD methods
- Add bucket permission methods
- Update bucket methods to include ownership

---

## Phase 4: Migration & Testing

### 4.1 Data Migration

**File: `internal/auth/migrations.go`** (NEW)
```go
func MigrateTenantSchema(db *sql.DB) error {
    // Create tenants table
    // Create bucket_permissions table
    // Add tenant_id column to users
    // Create default tenant for existing data
    // Assign all existing buckets to admin user
}
```

### 4.2 Testing Strategy

**Unit Tests:**
- Tenant CRUD operations
- Permission checking logic
- Bucket filtering by permissions

**Integration Tests:**
- Create tenant → Create user → Assign to tenant → Create bucket → Verify isolation
- Share bucket → Verify access
- Transfer ownership → Verify new owner has access

**E2E Tests:**
- Admin creates tenant
- Admin creates user in tenant
- User creates bucket (owned by user)
- User shares bucket with another user
- Other user can access shared bucket
- User cannot see buckets from different tenant

---

## Phase 5: Documentation

### 5.1 Update Documentation Files

**File: `README.md`**
- Add multi-tenancy feature description
- Document permission levels
- Add examples

**File: `TODO.md`**
- Mark multi-tenancy tasks as complete
- Add any remaining enhancements

**File: `docs/MULTITENANCY.md`** (NEW)
- Architecture overview
- Permission model explanation
- API examples
- Best practices

---

## Implementation Order

1. ✅ **Phase 1.1** - Database schema design
2. ✅ **Phase 1.2** - Backend types definition
3. ⏳ **Phase 2.1** - Tenant management backend
4. ⏳ **Phase 2.2** - Bucket ownership backend
5. ⏳ **Phase 2.3** - Permission filtering logic
6. ⏳ **Phase 2.4** - API endpoints
7. ⏳ **Phase 3.1** - Tenant management UI
8. ⏳ **Phase 3.2** - Bucket ownership UI
9. ⏳ **Phase 3.3** - User-tenant assignment UI
10. ⏳ **Phase 3.4** - Frontend types & API
11. ⏳ **Phase 4.1** - Data migration
12. ⏳ **Phase 4.2** - Testing
13. ⏳ **Phase 5.1** - Documentation

---

## Estimated Timeline

- **Phase 1**: 1 hour (Design & Types)
- **Phase 2**: 4-6 hours (Backend Implementation)
- **Phase 3**: 4-6 hours (Frontend Implementation)
- **Phase 4**: 2-3 hours (Migration & Testing)
- **Phase 5**: 1 hour (Documentation)

**Total**: ~12-17 hours of development

---

## Breaking Changes

⚠️ **API Changes:**
- `GET /buckets` will now return filtered results based on user permissions (instead of all buckets)
- Bucket creation now requires owner information
- Some endpoints will require additional permissions

⚠️ **Migration Required:**
- Existing buckets will be assigned to the admin user by default
- Users need to be explicitly granted access to existing buckets

---

## Future Enhancements

- [ ] Bucket access request workflow
- [ ] Time-limited bucket sharing
- [ ] Bucket usage quotas per tenant
- [ ] Audit logging for permission changes
- [ ] Role-based access control (RBAC) for fine-grained permissions
- [ ] API key scoping to specific buckets
- [ ] Tenant isolation for metrics and billing
