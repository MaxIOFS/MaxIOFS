# MaxIOFS Docker Configuration

This directory contains all Docker-related configuration files for MaxIOFS.

## Directory Structure

```
docker/
├── prometheus/
│   ├── prometheus.yml     # Prometheus scrape configuration
│   └── alerts.yml         # Prometheus alert rules (20 rules)
├── grafana/
│   ├── provisioning/
│   │   ├── datasources/
│   │   │   └── prometheus.yml     # Prometheus datasource config
│   │   └── dashboards/
│   │       └── dashboard.yml      # Dashboard provisioning config
│   └── dashboards/
│       └── maxiofs.json           # The MaxIOFS dashboard (Grafana home)
└── README.md              # This file
```

## Usage

### Basic Deployment (MaxIOFS only)

```bash
make docker-build
make docker-up
```

Access:
- S3 API: http://localhost:8080
- Web Console: http://localhost:8081 (admin/admin)

### With Monitoring (Prometheus + Grafana)

```bash
make docker-build
make docker-monitoring
```

Access:
- MaxIOFS Console: http://localhost:8081 (admin/admin)
- Prometheus: http://localhost:9091
- Grafana: http://localhost:3000 (admin/admin)

### Clustering

A multi-node cluster inside a single Docker host provides **no real HA**
(every "node" shares the same disk, power and kernel) — MaxIOFS therefore
ships no single-host cluster compose. For real clusters, deploy one node per
host (containers included: expose port 8082 between hosts, restricted to
cluster node IPs, and pin each node to its host with a local volume) and
follow [docs/CLUSTER.md](../docs/CLUSTER.md).

## Grafana Dashboard

`maxiofs.json` is Grafana's home dashboard. Two variables filter it: **Node** (`instance`) and **Bucket**. Sections:

- **Overview**: buckets, objects and bytes held, S3 requests per second, S3 error ratio, the fullest disk.
- **S3 API**: requests by operation, responses by HTTP status, errors by S3 code, latency p95 by operation and p99 by method, request and response sizes, traffic, busiest buckets.
- **Storage & Objects**: storage backend operations and latency, object operations, latency and sizes, bytes and objects by bucket.
- **Security**: sign-ins by method and outcome (S3 signatures, presigned URLs, form uploads, console password, 2FA and OAuth), failures by reason, bucket changes, Object Lock changes, retained objects by bucket and mode.
- **Background Tasks & Events**: runs and average duration of lifecycle, inventory, replication, stats, integrity, encryption, audit retention, STS sweep and cluster tasks; events sent to the console.
- **System**: CPU, memory and disk per node, metadata cache hit rate and size.
- **Performance**: the per-node latency percentiles, success rates and throughput of the last hour.

Every node of a cluster exports its own metrics: add each node's S3 port to the scrape targets. Counts of objects and bytes are what each node holds; with a replication factor above 1 each copy counts.

Refresh: 30 seconds. Time range: last hour.

## Prometheus Configuration

### Scrape Targets

- **maxiofs** - Scrapes `maxiofs:8080/metrics` every 30 seconds
- **prometheus** - Self-monitoring at `localhost:9090`

### Alert Rules (20 rules)

**Performance** (`maxiofs_performance`, 11 rules): high and critical p95 and p99 latency, low and critical success rate, low and zero throughput, mean latency spike, suspicious request rate, high latency variance.

**SLO** (`maxiofs_slo_violations`, 3 rules): availability below 99.9%, p95 above 50 ms, p99 above 100 ms.

**Health** (`maxiofs_health`, 6 rules):
- MaxIOFSDown: `/metrics` not answering for 2 minutes
- S3ServerErrors: more than 5% of S3 requests answered 5xx for 5 minutes
- StorageFailures: the storage backend failing an operation for 5 minutes
- DiskAlmostFull: data disk above 90% for 10 minutes
- SignInFailureBurst: more than 50 failed sign-ins of one method in 5 minutes
- BackgroundTaskFailing: a background task failed in the last hour

## Customization

### Modify Prometheus Scrape Interval

Edit `prometheus/prometheus.yml`:

```yaml
scrape_configs:
  - job_name: 'maxiofs'
    scrape_interval: 15s  # Change this value
```

### Add Custom Dashboards

1. Create JSON dashboard in `grafana/dashboards/`
2. Restart Grafana: `docker-compose restart grafana`
3. Dashboard auto-loads within 10 seconds

### Modify Alert Thresholds

Edit `prometheus/alerts.yml`:

```yaml
- alert: HighP95Latency
  expr: maxiofs_operation_latency_p95_milliseconds > 100  # Change threshold
```

Reload alerts:
```bash
curl -X POST http://localhost:9091/-/reload
```

## Troubleshooting

### Prometheus not scraping MaxIOFS

Check target status: http://localhost:9091/targets

Common issues:
- MaxIOFS not running: `docker-compose ps`
- Network issue: Verify `maxiofs-network` exists
- Metrics disabled: Check `MAXIOFS_METRICS_ENABLE=true`

### Grafana dashboards not loading

1. Check provisioning logs: `docker-compose logs grafana`
2. Verify files exist in container:
   ```bash
   docker exec maxiofs-grafana ls /var/lib/grafana/dashboards
   ```
3. Check datasource: http://localhost:3000/datasources

### Cluster nodes can't communicate

1. Check network: `docker network inspect maxiofs-network`
2. Verify all containers on same network:
   ```bash
   docker-compose ps
   ```
3. Test connectivity:
   ```bash
   docker exec maxiofs curl http://maxiofs-node2:8080/health
   ```

## Production Considerations

1. **Change Default Passwords**
   - Grafana: Set `GF_SECURITY_ADMIN_PASSWORD` in docker-compose.yaml
   - MaxIOFS: Set `MAXIOFS_AUTH_JWT_SECRET` to a strong random value

2. **Persistent Storage**
   - Volumes are created automatically
   - Backup: `docker run --rm -v maxiofs-data:/data -v $(pwd):/backup alpine tar czf /backup/maxiofs-backup.tar.gz -C /data .`

3. **Resource Limits**
   - Add to docker-compose.yaml:
     ```yaml
     deploy:
       resources:
         limits:
           cpus: '2'
           memory: 4G
     ```

4. **TLS/HTTPS**
   - Use reverse proxy (nginx/traefik)
   - Mount certificates to containers
   - Update `MAXIOFS_PUBLIC_API_URL` and `MAXIOFS_PUBLIC_CONSOLE_URL`

## Version Information

- **MaxIOFS**: 1.8.0
- **Prometheus**: 3.0.1
- **Grafana**: 11.5.0
- **Docker Compose**: v2.x required

## Additional Resources

- [MaxIOFS Documentation](../docs/)
- [Prometheus Documentation](https://prometheus.io/docs/)
- [Grafana Documentation](https://grafana.com/docs/)
- [Docker Compose Reference](https://docs.docker.com/compose/)
