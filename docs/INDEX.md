# MaxIOFS Documentation Index

Welcome to the MaxIOFS documentation. This index provides quick access to all available documentation.

## 📚 Documentation Structure

### Getting Started
- **[QUICKSTART.md](QUICKSTART.md)** - Quick start guide to get MaxIOFS running in minutes
- **[BUILD.md](BUILD.md)** - Build instructions for all platforms (Windows, Linux, macOS)
- **[MONOLITHIC_BUILD.md](MONOLITHIC_BUILD.md)** - Embedded frontend build guide
- **[README.md](../README.md)** - Main project overview and features

### Core Documentation
- **[ARCHITECTURE.md](ARCHITECTURE.md)** - System architecture and design principles
- **[API.md](API.md)** - Complete API reference (S3 API + Console API)
- **[CONFIGURATION.md](CONFIGURATION.md)** - Configuration options and examples
- **[MULTI_TENANCY.md](MULTI_TENANCY.md)** - Multi-tenancy guide and best practices

### Operations & Deployment
- **[DEPLOYMENT.md](DEPLOYMENT.md)** - Production deployment guide (Docker, Kubernetes, Systemd)
- **[SECURITY.md](SECURITY.md)** - Security features, hardening, and compliance

### Planning & Development
- **[IMPLEMENTATION_PLAN.md](IMPLEMENTATION_PLAN.md)** - Development roadmap and implementation plan
- **[TODO.md](../TODO.md)** - Project roadmap and feature tracking

---

## 📖 Quick Links by Topic

### For Users

**Getting Started:**
1. [Quick Start Guide](QUICKSTART.md) - Get running in 5 minutes
2. [Configuration Guide](CONFIGURATION.md) - Configure your installation
3. [API Reference](API.md) - Learn the API

**Operations:**
1. [Deployment Guide](DEPLOYMENT.md) - Deploy to production
2. [Security Guide](SECURITY.md) - Secure your installation
3. [Multi-Tenancy Guide](MULTI_TENANCY.md) - Set up multi-tenant environment

### For Developers

**Development:**
1. [Build Guide](BUILD.md) - Build from source
2. [Architecture](ARCHITECTURE.md) - Understand the system
3. [Implementation Plan](IMPLEMENTATION_PLAN.md) - Development roadmap

**Contributing:**
1. [TODO](../TODO.md) - See what's planned
2. [API Reference](API.md) - Understand the APIs

### For Administrators

**Setup:**
1. [Deployment](DEPLOYMENT.md) - Production deployment
2. [Configuration](CONFIGURATION.md) - All configuration options
3. [Security](SECURITY.md) - Harden your installation

**Operations:**
1. [Multi-Tenancy](MULTI_TENANCY.md) - Manage tenants
2. [Security](SECURITY.md) - Security best practices
3. [API Reference](API.md) - Manage via API

---

## 📋 Documentation by Feature

### S3 Compatibility
- [API Reference](API.md#s3-api-port-8080) - Complete S3 API documentation
- [Quick Start](QUICKSTART.md#s3-api-usage) - Using S3 CLI tools
- [Deployment](DEPLOYMENT.md#reverse-proxy-setup) - Expose S3 API securely

### Multi-Tenancy
- [Multi-Tenancy Guide](MULTI_TENANCY.md) - Complete multi-tenancy documentation
- [Security](SECURITY.md#multi-tenancy-isolation) - Tenant isolation
- [Configuration](CONFIGURATION.md#multi-tenancy-configuration) - Configure quotas

### Object Lock / WORM
- [API Reference](API.md#object-lock-operations) - Object Lock API
- [Quick Start](QUICKSTART.md#object-lock) - Enable Object Lock
- [Security](SECURITY.md#worm-compliance) - Compliance features

### Authentication & Security
- [Security Guide](SECURITY.md) - Complete security documentation
- [Configuration](CONFIGURATION.md#security-configuration) - Security settings
- [API Reference](API.md#authentication--authorization) - Auth mechanisms

### Monitoring & Metrics
- [Deployment](DEPLOYMENT.md#monitoring) - Set up Prometheus/Grafana
- [Configuration](CONFIGURATION.md#monitoring-configuration) - Metrics configuration
- [API Reference](API.md#metrics) - Metrics endpoints

---

## 🔍 Find What You Need

### Common Tasks

| Task | Documentation |
|------|--------------|
| Install MaxIOFS | [Quick Start](QUICKSTART.md) |
| Build from source | [Build Guide](BUILD.md) |
| Deploy to production | [Deployment Guide](DEPLOYMENT.md) |
| Configure settings | [Configuration Guide](CONFIGURATION.md) |
| Set up multi-tenancy | [Multi-Tenancy Guide](MULTI_TENANCY.md) |
| Secure installation | [Security Guide](SECURITY.md) |
| Use S3 API | [API Reference](API.md#s3-api-port-8080) |
| Manage via Console API | [API Reference](API.md#console-api-port-8081) |
| Enable Object Lock | [Quick Start](QUICKSTART.md#object-lock) |
| Set up Veeam backup | [Quick Start](QUICKSTART.md#veeam-integration) |
| Monitor metrics | [Deployment](DEPLOYMENT.md#monitoring) |
| Troubleshoot issues | [Deployment](DEPLOYMENT.md#troubleshooting) |

### Integration Guides

| Integration | Documentation |
|------------|--------------|
| AWS CLI | [API Reference](API.md#using-aws-cli) |
| boto3 (Python) | [API Reference](API.md#using-python-boto3) |
| s3cmd | [Quick Start](QUICKSTART.md#using-s3cmd) |
| Veeam Backup | [Quick Start](QUICKSTART.md#veeam-integration) |
| Prometheus | [Deployment](DEPLOYMENT.md#prometheus-integration) |
| Grafana | [Deployment](DEPLOYMENT.md#grafana-dashboard) |
| Docker | [Deployment](DEPLOYMENT.md#docker-deployment) |
| Kubernetes | [Deployment](DEPLOYMENT.md#kubernetes-deployment) |
| Nginx | [Deployment](DEPLOYMENT.md#nginx) |
| Traefik | [Deployment](DEPLOYMENT.md#traefik) |

---

## 📝 Documentation Versions

| Version | Status | Documentation |
|---------|--------|--------------|
| v1.1.x | Current | This documentation |
| v1.0.x | Legacy | [Archive](https://github.com/maxiofs/maxiofs/tree/v1.0/docs) |

---

## 🆘 Getting Help

If you can't find what you're looking for:

1. **Search the docs:** Use Ctrl+F in your browser
2. **Check examples:** Each guide has practical examples
3. **GitHub Issues:** https://github.com/yourusername/maxiofs/issues
4. **Community:** https://discord.gg/maxiofs

---

## 📄 Documentation Files

```
docs/
├── INDEX.md                    # This file - Documentation index
├── QUICKSTART.md               # Quick start guide (5 min setup)
├── BUILD.md                    # Build instructions
├── ARCHITECTURE.md             # System architecture
├── API.md                      # API reference (S3 + Console)
├── CONFIGURATION.md            # Configuration guide
├── DEPLOYMENT.md               # Deployment guide
├── SECURITY.md                 # Security guide
├── MULTI_TENANCY.md           # Multi-tenancy guide
└── IMPLEMENTATION_PLAN.md     # Development roadmap

Root files:
├── README.md                   # Project overview
└── TODO.md                     # Feature roadmap
```

---

## 🔄 Contributing to Documentation

To improve the documentation:

1. Fork the repository
2. Make your changes
3. Submit a pull request
4. Follow the documentation style guide

**Style Guidelines:**
- Use clear, concise language
- Include code examples
- Add command-line examples
- Use tables for reference data
- Include screenshots where helpful
- Test all commands before documenting

---

Last updated: 2025-10-05
