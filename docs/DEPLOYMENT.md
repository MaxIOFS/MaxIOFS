# MaxIOFS Production Deployment Guide

**Version**: 0.9.0-beta
**Last Updated**: January 16, 2026

> **BETA SOFTWARE**: Suitable for development, testing, and staging environments. Production use requires extensive testing in your environment.

---

## Overview

This guide covers production deployment scenarios for MaxIOFS:

1. **Standalone Binary** - Direct deployment on Linux/Windows
2. **Docker** - Containerized deployment with Docker Compose
3. **Systemd Service** - Linux service management
4. **Reverse Proxy** - Nginx/HAProxy with HTTPS
5. **Multi-Node Cluster** - High availability setup

---

## System Requirements

### Minimum Requirements

| Component | Minimum | Recommended |
|-----------|---------|-------------|
| CPU | 2 cores | 4+ cores |
| RAM | 2 GB | 8 GB |
| Storage | 10 GB | 100 GB+ SSD |
| Network | 100 Mbps | 1 Gbps |
| OS | Linux/Windows x64 | Linux x64 |

### Software Requirements

- **Go**: 1.24+ (for building from source)
- **Node.js**: 23+ (for building from source)
- **Docker**: 20.10+ (for Docker deployment)
- **Reverse Proxy**: Nginx 1.18+ or HAProxy 2.0+

---

## Standalone Binary Deployment

### Building from Source

```bash
# Clone repository
git clone https://github.com/yourusername/maxiofs.git
cd maxiofs

# Build
make build

# Binary location: ./build/maxiofs
```

### Running MaxIOFS

**Production configuration:**

```bash
# Create directories
sudo mkdir -p /var/lib/maxiofs/data
sudo mkdir -p /etc/maxiofs/keys

# Generate encryption key
sudo openssl rand -hex 32 > /etc/maxiofs/keys/master_key.key
sudo chmod 400 /etc/maxiofs/keys/master_key.key

# Create config file
sudo tee /etc/maxiofs/config.yaml > /dev/null <<EOF
data_dir: /var/lib/maxiofs/data
listen: 127.0.0.1:8080
console_listen: 127.0.0.1:8081
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
  retention_days: 180
EOF

# Run
sudo ./maxiofs --config /etc/maxiofs/config.yaml
```

### Accessing the Application

- **Web Console**: http://localhost:8081 (or configured console URL)
- **S3 API**: http://localhost:8080 (or configured S3 URL)
- **Default credentials**: admin/admin (⚠️ **CHANGE IMMEDIATELY**)

---

## Docker Deployment

**Complete Docker guide**: See [DOCKER.md](../DOCKER.md)

### Quick Start

```bash
# Clone repository
git clone https://github.com/yourusername/maxiofs.git
cd maxiofs

# Build and start
make docker-build
make docker-up

# With monitoring (Prometheus + Grafana)
make docker-monitoring

# 3-node cluster
make docker-cluster
```

**Access:**
- Web Console: http://localhost:8081
- S3 API: http://localhost:8080
- Prometheus: http://localhost:9091 (monitoring profile)
- Grafana: http://localhost:3000 (monitoring profile)

### Docker Compose Example

```yaml
version: '3.8'

services:
  maxiofs:
    image: maxiofs:latest
    container_name: maxiofs
    restart: unless-stopped
    environment:
      MAXIOFS_DATA_DIR: /data
      MAXIOFS_LISTEN: :8080
      MAXIOFS_CONSOLE_LISTEN: :8081
      MAXIOFS_PUBLIC_URL: https://s3.example.com
      MAXIOFS_CONSOLE_PUBLIC_URL: https://console.example.com
      MAXIOFS_LOG_LEVEL: info
    volumes:
      - ./data:/data
      - ./keys:/keys:ro
    ports:
      - "8080:8080"
      - "8081:8081"
```

---

## Systemd Service (Linux)

### Installation

```bash
# Create user
sudo useradd -r -s /bin/false maxiofs

# Set permissions
sudo chown -R maxiofs:maxiofs /var/lib/maxiofs
sudo chown -R maxiofs:maxiofs /etc/maxiofs

# Create systemd service
sudo tee /etc/systemd/system/maxiofs.service > /dev/null <<EOF
[Unit]
Description=MaxIOFS S3-Compatible Object Storage
After=network.target

[Service]
Type=simple
User=maxiofs
Group=maxiofs
ExecStart=/usr/local/bin/maxiofs --config /etc/maxiofs/config.yaml
Restart=on-failure
RestartSec=5s

# Security
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/maxiofs

# Limits
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

# Reload and enable
sudo systemctl daemon-reload
sudo systemctl enable maxiofs
sudo systemctl start maxiofs
```

### Managing the Service

```bash
# Status
sudo systemctl status maxiofs

# Logs
sudo journalctl -u maxiofs -f

# Restart
sudo systemctl restart maxiofs

# Stop
sudo systemctl stop maxiofs
```

---

## Reverse Proxy with Nginx

**Recommended for production** - provides HTTPS, load balancing, and caching.

### Nginx Configuration

```nginx
# /etc/nginx/sites-available/maxiofs

# S3 API
server {
    listen 80;
    server_name s3.example.com;

    # Redirect to HTTPS
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name s3.example.com;

    # TLS certificates
    ssl_certificate /etc/letsencrypt/live/s3.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/s3.example.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;

    # Proxy to MaxIOFS S3 API
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Large file uploads
        client_max_body_size 5G;
        proxy_request_buffering off;
    }
}

# Web Console
server {
    listen 80;
    server_name console.example.com;
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name console.example.com;

    ssl_certificate /etc/letsencrypt/live/console.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/console.example.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    location / {
        proxy_pass http://127.0.0.1:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### Enable Configuration

```bash
# Enable site
sudo ln -s /etc/nginx/sites-available/maxiofs /etc/nginx/sites-enabled/

# Test configuration
sudo nginx -t

# Reload Nginx
sudo systemctl reload nginx
```

### HTTPS with Let's Encrypt

```bash
# Install Certbot
sudo apt install certbot python3-certbot-nginx

# Obtain certificates
sudo certbot --nginx -d s3.example.com -d console.example.com

# Auto-renewal (cron job created automatically)
sudo certbot renew --dry-run
```

---

## Multi-Node Cluster Deployment

**Complete cluster guide**: See [CLUSTER.md](CLUSTER.md)

### Quick Cluster Setup

**Prerequisites:**
- 2+ servers with network connectivity
- Same MaxIOFS version on all nodes
- Load balancer (HAProxy/Nginx)

**Steps:**

1. **Deploy MaxIOFS on all nodes** (see Standalone/Docker deployment above)

2. **Initialize cluster on Node 1:**
   ```bash
   # Via Web Console: Cluster page → Initialize Cluster
   # Node Name: node-1
   # Region: us-east-1
   # Copy the generated cluster token
   ```

3. **Join Node 2 to cluster:**
   ```bash
   # Via Web Console: Cluster page → Add Node
   # Node Name: node-2
   # Endpoint: http://node1:8080
   # Node Token: <paste token from step 2>
   ```

4. **Configure load balancer** (HAProxy example):
   ```haproxy
   frontend s3_frontend
       bind *:8080
       default_backend s3_backend

   backend s3_backend
       balance roundrobin
       option httpchk GET /health
       server node1 10.0.1.10:8080 check
       server node2 10.0.1.20:8080 check
   ```

5. **Configure replication** (optional for HA):
   ```bash
   # Via Web Console: Cluster → Bucket Replication
   # Select bucket → Configure Replication
   # Destination: node-2
   # Sync interval: 60 seconds
   ```

6. **Verify cluster health:**
   ```bash
   # Check cluster status on Web Console
   # All nodes should show as "Healthy"
   ```

**Production recommendations:**
- Use 3+ nodes for fault tolerance
- Configure bucket replication for critical data
- Monitor cluster health (Prometheus/Grafana)
- Use dedicated network for cluster communication
- Enable TLS for inter-node communication

---

## Basic Troubleshooting

### Service Won't Start

**Check logs:**
```bash
# Systemd
sudo journalctl -u maxiofs -n 50

# Docker
docker logs maxiofs

# Standalone
./maxiofs --log-level debug
```

**Common issues:**
- Port already in use (check with `netstat -tlnp | grep 8080`)
- Permission denied (check file permissions on data directory)
- Invalid config file (verify YAML syntax)

### Cannot Access Web Console

**Verify service is running:**
```bash
# Check process
ps aux | grep maxiofs

# Test endpoint
curl http://localhost:8081/health
```

**Firewall check:**
```bash
# Allow ports
sudo ufw allow 8080/tcp
sudo ufw allow 8081/tcp
```

### Docker Container Issues

```bash
# Check container status
docker ps -a

# View logs
docker logs maxiofs

# Restart container
docker restart maxiofs

# Rebuild
docker-compose down
docker-compose up -d --build
```

### Login Issues

**Reset admin password:**
```bash
# Stop MaxIOFS
sudo systemctl stop maxiofs

# Reset via database (requires manual intervention)
# Or create new admin user via database
```

**Check credentials:**
- Default: admin/admin
- Verify in Web Console user management

---

## Security Recommendations

### Essential Security Measures

1. **Change default credentials** immediately
2. **Use HTTPS** in production (Let's Encrypt or commercial certificate)
3. **Configure firewall rules** to restrict access
4. **Run as non-root user** (systemd configuration above includes this)
5. **Enable server-side encryption** for data at rest
6. **Configure rate limiting** to prevent brute force attacks
7. **Enable audit logging** for compliance
8. **Regular backups** of data and configuration
9. **Monitor logs** for suspicious activity
10. **Keep software updated** with security patches

**See [SECURITY.md](SECURITY.md) for complete security guide**

### Basic Backup Strategy

```bash
#!/bin/bash
# /usr/local/bin/maxiofs-backup.sh

BACKUP_DIR="/backup/maxiofs"
DATA_DIR="/var/lib/maxiofs/data"
DATE=$(date +%Y%m%d_%H%M%S)

# Stop service (optional, for consistency)
sudo systemctl stop maxiofs

# Backup data
sudo tar -czf "$BACKUP_DIR/maxiofs-data-$DATE.tar.gz" "$DATA_DIR"

# Backup encryption key
sudo tar -czf "$BACKUP_DIR/maxiofs-keys-$DATE.tar.gz" /etc/maxiofs/keys

# Start service
sudo systemctl start maxiofs

# Retention (keep last 7 days)
find "$BACKUP_DIR" -name "maxiofs-*.tar.gz" -mtime +7 -delete
```

**Schedule backup:**
```bash
# Cron job (daily at 2 AM)
0 2 * * * /usr/local/bin/maxiofs-backup.sh
```

---

## Performance Tuning

### File Descriptor Limits

```bash
# Increase system limits
sudo tee -a /etc/security/limits.conf > /dev/null <<EOF
maxiofs soft nofile 65536
maxiofs hard nofile 65536
EOF

# For systemd service (already included in service file above)
LimitNOFILE=65536
```

### Nginx Tuning

```nginx
# Worker processes
worker_processes auto;

# Worker connections
events {
    worker_connections 4096;
}

# Client body buffer
client_body_buffer_size 128k;
client_max_body_size 5G;

# Timeouts
keepalive_timeout 65;
send_timeout 300;
```

---

## Monitoring

**Prometheus metrics available at**: `http://localhost:8081/api/metrics`

**Grafana dashboard**: Pre-configured dashboard included in Docker monitoring stack

```bash
# Start with monitoring
make docker-monitoring

# Access Grafana: http://localhost:3000 (admin/admin)
```

**Key metrics:**
- Request latency (p50, p95, p99)
- Throughput (requests/second)
- Storage usage
- Cluster health
- Replication lag

**See [PERFORMANCE.md](PERFORMANCE.md) for benchmarks and tuning**

---

## Beta Software Notice

**Current Status**: Beta phase (v0.8.0-beta)

**Suitable for:**
- Development environments
- Testing and staging
- Internal deployments with extensive monitoring

**NOT recommended for:**
- Mission-critical production without thorough testing
- Environments requiring certified security audits
- High-compliance regulated industries (without additional validation)

**Before production deployment:**
1. Conduct thorough testing in staging environment
2. Validate backup and restore procedures
3. Perform security assessment
4. Monitor closely for first 30 days
5. Have rollback plan ready

---

**Version**: 0.9.0-beta
**Last Updated**: January 16, 2026

For additional deployment information, see:
- [DOCKER.md](../DOCKER.md) - Complete Docker deployment guide
- [CLUSTER.md](CLUSTER.md) - Multi-node cluster setup
- [SECURITY.md](SECURITY.md) - Security best practices
- [CONFIGURATION.md](CONFIGURATION.md) - Configuration reference
