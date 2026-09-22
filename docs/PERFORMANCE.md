# MaxIOFS Performance & SLOs

**Version**: 1.7.0 | **Last Updated**: September 21, 2026

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

Veeam writes thousands of objects in bursts. MaxIOFS ships with Pebble pre-tuned for this pattern:

| Tuning | Value | Why it matters for Veeam |
|--------|-------|--------------------------|
| MemTable size | 64 MB | Absorbs write bursts in RAM; fewer L0 flushes during active backup jobs. |
| MemTableStopWritesThreshold | 12 | Prevents write stalls while compaction catches up after a large restore job. |
| Compaction concurrency | 2–4 goroutines | Keeps the LSM tree compact in the background, maintaining fast list performance between backup sessions. |
| Bloom filters (L1–L6) | 10 bits/key | Point lookups (HEAD requests, existence checks by Veeam) skip unnecessary disk reads on non-matching keys. |
| Block size | 32 KB | Efficient for sequential range scans (folder listings, GETs of consecutive objects). |

These values are fixed in the binary and do not require configuration. Only the cache size is user-tunable.

### Write durability cost

An overwrite retains a rollback copy of the object it replaces until the new index entry is
durable, so the metadata commit for an object write, a version write and a multipart-part write
waits for a WAL sync before returning — see [Metadata durability](ARCHITECTURE.md#storage-layer) in
ARCHITECTURE.md. The bucket mutation lock is released before that wait, so concurrent writers to
the same bucket share one fsync instead of serializing on it (Pebble's group commit).

Measured with `scripts/s3-battery.sh --bench` (8 KiB objects, one node, local SSD):

| | 1 stream | 32 streams |
|---|---|---|
| Create | 11.7 obj/s | 31.0 obj/s |
| Overwrite | 9.2 obj/s | 28.7 obj/s |

At one stream, releasing the lock before the fsync roughly doubles the rate over waiting for it
inside the lock (7.6 → 11.7 obj/s). At 32 streams the gain is within noise — the ceiling there is
the filesystem backend's own write path (three fsyncs and three renames per object between the
data file, its sidecar and the staged-commit protocol), not the metadata commit. A raw filesystem
probe with the same two-file, two-fsync pattern reaches roughly 90 obj/s on the same disk, so there
is headroom in that path that this release does not yet use.

Run the benchmark against your own storage before sizing a Veeam job:

```bash
scripts/s3-battery.sh --bench-only --console http://localhost:8081 --admin-password '...' \
  --bench-objects 1000 --bench-streams 32 --bench-label baseline
```

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

Do not extrapolate Windows latency or throughput to Linux production capacity.
Software-induced copying and repeated operations can still be identified on Windows;
their production impact requires measurements on representative storage and workloads.

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

## Write-Path Cost Accounting

`internal/object/write_cost_test.go` exercises the real object manager, encryption,
bucket metrics, Pebble and filesystem. It does not include HTTP, IAM, tenant quotas,
network, replication or multipart completion. No running server is needed or contacted.
Only test code is instrumented; production durability settings are unchanged.

The workload covers new objects, unversioned overwrites, versioned writes, new parts
and replacement parts, with 4 KiB and 1 MiB payloads and 1, 8 and 32 concurrent workers.
Replacement workers each own a key/part; this is not a single-hot-key contention test.
Versioned writes retain history. Existing replacement data has the same size as the new
payload. Setup, content verification and cleanup are excluded from the measured interval.
Every final object/part is read back and checked; successful writes must leave no backups.

The small accounting test runs in CI. The disk workload is opt-in:

```powershell
$env:MAXIOFS_WRITE_COST = '1'
$env:MAXIOFS_COST_OPS = '64'
go test ./internal/object -run '^TestWritePathCost$' -count=1 -v
```

```bash
MAXIOFS_WRITE_COST=1 MAXIOFS_COST_OPS=64 \
  go test ./internal/object -run '^TestWritePathCost$' -count=1 -v
```

`MAXIOFS_COST_OPS` is the operation count per scenario (32 to 10000, default 64).
`MAXIOFS_COST_DIR` optionally selects an existing parent on the disk under test.
The test creates and removes its own unique subdirectory, never the parent.
Use an isolated test volume, not the live data directory. Large operation counts
increase both disk traffic and retained version history. The default matrix writes
about 964 MiB of payload, plus staging, backups, setup data and metadata.

Each `WRITE_COST` line contains JSON with backend calls, selected metadata calls,
bytes consumed from backend reads/writes, local throughput and local p50/p95 latency.
Counters preserve the reader's `WriteTo` fast path. They count application interfaces,
not system calls, physical IOPS, SSD wear or cache misses. Backend write bytes exclude
plaintext staging, backup destination writes, sidecars and the WAL. Backend reads during
these successful write scenarios are backup reads. Metadata lookups may hit memory.
There is no total fsync counter in this test. OS tracing is needed for that measurement.

### Local Validation, September 21, 2026

All 30 scenarios passed on Windows/amd64, Go 1.26.6, with 32 operations per scenario.
These are short bursts, not sustained-load or capacity results. At 32 workers there is
only one measured operation per worker. Do not use the local latency as a production SLO.

Measured per-operation counters for 1 MiB payloads were stable across concurrency levels:

| Write | Bytes read for backup | Bytes passed to backend write | Bucket / object metadata lookups |
|-------|-----------------------|-------------------------------|----------------------------------|
| New object | 0 | 1,048,908 | 3 / 2 |
| Unversioned overwrite | 1,048,908 | 1,048,908 | 3 / 3 |
| Versioned write | 0 | 1,048,908 | 3 / 2 |
| New part | 0 | 1,048,576 | 0 / 0 |
| Replacement part | 1,048,576 | 1,048,576 | 0 / 0 |

All scenarios made one metadata object/version/part commit and one explicit backend
metadata read per operation. Part writes additionally looked up the upload and prior part.
The encryption format accounts for the extra object bytes.

Source inspection, separately from the counters, shows that `retainBackup` writes the
entire previous content to a temporary file and syncs it; `writeManifest` syncs the
manifest and calls `SyncDirectory`. The new data and sidecar have their own syncs.
Directory sync is a no-op on Windows and an actual directory fsync on Unix, another
reason Windows timings cannot predict Linux durability costs. `PutObject` also stages
the plaintext before writing encrypted bytes. The test has not attributed elapsed time
between those operations or compared against a weaker durability mode.

The hardware-independent finding is the extra full-content copy on replacement.
Removing it or reducing repeated lookups is a candidate for investigation, not evidence
that it is safe to remove recovery protection or that all filesystem checks are redundant.

### PUT Staging and Multipart Completion

`internal/object/staging_cost_test.go` extends the investigation to completion.
`TestStagingPassAccounting` uses small fixtures in CI. Set `MAXIOFS_WRITE_COST=1`
and run `go test ./internal/object -run '^TestMultipartPassCost$' -count=1 -v`
for 2 and 8 parts of 5 MiB each, both new objects and overwrites. It uses its own
temporary directories and does not connect to a server. Unlike `TestWritePathCost`,
this probe does not accept `MAXIOFS_COST_DIR` and does not measure elapsed time:
its directory snapshots would distort timing. Setup uploads are outside the counters.

The probe counts bytes read from parts, assembled plaintext, previous ciphertext
and the encryption input; it counts each backend publication and snapshots staging
file sizes immediately before it. Final plaintext, ETag, user metadata, encryption
markers, upload cleanup and closure of all part readers are checked.

Baseline before streaming completion, September 21, 2026; all eight small/large scenarios passed:

| Path | Logical content writes, excluding metadata | Content reads after upload |
|------|-------------------------------------------|----------------------------|
| New PUT | Plaintext staging N + encrypted output C | Staging N |
| Complete new multipart | Plaintext assembly N + staging N + encrypted output C | Parts N + assembly N + staging N |
| Complete over an existing object | Same as above, plus previous ciphertext backup B | Same as above, plus previous ciphertext B |

N is the payload size; C and B include the encryption format overhead. The staging
write is evidenced by the file size snapshot and the copy path, not a write-syscall
counter. These totals are logical file traffic, not physical device traffic or IOPS.
Uploading the parts adds another N of writes before completion.

For the 40 MiB new multipart case, completion read 40 MiB from each of the three
sources and wrote a 40 MiB assembly, a 40 MiB staging file and 41,955,852 encrypted
bytes: approximately 120 MiB read and 120 MiB written, excluding uploaded parts,
sidecars and WAL. The overwrite added another 41,955,852-byte backup.

The two-part completion held two part readers open at once; the eight-part completion
held eight. `combineMultipartParts` opens all requested parts before consuming them.
This scales descriptor usage with part count, rather than keeping one part open;
descriptor exhaustion was not tested. Completion also fetched each part's metadata
twice, once for validation and once for the multipart ETag (4 and 16 lookups).

### Streaming Completion, September 22, 2026

Completion now encrypts sequential part readers directly into one backend publication.
The same four large scenarios passed with one encrypted write, zero assembly reads,
zero staging bytes and at most one open part reader. The 40 MiB case read 41,943,040
bytes from parts and wrote 41,955,852 encrypted bytes. Encryption consumes that same
part stream; its input counter is not an additional disk read.

Completion therefore requires N bytes read and C bytes written for a new object.
Including the initial part uploads gives approximately 3N of logical content traffic,
excluding metadata, encryption overhead and replication. This is byte accounting,
not a throughput forecast or a test with a 1 TB object.

Overwriting still reads and writes the previous ciphertext for rollback. Ordinary
PUT staging is unchanged, as are the two metadata lookups per completed part.
Tests verify plaintext MD5, multipart ETag, metadata, cleanup, failure rollback and
retry, including cancellation and part open/read/close failures. The producer is
joined before completion returns, including when storage rejects the write early.

---

**See also**: [ARCHITECTURE.md](ARCHITECTURE.md) · [OPERATIONS.md](OPERATIONS.md) · [TESTING.md](TESTING.md)
