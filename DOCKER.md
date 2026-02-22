# MaxIOFS Docker Deployment Guide

This guide provides instructions for deploying MaxIOFS using Docker and Docker Compose with optional monitoring services.

## Quick Start

### Using Make (Recommended)

```bash
# Build the Docker image
make docker-build

# Start basic deployment (MaxIOFS only)
make docker-up

# Start with monitoring (Prometheus + Grafana)
make docker-monitoring

# Start 3-node cluster
make docker-cluster

# Start cluster + monitoring (full stack)
make docker-cluster-monitoring

# View logs
make docker-logs

# Stop services
make docker-down

# Clean volumes and containers
make docker-clean
```

### Using Docker Compose Directly

```bash
# Basic deployment (MaxIOFS only)
docker compose up -d

# With monitoring (Prometheus + Grafana)
docker compose --profile monitoring up -d

# 3-node cluster for HA testing
docker compose --profile cluster up -d

# Cluster + monitoring (full stack)
docker compose --profile monitoring --profile cluster up -d

# Stop services
docker compose down

# View logs
docker compose logs -f maxiofs
```

## Deployment Scenarios

### 1. Basic Deployment (MaxIOFS Only)

Minimal deployment with just the MaxIOFS server.

```bash
make docker-build
make docker-up
```

**Access:**
- S3 API: http://localhost:8080
- Web Console: http://localhost:8081 (admin/admin)

### 2. With Monitoring (Prometheus + Grafana)

Includes Prometheus for metrics collection and Grafana for visualization.

```bash
make docker-build
make docker-monitoring
```

**Access:**
- MaxIOFS Console: http://localhost:8081 (admin/admin)
- Prometheus: http://localhost:9091
- Grafana: http://localhost:3000 (admin/admin)

**Features:**
- Unified Grafana dashboard with 14 panels (loads automatically as HOME)
- Real-time metrics with 5-second auto-refresh
- Performance alerts (14 rules) for latency, throughput, and errors
- SLO violation monitoring

### 3. 3-Node Cluster

Multi-node cluster deployment for high availability testing.

```bash
make docker-build
make docker-cluster
```

**Access:**
- Node 1 Console: http://localhost:8081 (admin/admin)
- Node 2 Console: http://localhost:8083 (admin/admin)
- Node 3 Console: http://localhost:8085 (admin/admin)

**Features:**
- Independent data directories for each node
- Cluster management via web console
- Health monitoring and failover testing

### 4. Full Stack (Cluster + Monitoring)

Complete deployment with cluster and monitoring stack.

```bash
make docker-build
make docker-cluster-monitoring
```

**Access:**
- All node consoles: :8081, :8083, :8085
- Prometheus: http://localhost:9091
- Grafana: http://localhost:3000

## Services

### MaxIOFS (Main Service)

- **S3 API Port**: 8080
- **Web Console Port**: 8081
- **Health Check**: http://localhost:8081/api/v1/health
- **Data Volume**: `maxiofs-data`
- **Metrics**: Exposed at `/metrics` on port 8080

### Prometheus (monitoring profile)

- **Port**: 9091 (mapped from internal 9090)
- **URL**: http://localhost:9091
- **Configuration**: `docker/prometheus/prometheus.yml`
- **Alert Rules**: `docker/prometheus/alerts.yml` (14 rules)
- **Scrape Interval**: 30 seconds
- **Data Retention**: 30 days

### Grafana (monitoring profile)

- **Port**: 3000
- **URL**: http://localhost:3000
- **Default Credentials**: admin/admin (⚠️ change in production)
- **Dashboards**: Auto-loaded from `docker/grafana/dashboards/`
- **Default HOME Dashboard**: `maxiofs.json` (unified, 14 panels)

## Configuration

### Environment Variables

Key environment variables in `docker-compose.yaml`:

```yaml
# Server Configuration
MAXIOFS_LISTEN: ":8080"
MAXIOFS_CONSOLE_LISTEN: ":8081"
MAXIOFS_DATA_DIR: "/data"
MAXIOFS_LOG_LEVEL: "info"

# Public URLs
MAXIOFS_PUBLIC_API_URL: "http://localhost:8080"
MAXIOFS_PUBLIC_CONSOLE_URL: "http://localhost:8081"

# Security (⚠️ CHANGE IN PRODUCTION)
MAXIOFS_AUTH_ENABLE_AUTH: "true"
MAXIOFS_AUTH_JWT_SECRET: "change-this-secret-key-in-production"

# Metrics
MAXIOFS_METRICS_ENABLE: "true"
```

### Custom Configuration File

To use a custom `config.yaml`:

1. Create your configuration file:
```bash
cp config.example.yaml config.yaml
```

2. Edit `config.yaml` with your settings

3. Uncomment the volume mount in `docker-compose.yaml`:
```yaml
volumes:
  - maxiofs-data:/data
  - ./config.yaml:/app/config.yaml:ro  # Uncomment this line
```

**Note for Windows**: Docker Desktop may have issues with individual file mounts. If you encounter "user declined directory sharing" errors, use environment variables instead or mount a directory.

## Build Options

### Build with Specific Version

```bash
VERSION=0.9.2-beta make docker-build
```

### Build Without Cache

```bash
docker compose build --no-cache
```

### Multi-platform Build

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t maxiofs:0.9.2-beta .
```

## Volumes

The following Docker volumes are created automatically:

- **maxiofs-data**: Main node data (buckets, objects, metadata)
- **maxiofs-node2-data**: Node 2 data (cluster profile)
- **maxiofs-node3-data**: Node 3 data (cluster profile)
- **prometheus-data**: Prometheus metrics database
- **grafana-data**: Grafana dashboards and settings

### Backup Volumes

```bash
# Backup MaxIOFS data
docker run --rm -v maxiofs-data:/data -v $(pwd):/backup alpine tar czf /backup/maxiofs-backup.tar.gz -C /data .

# Restore from backup
docker run --rm -v maxiofs-data:/data -v $(pwd):/backup alpine tar xzf /backup/maxiofs-backup.tar.gz -C /data
```

## Grafana Dashboard

### Unified Dashboard (maxiofs.json)

The default HOME dashboard provides a comprehensive view with **14 panels** organized in 3 sections:

#### 📊 SISTEMA & RECURSOS (8 panels)
1. CPU Usage - Real-time CPU utilization
2. Memory Usage - Memory consumption
3. Disk Usage - Disk space utilization
4. Total Buckets - Number of buckets
5. Total Objects - Total object count
6. Storage Used - Bytes stored
7. System Resources Over Time - CPU/Memory/Disk trends
8. Storage by Bucket - Distribution by bucket (pie chart)

#### ⚡ PERFORMANCE & LATENCIAS (3 panels)
9. Operation Latencies (p50/p95/p99) - Latency by operation
10. Success Rate by Operation - Success rate with color-coded gauges
11. Operation Distribution - Operation mix (pie chart)

#### 📈 THROUGHPUT & REQUESTS (3 panels)
12. Throughput - Requests/sec
13. Throughput - Bytes/sec
14. Throughput - Objects/sec

**Settings:**
- Auto-refresh: 5 seconds
- Time range: Last 15 minutes
- Automatically loads as HOME dashboard

## Production Deployment

### 1. Security

**Change Default Passwords:**
```yaml
# Grafana
GF_SECURITY_ADMIN_PASSWORD: "strong-random-password-here"

# MaxIOFS
MAXIOFS_AUTH_JWT_SECRET: "use-long-random-secret-at-least-32-chars"
```

**Enable HTTPS with Reverse Proxy:**

Example with nginx:
```nginx
server {
    listen 443 ssl http2;
    server_name s3.yourdomain.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### 2. Resource Limits

Add to your services in `docker-compose.yaml`:

```yaml
deploy:
  resources:
    limits:
      cpus: '2'
      memory: 4G
    reservations:
      cpus: '1'
      memory: 2G
```

### 3. Persistent Logging

Configure log output to external systems:

```yaml
logging:
  driver: "json-file"
  options:
    max-size: "10m"
    max-file: "3"
```

### 4. Health Checks

The services include health checks for proper orchestration:

```yaml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:8081/api/v1/health"]
  interval: 30s
  timeout: 10s
  start_period: 15s
  retries: 3
```

## Monitoring & Alerts

### Prometheus Alert Rules

The deployment includes 14 alert rules in two groups:

**Performance Alerts (11 rules):**
- HighP95Latency: >100ms for 5 minutes
- CriticalP95Latency: >500ms for 2 minutes
- HighP99Latency: >200ms for 5 minutes
- CriticalP99Latency: >1000ms for 2 minutes
- LowSuccessRate: <95% for 3 minutes
- CriticalSuccessRate: <90% for 1 minute
- LowThroughput: <1 req/s for 5 minutes
- ZeroThroughput: 0 req/s for 10 minutes
- MeanLatencySpike: 2x increase in 5 minutes
- HighErrorCount: >100 errors in 5 minutes
- OperationFailureSpike: 5x increase in 1 minute

**SLO Violation Alerts (3 rules):**
- SLOLatencyViolation: p95 >50ms for 10 minutes
- SLOAvailabilityViolation: success rate <99.9% for 5 minutes
- SLOThroughputViolation: <1000 req/s for 10 minutes

### Customizing Alerts

Edit `docker/prometheus/alerts.yml` and reload:

```bash
curl -X POST http://localhost:9091/-/reload
```

## Troubleshooting

### Prometheus Not Scraping MaxIOFS

1. Check target status: http://localhost:9091/targets
2. Verify MaxIOFS is running: `docker compose ps`
3. Check network: `docker network inspect maxiofs-network`
4. Ensure metrics are enabled: `MAXIOFS_METRICS_ENABLE=true`

### Grafana Dashboards Not Loading

1. Check Grafana logs: `docker compose logs grafana`
2. Verify dashboard files:
   ```bash
   docker exec maxiofs-grafana ls /var/lib/grafana/dashboards
   ```
3. Check datasource: http://localhost:3000/datasources
4. Verify provisioning: `docker compose logs grafana | grep provisioning`

### Cluster Nodes Can't Communicate

1. Check network: `docker network inspect maxiofs-network`
2. Verify all containers are on same network: `docker compose ps`
3. Test connectivity:
   ```bash
   docker exec maxiofs curl http://maxiofs-node2:8080/health
   ```
4. Check firewall rules (if using external nodes)

### Performance Issues

1. Check resource usage: `docker stats`
2. Verify volume performance: `docker volume inspect maxiofs-data`
3. Review logs: `docker compose logs --tail=100 maxiofs`
4. Check Grafana dashboard for bottlenecks

### Container Won't Start

1. Check logs: `docker compose logs maxiofs`
2. Verify ports are available: `netstat -an | grep 8080`
3. Check volume permissions: `docker volume inspect maxiofs-data`
4. Validate configuration: `docker compose config`

## Useful Commands

### View Service Status

```bash
# Show all containers
docker compose ps

# Show resource usage
docker stats

# Check container health
docker inspect maxiofs --format='{{.State.Health.Status}}'
```

### Logs and Debugging

```bash
# View logs in real-time
docker compose logs -f

# View logs for specific service
docker compose logs -f maxiofs

# View last 100 lines
docker compose logs --tail=100

# Filter for errors
docker compose logs maxiofs | grep -i error
```

### Restart Services

```bash
# Restart specific service
docker compose restart maxiofs

# Restart all services
docker compose restart

# Recreate containers
docker compose up -d --force-recreate
```

### Clean Up

```bash
# Stop and remove containers
docker compose down

# Stop and remove containers + volumes (⚠️ DATA LOSS)
docker compose down -v

# Remove unused images
docker image prune -a

# Complete cleanup
make docker-clean
```

## Version Information

- **MaxIOFS**: 0.9.2-beta
- **Prometheus**: 3.0.1
- **Grafana**: 11.5.0
- **Docker Compose**: v2.x required

## Additional Resources

- [Docker Documentation](docker/README.md) - Detailed Docker configuration guide
- [MaxIOFS Documentation](docs/) - Complete documentation
- [Deployment Guide](docs/DEPLOYMENT.md) - Production deployment best practices
- [Performance Analysis](docs/PERFORMANCE.md) - Performance benchmarks and optimization
- [Cluster Management](docs/CLUSTER.md) - Multi-node cluster setup

## Support

For issues or questions:
- GitHub Issues: https://github.com/aluisco/maxiofs/issues
- Documentation: See `/docs` directory
- Health Check: http://localhost:8081/api/v1/health

---

**⚠️ Security Reminder**: Change all default passwords before deploying to production. Use HTTPS with proper TLS certificates. Enable firewall rules to restrict access.
