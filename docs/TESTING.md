# MaxIOFS Testing Guide

**Version**: 0.4.1-beta
**Last Updated**: November 12, 2025

---

## Test Coverage Status

### ✅ Working Tests (Up-to-date with Multi-tenancy)

#### 1. **Integration Tests (internal/)**

These tests are **fully functional** and up-to-date with the current multi-tenancy architecture:

**Location**: `internal/bucket/integration_test.go`
- ✅ **TestBucketManagerIntegration** - Complete bucket operations with tenantID
- ✅ **TestBucketManagerMultiTenant** - Multi-tenant isolation validation
- ✅ **TestBucketManagerConcurrency** - Concurrent operations with BadgerDB
- ✅ **TestBucketManagerPersistence** - Database persistence across restarts

**Location**: `internal/object/integration_test.go`
- ✅ **TestObjectManagerBasicOperations** - CRUD operations with multi-tenancy
- ✅ **TestObjectManagerTagging** - Object tagging functionality
- ✅ **TestObjectManagerObjectLock** - WORM compliance features
- ✅ **TestObjectManagerMultipartUpload** - Large file uploads
- ✅ **TestObjectManagerBucketMetricsIntegration** - Bucket metrics tracking
- ✅ **TestObjectManagerPersistence** - Metadata persistence

**Status**: ✅ **PASS** - All tests working correctly

**Run**:
```bash
go test -v ./internal/bucket/...
go test -v ./internal/object/...
```

---

### ⚠️ Tests Requiring Auth Setup (tests/integration/api/)

**Location**: `tests/integration/api/s3_test.go`

**Status**: ⚠️ **UPDATED BUT REQUIRES AUTH**

These tests have been **updated for multi-tenancy architecture** (BadgerDB, tenantID parameters), but they test the **S3 HTTP API** which now requires authentication or public bucket access.

**Current Issues**:
1. Tests use raw HTTP requests without S3 signature authentication
2. S3 handler correctly returns `403 AccessDenied` for unauthenticated requests
3. Tests need either:
   - Mock S3 signature authentication
   - Configure buckets as public
   - Use AWS SDK with proper credentials

**Tests**:
- **TestS3BasicOperations** - Basic S3 operations (CreateBucket, PutObject, GetObject, etc.)
- **TestS3MultipartUpload** - Multipart upload workflow
- **TestS3ConcurrentAccess** - Concurrent S3 operations
- **TestS3ErrorHandling** - Error case handling

**Recommendation**: These tests should either be:
1. **Converted to use AWS SDK** with proper S3 signature authentication
2. **Marked as skipped** in favor of the working integration tests in `internal/`
3. **Modified to test only public bucket scenarios**

---

### ❌ Obsolete Tests (tests/unit/)

**Location**: `tests/unit/`

**Status**: ❌ **OBSOLETE - NEED MAJOR UPDATES**

These unit tests were written before the multi-tenancy architecture and are now outdated:

**Tests/unit/bucket/manager_test.go**:
- Uses old `bucket.NewManager(storage)` signature
- Missing `metadata.Store` parameter
- Missing `tenantID` in all method calls
- ❌ **DOES NOT COMPILE**

**tests/unit/object/manager_test.go**:
- Uses old `object.NewManager(storage, config)` signature
- Missing `metadata.Store` parameter
- Missing `tenantID` in bucket operations
- ❌ **DOES NOT COMPILE**

**tests/unit/auth/manager_test.go**:
- Uses old `auth.NewManager(config)` signature
- Missing `dbPath` parameter for SQLite
- May have outdated user/credential structure
- ❌ **LIKELY DOES NOT COMPILE**

**Recommendation**:
- **Delete** these tests as duplicates of the better `internal/*/integration_test.go` tests
- **OR Update** them to match current architecture (significant work)

---

### ✅ Other Tests

**Location**: `pkg/compression/compression_test.go`
- ✅ **PASS** - Independent compression library tests

**Location**: `pkg/encryption/encryption_test.go`
- ✅ **PASS** - Independent encryption library tests

**Location**: `internal/metadata/badger_test.go`
- ✅ **PASS** - BadgerDB metadata store tests

**Location**: `tests/performance/benchmark_test.go`
- Status: **Unknown** - Performance benchmarks (may need updates)

---

## Running Tests

### Run All Working Tests

```bash
# Internal integration tests (RECOMMENDED)
go test -v ./internal/bucket/...
go test -v ./internal/object/...

# Metadata store tests
go test -v ./internal/metadata/...

# Package tests
go test -v ./pkg/compression/...
go test -v ./pkg/encryption/...
```

### Run Specific Test

```bash
# Bucket integration tests
go test -v -run TestBucketManagerIntegration ./internal/bucket/

# Object integration tests
go test -v -run TestObjectManagerBasicOperations ./internal/object/

# Multi-tenancy isolation
go test -v -run TestBucketManagerMultiTenant ./internal/bucket/
```

### Skip Failing Tests

```bash
# Skip S3 API tests (require auth setup)
go test -v ./tests/integration/api/... -skip "TestS3"

# Skip obsolete unit tests
go test -v ./tests/unit/... -skip "."
```

---

## Test Architecture

### Multi-Tenancy Test Pattern

All tests now follow this pattern:

```go
func setupIntegrationTest(t *testing.T) (Manager, func()) {
    // Create temp directory
    tempDir, _ := os.MkdirTemp("", "maxiofs-test-*")

    // Create storage backend
    storageBackend, _ := storage.NewFilesystemBackend(storage.Config{
        Root: tempDir,
    })

    // Create BadgerDB metadata store
    dbPath := filepath.Join(tempDir, "metadata")
    metadataStore, _ := metadata.NewBadgerStore(metadata.BadgerOptions{
        DataDir:           dbPath,
        SyncWrites:        true,
        CompactionEnabled: false,
        Logger:            logrus.StandardLogger(),
    })

    // Create manager with metadata store
    manager := bucket.NewManager(storageBackend, metadataStore)

    cleanup := func() {
        metadataStore.Close()
        os.RemoveAll(tempDir)
    }

    return manager, cleanup
}

func TestExample(t *testing.T) {
    manager, cleanup := setupIntegrationTest(t)
    defer cleanup()

    ctx := context.Background()
    tenantID := "test-tenant"  // Required for multi-tenancy

    // Create bucket with tenantID
    err := manager.CreateBucket(ctx, tenantID, "test-bucket")
    require.NoError(t, err)

    // All operations now require tenantID
    buckets, err := manager.ListBuckets(ctx, tenantID)
    require.NoError(t, err)
}
```

### Key Changes from Old Tests

1. **BadgerDB Metadata Store** - Required parameter for all managers
2. **TenantID Parameter** - All bucket/object operations require tenantID
3. **ListObjects Return** - Returns `*ListObjectsResult` instead of `([]Object, bool, error)`
4. **Auth Manager** - Requires `dbPath` parameter for SQLite database

---

## Test Coverage by Feature

| Feature | Coverage | Test Location | Status |
|---------|----------|---------------|--------|
| Bucket CRUD | ✅ Complete | `internal/bucket/integration_test.go` | ✅ PASS |
| Object CRUD | ✅ Complete | `internal/object/integration_test.go` | ✅ PASS |
| Versioning | ✅ Complete | `internal/bucket/integration_test.go` | ✅ PASS |
| Object Lock | ✅ Complete | `internal/object/integration_test.go` | ✅ PASS |
| Multipart Upload | ✅ Complete | `internal/object/integration_test.go` | ✅ PASS |
| Bucket Policy | ✅ Complete | `internal/bucket/integration_test.go` | ✅ PASS |
| Lifecycle | ✅ Complete | `internal/bucket/integration_test.go` | ✅ PASS |
| CORS | ✅ Complete | `internal/bucket/integration_test.go` | ✅ PASS |
| Tagging | ✅ Complete | `internal/object/integration_test.go` | ✅ PASS |
| Multi-Tenancy | ✅ Complete | `internal/bucket/integration_test.go` | ✅ PASS |
| Concurrency | ✅ Complete | `internal/bucket/integration_test.go` | ✅ PASS |
| Persistence | ✅ Complete | Both integration_test.go files | ✅ PASS |
| Compression | ✅ Complete | `pkg/compression/compression_test.go` | ✅ PASS |
| Encryption | ✅ Complete | `pkg/encryption/encryption_test.go` | ✅ PASS |
| S3 API Auth | ❌ Missing | N/A | ⚠️ TODO |
| 2FA | ❌ Missing | N/A | ⚠️ TODO |
| Prometheus Metrics | ❌ Missing | N/A | ⚠️ TODO |
| Quota Enforcement | ⚠️ Partial | Integration tests | ⚠️ NEEDS DEDICATED TEST |

---

## Recommendations

### Immediate Actions

1. **✅ Use `internal/*/integration_test.go`** - These are the primary, working tests
2. **❌ Delete or Skip** `tests/unit/` - Obsolete and don't compile
3. **⚠️ Fix or Skip** `tests/integration/api/s3_test.go` - Needs S3 auth setup

### Short Term (v0.4.0)

1. **Add S3 API Tests with Auth**:
   - Use AWS Go SDK with proper signature
   - Test presigned URLs
   - Test public bucket access
   - Test multi-tenancy via S3 API

2. **Add Feature-Specific Tests**:
   - Quota enforcement (storage, buckets, keys)
   - 2FA workflows
   - Session timeout
   - Rate limiting
   - Account lockout

3. **Add End-to-End Tests**:
   - Full user workflow (signup → bucket → upload → download)
   - Tenant creation → user creation → access key → S3 operations
   - Web console integration tests

### Long Term

1. **Increase Coverage** - Target 80%+ test coverage
2. **Add Performance Tests** - Update `tests/performance/benchmark_test.go`
3. **Add Load Tests** - Test with 100+ concurrent users
4. **Add Chaos Tests** - Test failure scenarios (disk full, network errors)

---

## Test File Organization

### Current Structure

```
MaxIOFS/
├── internal/
│   ├── bucket/
│   │   └── integration_test.go           ✅ WORKING
│   ├── object/
│   │   └── integration_test.go           ✅ WORKING
│   └── metadata/
│       └── badger_test.go                ✅ WORKING
├── pkg/
│   ├── compression/
│   │   └── compression_test.go           ✅ WORKING
│   └── encryption/
│       └── encryption_test.go            ✅ WORKING
└── tests/
    ├── integration/
    │   └── api/
    │       └── s3_test.go                ⚠️ NEEDS AUTH SETUP
    ├── unit/
    │   ├── bucket/
    │   │   ├── manager_test.go           ❌ OBSOLETE
    │   │   └── validation_test.go        ❌ OBSOLETE
    │   ├── object/
    │   │   └── manager_test.go           ❌ OBSOLETE
    │   ├── auth/
    │   │   └── manager_test.go           ❌ OBSOLETE
    │   └── storage/
    │       └── filesystem_test.go        ❌ OBSOLETE
    └── performance/
        └── benchmark_test.go             ⚠️ NEEDS REVIEW
```

### Recommended Structure (Future)

```
MaxIOFS/
├── internal/
│   └── */
│       └── *_test.go                     ✅ Unit tests (private API)
├── pkg/
│   └── */
│       └── *_test.go                     ✅ Unit tests (public API)
└── tests/
    ├── integration/
    │   ├── s3_auth_test.go               🆕 S3 with authentication
    │   ├── multitenancy_test.go          🆕 End-to-end tenancy
    │   └── console_test.go               🆕 Web console tests
    ├── e2e/
    │   ├── user_workflow_test.go         🆕 Full user flows
    │   └── tenant_workflow_test.go       🆕 Full tenant flows
    └── performance/
        ├── benchmark_test.go             ✅ Performance benchmarks
        └── load_test.go                  🆕 Load testing
```

---

## Writing New Tests

### Example: Multi-Tenancy Test

```go
func TestMultiTenancyIsolation(t *testing.T) {
    manager, cleanup := setupIntegrationTest(t)
    defer cleanup()

    ctx := context.Background()

    // Create buckets for different tenants
    tenant1 := "tenant-1"
    tenant2 := "tenant-2"

    err := manager.CreateBucket(ctx, tenant1, "bucket-1")
    require.NoError(t, err)

    err = manager.CreateBucket(ctx, tenant2, "bucket-2")
    require.NoError(t, err)

    // Verify isolation - tenant1 should only see bucket-1
    buckets1, err := manager.ListBuckets(ctx, tenant1)
    require.NoError(t, err)
    assert.Len(t, buckets1, 1)
    assert.Equal(t, "bucket-1", buckets1[0].Name)

    // Verify isolation - tenant2 should only see bucket-2
    buckets2, err := manager.ListBuckets(ctx, tenant2)
    require.NoError(t, err)
    assert.Len(t, buckets2, 1)
    assert.Equal(t, "bucket-2", buckets2[0].Name)
}
```

### Example: Quota Enforcement Test

```go
func TestQuotaEnforcement(t *testing.T) {
    om, bm, cleanup := setupObjectIntegrationTest(t)
    defer cleanup()

    ctx := context.Background()
    tenantID := "quota-tenant"
    bucketName := "quota-bucket"

    // Create tenant with 1MB quota (would need tenant manager)
    // For now, create bucket
    err := bm.CreateBucket(ctx, tenantID, bucketName)
    require.NoError(t, err)

    // Try to upload 2MB file (should fail if quota is 1MB)
    largeContent := make([]byte, 2*1024*1024) // 2MB
    headers := http.Header{}

    _, err = om.PutObject(ctx, bucketName, "large-file.bin",
        bytes.NewReader(largeContent), headers)

    // Should fail with quota exceeded error
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "quota")
}
```

---

## CI/CD Integration

### GitHub Actions Workflow (Recommended)

```yaml
name: Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Setup Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Run Integration Tests
        run: |
          go test -v -race -coverprofile=coverage.txt -covermode=atomic \
            ./internal/bucket/... \
            ./internal/object/... \
            ./internal/metadata/... \
            ./pkg/...

      - name: Upload Coverage
        uses: codecov/codecov-action@v3
        with:
          file: ./coverage.txt
```

---

## Troubleshooting

### BadgerDB Errors

**Error**: `Cannot create or access database`
**Solution**: Ensure temp directory has write permissions and enough disk space

### Transaction Conflicts

**Error**: `Transaction Conflict`
**Solution**: This is expected under high concurrency - tests handle this gracefully

### Auth Test Failures

**Error**: `403 AccessDenied`
**Solution**: Tests require either S3 signature auth or public bucket configuration

---

**Version**: 0.4.1-beta
**Last Updated**: November 12, 2025
