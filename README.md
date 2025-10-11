# MaxIOFS - Modern S3-Compatible Object Storage with Multi-Tenancy

MaxIOFS is a high-performance S3-compatible object storage system built in Go with an embedded Next.js web interface. It provides enterprise-grade object storage with WORM compliance, multi-tenancy isolation, advanced monitoring, and complete AWS S3 API compatibility.

## 🚀 Features

- **S3 API Compatibility**: Complete AWS S3 API implementation with 23+ advanced operations
- **Multi-Tenancy**: Full tenant isolation with quotas, permissions, and resource tracking
- **Object Lock & WORM**: Full support for Write Once Read Many compliance (COMPLIANCE/GOVERNANCE modes)
- **Object Sharing**: Clean, revocable share URLs with database-backed validation ✨ NEW
- **Veeam Compatible**: Works as immutable backup repository for Veeam Backup & Replication
- **Single Binary**: Self-contained executable (development: separate frontend/backend)
- **Modern Web Interface**: Next.js 15.5 + React 19 dashboard with real-time metrics
- **Role-Based Access Control**: Global Admin, Tenant Admin, and User roles
- **Dual Authentication**: Console web login (JWT + cookies) + S3 API access keys
- **High Performance**: 374 MB/s writes, 1703 MB/s reads (benchmarked)
- **Pluggable Storage**: Filesystem backend (S3, GCS, Azure planned)
- **Enterprise Security**: AES-256-GCM encryption, production-ready logging
- **Real-time Monitoring**: Prometheus metrics, structured logging, rate limiting

## 🏗️ Architecture

```
┌─────────────────┐
│   Web Console   │ ← Next.js 15.5 + React 19 (port 3001)
├─────────────────┤
│   Console API   │ ← REST API for frontend (port 8081, JWT auth)
├─────────────────┤
│   S3 API        │ ← S3-compatible API (port 8080, S3 auth)
├─────────────────┤
│ Multi-Tenancy   │ ← Tenant isolation & RBAC
├─────────────────┤
│   Core Engine   │ ← Bucket/Object/Auth Managers
├─────────────────┤
│   Storage       │ ← Filesystem Backend
└─────────────────┘
```

## 🆕 What's New in v1.2 - Frontend Upgrade & UI Cleanup

### Major Frontend Upgrade
- **Next.js 15.5**: Upgraded from 14.0.0 for better performance and features
- **React 19**: Upgraded from 18 for improved reactivity
- **Removed Placeholders**: Eliminated all non-implemented features from UI
- **Honest UI**: Only shows features that actually work in the backend

### Simplified Bucket Management
- **Bucket Creation**: Streamlined to show only implemented options
  - Removed: KMS encryption, Storage transitions, Requester Pays, Transfer Acceleration
  - Kept: Versioning, Object Lock, AES-256-GCM, Access Control, Tags
- **Bucket Settings**: Changed to read-only display of real configuration
  - Shows: Object Lock status, Encryption, Public Access Control, Tags
  - Removed: Editable fields, CORS, Logging, Notifications (not implemented)

### Dashboard Improvements
- **Real-Time Health**: Live S3 API health check from port 8080/health
- **Encrypted Buckets Counter**: Shows how many buckets have encryption enabled
- **Role-Based Visibility**: Metrics button hidden for non-Global Admin users

### Navigation Fixes
- **Improved Back Button**: Fixed navigation loops in bucket details
- **Direct Routing**: Always returns to bucket list, not browser history

## What's in v1.1 - Multi-Tenancy

### Complete Tenant Isolation
- **Tenant CRUD Operations**: Create, manage, and delete tenants
- **Resource Quotas**: Per-tenant limits for storage, buckets, and access keys
- **Real-time Statistics**: Live tracking of tenant resource usage
- **Status Management**: Active/inactive tenant states

### Role-Based Access Control
- **Global Admin**: Full system access across all tenants
- **Tenant Admin**: Manage resources within assigned tenant
- **Tenant User**: Basic access to tenant resources

### Object Sharing & Security
- **Object Sharing**: One-click share/unshare with clean URL generation
- **Revocable Links**: Database-backed validation for share URLs
- **Production Security**: All sensitive logs removed
- **Dual Token Storage**: localStorage + cookies for seamless auth

## 🚀 Quick Start

### Prerequisites

- Go 1.21+
- Node.js 18+
- npm/yarn

### Building MaxIOFS

MaxIOFS provides two build methods that work on **Windows, Linux, and macOS**:

#### Option 1: Using build.bat (Windows Recommended)
```bash
# Simple build (version defaults to "dev")
build.bat

# Build with specific version
set VERSION=v1.5.0
build.bat

# Using PowerShell
$env:VERSION="v1.5.0"
.\build.bat
```

#### Option 2: Using Makefile (Cross-Platform)
```bash
# Windows (PowerShell/cmd)
make build                    # Build frontend + backend
make build-server             # Build only backend
make VERSION=v1.5.0 build     # Build with version

# Linux/macOS (bash/zsh)
make build
make build-server VERSION=v1.5.0
make build-all                # Build for all platforms
```

**What gets built:**
- Frontend: Next.js static export in `web/frontend/out/`
- Backend: Go binary in `build/maxiofs.exe` (Windows) or `build/maxiofs` (Linux/macOS)
- The backend binary embeds the frontend automatically

**Version Information:**
Both build methods inject version, git commit, and build date into the binary:
```bash
.\build\maxiofs.exe --version
# Output: maxiofs version v1.5.0 (commit: abc1234, built: 2025-10-11T15:24:05Z)
```

**Note about Git Commit:**
If you see `commit: unknown`, it means git can't access the repository. Fix with:
```bash
git config --global --add safe.directory C:/Users/YourUser/Projects/MaxIOFS
```

### Running MaxIOFS

MaxIOFS requires the `--data-dir` flag to specify where to store data.

```bash
# Basic usage (HTTP)
.\build\maxiofs.exe --data-dir ./data

# With TLS/HTTPS (both cert and key required)
.\build\maxiofs.exe --data-dir ./data --tls-cert server.crt --tls-key server.key

# With debug logging
.\build\maxiofs.exe --data-dir ./data --log-level debug

# Check version
.\build\maxiofs.exe --version

# Show help
.\build\maxiofs.exe --help
```

**TLS Configuration:**
- If you provide `--tls-cert` and `--tls-key`, both servers (Console API and S3 API) will run in HTTPS mode
- Both flags must be provided together (one without the other will cause an error)
- Certificates should be in PEM format

**Default Credentials:**
- **Web Console**: `http://localhost:8081` → Login: `admin` / `admin` (Global Admin)
- **S3 API**: `http://localhost:8080` → Access Key: `maxioadmin` / Secret: `maxioadmin`

**With TLS enabled:**
- **Web Console**: `https://localhost:8081`
- **S3 API**: `https://localhost:8080`

### Frontend Development

The frontend is embedded in the binary by default. For development:

```bash
cd web/frontend
npm install
npm run dev      # Development server on port 3001 (Next.js 15.5 + React 19)
npm run build    # Production build (static export to web/frontend/out)
```

**Note**: The production build uses Pages Router for static export compatibility. The embedded version in the binary serves from the `web/frontend/out` directory.

## 📋 Key Capabilities

### Multi-Tenancy & Access Control

- **Tenant Isolation**: Complete separation of resources between tenants
- **Quota Management**: Per-tenant limits on storage (bytes), buckets, and access keys
- **Real-time Tracking**: Live statistics showing current usage vs. limits
- **Role Hierarchy**: Global Admin → Tenant Admin → Tenant User
- **Permission Filtering**: Users only see resources they have access to
- **Tenant Association**: Users and buckets belong to specific tenants

### Object Lock & WORM

- **COMPLIANCE Mode**: Objects cannot be deleted by anyone until retention expires
- **GOVERNANCE Mode**: Requires special permissions to bypass retention
- **Default Retention**: Auto-apply retention policies on upload
- **Legal Hold**: Additional immutability layer independent of retention
- **Veeam Integration**: Headers support for `x-amz-object-lock-mode` and retention dates

### S3 API Operations (23+)

**Bucket Operations:**
- Policy, Lifecycle, CORS configuration (Get/Put/Delete)
- Object Lock configuration
- Versioning (planned)

**Object Operations:**
- Standard CRUD (Get/Put/Delete/List/Head)
- Retention and Legal Hold management
- Tagging and ACL
- Copy and Multipart Upload (6 operations)
- Presigned URLs (V4/V2)
- Batch operations (1000 objects/request)

### Web Console Features

- **Dashboard**: Real-time metrics and system overview
- **Tenant Management**: Create tenants, set quotas, monitor usage ✨ NEW
- **Bucket Management**: Create buckets with Object Lock wizard + tenant ownership ✨ NEW
- **Object Browser**: Upload/download with retention display
- **Object Sharing**: Share objects with clean URLs, revoke shares anytime ✨ NEW
- **User Management**: Create users, assign to tenants, manage access keys ✨ NEW
- **Settings**: System configuration and monitoring
- **Role-Based UI**: Smart hiding of features based on user permissions ✨ NEW

## 🎯 Use Cases

### Multi-Tenant SaaS Platform

MaxIOFS enables complete tenant isolation for SaaS applications:

1. Create tenants for each customer/organization
2. Set resource quotas (storage, buckets, keys) per tenant
3. Create tenant admins to manage their own resources
4. Enforce resource limits automatically
5. Monitor per-tenant usage in real-time

**Benefits:**
- Complete data isolation between customers
- Prevent resource exhaustion with quotas
- Delegate administration to tenant admins
- Track usage for billing/reporting

### Immutable Backups with Veeam

MaxIOFS provides on-premise immutable storage for Veeam Backup & Replication:

1. Create bucket with Object Lock enabled (COMPLIANCE mode, 14+ days retention)
2. Configure Veeam to use MaxIOFS as S3 repository
3. Backups are automatically immutable for the retention period
4. Protection against ransomware and accidental deletion

**Veeam Configuration:**
- Service Point: `http://your-server:8080`
- Access Key: Your S3 credentials
- Enable "Make recent backups immutable for X days"

### General Object Storage

- Document management and archival (with tenant isolation)
- Media asset storage (per-customer or per-project)
- Log aggregation (multi-tenant logging)
- Data lake storage
- Application file storage with quota enforcement
- **Public file sharing**: Share documents via clean URLs without authentication ✨ NEW

## 📊 Performance

Benchmark results (filesystem backend):

- **Writes**: 374 MB/s (100MB files)
- **Reads**: 1703 MB/s (10MB files)
- **Memory**: ~15KB/op writes, ~11KB/op reads
- **Concurrency**: 50+ simultaneous operations
- **Multi-Tenancy Overhead**: < 5% (optimized filtering)

## 🔧 Configuration

Configuration via environment variables, YAML files, or command-line flags:

```yaml
# Example config.yaml
server:
  s3_port: 8080
  console_port: 8081
storage:
  backend: filesystem
  data_dir: ./data
auth:
  jwt_secret: your-secret-key
  default_admin: admin
tenancy:
  enabled: true
  default_quotas:
    storage_bytes: 107374182400  # 100GB
    max_buckets: 100
    max_access_keys: 10
```

## 📖 Documentation

**[📚 Complete Documentation Index](./docs/INDEX.md)**

### Quick Links
- **[Quick Start](./docs/QUICKSTART.md)** - Get running in 5 minutes
- **[API Reference](./docs/API.md)** - Complete S3 + Console API docs
- **[Deployment Guide](./docs/DEPLOYMENT.md)** - Production deployment
- **[Configuration](./docs/CONFIGURATION.md)** - All config options
- **[Security Guide](./docs/SECURITY.md)** - Security & hardening
- **[Multi-Tenancy](./docs/MULTI_TENANCY.md)** - Tenant management
- **[Architecture](./docs/ARCHITECTURE.md)** - System design
- **[Build Guide](./docs/BUILD.md)** - Build instructions

See [docs/INDEX.md](./docs/INDEX.md) for complete documentation index.

## 🧪 Testing

```bash
# Run all tests
make test

# Unit tests
go test ./internal/... -v

# Integration tests
go test ./tests/integration/... -v

# Benchmarks
go test ./tests/performance/... -bench=. -benchmem
```

**Test Coverage:**
- 29 unit tests (100% pass)
- 18 integration tests (100% pass)
- 18 performance benchmarks
- Multi-tenancy integration tested

## 📦 Deployment

### Single Binary

```bash
go build -o maxiofs ./cmd/maxiofs
./maxiofs --config config.yaml
```

### Docker

```bash
docker build -t maxiofs .
docker run -p 8080:8080 -p 8081:8081 -v ./data:/data maxiofs
```

### Kubernetes

```bash
# Coming soon: Helm charts with StatefulSet for persistence
```

## ⚠️ Security Notes

**Production-Ready Security:**

- ✅ **Password Hashing**: Bcrypt with automatic migration from SHA-256
- ✅ **Rate Limiting**: IP-based rate limiting (5 attempts/min)
- ✅ **Account Lockout**: Auto-lockout after 5 failed attempts (15 min)
- ✅ **Authentication**: JWT + cookies (dual storage)
- ✅ **Logging**: All sensitive logs removed
- ✅ **Multi-Tenancy**: Complete isolation with quotas
- ✅ **Audit Logging**: Full audit trail for compliance

**Production Checklist:**
- [ ] Change default credentials
- [ ] Generate strong JWT secret (32+ bytes)
- [ ] Enable HTTPS/TLS (use reverse proxy)
- [ ] Configure restrictive CORS
- [ ] Review [Security Guide](./docs/SECURITY.md)

See [docs/SECURITY.md](./docs/SECURITY.md) for complete security documentation.

## ⚙️ Known Limitations

### Web Interface Scalability
- **Object Listing**: Limited to first 10,000 objects per bucket for performance
  - For buckets with >10,000 objects, the count will show as `10,000+`
  - Use S3 API clients (aws-cli, s3cmd, MinIO client) for managing large buckets
  - Individual bucket views support pagination with prefix filtering
- **Backup Repositories**: Not recommended to browse Veeam backup buckets through web UI
  - Veeam creates thousands of small objects (metadata, blocks)
  - Use Veeam's native interface for backup management
  - S3 API performance is unaffected - only web UI has limits

### Veeam Integration
- **Multi-Bucket Auto-Provisioning**: Currently enabled by default
  - After creating a repository, disable manually with PowerShell:
    ```powershell
    $repo = Get-VBRBackupRepository -Name "YourRepoName"
    $repo.Options.MultiBucketOptions.IsEnabled = $false
    Set-VBRBackupRepository -Repository $repo
    ```
  - Working to identify the automatic detection mechanism MinIO uses

### Future Improvements
- Real object counting without loading all into memory
- Database-backed object metadata for instant queries
- Streaming pagination for large buckets
- Background indexing for search functionality

## 🛠️ Development Status

**Current Phase**: Phase 6 Complete - Multi-Tenancy & Security ✅

- ✅ **Phase 1-5**: Backend S3 API, Frontend, Object Lock, Testing
- ✅ **Phase 6**: Multi-tenancy, RBAC, Security hardening
- ⏳ **Phase 7**: Production deployment (bcrypt, Docker, K8s)
- ⏳ **Phase 8**: Additional storage backends

**Recent Achievements (v1.1):**
- Complete multi-tenant system with resource isolation
- Role-based access control (Global Admin, Tenant Admin, User)
- Real-time tenant statistics and quota tracking
- Production-ready frontend (all debug logs removed)
- Enhanced authentication flow with cookies
- UI restrictions based on user permissions
- **Object sharing system with clean URLs** ✨ NEW
- **Revocable share links with database validation** ✨ NEW
- **S3-compatible XML error responses** ✨ NEW
- **Clean UI (removed clutter)** ✨ NEW

See [TODO.md](./TODO.md) for detailed roadmap.

## 📊 Code Metrics

**Lines of Code:**
- Backend: ~16,000 lines (Go)
- Frontend: ~9,000 lines (TypeScript/React)
- Total: ~25,000 lines

**Components:**
- Backend: 26 Go packages
- Frontend: 30+ React components
- Tests: 65 total (unit + integration + benchmarks)

**Multi-Tenancy:**
- Tenant isolation: Complete
- Resource quotas: 3 types (storage, buckets, keys)
- User roles: 3 levels
- Real-time statistics: Yes

## 🚧 Known Limitations

- **Password Hashing**: SHA-256 (bcrypt needed for production)
- **Storage Backend**: Filesystem only (S3/GCS/Azure planned)
- **Object Versioning**: Placeholder only (not fully implemented)
- **Replication**: Single-node only (multi-node planned)
- **CORS**: Wildcard `*` (restrict for production)

## 🎯 Roadmap

### Immediate (Phase 7.1 - Critical)
- [ ] Bcrypt/Argon2 password hashing
- [ ] Rate limiting on login endpoint
- [ ] Production CORS configuration
- [ ] HTTPS/TLS setup

### Short-term (Phase 7.2-7.3)
- [ ] Complete API documentation
- [ ] CI/CD pipeline (GitHub Actions)
- [ ] Docker multi-stage builds
- [ ] Multi-tenancy documentation

### Medium-term (Phase 7.4-7.5)
- [ ] Kubernetes Helm charts
- [ ] Grafana dashboards
- [ ] Production deployment guide
- [ ] Alert configuration

### Long-term (Phase 8)
- [ ] S3/GCS/Azure backends
- [ ] Multi-node clustering
- [ ] Advanced lifecycle policies
- [ ] Enterprise integrations

## 📄 License

MIT License - see LICENSE file for details.

## 🤝 Contributing

Contributions welcome! Please ensure:
1. All tests pass (`make test`)
2. Code follows Go best practices
3. Documentation is updated
4. Commits are well-described
5. Multi-tenancy isolation is maintained

## 📞 Support

For issues and questions:
- GitHub Issues: Report bugs or request features
- Documentation: See `/docs` folder
- Quick Start: See [QUICKSTART.md](./docs/QUICKSTART.md)
- Multi-Tenancy: See [MULTI_TENANCY.md](./docs/MULTI_TENANCY.md) (coming soon)

---

**Version**: v1.1-dev (Multi-Tenancy)
**Status**: Production-Ready (Phases 1-6 Complete)
**Next Focus**: Production Hardening (Phase 7)
