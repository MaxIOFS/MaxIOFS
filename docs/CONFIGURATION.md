
# MaxIOFS Configuration Guide

Configuration reference for MaxIOFS v0.2.2-alpha
  
---

## Table of Contents
  
- [Configuration Methods](#configuration-methods)

- [Required Settings](#required-settings)

- [Server Settings](#server-settings)

- [TLS/HTTPS](#tlshttps)

- [Storage](#storage)

- [Authentication](#authentication)

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
| `data_dir`           | string | **required**  | Data directory      |
| `listen`             | string |    `:8080`    | S3 API address      |
| `console_listen`     | string |    `:8081`    | Web console address |
| `log_level`          | string |     `info`    | Log level           |
| `public_api_url`     | string |      auto     | Public S3 URL       |
| `public_console_url` | string |      auto     | Public console URL  |

  

**Example config.yaml:**

```yaml
data_dir: "/var/lib/maxiofs"
listen: ":8080"
console_listen: ":8081"
log_level: "info"
public_api_url: "https://s3.example.com"
public_console_url: "https://console.example.com"
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
listen: "127.0.0.1:9000"
console_listen: "127.0.0.1:9001"
enable_tls: false
public_api_url: "https://s3.example.com"
public_console_url: "https://console.example.com"
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

## Examples

### Development

```yaml
data_dir: "./data"
log_level: "debug"
```

### Production
 

```yaml
data_dir: "/var/lib/maxiofs"
listen: ":8080"
console_listen: ":8081"
log_level: "info"

public_api_url: "https://s3.example.com"
public_console_url: "https://console.example.com"

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