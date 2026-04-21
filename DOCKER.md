# MaxIOFS Docker Deployment Guide

## Quick Start

### docker run

```bash
docker run -d \
  --name maxiofs \
  --restart unless-stopped \
  -p 8080:8080 \
  -p 8081:8081 \
  -v /srv/maxiofs:/data \
  maxiofs/maxiofs:latest
```

- **Web Console:** http://localhost:8081 — login: `admin` / `admin`
- **S3 API:** http://localhost:8080
- **Config file:** `/srv/maxiofs/config.yaml` (created on first run)

> **Cluster deployments**: add `-p 8082:8082` to expose the cluster inter-node port. Restrict port 8082 at the firewall to cluster node IPs only — it should never be reachable from the public internet.

### Docker Compose

```bash
docker compose up -d       # start
docker compose down        # stop
docker compose logs -f     # view logs
```

The included `docker-compose.yaml` pulls `maxiofs/maxiofs:latest` automatically and mounts `./maxiofs-data` as the data directory. On first start, `./maxiofs-data/config.yaml` is created — edit it and restart to apply changes.

### With Monitoring (Prometheus + Grafana)

Requires cloning the repository (Prometheus and Grafana need local config files):

```bash
git clone https://github.com/MaxioFS/MaxioFS.git
cd MaxioFS
docker compose --profile monitoring up -d
```

Access:
- MaxIOFS Console: http://localhost:8081 (admin/admin)
- Prometheus: http://localhost:9091
- Grafana: http://localhost:3000 (admin/admin)

### Building Locally from Source

```bash
git clone https://github.com/MaxioFS/MaxioFS.git
cd MaxioFS
make docker-build      # build image
make docker-up         # start MaxIOFS
make docker-monitoring # start with Prometheus + Grafana
make docker-down       # stop all
make docker-logs       # view logs
make docker-clean      # remove containers and volumes
```

## Configuration

### config.yaml

On first run MaxIOFS creates `config.yaml` inside the data directory (`/data/config.yaml`). This file controls all static settings — encryption, SMTP, public URLs, TLS, log level.

Key settings:

```yaml
# Data directory — must match the volume mount
data_dir: "/data"

# Encryption at rest (set once — changing this key makes existing objects unreadable)
encryption:
  enabled: true
  master_key: "your-32-byte-random-key-here"

# Public URLs — used in presigned URLs and email links
public_api_url: "https://s3.yourdomain.com"
public_console_url: "https://console.yourdomain.com"

# SMTP
email:
  enabled: true
  smtp_host: "smtp.yourdomain.com"
  smtp_port: 587
  smtp_user: "user@yourdomain.com"
  smtp_password: "your-password"
  from_address: "maxiofs@yourdomain.com"
  tls_mode: "starttls"   # none | starttls | ssl

log_level: "info"   # debug | info | warn | error
```

### Accessing config.yaml

**Docker Compose (bind mount — default):**

```bash
nano ./maxiofs-data/config.yaml
docker compose restart maxiofs
```

**docker run with bind mount:**

```bash
nano /srv/maxiofs/config.yaml
docker restart maxiofs
```

**docker run with named volume** (Docker manages the storage location):

```bash
docker run -d \
  --name maxiofs \
  -p 8080:8080 \
  -p 8081:8081 \
  -v maxiofs-data:/data \
  maxiofs/maxiofs:latest

# Edit config via a helper container:
docker run --rm -it -v maxiofs-data:/data alpine vi /data/config.yaml
docker restart maxiofs
```

### Environment Variables

Only two settings can be overridden via environment variables:

```yaml
environment:
  MAXIOFS_DATA_DIR: "/data"    # must match the volume mount
  MAXIOFS_LOG_LEVEL: "info"    # overrides log_level in config.yaml
```

All other settings (encryption, SMTP, public URLs, TLS) require `config.yaml`.

## Volumes

**Default (Docker Compose):** bind mount at `./maxiofs-data` — data and config are visible on the host.

**Named volume:** Docker manages the storage at `/var/lib/docker/volumes/<name>/_data`. Use a helper container to access files inside.

### Backup

```bash
# Backup
docker run --rm \
  -v /srv/maxiofs:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/maxiofs-backup.tar.gz -C /data .

# Restore
docker run --rm \
  -v /srv/maxiofs:/data \
  -v $(pwd):/backup \
  alpine tar xzf /backup/maxiofs-backup.tar.gz -C /data
```

## Build Options

> Only needed to build locally instead of pulling from DockerHub.

```bash
# Standard build
make docker-build

# Specific version
VERSION=1.3.0 make docker-build

# Without cache
docker compose build --no-cache

# Multi-platform
docker buildx build --platform linux/amd64,linux/arm64 -t maxiofs:1.3.0 .
```

## Troubleshooting

**Container won't start:**
```bash
docker compose logs maxiofs
docker compose config   # validate compose file
```

**Prometheus not scraping MaxIOFS:**
1. Check targets: http://localhost:9091/targets
2. Verify `maxiofs` container is healthy: `docker compose ps`
3. Check network: `docker network inspect maxiofs-network`

**Grafana dashboards not loading:**
```bash
docker compose logs grafana
docker exec maxiofs-grafana ls /var/lib/grafana/dashboards
```

**Reload Prometheus config without restart:**
```bash
curl -X POST http://localhost:9091/-/reload
```

## Useful Commands

```bash
# Status
docker compose ps
docker stats

# Health
docker inspect maxiofs --format='{{.State.Health.Status}}'

# Logs
docker compose logs -f maxiofs
docker compose logs --tail=100 maxiofs

# Restart
docker compose restart maxiofs
docker compose up -d --force-recreate

# Cleanup
docker compose down
docker compose down -v   # ⚠️ also deletes volumes (data loss)
docker image prune -a
make docker-clean
```

## Version Information

- **MaxIOFS**: `maxiofs/maxiofs:latest` — also available as `maxiofs/maxiofs:1.3.0`
- **Prometheus**: 3.0.1 (monitoring profile)
- **Grafana**: 11.5.0 (monitoring profile)
- **Docker Compose**: v2.x required
- **Platforms**: linux/amd64, linux/arm64
