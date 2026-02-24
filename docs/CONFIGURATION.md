# MaxIOFS Configuration Guide

**Version**: 0.9.2-beta | **Last Updated**: February 22, 2026

## Configuration Architecture

MaxIOFS uses **hybrid configuration**:

| Type | Storage | Restart Required | Managed Via |
|------|---------|-----------------|-------------|
| **Static** | `config.yaml` / env vars / CLI flags | Yes | File editor, environment |
| **Dynamic** | SQLite (`maxiofs.db`) | No | Web Console (`/settings`) or REST API |

**Priority order** (highest wins):
1. CLI flags (`--data-dir /data`)
2. Environment variables (`MAXIOFS_DATA_DIR=/data`)
3. Config file (`config.yaml`)
4. Defaults

---

## Static Configuration

### Minimal Setup

Only `data_dir` is required. Everything else has sensible defaults:

```bash
./maxiofs --data-dir /var/lib/maxiofs
```

### Complete Config File

```yaml
# config.yaml
listen: ":8080"                              # S3 API listen address
console_listen: ":8081"                      # Web Console listen address
data_dir: "/var/lib/maxiofs"                 # Data directory (REQUIRED)
log_level: "info"                            # debug | info | warn | error
public_api_url: "https://s3.example.com"     # Public S3 URL (for presigned URLs)
public_console_url: "https://console.example.com"  # Public Console URL (for OAuth redirects)

# TLS (optional — reverse proxy recommended instead)
enable_tls: false
cert_file: ""
key_file: ""

# Trusted proxies (private networks trusted automatically)
trusted_proxies: []

# Storage
storage:
  backend: "filesystem"           # Only supported backend
  root: ""                        # Default: {data_dir}/objects
  enable_encryption: false        # AES-256-CTR at rest
  encryption_key: ""              # 64 hex chars (32 bytes). Generate: openssl rand -hex 32
  enable_object_lock: true        # S3 Object Lock / WORM retention

# Authentication
auth:
  enable_auth: true
  jwt_secret: ""                  # Auto-generated if empty (32 chars, random)

# Audit logging
audit:
  enable: true
  retention_days: 90
  db_path: ""                     # Default: {data_dir}/audit.db

# Metrics
metrics:
  enable: true
  path: "/metrics"
  interval: 60                    # Collection interval (seconds)
```

### Data Directory Structure

When MaxIOFS starts, it creates this structure under `data_dir`:

```
{data_dir}/
├── db/
│   └── maxiofs.db       ← SQLite: auth, users, tenants, keys, settings, cluster, IDP
├── audit.db             ← SQLite: audit logs (separate for isolation)
├── metadata/            ← Pebble: object metadata
└── objects/             ← Filesystem: object data
```

---

## CLI Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--config` | `-c` | — | Path to config file |
| `--data-dir` | `-d` | — | Data directory (**required**) |
| `--listen` | `-l` | `:8080` | S3 API listen address |
| `--console-listen` | — | `:8081` | Web Console listen address |
| `--log-level` | — | `info` | Log level (debug/info/warn/error) |
| `--tls-cert` | — | — | TLS certificate path (must be paired with `--tls-key`) |
| `--tls-key` | — | — | TLS private key path (must be paired with `--tls-cert`) |

---

## Environment Variables

All settings can be set via `MAXIOFS_` prefixed environment variables:

**Core:**
- `MAXIOFS_DATA_DIR` — Data directory
- `MAXIOFS_LISTEN` — S3 API listen address
- `MAXIOFS_CONSOLE_LISTEN` — Console listen address
- `MAXIOFS_LOG_LEVEL` — Log level
- `MAXIOFS_PUBLIC_API_URL` — Public S3 URL
- `MAXIOFS_PUBLIC_CONSOLE_URL` — Public Console URL

**TLS:**
- `MAXIOFS_ENABLE_TLS` — Enable TLS (true/false)
- `MAXIOFS_CERT_FILE` — TLS certificate path
- `MAXIOFS_KEY_FILE` — TLS private key path

**Storage:**
- `MAXIOFS_STORAGE_BACKEND` — Storage backend
- `MAXIOFS_STORAGE_ROOT` — Objects root directory
- `MAXIOFS_STORAGE_ENABLE_ENCRYPTION` — Enable encryption
- `MAXIOFS_STORAGE_ENCRYPTION_KEY` — Master encryption key (hex)

**Auth:**
- `MAXIOFS_AUTH_ENABLE_AUTH` — Enable authentication
- `MAXIOFS_AUTH_JWT_SECRET` — JWT signing secret

**Cluster:**
- `MAXIOFS_CLUSTER_NODE_NAME` — Node name for cluster
- `MAXIOFS_CLUSTER_REGION` — Geographic region

---

## Dynamic Settings

Runtime-configurable settings via Web Console (`/settings`) or API. Changes take effect **immediately** without restart.

### Security Settings

| Key | Default | Description |
|-----|---------|-------------|
| `security.ratelimit_login_per_minute` | 5 | IP-based login rate limit |
| `security.max_failed_attempts` | 5 | Failed logins before account lockout |
| `security.lockout_duration` | 900 | Lockout duration (seconds) |
| `security.session_timeout` | 86400 | JWT session lifetime (seconds) |
| `security.password_min_length` | 8 | Minimum password length |
| `security.require_2fa_admins` | false | Force 2FA for admin accounts |
| `security.cors_allowed_origins` | * | CORS allowed origins |
| `security.idle_timeout` | 3600 | Session idle timeout (seconds) |
| `security.max_sessions_per_user` | 5 | Maximum concurrent sessions |

### Audit Settings

| Key | Default | Description |
|-----|---------|-------------|
| `audit.enabled` | true | Enable audit logging |
| `audit.retention_days` | 90 | Log retention period (days) |
| `audit.log_read_operations` | false | Log object read events |
| `audit.log_list_operations` | false | Log list events |

### Storage Settings

| Key | Default | Description |
|-----|---------|-------------|
| `storage.max_multipart_parts` | 10000 | Max parts per multipart upload |
| `storage.multipart_part_size_min` | 5242880 | Minimum part size (bytes, 5MB) |
| `storage.max_object_size` | 5368709120 | Max object size (bytes, 5GB) |
| `storage.temp_cleanup_interval` | 3600 | Temp file cleanup interval (seconds) |

### Metrics Settings

| Key | Default | Description |
|-----|---------|-------------|
| `metrics.enabled` | true | Enable Prometheus metrics |
| `metrics.retention_hours` | 24 | Metrics history retention |

### System Settings

| Key | Default | Description |
|-----|---------|-------------|
| `system.log_level` | info | Runtime log level |
| `system.max_concurrent_uploads` | 100 | Upload concurrency limit |

### Settings API

```bash
# List all settings
GET /api/v1/settings

# Get settings by category
GET /api/v1/settings/category/{category}

# Update a setting
PUT /api/v1/settings/{key}
{ "value": "15" }

# Reset to default
POST /api/v1/settings/reset
```

---

## Configuration Examples

### Development

```bash
./maxiofs --data-dir ./data --log-level debug
```

### Production (Behind Reverse Proxy)

```yaml
# /etc/maxiofs/config.yaml
data_dir: /var/lib/maxiofs
listen: 127.0.0.1:8080
console_listen: 127.0.0.1:8081
public_api_url: https://s3.example.com
public_console_url: https://console.example.com
log_level: info

storage:
  enable_encryption: true
  encryption_key: "a1b2c3d4e5f6...64_hex_chars"

audit:
  retention_days: 180
```

### Docker

```yaml
# docker-compose.yaml
services:
  maxiofs:
    image: maxiofs:latest
    environment:
      MAXIOFS_DATA_DIR: /data
      MAXIOFS_PUBLIC_API_URL: https://s3.example.com
      MAXIOFS_PUBLIC_CONSOLE_URL: https://console.example.com
      MAXIOFS_STORAGE_ENABLE_ENCRYPTION: "true"
      MAXIOFS_STORAGE_ENCRYPTION_KEY: "a1b2c3d4...64_hex_chars"
    volumes:
      - maxiofs-data:/data
    ports:
      - "8080:8080"
      - "8081:8081"
```

### Cluster Node

```yaml
data_dir: /var/lib/maxiofs
listen: :8080
console_listen: :8081
public_api_url: https://node1.s3.example.com
public_console_url: https://node1.console.example.com
```

Cluster configuration (nodes, replication, sync) is managed via the Web Console or API — not config files. See [CLUSTER.md](CLUSTER.md).

---

**See also**: [DEPLOYMENT.md](DEPLOYMENT.md) · [SECURITY.md](SECURITY.md) · [CLUSTER.md](CLUSTER.md)
