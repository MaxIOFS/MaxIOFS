# MaxIOFS Architecture

**Version**: 0.9.1-beta
**S3 Compatibility**: 100%
**Last Updated**: February 19, 2026

## Overview

MaxIOFS is a single-binary S3-compatible object storage system built in Go with an embedded React (Vite) frontend. The architecture emphasizes simplicity, portability, and ease of deployment with tenant-scoped bucket namespaces. **Version 0.9.0-beta adds Identity Provider (IDP) integration** with LDAP/AD and OAuth2/OIDC SSO, plus tombstone-based cluster deletion sync. Multi-node cluster support provides high availability, intelligent routing, and automatic node-to-node replication.

**Testing Status**: Successfully validated with MinIO Warp stress testing (7000+ objects, bulk operations, metadata consistency under load). **100% S3 compatible** with all core operations fully functional. Production-ready for beta testing environments.

## System Architecture

### Single-Node Mode

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

### Multi-Node Cluster Mode (v0.6.0-beta)

```
                 ┌──────────────────┐
                 │  Load Balancer   │
                 │  (HAProxy/Nginx) │
                 └────────┬─────────┘
                          │
        ┌─────────────────┼─────────────────┐
        │                 │                 │
        ▼                 ▼                 ▼
┌───────────────┐ ┌───────────────┐ ┌───────────────┐
│   Node 1      │ │   Node 2      │ │   Node 3      │
│  (Primary)    │ │  (Secondary)  │ │  (Secondary)  │
├───────────────┤ ├───────────────┤ ├───────────────┤
│ Smart Router  │ │ Smart Router  │ │ Smart Router  │
│ - Health      │ │ - Health      │ │ - Health      │
│ - Location    │ │ - Location    │ │ - Location    │
│   Cache (5m)  │ │   Cache (5m)  │ │   Cache (5m)  │
├───────────────┤ ├───────────────┤ ├───────────────┤
│Cluster Manager│ │Cluster Manager│ │Cluster Manager│
│ - CRUD Nodes  │ │ - CRUD Nodes  │ │ - CRUD Nodes  │
│ - Health Chk  │ │ - Health Chk  │ │ - Health Chk  │
├───────────────┤ ├───────────────┤ ├───────────────┤
│Replication Mgr│ │Replication Mgr│ │Replication Mgr│
│ - HMAC Auth   │ │ - HMAC Auth   │ │ - HMAC Auth   │
│ - Tenant Sync │ │ - Tenant Sync │ │ - Tenant Sync │
│ - Object Sync │ │ - Object Sync │ │ - Object Sync │
├───────────────┤ ├───────────────┤ ├───────────────┤
│ Local Storage │ │ Local Storage │ │ Local Storage │
│ - Filesystem  │ │ - Filesystem  │ │ - Filesystem  │
│ - SQLite      │ │ - SQLite      │ │ - SQLite      │
│ - BadgerDB    │ │ - BadgerDB    │ │ - BadgerDB    │
└───────────────┘ └───────────────┘ └───────────────┘
        │                 │                 │
        └─────────────────┼─────────────────┘
              Cluster Replication
           (HMAC-SHA256, node_token)
```

**Cluster Features**:
- **Smart Router**: Automatically routes requests to correct node with health-aware failover
- **Bucket Location Cache**: 5-minute TTL cache reduces latency (5ms vs 50ms)
- **Health Monitoring**: Background checker monitors all nodes every 30 seconds
- **Cluster Replication**: Automatic node-to-node replication with HMAC authentication
- **Tenant Synchronization**: Auto-sync tenant data between nodes every 30 seconds

> **See [CLUSTER.md](CLUSTER.md) for complete cluster documentation**

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

**Cluster Manager** (v0.6.0-beta)
```go
type Manager interface {
    // Cluster initialization
    InitializeCluster(ctx context.Context, nodeName, region string) (string, error)

    // Node management (CRUD)
    AddNode(ctx context.Context, node *Node) error
    UpdateNode(ctx context.Context, nodeID string, updates map[string]interface{}) error
    RemoveNode(ctx context.Context, nodeID string) error
    ListNodes(ctx context.Context) ([]*Node, error)

    // Health monitoring
    CheckNodeHealth(ctx context.Context, nodeID string) (*HealthStatus, error)
    GetHealthHistory(ctx context.Context, nodeID string, limit int) ([]*HealthCheck, error)

    // Bucket location management
    GetBucketLocation(ctx context.Context, bucketName string) (string, error)
    SetBucketLocation(ctx context.Context, bucketName, nodeID string) error
    ClearLocationCache() error
}
```

**Smart Router** (v0.6.0-beta)
- Bucket location resolution with 5-minute TTL cache
- Health-aware request routing with automatic failover
- Internal proxy mode for cross-node requests
- Latency tracking per node

**Replication Manager** (v0.6.0-beta)
- HMAC-SHA256 authentication between nodes
- Automatic tenant synchronization (30s intervals)
- Configurable bucket replication (10s minimum interval)
- Queue-based async processing with retry logic
- Self-replication prevention

**Identity Provider Manager** (v0.9.0-beta)
- LDAP/Active Directory integration with bind authentication
- OAuth2/OIDC support with Google and Microsoft presets
- SSO login flow with auto-provisioning via group-to-role mappings
- Secrets encrypted at rest with AES-256-GCM
- SQLite store for provider configurations and group mappings

**Cluster Sync Managers** (v0.9.0-beta)
- 6 entity sync managers: users, tenants, access keys, bucket permissions, IDP providers, group mappings
- Tombstone-based deletion sync prevents entity resurrection in bidirectional sync
- `cluster_deletion_log` table with 7-day TTL cleanup
- Checksum-based change detection (only syncs when data changes)

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
  ├── audit.db                     ← SQLite audit logs (v0.4.0+)
  ├── cluster.db                   ← SQLite cluster state (v0.6.0-beta)
  ├── replication.db               ← SQLite bucket replication (v0.5.0-beta)
  ├── settings.db                  ← SQLite dynamic settings (v0.4.1-beta)
  └── metrics/                     ← BadgerDB metrics history (v0.4.1-beta)
      ├── MANIFEST
      └── 000001.sst
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

**SQLite Cluster Database** (v0.6.0-beta)
- Cluster configuration and node state
- Tables: `cluster_config`, `cluster_nodes`, `cluster_health_history`, `cluster_deletion_log`
- Stores node endpoints, regions, health status, and latency metrics
- Background health checker updates every 30 seconds
- Tombstone-based deletion log for cluster sync (v0.9.0-beta)
- Path: `{data_dir}/cluster.db`

**SQLite Replication Database** (v0.5.0-beta)
- User bucket replication rules and queue (external S3 endpoints)
- 3 tables: `replication_rules`, `replication_queue`, `replication_status`
- Separate from cluster replication system
- Path: `{data_dir}/replication.db`

**SQLite Settings Database** (v0.4.1-beta)
- Dynamic runtime configuration (no restart required)
- Security settings (rate limits, lockout thresholds, encryption)
- Path: `{data_dir}/settings.db`

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

### Cluster Authentication (v0.6.0-beta)
- **HMAC-SHA256 signatures** for inter-node communication
- Each node has a unique `node_token` (32-byte secure random)
- Request signing: `HMAC-SHA256(node_token, method + path + timestamp + nonce + body)`
- Headers: `X-MaxIOFS-Node-ID`, `X-MaxIOFS-Timestamp`, `X-MaxIOFS-Nonce`, `X-MaxIOFS-Signature`
- Timestamp validation: Max 5-minute clock skew allowed
- Constant-time signature comparison (timing attack prevention)
- No S3 credentials required for node-to-node replication

**Security Benefits**:
- Mutual authentication between cluster nodes
- Replay attack prevention (nonce + timestamp)
- Message integrity verification (signature covers entire request)
- Independent from S3 authentication system

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

### Cluster Request Routing (v0.6.0-beta)

**Scenario**: Client uploads object to bucket located on different node

```
1. Client → Load Balancer → Node 2 (receives request)
2. Node 2 S3 API: PUT /backups/file.txt
3. Authentication: Extract access_key → tenant_id = "tenant-abc123"
4. Smart Router checks bucket location:
   - Cache lookup: "tenant-abc123/backups" → Cache MISS
   - Query cluster.db bucket_locations table
   - Result: Bucket owned by Node 1
   - Update cache with 5-minute TTL
5. Health Check: Verify Node 1 is healthy
6. Internal Proxy: Node 2 forwards request to Node 1
   - HTTP PUT to Node 1 internal endpoint
   - Preserves all S3 headers and authentication
   - Transparent to client
7. Node 1 processes request locally (filesystem write)
8. Node 1 responds to Node 2
9. Node 2 returns response to client
```

**Performance Optimization**: Subsequent requests hit cache (5ms vs 50ms)

### Cluster Replication Flow (v0.6.0-beta)

**Scenario**: Automatic bucket replication from Node 1 to Node 2

```
1. Replication Scheduler (Node 1) checks every 5 seconds:
   - Query cluster_bucket_replication table
   - Find rule: bucket "backups" → Node 2, interval 10s
   - Check last_sync_at: 15 seconds ago (sync needed)

2. Queue Objects for Replication:
   - List all objects in tenant-abc123/backups
   - Insert into cluster_replication_queue table
   - Each object gets a queue entry

3. Replication Worker processes queue:
   - Get object: objectManager.GetObject(tenant-abc123, backups, file.txt)
   - Object is automatically decrypted (if encryption enabled)
   - Read plaintext data into memory buffer

4. Sign Request with HMAC:
   - timestamp = current Unix timestamp
   - nonce = random 16-byte hex string
   - message = "PUT" + "/api/internal/cluster/objects/tenant-abc123/backups/file.txt" + timestamp + nonce + body
   - signature = HMAC-SHA256(node_token, message)

5. Send to Destination Node:
   - HTTP PUT to Node 2: /api/internal/cluster/objects/tenant-abc123/backups/file.txt
   - Headers:
     * X-MaxIOFS-Node-ID: node-1-id
     * X-MaxIOFS-Timestamp: 1702123456
     * X-MaxIOFS-Nonce: a1b2c3d4e5f6g7h8
     * X-MaxIOFS-Signature: 9f8e7d6c5b4a3210...
   - Body: plaintext object data

6. Node 2 receives request:
   - Cluster Auth Middleware validates HMAC signature
   - Lookup node_token for node-1-id in cluster_nodes table
   - Verify signature matches (constant-time comparison)
   - Check timestamp skew < 5 minutes

7. Node 2 stores object:
   - objectManager.PutObject(tenant-abc123, backups, file.txt, reader, size, contentType, metadata)
   - Object is automatically re-encrypted with Node 2's encryption key
   - Filesystem write: /data/objects/tenant-abc123/backups/file.txt

8. Mark as Completed:
   - Update cluster_replication_status table
   - last_replicated_at = current timestamp
   - Remove from queue

9. Update Rule Status:
   - Update cluster_bucket_replication.last_sync_at = current timestamp
   - Next sync will occur in 10 seconds
```

**Key Security Features**:
- HMAC authentication (no S3 credentials exposed)
- Timestamp validation prevents replay attacks
- Nonce ensures request uniqueness
- Encryption handled transparently (decrypt → transfer → re-encrypt)

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
- ⚠️ Filesystem backend only (local/NAS/SAN storage)
- ⚠️ Object versioning (basic implementation, not fully validated)
- ⚠️ No automatic compression
- ⚠️ Limited encryption key management (master key in config, HSM planned for future release)

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

**Planned Features** (See [TODO.md](../TODO.md) for complete roadmap)
- Object versioning (enhanced validation and testing)
- Hardware Security Module (HSM) integration for encryption keys
- Advanced compression algorithms
- Plugin system for extensibility
- Cluster auto-scaling
- Cross-region replication

**Recently Implemented**
- ✅ Identity Provider system with LDAP/AD and OAuth2/OIDC SSO (v0.9.0-beta)
- ✅ Tombstone-based cluster deletion sync (v0.9.0-beta)
- ✅ Multi-node clustering (v0.6.0-beta)
- ✅ Cluster data replication with HA (v0.6.0-beta)
- ✅ Server-Side Encryption at Rest (v0.4.1-beta)
- ✅ Prometheus/Grafana monitoring (v0.3.2-beta)
- ✅ Comprehensive audit logging (v0.4.0-beta)

---

**Note**: This is a beta project. Architecture is stable but may evolve based on production feedback and requirements.
