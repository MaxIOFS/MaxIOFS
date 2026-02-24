# MaxIOFS Architecture

**Version**: 0.9.2-beta | **Last Updated**: February 23, 2026

## Overview

MaxIOFS is a single-binary S3-compatible object storage system built in Go with an embedded React (Vite) frontend. It provides complete multi-tenancy, identity provider integration (LDAP/AD + OAuth2/OIDC SSO), multi-node clustering with automatic replication, and a full-featured web console — all in one binary with zero external dependencies.

## System Architecture

### Single-Node Mode

```
┌──────────────────────────────────────┐
│      Single Binary (maxiofs)         │
├──────────────────────────────────────┤
│  S3 API Server (Port 8080)           │
│  ├─ AWS Signature v2/v4 auth         │
│  ├─ Bucket & object operations       │
│  ├─ Multipart uploads                │
│  ├─ Presigned URLs                   │
│  ├─ Object Lock (WORM)              │
│  └─ Tenant-transparent routing       │
├──────────────────────────────────────┤
│  Console Server (Port 8081)          │
│  ├─ Embedded React (Vite) SPA        │
│  ├─ REST API (~150 endpoints)        │
│  ├─ JWT + OAuth2/OIDC auth           │
│  └─ SSE real-time notifications      │
├──────────────────────────────────────┤
│  Core Services                       │
│  ├─ Multi-tenancy & quota mgmt       │
│  ├─ Identity Providers (LDAP/OAuth)  │
│  ├─ Lifecycle policies               │
│  ├─ Bucket replication (external S3) │
│  ├─ Audit logging                    │
│  ├─ Metrics & monitoring             │
│  └─ Webhook notifications            │
├──────────────────────────────────────┤
│  Storage Layer                       │
│  ├─ Filesystem (objects)             │
│  ├─ Pebble (metadata)               │
│  ├─ SQLite (auth, cluster, audit)    │
│  └─ AES-256-CTR encryption at rest   │
└──────────────────────────────────────┘
```

### Multi-Node Cluster Mode

```
              ┌──────────────────┐
              │  Load Balancer   │
              │  (HAProxy/Nginx) │
              └────────┬─────────┘
                       │
       ┌───────────────┼───────────────┐
       │               │               │
       ▼               ▼               ▼
┌────────────┐  ┌────────────┐  ┌────────────┐
│  Node 1    │  │  Node 2    │  │  Node 3    │
│ (Primary)  │  │(Secondary) │  │(Secondary) │
├────────────┤  ├────────────┤  ├────────────┤
│Smart Router│  │Smart Router│  │Smart Router│
│Health Check│  │Health Check│  │Health Check│
│Replication │  │Replication │  │Replication │
│  Manager   │  │  Manager   │  │  Manager   │
├────────────┤  ├────────────┤  ├────────────┤
│  6 Sync    │  │  6 Sync    │  │  6 Sync    │
│  Managers  │  │  Managers  │  │  Managers  │
├────────────┤  ├────────────┤  ├────────────┤
│Local Storage│ │Local Storage│ │Local Storage│
│FS+Pebble  │  │FS+Pebble  │  │FS+Pebble  │
│  +SQLite   │  │  +SQLite   │  │  +SQLite   │
└────────────┘  └────────────┘  └────────────┘
       │               │               │
       └───────────────┼───────────────┘
            HMAC-SHA256 Authenticated
             Cluster Replication
```

> Complete cluster documentation: [CLUSTER.md](CLUSTER.md)

---

## Core Packages

### HTTP Layer

| Package | Purpose |
|---------|---------|
| `internal/server` | Console HTTP server, REST API routes (~150 endpoints), SPA serving |
| `pkg/s3compat` | S3-compatible HTTP handler (bucket/object/multipart/presigned/ACL) |
| `internal/api` | Security handlers, request validation |
| `internal/middleware` | JWT auth, HMAC cluster auth, rate limiting, tracing, CORS |

### Business Logic

| Package | Purpose |
|---------|---------|
| `internal/auth` | User/access key management, bcrypt passwords, RBAC, S3 signatures, TOTP 2FA |
| `internal/bucket` | Bucket CRUD, policy evaluation, tenant-scoped operations |
| `internal/object` | Object CRUD, versioning, retention, tagging, multipart uploads |
| `internal/acl` | S3-compatible ACLs (canned + custom), permission evaluation |
| `internal/presigned` | Presigned URL generation/validation (S3-compatible paths) |
| `internal/share` | Share link management (time-limited public access) |
| `internal/lifecycle` | S3 lifecycle policies (expiration, transitions, abort incomplete) |
| `internal/notifications` | Webhook notifications (ObjectCreated, ObjectRemoved, ObjectRestored) |
| `internal/inventory` | S3 Inventory reports generation |
| `internal/idp` | Identity Provider management (LDAP/AD, OAuth2/OIDC), AES-256-GCM secrets |
| `internal/logging` | Configurable log outputs (stdout, HTTP webhook, syslog) |
| `internal/audit` | Immutable audit logging (20+ event types, SQLite storage) |
| `internal/metrics` | Prometheus metrics, system metrics, performance history (Pebble) |
| `internal/settings` | Dynamic runtime configuration (no restart required) |
| `internal/config` | Static configuration (YAML, env vars, CLI flags) |

### Cluster

| Package | Purpose |
|---------|---------|
| `internal/cluster` | Cluster manager, smart router, health checker, bucket location cache |
| `internal/cluster` | 6 sync managers: users, tenants, access keys, bucket permissions, IDP providers, group mappings |
| `internal/cluster` | Tombstone-based deletion sync, circuit breaker, rate limiter |
| `internal/cluster` | Bucket migration between nodes, replication queue/workers |
| `internal/replication` | External S3 replication (user-configured, separate from cluster) |

### Storage & Data

| Package | Purpose |
|---------|---------|
| `internal/storage` | Filesystem backend (tenant-scoped directories) |
| `internal/metadata` | Pebble metadata store (objects, buckets, versions, locks, tags) |
| `internal/db` | SQLite database management |
| `pkg/encryption` | AES-256-CTR streaming encryption at rest |

### Frontend

| Package | Purpose |
|---------|---------|
| `web` | Embedded React SPA (go:embed) |
| `web/frontend` | React 19 + TypeScript + Vite 7 + TailwindCSS 4 + TanStack Query v5 |

---

## Storage Layer

### Directory Structure

```
{data_dir}/
├── db/
│   ├── maxiofs.db          ← SQLite: auth, users, tenants, access keys,
│   ├── maxiofs.db-wal         settings, cluster config, replication rules,
│   └── maxiofs.db-shm         IDP providers, group mappings
├── audit.db                ← SQLite: immutable audit logs (separate for isolation)
├── metadata/               ← Pebble: object metadata, versions, locks, tags
│   ├── *.sst              ←   Sorted String Tables (LSM levels)
│   ├── MANIFEST-*         ←   Version manifest
│   ├── OPTIONS-*          ←   Engine options snapshot
│   └── WAL/               ←   Write-Ahead Log (crash safety)
└── objects/                ← Filesystem: actual object data
    ├── .maxiofs/           ←   Internal storage metadata
    ├── tenant-{hash}/      ←   Tenant-scoped directories
    │   ├── bucket-a/
    │   │   ├── file1.txt
    │   │   └── dir/file2.pdf
    │   └── bucket-b/
    ├── global-bucket/      ←   Global admin buckets (no tenant prefix)
    └── ...
```

### Database Responsibilities

| Database | Technology | Contents |
|----------|-----------|----------|
| `db/maxiofs.db` | SQLite (WAL mode) | Users, tenants, access keys, sessions, dynamic settings, cluster config, cluster nodes, replication rules, replication queue, bucket permissions, IDP providers, group mappings, deletion log, migrations |
| `audit.db` | SQLite | Immutable audit trail (authentication, CRUD, security events). Separate for isolation and retention management |
| `metadata/` | Pebble v1.1 | Object metadata (ETags, content-type, size), versioning info, object lock/retention, bucket configurations, tags, ACLs, multipart upload state |
| `objects/` | Filesystem | Raw object data, organized by tenant and bucket |

---

## Multi-Tenancy

### Hierarchy

```
Global Admin (no tenant)
├── Tenant A (tenant-{hash})
│   ├── Tenant Admin(s)
│   ├── Users
│   ├── Buckets (globally unique names)
│   ├── Access Keys
│   └── IDP Providers (optional)
└── Tenant B (tenant-{hash})
    ├── Tenant Admin(s)
    ├── Users
    ├── Buckets
    ├── Access Keys
    └── IDP Providers (optional)
```

### Roles (RBAC)

| Role | Scope | Capabilities |
|------|-------|-------------|
| **Global Admin** | System-wide | All operations, cluster management, all tenants |
| **Tenant Admin** | Single tenant | Manage users, buckets, access keys within tenant |
| **User** | Single tenant | Create buckets, upload/download objects, manage own keys |
| **Read-Only** | Single tenant | View buckets and download objects only |
| **Guest** | Single tenant | Minimal read access |

### Global Bucket Uniqueness

Bucket names are **globally unique** across all tenants (AWS S3 compatible):

- Tenant A creates "backups" → OK
- Tenant B tries to create "backups" → **Rejected** (name already taken)
- S3 clients see standard URLs: `http://endpoint/backups/file.txt`
- Backend transparently resolves: `access_key → user → tenant_id → tenant-{hash}/backups/file.txt`

### Quota Enforcement

| Quota | Enforcement | Error |
|-------|------------|-------|
| Storage (bytes) | Checked before every upload (S3 API + Console) | 403 Quota Exceeded |
| Buckets (count) | Checked on bucket creation | 403 Quota Exceeded |
| Access Keys (count) | Checked on key generation | 403 Quota Exceeded |

### Resource Isolation

- Each tenant has isolated filesystem directories
- API responses automatically filtered by tenant
- Zero cross-tenant visibility
- Global admins can access all tenants

---

## Authentication

MaxIOFS supports **four authentication methods**:

| Method | Use Case | Mechanism |
|--------|----------|-----------|
| **JWT** | Web Console | Username/password + optional 2FA → JWT token (24h default) |
| **OAuth2/OIDC** | SSO Login | Google, Microsoft, or custom OIDC → auto-provisioning via group mappings |
| **S3 Signatures** | S3 API | Access Key + Secret Key with AWS Signature v2/v4 |
| **HMAC-SHA256** | Cluster sync | Node token-based inter-node authentication |

> Complete details: [SECURITY.md](SECURITY.md) | SSO guide: [SSO.md](SSO.md)

---

## Data Flow

### Object Upload (S3 API)

```
1. Client → S3 API: PUT /my-bucket/file.txt (AWS Signature v4)
2. Auth: Extract access_key → lookup user → get tenant_id
3. Bucket resolution: "my-bucket" → metadata lookup → tenant-{hash}/my-bucket
4. Authorization: Verify user has write access to bucket
5. Quota check: Verify tenant storage limit
6. Optional: Encrypt data (if encryption enabled, AES-256-CTR)
8. Write to filesystem: {data_dir}/objects/tenant-{hash}/my-bucket/file.txt
9. Store metadata in Pebble: ETag, size, content-type, timestamps
10. Update bucket metrics: IncrementObjectCount
11. Optional: Trigger webhook notifications (ObjectCreated)
12. Optional: Queue for cluster replication
13. Return success to client
```

### Cluster Request Routing

```
1. Client → Load Balancer → Node 2 (receives request)
2. Node 2: Authenticate request, resolve tenant
3. Smart Router: Check bucket location in cache (5-min TTL)
4. Cache MISS → Query SQLite bucket_locations → Bucket on Node 1
5. Health check: Verify Node 1 is healthy
6. Internal proxy: Forward request to Node 1 (preserving all headers)
7. Node 1: Process locally → Return response → Node 2 → Client
8. Cache updated: Subsequent requests hit cache (5ms vs 50ms)
```

---

## Technology Stack

| Component | Technology | Version |
|-----------|-----------|---------|
| **Language** | Go | 1.25+ |
| **Frontend** | React + TypeScript | React 19, Vite 7 |
| **CSS** | TailwindCSS | 4 (Oxide) |
| **State Management** | TanStack Query | v5 |
| **Routing** | React Router | v7 |
| **Metadata Store** | Pebble | v1.1 |
| **Relational Store** | SQLite | WAL mode (via modernc.org/sqlite, no CGO) |
| **HTTP Router** | Gorilla Mux | v1.8 |
| **S3 Auth** | AWS SDK v2 | Signature v2/v4 |
| **LDAP** | go-ldap | v3 |
| **OAuth2** | golang.org/x/oauth2 | latest |
| **Metrics** | Prometheus client_golang | latest |
| **Testing** | Go testing + Vitest | standard |

## Current Limitations

- ⚠️ Filesystem backend only (local/NAS/SAN storage)
- ⚠️ Single master encryption key for all tenants
- ⚠️ No SAML SSO (OAuth2/OIDC recommended instead)
- ⚠️ No per-tenant rate limiting (global only)
- ⚠️ Not validated at high scale (100+ concurrent users, 100+ tenants)
- ⚠️ No external log shipping (syslog planned)

---

**See also**: [API.md](API.md) · [CLUSTER.md](CLUSTER.md) · [CONFIGURATION.md](CONFIGURATION.md) · [DEPLOYMENT.md](DEPLOYMENT.md) · [SECURITY.md](SECURITY.md) · [SSO.md](SSO.md) · [TESTING.md](TESTING.md) · [PERFORMANCE.md](PERFORMANCE.md)
