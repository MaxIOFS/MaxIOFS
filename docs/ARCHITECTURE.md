# MaxIOFS Architecture

**Version**: 1.7.0 | **Last Updated**: September 27, 2026

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
│  ├─ Webhook notifications            │
│  ├─ Maintenance mode (read-only)     │
│  └─ Object integrity verification    │
├──────────────────────────────────────┤
│  Storage Layer                       │
│  ├─ Filesystem (objects)             │
│  ├─ Pebble (metadata)               │
│  ├─ SQLite (auth, cluster, audit)    │
│  └─ AES-256-GCM encryption at rest   │
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
       HMAC-SHA256 Authenticated Cluster
      Replication — dedicated port :8082
        (TLS via internal CA; only S3
       8080 / Console 8081 face the LB)
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
| `internal/auth` | User/access key management, bcrypt passwords, IAM policy engine and roles, STS, S3 signatures, TOTP 2FA |
| `internal/bucket` | Bucket CRUD, policy evaluation, tenant-scoped operations |
| `internal/object` | Object CRUD, versioning, retention, tagging, multipart uploads, quota reservations |
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
| `internal/bandwidth` | Per-tenant transfer throttling |

### Cluster

| Package | Purpose |
|---------|---------|
| `internal/cluster` | Cluster manager, smart router, health checker, bucket location cache |
| `internal/cluster` | 6 sync managers: users, tenants, access keys, bucket permissions, IDP providers, group mappings |
| `internal/cluster` | Tombstone-based deletion sync, circuit breaker, rate limiter |
| `internal/cluster` | Membership, health, object placement and copies between nodes, replication queue/workers |
| `internal/cluster` | HA object manager: quorum writes, local rollback on quorum failure, anti-entropy |
| `internal/clusterauth` | Inter-node request signature definition |
| `internal/transfer` | Progress-based stall watchdog for requests that carry object data |
| `internal/replication` | External S3 replication (user-configured, separate from cluster) |

### Storage & Data

| Package | Purpose |
|---------|---------|
| `internal/storage` | Filesystem backend (layout v2: one directory per bucket, objects named by a digest of the key) |
| `internal/metadata` | Pebble metadata store (objects, buckets, versions, locks, tags) |
| `internal/db` | SQLite database management |
| `internal/kek` | Key Encryption Key store, rotation, cluster key, recovery bundle |
| `internal/rollback` | Retained copies of in-place writes; undo of interrupted writes at startup |
| `internal/recovery` | Metadata rebuild and reconcile from the object files (`maxiofs recover`, `maxiofs reconcile`) |
| `internal/layout` | Migration of an existing storage tree to the current layout |
| `internal/bgwork` | Background goroutine ownership for components (tracked stop) |
| `pkg/encryption` | AES-256-GCM authenticated encryption at rest |

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
└── objects/                ← Filesystem: object data (layout v2)
    ├── .maxiofs-layout     ←   On-disk layout version
    ├── .maxiofs/multipart/parts/{uploadID}/{00001}
    │                       ←   Uploaded parts until completion
    ├── maxiofs-mpu-backup-*   ← Retained copies of in-place writes, with a
    ├── maxiofs-part-backup-*    .json manifest; settled at the next start
    ├── backups/            ←   One directory per bucket (names are global)
    │   ├── .maxiofs-bucket ←     Marker: tenant-qualified bucket path
    │   ├── 3f/a2/3fa2…c9   ←     Object: SHA-256 of the key (of key and
    │   │                          version ID for a stored version)
    │   └── 3f/a2/3fa2…c9.metadata
    │                       ←     Sidecar: size, etag, content-type,
    │                              encryption fields (wrapped DEK)
    └── ...
```

The key never reaches the filesystem, so keys differing only in case, a key and
the same key with a trailing slash, and a key ending in `.metadata` are all
distinct objects. An installation on the previous layout is migrated on first
start; `maxiofs migrate-layout --dry-run` previews the move.

Every object file has a `.metadata` **sidecar** next to it holding everything
needed to reconstruct its metadata entry (including the encryption envelope) —
this is what makes filesystem-only disaster recovery (`maxiofs recover`)
possible. Writes commit in two phases: the new sidecar is staged at
`<object>.metadata-staging`, the data file is renamed into place, then the
staged sidecar replaces the final one. A crash at any point is resolved
deterministically on the next access (roll forward when the stored bytes match
the staged etag, roll back otherwise), so an interrupted overwrite can never
leave a sidecar that does not match its data. A leftover `.metadata-staging`
file after a hard crash is therefore normal and self-heals.

**Metadata durability**: object and multipart-part commits fsync before returning,
so their rollback copies can be discarded only after the new index entry is durable.
Object and version transactions release the bucket mutation lock after publication,
then wait for a WAL sync barrier. Concurrent writers can share Pebble's group commit
without returning success before durability or serializing disk waits per bucket.
Destructive operations (object and bucket deletes, multipart complete/abort) also
fsync immediately. Other asynchronous writes use the periodic WAL sync loop.
Startup settles retained overwrite copies before serving traffic; copies without
an index entry remain available for operator review, without restoring deleted objects.
The store writes a `CLEAN_SHUTDOWN` sentinel on close; when a
boot finds it missing, the server reconciles Pebble against the on-disk object
tree in the background (re-indexing sidecar pairs whose metadata commit was
lost, without pruning metadata or sidecars based on missing paths, recalculating bucket stats)
while continuing to serve traffic. Repairs share the object-key lock with reads,
writes, deletes and encryption migration.

### Database Responsibilities

| Database | Technology | Contents |
|----------|-----------|----------|
| `db/maxiofs.db` | SQLite (WAL mode) | Users, tenants, access keys, sessions, dynamic settings, cluster config, cluster nodes, replication rules, replication queue, bucket permissions, IDP providers, group mappings, deletion log, migrations |
| `audit.db` | SQLite | Immutable audit trail (authentication, CRUD, security events). Separate for isolation and retention management |
| `metadata/` | Pebble v2.1 | Object metadata (ETags, content-type, size), versioning info, object lock/retention, bucket configurations, tags, ACLs, multipart upload state |
| `objects/` | Filesystem | Encrypted object data and sidecars, one directory per bucket |

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

### Roles

Since 1.6.0 each role is a set of IAM policies; bucket permissions and attached
policies are evaluated with them AWS-style (default deny, explicit `Deny` wins).
See [SECURITY.md](SECURITY.md#iam).

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
- Backend transparently resolves: `access_key → user → tenant_id → bucket path tenant-{id}/backups` in the metadata; on disk the bucket is `objects/backups/`
- Buckets created by a global admin can be global buckets with no tenant owner; their bucket path has no tenant prefix and they remain visible to global admins.

### Quota Enforcement

| Quota | Enforcement | Error |
|-------|------------|-------|
| Tenant storage (bytes) | Reserved before every PUT and multipart completion (S3 API + Console) | 403 QuotaExceeded |
| Bucket storage (bytes) | Reserved before every PUT and multipart completion | 403 QuotaExceeded |
| Bucket objects (count) | Reserved when a write creates a new key | 403 QuotaExceeded |
| Buckets (count) | Checked on bucket creation | 403 QuotaExceeded |
| Access Keys (count) | Checked on key generation | 403 QuotaExceeded |

A write holds its room from before it stores anything until its usage is
committed, and the check counts every earlier write still in flight, so
concurrent writes on a node cannot pass against the same free space. Only
growth is checked: deletes, delete markers and overwrites that do not grow an
object are allowed over the quota. Reservations are per node; writes in flight
on different cluster nodes do not see each other.

### Resource Isolation

- Each bucket has its own directory; its marker records the owning tenant
- API responses automatically filtered by tenant
- Zero cross-tenant visibility
- Global admins can access all tenants and global buckets

---

## Authentication

MaxIOFS supports **five authentication methods**:

| Method | Use Case | Mechanism |
|--------|----------|-----------|
| **JWT** | Web Console | Username/password + optional 2FA → JWT token (24h default) |
| **OAuth2/OIDC** | SSO Login | Google, Microsoft, or custom OIDC → auto-provisioning via group mappings |
| **S3 Signatures** | S3 API | Access Key + Secret Key with AWS Signature v2/v4 |
| **STS** | S3 API | Temporary `ASIA` key + session token from `GetSessionToken`/`AssumeRole`, optional session policy (SigV4 only) |
| **HMAC-SHA256** | Cluster sync | Node token-based inter-node authentication |

> Complete details: [SECURITY.md](SECURITY.md) | SSO guide: [SSO.md](SSO.md)

---

## Data Flow

### Object Upload (S3 API)

```
1. Client → S3 API: PUT /my-bucket/file.txt (AWS Signature v4)
2. Auth: access key → user → tenant
3. Bucket resolution: "my-bucket" → bucket path tenant-{id}/my-bucket
4. Authorization: IAM policy evaluation of s3:PutObject on the bucket
5. Tenant quota pre-check on the declared size
6. Object Lock: lock headers validated, or the bucket default retention computed
7. Quota reservation: bucket and tenant room held for the write in flight
8. Encrypt: plaintext staged, then encrypted with a new per-object DEK (AES-256-GCM)
9. Write to filesystem: objects/my-bucket/ab/cd/<sha256(key)> + .metadata sidecar
   (two-phase commit); an overwrite retains the previous copy until step 10
10. Commit metadata in Pebble (ETag, size, retention, legal hold), fsynced
11. Release the reservation; update bucket metrics and tenant usage
12. Cluster (replication factor 2 or 3): replicate to the healthy peers; a peer
    that misses the write is caught up when it is back. A factor of 3 needs one
    peer to confirm, or the new version is removed locally and the request
    answers 503
13. Optional: Trigger webhook notifications (ObjectCreated)
14. Return success to client
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
| **Language** | Go | 1.26+ |
| **Frontend** | React + TypeScript | React 19, Vite 7 |
| **CSS** | TailwindCSS | 4 (Oxide) |
| **State Management** | TanStack Query | v5 |
| **Routing** | React Router | v7 |
| **Metadata Store** | Pebble | v2.1 |
| **Relational Store** | SQLite | WAL mode (via modernc.org/sqlite, no CGO) |
| **HTTP Router** | Gorilla Mux | v1.8 |
| **S3 Auth** | AWS SDK v2 | Signature v2/v4 |
| **LDAP** | go-ldap | v3 |
| **OAuth2** | golang.org/x/oauth2 | latest |
| **Metrics** | Prometheus client_golang | latest |
| **Testing** | Go testing + Vitest | standard |

## Current Limitations

- ⚠️ Filesystem backend only (local/NAS/SAN storage)
- ⚠️ One KEK for all tenants (rotatable; each object has its own DEK)
- ⚠️ Quotas enforced per node: writes in flight on different nodes do not see each other
- ⚠️ No SAML SSO (OAuth2/OIDC recommended instead)
- ⚠️ No per-tenant rate limiting (global only)
- ⚠️ Not validated at high scale (100+ concurrent users, 100+ tenants)
- ⚠️ External log shipping via syslog/HTTP (multiple targets configurable)

---

**See also**: [API.md](API.md) · [CLUSTER.md](CLUSTER.md) · [CONFIGURATION.md](CONFIGURATION.md) · [DEPLOYMENT.md](DEPLOYMENT.md) · [OPERATIONS.md](OPERATIONS.md) · [SECURITY.md](SECURITY.md) · [SSO.md](SSO.md) · [TESTING.md](TESTING.md) · [PERFORMANCE.md](PERFORMANCE.md)
