# MaxIOFS Configuration Guide# MaxIOFS Configuration Guide# MaxIOFS Configuration Guide# MaxIOFS Configuration



Complete guide for configuring MaxIOFS object storage system.



---Complete guide for configuring MaxIOFS object storage system.



## Table of Contents



- [Configuration Methods](#configuration-methods)## Table of Contents**Version**: 0.2.2-alpha

- [Required Configuration](#required-configuration)

- [Server Configuration](#server-configuration)

- [Authentication](#authentication)

- [Storage Settings](#storage-settings)- [Configuration Methods](#configuration-methods)

- [TLS/HTTPS](#tlshttps)

- [Metrics](#metrics)- [Quick Start](#quick-start)

- [Common Scenarios](#common-scenarios)

- [Security Best Practices](#security-best-practices)- [Configuration Parameters](#configuration-parameters)This guide explains how to configure MaxIOFS using configuration files, environment variables, and command-line flags.## Configuration Methods



---- [Common Scenarios](#common-scenarios)



## Configuration Methods- [Security Best Practices](#security-best-practices)



MaxIOFS supports three configuration methods in order of priority:- [Troubleshooting](#troubleshooting)



### 1. Command-Line Flags (Highest Priority)## Table of ContentsMaxIOFS can be configured in three ways (in order of precedence):



```bash---

./maxiofs --data-dir /var/lib/maxiofs --listen :8080 --log-level debug

```



### 2. Configuration File## Configuration Methods



```bash- [Quick Start](#quick-start)1. **Command-line flags** (highest priority)

./maxiofs --config /etc/maxiofs/config.yaml

```MaxIOFS supports three configuration methods (in order of priority):



### 3. Environment Variables (Lowest Priority)- [Configuration Methods](#configuration-methods)2. **Environment variables** (`MAXIOFS_*`)



```bash### 1. Command-Line Flags (Highest Priority)

export MAXIOFS_DATA_DIR=/var/lib/maxiofs

export MAXIOFS_LISTEN=:8080- [Configuration Parameters](#configuration-parameters)3. **Default values**

./maxiofs

``````bash



**Priority order**: Flags > Environment Variables > Config File > Defaults./maxiofs --data-dir /var/lib/maxiofs --listen :8080 --log-level debug- [Common Scenarios](#common-scenarios)



---```



## Required Configuration- [Security Best Practices](#security-best-practices)*Note: Configuration file support is planned but not yet implemented in alpha.*



### Data Directory### 2. Configuration File



The `data_dir` parameter is **required** and must be explicitly configured via:

- Command-line flag: `--data-dir <path>`

- Config file: `data_dir: <path>````bash

- Environment variable: `MAXIOFS_DATA_DIR=<path>`

./maxiofs --config /etc/maxiofs/config.yaml## Quick Start---

**Example:**

```bash```

# Using flag

./maxiofs --data-dir /var/lib/maxiofs



# Using config file### 3. Environment Variables (Lowest Priority)

./maxiofs --config config.yaml  # with data_dir: /var/lib/maxiofs inside

### Development Setup## Command-Line Flags

# Using environment variable

export MAXIOFS_DATA_DIR=/var/lib/maxiofs```bash

./maxiofs

```export MAXIOFS_DATA_DIR=/var/lib/maxiofs



---export MAXIOFS_LISTEN=:8080



## Server Configurationexport MAXIOFS_LOG_LEVEL=debug1. Copy the example configuration:```bash



### Basic Server Settings./maxiofs



| Parameter | Type | Default | Description |```   ```bashmaxiofs [options]

|-----------|------|---------|-------------|

| `data_dir` | string | **required** | Base directory for all data |

| `listen` | string | `:8080` | S3 API server address |

| `console_listen` | string | `:8081` | Web console address |**Priority**: Flags > Environment Variables > Config File > Defaults   cp config.example.yaml config.yaml

| `log_level` | string | `info` | Log level (debug, info, warn, error) |



**Example config.yaml:**

```yaml---   ```Options:

data_dir: "/var/lib/maxiofs"

listen: ":8080"

console_listen: ":8081"

log_level: "info"## Quick Start  --data-dir string       Data directory (REQUIRED)

```



**Command-line flags:**

```bash### Minimal Configuration2. Edit `config.yaml` with your settings  --listen string         S3 API address (default ":8080")

./maxiofs \

  --data-dir /var/lib/maxiofs \

  --listen :8080 \

  --console-listen :8081 \Create `config.yaml`:  --console-listen string Console API address (default ":8081")

  --log-level info

```



**Environment variables:**```yaml3. Run MaxIOFS:  --log-level string      Log level: debug, info, warn, error (default "info")

```bash

export MAXIOFS_DATA_DIR=/var/lib/maxiofsdata_dir: "./data"

export MAXIOFS_LISTEN=:8080

export MAXIOFS_CONSOLE_LISTEN=:8081```   ```bash  --tls-cert string       TLS certificate file (optional)

export MAXIOFS_LOG_LEVEL=info

```



### Public URLsRun:   ./maxiofs --config config.yaml  --tls-key string        TLS private key file (optional)



| Parameter | Type | Default | Description |

|-----------|------|---------|-------------|

| `public_api_url` | string | auto-detected | Public URL for S3 API |```bash   ```  --version               Show version information

| `public_console_url` | string | auto-detected | Public URL for web console |

./maxiofs --config config.yaml

These are used for:

- Presigned URL generation```  --help                  Show help

- CORS redirect URLs

- Link generation in the console



**Example:**### Recommended Configuration### Production Setup with TLS```

```yaml

public_api_url: "https://s3.example.com"

public_console_url: "https://console.example.com"

``````yaml



---# Required



## Authenticationdata_dir: "/var/lib/maxiofs"```yaml### Examples



### Authentication Settings



| Parameter | Type | Default | Description |# Server configurationlisten: ":9000"

|-----------|------|---------|-------------|

| `auth.enable_auth` | bool | `true` | Enable authentication |listen: ":8080"

| `auth.jwt_secret` | string | auto-generated | JWT signing secret |

console_listen: ":8081"console_listen: ":9001"**Basic usage:**

**Important Security Notes:**

log_level: "info"

1. **Default Admin User:**

   - Username: `admin`data_dir: "/var/lib/maxiofs"```bash

   - Password: `admin`

   - **⚠️ Change password after first login!**# Authentication



2. **No Default Access Keys:**auth:./maxiofs --data-dir ./data

   - For security reasons, no default S3 access keys are created

   - Access keys must be created manually via web console  enable_auth: true

   - Each installation generates unique keys

  jwt_secret: "change-this-to-a-random-secret"public_api_url: "https://s3.example.com"```

**Creating Access Keys:**

1. Login to web console: `http://localhost:8081` (admin/admin)

2. Navigate to Users section

3. Click "Create Access Key" for your user# Metricspublic_console_url: "https://console.example.com"

4. Copy and securely store the generated credentials

metrics:

**Example config.yaml:**

```yaml  enable: true**Custom ports:**

auth:

  enable_auth: true  interval: 60

  jwt_secret: "your-secure-random-secret-min-32-characters"

``````enable_tls: true```bash



**Generate secure JWT secret:**

```bash

# Linux/macOS---cert_file: "/etc/letsencrypt/live/s3.example.com/fullchain.pem"./maxiofs --data-dir /var/lib/maxiofs --listen :9000 --console-listen :9001

openssl rand -base64 32



# PowerShell

[Convert]::ToBase64String((1..32 | ForEach-Object { Get-Random -Maximum 256 }))## Configuration Parameterskey_file: "/etc/letsencrypt/live/s3.example.com/privkey.pem"```

```



---

### Server Settings

## Storage Settings



### Storage Configuration

| Parameter | Type | Default | Description |auth:**Debug logging:**

| Parameter | Type | Default | Description |

|-----------|------|---------|-------------||-----------|------|---------|-------------|

| `storage.backend` | string | `filesystem` | Storage backend type |

| `storage.root` | string | `{data_dir}/objects` | Root directory for objects || `data_dir` | string | **required** | Base directory for all data storage |  access_key: "your-admin-key"```bash

| `storage.enable_compression` | bool | `false` | Enable object compression |

| `storage.compression_level` | int | `6` | Compression level (1-9) || `listen` | string | `:8080` | S3 API server listen address |

| `storage.compression_type` | string | `gzip` | Type (gzip, lz4, zstd) |

| `storage.enable_encryption` | bool | `false` | Enable object encryption || `console_listen` | string | `:8081` | Web console listen address |  secret_key: "your-secure-secret"./maxiofs --data-dir ./data --log-level debug

| `storage.encryption_key` | string | - | Encryption key (base64) |

| `storage.enable_object_lock` | bool | `true` | Enable Object Lock support || `log_level` | string | `info` | Logging level (debug, info, warn, error) |



**Example config.yaml:**```

```yaml

storage:**Environment variables:**

  backend: "filesystem"

  enable_compression: true- `MAXIOFS_DATA_DIR`storage:

  compression_type: "zstd"

  compression_level: 3- `MAXIOFS_LISTEN`

  enable_object_lock: true

```- `MAXIOFS_CONSOLE_LISTEN`  enable_compression: true**With TLS:**



**Directory Structure:**- `MAXIOFS_LOG_LEVEL`

```

{data_dir}/  compression_type: "zstd"```bash

├── objects/              # Object storage (configurable)

├── auth/                # Authentication database### Public URLs

│   └── auth.db

└── metrics/             # Metrics history  enable_encryption: true./maxiofs --data-dir ./data --tls-cert cert.pem --tls-key key.pem

    └── history.db

```| Parameter | Type | Default | Description |



---|-----------|------|---------|-------------|  encryption_key: "your-32-character-encryption-key"```



## TLS/HTTPS| `public_api_url` | string | auto-detected | Public URL for S3 API (for presigned URLs) |



### TLS Configuration| `public_console_url` | string | auto-detected | Public URL for web console |```



| Parameter | Type | Default | Description |

|-----------|------|---------|-------------|

| `enable_tls` | bool | `false` | Enable TLS for both servers |**Example:**---

| `cert_file` | string | - | Path to TLS certificate (PEM) |

| `key_file` | string | - | Path to TLS private key (PEM) |```yaml



**Option 1: Direct TLS (simple deployments)**public_api_url: "https://s3.example.com"## Configuration Methods

```yaml

enable_tls: truepublic_console_url: "https://console.example.com"

cert_file: "/etc/maxiofs/tls/cert.pem"

key_file: "/etc/maxiofs/tls/key.pem"```## Environment Variables

```



Or using flags:

```bash### TLS/HTTPS SettingsMaxIOFS supports three configuration methods with the following priority (highest to lowest):

./maxiofs \

  --data-dir /var/lib/maxiofs \

  --tls-cert /etc/maxiofs/tls/cert.pem \

  --tls-key /etc/maxiofs/tls/key.pem| Parameter | Type | Default | Description |All configuration options can be set via environment variables using the `MAXIOFS_` prefix.

```

|-----------|------|---------|-------------|

**Option 2: Reverse Proxy (recommended for production)**

```yaml| `enable_tls` | bool | `false` | Enable TLS for both servers |1. **Command-line flags** (highest priority)

# MaxIOFS listens on localhost only

listen: "127.0.0.1:9000"| `cert_file` | string | - | Path to TLS certificate file (PEM) |

console_listen: "127.0.0.1:9001"

enable_tls: false| `key_file` | string | - | Path to TLS private key file (PEM) |2. **Environment variables**### Variable Names



# Set public URLs

public_api_url: "https://s3.example.com"

public_console_url: "https://console.example.com"**Command-line flags:**3. **Configuration file**

```

- `--tls-cert`: Certificate file path

**nginx configuration:**

```nginx- `--tls-key`: Private key file path4. **Default values** (lowest priority)Command-line flag → Environment variable:

# S3 API

server {

    listen 443 ssl http2;

    server_name s3.example.com;**Example:**- `--data-dir` → `MAXIOFS_DATA_DIR`

    

    ssl_certificate /etc/ssl/certs/fullchain.pem;```yaml

    ssl_certificate_key /etc/ssl/private/privkey.pem;

    enable_tls: true### 1. Configuration File- `--listen` → `MAXIOFS_LISTEN`

    location / {

        proxy_pass http://127.0.0.1:9000;cert_file: "/etc/maxiofs/tls/cert.pem"

        proxy_set_header Host $host;

        proxy_set_header X-Real-IP $remote_addr;key_file: "/etc/maxiofs/tls/key.pem"- `--console-listen` → `MAXIOFS_CONSOLE_LISTEN`

        proxy_set_header X-Forwarded-Proto $scheme;

    }```

}

Create a YAML file (e.g., `config.yaml`) and pass it to MaxIOFS:- `--log-level` → `MAXIOFS_LOG_LEVEL`

# Web Console

server {Or using flags:

    listen 443 ssl http2;

    server_name console.example.com;```bash- `--tls-cert` → `MAXIOFS_TLS_CERT`

    

    ssl_certificate /etc/ssl/certs/fullchain.pem;./maxiofs --data-dir ./data --tls-cert cert.pem --tls-key key.pem

    ssl_certificate_key /etc/ssl/private/privkey.pem;

    ``````bash- `--tls-key` → `MAXIOFS_TLS_KEY`

    location / {

        proxy_pass http://127.0.0.1:9001;

        proxy_set_header Host $host;

        proxy_set_header X-Real-IP $remote_addr;### Storage Settings./maxiofs --config config.yaml

        proxy_set_header X-Forwarded-Proto $scheme;

    }

}

```| Parameter | Type | Default | Description |```### Example



---|-----------|------|---------|-------------|



## Metrics| `storage.backend` | string | `filesystem` | Storage backend type |



### Metrics Configuration| `storage.root` | string | `{data_dir}/objects` | Root directory for object storage |



| Parameter | Type | Default | Description || `storage.enable_compression` | bool | `false` | Enable object compression |See [`config.example.yaml`](../config.example.yaml) for a complete example.```bash

|-----------|------|---------|-------------|

| `metrics.enable` | bool | `true` | Enable metrics collection || `storage.compression_level` | int | `6` | Compression level (1-9) |

| `metrics.path` | string | `/metrics` | Metrics endpoint path |

| `metrics.interval` | int | `60` | Collection interval (seconds) || `storage.compression_type` | string | `gzip` | Compression type (gzip, lz4, zstd) |export MAXIOFS_DATA_DIR=/var/lib/maxiofs



**Example:**| `storage.enable_encryption` | bool | `false` | Enable object encryption |

```yaml

metrics:| `storage.encryption_key` | string | - | Encryption key (base64 encoded) |### 2. Environment Variablesexport MAXIOFS_LISTEN=:8080

  enable: true

  path: "/metrics"| `storage.enable_object_lock` | bool | `true` | Enable Object Lock support |

  interval: 60

```export MAXIOFS_CONSOLE_LISTEN=:8081



**Access metrics:****Example:**

```bash

curl http://localhost:8080/metrics```yamlAll configuration options can be set via environment variables with the `MAXIOFS_` prefix:export MAXIOFS_LOG_LEVEL=info

```

storage:

---

  backend: "filesystem"

## Common Scenarios

  enable_compression: true

### Development/Testing

  compression_type: "zstd"```bash./maxiofs

**Minimal configuration:**

```yaml  compression_level: 3

data_dir: "./data"

```  enable_object_lock: trueexport MAXIOFS_LISTEN=":9000"```



**Full development configuration:**```

```yaml

data_dir: "./data"export MAXIOFS_DATA_DIR="/var/lib/maxiofs"

listen: ":8080"

console_listen: ":8081"### Authentication Settings

log_level: "debug"

export MAXIOFS_PUBLIC_API_URL="https://s3.example.com"### Docker

auth:

  enable_auth: true| Parameter | Type | Default | Description |



metrics:|-----------|------|---------|-------------|export MAXIOFS_ENABLE_TLS=true

  enable: true

  interval: 10| `auth.enable_auth` | bool | `true` | Enable authentication |

```

| `auth.jwt_secret` | string | auto-generated | JWT signing secret |export MAXIOFS_CERT_FILE="/etc/ssl/certs/maxiofs.crt"```yaml

**Start server:**

```bash

./maxiofs --config dev-config.yaml

```**🔒 Security Notes:**export MAXIOFS_KEY_FILE="/etc/ssl/private/maxiofs.key"# docker-compose.yml



### Production Deployment



**Production config.yaml:**- **No default access keys**: For security reasons, MaxIOFS does not create default S3 access keysexport MAXIOFS_AUTH_ACCESS_KEY="myadmin"services:

```yaml

# Base configuration- **Default admin user**: A default web console user is created:

data_dir: "/var/lib/maxiofs"

listen: ":8080"  - Username: `admin`export MAXIOFS_AUTH_SECRET_KEY="mysecret"  maxiofs:

console_listen: ":8081"

log_level: "info"  - Password: `admin`



# Public URLs  - **⚠️ Change password after first login!**export MAXIOFS_STORAGE_ENABLE_COMPRESSION=true    image: maxiofs/maxiofs:1.1.0-alpha

public_api_url: "https://s3.example.com"

public_console_url: "https://console.example.com"- **Creating access keys**: S3 access keys must be created manually:



# TLS (if not using reverse proxy)  1. Login to web console at `http://localhost:8081` (admin/admin)    environment:

enable_tls: true

cert_file: "/etc/maxiofs/tls/fullchain.pem"  2. Navigate to Users section

key_file: "/etc/maxiofs/tls/privkey.pem"

  3. Click "Create Access Key" for your user./maxiofs --data-dir /var/lib/maxiofs      - MAXIOFS_DATA_DIR=/data

# Storage with compression

storage:  4. Copy and save the generated credentials

  enable_compression: true

  compression_type: "zstd"```      - MAXIOFS_LOG_LEVEL=info

  compression_level: 3

  enable_object_lock: true**Example:**



# Authentication```yaml    ports:

auth:

  enable_auth: trueauth:

  jwt_secret: "CHANGE-THIS-TO-YOUR-SECURE-SECRET"

  enable_auth: true### 3. Command-line Flags      - "8080:8080"

# Metrics

metrics:  jwt_secret: "your-secure-random-jwt-secret-min-32-chars"

  enable: true

  interval: 60```      - "8081:8081"

```



**Systemd service (/etc/systemd/system/maxiofs.service):**

```ini### Metrics Settings```bash    volumes:

[Unit]

Description=MaxIOFS Object Storage

After=network.target

| Parameter | Type | Default | Description |./maxiofs \      - ./data:/data

[Service]

Type=simple|-----------|------|---------|-------------|

User=maxiofs

Group=maxiofs| `metrics.enable` | bool | `true` | Enable metrics collection |  --data-dir /var/lib/maxiofs \```

ExecStart=/usr/local/bin/maxiofs --config /etc/maxiofs/config.yaml

Restart=always| `metrics.path` | string | `/metrics` | Metrics endpoint path |

RestartSec=5

| `metrics.interval` | int | `60` | Collection interval (seconds) |  --listen :9000 \

# Security hardening

NoNewPrivileges=true

PrivateTmp=true

ProtectSystem=strict**Example:**  --console-listen :9001 \---

ProtectHome=true

ReadWritePaths=/var/lib/maxiofs```yaml



[Install]metrics:  --log-level info \

WantedBy=multi-user.target

```  enable: true



**Enable and start:**  path: "/metrics"  --tls-cert /etc/ssl/certs/maxiofs.crt \## Configuration Options

```bash

sudo systemctl daemon-reload  interval: 30

sudo systemctl enable maxiofs

sudo systemctl start maxiofs```  --tls-key /etc/ssl/private/maxiofs.key

sudo systemctl status maxiofs

```



### Docker Deployment**Access metrics:**```### Data Directory



**docker-compose.yml:**```bash

```yaml

version: '3.8'curl http://localhost:8080/metrics



services:```

  maxiofs:

    image: maxiofs:latestAvailable flags:**Flag**: `--data-dir`

    container_name: maxiofs

    ports:---

      - "8080:8080"

      - "8081:8081"- `--config, -c` - Configuration file path**Environment**: `MAXIOFS_DATA_DIR`

    volumes:

      - ./data:/data## Common Scenarios

      - ./config.yaml:/etc/maxiofs/config.yaml:ro

    environment:- `--data-dir, -d` - Data directory (required)**Required**: Yes

      - MAXIOFS_DATA_DIR=/data

    command: ["--config", "/etc/maxiofs/config.yaml"]### Development/Testing

    restart: unless-stopped

```- `--listen, -l` - API server address (default: `:8080`)**Default**: None



**config.yaml for Docker:**```yaml

```yaml

data_dir: "/data"data_dir: "./data"- `--console-listen` - Web console address (default: `:8081`)

listen: ":8080"

console_listen: ":8081"listen: ":8080"

log_level: "info"

console_listen: ":8081"- `--log-level` - Log level: debug, info, warn, error (default: `info`)The data directory stores all MaxIOFS data:

storage:

  enable_compression: truelog_level: "debug"

  compression_type: "zstd"

- `--tls-cert` - TLS certificate file- `{data-dir}/maxiofs.db` - SQLite metadata database

auth:

  enable_auth: trueauth:



metrics:  enable_auth: true- `--tls-key` - TLS private key file- `{data-dir}/objects/` - Object storage

  enable: true

```



### Reverse Proxy Setupmetrics:



**MaxIOFS config:**  enable: true

```yaml

data_dir: "/var/lib/maxiofs"  interval: 10## Configuration Parameters**Example structure:**

listen: "127.0.0.1:9000"

console_listen: "127.0.0.1:9001"```

log_level: "info"

```

public_api_url: "https://s3.example.com"

public_console_url: "https://console.example.com"### Production Deployment



# No TLS - handled by reverse proxy### Server Settings/var/lib/maxiofs/

enable_tls: false

```yaml

auth:

  enable_auth: true# Base configuration├── maxiofs.db           # Metadata

  jwt_secret: "your-secure-secret"

```data_dir: "/var/lib/maxiofs"



---listen: ":8080"| Parameter | Type | Default | Description |└── objects/             # Objects



## Security Best Practicesconsole_listen: ":8081"



### 1. Change Default Passwordlog_level: "info"|-----------|------|---------|-------------|    ├── global/          # Global admin buckets



**Immediately after first installation:**

1. Login to web console: `http://localhost:8081`

2. Use credentials: `admin` / `admin`# Public URLs (for presigned URLs and redirects)| `listen` | string | `:8080` | API server listen address |    └── tenant-123/      # Tenant buckets

3. Navigate to Settings or User Profile

4. Change admin password to a strong passwordpublic_api_url: "https://s3.example.com"



### 2. Create Secure Access Keyspublic_console_url: "https://console.example.com"| `console_listen` | string | `:8081` | Web console listen address |```



**For S3 API access:**

1. Login to web console

2. Go to Users section# TLS (if not using reverse proxy)| `data_dir` | string | `./data` | Data directory path (required) |

3. Create access keys for each user/application

4. Store keys securely (password manager, secrets vault)enable_tls: true

5. Never commit keys to version control

cert_file: "/etc/maxiofs/tls/fullchain.pem"| `log_level` | string | `info` | Log level (debug, info, warn, error) |**Important:**

**Best practices:**

- Use separate keys for different applicationskey_file: "/etc/maxiofs/tls/privkey.pem"

- Rotate keys regularly (every 90 days)

- Revoke unused keys immediately- Directory must be writable

- Never share keys between users

# Storage

### 3. Use Strong JWT Secret

storage:### Public URLs- Must persist across restarts

**Generate secure secret:**

```bash  enable_compression: true

openssl rand -base64 32

```  compression_type: "zstd"- Backup regularly



**Set in config:**  compression_level: 3

```yaml

auth:  enable_object_lock: true| Parameter | Type | Default | Description |

  jwt_secret: "your-generated-secret-minimum-32-characters"

```



### 4. Enable TLS/HTTPS# Authentication|-----------|------|---------|-------------|### Ports



**Always use HTTPS in production:**auth:

- Option 1: Configure TLS directly in MaxIOFS

- Option 2: Use reverse proxy (nginx, Caddy, Traefik) - **recommended**  enable_auth: true| `public_api_url` | string | `http://localhost:8080` | Public S3 API URL |



**Why reverse proxy is recommended:**  jwt_secret: "your-very-secure-random-secret-min-32-characters-long"

- Automatic certificate renewal (Let's Encrypt)

- Better performance| `public_console_url` | string | `http://localhost:8081` | Public web console URL |**S3 API Port**

- Advanced features (rate limiting, WAF)

- Centralized TLS management# Metrics



### 5. Restrict Network Accessmetrics:**Flag**: `--listen`



**Firewall rules:**  enable: true

```bash

# Allow only necessary ports  interval: 60These URLs are used for:**Environment**: `MAXIOFS_LISTEN`

sudo ufw allow 8080/tcp   # S3 API

sudo ufw allow 8081/tcp   # Web Console```



# Or restrict to specific IPs- Generating presigned URLs**Default**: `:8080`

sudo ufw allow from 10.0.0.0/8 to any port 8080

```### Docker Deployment



**For reverse proxy setups:**- CORS configuration

```yaml

# Bind to localhost only```yaml

listen: "127.0.0.1:9000"

console_listen: "127.0.0.1:9001"data_dir: "/data"- External access documentationPort for S3-compatible API.

```

listen: ":8080"

### 6. File Permissions

console_listen: ":8081"- Redirects in the web console

**Configuration files:**

```bashlog_level: "info"

chmod 600 /etc/maxiofs/config.yaml

chown maxiofs:maxiofs /etc/maxiofs/config.yaml**Console Port**

```

public_api_url: "http://localhost:8080"

**TLS certificates:**

```bashpublic_console_url: "http://localhost:8081"### TLS/SSL Settings**Flag**: `--console-listen`

chmod 600 /etc/maxiofs/tls/*.pem

chown maxiofs:maxiofs /etc/maxiofs/tls/*.pem

```

storage:**Environment**: `MAXIOFS_CONSOLE_LISTEN`

**Data directory:**

```bash  enable_compression: true

chmod 700 /var/lib/maxiofs

chown maxiofs:maxiofs /var/lib/maxiofs  compression_type: "zstd"| Parameter | Type | Default | Description |**Default**: `:8081`

```



### 7. Run as Non-Root User

auth:|-----------|------|---------|-------------|

**Create dedicated user:**

```bash  enable_auth: true

sudo useradd -r -s /bin/false maxiofs

sudo mkdir -p /var/lib/maxiofs| `enable_tls` | bool | `false` | Enable TLS/SSL |Port for web console and console API.

sudo chown maxiofs:maxiofs /var/lib/maxiofs

```metrics:



**Systemd service runs as maxiofs user** (see service file above)  enable: true| `cert_file` | string | - | TLS certificate file path |



---```



## AWS CLI Configuration| `key_file` | string | - | TLS private key file path |### Log Level



### Setting Up AWS CLI**Docker Compose:**



**After creating access keys in web console:**```yaml



```bashversion: '3.8'

# Configure profile

aws configure --profile maxiofsservices:### Storage Settings**Flag**: `--log-level`



# Enter your generated credentials:  maxiofs:

AWS Access Key ID: [your-access-key-from-console]

AWS Secret Access Key: [your-secret-key-from-console]    image: maxiofs:latest**Environment**: `MAXIOFS_LOG_LEVEL`

Default region name: us-east-1

Default output format: json    volumes:

```

      - ./data:/data| Parameter | Type | Default | Description |**Default**: `info`

### Usage Examples

      - ./config.yaml:/etc/maxiofs/config.yaml:ro

```bash

# List buckets    ports:|-----------|------|---------|-------------|**Options**: `debug`, `info`, `warn`, `error`

aws s3 --profile maxiofs --endpoint-url http://localhost:8080 ls

      - "8080:8080"

# Create bucket

aws s3 --profile maxiofs --endpoint-url http://localhost:8080 mb s3://my-bucket      - "8081:8081"| `storage.backend` | string | `filesystem` | Storage backend (filesystem) |



# Upload file    command: ["--config", "/etc/maxiofs/config.yaml"]

aws s3 --profile maxiofs --endpoint-url http://localhost:8080 cp file.txt s3://my-bucket/

```| `storage.root` | string | `{data_dir}/objects` | Storage root directory |- `debug` - Verbose logging (development)

# Download file

aws s3 --profile maxiofs --endpoint-url http://localhost:8080 cp s3://my-bucket/file.txt .



# List objects in bucket### Production with Reverse Proxy| `storage.enable_compression` | bool | `false` | Enable compression |- `info` - Normal logging (production)

aws s3 --profile maxiofs --endpoint-url http://localhost:8080 ls s3://my-bucket/



# Delete object

aws s3 --profile maxiofs --endpoint-url http://localhost:8080 rm s3://my-bucket/file.txt**MaxIOFS config:**| `storage.compression_level` | int | `6` | Compression level (1-9) |- `warn` - Warnings and errors only



# Sync directory```yaml

aws s3 --profile maxiofs --endpoint-url http://localhost:8080 sync ./local-dir s3://my-bucket/

```data_dir: "/var/lib/maxiofs"| `storage.compression_type` | string | `gzip` | Compression type (gzip, lz4, zstd) |- `error` - Errors only



---listen: "127.0.0.1:9000"



## Veeam Backup & Replicationconsole_listen: "127.0.0.1:9001"| `storage.enable_encryption` | bool | `false` | Enable encryption at rest |



### Configuring MaxIOFS as Object Storagelog_level: "info"



**Prerequisites:**| `storage.encryption_key` | string | - | AES-256 encryption key (32 chars) |### TLS/HTTPS

1. Create access keys in MaxIOFS web console

2. Create a dedicated bucket for Veeam backupspublic_api_url: "https://s3.example.com"



**In Veeam Console:**public_console_url: "https://console.example.com"| `storage.enable_object_lock` | bool | `true` | Enable S3 Object Lock |



1. **Add Object Storage Repository:**

   - Backup Infrastructure → Backup Repositories → Add Repository

   - Select "Object storage"# No TLS here - handled by reverse proxy**Certificate**



2. **Configure Service Point:**enable_tls: false

   - Type: S3 Compatible

   - Service point: `http://your-maxiofs-server:8080` (or HTTPS URL)### Authentication Settings**Flag**: `--tls-cert`

   - Credentials: Use your generated access key and secret key

auth:

3. **Select Bucket:**

   - Choose existing bucket or create new one  enable_auth: true**Environment**: `MAXIOFS_TLS_CERT`

   - Specify folder path (optional)

  jwt_secret: "your-secure-secret"

4. **Advanced Settings:**

   - MaxIOFS supports:```| Parameter | Type | Default | Description |**Optional**: Yes

     - Multi-part uploads

     - Object Lock (for immutability)

     - S3 v2 and v4 signatures

**nginx config:**|-----------|------|---------|-------------|

**Best Practices for Veeam:**

- Enable Object Lock in MaxIOFS for immutable backups```nginx

- Use separate bucket for Veeam backups

- Create dedicated user with minimal required permissions# S3 API| `auth.enable_auth` | bool | `true` | Enable authentication |Path to TLS certificate file (PEM format).

- Use HTTPS for production

- Monitor storage capacityserver {



---    listen 443 ssl http2;| `auth.jwt_secret` | string | auto-generated | JWT signing secret |



## Troubleshooting    server_name s3.example.com;



### Server Won't Start    | `auth.access_key` | string | `maxioadmin` | Default admin access key |**Private Key**



**Problem:** "data_dir is required" error    ssl_certificate /etc/ssl/certs/cert.pem;



**Solution:**    ssl_certificate_key /etc/ssl/private/key.pem;| `auth.secret_key` | string | `maxioadmin` | Default admin secret key |**Flag**: `--tls-key`

```bash

# Ensure data_dir is configured via one of:    



# 1. Command-line flag    location / {| `auth.users_file` | string | - | External users file (optional) |**Environment**: `MAXIOFS_TLS_KEY`

./maxiofs --data-dir /var/lib/maxiofs

        proxy_pass http://127.0.0.1:9000;

# 2. Config file

./maxiofs --config config.yaml  # with data_dir inside        proxy_set_header Host $host;**Optional**: Yes



# 3. Environment variable        proxy_set_header X-Real-IP $remote_addr;

export MAXIOFS_DATA_DIR=/var/lib/maxiofs

./maxiofs        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;### Metrics Settings

```

        proxy_set_header X-Forwarded-Proto $scheme;

### Permission Denied Errors

    }Path to TLS private key file (PEM format).

**Problem:** Cannot create files/directories

}

**Solution:**

```bash| Parameter | Type | Default | Description |

# Check permissions

ls -ld /var/lib/maxiofs# Web Console



# Fix ownershipserver {|-----------|------|---------|-------------|**Example:**

sudo chown -R maxiofs:maxiofs /var/lib/maxiofs

    listen 443 ssl http2;

# Fix permissions

sudo chmod -R 700 /var/lib/maxiofs    server_name console.example.com;| `metrics.enable` | bool | `true` | Enable metrics collection |```bash

```

    

### Port Already in Use

    ssl_certificate /etc/ssl/certs/cert.pem;| `metrics.path` | string | `/metrics` | Metrics endpoint path |./maxiofs --data-dir ./data \

**Problem:** "address already in use" error

    ssl_certificate_key /etc/ssl/private/key.pem;

**Solution:**

```bash    | `metrics.interval` | int | `60` | Collection interval (seconds) |  --tls-cert /etc/maxiofs/cert.pem \

# Find process using port

sudo netstat -tlnp | grep :8080    location / {



# Change port in config        proxy_pass http://127.0.0.1:9001;  --tls-key /etc/maxiofs/key.pem

listen: ":9000"

console_listen: ":9001"        proxy_set_header Host $host;

```

        proxy_set_header X-Real-IP $remote_addr;## Common Scenarios```

### Cannot Login to Web Console

        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;

**Problem:** Admin password not working

        proxy_set_header X-Forwarded-Proto $scheme;

**Solution:**

```bash    }

# Reset auth database (creates new admin user)

sudo systemctl stop maxiofs}### Local Development**Notes:**

sudo rm -f /var/lib/maxiofs/auth/auth.db

sudo systemctl start maxiofs```



# Default credentials will be recreated:- Both cert and key must be provided together

# Username: admin

# Password: admin---

```

```yaml- Alternatively, use a reverse proxy (nginx/traefik) for TLS

### S3 Access Denied

## Security Best Practices

**Problem:** S3 API returns 403 Forbidden

listen: ":8080"- Self-signed certificates work for testing

**Solution:**

1. Verify access keys are created in web console### 1. Access Keys Management

2. Check credentials in AWS CLI config:

   ```bashconsole_listen: ":8081"

   cat ~/.aws/credentials

   ```- **Never use default credentials in production**

3. Ensure keys are active (not disabled)

4. Check user has proper permissions- **Create unique access keys** for each user/applicationdata_dir: "./data"---

5. Enable debug logging to see authentication details:

   ```bash- **Rotate keys regularly** (every 90 days recommended)

   ./maxiofs --log-level debug

   ```- **Use separate keys** for different environments (dev/staging/prod)log_level: "debug"



### TLS Certificate Errors- **Revoke unused keys** immediately



**Problem:** Certificate validation failures## Default Credentials



**Solution:**### 2. Strong JWT Secret

```bash

# Verify certificateauth:

openssl x509 -in cert.pem -text -noout

Generate a secure JWT secret:

# Check certificate chain

openssl verify -CAfile ca.pem cert.pem  access_key: "maxioadmin"**Web Console:**



# Verify key matches certificate```bash

openssl x509 -noout -modulus -in cert.pem | openssl md5

openssl rsa -noout -modulus -in key.pem | openssl md5# Linux/macOS  secret_key: "maxioadmin"- Username: `admin`

# Hashes should match

```openssl rand -base64 32



---```- Password: `admin`



## Environment Variables Reference# PowerShell



All configuration parameters can be set via environment variables with `MAXIOFS_` prefix:[Convert]::ToBase64String((1..32 | ForEach-Object { Get-Random -Maximum 256 }))



| Parameter | Environment Variable |```

|-----------|---------------------|

| `data_dir` | `MAXIOFS_DATA_DIR` |Run:**S3 API:**

| `listen` | `MAXIOFS_LISTEN` |

| `console_listen` | `MAXIOFS_CONSOLE_LISTEN` |Set in config:

| `log_level` | `MAXIOFS_LOG_LEVEL` |

| `public_api_url` | `MAXIOFS_PUBLIC_API_URL` |```yaml```bash- Access Key: `maxioadmin`

| `public_console_url` | `MAXIOFS_PUBLIC_CONSOLE_URL` |

| `enable_tls` | `MAXIOFS_ENABLE_TLS` |auth:

| `cert_file` | `MAXIOFS_CERT_FILE` |

| `key_file` | `MAXIOFS_KEY_FILE` |  jwt_secret: "your-generated-32-char-minimum-secret"./maxiofs --config config.yaml- Secret Key: `maxioadmin`

| `storage.backend` | `MAXIOFS_STORAGE_BACKEND` |

| `storage.root` | `MAXIOFS_STORAGE_ROOT` |```

| `auth.enable_auth` | `MAXIOFS_AUTH_ENABLE_AUTH` |

| `auth.jwt_secret` | `MAXIOFS_AUTH_JWT_SECRET` |```

| `metrics.enable` | `MAXIOFS_METRICS_ENABLE` |

### 3. TLS/HTTPS

---

**⚠️ IMPORTANT**: Change default credentials after first login!

## Additional Resources

**Always use TLS in production:**

- **API Documentation**: [API.md](API.md)

- **Architecture**: [ARCHITECTURE.md](ARCHITECTURE.md)### Production with Reverse Proxy (nginx/Caddy)

- **Security**: [SECURITY.md](SECURITY.md)

- **Deployment**: [DEPLOYMENT.md](DEPLOYMENT.md)Option 1 - Direct TLS:

- **Multi-Tenancy**: [MULTI_TENANCY.md](MULTI_TENANCY.md)

```yaml---

For issues and support: https://github.com/maxiofs/maxiofs/issues

enable_tls: true

cert_file: "/path/to/cert.pem"```yaml

key_file: "/path/to/key.pem"

```listen: "127.0.0.1:9000"## Storage Paths



Option 2 - Reverse Proxy (recommended):console_listen: "127.0.0.1:9001"

- Use nginx/Caddy/Traefik for TLS termination

- Configure MaxIOFS to listen on localhost onlydata_dir: "/var/lib/maxiofs"### Object Storage

- Set correct `public_api_url` and `public_console_url`

log_level: "info"

### 4. File Permissions

Objects are stored in the filesystem:

```bash

# Configuration filepublic_api_url: "https://s3.example.com"

chmod 600 /etc/maxiofs/config.yaml

chown maxiofs:maxiofs /etc/maxiofs/config.yamlpublic_console_url: "https://console.example.com"```



# TLS certificates{data-dir}/objects/{tenant-id}/{bucket-name}/{object-key}

chmod 600 /etc/maxiofs/tls/*.pem

chown maxiofs:maxiofs /etc/maxiofs/tls/*.pem# TLS handled by reverse proxy```



# Data directoryenable_tls: false

chmod 700 /var/lib/maxiofs

chown maxiofs:maxiofs /var/lib/maxiofs**Examples:**

```

storage:- Global bucket: `./data/objects/global/my-bucket/file.txt`

### 5. Firewall Rules

  enable_compression: true- Tenant bucket: `./data/objects/tenant-123/backup/file.bin`

```bash

# Allow only necessary ports  compression_type: "zstd"

ufw allow 8080/tcp  # S3 API

ufw allow 8081/tcp  # Web Console (or block if using reverse proxy)  enable_encryption: true### Metadata Database

```

  encryption_key: "your-secure-32-character-key-here"

---

SQLite database at:

## Storage Paths

auth:```

MaxIOFS uses the following directory structure:

  access_key: "production-admin"{data-dir}/maxiofs.db

```

{data_dir}/  secret_key: "your-very-secure-secret-key"```

├── objects/           # Object storage (configurable via storage.root)

│   ├── bucket-name/```

│   └── tenant-{id}/

├── auth/             # Authentication databaseContains:

│   └── auth.db

└── metrics/          # Metrics historynginx configuration:- Users and credentials (bcrypt hashed)

    └── history.db

``````nginx- Tenants and quotas



---server {- Buckets metadata



## AWS CLI Configuration    listen 443 ssl http2;- Access keys



After creating access keys in the web console:    server_name s3.example.com;- Object metadata



```bash    

# Configure profile

aws configure --profile maxiofs    ssl_certificate /etc/letsencrypt/live/s3.example.com/fullchain.pem;---



# You'll be prompted for:    ssl_certificate_key /etc/letsencrypt/live/s3.example.com/privkey.pem;

AWS Access Key ID: [your-generated-access-key]

AWS Secret Access Key: [your-generated-secret-key]    ## Production Recommendations

Default region name: us-east-1

Default output format: json    location / {

```

        proxy_pass http://127.0.0.1:9000;### Basic Setup

**Usage:**

```bash        proxy_set_header Host $host;

# List buckets

aws s3 --profile maxiofs --endpoint-url http://localhost:8080 ls        proxy_set_header X-Real-IP $remote_addr;```bash



# Create bucket        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;# 1. Create data directory

aws s3 --profile maxiofs --endpoint-url http://localhost:8080 mb s3://my-bucket

        proxy_set_header X-Forwarded-Proto $scheme;mkdir -p /var/lib/maxiofs

# Upload file

aws s3 --profile maxiofs --endpoint-url http://localhost:8080 cp file.txt s3://my-bucket/    }chown maxiofs:maxiofs /var/lib/maxiofs

```

}chmod 750 /var/lib/maxiofs

---

```

## Veeam Backup & Replication

# 2. Run MaxIOFS

### Configure Object Storage Repository

### Production with Direct TLS./maxiofs --data-dir /var/lib/maxiofs --log-level info

1. **Create access keys in MaxIOFS web console**

   - Login to http://localhost:8081```

   - Create a dedicated user for Veeam

   - Generate access key for that user```yaml



2. **In Veeam Console:**listen: ":9000"### With Systemd

   - Add Object Storage Repository

   - **Service point**: `http://your-server:8080` (or HTTPS URL)console_listen: ":9001"

   - **Account**: Use generated credentials

   - **Bucket**: Create or select existing bucketdata_dir: "/var/lib/maxiofs"```ini

   - **Folder**: Optional path within bucket

log_level: "info"# /etc/systemd/system/maxiofs.service

3. **Compatibility mode:**

   - MaxIOFS supports S3 v2 and v4 signatures[Unit]

   - Use "S3 Compatible" mode in Veeam

   - Multi-part uploads supportedpublic_api_url: "https://s3.example.com:9000"Description=MaxIOFS Object Storage

   - Object Lock supported (for immutability)

public_console_url: "https://console.example.com:9001"After=network.target

---



## Troubleshooting

enable_tls: true[Service]

### Configuration Not Loading

cert_file: "/etc/letsencrypt/live/s3.example.com/fullchain.pem"Type=simple

**Problem**: Changes in config file not applied

key_file: "/etc/letsencrypt/live/s3.example.com/privkey.pem"User=maxiofs

**Solutions:**

1. Verify config file syntax: `yamllint config.yaml`Group=maxiofs

2. Check file path: `./maxiofs --config /full/path/to/config.yaml`

3. Check for flag overrides (flags have higher priority)storage:ExecStart=/usr/local/bin/maxiofs --data-dir /var/lib/maxiofs --log-level info

4. Enable debug logging: `--log-level debug`

  enable_compression: trueRestart=on-failure

### Data Directory Issues

  compression_type: "zstd"RestartSec=5

**Problem**: "failed to create data directory" error

  enable_encryption: true

**Solutions:**

1. Verify directory exists and has correct permissions:  encryption_key: "your-secure-32-character-key-here"[Install]

   ```bash

   mkdir -p /var/lib/maxiofsWantedBy=multi-user.target

   chown maxiofs:maxiofs /var/lib/maxiofs

   chmod 700 /var/lib/maxiofsauth:```

   ```

  access_key: "production-admin"

2. Check SELinux context (if applicable):

   ```bash  secret_key: "your-very-secure-secret-key"```bash

   semanage fcontext -a -t container_file_t "/var/lib/maxiofs(/.*)?"

   restorecon -Rv /var/lib/maxiofs```# Start service

   ```

systemctl daemon-reload

### TLS Certificate Errors

### Docker Deploymentsystemctl enable maxiofs

**Problem**: "certificate signed by unknown authority"

systemctl start maxiofs

**Solutions:**

1. Verify certificate chain includes all intermediates`docker-compose.yml`:```

2. Use fullchain.pem (not just cert.pem)

3. Check certificate validity: `openssl x509 -in cert.pem -text -noout````yaml

4. Verify key matches cert:

   ```bashversion: '3.8'### With Docker

   openssl x509 -noout -modulus -in cert.pem | openssl md5

   openssl rsa -noout -modulus -in key.pem | openssl md5

   # Hashes should match

   ```services:```bash



### Authentication Issues  maxiofs:docker run -d \



**Problem**: Cannot login to web console    image: maxiofs:latest  --name maxiofs \



**Solutions:**    container_name: maxiofs  -p 8080:8080 \

1. Reset admin password:

   ```bash    ports:  -p 8081:8081 \

   # Stop MaxIOFS

   # Delete auth database      - "8080:8080"  # API  -v /var/lib/maxiofs:/data \

   rm -f /var/lib/maxiofs/auth/auth.db

   # Restart MaxIOFS (will recreate admin user)      - "8081:8081"  # Console  -e MAXIOFS_DATA_DIR=/data \

   ```

    volumes:  -e MAXIOFS_LOG_LEVEL=info \

2. Check JWT secret consistency (if using multiple instances)

      - ./data:/data  maxiofs/maxiofs:1.1.0-alpha

**Problem**: S3 access denied

      - ./config.yaml:/config.yaml:ro```

**Solutions:**

1. Verify access key is active (check web console)      - ./certs:/certs:ro

2. Ensure correct credentials in AWS CLI config

3. Check signature version (v2 vs v4)    command: --config /config.yaml### With Reverse Proxy (nginx)

4. Enable debug logging to see authentication details

    environment:

### Performance Issues

      - MAXIOFS_LOG_LEVEL=info```nginx

**Problem**: Slow upload/download speeds

    restart: unless-stopped# nginx.conf

**Solutions:**

1. Enable compression for smaller objects:    healthcheck:server {

   ```yaml

   storage:      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]    listen 443 ssl http2;

     enable_compression: true

     compression_type: "zstd"      interval: 30s    server_name maxiofs.example.com;

     compression_level: 3

   ```      timeout: 10s



2. Check disk I/O: `iostat -x 1`      retries: 3    ssl_certificate /etc/ssl/cert.pem;

3. Monitor with metrics endpoint

4. Consider separate disk for data_dir```    ssl_certificate_key /etc/ssl/key.pem;



### Port Already in Use



**Problem**: "address already in use" error`config.yaml`:    # Web Console



**Solutions:**```yaml    location / {

1. Check what's using the port:

   ```bashlisten: ":8080"        proxy_pass http://localhost:8081;

   # Linux

   sudo netstat -tlnp | grep :8080console_listen: ":8081"        proxy_set_header Host $host;

   # Or

   sudo lsof -i :8080data_dir: "/data"        proxy_set_header X-Real-IP $remote_addr;

   

   # Windowslog_level: "info"    }

   netstat -ano | findstr :8080

   ```



2. Change listen address in config:public_api_url: "https://s3.example.com"    # S3 API

   ```yaml

   listen: ":9000"public_console_url: "https://console.example.com"    location /s3/ {

   console_listen: ":9001"

   ```        rewrite ^/s3/(.*) /$1 break;



---enable_tls: true        proxy_pass http://localhost:8080;



## Systemd Service (Linux)cert_file: "/certs/fullchain.pem"        proxy_set_header Host $host;



Create `/etc/systemd/system/maxiofs.service`:key_file: "/certs/privkey.pem"    }



```ini}

[Unit]

Description=MaxIOFS Object Storagestorage:```

Documentation=https://github.com/maxiofs/maxiofs

After=network.target  enable_compression: true



[Service]  compression_type: "zstd"---

Type=simple

User=maxiofs

Group=maxiofs

ExecStart=/usr/local/bin/maxiofs --config /etc/maxiofs/config.yamlauth:## Security Best Practices

Restart=always

RestartSec=5  access_key: "myadmin"

StandardOutput=journal

StandardError=journal  secret_key: "mysecret"1. **Change default credentials** immediately after first login

SyslogIdentifier=maxiofs

```2. **Use HTTPS** (TLS or reverse proxy)

# Security hardening

NoNewPrivileges=true3. **Restrict network access** (firewall rules)

PrivateTmp=true

ProtectSystem=strict### High-Security Configuration4. **Backup data directory** regularly

ProtectHome=true

ReadWritePaths=/var/lib/maxiofs5. **Use strong passwords** for all users



[Install]```yaml6. **Monitor logs** for suspicious activity

WantedBy=multi-user.target

```listen: "127.0.0.1:9000"7. **Keep MaxIOFS updated** to latest stable version



**Enable and start:**console_listen: "127.0.0.1:9001"

```bash

sudo systemctl daemon-reloaddata_dir: "/var/lib/maxiofs"---

sudo systemctl enable maxiofs

sudo systemctl start maxiofslog_level: "warn"

sudo systemctl status maxiofs

```## Troubleshooting



**View logs:**public_api_url: "https://s3.example.com"

```bash

sudo journalctl -u maxiofs -fpublic_console_url: "https://console.example.com"### Permission Denied

```



---

enable_tls: true```bash

## Environment Variables Reference

cert_file: "/etc/ssl/certs/maxiofs.crt"# Check data directory permissions

All configuration parameters can be set via environment variables with the `MAXIOFS_` prefix:

key_file: "/etc/ssl/private/maxiofs.key"ls -la /var/lib/maxiofs

| Config Parameter | Environment Variable |

|-----------------|---------------------|

| `data_dir` | `MAXIOFS_DATA_DIR` |

| `listen` | `MAXIOFS_LISTEN` |storage:# Fix ownership

| `console_listen` | `MAXIOFS_CONSOLE_LISTEN` |

| `log_level` | `MAXIOFS_LOG_LEVEL` |  enable_compression: truechown -R maxiofs:maxiofs /var/lib/maxiofs

| `public_api_url` | `MAXIOFS_PUBLIC_API_URL` |

| `public_console_url` | `MAXIOFS_PUBLIC_CONSOLE_URL` |  compression_type: "zstd"chmod 750 /var/lib/maxiofs

| `enable_tls` | `MAXIOFS_ENABLE_TLS` |

| `cert_file` | `MAXIOFS_CERT_FILE` |  compression_level: 9```

| `key_file` | `MAXIOFS_KEY_FILE` |

| `storage.backend` | `MAXIOFS_STORAGE_BACKEND` |  enable_encryption: true

| `storage.root` | `MAXIOFS_STORAGE_ROOT` |

| `auth.enable_auth` | `MAXIOFS_AUTH_ENABLE_AUTH` |  encryption_key: "use-a-strong-32-character-key!"### Port Already in Use

| `auth.jwt_secret` | `MAXIOFS_AUTH_JWT_SECRET` |

| `metrics.enable` | `MAXIOFS_METRICS_ENABLE` |  enable_object_lock: true



**Example:**```bash

```bash

export MAXIOFS_DATA_DIR=/var/lib/maxiofsauth:# Check what's using the port

export MAXIOFS_LOG_LEVEL=debug

export MAXIOFS_AUTH_JWT_SECRET=my-secret-key  enable_auth: truenetstat -tuln | grep 8080

./maxiofs

```  jwt_secret: "generate-strong-random-secret-here"



---  access_key: "admin-$(openssl rand -hex 8)"# Use different port



## Additional Resources  secret_key: "$(openssl rand -base64 32)"./maxiofs --data-dir ./data --listen :9000



- **API Documentation**: See `docs/API.md````

- **Architecture**: See `docs/ARCHITECTURE.md`

- **Multi-Tenancy**: See `docs/MULTI_TENANCY.md`metrics:

- **Security**: See `docs/SECURITY.md`

- **Deployment**: See `docs/DEPLOYMENT.md`  enable: true### Cannot Connect



For issues and support, visit: https://github.com/maxiofs/maxiofs/issues  path: "/internal/metrics"


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



```yaml See [TODO.md](../TODO.md) for roadmap.

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
