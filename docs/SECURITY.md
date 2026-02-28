# MaxIOFS Security Guide

**Version**: 0.9.2-beta | **Last Updated**: February 28, 2026

> **BETA SOFTWARE**: Core security features are implemented. Third-party audits have not been conducted. Test thoroughly before production use.

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
| Encryption at rest | ✅ | AES-256-CTR streaming encryption |
| IDP secrets encryption | ✅ | AES-256-GCM for stored OAuth secrets |
| Object Lock (WORM) | ✅ | COMPLIANCE and GOVERNANCE modes |
| ACLs | ✅ | S3-compatible canned + custom ACLs |
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
- `security.session_timeout` — Token lifetime (default: 24h)
- `security.idle_timeout` — Idle session timeout (default: 1h)
- `security.max_sessions_per_user` — Concurrent session limit (default: 5)

### S3 API Authentication

AWS Signature v2 and v4 — compatible with all S3 clients, SDKs, and tools.

```bash
# AWS CLI
aws configure set aws_access_key_id YOUR_ACCESS_KEY
aws configure set aws_secret_access_key YOUR_SECRET_KEY
aws --endpoint-url=http://localhost:8080 s3 ls
```

Access keys are generated via Web Console (Users → Access Keys) or API.

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

### Object Encryption (AES-256-CTR)

Streaming encryption for objects stored on disk.

**Enable:**
```yaml
# config.yaml
storage:
  enable_encryption: true
  encryption_key: "a1b2c3d4...64_hex_chars"  # 32 bytes = 256 bits
```

**Generate key:**
```bash
openssl rand -hex 32
```

**Characteristics:**
- Dual-level control: Server-wide default + per-bucket override
- Transparent: Automatic encrypt on upload, decrypt on download
- Mixed mode: Encrypted and unencrypted objects coexist
- Performance: ~5-10% latency increase for large files
- S3 API compatible: No client changes needed

**Key management best practices:**
- Store encryption key outside data directory
- Restrict file permissions (`chmod 400`)
- Back up the key securely — data is irrecoverable without it
- HSM integration is planned for a future release

### IDP Secrets Encryption (AES-256-GCM)

OAuth client secrets and LDAP bind passwords are encrypted at rest in the SQLite database using AES-256-GCM authenticated encryption. The encryption key is derived from the server's configuration.

---

## Cluster Replication Security

When objects are replicated between cluster nodes:

1. Source node **decrypts** the object (if encrypted)
2. Object data is sent over **TLS-encrypted** inter-node connection (automatic)
3. HMAC-SHA256 signature authenticates the transfer
4. Destination node **re-encrypts** with its own master key

Each node can have a different encryption key. Inter-node TLS is enabled automatically using the cluster's internal CA — no manual configuration needed.

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
12. **Monitor** — set up Prometheus alerts for security events

---

## Known Limitations

1. **No third-party security audit** — no SOC 2, ISO 27001 certification
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
