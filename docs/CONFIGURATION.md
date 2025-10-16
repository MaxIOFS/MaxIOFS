# MaxIOFS Configuration Guide# MaxIOFS Configuration



**Version**: 0.2.2-alpha**Version**: 0.2.0-dev



This guide explains how to configure MaxIOFS using configuration files, environment variables, and command-line flags.## Configuration Methods



## Table of ContentsMaxIOFS can be configured in three ways (in order of precedence):



- [Quick Start](#quick-start)1. **Command-line flags** (highest priority)

- [Configuration Methods](#configuration-methods)2. **Environment variables** (`MAXIOFS_*`)

- [Configuration Parameters](#configuration-parameters)3. **Default values**

- [Common Scenarios](#common-scenarios)

- [Security Best Practices](#security-best-practices)*Note: Configuration file support is planned but not yet implemented in alpha.*



## Quick Start---



### Development Setup## Command-Line Flags



1. Copy the example configuration:```bash

   ```bashmaxiofs [options]

   cp config.example.yaml config.yaml

   ```Options:

  --data-dir string       Data directory (REQUIRED)

2. Edit `config.yaml` with your settings  --listen string         S3 API address (default ":8080")

  --console-listen string Console API address (default ":8081")

3. Run MaxIOFS:  --log-level string      Log level: debug, info, warn, error (default "info")

   ```bash  --tls-cert string       TLS certificate file (optional)

   ./maxiofs --config config.yaml  --tls-key string        TLS private key file (optional)

   ```  --version               Show version information

  --help                  Show help

### Production Setup with TLS```



```yaml### Examples

listen: ":9000"

console_listen: ":9001"**Basic usage:**

data_dir: "/var/lib/maxiofs"```bash

./maxiofs --data-dir ./data

public_api_url: "https://s3.example.com"```

public_console_url: "https://console.example.com"

**Custom ports:**

enable_tls: true```bash

cert_file: "/etc/letsencrypt/live/s3.example.com/fullchain.pem"./maxiofs --data-dir /var/lib/maxiofs --listen :9000 --console-listen :9001

key_file: "/etc/letsencrypt/live/s3.example.com/privkey.pem"```



auth:**Debug logging:**

  access_key: "your-admin-key"```bash

  secret_key: "your-secure-secret"./maxiofs --data-dir ./data --log-level debug

```

storage:

  enable_compression: true**With TLS:**

  compression_type: "zstd"```bash

  enable_encryption: true./maxiofs --data-dir ./data --tls-cert cert.pem --tls-key key.pem

  encryption_key: "your-32-character-encryption-key"```

```

---

## Configuration Methods

## Environment Variables

MaxIOFS supports three configuration methods with the following priority (highest to lowest):

All configuration options can be set via environment variables using the `MAXIOFS_` prefix.

1. **Command-line flags** (highest priority)

2. **Environment variables**### Variable Names

3. **Configuration file**

4. **Default values** (lowest priority)Command-line flag → Environment variable:

- `--data-dir` → `MAXIOFS_DATA_DIR`

### 1. Configuration File- `--listen` → `MAXIOFS_LISTEN`

- `--console-listen` → `MAXIOFS_CONSOLE_LISTEN`

Create a YAML file (e.g., `config.yaml`) and pass it to MaxIOFS:- `--log-level` → `MAXIOFS_LOG_LEVEL`

- `--tls-cert` → `MAXIOFS_TLS_CERT`

```bash- `--tls-key` → `MAXIOFS_TLS_KEY`

./maxiofs --config config.yaml

```### Example



See [`config.example.yaml`](../config.example.yaml) for a complete example.```bash

export MAXIOFS_DATA_DIR=/var/lib/maxiofs

### 2. Environment Variablesexport MAXIOFS_LISTEN=:8080

export MAXIOFS_CONSOLE_LISTEN=:8081

All configuration options can be set via environment variables with the `MAXIOFS_` prefix:export MAXIOFS_LOG_LEVEL=info



```bash./maxiofs

export MAXIOFS_LISTEN=":9000"```

export MAXIOFS_DATA_DIR="/var/lib/maxiofs"

export MAXIOFS_PUBLIC_API_URL="https://s3.example.com"### Docker

export MAXIOFS_ENABLE_TLS=true

export MAXIOFS_CERT_FILE="/etc/ssl/certs/maxiofs.crt"```yaml

export MAXIOFS_KEY_FILE="/etc/ssl/private/maxiofs.key"# docker-compose.yml

export MAXIOFS_AUTH_ACCESS_KEY="myadmin"services:

export MAXIOFS_AUTH_SECRET_KEY="mysecret"  maxiofs:

export MAXIOFS_STORAGE_ENABLE_COMPRESSION=true    image: maxiofs/maxiofs:1.1.0-alpha

    environment:

./maxiofs --data-dir /var/lib/maxiofs      - MAXIOFS_DATA_DIR=/data

```      - MAXIOFS_LOG_LEVEL=info

    ports:

### 3. Command-line Flags      - "8080:8080"

      - "8081:8081"

```bash    volumes:

./maxiofs \      - ./data:/data

  --data-dir /var/lib/maxiofs \```

  --listen :9000 \

  --console-listen :9001 \---

  --log-level info \

  --tls-cert /etc/ssl/certs/maxiofs.crt \## Configuration Options

  --tls-key /etc/ssl/private/maxiofs.key

```### Data Directory



Available flags:**Flag**: `--data-dir`

- `--config, -c` - Configuration file path**Environment**: `MAXIOFS_DATA_DIR`

- `--data-dir, -d` - Data directory (required)**Required**: Yes

- `--listen, -l` - API server address (default: `:8080`)**Default**: None

- `--console-listen` - Web console address (default: `:8081`)

- `--log-level` - Log level: debug, info, warn, error (default: `info`)The data directory stores all MaxIOFS data:

- `--tls-cert` - TLS certificate file- `{data-dir}/maxiofs.db` - SQLite metadata database

- `--tls-key` - TLS private key file- `{data-dir}/objects/` - Object storage



## Configuration Parameters**Example structure:**

```

### Server Settings/var/lib/maxiofs/

├── maxiofs.db           # Metadata

| Parameter | Type | Default | Description |└── objects/             # Objects

|-----------|------|---------|-------------|    ├── global/          # Global admin buckets

| `listen` | string | `:8080` | API server listen address |    └── tenant-123/      # Tenant buckets

| `console_listen` | string | `:8081` | Web console listen address |```

| `data_dir` | string | `./data` | Data directory path (required) |

| `log_level` | string | `info` | Log level (debug, info, warn, error) |**Important:**

- Directory must be writable

### Public URLs- Must persist across restarts

- Backup regularly

| Parameter | Type | Default | Description |

|-----------|------|---------|-------------|### Ports

| `public_api_url` | string | `http://localhost:8080` | Public S3 API URL |

| `public_console_url` | string | `http://localhost:8081` | Public web console URL |**S3 API Port**

**Flag**: `--listen`

These URLs are used for:**Environment**: `MAXIOFS_LISTEN`

- Generating presigned URLs**Default**: `:8080`

- CORS configuration

- External access documentationPort for S3-compatible API.

- Redirects in the web console

**Console Port**

### TLS/SSL Settings**Flag**: `--console-listen`

**Environment**: `MAXIOFS_CONSOLE_LISTEN`

| Parameter | Type | Default | Description |**Default**: `:8081`

|-----------|------|---------|-------------|

| `enable_tls` | bool | `false` | Enable TLS/SSL |Port for web console and console API.

| `cert_file` | string | - | TLS certificate file path |

| `key_file` | string | - | TLS private key file path |### Log Level



### Storage Settings**Flag**: `--log-level`

**Environment**: `MAXIOFS_LOG_LEVEL`

| Parameter | Type | Default | Description |**Default**: `info`

|-----------|------|---------|-------------|**Options**: `debug`, `info`, `warn`, `error`

| `storage.backend` | string | `filesystem` | Storage backend (filesystem) |

| `storage.root` | string | `{data_dir}/objects` | Storage root directory |- `debug` - Verbose logging (development)

| `storage.enable_compression` | bool | `false` | Enable compression |- `info` - Normal logging (production)

| `storage.compression_level` | int | `6` | Compression level (1-9) |- `warn` - Warnings and errors only

| `storage.compression_type` | string | `gzip` | Compression type (gzip, lz4, zstd) |- `error` - Errors only

| `storage.enable_encryption` | bool | `false` | Enable encryption at rest |

| `storage.encryption_key` | string | - | AES-256 encryption key (32 chars) |### TLS/HTTPS

| `storage.enable_object_lock` | bool | `true` | Enable S3 Object Lock |

**Certificate**

### Authentication Settings**Flag**: `--tls-cert`

**Environment**: `MAXIOFS_TLS_CERT`

| Parameter | Type | Default | Description |**Optional**: Yes

|-----------|------|---------|-------------|

| `auth.enable_auth` | bool | `true` | Enable authentication |Path to TLS certificate file (PEM format).

| `auth.jwt_secret` | string | auto-generated | JWT signing secret |

| `auth.access_key` | string | `maxioadmin` | Default admin access key |**Private Key**

| `auth.secret_key` | string | `maxioadmin` | Default admin secret key |**Flag**: `--tls-key`

| `auth.users_file` | string | - | External users file (optional) |**Environment**: `MAXIOFS_TLS_KEY`

**Optional**: Yes

### Metrics Settings

Path to TLS private key file (PEM format).

| Parameter | Type | Default | Description |

|-----------|------|---------|-------------|**Example:**

| `metrics.enable` | bool | `true` | Enable metrics collection |```bash

| `metrics.path` | string | `/metrics` | Metrics endpoint path |./maxiofs --data-dir ./data \

| `metrics.interval` | int | `60` | Collection interval (seconds) |  --tls-cert /etc/maxiofs/cert.pem \

  --tls-key /etc/maxiofs/key.pem

## Common Scenarios```



### Local Development**Notes:**

- Both cert and key must be provided together

```yaml- Alternatively, use a reverse proxy (nginx/traefik) for TLS

listen: ":8080"- Self-signed certificates work for testing

console_listen: ":8081"

data_dir: "./data"---

log_level: "debug"

## Default Credentials

auth:

  access_key: "maxioadmin"**Web Console:**

  secret_key: "maxioadmin"- Username: `admin`

```- Password: `admin`



Run:**S3 API:**

```bash- Access Key: `maxioadmin`

./maxiofs --config config.yaml- Secret Key: `maxioadmin`

```

**⚠️ IMPORTANT**: Change default credentials after first login!

### Production with Reverse Proxy (nginx/Caddy)

---

```yaml

listen: "127.0.0.1:9000"## Storage Paths

console_listen: "127.0.0.1:9001"

data_dir: "/var/lib/maxiofs"### Object Storage

log_level: "info"

Objects are stored in the filesystem:

public_api_url: "https://s3.example.com"

public_console_url: "https://console.example.com"```

{data-dir}/objects/{tenant-id}/{bucket-name}/{object-key}

# TLS handled by reverse proxy```

enable_tls: false

**Examples:**

storage:- Global bucket: `./data/objects/global/my-bucket/file.txt`

  enable_compression: true- Tenant bucket: `./data/objects/tenant-123/backup/file.bin`

  compression_type: "zstd"

  enable_encryption: true### Metadata Database

  encryption_key: "your-secure-32-character-key-here"

SQLite database at:

auth:```

  access_key: "production-admin"{data-dir}/maxiofs.db

  secret_key: "your-very-secure-secret-key"```

```

Contains:

nginx configuration:- Users and credentials (bcrypt hashed)

```nginx- Tenants and quotas

server {- Buckets metadata

    listen 443 ssl http2;- Access keys

    server_name s3.example.com;- Object metadata

    

    ssl_certificate /etc/letsencrypt/live/s3.example.com/fullchain.pem;---

    ssl_certificate_key /etc/letsencrypt/live/s3.example.com/privkey.pem;

    ## Production Recommendations

    location / {

        proxy_pass http://127.0.0.1:9000;### Basic Setup

        proxy_set_header Host $host;

        proxy_set_header X-Real-IP $remote_addr;```bash

        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;# 1. Create data directory

        proxy_set_header X-Forwarded-Proto $scheme;mkdir -p /var/lib/maxiofs

    }chown maxiofs:maxiofs /var/lib/maxiofs

}chmod 750 /var/lib/maxiofs

```

# 2. Run MaxIOFS

### Production with Direct TLS./maxiofs --data-dir /var/lib/maxiofs --log-level info

```

```yaml

listen: ":9000"### With Systemd

console_listen: ":9001"

data_dir: "/var/lib/maxiofs"```ini

log_level: "info"# /etc/systemd/system/maxiofs.service

[Unit]

public_api_url: "https://s3.example.com:9000"Description=MaxIOFS Object Storage

public_console_url: "https://console.example.com:9001"After=network.target



enable_tls: true[Service]

cert_file: "/etc/letsencrypt/live/s3.example.com/fullchain.pem"Type=simple

key_file: "/etc/letsencrypt/live/s3.example.com/privkey.pem"User=maxiofs

Group=maxiofs

storage:ExecStart=/usr/local/bin/maxiofs --data-dir /var/lib/maxiofs --log-level info

  enable_compression: trueRestart=on-failure

  compression_type: "zstd"RestartSec=5

  enable_encryption: true

  encryption_key: "your-secure-32-character-key-here"[Install]

WantedBy=multi-user.target

auth:```

  access_key: "production-admin"

  secret_key: "your-very-secure-secret-key"```bash

```# Start service

systemctl daemon-reload

### Docker Deploymentsystemctl enable maxiofs

systemctl start maxiofs

`docker-compose.yml`:```

```yaml

version: '3.8'### With Docker



services:```bash

  maxiofs:docker run -d \

    image: maxiofs:latest  --name maxiofs \

    container_name: maxiofs  -p 8080:8080 \

    ports:  -p 8081:8081 \

      - "8080:8080"  # API  -v /var/lib/maxiofs:/data \

      - "8081:8081"  # Console  -e MAXIOFS_DATA_DIR=/data \

    volumes:  -e MAXIOFS_LOG_LEVEL=info \

      - ./data:/data  maxiofs/maxiofs:1.1.0-alpha

      - ./config.yaml:/config.yaml:ro```

      - ./certs:/certs:ro

    command: --config /config.yaml### With Reverse Proxy (nginx)

    environment:

      - MAXIOFS_LOG_LEVEL=info```nginx

    restart: unless-stopped# nginx.conf

    healthcheck:server {

      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]    listen 443 ssl http2;

      interval: 30s    server_name maxiofs.example.com;

      timeout: 10s

      retries: 3    ssl_certificate /etc/ssl/cert.pem;

```    ssl_certificate_key /etc/ssl/key.pem;



`config.yaml`:    # Web Console

```yaml    location / {

listen: ":8080"        proxy_pass http://localhost:8081;

console_listen: ":8081"        proxy_set_header Host $host;

data_dir: "/data"        proxy_set_header X-Real-IP $remote_addr;

log_level: "info"    }



public_api_url: "https://s3.example.com"    # S3 API

public_console_url: "https://console.example.com"    location /s3/ {

        rewrite ^/s3/(.*) /$1 break;

enable_tls: true        proxy_pass http://localhost:8080;

cert_file: "/certs/fullchain.pem"        proxy_set_header Host $host;

key_file: "/certs/privkey.pem"    }

}

storage:```

  enable_compression: true

  compression_type: "zstd"---



auth:## Security Best Practices

  access_key: "myadmin"

  secret_key: "mysecret"1. **Change default credentials** immediately after first login

```2. **Use HTTPS** (TLS or reverse proxy)

3. **Restrict network access** (firewall rules)

### High-Security Configuration4. **Backup data directory** regularly

5. **Use strong passwords** for all users

```yaml6. **Monitor logs** for suspicious activity

listen: "127.0.0.1:9000"7. **Keep MaxIOFS updated** to latest stable version

console_listen: "127.0.0.1:9001"

data_dir: "/var/lib/maxiofs"---

log_level: "warn"

## Troubleshooting

public_api_url: "https://s3.example.com"

public_console_url: "https://console.example.com"### Permission Denied



enable_tls: true```bash

cert_file: "/etc/ssl/certs/maxiofs.crt"# Check data directory permissions

key_file: "/etc/ssl/private/maxiofs.key"ls -la /var/lib/maxiofs



storage:# Fix ownership

  enable_compression: truechown -R maxiofs:maxiofs /var/lib/maxiofs

  compression_type: "zstd"chmod 750 /var/lib/maxiofs

  compression_level: 9```

  enable_encryption: true

  encryption_key: "use-a-strong-32-character-key!"### Port Already in Use

  enable_object_lock: true

```bash

auth:# Check what's using the port

  enable_auth: truenetstat -tuln | grep 8080

  jwt_secret: "generate-strong-random-secret-here"

  access_key: "admin-$(openssl rand -hex 8)"# Use different port

  secret_key: "$(openssl rand -base64 32)"./maxiofs --data-dir ./data --listen :9000

```

metrics:

  enable: true### Cannot Connect

  path: "/internal/metrics"

  interval: 30```bash

```# Check if MaxIOFS is running

ps aux | grep maxiofs

## Security Best Practices

# Check logs

### 1. Change Default Credentials./maxiofs --data-dir ./data --log-level debug

```

**Never use default credentials in production!**

### Database Locked

```bash

# Generate secure credentials```bash

export ADMIN_ACCESS_KEY="admin-$(openssl rand -hex 16)"# Check for stale processes

export ADMIN_SECRET_KEY="$(openssl rand -base64 32)"ps aux | grep maxiofs



# Use in config# Kill stale processes

auth:killall maxiofs

  access_key: "${ADMIN_ACCESS_KEY}"

  secret_key: "${ADMIN_SECRET_KEY}"# Restart MaxIOFS

```./maxiofs --data-dir ./data

```

### 2. Use TLS/SSL

---

Always use TLS in production:

## What's Not Configurable Yet (Alpha)

```yaml

enable_tls: trueThese features are planned but not yet configurable:

cert_file: "/path/to/fullchain.pem"

key_file: "/path/to/privkey.pem"- ❌ JWT token expiration

```- ❌ Rate limiting thresholds

- ❌ Tenant default quotas

Get free certificates from [Let's Encrypt](https://letsencrypt.org/):- ❌ Compression settings

```bash- ❌ Encryption at rest

certbot certonly --standalone -d s3.example.com- ❌ Audit logging

```- ❌ Prometheus metrics port

- ❌ CORS settings

### 3. Enable Encryption at Rest- ❌ Session timeouts



```yamlSee [TODO.md](../TODO.md) for roadmap.

storage:

  enable_encryption: true---

  encryption_key: "your-32-character-encryption-key"

```**Note**: This is an alpha release. Configuration options may change without notice.


**IMPORTANT:** 
- Use a strong, random 32-character key
- Store the key securely (e.g., HashiCorp Vault, AWS Secrets Manager)
- Back up the key - if lost, data cannot be recovered!

Generate a secure key:
```bash
openssl rand -base64 32 | cut -c1-32
```

### 4. Secure JWT Secret

```yaml
auth:
  jwt_secret: "generate-strong-random-secret"
```

Generate:
```bash
openssl rand -base64 32
```

### 5. Restrict Network Access

Bind to localhost and use reverse proxy:
```yaml
listen: "127.0.0.1:9000"
console_listen: "127.0.0.1:9001"
```

### 6. Regular Backups

Back up:
- Configuration files
- Encryption keys
- JWT secrets
- Data directory (`data_dir`)

### 7. Monitor Logs

```yaml
log_level: "info"  # or "warn" in production
```

Monitor for suspicious activity:
```bash
tail -f /var/log/maxiofs/maxiofs.log | grep -i "error\|unauthorized\|failed"
```

### 8. Use Object Lock for Compliance

```yaml
storage:
  enable_object_lock: true
```

Prevents accidental or malicious deletion of critical data.

## AWS CLI Configuration

Configure AWS CLI to use MaxIOFS:

```bash
aws configure --profile maxiofs
```

Enter:
- **AWS Access Key ID:** Your `access_key`
- **AWS Secret Access Key:** Your `secret_key`
- **Default region name:** `us-east-1`
- **Default output format:** `json`

Usage:
```bash
# List buckets
aws s3 --profile maxiofs --endpoint-url http://localhost:8080 ls

# Create bucket
aws s3 --profile maxiofs --endpoint-url http://localhost:8080 mb s3://mybucket

# Upload file
aws s3 --profile maxiofs --endpoint-url http://localhost:8080 cp file.txt s3://mybucket/

# Download file
aws s3 --profile maxiofs --endpoint-url http://localhost:8080 cp s3://mybucket/file.txt ./
```

For production with TLS:
```bash
aws s3 --profile maxiofs --endpoint-url https://s3.example.com ls
```

## Veeam Backup Configuration

MaxIOFS is compatible with Veeam Backup & Replication:

1. In Veeam, add a new **Object Storage Repository**
2. Select **S3 Compatible**
3. Enter settings:
   - **Service point:** Your `public_api_url` (e.g., `https://s3.example.com`)
   - **Region:** `us-east-1`
   - **Access key:** Your `access_key`
   - **Secret key:** Your `secret_key`
4. Veeam will detect MaxIOFS as S3-compatible storage

## Troubleshooting

### Check Configuration

View loaded configuration:
```bash
./maxiofs --config config.yaml --log-level debug
```

### Test Connectivity

```bash
# Health check
curl http://localhost:8080/health

# List buckets (S3 API)
aws s3 --endpoint-url http://localhost:8080 ls

# Access web console
curl http://localhost:8081
```

### Common Issues

**Issue:** `TLS enabled but cert-file or key-file not specified`  
**Solution:** Provide both `cert_file` and `key_file` when `enable_tls: true`

**Issue:** `Failed to create data directory`  
**Solution:** Ensure the process has write permissions to `data_dir`

**Issue:** `Connection refused`  
**Solution:** Check firewall rules and ensure the listen address is accessible

## Additional Resources

- [Main README](../README.md)
- [API Documentation](./API.md)
- [Architecture Overview](./ARCHITECTURE.md)
- [Deployment Guide](./DEPLOYMENT.md)
- [Security Guide](./SECURITY.md)

## Support

For issues and questions:
- GitHub Issues: https://github.com/aluisco/MaxIOFS/issues
- Documentation: https://github.com/aluisco/MaxIOFS/tree/main/docs
