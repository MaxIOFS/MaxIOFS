# Single Node

A production setup on one Linux server: a service account, systemd, a
configuration file and a firewall. For a quick trial, [Installation](../getting-started/installation.md)
is enough; the Debian and RPM packages already do everything on this page
except the reverse proxy.

## Requirements

| Component | Minimum | Recommended |
|-----------|---------|-------------|
| CPU | 2 cores | 4 cores or more |
| RAM | 2 GB | 8 GB |
| Disk | 10 GB | SSD for the data directory |
| Network | 100 Mbps | 1 Gbps |
| OS | Linux, Windows or macOS | Linux x64 |

Put the data directory on a filesystem with room to grow. MaxIOFS stops
accepting writes at 99.5% of the disk (`507 InsufficientStorage`); reads and
deletes keep working.

## 1. Service account and directories

```bash
sudo useradd -r -s /bin/false maxiofs
sudo mkdir -p /var/lib/maxiofs /etc/maxiofs
sudo chown -R maxiofs:maxiofs /var/lib/maxiofs
```

## 2. Binary

```bash
wget -O maxiofs https://github.com/MaxIOFS/MaxIOFS/releases/download/v1.10.0/maxiofs-linux-amd64-v1.10.0
sudo install -m 755 maxiofs /usr/local/bin/maxiofs
maxiofs --version
```

## 3. Configuration

```yaml title="/etc/maxiofs/config.yaml"
data_dir: /var/lib/maxiofs
listen: 127.0.0.1:8080            # behind a reverse proxy on the same host
console_listen: 127.0.0.1:8081
public_api_url: https://s3.example.com
public_console_url: https://console.example.com
trusted_proxies: ["127.0.0.1"]    # the proxy's address: see TLS and Reverse Proxy
log_level: info
```

`public_api_url` is the address clients use for S3: presigned URLs and share
links are built with it. `public_console_url` is the console's address, used
in emails and by identity providers to send users back after sign-in.
Without a reverse proxy, listen on `:8080` and `:8081` and use
`http://server:8080` and `http://server:8081`.

Encryption at rest is always on; there is nothing to configure. Every key is
listed in [Configuration](../reference/configuration.md).

## 4. systemd service

```ini title="/etc/systemd/system/maxiofs.service"
[Unit]
Description=MaxIOFS S3-Compatible Object Storage
After=network.target

[Service]
Type=simple
User=maxiofs
Group=maxiofs
ExecStart=/usr/local/bin/maxiofs --config /etc/maxiofs/config.yaml
Restart=on-failure
RestartSec=5s
# A large index takes longer to close than the 90 s systemd allows by default.
TimeoutStopSec=600
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now maxiofs
sudo systemctl status maxiofs
sudo journalctl -u maxiofs -f        # follow the log
```

!!! warning "Let it stop"
    On a stop MaxIOFS finishes its writes and closes the metadata index.
    Do not shorten `TimeoutStopSec`: a stop cut short is recovered on the next
    start, but that start takes longer.

## 5. Firewall

Clients need the S3 port and the console port, or only 443 when a reverse
proxy is in front:

```bash
# Without a proxy
sudo ufw allow 8080/tcp
sudo ufw allow 8081/tcp

# With a proxy on the same host
sudo ufw allow 443/tcp
```

## 6. Check it

```bash
curl http://127.0.0.1:8080/health
curl http://127.0.0.1:8081/api/v1/health
```

Then sign in to the console and follow [First Steps](../getting-started/first-steps.md).

## Production checklist

- [ ] Admin password changed, 2FA on for every administrator.
- [ ] Encryption recovery bundle downloaded and stored off the server
      (**Settings → Security**).
- [ ] HTTPS in front of both ports ([TLS and Reverse Proxy](reverse-proxy.md)).
- [ ] Firewall open only to the ports clients use.
- [ ] Service running as `maxiofs`, not root.
- [ ] Email configured for alerts (**Settings → Email**).
- [ ] Backups planned ([Backup and Recovery](../operations/backup-recovery.md)).
- [ ] Metrics scraped ([Prometheus and Grafana](../integrations/prometheus.md)).

More hardening advice is in [Security](../reference/security.md).
