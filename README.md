<div align="center">

# MaxIOFS

**Self-hosted S3-compatible object storage — single binary, batteries included**

[![Build](https://github.com/MaxioFS/MaxioFS/actions/workflows/main.yml/badge.svg)](https://github.com/MaxioFS/MaxioFS/actions/workflows/main.yml)
[![Version](https://img.shields.io/badge/version-1.0.0-blue)](https://github.com/MaxioFS/MaxioFS/releases/tag/v1.0.0)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.25+-00ADD8?logo=go)](https://go.dev)
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

## MaxIOFS vs MinIO

> Honest comparison — no marketing claims. Both are open-source S3-compatible servers written in Go. Choose based on your actual requirements.

| Feature | MaxIOFS | MinIO |
|---------|---------|-------|
| **Deployment** | Single binary, zero dependencies | Single binary, zero dependencies |
| **Web console** | Embedded, full-featured | Embedded (since AGPL rewrite) |
| **Native multi-tenancy** | ✅ Built-in — isolated tenants with quotas, per-tenant users and access keys, cross-tenant admin visibility | ❌ No native multi-tenancy — requires separate deployments per tenant or commercial AIStor |
| **User management** | ✅ Built-in users, roles, 2FA, lockout policies, rate limiting | ✅ Built-in (IAM-style) |
| **SSO / Identity Providers** | ✅ LDAP/AD + OAuth2/OIDC (Google, Microsoft) with auto-provisioning and group mappings | ✅ LDAP + OIDC (config-file based) |
| **Audit logging** | ✅ 20+ event types, filterable viewer, full CSV export, external syslog targets | ✅ Available (webhook-based) |
| **Object integrity** | ✅ Background scrubber — MD5 recomputed per object, per-bucket and cluster-wide | ✅ `mc admin heal` (distributed setups) |
| **Object Lock / WORM** | ✅ COMPLIANCE + GOVERNANCE, per-version enforcement, Veeam B&R validated | ✅ COMPLIANCE + GOVERNANCE |
| **Bucket notifications** | ✅ Webhook dispatch after every mutating operation | ✅ Webhook + Kafka + NATS + Redis... |
| **Static website hosting** | ✅ Subdomain routing, index/error documents, routing rules | ✅ |
| **Lifecycle rules** | ✅ Expiration + AbortIncompleteMultipart executed | ✅ More rule types (transitions) |
| **Replication** | ✅ To AWS S3, MinIO, or other MaxIOFS nodes | ✅ Active-active, multi-site |
| **Erasure coding** | ❌ Not supported | ✅ Core feature (distributed mode) |
| **Data tiering** | ❌ Not supported | ✅ ILM tiering to cloud |
| **S3 Select** | ❌ Not supported | ✅ Supported |
| **Encryption** | ✅ AES-256-GCM authenticated (server-side, 64 KB chunks), SSE-S3 headers | ✅ SSE-S3 / SSE-KMS / SSE-C |
| **PublicAccessBlock** | ✅ Stored and enforced on every request | ✅ Supported |
| **Server access logging** | ✅ Async delivery to target bucket in AWS S3 format | ✅ Supported |
| **Prometheus metrics** | ✅ `/metrics` + pre-built Grafana dashboard | ✅ |
| **Maintenance mode** | ✅ Read-only mode via console toggle | ❌ |
| **SMTP alerting** | ✅ Disk + quota threshold alerts via email | ❌ (external alertmanager needed) |
| **Metadata engine** | Pebble (CockroachDB LSM-tree, pure Go, crash-safe WAL) | Custom (etcd-based in distributed) |
| **License** | MIT | AGPL-3.0 (open source) / commercial |
| **Target scale** | Small to mid-range (single node to 5-node cluster) | Petabyte-scale distributed |

**Use MaxIOFS when:** you need multi-tenancy, built-in SSO, and a full web console without running multiple services, and your scale fits on a few nodes.

**Use MinIO when:** you need erasure coding, petabyte-scale distributed storage, S3 Select, or cloud tiering.

---

## Features

<details>
<summary><strong>S3 API — 100% compatible</strong></summary>

- Core operations: PUT, GET, DELETE, HEAD, LIST (objects and buckets)
- Multipart uploads with spec-compliant ETag (`hex(MD5(raw_binary_parts))-N`)
- Presigned URLs — Signature V4 and V2
- POST presigned URLs — HTML form upload with POST policy validation (expiration, conditions, content-length-range)
- Bucket versioning with delete markers
- Object Lock — COMPLIANCE and GOVERNANCE modes, per-version enforcement
- Bucket policies (S3 JSON policy evaluation engine)
- CORS — stored and enforced on actual requests, OPTIONS preflight handled before auth
- Lifecycle rules — `Expiration.Days/Date` and `AbortIncompleteMultipartUpload` executed by background worker
- Object tagging, object ACLs, bucket tagging
- Bucket notifications — webhook dispatch after PutObject, DeleteObject, CopyObject, CompleteMultipartUpload
- Static website hosting — subdomain routing, index document, error document, routing rules
- Replication — to AWS S3, MinIO, or other MaxIOFS instances (realtime, scheduled, batch)
- Server-side encryption (SSE-S3 / AES256) — per-bucket configuration via `GetBucketEncryption`/`PutBucketEncryption`, `x-amz-server-side-encryption` response headers on GET/PUT/HEAD
- Server access logging — async delivery to target bucket in AWS S3 access log format (`GetBucketLogging`/`PutBucketLogging`)
- `PublicAccessBlock` — stored and enforced; `IgnorePublicAcls`/`RestrictPublicBuckets` deny all public ACL access when set
- `GetObjectAttributes` — lightweight object metadata (ETag, size, storage class, parts) without downloading the object body
- Conditional writes — `PutObject If-None-Match: *` returns 412 if the object already exists (atomic create-if-absent)
- Object search & filters — content-type, size range, date range, tags
- `aws s3`, `aws s3api`, MinIO Client (`mc`), and S3 SDK compatible

</details>

<details>
<summary><strong>Multi-tenancy</strong></summary>

- Full tenant isolation — each tenant has its own users, access keys, buckets, and quotas
- Storage quotas per tenant with real-time enforcement
- Global admin cross-tenant visibility without impersonation
- Per-tenant identity provider routing (by email domain)
- Tenant-scoped bucket permissions with user and tenant-level grants
- Cascading deletes with validation

</details>

<details>
<summary><strong>Identity & Access</strong></summary>

- Local users with roles (global admin, tenant admin, user)
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

- AES-256-GCM authenticated encryption at rest (64 KB chunks, tamper detection)
- 169-file internal security audit — 28 vulnerabilities found and fixed in v1.0.0-rc1
- SSRF protection on all outbound HTTP (webhooks, log targets, replication endpoints)
- Auth cookies: `Secure` + `SameSite=Strict`
- OAuth2 CSRF state validation
- CORS allowlist (no wildcard)
- Replication credentials encrypted at rest
- Cluster inter-node TLS with auto-generated CA, CSR-based join (CA key never transmitted)
- Audit logging — 20+ event types (auth, object ops, admin actions), external syslog forwarding

</details>

<details>
<summary><strong>Cluster & High Availability</strong></summary>

- Multi-node cluster — up to 5 nodes tested
- Automatic failover and health monitoring
- HMAC-authenticated inter-node replication
- Bucket migration between nodes (full data + metadata + settings)
- 6-entity sync (users, tenants, access keys, bucket permissions, IDP providers, group mappings)
- Tombstone-based deletion sync — prevents entity resurrection in bidirectional sync
- JWT secret cluster sync — sessions valid across all nodes
- Deduplicated bucket list — replicated buckets appear once in listings

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

</details>

---

## Quick Start

### Docker — one command

```bash
docker run -d \
  --name maxiofs \
  -p 8080:8080 \
  -p 8081:8081 \
  -v maxiofs-data:/var/lib/maxiofs \
  maxiofs/maxiofs:latest
```

- **Web Console:** http://localhost:8081 — login: `admin` / `admin`
- **S3 API:** http://localhost:8080

> ⚠️ Change the default password immediately after first login.

### Docker Compose

```bash
git clone https://github.com/MaxioFS/MaxioFS.git
cd MaxioFS
make docker-up          # Single node
make docker-monitoring  # + Prometheus & Grafana
make docker-cluster     # 3-node cluster
```

### Binary

```bash
# Download the latest release for your platform
curl -L https://github.com/MaxioFS/MaxioFS/releases/latest/download/maxiofs-linux-amd64 -o maxiofs
chmod +x maxiofs
./maxiofs --data-dir ./data
```

### Test with AWS CLI

```bash
aws configure --profile maxiofs
# AWS Access Key ID: admin
# AWS Secret Access Key: admin
# Default region: us-east-1

aws --profile maxiofs --endpoint-url http://localhost:8080 s3 mb s3://my-bucket
aws --profile maxiofs --endpoint-url http://localhost:8080 s3 cp file.txt s3://my-bucket/
aws --profile maxiofs --endpoint-url http://localhost:8080 s3 ls s3://my-bucket/
```

### Install as a system service

**Debian / Ubuntu**
```bash
sudo dpkg -i maxiofs_1.0.0_amd64.deb
sudo systemctl enable --now maxiofs
```

**RHEL / Rocky / Alma / Fedora**
```bash
sudo rpm -i maxiofs-1.0.0-1.x86_64.rpm
sudo systemctl enable --now maxiofs
```

---

## Build from Source

Go 1.25+ and Node.js 24+ required to build. The resulting binary has no runtime dependencies.

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
| [DOCKER.md](DOCKER.md) | Docker and Compose reference |

---

## Performance

Tested with MinIO Warp on a single node (commodity hardware):

| Operation | p95 latency | Concurrency |
|-----------|-------------|-------------|
| PUT | < 10 ms | 50 clients |
| GET | < 13 ms | 100 clients |
| Success rate | > 99.99% | mixed load |

---

## Testing

```bash
go test ./...                          # 1,700+ backend tests
cd web/frontend && npm run test        # 64 frontend tests
```

---

## Known Limitations

- **No erasure coding** — single-node data redundancy relies on filesystem/RAID; cluster mode replicates full objects
- **No S3 Select** — object content querying not supported
- **No cloud tiering** — lifecycle rules expire objects but do not tier to cold storage
- **No per-tenant encryption keys** — single master key, no HSM integration
- **Cluster tested up to 5 nodes**
- **No SAML** — use OAuth2/OIDC instead
- **No SOC 2 / ISO 27001 certification** — comprehensive internal audit completed

---

## Release History

| Version | Highlights |
|---------|-----------|
| **v1.0.0** *(stable)* | Complete UI redesign, folder upload, POST presigned URLs, bucket notifications, lifecycle execution, full Veeam B&R compatibility, Object Lock per-version enforcement, 3 security fixes |
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
