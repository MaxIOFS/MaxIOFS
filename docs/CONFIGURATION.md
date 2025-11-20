
# MaxIOFS Configuration Guide

**Version**: 0.4.2-beta

Configuration reference for MaxIOFS v0.4.2-beta

---

## Table of Contents

- [Configuration Architecture](#configuration-architecture)
- [Configuration Methods](#configuration-methods)
- [Required Settings](#required-settings)
- [Server Settings](#server-settings)
- [TLS/HTTPS](#tlshttps)
- [Storage](#storage)
- [Server-Side Encryption (SSE)](#server-side-encryption-sse)
- [Authentication](#authentication)
- [Audit Logging](#audit-logging)
- [Dynamic Settings](#dynamic-settings)
- [Examples](#examples)

---

## Configuration Architecture

**New in v0.4.0**: MaxIOFS uses a **dual-configuration system** to separate infrastructure settings from operational policies:

### 1. Static Configuration (config.yaml)

Infrastructure settings that require server restart:
- Server ports and addresses (`listen`, `console_listen`)
- Data directory paths (`data_dir`)
- TLS certificates (`cert_file`, `key_file`)
- Storage backend selection (`storage.backend`)
- Encryption keys (`storage.encryption_key`)
- JWT secrets (`auth.jwt_secret`)

**Configured via**: `config.yaml`, environment variables, or CLI flags

**Changes require**: Server restart

### 2. Dynamic Configuration (Database)

Runtime settings that take effect immediately:
- Security policies (password requirements, session timeouts, rate limits)
- Audit configuration (retention days, operation logging)
- Storage defaults (versioning, compression, object lock)
- Metrics collection (enable/disable, intervals)
- System settings (maintenance mode, upload limits)

**Configured via**: Web Console (`/settings` page) or Settings API

**Changes require**: No restart (immediate effect)

**Storage**: SQLite database (`{data_dir}/auth.db`)

### Benefits

- **Infrastructure changes are deliberate** - Require intentional restart
- **Security policies are flexible** - Adjust on-the-fly without downtime
- **Settings are versioned** - config.yaml in git, DB managed by admins
- **Zero downtime configuration** - Change policies during operation

See [Dynamic Settings](#dynamic-settings) section for complete list of runtime-configurable settings.

---


## Configuration Methods


MaxIOFS supports three configuration methods for **static settings** (priority order):


1.  **Command-line flags** (highest)

2.  **Environment variables** (`MAXIOFS_` prefix)

3.  **Configuration file** (YAML/JSON/TOML)

  

---

  

## Required Settings

  

### Data Directory (Required)

  

Must be configured via flag, environment variable, or config file:

  



#### Method 1: Flag
```bash
./maxiofs  --data-dir  /var/lib/maxiofs
```
  

#### Method 2: Environment
```bash
export  MAXIOFS_DATA_DIR=/var/lib/maxiofs

./maxiofs
```
  

#### Method 3: Config file
```bash
./maxiofs  --config  config.yaml

```

  ---

  

## Server Settings


|       Parameter      |  Type  |     Default   |     Description     |
|----------------------|--------|---------------|---------------------|
| `data_dir`           | string | **required**  | Data directory path |
| `listen`             | string |    `:8080`    | **Bind address** for S3 API server |
| `console_listen`     | string |    `:8081`    | **Bind address** for web console |
| `log_level`          | string |     `info`    | Log level (debug/info/warn/error) |
| `public_api_url`     | string | `http://localhost:8080` | **External URL** for S3 API (used in generated links) |
| `public_console_url` | string | `http://localhost:8081` | **External URL** for web console (used in generated links) |



### Understanding Listen vs Public URLs

**Important:** `listen` and `console_listen` define **where the server binds** (network interface), while `public_api_url` and `public_console_url` define **the external URLs** used for generating links, shares, and presigned URLs.

**Use cases:**

1. **Direct access (no proxy):**
   - `listen: ":8080"` → Server listens on all interfaces, port 8080
   - `public_api_url: "http://localhost:8080"` → Clients access via localhost

2. **Behind reverse proxy (RECOMMENDED):**
   - `listen: "localhost:8080"` → Server only accessible from localhost
   - `public_api_url: "https://s3.midominio.com"` → Public domain (nginx forwards to localhost)
   - This ensures generated share links use the public domain, not localhost

**Example config.yaml:**

```yaml
data_dir: "/var/lib/maxiofs"

# Server binds to localhost only (not directly exposed)
listen: "localhost:8080"
console_listen: "localhost:8081"

log_level: "info"

# Public URLs (what users access via reverse proxy)
public_api_url: "https://s3.example.com"
public_console_url: "https://s3.example.com/ui"
```

  

---

  

## TLS/HTTPS

  

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `enable_tls` | bool | `false` | Enable TLS |
| `cert_file` | string | - | Certificate path |
| `key_file` | string | - | Private key path |

  

**Direct TLS:**

```yaml
enable_tls: true
cert_file: "/etc/maxiofs/tls/cert.pem"
key_file: "/etc/maxiofs/tls/key.pem"
```

  

**Reverse Proxy (Recommended):**

```yaml
# MaxIOFS listens on localhost only
listen: "localhost:8080"
console_listen: "localhost:8081"
enable_tls: false  # nginx handles TLS

# Public URLs match your nginx configuration
public_api_url: "https://s3.example.com"
public_console_url: "https://s3.example.com/ui"
```

**Nginx Configuration Example:**

```nginx
server {
    listen 443 ssl http2;
    server_name s3.example.com;

    ssl_certificate /etc/letsencrypt/live/s3.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/s3.example.com/privkey.pem;

    # S3 API (root path)
    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Required for large uploads
        client_max_body_size 0;
    }

    # Web Console UI
    location /ui {
        proxy_pass http://localhost:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```



---

  

## Storage

  

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `storage.backend` | string | `filesystem` | Storage type |
| `storage.enable_compression` | bool | `false` | Enable compression |
| `storage.compression_type` | string | `gzip` | Type (gzip/lz4/zstd) |
| `storage.compression_level` | int | `6` | Level (1-9) |
| `storage.enable_object_lock` | bool | `true` | Object Lock |
| `storage.enable_encryption` | bool | `false` | Enable server-side encryption (SSE) |
| `storage.encryption_key` | string | - | Master encryption key (64 hex chars) |



**Example:**

```yaml
storage:
  backend: "filesystem"
  enable_compression: true
  compression_type: "zstd"
  compression_level: 3
  enable_object_lock: true
  enable_encryption: true
  encryption_key: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
```

---

## Server-Side Encryption (SSE)

**New in v0.4.2-beta**

MaxIOFS supports AES-256-CTR encryption at rest for all stored objects.

### Configuration Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `storage.enable_encryption` | bool | No | `false` | Enable encryption for new object uploads |
| `storage.encryption_key` | string | Yes (if encryption enabled) | - | Master encryption key (must be exactly 64 hexadecimal characters = 32 bytes) |

### Setup Instructions

**1. Generate Master Key:**

```bash
# Generate a cryptographically secure 32-byte (256-bit) key
openssl rand -hex 32
```

**2. Configure in config.yaml:**

```yaml
storage:
  # Enable encryption for new uploads
  enable_encryption: true

  # Master Encryption Key (AES-256)
  # ⚠️ CRITICAL: Must be EXACTLY 64 hexadecimal characters (32 bytes)
  # ⚠️ BACKUP THIS KEY SECURELY - Loss means PERMANENT data loss
  # Generate with: openssl rand -hex 32
  encryption_key: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
```

**3. Restart Server:**

```bash
systemctl restart maxiofs
```

### Encryption Behavior

**Dual-Level Control:**
- **Server-Level**: `enable_encryption` flag (config.yaml)
  - `true`: New objects CAN be encrypted (if bucket also enabled)
  - `false`: New objects will NOT be encrypted

- **Bucket-Level**: Per-bucket encryption setting (Web Console)
  - Users choose encryption when creating buckets
  - Encryption happens ONLY if BOTH server AND bucket enabled

**Decryption:**
- Automatic for all encrypted objects (transparent to S3 clients)
- Works even if `enable_encryption: false` (read-only mode)
- Mixed encrypted/unencrypted objects supported in same bucket

**Key Persistence:**
- Master key loaded at startup (survives restarts)
- If `encryption_key` exists, encrypted objects remain accessible
- Removing `encryption_key` makes encrypted objects UNREADABLE

### Security Warnings

**⚠️ CRITICAL:**

1. **NEVER commit encryption keys to version control**
   - Add `config.yaml` to `.gitignore`
   - Use environment variables in production:
     ```bash
     export MAXIOFS_STORAGE_ENCRYPTION_KEY="your-64-char-hex-key"
     ```

2. **BACKUP the master key securely:**
   - Store in password manager (1Password, LastPass, Bitwarden)
   - Use encrypted vault or HSM for production
   - **Losing the key means PERMANENT data loss for encrypted objects**

3. **Key rotation:**
   - Currently manual process
   - Changing key makes old encrypted objects UNREADABLE
   - Plan rotation strategy carefully (not recommended for production)

4. **File permissions:**
   ```bash
   chmod 600 config.yaml  # Restrict access to owner only
   ```

### Performance

**Benchmarks** (Windows 11, Go 1.24):
- **1MB file**: ~200 MiB/s encryption, ~210 MiB/s decryption
- **10MB file**: ~180 MiB/s encryption, ~190 MiB/s decryption
- **100MB file**: ~150 MiB/s encryption, ~160 MiB/s decryption
- **Memory usage**: Constant ~32KB buffer (streaming encryption)
- **CPU overhead**: <5% for encryption/decryption operations

### Web Console Integration

**Bucket Creation:**
- Encryption checkbox visible only if server has `encryption_key` configured
- Warning displayed if server doesn't support encryption
- Users can choose encryption per bucket

**Visual Indicators:**
- Alert icons show encryption status
- Warning messages when encryption unavailable

### Environment Variable Support

```bash
# Enable encryption via environment variable
export MAXIOFS_STORAGE_ENABLE_ENCRYPTION=true

# Set encryption key via environment variable (recommended for production)
export MAXIOFS_STORAGE_ENCRYPTION_KEY="0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

# Start server
./maxiofs --data-dir /var/lib/maxiofs
```

### Compliance

**Standards:**
- ✅ AES-256 encryption (NIST approved)
- ✅ FIPS 140-2 compliant algorithm
- ✅ Data at rest protection

**Limitations:**
- ⚠️ Metadata NOT encrypted (only object data)
- ⚠️ Single master key (no per-tenant keys)
- ⚠️ Manual key rotation
- ⚠️ No HSM integration (planned for v0.5.0)

---

## Authentication

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `auth.enable_auth` | bool | `true` | Enable auth |
| `auth.jwt_secret` | string | auto | JWT secret |

**⚠️ Security:**

-  **Default admin**: `admin` / `admin` - **Change password!**
-  **No default S3 keys** - Create via web console

**Create Access Keys:**

1. Login: `http://localhost:8081` (admin/admin)
2. Navigate to Users
3. Click "Create Access Key"
4. Save credentials securely

**Generate JWT secret:**

```bash
openssl  rand  -base64  32
```

**Example:**

```yaml
auth:
enable_auth: true
jwt_secret: "your-secure-secret-min-32-chars"
```

---

## Audit Logging

**New in v0.4.0-beta**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `audit.enabled` | bool | `true` | Enable audit logging |
| `audit.retention_days` | int | `90` | Auto-delete logs older than N days |
| `audit.db_path` | string | `./data/audit_logs.db` | SQLite database path |

**Features:**
- Tracks 20+ event types (authentication, user management, buckets, 2FA, etc.)
- Automatic retention management with daily cleanup
- Multi-tenant isolation (global/tenant admin access)
- Compliance-ready (GDPR, SOC 2, HIPAA, ISO 27001, PCI DSS)

**Example:**

```yaml
audit:
  enabled: true
  retention_days: 90
  db_path: "./data/audit_logs.db"
```

**Environment Variables:**

```bash
export AUDIT_ENABLED=true
export AUDIT_RETENTION_DAYS=90
export AUDIT_DB_PATH="./data/audit_logs.db"
```

**Disable Audit Logging:**

```yaml
audit:
  enabled: false
```

**Web Console Access:**
- Audit logs available at: `/audit-logs`
- Access restricted to global admins and tenant admins
- Features: advanced filtering, search, CSV export, quick date filters

---

## Examples

### Development

```yaml
data_dir: "./data"
log_level: "debug"
```

### Production (Direct TLS)

```yaml
data_dir: "/var/lib/maxiofs"
listen: ":8080"
console_listen: ":8081"
log_level: "info"

public_api_url: "https://s3.example.com:8080"
public_console_url: "https://console.example.com:8081"

enable_tls: true
cert_file: "/etc/letsencrypt/live/s3.example.com/fullchain.pem"
key_file: "/etc/letsencrypt/live/s3.example.com/privkey.pem"

storage:
  enable_compression: true
  compression_type: "zstd"
  compression_level: 3

auth:
  enable_auth: true
  jwt_secret: "your-secure-secret"

metrics:
  enable: true
  interval: 60
```

### Production (Behind Reverse Proxy - RECOMMENDED)

```yaml
data_dir: "/var/lib/maxiofs"

# Listen on localhost only (nginx handles public traffic)
listen: "localhost:8080"
console_listen: "localhost:8081"

log_level: "info"

# Public URLs (what users access)
public_api_url: "https://s3.midominio.com"
public_console_url: "https://s3.midominio.com/ui"

# No TLS (nginx handles it)
enable_tls: false

storage:
  enable_compression: true
  compression_type: "zstd"
  compression_level: 3

auth:
  enable_auth: true
  jwt_secret: "your-secure-secret"

metrics:
  enable: true
  interval: 60
```

### Docker

```yaml
version: '3.8'
services:
maxiofs:
image: maxiofs:latest
ports:
- "8080:8080"
- "8081:8081"
volumes:
- ./data:/data
- ./config.yaml:/etc/maxiofs/config.yaml:ro
environment:
- MAXIOFS_DATA_DIR=/data
command: ["--config", "/etc/maxiofs/config.yaml"]
```

---

## Dynamic Settings

**New in v0.4.0**: Runtime-configurable settings stored in SQLite database.

### Overview

Dynamic settings can be modified through:
1. **Web Console**: Navigate to `/settings` (Global Admins only)
2. **Settings API**: RESTful endpoints for programmatic access

All changes:
- Take effect immediately (no restart required)
- Are logged in audit system
- Are validated by type (string, int, bool, json)
- Require global admin permissions

### Settings API Endpoints

```
GET    /api/v1/settings                     # List all settings
GET    /api/v1/settings?category=security   # Filter by category
GET    /api/v1/settings/categories          # List categories
GET    /api/v1/settings/:key                # Get specific setting
PUT    /api/v1/settings/:key                # Update single setting
POST   /api/v1/settings/bulk                # Bulk update (transactional)
```

### Available Settings (23 total)

#### Security Category (11 settings)

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `security.session_timeout` | int | 86400 | JWT session duration in seconds (24 hours) |
| `security.max_failed_attempts` | int | 5 | Login attempts before account lockout |
| `security.lockout_duration` | int | 900 | Account lockout duration in seconds (15 minutes) |
| `security.require_2fa_admin` | bool | false | Require 2FA for all admin users |
| `security.password_min_length` | int | 8 | Minimum password length |
| `security.password_require_uppercase` | bool | true | Require uppercase letters in passwords |
| `security.password_require_numbers` | bool | true | Require numbers in passwords |
| `security.password_require_special` | bool | false | Require special characters in passwords |
| `security.ratelimit_enabled` | bool | true | Enable rate limiting |
| `security.ratelimit_login_per_minute` | int | 5 | Maximum login attempts per minute per IP |
| `security.ratelimit_api_per_second` | int | 100 | Maximum API requests per second per user |

#### Audit Category (4 settings)

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `audit.enabled` | bool | true | Enable audit logging |
| `audit.retention_days` | int | 90 | Audit log retention period in days |
| `audit.log_s3_operations` | bool | true | Log S3 API operations |
| `audit.log_console_operations` | bool | true | Log Console API operations |

#### Storage Category (4 settings)

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `storage.default_bucket_versioning` | bool | false | Enable versioning by default for new buckets |
| `storage.default_object_lock_days` | int | 7 | Default object lock retention period in days |
| `storage.enable_compression` | bool | false | Enable transparent compression for objects |
| `storage.compression_level` | int | 6 | Compression level (1-9, higher = better compression) |

#### Metrics Category (2 settings)

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `metrics.enabled` | bool | true | Enable Prometheus metrics endpoint |
| `metrics.collection_interval` | int | 10 | Metrics collection interval in seconds |

#### System Category (2 settings)

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `system.maintenance_mode` | bool | false | Enable maintenance mode (read-only access) |
| `system.max_upload_size_mb` | int | 5120 | Maximum upload size in MB (5GB default) |

### Example: Update Single Setting

```bash
curl -X PUT http://localhost:8081/api/v1/settings/security.session_timeout \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"value": "7200"}'
```

### Example: Bulk Update

```bash
curl -X POST http://localhost:8081/api/v1/settings/bulk \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "settings": {
      "security.session_timeout": "7200",
      "security.max_failed_attempts": "3",
      "audit.retention_days": "180"
    }
  }'
```

### Web Console UI

Navigate to **Settings** page (`http://localhost:8081/settings`) for:
- Category-based tabbed interface
- Real-time editing with change tracking
- Visual status indicators (● Enabled / ○ Disabled)
- Human-readable value formatting (hours, days, MB, etc.)
- Bulk save with transaction support
- Full audit integration

---


## Additional Resources

- [API Documentation](API.md)
- [Security Guide](SECURITY.md)
- [Deployment Guide](DEPLOYMENT.md)
- Complete reference: `config.example.yaml`