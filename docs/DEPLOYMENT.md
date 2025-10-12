# MaxIOFS Deployment Guide

**Version**: 0.2.0-dev

## Overview

MaxIOFS is an S3-compatible object storage system currently in **alpha development**. This guide covers basic deployment methods suitable for testing and development environments.

**Default Credentials:**
- Web Console: `admin` / `admin`
- S3 API: Access Key `maxioadmin` / Secret Key `maxioadmin`

**⚠️ Change these credentials immediately after deployment.**

---

## System Requirements

### Minimum Requirements
- CPU: 2 cores
- RAM: 2 GB
- Storage: 10 GB
- OS: Linux, Windows, or macOS

### Software Requirements
- Go 1.21+ (for building)
- Node.js 18+ (for building)
- SQLite3 (embedded)

---

## Standalone Binary Deployment

### Building from Source

**Windows:**
```bash
git clone https://github.com/yourusername/maxiofs.git
cd maxiofs
build.bat
```

**Linux/macOS:**
```bash
git clone https://github.com/yourusername/maxiofs.git
cd maxiofs
make build
```

### Running MaxIOFS

**Basic usage:**
```bash
./maxiofs --data-dir ./data --log-level info
```

**Custom ports:**
```bash
./maxiofs --data-dir /var/lib/maxiofs --listen :9000 --console-listen :9001
```

**Available Options:**
```
--data-dir string       Data directory (REQUIRED)
--listen string         S3 API port (default ":8080")
--console-listen string Console port (default ":8081")
--log-level string      Log level (default "info")
--tls-cert string       TLS certificate (optional)
--tls-key string        TLS private key (optional)
```

### Accessing the Application

- **Web Console**: http://localhost:8081
- **S3 API**: http://localhost:8080

---

## Docker Deployment

### Using Docker

**Pull and run:**
```bash
docker run -d \
  --name maxiofs \
  -p 8080:8080 \
  -p 8081:8081 \
  -v $(pwd)/data:/data \
  maxiofs/maxiofs:0.2.0-dev
```

### Docker Compose

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  maxiofs:
    image: maxiofs/maxiofs:0.2.0-dev
    container_name: maxiofs
    ports:
      - "8080:8080"
      - "8081:8081"
    volumes:
      - ./data:/data
    environment:
      - MAXIOFS_DATA_DIR=/data
      - MAXIOFS_LOG_LEVEL=info
    restart: unless-stopped
```

**Run:**
```bash
docker-compose up -d
```

---

## Systemd Service (Linux)

### Installation

**1. Create directories:**
```bash
sudo mkdir -p /opt/maxiofs
sudo mkdir -p /var/lib/maxiofs
```

**2. Install binary:**
```bash
sudo cp maxiofs /opt/maxiofs/
```

**3. Create system user:**
```bash
sudo useradd -r -s /bin/false maxiofs
sudo chown -R maxiofs:maxiofs /var/lib/maxiofs
```

**4. Create service file:**

Create `/etc/systemd/system/maxiofs.service`:

```ini
[Unit]
Description=MaxIOFS Object Storage
After=network.target

[Service]
Type=simple
User=maxiofs
Group=maxiofs
WorkingDirectory=/opt/maxiofs
ExecStart=/opt/maxiofs/maxiofs --data-dir /var/lib/maxiofs --log-level info
Restart=on-failure
RestartSec=5s

# Security
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/maxiofs

[Install]
WantedBy=multi-user.target
```

**5. Enable and start:**
```bash
sudo systemctl daemon-reload
sudo systemctl enable maxiofs
sudo systemctl start maxiofs
sudo systemctl status maxiofs
```

### Managing the Service

```bash
# Start
sudo systemctl start maxiofs

# Stop
sudo systemctl stop maxiofs

# Restart
sudo systemctl restart maxiofs

# View logs
sudo journalctl -u maxiofs -f
```

---

## Reverse Proxy with Nginx

For HTTPS and additional security, use Nginx as a reverse proxy.

### Installation

```bash
# Ubuntu/Debian
sudo apt install nginx

# CentOS/RHEL
sudo yum install nginx
```

### Configuration

Create `/etc/nginx/sites-available/maxiofs`:

```nginx
# S3 API
server {
    listen 80;
    server_name s3.yourdomain.com;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Large file support
        client_max_body_size 0;
        proxy_request_buffering off;
        proxy_connect_timeout 300s;
        proxy_send_timeout 300s;
        proxy_read_timeout 300s;
    }
}

# Web Console
server {
    listen 80;
    server_name console.yourdomain.com;

    location / {
        proxy_pass http://localhost:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

**Enable configuration:**
```bash
sudo ln -s /etc/nginx/sites-available/maxiofs /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl reload nginx
```

### Adding HTTPS with Let's Encrypt

```bash
# Install certbot
sudo apt install certbot python3-certbot-nginx

# Obtain certificates
sudo certbot --nginx -d s3.yourdomain.com -d console.yourdomain.com
```

Certbot will automatically configure HTTPS.

---

## Basic Troubleshooting

### Service Won't Start

```bash
# Check logs
sudo journalctl -u maxiofs -n 50

# Check ports
sudo netstat -tlnp | grep -E '8080|8081'

# Check permissions
ls -la /var/lib/maxiofs
```

### Cannot Access Web Console

```bash
# Verify service is running
sudo systemctl status maxiofs

# Check firewall
sudo ufw status
sudo ufw allow 8080
sudo ufw allow 8081
```

### Docker Container Issues

```bash
# Check logs
docker logs maxiofs

# Check status
docker ps -a | grep maxiofs

# Restart
docker restart maxiofs
```

### Login Issues

```bash
# Default credentials
# Console: admin/admin
# S3 API: maxioadmin/maxioadmin

# To reset (WARNING: deletes all data)
sudo systemctl stop maxiofs
sudo rm /var/lib/maxiofs/maxiofs.db
sudo systemctl start maxiofs
```

---

## Security Recommendations

1. **Change default credentials** immediately
2. **Use HTTPS** via reverse proxy
3. **Configure firewall** rules
4. **Secure data directory** permissions (750 or 700)
5. **Regular backups** of data directory
6. **Don't expose directly** to internet

### Basic Backup Script

```bash
#!/bin/bash
BACKUP_DIR="/backup/maxiofs"
DATA_DIR="/var/lib/maxiofs"
DATE=$(date +%Y%m%d_%H%M%S)

mkdir -p $BACKUP_DIR
tar -czf $BACKUP_DIR/maxiofs_$DATE.tar.gz -C $DATA_DIR .

# Keep last 7 backups
ls -t $BACKUP_DIR/maxiofs_*.tar.gz | tail -n +8 | xargs rm -f
```

---

## Alpha Software Notice

**MaxIOFS is currently in alpha development.**

**This means:**
- ❌ Not production-ready
- ❌ APIs may change
- ❌ Limited testing at scale
- ❌ Potential data loss risk
- ❌ No guarantees or SLA

**Current Limitations:**
- Single-instance only (no clustering)
- Limited S3 API compatibility
- No built-in monitoring
- Basic authentication only
- SQLite database (not for high concurrency)
- No data replication

**Recommended Use Cases:**
- Development and testing
- Learning S3 APIs
- Proof-of-concept

**Not Recommended For:**
- Production workloads
- Critical data storage
- High-availability requirements
- High-concurrency scenarios

**Always maintain backups** and test thoroughly.

---

**Version**: 0.2.0-dev
**Last Updated**: October 2025
