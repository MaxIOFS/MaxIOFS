# MaxIOFS Architecture

**Version**: 0.2.0-dev

## Overview

MaxIOFS is a single-binary S3-compatible object storage system built in Go with an embedded Next.js frontend. The architecture emphasizes simplicity, portability, and ease of deployment.

## System Architecture

```
┌─────────────────────────────────────┐
│    Single Binary (maxiofs.exe)     │
├─────────────────────────────────────┤
│  Web Console (Port 8081)            │
│  - Embedded Next.js frontend        │
│  - Console REST API                 │
│  - JWT authentication               │
├─────────────────────────────────────┤
│  S3 API (Port 8080)                 │
│  - S3-compatible REST API           │
│  - AWS Signature v2/v4 auth         │
│  - Bucket & object operations       │
├─────────────────────────────────────┤
│  Core Logic                         │
│  - Bucket management                │
│  - Object management                │
│  - Multi-tenancy                    │
│  - Authentication & authorization   │
├─────────────────────────────────────┤
│  Storage Backend                    │
│  - Filesystem storage               │
│  - SQLite metadata                  │
│  - Object Lock support              │
└─────────────────────────────────────┘
```

## Core Components

### 1. HTTP Layer

**Console Server (Port 8081)**
- Serves embedded Next.js static files
- REST API for web console operations
- JWT-based authentication
- User, bucket, and tenant management

**S3 API Server (Port 8080)**
- Full S3-compatible REST API
- AWS Signature v2/v4 authentication
- Standard S3 operations (Get/Put/Delete/List)
- Multipart uploads
- Presigned URLs
- Object Lock

### 2. Business Logic

**Bucket Manager**
```go
type Manager interface {
    CreateBucket(ctx context.Context, name, tenantID, ownerID string) error
    DeleteBucket(ctx context.Context, name string) error
    ListBuckets(ctx context.Context, tenantID string) ([]*Bucket, error)
    GetBucket(ctx context.Context, name string) (*Bucket, error)
}
```

**Object Manager**
```go
type Manager interface {
    PutObject(ctx context.Context, bucket, key string, data io.Reader) error
    GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error)
    DeleteObject(ctx context.Context, bucket, key string) error
    ListObjects(ctx context.Context, bucket, prefix string, maxKeys int) ([]*Object, error)
}
```

**Multi-Tenancy Manager**
- Tenant isolation
- Quota enforcement (storage, buckets, keys)
- Resource accounting

### 3. Storage Layer

**Filesystem Backend**
- Object storage on local filesystem
- Atomic write operations
- Directory-based bucket organization
- Path: `{data_dir}/objects/{tenant_id}/{bucket}/{object}`

**SQLite Database**
- Metadata storage
- Bucket information
- User credentials (bcrypt hashed)
- Tenant quotas and usage
- Access keys
- Path: `{data_dir}/maxiofs.db`

## Authentication

### Console Authentication
- Username/password login
- JWT tokens (1 hour expiration)
- Stored in localStorage
- Role-based access control (RBAC)

### S3 API Authentication
- Access Key / Secret Key
- AWS Signature v2 and v4
- Compatible with AWS CLI, SDKs, and S3 tools

## Multi-Tenancy

```
Global Admin (No tenant)
    ├── Tenant A
    │   ├── Tenant Admin
    │   ├── Users
    │   ├── Buckets
    │   └── Access Keys
    └── Tenant B
        ├── Tenant Admin
        ├── Users
        ├── Buckets
        └── Access Keys
```

**Resource Isolation**
- Each tenant has isolated resources
- Quota enforcement (storage, buckets, keys)
- No cross-tenant access
- Global admins can manage all tenants

## Data Flow

### Object Upload
```
1. Client → S3 API (PUT /bucket/object)
2. Authentication (AWS Signature)
3. Authorization (tenant/bucket ownership)
4. Quota check (tenant storage limit)
5. Write to filesystem
6. Update metadata in SQLite
7. Update tenant usage counters
8. Return success response
```

### Object Download
```
1. Client → S3 API (GET /bucket/object)
2. Authentication (AWS Signature)
3. Authorization (tenant/bucket access)
4. Read object metadata from SQLite
5. Stream object data from filesystem
6. Return object with headers
```

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

**Alpha Status**
- ⚠️ Single-node only (no clustering)
- ⚠️ Filesystem backend only
- ⚠️ No object versioning (placeholder only)
- ⚠️ No automatic compression
- ⚠️ No encryption at rest
- ⚠️ No data replication
- ⚠️ Limited metrics

**Performance**
- Not validated in production
- No load testing performed
- Benchmarks are preliminary only

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
- `GET /metrics` - Prometheus metrics (basic)

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

**Note**: This is an alpha project. Architecture may change based on feedback and requirements.
