# MaxIOFS - High-Performance S3-Compatible Object Storage

MaxIOFS is a high-performance, S3-compatible object storage system built in Go with an embedded Next.js web interface. It provides a single binary deployment similar to MinIO, with full object locking capabilities and complete AWS S3 API compatibility.

## 🚀 Features

- **S3 API Compatibility**: Complete compatibility with AWS S3 API
- **Object Locking**: Full support for WORM (Write Once Read Many) compliance
- **Single Binary**: Self-contained executable with embedded web interface
- **High Performance**: Built in Go for maximum speed and efficiency
- **Web Management**: Modern Next.js-based admin interface
- **Multi-Backend Storage**: Support for local filesystem, cloud storage, and distributed storage
- **Encryption**: At-rest and in-transit encryption
- **Compression**: Automatic data compression
- **Metrics & Monitoring**: Built-in metrics and monitoring capabilities

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

## 📊 Monitoring

Built-in metrics compatible with:
- Prometheus
- Grafana
- Custom monitoring solutions

## 📄 License

MIT License - see LICENSE file for details.