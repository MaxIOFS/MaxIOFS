# MaxIOFS API Reference

**Version**: 0.8.0-beta
**S3 Compatibility**: 98%
**Last Updated**: January 16, 2026

## Overview

MaxIOFS provides two APIs:

1. **S3 API** (Port 8080) - 98% AWS S3-compatible REST API
2. **Console API** (Port 8081) - Management REST API for web console

### Recent Updates (v0.8.0-beta)

- ✅ **Multi-Node Cluster Support** - Complete cluster infrastructure with HA
- ✅ **Cluster Management API** - 18 REST endpoints for cluster management
- ✅ **Smart Router** - Health-aware request routing with automatic failover
- ✅ **Cluster Replication** - HMAC-authenticated node-to-node replication
- ✅ **Health Monitoring** - Background health checker with latency tracking

### Previous Updates (v0.4.2-beta)

- ✅ **Global Bucket Uniqueness** - Bucket names now globally unique across all tenants (AWS S3 compatible)
- ✅ **S3-Compatible URLs** - Presigned and share URLs without tenant prefix for standard S3 client compatibility
- ✅ **Bucket Notifications (Webhooks)** - AWS S3 compatible event notifications (ObjectCreated, ObjectRemoved, ObjectRestored)
- ✅ **Automatic Tenant Resolution** - Backend automatically resolves bucket ownership from bucket name

---

## S3 API (Port 8080)

### Authentication

MaxIOFS supports AWS Signature v2 and v4 authentication.

**Creating Access Keys:**
1. Login to web console at `http://localhost:8081` (admin/admin)
2. Navigate to Users section
3. Click "Create Access Key"
4. Copy the generated credentials

**Using AWS CLI:**
```bash
# Replace with your generated credentials
aws configure set aws_access_key_id YOUR_ACCESS_KEY
aws configure set aws_secret_access_key YOUR_SECRET_KEY
aws --endpoint-url=http://localhost:8080 s3 ls
```

**Using Python boto3:**
```python
import boto3

s3 = boto3.client(
    's3',
    endpoint_url='http://localhost:8080',
    aws_access_key_id='YOUR_ACCESS_KEY',
    aws_secret_access_key='YOUR_SECRET_KEY'
)

# List buckets
buckets = s3.list_buckets()
```

### Supported Operations

#### Bucket Operations
- `ListBuckets` - List all buckets
- `CreateBucket` - Create new bucket
- `DeleteBucket` - Delete empty bucket
- `HeadBucket` - Check if bucket exists
- `GetBucketVersioning` / `PutBucketVersioning`
- `GetBucketCORS` / `PutBucketCORS` / `DeleteBucketCORS`

#### Object Operations
- `PutObject` - Upload object
- `GetObject` - Download object
- `DeleteObject` - Delete object
- `HeadObject` - Get object metadata
- `ListObjects` / `ListObjectsV2` - List objects in bucket
- `CopyObject` - Copy object within/between buckets

#### Multipart Upload (6 operations)
- `CreateMultipartUpload` - Start multipart upload
- `UploadPart` - Upload a part
- `CompleteMultipartUpload` - Finish upload
- `AbortMultipartUpload` - Cancel upload
- `ListParts` - List uploaded parts
- `ListMultipartUploads` - List active uploads

#### Object Lock Operations
- `PutObjectRetention` / `GetObjectRetention` - WORM retention
- `PutObjectLegalHold` / `GetObjectLegalHold` - Legal hold

#### Batch Operations
- `DeleteMultipleObjects` - Delete up to 1000 objects

#### Advanced Features
- Presigned URLs (GET/PUT with expiration, S3-compatible paths)
- Range requests (partial downloads)

#### Bucket Notifications (v0.4.2+)
- `PutBucketNotificationConfiguration` - Configure webhook notifications
- `GetBucketNotificationConfiguration` - Get notification settings
- Supported event types: ObjectCreated:*, ObjectRemoved:*, ObjectRestored:Post

### Examples

**Create bucket:**
```bash
aws --endpoint-url=http://localhost:8080 s3 mb s3://my-bucket
```

**Upload file:**
```bash
aws --endpoint-url=http://localhost:8080 s3 cp file.txt s3://my-bucket/
```

**List objects:**
```bash
aws --endpoint-url=http://localhost:8080 s3 ls s3://my-bucket/
```

**Download file:**
```bash
aws --endpoint-url=http://localhost:8080 s3 cp s3://my-bucket/file.txt .
```

**Multipart upload (large files):**
```bash
aws --endpoint-url=http://localhost:8080 s3 cp large-file.bin s3://my-bucket/
```

### S3 API Compatibility

**Supported:**
- ✅ Standard bucket and object operations
- ✅ Global bucket uniqueness (AWS S3 compatible)
- ✅ Multipart uploads
- ✅ Object Lock (COMPLIANCE/GOVERNANCE)
- ✅ Presigned URLs (S3-compatible paths)
- ✅ AWS Signature v2 and v4
- ✅ Versioning configuration
- ✅ CORS configuration
- ✅ Bucket lifecycle policies (100% complete)
- ✅ Bucket notifications (webhooks)
- ✅ Server-Side Encryption at rest (AES-256-CTR)

**Not Supported:**
- ❌ Server-Side Encryption with KMS (uses local master key)
- ❌ Object ACLs (basic ACL operations supported)

For detailed S3 API specs, see [AWS S3 API Documentation](https://docs.aws.amazon.com/AmazonS3/latest/API/).

---

## Console API (Port 8081)

REST API for web console management. All endpoints require JWT authentication and are prefixed with `/api/v1`.

### Authentication

**Login:**
```http
POST /api/v1/auth/login
Content-Type: application/json

{
  "username": "admin",
  "password": "admin"
}
```

**Response:**
```json
{
  "success": true,
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "user": {
    "id": "user-123",
    "username": "admin",
    "roles": ["admin"]
  }
}
```

**Authenticated Requests:**
```http
GET /api/v1/users
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

### Available Endpoints

#### API Root
- `GET /api/v1/` - Get API information and available endpoints

#### Authentication
- `POST /api/v1/auth/login` - Login with username/password (with optional TOTP code)
- `GET /api/v1/auth/me` - Get current user info
- `POST /api/v1/auth/logout` - Logout
- `POST /api/v1/auth/2fa/enable` - Enable 2FA for current user
- `POST /api/v1/auth/2fa/verify` - Verify 2FA setup
- `POST /api/v1/auth/2fa/disable` - Disable 2FA for current user
- `GET /api/v1/auth/2fa/backup-codes` - Get backup codes

#### User Management
- `GET /api/v1/users` - List users
- `POST /api/v1/users` - Create user
- `PUT /api/v1/users/{id}` - Update user
- `DELETE /api/v1/users/{id}` - Delete user
- `POST /api/v1/users/{id}/unlock` - Unlock locked account

#### Tenant Management
- `GET /api/v1/tenants` - List tenants
- `POST /api/v1/tenants` - Create tenant
- `PUT /api/v1/tenants/{id}` - Update tenant
- `DELETE /api/v1/tenants/{id}` - Delete tenant
- `GET /api/v1/tenants/{id}/stats` - Get tenant statistics

#### Access Key Management
- `GET /api/v1/access-keys` - List access keys
- `POST /api/v1/access-keys` - Create access key
- `DELETE /api/v1/access-keys/{id}` - Revoke access key

#### Bucket Management
- `GET /api/v1/buckets` - List buckets
- `POST /api/v1/buckets` - Create bucket
- `DELETE /api/v1/buckets/{name}` - Delete bucket
- `GET /api/v1/buckets/{name}/stats` - Get bucket statistics

#### Object Management
- `GET /api/v1/buckets/{bucket}/objects` - List objects
- `POST /api/v1/buckets/{bucket}/objects` - Upload object (multipart/form-data)
- `DELETE /api/v1/buckets/{bucket}/objects/{key}` - Delete object
- `POST /api/v1/buckets/{bucket}/objects/{key}/share` - Generate share URL
- `DELETE /api/v1/buckets/{bucket}/objects/{key}/share` - Revoke share

#### Metrics
- `GET /api/v1/metrics` - Dashboard metrics
- `GET /api/v1/metrics/system` - System metrics (CPU, memory, disk)
- `GET /metrics` - Prometheus metrics endpoint (comprehensive monitoring)

#### Audit Logs (v0.4.0+)
- `GET /api/v1/audit-logs` - List audit logs with filtering
- `GET /api/v1/audit-logs/{id}` - Get specific audit log entry

**Query Parameters for GET /api/v1/audit-logs:**
- `tenant_id` - Filter by tenant (global admin only)
- `user_id` - Filter by user
- `event_type` - Filter by event type (login_success, user_created, etc.)
- `resource_type` - Filter by resource (system, user, bucket, etc.)
- `action` - Filter by action (login, create, delete, etc.)
- `status` - Filter by status (success, failed)
- `start_date` - Unix timestamp start range
- `end_date` - Unix timestamp end range
- `page` - Page number (default: 1)
- `page_size` - Results per page (default: 50, max: 100)

**Access Control:**
- Global admins: Can view all audit logs across all tenants
- Tenant admins: Can view only their tenant's logs
- Regular users: Cannot access audit logs

#### Cluster Management (v0.8.0-beta)

**Cluster Configuration:**
- `POST /api/v1/cluster/initialize` - Initialize cluster on this node
- `GET /api/v1/cluster/status` - Get cluster configuration and status
- `DELETE /api/v1/cluster/leave` - Remove this node from cluster

**Node Management:**
- `GET /api/v1/cluster/nodes` - List all cluster nodes
- `POST /api/v1/cluster/nodes` - Add node to cluster
- `PUT /api/v1/cluster/nodes/{id}` - Update node configuration
- `DELETE /api/v1/cluster/nodes/{id}` - Remove node from cluster

**Health Monitoring:**
- `POST /api/v1/cluster/nodes/{id}/health` - Manually check node health
- `GET /api/v1/cluster/health/history` - Get health check history
- `GET /api/v1/cluster/health/summary` - Get cluster health summary

**Bucket Location Management:**
- `GET /api/v1/cluster/buckets/locations` - List all bucket locations
- `PUT /api/v1/cluster/buckets/{name}/location` - Set bucket location
- `DELETE /api/v1/cluster/cache` - Clear bucket location cache

**Cluster Replication:**
- `GET /api/v1/cluster/replication/rules` - List cluster replication rules
- `POST /api/v1/cluster/replication/rules` - Create replication rule
- `PUT /api/v1/cluster/replication/rules/{id}` - Update replication rule
- `DELETE /api/v1/cluster/replication/rules/{id}` - Delete replication rule
- `POST /api/v1/cluster/replication/sync` - Manually trigger sync

**Internal Cluster Endpoints** (HMAC-authenticated, inter-node only):
- `POST /api/internal/cluster/tenant-sync` - Receive tenant synchronization
- `PUT /api/internal/cluster/objects/{tenantID}/{bucket}/{key}` - Receive object replication
- `DELETE /api/internal/cluster/objects/{tenantID}/{bucket}/{key}` - Receive delete replication

**Initialize Cluster Example:**
```bash
curl -X POST http://localhost:8081/api/v1/cluster/initialize \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "node_name": "node-east-1",
    "s3_endpoint": "http://10.0.1.10:8080",
    "console_endpoint": "http://10.0.1.10:8081",
    "region": "us-east-1",
    "datacenter": "dc-east"
  }'
```

**Response:**
```json
{
  "success": true,
  "cluster_id": "cls-a1b2c3d4",
  "node_id": "node-e5f6g7h8",
  "node_token": "5f8a2b3c4d5e6f7g8h9i0j1k2l3m4n5o",
  "message": "Cluster initialized successfully"
}
```

**Add Node to Cluster Example:**
```bash
curl -X POST http://localhost:8081/api/v1/cluster/nodes \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "node_name": "node-west-1",
    "s3_endpoint": "http://10.0.2.20:8080",
    "console_endpoint": "http://10.0.2.20:8081",
    "region": "us-west-1",
    "datacenter": "dc-west"
  }'
```

**Response:**
```json
{
  "success": true,
  "node": {
    "id": "node-i9j0k1l2",
    "node_name": "node-west-1",
    "s3_endpoint": "http://10.0.2.20:8080",
    "console_endpoint": "http://10.0.2.20:8081",
    "region": "us-west-1",
    "datacenter": "dc-west",
    "status": "healthy",
    "is_primary": false,
    "created_at": "2025-12-09T10:30:00Z"
  }
}
```

**Get Cluster Status Example:**
```bash
curl http://localhost:8081/api/cluster/status \
  -H "Authorization: Bearer $TOKEN"
```

**Response:**
```json
{
  "success": true,
  "cluster": {
    "id": "cls-a1b2c3d4",
    "initialized": true,
    "primary_node_id": "node-e5f6g7h8",
    "node_count": 3,
    "healthy_nodes": 3,
    "total_buckets": 42,
    "replication_rules": 15
  },
  "nodes": [
    {
      "id": "node-e5f6g7h8",
      "node_name": "node-east-1",
      "s3_endpoint": "http://10.0.1.10:8080",
      "status": "healthy",
      "is_primary": true,
      "last_health_check": "2025-12-09T10:35:00Z",
      "latency_ms": 5,
      "bucket_count": 15
    },
    {
      "id": "node-i9j0k1l2",
      "node_name": "node-west-1",
      "s3_endpoint": "http://10.0.2.20:8080",
      "status": "healthy",
      "is_primary": false,
      "last_health_check": "2025-12-09T10:35:00Z",
      "latency_ms": 23,
      "bucket_count": 12
    }
  ]
}
```

**Create Cluster Replication Rule Example:**
```bash
curl -X POST http://localhost:8081/api/cluster/replication/rules \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "source_bucket": "backups",
    "destination_node_id": "node-i9j0k1l2",
    "sync_interval_seconds": 30,
    "enabled": true,
    "replicate_deletes": true
  }'
```

**Response:**
```json
{
  "success": true,
  "rule": {
    "id": "rep-m3n4o5p6",
    "tenant_id": "tenant-abc123",
    "source_bucket": "backups",
    "destination_node_id": "node-i9j0k1l2",
    "sync_interval_seconds": 30,
    "enabled": true,
    "replicate_deletes": true,
    "last_sync_at": null,
    "created_at": "2025-12-09T10:40:00Z"
  }
}
```

**Access Control (Cluster Endpoints):**
- **Global admins only**: All cluster management operations require global admin privileges
- **Tenant admins**: Cannot manage cluster (cluster is global infrastructure)
- **Internal endpoints**: Require HMAC-SHA256 authentication with `node_token`

> **See [CLUSTER.md](CLUSTER.md) for complete cluster documentation and architecture details**

### Example Usage

**Create tenant:**
```bash
curl -X POST http://localhost:8081/api/tenants \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "acme",
    "displayName": "ACME Corp",
    "maxStorageBytes": 107374182400,
    "maxBuckets": 100,
    "maxAccessKeys": 50
  }'
```

**Create user:**
```bash
curl -X POST http://localhost:8081/api/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "john",
    "password": "password123",
    "email": "john@acme.com",
    "roles": ["user"],
    "tenantId": "tenant-123"
  }'
```

**Get dashboard metrics:**
```bash
curl http://localhost:8081/api/metrics \
  -H "Authorization: Bearer $TOKEN"
```

---

## Error Responses

### S3 API Errors (XML)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>NoSuchBucket</Code>
  <Message>The specified bucket does not exist</Message>
  <Resource>/my-bucket</Resource>
</Error>
```

**Common S3 Error Codes:**
- `NoSuchBucket` - Bucket does not exist
- `BucketAlreadyExists` - Bucket name taken
- `NoSuchKey` - Object not found
- `AccessDenied` - Insufficient permissions
- `InvalidAccessKeyId` - Invalid credentials
- `SignatureDoesNotMatch` - Invalid signature
- `QuotaExceeded` - Tenant quota exceeded
- `ObjectLocked` - Object cannot be deleted (WORM)

### Console API Errors (JSON)

```json
{
  "success": false,
  "error": "Invalid credentials"
}
```

**HTTP Status Codes:**
- `200` - Success
- `400` - Bad Request
- `401` - Unauthorized (invalid/missing token)
- `403` - Forbidden (insufficient permissions)
- `404` - Not Found
- `409` - Conflict (duplicate resource)
- `500` - Internal Server Error

---

## Rate Limiting

**Login Rate Limits:**
- Max 5 login attempts per minute per IP
- Account locked for 15 minutes after 5 failed attempts
- Manual unlock by admin required

**API Rate Limits:**
- No global rate limits (alpha version)
- Tenant quotas enforce resource limits

---

## Health & Monitoring

**Health Check:**
```bash
curl http://localhost:8080/health
```

**Readiness Probe:**
```bash
curl http://localhost:8080/ready
```

**Prometheus Metrics:**
```bash
curl http://localhost:8080/metrics
```

**Response (sample):**
```prometheus
# HELP maxiofs_api_requests_total Total number of API requests
# TYPE maxiofs_api_requests_total counter
maxiofs_api_requests_total{method="GET",endpoint="/buckets"} 42

# HELP maxiofs_storage_used_bytes Current storage usage in bytes
# TYPE maxiofs_storage_used_bytes gauge
maxiofs_storage_used_bytes{tenant="tenant-abc123"} 1073741824

# HELP maxiofs_api_request_duration_seconds API request duration in seconds
# TYPE maxiofs_api_request_duration_seconds histogram
maxiofs_api_request_duration_seconds_bucket{endpoint="/objects",le="0.1"} 95
```

---

## Additional Resources

- [AWS S3 API Documentation](https://docs.aws.amazon.com/AmazonS3/latest/API/)
- [AWS CLI S3 Commands](https://docs.aws.amazon.com/cli/latest/reference/s3/)
- [boto3 S3 Client](https://boto3.amazonaws.com/v1/documentation/api/latest/reference/services/s3.html)

---

**Note**: This is a beta API. Core endpoints are stable, but minor changes may occur based on feedback.
