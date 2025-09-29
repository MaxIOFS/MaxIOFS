# MaxIOFS Architecture

## Overview

MaxIOFS is designed as a high-performance, S3-compatible object storage system with the following key architectural principles:

- **Single Binary Deployment**: Self-contained executable with embedded web interface
- **Modular Design**: Clean separation of concerns with pluggable components
- **S3 Compatibility**: Full AWS S3 API compatibility for seamless migration
- **Performance First**: Built in Go for maximum speed and efficiency
- **Scalability**: Designed to handle large-scale deployments

## System Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        MaxIOFS Binary                          │
├─────────────────────────────────────────────────────────────────┤
│                     HTTP Layer                                 │
│  ┌─────────────────┐           ┌─────────────────┐            │
│  │   API Server    │           │  Console Server │            │
│  │   (Port 8080)   │           │   (Port 8081)   │            │
│  │                 │           │                 │            │
│  │ S3 Compatible   │           │ Web Management  │            │
│  │ REST API        │           │ Interface       │            │
│  └─────────────────┘           └─────────────────┘            │
├─────────────────────────────────────────────────────────────────┤
│                   Middleware Layer                             │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐         │
│  │   Auth   │ │  Metrics │ │   CORS   │ │ Logging  │         │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘         │
├─────────────────────────────────────────────────────────────────┤
│                   Business Logic Layer                         │
│  ┌─────────────────┐           ┌─────────────────┐            │
│  │ Bucket Manager  │           │ Object Manager  │            │
│  │                 │           │                 │            │
│  │ • Bucket CRUD   │           │ • Object CRUD   │            │
│  │ • Policies      │           │ • Object Lock   │            │
│  │ • Versioning    │           │ • Multipart     │            │
│  │ • Lifecycle     │           │ • Encryption    │            │
│  └─────────────────┘           └─────────────────┘            │
├─────────────────────────────────────────────────────────────────┤
│                    Storage Layer                               │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │                Storage Backend                          │   │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐   │   │
│  │  │Filesystem│ │    S3    │ │   GCS    │ │  Azure   │   │   │
│  │  │ Backend  │ │ Backend  │ │ Backend  │ │ Backend  │   │   │
│  │  └──────────┘ └──────────┘ └──────────┘ └──────────┘   │   │
│  └─────────────────────────────────────────────────────────┘   │
├─────────────────────────────────────────────────────────────────┤
│                   Data Layer                                   │
│  ┌─────────────────┐           ┌─────────────────┐            │
│  │   Metadata DB   │           │  Object Storage │            │
│  │                 │           │                 │            │
│  │ • Bucket info   │           │ • Object data   │            │
│  │ • Object meta   │           │ • Versions      │            │
│  │ • User data     │           │ • Temp files    │            │
│  │ • Policies      │           │ • Multiparts    │            │
│  └─────────────────┘           └─────────────────┘            │
└─────────────────────────────────────────────────────────────────┘
```

## Component Details

### 1. HTTP Layer

#### API Server (Port 8080)
- Implements complete S3 REST API
- Handles all object and bucket operations
- Supports all S3 authentication methods
- Provides health and readiness endpoints

#### Console Server (Port 8081)
- Serves embedded Next.js web interface
- Provides management APIs for web console
- Handles static asset serving
- Implements console-specific authentication

### 2. Middleware Layer

#### Authentication Middleware
- JWT-based authentication
- S3 signature verification (v2 and v4)
- Access key/secret key validation
- Role-based access control

#### Metrics Middleware
- Request counting and timing
- Error rate tracking
- Storage usage metrics
- Performance monitoring

#### CORS Middleware
- Cross-origin request handling
- Configurable CORS policies
- Preflight request support

#### Logging Middleware
- Structured logging with logrus
- Request/response logging
- Error tracking and alerting

### 3. Business Logic Layer

#### Bucket Manager
Responsible for all bucket-level operations:

```go
type Manager interface {
    CreateBucket(ctx context.Context, name string) error
    DeleteBucket(ctx context.Context, name string) error
    ListBuckets(ctx context.Context) ([]Bucket, error)
    BucketExists(ctx context.Context, name string) (bool, error)

    // Policy management
    GetBucketPolicy(ctx context.Context, name string) (*Policy, error)
    SetBucketPolicy(ctx context.Context, name string, policy *Policy) error

    // Versioning
    GetVersioning(ctx context.Context, name string) (*VersioningConfig, error)
    SetVersioning(ctx context.Context, name string, config *VersioningConfig) error

    // Lifecycle
    GetLifecycle(ctx context.Context, name string) (*LifecycleConfig, error)
    SetLifecycle(ctx context.Context, name string, config *LifecycleConfig) error

    IsReady() bool
}
```

#### Object Manager
Handles all object-level operations:

```go
type Manager interface {
    GetObject(ctx context.Context, bucket, key string) (*Object, io.ReadCloser, error)
    PutObject(ctx context.Context, bucket, key string, data io.Reader, headers http.Header) (*Object, error)
    DeleteObject(ctx context.Context, bucket, key string) error
    ListObjects(ctx context.Context, bucket, prefix, delimiter, marker string, maxKeys int) ([]Object, bool, error)

    // Metadata operations
    GetObjectMetadata(ctx context.Context, bucket, key string) (*Object, error)
    UpdateObjectMetadata(ctx context.Context, bucket, key string, metadata map[string]string) error

    // Object Lock
    GetObjectRetention(ctx context.Context, bucket, key string) (*RetentionConfig, error)
    SetObjectRetention(ctx context.Context, bucket, key string, config *RetentionConfig) error
    GetObjectLegalHold(ctx context.Context, bucket, key string) (*LegalHoldConfig, error)
    SetObjectLegalHold(ctx context.Context, bucket, key string, config *LegalHoldConfig) error

    // Multipart uploads
    CreateMultipartUpload(ctx context.Context, bucket, key string) (*MultipartUpload, error)
    UploadPart(ctx context.Context, uploadID string, partNumber int, data io.Reader) (*Part, error)
    CompleteMultipartUpload(ctx context.Context, uploadID string, parts []Part) (*Object, error)
    AbortMultipartUpload(ctx context.Context, uploadID string) error

    IsReady() bool
}
```

### 4. Storage Layer

#### Storage Backend Interface
Abstraction layer for different storage backends:

```go
type Backend interface {
    // Basic operations
    Put(ctx context.Context, path string, data io.Reader, metadata map[string]string) error
    Get(ctx context.Context, path string) (io.ReadCloser, map[string]string, error)
    Delete(ctx context.Context, path string) error
    Exists(ctx context.Context, path string) (bool, error)

    // Listing
    List(ctx context.Context, prefix string, recursive bool) ([]ObjectInfo, error)

    // Metadata
    GetMetadata(ctx context.Context, path string) (map[string]string, error)
    SetMetadata(ctx context.Context, path string, metadata map[string]string) error

    // Lifecycle
    Close() error
}
```

#### Supported Backends
1. **Filesystem Backend**: Local filesystem storage
2. **S3 Backend**: Use another S3-compatible service as backend
3. **GCS Backend**: Google Cloud Storage backend
4. **Azure Backend**: Azure Blob Storage backend

### 5. Data Layer

#### Metadata Database
- Stores bucket configurations
- Object metadata and indexing
- User and access control data
- Audit logs and metrics

#### Object Storage
- Actual object data storage
- Version management
- Temporary multipart data
- Compressed and encrypted data

## Security Architecture

### Authentication & Authorization

```
┌─────────────────────────────────────────────────────────────┐
│                    Request Flow                             │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                 Authentication                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐        │
│  │     JWT     │  │  S3 Sig v4  │  │  Access Key │        │
│  │ Validation  │  │ Validation  │  │ Validation  │        │
│  └─────────────┘  └─────────────┘  └─────────────┘        │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                  Authorization                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐        │
│  │    RBAC     │  │   Policies  │  │   ACLs      │        │
│  │  Checking   │  │  Evaluation │  │  Checking   │        │
│  └─────────────┘  └─────────────┘  └─────────────┘        │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
                        Business Logic
```

### Object Lock Implementation

MaxIOFS implements S3-compatible Object Lock with:

1. **Retention Modes**:
   - GOVERNANCE: Can be bypassed with special permissions
   - COMPLIANCE: Cannot be bypassed by any user

2. **Legal Hold**: Independent object protection mechanism

3. **Default Bucket Configuration**: Apply retention to all new objects

## Performance Optimizations

### 1. Concurrent Processing
- Goroutine pools for request handling
- Parallel multipart upload processing
- Background cleanup and maintenance tasks

### 2. Caching Strategy
- Metadata caching in memory
- Object data caching for frequently accessed items
- Response caching for immutable data

### 3. Connection Pooling
- HTTP client connection reuse
- Database connection pooling
- Storage backend connection management

### 4. Data Compression
- Automatic compression for compatible objects
- Configurable compression algorithms (gzip, lz4, zstd)
- Transparent decompression on retrieval

### 5. Data Encryption
- At-rest encryption with configurable keys
- In-transit encryption with TLS
- Client-side encryption support

## Monitoring and Observability

### Metrics Collection
- Prometheus-compatible metrics endpoint
- Custom metrics for business logic
- System resource monitoring

### Logging
- Structured JSON logging
- Configurable log levels
- Request tracing and correlation IDs

### Health Checks
- Liveness and readiness probes
- Component health status
- Dependency health checking

## Deployment Architecture

### Single Node Deployment
```
┌─────────────────────────────────────┐
│           MaxIOFS Binary            │
│                                     │
│  ┌─────────────────────────────┐    │
│  │        API Server           │    │
│  │      (Port 8080)            │    │
│  └─────────────────────────────┘    │
│                                     │
│  ┌─────────────────────────────┐    │
│  │      Console Server         │    │
│  │      (Port 8081)            │    │
│  └─────────────────────────────┘    │
│                                     │
│  ┌─────────────────────────────┐    │
│  │      Local Storage          │    │
│  │      (/data/objects)        │    │
│  └─────────────────────────────┘    │
└─────────────────────────────────────┘
```

### Docker Deployment
```
┌─────────────────────────────────────┐
│         Docker Container            │
│                                     │
│  ┌─────────────────────────────┐    │
│  │        MaxIOFS              │    │
│  │                             │    │
│  │  Ports: 8080, 8081          │    │
│  │  Volumes: /data             │    │
│  └─────────────────────────────┘    │
└─────────────────────────────────────┘
```

### Kubernetes Deployment
```
┌─────────────────────────────────────┐
│           Kubernetes                │
│                                     │
│  ┌─────────────────────────────┐    │
│  │        MaxIOFS Pod          │    │
│  │                             │    │
│  │  ┌─────────────────────┐    │    │
│  │  │    MaxIOFS          │    │    │
│  │  │    Container        │    │    │
│  │  └─────────────────────┘    │    │
│  │                             │    │
│  │  ┌─────────────────────┐    │    │
│  │  │  Persistent Volume  │    │    │
│  │  │     (/data)         │    │    │
│  │  └─────────────────────┘    │    │
│  └─────────────────────────────┘    │
│                                     │
│  ┌─────────────────────────────┐    │
│  │        Service              │    │
│  │  API: 8080                  │    │
│  │  Console: 8081              │    │
│  └─────────────────────────────┘    │
└─────────────────────────────────────┘
```

## Configuration Management

### Configuration Sources (Priority Order)
1. Command-line flags
2. Environment variables (MAXIOFS_*)
3. Configuration files (YAML/JSON)
4. Default values

### Configuration Structure
```yaml
server:
  listen: ":8080"
  console_listen: ":8081"
  data_dir: "./data"
  log_level: "info"

tls:
  enabled: false
  cert_file: ""
  key_file: ""

storage:
  backend: "filesystem"
  root: "./data/objects"
  compression:
    enabled: false
    type: "gzip"
    level: 6
  encryption:
    enabled: false
    key: ""
  object_lock:
    enabled: true

auth:
  enabled: true
  jwt_secret: ""
  access_key: "maxioadmin"
  secret_key: "maxioadmin"
  users_file: ""

metrics:
  enabled: true
  path: "/metrics"
  interval: 60
```

## Future Enhancements

### Planned Features
1. **Distributed Mode**: Multi-node clustering support
2. **Replication**: Cross-zone and cross-region replication
3. **Advanced Analytics**: Usage analytics and reporting
4. **Plugin System**: Custom storage backends and middleware
5. **Advanced Security**: LDAP/AD integration, advanced RBAC

### Scalability Roadmap
1. **Phase 1**: Single-node optimization (Current)
2. **Phase 2**: Multi-node clustering
3. **Phase 3**: Distributed consensus and data distribution
4. **Phase 4**: Global scale with edge caching