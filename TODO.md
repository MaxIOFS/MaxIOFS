# MaxIOFS - TODO & Roadmap

**Version**: 0.7.0-beta
**Last Updated**: January 27, 2026
**Status**: Beta - 98% S3 Compatible

## 📊 Project Status

- S3 API Compatibility: 98%
- Backend Test Coverage: 54.8% (500 tests) → Target: 90%+
- Frontend Test Coverage: 100% (64 tests)
- Features Complete: ~97%
- Production Ready: Testing Phase
- **⚠️ Cluster Production Viability: BLOCKED (2 critical architecture issues)**

## 🚨 CRITICAL BLOCKERS - CLUSTER ARCHITECTURE

### ⚠️ BLOCKER #1: ListBuckets Does NOT Aggregate Cross-Node (UX Breaking)
**Severity**: CRITICAL - Production Blocker
**Status**: 🔴 BLOCKS PRODUCTION DEPLOYMENT
**Discovered**: January 28, 2026

**Problem Description**:
When a tenant has buckets distributed across multiple cluster nodes, the web interface and S3 API only show buckets from the LOCAL node where the request lands.

**Current Broken Behavior**:
```
Scenario: 3-node cluster with load balancer
- Nodo-1 has: bucket-a
- Nodo-2 has: bucket-b
- Nodo-3 has: bucket-c

User connects via load balancer:
  → Request to Nodo-1: sees ONLY "bucket-a"
  → User refreshes: Request to Nodo-2: sees ONLY "bucket-b"
  → User refreshes: Request to Nodo-3: sees ONLY "bucket-c"

RESULT: User NEVER sees all 3 buckets simultaneously
        Inconsistent UX - buckets appear/disappear on refresh
```

**Root Cause Analysis**:
1. **Web API** (`internal/server/console_api.go:398`) - Lists only local BadgerDB
2. **S3 API** (`pkg/s3compat/handler.go:85`) - Lists only local BadgerDB
3. **BucketManager** (`internal/bucket/manager_badger.go`) - Queries local metadata store only
4. **MetadataStore** (`internal/metadata/badger.go`) - Iterates local BadgerDB only
5. **NO cross-node aggregation mechanism exists**

**Why It Happens**:
- Each node maintains independent BadgerDB instance
- `ListBuckets()` has NO cluster awareness
- `ClusterRouter` requires bucket name (can't be used for listing)
- `BucketLocationManager` only works when bucket name is already known

**Impact**:
- ❌ Web interface shows inconsistent bucket lists
- ❌ S3 clients (AWS CLI, boto3, s3cmd) see partial bucket lists
- ❌ Users cannot reliably discover all their buckets
- ❌ Breaks fundamental UX expectations
- ❌ Makes multi-node cluster unusable in practice

**Solution Required**: Implement `BucketAggregator` to query ALL nodes

---

### 🔥 BLOCKER #2: Tenant Storage Quotas Are NOT Cluster-Aware (SECURITY VULNERABILITY)
**Severity**: CRITICAL - Security Vulnerability
**Status**: 🔴 BLOCKS PRODUCTION DEPLOYMENT
**CVE Risk**: HIGH - Quota bypass vulnerability
**Discovered**: January 28, 2026

**Problem Description**:
Tenant storage quotas are enforced PER-NODE with 30-second sync intervals, allowing tenants to exceed quota by factor of N (number of nodes) during the sync window.

**Exploitable Attack Vector**:
```
Setup: 3-node cluster, Tenant quota = 1TB

ATTACK TIMELINE:
T=0s:  All nodes: current_storage_bytes = 0
T=1s:  Upload 1TB to Nodo-A → Quota check: 0 + 1TB ≤ 1TB ✓ ALLOWED
T=2s:  Upload 1TB to Nodo-B → Quota check: 0 + 1TB ≤ 1TB ✓ ALLOWED (stale value!)
T=3s:  Upload 1TB to Nodo-C → Quota check: 0 + 1TB ≤ 1TB ✓ ALLOWED (stale value!)
T=30s: Sync executes → Each node reports 1TB locally

RESULT: 3TB physically stored with 1TB quota
        200% QUOTA BYPASS
```

**Real-World Impact**:
```
Tenant with 1TB quota on 3-node cluster:
- Can store up to 3TB before ANY node detects the violation
- Quota shows 1TB but actual storage is 3TB
- Billing fraud potential
- Storage exhaustion risk
- No mechanism to detect or prevent
```

**Root Cause Analysis**:
1. **Local-Only Quota Checks** (`internal/auth/tenant.go:451-494`)
   - `CheckTenantStorageQuota()` queries LOCAL `current_storage_bytes` only
   - No distributed consensus or real-time sync

2. **30-Second Batch Sync** (`internal/cluster/tenant_sync.go:106`)
   - `syncAllTenants()` runs every 30 seconds
   - Each node OVERWRITES remote values during sync
   - Race condition window: 0-30 seconds

3. **Per-Node Storage Tracking** (`internal/auth/tenant.go:398-421`)
   - `IncrementTenantStorage()` updates ONLY local SQLite
   - No broadcast to other nodes
   - Stale reads guaranteed during sync intervals

4. **No Distributed Transactions**
   - No locks across nodes
   - No two-phase commit
   - No quota reservations

**Attack Success Rate**: 100% (deterministic bypass)

**Affected Code Paths**:
- `pkg/s3compat/handler.go:842` - PutObject quota validation
- `internal/object/manager.go:433` - Post-upload quota check
- `internal/object/manager.go:1472` - Multipart quota check
- `internal/auth/tenant.go:451` - Quota enforcement logic
- `internal/cluster/tenant_sync.go:106` - Sync mechanism

**Impact**:
- 🔥 Security vulnerability: Quota bypass exploit
- 🔥 Billing fraud potential for commercial deployments
- 🔥 Storage exhaustion risk (DoS via quota bypass)
- 🔥 Data integrity: Quota values never consistent
- 🔥 Compliance violation: Cannot enforce storage limits
- 🔥 Multi-tenancy broken: Tenants can steal resources

**Solution Required**: Implement Distributed Quota Counter with real-time sync

---

### 📋 Implementation Plan - 3 Phases

#### Phase 1: Bucket Aggregator (IMMEDIATE - 2-3 days)
**Priority**: P0 - Fixes UX blocker
**Complexity**: Low-Medium
**Breaking Changes**: None

**Deliverables**:
- [ ] Create `internal/cluster/bucket_aggregator.go`
  - `ListAllBuckets(ctx, tenantID)` - Queries all healthy nodes in parallel
  - `queryBucketsFromNode(ctx, node, tenantID)` - HTTP request to node
  - `BucketWithLocation` struct - Bucket + NodeID + NodeName

- [ ] Modify `internal/server/console_api.go:398`
  - Add cluster-aware branch: `if clusterManager.IsClusterEnabled()`
  - Use `bucketAggregator.ListAllBuckets()` for cluster mode
  - Fallback to `bucketManager.ListBuckets()` for standalone

- [ ] Modify `pkg/s3compat/handler.go:85`
  - Same cluster-aware logic for S3 ListBuckets API
  - Return aggregated results with metadata

- [ ] Update Web UI Response Format
  ```json
  {
    "buckets": [
      {
        "name": "bucket-a",
        "node_id": "node-1",
        "node_name": "Nodo Principal",
        "size_bytes": 483729408000,
        "object_count": 12453
      }
    ],
    "total": 3,
    "cluster_mode": true
  }
  ```

- [ ] Tests
  - `TestBucketAggregator_ListAllBuckets` - Multi-node aggregation
  - `TestBucketAggregator_NodeFailure` - Handles node down gracefully
  - `TestBucketAggregator_EmptyCluster` - No buckets scenario
  - Integration test with 3 simulated nodes

**Success Criteria**:
- ✅ User sees ALL buckets regardless of which node serves request
- ✅ Web UI displays node location for each bucket
- ✅ S3 API returns complete bucket list
- ✅ Performance impact < 100ms for 3-node cluster

---

#### Phase 2: Distributed Quota Counter (CRITICAL - 4-5 days)
**Priority**: P0 - Fixes security vulnerability
**Complexity**: High
**Breaking Changes**: Database schema addition

**Deliverables**:
- [ ] Create `internal/cluster/distributed_quota.go`
  - `DistributedQuotaManager` struct with distributed locks
  - `ReserveQuota(ctx, tenantID, bytes)` - Reserve quota before upload
  - `CommitReservation(ctx, reservationID)` - Commit after successful upload
  - `ReleaseReservation(ctx, reservationID)` - Release on upload failure
  - `queryTotalUsageAcrossCluster(ctx, tenantID)` - Real-time aggregation
  - `broadcastReservation(ctx, reservation)` - Notify all nodes

- [ ] Database Schema Addition
  ```sql
  CREATE TABLE cluster_quota_reservations (
      id TEXT PRIMARY KEY,
      tenant_id TEXT NOT NULL,
      node_id TEXT NOT NULL,
      bytes INTEGER NOT NULL,
      status TEXT DEFAULT 'active',  -- active, committed, expired, released
      created_at INTEGER NOT NULL,
      expires_at INTEGER NOT NULL,   -- Auto-expire after 5 minutes
      FOREIGN KEY (tenant_id) REFERENCES tenants(id)
  );

  CREATE INDEX idx_quota_res_tenant ON cluster_quota_reservations(tenant_id);
  CREATE INDEX idx_quota_res_status ON cluster_quota_reservations(status);
  CREATE INDEX idx_quota_res_expires ON cluster_quota_reservations(expires_at);
  ```

- [ ] Modify Upload Handlers
  - `pkg/s3compat/handler.go:842` - Add reservation logic to PutObject
  - `internal/object/manager.go:419` - Add reservation to object manager
  - `internal/object/manager.go:1451` - Add reservation to multipart

- [ ] Distributed Lock Implementation (Choose one):
  **Option A: SQLite-based (simpler, good for < 10 nodes)**
  ```sql
  CREATE TABLE cluster_locks (
      resource_key TEXT PRIMARY KEY,
      node_id TEXT NOT NULL,
      acquired_at INTEGER NOT NULL,
      expires_at INTEGER NOT NULL
  );
  ```

  **Option B: Redis-based (recommended for production)**
  - Use Redis SET NX EX for distributed locks
  - Add Redis as optional dependency
  - Fallback to SQLite if Redis unavailable

- [ ] Real-Time Usage Sync
  - Replace 30-second batch sync with event-driven broadcast
  - HTTP endpoint: `POST /api/internal/cluster/quota-update`
  - Broadcast on every `IncrementTenantStorage()`/`DecrementTenantStorage()`

- [ ] Background Cleanup Worker
  - Expire reservations after 5 minutes
  - Reconcile quota discrepancies
  - Alert on quota inconsistencies

**Tests**:
- [ ] `TestDistributedQuota_ReserveAndCommit` - Happy path
- [ ] `TestDistributedQuota_ReserveAndRelease` - Upload failure
- [ ] `TestDistributedQuota_QuotaExceeded` - Quota enforcement
- [ ] `TestDistributedQuota_ParallelUploads` - Race conditions
- [ ] `TestDistributedQuota_NodeFailure` - Node down during reservation
- [ ] `TestDistributedQuota_ExpirationCleanup` - Expired reservations
- [ ] Integration test: 3 nodes, parallel uploads, verify no quota bypass

**Security Testing**:
- [ ] Penetration test: Attempt quota bypass with parallel uploads
- [ ] Load test: 1000 concurrent uploads to verify no race conditions
- [ ] Chaos test: Kill nodes during upload to verify reservation cleanup

**Success Criteria**:
- ✅ ZERO quota bypass possible regardless of timing
- ✅ Quota enforcement < 1 second delay across cluster
- ✅ No data loss on node failures
- ✅ Automatic cleanup of stale reservations
- ✅ Performance impact < 50ms per upload

---

#### Phase 3: Production Hardening (1-2 days)
**Priority**: P0 - Required for production
**Complexity**: Medium

**Deliverables**:
- [ ] Monitoring & Alerts
  - Prometheus metrics: `cluster_quota_reservations_active`
  - Alert on quota discrepancies > 5%
  - Alert on reservation cleanup failures

- [ ] Audit Logging
  - Log all quota reservations with node ID
  - Log quota bypass attempts
  - Track quota changes across nodes

- [ ] Documentation
  - Update `docs/CLUSTER.md` with quota architecture
  - Add troubleshooting guide for quota issues
  - Document Redis setup (optional)

- [ ] Migration Path
  - Database migration for `cluster_quota_reservations` table
  - Backward compatibility for existing deployments
  - Rollback procedure documented

**Success Criteria**:
- ✅ Complete monitoring coverage
- ✅ Clear documentation for operators
- ✅ Zero-downtime migration path

---

### 📊 Timeline & Resources

**Estimated Total Effort**: 7-10 days
**Required Skills**: Go, distributed systems, SQL, HTTP, testing
**Dependencies**: None (can start immediately)

**Proposed Schedule**:
- **Week 1**: Phase 1 (Bucket Aggregator) - Days 1-3
- **Week 2**: Phase 2 (Distributed Quota) - Days 4-8
- **Week 2**: Phase 3 (Production Hardening) - Days 9-10

**Risk Assessment**:
- **Technical Risk**: Medium (distributed systems complexity)
- **Testing Risk**: Low (comprehensive test plan)
- **Deployment Risk**: Low (backward compatible)

---

### 🔗 Technical References

**Affected Files**:
- `internal/server/console_api.go:398` - Web bucket listing
- `pkg/s3compat/handler.go:85` - S3 ListBuckets handler
- `internal/bucket/manager_badger.go` - Bucket manager implementation
- `internal/metadata/badger.go` - Metadata store queries
- `internal/auth/tenant.go:451-494` - Quota enforcement
- `internal/cluster/tenant_sync.go:106` - Tenant sync mechanism
- `internal/cluster/router.go:115` - Cluster routing (not used for ListBuckets)
- `internal/cluster/bucket_location.go:57` - Bucket location tracking

**Database Tables**:
- `tenants` - Contains `max_storage_bytes`, `current_storage_bytes` (local-only)
- `cluster_quota_reservations` - NEW table for distributed quota tracking
- `cluster_locks` - NEW table for distributed locks (SQLite option)

**Investigation Reports**:
- Investigation Date: January 28, 2026
- Investigation Agent ID: a93235f (ListBuckets cross-node)
- Investigation Agent ID: a5cbb7e (Quota enforcement)

---

## 📌 Current Sprint

### Sprint 9: Critical Cluster Architecture Fixes - 🚨 BLOCKED
**Goal**: Fix 2 critical cluster architecture issues that block production deployment

**Status**: ⚠️ PRODUCTION BLOCKER - Must complete before v0.8.0 release

**Issues**:
1. 🔴 ListBuckets does NOT aggregate cross-node (UX blocker)
2. 🔥 Tenant quotas are NOT cluster-aware (security vulnerability - CVE risk)

**Timeline**: 7-10 days (3 phases)
- Phase 1: Bucket Aggregator (2-3 days) - P0
- Phase 2: Distributed Quota Counter (4-5 days) - P0
- Phase 3: Production Hardening (1-2 days) - P0

**See**: 🚨 CRITICAL BLOCKERS section above for full technical details

---

### Sprint 8: Backend Test Coverage Expansion (54.8% → 90%+) - ⏸️ PAUSED
**Goal**: Systematically test all modules to reach production-ready coverage levels

**Status**: PAUSED - Blocked by Sprint 9 cluster architecture fixes

**Approach**:
- Test modules in priority order (0% → 90%+ coverage)
- Focus on core functionality, edge cases, and error handling
- No skipping of complex or difficult test scenarios
- Comprehensive integration tests for cross-module interactions

**Target**: Add 400-500 new tests across 15+ modules

**Progress So Far**:
- ✅ internal/config: 35.8% → 94.0% (+58.2 points, 23 tests)
- ✅ internal/cluster: 32.7% → 64.0% (+31.3 points, 44 tests)
  - tenant_sync_test.go (12 tests)
  - manager_test.go (6 new tests)
  - proxy_test.go (7 tests - new file)
  - replication_worker_test.go (11 tests - new file)
  - migration_test.go (8 new tests)
- 🚧 internal/cluster: Remaining 9 migration functions pending
- ⏸️ cmd/maxiofs: 0% → 90%+ (paused)
- ⏸️ web: 0% → 90%+ (paused)

### Sprint 7: Complete Bucket Migration & Data Synchronization - ✅ COMPLETE
- [x] ✅ Real object copying from physical storage (fixed empty body bug)
- [x] ✅ Bucket permissions migration (user and tenant permissions)
- [x] ✅ Bucket ACL migration (from BadgerDB)
- [x] ✅ Bucket configuration migration (versioning, lifecycle, tags, CORS, encryption, policy, notification)
- [x] ✅ Access key synchronization system (AccessKeySyncManager with 6 tests)
- [x] ✅ Bucket permission synchronization system (BucketPermissionSyncManager with 7 tests)
- [x] ✅ Database schema updates (cluster_access_key_sync, cluster_bucket_permission_sync tables)
- [x] ✅ Server integration (sync managers lifecycle, route registration)
- [x] ✅ HMAC-authenticated sync endpoints
- [x] ✅ Comprehensive testing (13 new tests, 100% pass rate)
- [x] ✅ Documentation updates (CHANGELOG.md)

### Sprint 6: Cluster Bucket Migration - ✅ COMPLETE
- [x] ✅ Database schema for migration tracking (cluster_migrations table)
- [x] ✅ MigrationJob model with full CRUD operations
- [x] ✅ Migration orchestration (MigrateBucket method)
- [x] ✅ Node health validation and error handling
- [x] ✅ Detailed logging throughout migration process
- [x] ✅ BucketLocationManager (get/set/initialize location with caching)
- [x] ✅ Location storage in bucket metadata (cluster:location key)
- [x] ✅ Integration of BucketLocationManager with migration flow
- [x] ✅ Object counting and size calculation (countBucketObjects)
- [x] ✅ Object copying with progress tracking (copyBucketObjects)
- [x] ✅ Data integrity verification (verifyMigration with ETag validation)
- [x] ✅ REST API endpoints (POST /api/v1/cluster/buckets/{bucket}/migrate)
- [x] ✅ REST API endpoints (GET /api/v1/cluster/migrations, GET /api/v1/cluster/migrations/{id})
- [x] ✅ Frontend UI for migration management (/cluster/migrations page with filters, progress tracking)
- [x] ✅ Documentation updates (CLUSTER.md updated with migration guide)
- [x] ✅ Backend tests (TestMigrationJobCRUD, TestGetMigrationJob_NotFound)

### Sprint 5: Production Readiness & Stability - ✅ COMPLETE
- [x] ✅ Expand test coverage for internal/auth (30.2% → **47.1% coverage**, +16.9 points, 56% improvement)
  - 13 new test functions with 80+ test cases for S3 authentication
  - s3auth.go coverage: 83-100% across all functions
  - Fixed 4 critical bugs in SigV4/SigV2 authentication

## 🔴 HIGH PRIORITY

### Backend Test Coverage Expansion - 🚧 IN PROGRESS (Sprint 8)
**Goal**: Increase backend test coverage from 54.8% to 90%+ (Target: +35.2 percentage points)

**Current Status**: 54.8% coverage (500 tests), 354 functions with 0% coverage

**Critical Commitment**:
- Test ALL modules systematically without skipping difficult/complex tests
- Focus on core functionality and edge cases
- No shortcuts on complicated scenarios

**Priority Order** (by coverage % and criticality):

#### Phase 1: Critical Infrastructure (0-40% coverage)
- [ ] **cmd/maxiofs** - 0.0% coverage (3 functions: main, runServer, setupLogging)
  - Integration tests for CLI initialization and server startup
  - Configuration loading and validation tests
  - Logging setup verification tests

- [ ] **web** - 0.0% coverage (2 functions: GetFrontendFS, embed)
  - Frontend file serving tests
  - Embedded filesystem tests

- [ ] **internal/cluster** - 32.7% coverage (70+ functions with 0%)
  - Files needing tests:
    - `migration.go` (15 functions) - Bucket migration orchestration
    - `user_sync.go` (11 functions) - User synchronization across nodes
    - `tenant_sync.go` (11 functions) - Tenant synchronization
    - `replication_worker.go` (10 functions) - Cluster replication worker
    - `manager.go` (9 functions) - Cluster manager operations
    - `proxy.go` (8 functions) - Request proxying between nodes

- [ ] **internal/config** - 35.8% coverage
  - Configuration parsing and validation
  - Environment variable loading
  - Default value handling
  - Configuration migration/upgrade tests

#### Phase 2: Core Functionality (40-50% coverage)
- [ ] **internal/bucket** - 45.9% coverage (20+ functions with 0%)
  - Files needing tests:
    - `manager_badger.go` (7 functions) - ForceDeleteBucket, RecalculateMetrics, ACL operations
    - `validation.go` (9 functions) - Policy, versioning, lifecycle, object lock validation
    - `filter.go` (3 functions) - Permission filtering and access checks

- [ ] **pkg/s3compat** - 45.7% coverage (42+ functions with 0%)
  - Files needing tests:
    - `handler.go` (24 functions) - S3 API request handlers
    - `presigned.go` (13 functions) - Pre-signed URL generation and validation
    - `batch.go` (5 functions) - Batch operations (DeleteObjects)

- [ ] **internal/object** - 48.2% coverage (28+ functions with 0%)
  - Files needing tests:
    - `manager.go` (15 functions) - Object operations
    - `retention.go` (9 functions) - Object retention and compliance mode
    - `lock.go` (4 functions) - Object locking mechanisms

#### Phase 3: Storage & Metadata (50-55% coverage)
- [ ] **internal/metadata** - 52.4% coverage (32+ functions with 0%)
  - Files needing tests:
    - `badger.go` (14 functions) - BadgerDB operations
    - `badger_objects.go` (4 functions) - Object metadata operations
    - `badger_multipart.go` (4 functions) - Multipart upload metadata
    - `badger_buckets.go` - Bucket metadata operations

- [ ] **internal/server** - 53.0% coverage (30+ functions with 0%)
  - Files needing tests:
    - `server.go` (13 functions) - Server lifecycle and initialization
    - `console_api_logs.go` (4 functions) - Frontend log aggregation
    - `profiling_handlers.go` (5 functions) - pprof endpoint handlers

- [ ] **internal/replication** - 54.0% coverage (17+ functions with 0%)
  - Files needing tests:
    - `s3client.go` (9 functions) - S3 remote client operations
    - `adapter.go` (4 functions) - Object adapter for replication
    - `worker.go` - Replication worker operations

#### Phase 4: Authentication & Authorization (60-75% coverage)
- [ ] **internal/auth** - 71.0% coverage (25+ functions with 0%)
  - Files needing tests:
    - `manager.go` (12 functions) - Middleware, bucket permissions, 2FA operations
    - `sqlite.go` (13 functions) - Database schema, migrations, 2FA persistence

#### Phase 5: Supporting Systems (55-70% coverage)
- [ ] **internal/metrics** - 67.4% coverage (29 functions with 0%)
  - Files needing tests:
    - `manager.go` (29 functions) - Metrics collection and management

- [ ] **pkg/encryption** - 57.1% coverage (7 functions with 0%)
  - Encryption/decryption operations
  - Key derivation and management
  - Stream encryption tests

- [ ] **pkg/compression** - 61.2% coverage (9 functions with 0%)
  - Gzip compression/decompression
  - Stream compression tests
  - Error handling for corrupted data

- [ ] **internal/audit** - 62.8% coverage (4 functions with 0%)
  - `StartRetentionJob`, `runRetentionCleanup` - Audit log retention
  - Long-running background job tests

- [ ] **internal/share** - 63.5% coverage
  - Share link creation and validation
  - Expiration handling
  - Access control tests

- [ ] **internal/lifecycle** - 69.0% coverage
  - Lifecycle rule application
  - Object expiration worker
  - Edge cases and error scenarios

**Testing Guidelines**:
1. **No Skipping**: Do not skip complex or time-consuming tests
2. **Edge Cases**: Test error conditions, boundary values, concurrent access
3. **Integration**: Test interactions between modules
4. **Performance**: Include performance regression tests where applicable
5. **Race Conditions**: Run tests with `-race` flag
6. **Cleanup**: Ensure proper resource cleanup in all tests

**Tracking**: Update this list as each module reaches 90%+ coverage

---

### Cluster Bucket Migration - ✅ COMPLETE (100%)
**Goal**: Enable moving buckets between cluster nodes for capacity rebalancing and maintenance

**Phase 1: Core Infrastructure** (✅ Complete)
- ✅ Database schema (cluster_migrations table with indexes)
- ✅ MigrationJob model and persistence layer
- ✅ Migration orchestration framework (MigrateBucket method)
- ✅ Node validation (health checks, same-node prevention)
- ✅ Error handling and status tracking
- ✅ Comprehensive logging

**Phase 2: Integration & Implementation** (✅ Complete)
- ✅ BucketLocationManager created (get/set/initialize location)
- ✅ Two-level caching system (memory + BadgerDB metadata)
- ✅ Location storage in bucket metadata (cluster:location key)
- ✅ Automatic cache invalidation on updates
- ✅ Integration of BucketLocationManager with migration flow
- ✅ Object copying between nodes (copyBucketObjects method)
- ✅ Object counting and size calculation (countBucketObjects method)
- ✅ Data integrity verification with ETag validation (verifyMigration method)
- ✅ Progress tracking (updates every 10 objects)
- ✅ Error handling with retry logic
- ✅ Optional source deletion (after verification)

**Phase 3: API & Frontend** (✅ Complete)
- ✅ REST API: `POST /api/v1/cluster/buckets/{bucket}/migrate`
- ✅ REST API: `GET /api/v1/cluster/migrations` (list all migrations)
- ✅ REST API: `GET /api/v1/cluster/migrations/{id}` (get specific migration)
- ✅ Frontend: Migration initiation UI (/cluster/migrations page)
- ✅ Frontend: Real-time progress visualization (progress bars, status badges)
- ✅ Frontend: Migration history view (with filters: All, Active, Completed, Failed)
- ✅ Frontend: Source/target node validation (prevents same-node migration)
- ✅ Frontend: Consistent UI design with other cluster pages

**Phase 4: Testing & Documentation** (✅ Complete)
- ✅ Unit tests for migration logic (TestMigrationJobCRUD, TestGetMigrationJob_NotFound)
- ✅ Integration tests (cluster test suite passing)
- ✅ Documentation: docs/CLUSTER.md updates (migration guide added)
- ✅ User guide for migration scenarios (use cases documented)

**Use Cases**:
- Capacity rebalancing (move buckets from full nodes)
- New node onboarding (migrate instead of sync)
- Node maintenance (empty before removal)
- Performance optimization (hot buckets to fast nodes)

### Performance Profiling & Optimization (v0.6.1) - ✅ COMPLETE
- ✅ Sprint 2: Load Testing Infrastructure (k6 test suite, Makefile integration, documentation)
- ✅ Sprint 3: Performance Analysis (Windows/Linux baselines, bottleneck identification, optimization)
- ✅ Sprint 4: Production Monitoring (Complete - Frontend, Prometheus, Grafana, Alerts, SLOs)
- ✅ Build Requirements Update (Node.js 24+, Go 1.25+)
- ✅ Frontend Dependencies Upgrade (Tailwind v4, Vitest v4)
- ✅ S3 Test Suite Expansion (+42 tests, 30.9% → 45.7% coverage)
- ✅ Server Integration Tests (+4 tests, 12.7% → 18.3% coverage)

### Bucket Replication & Cluster Management (v0.5.0 - v0.6.0) - ✅ COMPLETE
- ✅ Phase 1: S3-compatible replication (Backend CRUD, queue infrastructure, SQLite persistence, retry logic, scheduler)
- ✅ Phase 2: Cluster management (SQLite schema, health checker, smart router, failover, proxy mode)
- ✅ Phase 3: Cluster Dashboard UI (Frontend integration, TypeScript types, API client, status overview)
- ✅ Phase 4: Testing & documentation (27 cluster tests passing, CLUSTER.md complete with 2136 lines)

## 🟡 MEDIUM PRIORITY

### Test Coverage Expansion
- [x] pkg/s3compat (30.9% → **45.7% coverage** ✅) - **42 tests added** (+14.8 points, 48% improvement)
- [x] internal/server (12.7% → **18.3% coverage** ✅) - **4 integration tests added** (+5.6 points, 44% improvement)
- [x] internal/auth (30.2% → **47.1% coverage** ✅) - **13 test functions added** (+16.9 points, 56% improvement)
  - Comprehensive S3 signature authentication tests (SigV4, SigV2, Bearer tokens)
  - Fixed 4 critical bugs in authentication implementation
- [x] internal/metrics (25.8% → **36.2% coverage** ✅) - **102 test functions created** (+10.4 points, 40.3% improvement)
  - System metrics tracking tests (28 tests: CPU, memory, disk, uptime, requests, concurrent safety)
  - Collector tests (17 tests: system/runtime metrics, background collection, concurrent operations)
  - History store tests (29 tests for SQLite, 28 tests for BadgerDB - *pending DB refactoring*)

### Integration Testing
- [x] ✅ **Bucket Migration End-to-End Testing**
  - 3 integration tests with simulated nodes (47 total cluster tests passing)
  - TestBucketMigrationEndToEnd (5 sub-tests: objects, ACLs, permissions, config, inventory)
  - TestMigrationDataIntegrity (210KB data verification)
  - TestMigrationErrorHandling (3 sub-tests: invalid path, JSON, HMAC)

### Improvements & Optimization
- [x] **Memory/CPU Profiling** - Performance benchmarking infrastructure ✅
  - Go benchmarks for authentication, storage, and encryption operations (36 benchmarks total)
  - Makefile targets (`make bench`, `make bench-profile`) for local execution
  - CI/CD integration in nightly builds with automated benchmark results
  - pprof endpoints (`/debug/pprof/*`) for production profiling (admin-only)
- [x] **Database Migrations Versioning** - Schema version control ✅
  - Migration system with version tracking (8 migrations from v0.1.0 to v0.6.2)
  - Automatic schema upgrades on application startup
  - 18 comprehensive tests with 100% pass rate
  - Transaction-based migrations for data integrity

## 🟢 LOW PRIORITY

### Bucket Inventory - ✅ COMPLETE
- [x] Database schema (configs + reports tables)
- [x] Backend (worker, generator, manager)
- [x] REST API (PUT/GET/DELETE config, GET reports)
- [x] Cluster migration integration
- [x] Frontend UI (config form, reports history)
- [x] Tests (11 functions, 100% coverage)

### Other Low Priority Features
- [ ] Object Metadata Search - Full-text search capability
- [ ] Official Docker Hub Images - Public registry
- [ ] Additional Storage Backends - S3, GCS, Azure blob

## ✅ COMPLETED FEATURES

### Recent Completed Work
- ✅ **Bucket Inventory System** (v0.7.0)
- ✅ **Complete Bucket Migration & Data Synchronization** (Sprint 7)
- ✅ **Performance Profiling & Benchmarking** (v0.7.0)
- ✅ **Database Migration System** (v0.7.0)
- ✅ **Metrics Test Suite Expansion** (v0.7.0)

### v0.7.0-beta (Current)
- ✅ **Bucket Inventory System** - Automated inventory report generation
  - S3-compatible inventory reports with daily/weekly schedules
  - CSV and JSON output formats with 12 configurable fields
  - REST API endpoints for configuration and report management
  - Frontend UI with inventory tab in bucket settings
  - Cluster migration integration for inventory configs
  - 11 comprehensive tests (100% coverage)
- ✅ **Database Migration System** - Comprehensive schema versioning
  - Migration framework with automatic execution on startup
  - 8 historical migrations from v0.1.0 to v0.6.2
  - Version tracking with `schema_version` table
  - Transaction-based migrations for data integrity
  - 18 comprehensive tests (100% pass rate)
- ✅ **Performance Profiling & Benchmarking** - Production monitoring infrastructure
  - 36 Go benchmarks for storage, encryption, and authentication
  - CI/CD integration in nightly builds with automated results
  - Cross-platform Makefile targets (`make bench`, `make bench-profile`)
  - pprof endpoints (`/debug/pprof/*`) for production profiling
- ✅ **Metrics Test Suite Expansion** - Improved monitoring coverage
  - 102 total test functions created (45 active, 57 pending DB refactoring)
  - System metrics tests (28 tests for CPU, memory, disk, requests)
  - Collector tests (17 tests for metrics collection and health)
  - Test coverage improvement: 25.8% → 36.2% (+10.4 points, +40.3%)
- ✅ **CI/CD Improvements** - Enhanced build pipeline
  - RPM package generation for RHEL/CentOS/Fedora (AMD64 + ARM64)
  - Docker-based RPM builds using Rocky Linux 9
  - Fixed permission issues in artifact preparation
  - Automated benchmark execution and S3 upload
- ✅ Console API Documentation Fixes (GitHub Issues #2 and #3)
  - Fixed all API endpoint documentation (corrected `/api/` to `/api/v1/` prefix)
  - Added `GET /api/v1/` root endpoint (returns API information in JSON)
  - Updated `docs/API.md`, `docs/CLUSTER.md`, `docs/MULTI_TENANCY.md`
- ✅ LICENSE File Addition (MIT License added to repository root)
- ✅ AWS-Compatible Access Key Format
  - Access Key ID: AKIA + 16 uppercase alphanumeric (AWS standard format)
  - Secret Access Key: 40-character base64 encoding (AWS standard format)
  - Backward compatible with existing access keys

### v0.6.1-beta
- ✅ Build Requirements Update (Node.js 24+, Go 1.25+)
- ✅ Tailwind CSS v3 → v4 Migration (10x faster Oxide engine)
- ✅ Vitest v3 → v4 Migration (59% faster test execution)
- ✅ S3 API Test Suite Expansion (+42 tests, coverage: 30.9% → 45.7%)
- ✅ Server Integration Tests (+4 tests, coverage: 12.7% → 18.3%)
- ✅ Auth Module Test Suite (+13 test functions, coverage: 30.2% → 47.1%)
  - S3 signature authentication tests (SigV4, SigV2, Bearer tokens, pre-signed URLs)
  - Fixed 4 critical authentication bugs (parseV4Authorization, ValidateTimestamp, GetResourceARN)
- ✅ Docker Infrastructure Improvements (organized config, profiles, unified dashboard)
- ✅ Documentation Accuracy Fixes (React framework references)
- ✅ UI Bug Fixes (Tailwind v4 opacity syntax compatibility)
- ✅ Code Cleanup (removed unused Next.js server code)

### v0.6.0-beta
- ✅ Cluster Management System (multi-node coordination, health monitoring, smart routing)
- ✅ Performance Metrics Collection (latency percentiles, throughput tracking, operation tracing)
- ✅ Load Testing Infrastructure (k6 test suite, performance baselines)
- ✅ Frontend Performance Dashboard (real-time metrics visualization)
- ✅ Prometheus Integration (9 performance metrics, /metrics endpoint)
- ✅ Grafana Dashboard (7 visualization panels, auto-refresh)
- ✅ Performance Alerting (14 Prometheus alert rules)
- ✅ SLO Documentation (5 core SLOs with targets and baselines)

### v0.5.0
- ✅ Bucket Replication (S3-compatible cross-bucket replication)
- ✅ Multi-tenant Improvements (tenant isolation, global admin controls)

### v0.4.0
- ✅ Dynamic Settings System (runtime configuration without restarts)
- ✅ Server-side Encryption (AES-256-CTR streaming)
- ✅ Comprehensive Audit Logging
- ✅ Two-Factor Authentication (2FA with TOTP)
- ✅ Bucket Notifications (webhooks on S3 events)

### v0.3.0
- ✅ Bucket Versioning (multiple versions, delete markers)
- ✅ Lifecycle Policies (expiration, noncurrent version cleanup)
- ✅ Object Lock (COMPLIANCE/GOVERNANCE modes)
- ✅ Bulk Operations (DeleteObjects batch delete)

### v0.2.0
- ✅ Bucket Policy (complete PUT/GET/DELETE)
- ✅ Bucket CORS (visual UI editor)
- ✅ Bucket Tagging (visual UI manager)
- ✅ Object Tagging & ACL
- ✅ Presigned URLs (GET/PUT with expiration)

### v0.1.0
- ✅ Core S3 Operations (PutObject, GetObject, DeleteObject, ListObjects)
- ✅ Bucket Management (Create, List, Delete)
- ✅ Multipart Uploads
- ✅ Web Console UI
- ✅ Multi-tenancy Support
- ✅ SQLite + BadgerDB Storage

---

## 🗺️ Long-Term Roadmap

### Short Term (v0.8.0-beta - Q1 2026)

**Testing & Quality**
- [x] 🚧 Backend test coverage expansion to 90%+ (current: 54.8%, target: 90%+) - **IN PROGRESS (Sprint 8)**
  - Comprehensive test plan created with 15+ modules prioritized
  - Systematic testing approach: 0% → 90%+ coverage per module
  - 354 functions identified with 0% coverage
  - Commitment to test complex/difficult scenarios without skipping
- [ ] Chaos engineering tests (node failures, network partitions)
- [x] ✅ End-to-end integration test suite for multi-node cluster scenarios
  - Implemented in `internal/cluster/replication_integration_test.go` and `migration_integration_test.go`
  - SimulatedNode framework with 47 cluster tests passing
  - Bucket migration E2E tests with data integrity verification

**Observability Enhancements**
- [ ] Distributed tracing integration (OpenTelemetry/Jaeger) - for cross-service tracing
- [x] ✅ Request correlation IDs across cluster nodes
  - Implemented in `internal/middleware/tracing.go`
  - Automatic trace ID generation (UUID) for each request
  - Context propagation with GetTraceID(), GetStartTime(), GetOperation() helpers
  - JSON logging with trace_id field
- [x] ✅ Advanced query capabilities for historical metrics
  - Implemented in `internal/metrics/history.go`
  - SQLite storage with temporal indexes
  - Hourly aggregation for long-term storage (>7 days)
  - Intelligent queries with automatic optimization
  - Retention policy (365 days default, configurable)
  - API endpoint `/api/v1/metrics/history`

**Documentation**
- [x] ⚠️ Complete API reference documentation with OpenAPI/Swagger spec
  - `docs/API.md` provides exhaustive manual documentation (577 lines)
  - Documents 45+ endpoints with examples (curl, Python boto3, AWS CLI)
  - Missing: Auto-generated OpenAPI/Swagger JSON specification
- [ ] Video tutorials and getting started guides
- [ ] Migration guides from MinIO/AWS S3 to MaxIOFS
- [x] ✅ Troubleshooting playbooks for common issues
  - `docs/TESTING.md` - Test troubleshooting (467 lines)
  - `docs/CLUSTER.md` - Cluster setup and issues
  - `docs/PERFORMANCE.md` - Performance analysis (405 lines)
  - `docs/DEPLOYMENT.md` - Deployment scenarios
  - `docs/SECURITY.md` - Security configuration

### Medium Term (v0.9.0-beta - Q2 2026)

**Advanced Storage Features**
- [ ] Storage tiering (hot/warm/cold with automatic transitions)
- [ ] Deduplication (hash-based duplicate detection)
- [ ] Erasure coding for fault tolerance

**Enterprise Features**
- [ ] LDAP/Active Directory integration
- [ ] SAML/OAuth2 SSO support
- [ ] Advanced billing and usage reports per tenant

**Cluster Enhancements**
- [ ] Automatic cluster scaling (add/remove nodes dynamically)
- [ ] Geographic distribution with latency-based routing
- [ ] Cross-datacenter replication with conflict resolution
- [ ] Consensus-based configuration (Raft/etcd integration)
- [ ] Split-brain prevention mechanisms

**Performance Optimizations**
- [ ] Read-through caching with Redis/Memcached
- [ ] CDN integration for static object delivery
- [ ] Delta sync for large object updates

### Long Term (v1.0.0+ - Q4 2026 and beyond)

**Production Readiness**
- [ ] Third-party security audit completion
- [ ] 90%+ test coverage across all modules (current: 36.2%)
- [ ] 6+ months production usage validation (tracking started January 2026)
- [ ] Zero critical bugs policy enforcement
- [ ] Performance validated at enterprise scale (100K+ objects, 1M+ target)
- [ ] High availability clustering validation (multi-node failover scenarios)

**Alternative Storage Backends**
- [ ] PostgreSQL metadata backend option
- [ ] MySQL/MariaDB metadata support
- [ ] Distributed key-value stores (etcd, Consul)
- [ ] Cloud storage backends (AWS S3, GCS, Azure Blob)

**Advanced S3 Compatibility**
- [ ] S3 Batch Operations API
- [ ] S3 Analytics and insights (usage patterns, access frequency)
- [ ] S3 Intelligent-Tiering automation
- [ ] S3 Glacier storage class

**Platform Expansion**
- [ ] Kubernetes operator for automated deployment
- [ ] Helm charts for production clusters
- [ ] Terraform/Pulumi providers
- [ ] Cloud marketplace listings (AWS, Azure, GCP)
- [ ] SaaS multi-tenant platform

**Ecosystem Integration**
- [ ] Enhanced backup tool integrations (Restic, Duplicati, Bacula)
- [ ] Media server optimizations (Plex, Jellyfin, Emby)
- [ ] Data pipeline integration (Apache Kafka, Spark, Flink)
- [ ] BI tool connectors (Tableau, PowerBI, Looker)

### Target Release Schedule

- **v0.7.0-beta**: ✅ January 2026 - Monitoring & Performance Phase (COMPLETED)
- **v0.8.0-beta**: March 2026 - Testing & Documentation Phase
- **v0.9.0-beta**: June 2026 - Enterprise & Advanced Features Phase
- **v1.0.0-rc1**: September 2026 - Release Candidate (security audit complete)
- **v1.0.0**: November 2026 - Production Stable Release

---

## 📝 Notes

**Already Implemented** (previously listed as future work):
- ✅ Compression support (gzip) - pkg/compression with streaming
- ✅ Object immutability - Object Lock GOVERNANCE/COMPLIANCE modes
- ✅ Advanced RBAC - Custom bucket policies (S3-compatible JSON)
- ✅ Tenant resource quotas - MaxStorageBytes, MaxBuckets, MaxAccessKeys
- ✅ Multi-region replication - Cluster + S3 replication
- ✅ Parallel multipart upload - Full multipart API
- ✅ Complete ACL system - Canned ACLs + custom grants
- ✅ Real-time dashboards - Metrics UI with Prometheus/Grafana
- ✅ Performance profiling - pprof endpoints (admin-only)
- ✅ Load testing - K6 suite with 10,000+ operations
- ✅ S3 Inventory reports - Automated daily/weekly generation
- ✅ Backup tool support - VEEAM SOSAPI compatibility

**Documentation:**
- For detailed changelog, see [CHANGELOG.md](CHANGELOG.md)
- For performance metrics and analysis, see [docs/PERFORMANCE.md](docs/PERFORMANCE.md)
- For testing documentation, see [tests/performance/README.md](tests/performance/README.md)
- For API documentation, see [docs/API.md](docs/API.md)

**Roadmap Notes:**
- Subject to change based on community feedback and production usage
- Security and stability take priority over new features
- Breaking changes will be avoided in beta phase
- Community contributions welcome for all planned features
