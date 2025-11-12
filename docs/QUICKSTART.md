# MaxIOFS Quick Start Guide

**Version**: 0.3.2-beta
**Time to Complete**: 15-20 minutes

## Overview

This guide will help you:
1. Install MaxIOFS
2. Access the web console
3. Create your first tenant and user
4. Generate S3 access keys
5. Create a bucket and upload your first object

---

## Prerequisites

- **OS**: Windows, Linux (x64/ARM64), or macOS
- **Storage**: 10 GB minimum
- **RAM**: 2 GB minimum
- **Ports**: 8080 (S3 API) and 8081 (Web Console) available

---

## Step 1: Installation

### Option A: Binary Installation (Recommended)

**Download the binary for your platform:**

```bash
# Linux/macOS
wget https://github.com/yourusername/maxiofs/releases/latest/download/maxiofs-linux-amd64
chmod +x maxiofs-linux-amd64
mv maxiofs-linux-amd64 maxiofs

# Or build from source
git clone https://github.com/yourusername/maxiofs.git
cd maxiofs
make build
```

**Windows:**
```powershell
# Download from releases or build from source
git clone https://github.com/yourusername/maxiofs.git
cd maxiofs
.\build.bat
```

### Option B: Docker (Quick Start)

```bash
# Clone repository
git clone https://github.com/yourusername/maxiofs.git
cd maxiofs

# Build and start
make docker-build
make docker-up

# Or with monitoring (Prometheus + Grafana)
make docker-monitoring
```

---

## Step 2: Start MaxIOFS

### Standalone Binary

```bash
# Create data directory
mkdir -p ./data

# Start MaxIOFS
./maxiofs --data-dir ./data --log-level info
```

**Expected output:**
```
INFO[0000] MaxIOFS v0.3.2-beta starting...
INFO[0000] S3 API listening on :8080
INFO[0000] Web Console listening on :8081
INFO[0000] Prometheus metrics available at /metrics
```

### Docker

If using Docker, services are already running. Skip to Step 3.

---

## Step 3: Access Web Console

1. **Open your browser** and navigate to:
   ```
   http://localhost:8081
   ```

2. **Login with default credentials:**
   - **Username**: `admin`
   - **Password**: `admin`

   ⚠️ **IMPORTANT**: Change this password immediately after first login!

3. **Change admin password** (recommended):
   - Click on profile icon → Settings
   - Go to Security tab
   - Change password
   - **Optional**: Enable Two-Factor Authentication (2FA) for additional security

---

## Step 4: Create Your First Tenant

Tenants provide isolated namespaces for different teams, departments, or customers.

1. **Navigate to Tenants** in the sidebar

2. **Click "Create Tenant"**

3. **Fill in tenant details:**
   ```
   Name: acme
   Display Name: ACME Corporation
   Max Storage: 10 GB (10737418240 bytes)
   Max Buckets: 10
   Max Access Keys: 5
   ```

4. **Click "Create"**

You should now see your new tenant in the list.

---

## Step 5: Create a Tenant Admin User

1. **Navigate to Users** in the sidebar

2. **Click "Create User"**

3. **Fill in user details:**
   ```
   Username: acme-admin
   Password: SecurePassword123!
   Email: admin@acme.com
   Role: Admin
   Tenant: ACME Corporation
   ```

4. **Click "Create"**

Now you have a tenant-specific admin who can manage users and buckets for ACME Corporation.

---

## Step 6: Generate S3 Access Keys

S3 access keys are required to use the S3 API with tools like AWS CLI.

1. **Navigate to Users** in the sidebar

2. **Click on the user** you just created (`acme-admin`)

3. **Scroll to "Access Keys" section**

4. **Click "Create Access Key"**

5. **Copy and save the credentials** (they won't be shown again):
   ```
   Access Key ID: AKIA...
   Secret Access Key: wJalr...
   ```

   ⚠️ **Save these securely** - you won't be able to see the secret again!

---

## Step 7: Configure AWS CLI

Install AWS CLI if you haven't already:

```bash
# Ubuntu/Debian
sudo apt install awscli

# macOS
brew install awscli

# Windows
# Download from: https://aws.amazon.com/cli/
```

**Configure AWS CLI with your MaxIOFS credentials:**

```bash
aws configure set aws_access_key_id YOUR_ACCESS_KEY_ID
aws configure set aws_secret_access_key YOUR_SECRET_ACCESS_KEY
aws configure set region us-east-1
aws configure set output json
```

---

## Step 8: Create Your First Bucket

**Using Web Console:**
1. Navigate to Buckets
2. Click "Create Bucket"
3. Enter bucket name: `my-first-bucket`
4. Click "Create"

**Using AWS CLI:**
```bash
aws --endpoint-url=http://localhost:8080 s3 mb s3://my-first-bucket
```

**Expected output:**
```
make_bucket: my-first-bucket
```

---

## Step 9: Upload Your First Object

**Create a test file:**
```bash
echo "Hello from MaxIOFS!" > test.txt
```

**Upload using AWS CLI:**
```bash
aws --endpoint-url=http://localhost:8080 s3 cp test.txt s3://my-first-bucket/
```

**Expected output:**
```
upload: ./test.txt to s3://my-first-bucket/test.txt
```

**Verify upload:**
```bash
aws --endpoint-url=http://localhost:8080 s3 ls s3://my-first-bucket/
```

**Expected output:**
```
2025-11-12 10:30:00        21 test.txt
```

---

## Step 10: Download Your Object

```bash
aws --endpoint-url=http://localhost:8080 s3 cp s3://my-first-bucket/test.txt downloaded.txt
```

**Verify content:**
```bash
cat downloaded.txt
```

**Expected output:**
```
Hello from MaxIOFS!
```

---

## 🎉 Congratulations!

You've successfully:
- ✅ Installed and started MaxIOFS
- ✅ Created a tenant and admin user
- ✅ Generated S3 access keys
- ✅ Created a bucket
- ✅ Uploaded and downloaded an object

---

## What's Next?

### Explore Advanced Features

1. **Enable Versioning:**
   ```bash
   aws --endpoint-url=http://localhost:8080 s3api put-bucket-versioning \
     --bucket my-first-bucket \
     --versioning-configuration Status=Enabled
   ```

2. **Upload Large Files (Multipart):**
   ```bash
   # AWS CLI automatically uses multipart for files >5MB
   aws --endpoint-url=http://localhost:8080 s3 cp large-file.bin s3://my-first-bucket/
   ```

3. **Create Presigned URLs:**
   ```bash
   aws --endpoint-url=http://localhost:8080 s3 presign \
     s3://my-first-bucket/test.txt \
     --expires-in 3600
   ```

4. **Enable Object Lock (WORM):**
   ```bash
   # Create bucket with Object Lock
   aws --endpoint-url=http://localhost:8080 s3api create-bucket \
     --bucket compliance-bucket \
     --object-lock-enabled-for-bucket
   ```

5. **Set up Monitoring:**
   ```bash
   # Start with monitoring stack (Docker)
   make docker-monitoring

   # Access Grafana
   # http://localhost:3000 (admin/admin)
   # Pre-configured MaxIOFS dashboard included
   ```

### Learn More

- **[API Reference](API.md)** - Complete API documentation
- **[Configuration Guide](CONFIGURATION.md)** - Advanced configuration options
- **[Security Guide](SECURITY.md)** - Best practices for securing MaxIOFS
- **[Multi-Tenancy Guide](MULTI_TENANCY.md)** - Managing multiple tenants
- **[Deployment Guide](DEPLOYMENT.md)** - Production deployment options
- **[Architecture Overview](ARCHITECTURE.md)** - Technical architecture details

---

## Common Commands Cheat Sheet

```bash
# List all buckets
aws --endpoint-url=http://localhost:8080 s3 ls

# List objects in bucket
aws --endpoint-url=http://localhost:8080 s3 ls s3://my-bucket/

# Upload file
aws --endpoint-url=http://localhost:8080 s3 cp file.txt s3://my-bucket/

# Download file
aws --endpoint-url=http://localhost:8080 s3 cp s3://my-bucket/file.txt .

# Delete object
aws --endpoint-url=http://localhost:8080 s3 rm s3://my-bucket/file.txt

# Delete bucket (must be empty)
aws --endpoint-url=http://localhost:8080 s3 rb s3://my-bucket

# Sync directory
aws --endpoint-url=http://localhost:8080 s3 sync ./local-dir s3://my-bucket/remote-dir

# Get bucket versioning status
aws --endpoint-url=http://localhost:8080 s3api get-bucket-versioning --bucket my-bucket
```

---

## Troubleshooting

### Cannot access web console

**Check if MaxIOFS is running:**
```bash
# Check processes
ps aux | grep maxiofs

# Check ports
netstat -tlnp | grep -E '8080|8081'
```

### AWS CLI connection refused

**Verify endpoint URL:**
```bash
# Correct format
aws --endpoint-url=http://localhost:8080 s3 ls

# Wrong (missing http://)
aws --endpoint-url=localhost:8080 s3 ls
```

### Access Denied errors

**Verify credentials:**
```bash
# Check configured credentials
aws configure list

# Verify access key is valid in web console
# Navigate to Users → Select User → Access Keys
```

### Bucket already exists error

**Remember**: Each tenant has its own bucket namespace. The error means a bucket with that name already exists in YOUR tenant's namespace.

**Solution**: Choose a different bucket name or delete the existing bucket first.

---

## Getting Help

- **Documentation**: Check docs folder for detailed guides
- **GitHub Issues**: https://github.com/yourusername/maxiofs/issues
- **Logs**: Check MaxIOFS console output for error messages

---

**Version**: 0.3.2-beta
**Last Updated**: November 12, 2025
