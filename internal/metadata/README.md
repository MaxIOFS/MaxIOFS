# Metadata Store - BadgerDB Implementation

## Overview

This package provides a high-performance metadata storage layer for MaxIOFS using BadgerDB. It replaces the filesystem-based `.metadata` files with a structured, indexed database for efficient object and bucket metadata management.

## Architecture

### Key Components

1. **Store Interface** (`store.go`): Defines the contract for metadata operations
2. **BadgerStore** (`badger.go`, `badger_objects.go`, `badger_multipart.go`): BadgerDB implementation
3. **Types** (`types.go`): All metadata structures (Bucket, Object, Multipart, etc.)

### Key Naming Scheme

BadgerDB uses a structured key naming scheme for efficient lookups:

```
# Buckets
bucket:{tenantID}:{name} → BucketMetadata

# Objects
obj:{bucket}:{key} → ObjectMetadata

# Object Versions
version:{bucket}:{key}:{versionID} → ObjectVersion

# Multipart Uploads
multipart:{uploadID} → MultipartUploadMetadata
multipart_idx:{bucket}:{uploadID} → "" (index for listing)

# Parts
part:{uploadID}:{partNumber} → PartMetadata

# Tag Indices
tag_idx:{bucket}:{tagKey}:{tagValue}:{objectKey} → "" (secondary index)
```

## Features

### Bucket Operations
- ✅ Create/Get/Update/Delete buckets
- ✅ List buckets by tenant
- ✅ Atomic bucket metrics updates (object count, total size)
- ✅ Bucket statistics recalculation

### Object Operations
- ✅ Put/Get/Delete object metadata
- ✅ List objects with prefix filtering and pagination
- ✅ Object existence checks
- ✅ Custom metadata support

### Object Versioning
- ✅ Store and retrieve object versions
- ✅ List all versions of an object
- ✅ Delete specific versions

### Object Tagging
- ✅ Put/Get/Delete object tags
- ✅ Secondary index for tag-based searches
- ✅ List objects by tag criteria

### Multipart Uploads
- ✅ Create/Get/Abort multipart uploads
- ✅ List in-progress uploads by bucket
- ✅ TTL-based automatic cleanup (7 days)
- ✅ Put/Get/List parts
- ✅ Complete multipart upload

### Advanced Features
- ✅ **Compression**: Automatic value compression to save disk space
- ✅ **Caching**: Index cache (100MB) and block cache (256MB) for performance
- ✅ **Garbage Collection**: Automatic background GC every 5 minutes
- ✅ **Backup**: Full database backup support
- ✅ **Compaction**: Manual compaction API
- ✅ **Logging**: Integrated with logrus

## Usage

### Creating a Store

```go
import "github.com/maxiofs/maxiofs/internal/metadata"

store, err := metadata.NewBadgerStore(metadata.BadgerOptions{
    DataDir:           "/path/to/data",
    SyncWrites:        false,  // true for durability, false for performance
    CompactionEnabled: true,   // Enable automatic GC
    Logger:            logger,
})
if err != nil {
    log.Fatal(err)
}
defer store.Close()
```

### Bucket Operations

```go
// Create bucket
bucket := &metadata.BucketMetadata{
    Name:      "my-bucket",
    TenantID:  "tenant-123",
    OwnerID:   "user-456",
    OwnerType: "user",
    Region:    "us-east-1",
}
err := store.CreateBucket(ctx, bucket)

// Get bucket
bucket, err := store.GetBucket(ctx, "tenant-123", "my-bucket")

// Update metrics atomically
err = store.UpdateBucketMetrics(ctx, "tenant-123", "my-bucket", +1, 1024) // +1 object, +1024 bytes
```

### Object Operations

```go
// Put object
obj := &metadata.ObjectMetadata{
    Bucket:      "my-bucket",
    Key:         "path/to/file.txt",
    Size:        1024,
    ETag:        "abc123",
    ContentType: "text/plain",
    Metadata: map[string]string{
        "user-defined": "value",
    },
    Tags: map[string]string{
        "Environment": "Production",
    },
}
err := store.PutObject(ctx, obj)

// List objects with prefix
objects, nextMarker, err := store.ListObjects(ctx, "my-bucket", "path/", "", 1000)

// Search by tags
objects, err := store.ListObjectsByTags(ctx, "my-bucket", map[string]string{
    "Environment": "Production",
})
```

### Multipart Uploads

```go
// Initiate upload
upload := &metadata.MultipartUploadMetadata{
    UploadID:    generateUploadID(),
    Bucket:      "my-bucket",
    Key:         "large-file.bin",
    ContentType: "application/octet-stream",
    OwnerID:     "user-456",
}
err := store.CreateMultipartUpload(ctx, upload)

// Upload parts
part := &metadata.PartMetadata{
    UploadID:   upload.UploadID,
    PartNumber: 1,
    Size:       5242880, // 5MB
    ETag:       "part1-etag",
}
err = store.PutPart(ctx, part)

// Complete upload
finalObject := &metadata.ObjectMetadata{
    Bucket: "my-bucket",
    Key:    "large-file.bin",
    Size:   totalSize,
    ETag:   finalETag,
}
err = store.CompleteMultipartUpload(ctx, upload.UploadID, finalObject)
```

## Performance Characteristics

### Read Performance
- **GetObject**: ~0.1-0.5ms (in-memory cache)
- **ListObjects**: O(log n) with prefix index
- **Tag Search**: O(log n) with secondary index

### Write Performance
- **PutObject**: ~1-5ms (with SyncWrites=false)
- **PutObject**: ~5-20ms (with SyncWrites=true)

### Storage Efficiency
- **Compression**: ~40-60% space savings
- **Index Size**: ~1-2% of data size

## Configuration Best Practices

### For Production
```go
BadgerOptions{
    DataDir:           "/data/metadata",
    SyncWrites:        true,   // Durability over speed
    CompactionEnabled: true,   // Keep DB optimized
    Logger:            logger,
}
```

### For Development/Testing
```go
BadgerOptions{
    DataDir:           "/tmp/metadata",
    SyncWrites:        false,  // Speed over durability
    CompactionEnabled: false,  // Manual control
    Logger:            logger,
}
```

## Testing

Run all tests:
```bash
go test -v ./internal/metadata/...
```

Run specific test:
```bash
go test -v ./internal/metadata/... -run TestBucketOperations
```

## Migration from Filesystem

See `docs/MIGRATION_GUIDE.md` (Sprint 4) for migrating from `.metadata` files to BadgerDB.

## Limitations & Future Work

### Current Limitations
- Tag search only supports AND operation (all tags must match)
- No cross-bucket queries
- No full-text search capabilities

### Future Enhancements (Sprint 2+)
- Integration with BucketManager and ObjectManager
- Migration tool from filesystem metadata
- Metrics dashboard for metadata operations
- Advanced query capabilities

## Dependencies

- [BadgerDB v4](https://github.com/dgraph-io/badger/v4): Embedded key-value database
- [Logrus](https://github.com/sirupsen/logrus): Logging framework

## License

See project root LICENSE file.
