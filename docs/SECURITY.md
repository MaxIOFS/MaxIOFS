# MaxIOFS Security Guide

**Version**: 1.5.2 | **Last Updated**: July 18, 2026

## Security Overview

| Feature | Status | Details |
|---------|--------|---------|
| Password hashing | ✅ | Bcrypt (Go DefaultCost = 10) |
| JWT sessions | ✅ | Configurable timeout, idle timeout |
| Two-Factor Authentication | ✅ | TOTP (Google Authenticator, Authy) |
| S3 Signature auth | ✅ | AWS v2 and v4 |
| OAuth2/OIDC SSO | ✅ | Google, Microsoft, custom OIDC |
| LDAP/AD integration | ✅ | Bind authentication |
| Cluster HMAC auth | ✅ | HMAC-SHA256 with timestamp/nonce |
| Inter-node TLS | ✅ | Auto-generated internal CA, ECDSA P-256 |
| Role-Based Access Control | ✅ | 5 roles (admin, tenant-admin, user, readonly, guest) |
| Rate limiting | ✅ | IP-based login throttling |
| Account lockout | ✅ | Configurable threshold and duration |
| Encryption at rest | ✅ | AES-256-GCM authenticated encryption (64 KB chunks) |
| IDP secrets encryption | ✅ | AES-256-GCM for stored OAuth secrets |
| Object Lock (WORM) | ✅ | COMPLIANCE and GOVERNANCE modes |
| ACLs | ✅ | S3-compatible canned + custom ACLs |
| PublicAccessBlock | ✅ | Per-bucket flags enforced on every request |
| Audit logging | ✅ | 20+ event types, SQLite storage |
| Real-time notifications | ✅ | SSE push for security events |
| Multi-tenant isolation | ✅ | Complete data and API isolation |

---

## Authentication

### Console Authentication (JWT)

1. User submits username + password
2. If 2FA enabled: TOTP code required
3. Server validates credentials → issues JWT token
4. Token stored in browser localStorage
5. Included in all API requests via `Authorization: Bearer <token>`

**Session settings** (configurable at runtime):
- `security.session_timeout` — Refresh-token lifetime / inactivity timeout (default: 24h)
- `security.access_token_lifetime` — Access-token lifetime before refresh (default: 15 minutes)

### S3 API Authentication

AWS Signature v2 and v4 — compatible with all S3 clients, SDKs, and tools.

```bash
# AWS CLI
aws configure set aws_access_key_id YOUR_ACCESS_KEY
aws configure set aws_secret_access_key YOUR_SECRET_KEY
aws --endpoint-url=http://localhost:8080 s3 ls
```

Access keys are generated via Web Console (Users → Access Keys) or API. The default `admin / admin` credentials are only for the initial Web Console login; no S3 access key is created by default.

### OAuth2/OIDC SSO

SSO via Google Workspace, Microsoft Entra ID (Azure AD), or custom OIDC providers. Users are auto-provisioned based on group-to-role mappings.

> Complete SSO guide: [SSO.md](SSO.md)

### LDAP/Active Directory

Bind authentication against LDAP/AD directories. Configured via Web Console → Settings → Identity Providers.

### Cluster Authentication & Encryption

Inter-node communication is both **encrypted** (TLS) and **authenticated** (HMAC-SHA256):

**TLS Encryption (automatic):**
- On cluster initialization, an internal CA (ECDSA P-256, 10-year validity) is generated
- Each node gets a certificate signed by the CA (1-year validity, auto-renewed)
- All inter-node HTTP communication uses mutual TLS with the internal CA
- No configuration required — fully automatic and transparent
- Certificate auto-renewal runs monthly; hot-swapped without restart

**HMAC-SHA256 Authentication:**
```
Signature = HMAC-SHA256(node_token, METHOD + PATH + TIMESTAMP + NONCE + BODY)
```

- Timestamp validation: ±5 minutes maximum clock skew
- Constant-time signature comparison (timing attack prevention)
- Nonce for replay attack prevention

> Complete cluster security: [CLUSTER.md](CLUSTER.md#security)

---

## Two-Factor Authentication (2FA)

TOTP-based 2FA compatible with Google Authenticator, Authy, and similar apps.

**Setup:**
1. Navigate to Profile → Security
2. Click "Enable 2FA"
3. Scan QR code with authenticator app
4. Enter verification code to confirm
5. Save backup codes (10 single-use recovery codes)

**Recovery:** Use backup codes if authenticator device is lost.

---

## Authorization (RBAC)

| Action | Global Admin | Tenant Admin | User | Read-Only |
|--------|:---:|:---:|:---:|:---:|
| Manage tenants | ✅ | ❌ | ❌ | ❌ |
| Manage cluster | ✅ | ❌ | ❌ | ❌ |
| Modify security settings | ✅ | ❌ | ❌ | ❌ |
| Manage IDP providers | ✅ | ✅ | ❌ | ❌ |
| Manage tenant users | ✅ | ✅ | ❌ | ❌ |
| View audit logs | ✅ | ✅ (own tenant) | ❌ | ❌ |
| Create/delete buckets | ✅ | ✅ | ✅ | ❌ |
| Upload/download objects | ✅ | ✅ | ✅ | ⬇️ only |
| Manage own access keys | ✅ | ✅ | ✅ | ❌ |

---

## Password Security

- **Hashing**: Bcrypt with Go's `bcrypt.DefaultCost` (cost factor 10)
- **Storage**: SQLite database (`maxiofs.db`), never stored in plaintext
- **Minimum length**: 8 characters (configurable via `security.password_min_length`)
- **Recommendation**: 12+ characters with mixed case, numbers, and symbols

---

## Rate Limiting & Account Protection

### IP Rate Limiting

Prevents brute-force attacks from a single IP address:
- Default: 5 login attempts per minute per IP
- Configurable: `security.ratelimit_login_per_minute`
- Exceeded: HTTP 429 Too Many Requests

### Account Lockout

Protects individual accounts after repeated failed logins:
- Default threshold: 5 failed attempts (`security.max_failed_attempts`)
- Default duration: 15 minutes (`security.lockout_duration`)
- Admin notification: Real-time SSE push when account is locked
- Manual unlock: Admin can unlock via Web Console (Users → Unlock)
- Counter reset: Successful login resets the failed attempts counter

---

## Encryption at Rest

### Object Encryption (AES-256-GCM, envelope — always on)

Server-side encryption is **always active** (like AWS S3): every new object is
encrypted with its own random Data Encryption Key (DEK) using authenticated
AES-256-GCM in 64 KB chunks — any tampered chunk is detected and rejected on
read. Each object's DEK is wrapped with the Key Encryption Key (KEK) and
stored in the object's on-disk `.metadata` sidecar, so objects remain
recoverable from the filesystem alone given a KEK backup.

**Key management:**
- The KEK lives in the database and is **generated automatically** on first
  start — no configuration needed. (Pre-v1.5 deployments with
  `storage.encryption_key` in config.yaml: that key is seeded into the
  database as version 1 on the first start and never read again; existing
  objects keep decrypting.)
- **Recovery bundle**: download it from Settings → Security (passphrase-
  encrypted export of every key version) and store it OUTSIDE the server —
  the console shows a banner until you do. Without a bundle, losing the
  database means losing every encrypted object.
- **Key rotation**: Settings → Security → Rotate key (or
  `POST /api/v1/settings/encryption/rotate-kek`). Object data is never
  re-encrypted — the background worker re-wraps each object's DEK to the new
  version. After rotating, download a fresh bundle.
- Objects written before encryption became mandatory (plaintext or legacy
  direct-encrypted) are converted in the background when server load is low;
  progress is visible in Settings → Security.

**Cluster**: nodes share a cluster-wide KEK (distributed on join and on
rotation), so HA replication moves ciphertext as-is — no decrypt/re-encrypt
per hop.

**Disaster recovery**: if the metadata database is lost but the object files
survive, `maxiofs recover --data-dir <dir> --recovery-bundle <bundle>`
rebuilds the metadata store from the filesystem and restores the keys —
original modification times included, so lifecycle timers and cluster
reconciliation stay correct. Two deliberate biases towards recovering data:
versioned objects whose latest "version" was a deletion come back visible
(delete markers exist only in the lost database — re-delete if the deletion
should stand), and objects whose key material cannot be verified are still
indexed with the failure reported instead of silently disappearing from
listings. See `maxiofs recover --help` and the runbook in
[OPERATIONS.md](OPERATIONS.md#backups--disaster-recovery).

**Characteristics:**
- Transparent: Automatic encrypt on upload, decrypt on download
- Mixed mode: pre-existing plaintext/legacy objects keep reading correctly
- Performance: ~5-10% latency increase for large files
- S3 API compatible: No client changes needed
- SSE-C / external KMS (Vault, AWS KMS) are planned on top of the envelope

### IDP Secrets Encryption (AES-256-GCM)

OAuth client secrets and LDAP bind passwords are encrypted at rest in the SQLite database using AES-256-GCM authenticated encryption. The encryption key is derived from the server's configuration.

---

## Cluster Replication Security

When objects are replicated between cluster nodes:

1. The stored **ciphertext is sent as-is** — objects are never decrypted in
   transit (all nodes share a cluster-wide KEK distributed on join/rotation,
   so every node can unwrap the object's DEK at read time)
2. The transfer additionally travels over the **TLS-encrypted** inter-node
   connection (automatic)
3. HMAC-SHA256 signature authenticates the transfer
4. The destination stores a byte-identical copy (metadata, timestamps and
   client checksums included)

Objects wrapped with a node-local (pre-join) key fall back to a decrypt/
re-encrypt transfer until the background worker re-wraps them to the shared
key. Inter-node TLS is enabled automatically using the cluster's internal
CA — no manual configuration needed.

---

## Audit Logging

SQLite-based immutable audit trail tracking 20+ event types.

### Event Categories

| Category | Events |
|----------|--------|
| Authentication | Login success/failure, logout, token issued/expired |
| 2FA | 2FA enabled/disabled, verification success/failure |
| User Management | User created/updated/deleted, password changed |
| Access Keys | Key generated/revoked |
| Bucket Operations | Created/deleted, versioning/policy/CORS/ACL changed |
| Object Operations | Uploaded/downloaded/deleted, multipart, lock/retention |
| Security | Account locked/unlocked, rate limit triggered |
| System | Config changed, server started, encryption status |
| IDP | Provider created/updated/deleted, group mapping changed |
| Cluster | Node added/removed, replication rule changed |

### Access Control

- **Global admins**: View all audit logs across all tenants
- **Tenant admins**: View only their tenant's audit logs
- **Users**: No audit log access

### Configuration

- Retention: `audit.retention_days` (default: 90 days, configurable at runtime)
- Storage: Separate SQLite database (`audit.db`) for isolation
- Export: CSV export via Web Console with date/event/user/tenant filters
- API: `GET /api/v1/audit-logs` with query parameters for filtering

---

## Real-Time Security Notifications

Server-Sent Events (SSE) push notifications for security events:

- Account lockouts → instant admin notification
- Topbar bell icon with unread count badge
- Persistent storage (survives page reloads)
- Tenant isolation (global admins see all, tenant admins see own tenant)
- Automatic SSE connection on admin login

No configuration required — enabled automatically for admin users.

---

## Object Lock (WORM)

Write-Once-Read-Many compliance for regulatory requirements.

| Mode | Protection | Override |
|------|-----------|---------|
| **GOVERNANCE** | Prevents accidental deletion | Admin can override with special permission |
| **COMPLIANCE** | Strict immutability | Cannot be deleted until retention period expires |

**Legal Hold**: Independent of retention — can be applied/removed anytime, prevents deletion regardless of retention settings.

```bash
# Enable on bucket
aws s3api create-bucket --bucket compliance \
  --endpoint-url http://localhost:8080 --object-lock-enabled-for-bucket

# Set retention
aws s3api put-object-retention --bucket compliance --key doc.pdf \
  --endpoint-url http://localhost:8080 \
  --retention Mode=COMPLIANCE,RetainUntilDate=2027-01-01T00:00:00Z
```

---

## S3 ACLs

S3-compatible Access Control Lists for bucket and object-level permissions.

**Supported canned ACLs:**
- `private` (default)
- `public-read`
- `public-read-write`
- `authenticated-read`
- `bucket-owner-read`
- `bucket-owner-full-control`

Custom ACLs with grant-based permissions (READ, WRITE, READ_ACP, WRITE_ACP, FULL_CONTROL) are also supported.

---

## PublicAccessBlock

Per-bucket public access controls that override ACLs entirely. Configure via the S3 API or Web Console.

| Flag | Effect |
|------|--------|
| `BlockPublicAcls` | Rejects PUT requests that set a public canned ACL |
| `IgnorePublicAcls` | All existing and future public ACLs are ignored — effectively denies all ACL-based public access |
| `BlockPublicPolicy` | Rejects bucket policies that grant public access |
| `RestrictPublicBuckets` | Restricts access to only the bucket owner and AWS services, regardless of policy |

When `IgnorePublicAcls` or `RestrictPublicBuckets` is set, every unauthenticated request to the bucket is denied with `403 AccessDenied`, regardless of any `public-read` or `public-read-write` ACL that may be set on the bucket or object.

```bash
# Block all public access
aws s3api put-public-access-block \
  --endpoint-url http://localhost:8080 \
  --bucket my-bucket \
  --public-access-block-configuration \
    BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true

# Read current configuration
aws s3api get-public-access-block \
  --endpoint-url http://localhost:8080 \
  --bucket my-bucket
```

---

## Security Best Practices

1. **Change default credentials immediately** (admin/admin)
2. **Use HTTPS** — reverse proxy with TLS for all traffic
3. **Enable encryption at rest** — protect data on disk
4. **Enable 2FA** for all admin accounts
5. **Configure rate limiting** — adjust thresholds for your environment
6. **Run as non-root** — use dedicated service account
7. **Restrict file permissions** — `chmod 700` on data directory
8. **Configure firewall** — expose only necessary ports
9. **Enable audit logging** — review logs regularly
10. **Inter-node TLS is automatic** — cluster nodes encrypt all communication using auto-generated certificates
11. **Back up encryption keys** — data is irrecoverable without them
12. **Enable PublicAccessBlock** — use `BlockPublicAcls` + `RestrictPublicBuckets` on any bucket that should never be public
13. **Monitor** — set up Prometheus alerts for security events

---

## Known Limitations

1. **No SOC 2 / ISO 27001 certification** — no formal third-party certification exists; internal audits have been performed but are not a substitute for accredited third-party assessment
2. **Single master encryption key** — no per-tenant keys, no HSM integration
3. **No SAML SSO** — OAuth2/OIDC recommended instead
4. **Basic session management** — no device tracking or geographic restrictions
5. **SQLite audit storage** — may have scale limits at very high event volumes
6. **External log shipping via syslog/HTTP only** — no built-in managed SIEM integrations or log analytics backends
7. **Manual key rotation** — requires re-encrypting all objects

---

## Reporting Security Issues

**DO NOT** open public GitHub issues for security vulnerabilities.
Contact the maintainers directly via the security contact in the repository.

---

**See also**: [SSO.md](SSO.md) · [CLUSTER.md](CLUSTER.md#security) · [CONFIGURATION.md](CONFIGURATION.md)
