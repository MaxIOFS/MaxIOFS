# MaxIOFS Security Guide

**Version**: 0.4.2-beta

> **BETA SOFTWARE DISCLAIMER**
>
> MaxIOFS is in **beta stage**. Core security features are implemented and production bugs have been fixed, but this software has not undergone comprehensive third-party security audits or extensive penetration testing. Use in production requires thorough testing in your environment.

## Overview

MaxIOFS implements essential security features for object storage:

- **Real-Time Security Notifications (SSE)** (v0.4.2+)
- **Dynamic Security Configuration** (v0.4.2+)
- **Server-Side Encryption at Rest (SSE)** (v0.4.1+)
- **Comprehensive Audit Logging** (v0.4.0+)
- Dual authentication (JWT + S3 signatures)
- **Two-Factor Authentication (2FA) with TOTP**
- Role-Based Access Control (RBAC)
- Bcrypt password hashing
- **Configurable rate limiting and account lockout**
- Object Lock (WORM compliance)
- Multi-tenant isolation

---

## Real-Time Security Notifications

**New in v0.4.2-beta**

MaxIOFS provides real-time push notifications using Server-Sent Events (SSE) to alert administrators immediately when security events occur.

### Features

**Notification Capabilities:**
- **Real-Time Push**: Zero-latency notifications using SSE
- **User Locked Alerts**: Immediate notification when accounts are locked
- **Topbar Bell Icon**: Unread count badge with dropdown
- **Read/Unread Tracking**: Visual indicators for notification status
- **Persistent Storage**: Notifications survive page reloads (localStorage)
- **Limited History**: Last 3 notifications to prevent UI clutter
- **Tenant Isolation**: Global admins see all, tenant admins see only their tenant

**Security Properties:**
- JWT authentication required for SSE connection
- Automatic disconnection on token expiration
- No sensitive data in notifications (only event metadata)
- Connection tracking and automatic cleanup

### How It Works

1. **Connection**: Admin logs in, frontend automatically connects to SSE endpoint
2. **Event Trigger**: Security event occurs (e.g., user account locked)
3. **Notification**: Backend broadcasts to all connected admin clients
4. **Display**: Notification appears in topbar with unread badge
5. **Action**: Admin clicks to view details and mark as read

### Configuration

No configuration required - SSE notifications are automatically enabled for all admin users (global admins and tenant admins).

**Access Requirements:**
- Must be logged in with admin role
- Valid JWT token in localStorage
- Browser must support Server-Sent Events (all modern browsers)

**Example Notification:**
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

### Best Practices

1. **Monitor Regularly**: Check notifications frequently during business hours
2. **Investigate Immediately**: User lockout may indicate brute force attack
3. **Review Audit Logs**: Use audit logs for detailed investigation
4. **Adjust Thresholds**: Use dynamic security configuration to tune sensitivity

---

## Dynamic Security Configuration

**New in v0.4.2-beta**

MaxIOFS allows administrators to adjust security thresholds dynamically without server restarts.

### Configurable Settings

**Security Settings (via Web Console `/settings`):**

1. **`security.ratelimit_login_per_minute`** (Default: 5)
   - IP-based rate limiting threshold
   - Prevents brute force from single IP address
   - Affects all users from that IP
   - Recommended: Higher for users behind proxies (e.g., 15)

2. **`security.max_failed_attempts`** (Default: 5)
   - Account lockout threshold
   - Protects individual user accounts
   - Independent of IP rate limiting
   - Recommended: 5-10 attempts

3. **`security.lockout_duration`** (Default: 900 seconds = 15 minutes)
   - How long accounts stay locked after exceeding max_failed_attempts
   - Measured in seconds
   - Recommended: 900-1800 seconds (15-30 minutes)

### Key Differences

**IP Rate Limiting vs Account Lockout:**

| Feature | IP Rate Limiting | Account Lockout |
|---------|------------------|-----------------|
| **Scope** | Per IP address | Per user account |
| **Purpose** | Prevent brute force from single source | Protect individual accounts |
| **Affects** | All users from that IP | Single user account |
| **Typical Value** | 15 attempts/minute | 5 failed attempts |
| **Use Case** | Shared IP (proxy, NAT) | Individual password guessing |

### Configuration via Web Console

1. Navigate to `/settings` (global admin only)
2. Select "Security" category
3. Modify desired values
4. Click "Save Changes"
5. Changes take effect immediately (no restart)
6. All changes logged in audit trail

### Configuration via API

```bash
# Update rate limit
curl -X PUT https://your-server/api/v1/settings/security.ratelimit_login_per_minute \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"value": "15"}'

# Update lockout threshold
curl -X PUT https://your-server/api/v1/settings/security.max_failed_attempts \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"value": "10"}'

# Update lockout duration (30 minutes)
curl -X PUT https://your-server/api/v1/settings/security.lockout_duration \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"value": "1800"}'
```

### Best Practices

1. **Separate Concerns**: Set IP rate limiting higher than account lockout
2. **Consider Environment**: Users behind proxies need higher IP limits
3. **Balance Security**: Too strict = legitimate users locked out, too loose = vulnerable
4. **Monitor Effectiveness**: Review audit logs to see if thresholds are appropriate
5. **Document Changes**: Note why thresholds were changed in your runbook

### Audit Trail

All security setting changes are logged with:
- User who made the change
- Timestamp of change
- Old and new values
- IP address of requester

**Example Audit Entry:**
```json
{
  "event_type": "setting_updated",
  "user_id": "admin-123",
  "username": "admin",
  "resource_name": "security.max_failed_attempts",
  "details": {
    "old_value": "5",
    "new_value": "10"
  },
  "timestamp": 1732435200,
  "ip_address": "192.168.1.100"
}
```

---

## Server-Side Encryption (SSE)

**New in v0.4.2-beta**

MaxIOFS provides AES-256-CTR encryption at rest for all stored objects, protecting data from unauthorized filesystem access.

### Features

**Encryption Capabilities:**
- **AES-256-CTR Encryption**: Industry-standard 256-bit encryption
- **Streaming Encryption**: Constant memory usage (~32KB) for files of any size
- **Persistent Master Key**: Stored in `config.yaml`, survives server restarts
- **Flexible Control**: Dual-level encryption (server + bucket)
- **Automatic Decryption**: Transparent to S3 clients
- **Backward Compatible**: Mixed encrypted/unencrypted objects supported
- **Zero Performance Impact**: ~150+ MiB/s throughput maintained

**Security Properties:**
- Unique initialization vector (IV) per object
- Metadata-based encryption detection
- Master key validation on startup
- No key storage in metadata or logs

### Configuration

**Enable Encryption:**

1. **Generate Master Key:**
```bash
openssl rand -hex 32
```

2. **Configure in config.yaml:**
```yaml
storage:
  # Enable encryption for new object uploads
  enable_encryption: true

  # Master encryption key (AES-256)
  # ⚠️ CRITICAL: Must be EXACTLY 64 hexadecimal characters (32 bytes)
  # ⚠️ BACKUP THIS KEY SECURELY - Loss means PERMANENT data loss
  encryption_key: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
```

3. **Restart MaxIOFS:**
```bash
systemctl restart maxiofs
```

### Encryption Behavior

**Server-Level Control:**
- `enable_encryption: true` - New objects CAN be encrypted (if bucket also enabled)
- `enable_encryption: false` - New objects will NOT be encrypted
- `encryption_key` present - Existing encrypted objects remain accessible

**Bucket-Level Control:**
- Users choose encryption when creating buckets via Web Console
- Per-bucket encryption setting stored in bucket metadata
- Encryption occurs ONLY if BOTH server AND bucket encryption enabled

**Decryption:**
- Automatic for all encrypted objects (transparent to clients)
- Works even if `enable_encryption: false` (read-only mode)
- Mixed encrypted/unencrypted objects coexist in same bucket

### Key Management Best Practices

**⚠️ CRITICAL SECURITY WARNINGS:**

1. **NEVER commit encryption keys to version control**
   - Add `config.yaml` to `.gitignore`
   - Use environment variables or secret managers in production

2. **BACKUP the master key securely:**
   - Store in password manager (1Password, LastPass, Bitwarden)
   - Use encrypted vault or HSM for production
   - Losing the key means PERMANENT data loss

3. **Key rotation:**
   - Currently manual process
   - Changing key makes old encrypted objects unreadable
   - Plan rotation strategy carefully

4. **Access control:**
   - Restrict `config.yaml` file permissions (`chmod 600`)
   - Limit access to encryption key to authorized personnel only

### API Compatibility

Encryption is transparent to S3 clients:
- No API changes required
- Works with AWS CLI, SDKs, and third-party tools
- Objects automatically encrypted on upload (if enabled)
- Objects automatically decrypted on download

**Example (AWS CLI):**
```bash
# Upload (automatically encrypted if bucket has encryption enabled)
aws s3 cp file.txt s3://encrypted-bucket/

# Download (automatically decrypted)
aws s3 cp s3://encrypted-bucket/file.txt downloaded.txt
```

### Web Console Integration

**Bucket Creation:**
- Encryption checkbox visible only if server has `encryption_key` configured
- Warning displayed if server doesn't support encryption
- Users can choose encryption per bucket

**Visual Indicators:**
- Alert icons show encryption status
- Warning messages when encryption unavailable

### Performance Considerations

**Benchmarks** (Windows 11, Go 1.24):
- **1MB file**: ~200 MiB/s encryption, ~210 MiB/s decryption
- **10MB file**: ~180 MiB/s encryption, ~190 MiB/s decryption
- **100MB file**: ~150 MiB/s encryption, ~160 MiB/s decryption
- **Memory usage**: Constant ~32KB buffer
- **CPU overhead**: <5% for encryption/decryption

### Compliance & Standards

**Industry Standards:**
- ✅ AES-256 encryption (NIST approved)
- ✅ FIPS 140-2 compliant algorithm
- ✅ Data at rest protection
- ✅ Transparent encryption/decryption

**Limitations:**
- ⚠️ Metadata NOT encrypted (only object data)
- ⚠️ Single master key (no per-tenant keys yet)
- ⚠️ Manual key rotation required
- ⚠️ No HSM integration (planned for v0.5.0)

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
4. ~~**Limited Audit Logging**~~ - ✅ **COMPREHENSIVE AUDIT LOGGING** (v0.4.0-beta)
5. ~~**No Encryption at Rest**~~ - ✅ **AES-256 ENCRYPTION IMPLEMENTED** (v0.4.2-beta)
6. **Simple Rate Limiting** - Login endpoint only
7. **No Security Audits** - Not professionally audited by third parties
8. ✅ **Session Management** - Improved with idle timer and timeout enforcement (v0.3.1-beta)
9. **Limited Key Management** - Master key in config file (HSM planned for v0.5.0)
10. **SQLite Database** - No at-rest encryption (use LUKS/BitLocker for system disk)

### Mitigations

- Use strong passwords
- ✅ Enable 2FA for all users (especially admins)
- ✅ Enable server-side encryption (AES-256) for sensitive data
- Enable account lockout
- Configure firewall rules
- Use reverse proxy with TLS
- Filesystem-level encryption for system disk (LUKS/BitLocker)
- Restrict file permissions (especially `config.yaml` with encryption key)
- Backup encryption key securely
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
- [ ] Generate and configure encryption key (AES-256)
- [ ] Backup encryption key securely
- [ ] Configure HTTPS (reverse proxy)
- [ ] Set up firewall rules
- [ ] Run as non-root user
- [ ] Restrict file permissions (`chmod 600 config.yaml`)
- [ ] Configure CORS properly
- [ ] Enable rate limiting
- [ ] Set up backups (including encryption key)

**Ongoing:**
- [ ] Monitor logs
- [ ] Keep MaxIOFS updated
- [ ] Review user permissions
- [ ] Test backup restoration
- [ ] Update TLS certificates

---

**Note**: This is beta software. Conduct thorough security assessment and testing before production use.

---

**Version**: 0.4.2-beta
**Last Updated**: November 23, 2025
