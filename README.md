# MaxIOFS - Modern S3-Compatible Object Storage

MaxIOFS is a modern, high-performance S3-compatible object storage system built in Go with an embedded Next.js web interface. Designed from the ground up with a pluggable architecture, it provides enterprise-grade object storage with advanced monitoring, modern UI/UX, and complete AWS S3 API compatibility.

## 🚀 Features

- **🔄 S3 API Compatibility**: Complete compatibility with AWS S3 API
- **🔒 Object Locking**: Full support for WORM (Write Once Read Many) compliance
- **� Veeam Compatible**: Certified for Veeam Backup & Replication immutable repositories
- **�📦 Single Binary**: Self-contained executable with embedded web interface
- **⚡ High Performance**: Built in Go for maximum speed and efficiency
- **🎨 Modern Web UI**: Next.js 14-based admin interface with Tailwind CSS
- **🔌 Pluggable Backends**: Support for filesystem, S3, GCS, Azure Blob Storage
- **🛡️ Enterprise Security**: At-rest and in-transit encryption with advanced auth
- **📊 Advanced Monitoring**: Prometheus metrics with custom dashboards
- **🔧 Developer Friendly**: CLI with Cobra, configuration with Viper
- **🐳 Container Ready**: Optimized Docker images and Kubernetes support

## 📋 Architecture

```
┌─────────────────┐
│   Web UI        │ ← Next.js Frontend (Embedded)
│   (Next.js)     │
├─────────────────┤
│   API Gateway   │ ← S3 Compatible REST API
│   (Go)          │
├─────────────────┤
│   Core Engine   │ ← Object Management, Bucket Management
│   (Go)          │
├─────────────────┤
│   Storage Layer │ ← Pluggable storage backends
│   (Go)          │
└─────────────────┘
```

## 🏗️ Project Structure

```
MaxIOFS/
├── cmd/
│   └── maxiofs/           # Main application entry point
├── internal/
│   ├── api/               # S3 API implementation
│   ├── auth/              # Authentication & authorization
│   ├── bucket/            # Bucket management
│   ├── object/            # Object operations & locking
│   ├── storage/           # Storage backend abstractions
│   ├── config/            # Configuration management
│   ├── middleware/        # HTTP middleware
│   └── metrics/           # Metrics collection
├── pkg/
│   ├── s3compat/          # S3 compatibility layer
│   ├── encryption/        # Encryption utilities
│   └── compression/       # Compression utilities
├── web/
│   ├── frontend/          # Next.js admin interface
│   └── assets/            # Static assets
├── scripts/               # Build and deployment scripts
├── docker/                # Docker configuration
├── tests/                 # Test suites
└── docs/                  # Documentation
```

## 🛠️ Development

### Prerequisites

- Go 1.21+
- Node.js 18+
- npm/yarn

### Building

```bash
# Build the complete system
make build

# Development mode
make dev

# Run tests
make test
```

## 🎯 Use Cases

### Backup & Recovery with Veeam

MaxIOFS is fully compatible with **Veeam Backup & Replication** as an immutable backup repository:

- ✅ S3-compatible API with Object Lock support
- ✅ COMPLIANCE and GOVERNANCE retention modes
- ✅ Automatic retention application on backup uploads
- ✅ Protection against ransomware and accidental deletion
- ✅ On-premise deployment (no cloud dependency)

**Quick Start**: See [Veeam Configuration Guide](./docs/VEEAM_QUICKSTART.md)

### Enterprise Object Storage

- Document management systems
- Media asset management
- Log aggregation and archival
- Data lake storage
- Backup and disaster recovery

## 📦 Deployment

MaxIOFS can be deployed as:

1. **Single Binary**: Self-contained executable
2. **Docker Container**: Official Docker images
3. **Kubernetes**: Helm charts available

## 🔧 Configuration

Configuration via:
- Environment variables
- YAML configuration files
- Command-line flags

## 📖 Documentation

- [Architecture Overview](./docs/ARCHITECTURE.md)
- [Quick Start Guide](./docs/QUICKSTART.md)
- [Veeam Compatibility Guide](./docs/VEEAM_COMPATIBILITY.md)
- [Veeam Quick Start](./docs/VEEAM_QUICKSTART.md)
- [API Reference](./docs/API.md)

## 📊 Monitoring

Built-in metrics compatible with:
- Prometheus
- Grafana
- Custom monitoring solutions

## 📄 License

MIT License - see LICENSE file for details.