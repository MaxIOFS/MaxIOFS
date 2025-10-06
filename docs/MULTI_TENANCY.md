# MaxIOFS Multi-Tenancy Guide

## Overview

MaxIOFS provides complete multi-tenancy support with resource isolation, quota management, and role-based access control. This guide covers the multi-tenancy architecture, configuration, and best practices.

## Table of Contents

- [Architecture](#architecture)
- [Tenant Management](#tenant-management)
- [Resource Isolation](#resource-isolation)
- [Quota Management](#quota-management)
- [User Roles](#user-roles)
- [API Usage](#api-usage)
- [Best Practices](#best-practices)

---

## Architecture

### Multi-Tenancy Model

MaxIOFS implements a **hierarchical multi-tenancy** model:

```
┌─────────────────────────────────────────┐
│           Global Admin                  │
│  (No tenant, full system access)        │
└─────────────────────────────────────────┘
                    │
        ┌───────────┴───────────┬───────────┐
        │                       │           │
┌───────▼────────┐    ┌─────────▼─────┐    │
│   Tenant A     │    │   Tenant B    │    │...
│                │    │               │    │
│ ┌────────────┐ │    │ ┌───────────┐ │    │
│ │Tenant Admin│ │    │ │Tenant Admin│ │    │
│ └────────────┘ │    │ └───────────┘ │    │
│       │        │    │       │       │    │
│  ┌────┴────┐   │    │  ┌────┴────┐  │    │
│  │ Users   │   │    │  │ Users   │  │    │
│  │ Buckets │   │    │  │ Buckets │  │    │
│  │ Keys    │   │    │  │ Keys    │  │    │
│  └─────────┘   │    │  └─────────┘  │    │
└────────────────┘    └───────────────┘    │
```

### Tenant Structure

Each tenant has:
- **Unique identifier** (UUID)
- **Display name** (human-readable)
- **Resource quotas** (storage, buckets, keys)
- **Isolated resources** (buckets, objects, users)
- **Usage statistics** (real-time tracking)

### Database Schema

```sql
-- Tenants table
CREATE TABLE tenants (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    display_name TEXT NOT NULL,
    description TEXT,
    status TEXT DEFAULT 'active',
    max_storage_bytes INTEGER,
    current_storage_bytes INTEGER DEFAULT 0,
    max_buckets INTEGER,
    current_buckets INTEGER DEFAULT 0,
    max_access_keys INTEGER,
    current_access_keys INTEGER DEFAULT 0,
    metadata TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Users with tenant association
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    tenant_id TEXT,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    roles TEXT NOT NULL,
    status TEXT DEFAULT 'active',
    FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE
);

-- Buckets with tenant ownership
CREATE TABLE buckets (
    name TEXT PRIMARY KEY,
    tenant_id TEXT,
    owner_id TEXT NOT NULL,
    owner_type TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    FOREIGN KEY(tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    FOREIGN KEY(owner_id) REFERENCES users(id) ON DELETE CASCADE
);
```

---

## Tenant Management

### Creating Tenants

**Via Web Console:**
1. Login as Global Admin
2. Navigate to Tenants page
3. Click "Create Tenant"
4. Fill in details:
   - Name (lowercase, unique)
   - Display Name
   - Max Storage (GB)
   - Max Buckets
   - Max Access Keys

**Via API:**
```http
POST /api/tenants HTTP/1.1
Host: localhost:8081
Authorization: Bearer <global-admin-token>
Content-Type: application/json

{
  "name": "acme",
  "displayName": "ACME Corporation",
  "description": "ACME Corp tenant",
  "maxStorageBytes": 107374182400,
  "maxBuckets": 100,
  "maxAccessKeys": 50
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "id": "tenant-abc123",
    "name": "acme",
    "displayName": "ACME Corporation",
    "status": "active",
    "maxStorageBytes": 107374182400,
    "currentStorageBytes": 0,
    "maxBuckets": 100,
    "currentBuckets": 0,
    "maxAccessKeys": 50,
    "currentAccessKeys": 0,
    "createdAt": 1696512000
  }
}
```

### Listing Tenants

**Global Admin:**
```http
GET /api/tenants HTTP/1.1
Authorization: Bearer <global-admin-token>
```

Returns all tenants with statistics.

**Tenant Admin:**
```http
GET /api/tenants HTTP/1.1
Authorization: Bearer <tenant-admin-token>
```

Returns only their own tenant.

### Updating Tenants

**Modify quotas and settings:**
```http
PUT /api/tenants/{tenantId} HTTP/1.1
Authorization: Bearer <global-admin-token>
Content-Type: application/json

{
  "displayName": "ACME Corp",
  "maxStorageBytes": 214748364800,
  "maxBuckets": 200,
  "status": "active"
}
```

### Deleting Tenants

⚠️ **Warning:** Deleting a tenant removes all associated resources (users, buckets, objects).

```http
DELETE /api/tenants/{tenantId} HTTP/1.1
Authorization: Bearer <global-admin-token>
```

**Cascade deletion:**
1. All tenant users deleted
2. All access keys revoked
3. All buckets deleted
4. All objects removed
5. Tenant record deleted

---

## Resource Isolation

### Data Isolation Layers

#### 1. Database-Level Isolation

Every resource has a `tenant_id` foreign key:

```go
type Bucket struct {
    Name       string
    TenantID   string  // Isolation key
    OwnerID    string
    CreatedAt  int64
}

type User struct {
    ID        string
    TenantID  string  // Isolation key
    Username  string
    Roles     []string
}
```

#### 2. API-Level Filtering

All API endpoints automatically filter by tenant:

```go
func (s *Server) handleListBuckets(w http.ResponseWriter, r *http.Request) {
    user := auth.GetUserFromContext(r.Context())

    buckets, err := s.bucketManager.ListBuckets(r.Context())
    if err != nil {
        s.writeError(w, err.Error(), http.StatusInternalServerError)
        return
    }

    // Automatic tenant filtering
    filtered := filterBucketsByTenant(buckets, user)

    s.writeJSON(w, filtered)
}

func filterBucketsByTenant(buckets []Bucket, user *User) []Bucket {
    result := []Bucket{}

    for _, bucket := range buckets {
        // Global admin sees all buckets
        if user.TenantID == "" && hasRole(user, "admin") {
            result = append(result, bucket)
            continue
        }

        // Tenant users see only their tenant's buckets
        if bucket.TenantID == user.TenantID {
            result = append(result, bucket)
        }
    }

    return result
}
```

#### 3. Storage-Level Isolation

Objects are stored in tenant-specific directories:

```
/data/objects/
  ├── global/               # Global admin buckets (no tenant)
  │   └── admin-bucket/
  ├── tenant-abc123/        # ACME tenant
  │   ├── acme-backups/
  │   └── acme-documents/
  └── tenant-xyz789/        # Other tenant
      └── other-bucket/
```

**Benefits:**
- Easy to backup per tenant
- Simple quota calculation
- Clear resource ownership
- Tenant data portability

---

## Quota Management

### Quota Types

MaxIOFS enforces three types of quotas per tenant:

#### 1. Storage Quota (Bytes)

Maximum total size of all objects in tenant's buckets.

**Enforcement:**
```go
func (s *Server) checkStorageQuota(ctx context.Context, tenantID string, uploadSize int64) error {
    tenant, err := s.tenantManager.GetTenant(ctx, tenantID)
    if err != nil {
        return err
    }

    if tenant.CurrentStorageBytes + uploadSize > tenant.MaxStorageBytes {
        return fmt.Errorf("storage quota exceeded: %d/%d bytes",
            tenant.CurrentStorageBytes + uploadSize,
            tenant.MaxStorageBytes)
    }

    return nil
}
```

**Checked on:**
- Object upload (PUT)
- Multipart upload completion

#### 2. Bucket Quota

Maximum number of buckets per tenant.

**Enforcement:**
```go
func (s *Server) checkBucketQuota(ctx context.Context, tenantID string) error {
    tenant, err := s.tenantManager.GetTenant(ctx, tenantID)
    if err != nil {
        return err
    }

    if tenant.CurrentBuckets >= tenant.MaxBuckets {
        return fmt.Errorf("bucket quota exceeded: %d/%d",
            tenant.CurrentBuckets,
            tenant.MaxBuckets)
    }

    return nil
}
```

**Checked on:**
- Bucket creation (PUT bucket)

#### 3. Access Key Quota

Maximum number of S3 access keys per tenant.

**Enforcement:**
```go
func (s *Server) checkAccessKeyQuota(ctx context.Context, tenantID string) error {
    tenant, err := s.tenantManager.GetTenant(ctx, tenantID)
    if err != nil {
        return err
    }

    if tenant.CurrentAccessKeys >= tenant.MaxAccessKeys {
        return fmt.Errorf("access key quota exceeded: %d/%d",
            tenant.CurrentAccessKeys,
            tenant.MaxAccessKeys)
    }

    return nil
}
```

**Checked on:**
- Access key creation

### Real-Time Usage Tracking

Usage is updated in real-time:

```go
// On object upload
func (s *Server) handleUploadObject(w http.ResponseWriter, r *http.Request) {
    // ... upload object ...

    // Update tenant storage usage
    s.tenantManager.IncrementStorageUsage(tenantID, objectSize)
}

// On bucket creation
func (s *Server) handleCreateBucket(w http.ResponseWriter, r *http.Request) {
    // ... create bucket ...

    // Update tenant bucket count
    s.tenantManager.IncrementBucketCount(tenantID)
}

// On object deletion
func (s *Server) handleDeleteObject(w http.ResponseWriter, r *http.Request) {
    // ... delete object ...

    // Update tenant storage usage
    s.tenantManager.DecrementStorageUsage(tenantID, objectSize)
}
```

### Quota Alerts (Future)

Planned features:
- Email alerts at 80%, 90%, 100%
- Webhook notifications
- Automatic quota increase requests
- Usage reports

---

## User Roles

### Role Types

#### 1. Global Admin
- **Tenant ID:** Empty (`""`)
- **Roles:** `["admin"]`
- **Scope:** Full system

**Permissions:**
```go
const (
    CanManageAllTenants     = true
    CanManageAllUsers       = true
    CanManageAllBuckets     = true
    CanViewSystemMetrics    = true
    CanModifySystemConfig   = true
    CanUnlockAnyAccount     = true
)
```

**Creating Global Admin:**
```http
POST /api/users HTTP/1.1
Authorization: Bearer <existing-admin-token>
Content-Type: application/json

{
  "username": "newadmin",
  "password": "secure-password",
  "email": "admin@example.com",
  "roles": ["admin"],
  "tenantId": ""  // Empty for global admin
}
```

#### 2. Tenant Admin
- **Tenant ID:** Set to tenant UUID
- **Roles:** `["admin"]`
- **Scope:** Single tenant

**Permissions:**
```go
const (
    CanManageTenantUsers    = true   // Only in same tenant
    CanManageTenantBuckets  = true   // Only in same tenant
    CanViewTenantMetrics    = true
    CanUnlockTenantUsers    = true   // Only in same tenant
    CanManageOtherTenants   = false
)
```

**Creating Tenant Admin:**
```http
POST /api/users HTTP/1.1
Authorization: Bearer <global-admin-token>
Content-Type: application/json

{
  "username": "acme-admin",
  "password": "secure-password",
  "email": "admin@acme.com",
  "roles": ["admin"],
  "tenantId": "tenant-abc123"
}
```

#### 3. Tenant User
- **Tenant ID:** Set to tenant UUID
- **Roles:** `["user"]`
- **Scope:** Limited access within tenant

**Permissions:**
```go
const (
    CanAccessAssignedBuckets = true
    CanUploadDownloadObjects = true
    CanManageOwnAccessKeys   = true
    CanViewOwnMetrics        = true
    CanManageUsers           = false
    CanManageBuckets         = false
)
```

**Creating Tenant User:**
```http
POST /api/users HTTP/1.1
Authorization: Bearer <tenant-admin-token>
Content-Type: application/json

{
  "username": "john.doe",
  "password": "secure-password",
  "email": "john@acme.com",
  "roles": ["user"],
  "tenantId": "tenant-abc123"
}
```

### Permission Matrix

| Action | Global Admin | Tenant Admin | Tenant User |
|--------|-------------|--------------|-------------|
| Create Tenant | ✅ | ❌ | ❌ |
| Delete Tenant | ✅ | ❌ | ❌ |
| Modify Tenant Quotas | ✅ | ❌ | ❌ |
| View All Tenants | ✅ | Own Only | ❌ |
| Create User (Any Tenant) | ✅ | Own Only | ❌ |
| Delete User (Any Tenant) | ✅ | Own Only | ❌ |
| Unlock User | ✅ | Own Tenant | ❌ |
| Create Bucket | ✅ | ✅ | Limited |
| Delete Bucket | ✅ | ✅ | Limited |
| Upload Object | ✅ | ✅ | ✅ |
| Download Object | ✅ | ✅ | ✅ |
| View System Metrics | ✅ | ❌ | ❌ |
| View Tenant Metrics | ✅ | ✅ | Own Only |
| Create Access Key | ✅ | ✅ | Own Only |
| Revoke Access Key | ✅ | ✅ | Own Only |

---

## API Usage

### Authentication Context

All API requests include user context:

```go
type UserContext struct {
    UserID   string
    Username string
    TenantID string   // Empty for global admin
    Roles    []string
}

// Extract from JWT token
func GetUserFromContext(ctx context.Context) *UserContext {
    claims := ctx.Value("user").(jwt.MapClaims)
    return &UserContext{
        UserID:   claims["user_id"].(string),
        Username: claims["username"].(string),
        TenantID: claims["tenant_id"].(string),
        Roles:    claims["roles"].([]string),
    }
}
```

### Tenant-Scoped Queries

**Example: List buckets for current user's tenant**

```go
func (bm *BucketManager) ListBucketsForUser(ctx context.Context, user *UserContext) ([]Bucket, error) {
    var buckets []Bucket

    // Global admin sees all buckets
    if user.TenantID == "" && hasRole(user.Roles, "admin") {
        rows, _ := bm.db.Query("SELECT * FROM buckets")
        // ... scan all buckets
        return buckets, nil
    }

    // Tenant users see only their tenant's buckets
    rows, _ := bm.db.Query("SELECT * FROM buckets WHERE tenant_id = ?", user.TenantID)
    // ... scan tenant buckets

    return buckets, nil
}
```

### S3 API with Tenants

S3 access keys are scoped to tenants:

```go
type AccessKey struct {
    ID          string
    AccessKey   string
    SecretKey   string
    UserID      string
    TenantID    string  // Inherited from user
    Permissions []string
}

// S3 authentication includes tenant context
func (am *AuthManager) ValidateS3Signature(accessKey, signature string) (*User, error) {
    key, err := am.GetAccessKey(accessKey)
    if err != nil {
        return nil, err
    }

    // Verify signature...

    // Load user with tenant context
    user, err := am.GetUser(key.UserID)
    if err != nil {
        return nil, err
    }

    // All subsequent operations use user.TenantID for isolation
    return user, nil
}
```

---

## Best Practices

### 1. Tenant Design

**Do:**
- ✅ Use meaningful tenant names (e.g., company name)
- ✅ Set appropriate quotas based on SLA
- ✅ Monitor tenant usage regularly
- ✅ Document tenant ownership

**Don't:**
- ❌ Create tenants for individual users (use roles instead)
- ❌ Share access keys across tenants
- ❌ Bypass tenant isolation in custom code

### 2. Quota Planning

**Storage Quotas:**
```
Small Tenant:   10 GB   (10,737,418,240 bytes)
Medium Tenant:  100 GB  (107,374,182,400 bytes)
Large Tenant:   1 TB    (1,099,511,627,776 bytes)
Enterprise:     10 TB+  (custom)
```

**Bucket Quotas:**
```
Basic:      10 buckets
Standard:   100 buckets
Premium:    1000 buckets
```

**Access Key Quotas:**
```
Basic:      5 keys
Standard:   50 keys
Premium:    500 keys
```

### 3. User Management

**Hierarchy:**
```
1 Global Admin (system setup)
  ↓
Multiple Tenants
  ↓
1-2 Tenant Admins per tenant
  ↓
Multiple Tenant Users
```

**Principle of Least Privilege:**
- Grant minimum permissions needed
- Use Tenant Users for most accounts
- Reserve Tenant Admin for managers
- Limit Global Admins to 1-2 people

### 4. Resource Naming

**Buckets:**
```
{tenant-name}-{purpose}

Examples:
  acme-backups
  acme-documents
  acme-logs
```

**Users:**
```
{firstname}.{lastname}  (for humans)
{service}-{tenant}      (for applications)

Examples:
  john.doe
  backup-service-acme
```

### 5. Monitoring

**Track per tenant:**
- Storage usage over time
- API request rates
- Error rates
- Quota utilization (%)

**Alert on:**
- Quota reaching 80%
- Unusual access patterns
- Failed authentication attempts
- API errors spike

### 6. Migration

**Moving data between tenants:**

```bash
# 1. Export from source tenant
aws s3 sync s3://source-bucket ./export-data \
  --endpoint-url http://localhost:8080

# 2. Change authentication to destination tenant
export AWS_ACCESS_KEY_ID=<dest-access-key>
export AWS_SECRET_ACCESS_KEY=<dest-secret-key>

# 3. Import to destination tenant
aws s3 sync ./export-data s3://dest-bucket \
  --endpoint-url http://localhost:8080
```

---

## Use Cases

### 1. SaaS Application

**Scenario:** Multi-customer SaaS platform

**Setup:**
- 1 tenant per customer
- Tenant Admin = Customer Admin
- Tenant Users = Customer's end users
- Quotas based on subscription tier

**Example:**
```
Tenant: "customer-123"
  ├── Admin: "admin@customer123.com"
  ├── Users: 50 employees
  ├── Buckets:
  │   ├── customer-123-uploads
  │   └── customer-123-backups
  └── Quota: 500 GB
```

### 2. Department Isolation

**Scenario:** Large organization with departments

**Setup:**
- 1 tenant per department
- Tenant Admin = Department Manager
- Tenant Users = Department Staff

**Example:**
```
Global Admin: IT Department

Tenants:
  ├── "engineering"
  │   ├── Admin: "eng-manager"
  │   ├── Users: 100
  │   └── Quota: 5 TB
  │
  ├── "marketing"
  │   ├── Admin: "marketing-manager"
  │   ├── Users: 50
  │   └── Quota: 1 TB
  │
  └── "finance"
      ├── Admin: "finance-manager"
      ├── Users: 30
      └── Quota: 500 GB
```

### 3. Managed Service Provider

**Scenario:** MSP serving multiple clients

**Setup:**
- 1 tenant per client
- MSP staff = Global Admins
- Client staff = Tenant Admins/Users

**Example:**
```
MSP: "TechCorp MSP"
  ├── Global Admins: MSP staff
  │
  └── Tenants:
      ├── "client-acme"
      ├── "client-widgets-inc"
      └── "client-gadgets-co"
```

---

## Troubleshooting

### User Can't See Resources

**Check:**
1. User's tenant ID matches resource's tenant ID
2. User has appropriate role
3. Resource wasn't deleted

```sql
-- Verify user tenant
SELECT id, username, tenant_id, roles FROM users WHERE username = 'john.doe';

-- Verify bucket tenant
SELECT name, tenant_id, owner_id FROM buckets WHERE name = 'my-bucket';

-- Check if they match
SELECT
  u.username,
  u.tenant_id AS user_tenant,
  b.tenant_id AS bucket_tenant,
  CASE WHEN u.tenant_id = b.tenant_id THEN 'MATCH' ELSE 'MISMATCH' END AS status
FROM users u, buckets b
WHERE u.username = 'john.doe' AND b.name = 'my-bucket';
```

### Quota Exceeded Errors

**Check current usage:**
```sql
SELECT
  id,
  name,
  current_storage_bytes,
  max_storage_bytes,
  ROUND(current_storage_bytes * 100.0 / max_storage_bytes, 2) AS usage_pct
FROM tenants
WHERE id = 'tenant-abc123';
```

**Increase quota (Global Admin only):**
```http
PUT /api/tenants/tenant-abc123
Authorization: Bearer <global-admin-token>

{
  "maxStorageBytes": 214748364800
}
```

### Tenant Deletion Issues

**Error:** "Cannot delete tenant with active resources"

**Solution:** Delete in order:
1. Delete all objects in tenant buckets
2. Delete all tenant buckets
3. Delete/reassign all tenant users
4. Delete tenant

```bash
# Script to clean tenant
TENANT_ID="tenant-abc123"

# 1. Delete objects
for bucket in $(aws s3 ls --endpoint-url http://localhost:8080 | awk '{print $3}'); do
  aws s3 rm s3://$bucket --recursive --endpoint-url http://localhost:8080
done

# 2. Delete buckets
for bucket in $(aws s3 ls --endpoint-url http://localhost:8080 | awk '{print $3}'); do
  aws s3 rb s3://$bucket --endpoint-url http://localhost:8080
done

# 3. Delete users via API
curl -X DELETE http://localhost:8081/api/users/{userId} \
  -H "Authorization: Bearer <token>"

# 4. Delete tenant
curl -X DELETE http://localhost:8081/api/tenants/$TENANT_ID \
  -H "Authorization: Bearer <token>"
```

---

## Future Enhancements

### Planned Features

1. **Tenant Templates**
   - Pre-configured tenant setups
   - Default policies and permissions
   - Automated onboarding

2. **Tenant Marketplace**
   - Self-service tenant creation
   - Subscription management
   - Billing integration

3. **Advanced Quotas**
   - Bandwidth limits
   - API rate limits per tenant
   - Time-based quotas

4. **Tenant Analytics**
   - Usage dashboards
   - Cost allocation
   - Trend analysis

5. **Multi-Region Tenants**
   - Geo-distributed storage
   - Regional compliance
   - Data residency controls

---

## Conclusion

MaxIOFS multi-tenancy provides:
- ✅ Complete resource isolation
- ✅ Flexible quota management
- ✅ Role-based access control
- ✅ Real-time usage tracking
- ✅ Enterprise-grade security

For support: https://maxiofs.io/docs/multi-tenancy
