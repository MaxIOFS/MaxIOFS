# MaxIOFS Deployment Guide

## Overview

MaxIOFS can be deployed in multiple environments, from development to production. This guide covers all deployment scenarios.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Development Deployment](#development-deployment)
- [Production Deployment](#production-deployment)
- [Docker Deployment](#docker-deployment)
- [Kubernetes Deployment](#kubernetes-deployment)
- [Reverse Proxy Setup](#reverse-proxy-setup)
- [High Availability](#high-availability)

---

## Prerequisites

### System Requirements

**Minimum:**
- CPU: 2 cores
- RAM: 2 GB
- Storage: 10 GB (for application + data)
- OS: Linux, Windows, or macOS

**Recommended Production:**
- CPU: 4+ cores
- RAM: 8+ GB
- Storage: SSD with sufficient space for objects
- OS: Ubuntu 22.04 LTS or later

### Software Requirements

- Go 1.21+ (for building from source)
- Node.js 18+ and npm (for building frontend)
- SQLite3 (embedded, no separate installation needed)

---

## Development Deployment

### Quick Start

1. **Clone the repository:**
```bash
git clone https://github.com/yourusername/maxiofs.git
cd maxiofs
```

2. **Build the application:**
```bash
# Windows
build.bat

# Linux/macOS
make build
```

3. **Run MaxIOFS:**
```bash
# Windows
./maxiofs.exe --data-dir ./data --log-level debug

# Linux/macOS
./maxiofs --data-dir ./data --log-level debug
```

4. **Access the application:**
- Web Console: http://localhost:8081
- S3 API: http://localhost:8080

**Default credentials:**
- Username: `admin`
- Password: `admin`
- S3 Access Key: `maxioadmin`
- S3 Secret Key: `maxioadmin`

### Development with Hot Reload

**Backend:**
```bash
go run ./cmd/maxiofs --data-dir ./data --log-level debug
```

**Frontend:**
```bash
cd web/frontend
npm run dev
```

Frontend will be available at http://localhost:3000

---

## Production Deployment

### 1. Build for Production

```bash
# Build with version information
VERSION="v1.1.0"
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

go build -ldflags "-X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE" \
  -o maxiofs ./cmd/maxiofs

# Build frontend
cd web/frontend
npm install
npm run build
cd ../..
```

### 2. Create Data Directory

```bash
mkdir -p /var/lib/maxiofs
chmod 750 /var/lib/maxiofs
```

### 3. Create Configuration File

Create `/etc/maxiofs/config.yaml`:

```yaml
# Server Configuration
server:
  s3_port: 8080
  console_port: 8081
  data_dir: /var/lib/maxiofs
  log_level: info

# Security
security:
  jwt_secret: "your-secure-random-secret-here"  # Generate with: openssl rand -base64 32
  session_timeout: 3600  # 1 hour

# Rate Limiting
rate_limit:
  enabled: true
  login_attempts: 5
  lockout_duration: 900  # 15 minutes

# Storage
storage:
  backend: filesystem
  path: /var/lib/maxiofs/objects

# Monitoring
monitoring:
  prometheus_enabled: true
  metrics_port: 9090
```

### 4. Create Systemd Service

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
ExecStart=/opt/maxiofs/maxiofs --config /etc/maxiofs/config.yaml
Restart=on-failure
RestartSec=5s

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/maxiofs

# Resource limits
LimitNOFILE=65536
LimitNPROC=4096

[Install]
WantedBy=multi-user.target
```

### 5. Create User and Set Permissions

```bash
# Create system user
useradd -r -s /bin/false maxiofs

# Set ownership
chown -R maxiofs:maxiofs /var/lib/maxiofs
chown -R maxiofs:maxiofs /opt/maxiofs

# Set permissions
chmod 750 /var/lib/maxiofs
chmod 750 /opt/maxiofs
chmod 600 /etc/maxiofs/config.yaml
```

### 6. Start the Service

```bash
# Enable and start
systemctl enable maxiofs
systemctl start maxiofs

# Check status
systemctl status maxiofs

# View logs
journalctl -u maxiofs -f
```

---

## Docker Deployment

### Using Docker Compose

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  maxiofs:
    image: maxiofs/maxiofs:latest
    container_name: maxiofs
    ports:
      - "8080:8080"  # S3 API
      - "8081:8081"  # Console
    volumes:
      - ./data:/data
      - ./config.yaml:/etc/maxiofs/config.yaml:ro
    environment:
      - MAXIOFS_DATA_DIR=/data
      - MAXIOFS_LOG_LEVEL=info
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8081/health"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 40s
```

Run with:
```bash
docker-compose up -d
```

### Building Docker Image

Create `Dockerfile`:

```dockerfile
# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY . .

RUN apk add --no-cache git make nodejs npm
RUN cd web/frontend && npm install && npm run build
RUN go mod download
RUN CGO_ENABLED=1 go build -o maxiofs ./cmd/maxiofs

# Runtime stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates sqlite-libs

WORKDIR /app

COPY --from=builder /app/maxiofs .
COPY --from=builder /app/web/frontend/dist ./web/frontend/dist

RUN adduser -D -s /bin/sh maxiofs && \
    mkdir -p /data && \
    chown -R maxiofs:maxiofs /data /app

USER maxiofs

EXPOSE 8080 8081

VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8081/health || exit 1

ENTRYPOINT ["./maxiofs"]
CMD ["--data-dir", "/data", "--log-level", "info"]
```

Build and run:
```bash
docker build -t maxiofs:latest .
docker run -d -p 8080:8080 -p 8081:8081 -v $(pwd)/data:/data maxiofs:latest
```

---

## Kubernetes Deployment

### Namespace and ConfigMap

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: maxiofs

---
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: maxiofs-config
  namespace: maxiofs
data:
  config.yaml: |
    server:
      s3_port: 8080
      console_port: 8081
      data_dir: /data
      log_level: info
    security:
      jwt_secret: "${JWT_SECRET}"
      session_timeout: 3600
    rate_limit:
      enabled: true
      login_attempts: 5
      lockout_duration: 900
```

### Deployment

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: maxiofs
  namespace: maxiofs
spec:
  replicas: 1
  selector:
    matchLabels:
      app: maxiofs
  template:
    metadata:
      labels:
        app: maxiofs
    spec:
      containers:
      - name: maxiofs
        image: maxiofs/maxiofs:latest
        ports:
        - containerPort: 8080
          name: s3-api
        - containerPort: 8081
          name: console
        volumeMounts:
        - name: data
          mountPath: /data
        - name: config
          mountPath: /etc/maxiofs
          readOnly: true
        env:
        - name: MAXIOFS_DATA_DIR
          value: "/data"
        - name: MAXIOFS_LOG_LEVEL
          value: "info"
        resources:
          requests:
            memory: "512Mi"
            cpu: "250m"
          limits:
            memory: "2Gi"
            cpu: "1000m"
        livenessProbe:
          httpGet:
            path: /health
            port: 8081
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health
            port: 8081
          initialDelaySeconds: 10
          periodSeconds: 5
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: maxiofs-pvc
      - name: config
        configMap:
          name: maxiofs-config
```

### Persistent Volume Claim

```yaml
# pvc.yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: maxiofs-pvc
  namespace: maxiofs
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 100Gi
  storageClassName: fast-ssd
```

### Service

```yaml
# service.yaml
apiVersion: v1
kind: Service
metadata:
  name: maxiofs
  namespace: maxiofs
spec:
  type: LoadBalancer
  ports:
  - port: 8080
    targetPort: 8080
    name: s3-api
  - port: 8081
    targetPort: 8081
    name: console
  selector:
    app: maxiofs
```

### Deploy to Kubernetes

```bash
kubectl apply -f namespace.yaml
kubectl apply -f configmap.yaml
kubectl apply -f pvc.yaml
kubectl apply -f deployment.yaml
kubectl apply -f service.yaml

# Check status
kubectl get all -n maxiofs
kubectl logs -f deployment/maxiofs -n maxiofs
```

---

## Reverse Proxy Setup

### Nginx

```nginx
# /etc/nginx/sites-available/maxiofs
upstream maxiofs_s3 {
    server localhost:8080;
}

upstream maxiofs_console {
    server localhost:8081;
}

# S3 API
server {
    listen 80;
    server_name s3.yourdomain.com;

    # Redirect to HTTPS
    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name s3.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/s3.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/s3.yourdomain.com/privkey.pem;

    # Security headers
    add_header X-Content-Type-Options nosniff;
    add_header X-Frame-Options DENY;
    add_header X-XSS-Protection "1; mode=block";

    # S3 API
    location / {
        proxy_pass http://maxiofs_s3;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Increase timeouts for large uploads
        proxy_connect_timeout 300s;
        proxy_send_timeout 300s;
        proxy_read_timeout 300s;

        # Disable buffering for large files
        proxy_request_buffering off;
        proxy_buffering off;

        client_max_body_size 0;
    }
}

# Web Console
server {
    listen 80;
    server_name console.yourdomain.com;

    return 301 https://$server_name$request_uri;
}

server {
    listen 443 ssl http2;
    server_name console.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/console.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/console.yourdomain.com/privkey.pem;

    # Security headers
    add_header X-Content-Type-Options nosniff;
    add_header X-Frame-Options SAMEORIGIN;
    add_header X-XSS-Protection "1; mode=block";
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

    location / {
        proxy_pass http://maxiofs_console;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket support (if needed)
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

Enable the site:
```bash
ln -s /etc/nginx/sites-available/maxiofs /etc/nginx/sites-enabled/
nginx -t
systemctl reload nginx
```

### Traefik

```yaml
# docker-compose.yml with Traefik
version: '3.8'

services:
  traefik:
    image: traefik:v2.10
    command:
      - "--providers.docker=true"
      - "--entrypoints.web.address=:80"
      - "--entrypoints.websecure.address=:443"
      - "--certificatesresolvers.letsencrypt.acme.email=admin@yourdomain.com"
      - "--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json"
      - "--certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web"
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./letsencrypt:/letsencrypt

  maxiofs:
    image: maxiofs/maxiofs:latest
    volumes:
      - ./data:/data
    labels:
      # S3 API
      - "traefik.http.routers.maxiofs-s3.rule=Host(`s3.yourdomain.com`)"
      - "traefik.http.routers.maxiofs-s3.entrypoints=websecure"
      - "traefik.http.routers.maxiofs-s3.tls.certresolver=letsencrypt"
      - "traefik.http.routers.maxiofs-s3.service=maxiofs-s3"
      - "traefik.http.services.maxiofs-s3.loadbalancer.server.port=8080"

      # Console
      - "traefik.http.routers.maxiofs-console.rule=Host(`console.yourdomain.com`)"
      - "traefik.http.routers.maxiofs-console.entrypoints=websecure"
      - "traefik.http.routers.maxiofs-console.tls.certresolver=letsencrypt"
      - "traefik.http.routers.maxiofs-console.service=maxiofs-console"
      - "traefik.http.services.maxiofs-console.loadbalancer.server.port=8081"
```

---

## High Availability

### Multi-Instance Deployment (Future)

MaxIOFS currently runs in single-instance mode. For high availability:

**Current Workarounds:**
1. **Active-Passive Failover:** Use keepalived or similar for failover
2. **Backup & Restore:** Regular backups of data directory and SQLite database
3. **Load Balancer:** Use HAProxy/Nginx to distribute S3 API requests

**Planned Features:**
- Distributed consensus with Raft
- Multi-node data replication
- Shared storage backend support

### Backup Strategy

```bash
#!/bin/bash
# backup.sh

BACKUP_DIR="/backup/maxiofs"
DATA_DIR="/var/lib/maxiofs"
DATE=$(date +%Y%m%d_%H%M%S)

# Stop service (optional for consistency)
systemctl stop maxiofs

# Backup SQLite database
sqlite3 $DATA_DIR/maxiofs.db ".backup '$BACKUP_DIR/maxiofs_$DATE.db'"

# Backup objects
tar -czf $BACKUP_DIR/objects_$DATE.tar.gz -C $DATA_DIR objects/

# Restart service
systemctl start maxiofs

# Cleanup old backups (keep 7 days)
find $BACKUP_DIR -name "*.db" -mtime +7 -delete
find $BACKUP_DIR -name "*.tar.gz" -mtime +7 -delete
```

Schedule with cron:
```cron
0 2 * * * /usr/local/bin/backup.sh
```

---

## Monitoring

### Prometheus Integration

MaxIOFS exposes Prometheus metrics on port 9090 (configurable).

**Prometheus config:**
```yaml
scrape_configs:
  - job_name: 'maxiofs'
    static_configs:
      - targets: ['localhost:9090']
    scrape_interval: 15s
```

### Grafana Dashboard

Import the MaxIOFS Grafana dashboard (ID: coming soon) or create custom panels:

- Storage usage over time
- Request rate and latency
- Error rates
- Active connections
- Tenant quotas

---

## Security Checklist

- [ ] Change default credentials immediately
- [ ] Use strong JWT secret (32+ characters)
- [ ] Enable HTTPS with valid certificates
- [ ] Configure rate limiting
- [ ] Set up regular backups
- [ ] Use restrictive file permissions
- [ ] Enable audit logging
- [ ] Keep MaxIOFS updated
- [ ] Use firewall rules to restrict access
- [ ] Implement WAF for additional protection

---

## Troubleshooting

### Service won't start
```bash
# Check logs
journalctl -u maxiofs -n 100 --no-pager

# Check permissions
ls -la /var/lib/maxiofs
ls -la /opt/maxiofs

# Verify configuration
/opt/maxiofs/maxiofs --config /etc/maxiofs/config.yaml --validate
```

### High memory usage
```bash
# Check current usage
ps aux | grep maxiofs

# Set memory limits in systemd
echo "MemoryMax=2G" >> /etc/systemd/system/maxiofs.service
systemctl daemon-reload
systemctl restart maxiofs
```

### Slow performance
1. Check disk I/O with `iostat`
2. Verify SSD is being used
3. Increase file descriptor limits
4. Check network latency
5. Review object size distribution

---

## Support

For issues and support:
- GitHub Issues: https://github.com/yourusername/maxiofs/issues
- Documentation: https://maxiofs.io/docs
- Community: https://discord.gg/maxiofs
