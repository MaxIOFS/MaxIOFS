# MaxIOFS Performance Testing with k6

This directory contains comprehensive load testing infrastructure for MaxIOFS using [k6](https://k6.io/), an industry-standard performance testing tool.

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Test Scripts](#test-scripts)
- [Running Tests](#running-tests)
- [Interpreting Results](#interpreting-results)
- [Customization](#customization)
- [Troubleshooting](#troubleshooting)
- [Best Practices](#best-practices)

## Overview

The performance testing suite includes:

- **Upload Test** (`upload_test.js`) - Tests upload performance with various file sizes
- **Download Test** (`download_test.js`) - Tests download performance and cache behavior
- **Mixed Workload** (`mixed_workload.js`) - Simulates realistic production traffic
- **Common Library** (`common.js`) - Shared utilities, metrics, and helpers

### Architecture

```
┌─────────────────┐
│  k6 Test Runner │
│  (JavaScript)   │
└────────┬────────┘
         │
         ├─ Upload Test (50 VUs ramp-up)
         ├─ Download Test (100 VUs sustained)
         └─ Mixed Workload (spike test)
              │
              ├─ 50% Downloads
              ├─ 30% Uploads
              ├─ 15% List operations
              └─ 5% Deletes
                   │
                   v
         ┌──────────────────┐
         │  MaxIOFS Server  │
         │  S3 API :8080    │
         └──────────────────┘
```

## Prerequisites

### 1. Install k6

**macOS (Homebrew):**
```bash
brew install k6
```

**Linux (Debian/Ubuntu):**
```bash
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update
sudo apt-get install k6
```

**Windows (Chocolatey):**
```powershell
choco install k6
```

**Alternative (all platforms):**
Download from [k6.io/docs/get-started/installation](https://k6.io/docs/get-started/installation/)

### 2. Start MaxIOFS Server

```bash
# Build and start server
make build
./build/maxiofs --data-dir ./data

# Or use Docker
docker-compose up -d
```

### 3. Create S3 Credentials

1. Open web console: http://localhost:8081
2. Login as admin
3. Create a new user with S3 access
4. Generate access key and secret key
5. Save credentials for test configuration

## Quick Start

### 1. Set Environment Variables

```bash
export S3_ENDPOINT=http://localhost:8080
export CONSOLE_ENDPOINT=http://localhost:8081
export ACCESS_KEY=your_access_key
export SECRET_KEY=your_secret_key
export TEST_BUCKET=perf-test-bucket
```

**Windows (PowerShell):**
```powershell
$env:S3_ENDPOINT = "http://localhost:8080"
$env:ACCESS_KEY = "your_access_key"
$env:SECRET_KEY = "your_secret_key"
```

### 2. Run Quick Smoke Test

```bash
# Using Makefile
make perf-test-quick

# Or directly with k6
k6 run --vus 5 --duration 30s tests/performance/mixed_workload.js
```

### 3. Run Full Test Suite

```bash
make perf-test-all
```

This will run all tests sequentially (10-15 minutes total).

## Test Scripts

### Upload Test (`upload_test.js`)

**Purpose:** Validate upload performance under increasing load

**Scenario:**
- Ramp from 1 to 50 virtual users over 2 minutes
- Hold at 50 VUs for 2 minutes
- Ramp down to 0 over 30 seconds

**File Size Distribution:**
- 50% small files (1KB)
- 30% medium files (10KB)
- 15% large files (100KB)
- 4% very large (1MB)
- 1% huge files (5MB)

**Success Criteria:**
- ✅ 95% success rate
- ✅ p95 latency < 2 seconds
- ✅ p99 latency < 5 seconds
- ✅ At least 1MB uploaded
- ✅ At least 100 objects created

**Run:**
```bash
make perf-test-upload
# or
k6 run tests/performance/upload_test.js
```

### Download Test (`download_test.js`)

**Purpose:** Validate download performance and cache behavior

**Scenario:**
- Sustained load with 100 concurrent virtual users
- Run for 3 minutes
- Tests pre-populated objects (created during setup)

**Test Objects:**
- 11 objects ranging from 512 bytes to 5MB
- Weighted access pattern (simulates hot/cold cache)
- Small files accessed more frequently (cache hit simulation)

**Success Criteria:**
- ✅ 98% success rate (higher than uploads)
- ✅ p95 latency < 500ms (faster than uploads)
- ✅ p99 latency < 1 second
- ✅ At least 10MB downloaded

**Cache Analysis:**
The test automatically analyzes cache effectiveness:
- p50/p95 ratio < 2.0x = Good cache hit rate
- p50/p95 ratio 2-5x = Moderate effectiveness
- p50/p95 ratio > 5x = Poor cache (high variance)

**Run:**
```bash
make perf-test-download
# or
k6 run tests/performance/download_test.js
```

### Mixed Workload Test (`mixed_workload.js`)

**Purpose:** Simulate realistic production traffic patterns

**Scenario:**
- Spike test: 25 VUs normal → 100 VUs spike → 25 VUs
- Stages:
  1. 30s: Ramp to 25 VUs (normal load)
  2. 10s: Spike to 100 VUs
  3. 30s: Hold at 100 VUs
  4. 10s: Drop back to 25 VUs
  5. 30s: Ramp down to 0

**Operation Distribution:**
- 50% Downloads (most common)
- 30% Uploads (second most common)
- 15% List operations
- 5% Deletes (cleanup)

**File Size Distribution:**
- 40% small (1KB)
- 30% medium (10KB)
- 20% medium-large (50KB)
- 7% large (100KB)
- 2% very large (512KB)
- 1% huge (1MB)

**Success Criteria:**
- ✅ 90% upload success (allows for contention)
- ✅ 95% download success
- ✅ 98% list success
- ✅ 90% delete success
- ✅ p95 upload latency < 3 seconds
- ✅ p95 download latency < 1 second
- ✅ p95 list latency < 500ms
- ✅ p95 delete latency < 200ms

**Run:**
```bash
make perf-test-mixed
# or
k6 run tests/performance/mixed_workload.js
```

## Running Tests

### Using Makefile (Recommended)

```bash
# Quick smoke test (30 seconds)
make perf-test-quick

# Individual tests
make perf-test-upload
make perf-test-download
make perf-test-mixed

# All tests sequentially (10-15 min)
make perf-test-all

# Stress test (WARNING: intensive)
make perf-test-stress

# Custom test
make perf-test-custom VUS=100 DURATION=5m SCRIPT=mixed_workload.js
```

### Direct k6 Execution

```bash
# Basic execution
k6 run tests/performance/upload_test.js

# Custom VUs and duration
k6 run --vus 50 --duration 5m tests/performance/mixed_workload.js

# With environment variables
k6 run \
  --env S3_ENDPOINT=http://server:8080 \
  --env ACCESS_KEY=mykey \
  --env SECRET_KEY=mysecret \
  tests/performance/upload_test.js

# Output results to file
k6 run tests/performance/upload_test.js \
  --out json=results.json

# Summary only (no progress)
k6 run --quiet tests/performance/upload_test.js
```

### Advanced Options

```bash
# Increase HTTP timeout
k6 run --http-debug tests/performance/upload_test.js

# Disable TLS verification (for self-signed certs)
k6 run --insecure-skip-tls-verify tests/performance/upload_test.js

# Run with specific scenario
k6 run --scenario-name upload_rampup tests/performance/upload_test.js

# Verbose output
k6 run --verbose tests/performance/upload_test.js

# Disable color output (for CI/CD)
k6 run --no-color tests/performance/upload_test.js
```

## Interpreting Results

### Summary Output

After each test, k6 displays a summary:

```
✓ upload status is 200/201
✓ download status is 200

checks.........................: 97.50% ✓ 1950    ✗ 50
data_received..................: 5.2 GB 867 kB/s
data_sent......................: 4.8 GB 800 kB/s
http_req_duration..............: avg=125ms min=10ms med=95ms max=2.5s p(90)=200ms p(95)=350ms
http_reqs......................: 2000   333.33/s

upload_success.................: 95.50% (rate)
upload_latency_ms..............: avg=150ms p(95)=450ms p(99)=850ms

bytes_uploaded.................: 4.8 GB
objects_created................: 1910
```

### Key Metrics Explained

| Metric | Description | Good Value |
|--------|-------------|------------|
| `checks` | Validation pass rate | > 95% |
| `http_req_duration` | Overall request latency | p95 < 2s |
| `http_req_failed` | Error rate | < 5% |
| `upload_success` | Upload success rate | > 95% |
| `download_success` | Download success rate | > 98% |
| `upload_latency_ms` | Upload-specific latency | p95 < 2s |
| `download_latency_ms` | Download-specific latency | p95 < 500ms |
| `bytes_uploaded` | Total data sent | Depends on test |
| `bytes_downloaded` | Total data received | Depends on test |

### Pass/Fail Thresholds

Tests use built-in thresholds that cause test failure if not met:

```javascript
thresholds: {
  'upload_success': ['rate>0.95'],           // Fail if < 95% success
  'upload_latency_ms': ['p(95)<2000'],       // Fail if p95 > 2s
  'http_req_duration': ['p(95)<3000'],       // Fail if p95 > 3s
  'http_req_failed': ['rate<0.05'],          // Fail if > 5% errors
}
```

**Exit codes:**
- `0` - All thresholds passed
- `99` - One or more thresholds failed
- `1` - Script error

### JSON Output

Generate detailed JSON report:

```bash
k6 run --out json=results.json tests/performance/upload_test.js
```

Parse with `jq`:
```bash
# Get p95 latency
jq '.metrics.http_req_duration.values["p(95)"]' results.json

# Get error rate
jq '.metrics.http_req_failed.values.rate' results.json
```

## Customization

### Modify Test Parameters

Edit test scripts to change scenarios:

```javascript
// upload_test.js
export const options = {
  scenarios: {
    upload_rampup: {
      executor: 'ramping-vus',
      startVUs: 1,
      stages: [
        { duration: '5m', target: 100 },  // Ramp to 100 VUs
        { duration: '10m', target: 100 }, // Hold
        { duration: '2m', target: 0 },    // Ramp down
      ],
    },
  },
};
```

### Custom File Sizes

```javascript
// In test script
const fileSizes = [
  { size: 10240, weight: 60, name: '10KB' },   // 60% 10KB files
  { size: 1048576, weight: 40, name: '1MB' },  // 40% 1MB files
];
```

### Custom Thresholds

```javascript
export const options = {
  thresholds: {
    'upload_latency_ms': ['p(95)<1000', 'p(99)<3000'],
    'http_req_failed': ['rate<0.01'],  // Stricter: < 1% errors
  },
};
```

### Environment-Specific Config

Create `.env` file:
```bash
S3_ENDPOINT=http://prod-server:8080
ACCESS_KEY=prod_key
SECRET_KEY=prod_secret
TEST_BUCKET=prod-perf-test
```

Load with:
```bash
source .env
make perf-test-upload
```

## Troubleshooting

### Error: "k6 not found"

**Solution:** Install k6:
```bash
# macOS
brew install k6

# Linux
sudo apt-get install k6

# Windows
choco install k6
```

### Error: "401 Unauthorized"

**Cause:** Invalid or missing S3 credentials

**Solution:**
1. Verify credentials: Login to web console → Settings → S3 Keys
2. Set environment variables correctly:
   ```bash
   export ACCESS_KEY=your_key
   export SECRET_KEY=your_secret
   ```
3. Check credentials don't have special characters that need escaping

### Error: "Connection refused"

**Cause:** MaxIOFS server not running

**Solution:**
```bash
# Check if server is running
curl http://localhost:8080/

# Start server
make build
./build/maxiofs --data-dir ./data
```

### High Error Rates (> 5%)

**Possible Causes:**
1. **Insufficient resources** - Server CPU/memory exhausted
2. **Network issues** - Latency or packet loss
3. **Disk I/O bottleneck** - Storage can't keep up

**Solution:**
1. Monitor server resources: `htop`, `iotop`
2. Reduce VUs: `k6 run --vus 10 tests/performance/upload_test.js`
3. Check server logs for errors
4. Verify disk has sufficient space and I/O capacity

### Test Hangs or Timeouts

**Solution:**
1. Increase HTTP timeout in `common.js`:
   ```javascript
   const res = http.put(url, data, {
     headers: headers,
     timeout: '120s'  // Increase from default 60s
   });
   ```
2. Check for server deadlocks in logs
3. Reduce concurrent VUs

### Test Bucket Already Exists

**Normal behavior** - Tests reuse buckets by design

**To start fresh:**
```bash
# Using AWS CLI
aws s3 rb s3://perf-test-bucket --force --endpoint-url http://localhost:8080

# Or via web console
# Navigate to Buckets → Delete perf-test-bucket
```

## Best Practices

### 1. Start Small

Always start with low VU counts and short durations:
```bash
k6 run --vus 5 --duration 30s tests/performance/mixed_workload.js
```

Gradually increase load to find limits.

### 2. Monitor Server Resources

Run performance tests while monitoring:
```bash
# Terminal 1: Run k6 test
make perf-test-upload

# Terminal 2: Monitor server
htop

# Terminal 3: Monitor disk I/O
iotop -o

# Terminal 4: Server logs
tail -f logs/maxiofs.log
```

### 3. Isolate Tests

- Run one test at a time
- Don't run tests on production servers
- Use dedicated test buckets
- Clean up between major test runs

### 4. Establish Baselines

Run tests regularly to establish performance baselines:

```bash
# Record baseline
make perf-test-all | tee baseline-$(date +%Y%m%d).txt

# Compare later
diff baseline-20250101.txt current-results.txt
```

### 5. Test Realistic Scenarios

Configure tests to match your production traffic:
- Adjust file size distribution
- Match operation ratios (upload/download/list/delete)
- Use realistic think times

### 6. Continuous Performance Testing

Integrate into CI/CD:

```yaml
# .github/workflows/performance.yml
name: Performance Tests
on:
  push:
    branches: [main]
jobs:
  k6-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Install k6
        run: |
          curl https://github.com/grafana/k6/releases/download/v0.47.0/k6-v0.47.0-linux-amd64.tar.gz -L | tar xvz
          sudo cp k6-v0.47.0-linux-amd64/k6 /usr/bin
      - name: Start MaxIOFS
        run: |
          make build
          ./build/maxiofs --data-dir ./data &
          sleep 5
      - name: Run performance tests
        run: make perf-test-quick
        env:
          ACCESS_KEY: ${{ secrets.TEST_ACCESS_KEY }}
          SECRET_KEY: ${{ secrets.TEST_SECRET_KEY }}
```

## Further Reading

- [k6 Documentation](https://k6.io/docs/)
- [k6 Test Types](https://k6.io/docs/test-types/introduction/)
- [k6 Best Practices](https://k6.io/docs/testing-guides/automated-performance-testing/)
- [MaxIOFS Documentation](../../docs/)

## Support

For issues or questions:
- GitHub Issues: https://github.com/maxiofs/maxiofs/issues
- Documentation: https://maxiofs.io/docs
- Community: https://discord.gg/maxiofs
