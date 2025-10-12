# MaxIOFS - S3-Compatible Object Storage

**Version**: 1.1.0-alpha
**Status**: Alpha - Active Development
**License**: MIT

MaxIOFS is an S3-compatible object storage system built in Go with an embedded Next.js web interface. It's designed to be simple, portable, and easy to deploy as a single binary.

## ⚠️ Project Status

**This project is in ALPHA phase**. This means:
- ✅ Works for basic use cases
- ⚠️ May have undiscovered bugs
- ⚠️ API may change without prior notice
- ❌ DO NOT use in production without extensive testing
- ❌ DO NOT trust as the only copy of important data

## 🎯 What Works?

### Core Features
- ✅ Basic S3 API (PutObject, GetObject, DeleteObject, ListObjects)
- ✅ Buckets (create, list, delete)
- ✅ Multipart uploads
- ✅ Dual authentication (Web Console + S3 API)
- ✅ Basic Object Lock (COMPLIANCE/GOVERNANCE)
- ✅ Presigned URLs
- ✅ Monolithic build (single binary with embedded frontend)

### Web Console
- ✅ Dashboard with basic metrics
- ✅ Bucket management
- ✅ Object browser (upload/download)
- ✅ User and access key management
- ✅ Basic multi-tenancy (isolates resources per tenant)

### New in 1.1.0-alpha
- ✅ Migration to Pages Router for static export
- ✅ Real monolithic build (frontend embedded in Go binary)
- ✅ HTTP and HTTPS support with relative URLs
- ✅ Critical fix: `--data-dir` now works correctly

## 🚀 Quick Start

### Prerequisites
- Go 1.21+
- Node.js 18+ (only for development)

### Build

```bash
# Windows
.\build.bat

# Linux/macOS
make build
```

This generates `build/maxiofs.exe` (Windows) or `build/maxiofs` (Linux/macOS) with the embedded frontend.

### Run

```bash
# HTTP (development)
.\build\maxiofs.exe --data-dir ./data

# HTTPS (with certificates)
.\build\maxiofs.exe --data-dir ./data --tls-cert cert.pem --tls-key key.pem
```

**Access:**
- Web Console: `http://localhost:8081` (user: `admin`, password: `admin`)
- S3 API: `http://localhost:8080` (Access Key: `maxioadmin`, Secret: `maxioadmin`)

## 📋 Missing Features / Known Limitations

### Important Limitations
- ⚠️ **Passwords**: Bcrypt implemented but needs more testing
- ⚠️ **Multi-tenancy**: Implemented but without extensive testing
- ⚠️ **Object Lock**: Functional but not tested with Veeam or other clients
- ⚠️ **Performance**: Local benchmarks but not validated in production
- ⚠️ **Storage**: Filesystem only, no replication or redundancy
- ⚠️ **Scalability**: Single-node, no clustering

### Pending Features
- [ ] Exhaustive testing of all functionalities
- [ ] Complete API documentation
- [ ] Veeam integration (tested)
- [ ] CI/CD pipeline
- [ ] Official Docker images
- [ ] Helm charts for Kubernetes
- [ ] Additional backends (S3, GCS, Azure)
- [ ] Complete object versioning
- [ ] Node replication

## 🔧 Configuration

```bash
# View all options
.\build\maxiofs.exe --help

# Main options
--data-dir string         # Data directory (REQUIRED)
--listen string           # S3 API port (default ":8080")
--console-listen string   # Web Console port (default ":8081")
--log-level string        # Log level (debug, info, warn, error)
--tls-cert string         # TLS certificate
--tls-key string          # TLS private key
```

## 📖 Architecture

```
┌─────────────────────────────────────┐
│    Single Binary (maxiofs.exe)     │
├─────────────────────────────────────┤
│  Web Console (Embedded Frontend)   │  :8081
│  - Next.js Pages Router             │
│  - Static files in /out             │
├─────────────────────────────────────┤
│  Console API (REST)                 │  :8081/api/v1
│  - JWT authentication               │
│  - User/Bucket/Tenant management    │
├─────────────────────────────────────┤
│  S3 API (S3-compatible)             │  :8080
│  - S3 signature v2/v4               │
│  - Basic S3 operations              │
├─────────────────────────────────────┤
│  Storage Backend (Filesystem)       │
│  - Atomic writes                    │
│  - Object Lock support              │
└─────────────────────────────────────┘
```

## 🧪 Testing

**Test Coverage: ~60%** (estimated, needs validation)

```bash
# Unit tests
go test ./internal/... -v

# Integration tests (if they exist)
go test ./tests/integration/... -v

# Frontend dev
cd web/frontend
npm run dev
```

## ⚠️ Security

**DO NOT use in production without:**
1. Changing default credentials
2. Implementing HTTPS (reverse proxy recommended)
3. Configuring appropriate firewall
4. Backing up data
5. Extensive testing

**Implemented Security Features:**
- ✅ Bcrypt for passwords
- ✅ JWT authentication
- ✅ Basic rate limiting
- ✅ Account lockout after failed attempts

**Pending:**
- [ ] Complete audit logging
- [ ] Validated granular RBAC
- [ ] Complete security hardening

## 📊 Performance

**Preliminary benchmarks (not validated in production):**
- Writes: ~374 MB/s (local filesystem)
- Reads: ~1703 MB/s (local filesystem)

*Note: These numbers are from local tests and may vary significantly depending on hardware, network, and configuration.*

## 🛠️ Development

### Project Structure

```
MaxIOFS/
├── cmd/maxiofs/          # Main binary
├── internal/             # Core logic
│   ├── api/             # Console API
│   ├── auth/            # Authentication
│   ├── bucket/          # Bucket management
│   ├── config/          # Configuration
│   ├── object/          # Object management
│   ├── server/          # HTTP servers
│   └── storage/         # Storage backend
├── pkg/s3compat/        # S3 API implementation
├── web/
│   ├── embed.go         # Frontend embed
│   └── frontend/        # Next.js app
└── build/               # Build output
```

### Build Process

The `build.bat` does:
1. Build the frontend (`npm run build` → `web/frontend/out/`)
2. Embed the frontend in Go (`web/embed.go`)
3. Build the Go binary with embedded frontend

## 🐛 Known Bugs

- [ ] Needs more Object Lock testing
- [ ] Multi-tenancy without complete testing
- [ ] Possible race conditions in concurrent operations
- [ ] UI may have unhandled edge cases

**Report bugs:** GitHub Issues

## 📝 Contributing

Pull requests welcome for:
- Bug fixes
- Additional tests
- Documentation
- Performance improvements

**DO NOT accept without:**
- Passing tests
- Documented code
- Descriptive commits

## 🗺️ Roadmap (Aspirational)

### Short Term
- [ ] Exhaustive testing
- [ ] API documentation
- [ ] Veeam integration validation
- [ ] Basic CI/CD

### Medium Term
- [ ] Docker/Kubernetes support
- [ ] Complete monitoring/metrics
- [ ] S3 backend (store on AWS S3)
- [ ] Performance tuning

### Long Term
- [ ] Multi-node clustering
- [ ] Complete object versioning
- [ ] Node replication
- [ ] GCS/Azure backends

## 📄 License

MIT License - See LICENSE file

## 💬 Support

- GitHub Issues: For bugs and feature requests
- Documentation: See `/docs` (in development)

---

**Reminder**: This is an ALPHA project. Use at your own risk.
