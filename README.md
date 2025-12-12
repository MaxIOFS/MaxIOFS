# MaxIOFS - S3-Compatible Object Storage

**Version**: 0.6.0-beta
**Status**: Beta - 98% S3 Compatible
**License**: MIT

MaxIOFS is an S3-compatible object storage system built in Go with an embedded Next.js web interface. Designed to be simple, portable, and deployable as a single binary.

## 🎉 Project Status

**This project is now in BETA phase - v0.6.0-beta**. This means:
- ✅ **All core S3 features fully implemented and tested**
- ✅ **AWS CLI compatibility validated for all major operations (98% compatible)**
- ✅ Successfully tested with MinIO Warp (7000+ objects, bulk operations validated)
- ✅ Metadata consistency verified under load
- ✅ Bucket Policy, Versioning, Lifecycle, and Delete Markers working
- ✅ **Server-side encryption at rest (AES-256-CTR streaming)**
- ✅ **Comprehensive audit logging system**
- ✅ **Two-Factor Authentication (2FA) with TOTP**
- ✅ **Dynamic settings management without restarts**
- ✅ **Complete documentation available in `/docs`**
- ✅ **Automated testing suite with race condition verification**
- ✅ **Zero known bugs** (all reported issues verified and resolved)
- ⚠️ Suitable for testing, development, and staging environments
- ⚠️ Production use requires your own extensive testing
- 💡 Ready for production-like workloads with proper monitoring

## 🎯 Features

### S3 API Compatibility (98%)
- ✅ **Global Bucket Uniqueness** - Bucket names globally unique across all tenants (AWS S3 compatible) - *New in 0.4.2*
- ✅ Core operations (PutObject, GetObject, DeleteObject, ListObjects)
- ✅ Bucket management (Create, List, Delete, GetBucketInfo)
- ✅ Multipart uploads (complete workflow)
- ✅ Presigned URLs (GET/PUT with expiration, S3-compatible paths without tenant prefix) - *Updated in 0.4.2*
- ✅ **Bulk operations (DeleteObjects - batch delete up to 1000 objects)**
- ✅ Object Lock (COMPLIANCE/GOVERNANCE modes)
- ✅ **Bucket Versioning** (Multiple versions, Delete Markers, GetObjectVersions, DeleteVersion)
- ✅ **Lifecycle Policies** (100% Complete - Noncurrent version expiration AND expired delete marker cleanup, worker runs hourly)
- ✅ **Bucket Notifications** (Webhooks on object events - ObjectCreated, ObjectRemoved, ObjectRestored) - *New in 0.4.2*
- ✅ **Bucket Policy** (Complete PUT/GET/DELETE, JSON validation, AWS CLI compatible)
- ✅ **Bucket CORS** (Get/Put/Delete CORS rules, Visual UI editor)
- ✅ **Bucket Tagging** (Get/Put/Delete tags, Visual UI manager)
- ✅ **Bucket Lifecycle Configuration** (Get/Put/Delete lifecycle rules)
- ✅ **Bucket Replication** (S3-compatible cross-bucket replication to AWS S3, MinIO, or other MaxIOFS instances) - *New in 0.5.0*
- ✅ **Object Tagging** (Get/Put/Delete tags)
- ✅ Object ACL (Get/Put access control lists)
- ✅ Object Retention (WORM with legal hold support)
- ✅ CopyObject (with metadata preservation, cross-bucket support)
- ✅ **Conditional Requests** (If-Match, If-None-Match for HTTP caching)
- ✅ **Range Requests** (Partial downloads with bytes=start-end)

### Configuration & Settings
- ✅ **Dynamic Settings System** - Runtime configuration management without restarts - *New in 0.4.0*
  - Dual-configuration architecture (static config.yaml + dynamic database settings)
  - 23 configurable settings across 5 categories (Security, Audit, Storage, Metrics, System)
  - Web Console settings page with modern tabbed interface
  - Real-time editing with change tracking and bulk save
  - Type validation (string, int, bool, json) with smart controls
  - Visual status indicators and human-readable value formatting
  - Full audit trail for all configuration changes
  - Global admin only access with permission enforcement

### Bucket Notifications
- ✅ **Event Notifications (Webhooks)** - Send HTTP webhooks on S3 events - *New in 0.4.2*
  - AWS S3 compatible event format (EventVersion 2.1)
  - Event types: ObjectCreated:*, ObjectRemoved:*, ObjectRestored:Post
  - Wildcard event matching (e.g., s3:ObjectCreated:* matches Put, Post, Copy)
  - Webhook delivery with retry mechanism (3 attempts, 2-second delay)
  - Per-rule filtering: Prefix and suffix filters for object keys
  - Custom HTTP headers support per notification rule
  - Enable/disable rules without deletion
  - Web Console UI with tab-based bucket settings
  - Add/Edit/Delete notification rules via intuitive modal
  - Configuration stored in BadgerDB with in-memory caching
  - Multi-tenant support with global admin access
  - Full audit logging for all configuration changes

### Bucket Replication
- ✅ **S3-Compatible Replication** - Cross-bucket replication to AWS S3, MinIO, or other MaxIOFS instances - *New in 0.5.0*
  - S3 protocol-level replication using standard S3 API calls
  - Destination configuration: Endpoint URL, bucket name, access key, secret key, region
  - Three replication modes:
    - **Realtime**: Immediate replication on object changes
    - **Scheduled**: Periodic batch replication at configurable intervals (minutes)
    - **Batch**: Manual replication on demand
  - Conflict resolution strategies: Last Write Wins, Version-Based, Primary Wins
  - Queue-based async processing with configurable worker pools
  - Automatic retry with exponential backoff for failed replications
  - Selective replication: Prefix filters, delete replication, metadata replication
  - Priority-based rule ordering for multiple replication targets
  - Comprehensive metrics: Pending, completed, failed objects, bytes replicated
  - SQLite-backed persistence for rules, queue items, and replication status
  - Web Console integration in bucket settings with visual rule management
  - Full audit logging for all replication operations
  - 23 automated tests covering CRUD, queueing, processing, and edge cases

### Cluster Management
- ✅ **Multi-Node Cluster Support** - High availability cluster with intelligent routing and failover - *New in 0.6.0*
  - **Cluster Manager**: Complete CRUD operations for cluster nodes, health monitoring, and configuration
  - **Smart Router with Failover**: Intelligent request routing to healthy nodes with automatic failover
  - **Bucket Location Cache**: 5-minute TTL cache for bucket-to-node mappings (5ms latency vs 50ms for misses)
  - **Internal Proxy Mode**: Any node can receive any S3 request and proxy internally to the correct node
  - **Health Checker**: Background worker checking all nodes every 30 seconds with latency tracking
  - **SQLite Persistence**: 3 tables (cluster_config, cluster_nodes, cluster_health_history) for cluster state
  - **Console API Endpoints**: 13 REST endpoints for cluster management (initialize, join, nodes CRUD, health, cache)
  - **27 automated tests** covering cluster operations (100% pass rate)
- ✅ **Cluster Bucket Replication** - Node-to-node replication for high availability - *New in 0.6.0*
  - **HMAC Authentication**: Nodes authenticate using HMAC-SHA256 with `node_token` (no S3 credentials)
  - **Automatic Tenant Sync**: Continuous synchronization of tenant data every 30 seconds
  - **Encryption Handling**: Transparent decrypt-on-source, re-encrypt-on-destination
  - **Configurable Sync Intervals**: From 10 seconds (real-time HA) to hours/days (backups)
  - **Self-Replication Prevention**: Built-in validation prevents nodes from replicating to themselves
  - **Bulk Configuration**: Configure entire node-to-node replication for all buckets at once
  - **Separation from User Replication**: Completely separate system from external S3 replication
  - **5 integration tests** simulating two-node cluster communication (HMAC auth, tenant sync, object/delete replication)
- ✅ **Cluster Dashboard UI** - Complete web console for cluster management - *New in 0.6.0*
  - **Cluster Page**: New `/cluster` route accessible to global administrators
  - **Cluster Status Overview**: Real-time dashboard showing total/healthy/degraded/unavailable nodes and bucket statistics
  - **Nodes Management Table**: Interactive table with health indicators, latency, capacity, bucket count, and priority
  - **Initialize Cluster**: Dialog to create new cluster with automatic token generation
  - **Add Node**: Join existing cluster or add remote nodes with endpoint, token, and configuration
  - **Edit Node**: Update node settings (name, priority, region, metadata)
  - **Operations**: Remove nodes, trigger health checks, refresh status with complete error handling
  - **Color-Coded Health Status**: Green (healthy), yellow (degraded), red (unavailable), gray (unknown)
  - **Navigation Integration**: "Cluster" menu item with Server icon (global admin only)
  - **TypeScript Types**: 14 interfaces for complete type safety
  - **API Client**: 13 cluster management methods fully integrated

### Authentication & Security
- ✅ **Real-Time Push Notifications (SSE)** - Server-Sent Events for instant admin alerts - *New in 0.4.2*
  - Live notifications in topbar bell icon with unread count badge
  - User locked notifications when accounts are blocked
  - Read/unread state tracking with localStorage persistence
  - Limited to last 3 notifications to prevent UI clutter
  - Tenant isolation (global admins see all, tenant admins see only their tenant)
  - Automatic reconnection and token detection
  - Zero polling - true push-based notifications
- ✅ **Dynamic Security Configuration** - Adjust security settings without restart - *New in 0.4.2*
  - Configurable IP-based rate limiting (default: 5 attempts/minute)
  - Configurable account lockout threshold (default: 5 failed attempts)
  - Configurable lockout duration (default: 15 minutes)
  - Separate controls for IP rate limiting vs account lockout
  - Changes take effect immediately via Settings page
  - All configuration changes logged in audit trail
- ✅ **Server-Side Encryption at Rest (SSE)** - AES-256-CTR encryption for all objects - *New in 0.4.1*
  - Persistent master key (config.yaml) - survives server restarts
  - Streaming encryption - constant memory usage, supports files of ANY size
  - Flexible control: Global (server-level) + per-bucket configuration
  - Automatic decryption - encrypted files always accessible with master key
  - Mixed mode support - encrypted and unencrypted objects coexist
  - Web Console integration - visual encryption status and controls
  - Zero performance impact - tested at 150+ MiB/s for 100MB files
- ✅ **Comprehensive Audit Logging System** - Track all system events with compliance-ready logs - *New in 0.4.0*
  - 20+ event types (authentication, user management, bucket operations, 2FA events)
  - Advanced filtering (event type, status, date range, resource type)
  - CSV export for compliance reporting
  - Automatic retention management (configurable, default 90 days)
  - Multi-tenant isolation (global/tenant admin access control)
- ✅ **Two-Factor Authentication (2FA)** - TOTP-based with QR codes, backup codes - *New in 0.3.2*
- ✅ Dual authentication (JWT for Console, S3 Signature v2/v4 for API)
- ✅ Bcrypt password hashing
- ✅ Access keys with secret key management
- ✅ **Configurable security policies** - Password requirements, session timeouts, rate limits - *New in 0.4.0*
- ✅ Account lockout after failed attempts (configurable)
- ✅ CORS support (configurable per bucket)
- ✅ Multi-tenancy with resource isolation

### Web Console
- ✅ Modern responsive UI with dark mode support
- ✅ Dashboard with real-time statistics and metrics
- ✅ Bucket browser with object operations
- ✅ File upload/download with drag-and-drop
- ✅ File sharing with expirable links
- ✅ User management (Create, Edit, Delete, Roles)
- ✅ Access key management (Create, Revoke, View)
- ✅ Tenant management with quotas (Storage, Buckets, Keys)
- ✅ **Bucket configuration editors** (Tabbed interface with Visual + XML modes):
  - **General**: Versioning, encryption, and bucket tags
  - **Security & Access**: Bucket policy, ACL, and CORS configuration
  - **Lifecycle**: Rule-based lifecycle policies
  - **Notifications**: Webhook event notifications with rule management - *New in 0.4.2*
  - **Replication**: S3-compatible bucket replication rules with destination configuration - *New in 0.5.0*
- ✅ **System Settings Page** (Global Admins only) - *New in 0.4.0*
  - Dual-configuration architecture (static + dynamic settings)
  - Modern tabbed interface (Security, Audit, Storage, Metrics, System)
  - Real-time editing with visual change tracking
  - Smart controls: toggles (bool), number inputs (int), text inputs (string)
  - Status badges showing enabled/disabled states
  - Human-readable formatting with units (hours, days, MB, etc.)
  - Bulk save with transaction support
  - Full integration with audit logging
- ✅ Security audit page
- ✅ **Audit Logs Page** (Global/Tenant Admins only) - *New in 0.4.0*
  - Real-time event tracking with advanced filters
  - Quick date filters (Today, Last 7 Days, Last 30 Days)
  - Search across users, events, resources, and IPs
  - Color-coded critical events with visual alerts
  - Expandable row details with full event metadata
  - CSV export functionality
- ✅ **Cluster Management Page** (Global Admins only) - *New in 0.6.0*
  - Cluster status overview with node health statistics
  - Interactive nodes table with health indicators and metrics
  - Initialize cluster with token generation
  - Add/Edit/Remove nodes with form validation
  - Manual health checks and status refresh
  - Complete CRUD operations for cluster management
- ✅ Metrics monitoring (System, Storage, Requests, Performance)

### Storage & Performance
- ✅ **BadgerDB metadata store** (high-performance key-value database)
- ✅ **Transaction retry logic** for concurrent operations
- ✅ **Metadata-first deletion** (ensures consistency)
- ✅ Filesystem storage backend for objects
- ✅ Atomic write operations with rollback
- ✅ SQLite for authentication and user management
- ✅ **Production-Ready Performance** (tested on Linux, 80 cores, 125GB RAM) - *Baseline established in 0.6.0*
  - Upload: **p95 < 10ms** at 50 concurrent users (1.7-2.4 MB/s)
  - Download: **p95 < 13ms** at 100 concurrent users (172 MB/s)
  - List operations: **p95 < 28ms** under heavy load
  - Delete operations: **p95 < 7ms** under heavy load
  - **>99.99% success rate** across 100,000+ requests
  - See [docs/PERFORMANCE.md](docs/PERFORMANCE.md) for detailed analysis

### Logging & Monitoring
- ✅ **Advanced Logging System** - Flexible multi-output logging infrastructure
  - **Syslog Integration**: TCP/UDP protocol support for centralized logging
  - **HTTP Output**: Send logs to Splunk, Elastic, Loki, or custom endpoints
  - **Batch Processing**: Configurable batching and flush intervals for efficiency
  - **Authentication**: Bearer token support for secure HTTP endpoints
  - **Dynamic Configuration**: Runtime log level, format, and output changes
  - **Multiple Formats**: JSON and text logging with caller information
  - **Filtering**: Prefix/suffix filters for targeted log routing
- ✅ Single binary with embedded frontend
- ✅ **Docker & Docker Compose support** - *New in 0.3.2*
- ✅ **Prometheus metrics endpoint** (`/metrics`) - *New in 0.3.2*
  - HTTP requests, S3 operations, storage metrics
  - Authentication attempts, system resources (CPU, memory, disk)
  - Bucket/object operations, background tasks, cache metrics
  - Historical metrics stored in BadgerDB (365-day retention)
- ✅ **Health check endpoint** (`/health`) - Kubernetes/Docker ready
- ✅ **Automatic system metrics collection** - CPU, memory, disk usage tracking
- ✅ **Pre-built Grafana dashboard** (System, Storage, Requests, Performance) - *New in 0.3.2*
- ✅ HTTP and HTTPS support
- ✅ Configurable via CLI flags
- ✅ Production-ready with proper error handling
- ✅ ARM64 and Debian packaging support

## 🚀 Quick Start

### Option 1: Docker (Recommended)

**Basic deployment:**
```bash
make docker-build    # Build the image
make docker-up       # Start MaxIOFS
```

**With monitoring (Prometheus + Grafana):**
```bash
make docker-build       # Build the image
make docker-monitoring  # Start with monitoring stack
```

**3-Node Cluster (HA testing):**
```bash
make docker-build    # Build the image
make docker-cluster  # Start 3-node cluster
```

**Full Stack (Cluster + Monitoring):**
```bash
make docker-build                # Build the image
make docker-cluster-monitoring   # Start cluster with monitoring
```

**Access:**
- Web Console: http://localhost:8081 (admin/admin)
- S3 API: http://localhost:8080
- Prometheus: http://localhost:9091 (monitoring profile only)
- Grafana: http://localhost:3000 (admin/admin, monitoring profile only)
  - **Unified Dashboard**: Single comprehensive dashboard with 14 panels (loads as HOME)
  - Real-time metrics with 5-second auto-refresh
  - Performance alerts for latency, throughput, and SLO violations

**Cluster nodes (cluster profile):**
- Node 1: http://localhost:8081, :8080
- Node 2: http://localhost:8083, :8082
- Node 3: http://localhost:8085, :8084

**Other commands:**
```bash
make docker-down     # Stop all services
make docker-logs     # View logs
make docker-ps       # Show running containers
make docker-clean    # Clean volumes and containers
```

**Documentation:**
- [DOCKER.md](DOCKER.md) - Complete Docker deployment guide
- [docker/README.md](docker/README.md) - Detailed configuration and troubleshooting
- [DEPLOYMENT.md](docs/DEPLOYMENT.md) - Production deployment best practices

### Option 2: Build from Source

### Prerequisites
- Go 1.24+ (required)
- Node.js 23+ (required)

### Build

```bash
# Windows/Linux/macOS
make build
```

Output: `build/maxiofs.exe` (Windows) or `build/maxiofs` (Linux/macOS)

### Run

```bash
# Basic HTTP
.\build\maxiofs.exe --data-dir ./data

# With HTTPS
.\build\maxiofs.exe --data-dir ./data --tls-cert cert.pem --tls-key key.pem
```

### Access

- **Web Console**: `http://localhost:8081`
  - Default user: `admin` / `admin`
  - ⚠️ **Change password after first login!**
- **S3 API**: `http://localhost:8080`
  - **No default access keys** - Create them via web console
  - Login to console → Users → Create Access Key

**🔒 Security Note**: Access keys must be created manually through the web console. No default S3 credentials are provided for security reasons.

## 🔧 Configuration

```bash
Usage: maxiofs [OPTIONS]

Required:
  --data-dir string         Data directory path

Optional:
  --listen string           S3 API address (default ":8080")
  --console-listen string   Console API address (default ":8081")
  --log-level string        Log level: debug|info|warn|error (default "info")
  --tls-cert string         TLS certificate file
  --tls-key string          TLS private key file

Example:
  maxiofs --data-dir /var/lib/maxiofs --log-level debug
```

## 📖 Architecture

```
┌─────────────────────────────────────────┐
│      Single Binary (maxiofs.exe)        │
├─────────────────────────────────────────┤
│  Web Console (Embedded Next.js)   :8081│
│  - Static files in Go binary           │
│  - Dark mode support                   │
│  - Responsive design                   │
├─────────────────────────────────────────┤
│  Console REST API              :8081/api│
│  - JWT authentication                  │
│  - User/Bucket/Tenant management       │
│  - File operations & sharing           │
│  - Settings management (dynamic)       │
│  - Audit logs (query & export)         │
│  - Metrics endpoints:                  │
│    • /api/metrics (general stats)      │
│    • /api/metrics/system (CPU/RAM)     │
│    • /api/metrics/s3 (S3 operations)   │
│    • /api/metrics/history (time-series)│
│  - Health check: /health               │
├─────────────────────────────────────────┤
│  S3-Compatible API                 :8080│
│  - AWS Signature v2/v4                 │
│  - 40+ S3 operations                   │
│  - Multipart upload support            │
├─────────────────────────────────────────┤
│  Storage Layer                          │
│  - BadgerDB (object metadata)          │
│  - SQLite (auth & user management)     │
│  - Filesystem (object storage)         │
│  - Transaction retry with backoff      │
└─────────────────────────────────────────┘
```

## 📊 Project Structure

```
MaxIOFS/
├── cmd/maxiofs/              # Main application entry
├── internal/
│   ├── api/                  # Console REST API handlers
│   ├── auth/                 # Authentication & authorization
│   ├── bucket/               # Bucket management
│   ├── config/               # Configuration management
│   ├── metadata/             # BadgerDB metadata store
│   ├── metrics/              # System metrics collection
│   ├── object/               # Object storage operations
│   ├── server/               # HTTP server setup
│   ├── storage/              # Filesystem storage backend
│   └── db/                   # SQLite for auth (legacy)
├── pkg/s3compat/             # S3 API implementation
│   ├── handler.go            # Main S3 handler
│   ├── bucket_ops.go         # Bucket operations
│   ├── object_ops.go         # Object operations
│   ├── multipart.go          # Multipart upload
│   └── auth.go               # S3 signature validation
├── web/
│   ├── embed.go              # Frontend embedding
│   └── frontend/             # Next.js application
│       ├── src/
│       │   ├── components/   # React components
│       │   ├── pages/        # Page components
│       │   ├── lib/          # API client & utilities
│       │   └── hooks/        # Custom React hooks
│       └── public/           # Static assets
├── build/                    # Build output directory
└── data/                     # Runtime data (gitignored)
```

## 🧪 Testing

### Automated Test Suite

**Frontend Tests** (Complete - 100%):
- ✅ **Login.test.tsx** - 11 tests covering authentication flows
  - Form rendering and validation
  - Successful login with redirection
  - Error handling (401, network errors)
  - Two-Factor Authentication (2FA) flow
  - Loading states and user interactions
- ✅ **Dashboard.test.tsx** - 12 tests covering dashboard UI
  - Metrics display (buckets, objects, storage, users)
  - Navigation between sections
  - Health check integration
  - Empty states and error handling
- ✅ **Buckets.test.tsx** - 19 tests covering bucket management
  - List, create, delete operations
  - Search and filtering functionality
  - Pagination controls
  - Empty states and error handling
- ✅ **Users.test.tsx** - 22 tests covering user management
  - User list display with roles and status
  - Search by username and email
  - Create and delete user flows
  - User metrics display
  - Permission-based UI rendering
- ✅ **Test Infrastructure**:
  - Vitest + React Testing Library setup
  - Custom render helpers with providers (React Query, Router)
  - Complete mocks (API, Window, LocalStorage, SweetAlert)
  - Test scripts: test, test:run, test:ui, test:coverage
  - **64 tests total**, 100% pass rate, CI/CD ready

**Backend Tests** (Phase 1 - Complete):
- ✅ **internal/auth/** - 11 tests covering authentication flows
  - Password hashing/verification, JWT validation, 2FA setup
  - Account lockout, rate limiting, user CRUD, access keys
  - Coverage: 27.8% of statements
- ✅ **internal/server/** - 9 tests covering Console API endpoints
  - Login, user management, bucket operations, metrics
  - Coverage: 4.9% of statements
- ✅ **pkg/s3compat/** - 18 tests covering S3 API operations
  - Bucket operations, object operations, versioning, lifecycle
  - Multipart uploads, range requests, batch deletes, copy operations
  - Bucket policies, CORS, tagging, ListObjectVersions
  - Coverage: 30.9% of statements (improved from 16.6%)
- ✅ **internal/logging/** - 26 tests covering logging infrastructure
  - HTTP output tests (7 tests): Batching, authentication, flush intervals, close, server errors
  - Syslog output tests (6 tests): TCP connection, message delivery, log levels, multiple writes
  - Manager tests (13 tests): Configuration, log levels, formats, caller info, error handling
  - Coverage: 100% pass rate, all core logging functionality validated
- ✅ **internal/object/** - Race condition verification tests (2 tests)
  - Concurrent multipart uploads (10 parts simultaneously)
  - Multiple simultaneous uploads (5 uploads, 25 parts total)
  - Verified: No race conditions, BadgerDB handles concurrency correctly
- ✅ **internal/cluster/** - 27 tests covering cluster operations
  - Cluster management tests (22 tests): Node CRUD, health checks, configuration
  - **Cluster replication integration tests (5 tests)**: Simulated two-node cluster
    - SimulatedNode infrastructure with in-memory storage and SQLite
    - HMAC-SHA256 authentication (valid and invalid signatures)
    - Tenant synchronization with checksum validation
    - Object replication (PUT) with authenticated HTTP requests
    - Delete replication with HMAC signatures
    - Self-replication prevention validation
  - Uses pure Go SQLite driver (`modernc.org/sqlite`) - no CGO required
  - All tests pass in under 2 seconds
- ✅ **Test Infrastructure**:
  - Helper functions for test setup and authentication
  - Isolated test environments with temporary databases
  - AWS SigV4 authentication for S3 API tests
  - Mock servers for HTTP and syslog testing
  - SimulatedNode infrastructure for cluster testing
  - **71 backend tests**, 100% pass rate, CI/CD ready

**Feature Implementation**:
- ✅ **Lifecycle Worker** - 100% Complete (November 20, 2025)
  - Noncurrent version expiration (deletes old object versions)
  - Expired delete marker cleanup (removes "zombie" delete markers)
  - Worker runs hourly, processes all buckets with lifecycle policies
  - Full AWS S3 compatibility for lifecycle management

**Bug Verification**:
- ✅ **Race conditions**: Tested and verified - no issues found
- ✅ **Error consistency**: Verified - S3 uses XML, Console uses JSON (by design)
- ✅ **Zero known bugs** - All reported issues investigated and resolved

**Run Tests**:
```bash
# Backend tests
go test ./...                              # Run all backend tests
go test -cover ./internal/...              # Run with coverage
go test -v ./internal/auth/                # Verbose output
go test ./internal/logging                 # Run logging tests
go test ./internal/cluster -v              # Run cluster tests (includes integration tests)
go test ./internal/cluster -v -run "TestHMAC|TestTenant|TestObject|TestDelete|TestSelf"  # Run only cluster replication tests

# Frontend tests
cd web/frontend
npm run test                     # Run in watch mode (development)
npm run test:run                 # Run once (CI/CD)
npm run test:ui                  # Visual interface (recommended)
npm run test:coverage            # Generate coverage report
```

### Testing with AWS CLI

```bash
# Step 1: Create access keys via web console
# - Login to http://localhost:8081 (admin/admin)
# - Go to Users section
# - Click "Create Access Key" for your user
# - Copy the generated Access Key ID and Secret Access Key

# Step 2: Configure AWS CLI with your generated credentials
aws configure --profile maxiofs
AWS Access Key ID: [your-generated-access-key]
AWS Secret Access Key: [your-generated-secret-key]
Default region name: us-east-1
Default output format: json

# Step 3: Use AWS CLI
# Create bucket
aws --profile maxiofs --endpoint-url http://localhost:8080 s3 mb s3://test-bucket

# Upload file
aws --profile maxiofs --endpoint-url http://localhost:8080 s3 cp file.txt s3://test-bucket/

# List objects
aws --profile maxiofs --endpoint-url http://localhost:8080 s3 ls s3://test-bucket/

# Download file
aws --profile maxiofs --endpoint-url http://localhost:8080 s3 cp s3://test-bucket/file.txt downloaded.txt

# Bulk delete
aws --profile maxiofs --endpoint-url http://localhost:8080 s3 rm s3://test-bucket/ --recursive
```

### Stress Testing with Warp

MaxIOFS has been tested with [MinIO Warp](https://github.com/minio/warp) for performance validation:

```bash
# Install warp
# Download from https://github.com/minio/warp/releases

# Run mixed workload test
warp mixed --host localhost:8080 \
  --access-key YOUR_ACCESS_KEY \
  --secret-key YOUR_SECRET_KEY \
  --bucket test-bucket \
  --duration 5m

# Example results (hardware dependent):
# - Successfully handles 7000+ objects
# - Bulk delete operations complete without errors
# - Metadata consistency maintained under load
# - No BadgerDB transaction conflicts with retry logic
```

**Note**: Performance varies significantly based on hardware, OS, and workload characteristics.

## ⚠️ Known Limitations

### Critical
- ⚠️ Filesystem backend only (no S3/GCS/Azure backends)
- ⚠️ Object Lock not validated with Veeam or other backup tools
- ⚠️ Multi-tenancy needs more real-world production testing
- ⚠️ Cluster features tested with up to 5 nodes (larger deployments need validation)

### Performance
- ✅ **Validated with MinIO Warp stress testing (7000+ objects)**
- ✅ **Bulk operations tested and working correctly**
- ✅ **BadgerDB transaction conflicts resolved with retry logic**
- Local benchmarks: ~374 MB/s writes, ~1703 MB/s reads
- *Numbers are from local tests and vary by hardware*

### Security
- ⚠️ Default credentials must be changed
- ⚠️ HTTPS recommended for production
- ⚠️ No third-party security audit performed
- ✅ Comprehensive audit logging system (20+ event types)
- ✅ Two-Factor Authentication (2FA) with TOTP
- ✅ Server-side encryption at rest (AES-256-CTR)
- ✅ Security testing 100% complete (rate limiting, permissions, JWT, credential protection)

### Bugs
- ✅ **Zero known bugs** (November 2025)
- ✅ All reported issues verified and resolved
- ✅ Race conditions tested - no issues found
- ✅ Concurrent operations handle correctly

## 🛠️ Development

### Building from Source

```bash
# Install dependencies
cd web/frontend
npm install
cd ../..

# Build
# Windows/Linux/macOS
make build   
```

### Running in Development Mode

```bash
# Terminal 1: Backend
go run cmd/maxiofs/main.go --data-dir ./data --log-level debug

# Terminal 2: Frontend (optional, for UI dev)
cd web/frontend
npm run dev
```

### Running Tests

```bash
# Backend unit tests
go test ./internal/... -v

# With coverage
go test ./internal/... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Available Make Targets

**Build targets:**
```bash
make build           # Build for current platform
make build-all       # Build for all platforms (Linux, Windows, macOS)
make build-web       # Build frontend only
```

**Docker targets (cross-platform):**
```bash
make docker-build    # Build Docker image
make docker-up       # Start with docker-compose
make docker-down     # Stop services
make docker-logs     # View logs
make docker-monitoring  # Start with Prometheus/Grafana
make docker-clean    # Clean volumes and containers
```

**Docker targets (Windows PowerShell):**
```bash
make docker-build-ps     # Build with PowerShell script
make docker-run-ps       # Build and run
make docker-up-ps        # Start containers
make docker-down-ps      # Stop containers
make docker-monitoring-ps # Start with monitoring
make docker-clean-ps     # Clean with script
```

**Development targets:**
```bash
make dev            # Run in development mode
make test           # Run all tests
make lint           # Run linter
make clean          # Clean build artifacts
```

**Package targets:**
```bash
make deb            # Build Debian package
make rpm            # Build RPM package (requires alien)
```

## 🔒 Security Best Practices

1. **Change default credentials** immediately
2. **Use HTTPS** in production (TLS certs or reverse proxy)
3. **Configure firewall** rules (restrict port access)
4. **Regular backups** of data directory
5. **Monitor logs** for suspicious activity
6. **Update regularly** for security patches

## 📝 Contributing

Contributions welcome! Please:
1. Fork the repository
2. Create a feature branch
3. Write tests for new features
4. Ensure all tests pass
5. Submit a pull request

## 🗺️ Roadmap

### Completed (v0.6.0-beta - Current)
- [x] **Bucket Replication** (S3-compatible cross-bucket replication to AWS S3, MinIO, or other MaxIOFS instances)
  - Realtime, scheduled, and batch replication modes
  - S3 protocol-level replication with endpoint URL, access key, secret key configuration
  - Queue-based async processing with retry logic and conflict resolution
  - Web Console integration with visual rule management in bucket settings
  - 23 automated tests covering all functionality
- [x] **Advanced Logging System** (HTTP and Syslog output with batching)
- [x] **Comprehensive Test Coverage Expansion** (504 backend tests, ~53% coverage)
- [x] **Multiple Bug Fixes** (Session management, VEEAM SOSAPI, ListObjectVersions)

### Completed (v0.4.2-beta)
- [x] **Global Bucket Uniqueness** (Bucket names globally unique across all tenants for AWS S3 compatibility)
- [x] **S3-Compatible URLs** (Presigned and share URLs without tenant prefix for standard S3 client compatibility)
- [x] **Automatic Tenant Resolution** (Backend automatically resolves bucket ownership from bucket name)
- [x] **Bucket Notifications (Webhooks)** (AWS S3 compatible event notifications - ObjectCreated, ObjectRemoved, ObjectRestored)
- [x] **Real-Time Push Notifications (SSE)** (Server-Sent Events for instant admin alerts when users are locked)
- [x] **Dynamic Security Configuration** (Configure rate limits and lockout thresholds without restart via Settings page)
- [x] **Configurable Rate Limiting** (IP-based rate limiting adjustable from 5 to 15+ attempts/minute)
- [x] **Configurable Account Lockout** (Account lockout threshold and duration adjustable without restart)
- [x] **Critical Bug Fixes** (Rate limiter double-counting, failed attempts counter reset, SSE callback execution, frontend token detection)
- [x] **Frontend Modal State Management** (Fixed presigned URL modal state persistence bug)

### Completed (v0.4.1-beta)
- [x] **Server-Side Encryption (SSE)** (AES-256-CTR encryption at rest with persistent master key)
- [x] **Streaming Encryption** (Constant memory usage ~32KB, supports files of any size)
- [x] **Flexible Encryption Control** (Dual-level: server + per-bucket settings)
- [x] **Settings Persistence** (SQLite-based runtime configuration storage)
- [x] **Metrics Historical Storage** (BadgerDB for persistent metrics across restarts)
- [x] **Critical Security Fixes** (Tenant menu visibility, admin privilege escalation, password change detection)
- [x] **Enhanced UI/UX** (Unified card design, improved audit logs, encryption status indicators)
- [x] **Documentation Package** (Complete offline docs in Debian packages at /usr/share/doc/maxiofs/)
- [x] **Automated Testing Suite Phase 1** (Auth + Server API tests, 28 tests total, 100% pass rate)
- [x] **Race Condition Verification** (Concurrent multipart uploads tested - no issues found)
- [x] **Bug Verification Complete** (All reported bugs investigated and resolved - zero known bugs)
- [x] **Lifecycle Feature 100% Complete** (Noncurrent version expiration + Expired delete marker cleanup)

### Completed (v0.4.0-beta)
- [x] **Dynamic Settings System** (23 configurable runtime settings)
- [x] **Comprehensive Audit Logging** (20+ event types, compliance-ready)
- [x] **Two-Factor Authentication** (TOTP with QR codes and backup codes)
- [x] **Prometheus/Grafana Integration** (Metrics endpoint + pre-built dashboard)
- [x] **Frontend UI Complete Redesign** (Modern design system, all 11 pages)
- [x] **User Management** (Role-based validation, proper permission enforcement)
- [x] **Security Testing** (100% complete - all applicable tests passing: rate limiting, permissions, 2FA, JWT, audit, credential protection, bucket policies, CORS configuration)
- [x] **Quota System Fixed** (Frontend + S3 API working correctly)
- [x] **Multi-tenancy Validation** (Complete resource isolation tested)
- [x] **Docker Support** (Docker Compose with Grafana/Prometheus)

### Completed (v0.3.2-beta)
- [x] **S3 Core Compatibility Complete** (All major operations tested)
- [x] **Bucket Tagging UI** (Visual tag manager with Console API)
- [x] **CORS UI** (Visual rule editor with dual visual/XML modes)
- [x] **Warp stress testing completed** (7000+ objects validated)
- [x] **Bulk operations validated** (DeleteObjects working)
- [x] **Metadata consistency verified** under concurrent load
- [x] **Cross-platform builds** (Windows, Linux x64/ARM64, macOS)
- [x] **Debian packaging support** (.deb packages for easy installation)
- [x] **Session management** (Idle timer and timeout enforcement)
- [x] **Production bug fixes** (Object deletion, GOVERNANCE mode, URL redirects)

### Short Term (v0.5.0)
- [ ] **Performance Profiling & Optimization** (Memory/CPU profiling, load testing)
- [ ] **CI/CD Pipeline** (GitHub Actions for automated builds and releases)
- [x] ~~**Bucket Notifications** (Webhooks on object events)~~ **IMPLEMENTED in v0.4.2-beta**
- [x] ~~**Bucket Replication** (Basic S3-compatible replication)~~ **IMPLEMENTED in v0.5.0-beta**
- [x] ~~**Multi-Node Cluster Support** (HA cluster with routing and failover)~~ **IMPLEMENTED in v0.6.0-beta**
- [x] ~~**Cluster Bucket Replication** (Node-to-node replication for HA)~~ **IMPLEMENTED in v0.6.0-beta**
- [x] ~~**Cluster Dashboard UI** (Web console for cluster management)~~ **IMPLEMENTED in v0.6.0-beta**
- [ ] **Multi-Region Replication** (Geographic replication with region health checks and automatic failover)
- [ ] **Encryption Key Rotation** (Automatic key rotation with dual-key support)
- [ ] **Per-Tenant Encryption Keys** (Tenant-level key isolation for multi-tenancy)
- [ ] **HSM Integration** (Hardware Security Module for production key management)
- [ ] **Metadata Encryption** (Encrypt object metadata in addition to object data)
- [ ] Official Docker images on Docker Hub
- [ ] Hot reload for frontend development

### Medium Term (v0.6.0-v0.8.0)
- [x] ~~Object versioning (full implementation with complete lifecycle)~~ **100% IMPLEMENTED** - Versioning + lifecycle worker with noncurrent version expiration AND expired delete marker cleanup
- [x] ~~Bucket replication (cross-bucket/cross-region)~~ **BASIC IMPLEMENTATION COMPLETE in v0.5.0** - S3-compatible replication to external endpoints
- [ ] **Advanced Replication** (Bidirectional sync, multi-region with automatic failover)
- [ ] **Encryption Algorithm Selection** (ChaCha20-Poly1305, AES-GCM options)
- [ ] **Compliance Reporting** (Encryption coverage, key usage analytics)
- [ ] Kubernetes Helm charts
- [ ] CI/CD pipeline with automated releases

### Long Term (v1.0.0+)
- [ ] Additional storage backends (S3, GCS, Azure)
- [ ] LDAP/SSO integration
- [ ] **External Key Management Service** (AWS KMS, Azure Key Vault, HashiCorp Vault)

## 📄 License

MIT License - See LICENSE file for details

## 💬 Support

- **Issues**: [GitHub Issues](https://github.com/aluisco/maxiofs/issues)
- **Discussions**: [GitHub Discussions](https://github.com/aluisco/maxiofs/discussions)
- **Documentation**: See `/docs` directory

---

**⚠️ Reminder**: This is a BETA project. Suitable for development, testing, and staging environments. Production use requires your own extensive testing. Always backup your data.
