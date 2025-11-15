# MaxIOFS Security Guide

**Version**: 0.4.0-beta

> **BETA SOFTWARE DISCLAIMER**
>
> MaxIOFS is in **beta stage**. Core security features are implemented and production bugs have been fixed, but this software has not undergone comprehensive third-party security audits or extensive penetration testing. Use in production requires thorough testing in your environment.

## Overview

MaxIOFS implements essential security features for object storage:

- **Comprehensive Audit Logging** (v0.4.0+)
- Dual authentication (JWT + S3 signatures)
- **Two-Factor Authentication (2FA) with TOTP**
- Role-Based Access Control (RBAC)
- Bcrypt password hashing
- Rate limiting and account lockout
- Object Lock (WORM compliance)
- Multi-tenant isolation

---

## Authentication

### 1. Console Authentication (JWT)

Web console uses JWT tokens with username/password.

**Login Request:**
```http
POST /api/auth/login
Content-Type: application/json

{
  "username": "admin",
  "password": "your-password"
}
```

**Response:**
```json
{
  "success": true,
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": "user-123",
    "username": "admin",
    "roles": ["admin"]
  }
}
```

**Features:**
- Token expiration: 1 hour (default)
- Stored in localStorage
- Required for all console API endpoints
- **Optional 2FA verification with TOTP codes**

### 2. Two-Factor Authentication (2FA)

MaxIOFS supports TOTP-based 2FA for enhanced account security (available since v0.3.2-beta).

**Setup:**
1. User enables 2FA in Settings → Security
2. System generates QR code for authenticator app (Google Authenticator, Authy, etc.)
3. User scans QR code with authenticator app
4. User confirms setup with verification code
5. System generates backup codes for account recovery

**Login Flow with 2FA:**
```http
POST /api/auth/login
Content-Type: application/json

{
  "username": "admin",
  "password": "your-password",
  "totpCode": "123456"
}
```

**Features:**
- TOTP-based (Time-based One-Time Password)
- Compatible with standard authenticator apps
- Backup codes for emergency access
- Global admin can deactivate 2FA for users if needed
- User list shows 2FA status indicator
- Optional - users can choose to enable it

**Backup Codes:**
- Generated during 2FA setup
- Each code can be used once
- Store securely offline
- Required if authenticator device is lost

### 3. S3 API Authentication

S3-compatible authentication with access keys.

**Supported:**
- AWS Signature V2
- AWS Signature V4

**Example:**
```http
GET /bucket/object HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 Credential=...
X-Amz-Date: 20251012T120000Z
```

**Features:**
- Compatible with AWS CLI, SDKs, and tools
- Per-user access keys
- Multiple keys per user supported
- Keys can be revoked individually

---

## Authorization (RBAC)

MaxIOFS implements a 3-tier role system:

### Global Admin

**Scope:** Entire system

**Permissions:**
- Manage all tenants and users
- Full access to all buckets/objects
- View system-wide metrics
- Unlock user accounts
- System configuration

### Tenant Admin

**Scope:** Single tenant

**Permissions:**
- Manage users within tenant
- Create/delete tenant buckets
- Manage tenant access keys
- View tenant metrics
- Unlock tenant user accounts

### Tenant User

**Scope:** Limited access

**Permissions:**
- Access assigned buckets
- Upload/download objects
- Manage own access keys
- View own metrics

### Permission Enforcement

Permissions are enforced at:
1. API level (before processing)
2. Database level (tenant filtering)
3. Storage level (directory isolation)

---

## Password Security

### Bcrypt Hashing

MaxIOFS uses **bcrypt** for password storage.

**Features:**
- Industry-standard algorithm
- Cost factor: 10 (2^10 iterations)
- Automatic salt generation
- Resistant to brute-force attacks

**Implementation:**
```go
// Hash password
hashedPassword, _ := bcrypt.GenerateFromPassword(
    []byte(password),
    bcrypt.DefaultCost
)

// Verify password
err := bcrypt.CompareHashAndPassword(
    []byte(storedHash),
    []byte(password)
)
```

### Password Requirements

**Current Policy:**
- Minimum length: 8 characters
- No complexity requirements (alpha)

**Recommended for Production:**
- Minimum 12-16 characters
- Mix of character types
- Regular rotation

---

## Rate Limiting & Account Protection

### Login Rate Limiting

**Policy:**
- Maximum 5 attempts per minute per IP
- Automatic reset after 1 minute
- Applied to console login only

### Account Lockout

**Policy:**
- Triggered after 5 failed login attempts
- Lockout duration: 15 minutes
- Auto-unlock after duration
- Manual unlock by admin

**Manual Unlock:**
```http
POST /api/users/{userId}/unlock
Authorization: Bearer <admin-token>
```

**Permissions:**
- Global Admin: Can unlock any account
- Tenant Admin: Can unlock tenant users only

---

## Audit Logging

**New in v0.4.0-beta**

MaxIOFS includes a comprehensive audit logging system that tracks all critical security and administrative events for compliance and forensic analysis.

### Features

**Event Tracking:**
- **Authentication Events**: Login (success/failed), Logout, User Blocked/Unblocked
- **User Management**: User Created/Deleted/Updated, Role Changes, Status Changes
- **Bucket Operations**: Bucket Created/Deleted (via Console or S3 API)
- **Access Keys**: Key Created/Deleted, Status Changed
- **Tenant Management**: Tenant Created/Deleted/Updated (Global Admin only)
- **Security Events**: Password Changed, 2FA Enabled/Disabled, 2FA Verification

**Access Control:**
- Global admins can view all audit logs across all tenants
- Tenant admins can ONLY view logs from their own tenant
- Regular users cannot access audit logs
- All access attempts are themselves logged

**Data Retention:**
- Configurable retention period (default: 90 days)
- Automatic cleanup via daily background job
- Logs older than retention period are purged automatically
- No manual intervention required

### Configuration

**config.yaml:**
```yaml
audit:
  enabled: true                    # Enable/disable audit logging
  retention_days: 90               # Auto-delete logs older than N days
  db_path: "./data/audit_logs.db"  # SQLite database path
```

**Environment Variables:**
```bash
AUDIT_ENABLED=true
AUDIT_RETENTION_DAYS=90
AUDIT_DB_PATH="./data/audit_logs.db"
```

### API Access

**List Audit Logs:**
```http
GET /api/v1/audit-logs?page=1&page_size=50
Authorization: Bearer <admin-token>
```

**Query Parameters:**
- `tenant_id` - Filter by tenant (global admin only)
- `user_id` - Filter by user
- `event_type` - Filter by event type (login_success, user_created, etc.)
- `resource_type` - Filter by resource (system, user, bucket, etc.)
- `action` - Filter by action (login, create, delete, etc.)
- `status` - Filter by status (success, failed)
- `start_date` - Unix timestamp start range
- `end_date` - Unix timestamp end range
- `page` - Page number (default: 1)
- `page_size` - Results per page (default: 50, max: 100)

**Example Response:**
```json
{
  "logs": [
    {
      "id": 1,
      "timestamp": 1700000000,
      "tenant_id": "tenant-123",
      "user_id": "user-456",
      "username": "admin",
      "event_type": "login_success",
      "resource_type": "system",
      "action": "login",
      "status": "success",
      "ip_address": "192.168.1.100",
      "user_agent": "Mozilla/5.0...",
      "details": "{}"
    }
  ],
  "total": 150,
  "page": 1,
  "page_size": 50
}
```

### Web Console Access

Audit logs are accessible via the Web Console at `/audit-logs` (admin only).

**Features:**
- Advanced filtering (event type, status, resource type, date range)
- Quick date filters (Today, Last 7 Days, Last 30 Days, All Time)
- Real-time search across users, events, resources, and IP addresses
- Color-coded critical events (login failures, security events)
- Expandable rows with full event details
- CSV export for compliance reporting

### Security Considerations

**Data Privacy:**
- ✅ Passwords are NEVER logged (even hashed passwords)
- ✅ Secrets and tokens are never included in logs
- ✅ User agents stored for security analysis
- ✅ IP addresses logged for security auditing
- ⚠️ Consider GDPR compliance for IP address logging

**Immutability:**
- ✅ No UPDATE or DELETE operations via API
- ✅ Only system maintenance jobs can purge old logs
- ✅ Append-only design ensures audit trail integrity
- ✅ Logs stored in separate SQLite database

### Compliance Support

This audit logging system helps with:
- ✅ **GDPR Article 30**: Records of processing activities
- ✅ **SOC 2 Type II**: Audit trail requirements
- ✅ **HIPAA**: Access logging for protected health information systems
- ✅ **ISO 27001**: Information security event logging
- ✅ **PCI DSS**: User activity tracking and audit trails

### Event Types Reference

| Event Type | Description | Example Trigger |
|-----------|-------------|-----------------|
| `login_success` | Successful login | User logs in with correct credentials |
| `login_failed` | Failed login attempt | Wrong password or username |
| `logout` | User logout | User clicks logout |
| `user_blocked` | Account locked | Too many failed login attempts |
| `user_unblocked` | Account unlocked | Admin unlocks user |
| `user_created` | New user created | Admin creates user |
| `user_deleted` | User removed | Admin deletes user |
| `user_updated` | User modified | Admin changes user settings |
| `password_changed` | Password updated | User changes password |
| `2fa_enabled` | 2FA activated | User enables 2FA |
| `2fa_disabled` | 2FA deactivated | User or admin disables 2FA |
| `2fa_verify_success` | 2FA code valid | Correct TOTP code entered |
| `2fa_verify_failed` | 2FA code invalid | Wrong TOTP code entered |
| `bucket_created` | Bucket created | Via Console or S3 API |
| `bucket_deleted` | Bucket deleted | Via Console or S3 API |
| `access_key_created` | Access key created | Admin creates key for user |
| `access_key_deleted` | Access key revoked | Admin or user deletes key |
| `tenant_created` | Tenant created | Global admin creates tenant |
| `tenant_deleted` | Tenant deleted | Global admin deletes tenant |
| `tenant_updated` | Tenant modified | Global admin updates tenant |

---

## Object Lock (WORM)

S3-compatible Object Lock for immutable storage.

### Retention Modes

**COMPLIANCE Mode:**
- Immutable until expiration
- Cannot be bypassed
- Use: Regulatory compliance

**GOVERNANCE Mode:**
- Protected but can be bypassed with permissions
- More flexible
- Use: Internal policies

### Legal Hold

- Indefinite protection
- Independent of retention
- Use: Litigation, investigations

### Enabling Object Lock

**Bucket creation:**
```bash
aws s3api create-bucket \
  --bucket my-bucket \
  --object-lock-enabled-for-bucket
```

**Set retention:**
```bash
aws s3api put-object-retention \
  --bucket my-bucket \
  --key file.txt \
  --retention Mode=COMPLIANCE,RetainUntilDate=2026-01-01T00:00:00Z
```

**Set legal hold:**
```bash
aws s3api put-object-legal-hold \
  --bucket my-bucket \
  --key file.txt \
  --legal-hold Status=ON
```

---

## Security Best Practices

### 1. Change Default Credentials

**First priority:**
```
Console: admin/admin → Change password immediately
S3 API: No default keys → Create secure access keys via console
```

**Steps:**
1. Login to web console with admin/admin
2. Change admin password
3. Create S3 access keys for your applications
4. Store keys securely (password manager/vault)

### 2. Use HTTPS

MaxIOFS doesn't include TLS. Use reverse proxy:

```nginx
server {
    listen 443 ssl http2;
    server_name storage.example.com;

    ssl_certificate /etc/ssl/cert.pem;
    ssl_certificate_key /etc/ssl/key.pem;

    location / {
        proxy_pass http://localhost:8081;
    }
}
```

### 3. Configure CORS Carefully

```yaml
cors:
  enabled: true
  allowed_origins:
    - "https://your-app.example.com"
  # Never use "*" in production
```

### 4. Run as Non-Root User

```bash
sudo useradd -r -s /bin/false maxiofs
sudo chown -R maxiofs:maxiofs /var/lib/maxiofs
```

### 5. Restrict File Permissions

```bash
# Data directory
chmod 750 /var/lib/maxiofs

# Database
chmod 600 /var/lib/maxiofs/maxiofs.db
```

### 6. Enable Firewall

```bash
# Only expose HTTPS via reverse proxy
ufw allow 443/tcp
ufw deny 8080/tcp
ufw deny 8081/tcp
```

### 7. Regular Backups

```bash
# Backup data directory
tar -czf backup.tar.gz /var/lib/maxiofs
```

### 8. Monitor Logs

```bash
# Check for suspicious activity
journalctl -u maxiofs | grep -i "failed\|locked\|denied"
```

---

## Known Limitations

### Beta Security Limitations

1. **No Built-in TLS** - Use reverse proxy (recommended approach)
2. **Basic Password Policy** - Only minimum length enforced
3. ~~**No Multi-Factor Authentication**~~ - ✅ **2FA IMPLEMENTED** (v0.3.2-beta with TOTP)
4. **Limited Audit Logging** - Basic auth events only
5. **No Encryption at Rest** - Use filesystem encryption (LUKS, BitLocker, etc.)
6. **Simple Rate Limiting** - Login endpoint only
7. **No Security Audits** - Not professionally audited by third parties
8. ✅ **Session Management** - Improved with idle timer and timeout enforcement (v0.3.1-beta)
9. **No Key Management System** - Secrets in config files
10. **SQLite Database** - No at-rest encryption

### Mitigations

- Use strong passwords
- ✅ Enable 2FA for all users (especially admins)
- Enable account lockout
- Configure firewall rules
- Use reverse proxy with TLS
- Filesystem-level encryption
- Restrict file permissions
- Regular security updates

---

## Reporting Security Issues

**DO NOT** open public GitHub issues for vulnerabilities.

**Email:** security@yourdomain.com (update with actual contact)

**Include:**
- Vulnerability description
- Steps to reproduce
- Potential impact
- Suggested fix (optional)

Response time: Within 48 hours

---

## Security Checklist

**Initial Setup:**
- [ ] Change default credentials
- [ ] Enable 2FA for admin accounts
- [ ] Configure HTTPS (reverse proxy)
- [ ] Set up firewall rules
- [ ] Run as non-root user
- [ ] Restrict file permissions
- [ ] Configure CORS properly
- [ ] Enable rate limiting
- [ ] Set up backups

**Ongoing:**
- [ ] Monitor logs
- [ ] Keep MaxIOFS updated
- [ ] Review user permissions
- [ ] Test backup restoration
- [ ] Update TLS certificates

---

**Note**: This is beta software. Conduct thorough security assessment and testing before production use.

---

**Version**: 0.3.2-beta
**Last Updated**: November 2025
