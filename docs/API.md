# MaxIOFS API Reference

## Overview

MaxIOFS provides two separate APIs:

1. **S3 API** (Port 8080) - AWS S3-compatible REST API for object storage operations
2. **Console API** (Port 8081) - Management REST API for the web console

## S3 API (Port 8080)

The S3 API provides full AWS S3 compatibility with support for both signature V2 and V4 authentication.

### Authentication

#### AWS Signature V4
```bash
# Using AWS CLI
aws configure set aws_access_key_id maxioadmin
aws configure set aws_secret_access_key maxioadmin
aws configure set default.s3.signature_version s3v4
aws --endpoint-url=http://localhost:8080 s3 ls
```

#### AWS Signature V2
```bash
# Using s3cmd
s3cmd --access_key=maxioadmin --secret_key=maxioadmin --host=localhost:8080 --no-ssl ls
```

### Bucket Operations

#### List Buckets
```http
GET / HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

**Response:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<ListAllMyBucketsResult>
  <Buckets>
    <Bucket>
      <Name>my-bucket</Name>
      <CreationDate>2025-10-05T12:00:00.000Z</CreationDate>
    </Bucket>
  </Buckets>
</ListAllMyBucketsResult>
```

#### Create Bucket
```http
PUT /my-bucket HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

**Response:** 200 OK

#### Delete Bucket
```http
DELETE /my-bucket HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

**Response:** 204 No Content

#### Head Bucket
```http
HEAD /my-bucket HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

**Response:** 200 OK (bucket exists) or 404 Not Found

### Object Operations

#### Put Object
```http
PUT /my-bucket/myfile.txt HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
Content-Type: text/plain
Content-Length: 13

Hello, World!
```

**Response:** 200 OK
```xml
<PutObjectResult>
  <ETag>"d41d8cd98f00b204e9800998ecf8427e"</ETag>
</PutObjectResult>
```

#### Get Object
```http
GET /my-bucket/myfile.txt HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

**Response:** 200 OK + object data

#### Delete Object
```http
DELETE /my-bucket/myfile.txt HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

**Response:** 204 No Content

#### Head Object
```http
HEAD /my-bucket/myfile.txt HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

**Response Headers:**
```
Content-Length: 13
Content-Type: text/plain
ETag: "d41d8cd98f00b204e9800998ecf8427e"
Last-Modified: Sat, 05 Oct 2025 12:00:00 GMT
```

#### List Objects (V2)
```http
GET /my-bucket?list-type=2 HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

**Query Parameters:**
- `list-type=2` - Use ListObjectsV2
- `prefix` - Filter by prefix
- `delimiter` - Group by delimiter
- `max-keys` - Maximum objects to return (default 1000)
- `continuation-token` - Pagination token

**Response:**
```xml
<ListBucketResult>
  <Name>my-bucket</Name>
  <Prefix></Prefix>
  <KeyCount>2</KeyCount>
  <MaxKeys>1000</MaxKeys>
  <IsTruncated>false</IsTruncated>
  <Contents>
    <Key>file1.txt</Key>
    <LastModified>2025-10-05T12:00:00.000Z</LastModified>
    <ETag>"abc123"</ETag>
    <Size>1024</Size>
    <StorageClass>STANDARD</StorageClass>
  </Contents>
</ListBucketResult>
```

### Multipart Upload Operations

#### Initiate Multipart Upload
```http
POST /my-bucket/large-file.bin?uploads HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

**Response:**
```xml
<InitiateMultipartUploadResult>
  <Bucket>my-bucket</Bucket>
  <Key>large-file.bin</Key>
  <UploadId>VXBsb2FkSUQ</UploadId>
</InitiateMultipartUploadResult>
```

#### Upload Part
```http
PUT /my-bucket/large-file.bin?partNumber=1&uploadId=VXBsb2FkSUQ HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
Content-Length: 5242880

[binary data]
```

**Response:**
```
ETag: "part1-etag"
```

#### Complete Multipart Upload
```http
POST /my-bucket/large-file.bin?uploadId=VXBsb2FkSUQ HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...

<CompleteMultipartUpload>
  <Part>
    <PartNumber>1</PartNumber>
    <ETag>"part1-etag"</ETag>
  </Part>
  <Part>
    <PartNumber>2</PartNumber>
    <ETag>"part2-etag"</ETag>
  </Part>
</CompleteMultipartUpload>
```

#### Abort Multipart Upload
```http
DELETE /my-bucket/large-file.bin?uploadId=VXBsb2FkSUQ HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

#### List Parts
```http
GET /my-bucket/large-file.bin?uploadId=VXBsb2FkSUQ HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

#### List Multipart Uploads
```http
GET /my-bucket?uploads HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

### Object Lock Operations

#### Put Object Retention
```http
PUT /my-bucket/myfile.txt?retention HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...

<Retention>
  <Mode>COMPLIANCE</Mode>
  <RetainUntilDate>2026-01-01T00:00:00Z</RetainUntilDate>
</Retention>
```

#### Get Object Retention
```http
GET /my-bucket/myfile.txt?retention HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

**Response:**
```xml
<Retention>
  <Mode>COMPLIANCE</Mode>
  <RetainUntilDate>2026-01-01T00:00:00Z</RetainUntilDate>
</Retention>
```

#### Put Object Legal Hold
```http
PUT /my-bucket/myfile.txt?legal-hold HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...

<LegalHold>
  <Status>ON</Status>
</LegalHold>
```

#### Get Object Legal Hold
```http
GET /my-bucket/myfile.txt?legal-hold HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

### Bucket Configuration Operations

#### Put Bucket Versioning
```http
PUT /my-bucket?versioning HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...

<VersioningConfiguration>
  <Status>Enabled</Status>
</VersioningConfiguration>
```

#### Get Bucket Versioning
```http
GET /my-bucket?versioning HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

#### Put Bucket CORS
```http
PUT /my-bucket?cors HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...

<CORSConfiguration>
  <CORSRule>
    <AllowedOrigin>*</AllowedOrigin>
    <AllowedMethod>GET</AllowedMethod>
    <AllowedMethod>PUT</AllowedMethod>
    <AllowedHeader>*</AllowedHeader>
  </CORSRule>
</CORSConfiguration>
```

#### Get Bucket CORS
```http
GET /my-bucket?cors HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

#### Delete Bucket CORS
```http
DELETE /my-bucket?cors HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...
```

### Presigned URLs

#### Generate Presigned URL (V4)
```go
import (
    "github.com/aws/aws-sdk-go/aws"
    "github.com/aws/aws-sdk-go/aws/credentials"
    "github.com/aws/aws-sdk-go/aws/session"
    "github.com/aws/aws-sdk-go/service/s3"
)

sess := session.Must(session.NewSession(&aws.Config{
    Region:      aws.String("us-east-1"),
    Endpoint:    aws.String("http://localhost:8080"),
    Credentials: credentials.NewStaticCredentials("maxioadmin", "maxioadmin", ""),
    S3ForcePathStyle: aws.Bool(true),
}))

svc := s3.New(sess)
req, _ := svc.GetObjectRequest(&s3.GetObjectInput{
    Bucket: aws.String("my-bucket"),
    Key:    aws.String("myfile.txt"),
})

url, err := req.Presign(15 * time.Minute)
```

### Batch Operations

#### Delete Multiple Objects
```http
POST /my-bucket?delete HTTP/1.1
Host: localhost:8080
Authorization: AWS4-HMAC-SHA256 ...

<Delete>
  <Object>
    <Key>file1.txt</Key>
  </Object>
  <Object>
    <Key>file2.txt</Key>
  </Object>
</Delete>
```

**Response:**
```xml
<DeleteResult>
  <Deleted>
    <Key>file1.txt</Key>
  </Deleted>
  <Deleted>
    <Key>file2.txt</Key>
  </Deleted>
</DeleteResult>
```

---

## Console API (Port 8081)

The Console API provides management endpoints for the web interface. All endpoints require JWT authentication.

### Authentication

#### Login
```http
POST /api/auth/login HTTP/1.1
Host: localhost:8081
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
    "email": "admin@example.com",
    "roles": ["admin"],
    "status": "active"
  }
}
```

#### Get Current User
```http
GET /api/auth/me HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

#### Logout
```http
POST /api/auth/logout HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

### User Management

#### List Users
```http
GET /api/users HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

**Response:**
```json
{
  "success": true,
  "data": [
    {
      "id": "user-123",
      "username": "admin",
      "email": "admin@example.com",
      "roles": ["admin"],
      "status": "active",
      "tenantId": "",
      "createdAt": 1696512000
    }
  ]
}
```

#### Create User
```http
POST /api/users HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
Content-Type: application/json

{
  "username": "newuser",
  "email": "user@example.com",
  "password": "password123",
  "roles": ["user"],
  "tenantId": "tenant-456"
}
```

#### Update User
```http
PUT /api/users/{userId} HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
Content-Type: application/json

{
  "email": "newemail@example.com",
  "roles": ["admin"],
  "status": "active"
}
```

#### Delete User
```http
DELETE /api/users/{userId} HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

#### Unlock User Account
```http
POST /api/users/{userId}/unlock HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

### Tenant Management

#### List Tenants
```http
GET /api/tenants HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

**Response:**
```json
{
  "success": true,
  "data": [
    {
      "id": "tenant-456",
      "name": "acme",
      "displayName": "ACME Corporation",
      "status": "active",
      "maxStorageBytes": 107374182400,
      "currentStorageBytes": 1073741824,
      "maxBuckets": 100,
      "currentBuckets": 5,
      "maxAccessKeys": 50,
      "currentAccessKeys": 3,
      "createdAt": 1696512000
    }
  ]
}
```

#### Create Tenant
```http
POST /api/tenants HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
Content-Type: application/json

{
  "name": "acme",
  "displayName": "ACME Corporation",
  "maxStorageBytes": 107374182400,
  "maxBuckets": 100,
  "maxAccessKeys": 50
}
```

#### Update Tenant
```http
PUT /api/tenants/{tenantId} HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
Content-Type: application/json

{
  "displayName": "ACME Corp",
  "maxStorageBytes": 214748364800,
  "status": "active"
}
```

#### Delete Tenant
```http
DELETE /api/tenants/{tenantId} HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

### Access Key Management

#### List Access Keys
```http
GET /api/access-keys HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

#### Create Access Key
```http
POST /api/access-keys HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
Content-Type: application/json

{
  "userId": "user-123",
  "permissions": ["s3:*"]
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "id": "key-789",
    "accessKey": "AKIAIOSFODNN7EXAMPLE",
    "secretKey": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    "userId": "user-123",
    "status": "active",
    "createdAt": 1696512000
  }
}
```

#### Delete Access Key
```http
DELETE /api/access-keys/{keyId} HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

### Bucket Management

#### List Buckets
```http
GET /api/buckets HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

#### Create Bucket
```http
POST /api/buckets HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
Content-Type: application/json

{
  "name": "my-bucket",
  "region": "us-east-1",
  "versioning": true,
  "objectLock": false
}
```

#### Delete Bucket
```http
DELETE /api/buckets/{bucketName} HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

### Object Management

#### List Objects
```http
GET /api/buckets/{bucketName}/objects HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

**Query Parameters:**
- `prefix` - Filter by prefix
- `delimiter` - Group by delimiter
- `maxKeys` - Maximum objects (default 1000)

#### Upload Object
```http
POST /api/buckets/{bucketName}/objects HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
Content-Type: multipart/form-data

--boundary
Content-Disposition: form-data; name="file"; filename="myfile.txt"

[file content]
--boundary--
```

#### Delete Object
```http
DELETE /api/buckets/{bucketName}/objects/{objectKey} HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

#### Share Object
```http
POST /api/buckets/{bucketName}/objects/{objectKey}/share HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
Content-Type: application/json

{
  "expiresIn": 3600
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "shareUrl": "http://localhost:8080/my-bucket/myfile.txt",
    "expiresAt": 1696515600
  }
}
```

#### Unshare Object
```http
DELETE /api/buckets/{bucketName}/objects/{objectKey}/share HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

### Metrics

#### Get System Metrics
```http
GET /api/metrics/system HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

**Response:**
```json
{
  "success": true,
  "data": {
    "cpu": {
      "usage_percent": 12.5
    },
    "memory": {
      "total_bytes": 16777216000,
      "used_bytes": 8388608000,
      "usage_percent": 50.0
    },
    "disk": {
      "total_bytes": 1099511627776,
      "used_bytes": 549755813888,
      "usage_percent": 50.0
    },
    "timestamp": 1696512000
  }
}
```

#### Get Dashboard Metrics
```http
GET /api/metrics HTTP/1.1
Host: localhost:8081
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

---

## Error Responses

### S3 API Errors

All S3 API errors follow the AWS S3 error format:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<Error>
  <Code>NoSuchBucket</Code>
  <Message>The specified bucket does not exist</Message>
  <Resource>/my-bucket</Resource>
  <RequestId>request-123</RequestId>
</Error>
```

**Common Error Codes:**
- `NoSuchBucket` - Bucket does not exist
- `BucketAlreadyExists` - Bucket name already taken
- `NoSuchKey` - Object does not exist
- `AccessDenied` - Insufficient permissions
- `InvalidAccessKeyId` - Invalid access key
- `SignatureDoesNotMatch` - Invalid signature
- `QuotaExceeded` - Tenant quota exceeded
- `ObjectLocked` - Object is locked and cannot be deleted

### Console API Errors

Console API errors return JSON format:

```json
{
  "success": false,
  "error": "Invalid credentials"
}
```

**HTTP Status Codes:**
- `200` - Success
- `400` - Bad Request
- `401` - Unauthorized
- `403` - Forbidden
- `404` - Not Found
- `409` - Conflict
- `500` - Internal Server Error

---

## Rate Limiting

### Login Rate Limiting
- **Limit:** 5 login attempts per minute per IP
- **Lockout:** Account locked for 15 minutes after 5 failed attempts
- **Unlock:** Manual unlock by Global Admin or Tenant Admin

### API Rate Limiting
- No global rate limits currently implemented
- Tenant quotas enforce storage and resource limits

---

## Examples

### Using AWS CLI
```bash
# Configure
aws configure set aws_access_key_id maxioadmin
aws configure set aws_secret_access_key maxioadmin

# List buckets
aws --endpoint-url=http://localhost:8080 s3 ls

# Upload file
aws --endpoint-url=http://localhost:8080 s3 cp file.txt s3://my-bucket/

# Download file
aws --endpoint-url=http://localhost:8080 s3 cp s3://my-bucket/file.txt .

# Delete object
aws --endpoint-url=http://localhost:8080 s3 rm s3://my-bucket/file.txt
```

### Using curl
```bash
# List buckets (simplified, no signature)
curl -X GET http://localhost:8080/ \
  -H "Authorization: AWS maxioadmin:..."

# Console API - Login
curl -X POST http://localhost:8081/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin"}'

# Console API - List users
curl -X GET http://localhost:8081/api/users \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIs..."
```

### Using Python boto3
```python
import boto3

# Create S3 client
s3 = boto3.client(
    's3',
    endpoint_url='http://localhost:8080',
    aws_access_key_id='maxioadmin',
    aws_secret_access_key='maxioadmin'
)

# List buckets
response = s3.list_buckets()
for bucket in response['Buckets']:
    print(bucket['Name'])

# Upload file
s3.upload_file('local.txt', 'my-bucket', 'remote.txt')

# Download file
s3.download_file('my-bucket', 'remote.txt', 'local.txt')
```

---

## API Compatibility

### Supported S3 Operations
- ✅ Bucket CRUD (Create, List, Delete, Head)
- ✅ Object CRUD (Put, Get, Delete, Head, List)
- ✅ Multipart Upload (6 operations)
- ✅ Object Lock (Retention, Legal Hold)
- ✅ Versioning (configuration)
- ✅ CORS (configuration)
- ✅ Presigned URLs (V2, V4)
- ✅ Batch Delete (up to 1000 objects)

### Not Supported (Yet)
- ❌ Bucket Lifecycle Policies (planned)
- ❌ Server-Side Encryption with KMS
- ❌ Object ACLs (planned)
- ❌ Bucket Logging
- ❌ Bucket Notification
- ❌ Object Replication
