# MaxIOFS Quick Start Guide

This guide will help you get MaxIOFS up and running quickly.

## Prerequisites

- Go 1.21 or later
- Node.js 18 or later
- npm or yarn
- Make (optional, for build automation)

## Quick Installation

### Option 1: From Source

1. **Clone the repository**:
```bash
git clone https://github.com/maxiofs/maxiofs.git
cd maxiofs
```

2. **Build the application**:
```bash
make build
```

3. **Run MaxIOFS**:
```bash
./build/maxiofs
```

### Option 2: Using Docker

1. **Run with Docker**:
```bash
docker run -d \
  --name maxiofs \
  -p 9000:9000 \
  -p 9001:9001 \
  -v maxiofs-data:/data \
  maxiofs/maxiofs:latest
```

### Option 3: Using Docker Compose

1. **Create docker-compose.yml**:
```yaml
version: '3.8'
services:
  maxiofs:
    image: maxiofs/maxiofs:latest
    ports:
      - "9000:9000"
      - "9001:9001"
    volumes:
      - maxiofs-data:/data
    environment:
      - MAXIOFS_ACCESS_KEY=admin
      - MAXIOFS_SECRET_KEY=password123
    restart: unless-stopped

volumes:
  maxiofs-data:
```

2. **Start the service**:
```bash
docker-compose up -d
```

## First Steps

### 1. Access the Web Console

Open your browser and go to: http://localhost:8081

Default credentials:
- **Access Key**: `maxioadmin`
- **Secret Key**: `maxioadmin`

### 2. Configure AWS CLI

Configure the AWS CLI to use MaxIOFS:

```bash
aws configure set aws_access_key_id maxioadmin
aws configure set aws_secret_access_key maxioadmin
aws configure set default.region us-east-1
aws configure set default.output json
```

### 3. Test S3 API

Create a bucket:
```bash
aws --endpoint-url http://localhost:8080 s3 mb s3://test-bucket
```

Upload a file:
```bash
echo "Hello MaxIOFS!" > test.txt
aws --endpoint-url http://localhost:8080 s3 cp test.txt s3://test-bucket/
```

List objects:
```bash
aws --endpoint-url http://localhost:8080 s3 ls s3://test-bucket/
```

Download a file:
```bash
aws --endpoint-url http://localhost:8080 s3 cp s3://test-bucket/test.txt downloaded.txt
```

## Configuration

### Command Line Options

```bash
./maxiofs --help
```

Common options:
- `--listen` : API server listen address (default: ":8080")
- `--console-listen` : Console server listen address (default: ":8081")
- `--data-dir` : Data directory path (default: "./data")
- `--log-level` : Log level (debug, info, warn, error)

### Environment Variables

All configuration can be set via environment variables with `MAXIOFS_` prefix:

```bash
export MAXIOFS_LISTEN=":8080"
export MAXIOFS_CONSOLE_LISTEN=":8081"
export MAXIOFS_DATA_DIR="/var/lib/maxiofs"
export MAXIOFS_LOG_LEVEL="info"
export MAXIOFS_ACCESS_KEY="your-access-key"
export MAXIOFS_SECRET_KEY="your-secret-key"
```

### Configuration File

Create a `config.yaml` file:

```yaml
server:
  listen: ":8080"
  console_listen: ":8081"
  data_dir: "/var/lib/maxiofs"
  log_level: "info"

auth:
  access_key: "your-access-key"
  secret_key: "your-secret-key"

storage:
  backend: "filesystem"
  enable_compression: true
  enable_encryption: false
  enable_object_lock: true
```

Run with config file:
```bash
./maxiofs --config config.yaml
```

## Web Console Features

The web console provides:

1. **Dashboard**: Overview of storage usage and system health
2. **Bucket Management**: Create, delete, and configure buckets
3. **Object Browser**: Upload, download, and manage objects
4. **User Management**: Manage access keys and permissions
5. **System Metrics**: Monitor performance and usage
6. **Configuration**: System settings and policies

## S3 API Compatibility

MaxIOFS supports the following S3 operations:

### Bucket Operations
- CreateBucket
- DeleteBucket
- ListBuckets
- HeadBucket
- GetBucketLocation
- GetBucketVersioning / PutBucketVersioning
- GetBucketPolicy / PutBucketPolicy / DeleteBucketPolicy

### Object Operations
- GetObject
- PutObject
- DeleteObject
- HeadObject
- ListObjects / ListObjectsV2
- CopyObject

### Advanced Features
- Multipart uploads
- Object versioning
- Object locking (WORM compliance)
- Bucket policies
- Object tagging
- Lifecycle management

## Security

### Access Control

1. **Access Keys**: Configure access/secret key pairs
2. **Bucket Policies**: JSON-based access policies
3. **Object ACLs**: Object-level access control
4. **TLS/SSL**: Enable HTTPS for secure communication

### Encryption

1. **At-rest encryption**: Encrypt stored objects
2. **In-transit encryption**: TLS for API communications
3. **Client-side encryption**: Support for client-encrypted objects

### Object Lock

Enable WORM (Write Once Read Many) compliance:

```bash
# Enable object lock on bucket creation
aws --endpoint-url http://localhost:9000 s3api create-bucket \
  --bucket locked-bucket \
  --object-lock-enabled-for-bucket
```

## Monitoring

### Health Checks

- **Health endpoint**: `GET /health`
- **Ready endpoint**: `GET /ready`

### Metrics

MaxIOFS exposes Prometheus-compatible metrics at `/metrics`:

```bash
curl http://localhost:9000/metrics
```

### Logging

Structured JSON logging with configurable levels:
- `debug`: Detailed debug information
- `info`: General information
- `warn`: Warning messages
- `error`: Error messages

## Troubleshooting

### Common Issues

1. **Port conflicts**: Ensure ports 9000 and 9001 are available
2. **Permission errors**: Check file system permissions for data directory
3. **Memory issues**: Monitor memory usage for large uploads

### Debug Mode

Enable debug logging:
```bash
./maxiofs --log-level debug
```

### Logs Location

By default, logs are written to stdout. To save to file:
```bash
./maxiofs 2>&1 | tee maxiofs.log
```

## Performance Tuning

### For High-Throughput Workloads

1. **Increase file descriptors**:
```bash
ulimit -n 65536
```

2. **Use SSD storage** for better I/O performance

3. **Tune compression settings** based on data type

4. **Monitor system resources** and scale accordingly

### For Large Files

1. **Use multipart uploads** for files > 100MB
2. **Adjust read/write buffer sizes**
3. **Consider distributed deployment** for very large workloads

## Next Steps

1. **Read the Architecture Guide**: [docs/ARCHITECTURE.md](ARCHITECTURE.md)
2. **Explore Configuration Options**: [docs/CONFIGURATION.md](CONFIGURATION.md)
3. **Set up Monitoring**: [docs/MONITORING.md](MONITORING.md)
4. **Production Deployment**: [docs/DEPLOYMENT.md](DEPLOYMENT.md)

## Getting Help

- **Documentation**: Check the `docs/` directory
- **Issues**: Report bugs on GitHub Issues
- **Community**: Join our Discord/Slack community
- **Support**: Commercial support available

## License

MaxIOFS is licensed under the MIT License. See [LICENSE](../LICENSE) for details.