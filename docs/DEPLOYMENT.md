# MaxIOFS Deployment Guide

**Version**: 1.5.1 | **Last Updated**: July 18, 2026

> Suitable for production use — always backup your data and change default credentials before deploying.

## System Requirements

| Component | Minimum | Recommended |
|-----------|---------|-------------|
| CPU | 2 cores | 4+ cores |
| RAM | 2 GB | 8 GB |
| Storage | 10 GB | 100 GB+ SSD |
| Network | 100 Mbps | 1 Gbps |
| OS | Linux x64, Windows x64, macOS | Linux x64 |

### Build Requirements (source only)

- **Go** 1.26+
- **Node.js** 24+ (for frontend build)

The compiled binary is self-contained and requires neither Go nor Node at runtime.

---

## Deployment Options

### 1. Standalone Binary

```bash
# Build from source
git clone https://github.com/MaxioFS/MaxioFS.git
cd MaxIOFS
make build
# Binary: ./build/maxiofs

# Or use pre-built binary
chmod +x maxiofs
./maxiofs --data-dir /var/lib/maxiofs
```

**Access:**
- Web Console: http://localhost:8081
- S3 API: http://localhost:8080
- Cluster inter-node: :8082 (TLS; only listens once the node joins/initializes a cluster — never expose it publicly)
- Default credentials: **admin / admin** (⚠️ change immediately)

### 2. Docker

```bash
# Build and start
make docker-build
make docker-up

# With monitoring (Prometheus + Grafana)
make docker-monitoring
```

**Docker Compose:**

```yaml
version: '3.8'
services:
  maxiofs:
    image: maxiofs:latest
    restart: unless-stopped
    environment:
      MAXIOFS_DATA_DIR: /data
      # Encryption at rest is always on — the key is generated automatically
      # and stored in the database. After the first start, download the
      # recovery bundle from Settings → Security and keep it off this host.
    volumes:
      - maxiofs-data:/data
    ports:
      - "8080:8080"
      - "8081:8081"
      # Cluster deployments only — inter-node port (restrict to node IPs):
      # - "8082:8082"

volumes:
  maxiofs-data:
```

See [DOCKER.md](../DOCKER.md) for complete Docker documentation.

### 3. Systemd Service (Linux)

```bash
# Create user and directories
sudo useradd -r -s /bin/false maxiofs
sudo mkdir -p /var/lib/maxiofs /etc/maxiofs
sudo chown -R maxiofs:maxiofs /var/lib/maxiofs

# Create config
sudo tee /etc/maxiofs/config.yaml > /dev/null <<EOF
data_dir: /var/lib/maxiofs
listen: 127.0.0.1:8080
console_listen: 127.0.0.1:8081
public_api_url: https://s3.example.com
public_console_url: https://console.example.com
# cluster_listen: :8082    # Inter-node port (cluster deployments; firewall to node IPs)
log_level: info
# Encryption at rest is always on — the key is generated automatically and
# stored in the database. After the first start, download the recovery
# bundle from Settings → Security and keep it OUTSIDE this server.
EOF

# Install binary
sudo cp maxiofs /usr/local/bin/
sudo chmod 755 /usr/local/bin/maxiofs

# Create service
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
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/maxiofs
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

# Enable and start
sudo systemctl daemon-reload
sudo systemctl enable --now maxiofs
```

**Managing:**

```bash
sudo systemctl status maxiofs     # Check status
sudo journalctl -u maxiofs -f     # Follow logs
sudo systemctl restart maxiofs    # Restart
```

### 4. DEB/RPM Package

Pre-built packages are available for Debian/Ubuntu and RHEL/Rocky:

```bash
# Debian/Ubuntu
sudo dpkg -i maxiofs_1.5.1_amd64.deb
sudo systemctl enable --now maxiofs

# RHEL/Rocky
sudo rpm -i maxiofs-1.5.1-1.x86_64.rpm
sudo systemctl enable --now maxiofs
```

---

## Reverse Proxy (Recommended for Production)

### Nginx

```nginx
# /etc/nginx/sites-available/maxiofs

# S3 API
server {
    listen 443 ssl http2;
    server_name s3.example.com;

    ssl_certificate /etc/letsencrypt/live/s3.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/s3.example.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        client_max_body_size 5G;
        proxy_request_buffering off;
    }
}

# Web Console
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

# HTTP → HTTPS redirect
server {
    listen 80;
    server_name s3.example.com console.example.com;
    return 301 https://$server_name$request_uri;
}
```

```bash
sudo ln -s /etc/nginx/sites-available/maxiofs /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
```

### HAProxy

```haproxy
frontend s3_frontend
    bind *:8080
    default_backend s3_backend

backend s3_backend
    balance roundrobin
    option httpchk GET /health
    server node1 10.0.1.10:8080 check inter 10s fall 3 rise 2
    server node2 10.0.1.20:8080 check inter 10s fall 3 rise 2

frontend console_frontend
    bind *:8081
    default_backend console_backend

backend console_backend
    balance roundrobin
    option httpchk GET /health
    server node1 10.0.1.10:8081 check inter 10s fall 3 rise 2
    server node2 10.0.1.20:8081 check inter 10s fall 3 rise 2
```

### Let's Encrypt

```bash
sudo apt install certbot python3-certbot-nginx
sudo certbot --nginx -d s3.example.com -d console.example.com
```

---

## Multi-Node Cluster

See [CLUSTER.md](CLUSTER.md) for complete documentation. Quick overview:

1. **Open port 8082 between nodes** (inter-node TLS traffic) — restricted to cluster node IPs, never behind the client load balancer
2. **Initialize cluster** on Node 1 (Web Console → Cluster → Initialize)
3. **Join nodes** using the cluster token
4. **Configure replication** for HA buckets
5. **Set up load balancer** (HAProxy/Nginx) in front of all nodes — ports 8080/8081 only

---

## Monitoring

### Prometheus + Grafana

MaxIOFS exposes metrics at `/metrics` on both ports (8080 and 8081):

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'maxiofs'
    static_configs:
      - targets: ['localhost:8080']
```

Pre-built Grafana dashboards are included in `docker/grafana/dashboards/`.

### Health Checks

```bash
curl http://localhost:8080/health    # S3 API health
curl http://localhost:8081/health    # Console health
```

---

## Backup Strategy

```bash
#!/bin/bash
BACKUP_DIR="/backup/maxiofs"
DATA_DIR="/var/lib/maxiofs"
DATE=$(date +%Y%m%d_%H%M%S)

# Hot backup (service running)
sudo -u maxiofs sqlite3 "$DATA_DIR/db/maxiofs.db" ".backup '$BACKUP_DIR/maxiofs-$DATE.db'"
sudo -u maxiofs sqlite3 "$DATA_DIR/audit.db" ".backup '$BACKUP_DIR/audit-$DATE.db'"
tar -czf "$BACKUP_DIR/objects-$DATE.tar.gz" "$DATA_DIR/objects"
tar -czf "$BACKUP_DIR/metadata-$DATE.tar.gz" "$DATA_DIR/metadata"

# Retention (7 days)
find "$BACKUP_DIR" -name "*.tar.gz" -mtime +7 -delete
find "$BACKUP_DIR" -name "*.db" -mtime +7 -delete
```

---

## Troubleshooting

### Service Won't Start

```bash
# Check logs
sudo journalctl -u maxiofs -n 50
# Or run in foreground with debug
./maxiofs --data-dir /data --log-level debug

# Common issues:
# - Port already in use: netstat -tlnp | grep 8080
# - Permission denied: check ownership of data directory
# - Invalid YAML: validate config syntax
```

### Can't Access Web Console

```bash
curl http://localhost:8081/health        # Test endpoint
sudo ufw allow 8080/tcp; sudo ufw allow 8081/tcp  # Firewall
# Cluster deployments: open 8082 to cluster node IPs ONLY (inter-node TLS traffic)
# sudo ufw allow from <node-ip> to any port 8082 proto tcp
```

### Login Issues

- Default credentials: admin / admin
- Forgot password: No CLI reset tool yet — requires direct database modification
- Account locked: Admin can unlock via Web Console (Users → Unlock)

---

## Security Checklist

1. ☐ Change default admin password
2. ☐ Download the encryption recovery bundle (Settings → Security) and store it outside the server — encryption at rest is always on
3. ☐ Use HTTPS (reverse proxy or direct TLS)
4. ☐ Configure firewall rules
5. ☐ Run as non-root user (systemd service)
6. ☐ Enable audit logging
7. ☐ Configure rate limiting (dynamic settings)
8. ☐ Enable 2FA for admin accounts
9. ☐ Set up regular backups
10. ☐ Monitor logs and metrics

See [SECURITY.md](SECURITY.md) for detailed security guidance.

---

**See also**: [CONFIGURATION.md](CONFIGURATION.md) · [CLUSTER.md](CLUSTER.md) · [OPERATIONS.md](OPERATIONS.md) · [SECURITY.md](SECURITY.md) · [DOCKER.md](../DOCKER.md)
