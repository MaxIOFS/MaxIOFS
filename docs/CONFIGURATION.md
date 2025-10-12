# MaxIOFS Configuration

**Version**: 0.2.0-dev

## Configuration Methods

MaxIOFS can be configured in three ways (in order of precedence):

1. **Command-line flags** (highest priority)
2. **Environment variables** (`MAXIOFS_*`)
3. **Default values**

*Note: Configuration file support is planned but not yet implemented in alpha.*

---

## Command-Line Flags

```bash
maxiofs [options]

Options:
  --data-dir string       Data directory (REQUIRED)
  --listen string         S3 API address (default ":8080")
  --console-listen string Console API address (default ":8081")
  --log-level string      Log level: debug, info, warn, error (default "info")
  --tls-cert string       TLS certificate file (optional)
  --tls-key string        TLS private key file (optional)
  --version               Show version information
  --help                  Show help
```

### Examples

**Basic usage:**
```bash
./maxiofs --data-dir ./data
```

**Custom ports:**
```bash
./maxiofs --data-dir /var/lib/maxiofs --listen :9000 --console-listen :9001
```

**Debug logging:**
```bash
./maxiofs --data-dir ./data --log-level debug
```

**With TLS:**
```bash
./maxiofs --data-dir ./data --tls-cert cert.pem --tls-key key.pem
```

---

## Environment Variables

All configuration options can be set via environment variables using the `MAXIOFS_` prefix.

### Variable Names

Command-line flag → Environment variable:
- `--data-dir` → `MAXIOFS_DATA_DIR`
- `--listen` → `MAXIOFS_LISTEN`
- `--console-listen` → `MAXIOFS_CONSOLE_LISTEN`
- `--log-level` → `MAXIOFS_LOG_LEVEL`
- `--tls-cert` → `MAXIOFS_TLS_CERT`
- `--tls-key` → `MAXIOFS_TLS_KEY`

### Example

```bash
export MAXIOFS_DATA_DIR=/var/lib/maxiofs
export MAXIOFS_LISTEN=:8080
export MAXIOFS_CONSOLE_LISTEN=:8081
export MAXIOFS_LOG_LEVEL=info

./maxiofs
```

### Docker

```yaml
# docker-compose.yml
services:
  maxiofs:
    image: maxiofs/maxiofs:1.1.0-alpha
    environment:
      - MAXIOFS_DATA_DIR=/data
      - MAXIOFS_LOG_LEVEL=info
    ports:
      - "8080:8080"
      - "8081:8081"
    volumes:
      - ./data:/data
```

---

## Configuration Options

### Data Directory

**Flag**: `--data-dir`
**Environment**: `MAXIOFS_DATA_DIR`
**Required**: Yes
**Default**: None

The data directory stores all MaxIOFS data:
- `{data-dir}/maxiofs.db` - SQLite metadata database
- `{data-dir}/objects/` - Object storage

**Example structure:**
```
/var/lib/maxiofs/
├── maxiofs.db           # Metadata
└── objects/             # Objects
    ├── global/          # Global admin buckets
    └── tenant-123/      # Tenant buckets
```

**Important:**
- Directory must be writable
- Must persist across restarts
- Backup regularly

### Ports

**S3 API Port**
**Flag**: `--listen`
**Environment**: `MAXIOFS_LISTEN`
**Default**: `:8080`

Port for S3-compatible API.

**Console Port**
**Flag**: `--console-listen`
**Environment**: `MAXIOFS_CONSOLE_LISTEN`
**Default**: `:8081`

Port for web console and console API.

### Log Level

**Flag**: `--log-level`
**Environment**: `MAXIOFS_LOG_LEVEL`
**Default**: `info`
**Options**: `debug`, `info`, `warn`, `error`

- `debug` - Verbose logging (development)
- `info` - Normal logging (production)
- `warn` - Warnings and errors only
- `error` - Errors only

### TLS/HTTPS

**Certificate**
**Flag**: `--tls-cert`
**Environment**: `MAXIOFS_TLS_CERT`
**Optional**: Yes

Path to TLS certificate file (PEM format).

**Private Key**
**Flag**: `--tls-key`
**Environment**: `MAXIOFS_TLS_KEY`
**Optional**: Yes

Path to TLS private key file (PEM format).

**Example:**
```bash
./maxiofs --data-dir ./data \
  --tls-cert /etc/maxiofs/cert.pem \
  --tls-key /etc/maxiofs/key.pem
```

**Notes:**
- Both cert and key must be provided together
- Alternatively, use a reverse proxy (nginx/traefik) for TLS
- Self-signed certificates work for testing

---

## Default Credentials

**Web Console:**
- Username: `admin`
- Password: `admin`

**S3 API:**
- Access Key: `maxioadmin`
- Secret Key: `maxioadmin`

**⚠️ IMPORTANT**: Change default credentials after first login!

---

## Storage Paths

### Object Storage

Objects are stored in the filesystem:

```
{data-dir}/objects/{tenant-id}/{bucket-name}/{object-key}
```

**Examples:**
- Global bucket: `./data/objects/global/my-bucket/file.txt`
- Tenant bucket: `./data/objects/tenant-123/backup/file.bin`

### Metadata Database

SQLite database at:
```
{data-dir}/maxiofs.db
```

Contains:
- Users and credentials (bcrypt hashed)
- Tenants and quotas
- Buckets metadata
- Access keys
- Object metadata

---

## Production Recommendations

### Basic Setup

```bash
# 1. Create data directory
mkdir -p /var/lib/maxiofs
chown maxiofs:maxiofs /var/lib/maxiofs
chmod 750 /var/lib/maxiofs

# 2. Run MaxIOFS
./maxiofs --data-dir /var/lib/maxiofs --log-level info
```

### With Systemd

```ini
# /etc/systemd/system/maxiofs.service
[Unit]
Description=MaxIOFS Object Storage
After=network.target

[Service]
Type=simple
User=maxiofs
Group=maxiofs
ExecStart=/usr/local/bin/maxiofs --data-dir /var/lib/maxiofs --log-level info
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
# Start service
systemctl daemon-reload
systemctl enable maxiofs
systemctl start maxiofs
```

### With Docker

```bash
docker run -d \
  --name maxiofs \
  -p 8080:8080 \
  -p 8081:8081 \
  -v /var/lib/maxiofs:/data \
  -e MAXIOFS_DATA_DIR=/data \
  -e MAXIOFS_LOG_LEVEL=info \
  maxiofs/maxiofs:1.1.0-alpha
```

### With Reverse Proxy (nginx)

```nginx
# nginx.conf
server {
    listen 443 ssl http2;
    server_name maxiofs.example.com;

    ssl_certificate /etc/ssl/cert.pem;
    ssl_certificate_key /etc/ssl/key.pem;

    # Web Console
    location / {
        proxy_pass http://localhost:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    # S3 API
    location /s3/ {
        rewrite ^/s3/(.*) /$1 break;
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
    }
}
```

---

## Security Best Practices

1. **Change default credentials** immediately after first login
2. **Use HTTPS** (TLS or reverse proxy)
3. **Restrict network access** (firewall rules)
4. **Backup data directory** regularly
5. **Use strong passwords** for all users
6. **Monitor logs** for suspicious activity
7. **Keep MaxIOFS updated** to latest stable version

---

## Troubleshooting

### Permission Denied

```bash
# Check data directory permissions
ls -la /var/lib/maxiofs

# Fix ownership
chown -R maxiofs:maxiofs /var/lib/maxiofs
chmod 750 /var/lib/maxiofs
```

### Port Already in Use

```bash
# Check what's using the port
netstat -tuln | grep 8080

# Use different port
./maxiofs --data-dir ./data --listen :9000
```

### Cannot Connect

```bash
# Check if MaxIOFS is running
ps aux | grep maxiofs

# Check logs
./maxiofs --data-dir ./data --log-level debug
```

### Database Locked

```bash
# Check for stale processes
ps aux | grep maxiofs

# Kill stale processes
killall maxiofs

# Restart MaxIOFS
./maxiofs --data-dir ./data
```

---

## What's Not Configurable Yet (Alpha)

These features are planned but not yet configurable:

- ❌ JWT token expiration
- ❌ Rate limiting thresholds
- ❌ Tenant default quotas
- ❌ Compression settings
- ❌ Encryption at rest
- ❌ Audit logging
- ❌ Prometheus metrics port
- ❌ CORS settings
- ❌ Session timeouts

See [TODO.md](../TODO.md) for roadmap.

---

**Note**: This is an alpha release. Configuration options may change without notice.
