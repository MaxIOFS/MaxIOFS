# MaxIOFS Security Guide

**Version**: 0.2.0-dev

> **ALPHA SOFTWARE DISCLAIMER**
>
> MaxIOFS is in **alpha stage**. Core security features are implemented but this software has not undergone comprehensive security audits or extensive penetration testing. Use in production at your own risk.

## Overview

MaxIOFS implements essential security features for object storage:

- Dual authentication (JWT + S3 signatures)
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

### 2. S3 API Authentication

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

### Alpha Security Limitations

1. **No Built-in TLS** - Use reverse proxy
2. **Basic Password Policy** - Only minimum length enforced
3. **No Multi-Factor Authentication** - Password-only
4. **Limited Audit Logging** - Basic auth events only
5. **No Encryption at Rest** - Use filesystem encryption
6. **Simple Rate Limiting** - Login endpoint only
7. **No Security Audits** - Not professionally audited
8. **No Session Invalidation** - Old tokens valid until expiry
9. **No Key Management** - Secrets in config files
10. **SQLite Database** - No at-rest encryption

### Mitigations

- Use strong passwords
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

**Note**: This is alpha software. Conduct security assessment before production use.

---

**Version**: 0.2.0-dev
**Last Updated**: October 2025
