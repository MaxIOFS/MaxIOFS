# MaxIOFS Configuration Guide

## Overview

MaxIOFS can be configured through:
1. Command-line flags
2. Configuration file (YAML)
3. Environment variables

Configuration precedence (highest to lowest):
1. Command-line flags
2. Environment variables
3. Configuration file
4. Default values

---

## Command-Line Flags

### Basic Options

```bash
maxiofs [flags]

Flags:
  --config string         Path to configuration file (default: ./config.yaml)
  --data-dir string       Data directory path (default: ./data)
  --log-level string      Log level: debug, info, warn, error (default: info)
  --s3-port int          S3 API port (default: 8080)
  --console-port int     Console API port (default: 8081)
  --version              Show version information
  --help                 Show help message
```

### Examples

```bash
# Basic usage
./maxiofs --data-dir /var/lib/maxiofs --log-level info

# With config file
./maxiofs --config /etc/maxiofs/config.yaml

# Custom ports
./maxiofs --s3-port 9000 --console-port 9001

# Debug mode
./maxiofs --log-level debug
```

---

## Configuration File

### Full Configuration Example

Create `config.yaml`:

```yaml
# Server Configuration
server:
  # S3 API server port
  s3_port: 8080

  # Console API server port
  console_port: 8081

  # Data directory for objects and metadata
  data_dir: ./data

  # Log level: debug, info, warn, error
  log_level: info

  # Enable structured JSON logging
  json_logs: false

  # Server timeouts
  read_timeout: 60s
  write_timeout: 60s
  idle_timeout: 120s

# Security Configuration
security:
  # JWT secret for token signing (CHANGE THIS!)
  jwt_secret: "change-this-to-a-random-32-character-string"

  # JWT token expiration (seconds)
  token_expiration: 3600  # 1 hour

  # Session timeout (seconds)
  session_timeout: 3600

  # Enable HTTPS/TLS
  tls_enabled: false

  # TLS certificate paths (if tls_enabled: true)
  tls_cert: /path/to/cert.pem
  tls_key: /path/to/key.pem

# Rate Limiting Configuration
rate_limit:
  # Enable rate limiting
  enabled: true

  # Maximum login attempts per minute per IP
  login_attempts: 5

  # Account lockout duration after failed attempts (seconds)
  lockout_duration: 900  # 15 minutes

  # Maximum failed attempts before lockout
  max_failed_attempts: 5

# Storage Configuration
storage:
  # Storage backend type: filesystem (more planned: s3, gcs, azure)
  backend: filesystem

  # Filesystem backend path
  path: ./data/objects

  # Enable compression for objects
  compression: true

  # Enable encryption for objects
  encryption: true

  # Encryption algorithm: aes-256-gcm
  encryption_algorithm: aes-256-gcm

# Database Configuration
database:
  # SQLite database path
  path: ./data/maxiofs.db

  # Enable WAL mode for better concurrency
  wal_mode: true

  # Connection pool size
  max_connections: 25

  # Busy timeout (milliseconds)
  busy_timeout: 5000

# Monitoring Configuration
monitoring:
  # Enable Prometheus metrics
  prometheus_enabled: true

  # Prometheus metrics port
  metrics_port: 9090

  # Enable request logging
  request_logging: true

  # Enable audit logging
  audit_logging: true

  # Audit log path
  audit_log_path: ./data/audit.log

# CORS Configuration (for S3 API external clients)
cors:
  # Enable CORS
  enabled: true

  # Allowed origins (use ["*"] for development only!)
  allowed_origins:
    - "https://yourdomain.com"
    - "https://console.yourdomain.com"

  # Allowed methods
  allowed_methods:
    - GET
    - POST
    - PUT
    - DELETE
    - HEAD

  # Allowed headers
  allowed_headers:
    - Authorization
    - Content-Type
    - X-Amz-*

  # Max age for preflight cache (seconds)
  max_age: 3600

# Multi-Tenancy Configuration
multi_tenancy:
  # Enable multi-tenancy
  enabled: true

  # Default quotas for new tenants
  default_max_storage_bytes: 107374182400  # 100 GB
  default_max_buckets: 100
  default_max_access_keys: 50

# Object Lock Configuration
object_lock:
  # Enable Object Lock / WORM
  enabled: true

  # Default retention mode: GOVERNANCE or COMPLIANCE
  default_mode: GOVERNANCE

  # Default retention days (0 = no default)
  default_retention_days: 0

# Advanced Configuration
advanced:
  # Maximum multipart upload parts
  max_multipart_parts: 10000

  # Minimum multipart part size (bytes)
  min_part_size: 5242880  # 5 MB

  # Maximum object size (bytes, 0 = unlimited)
  max_object_size: 0

  # Presigned URL expiration (seconds)
  presigned_url_expiration: 3600

  # Enable batch operations
  batch_operations_enabled: true

  # Maximum objects per batch operation
  max_batch_size: 1000
```

### Minimal Configuration

For quick start with defaults:

```yaml
server:
  data_dir: ./data
  log_level: info

security:
  jwt_secret: "your-secure-random-secret"
```

---

## Environment Variables

All configuration options can be set via environment variables using the `MAXIOFS_` prefix.

### Naming Convention

Configuration path → Environment variable:
- `server.s3_port` → `MAXIOFS_SERVER_S3_PORT`
- `security.jwt_secret` → `MAXIOFS_SECURITY_JWT_SECRET`
- `rate_limit.enabled` → `MAXIOFS_RATE_LIMIT_ENABLED`

### Common Environment Variables

```bash
# Server
export MAXIOFS_SERVER_S3_PORT=8080
export MAXIOFS_SERVER_CONSOLE_PORT=8081
export MAXIOFS_SERVER_DATA_DIR=/var/lib/maxiofs
export MAXIOFS_SERVER_LOG_LEVEL=info

# Security
export MAXIOFS_SECURITY_JWT_SECRET="your-secret-here"
export MAXIOFS_SECURITY_TOKEN_EXPIRATION=3600

# Rate Limiting
export MAXIOFS_RATE_LIMIT_ENABLED=true
export MAXIOFS_RATE_LIMIT_LOGIN_ATTEMPTS=5

# Storage
export MAXIOFS_STORAGE_BACKEND=filesystem
export MAXIOFS_STORAGE_PATH=/var/lib/maxiofs/objects

# Monitoring
export MAXIOFS_MONITORING_PROMETHEUS_ENABLED=true
export MAXIOFS_MONITORING_METRICS_PORT=9090
```

### Docker Environment Variables

```yaml
# docker-compose.yml
services:
  maxiofs:
    image: maxiofs/maxiofs:latest
    environment:
      - MAXIOFS_SERVER_DATA_DIR=/data
      - MAXIOFS_SERVER_LOG_LEVEL=info
      - MAXIOFS_SECURITY_JWT_SECRET=${JWT_SECRET}
      - MAXIOFS_RATE_LIMIT_ENABLED=true
      - MAXIOFS_MONITORING_PROMETHEUS_ENABLED=true
    volumes:
      - ./data:/data
```

---

## Configuration Sections

### Server Configuration

Controls server behavior, ports, and timeouts.

```yaml
server:
  s3_port: 8080              # S3 API port
  console_port: 8081         # Web console port
  data_dir: ./data           # Data directory
  log_level: info            # Log verbosity
  json_logs: false           # JSON log format
  read_timeout: 60s          # HTTP read timeout
  write_timeout: 60s         # HTTP write timeout
  idle_timeout: 120s         # HTTP idle timeout
```

**Recommendations:**
- Production: `log_level: info`, `json_logs: true`
- Development: `log_level: debug`, `json_logs: false`

### Security Configuration

Authentication, authorization, and encryption settings.

```yaml
security:
  jwt_secret: "CHANGE-ME"    # JWT signing secret (32+ chars)
  token_expiration: 3600     # Token lifetime (seconds)
  session_timeout: 3600      # Session timeout
  tls_enabled: false         # Enable HTTPS
  tls_cert: /path/cert.pem   # TLS certificate
  tls_key: /path/key.pem     # TLS private key
```

**Security Best Practices:**
- Generate strong JWT secret: `openssl rand -base64 32`
- Enable TLS in production
- Use short token expiration (1-4 hours)
- Rotate JWT secret periodically

### Rate Limiting

Protect against brute force attacks.

```yaml
rate_limit:
  enabled: true              # Enable rate limiting
  login_attempts: 5          # Max attempts per minute
  lockout_duration: 900      # Lockout time (seconds)
  max_failed_attempts: 5     # Attempts before lockout
```

**Account Lockout Flow:**
1. User fails login 5 times
2. Account locked for 15 minutes
3. Manual unlock by admin (optional)
4. Auto-unlock after duration

### Storage Configuration

Storage backend and encryption settings.

```yaml
storage:
  backend: filesystem        # Backend type
  path: ./data/objects       # Storage path
  compression: true          # Enable gzip compression
  encryption: true           # Enable encryption
  encryption_algorithm: aes-256-gcm
```

**Supported Backends:**
- `filesystem` - Local filesystem (current)
- `s3` - AWS S3 or compatible (planned)
- `gcs` - Google Cloud Storage (planned)
- `azure` - Azure Blob Storage (planned)

### Database Configuration

SQLite database settings.

```yaml
database:
  path: ./data/maxiofs.db    # Database file path
  wal_mode: true             # Write-Ahead Logging
  max_connections: 25        # Connection pool size
  busy_timeout: 5000         # Lock timeout (ms)
```

**Performance Tuning:**
- Enable WAL mode for better concurrency
- Increase `max_connections` for high load
- Adjust `busy_timeout` for long operations

### Monitoring Configuration

Metrics, logging, and observability.

```yaml
monitoring:
  prometheus_enabled: true   # Enable Prometheus metrics
  metrics_port: 9090         # Metrics endpoint port
  request_logging: true      # Log all requests
  audit_logging: true        # Audit log for compliance
  audit_log_path: ./audit.log
```

**Prometheus Metrics Endpoint:**
```
http://localhost:9090/metrics
```

### CORS Configuration

Cross-Origin Resource Sharing for S3 API.

```yaml
cors:
  enabled: true
  allowed_origins:
    - "https://app.yourdomain.com"
  allowed_methods:
    - GET
    - PUT
    - POST
    - DELETE
  allowed_headers:
    - Authorization
    - Content-Type
  max_age: 3600
```

**Important:**
- Use specific origins in production
- Never use `["*"]` in production
- Console API doesn't need CORS (monolithic deployment)

### Multi-Tenancy Configuration

Tenant quotas and defaults.

```yaml
multi_tenancy:
  enabled: true
  default_max_storage_bytes: 107374182400  # 100 GB
  default_max_buckets: 100
  default_max_access_keys: 50
```

**Quota Enforcement:**
- Storage quota checked on upload
- Bucket quota checked on creation
- Access key quota checked on key creation

### Object Lock Configuration

WORM (Write Once Read Many) settings.

```yaml
object_lock:
  enabled: true
  default_mode: GOVERNANCE   # or COMPLIANCE
  default_retention_days: 0  # 0 = no default
```

**Retention Modes:**
- `GOVERNANCE` - Can be removed by privileged users
- `COMPLIANCE` - Cannot be removed until expiry

---

## Configuration Validation

Validate configuration before starting:

```bash
maxiofs --config config.yaml --validate
```

Output:
```
✓ Configuration is valid
  - Server ports: 8080 (S3), 8081 (Console)
  - Data directory: ./data (writable)
  - JWT secret: configured (32 characters)
  - Rate limiting: enabled
  - Storage backend: filesystem
```

---

## Configuration Templates

### Production Template

```yaml
server:
  s3_port: 8080
  console_port: 8081
  data_dir: /var/lib/maxiofs
  log_level: info
  json_logs: true
  read_timeout: 60s
  write_timeout: 300s

security:
  jwt_secret: "${JWT_SECRET}"  # From environment
  token_expiration: 3600
  tls_enabled: false  # Use reverse proxy instead

rate_limit:
  enabled: true
  login_attempts: 5
  lockout_duration: 900

storage:
  backend: filesystem
  path: /var/lib/maxiofs/objects
  compression: true
  encryption: true

monitoring:
  prometheus_enabled: true
  metrics_port: 9090
  audit_logging: true
  audit_log_path: /var/log/maxiofs/audit.log
```

### Development Template

```yaml
server:
  data_dir: ./data
  log_level: debug
  json_logs: false

security:
  jwt_secret: "dev-secret-do-not-use-in-production"
  token_expiration: 86400  # 24 hours for dev

rate_limit:
  enabled: false  # Disable for development

cors:
  enabled: true
  allowed_origins: ["*"]  # OK for development
```

### Docker Template

```yaml
server:
  s3_port: 8080
  console_port: 8081
  data_dir: /data
  log_level: info
  json_logs: true

security:
  jwt_secret: "${JWT_SECRET}"

monitoring:
  prometheus_enabled: true
```

---

## Secrets Management

### Using Environment Variables

```bash
# .env file
JWT_SECRET=$(openssl rand -base64 32)
```

Load in shell:
```bash
export $(cat .env | xargs)
./maxiofs --config config.yaml
```

### Using Docker Secrets

```yaml
# docker-compose.yml
services:
  maxiofs:
    image: maxiofs/maxiofs:latest
    secrets:
      - jwt_secret
    environment:
      - MAXIOFS_SECURITY_JWT_SECRET_FILE=/run/secrets/jwt_secret

secrets:
  jwt_secret:
    file: ./secrets/jwt_secret.txt
```

### Using Kubernetes Secrets

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: maxiofs-secrets
type: Opaque
data:
  jwt-secret: <base64-encoded-secret>
---
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: maxiofs
        env:
        - name: MAXIOFS_SECURITY_JWT_SECRET
          valueFrom:
            secretKeyRef:
              name: maxiofs-secrets
              key: jwt-secret
```

---

## Troubleshooting

### Configuration Not Loading

```bash
# Check file path
ls -la /etc/maxiofs/config.yaml

# Validate YAML syntax
yamllint config.yaml

# Test with explicit path
./maxiofs --config /etc/maxiofs/config.yaml
```

### Environment Variables Not Working

```bash
# Verify variables are set
env | grep MAXIOFS

# Check precedence (env vars override config file)
./maxiofs --config config.yaml  # Env vars will still apply
```

### Permission Errors

```bash
# Check data directory permissions
ls -la /var/lib/maxiofs

# Fix ownership
chown -R maxiofs:maxiofs /var/lib/maxiofs
chmod 750 /var/lib/maxiofs
```

---

## Best Practices

1. **Always change default secrets** in production
2. **Use environment variables** for sensitive data
3. **Enable audit logging** for compliance
4. **Configure rate limiting** to prevent abuse
5. **Use TLS/HTTPS** or reverse proxy in production
6. **Regular backups** of data directory and database
7. **Monitor metrics** via Prometheus
8. **Validate configuration** before deployment
9. **Use JSON logs** for production (easier parsing)
10. **Set appropriate timeouts** for your use case

---

## Configuration Reference

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `server.s3_port` | int | 8080 | S3 API port |
| `server.console_port` | int | 8081 | Console port |
| `server.data_dir` | string | ./data | Data directory |
| `server.log_level` | string | info | Log level |
| `security.jwt_secret` | string | - | JWT secret (required) |
| `security.token_expiration` | int | 3600 | Token lifetime (seconds) |
| `rate_limit.enabled` | bool | true | Enable rate limiting |
| `rate_limit.login_attempts` | int | 5 | Max attempts/minute |
| `storage.backend` | string | filesystem | Storage backend |
| `storage.compression` | bool | true | Enable compression |
| `monitoring.prometheus_enabled` | bool | true | Enable metrics |

For complete reference, see the full configuration example above.
