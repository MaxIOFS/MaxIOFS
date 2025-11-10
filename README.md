# MaxIOFS - S3-Compatible Object Storage

**Version**: 0.3.2-beta
**Status**: Beta - 98% S3 Compatible
**License**: MIT

MaxIOFS is an S3-compatible object storage system built in Go with an embedded Next.js web interface. Designed to be simple, portable, and deployable as a single binary.

## 🎉 Project Status

**This project is now in BETA phase**. This means:
- ✅ **All core S3 features fully implemented and tested**
- ✅ **AWS CLI compatibility validated for all major operations**
- ✅ Successfully tested with MinIO Warp (7000+ objects, bulk operations validated)
- ✅ Metadata consistency verified under load
- ✅ Bucket Policy, Versioning, Lifecycle, and Delete Markers working
- ⚠️ Suitable for testing, development, and staging environments
- ⚠️ Production use requires your own extensive testing
- ❌ Not yet recommended as sole storage for critical production data

## 🎯 Features

### S3 API Compatibility (98%)
- ✅ Core operations (PutObject, GetObject, DeleteObject, ListObjects)
- ✅ Bucket management (Create, List, Delete, GetBucketInfo)
- ✅ Multipart uploads (complete workflow)
- ✅ Presigned URLs (GET/PUT with expiration)
- ✅ **Bulk operations (DeleteObjects - batch delete up to 1000 objects)**
- ✅ Object Lock (COMPLIANCE/GOVERNANCE modes)
- ✅ **Bucket Versioning** (Multiple versions, Delete Markers, ListObjectVersions) - *Fixed in 0.3.2*
- ✅ **Bucket Policy** (Complete PUT/GET/DELETE, JSON validation, AWS CLI compatible)
- ✅ **Bucket CORS** (Get/Put/Delete CORS rules, Visual UI editor)
- ✅ **Bucket Tagging** (Get/Put/Delete tags, Visual UI manager)
- ✅ **Bucket Lifecycle** (Get/Put/Delete lifecycle configurations)
- ✅ **Object Tagging** (Get/Put/Delete tags)
- ✅ Object ACL (Get/Put access control lists)
- ✅ Object Retention (WORM with legal hold support)
- ✅ CopyObject (with metadata preservation, cross-bucket support)
- ✅ **Conditional Requests** (If-Match, If-None-Match for HTTP caching) - *New in 0.3.2*
- ✅ **Range Requests** (Partial downloads with bytes=start-end)

### Authentication & Security
- ✅ **Two-Factor Authentication (2FA)** - TOTP-based with QR codes, backup codes - *New in 0.3.2*
- ✅ Dual authentication (JWT for Console, S3 Signature v2/v4 for API)
- ✅ Bcrypt password hashing
- ✅ Access keys with secret key management
- ✅ Rate limiting per endpoint
- ✅ Account lockout after failed attempts
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
- ✅ **Bucket configuration editors** (Visual + XML modes):
  - **Bucket Tags**: Visual tag manager with key-value pairs
  - **CORS**: Visual rule editor with origins, methods, headers
  - **Policy**: Template-based editor + raw JSON mode
  - **Versioning**: Enable/disable with one click
  - **Lifecycle**: Rule-based configuration
- ✅ System settings overview
- ✅ Security audit page
- ✅ Metrics monitoring (System, Storage, Requests, Performance)

### Storage & Performance
- ✅ **BadgerDB metadata store** (high-performance key-value database)
- ✅ **Transaction retry logic** for concurrent operations
- ✅ **Metadata-first deletion** (ensures consistency)
- ✅ Filesystem storage backend for objects
- ✅ Atomic write operations with rollback
- ✅ SQLite for authentication and user management

### Deployment & Monitoring
- ✅ Single binary with embedded frontend
- ✅ **Docker & Docker Compose support** - *New in 0.3.2*
- ✅ **Prometheus metrics endpoint** - *New in 0.3.2*
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

**Access:**
- Web Console: http://localhost:8081 (admin/admin)
- S3 API: http://localhost:8080
- Prometheus: http://localhost:9091 (monitoring profile only)
- Grafana: http://localhost:3000 (admin/admin, monitoring profile only)

**Other commands:**
```bash
make docker-down     # Stop all services
make docker-logs     # View logs
make docker-clean    # Clean volumes and containers
```

See [DEPLOYMENT.md](docs/DEPLOYMENT.md) for more Docker options.

### Option 2: Build from Source

### Prerequisites
- Go 1.21+ (for building)
- Node.js 18+ (for building)

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
│  - File operations                     │
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
- ⚠️ Single-node only (no clustering/replication)
- ⚠️ Filesystem backend only (no S3/GCS/Azure backends)
- ⚠️ Object Lock not validated with Veeam or other backup tools
- ⚠️ Multi-tenancy needs more real-world production testing

### Performance
- ✅ **Validated with MinIO Warp stress testing (7000+ objects)**
- ✅ **Bulk operations tested and working correctly**
- ✅ **BadgerDB transaction conflicts resolved with retry logic**
- Local benchmarks: ~374 MB/s writes, ~1703 MB/s reads
- *Numbers are from local tests and vary by hardware*

### Security
- ⚠️ Default credentials must be changed
- ⚠️ HTTPS recommended for production
- ⚠️ No security audit performed
- ⚠️ Audit logging incomplete

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

### Completed (v0.3.1-beta)
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

### Short Term (v0.4.0)
- [ ] Comprehensive test suite (80%+ coverage)
- [ ] Complete API documentation
- [ ] Docker images
- [ ] Security audit

### Medium Term (v0.4.0-v0.5.0)
- [ ] Object versioning (full implementation)
- [ ] Bucket replication (cross-bucket/cross-region)
- [ ] Prometheus metrics export
- [ ] Kubernetes Helm charts
- [ ] CI/CD pipeline

### Long Term (v1.0.0+)
- [ ] Multi-node clustering
- [ ] Replication between nodes (sync/async)
- [ ] Additional storage backends (S3, GCS, Azure)
- [ ] LDAP/SSO integration

## 📄 License

MIT License - See LICENSE file for details

## 💬 Support

- **Issues**: [GitHub Issues](https://github.com/aluisco/maxiofs/issues)
- **Discussions**: [GitHub Discussions](https://github.com/aluisco/maxiofs/discussions)
- **Documentation**: See `/docs` directory

---

**⚠️ Reminder**: This is a BETA project. Suitable for development, testing, and staging environments. Production use requires your own extensive testing. Always backup your data.
