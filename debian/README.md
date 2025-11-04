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
├── db/                   - BadgerDB metadata
└── objects/              - Object storage

/var/log/maxiofs/         - Log files (if file logging enabled)

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
sudo dpkg -i build/maxiofs_v0.3.7-beta_amd64.deb
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

### Remove package (keep data)
```bash
sudo apt-get remove maxiofs
```
Data remains in `/var/lib/maxiofs` for recovery.

### Remove package and data (purge)
```bash
sudo apt-get purge maxiofs
```
⚠️ **This permanently deletes all data!**

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

### Backup
```bash
# Stop service
sudo systemctl stop maxiofs

# Backup data directory
sudo tar -czf maxiofs-backup-$(date +%Y%m%d).tar.gz \
  -C /var/lib maxiofs

# Backup configuration
sudo cp /etc/maxiofs/config.yaml config-backup.yaml

# Restart service
sudo systemctl start maxiofs
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

### Manual upgrade
```bash
# Download new package
wget https://example.com/maxiofs_v1.0.0_amd64.deb

# Install (keeps configuration and data)
sudo dpkg -i maxiofs_v1.0.0_amd64.deb

# Restart service
sudo systemctl restart maxiofs
```

The package preserves:
- Configuration files in `/etc/maxiofs/`
- Data in `/var/lib/maxiofs/`
- Existing systemd service settings

## Package Files

- `control`: Package metadata (name, version, dependencies)
- `changelog`: Version history
- `compat`: Debhelper compatibility level
- `copyright`: License information (MIT)
- `rules`: Build script
- `maxiofs.service`: Systemd unit file
- `postinst`: Post-installation script (creates user, sets permissions)
- `prerm`: Pre-removal script (stops service)
- `postrm`: Post-removal script (cleanup, optional purge)

## Support

- GitHub: https://github.com/yourusername/maxiofs
- Documentation: See `/docs` directory
- Issues: https://github.com/yourusername/maxiofs/issues

## License

MaxIOFS is licensed under the MIT License. See `/usr/share/doc/maxiofs/copyright` for details.
