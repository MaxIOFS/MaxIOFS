# MaxIOFS Multi-Tenancy Guide

**Version**: 0.2.0-dev

> **ALPHA WARNING**: Multi-tenancy is functional but experimental. Features are not fully tested at scale. Not recommended for production without thorough testing.

## Overview

MaxIOFS provides basic multi-tenancy with resource isolation and quota enforcement.

**What Works:**
- 3-tier hierarchy (Global Admin, Tenant Admin, User)
- Tenant isolation in database and API
- Quota enforcement (storage, buckets, keys)
- Real-time bucket count tracking
- Web console and API management

**What's Limited:**
- Storage tracking (checked on upload, not on delete)
- High-scale scenarios (100+ tenants untested)
- S3 API with tenant keys (basic implementation)

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

-- Buckets with tenant FK
CREATE TABLE buckets (
    name TEXT PRIMARY KEY,
    tenant_id TEXT,
    FOREIGN KEY(tenant_id) REFERENCES tenants(id)
);
```

### API Filtering

All endpoints automatically filter by tenant:
- Global Admins see all
- Tenant Admins/Users see only their tenant

---

## Quota Management

### 1. Storage Quota

**Maximum total size** of tenant's objects.

**Enforcement:**
- Checked before upload (web console)
- Returns 403 if exceeded

**Limitations:**
- Not decremented on delete (alpha)
- Requires manual recalculation
- Not enforced on S3 API (alpha)

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
POST /api/tenants
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
POST /api/users
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
POST /api/users
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
PUT /api/tenants/{tenantId}
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
- Bucket quota enforcement
- Access key quota enforcement
- Bucket count tracking
- User authentication with tenant

### ⚠️ Partially Tested

- Storage quota (upload only, not delete)
- S3 API with tenant keys
- Tenant deletion (soft delete)

### ❌ Not Tested

- High-scale (100+ tenants)
- Storage accuracy after many ops
- Cross-tenant migration
- Tenant data export

---

## Alpha Limitations

### Known Issues

1. **Storage Tracking** - Not updated on delete
2. **S3 API** - Quota enforcement incomplete
3. **No Alerts** - No notifications for quota limits
4. **No Billing** - Usage tracked but no billing
5. **Soft Delete** - Tenant deletion doesn't cascade

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

### Storage Inaccurate

Storage may drift in alpha. Manual recalculation required.

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

Planned for beta and beyond:

1. Accurate storage tracking
2. Usage dashboards
3. Quota alerts
4. Bandwidth quotas
5. API rate limits per tenant
6. Data export tools

---

**Remember**: Multi-tenancy is functional but experimental. Test thoroughly before production use.

---

**Version**: 0.2.0-dev
**Last Updated**: October 2025
