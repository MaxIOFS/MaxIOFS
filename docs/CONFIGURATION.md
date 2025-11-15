
# MaxIOFS Configuration Guide

**Version**: 0.4.0-beta

Configuration reference for MaxIOFS v0.4.0-beta
  
---

## Table of Contents

- [Configuration Methods](#configuration-methods)

- [Required Settings](#required-settings)

- [Server Settings](#server-settings)

- [TLS/HTTPS](#tlshttps)

- [Storage](#storage)

- [Authentication](#authentication)

- [Audit Logging](#audit-logging)

- [Examples](#examples)

---
  

## Configuration Methods


MaxIOFS supports three configuration methods (priority order):


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

  

**Example:**

```yaml
storage:
backend: "filesystem"
enable_compression: true
compression_type: "zstd"
compression_level: 3
enable_object_lock: true
```

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


## Additional Resources

- [API Documentation](API.md)
- [Security Guide](SECURITY.md)
- [Deployment Guide](DEPLOYMENT.md)
- Complete reference: `config.example.yaml`