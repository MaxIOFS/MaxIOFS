# MaxIOFS Configuration Guide

**Version**: 0.6.2-beta
**Last Updated**: December 13, 2025

---

## Configuration Architecture

MaxIOFS uses **hybrid configuration** for flexibility:

| Type | Storage | Use Case | Examples |
|------|---------|----------|----------|
| **Static** | config.yaml | Infrastructure settings | Data directory, TLS, ports |
| **Dynamic** | SQLite database | Runtime tunable settings | Rate limits, retention, quotas |

**Benefits:**
- Static: Server startup configuration (requires restart)
- Dynamic: Modify at runtime without restart (via Web Console or API)

---

## Configuration Methods

**Priority order** (highest to lowest):
1. Command-line flags (`--data-dir /data`)
2. Environment variables (`MAXIOFS_DATA_DIR=/data`)
3. Config file (`config.yaml`)

---

## Required Settings

### Data Directory (Required)

**Where MaxIOFS stores all data** (buckets, metadata, config, audit logs).

**Methods:**
```bash
# Flag (highest priority)
./maxiofs --data-dir /opt/maxiofs/data

# Environment variable
export MAXIOFS_DATA_DIR=/opt/maxiofs/data
./maxiofs

# Config file
# config.yaml
data_dir: /opt/maxiofs/data
```

**Directory structure:**
```
/opt/maxiofs/data/
├── buckets/           # Object storage
├── auth.db            # Authentication database
├── audit.db           # Audit logs database
├── config_store.db    # Dynamic configuration
└── master_key.key     # Encryption key (if enabled)
```

---

## Server Settings

### Listen Addresses

| Setting | Flag | Env Var | Default | Description |
|---------|------|---------|---------|-------------|
| S3 API | `--listen` | `MAXIOFS_LISTEN` | `:8080` | S3 API endpoint |
| Console | `--console-listen` | `MAXIOFS_CONSOLE_LISTEN` | `:8081` | Web Console endpoint |

### Public URLs

**Required when behind reverse proxy:**

| Setting | Flag | Env Var | Example |
|---------|------|---------|---------|
| S3 Public URL | `--public-url` | `MAXIOFS_PUBLIC_URL` | `https://s3.example.com` |
| Console Public URL | `--console-public-url` | `MAXIOFS_CONSOLE_PUBLIC_URL` | `https://console.example.com` |

**Use case:** Reverse proxy maps external URL to internal listen address.

---

## TLS/HTTPS

**Direct TLS** (without reverse proxy):

```yaml
# config.yaml
tls:
  enabled: true
  cert_file: /etc/maxiofs/server.crt
  key_file: /etc/maxiofs/server.key
```

**Recommended:** Use reverse proxy (Nginx/HAProxy) for TLS termination (see [DEPLOYMENT.md](DEPLOYMENT.md))

**Generate self-signed cert** (testing only):
```bash
openssl req -x509 -newkey rsa:4096 -keyout key.pem -out cert.pem -days 365 -nodes
```

---

## Storage

**Default:** Filesystem backend (no additional configuration)

**Filesystem settings:**

| Setting | Default | Description |
|---------|---------|-------------|
| `data_dir` | (required) | Base directory for all storage |
| `buckets_dir` | `{data_dir}/buckets` | Object storage location |

**Future:** Cloud backends (S3, GCS, Azure) planned for v0.8.0

---

## Server-Side Encryption (SSE)

**AES-256-CTR streaming encryption** at rest.

### Configuration

```yaml
# config.yaml
encryption:
  enabled: true
  master_key: /etc/maxiofs/keys/master_key.key
```

**Generate master key:**
```bash
openssl rand -hex 32 > /etc/maxiofs/keys/master_key.key
chmod 400 /etc/maxiofs/keys/master_key.key
```

**Per-bucket encryption:** Can be enabled/disabled via Web Console (overrides server default)

**See [SECURITY.md](SECURITY.md#server-side-encryption-sse) for detailed encryption documentation**

---

## Authentication

**Default credentials:** admin/admin (⚠️ **CHANGE IMMEDIATELY**)

**Session timeout:**
```yaml
# config.yaml
auth:
  session_timeout: 86400  # 24 hours (seconds)
```

**Supported authentication:**
- JWT (Web Console)
- S3 Signature V2/V4 (S3 API)
- HMAC-SHA256 (Cluster)

**See [SECURITY.md](SECURITY.md#authentication) for complete authentication documentation**

---

## Audit Logging

**Default:** Enabled with 90-day retention

```yaml
# config.yaml
audit:
  enabled: true
  retention_days: 90
```

**Storage:** SQLite database (`audit.db`)

**See [SECURITY.md](SECURITY.md#audit-logging) for event types and configuration**

---

## Cluster Configuration

**Cluster configuration is managed via Web Console or API** - no config file required.

### Key Cluster Settings

| Setting | Default | Configurable | Description |
|---------|---------|--------------|-------------|
| Health check interval | 30s | Code only | Node health monitoring frequency |
| Cache TTL | 5 min | Code only | Bucket location cache lifetime |
| Node priority | 100 | Web Console/API | Routing priority (lower = higher) |
| Sync interval | 60s | Web Console/API | Replication frequency per rule |

### Cluster Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `MAXIOFS_CLUSTER_NODE_NAME` | Node name for cluster | `node-east-1` |
| `MAXIOFS_CLUSTER_REGION` | Geographic region | `us-east-1` |

**Complete cluster configuration:** See [CLUSTER.md](CLUSTER.md)

---

## Examples

### Development

**Basic local development:**
```bash
./maxiofs --data-dir ./data --log-level debug
```

**With config file:**
```yaml
# dev-config.yaml
data_dir: ./data
listen: :8080
console_listen: :8081
log_level: debug
```

```bash
./maxiofs --config dev-config.yaml
```

### Production (Behind Reverse Proxy - RECOMMENDED)

```yaml
# /etc/maxiofs/config.yaml
data_dir: /var/lib/maxiofs
listen: 127.0.0.1:8080        # Internal only
console_listen: 127.0.0.1:8081 # Internal only
public_url: https://s3.example.com
console_public_url: https://console.example.com
log_level: info

encryption:
  enabled: true
  master_key: /etc/maxiofs/keys/master_key.key

auth:
  session_timeout: 28800  # 8 hours

audit:
  enabled: true
  retention_days: 180     # 6 months
```

**Reverse proxy configuration:** See [DEPLOYMENT.md](DEPLOYMENT.md)

### Production (Direct TLS)

```yaml
# /etc/maxiofs/config.yaml
data_dir: /var/lib/maxiofs
listen: :8080
console_listen: :8081
log_level: info

tls:
  enabled: true
  cert_file: /etc/letsencrypt/live/example.com/fullchain.pem
  key_file: /etc/letsencrypt/live/example.com/privkey.pem

encryption:
  enabled: true
  master_key: /etc/maxiofs/keys/master_key.key
```

### Docker

**Using environment variables:**
```yaml
# docker-compose.yaml
services:
  maxiofs:
    image: maxiofs:latest
    environment:
      MAXIOFS_DATA_DIR: /data
      MAXIOFS_LISTEN: :8080
      MAXIOFS_CONSOLE_LISTEN: :8081
      MAXIOFS_PUBLIC_URL: https://s3.example.com
      MAXIOFS_LOG_LEVEL: info
    volumes:
      - ./data:/data
      - ./master_key.key:/etc/maxiofs/master_key.key:ro
    ports:
      - "8080:8080"
      - "8081:8081"
```

**See [DOCKER.md](../DOCKER.md) for complete Docker deployment guide**

---

## Dynamic Settings

**Runtime-configurable settings** via Web Console (`/settings`) or API.

### Settings Categories

**Security (11 settings):**
- `security.ratelimit_login_per_minute` - IP-based rate limiting (default: 5)
- `security.max_failed_attempts` - Account lockout threshold (default: 5)
- `security.lockout_duration` - Lock duration in seconds (default: 900)
- `security.session_timeout` - JWT session lifetime (default: 86400)
- Plus 7 more security-related settings

**Audit (4 settings):**
- `audit.retention_days` - Audit log retention (default: 90)
- `audit.enabled` - Enable/disable audit logging
- Plus 2 more audit settings

**Storage (4 settings):**
- `storage.max_multipart_parts` - Max parts per multipart upload (default: 10000)
- `storage.multipart_part_size_min` - Minimum part size in bytes
- Plus 2 more storage settings

**Metrics (2 settings):**
- `metrics.enabled` - Enable Prometheus metrics
- `metrics.retention_hours` - Metrics retention period

**System (2 settings):**
- `system.log_level` - Logging level (debug/info/warn/error)
- `system.max_concurrent_uploads` - Upload concurrency limit

### Settings API

**List all settings:**
```bash
GET /api/v1/settings
```

**Update setting:**
```bash
PUT /api/v1/settings/security.ratelimit_login_per_minute
{
  "value": "15"
}
```

**Web Console:** Navigate to `/settings` → Select category → Modify values → Save

**All changes take effect immediately** without server restart.

---

## Environment Variables Reference

**Core Settings:**
- `MAXIOFS_DATA_DIR` - Data directory path
- `MAXIOFS_LISTEN` - S3 API listen address
- `MAXIOFS_CONSOLE_LISTEN` - Console listen address
- `MAXIOFS_PUBLIC_URL` - Public S3 API URL
- `MAXIOFS_CONSOLE_PUBLIC_URL` - Public Console URL
- `MAXIOFS_LOG_LEVEL` - Log level (debug/info/warn/error)

**TLS:**
- `MAXIOFS_TLS_ENABLED` - Enable TLS (true/false)
- `MAXIOFS_TLS_CERT` - TLS certificate path
- `MAXIOFS_TLS_KEY` - TLS private key path

**Encryption:**
- `MAXIOFS_ENCRYPTION_ENABLED` - Enable SSE (true/false)
- `MAXIOFS_ENCRYPTION_MASTER_KEY` - Master key path

**Cluster:**
- `MAXIOFS_CLUSTER_NODE_NAME` - Node name
- `MAXIOFS_CLUSTER_REGION` - Geographic region

**Complete list:** Use `./maxiofs --help` to see all available flags

---

## Additional Resources

- **[DEPLOYMENT.md](DEPLOYMENT.md)** - Production deployment guide with reverse proxy examples
- **[SECURITY.md](SECURITY.md)** - Security configuration and best practices
- **[CLUSTER.md](CLUSTER.md)** - Complete cluster configuration guide
- **[DOCKER.md](../DOCKER.md)** - Docker deployment with docker-compose
- **[API.md](API.md)** - S3 API compatibility and usage

---

**Version**: 0.6.2-beta
**Last Updated**: December 13, 2025
