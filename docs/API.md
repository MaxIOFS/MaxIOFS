# MaxIOFS API Reference

**Version**: 0.5.0-beta
**S3 Compatibility**: 98%
**Last Updated**: November 23, 2025

## Overview

MaxIOFS provides two APIs:

1. **S3 API** (Port 8080) - 98% AWS S3-compatible REST API
2. **Console API** (Port 8081) - Management REST API for web console

### Recent Updates (v0.4.2-beta)

- ✅ **Global Bucket Uniqueness** - Bucket names now globally unique across all tenants (AWS S3 compatible)
- ✅ **S3-Compatible URLs** - Presigned and share URLs without tenant prefix for standard S3 client compatibility
- ✅ **Bucket Notifications (Webhooks)** - AWS S3 compatible event notifications (ObjectCreated, ObjectRemoved, ObjectRestored)
- ✅ **Automatic Tenant Resolution** - Backend automatically resolves bucket ownership from bucket name
- ✅ **Frontend Modal Improvements** - Fixed presigned URL modal state persistence

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

REST API for web console management. All endpoints require JWT authentication.

### Authentication

**Login:**
```http
POST /api/auth/login
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
GET /api/users
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

### Available Endpoints

#### Authentication
- `POST /api/auth/login` - Login with username/password (with optional TOTP code)
- `GET /api/auth/me` - Get current user info
- `POST /api/auth/logout` - Logout
- `POST /api/auth/2fa/enable` - Enable 2FA for current user
- `POST /api/auth/2fa/verify` - Verify 2FA setup
- `POST /api/auth/2fa/disable` - Disable 2FA for current user
- `GET /api/auth/2fa/backup-codes` - Get backup codes

#### User Management
- `GET /api/users` - List users
- `POST /api/users` - Create user
- `PUT /api/users/{id}` - Update user
- `DELETE /api/users/{id}` - Delete user
- `POST /api/users/{id}/unlock` - Unlock locked account

#### Tenant Management
- `GET /api/tenants` - List tenants
- `POST /api/tenants` - Create tenant
- `PUT /api/tenants/{id}` - Update tenant
- `DELETE /api/tenants/{id}` - Delete tenant
- `GET /api/tenants/{id}/stats` - Get tenant statistics

#### Access Key Management
- `GET /api/access-keys` - List access keys
- `POST /api/access-keys` - Create access key
- `DELETE /api/access-keys/{id}` - Revoke access key

#### Bucket Management
- `GET /api/buckets` - List buckets
- `POST /api/buckets` - Create bucket
- `DELETE /api/buckets/{name}` - Delete bucket
- `GET /api/buckets/{name}/stats` - Get bucket statistics

#### Object Management
- `GET /api/buckets/{bucket}/objects` - List objects
- `POST /api/buckets/{bucket}/objects` - Upload object (multipart/form-data)
- `DELETE /api/buckets/{bucket}/objects/{key}` - Delete object
- `POST /api/buckets/{bucket}/objects/{key}/share` - Generate share URL
- `DELETE /api/buckets/{bucket}/objects/{key}/share` - Revoke share

#### Metrics
- `GET /api/metrics` - Dashboard metrics
- `GET /api/metrics/system` - System metrics (CPU, memory, disk)
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
