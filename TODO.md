# MaxIOFS - TODO & Roadmap

**Version**: 0.4.3-beta
**Last Updated**: December 3, 2025
**Status**: Beta - 98% S3 Compatible

## 📊 Project Status

```
┌─────────────────────────────────────────┐
│  MaxIOFS v0.4.3-beta - BETA STATUS      │
├─────────────────────────────────────────┤
│  S3 API Compatibility:        98%       │
│  Backend Test Coverage:       ~53%      │
│  Frontend Test Coverage:      100%      │
│  Features Complete:           ~96%      │
│  Production Ready:            Testing   │
└─────────────────────────────────────────┘

Test Coverage by Module:
  • pkg/s3compat       - 18 tests, 30.9% coverage
  • internal/auth      - 11 tests, 28.0% coverage
  • internal/server    - 28 tests, 12.7% coverage
  • internal/bucket    - 47 tests, 49.8% coverage
  • internal/object    - 83 tests, 48.4% coverage
  • internal/acl       - 25 tests, 77.0% coverage
  • internal/middleware- 30 tests, 87.4% coverage
  • internal/lifecycle - 12 tests, 67.9% coverage
  • internal/storage   - 40 tests, 79.1% coverage
  • internal/metadata  - 30 tests, 52.4% coverage
  • internal/logging   - 26 tests, 100% pass rate
  • internal/metrics   - 29 tests, 17.4% coverage
  • internal/settings  - 14 tests, 83.6% coverage
  • internal/share     - 14 tests, 63.5% coverage
  • internal/notifications - 15 tests
  • internal/presigned - 21 tests, 84.4% coverage
  • internal/config    - 13 tests, 35.8% coverage
  • internal/replication - 23 tests, 100% pass rate ✅ COMPLETE
  • Frontend (React)   - 64 tests, 100% pass rate

Total Backend Tests: 504 (100% pass rate)
Total Frontend Tests: 64 (100% pass rate)
```

## 📌 Pending Tasks

### 🔴 HIGH PRIORITY (New Features - In Planning)

#### 🎯 **BUCKET REPLICATION & MULTI-REGION** (v0.5.0)
**Status**: Phase 1 Complete ✅ | Phase 2-4 Pending
**Priority**: HIGH
**Complexity**: High

**Phase 1 (COMPLETE)**: Basic S3-compatible replication
- ✅ Backend module with CRUD operations for replication rules
- ✅ Queue-based async processing with worker pools
- ✅ SQLite persistence for rules, queue, and status
- ✅ Retry logic with exponential backoff
- ✅ Frontend integration in bucket settings page
- ✅ 23 automated tests (100% pass rate)

**Remaining Phases**:
- Phase 2: Multi-region support with health checks
- Phase 3: Web console enhancements and metrics dashboard
- Phase 4: Advanced features (bidirectional sync, automatic failover)

See detailed implementation plan below in "🚀 IMPLEMENTATION PLAN" section.

### 🟡 MEDIUM PRIORITY (Test Coverage Expansion)
- [ ] **pkg/s3compat** (30.9% coverage) - Expand S3 API compatibility tests
- [ ] **internal/auth** (28.0% coverage) - Expand authentication/authorization tests
- [ ] **internal/server** (12.7% coverage) - Expand server/console API tests
- [ ] **internal/metrics** (17.4% coverage) - Expand metrics manager tests

### 🟡 MEDIUM PRIORITY (Improvements & optimization)
- [ ] Memory/CPU Profiling - Identify and fix bottlenecks
- ✅ ~~Add Tests to Nightly Builds~~ - **COMPLETED** (Tests fail builds on failure)
- [ ] Enhanced Health Checks - Readiness probes with dependency checks
- [ ] Database Migrations Versioning - Schema version control

### 🟢 LOW PRIORITY (Nice to have)
- [ ] Bucket Inventory - Periodic reports
- [ ] Object Metadata Search - Full-text search capability
- [ ] Hot Reload for Frontend Dev - Improved DX
- [ ] Official Docker Hub Images - Public registry
- [ ] Additional Storage Backends - S3, GCS, Azure blob

---

## 🚀 IMPLEMENTATION PLAN: Replication & Multi-Region

### Overview
Implement bucket replication across multiple MaxIOFS instances and multi-region support for high availability and disaster recovery.

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      REPLICATION LAYER                       │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────┐    Replication    ┌──────────────┐       │
│  │   Region A   │◄──────────────────►│   Region B   │       │
│  │  MaxIOFS #1  │     (bidirectional)│  MaxIOFS #2  │       │
│  └──────┬───────┘                    └───────┬──────┘       │
│         │                                    │               │
│    ┌────▼────┐                          ┌───▼─────┐        │
│    │ Bucket1 │                          │ Bucket1 │        │
│    │ Bucket2 │                          │ Bucket2 │        │
│    └─────────┘                          └─────────┘        │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

### Phase 1: Replication Foundation (Week 1-2)

#### 1.1 Replication Configuration Module
**Path**: `internal/replication/`

**Components**:
- `config.go` - Replication rules and policies
- `types.go` - Data structures for replication
- `manager.go` - Replication manager orchestration
- `worker.go` - Background replication workers
- `queue.go` - Replication queue (pending operations)

**Database Schema** (SQLite):
```sql
-- Replication rules
CREATE TABLE replication_rules (
    id TEXT PRIMARY KEY,
    source_bucket TEXT NOT NULL,
    destination_endpoint TEXT NOT NULL,
    destination_bucket TEXT NOT NULL,
    destination_access_key TEXT NOT NULL,
    destination_secret_key TEXT NOT NULL,
    enabled BOOLEAN DEFAULT true,
    bidirectional BOOLEAN DEFAULT false,
    replicate_deletes BOOLEAN DEFAULT true,
    replicate_metadata BOOLEAN DEFAULT true,
    prefix_filter TEXT,
    status TEXT DEFAULT 'active', -- active, paused, failed
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Replication queue (pending operations)
CREATE TABLE replication_queue (
    id TEXT PRIMARY KEY,
    rule_id TEXT NOT NULL,
    operation TEXT NOT NULL, -- put, delete, copy
    bucket TEXT NOT NULL,
    object_key TEXT NOT NULL,
    version_id TEXT,
    size INTEGER,
    etag TEXT,
    status TEXT DEFAULT 'pending', -- pending, in_progress, completed, failed
    retry_count INTEGER DEFAULT 0,
    max_retries INTEGER DEFAULT 3,
    last_error TEXT,
    created_at INTEGER NOT NULL,
    processed_at INTEGER,
    FOREIGN KEY (rule_id) REFERENCES replication_rules(id)
);

-- Replication status tracking
CREATE TABLE replication_status (
    rule_id TEXT PRIMARY KEY,
    total_objects INTEGER DEFAULT 0,
    replicated_objects INTEGER DEFAULT 0,
    failed_objects INTEGER DEFAULT 0,
    total_bytes INTEGER DEFAULT 0,
    replicated_bytes INTEGER DEFAULT 0,
    last_sync_at INTEGER,
    last_error TEXT,
    FOREIGN KEY (rule_id) REFERENCES replication_rules(id)
);
```

**Key Methods**:
```go
// Replication Manager Interface
type Manager interface {
    // Rule management
    CreateRule(ctx context.Context, rule *ReplicationRule) error
    GetRule(ctx context.Context, ruleID string) (*ReplicationRule, error)
    ListRules(ctx context.Context, bucketName string) ([]*ReplicationRule, error)
    UpdateRule(ctx context.Context, rule *ReplicationRule) error
    DeleteRule(ctx context.Context, ruleID string) error

    // Operations
    EnqueueReplication(ctx context.Context, op ReplicationOperation) error
    ProcessQueue(ctx context.Context) error
    GetStatus(ctx context.Context, ruleID string) (*ReplicationStatus, error)

    // Control
    Start(ctx context.Context) error
    Stop() error
    PauseRule(ctx context.Context, ruleID string) error
    ResumeRule(ctx context.Context, ruleID string) error
}
```

**Implementation Checklist**:
- [x] ✅ Create `internal/replication/` directory structure - **COMPLETE**
- [x] ✅ Implement data structures and types (types.go) - **COMPLETE**
- [x] ✅ Create SQLite schema and migrations (schema.go) - **COMPLETE**
- [x] ✅ Implement ReplicationManager with CRUD operations (manager.go) - **COMPLETE**
- [x] ✅ Implement ReplicationQueue with retry logic (worker.go) - **COMPLETE**
- [x] ✅ Add unit tests (23 tests, 100% pass rate) - **COMPLETE**
- [x] ✅ Console API endpoints for replication (console_api_replication.go) - **COMPLETE**
- [x] ✅ Frontend UI integration in bucket settings - **COMPLETE**
- [x] ✅ S3 parameter configuration (endpoint, access key, secret key) - **COMPLETE**

#### 1.2 S3 Client for Cross-Instance Communication
**Path**: `internal/replication/s3client/`

**Features**:
- AWS SigV4 authentication for MaxIOFS-to-MaxIOFS communication
- Connection pooling for efficient multi-object transfers
- Retry logic with exponential backoff
- Progress tracking for large objects
- Multipart upload support for files >5MB

**Implementation Checklist**:
- [ ] Create S3 client wrapper using AWS SDK
- [ ] Implement authentication with access/secret keys
- [ ] Add connection pooling and keep-alive
- [ ] Implement retry logic with circuit breaker
- [ ] Add progress callbacks for monitoring
- [ ] Unit tests for client operations

#### 1.3 Replication Worker
**Path**: `internal/replication/worker.go`

**Features**:
- Background goroutine pool for parallel replication
- Configurable concurrency (default: 10 workers)
- Queue polling with exponential backoff on errors
- Automatic retry of failed operations
- Metrics collection (operations/sec, bytes/sec, errors)

**Worker Flow**:
```
1. Poll replication queue for pending operations
2. For each operation:
   a. Lock operation (mark as in_progress)
   b. Fetch source object metadata
   c. Check if destination needs update (compare ETags)
   d. Transfer object to destination
   e. Verify transfer (compare size/ETag)
   f. Mark as completed or failed
   g. Update replication status
3. Sleep if queue is empty, retry failed with backoff
```

**Implementation Checklist**:
- [ ] Implement worker pool with configurable size
- [ ] Add queue polling with graceful shutdown
- [ ] Implement object transfer logic
- [ ] Add ETag verification for integrity
- [ ] Implement retry logic with exponential backoff
- [ ] Add metrics collection
- [ ] Integration tests with mock S3

### Phase 2: Multi-Region Support (Week 2-3)

#### 2.1 Region Configuration
**Path**: `internal/region/`

**Database Schema**:
```sql
CREATE TABLE regions (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    endpoint TEXT NOT NULL,
    access_key TEXT NOT NULL,
    secret_key TEXT NOT NULL,
    is_local BOOLEAN DEFAULT false,
    priority INTEGER DEFAULT 0, -- for read preference
    health_status TEXT DEFAULT 'healthy', -- healthy, degraded, unavailable
    last_health_check INTEGER,
    created_at INTEGER NOT NULL
);

CREATE TABLE bucket_regions (
    bucket_name TEXT NOT NULL,
    region_id TEXT NOT NULL,
    is_primary BOOLEAN DEFAULT false,
    sync_status TEXT DEFAULT 'synced', -- synced, syncing, out_of_sync
    last_sync_at INTEGER,
    PRIMARY KEY (bucket_name, region_id),
    FOREIGN KEY (region_id) REFERENCES regions(id)
);
```

**Region Manager**:
```go
type RegionManager interface {
    // Region CRUD
    AddRegion(ctx context.Context, region *Region) error
    GetRegion(ctx context.Context, regionID string) (*Region, error)
    ListRegions(ctx context.Context) ([]*Region, error)
    RemoveRegion(ctx context.Context, regionID string) error

    // Bucket-Region mapping
    AssignBucketToRegion(ctx context.Context, bucket, regionID string, isPrimary bool) error
    GetBucketRegions(ctx context.Context, bucket string) ([]*Region, error)
    GetPrimaryRegion(ctx context.Context, bucket string) (*Region, error)

    // Health checks
    CheckRegionHealth(ctx context.Context, regionID string) error
    GetHealthyRegions(ctx context.Context) ([]*Region, error)
}
```

**Implementation Checklist**:
- [ ] Create region configuration module
- [ ] Implement region health checks (periodic ping)
- [ ] Add bucket-to-region mapping
- [ ] Implement primary/secondary region logic
- [ ] Add failover logic (automatic switch on primary failure)
- [ ] Unit and integration tests

#### 2.2 Smart Object Routing
**Path**: `internal/region/router.go`

**Features**:
- Read from nearest healthy region
- Write to primary region + async replication to secondaries
- Automatic failover on region failure
- Read preference: primary → secondary → tertiary
- Sticky sessions for read consistency

**Routing Logic**:
```
PUT /bucket/key:
  1. Write to primary region (synchronous)
  2. Enqueue replication to secondary regions (async)
  3. Return success to client

GET /bucket/key:
  1. Check primary region health
  2. If healthy: read from primary
  3. If unhealthy: read from secondary (ordered by priority)
  4. If all unhealthy: return 503 Service Unavailable

DELETE /bucket/key:
  1. Delete from primary region (synchronous)
  2. Enqueue deletion to secondary regions (async)
  3. Return success to client
```

**Implementation Checklist**:
- [ ] Implement object router with region awareness
- [ ] Add read routing logic with fallback
- [ ] Implement write-to-primary + async replication
- [ ] Add health check integration
- [ ] Implement failover logic
- [ ] Integration tests with multi-region setup

#### 2.3 Conflict Resolution
**Path**: `internal/replication/conflict.go`

**Strategies**:
1. **Last-Write-Wins (LWW)** - Use object's LastModified timestamp
2. **Version-Based** - Use version IDs for conflict detection
3. **Primary-Wins** - Primary region always wins in conflicts

**Conflict Detection**:
- Compare ETags across regions
- Compare LastModified timestamps
- Detect split-brain scenarios (both regions modified)

**Implementation Checklist**:
- [ ] Implement conflict detection algorithm
- [ ] Add LWW resolution strategy
- [ ] Add version-based resolution
- [ ] Log conflicts for audit
- [ ] Add manual conflict resolution API
- [ ] Unit tests for conflict scenarios

### Phase 3: Web Console & API (Week 3-4)

#### 3.1 Replication Console API
**Path**: `internal/server/console/replication.go`

**Endpoints**:
```
POST   /api/v1/replication/rules         - Create replication rule
GET    /api/v1/replication/rules         - List all rules
GET    /api/v1/replication/rules/:id     - Get rule details
PUT    /api/v1/replication/rules/:id     - Update rule
DELETE /api/v1/replication/rules/:id     - Delete rule
POST   /api/v1/replication/rules/:id/pause  - Pause rule
POST   /api/v1/replication/rules/:id/resume - Resume rule

GET    /api/v1/replication/status/:id    - Get replication status
GET    /api/v1/replication/queue         - View replication queue
POST   /api/v1/replication/retry-failed  - Retry all failed operations

GET    /api/v1/regions                   - List regions
POST   /api/v1/regions                   - Add region
GET    /api/v1/regions/:id               - Get region details
PUT    /api/v1/regions/:id               - Update region
DELETE /api/v1/regions/:id               - Remove region
POST   /api/v1/regions/:id/health        - Check region health
```

**Implementation Checklist**:
- [ ] Create Console API endpoints
- [ ] Add request validation
- [ ] Implement authorization checks (admin only)
- [ ] Add rate limiting
- [ ] API documentation (OpenAPI/Swagger)
- [ ] Integration tests for all endpoints

#### 3.2 Replication UI (Frontend)
**Path**: `web/frontend/src/pages/Replication/`

**Pages**:
1. **Replication Rules** (`/replication/rules`)
   - List all replication rules
   - Create new rule wizard
   - Edit/delete existing rules
   - Enable/disable rules
   - View rule status and metrics

2. **Replication Status** (`/replication/status`)
   - Real-time replication progress
   - Queue size and processing rate
   - Failed operations list with retry option
   - Bandwidth usage charts
   - Latency metrics

3. **Regions** (`/replication/regions`)
   - List configured regions
   - Add/remove regions
   - Health status indicators
   - Bucket-region assignments
   - Failover configuration

**Implementation Checklist**:
- [ ] Create Replication pages in React
- [ ] Implement rule creation wizard
- [ ] Add real-time status updates (SSE or polling)
- [ ] Create charts for metrics (recharts)
- [ ] Add region health indicators
- [ ] Implement bucket-region mapping UI
- [ ] Unit tests for components (Vitest)
- [ ] E2E tests for workflows

### Phase 4: Testing & Documentation (Week 4)

#### 4.1 Comprehensive Testing

**Unit Tests** (Target: 80%+):
- [ ] Replication manager CRUD operations
- [ ] Queue management and retry logic
- [ ] Conflict resolution algorithms
- [ ] Region manager operations
- [ ] Object routing logic

**Integration Tests**:
- [ ] End-to-end replication flow (2 MaxIOFS instances)
- [ ] Bidirectional replication
- [ ] Delete replication
- [ ] Conflict resolution scenarios
- [ ] Failover testing (region goes down)
- [ ] Large file replication (multipart)
- [ ] Network failure recovery

**Load Tests**:
- [ ] Replicate 10,000+ small objects
- [ ] Replicate 100+ large objects (>100MB)
- [ ] Concurrent replication (multiple buckets)
- [ ] Measure replication lag under load

#### 4.2 Documentation

**User Documentation**:
- [ ] Replication setup guide
- [ ] Multi-region configuration guide
- [ ] Conflict resolution explanation
- [ ] Troubleshooting guide
- [ ] Best practices

**API Documentation**:
- [ ] OpenAPI/Swagger specs for new endpoints
- [ ] Request/response examples
- [ ] Error codes and meanings

**Architecture Documentation**:
- [ ] Replication architecture diagram
- [ ] Data flow diagrams
- [ ] Database schema documentation
- [ ] Sequence diagrams for key operations

### Technical Considerations

#### Performance
- **Async Replication**: Don't block PUT operations waiting for replication
- **Batching**: Group small objects for efficient transfer
- **Compression**: Optionally compress objects during transfer
- **Delta Sync**: Only transfer changed bytes (for large objects)
- **Connection Pooling**: Reuse HTTP connections

#### Reliability
- **Retry Logic**: Exponential backoff with max retries
- **Circuit Breaker**: Stop attempting failed endpoints temporarily
- **Dead Letter Queue**: Move permanently failed operations
- **Idempotency**: Handle duplicate operations gracefully
- **Transactional Updates**: Atomic queue operations

#### Security
- **Encrypted Transport**: HTTPS for cross-region communication
- **Credential Rotation**: Support updating destination credentials
- **Access Control**: Only admins can configure replication
- **Audit Logging**: Log all replication operations

#### Monitoring
- **Metrics**:
  - Replication lag (seconds behind)
  - Operations per second
  - Bytes per second
  - Failed operations count
  - Queue depth
  - Region health status

- **Alerts**:
  - Replication lag exceeds threshold
  - Region becomes unhealthy
  - Queue depth exceeds limit
  - Failed operations exceed rate

### Configuration Example

```yaml
# config.yaml
replication:
  enabled: true
  workers: 10
  batch_size: 100
  retry_delay: 30s
  max_retries: 3

regions:
  - id: us-east-1
    name: US East
    endpoint: https://maxiofs-1.example.com
    access_key: ${REGION1_ACCESS_KEY}
    secret_key: ${REGION1_SECRET_KEY}
    is_local: true
    priority: 1

  - id: us-west-1
    name: US West
    endpoint: https://maxiofs-2.example.com
    access_key: ${REGION2_ACCESS_KEY}
    secret_key: ${REGION2_SECRET_KEY}
    is_local: false
    priority: 2
```

### Success Criteria

- [ ] Replication works bidirectionally between 2+ instances
- [ ] Replication lag < 5 seconds for small objects under normal load
- [ ] Replication lag < 60 seconds for large objects (>100MB)
- [ ] 99.9% replication success rate
- [ ] Automatic failover works within 30 seconds
- [ ] Zero data loss during failover
- [ ] Web console shows real-time replication status
- [ ] All tests pass with 80%+ coverage
- [ ] Documentation complete and reviewed

---

## ✅ Recently Completed (Last 30 Days)

### December 3, 2025
- ✅ **Bucket Replication System** - Complete S3-compatible replication implementation
  - Backend module: types, schema, manager, worker, queue (internal/replication/)
  - Console API endpoints for rule management
  - Frontend integration in bucket settings with visual rule editor
  - S3 protocol-level replication (endpoint URL, access key, secret key)
  - Three modes: realtime, scheduled, batch with configurable intervals
  - Queue-based async processing with retry logic
  - Conflict resolution strategies (LWW, version-based, primary-wins)
  - SQLite persistence for rules, queue items, and status tracking
  - 23 automated tests covering CRUD, queueing, processing (100% pass rate)
- ✅ **Metrics Module Test Suite** (0% → 17.4%, +29 tests) - CRITICAL for monitoring
- ✅ **Settings Module Test Suite** (0% → 83.6%, +14 tests) - CRITICAL for configuration
- ✅ **Share Module Test Suite** (0% → 63.5%, +14 tests) - Presigned URL shares
- ✅ **Notifications Module Test Suite** (+15 tests) - SSE push notifications
- ✅ **Presigned Module Test Suite** (0% → 84.4%, +21 tests) - Temporary access URLs
- ✅ **Config Module Test Suite** (0% → 35.8%, +13 tests) - Application configuration
- ✅ **GitHub Actions Updated** - Tests run before nightly builds, coverage reports to S3
- ✅ **CHANGELOG Optimized** - Reduced from 2372 lines to 232 lines (90% reduction)
- ✅ **Backend Coverage Improved** - 458 → 504 tests (52% → ~53% coverage)

### December 2, 2025
- ✅ **Middleware Module Test Suite** (0% → 87.4%, +30 tests) - CRITICAL (Infrastructure)

### November 30, 2025
- ✅ **ACL Module Test Suite** (0% → 77.0%, +25 tests) - CRITICAL (Security)
- ✅ **Lifecycle Module Test Suite** (0% → 67.9%, +12 tests) - CRITICAL
- ✅ **Bucket Module Test Suite** (0% → 49.8%, +47 tests) - CRITICAL
- ✅ **Storage Module Test Suite** (0% → 79.1%, +40 tests) - CRITICAL
- ✅ **Metadata Module Test Suite** (30% → 52.4%, +30 tests) - CRITICAL
- ✅ Console API Test Coverage Expansion (4.4% → 12.7%, +19 tests)
- ✅ Object Module Test Coverage Expansion (36.7% → 48.4%, +83 tests)
- ✅ Bug fix: Frontend session logout on background queries
- ✅ Bug fix: VEEAM SOSAPI capacity reporting for tenants

### November 29, 2025
- ✅ Logging System Test Suite Complete (26 tests)
- ✅ S3 API Test Coverage expanded (16.6% → 30.9%)
- ✅ Bug fix: ListObjectVersions for non-versioned buckets

### November 28, 2025
- ✅ Frontend Testing Infrastructure (64 tests, 100% pass)
- ✅ Login, Dashboard, Buckets, Users tests complete

### November 26, 2025
- ✅ S3 API Test Coverage Phase 1 (13 tests, AWS SigV4 auth)

### November 24, 2025
- ✅ Real-Time Push Notifications (SSE)
- ✅ Dynamic Security Configuration
- ✅ Multiple critical bug fixes

### November 20, 2025
- ✅ Lifecycle Worker - 100% Complete
- ✅ Noncurrent version expiration + delete marker cleanup

## 🗺️ Roadmap

### Short Term (v0.5.0)
- Performance profiling and optimization
- CI/CD pipeline implementation
- Encryption key rotation
- Per-tenant encryption keys
- HSM integration

### Medium Term (v0.6.0 - v0.8.0)
- Bucket replication (cross-bucket/cross-region)
- Enhanced monitoring and alerting
- Kubernetes Helm charts
- Advanced compliance reporting

### Long Term (v1.0.0+)
- Multi-node clustering
- Node-to-node replication
- Additional storage backends (S3, GCS, Azure)
- LDAP/SSO integration
- External key management (AWS KMS, Azure Key Vault)

## 📝 Notes

**For detailed technical information, bug fixes, and implementation details, see:**
- `CHANGELOG.md` - Complete history of changes, bugs fixed, features added
- `README.md` - Feature documentation and usage guide
- `/docs` - Comprehensive technical documentation

**This TODO file tracks:**
- Current project status and metrics
- Pending tasks by priority
- Recent completions (summary only)
- Future roadmap

---

**Last Review**: November 29, 2025
**Next Review**: When starting work on v0.5.0
