# MaxIOFS Performance & SLOs

**Version**: 1.2.0 | **Last Updated**: April 2, 2026

## Performance Summary

MaxIOFS demonstrates excellent performance on Linux production environments with sub-10ms latencies for all S3 operations under heavy concurrent load.

| Operation | p50 | p95 | p99 | Throughput |
|-----------|-----|-----|-----|------------|
| **Upload** | 4 ms | 9 ms | 13 ms | 1.7–2.4 MB/s |
| **Download** | 3 ms | 7–13 ms | 10–23 ms | 172 MB/s |
| **List** | 13 ms | 28 ms | 34 ms | — |
| **Delete** | 4 ms | 7 ms | 9 ms | — |

> Baseline: Debian Linux, 80 CPU cores, 125GB RAM, SSD/NVMe. Tested with K6 load testing (100,000+ operations, 100 concurrent VUs, 100% success rate).

---

## Test Environments

### Linux (Production Reference)

```
Platform:  Debian 6.1 (kernel 6.1.0-41-amd64)
CPU:       80 cores
Memory:    125 GB
Disk:      SSD/NVMe
```

### Windows (Development Only)

```
Platform:  Windows 11 (Notebook)
Disk:      NTFS filesystem
```

**Critical finding**: Linux is **10–300x faster** than Windows across all metrics due to NTFS overhead, disk I/O subsystem, and OS scheduler differences. **Windows test results are not representative of production performance** and should be used for functional testing only.

### Comparison (Mixed Workload p95)

| Operation | Windows p95 | Linux p95 | Improvement |
|-----------|-------------|-----------|-------------|
| Upload | 2,105 ms | 9 ms | **234x** |
| Download | 221 ms | 7 ms | **32x** |
| List | 1,008 ms | 28 ms | **36x** |
| Delete | 86 ms | 7 ms | **12x** |

---

## Service Level Objectives (SLOs)

### 1. Availability

| Target | Measurement | Error Budget |
|--------|-------------|-------------|
| **99.9%** | Rolling 30-day window | 43 minutes/month |

```promql
(sum(maxiofs_s3_operations_total) - sum(maxiofs_s3_errors_total))
  / sum(maxiofs_s3_operations_total) * 100
```

### 2. Latency (P95)

| Target | Measurement | Safety Margin |
|--------|-------------|--------------|
| **< 50 ms** for core S3 ops | Rolling 1-hour window | 5x above baseline |

| Threshold | Classification |
|-----------|---------------|
| < 20 ms | Excellent |
| 20–50 ms | Within SLO |
| 50–100 ms | Warning |
| > 100 ms | SLO violation |

### 3. Latency (P99)

| Target | Measurement | Safety Margin |
|--------|-------------|--------------|
| **< 100 ms** for core S3 ops | Rolling 1-hour window | 10x above baseline |

### 4. Throughput

| Target | Measurement | Baseline |
|--------|-------------|----------|
| **> 1,000 req/s** sustained | Rolling 5-minute window | 1,500–2,000 req/s achieved |

### 5. Error Rate

| Target | Measurement | Counts Against SLO |
|--------|-------------|-------------------|
| **< 1%** server errors | Rolling 1-hour window | 5xx only (4xx excluded) |

---

## Performance Targets by Operation

| Operation | P50 | P95 | P99 |
|-----------|-----|-----|-----|
| PutObject | < 10 ms | < 50 ms | < 100 ms |
| GetObject | < 10 ms | < 50 ms | < 100 ms |
| DeleteObject | < 10 ms | < 50 ms | < 100 ms |
| ListObjects | < 20 ms | < 75 ms | < 150 ms |
| HeadObject | < 5 ms | < 25 ms | < 50 ms |
| MultipartUpload | < 50 ms | < 200 ms | < 500 ms |

---

## Monitoring & Alerting

### Prometheus Metrics

Exposed at `/metrics` on both ports:

- `maxiofs_operation_latency_p50_milliseconds{operation}`
- `maxiofs_operation_latency_p95_milliseconds{operation}`
- `maxiofs_operation_latency_p99_milliseconds{operation}`
- `maxiofs_operation_success_rate_percent{operation}`
- `maxiofs_throughput_requests_per_second`
- `maxiofs_throughput_bytes_per_second`

### Alert Rules

Defined in `docker/prometheus/alerts.yml`:

| Alert | Condition | Severity |
|-------|-----------|----------|
| HighP95Latency | p95 > 100ms for 5m | warning |
| CriticalP95Latency | p95 > 500ms for 2m | critical |
| LowSuccessRate | success < 95% for 3m | critical |
| SLOViolationAvailability | hourly avg < 99.9% | critical |
| SLOViolationLatencyP95 | hourly avg p95 > 50ms | warning |

### Grafana Dashboard

Pre-built dashboard in `docker/grafana/dashboards/`:
- Latency percentiles (p50/p95/p99) over time
- Success rate gauges with color thresholds
- Throughput metrics (req/s, bytes/s)
- Operation distribution
- Mean latency trends

---

## Error Budget Policy

**Monthly error budget** = (1 − SLO target) × total time

For 99.9% availability: **43.2 minutes/month**

| Budget Consumed | Action |
|-----------------|--------|
| 0–25% | Normal operations |
| 25–50% | Increase monitoring, review recent changes |
| 50–75% | Freeze new features, focus on reliability |
| 75–100% | Code freeze, all hands on reliability |
| > 100% | Mandatory post-incident review |

---

## Metadata Store Performance (Pebble)

MaxIOFS stores all object and bucket metadata in an embedded Pebble LSM-tree database. Understanding its behaviour is essential for diagnosing latency spikes in large deployments.

### Cold vs. hot reads

The most common performance complaint is **"the first folder listing after a restart is slow."** This is expected and inherent to LSM-tree engines:

- **Cold read** (first access after restart): Pebble reads SSTable blocks from disk and populates the block cache. Duration depends on disk speed and folder size.
- **Hot read** (subsequent access): Data is served from the block cache in RAM — typically sub-millisecond.

There is no way to avoid the cold-read penalty entirely; you minimise its frequency by making the cache large enough to hold your working set.

### Tuning the block cache

The single most impactful setting is `storage.metadata_cache_size_mb` in `config.yaml`:

```yaml
storage:
  metadata_cache_size_mb: 1024   # MB — increase for write-heavy or large-bucket deployments
```

| Scenario | Recommended cache |
|----------|------------------|
| Dev / small deployment | 256 MB (default) |
| Medium workload | 512 MB |
| Veeam B&R / 20k–100k objects per bucket | 1 024 MB |
| Very large deployments | 2 048 MB+ |

> Each object metadata record occupies ~500–800 bytes in the cache. A Veeam bucket with 40 000 objects fits in ~20–32 MB of cache — well within even the default 256 MB.

### Write-heavy workloads (Veeam Backup & Replication)

Veeam writes thousands of objects in bursts. MaxIOFS v1.2.0 ships with Pebble pre-tuned for this pattern:

| Tuning | Value | Why it matters for Veeam |
|--------|-------|--------------------------|
| MemTable size | 64 MB | Absorbs write bursts in RAM; fewer L0 flushes during active backup jobs. |
| MemTableStopWritesThreshold | 12 | Prevents write stalls while compaction catches up after a large restore job. |
| Compaction concurrency | 2–4 goroutines | Keeps the LSM tree compact in the background, maintaining fast list performance between backup sessions. |
| Bloom filters (L1–L6) | 10 bits/key | Point lookups (HEAD requests, existence checks by Veeam) skip unnecessary disk reads on non-matching keys. |
| Block size | 32 KB | Efficient for sequential range scans (folder listings, GETs of consecutive objects). |

These values are fixed in the binary and do not require configuration. Only the cache size is user-tunable.

### Estimating required cache for your workload

```
objects_per_bucket × avg_metadata_size_bytes ÷ 1_048_576 = MB needed per bucket

Example (Veeam, 40 000 objects, 700 bytes avg):
  40 000 × 700 ÷ 1 048 576 ≈ 27 MB per bucket

For 10 such buckets:
  27 × 10 = 270 MB → 512 MB cache recommended (headroom for indexing overhead)
```

---

## Optimization Guidelines

**When to optimize**: SLO violations > 5% of the time over 7 days.

**Priority order**:
1. **Metadata cache** — increase `metadata_cache_size_mb` if folder listings are slow after restart
2. Disk I/O — use SSD/NVMe, enable OS caching
3. Memory — reduce GC pressure, reuse buffers
4. Concurrency — optimize lock contention
5. Network — connection pooling, HTTP/2

**Do NOT optimize for Windows performance** — bottlenecks are environmental (NTFS, OS scheduler), not code-level.

---

## Load Testing

K6 test scripts are in `tests/performance/`:

| Script | Description |
|--------|-------------|
| `upload_test.js` | Upload operations (ramp 1→50 VUs) |
| `download_test.js` | Download operations (ramp 1→100 VUs) |
| `mixed_workload.js` | 40% upload, 50% download, 7% list, 3% delete |
| `run_linux_tests.sh` | Automated Linux test runner |

```bash
# Run on Linux production environment (NOT Windows)
./tests/performance/run_linux_tests.sh
```

---

**See also**: [ARCHITECTURE.md](ARCHITECTURE.md) · [OPERATIONS.md](OPERATIONS.md) · [TESTING.md](TESTING.md)
