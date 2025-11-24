# MaxIOFS Documentation

**Version**: 0.4.2-beta
**S3 Compatibility**: 98%
**Last Updated**: November 23, 2025

---

## 📚 Documentation Index

Welcome to the MaxIOFS documentation! This guide will help you find the right documentation for your needs.

### 🚀 Getting Started

Start here if you're new to MaxIOFS:

- **[Quick Start Guide](QUICKSTART.md)** - Get up and running in 15-20 minutes
  - Installation
  - First tenant and user
  - Creating buckets
  - Uploading objects
  - AWS CLI configuration

---

## 📖 Core Documentation

### Configuration & Deployment

- **[Configuration Guide](CONFIGURATION.md)** - Complete configuration reference
  - Command-line flags
  - Environment variables
  - Configuration file format
  - TLS/HTTPS setup
  - Reverse proxy configuration

- **[Deployment Guide](DEPLOYMENT.md)** - Production deployment options
  - Standalone binary deployment
  - Docker deployment with docker-compose
  - Systemd service configuration
  - Reverse proxy with Nginx
  - Monitoring with Prometheus + Grafana
  - Security recommendations

### Architecture & Design

- **[Architecture Overview](ARCHITECTURE.md)** - System architecture and design
  - Component architecture
  - Storage layer design
  - Multi-tenancy implementation
  - Authentication mechanisms
  - Data flow diagrams
  - Performance characteristics

### API Reference

- **[API Reference](API.md)** - Complete API documentation
  - S3 API compatibility (98%)
  - Console REST API
  - Authentication methods
  - Endpoint reference
  - Error codes
  - Usage examples

### Features & Capabilities

- **[Multi-Tenancy Guide](MULTI_TENANCY.md)** - Multi-tenant architecture
  - Tenant hierarchy
  - Resource isolation
  - Quota management (storage, buckets, access keys)
  - Permission matrix
  - Best practices

- **[Security Guide](SECURITY.md)** - Security features and best practices
  - Server-Side Encryption at Rest (SSE) with AES-256
  - Authentication (JWT + S3 signatures)
  - Two-Factor Authentication (2FA) with TOTP
  - Comprehensive Audit Logging
  - Role-Based Access Control (RBAC)
  - Rate limiting and account lockout
  - Object Lock (WORM compliance)
  - Security checklist

---

## 🎯 Documentation by Use Case

### For Administrators

1. Start with [Quick Start Guide](QUICKSTART.md)
2. Review [Security Guide](SECURITY.md)
3. Configure production deployment: [Deployment Guide](DEPLOYMENT.md)
4. Set up monitoring: [Deployment Guide](DEPLOYMENT.md#docker-deployment) (monitoring profile)
5. Configure multi-tenancy: [Multi-Tenancy Guide](MULTI_TENANCY.md)

### For Developers

1. Understand the architecture: [Architecture Overview](ARCHITECTURE.md)
2. Learn the APIs: [API Reference](API.md)
3. Configure for development: [Configuration Guide](CONFIGURATION.md)
4. Integrate S3 clients: [API Reference](API.md#s3-api-port-8080)

### For DevOps Engineers

1. Deploy with Docker: [Deployment Guide](DEPLOYMENT.md#docker-deployment)
2. Set up monitoring: [Deployment Guide](DEPLOYMENT.md) + Prometheus/Grafana
3. Configure reverse proxy: [Configuration Guide](CONFIGURATION.md#tlshttps)
4. Implement security: [Security Guide](SECURITY.md)

---

## 🆕 What's New in v0.4.2-beta

### Major Features

- ✅ **Global Bucket Uniqueness** - AWS S3 compatible bucket naming
  - Bucket names are now globally unique across all tenants
  - Prevents bucket name conflicts between different tenants
  - Improves S3 client compatibility
  - Validation layer added without changing database schema

- ✅ **S3-Compatible URLs** - Standard S3 URL format without tenant prefix
  - Presigned URLs no longer include tenant-id in path
  - Share URLs follow standard S3 format
  - Better compatibility with standard S3 clients
  - Automatic tenant resolution from bucket name

- ✅ **Bucket Notifications (Webhooks)** - AWS S3 compatible event notifications
  - Event types: ObjectCreated:*, ObjectRemoved:*, ObjectRestored:Post
  - Wildcard event matching (e.g., s3:ObjectCreated:* matches Put, Post, Copy)
  - Webhook delivery with retry mechanism (3 attempts)
  - Per-rule filtering with prefix and suffix filters
  - Custom HTTP headers support per notification rule
  - Web Console UI with tab-based bucket settings
  - Full audit logging for all configuration changes

- ✅ **Frontend Improvements**
  - Fixed presigned URL modal state persistence bug
  - Improved React component lifecycle management
  - Better user experience when switching between objects

### Previous Features (v0.4.1-beta)

- ✅ **Server-Side Encryption at Rest (SSE)** - AES-256-CTR encryption for all stored objects
- ✅ **Dynamic Settings System** - Runtime configuration stored in SQLite
- ✅ **Metrics Historical Storage** - BadgerDB for metrics persistence
- ✅ **Critical Security Fixes** - Tenant menu, privilege escalation, password detection
- ✅ **UI/UX Improvements** - Unified card design, enhanced audit logs

### Previous Features (v0.4.0-beta)

- ✅ **Comprehensive Audit Logging System** - Track all critical system events
- ✅ **Professional Audit Logs UI** - Modern web interface with filters
- ✅ **RESTful Audit API** - Programmatic access to logs

### Previous Features (v0.3.2-beta)

- ✅ **Two-Factor Authentication (2FA)** - TOTP-based with backup codes
- ✅ **Prometheus Monitoring** - Comprehensive metrics export
- ✅ **Grafana Dashboard** - Pre-configured dashboard for visualization
- ✅ **Docker Support** - Complete docker-compose with monitoring stack
- ✅ **HTTP Conditional Requests** - If-Match, If-None-Match for caching

### S3 Compatibility

- **98% S3 Compatible**
- All core operations fully functional
- Validated with MinIO Warp stress testing (7000+ objects)
- Production-ready for S3 workloads

---

## 🔧 Quick Reference

### Default Credentials

**Web Console** (http://localhost:8081):
- Username: `admin`
- Password: `admin`
- ⚠️ **Change immediately after first login!**

**S3 API** (http://localhost:8080):
- No default access keys
- Create via web console after login

### Default Ports

- **8080** - S3 API endpoint
- **8081** - Web Console
- **9091** - Prometheus (with monitoring profile)
- **3000** - Grafana (with monitoring profile)

### Common Commands

```bash
# Start MaxIOFS
./maxiofs --data-dir ./data

# Start with Docker
make docker-up

# Start with monitoring
make docker-monitoring

# AWS CLI with MaxIOFS
aws --endpoint-url=http://localhost:8080 s3 ls

# Create bucket
aws --endpoint-url=http://localhost:8080 s3 mb s3://my-bucket

# Upload file
aws --endpoint-url=http://localhost:8080 s3 cp file.txt s3://my-bucket/
```

---

## 📊 Feature Matrix

### Authentication & Security

| Feature | Status | Documentation |
|---------|--------|---------------|
| Server-Side Encryption (AES-256) | ✅ Complete | [Security Guide](SECURITY.md#server-side-encryption-sse) |
| Comprehensive Audit Logging | ✅ Complete | [Security Guide](SECURITY.md#audit-logging) |
| Username/Password | ✅ Complete | [Security Guide](SECURITY.md) |
| Two-Factor Authentication (2FA) | ✅ Complete | [Security Guide](SECURITY.md#2-two-factor-authentication-2fa) |
| JWT Tokens | ✅ Complete | [Security Guide](SECURITY.md#1-console-authentication-jwt) |
| S3 Signature v2/v4 | ✅ Complete | [API Reference](API.md#authentication) |
| Role-Based Access Control | ✅ Complete | [Security Guide](SECURITY.md#authorization-rbac) |
| Account Lockout | ✅ Complete | [Security Guide](SECURITY.md#account-lockout) |
| Rate Limiting | ✅ Complete | [Security Guide](SECURITY.md#login-rate-limiting) |
| Session Timeout | ✅ Complete | [Security Guide](SECURITY.md) |

### S3 API Features

| Feature | Status | Documentation |
|---------|--------|---------------|
| Bucket Operations | ✅ Complete | [API Reference](API.md#bucket-operations) |
| Object Operations | ✅ Complete | [API Reference](API.md#object-operations) |
| Multipart Uploads | ✅ Complete | [API Reference](API.md#multipart-upload-6-operations) |
| Versioning | ✅ Complete | [API Reference](API.md) |
| Object Lock (WORM) | ✅ Complete | [API Reference](API.md#object-lock-operations) |
| Presigned URLs | ✅ Complete | [API Reference](API.md#advanced-features) |
| Range Requests | ✅ Complete | [API Reference](API.md#advanced-features) |
| CORS Configuration | ✅ Complete | [API Reference](API.md) |
| Bucket Policies | ✅ Complete | [API Reference](API.md) |
| Object Tagging | ✅ Complete | [API Reference](API.md) |
| Conditional Requests | ✅ Complete | [API Reference](API.md) |

### Multi-Tenancy

| Feature | Status | Documentation |
|---------|--------|---------------|
| Tenant Isolation | ✅ Complete | [Multi-Tenancy Guide](MULTI_TENANCY.md) |
| Storage Quotas | ✅ Complete | [Multi-Tenancy Guide](MULTI_TENANCY.md#1-storage-quota) |
| Bucket Quotas | ✅ Complete | [Multi-Tenancy Guide](MULTI_TENANCY.md#2-bucket-quota) |
| Access Key Quotas | ✅ Complete | [Multi-Tenancy Guide](MULTI_TENANCY.md#3-access-key-quota) |
| Tenant-Scoped Namespaces | ✅ Complete | [Multi-Tenancy Guide](MULTI_TENANCY.md#tenant-scoped-bucket-namespaces) |

### Monitoring & Observability

| Feature | Status | Documentation |
|---------|--------|---------------|
| Prometheus Metrics | ✅ Complete | [Deployment Guide](DEPLOYMENT.md) |
| Grafana Dashboard | ✅ Complete | [Deployment Guide](DEPLOYMENT.md) |
| Health Endpoints | ✅ Complete | [API Reference](API.md#health--monitoring) |
| Structured Logging | ✅ Complete | [Architecture](ARCHITECTURE.md#monitoring) |

---

## 🐛 Known Limitations

### Beta Status

MaxIOFS is in **beta development**. Core features are implemented and tested, but:

- ⚠️ No third-party security audits
- ⚠️ Limited testing at extreme scale (100+ concurrent users)
- ⚠️ Single-node only (no clustering)
- ⚠️ Filesystem backend only
- ⚠️ Limited encryption key management (master key in config, HSM planned for v0.5.0)

See [Architecture Overview](ARCHITECTURE.md#current-limitations) for complete list.

---

## 🤝 Contributing

Want to improve the documentation?

1. Fork the repository
2. Update documentation files in `docs/` folder
3. Ensure all examples are tested and working
4. Submit a pull request

**Documentation Guidelines:**
- Write in clear, concise English
- Include code examples where applicable
- Use proper Markdown formatting
- Update the version and date at the top of each file

---

## 📞 Getting Help

### Documentation Issues

If you find errors or gaps in the documentation:
1. Check existing GitHub issues
2. Open a new issue with label `documentation`
3. Provide specific details about what's missing or incorrect

### Technical Support

- **GitHub Issues**: https://github.com/yourusername/maxiofs/issues
- **Discussions**: https://github.com/yourusername/maxiofs/discussions

### Security Issues

**DO NOT** open public issues for security vulnerabilities.

Email: security@yourdomain.com (update with actual contact)

---

## 📝 Documentation Roadmap

### Planned Documentation

- [ ] Performance tuning guide
- [ ] Backup and restore procedures
- [ ] Migration guide (from MinIO, S3, etc.)
- [ ] Troubleshooting guide
- [ ] Integration examples (Veeam, Duplicati, etc.)
- [ ] Developer contribution guide

---

**Version**: 0.4.2-beta
**Last Updated**: November 23, 2025
