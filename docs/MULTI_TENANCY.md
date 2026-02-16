# MaxIOFS Multi-Tenancy Guide

**Version**: 0.9.0-beta

> **BETA STATUS**: Multi-tenancy is functional and has been tested with warp stress testing. Features show stability under load and production bugs have been fixed. Suitable for staging and testing environments. Production use requires thorough validation in your specific environment.

## Overview

MaxIOFS provides basic multi-tenancy with resource isolation and quota enforcement.

**What Works:**
- 3-tier hierarchy (Global Admin, Tenant Admin, User)
- Tenant isolation in database and API
- Quota enforcement (storage, buckets, keys)
- Real-time bucket count tracking
- Web console and API management

**What's Limited:**
- High-scale scenarios (100+ tenants untested)
- Advanced per-tenant rate limiting

---

## Tenant Hierarchy

```
┌─────────────────────────┐
│    Global Admin         │
│  (No tenant, full)      │
└──────────┬──────────────┘
           │
    ┌──────┴──────┬────────┐
    │             │        │
┌───▼───┐   ┌─────▼──┐    │
│Tenant A│   │Tenant B│   ...
│        │   │        │
│ Admin  │   │ Admin  │
│   ↓    │   │   ↓    │
│ Users  │   │ Users  │
└────────┘   └────────┘
```

### Roles

**Global Admin:**
- No tenant assignment
- Full system access
- Manages all tenants
- Typically 1-2 accounts

**Tenant Admin:**
- Assigned to specific tenant
- Manages tenant users/buckets
- Cannot modify quotas
- One or more per tenant

**Tenant User:**
- Assigned to specific tenant
- Creates buckets/objects (quota permitting)
- Manages own access keys
- Multiple per tenant

---

## Resource Isolation

### Global Bucket Uniqueness (v0.4.2-beta)

**Key Feature**: Bucket names are globally unique across all tenants (AWS S3 compatible).

**What this means:**
- **Bucket names must be unique across the entire system** (like AWS S3)
- Tenant A can create a bucket named "backups"
- Tenant B **CANNOT** create a bucket named "backups" (will be rejected)
- Each bucket name is globally unique, preventing conflicts
- Tenants still cannot see or access other tenants' buckets (isolation maintained)

**Why this change:**
- **AWS S3 Compatibility**: AWS S3 requires globally unique bucket names
- Better S3 client compatibility (no tenant prefix in URLs)
- Standard S3 URL format: `http://endpoint/bucket/object` (not `/tenant-id/bucket/object`)

**How it works:**

```
Physical Storage Structure:
/data/objects/
  ├── tenant-abc123/
  │   ├── acme-backups/        ← Tenant A's "acme-backups" bucket
  │   ├── acme-archives/
  │   └── acme-logs/
  ├── tenant-xyz789/
  │   ├── xyz-backups/          ← Tenant B's "xyz-backups" bucket (different name!)
  │   ├── xyz-media/
  │   └── xyz-databases/
  └── global-bucket/             ← Global admin bucket (no tenant prefix)

Metadata Storage (BadgerDB):
bucket:tenant-abc123:acme-backups     ← Metadata for Tenant A's "acme-backups"
bucket:tenant-abc123:acme-archives
bucket:tenant-abc123:acme-logs
bucket:tenant-xyz789:xyz-backups      ← Metadata for Tenant B's "xyz-backups"
bucket:tenant-xyz789:xyz-media
bucket:tenant-xyz789:xyz-databases
bucket:global:global-bucket           ← Global admin bucket
```

**Automatic Tenant Resolution**: 100% transparent to S3 clients
- Client request: `GET /acme-backups/file.txt` with tenant credentials
- Backend resolves: `bucket_name` → lookup in metadata → `tenant_id` → `tenant-abc123/acme-backups/file.txt`
- Client uses standard S3 URL format (no tenant prefix)
- Presigned URLs: `http://endpoint/acme-backups/file.txt?signature=...`

### Database Schema

```sql
-- Tenants
CREATE TABLE tenants (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    display_name TEXT NOT NULL,
    max_storage_bytes INTEGER,
    current_storage_bytes INTEGER DEFAULT 0,
    max_buckets INTEGER,
    current_buckets INTEGER DEFAULT 0,
    max_access_keys INTEGER,
    current_access_keys INTEGER DEFAULT 0
);

-- Users with tenant FK
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    tenant_id TEXT,
    username TEXT UNIQUE NOT NULL,
    FOREIGN KEY(tenant_id) REFERENCES tenants(id)
);

-- Buckets with tenant_id (globally unique names)
-- Note: In BadgerDB, buckets are stored with keys: bucket:{tenant_id}:{name}
-- However, bucket names are validated for global uniqueness (v0.4.2-beta)
-- This ensures AWS S3 compatibility where bucket names are globally unique
CREATE TABLE buckets (
    name TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    owner_id TEXT,
    owner_type TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (tenant_id, name),  -- Stored per tenant
    UNIQUE (name)  -- v0.4.2-beta: Globally unique bucket names
    FOREIGN KEY(tenant_id) REFERENCES tenants(id)
);
```

### API Filtering

All endpoints automatically filter by tenant:
- Global Admins see all tenants' resources
- Tenant Admins/Users see only their tenant's resources
- **Bucket names are globally unique** (v0.4.2-beta): No duplicate names across tenants (AWS S3 compatible)

---

## Quota Management

### 1. Storage Quota

**Maximum total size** of tenant's objects.

**Enforcement:**
- ✅ Checked before upload (web console and S3 API)
- ✅ Returns 403 if exceeded
- ✅ Real-time tracking (v0.3.2-beta)

**Status:** ✅ **Fully implemented and fixed** (v0.3.2-beta)

**Error Example:**
```json
{
  "error": "Tenant storage quota exceeded (105GB/100GB)"
}
```

### 2. Bucket Quota

**Maximum number of buckets** per tenant.

**Enforcement:**
- Checked on bucket creation
- Counter updated automatically
- Returns 403 if exceeded

**Status:** ✅ Fully implemented

**Error Example:**
```json
{
  "error": "Tenant bucket quota exceeded (100/100)"
}
```

### 3. Access Key Quota

**Maximum number of S3 keys** per tenant.

**Enforcement:**
- Checked on key generation
- Real-time tracking
- Returns 403 if exceeded

**Status:** ✅ Implemented

---

## Creating Tenants and Users

### Create Tenant (Global Admin Only)

```http
POST /api/v1/tenants
Authorization: Bearer <global-admin-token>
Content-Type: application/json

{
  "name": "acme",
  "displayName": "ACME Corporation",
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
    "maxStorageBytes": 107374182400,
    "currentStorageBytes": 0,
    "maxBuckets": 100,
    "currentBuckets": 0
  }
}
```

### Create Tenant Admin

```http
POST /api/v1/users
Authorization: Bearer <global-admin-token>
Content-Type: application/json

{
  "username": "acme-admin",
  "password": "SecurePassword123!",
  "email": "admin@acme.com",
  "roles": ["admin"],
  "tenantId": "tenant-abc123"
}
```

### Create Tenant User

```http
POST /api/v1/users
Authorization: Bearer <tenant-admin-token>
Content-Type: application/json

{
  "username": "john.doe",
  "password": "SecurePassword123!",
  "email": "john@acme.com",
  "roles": ["user"],
  "tenantId": "tenant-abc123"
}
```

### Update Tenant Quotas

Global Admins only:

```http
PUT /api/v1/tenants/{tenantId}
Authorization: Bearer <global-admin-token>
Content-Type: application/json

{
  "maxStorageBytes": 214748364800,
  "maxBuckets": 200,
  "maxAccessKeys": 100
}
```

---

## Best Practices

### Quota Planning

**Recommended Starting Quotas:**

```
Small Tenant:
  Storage:     10 GB
  Buckets:     10
  Keys:        5

Medium Tenant:
  Storage:     100 GB
  Buckets:     100
  Keys:        50

Large Tenant:
  Storage:     1 TB
  Buckets:     1000
  Keys:        500
```

### User Hierarchy

```
1-2 Global Admins (system setup)
  ↓
Multiple Tenants (per customer/dept)
  ↓
1-3 Tenant Admins (managers)
  ↓
Multiple Users (end users)
```

### Resource Naming

**Buckets:**
```
{tenant}-{purpose}

Examples:
  acme-backups
  acme-documents
```

**Users:**
```
{firstname}.{lastname}    # Humans
{tenant}-admin            # Admins
{service}-{tenant}        # Services
```

---

## Testing Status

### ✅ Tested and Working

- Creating tenants via API
- Tenant isolation in console
- ✅ **Storage quota enforcement** (frontend + S3 API, v0.3.2-beta)
- Bucket quota enforcement
- Access key quota enforcement
- Bucket count tracking
- User authentication with tenant
- S3 API with tenant keys
- Tenant deletion with validation

### ❌ Not Tested

- High-scale (100+ tenants)
- Storage accuracy after many ops
- Cross-tenant migration
- Tenant data export

---

## Beta Limitations

### Known Issues

1. ~~**Storage Tracking**~~ - ✅ **FIXED** (v0.3.2-beta)
2. ~~**S3 API Quota**~~ - ✅ **FIXED** (v0.3.2-beta)
3. **No Alerts** - No notifications for quota limits
4. **No Billing** - Usage tracked but no billing integration

### Not Implemented

- Bandwidth quotas
- Per-tenant rate limiting
- Usage dashboards
- Quota increase requests
- Data export tools
- Multi-region support

---

## Troubleshooting

### User Can't See Buckets

```sql
-- Check tenant assignment
SELECT username, tenant_id FROM users WHERE username = 'john';
SELECT name, tenant_id FROM buckets WHERE name = 'my-bucket';
```

### Quota Exceeded

```sql
-- Check usage
SELECT
  name,
  current_buckets,
  max_buckets,
  ROUND(current_buckets * 100.0 / max_buckets, 1) AS usage_pct
FROM tenants
WHERE id = 'tenant-abc123';
```

### Storage Tracking

✅ Storage tracking is now accurate in v0.3.2-beta (fixed at frontend and S3 API level).

---

## Permission Matrix

| Action | Global Admin | Tenant Admin | User |
|--------|--------------|--------------|------|
| Create Tenant | ✅ | ❌ | ❌ |
| Modify Quotas | ✅ | ❌ | ❌ |
| View All Tenants | ✅ | Own | ❌ |
| Create User | ✅ | Own Tenant | ❌ |
| Create Bucket | ✅ | ✅ | ✅ |
| Generate Key | ✅ | ✅ | Own |

---

## Future Roadmap

Planned for future releases:

1. ~~Accurate storage tracking~~ - ✅ **COMPLETED** (v0.3.2-beta)
2. Usage dashboards (partially complete with Prometheus/Grafana)
3. Quota alerts
4. Bandwidth quotas
5. API rate limits per tenant
6. Data export tools

---

**Remember**: Multi-tenancy is functional and stable in beta. Test thoroughly in your environment before production use.

---

**Version**: 0.9.0-beta
**Last Updated**: January 16, 2026
