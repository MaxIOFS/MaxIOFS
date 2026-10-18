package storage

import "github.com/maxiofs/maxiofs/internal/config"

// Config alias for storage configuration
type Config = config.StorageConfig

// Sidecar fields recording which object the file on disk belongs to, so the
// identity survives without the path encoding it.
const (
	MetadataBucketField  = "maxiofs-bucket"
	MetadataKeyField     = "maxiofs-key"
	MetadataVersionField = "maxiofs-version"
	// MetadataShardField is the index of a shard among its write's shards.
	MetadataShardField = "maxiofs-shard"
)

// Sidecar fields keeping an object's Object Lock state, so that a metadata
// store rebuilt from the files gets it back: the retention mode and the date
// it holds until (RFC 3339), and the legal hold when on.
const (
	MetadataLockModeField  = "maxiofs-lock-mode"
	MetadataLockUntilField = "maxiofs-lock-until"
	MetadataLegalHoldField = "maxiofs-legal-hold"
)

// MetadataGeneratedKey marks a metadata map that was derived from the bytes on
const MetadataGeneratedKey = "maxiofs-metadata-generated"

// Common storage errors
var (
	ErrObjectNotFound   = NewError("ObjectNotFound", "The specified object does not exist")
	ErrObjectExists     = NewError("ObjectExists", "The specified object already exists")
	ErrInvalidPath      = NewError("InvalidPath", "The specified path is invalid")
	ErrPermissionDenied = NewError("PermissionDenied", "Permission denied")
	ErrStorageNotReady  = NewError("StorageNotReady", "Storage backend is not ready")
)

// ObjectRef identifies an object independently of how a backend lays it out.
type ObjectRef struct {
	// Bucket is the logical bucket path: "<tenantID>/<name>", or "<name>" when
	// the bucket belongs to no tenant.
	Bucket    string
	Key       string
	VersionID string
	// Shard is 0 for the write's own file and i+1 for its shard i.
	Shard int
}

// ShardOf is the ref of shard i of r's write.
func (r ObjectRef) ShardOf(i int) ObjectRef {
	r.Shard = i + 1
	return r
}

// ShardIndex is the index of the shard r names, when it names one.
func (r ObjectRef) ShardIndex() (int, bool) {
	return r.Shard - 1, r.Shard > 0
}

// Write is the ref of the write itself, of which r may name a shard.
func (r ObjectRef) Write() ObjectRef {
	r.Shard = 0
	return r
}

// StorageError represents a storage-specific error
type StorageError struct {
	Code    string
	Message string
	Cause   error
}

func (e *StorageError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

// NewError creates a new storage error
func NewError(code, message string) *StorageError {
	return &StorageError{
		Code:    code,
		Message: message,
	}
}

// NewErrorWithCause creates a new storage error with underlying cause
func NewErrorWithCause(code, message string, cause error) *StorageError {
	return &StorageError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}
