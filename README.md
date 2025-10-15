# MaxIOFS - S3-Compatible Object Storage

**Version**: 0.2.0-alpha
**Status**: Active Development (Alpha Phase)
**License**: MIT

MaxIOFS is an S3-compatible object storage system built in Go with an embedded Next.js web interface. Designed to be simple, portable, and deployable as a single binary.

## ⚠️ Project Status

**This project is in ALPHA phase**. This means:
- ✅ Works for basic to intermediate use cases
- ⚠️ May have undiscovered bugs
- ⚠️ API may change without prior notice
- ❌ DO NOT use in production without extensive testing
- ❌ DO NOT trust as the only copy of important data

## 🎯 Features

### S3 API Compatibility
- ✅ Core operations (PutObject, GetObject, DeleteObject, ListObjects)
- ✅ Bucket management (Create, List, Delete, GetBucketInfo)
- ✅ Multipart uploads (complete workflow)
- ✅ Presigned URLs (GET/PUT with expiration)
- ✅ Object Lock (COMPLIANCE/GOVERNANCE modes)
- ✅ Bucket Versioning (Enable/Suspend/Query)
- ✅ Bucket Policy (Get/Put/Delete JSON policies)
- ✅ Bucket CORS (Get/Put/Delete CORS rules)
- ✅ Bucket Lifecycle (Get/Put/Delete lifecycle configurations)
- ✅ Object Tagging (Get/Put/Delete tags)
- ✅ Object ACL (Get/Put access control lists)
- ✅ Object Retention (WORM with legal hold support)
- ✅ CopyObject (with metadata preservation)

### Authentication & Security
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
- ✅ Bucket configuration editor (Versioning, Policy, CORS, Lifecycle, Object Lock)
- ✅ System settings overview
- ✅ Security audit page
- ✅ Metrics monitoring (System, Storage, Requests, Performance)

### Deployment
- ✅ Single binary with embedded frontend
- ✅ HTTP and HTTPS support
- ✅ Configurable via CLI flags
- ✅ SQLite database (embedded)
- ✅ Filesystem storage backend

## 🚀 Quick Start

### Prerequisites
- Go 1.21+ (for building)
- Node.js 18+ (for building)

### Build

```bash
# Windows
.\build.bat

# Linux/macOS
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
- **S3 API**: `http://localhost:8080`
  - Default Access Key: `maxioadmin`
  - Default Secret Key: `maxioadmin`

**⚠️ Change default credentials immediately!**

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
│  - SQLite metadata database            │
│  - Filesystem object storage           │
│  - Atomic write operations             │
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
│   ├── database/             # SQLite database layer
│   ├── metrics/              # System metrics collection
│   ├── object/               # Object storage operations
│   ├── server/               # HTTP server setup
│   ├── storage/              # Storage backend
│   └── tenant/               # Multi-tenancy logic
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

## 🧪 Testing with AWS CLI

```bash
# Configure credentials
aws configure --profile maxiofs
AWS Access Key ID: maxioadmin
AWS Secret Access Key: maxioadmin
Default region name: us-east-1
Default output format: json

# Create bucket
aws --profile maxiofs --endpoint-url http://localhost:8080 s3 mb s3://test-bucket

# Upload file
aws --profile maxiofs --endpoint-url http://localhost:8080 s3 cp file.txt s3://test-bucket/

# List objects
aws --profile maxiofs --endpoint-url http://localhost:8080 s3 ls s3://test-bucket/

# Download file
aws --profile maxiofs --endpoint-url http://localhost:8080 s3 cp s3://test-bucket/file.txt downloaded.txt
```

## ⚠️ Known Limitations

### Critical
- ⚠️ Single-node only (no clustering/replication)
- ⚠️ Filesystem backend only (no S3/GCS/Azure backends)
- ⚠️ Limited performance testing (not validated at scale)
- ⚠️ Multi-tenancy needs more real-world testing
- ⚠️ Object Lock not validated with Veeam or other backup tools

### Performance
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
.\build.bat  # Windows
make build   # Linux/macOS
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

### Short Term (v0.3.0)
- [ ] Comprehensive test suite (80%+ coverage)
- [ ] Complete API documentation
- [ ] Docker images
- [ ] Performance benchmarking suite

### Medium Term (v0.4.0-v0.5.0)
- [ ] Object versioning (full implementation)
- [ ] Prometheus metrics export
- [ ] Kubernetes Helm charts
- [ ] CI/CD pipeline

### Long Term (v1.0.0+)
- [ ] Multi-node clustering
- [ ] Replication between nodes
- [ ] Additional storage backends (S3, GCS, Azure)
- [ ] LDAP/SSO integration

## 📄 License

MIT License - See LICENSE file for details

## 💬 Support

- **Issues**: [GitHub Issues](https://github.com/yourusername/maxiofs/issues)
- **Discussions**: [GitHub Discussions](https://github.com/yourusername/maxiofs/discussions)
- **Documentation**: See `/docs` directory

---

**⚠️ Reminder**: This is an ALPHA project. Use at your own risk. Always backup your data.
