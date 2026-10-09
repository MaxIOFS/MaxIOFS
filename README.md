<div align="center">

# MaxIOFS

**Self-hosted S3-compatible object storage — single binary, batteries included**

[![Build](https://github.com/MaxioFS/MaxioFS/actions/workflows/main.yml/badge.svg)](https://github.com/MaxioFS/MaxioFS/actions/workflows/main.yml)
[![Version](https://img.shields.io/badge/version-1.7.0-blue)](https://github.com/MaxioFS/MaxioFS/releases/tag/v1.7.0)
[![Downloads](https://img.shields.io/github/downloads/MaxIOFS/MaxIOFS/total)](https://github.com/MaxIOFS/MaxIOFS/releases)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26+-00ADD8?logo=go)](https://go.dev)
[![S3 Compatible](https://img.shields.io/badge/S3-100%25%20compatible-orange)](docs/API.md)
[![Security Audited](https://img.shields.io/badge/security-audited-brightgreen)](docs/SECURITY.md)

[Quick Start](#-quick-start) · [Documentation](docs/) · [Changelog](CHANGELOG.md) · [Website](https://maxiofs.com)

<br/>

<a href="https://www.paypal.com/donate/?hosted_button_id=JN4GCXUFVPT52">
  <img src="https://www.paypalobjects.com/en_US/i/btn/btn_donate_LG.gif" alt="Donate with PayPal"/>
</a>

</div>

---

MaxIOFS is a high-performance, S3-compatible object storage server written in Go. It ships as a **single binary** with an embedded React web console, a Pebble LSM-tree metadata engine, and native multi-tenancy — no external databases, no separate console process, no cloud account required.

## Why MaxIOFS?

Most S3-compatible servers give you object storage. MaxIOFS gives you object storage **plus** everything a real deployment needs out of the box: user management, multi-tenancy, SSO, audit logs, background integrity verification, and a full web console — all in one 20 MB binary.

---

## How MaxIOFS compares

> Honest comparison — no marketing claims. Choose based on your actual requirements.
>
> **AIStor** is MinIO's product; MinIO Community Edition is no longer maintained,
> so AIStor is the MinIO compared here. Its **Free** tier has no capacity limit
> and no cost, but runs on a single node and needs a licence file; multi-node,
> high availability and replication are **Enterprise**. **Garage** is a
> lightweight store for geo-distributed clusters. **Ceph** is compared through
> its S3 gateway (RGW).

| Feature | MaxIOFS | AIStor | Garage | Ceph (RGW) |
|---------|---------|--------|--------|------------|
| **Licence** | MIT — use, modify and redistribute freely | Proprietary; licence file required, no redistribution | AGPL-3.0 | LGPL |
| **Deployment** | Single binary, zero dependencies | Single binary + licence file | Single binary | Monitors, managers, OSDs and RGW daemons, usually through cephadm or Rook |
| **Multi-node cluster / HA** | ✅ Up to 5 nodes tested; 1 to 3 copies of each object, on the nodes with the most free space | ⚠️ Enterprise only; Free is single node | ✅ Copies spread across zones (sites) | ✅ Hundreds of nodes |
| **Erasure coding** | ❌ Not yet — whole copies | ✅ Free: across one node's drives; Enterprise: across nodes | ❌ Replication only | ✅ |
| **Replication to another site or S3 endpoint** | ✅ AWS S3, any S3-compatible endpoint, or another MaxIOFS (realtime, scheduled, batch) | ⚠️ Enterprise only (site, bucket, batch) | ❌ No replication API | ⚠️ Between zones of a multisite configuration |
| **Tiering** (lifecycle transitions to colder storage) | ❌ | ⚠️ Enterprise only | ❌ | ✅ Storage classes and cloud transition |
| **Native multi-tenancy** | ✅ Isolated tenants with quotas, per-tenant users and keys | ❌ Separate deployments per tenant | ❌ | ✅ RGW tenants |
| **Access control** | ✅ AWS IAM protocol — users, versioned policies, roles, `AssumeRole`; bucket policies and ACLs | ✅ IAM-style policies | ⚠️ Per-access-key-per-bucket permissions; no bucket policies or ACLs | ✅ Users, bucket and user policies, roles, ACLs |
| **STS temporary credentials** | ✅ `GetSessionToken`, `AssumeRole`, LDAP and OIDC federation | ✅ | ❌ | ✅ |
| **Web console** | ✅ Embedded | ✅ | ❌ CLI and admin API; third-party web UIs | ✅ Ceph Dashboard |
| **SSO / identity providers** | ✅ LDAP/AD and OAuth2/OIDC, auto-provisioning, group mappings | ✅ LDAP and OIDC | ❌ | ✅ LDAP, Keystone, OIDC |
| **Versioning** | ✅ | ✅ | ❌ | ✅ |
| **Object Lock / WORM** | ✅ COMPLIANCE + GOVERNANCE, legal hold, Veeam B&R validated | ✅ | ❌ | ✅ |
| **Lifecycle rules** | ✅ Expiration of objects and noncurrent versions, expired delete markers, incomplete uploads | ⚠️ Expiration; transitions Enterprise only | ⚠️ Expiration and incomplete uploads only | ✅ Including transitions |
| **Bucket notifications** | ✅ Webhook | ✅ Webhook, Kafka, NATS, Redis and more | ❌ | ✅ HTTP, Kafka, AMQP |
| **Static website hosting** | ✅ Index and error documents, routing rules | ✅ | ⚠️ Index and error documents, no redirects | ✅ |
| **S3 Select** | ✅ SQL on CSV/JSON | ✅ | ❌ | ✅ CSV, JSON, Parquet |
| **Encryption at rest** | ✅ Always on: AES-256-GCM envelope, per-object keys, KEK rotation, recovery bundle | ✅ SSE-S3, SSE-KMS, SSE-C | ⚠️ SSE-C only; disk encryption recommended | ✅ SSE-S3, SSE-KMS, SSE-C |
| **Background integrity check** | ✅ Scrubber recomputes each object's MD5 | ✅ Bitrot detection and healing | ✅ Data scrub | ✅ Deep scrub |
| **Audit logging** | ✅ 20+ event types, console viewer, CSV export, syslog | ✅ Webhook | — | ✅ Operations log |
| **Metrics** | ✅ Prometheus, Grafana dashboard | ✅ Prometheus | ✅ Prometheus | ✅ Prometheus |
| **Support** | Community | Free: community; Enterprise: 24/7, SLA | Community | Community; commercial vendors |
| **Target scale** | Single node to a 5-node cluster | Free: one node; Enterprise: petabytes to exabytes | Small to medium clusters across sites | Large clusters, petabytes and beyond |

**Use MaxIOFS when:** you want one binary with the console, multi-tenancy, SSO,
IAM, Object Lock and replication included, on one node or a few, without a
licence fee. MIT also lets you modify and redistribute it.

**Use AIStor when:** one machine is enough and you want erasure coding across its
drives (Free), or you need petabyte-scale clusters, tiering and a support SLA
(Enterprise). You accept a proprietary licence.

**Use Garage when:** you spread a small cluster over several sites with slow or
unreliable links, on modest hardware, and do not need versioning, Object Lock,
bucket policies or server-side encryption.

**Use Ceph when:** you need large scale, erasure coding across nodes, tiering, or
block and file storage next to object storage, and you have the people to run a
Ceph cluster.

---

## Features

<details>
<summary><strong>S3 API — 100% compatible</strong></summary>

- Core operations: PUT, GET, DELETE, HEAD, LIST (objects and buckets)
- Multipart uploads with spec-compliant ETag (`hex(MD5(raw_binary_parts))-N`)
- Presigned URLs — Signature V4 and V2
- POST presigned URLs — HTML form upload with POST policy validation (expiration, conditions, content-length-range)
- Bucket versioning with delete markers
- Object Lock — COMPLIANCE and GOVERNANCE modes, legal hold, per-version enforcement; retention and legal hold sent with a PUT are stored in the same metadata write as the object
- Bucket policies (S3 JSON policy evaluation engine with full Condition block evaluation)
- CORS — stored and enforced on actual requests, OPTIONS preflight handled before auth
- Lifecycle rules — `Expiration.Days/Date` and `AbortIncompleteMultipartUpload` executed by background worker
- Object tagging, object ACLs, bucket tagging
- Bucket notifications — webhook dispatch after PutObject, DeleteObject, CopyObject, CompleteMultipartUpload
- Static website hosting — subdomain routing, index document, error document, routing rules
- Replication — to AWS S3, any S3-compatible endpoint, or other MaxIOFS instances (realtime, scheduled, batch)
- Server-side encryption (SSE-S3 / AES256) — per-bucket configuration via `GetBucketEncryption`/`PutBucketEncryption`, `x-amz-server-side-encryption` response headers on GET/PUT/HEAD
- Server access logging — async delivery to target bucket in AWS S3 access log format (`GetBucketLogging`/`PutBucketLogging`)
- `PublicAccessBlock` — stored and enforced; `IgnorePublicAcls`/`RestrictPublicBuckets` deny all public ACL access when set
- `OwnershipControls` — `GET/PUT/DELETE ?ownershipControls`; default `BucketOwnerEnforced`; prevents AWS SDK v2 `OwnershipControlsNotFoundError`
- `RestoreObject` — `POST ?restore`; accepts `<RestoreRequest>` XML; returns `x-amz-restore` header on HEAD/GET; tools with Glacier lifecycle rules (Veeam, NetBackup) work without errors
- `SelectObjectContent` — `POST ?select`; run SQL queries (SELECT, WHERE, GROUP BY, ORDER BY, aggregates) on CSV and JSON Lines objects; streams results using the Amazon Event Stream binary protocol
- `GetObjectAttributes` — lightweight object metadata (ETag, size, storage class, parts) without downloading the object body
- Conditional writes — `PutObject If-None-Match: *` returns 412 if the object already exists (atomic create-if-absent)
- Object search & filters — content-type, size range, date range, tags
- AWS IAM and STS protocols on the S3 endpoint — `aws iam` and `aws sts` work with `--endpoint-url`
- Works with `aws s3`, `aws s3api`, the `mc` client, and the S3 SDKs

</details>

<details>
<summary><strong>Web Console</strong></summary>

- **Bucket browser** — AWS S3-style interface with breadcrumb navigation, folder tree, and drag-and-drop upload
- **Object detail view** — full metadata page per object with three tabs: Properties (S3 URI, ARN, ETag, size, content type, custom metadata), Permissions (ACL owner and grants), and Versions (version history with delete markers)
- **Actions toolbar** — context-aware dropdown matching AWS S3 conventions: Copy S3 URI, Copy Object URL, Download, Download folder as ZIP, Calculate folder size, Share public link, Generate presigned URL, View versions, Legal Hold toggle, Rename, Edit tags, Delete
- **Version browser** — "Show Versions" toggle replaces the object list with a flat view of every version and delete marker; supports one-click restore (re-promotes an old version or removes a delete marker), and permanent deletion of specific versions
- **Upload files and folders** — multi-file upload with live progress per file, folder upload preserving directory structure
- **Object rename and tag editing** — rename objects and manage key/value tag sets directly from the console
- **Sliding-window sessions** — token window resets on every API call, preventing silent logout during active work; idle logout is suspended during active uploads
- **Object search & filters** — filter by content-type, size range, date range, and tags
- Responsive layout — works on 1080p through 4K displays
- **Multilingual UI** — English, Spanish, French, German, Italian, Brazilian Portuguese, Simplified Chinese, Japanese, Russian; language packs loaded on demand, zero impact on initial bundle size

</details>

<details>
<summary><strong>Multi-tenancy</strong></summary>

- Full tenant isolation — each tenant has its own users, access keys, buckets, and quotas
- Storage quotas per tenant, and per bucket by size and object count (exposed to Veeam through SOSAPI capacity)
- Concurrent writes reserve quota room, so two uploads cannot both pass against the same free space; deletes and overwrites that do not grow an object are allowed over the quota
- Per-tenant bandwidth throttling
- Global admin cross-tenant visibility without impersonation
- Per-tenant identity provider routing (by email domain)
- Tenant-scoped bucket permissions with user, group, and tenant-level grants
- User groups — create groups, add members, grant bucket access to an entire group at once
- Cascading deletes with validation

</details>

<details>
<summary><strong>Identity & Access</strong></summary>

- IAM is the authorization model — roles, bucket permissions and attached policies are all IAM policies, evaluated AWS-style (default deny, explicit `Deny` wins)
- Managed policies with versions, inline policies, roles with trust policies and `AssumeRole`; permissions picked from a catalogue in the console
- STS temporary credentials — `GetSessionToken`, `AssumeRole`, session policies, LDAP and OIDC federation
- Local users with roles (global admin, tenant admin, user)
- User groups with scoped membership (global or tenant) and bucket permission grants
- LDAP/AD integration — bind, search filter, group-to-role mappings
- OAuth2/OIDC — Google and Microsoft presets, auto-provisioning via group mappings
- Two-Factor Authentication (TOTP) with QR code enrollment
- JWT sessions with refresh tokens — survive server restarts, shared across cluster nodes
- S3 Signature V4 and V2 for API access
- Access key management (multiple keys per user, per-tenant scope)
- Rate limiting, account lockout, password policies

</details>

<details>
<summary><strong>Security</strong></summary>

- Always-on envelope encryption at rest — AES-256-GCM in 64 KB chunks with tamper detection, a per-object DEK wrapped by a database KEK
- KEK rotation without re-encrypting data; encrypted recovery bundle for disaster recovery
- Multiple internal security audits — all identified vulnerabilities fixed
- SSRF protection on all outbound HTTP (webhooks, log targets, replication endpoints)
- Auth cookies: `Secure` + `SameSite=Strict`
- OAuth2 CSRF state validation
- CORS allowlist (no wildcard)
- Replication credentials encrypted at rest
- Cluster inter-node TLS with auto-generated CA, CSR-based join (CA key never transmitted)
- Inter-node requests signed with HMAC and refused on replay or tampering
- Audit logging — 20+ event types (auth, object ops, admin actions), external syslog forwarding
- Object Lock enforcement in the console UI — locked objects cannot be deleted or bulk-deleted

</details>

<details>
<summary><strong>Cluster & High Availability</strong></summary>

- Multi-node cluster — up to 5 nodes tested
- **Dedicated cluster port 8082** — inter-node coordination fully separated from S3 (8080) and Console (8081)
- Automatic failover and health monitoring
- Synchronous replication: a factor of 2 is a mirror (RAID 1) that keeps writing with one node down; a factor of 3 needs one of its two peers
- A node that comes back is caught up at once with the writes and deletes it missed
- Ciphertext replication with a cluster-shared KEK; retention and legal hold travel with the object
- Anti-entropy, dead-node redistribution, storage-pressure health state
- Elected coordinator for configuration changes; a surviving node takes over when it fails
- HMAC-authenticated inter-node replication
- Every node holds every bucket; each object's copies go to the nodes with the most free space. A factor of 1 keeps one copy (like RAID 0: a node down makes its objects unreadable until it is back)
- 6-entity sync (users, tenants, access keys, bucket permissions, IDP providers, group mappings)
- Tombstone-based deletion sync — prevents entity resurrection in bidirectional sync
- JWT secret cluster sync — sessions valid across all nodes
- A bucket is listed once, whichever node answers

</details>

<details>
<summary><strong>Operations</strong></summary>

- Prometheus metrics endpoint (`/metrics`)
- Pre-built Grafana dashboard (14 panels — latency p50/p95/p99, throughput, storage)
- Background object integrity scrubber — MD5 recomputed from disk vs stored ETag, 24h cycle
- Maintenance mode — toggle read-only via web console, no restart required
- Disk space and tenant quota alerts — SSE notifications + SMTP email on threshold escalation
- External syslog targets — TCP/UDP/TLS, RFC 5424 structured data
- Log level configurable at runtime
- Crash-safe metadata — object commits are fsynced before returning, with one WAL sync shared by concurrent writers; other writes use a per-second WAL sync; an unclean shutdown triggers a non-destructive reconcile
- Interrupted writes are undone or confirmed at the next start, before the server serves traffic
- Offline tools — `maxiofs recover` rebuilds the metadata store from the object files, `maxiofs reconcile` indexes objects on disk that the index lacks, `maxiofs repair-pointers` rebuilds latest-object pointers from versions, `maxiofs migrate-layout` moves objects to the current on-disk layout

</details>

---

## Quick Start

### Docker — one command

```bash
docker run -d \
  --name maxiofs \
  -p 8080:8080 \
  -p 8081:8081 \
  -v maxiofs-data:/data \
  -e MAXIOFS_BOOTSTRAP_ACCESS_KEY=maxiofsadmin \
  -e MAXIOFS_BOOTSTRAP_SECRET_KEY=pick-your-own-secret \
  maxiofs/maxiofs:latest
```

- **Web Console:** http://localhost:8081 — login: `admin` / `admin`
- **S3 API:** http://localhost:8080 — with the key pair above, ready to use

The two `BOOTSTRAP` variables hand the administrator that S3 key pair when the
deployment has none, so a client connects without a trip through the console
first. They apply once: a deployment that already has a key ignores them.

> ⚠️ Change the default password immediately after first login.

> **Cluster deployments**: also expose `-p 8082:8082` and restrict port 8082 to cluster node IPs only. See [CLUSTER.md](docs/CLUSTER.md).

### Docker Compose

```bash
git clone https://github.com/MaxioFS/MaxioFS.git
cd MaxioFS
make docker-up          # Single node
make docker-monitoring  # + Prometheus & Grafana
```

### Binary

```bash
# Download the latest release for your platform
curl -L https://github.com/MaxioFS/MaxioFS/releases/latest/download/maxiofs-linux-amd64 -o maxiofs
chmod +x maxiofs
./maxiofs --data-dir ./data
```

### Test with AWS CLI

First create an S3 access key in the Web Console:

1. Open http://localhost:8081 and log in with `admin` / `admin`
2. Go to the user/access keys section
3. Create a new access key and secret key for S3 clients

```bash
aws configure --profile maxiofs
# AWS Access Key ID: <your-created-access-key>
# AWS Secret Access Key: <your-created-secret-key>
# Default region: us-east-1

aws --profile maxiofs --endpoint-url http://localhost:8080 s3 mb s3://my-bucket
aws --profile maxiofs --endpoint-url http://localhost:8080 s3 cp file.txt s3://my-bucket/
aws --profile maxiofs --endpoint-url http://localhost:8080 s3 ls s3://my-bucket/
```

### Install as a system service

**Debian / Ubuntu**
```bash
sudo dpkg -i maxiofs_1.7.0_amd64.deb
sudo systemctl enable --now maxiofs
```

**RHEL / Rocky / Alma / Fedora**
```bash
sudo rpm -i maxiofs-1.7.0-1.x86_64.rpm
sudo systemctl enable --now maxiofs
```

---

## Build from Source

Go 1.26+ and Node.js 24+ required to build. The resulting binary has no runtime dependencies.

```bash
git clone https://github.com/MaxioFS/MaxioFS.git
cd MaxioFS
make build        # Build for current platform
make build-all    # Cross-compile for Linux, macOS, Windows
make deb          # Build Debian package (Linux only)
make rpm          # Build RPM package (Linux only)
```

---

## Documentation

| Guide | Description |
|-------|-------------|
| [DEPLOYMENT.md](docs/DEPLOYMENT.md) | Production deployment (systemd, nginx, TLS) |
| [CONFIGURATION.md](docs/CONFIGURATION.md) | Full configuration reference |
| [API.md](docs/API.md) | S3 API compatibility matrix |
| [SECURITY.md](docs/SECURITY.md) | Security features and hardening guide |
| [CLUSTER.md](docs/CLUSTER.md) | Multi-node cluster setup |
| [SSO.md](docs/SSO.md) | LDAP and OAuth2/OIDC setup |
| [OPERATIONS.md](docs/OPERATIONS.md) | Day-2 operations runbook |
| [PERFORMANCE.md](docs/PERFORMANCE.md) | Benchmarks and tuning |
| [TESTING.md](docs/TESTING.md) | Test suite, per-package catalogue, benchmarks |
| [DOCKER.md](DOCKER.md) | Docker and Compose reference |

---

## Performance

Measured with `warp`, the S3 benchmarking tool, on a single node (commodity hardware):

| Operation | p95 latency | Concurrency |
|-----------|-------------|-------------|
| PUT | < 10 ms | 50 clients |
| GET | < 13 ms | 100 clients |
| Success rate | > 99.99% | mixed load |

---

## Testing

```bash
go test ./...                          # 4,400+ backend tests (323 files)
cd web/frontend && npm run test        # 112 frontend tests
```

---

## Known Limitations

- **No erasure coding** — single-node data redundancy relies on filesystem/RAID; a cluster keeps whole copies of each object, one per node up to the factor
- **No cloud tiering** — lifecycle rules expire objects but do not tier to cold storage
- **S3 Select compression** — GZIP/BZIP2 compressed input not supported; objects must be stored uncompressed
- **No per-tenant encryption keys** — envelope encryption uses one server KEK (rotatable, per-object DEKs); SSE-C and external KMS/HSM integration are planned
- **Quotas are enforced per node** — writes in flight on different cluster nodes do not see each other, so usage can pass a quota by what was in flight at the same time
- **Cluster tested up to 5 nodes**
- **No SAML** — use OAuth2/OIDC instead
- **No SOC 2 / ISO 27001 certification** — comprehensive internal audit completed

---

## Release History

| Version | Highlights |
|---------|-----------|
| **v1.7.0** *(stable)* | **New on-disk storage layout**: an object is one file named by a digest of its key, so two keys differing only in case, a key and its own name plus a slash, and a key ending in `.metadata` are all distinct — an existing installation migrates on first start. **Interrupted writes are undone or confirmed at the next start**, before the server serves traffic — a killed overwrite, multipart completion or part replacement no longer leaves an object whose size and ETag disagree with what a GET delivers. **Security**: a session policy is now consulted for every resource an operation touches, not only the one in the URL; 21 console mutation routes and 5 IAM routes resolved the wrong tenant; `/debug/pprof/*` answered any authenticated request instead of a global administrator only. The S3 rate limit answers `SlowDown` in an XML document instead of a bare `429`. |
| **v1.6.0** | **IAM is the authorization model** — roles, bucket permissions and attached policies are all IAM policies, evaluated AWS-style (default deny, explicit `Deny` wins); existing permissions are converted once on upgrade. **AWS STS and IAM protocols** on `POST /` of the S3 endpoint, so `aws sts` and `aws iam --endpoint-url` work unmodified; short-lived `ASIA` credentials with optional session policies and federation for headless clients. **Large security pass**: inter-node request forgery and replay, five endpoints that authorized nothing, tenant-boundary escapes, ACLs overruling policies. Shutdown no longer risks a Pebble panic. |
| **v1.5.2** | **Urgent fix: listing pagination lost one object per page of 1,000** (present since the Pebble migration; made backup verify/repair tools see existing files as missing — critical for Veeam/Duplicati targets); **Pebble hard-kill durability** (per-second WAL fsync, synchronous deletes, non-destructive metadata re-indexing after unclean shutdown); `maxiofs repair-pointers` recovery tool; dead-code sweep across backend and frontend. *(v1.5.1 was withdrawn shortly after publication and is not available.)* |
| **v1.5.0** | **Always-on envelope encryption** (per-object DEK + database KEK, AWS SSE-S3 model, multi-format reader for full backward compatibility); encryption **recovery bundle** + `maxiofs recover` offline disaster-recovery CLI (rebuilds metadata from the object files alone); **KEK rotation** without re-encrypting data (background re-wrap worker); **ciphertext HA replication** with a cluster-shared KEK (no decrypt/re-encrypt per hop); **per-tenant bandwidth throttling**; **per-bucket storage quotas** with Veeam SOSAPI capacity exposure; major data-safety fixes (quota-rejected overwrites destroyed the original object, crash window in overwrites, HA replication silently 404ing, falsified replica timestamps causing perpetual anti-entropy churn, silent 200 on failed metadata saves, Windows AV-lock delete orphans); console SSE auto-reconnect |
| **v1.4.2** | Bug fixes & hardening: web console bucket inventory can now be disabled/deleted once enabled; rate-limiter goroutine-leak fix; concurrency & correctness fixes from code audit (quota TOCTOU, versioning metrics race, refresh-token validation, object-name path-traversal, proxy memory use); security hardening (PBKDF2-SHA256 key derivation, access secrets encrypted at rest, CSP/HSTS, request-body caps); dependency updates (Go minor/patch + frontend npm within semver ranges) |
| **v1.4.1** | Security: OAuth tokens no longer embedded in redirect URL (RFC 6819 one-time code exchange); deactivated users now blocked immediately on JWT validation. Multilingual UI — 9 languages (EN/ES/FR/DE/IT/PT/ZH/JA/RU), on-demand loading. Event-driven cluster config sync — immediate fan-out on all mutations. BadgerDB fully removed. Quota enforcement fixed for versioned buckets. 9 bugs fixed from full code audit |
| **v1.4.0** | Role capabilities system (11 capabilities, per-role defaults, per-user overrides, S3 + console enforcement); multipart metadata security fix (request headers leaked into object user metadata); Object Lock retention/legal-hold now honor versionId; versioning metrics consistency; CopyObject tag directives; presigned URL improvements; S3 Select JSON Lines fix; multipart pagination; inventory fixes; lifecycle delete-marker cleanup; 50+ bug fixes |
| **v1.3.0** | HA cluster: write quorum, read fallback with ordered retry, anti-entropy scrubber, dead-node redistribution, storage-pressure health state, stale-node reconciler, inter-node S3 proxy with HMAC auth; cluster join/sync/TLS fixes, session fixes, Object Lock improvements, 40+ bug fixes |
| **v1.2.0** | Pebble v2 metadata engine (auto-migration, configurable cache, Veeam B&R tuning), S3 Select, RestoreObject, OwnershipControls, BucketNotifications webhook delivery, BucketLogging, BucketInventory S3 API, version browser UI, Docker multi-arch, **critical fix**: metadata written since last compaction was silently lost on shutdown, 30+ additional bug fixes |
| **v1.1.0** | AWS S3-style Actions toolbar, object detail view, object rename & tags, folder ZIP download, SigV2 fix, bucket policy Condition enforcement, PublicAccessBlock enforcement, DeleteBucket Object Lock bypass fix, encryption applied globally, 3 data races fixed |
| **v1.0.0** | Complete UI redesign, folder upload, POST presigned URLs, bucket notifications, lifecycle execution, full Veeam B&R compatibility, Object Lock per-version enforcement, 3 security fixes |
| **v1.0.0-rc1** | 28-vulnerability security audit: AES-256-GCM, CSR cluster join, SSRF hardening, static website hosting, frontend bundle −45% |
| **v1.0.0-beta** | Pebble metadata engine, object integrity scrubber, maintenance mode, disk/quota email alerts |
| **v0.9.1** | Tenant isolation hardening (12 fixes), external syslog targets, cluster join UI |
| **v0.9.0** | LDAP/OAuth SSO, tombstone sync, JWT cluster sync |
| **v0.8.0** | Object search & filters, cluster hardening |

[Full changelog →](CHANGELOG.md)

---

## Contributing

Pull requests are welcome. For significant changes, open an issue first to discuss the approach.

```bash
git clone https://github.com/MaxioFS/MaxioFS.git
cd MaxioFS
go test ./...                    # Make sure all tests pass
cd web/frontend && npm run test  # Frontend tests
```

Please keep existing tests passing and add tests for new behavior.

---

## Security

Found a vulnerability? Please report it privately via [GitHub Security Advisories](https://github.com/MaxioFS/MaxioFS/security/advisories/new) rather than opening a public issue.

Default credentials are `admin`/`admin` — **change them immediately in any non-test deployment.**

---

## License

[MIT](LICENSE) © 2024–2026 Aluisco Ricardo / MaxIOFS

---

<div align="center">

**[maxiofs.com](https://maxiofs.com)** · [Issues](https://github.com/MaxioFS/MaxioFS/issues) · [Discussions](https://github.com/MaxioFS/MaxioFS/discussions)

</div>
