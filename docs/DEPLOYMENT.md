# MaxIOFS Deployment Guide

**Version**: 0.6.0-beta

## Overview

MaxIOFS is an S3-compatible object storage system currently in **beta development** with validated stress testing and production bug fixes. This guide covers deployment methods suitable for testing, development, and staging environments.

**Testing Status**: Successfully validated with MinIO Warp (7000+ objects, bulk operations working correctly). Cross-platform support for Windows, Linux (x64/ARM64), and macOS.

**Default Credentials:**
- Web Console: `admin` / `admin` - **⚠️ Change password after first login**
- S3 API: **No default access keys** - Create them via web console for security

**🔒 Security Note**: Access keys must be created manually through the web console after login.

---

## System Requirements

### Minimum Requirements
- CPU: 2 cores
- RAM: 2 GB
- Storage: 10 GB
- OS: Linux, Windows, or macOS

### Software Requirements
- Go 1.24+ (required for building)
- Node.js 23+ (required for building)
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

MaxIOFS includes complete Docker support with optional monitoring stack (Prometheus + Grafana).

### Quick Start with Docker Compose

**Option 1: Basic deployment**
```bash
make docker-build    # Build the Docker image
make docker-up       # Start with docker-compose
```

**Option 2: With monitoring (Prometheus + Grafana)**
```bash
make docker-build       # Build the Docker image
make docker-monitoring  # Start with monitoring stack
```

**Other commands:**
```bash
make docker-down     # Stop all services
make docker-logs     # View logs (Ctrl+C to exit)
make docker-clean    # Clean up volumes and containers
```

**Access:**
- **Web Console**: http://localhost:8081 (admin/admin)
- **S3 API**: http://localhost:8080
- **Prometheus**: http://localhost:9091 (only with monitoring profile)
- **Grafana**: http://localhost:3000 (admin/admin, only with monitoring profile)
  - Pre-configured MaxIOFS dashboard included
  - Real-time metrics visualization
  - API requests, storage usage, error rates, latency tracking

### Windows PowerShell Scripts

For advanced Docker operations on Windows, use the PowerShell script targets:

```powershell
make docker-build-ps        # Build with docker-build.ps1
make docker-run-ps          # Build and start
make docker-up-ps           # Start existing containers
make docker-down-ps         # Stop containers
make docker-monitoring-ps   # Start with monitoring
make docker-clean-ps        # Clean with PowerShell script
```

### Manual Docker Commands

**Pull and run:**
```bash
docker run -d \
  --name maxiofs \
  -p 8080:8080 \
  -p 8081:8081 \
  -v $(pwd)/data:/data \
  maxiofs:latest
```

### Docker Compose File

The project includes a complete `docker-compose.yaml` with:
- Multi-stage build (Node.js + Go + Alpine)
- Optional monitoring profile (Prometheus + Grafana)
- **Pre-configured Grafana dashboards** for MaxIOFS monitoring
  - Dashboard automatically provisioned on startup
  - Located in `docker/grafana/dashboards/maxiofs.json`
  - Includes panels for: API requests, storage usage, error rates, latency percentiles
- Volume persistence for data

**Basic deployment:**
```bash
docker-compose up -d
```

**With monitoring:**
```bash
docker-compose --profile monitoring up -d
```

**Stop:**
```bash
docker-compose down
```

### Environment Variables

Available in docker-compose.yaml:

```yaml
environment:
  - MAXIOFS_DATA_DIR=/data
  - MAXIOFS_LOG_LEVEL=info
  - MAXIOFS_LISTEN=:8080
  - MAXIOFS_CONSOLE_LISTEN=:8081
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

## Multi-Node Cluster Deployment (v0.6.0-beta)

MaxIOFS supports multi-node clustering for high availability and horizontal scaling. This section covers deploying a production cluster.

### Cluster Architecture

```
                 ┌──────────────────┐
                 │  Load Balancer   │
                 │  (HAProxy/Nginx) │
                 └────────┬─────────┘
                          │
        ┌─────────────────┼─────────────────┐
        │                 │                 │
        ▼                 ▼                 ▼
    Node 1            Node 2            Node 3
  10.0.1.10         10.0.1.20         10.0.1.30
  (Primary)         (Secondary)       (Secondary)
```

### Prerequisites

- 3+ Linux servers (recommended)
- Network connectivity between all nodes
- Load balancer (HAProxy or Nginx)
- Synchronized system time (NTP)

### Step 1: Deploy MaxIOFS on All Nodes

On each node, install MaxIOFS using systemd:

```bash
# Node 1 (10.0.1.10)
sudo mkdir -p /opt/maxiofs /var/lib/maxiofs
sudo cp maxiofs /opt/maxiofs/
sudo useradd -r -s /bin/false maxiofs
sudo chown -R maxiofs:maxiofs /var/lib/maxiofs

# Create systemd service
sudo cat > /etc/systemd/system/maxiofs.service <<EOF
[Unit]
Description=MaxIOFS Object Storage - Node 1
After=network.target

[Service]
Type=simple
User=maxiofs
Group=maxiofs
WorkingDirectory=/opt/maxiofs
ExecStart=/opt/maxiofs/maxiofs --data-dir /var/lib/maxiofs --log-level info
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable maxiofs
sudo systemctl start maxiofs
```

Repeat for Node 2 (10.0.1.20) and Node 3 (10.0.1.30).

### Step 2: Initialize Cluster on Primary Node

On Node 1 (primary), initialize the cluster:

```bash
# Login to web console at http://10.0.1.10:8081
# Navigate to Cluster → Initialize Cluster

curl -X POST http://10.0.1.10:8081/api/cluster/initialize \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "node_name": "node-east-1",
    "s3_endpoint": "http://10.0.1.10:8080",
    "console_endpoint": "http://10.0.1.10:8081",
    "region": "us-east-1",
    "datacenter": "dc-east"
  }'
```

**Save the response** - it contains the `cluster_id` and `node_token` (needed for authentication).

### Step 3: Join Secondary Nodes to Cluster

On Node 1, add Node 2 and Node 3:

```bash
# Add Node 2
curl -X POST http://10.0.1.10:8081/api/cluster/nodes \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "node_name": "node-east-2",
    "s3_endpoint": "http://10.0.1.20:8080",
    "console_endpoint": "http://10.0.1.20:8081",
    "region": "us-east-1",
    "datacenter": "dc-east"
  }'

# Add Node 3
curl -X POST http://10.0.1.10:8081/api/cluster/nodes \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "node_name": "node-east-3",
    "s3_endpoint": "http://10.0.1.30:8080",
    "console_endpoint": "http://10.0.1.30:8081",
    "region": "us-east-1",
    "datacenter": "dc-east"
  }'
```

### Step 4: Configure Load Balancer

**Option A: HAProxy Configuration**

Create `/etc/haproxy/haproxy.cfg`:

```haproxy
global
    log /dev/log local0
    maxconn 4096

defaults
    log global
    mode http
    option httplog
    option dontlognull
    timeout connect 10s
    timeout client 300s
    timeout server 300s

# S3 API Load Balancer
frontend s3_frontend
    bind *:8080
    default_backend s3_backend

backend s3_backend
    mode http
    balance roundrobin
    option httpchk GET /health
    http-check expect status 200
    server node1 10.0.1.10:8080 check inter 10s fall 3 rise 2
    server node2 10.0.1.20:8080 check inter 10s fall 3 rise 2
    server node3 10.0.1.30:8080 check inter 10s fall 3 rise 2

# Web Console Load Balancer
frontend console_frontend
    bind *:8081
    default_backend console_backend

backend console_backend
    mode http
    balance roundrobin
    option httpchk GET /health
    http-check expect status 200
    server node1 10.0.1.10:8081 check inter 10s fall 3 rise 2
    server node2 10.0.1.20:8081 check inter 10s fall 3 rise 2
    server node3 10.0.1.30:8081 check inter 10s fall 3 rise 2
```

Start HAProxy:
```bash
sudo systemctl enable haproxy
sudo systemctl start haproxy
sudo systemctl status haproxy
```

**Option B: Nginx Load Balancer**

Create `/etc/nginx/nginx.conf`:

```nginx
http {
    upstream s3_backend {
        least_conn;
        server 10.0.1.10:8080 max_fails=3 fail_timeout=30s;
        server 10.0.1.20:8080 max_fails=3 fail_timeout=30s;
        server 10.0.1.30:8080 max_fails=3 fail_timeout=30s;
    }

    upstream console_backend {
        least_conn;
        server 10.0.1.10:8081 max_fails=3 fail_timeout=30s;
        server 10.0.1.20:8081 max_fails=3 fail_timeout=30s;
        server 10.0.1.30:8081 max_fails=3 fail_timeout=30s;
    }

    # S3 API
    server {
        listen 8080;
        location / {
            proxy_pass http://s3_backend;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
            client_max_body_size 0;
            proxy_request_buffering off;
        }
    }

    # Web Console
    server {
        listen 8081;
        location / {
            proxy_pass http://console_backend;
            proxy_set_header Host $host;
            proxy_set_header X-Real-IP $remote_addr;
        }
    }
}
```

### Step 5: Configure Cluster Replication

Set up automatic bucket replication for high availability:

```bash
# Navigate to Cluster → Replication in web console
# Or use API:

curl -X POST http://load-balancer:8081/api/cluster/replication/rules \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "source_bucket": "backups",
    "destination_node_id": "node-i9j0k1l2",
    "sync_interval_seconds": 30,
    "enabled": true,
    "replicate_deletes": true
  }'
```

**Recommended sync intervals:**
- Real-time HA: 10-30 seconds
- Near real-time: 60-300 seconds (1-5 minutes)
- Periodic backup: 3600+ seconds (1+ hours)

### Step 6: Verify Cluster Health

```bash
# Check cluster status
curl http://load-balancer:8081/api/cluster/status \
  -H "Authorization: Bearer $TOKEN"

# Check individual node health
curl http://load-balancer:8081/api/cluster/health/summary \
  -H "Authorization: Bearer $TOKEN"
```

### Production Cluster Recommendations

1. **Use odd number of nodes** (3, 5, 7) for better fault tolerance
2. **Deploy across multiple availability zones** for geographic redundancy
3. **Use dedicated load balancer** (not running on cluster nodes)
4. **Enable HTTPS** on load balancer with valid certificates
5. **Configure monitoring** (Prometheus + Grafana)
6. **Set up automated backups** of cluster database (cluster.db)
7. **Use consistent time synchronization** (NTP) across all nodes
8. **Monitor replication lag** to ensure data consistency

### Cluster Maintenance

**Adding a new node:**
```bash
curl -X POST http://load-balancer:8081/api/cluster/nodes \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"node_name": "node-4", ...}'
```

**Removing a node:**
```bash
curl -X DELETE http://load-balancer:8081/api/cluster/nodes/{node-id} \
  -H "Authorization: Bearer $TOKEN"
```

**Update load balancer** configuration after adding/removing nodes.

### Cluster Troubleshooting

**Node appears unhealthy:**
```bash
# Check node health manually
curl -X POST http://load-balancer:8081/api/cluster/nodes/{node-id}/health \
  -H "Authorization: Bearer $TOKEN"

# Check health history
curl http://load-balancer:8081/api/cluster/health/history \
  -H "Authorization: Bearer $TOKEN"
```

**Replication not working:**
```bash
# Check replication rules
curl http://load-balancer:8081/api/cluster/replication/rules \
  -H "Authorization: Bearer $TOKEN"

# Manually trigger sync
curl -X POST http://load-balancer:8081/api/cluster/replication/sync \
  -H "Authorization: Bearer $TOKEN"
```

**Clear bucket location cache** if routing issues occur:
```bash
curl -X DELETE http://load-balancer:8081/api/cluster/cache \
  -H "Authorization: Bearer $TOKEN"
```

> **See [CLUSTER.md](CLUSTER.md) for complete cluster documentation**

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
# Console: admin/admin (change password after first login)
# S3 API: Create access keys via web console

# To reset admin password (WARNING: deletes auth database)
sudo systemctl stop maxiofs
sudo rm -f /var/lib/maxiofs/auth/auth.db
sudo systemctl start maxiofs
# Admin user will be recreated with default password
```

---

## Security Recommendations

1. **Change default credentials** immediately
2. **Enable 2FA** for admin accounts
3. **Use HTTPS** via reverse proxy
4. **Configure firewall** rules
5. **Secure data directory** permissions (750 or 700)
6. **Regular backups** of data directory
7. **Don't expose directly** to internet
8. **Monitor with Prometheus/Grafana** (use monitoring profile)

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

## Beta Software Notice

**MaxIOFS is currently in beta development.**

**This means:**
- ✅ Core S3 functionality validated
- ✅ Production bug fixes implemented
- ✅ Cross-platform support (Windows, Linux x64/ARM64, macOS)
- ✅ Debian packaging available
- ⚠️ Suitable for staging and testing environments
- ⚠️ Production use requires extensive testing
- ⚠️ Limited testing at high scale (100+ concurrent users)
- ⚠️ No official SLA or support guarantees

**Current Limitations:**
- Filesystem backend only (cloud storage backends planned)
- SQLite database (not optimized for extreme concurrency)

**Recommended Use Cases:**
- Development and testing
- Staging environments
- Internal file storage
- Backup storage (with external redundancy)
- Learning S3 APIs

**Not Recommended For:**
- Mission-critical production workloads (without thorough testing)
- High-availability requirements
- Extreme high-concurrency scenarios (1000+ concurrent users)

**Always maintain backups** and test thoroughly in your environment before production use.

---

**Version**: 0.6.0-beta
**Last Updated**: December 9, 2025
