# MaxIOFS - Modern S3-Compatible Object Storage

MaxIOFS is a high-performance S3-compatible object storage system built in Go with an embedded Next.js web interface. It provides enterprise-grade object storage with WORM compliance, advanced monitoring, and complete AWS S3 API compatibility.

## 🚀 Features

- **S3 API Compatibility**: Complete AWS S3 API implementation with 23+ advanced operations
- **Object Lock & WORM**: Full support for Write Once Read Many compliance (COMPLIANCE/GOVERNANCE modes)
- **Veeam Compatible**: Works as immutable backup repository for Veeam Backup & Replication
- **Single Binary**: Self-contained executable with embedded web UI
- **Modern Web Interface**: Next.js 14 dashboard with real-time metrics and management
- **Dual Authentication**: Console web login + S3 API access keys
- **High Performance**: 374 MB/s writes, 1703 MB/s reads (benchmarked)
- **Pluggable Storage**: Filesystem backend (S3, GCS, Azure planned)
- **Enterprise Security**: AES-256-GCM encryption, JWT authentication
- **Production Ready**: Prometheus metrics, structured logging, rate limiting

## 🏗️ Architecture

```
┌─────────────────┐
│   Web Console   │ ← Next.js 14 UI (port 8081)
├─────────────────┤
│   Console API   │ ← REST API for frontend
├─────────────────┤
│   S3 API        │ ← S3-compatible API (port 8080)
├─────────────────┤
│   Core Engine   │ ← Bucket/Object/Auth Managers
├─────────────────┤
│   Storage       │ ← Filesystem Backend
└─────────────────┘
```

## 🚀 Quick Start

### Prerequisites

- Go 1.21+
- Node.js 18+
- npm/yarn

### Running MaxIOFS

```bash
# Build and run
go build -o maxiofs.exe ./cmd/maxiofs
./maxiofs.exe

# Or with make
make build
./maxiofs.exe

# Development mode (auto-reload)
make dev
```

**Default Credentials:**
- **Web Console**: `http://localhost:8081` → Login: `admin` / `admin`
- **S3 API**: `http://localhost:8080` → Access Key: `maxioadmin` / Secret: `maxioadmin`

### Frontend Development

```bash
cd web/frontend
npm install
npm run dev  # Development server on port 3000
npm run build  # Production build
```

## 📋 Key Capabilities

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
- **Bucket Management**: Create buckets with Object Lock wizard
- **Object Browser**: Upload/download with retention display
- **User Management**: Create users, manage access keys
- **Settings**: System configuration and monitoring

## 🎯 Use Cases

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

- Document management and archival
- Media asset storage
- Log aggregation
- Data lake storage
- Application file storage

## 📊 Performance

Benchmark results (filesystem backend):

- **Writes**: 374 MB/s (100MB files)
- **Reads**: 1703 MB/s (10MB files)
- **Memory**: ~15KB/op writes, ~11KB/op reads
- **Concurrency**: 50+ simultaneous operations

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
```

## 📖 Documentation

- [Architecture Guide](./docs/ARCHITECTURE.md) - System design and components
- [Quick Start Guide](./docs/QUICKSTART.md) - Setup and configuration
- [TODO](./TODO.md) - Development roadmap and progress

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
# Coming soon: Helm charts
```

## ⚠️ Security Notes

**For Production Deployments:**

- Replace SHA-256 password hashing with bcrypt/argon2
- Implement rate limiting on authentication endpoints
- Configure CORS restrictively (no wildcard `*`)
- Enable HTTPS/TLS with valid certificates
- Use strong JWT secrets (min 32 random bytes)
- Implement password policies (min 8 chars, complexity)
- Enable audit logging for compliance

## 🛠️ Development Status

**Current Phase**: Production Ready (Phases 1-5 Complete)

- ✅ Backend S3 API (23+ operations)
- ✅ Frontend Next.js dashboard
- ✅ Dual authentication system
- ✅ Object Lock & WORM
- ✅ Unit/Integration/Performance tests
- ⏳ Production hardening (Phase 6)
- ⏳ Additional storage backends (Phase 7)

See [TODO.md](./TODO.md) for detailed roadmap.

## 📄 License

MIT License - see LICENSE file for details.

## 🤝 Contributing

Contributions welcome! Please ensure:
1. All tests pass
2. Code follows Go best practices
3. Documentation is updated
4. Commits are well-described

## 📞 Support

For issues and questions:
- GitHub Issues: [Report bugs or request features]
- Documentation: See `/docs` folder
- Quick Start: See [QUICKSTART.md](./docs/QUICKSTART.md)