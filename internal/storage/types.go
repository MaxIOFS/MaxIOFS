package storage

import "github.com/maxiofs/maxiofs/internal/config"

// Config alias for storage configuration
type Config = config.StorageConfig

// Common storage errors
var (
	ErrObjectNotFound    = NewError("ObjectNotFound", "The specified object does not exist")
	ErrObjectExists      = NewError("ObjectExists", "The specified object already exists")
	ErrInvalidPath       = NewError("InvalidPath", "The specified path is invalid")
	ErrPermissionDenied  = NewError("PermissionDenied", "Permission denied")
	ErrStorageNotReady   = NewError("StorageNotReady", "Storage backend is not ready")
)

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