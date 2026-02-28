# MaxIOFS - S3-Compatible Object Storage

**Version**: 0.9.2-beta
**Status**: Beta - 100% S3 Compatible
**License**: MIT
**Website**: [maxiofs.com](https://maxiofs.com)

MaxIOFS is a high-performance, S3-compatible object storage system built in Go with an embedded React web interface. Designed to be simple, portable, and deployable as a single binary.

## 🎉 Project Status

**BETA PHASE** - Production-ready features with ongoing testing:
- ✅ **100% S3 API compatibility** - Validated with AWS CLI and MinIO Warp
- ✅ **Zero known bugs** - All reported issues resolved
- ✅ **Comprehensive testing** - 1,750+ automated tests (1,700+ backend + 64 frontend)
- ✅ **Production performance** - Validated with 100,000+ requests
- ✅ **Complete documentation** - See `/docs` directory
- ⚠️ Suitable for testing, development, and staging environments
- ⚠️ Production use requires your own extensive testing

## 🎯 Key Features

### S3 API Compatibility (100%)
- Core operations (PUT, GET, DELETE, LIST objects & buckets)
- Multipart uploads with complete workflow
- Presigned URLs with expiration
- Bucket versioning with delete markers
- Bucket policies, CORS, tagging, and lifecycle rules
- Object Lock (COMPLIANCE/GOVERNANCE modes)
- Object search & filters (content-type, size range, date range, tags)
- Bucket policy enforcement (AWS S3-compatible evaluation engine)
- Presigned URL signature validation (V4 and V2)
- Bucket notifications (webhooks)
- S3-compatible replication to AWS S3, MinIO, or other MaxIOFS instances

### Identity & Access Management
- **Identity Provider System** — LDAP/AD and OAuth2/OIDC (Google, Microsoft presets)
- SSO login with auto-provisioning via group-to-role mappings
- Two-Factor Authentication (2FA) with TOTP
- Server-side encryption at rest (AES-256-CTR)
- Comprehensive audit logging (20+ event types)
- Dynamic security configuration (rate limits, lockout policies)
- Multi-tenancy with resource isolation
- JWT authentication for Console, S3 Signature v2/v4 for API

### Cluster & High Availability
- Multi-node cluster support with intelligent routing
- Automatic failover and health monitoring
- Node-to-node replication with HMAC authentication
- **Complete Bucket Migration** ✅
  - Move buckets between nodes for capacity rebalancing and maintenance
  - Migrates ALL data: objects, permissions, ACLs, and configurations
  - Real-time progress tracking with ETag verification
  - Automatic migration of bucket settings (versioning, lifecycle, tags, CORS, encryption, policies)
  - Web console UI with migration history and filters
  - REST API for automation
- **Automatic Data Synchronization** ✅
  - All 6 entity types synced: users, tenants, access keys, bucket permissions, IDP providers, group mappings
  - Tombstone-based deletion sync prevents entity resurrection in bidirectional sync
  - Checksum-based change detection to minimize network traffic
  - Configurable sync intervals (default: 30 seconds)
- **JWT Secret Persistence & Cluster Sync** ✅
  - JWT secrets persisted in database — sessions survive server restarts
  - Automatic JWT secret synchronization on cluster join via HMAC-authenticated endpoint
  - Users authenticated on one node are valid across all cluster nodes
- Bucket location caching for performance
- Web console for cluster management

### Web Console
- Modern responsive UI with dark mode
- Real-time dashboard with metrics
- Bucket browser with drag-and-drop uploads
- User and tenant management
- Identity provider management (LDAP/OAuth CRUD, test connection, group mappings)
- Access key management
- Settings configuration (no restart required)
- Audit logs viewer with CSV export
- Cluster management interface

### Monitoring & Performance
- Prometheus metrics endpoint (`/metrics`)
- Pre-built Grafana dashboard (14 panels)
- Health check endpoint (`/health`)
- Maintenance mode (read-only during updates)
- Disk space and tenant quota alerts (SSE + email)
- Production-tested performance:
  - Upload: p95 < 10ms (50 concurrent users)
  - Download: p95 < 13ms (100 concurrent users)
  - >99.99% success rate under load

## 🚀 Quick Start

### Docker (Recommended)

```bash
# Basic deployment
make docker-build
make docker-up

# With monitoring (Prometheus + Grafana)
make docker-monitoring

# 3-node cluster
make docker-cluster
```

**Access:**
- Web Console: http://localhost:8081 (admin/admin)
- S3 API: http://localhost:8080
- Prometheus: http://localhost:9091 (monitoring profile)
- Grafana: http://localhost:3000 (admin/admin, monitoring profile)

**📖 See [DOCKER.md](DOCKER.md) for complete Docker documentation**

### Build from Source

**Build prerequisites only** (Go 1.25+, Node.js 24+) — required for compiling. The resulting binary is self-contained and runs on any system without Go or Node.

```bash
# Build
make build

# Run
./build/maxiofs --data-dir ./data

# Access
# Web Console: http://localhost:8081 (admin/admin)
# S3 API: http://localhost:8080
```

**⚠️ Change default credentials immediately!**

## 📖 Documentation

Comprehensive documentation available in `/docs`:

- **[DEPLOYMENT.md](docs/DEPLOYMENT.md)** - Production deployment guide
- **[CONFIGURATION.md](docs/CONFIGURATION.md)** - Configuration reference
- **[API.md](docs/API.md)** - S3 API compatibility reference
- **[ARCHITECTURE.md](docs/ARCHITECTURE.md)** - System architecture
- **[SECURITY.md](docs/SECURITY.md)** - Security features and best practices
- **[CLUSTER.md](docs/CLUSTER.md)** - Multi-node cluster setup
- **[TESTING.md](docs/TESTING.md)** - Testing guide
- **[PERFORMANCE.md](docs/PERFORMANCE.md)** - Performance benchmarks
- **[OPERATIONS.md](docs/OPERATIONS.md)** - Operations runbook and production guides
- **[SSO.md](docs/SSO.md)** - SSO/LDAP/OAuth setup guide
- **[DOCKER.md](DOCKER.md)** - Docker deployment guide

## 🧪 Testing

**Automated Tests:**
- 1,700+ backend tests across 103 test files (Auth, S3 API, Cluster, Metadata, Storage, ACL, Encryption, Replication, and more)
- 64 frontend tests (React components, API integration)
- 100% pass rate, CI/CD ready

**Test with AWS CLI:**
```bash
# Configure
aws configure --profile maxiofs

# Create bucket
aws --profile maxiofs --endpoint-url http://localhost:8080 s3 mb s3://test-bucket

# Upload file
aws --profile maxiofs --endpoint-url http://localhost:8080 s3 cp file.txt s3://test-bucket/
```

**📖 See [TESTING.md](docs/TESTING.md) for complete testing guide**

## ⚠️ Known Limitations

- Multi-tenancy needs more production testing
- Cluster tested with up to 5 nodes
- No third-party security audit performed
- Default credentials must be changed
- HTTPS recommended for production

## 🛠️ Development

```bash
# Run tests
go test ./...                    # Backend tests
cd web/frontend && npm run test  # Frontend tests

# Run in development
go run cmd/maxiofs/main.go --data-dir ./data --log-level debug

# Build for all platforms
make build-all
```

**📖 See [ARCHITECTURE.md](docs/ARCHITECTURE.md) for development guide**

## 📝 Release History

**See [CHANGELOG.md](CHANGELOG.md) for complete version history and roadmap**

Recent releases:
- **v0.9.2-beta** - Object integrity verification, maintenance mode, disk/quota alerts, stale node reconciler
- **v0.9.1-beta** - IDP tenant isolation fixes, user/access-key/bucket-permission handler auth hardening, cross-tenant data leak fixes
- **v0.9.0-beta** - Identity providers (LDAP/OAuth SSO), tombstone-based cluster deletion sync, JWT secret persistence & cluster sync, security fixes
- **v0.8.0-beta** - Object search & filters, security fixes, cluster production hardening
- **v0.7.0-beta** - Bucket inventory, performance profiling, database migrations
- **v0.6.0-beta** - Multi-node cluster support with HA replication

## 🔒 Security

**Default Credentials:** admin/admin (⚠️ **CHANGE IMMEDIATELY**)

**Best Practices:**
1. Change default credentials
2. Use HTTPS in production
3. Configure firewall rules
4. Regular backups
5. Monitor audit logs
6. Update regularly

**📖 See [SECURITY.md](docs/SECURITY.md) for security documentation**

## 📄 License

MIT License - See [LICENSE](LICENSE) file for details

## 💬 Support

- **Website**: [maxiofs.com](https://maxiofs.com)
- **Issues**: [GitHub Issues](https://github.com/maxiofs/maxiofs/issues)
- **Discussions**: [GitHub Discussions](https://github.com/maxiofs/maxiofs/discussions)
- **Documentation**: See `/docs` directory

---

**⚠️ BETA Software**: Suitable for development, testing, and staging. Production use requires extensive testing. Always backup your data.
