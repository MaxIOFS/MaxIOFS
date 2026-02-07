# MaxIOFS Security Guide

**Version**: 0.8.0-beta
**Last Updated**: January 16, 2026

> **BETA SOFTWARE DISCLAIMER**: MaxIOFS is in beta stage. Core security features are implemented, but third-party security audits have not been conducted. Production use requires thorough testing in your environment.

---

## Overview

MaxIOFS implements comprehensive security features for object storage:

- **HMAC-SHA256 Cluster Authentication** (v0.6.0) - Secure inter-node communication
- **Real-Time Security Notifications** (SSE) (v0.4.2)
- **Dynamic Security Configuration** (v0.4.2) - Runtime security tuning
- **Server-Side Encryption at Rest** (AES-256-CTR) (v0.4.1)
- **Comprehensive Audit Logging** (20+ event types) (v0.4.0)
- **Two-Factor Authentication** (2FA/TOTP)
- Triple authentication: JWT + S3 signatures + HMAC for clusters
- Role-Based Access Control (RBAC)
- Bcrypt password hashing (cost: 12)
- Configurable rate limiting and account lockout
- Object Lock (WORM compliance)
- Multi-tenant isolation

---

## Real-Time Security Notifications

**Server-Sent Events (SSE)** provide real-time push notifications for security events.

### Features

- **Zero-latency alerts** when accounts are locked
- **Topbar bell icon** with unread count badge
- **Persistent storage** (notifications survive page reloads)
- **Tenant isolation** (global admins see all, tenant admins see only their tenant)
- **JWT authentication** required for SSE connection

### Event Flow

1. Admin logs in → Frontend auto-connects to SSE endpoint
2. Security event occurs (e.g., account locked)
3. Backend broadcasts to all connected admins
4. Notification appears in topbar
5. Admin clicks to view and mark as read

### Example Notification

```json
{
  "type": "user_locked",
  "message": "User john has been locked due to failed login attempts",
  "data": {
    "userId": "user-123",
    "username": "john",
    "tenantId": "tenant-abc"
  },
  "timestamp": 1732435200
}
```

**No configuration required** - automatically enabled for all admin users.

---

## Dynamic Security Configuration

Adjust security thresholds **without server restarts** via Web Console (`/settings`) or API.

### Configurable Settings

| Setting | Default | Description | Recommended |
|---------|---------|-------------|-------------|
| `security.ratelimit_login_per_minute` | 5 | IP-based rate limiting | 15 (if behind proxy) |
| `security.max_failed_attempts` | 5 | Account lockout threshold | 5-10 |
| `security.lockout_duration` | 900s (15 min) | Lock duration in seconds | 900-1800s |

### IP Rate Limiting vs Account Lockout

| Feature | IP Rate Limiting | Account Lockout |
|---------|------------------|-----------------|
| **Scope** | Per IP address | Per user account |
| **Purpose** | Prevent brute force from single source | Protect individual accounts |
| **Affects** | All users from that IP | Single user account |
| **Typical Value** | 15 attempts/minute | 5 failed attempts |

### Configuration

**Via Web Console:**
1. Navigate to `/settings` (global admin only)
2. Select "Security" category
3. Modify values → Click "Save Changes"
4. Changes take effect immediately

**Via API:**
```bash
PUT /api/v1/settings/security.ratelimit_login_per_minute
{
  "value": "15"
}
```

All changes are logged in audit trail.

---

## Server-Side Encryption (SSE)

**AES-256-CTR streaming encryption** for objects at rest.

### Features

- **Dual-level control**: Server-wide and per-bucket encryption
- **Transparent encryption/decryption**: Automatic on upload/download
- **Mixed mode**: Encrypted and unencrypted objects can coexist
- **Persistent master key**: Stored in `config.yaml`
- **Visual indicators**: Web console shows encryption status

### Configuration

**Server-Level Encryption** (config.yaml):
```yaml
encryption:
  enabled: true
  master_key: /path/to/master_key.key  # 256-bit AES key
```

**Bucket-Level Encryption** (Web Console):
- Navigate to Bucket → Settings
- Enable "Server-Side Encryption"
- Override server default (if server encryption disabled)

### Key Management

**Master Key Generation:**
```bash
openssl rand -hex 32 > master_key.key
chmod 400 master_key.key
```

**Best Practices:**
1. Store master key outside data directory
2. Restrict permissions (400 or 600)
3. Backup master key securely (encrypted backups)
4. Use Hardware Security Module (HSM) for production (planned for future release)
5. Rotate keys regularly (manual process currently)

**Key Rotation** (manual):
1. Generate new key
2. Decrypt all objects with old key
3. Re-encrypt with new key
4. Update config.yaml
5. Securely delete old key

### API Compatibility

Fully compatible with S3 API - no client changes required. Objects are transparently encrypted/decrypted.

**Performance**: Minimal overhead (~5-10% latency increase for large files)

---

## Authentication

MaxIOFS supports **three authentication methods** for different use cases.

### Authentication Methods

| Method | Use Case | Credentials | Security |
|--------|----------|-------------|----------|
| **JWT (Console)** | Web Console access | Username + Password + 2FA | Session timeout, HTTPS recommended |
| **S3 Signatures (v2/v4)** | S3 API access | Access Key + Secret Key | HMAC-SHA256 signed requests |
| **HMAC-SHA256 (Cluster)** | Inter-node communication | Node token | HMAC with timestamp validation |

### 1. Console Authentication (JWT)

**Login Flow:**
1. User submits username/password
2. If 2FA enabled: TOTP code required
3. Server validates credentials → Issues JWT token
4. Token stored in localStorage
5. Token included in all API requests

**Session Management:**
- Token lifetime: 24 hours (configurable)
- Automatic logout on expiration
- Manual logout clears token

### 2. Two-Factor Authentication (2FA)

**TOTP-based** (compatible with Google Authenticator, Authy, etc.)

**Setup:**
1. Navigate to Profile → Security
2. Click "Enable 2FA"
3. Scan QR code with authenticator app
4. Enter verification code
5. Save **backup codes** (10 single-use codes)

**Recovery:** Use backup codes if device is lost.

### 3. S3 API Authentication

**Signature V4** (AWS-compatible):
```
Authorization: AWS4-HMAC-SHA256 Credential=<access_key_id>/...
```

**Signature V2** (legacy support):
```
Authorization: AWS <access_key_id>:<signature>
```

**Access Keys:**
- Generated via Web Console (Users → Access Keys)
- Format: `AKIA...` (20 chars) + secret (40 chars)
- Support multiple keys per user (max: tenant quota)
- Can be disabled or deleted

### 4. Cluster Authentication (HMAC-SHA256)

**Inter-node authentication** using shared `node_token`.

**Message Format:**
```
HMAC-SHA256(node_token, METHOD + PATH + TIMESTAMP + NONCE + BODY)
```

**Headers:**
```
X-MaxIOFS-Node-ID: <sender-node-id>
X-MaxIOFS-Timestamp: <unix-timestamp>
X-MaxIOFS-Nonce: <random-uuid>
X-MaxIOFS-Signature: <hex-encoded-hmac>
```

**Validation:**
- Timestamp skew: max ±5 minutes
- Signature comparison: constant-time
- Failed auth: HTTP 401 Unauthorized

**See [CLUSTER.md](CLUSTER.md#security) for complete cluster security documentation**

---

## Authorization (RBAC)

**Role-Based Access Control** with three roles:

| Role | Permissions | Scope |
|------|-------------|-------|
| **Global Admin** | Full system access | All tenants |
| **Tenant Admin** | Manage tenant resources | Single tenant |
| **Tenant User** | Read/write buckets and objects | Single tenant |

### Permission Matrix

| Action | Global Admin | Tenant Admin | Tenant User |
|--------|--------------|--------------|-------------|
| Manage all tenants | ✅ | ❌ | ❌ |
| Manage tenant users | ✅ | ✅ | ❌ |
| Create/delete buckets | ✅ | ✅ | ✅ |
| Upload/download objects | ✅ | ✅ | ✅ |
| View audit logs | ✅ | ✅ (tenant only) | ❌ |
| Modify security settings | ✅ | ❌ | ❌ |
| Manage cluster | ✅ | ❌ | ❌ |

---

## Password Security

**Bcrypt hashing** with cost factor 12.

### Password Requirements

- Minimum length: 8 characters
- Recommended: 12+ characters with mixed case, numbers, symbols
- No dictionary words
- Unique per account

### Implementation

```go
bcrypt.GenerateFromPassword(password, 12)
```

**Hash storage:** SQLite database (auth.db)

---

## Rate Limiting & Account Protection

### Login Rate Limiting

**IP-based rate limiting** prevents brute force attacks:
- Default: 5 attempts per minute per IP
- Configurable via security settings
- Affects all login attempts from same IP
- HTTP 429 response when exceeded

### Account Lockout

**User-specific lockout** after failed attempts:
- Default: 5 failed attempts
- Lockout duration: 15 minutes (configurable)
- Independent of IP rate limiting
- Automatic unlock after duration
- Manual unlock: Admin can reset via Web Console

**Lockout Flow:**
1. User fails login 5 times
2. Account locked for 15 minutes
3. Admin notified via SSE (real-time)
4. Failed attempts counter reset after successful login
5. Audit log entry created

---

## Audit Logging

**SQLite-based audit system** tracking 20+ event types.

### Features

- **Comprehensive coverage**: Authentication, bucket ops, user management, config changes
- **Tenant isolation**: Tenant admins see only their tenant events
- **Automatic retention**: Default 90 days (configurable)
- **CSV export**: Web Console supports filtered export
- **API access**: RESTful API for programmatic access

### Event Types

| Category | Events |
|----------|--------|
| **Authentication** | Login success/failure, logout, 2FA events, token issued/expired |
| **User Management** | User created/updated/deleted, access key generated/revoked |
| **Bucket Operations** | Bucket created/deleted, policy updated, versioning changed |
| **Object Operations** | Object uploaded/downloaded/deleted, multipart upload |
| **Security** | Account locked, password changed, 2FA enabled/disabled |
| **System** | Config changed, server started/stopped, encryption status changed |

### Configuration

**Retention Period** (config.yaml):
```yaml
audit:
  retention_days: 90
```

**Web Console Access:**
- Navigate to Audit Logs page
- Filter by: Date range, Event type, User, Tenant, Bucket
- Export to CSV for compliance reporting

**API Access:**
```bash
GET /api/v1/audit-logs?start=<timestamp>&end=<timestamp>&event_type=<type>
```

---

## Object Lock (WORM)

**Write-Once-Read-Many** compliance for regulatory requirements.

### Retention Modes

| Mode | Description | Override |
|------|-------------|----------|
| **GOVERNANCE** | Protects from deletion | Admin can override |
| **COMPLIANCE** | Strict immutability | Cannot be deleted until expiration |

### Legal Hold

**Independent of retention period:**
- Can be applied/removed anytime
- Prevents deletion regardless of retention
- Requires special permissions

### Configuration

**Bucket-Level:**
```bash
aws s3api create-bucket --bucket compliance-bucket --object-lock-enabled-for-bucket
```

**Object-Level:**
```bash
aws s3api put-object-retention \
  --bucket compliance-bucket \
  --key document.pdf \
  --retention Mode=COMPLIANCE,RetainUntilDate=2026-01-01T00:00:00Z
```

---

## Security Best Practices

### Essential Security Measures

1. **Change Default Credentials**
   - Default: admin/admin
   - Change immediately after installation
   - Use strong passwords (12+ chars)

2. **Use HTTPS**
   - Generate TLS certificates (Let's Encrypt recommended)
   - Configure reverse proxy (Nginx/HAProxy)
   - Redirect HTTP → HTTPS

3. **Configure CORS Carefully**
   - Whitelist only trusted origins
   - Avoid `*` wildcard in production
   - Test CORS rules before deployment

4. **Run as Non-Root User**
   ```bash
   useradd -r -s /bin/false maxiofs
   chown -R maxiofs:maxiofs /opt/maxiofs
   ```

5. **Restrict File Permissions**
   ```bash
   chmod 700 /opt/maxiofs/data
   chmod 400 /opt/maxiofs/master_key.key
   ```

6. **Enable Firewall**
   ```bash
   ufw allow 8080/tcp  # S3 API
   ufw allow 8081/tcp  # Console
   ufw enable
   ```

7. **Regular Backups**
   - Backup data directory
   - Backup SQLite databases (auth.db, audit.db)
   - Backup master encryption key (encrypted)
   - Test restore procedures

8. **Monitor Logs**
   - Review audit logs daily
   - Set up alerts for suspicious activity
   - Monitor failed login attempts
   - Track account lockouts

9. **Enable 2FA**
   - Require 2FA for all admin accounts
   - Use hardware tokens for high-security environments

10. **Network Segmentation**
    - Isolate MaxIOFS network
    - Use VPN for admin access
    - Restrict cluster communication to private network

---

## Known Limitations

### Beta Security Limitations

1. **No Third-Party Security Audit**
   - No external penetration testing
   - No security certification (SOC 2, ISO 27001)
   - Recommended: Perform internal security assessment

2. **Limited Encryption Options**
   - Single master key for all tenants
   - No per-tenant encryption keys
   - No HSM integration (planned for future release)
   - Manual key rotation required

3. **Authentication Limitations**
   - No LDAP/Active Directory integration (planned v0.8.0)
   - No SAML/OAuth2 SSO (planned v0.8.0)
   - Session management basic (no device tracking)

4. **Audit Log Retention**
   - SQLite storage (size limits for very high volume)
   - No external log shipping (syslog planned for future release)
   - Manual export required for long-term archival

### Mitigations

- Deploy behind WAF (Web Application Firewall)
- Use intrusion detection system (IDS)
- Implement network monitoring
- Regular security assessments
- Stay updated with security patches

---

## Reporting Security Issues

**DO NOT** open public GitHub issues for security vulnerabilities.

**Report to**: [Insert your security contact email]

**Include:**
- Vulnerability description
- Steps to reproduce
- Potential impact
- Suggested mitigation (if any)

**Response time**: 48 hours for acknowledgment

---

## Security Checklist

### Initial Setup

- [ ] Change default admin credentials
- [ ] Enable HTTPS with valid TLS certificate
- [ ] Configure firewall rules
- [ ] Generate and secure master encryption key
- [ ] Enable server-side encryption
- [ ] Set appropriate file permissions (700 data, 400 keys)
- [ ] Create non-root user for service

### User Management

- [ ] Enforce strong password policy
- [ ] Enable 2FA for all admin accounts
- [ ] Review and audit user permissions
- [ ] Set tenant quotas appropriately
- [ ] Disable unused accounts
- [ ] Rotate access keys regularly (every 90 days)

### Configuration

- [ ] Configure rate limiting thresholds
- [ ] Set account lockout parameters
- [ ] Enable audit logging
- [ ] Configure log retention (minimum 90 days)
- [ ] Set session timeout (default: 24h)
- [ ] Configure CORS whitelist

### Monitoring

- [ ] Review audit logs daily
- [ ] Monitor failed login attempts
- [ ] Track account lockouts
- [ ] Set up security alerts (SSE notifications)
- [ ] Monitor system resource usage
- [ ] Check for security updates

### Cluster Security (if applicable)

- [ ] Use strong cluster tokens (256-bit entropy)
- [ ] Rotate cluster tokens every 90 days
- [ ] Restrict cluster network (firewall rules)
- [ ] Use TLS for inter-node communication
- [ ] Monitor cluster health and replication

### Compliance

- [ ] Export audit logs for compliance reporting
- [ ] Backup encryption keys securely
- [ ] Document security procedures
- [ ] Perform regular security assessments
- [ ] Test disaster recovery procedures
- [ ] Maintain security documentation

---

**Version**: 0.8.0-beta
**Last Updated**: January 16, 2026

For additional security information, see:
- [CLUSTER.md](CLUSTER.md#security) - Cluster security details
- [DEPLOYMENT.md](DEPLOYMENT.md) - Production security best practices
- [CONFIGURATION.md](CONFIGURATION.md) - Security configuration options
