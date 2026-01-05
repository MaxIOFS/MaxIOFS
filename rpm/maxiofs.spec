# MaxIOFS RPM Specification
# This spec file is used to build RPM packages for RHEL/CentOS/Fedora/Rocky/Alma Linux
#
# Build with: rpmbuild -ba maxiofs.spec
# Or use: make rpm (recommended)

%define name maxiofs
%define version 0.7.0
%define release 1
%define debug_package %{nil}

Name:           %{name}
Version:        %{version}
Release:        %{release}%{?dist}
Summary:        High-Performance S3-Compatible Object Storage
License:        MIT
URL:            https://github.com/maxiofs/maxiofs
Source0:        %{name}-%{version}.tar.gz

BuildRequires:  golang >= 1.25
BuildRequires:  nodejs >= 24
BuildRequires:  npm
BuildRequires:  systemd-rpm-macros

Requires:       glibc
Requires:       logrotate
Requires(pre):  shadow-utils
Requires(post): systemd
Requires(preun): systemd
Requires(postun): systemd

Recommends:     nginx
Suggests:       certbot

%description
MaxIOFS is a high-performance, S3-compatible object storage system
built in Go with an embedded React web interface.

Features:
* Full S3 API compatibility (98% tests passing)
* Server-Side Encryption (SSE) with AES-256-CTR
* Comprehensive audit logging system
* Two-Factor Authentication (2FA) with TOTP
* Multi-tenancy with resource isolation and quotas
* Advanced ACL system with S3 canned ACLs
* Lifecycle management for automatic data archival
* Real-time metrics and monitoring
* Web-based management console
* Cross-region replication
* Versioning support
* Pre-signed URLs for temporary access

%prep
# This section is handled by the Makefile

%build
# This section is handled by the Makefile

%install
# Create directory structure
mkdir -p %{buildroot}/opt/maxiofs
mkdir -p %{buildroot}/etc/maxiofs
mkdir -p %{buildroot}/etc/logrotate.d
mkdir -p %{buildroot}%{_unitdir}
mkdir -p %{buildroot}/var/lib/maxiofs
mkdir -p %{buildroot}/var/log/maxiofs
mkdir -p %{buildroot}%{_docdir}/%{name}

# Install binary
install -m 0755 build/maxiofs %{buildroot}/opt/maxiofs/maxiofs

# Install config example ONLY (actual config created in %post)
install -m 0644 config.example.yaml %{buildroot}/etc/maxiofs/config.example.yaml

# Install systemd service
install -m 0644 rpm/maxiofs.service %{buildroot}%{_unitdir}/maxiofs.service

# Install logrotate config
install -m 0644 rpm/maxiofs.logrotate %{buildroot}/etc/logrotate.d/maxiofs

# Install documentation
install -m 0644 README.md %{buildroot}%{_docdir}/%{name}/
install -m 0644 CHANGELOG.md %{buildroot}%{_docdir}/%{name}/
install -m 0644 TODO.md %{buildroot}%{_docdir}/%{name}/
install -m 0644 LICENSE %{buildroot}%{_docdir}/%{name}/ || true
install -m 0644 docs/*.md %{buildroot}%{_docdir}/%{name}/

%pre
# Create maxiofs system user if it doesn't exist
if ! getent passwd maxiofs >/dev/null 2>&1; then
    echo "Creating maxiofs system user..."
    useradd --system --no-create-home --user-group \
            --home-dir /var/lib/maxiofs \
            --comment "MaxIOFS Server" \
            --shell /sbin/nologin maxiofs
fi

%post
# Set ownership of directories
chown -R maxiofs:maxiofs /var/lib/maxiofs
chown -R maxiofs:maxiofs /var/log/maxiofs

# Create config.yaml from example if it doesn't exist (NEVER overwrite existing config)
if [ ! -f /etc/maxiofs/config.yaml ]; then
    echo "Creating initial config.yaml from example..."
    cp /etc/maxiofs/config.example.yaml /etc/maxiofs/config.yaml
    # Adjust data_dir for system installation
    sed -i 's|data_dir: "./data"|data_dir: "/var/lib/maxiofs"|' /etc/maxiofs/config.yaml
else
    echo "Preserving existing config.yaml (contains encryption keys)"
fi

# Set ownership and permissions for config files
if [ -f /etc/maxiofs/config.yaml ]; then
    chown maxiofs:maxiofs /etc/maxiofs/config.yaml
    chmod 0640 /etc/maxiofs/config.yaml
fi

if [ -f /etc/maxiofs/config.example.yaml ]; then
    chown maxiofs:maxiofs /etc/maxiofs/config.example.yaml
    chmod 0644 /etc/maxiofs/config.example.yaml
fi

# Set permissions
chmod 0750 /var/lib/maxiofs
chmod 0750 /var/log/maxiofs

# Reload systemd
%systemd_post maxiofs.service

# Check if this is a new installation or an upgrade
if [ $1 -eq 1 ]; then
    # New installation
    echo ""
    echo "========================================"
    echo "MaxIOFS has been installed successfully!"
    echo "========================================"
    echo ""
    echo "Configuration file: /etc/maxiofs/config.yaml"
    echo "Example config: /etc/maxiofs/config.example.yaml"
    echo "Data directory: /var/lib/maxiofs"
    echo "Log directory: /var/log/maxiofs"
    echo ""
    echo "IMPORTANT: Before starting MaxIOFS, please:"
    echo "  1. Edit /etc/maxiofs/config.yaml with your settings"
    echo "  2. Ensure the data_dir is configured correctly"
    echo "  3. Review security settings (TLS, authentication)"
    echo ""
    echo "To start MaxIOFS:"
    echo "  sudo systemctl start maxiofs"
    echo ""
    echo "To enable at boot:"
    echo "  sudo systemctl enable maxiofs"
    echo ""
    echo "To check status:"
    echo "  sudo systemctl status maxiofs"
    echo ""
    echo "To view logs:"
    echo "  sudo journalctl -u maxiofs -f"
    echo ""
    echo "Web Console: http://localhost:8081"
    echo "S3 API: http://localhost:8080"
    echo ""
    echo "════════════════════════════════════════════════════"
    echo "⚠️  CRITICAL SECURITY WARNING ⚠️"
    echo "════════════════════════════════════════════════════"
    echo "The config.yaml file contains your ENCRYPTION KEY."
    echo "If this key is lost, ALL encrypted data is PERMANENTLY LOST."
    echo ""
    echo "IMMEDIATELY create a backup:"
    echo "  sudo cp /etc/maxiofs/config.yaml /etc/maxiofs/config.yaml.backup"
    echo ""
    echo "Store the backup in a SECURE location (off-server recommended)"
    echo "════════════════════════════════════════════════════"
    echo ""
else
    # Upgrade
    echo ""
    echo "========================================"
    echo "MaxIOFS has been upgraded successfully!"
    echo "========================================"
    echo ""
    echo "✅ Your configuration has been PRESERVED: /etc/maxiofs/config.yaml"
    echo "   (Encryption keys and settings remain unchanged)"
    echo ""
    echo "📄 Updated example config available at:"
    echo "   /etc/maxiofs/config.example.yaml"
    echo ""
    echo "📁 Data directory: /var/lib/maxiofs"
    echo ""
    echo "To restart MaxIOFS with the new version:"
    echo "  sudo systemctl restart maxiofs"
    echo ""
    echo "To check status:"
    echo "  sudo systemctl status maxiofs"
    echo ""
    echo "════════════════════════════════════════════════════"
    echo "⚠️  REMINDER: Backup your config.yaml regularly"
    echo "════════════════════════════════════════════════════"
    echo ""
fi

%preun
%systemd_preun maxiofs.service

%postun
%systemd_postun_with_restart maxiofs.service

if [ $1 -eq 0 ]; then
    # Complete removal (not upgrade)
    echo ""
    echo "========================================"
    echo "MaxIOFS has been removed."
    echo "========================================"
    echo ""
    echo "IMPORTANT: Your data has been preserved:"
    echo "  - Configuration: /etc/maxiofs/config.yaml (includes encryption key)"
    echo "  - Data directory: /var/lib/maxiofs"
    echo "  - Logs: /var/log/maxiofs"
    echo ""
    echo "You can reinstall MaxIOFS and your data will be accessible."
    echo ""
    echo "To completely remove all data including encryption keys:"
    echo "  sudo rm -rf /etc/maxiofs /var/lib/maxiofs /var/log/maxiofs"
    echo "  sudo userdel maxiofs"
    echo ""
    echo "WARNING: This will make your encrypted data permanently inaccessible!"
    echo ""
fi

%files
%defattr(-,root,root,-)
/opt/maxiofs/maxiofs
%{_unitdir}/maxiofs.service
/etc/logrotate.d/maxiofs
%config(noreplace) /etc/maxiofs/config.example.yaml
%attr(0750,maxiofs,maxiofs) %dir /var/lib/maxiofs
%attr(0750,maxiofs,maxiofs) %dir /var/log/maxiofs
%{_docdir}/%{name}/

%changelog
* Sun Jan 05 2026 Aluisco Ricardo <aluisco@maxiofs.com> - 0.7.0-1
- Version 0.7.0
- CRITICAL FIX: RPM packages now preserve /etc/maxiofs/config.yaml
- Improved packaging to prevent encryption key loss on upgrades
- Added comprehensive post-installation messages
- Enhanced security with proper file permissions

* Fri Nov 01 2025 Aluisco Ricardo <aluisco@maxiofs.com> - 0.6.2-1
- Version 0.6.2-beta
- Full S3 API compatibility
- Server-side encryption
- Multi-tenancy support
- Web console
