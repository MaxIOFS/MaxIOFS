# MaxIOFS Architecture

**Version**: 0.5.0-beta
**S3 Compatibility**: 98%
**Last Updated**: November 23, 2025

## Overview

MaxIOFS is a single-binary S3-compatible object storage system built in Go with an embedded React (Vite) frontend. The architecture emphasizes simplicity, portability, and ease of deployment with tenant-scoped bucket namespaces.

**Testing Status**: Successfully validated with MinIO Warp stress testing (7000+ objects, bulk operations, metadata consistency under load). **98% S3 compatible** with all core operations fully functional. Production-ready for beta testing environments.

## System Architecture

```
┌─────────────────────────────────────┐
│    Single Binary (maxiofs.exe)     │
├─────────────────────────────────────┤
│  Web Console (Port 8081)            │
│  - Embedded React (Vite) frontend   │
│  - Console REST API                 │
│  - JWT authentication               │
├─────────────────────────────────────┤
│  S3 API (Port 8080)                 │
│  - S3-compatible REST API           │
│  - AWS Signature v2/v4 auth         │
│  - Tenant-transparent routing       │
│  - Bucket & object operations       │
├─────────────────────────────────────┤
│  Core Logic                         │
│  - Tenant-scoped bucket mgmt        │
│  - Object management with metrics   │
│  - Multi-tenancy isolation          │
│  - Authentication & authorization   │
├─────────────────────────────────────┤
│  Storage Backend                    │
│  - Tenant-scoped filesystem         │
│  - SQLite metadata                  │
│  - Object Lock support              │
└─────────────────────────────────────┘
```

## Core Components

### 1. HTTP Layer

**Console Server (Port 8081)**
- Serves embedded React (Vite) static files
- REST API for web console operations
- JWT-based authentication
- User, bucket, and tenant management
- Tenant-aware API routing

**S3 API Server (Port 8080)**
- Full S3-compatible REST API
- AWS Signature v2/v4 authentication
- Standard S3 operations (Get/Put/Delete/List)
- Multipart uploads
- Presigned URLs
- Object Lock

### 2. Business Logic

**Bucket Manager** (Tenant-scoped)
```go
type Manager interface {
    // All methods now accept tenantID as first parameter after context
    CreateBucket(ctx context.Context, tenantID, name string) error
    DeleteBucket(ctx context.Context, tenantID, name string) error
    ListBuckets(ctx context.Context, tenantID string) ([]*Bucket, error)
    GetBucketInfo(ctx context.Context, tenantID, name string) (*Bucket, error)
    IncrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
    DecrementObjectCount(ctx context.Context, tenantID, name string, sizeBytes int64) error
}
```

**Object Manager** (Receives bucket paths)
```go
type Manager interface {
    // Receives bucketPath in format "{tenantID}/{bucketName}" or "{bucketName}" for global
    PutObject(ctx context.Context, bucketPath, key string, data io.Reader, headers http.Header) error
    GetObject(ctx context.Context, bucketPath, key string) (*Object, io.ReadCloser, error)
    DeleteObject(ctx context.Context, bucketPath, key string) error
    ListObjects(ctx context.Context, bucketPath, prefix, delimiter, marker string, maxKeys int) error
}
```

**Multi-Tenancy Manager**
- Tenant isolation
- Quota enforcement (storage, buckets, keys)
- Resource accounting

**Audit Manager** (v0.4.0+)
- Event logging for compliance and security
- SQLite-based storage for audit logs
- Automatic retention management
- Multi-tenant isolation (global/tenant admin access)
- Tracks 20+ event types (authentication, user management, buckets, 2FA, etc.)

### 3. Storage Layer

**Filesystem Backend** (Tenant-Scoped)
- Object storage on local filesystem with tenant isolation
- Atomic write operations
- Tenant-scoped directory organization
- **Object path**: `{data_dir}/objects/{tenant_id}/{bucket_name}/{object_key}`
- **Metadata path**: `{data_dir}/.maxiofs/buckets/{tenant_id}/{bucket_name}.json`
- **Global buckets** (admin): `{data_dir}/objects/{bucket_name}/{object_key}`

**Example Storage Structure:**
```
/data/
  ├── objects/
  │   ├── tenant-abc123/
  │   │   ├── backups/file1.txt
  │   │   └── archives/file2.zip
  │   ├── tenant-xyz789/
  │   │   └── backups/file3.txt    ← Same bucket name, different tenant
  │   └── global-bucket/admin.dat  ← Global admin bucket
  ├── db/                          ← BadgerDB metadata store
  │   ├── MANIFEST
  │   ├── 000001.vlog
  │   └── 000001.sst
  └── audit.db                     ← SQLite audit logs (v0.4.0+)
```

**BadgerDB Metadata Store**
- High-performance key-value store for metadata
- Object metadata (versioning, lock, retention, tags)
- Bucket information (tenant-scoped)
- User credentials (bcrypt hashed)
- Tenant quotas and real-time usage tracking
- Access keys with tenant associations
- Path: `{data_dir}/db/`

**SQLite Audit Database** (v0.4.0+)
- Audit log storage (separate from operational metadata)
- Immutable append-only logs
- Tracks all system events (authentication, user management, buckets, 2FA)
- Automatic retention management (default: 90 days)
- Path: `{data_dir}/audit.db`

## Authentication

### Console Authentication
- Username/password login
- **Two-Factor Authentication (2FA)** with TOTP (optional, v0.3.2-beta)
- JWT tokens (24 hour expiration with idle timeout)
- Stored in localStorage
- Role-based access control (RBAC) with 4 roles: admin, user, readonly, guest

### S3 API Authentication
- Access Key / Secret Key
- AWS Signature v2 and v4
- Compatible with AWS CLI, SDKs, and S3 tools

## Multi-Tenancy

```
Global Admin (No tenant)
    ├── Tenant A (tenant-abc123)
    │   ├── Tenant Admin
    │   ├── Users
    │   ├── Buckets (namespace: tenant-abc123/*)
    │   │   ├── backups → /objects/tenant-abc123/backups/
    │   │   └── archives → /objects/tenant-abc123/archives/
    │   └── Access Keys
    └── Tenant B (tenant-xyz789)
        ├── Tenant Admin
        ├── Users
        ├── Buckets (namespace: tenant-xyz789/*)
        │   ├── backups → /objects/tenant-xyz789/backups/  ← Same name, isolated!
        │   └── media → /objects/tenant-xyz789/media/
        └── Access Keys
```

**Resource Isolation**
- Each tenant has **isolated bucket namespace**
  - Tenant A can create "backups" bucket
  - Tenant B can also create "backups" bucket (no conflict!)
  - Storage paths: `tenant-abc123/backups` vs `tenant-xyz789/backups`
- Quota enforcement (storage, buckets, keys)
- Zero cross-tenant visibility or access
- Global admins can manage all tenants and see all namespaces

**S3 API Transparency**
- Clients only see bucket names (e.g., "backups")
- Backend automatically resolves: `access_key` → `user` → `tenant_id` → `tenant-abc123/backups`
- 100% S3-compatible - no special client configuration needed

## Data Flow

### Object Upload (Tenant-Scoped)
```
1. Client → S3 API: PUT /backups/file.txt
2. Authentication: Extract access_key from AWS Signature
3. Tenant Resolution:
   - Query: access_key → user → tenant_id = "tenant-abc123"
   - Construct bucket path: "tenant-abc123/backups"
4. Authorization: Check user owns bucket in tenant namespace
5. Quota check: Verify tenant storage limit not exceeded
6. Write to filesystem: /data/objects/tenant-abc123/backups/file.txt
7. Update bucket metrics: IncrementObjectCount(tenant-abc123, backups, size)
8. Update metadata in SQLite
9. Return success response to client
```

**Key Point**: Client never sees "tenant-abc123/backups", only "backups"

### Object Download (Tenant-Scoped)
```
1. Client → S3 API: GET /backups/file.txt
2. Authentication: Extract access_key from AWS Signature
3. Tenant Resolution:
   - Query: access_key → user → tenant_id = "tenant-abc123"
   - Construct bucket path: "tenant-abc123/backups"
4. Authorization: Verify user has access to bucket in tenant namespace
5. Read from filesystem: /data/objects/tenant-abc123/backups/file.txt
6. Stream object data with S3-compatible headers
7. Return object to client
```

**Key Point**: Client requests "backups/file.txt", backend serves from "tenant-abc123/backups/file.txt"

## Security

**Authentication**
- Bcrypt password hashing (cost 10)
- JWT tokens with expiration
- AWS Signature v2/v4 for S3 API

**Authorization**
- Role-based access control (RBAC)
- Tenant-level isolation
- Bucket ownership validation
- Object-level permissions

**Data Protection**
- Object Lock (WORM compliance)
- Filesystem permissions (0755 directories, 0644 files)
- Rate limiting (planned)
- Account lockout (planned)

## Current Limitations

**Beta Status**
- ⚠️ Single-node only (no clustering)
- ⚠️ Filesystem backend only
- ⚠️ Object versioning (basic implementation, not fully validated)
- ⚠️ No automatic compression
- ⚠️ No encryption at rest
- ⚠️ No data replication
- ⚠️ Limited metrics

**Performance**
- ✅ **Validated with MinIO Warp stress testing**
  - 7000+ objects successfully processed
  - Bulk delete operations working correctly
  - BadgerDB transaction conflicts resolved with retry logic
  - Metadata consistency maintained under load
- ✅ **Cross-platform support** (Windows, Linux x64/ARM64, macOS)
- ✅ **Production bug fixes** (Object deletion, GOVERNANCE mode, session management)
- ⚠️ Not validated in high-scale production environments (100+ concurrent users)
- Local benchmarks: ~374 MB/s writes, ~1703 MB/s reads

## Deployment Options

### Standalone Binary
```bash
./maxiofs --data-dir /var/lib/maxiofs
```

### Docker
```bash
docker run -d \
  -p 8080:8080 \
  -p 8081:8081 \
  -v /data:/data \
  maxiofs/maxiofs:1.1.0-alpha
```

### Systemd Service
```ini
[Service]
ExecStart=/usr/local/bin/maxiofs --data-dir /var/lib/maxiofs
```

## Monitoring

**Health Endpoints**
- `GET /health` - Basic health check
- `GET /ready` - Readiness probe
- `GET /metrics` - Prometheus metrics (comprehensive, v0.3.2-beta)

**Prometheus Integration** (v0.3.2-beta)
- Real-time metrics export for monitoring and alerting
- Pre-built Grafana dashboard included
- Docker Compose support with monitoring stack
- Metrics include: API requests, storage usage, error rates, latency

**Logs**
- Structured logging with logrus
- Configurable levels (debug, info, warn, error)
- JSON format optional

## Future Considerations

**Not Implemented Yet**
- Multi-node clustering
- Object versioning (real implementation)
- Data replication
- Encryption at rest
- Advanced metrics and monitoring
- Plugin system
- Additional storage backends (S3, GCS, Azure)

See [TODO.md](../TODO.md) for roadmap.

---

**Note**: This is a beta project. Architecture is stable but may evolve based on production feedback and requirements.
