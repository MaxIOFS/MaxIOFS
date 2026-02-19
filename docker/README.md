# MaxIOFS Docker Configuration

This directory contains all Docker-related configuration files for MaxIOFS.

## Directory Structure

```
docker/
├── prometheus/
│   ├── prometheus.yml     # Prometheus scrape configuration
│   └── alerts.yml         # Prometheus alert rules (14 rules)
├── grafana/
│   ├── provisioning/
│   │   ├── datasources/
│   │   │   └── prometheus.yml     # Prometheus datasource config
│   │   └── dashboards/
│   │       └── dashboard.yml      # Dashboard provisioning config
│   └── dashboards/
│       ├── maxiofs-overview.json     # System & storage overview (8 panels)
│       └── maxiofs-performance.json  # Performance metrics (7 panels)
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

### 3-Node Cluster

```bash
make docker-build
make docker-cluster
```

Access:
- Node 1 Console: http://localhost:8081 (admin/admin)
- Node 2 Console: http://localhost:8083 (admin/admin)
- Node 3 Console: http://localhost:8085 (admin/admin)

### Full Stack (Cluster + Monitoring)

```bash
make docker-build
make docker-cluster-monitoring
```

Access:
- Node 1-3 Consoles: http://localhost:8081, :8083, :8085
- Prometheus: http://localhost:9091
- Grafana: http://localhost:3000

## Grafana Dashboard

### maxiofs.json - Dashboard Unificado (HOME)

**UN SOLO dashboard** con todas las métricas organizadas en 3 secciones:

#### 📊 SISTEMA & RECURSOS (8 paneles)
1. **CPU Usage** - Utilización de CPU en tiempo real
2. **Memory Usage** - Uso de memoria
3. **Disk Usage** - Espacio en disco
4. **Total Buckets** - Número de buckets
5. **Total Objects** - Cantidad total de objetos
6. **Storage Used** - Bytes almacenados
7. **System Resources Over Time** - Tendencias CPU/Memory/Disk
8. **Storage by Bucket** - Distribución por bucket (pie chart)

#### ⚡ PERFORMANCE & LATENCIAS (3 paneles)
9. **Operation Latencies (p50/p95/p99)** - Latencias por operación
10. **Success Rate by Operation** - Tasa de éxito (gauges con colores)
11. **Operation Distribution** - Distribución de operaciones (pie chart)

#### 📈 THROUGHPUT & REQUESTS (3 paneles)
12. **Throughput - Requests/sec** - Throughput de requests
13. **Throughput - Bytes/sec** - Tasa de transferencia
14. **Throughput - Objects/sec** - Operaciones por segundo

**Total**: 14 paneles en un solo dashboard
**Auto-refresh**: 5 segundos
**Time range**: Últimos 15 minutos
**HOME por defecto**: ✅ Se abre automáticamente al entrar a Grafana

## Prometheus Configuration

### Scrape Targets

- **maxiofs** - Scrapes `maxiofs:8080/metrics` every 30 seconds
- **prometheus** - Self-monitoring at `localhost:9090`

### Alert Rules (14 rules)

**Performance Alerts** (11 rules):
- HighP95Latency (>100ms for 5 min)
- CriticalP95Latency (>500ms for 2 min)
- HighP99Latency (>200ms for 5 min)
- CriticalP99Latency (>1000ms for 2 min)
- LowSuccessRate (<95% for 3 min)
- CriticalSuccessRate (<90% for 1 min)
- LowThroughput (<1 req/s for 5 min)
- ZeroThroughput (=0 for 10 min)
- MeanLatencySpike (2x increase in 5 min)
- HighErrorCount (>100 errors in 5 min)
- OperationFailureSpike (5x increase in 1 min)

**SLO Violation Alerts** (3 rules):
- SLOLatencyViolation (p95 >50ms for 10 min)
- SLOAvailabilityViolation (success rate <99.9% for 5 min)
- SLOThroughputViolation (<1000 req/s for 10 min)

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

- **MaxIOFS**: 0.9.1-beta
- **Prometheus**: 3.0.1
- **Grafana**: 11.5.0
- **Docker Compose**: v2.x required

## Additional Resources

- [MaxIOFS Documentation](../docs/)
- [Prometheus Documentation](https://prometheus.io/docs/)
- [Grafana Documentation](https://grafana.com/docs/)
- [Docker Compose Reference](https://docs.docker.com/compose/)
