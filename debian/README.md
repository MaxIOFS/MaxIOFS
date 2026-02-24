# MaxIOFS Debian Package

This directory contains all files needed to build a Debian package for MaxIOFS.

## Package Structure

The Debian package installs MaxIOFS with the following layout:

```
/opt/maxiofs/             - Application binary
├── maxiofs               - Main executable

/etc/maxiofs/             - Configuration
├── config.yaml           - Main configuration file

/etc/logrotate.d/         - Log rotation
└── maxiofs               - Logrotate configuration

/var/lib/maxiofs/         - Data directory
├── db/                   - SQLite database
├── metadata/             - Pebble: object metadata (LSM-tree, crash-safe WAL)
└── objects/              - Object storage

/var/log/maxiofs/         - Log files (if file logging enabled)

/usr/share/doc/maxiofs/   - Documentation
├── README.md             - Main project README
├── CHANGELOG.md          - Version history and release notes
├── TODO.md               - Roadmap and planned features
├── LICENSE               - Software license
├── API.md                - Complete API reference
├── ARCHITECTURE.md       - System architecture overview
├── CONFIGURATION.md      - Configuration reference
├── DEPLOYMENT.md         - Deployment guide
├── MULTI_TENANCY.md      - Multi-tenancy guide
├── QUICKSTART.md         - Quick start guide
├── SECURITY.md           - Security guide and best practices
└── TESTING.md            - Testing guide

/lib/systemd/system/      - Systemd service
└── maxiofs.service       - Service unit file
```

## Building the Package

### Prerequisites

On a Linux system (Debian/Ubuntu):
```bash
sudo apt-get install dpkg-dev build-essential golang nodejs npm git
```

### Build Commands

```bash
# Build the Debian package
make deb

# Build with specific version
make deb VERSION=v1.0.0

# Build and install locally (for testing)
make deb-install

# Uninstall
make deb-uninstall

# Clean build artifacts
make deb-clean
```

## Installation

### Install the package
```bash
sudo dpkg -i build/maxiofs_v0.9.2-beta_amd64.deb
```

### Configure MaxIOFS
Edit the configuration file:
```bash
sudo nano /etc/maxiofs/config.yaml
```

Key settings to configure:
- `listen`: API server address (default: :8080)
- `console_listen`: Web console address (default: :8081)
- `public_api_url`: Public S3 endpoint URL
- `public_console_url`: Public web console URL
- `enable_tls`: Enable HTTPS (recommended for production)
- `cert_file` / `key_file`: TLS certificate paths
- `storage.enable_compression`: Enable object compression
- `storage.enable_encryption`: Enable encryption at rest
- `auth.enable_auth`: Enable authentication (recommended)

### Start the service
```bash
# Start now
sudo systemctl start maxiofs

# Enable on boot
sudo systemctl enable maxiofs

# Check status
sudo systemctl status maxiofs

# View logs
sudo journalctl -u maxiofs -f
```

## Post-Installation

### Access MaxIOFS

1. **Web Console**: http://localhost:8081
   - Default credentials: `admin` / `admin`
   - ⚠️ **Change the password immediately after first login!**

2. **S3 API**: http://localhost:8080
   - Create access keys via the web console
   - Go to Users → Select user → Create Access Key

### Create S3 Access Keys

MaxIOFS does not create default S3 access keys for security reasons. You must create them manually:

1. Login to web console (http://localhost:8081)
2. Navigate to **Users** page
3. Select the admin user (or create a new user)
4. Click **Create Access Key**
5. Copy the Access Key ID and Secret Key
6. Configure your S3 client with these credentials

### Configure AWS CLI

```bash
aws configure --profile maxiofs
# AWS Access Key ID: [your-generated-access-key]
# AWS Secret Access Key: [your-generated-secret-key]
# Default region name: us-east-1
# Default output format: json

# Test connection
aws s3 --profile maxiofs --endpoint-url http://localhost:8080 ls

# Create bucket
aws s3 --profile maxiofs --endpoint-url http://localhost:8080 mb s3://mybucket

# Upload file
aws s3 --profile maxiofs --endpoint-url http://localhost:8080 cp file.txt s3://mybucket/
```

## Uninstallation

### Understanding Debian Package Removal

MaxIOFS follows Debian packaging best practices for configuration file handling:

#### Remove (apt-get remove)
```bash
sudo apt-get remove maxiofs
```

**What is preserved:**
- ✅ Configuration: `/etc/maxiofs/config.yaml` (includes encryption key)
- ✅ Data directory: `/var/lib/maxiofs/` (all your data)
- ✅ Logs: `/var/log/maxiofs/`
- ✅ System user: `maxiofs`

**What is removed:**
- ❌ Binary: `/opt/maxiofs/maxiofs`
- ❌ Systemd service
- ❌ Documentation

**Why preserve config.yaml?**
The `config.yaml` file contains your encryption key. Without it, all encrypted data becomes permanently inaccessible. Debian marks this as a "conffile" to protect it from accidental deletion.

You can safely reinstall MaxIOFS and your data will be immediately accessible.

#### Purge (apt-get purge)
```bash
sudo apt-get purge maxiofs
```

**⚠️ WARNING: This PERMANENTLY DELETES EVERYTHING:**
- ❌ Configuration: `/etc/maxiofs/` (including encryption key)
- ❌ Data directory: `/var/lib/maxiofs/` (all your data)
- ❌ Logs: `/var/log/maxiofs/`
- ❌ System user: `maxiofs`
- ❌ Binary and documentation

**After purge, your encrypted data is PERMANENTLY INACCESSIBLE!**

### Configuration File Protection (conffiles)

The `/etc/maxiofs/config.yaml` file is marked as a Debian "conffile" which means:

1. **Protected from accidental deletion**: Survives `apt-get remove`
2. **Preserved during upgrades**: Your settings are never overwritten
3. **Manual confirmation required**: If the package includes changes to config.yaml, Debian will:
   - Keep your existing configuration by default
   - Prompt you to choose between keeping or replacing it
   - Show you the differences between versions
4. **Only deleted on purge**: Explicit `apt-get purge` is required to remove it

**Example upgrade scenario:**
```bash
# Upgrade MaxIOFS
sudo apt-get install maxiofs

# Debian detects config.yaml has changed
Configuration file '/etc/maxiofs/config.yaml'
 ==> Modified (by you or by a script) since installation.
 ==> Package distributor has shipped an updated version.
   What would you like to do about it ?  Your options are:
    Y or I  : install the package maintainer's version
    N or O  : keep your currently-installed version
      D     : show the differences between the versions
      Z     : start a shell to examine the situation
 The default action is to keep your current version.
*** config.yaml (Y/I/N/O/D/Z) [default=N] ? N

# Your encryption key and configuration are preserved!
```

## File Permissions

The package automatically creates a `maxiofs` system user and sets appropriate permissions:

- `/var/lib/maxiofs`: `0750` owned by `maxiofs:maxiofs` (data directory)
- `/var/log/maxiofs`: `0750` owned by `maxiofs:maxiofs` (log directory)
- `/etc/maxiofs/config.yaml`: `0640` owned by `root:maxiofs` (config file)
- `/opt/maxiofs/maxiofs`: `0755` owned by `root:root` (executable)

## Security Features

The systemd service includes security hardening:

- `PrivateTmp=true`: Isolated /tmp directory
- `NoNewPrivileges=true`: Prevents privilege escalation
- `ProtectSystem=strict`: Read-only /usr, /boot, /efi
- `ProtectHome=true`: Inaccessible /home directories
- `ReadWritePaths=/var/lib/maxiofs /var/log/maxiofs`: Only necessary paths writable
- `CapabilityBoundingSet=`: Minimal capabilities
- `SystemCallFilter=@system-service`: Restricted system calls

## Production Deployment

### With Nginx Reverse Proxy (Recommended)

Configure MaxIOFS to listen on localhost:
```yaml
# /etc/maxiofs/config.yaml
listen: "localhost:8080"
console_listen: "localhost:8081"
public_api_url: "https://s3.mydomain.com"
public_console_url: "https://console.mydomain.com"
enable_tls: false  # nginx handles TLS
```

Nginx configuration:
```nginx
# S3 API
server {
    listen 443 ssl http2;
    server_name s3.mydomain.com;
    
    ssl_certificate /etc/letsencrypt/live/s3.mydomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/s3.mydomain.com/privkey.pem;
    
    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # For large file uploads
        client_max_body_size 10G;
        proxy_request_buffering off;
    }
}

# Web Console
server {
    listen 443 ssl http2;
    server_name console.mydomain.com;
    
    ssl_certificate /etc/letsencrypt/live/console.mydomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/console.mydomain.com/privkey.pem;
    
    location / {
        proxy_pass http://localhost:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### Direct TLS (Without Reverse Proxy)

Configure MaxIOFS with TLS:
```yaml
# /etc/maxiofs/config.yaml
listen: ":8080"
console_listen: ":8081"
public_api_url: "https://s3.mydomain.com"
public_console_url: "https://console.mydomain.com"

enable_tls: true
cert_file: "/etc/letsencrypt/live/s3.mydomain.com/fullchain.pem"
key_file: "/etc/letsencrypt/live/s3.mydomain.com/privkey.pem"

storage:
  enable_compression: true
  compression_type: "zstd"
  enable_encryption: true
  encryption_key: "your-32-character-secret-key-here"
```

## Log Management

### Logrotate Configuration

The package automatically installs logrotate configuration at `/etc/logrotate.d/maxiofs`:

**Configuration:**
- **Rotation**: Daily
- **Retention**: 7 days (1 week)
- **Compression**: Enabled (gzip)
- **Delay compression**: First rotation not compressed
- **Copy truncate**: Safe for running services
- **Post-rotate**: Reloads MaxIOFS service

**Log files affected:**
- `/var/log/maxiofs/*.log`

**Manual rotation:**
```bash
# Force rotation now
sudo logrotate -f /etc/logrotate.d/maxiofs

# Test configuration
sudo logrotate -d /etc/logrotate.d/maxiofs

# View rotation status
sudo cat /var/lib/logrotate/status | grep maxiofs
```

**Customize retention period:**
Edit `/etc/logrotate.d/maxiofs` and change `rotate 7` to your desired number of days:
```bash
sudo nano /etc/logrotate.d/maxiofs
# Change: rotate 7  →  rotate 30  (for 30 days)
sudo systemctl restart logrotate
```

**Log file sizes:**
By default, rotation is daily. To add size limits:
```bash
# Edit config
sudo nano /etc/logrotate.d/maxiofs

# Add size limit (rotates daily OR when reaching size)
/var/log/maxiofs/*.log {
    daily
    rotate 7
    size 100M
    ...
}
```

## Monitoring

### Check Service Status
```bash
sudo systemctl status maxiofs
```

### View Logs
```bash
# Real-time logs
sudo journalctl -u maxiofs -f

# Last 100 lines
sudo journalctl -u maxiofs -n 100

# Logs since boot
sudo journalctl -u maxiofs -b
```

### Metrics Endpoint
MaxIOFS exposes Prometheus-compatible metrics:
```bash
curl http://localhost:8080/metrics
```

Metrics include:
- Request counts and latencies
- Storage usage
- Active connections
- CPU and memory usage
- System resources

### Health Check
```bash
# API health
curl http://localhost:8080/health

# Console health
curl http://localhost:8081/health
```

## Troubleshooting

### Service won't start
```bash
# Check logs
sudo journalctl -u maxiofs -n 50

# Check configuration
sudo maxiofs --config /etc/maxiofs/config.yaml --help

# Verify permissions
ls -la /var/lib/maxiofs
ls -la /etc/maxiofs
```

### Permission denied errors
```bash
# Fix ownership
sudo chown -R maxiofs:maxiofs /var/lib/maxiofs /var/log/maxiofs
sudo chmod 0750 /var/lib/maxiofs /var/log/maxiofs
```

### Port already in use
```bash
# Check what's using the port
sudo netstat -tlnp | grep :8080
sudo netstat -tlnp | grep :8081

# Change ports in config
sudo nano /etc/maxiofs/config.yaml
sudo systemctl restart maxiofs
```

### Cannot connect to S3 endpoint
1. Check service is running: `sudo systemctl status maxiofs`
2. Verify ports are open: `sudo netstat -tlnp | grep maxiofs`
3. Check firewall: `sudo ufw status`
4. Test locally: `curl http://localhost:8080/health`
5. Review public URLs in config: `public_api_url` and `public_console_url`

## Backup and Recovery

### ⚠️ CRITICAL: Backup Your Encryption Key

**The `/etc/maxiofs/config.yaml` file contains your encryption key. Without it, all encrypted data is permanently lost.**

**Best practices:**
1. ✅ Backup `config.yaml` immediately after installation
2. ✅ Store backups in a secure location (encrypted, off-site)
3. ✅ Never commit `config.yaml` to version control
4. ✅ Test restore procedures regularly
5. ✅ Backup before any system maintenance

### Backup

#### Full Backup (Recommended)
```bash
# Stop service
sudo systemctl stop maxiofs

# Create backup directory with timestamp
BACKUP_DIR="maxiofs-backup-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$BACKUP_DIR"

# Backup configuration (CRITICAL - contains encryption key)
sudo cp /etc/maxiofs/config.yaml "$BACKUP_DIR/"

# Backup data directory
sudo tar -czf "$BACKUP_DIR/data.tar.gz" -C /var/lib maxiofs

# Optional: Backup logs
sudo tar -czf "$BACKUP_DIR/logs.tar.gz" -C /var/log maxiofs

# Create archive
tar -czf "${BACKUP_DIR}.tar.gz" "$BACKUP_DIR"

# Secure the backup
chmod 600 "${BACKUP_DIR}.tar.gz"

# Restart service
sudo systemctl start maxiofs

echo "Backup created: ${BACKUP_DIR}.tar.gz"
echo "⚠️ IMPORTANT: Store this backup in a secure location!"
```

#### Quick Config Backup (Before Upgrades)
```bash
# Backup just the configuration file
sudo cp /etc/maxiofs/config.yaml \
  ~/maxiofs-config-backup-$(date +%Y%m%d).yaml

# Secure it
chmod 600 ~/maxiofs-config-backup-*.yaml
```

### Restore
```bash
# Stop service
sudo systemctl stop maxiofs

# Restore data
sudo rm -rf /var/lib/maxiofs
sudo tar -xzf maxiofs-backup-YYYYMMDD.tar.gz -C /var/lib

# Restore ownership
sudo chown -R maxiofs:maxiofs /var/lib/maxiofs

# Restore configuration
sudo cp config-backup.yaml /etc/maxiofs/config.yaml
sudo chown root:maxiofs /etc/maxiofs/config.yaml
sudo chmod 0640 /etc/maxiofs/config.yaml

# Restart service
sudo systemctl start maxiofs
```

## Upgrading

### Upgrade Process

MaxIOFS upgrades are safe and preserve all your data and configuration:

```bash
# Recommended: Backup config.yaml before upgrade
sudo cp /etc/maxiofs/config.yaml ~/config-backup.yaml

# Download new package
wget https://example.com/maxiofs_v1.0.0_amd64.deb

# Install upgrade
sudo dpkg -i maxiofs_v1.0.0_amd64.deb

# The service is automatically restarted with your existing configuration
```

### What Gets Preserved During Upgrades

✅ **Automatically preserved:**
- Configuration: `/etc/maxiofs/config.yaml` (includes encryption key)
- All settings, credentials, and encryption keys
- Data directory: `/var/lib/maxiofs/`
- All buckets, objects, and metadata
- Logs: `/var/log/maxiofs/`
- System user and permissions

✅ **Updated:**
- Binary: `/opt/maxiofs/maxiofs` (new version)
- Systemd service file (if changed)
- Documentation: `/usr/share/doc/maxiofs/`

### Handling Configuration Changes

If the new package includes changes to `config.yaml`, Debian will prompt you:

```
Configuration file '/etc/maxiofs/config.yaml'
 ==> Modified (by you or by a script) since installation.
 ==> Package distributor has shipped an updated version.
   What would you like to do about it ?  Your options are:
    Y or I  : install the package maintainer's version
    N or O  : keep your currently-installed version
      D     : show the differences between the versions
      Z     : start a shell to examine the situation
 The default action is to keep your current version.
```

**Recommended choice: N (keep your version)**
- Your encryption key is preserved
- Your custom settings remain intact
- You can manually review and merge new options later

### Rollback to Previous Version

If you need to downgrade:

```bash
# Stop service
sudo systemctl stop maxiofs

# Install previous version
sudo dpkg -i maxiofs_v0.4.0-beta_amd64.deb

# Your config.yaml is still preserved
# Restart service
sudo systemctl start maxiofs
```

## Package Files

- `control`: Package metadata (name, version, dependencies)
- `changelog`: Version history
- `compat`: Debhelper compatibility level
- `copyright`: License information (MIT)
- `rules`: Build script
- `conffiles`: List of configuration files protected by Debian (config.yaml)
- `maxiofs.service`: Systemd unit file
- `maxiofs.logrotate`: Logrotate configuration
- `postinst`: Post-installation script (creates user, sets permissions, detects upgrades)
- `prerm`: Pre-removal script (stops service)
- `postrm`: Post-removal script (cleanup on remove, purge all data on purge)

## Documentation

After installation, complete documentation is available locally at:

```bash
# View available documentation
ls -lh /usr/share/doc/maxiofs/

# Quick start guide
less /usr/share/doc/maxiofs/QUICKSTART.md

# Configuration reference
less /usr/share/doc/maxiofs/CONFIGURATION.md

# Security guide (including encryption setup)
less /usr/share/doc/maxiofs/SECURITY.md

# Complete API reference
less /usr/share/doc/maxiofs/API.md

# View changelog
less /usr/share/doc/maxiofs/CHANGELOG.md
```

**Available documentation files:**
- `README.md` - Main project overview
- `QUICKSTART.md` - Get started in 15-20 minutes
- `CONFIGURATION.md` - Complete configuration reference
- `SECURITY.md` - Security features and best practices
- `DEPLOYMENT.md` - Production deployment guide
- `API.md` - Complete API documentation
- `ARCHITECTURE.md` - System architecture overview
- `MULTI_TENANCY.md` - Multi-tenancy guide
- `TESTING.md` - Testing guide
- `CHANGELOG.md` - Version history and release notes
- `TODO.md` - Roadmap and planned features

**No internet connection required** - All documentation is included in the package!

## Support

- GitHub: https://github.com/maxiofs/maxiofs
- Documentation: `/usr/share/doc/maxiofs/` (included in package)
- Issues: https://github.com/maxiofs/maxiofs/issues

## License

MaxIOFS is licensed under the MIT License. See `/usr/share/doc/maxiofs/copyright` for details.
