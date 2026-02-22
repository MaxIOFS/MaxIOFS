# Testing

**Version**: 0.9.2-beta  
**Last Updated**: February 21, 2026

---

## Overview

MaxIOFS has **112 Go test files**, **5 frontend test files** (Vitest), and **4 K6 performance scripts**. All Go tests run with the `-race` flag enabled by default. The project uses pure-Go SQLite (`modernc.org/sqlite`) and BadgerDB, so tests require no external dependencies — no Docker, no databases, no network services.

### Test Stack

| Layer | Tool | Assertion Library |
|-------|------|-------------------|
| Backend unit/integration | `go test` | `stretchr/testify` (require + assert) |
| Frontend unit | Vitest 4 + jsdom | `@testing-library/react` |
| Performance/load | K6 | Built-in K6 checks |
| Benchmarks | `go test -bench` | Standard `testing.B` |

---

## Running Tests

### Makefile Targets

```bash
# All tests with race detection and coverage
make test

# Unit tests only (skips long-running tests)
make test-unit

# Integration tests
make test-integration

# Benchmarks (storage + encryption)
make bench

# Benchmarks with CPU profiling
make bench-profile

# Lint (Go + frontend)
make lint

# Format
make fmt
```

### Go Commands

```bash
# All tests
go test -v -race -coverprofile=coverage.out ./...

# Specific package
go test -v -race ./internal/auth/...

# Specific test function
go test -v -race -run TestRateLimiter ./internal/auth/...

# Short mode (skips integration tests)
go test -v -race -short ./...

# Coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Frontend Tests

```bash
cd web/frontend

# Run all tests
npx vitest run

# Watch mode
npx vitest

# Coverage
npx vitest run --coverage
```

---

## Test Coverage by Package

### `internal/acl/` — 1 test file

| File | Description |
|------|-------------|
| `acl_test.go` | ACL parsing, canned ACL application, permission evaluation |

### `internal/api/` — 1 test file

| File | Description |
|------|-------------|
| `handler_security_test.go` | API handler security: auth bypass, injection, header validation |

### `internal/audit/` — 1 test file

| File | Description |
|------|-------------|
| `sqlite_test.go` | Audit log storage, querying, retention, SQLite operations |

### `internal/auth/` — 11 test files

| File | Description |
|------|-------------|
| `auth_test.go` | Core authentication flows |
| `audit_helpers_test.go` | Auth audit event helper functions |
| `manager_jwt_secret_test.go` | JWT secret rotation and validation |
| `manager_s3sig_test.go` | S3 Signature V4 verification |
| `manager_simple_test.go` | Basic auth manager operations (CRUD users, keys) |
| `manager_tenant_test.go` | Tenant-scoped auth operations |
| `permissions_test.go` | RBAC permission checks |
| `quota_cluster_test.go` | Cluster-wide quota enforcement |
| `rate_limiter_test.go` | Login rate limiting, account lockout |
| `s3auth_test.go` | S3 authentication flow end-to-end |
| `totp_test.go` | TOTP 2FA enrollment, verification, recovery codes |

### `internal/bucket/` — 4 test files

| File | Description |
|------|-------------|
| `delete_bucket_test.go` | Bucket deletion with object cleanup |
| `integration_test.go` | Bucket CRUD + versioning integration |
| `manager_test.go` | Bucket manager core operations |
| `policy_evaluation_test.go` | S3 bucket policy evaluation |

### `internal/cluster/` — 25 test files

| File | Description |
|------|-------------|
| `access_key_sync_test.go` | Cross-node access key synchronization |
| `bucket_aggregator_test.go` | Multi-node bucket list aggregation |
| `bucket_location_test.go` | Bucket location cache and routing |
| `bucket_permission_sync_test.go` | Bucket permission replication |
| `cache_test.go` | Cluster cache invalidation |
| `circuit_breaker_test.go` | Circuit breaker for unhealthy nodes |
| `deletion_log_test.go` | Tombstone-based deletion sync |
| `group_mapping_sync_test.go` | IDP group mapping synchronization |
| `health_test.go` | Node health checks (30s intervals) |
| `idp_provider_sync_test.go` | IDP provider configuration sync |
| `join_cluster_test.go` | Node join/leave workflow |
| `manager_test.go` | Cluster manager core operations |
| `metrics_test.go` | Cluster-wide metrics aggregation |
| `migration_integration_test.go` | Bucket migration end-to-end |
| `migration_test.go` | Migration unit operations |
| `proxy_test.go` | Request proxying to remote nodes |
| `quota_aggregator_test.go` | Cross-node quota aggregation |
| `quota_integration_test.go` | Quota enforcement in cluster |
| `rate_limiter_test.go` | Cluster-wide rate limiting |
| `replication_integration_test.go` | Replication end-to-end |
| `replication_manager_test.go` | Replication manager logic |
| `replication_worker_test.go` | Replication worker operations |
| `router_test.go` | Request routing to bucket owner |
| `tenant_sync_test.go` | Tenant sync across nodes |
| `user_sync_test.go` | User sync across nodes |

### `internal/config/` — 1 test file

| File | Description |
|------|-------------|
| `config_test.go` | Config loading (YAML, env vars, CLI flags), validation, defaults |

### `internal/db/migrations/` — 1 test file

| File | Description |
|------|-------------|
| `migrations_test.go` | SQLite schema migrations, version tracking |

### `internal/idp/` — 5 test files

| File | Description |
|------|-------------|
| `crypto_test.go` | AES-256-GCM encryption for IDP secrets |
| `manager_test.go` | IDP provider CRUD, user authorization |
| `store_test.go` | IDP persistent storage operations |
| `ldap/ldap_test.go` | LDAP bind, search, group resolution |
| `oauth/provider_test.go` | OAuth2/OIDC flow, token exchange, user mapping |

### `internal/inventory/` — 3 test files

| File | Description |
|------|-------------|
| `generator_test.go` | S3 inventory report generation (CSV/Parquet) |
| `manager_test.go` | Inventory schedule management |
| `worker_test.go` | Background inventory worker |

### `internal/lifecycle/` — 1 test file

| File | Description |
|------|-------------|
| `worker_test.go` | Lifecycle rule evaluation and object expiration |

### `internal/logging/` — 3 test files

| File | Description |
|------|-------------|
| `http_test.go` | HTTP log target delivery with WaitGroup-tracked goroutines |
| `manager_test.go` | Log manager routing, multi-target dispatch |
| `syslog_test.go` | Syslog target output |

### `internal/metadata/` — 7 test files

| File | Description |
|------|-------------|
| `badger_comprehensive_test.go` | BadgerDB operations comprehensive coverage |
| `badger_test.go` | BadgerDB core CRUD |
| `multipart_comprehensive_test.go` | Multipart upload metadata tracking |
| `objects_test.go` | Object metadata storage and retrieval |
| `search_objects_test.go` | Object search by prefix, delimiter, pagination |
| `tags_comprehensive_test.go` | Object and bucket tag operations |
| `versioning_test.go` | Version ID generation, version listing |

### `internal/metrics/` — 6 test files

| File | Description |
|------|-------------|
| `badger_history_test.go` | Metrics history stored in BadgerDB |
| `collector_test.go` | Prometheus metrics collection |
| `history_test.go` | Metrics history aggregation |
| `manager_test.go` | Metrics manager lifecycle |
| `performance_test.go` | Performance metrics tracking |
| `system_metrics_test.go` | System resource metrics (CPU, memory, disk) |

### `internal/middleware/` — 3 test files

| File | Description |
|------|-------------|
| `cluster_auth_test.go` | HMAC inter-node authentication middleware |
| `middleware_test.go` | Auth, CORS, rate limit middleware chain |
| `tracing_test.go` | Request tracing and request ID propagation |

### `internal/notifications/` — 1 test file

| File | Description |
|------|-------------|
| `manager_test.go` | SSE notification delivery, client management |

### `internal/object/` — 15 test files

| File | Description |
|------|-------------|
| `adapter_test.go` | Storage adapter abstraction |
| `integration_test.go` | Full object lifecycle (put → get → delete) |
| `lock_default_retention_test.go` | Default retention policy application |
| `lock_test.go` | Object Lock (WORM) compliance/governance modes |
| `manager_coverage_test.go` | Manager edge cases for coverage |
| `manager_critical_functions_test.go` | Critical path testing (copy, multipart complete) |
| `manager_final_coverage_test.go` | Final coverage gap tests |
| `manager_internal_test.go` | Internal manager functions |
| `manager_low_coverage_test.go` | Low-coverage path tests |
| `manager_versioning_acl_test.go` | Versioning + ACL interaction |
| `multipart_race_test.go` | Concurrent multipart upload race conditions |
| `retention_comprehensive_test.go` | Retention policy comprehensive coverage |
| `retention_test.go` | Basic retention tests |
| `search_objects_test.go` | Object search and listing |
| `versioning_delete_test.go` | Version-aware delete operations |

### `internal/presigned/` — 1 test file

| File | Description |
|------|-------------|
| `presigned_test.go` | Pre-signed URL generation and validation |

### `internal/replication/` — 2 test files

| File | Description |
|------|-------------|
| `manager_test.go` | Replication rule management |
| `replication_e2e_test.go` | End-to-end replication with `require.Eventually` |

### `internal/server/` — 6 test files

| File | Description |
|------|-------------|
| `bucket_aggregation_test.go` | Multi-node bucket aggregation API |
| `console_api_test.go` | Console REST API endpoint tests |
| `console_idp_test.go` | Console IDP management endpoints |
| `route_ordering_test.go` | Route priority and matching order |
| `search_api_test.go` | Object search API endpoints |
| `server_test.go` | Server startup, shutdown, configuration |

### `internal/settings/` — 1 test file

| File | Description |
|------|-------------|
| `manager_test.go` | Dynamic settings CRUD, defaults, validation |

### `internal/share/` — 1 test file

| File | Description |
|------|-------------|
| `manager_test.go` | Pre-signed share link generation and access |

### `internal/storage/` — 2 test files

| File | Description |
|------|-------------|
| `filesystem_test.go` | Filesystem storage operations (read, write, delete, stat) |
| `storage_bench_test.go` | Storage benchmarks (throughput, latency by file size) |

### `pkg/compression/` — 1 test file

| File | Description |
|------|-------------|
| `compression_test.go` | gzip/zstd compression roundtrip |

### `pkg/encryption/` — 2 test files

| File | Description |
|------|-------------|
| `encryption_test.go` | AES-256-CTR encrypt/decrypt roundtrip |
| `encryption_bench_test.go` | Encryption throughput benchmarks |

### `pkg/s3compat/` — 5 test files

| File | Description |
|------|-------------|
| `acl_debug_test.go` | ACL compatibility debugging |
| `acl_security_test.go` | ACL security edge cases |
| `handler_coverage_test.go` | S3 handler comprehensive coverage |
| `presigned_test.go` | S3 pre-signed request handling |
| `s3_test.go` | S3 protocol compatibility tests |

### `cmd/maxiofs/` — 1 test file

| File | Description |
|------|-------------|
| `main_test.go` | CLI flags, version output, config loading, server bootstrap |

### `web/` — 1 test file

| File | Description |
|------|-------------|
| `embed_test.go` | Frontend static file embedding verification |

---

## Frontend Tests

Located in `web/frontend/src/__tests__/`, using **Vitest 4** with **jsdom** environment and **@testing-library/react**.

| File | Description |
|------|-------------|
| `Buckets.test.tsx` | Bucket list, creation, deletion UI |
| `Dashboard.test.tsx` | Dashboard metrics rendering, storage distribution |
| `IdentityProviders.test.tsx` | IDP management UI flows |
| `Login.test.tsx` | Login form, OAuth redirect, 2FA prompt |
| `Users.test.tsx` | User management CRUD UI |

### Configuration

```typescript
// vitest.config.ts
{
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    css: true,
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json', 'html'],
    },
  },
}
```

### Running Frontend Tests

```bash
cd web/frontend
npx vitest run              # Run once
npx vitest                  # Watch mode
npx vitest run --coverage   # With coverage
```

---

## Performance Tests (K6)

Located in `tests/performance/`. Requires [K6](https://k6.io/docs/get-started/installation/) installed.

### Scripts

| Script | Description | VUs | Duration |
|--------|-------------|-----|----------|
| `upload_test.js` | Upload throughput | Ramp to 50 | 2 min |
| `download_test.js` | Download throughput | Sustained 100 | 3 min |
| `mixed_workload.js` | Mixed read/write/list | Spike 25→100 | Variable |
| `common.js` | Shared helpers (auth, bucket setup) | — | — |

### Running

```bash
# Set credentials
export S3_ENDPOINT=http://localhost:8080
export ACCESS_KEY=your-access-key
export SECRET_KEY=your-secret-key

# Individual tests
make perf-test-upload
make perf-test-download
make perf-test-mixed

# Quick smoke test (5 VUs, 30s)
make perf-test-quick

# Stress test (200 VUs, 5 min)
make perf-test-stress

# All tests sequentially (~15 min)
make perf-test-all

# Custom parameters
make perf-test-custom VUS=50 DURATION=2m SCRIPT=upload_test.js
```

---

## Benchmarks

Two packages have dedicated benchmarks:

### Storage Benchmarks

```bash
go test ./internal/storage -bench=. -benchmem -benchtime=3s
```

Tests filesystem throughput for various file sizes (1KB to 100MB), measuring write/read/delete latency.

### Encryption Benchmarks

```bash
go test ./pkg/encryption -bench=. -benchmem -benchtime=3s
```

Tests AES-256-CTR encryption/decryption throughput and memory allocation.

### Profiling

```bash
# Generate CPU profiles
make bench-profile

# Analyze
go tool pprof bench-results/cpu-storage.prof
go tool pprof bench-results/cpu-encryption.prof
```

---

## Test Patterns

### Common Setup

Most tests follow the same pattern: create a temp directory, initialize SQLite + BadgerDB, and clean up.

```go
func TestSomething(t *testing.T) {
    // Create isolated temp directory
    dir := t.TempDir()
    
    // Initialize SQLite (no CGO needed - uses modernc.org/sqlite)
    db, err := sql.Open("sqlite", filepath.Join(dir, "test.db"))
    require.NoError(t, err)
    defer db.Close()
    
    // Initialize BadgerDB for metadata
    opts := badger.DefaultOptions(filepath.Join(dir, "metadata"))
    opts.Logger = nil // Suppress logs in tests
    bdb, err := badger.Open(opts)
    require.NoError(t, err)
    defer bdb.Close()
    
    // Run test logic...
}
```

### Race Detection

All tests run with `-race` by default (`make test` and `make test-unit` both include `-race`). Tests for concurrent operations use goroutines with proper synchronization:

```go
func TestConcurrentAccess(t *testing.T) {
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            // concurrent operation
        }()
    }
    wg.Wait()
}
```

### Eventually Pattern

For asynchronous operations (replication, notifications), use `require.Eventually`:

```go
require.Eventually(t, func() bool {
    result, err := checkSomething()
    return err == nil && result.Ready
}, 5*time.Second, 100*time.Millisecond, "expected condition within 5s")
```

### Table-Driven Tests

Most tests use table-driven patterns:

```go
tests := []struct {
    name    string
    input   Input
    want    Output
    wantErr bool
}{
    {"valid input", Input{...}, Output{...}, false},
    {"missing field", Input{}, Output{}, true},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        got, err := DoSomething(tt.input)
        if tt.wantErr {
            require.Error(t, err)
            return
        }
        require.NoError(t, err)
        assert.Equal(t, tt.want, got)
    })
}
```

### Windows Compatibility

BadgerDB requires explicit cleanup on Windows due to file locking. Tests close the database before `t.TempDir()` cleanup:

```go
defer func() {
    bdb.Close() // Must close before TempDir cleanup on Windows
}()
```

---

## Test Distribution Summary

| Area | Files | Key Packages |
|------|-------|-------------|
| Cluster | 25 | Sync, replication, routing, migration, health |
| Object | 15 | CRUD, versioning, locking, retention, multipart |
| Auth | 11 | Users, keys, JWT, S3 sig, TOTP, rate limiting |
| Metadata | 7 | BadgerDB, search, tags, versioning, multipart |
| Metrics | 6 | Prometheus, history, system, performance |
| Server | 6 | Routes, console API, IDP endpoints, search |
| IDP | 5 | LDAP, OAuth, crypto, storage |
| S3 Compat | 5 | Protocol compliance, ACL, pre-signed, handlers |
| Bucket | 4 | CRUD, deletion, policy, integration |
| Logging | 3 | HTTP targets, syslog, manager |
| Middleware | 3 | Auth, CORS, tracing |
| Inventory | 3 | Report generation, scheduling |
| Storage | 2 | Filesystem, benchmarks |
| Encryption | 2 | AES-256-CTR, benchmarks |
| Replication | 2 | Manager, end-to-end |
| Other | 10 | Config, migrations, lifecycle, notifications, presigned, settings, share, compression, cmd, embed |
| **Total Go** | **112** | |
| Frontend | 5 | Dashboard, Login, Users, Buckets, IDP |
| Performance | 4 | Upload, download, mixed, common |
