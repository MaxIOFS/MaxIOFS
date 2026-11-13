# Prometheus and Grafana

MaxIOFS exposes Prometheus metrics at `/metrics` on the **S3 port** (8080).
The repository includes a Grafana dashboard and alert rules.

## 1. Set a token

`/metrics` answers only with a bearer token, so the list of buckets and
their sizes is not public. In the console, **Settings → Metrics**, set
`metrics.token` to a long random value:

```bash
openssl rand -hex 32
```

Without a token set, `/metrics` answers `401`.

## 2. Scrape it

Save the token in a file Prometheus can read, then:

```yaml title="prometheus.yml"
scrape_configs:
  - job_name: maxiofs
    metrics_path: /metrics
    scrape_interval: 30s
    authorization:
      type: Bearer
      credentials_file: /etc/prometheus/maxiofs.token
    static_configs:
      - targets: ['maxiofs.example.com:8080']
```

For a cluster, list every node as a target.

Test it by hand:

```bash
curl -H "Authorization: Bearer $(cat maxiofs.token)" http://localhost:8080/metrics | head
```

## 3. Grafana dashboard

Import `docker/grafana/dashboards/maxiofs.json` from the repository
(**Dashboards → New → Import**) and pick your Prometheus data source.

## Everything in Docker

The repository's Compose file starts MaxIOFS, Prometheus with the alert rules,
and Grafana with the dashboard already provisioned:

```bash
git clone https://github.com/MaxIOFS/MaxIOFS.git
cd MaxIOFS
echo "<your metrics token>" > docker/prometheus/secrets/maxiofs.token
docker compose --profile monitoring up -d
```

| Service | Address |
|---------|---------|
| MaxIOFS console | http://localhost:8081 |
| Prometheus | http://localhost:9091 |
| Grafana | http://localhost:3000 (admin / admin) |

## Metrics

All metrics start with `maxiofs_`.

| Family | Metrics |
|--------|---------|
| HTTP | `http_requests_total`, `http_request_duration_seconds`, `http_request_size_bytes`, `http_response_size_bytes` |
| S3 | `s3_operations_total`, `s3_operation_duration_seconds`, `s3_errors_total`, `s3_refused_total` |
| Objects and buckets | `object_operations_total`, `object_operation_duration_seconds`, `object_size_bytes`, `bucket_objects_total`, `bucket_bytes_total`, `bucket_operations_total` |
| Storage | `storage_operations_total`, `storage_operation_duration_seconds` |
| Object Lock | `object_lock_operations_total`, `object_lock_retention_objects_total` |
| Authentication | `auth_attempts_total`, `auth_failures_total` |
| System | `system_cpu_usage_percent`, `system_memory_usage_percent`, `system_disk_usage_percent`, `system_disk_used_bytes`, `system_disk_total_bytes`, `system_events_total` |
| Background work | `background_tasks_total`, `background_task_duration_seconds` |
| Cache | `cache_hit_rate`, `cache_size_bytes` |
| Performance | `operation_latency_p50_milliseconds`, `..._p95_...`, `..._p99_...`, `operation_latency_mean_milliseconds`, `operation_success_rate_percent`, `operation_count_total`, `throughput_requests_per_second`, `throughput_bytes_per_second`, `throughput_objects_per_second` |

## Alerts

`docker/prometheus/alerts.yml` has ready-made rules, among them:

| Alert | Fires when |
|-------|------------|
| `MaxIOFSDown` | The target stops answering |
| `DiskAlmostFull` | The data disk is nearly full |
| `S3ServerErrors` | S3 requests fail with server errors |
| `StorageFailures` | Reads or writes to disk fail |
| `SignInFailureBurst` | Many failed sign-ins in a short time |
| `BackgroundTaskFailing` | A background task (lifecycle, integrity, replication…) keeps failing |
| `HighP95Latency`, `HighP99Latency` | Latency above the target |
| `LowSuccessRate` | Too many requests fail |

Copy the file next to your `prometheus.yml`, add it under `rule_files`, and
adjust the thresholds to your installation.

## Health checks

For load balancers and uptime monitors, without a token:

```bash
curl http://localhost:8080/health          # S3 API
curl http://localhost:8081/api/v1/health   # console
```
