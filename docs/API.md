# MaxIOFS API Reference

**Version**: 0.9.2-beta | **Last Updated**: February 28, 2026

## Overview

MaxIOFS exposes two HTTP servers:

| Server | Default Port | Purpose | Authentication |
|--------|-------------|---------|----------------|
| **S3 API** | 8080 | AWS S3-compatible REST API | AWS Signature v2/v4 |
| **Console API** | 8081 | Web Console REST API + embedded frontend | JWT / OAuth2 |

---

## S3 API (Port 8080)

100% compatible with AWS S3 clients, SDKs, and CLI tools.

### Quick Start

```bash
# Configure AWS CLI
aws configure set aws_access_key_id YOUR_ACCESS_KEY
aws configure set aws_secret_access_key YOUR_SECRET_KEY

# Use MaxIOFS
aws --endpoint-url=http://localhost:8080 s3 mb s3://my-bucket
aws --endpoint-url=http://localhost:8080 s3 cp file.txt s3://my-bucket/
aws --endpoint-url=http://localhost:8080 s3 ls s3://my-bucket/
```

### Bucket Operations

| Operation | Method | Path / Query |
|-----------|--------|-------------|
| ListBuckets | GET | `/` |
| CreateBucket | PUT | `/{bucket}` |
| DeleteBucket | DELETE | `/{bucket}` |
| HeadBucket | HEAD | `/{bucket}` |
| GetBucketVersioning | GET | `/{bucket}?versioning` |
| PutBucketVersioning | PUT | `/{bucket}?versioning` |
| GetBucketCORS | GET | `/{bucket}?cors` |
| PutBucketCORS | PUT | `/{bucket}?cors` |
| DeleteBucketCORS | DELETE | `/{bucket}?cors` |
| GetBucketACL | GET | `/{bucket}?acl` |
| PutBucketACL | PUT | `/{bucket}?acl` |
| GetBucketPolicy | GET | `/{bucket}?policy` |
| PutBucketPolicy | PUT | `/{bucket}?policy` |
| DeleteBucketPolicy | DELETE | `/{bucket}?policy` |
| GetBucketTagging | GET | `/{bucket}?tagging` |
| PutBucketTagging | PUT | `/{bucket}?tagging` |
| DeleteBucketTagging | DELETE | `/{bucket}?tagging` |
| GetBucketLifecycle | GET | `/{bucket}?lifecycle` |
| PutBucketLifecycle | PUT | `/{bucket}?lifecycle` |
| DeleteBucketLifecycle | DELETE | `/{bucket}?lifecycle` |
| GetBucketNotification | GET | `/{bucket}?notification` |
| PutBucketNotification | PUT | `/{bucket}?notification` |
| GetObjectLockConfig | GET | `/{bucket}?object-lock` |
| PutObjectLockConfig | PUT | `/{bucket}?object-lock` |
| ListMultipartUploads | GET | `/{bucket}?uploads` |

### Object Operations

| Operation | Method | Path / Query |
|-----------|--------|-------------|
| GetObject | GET | `/{bucket}/{key+}` |
| PutObject | PUT | `/{bucket}/{key+}` |
| DeleteObject | DELETE | `/{bucket}/{key+}` |
| HeadObject | HEAD | `/{bucket}/{key+}` |
| CopyObject | PUT | `/{bucket}/{key+}` (header: `x-amz-copy-source`) |
| ListObjects | GET | `/{bucket}` |
| ListObjectsV2 | GET | `/{bucket}?list-type=2` |
| DeleteMultipleObjects | POST | `/{bucket}?delete` |

### Multipart Upload Operations

| Operation | Method | Path / Query |
|-----------|--------|-------------|
| CreateMultipartUpload | POST | `/{bucket}/{key+}?uploads` |
| UploadPart | PUT | `/{bucket}/{key+}?partNumber=N&uploadId=ID` |
| CompleteMultipartUpload | POST | `/{bucket}/{key+}?uploadId=ID` |
| AbortMultipartUpload | DELETE | `/{bucket}/{key+}?uploadId=ID` |
| ListParts | GET | `/{bucket}/{key+}?uploadId=ID` |

### Object Lock / Retention

| Operation | Method | Path / Query |
|-----------|--------|-------------|
| GetObjectRetention | GET | `/{bucket}/{key+}?retention` |
| PutObjectRetention | PUT | `/{bucket}/{key+}?retention` |
| GetObjectLegalHold | GET | `/{bucket}/{key+}?legal-hold` |
| PutObjectLegalHold | PUT | `/{bucket}/{key+}?legal-hold` |

### ACL Operations

| Operation | Method | Path / Query |
|-----------|--------|-------------|
| GetObjectACL | GET | `/{bucket}/{key+}?acl` |
| PutObjectACL | PUT | `/{bucket}/{key+}?acl` |

### Tagging Operations

| Operation | Method | Path / Query |
|-----------|--------|-------------|
| GetObjectTagging | GET | `/{bucket}/{key+}?tagging` |
| PutObjectTagging | PUT | `/{bucket}/{key+}?tagging` |
| DeleteObjectTagging | DELETE | `/{bucket}/{key+}?tagging` |

### Additional Features

- **Presigned URLs** — GET/PUT with configurable expiration (S3-compatible paths)
- **Range Requests** — Partial object downloads via `Range` header
- **Conditional Requests** — `If-Match`, `If-None-Match`, `If-Modified-Since`

### Health Endpoints (No Auth)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check |
| GET | `/ready` | Readiness probe |
| GET | `/metrics` | Prometheus metrics |

---

## Console API (Port 8081)

REST API for web console management. All endpoints prefixed with `/api/v1` unless noted. JWT authentication required (via `Authorization: Bearer <token>` header).

### Authentication

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| POST | `/api/v1/auth/login` | Login (username + password + optional TOTP) | None |
| POST | `/api/v1/auth/logout` | Logout | JWT |
| GET | `/api/v1/auth/me` | Get current user info | JWT |

### Two-Factor Authentication

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/auth/2fa/setup` | Start 2FA setup (returns QR code) |
| POST | `/api/v1/auth/2fa/verify` | Verify 2FA setup with TOTP code |
| POST | `/api/v1/auth/2fa/disable` | Disable 2FA |
| POST | `/api/v1/auth/2fa/validate` | Validate a TOTP code |
| POST | `/api/v1/auth/2fa/backup-codes` | Regenerate backup codes |
| GET | `/api/v1/auth/2fa/backup-codes` | Get backup codes |

### OAuth / SSO

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| GET | `/api/v1/auth/oauth/providers` | List active OAuth providers | None |
| POST | `/api/v1/auth/oauth/login` | Start OAuth login flow | None |
| GET | `/api/v1/auth/oauth/callback` | OAuth callback from provider | None |
| GET | `/api/v1/auth/oauth/complete` | Complete OAuth login | None |

### Users

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/users` | List users |
| POST | `/api/v1/users` | Create user |
| GET | `/api/v1/users/{id}` | Get user details |
| PUT | `/api/v1/users/{id}` | Update user |
| DELETE | `/api/v1/users/{id}` | Delete user |
| PUT | `/api/v1/users/{id}/password` | Change password |
| PATCH | `/api/v1/users/{id}/status` | Update user status (activate/deactivate) |
| POST | `/api/v1/users/{id}/unlock` | Unlock locked account |

### Access Keys

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/access-keys` | List all access keys |
| GET | `/api/v1/access-keys/user/{userId}` | List user's access keys |
| POST | `/api/v1/access-keys` | Create access key |
| DELETE | `/api/v1/access-keys/{id}` | Delete access key |

### Tenants

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/tenants` | List tenants |
| POST | `/api/v1/tenants` | Create tenant |
| GET | `/api/v1/tenants/{id}` | Get tenant details |
| PUT | `/api/v1/tenants/{id}` | Update tenant |
| DELETE | `/api/v1/tenants/{id}` | Delete tenant |
| GET | `/api/v1/tenants/{id}/stats` | Get tenant statistics |

### Buckets

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/buckets` | List buckets |
| POST | `/api/v1/buckets` | Create bucket |
| GET | `/api/v1/buckets/{name}` | Get bucket details |
| DELETE | `/api/v1/buckets/{name}` | Delete bucket |

### Bucket Configuration

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/buckets/{name}/permissions` | List bucket permissions |
| POST | `/api/v1/buckets/{name}/permissions` | Add permission |
| DELETE | `/api/v1/buckets/{name}/permissions/{id}` | Remove permission |
| PUT | `/api/v1/buckets/{name}/permissions/{id}` | Update permission |
| GET | `/api/v1/buckets/{name}/versioning` | Get versioning config |
| PUT | `/api/v1/buckets/{name}/versioning` | Set versioning config |
| GET | `/api/v1/buckets/{name}/lifecycle` | Get lifecycle rules |
| PUT | `/api/v1/buckets/{name}/lifecycle` | Set lifecycle rules |
| DELETE | `/api/v1/buckets/{name}/lifecycle` | Delete lifecycle rules |
| GET | `/api/v1/buckets/{name}/cors` | Get CORS config |
| PUT | `/api/v1/buckets/{name}/cors` | Set CORS config |
| DELETE | `/api/v1/buckets/{name}/cors` | Delete CORS config |
| GET | `/api/v1/buckets/{name}/acl` | Get bucket ACL |
| PUT | `/api/v1/buckets/{name}/acl` | Set bucket ACL |
| GET | `/api/v1/buckets/{name}/policy` | Get bucket policy |
| PUT | `/api/v1/buckets/{name}/policy` | Set bucket policy |
| DELETE | `/api/v1/buckets/{name}/policy` | Delete bucket policy |
| GET | `/api/v1/buckets/{name}/tagging` | Get bucket tags |
| PUT | `/api/v1/buckets/{name}/tagging` | Set bucket tags |
| DELETE | `/api/v1/buckets/{name}/tagging` | Delete bucket tags |
| GET | `/api/v1/buckets/{name}/notifications` | Get notification config |
| PUT | `/api/v1/buckets/{name}/notifications` | Set notification config |
| DELETE | `/api/v1/buckets/{name}/notifications` | Delete notification config |
| PUT | `/api/v1/buckets/{name}/object-lock` | Enable object lock |
| GET | `/api/v1/buckets/{name}/inventory` | Get inventory config |
| PUT | `/api/v1/buckets/{name}/inventory` | Set inventory config |
| DELETE | `/api/v1/buckets/{name}/inventory` | Delete inventory config |
| GET | `/api/v1/buckets/{name}/inventory/reports` | List inventory reports |
| POST | `/api/v1/buckets/{name}/verify-integrity` | Verify bucket object integrity (admin only, rate-limited) |
| POST | `/api/v1/buckets/{name}/recalculate-stats` | Recalculate bucket object count and size (admin only) |

### Bucket Replication (External S3)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/buckets/{name}/replication` | List replication rules |
| POST | `/api/v1/buckets/{name}/replication` | Create replication rule |
| GET | `/api/v1/buckets/{name}/replication/{id}` | Get rule details |
| PUT | `/api/v1/buckets/{name}/replication/{id}` | Update rule |
| DELETE | `/api/v1/buckets/{name}/replication/{id}` | Delete rule |
| GET | `/api/v1/buckets/{name}/replication/{id}/status` | Get replication status |
| POST | `/api/v1/buckets/{name}/replication/{id}/sync` | Trigger manual sync |

### Objects

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/buckets/{bucket}/objects` | List objects |
| GET | `/api/v1/buckets/{bucket}/objects/search` | Search objects (filters) |
| GET | `/api/v1/buckets/{bucket}/objects/{key+}` | Download object |
| PUT | `/api/v1/buckets/{bucket}/objects/{key+}` | Upload object |
| DELETE | `/api/v1/buckets/{bucket}/objects/{key+}` | Delete object |
| GET | `/api/v1/buckets/{bucket}/objects/{key+}/acl` | Get object ACL |
| PUT | `/api/v1/buckets/{bucket}/objects/{key+}/acl` | Set object ACL |
| GET | `/api/v1/buckets/{bucket}/objects/{key+}/legal-hold` | Get legal hold |
| PUT | `/api/v1/buckets/{bucket}/objects/{key+}/legal-hold` | Set legal hold |
| GET | `/api/v1/buckets/{bucket}/objects/{key+}/versions` | List object versions |

### Shares & Presigned URLs

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/shares` | List active share links |
| POST | `/api/v1/shares` | Create share link |
| DELETE | `/api/v1/shares/{id}` | Revoke share link |
| POST | `/api/v1/presign` | Generate presigned URL |

### Metrics & Monitoring

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/metrics` | Dashboard metrics |
| GET | `/api/v1/metrics/system` | System metrics (CPU, memory, disk) |
| GET | `/api/v1/metrics/storage` | Storage metrics |
| GET | `/api/v1/metrics/performance` | Performance metrics |
| GET | `/api/v1/metrics/history` | Metrics history |
| GET | `/api/v1/performance/overview` | Performance overview |
| GET | `/api/v1/performance/operations` | Operation-level metrics |
| GET | `/api/v1/performance/history` | Performance history |
| POST | `/api/v1/performance/reset` | Reset performance counters |

### Audit Logs

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/audit-logs` | List audit logs (with filtering) |
| GET | `/api/v1/audit-logs/{id}` | Get specific audit log entry |

**Query parameters**: `tenant_id`, `user_id`, `event_type`, `resource_type`, `action`, `status`, `start_date`, `end_date`, `page`, `page_size`

### Settings

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/settings` | List all settings |
| GET | `/api/v1/settings/{key}` | Get setting value |
| GET | `/api/v1/settings/category/{category}` | List settings by category |
| PUT | `/api/v1/settings/{key}` | Update setting |
| POST | `/api/v1/settings/reset` | Reset all to defaults |

### Logging Configuration

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/logging/test-syslog` | Test syslog output |
| POST | `/api/v1/logging/test-http` | Test HTTP log output |
| POST | `/api/v1/logging/test-file` | Test file log output |

### Identity Providers (IDP)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/identity-providers` | List all providers |
| POST | `/api/v1/identity-providers` | Create provider |
| GET | `/api/v1/identity-providers/{id}` | Get provider details |
| PUT | `/api/v1/identity-providers/{id}` | Update provider |
| DELETE | `/api/v1/identity-providers/{id}` | Delete provider |
| POST | `/api/v1/identity-providers/{id}/test` | Test provider connection |
| POST | `/api/v1/identity-providers/{id}/search-users` | Search users in provider |
| POST | `/api/v1/identity-providers/{id}/search-groups` | Search groups in provider |
| POST | `/api/v1/identity-providers/{id}/import-user` | Import user from provider |
| POST | `/api/v1/identity-providers/{id}/sync` | Sync all group memberships |

### Group Mappings (IDP)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/identity-providers/{id}/group-mappings` | List group mappings |
| POST | `/api/v1/identity-providers/{id}/group-mappings` | Create group mapping |
| PUT | `/api/v1/identity-providers/{id}/group-mappings/{mapId}` | Update mapping |
| DELETE | `/api/v1/identity-providers/{id}/group-mappings/{mapId}` | Delete mapping |
| POST | `/api/v1/identity-providers/{id}/group-mappings/{mapId}/sync` | Sync specific mapping |

### Cluster Management

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/cluster/initialize` | Initialize cluster on this node |
| POST | `/api/v1/cluster/join` | Join existing cluster |
| POST | `/api/v1/cluster/leave` | Leave cluster |
| GET | `/api/v1/cluster/status` | Get cluster status |
| GET | `/api/v1/cluster/config` | Get cluster configuration |
| GET | `/api/v1/cluster/nodes` | List all nodes |
| POST | `/api/v1/cluster/nodes` | Add node |
| GET | `/api/v1/cluster/nodes/{id}` | Get node details |
| PUT | `/api/v1/cluster/nodes/{id}` | Update node |
| DELETE | `/api/v1/cluster/nodes/{id}` | Remove node |
| GET | `/api/v1/cluster/health` | Cluster health summary |
| GET | `/api/v1/cluster/health/history` | Health check history |
| POST | `/api/v1/cluster/health/refresh` | Trigger manual health check |
| GET | `/api/v1/cluster/cache/stats` | Cache statistics |
| DELETE | `/api/v1/cluster/cache` | Clear bucket location cache |

### Cluster Replication

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/cluster/replication` | List replication rules |
| POST | `/api/v1/cluster/replication` | Create replication rule |
| PUT | `/api/v1/cluster/replication/{id}` | Update rule |
| DELETE | `/api/v1/cluster/replication/{id}` | Delete rule |
| POST | `/api/v1/cluster/replication/bulk` | Bulk replicate all buckets |

### Cluster Migrations

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/cluster/buckets/{bucket}/migrate` | Start bucket migration |
| GET | `/api/v1/cluster/migrations` | List migrations |
| GET | `/api/v1/cluster/migrations/{id}` | Get migration details |

### Notifications (SSE)

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| GET | `/api/v1/notifications/stream` | SSE event stream | JWT |

### System

| Method | Path | Description | Auth |
|--------|------|-------------|------|
| GET | `/api/v1/version` | Server version info | None |
| GET | `/api/v1/config` | Public server configuration (includes `maintenanceMode`) | JWT |
| GET | `/health` | Health check | None |
| GET | `/api/v1/security/status` | Security status overview | JWT |

### Profiling

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/profiling` | Go pprof data (global admin only) |

---

## Error Responses

### S3 API (XML)

```xml
<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>NoSuchBucket</Code>
  <Message>The specified bucket does not exist</Message>
  <Resource>/my-bucket</Resource>
</Error>
```

Common codes: `NoSuchBucket`, `NoSuchKey`, `BucketAlreadyExists`, `AccessDenied`, `InvalidAccessKeyId`, `SignatureDoesNotMatch`, `QuotaExceeded`, `ObjectLocked`

### Console API (JSON)

```json
{
  "success": false,
  "error": "Invalid credentials"
}
```

HTTP status codes: 200 (success), 400 (bad request), 401 (unauthorized), 403 (forbidden), 404 (not found), 409 (conflict), 429 (rate limited), 500 (server error)

---

## Prometheus Metrics

Available at `/metrics` on both ports. Key metrics:

```
maxiofs_s3_operations_total{operation, status}
maxiofs_s3_operation_duration_seconds{operation}
maxiofs_storage_used_bytes{tenant}
maxiofs_objects_total{tenant}
maxiofs_buckets_total{tenant}
maxiofs_api_requests_total{method, endpoint}
cluster_nodes_total
cluster_nodes_healthy
cluster_replication_objects_pending
cluster_cache_hit_ratio
```

---

**See also**: [ARCHITECTURE.md](ARCHITECTURE.md) · [CLUSTER.md](CLUSTER.md) · [OPERATIONS.md](OPERATIONS.md) · [SSO.md](SSO.md) · [SECURITY.md](SECURITY.md)
