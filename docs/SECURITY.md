# MaxIOFS Security Guide

## Overview

MaxIOFS implements multiple layers of security to protect your data and ensure compliance with industry standards. This guide covers security features, best practices, and hardening recommendations.

## Table of Contents

- [Authentication & Authorization](#authentication--authorization)
- [Password Security](#password-security)
- [Rate Limiting & Account Lockout](#rate-limiting--account-lockout)
- [Encryption](#encryption)
- [Network Security](#network-security)
- [Multi-Tenancy Isolation](#multi-tenancy-isolation)
- [Audit Logging](#audit-logging)
- [Security Hardening](#security-hardening)
- [Compliance](#compliance)

---

## Authentication & Authorization

### Dual Authentication System

MaxIOFS provides two authentication mechanisms:

#### 1. Console Authentication (JWT)
- **Purpose:** Web console access
- **Method:** Username/password with JWT tokens
- **Storage:** localStorage + HTTP-only cookies
- **Expiration:** Configurable (default: 1 hour)

```http
POST /api/auth/login
Content-Type: application/json

{
  "username": "admin",
  "password": "secure-password"
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

#### 2. S3 API Authentication (AWS Signature)
- **Purpose:** S3 API access
- **Method:** Access Key/Secret Key with AWS Signature V2/V4
- **Compatibility:** Full AWS S3 signature compatibility

**Signature V4 Example:**
```http
GET / HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20251005/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=...
X-Amz-Date: 20251005T120000Z
```

### Role-Based Access Control (RBAC)

MaxIOFS implements three role levels:

#### 1. Global Admin
- **Scope:** Full system access
- **Permissions:**
  - Manage all tenants
  - Create/delete/modify any resource
  - Access all buckets and objects
  - View system metrics
  - Unlock any user account
  - Modify system configuration

#### 2. Tenant Admin
- **Scope:** Single tenant
- **Permissions:**
  - Manage users within tenant
  - Create/delete buckets for tenant
  - Manage access keys for tenant users
  - View tenant metrics
  - Unlock tenant users

#### 3. Tenant User
- **Scope:** Limited tenant access
- **Permissions:**
  - Access assigned buckets
  - Upload/download objects
  - Manage own access keys
  - View own metrics

### Permission Enforcement

```go
// Backend permission check example
func (s *Server) checkBucketPermission(ctx context.Context, bucketName string, action string) error {
    user := auth.GetUserFromContext(ctx)

    // Global admins can do anything
    if user.TenantID == "" && hasRole(user, "admin") {
        return nil
    }

    // Check bucket ownership
    bucket, err := s.bucketManager.GetBucket(ctx, bucketName)
    if err != nil {
        return err
    }

    // Tenant admins can access their tenant's buckets
    if user.TenantID == bucket.TenantID && hasRole(user, "admin") {
        return nil
    }

    // Regular users need explicit permission
    return s.checkUserBucketPermission(user.ID, bucketName, action)
}
```

---

## Password Security

### Bcrypt Hashing

MaxIOFS uses **bcrypt** for password hashing with automatic migration from legacy SHA-256.

**Features:**
- Cost factor: 10 (default bcrypt cost)
- Salted hashing (unique per password)
- Resistant to rainbow table attacks
- Adaptive hashing (future-proof)

**Implementation:**
```go
import "golang.org/x/crypto/bcrypt"

// Hash password
func hashPassword(password string) (string, error) {
    hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    return string(hashedBytes), err
}

// Verify password
func verifyPassword(hashedPassword, password string) error {
    return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}
```

### Password Requirements

**Current Policy:**
- Minimum length: 8 characters
- No complexity requirements (yet)

**Recommended Policy (configure in production):**
- Minimum 12 characters
- At least 1 uppercase letter
- At least 1 lowercase letter
- At least 1 number
- At least 1 special character
- Password history (prevent reuse)

### Automatic Migration

MaxIOFS automatically migrates legacy SHA-256 passwords to bcrypt on login:

```go
// Try bcrypt first
err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
if err == nil {
    return user, nil  // bcrypt success
}

// Fallback to SHA-256 (legacy)
sha256Hash := sha256.Sum256([]byte(password))
if hex.EncodeToString(sha256Hash[:]) == user.PasswordHash {
    // Migrate to bcrypt
    newHash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    updateUserPassword(user.ID, string(newHash))
    return user, nil
}
```

---

## Rate Limiting & Account Lockout

### IP-Based Rate Limiting

MaxIOFS implements rate limiting on login attempts:

**Configuration:**
```yaml
rate_limit:
  enabled: true
  login_attempts: 5        # Max 5 attempts per minute
  lockout_duration: 900    # 15 minutes lockout
  max_failed_attempts: 5   # Lockout threshold
```

**Algorithm:**
```go
type LoginRateLimiter struct {
    attempts map[string]*LoginAttempt
    mu       sync.RWMutex
}

type LoginAttempt struct {
    Count     int
    FirstTry  time.Time
    LastTry   time.Time
}

func (l *LoginRateLimiter) AllowLogin(ip string) bool {
    // Reset after 1 minute
    if time.Since(attempt.FirstTry) > time.Minute {
        l.attempts[ip] = &LoginAttempt{Count: 1, FirstTry: time.Now()}
        return true
    }

    // Check rate limit
    if attempt.Count >= 5 {
        return false
    }

    attempt.Count++
    return true
}
```

### Account Lockout

**Lockout Flow:**
1. User fails login 5 times
2. Account locked for 15 minutes
3. `locked_until` timestamp set in database
4. Login blocked until expiry or manual unlock

**Database Schema:**
```sql
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    username TEXT UNIQUE,
    password_hash TEXT,
    failed_login_attempts INTEGER DEFAULT 0,
    locked_until INTEGER DEFAULT 0,
    last_failed_login INTEGER DEFAULT 0
);
```

### Manual Unlock

Admins can manually unlock accounts:

**API Endpoint:**
```http
POST /api/users/{userId}/unlock
Authorization: Bearer <admin-token>
```

**Permission Matrix:**
| Admin Type | Can Unlock |
|------------|-----------|
| Global Admin | Any user |
| Tenant Admin | Users in same tenant |
| Tenant User | ❌ No permission |

---

## Encryption

### Data at Rest

MaxIOFS supports AES-256-GCM encryption for stored objects:

**Configuration:**
```yaml
storage:
  encryption: true
  encryption_algorithm: aes-256-gcm
```

**Features:**
- AES-256 encryption with GCM mode
- Authenticated encryption (prevents tampering)
- Per-object encryption keys (derived from master key)
- Automatic encryption/decryption on upload/download

**Implementation:**
```go
import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
)

func encryptObject(data []byte, key []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }

    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }

    nonce := make([]byte, gcm.NonceSize())
    io.ReadFull(rand.Reader, nonce)

    ciphertext := gcm.Seal(nonce, nonce, data, nil)
    return ciphertext, nil
}
```

### Data in Transit

**Options:**

#### 1. Embedded TLS (Not Implemented Yet)
```yaml
security:
  tls_enabled: true
  tls_cert: /path/to/cert.pem
  tls_key: /path/to/key.pem
```

#### 2. Reverse Proxy (Recommended)
Use Nginx or Traefik with Let's Encrypt:

```nginx
server {
    listen 443 ssl http2;
    ssl_certificate /etc/letsencrypt/live/domain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/domain.com/privkey.pem;

    location / {
        proxy_pass http://localhost:8081;
    }
}
```

### Key Management

**Current:**
- Master key stored in configuration (not recommended for production)

**Recommended:**
- Use external KMS (AWS KMS, HashiCorp Vault, etc.)
- Environment variables for keys
- Hardware Security Module (HSM) for enterprise

---

## Network Security

### CORS Configuration

MaxIOFS allows configurable CORS for S3 API:

```yaml
cors:
  enabled: true
  allowed_origins:
    - "https://app.yourdomain.com"
    - "https://backup.yourdomain.com"
  allowed_methods: [GET, PUT, POST, DELETE, HEAD]
  allowed_headers: [Authorization, Content-Type, X-Amz-*]
  max_age: 3600
```

**Security Rules:**
- ✅ Specific origins in production
- ❌ Never use wildcard `*` in production
- ✅ Limit allowed methods
- ✅ Console API doesn't need CORS (monolithic)

### Firewall Rules

Recommended firewall configuration:

```bash
# UFW (Ubuntu)
ufw allow 22/tcp      # SSH
ufw allow 80/tcp      # HTTP (redirect to HTTPS)
ufw allow 443/tcp     # HTTPS
ufw deny 8080/tcp     # Block direct S3 API access
ufw deny 8081/tcp     # Block direct Console access
ufw enable

# iptables
iptables -A INPUT -p tcp --dport 22 -j ACCEPT
iptables -A INPUT -p tcp --dport 443 -j ACCEPT
iptables -A INPUT -p tcp --dport 8080 -j DROP   # S3 API
iptables -A INPUT -p tcp --dport 8081 -j DROP   # Console
```

**Best Practice:** Only expose 443 (HTTPS) via reverse proxy.

---

## Multi-Tenancy Isolation

### Data Isolation

MaxIOFS ensures complete tenant isolation:

**1. Database-Level Isolation:**
```sql
-- All resources have tenant_id
CREATE TABLE buckets (
    name TEXT PRIMARY KEY,
    tenant_id TEXT,
    owner_id TEXT,
    FOREIGN KEY(tenant_id) REFERENCES tenants(id)
);

CREATE TABLE users (
    id TEXT PRIMARY KEY,
    tenant_id TEXT,
    FOREIGN KEY(tenant_id) REFERENCES tenants(id)
);
```

**2. API-Level Filtering:**
```go
// Automatic tenant filtering
func (s *Server) handleListBuckets(w http.ResponseWriter, r *http.Request) {
    user := auth.GetUserFromContext(r.Context())

    buckets, _ := s.bucketManager.ListBuckets(r.Context())

    // Filter by tenant
    filtered := []Bucket{}
    for _, bucket := range buckets {
        // Global admin sees all
        if user.TenantID == "" {
            filtered = append(filtered, bucket)
            continue
        }

        // Tenant users see only their tenant's buckets
        if bucket.TenantID == user.TenantID {
            filtered = append(filtered, bucket)
        }
    }

    writeJSON(w, filtered)
}
```

**3. Storage-Level Isolation:**
```
/data/objects/
  ├── tenant-123/
  │   ├── bucket1/
  │   └── bucket2/
  └── tenant-456/
      └── bucket3/
```

### Quota Enforcement

Tenants have resource quotas:

```go
// Check storage quota before upload
func (s *Server) checkStorageQuota(tenantID string, uploadSize int64) error {
    tenant, _ := s.tenantManager.GetTenant(tenantID)

    if tenant.CurrentStorageBytes + uploadSize > tenant.MaxStorageBytes {
        return errors.New("storage quota exceeded")
    }

    return nil
}
```

**Quota Types:**
- Storage (bytes)
- Number of buckets
- Number of access keys

---

## Audit Logging

### Audit Log Format

MaxIOFS logs all security-relevant events:

```json
{
  "timestamp": "2025-10-05T12:00:00Z",
  "event_type": "login",
  "user_id": "user-123",
  "username": "admin",
  "ip_address": "192.168.1.100",
  "success": true,
  "tenant_id": "tenant-456",
  "metadata": {}
}
```

### Logged Events

**Authentication:**
- ✅ Login attempts (success/failure)
- ✅ Logout
- ✅ Token refresh
- ✅ Account lockout
- ✅ Account unlock

**Authorization:**
- ✅ Permission denied
- ✅ Role changes
- ✅ Access key creation/deletion

**Data Operations:**
- ✅ Bucket creation/deletion
- ✅ Object upload/download/deletion
- ✅ Object Lock operations
- ✅ Tenant creation/deletion

**Configuration:**
```yaml
monitoring:
  audit_logging: true
  audit_log_path: /var/log/maxiofs/audit.log
```

### Log Retention

**Recommendations:**
- Retain for 90+ days (compliance)
- Rotate logs daily
- Archive to external storage
- Encrypt archived logs

```bash
# Logrotate configuration
/var/log/maxiofs/audit.log {
    daily
    rotate 90
    compress
    delaycompress
    notifempty
    create 0640 maxiofs maxiofs
    postrotate
        systemctl reload maxiofs
    endscript
}
```

---

## Security Hardening

### 1. Change Default Credentials

**Immediately after installation:**
```bash
# Via Web Console
1. Login with admin/admin
2. Go to Users → admin
3. Change password

# Via API
curl -X PUT http://localhost:8081/api/users/admin \
  -H "Authorization: Bearer <token>" \
  -d '{"password": "new-secure-password"}'
```

### 2. Generate Strong JWT Secret

```bash
# Generate 32-byte random secret
openssl rand -base64 32

# Set in configuration
export MAXIOFS_SECURITY_JWT_SECRET="<generated-secret>"
```

### 3. Enable HTTPS

**Option 1: Reverse Proxy (Recommended)**
```nginx
server {
    listen 443 ssl http2;
    ssl_certificate /etc/letsencrypt/live/domain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/domain.com/privkey.pem;

    # Security headers
    add_header Strict-Transport-Security "max-age=31536000" always;
    add_header X-Content-Type-Options nosniff;
    add_header X-Frame-Options DENY;
    add_header X-XSS-Protection "1; mode=block";
}
```

### 4. Restrict File Permissions

```bash
# Data directory
chmod 750 /var/lib/maxiofs
chown maxiofs:maxiofs /var/lib/maxiofs

# Configuration
chmod 600 /etc/maxiofs/config.yaml
chown maxiofs:maxiofs /etc/maxiofs/config.yaml

# Binary
chmod 755 /opt/maxiofs/maxiofs
chown root:root /opt/maxiofs/maxiofs
```

### 5. Run as Non-Root User

```bash
# Create dedicated user
useradd -r -s /bin/false maxiofs

# Systemd service
[Service]
User=maxiofs
Group=maxiofs
```

### 6. Enable Rate Limiting

```yaml
rate_limit:
  enabled: true
  login_attempts: 5
  lockout_duration: 900
```

### 7. Regular Security Updates

```bash
# Update MaxIOFS
wget https://github.com/maxiofs/maxiofs/releases/latest/maxiofs-linux-amd64
chmod +x maxiofs-linux-amd64
systemctl stop maxiofs
mv maxiofs-linux-amd64 /opt/maxiofs/maxiofs
systemctl start maxiofs
```

---

## Compliance

### WORM Compliance

MaxIOFS supports Object Lock for WORM compliance:

**Features:**
- COMPLIANCE mode (immutable until expiry)
- GOVERNANCE mode (privileged delete allowed)
- Legal Hold (indefinite retention)

**Use Cases:**
- Financial records retention
- Healthcare data (HIPAA)
- Legal evidence preservation
- Backup immutability (ransomware protection)

### GDPR Compliance

**Data Protection:**
- ✅ Encryption at rest
- ✅ Access control (RBAC)
- ✅ Audit logging
- ✅ Right to erasure (object deletion)
- ✅ Data portability (S3 API)

**Recommendations:**
- Document data processing activities
- Implement data retention policies
- Enable audit logging
- Regular security audits

### SOC 2 / ISO 27001

**Security Controls:**
- ✅ Access control (RBAC)
- ✅ Authentication (MFA recommended)
- ✅ Audit logging
- ✅ Encryption
- ✅ Vulnerability management
- ✅ Incident response (via audit logs)

---

## Security Checklist

### Deployment Checklist

- [ ] Change default credentials
- [ ] Generate strong JWT secret
- [ ] Enable HTTPS/TLS
- [ ] Configure restrictive CORS
- [ ] Enable rate limiting
- [ ] Set up audit logging
- [ ] Configure log retention
- [ ] Restrict file permissions
- [ ] Run as non-root user
- [ ] Enable firewall rules
- [ ] Set up monitoring/alerting
- [ ] Regular backups
- [ ] Incident response plan

### Ongoing Security

- [ ] Regular security updates
- [ ] Review audit logs weekly
- [ ] Rotate JWT secret quarterly
- [ ] Review user permissions monthly
- [ ] Security penetration testing annually
- [ ] Backup testing monthly
- [ ] Disaster recovery drills quarterly

---

## Incident Response

### Account Compromise

1. **Immediate Actions:**
   ```bash
   # Lock account
   curl -X POST http://localhost:8081/api/users/{userId}/lock

   # Revoke access keys
   curl -X DELETE http://localhost:8081/api/access-keys/{keyId}

   # Review audit logs
   grep "user-123" /var/log/maxiofs/audit.log
   ```

2. **Investigation:**
   - Review audit logs for suspicious activity
   - Check IP addresses
   - Identify compromised resources
   - Assess data exposure

3. **Remediation:**
   - Reset password
   - Generate new access keys
   - Review and update permissions
   - Notify affected parties (if required)

### Data Breach

1. **Contain:**
   - Identify affected buckets/objects
   - Revoke unauthorized access
   - Enable Object Lock on sensitive data

2. **Assess:**
   - Determine scope of breach
   - Identify data types exposed
   - Review compliance requirements

3. **Notify:**
   - Internal stakeholders
   - Affected users (GDPR: within 72 hours)
   - Regulatory authorities (if required)

---

## Security Contact

For security vulnerabilities, please report to:
- Email: security@maxiofs.io
- PGP Key: [Key ID]
- Bug Bounty: https://maxiofs.io/security/bounty

**Do not disclose vulnerabilities publicly until patched.**
