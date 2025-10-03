package object

import (
	"errors"
	"fmt"
	"time"
)

// Common object errors
var (
	ErrObjectNotFound     = errors.New("object not found")
	ErrObjectExists       = errors.New("object already exists")
	ErrBucketNotFound     = errors.New("bucket not found")
	ErrInvalidObjectName  = errors.New("invalid object name")
	ErrObjectLocked       = errors.New("object is locked")
	ErrRetentionPeriod    = errors.New("object is in retention period")
	ErrInvalidPath        = errors.New("invalid object path")
	ErrInvalidRange       = errors.New("invalid range")
	ErrPreconditionFailed = errors.New("precondition failed")
	ErrNotModified        = errors.New("not modified")
	ErrInvalidUploadID    = errors.New("invalid upload ID")
	ErrUploadNotFound     = errors.New("multipart upload not found")
	ErrPartNotFound       = errors.New("part not found")
	ErrInvalidPart        = errors.New("invalid part")
	ErrInvalidPartOrder   = errors.New("invalid part order")
	ErrTooManyParts       = errors.New("too many parts")
	ErrPartTooSmall       = errors.New("part too small")
	ErrEntityTooLarge     = errors.New("entity too large")
	ErrMalformedXML       = errors.New("malformed XML")
	ErrInvalidTag         = errors.New("invalid tag")
	ErrTooManyTags        = errors.New("too many tags")
	ErrAccessDenied       = errors.New("access denied")

	// Object Lock errors (simple)
	ErrObjectUnderLegalHold     = errors.New("object is under legal hold")
	ErrNoRetentionConfiguration = errors.New("no retention configuration found")
	ErrRetentionLocked          = errors.New("object retention is locked")
	ErrCannotShortenCompliance  = errors.New("cannot shorten compliance mode retention")
	ErrCannotShortenGovernance  = errors.New("cannot shorten governance mode retention without bypass permission")
	ErrInsufficientPermissions  = errors.New("insufficient permissions for object lock operation")
	ErrInvalidRetentionMode     = errors.New("invalid retention mode")
	ErrInvalidLegalHoldStatus   = errors.New("invalid legal hold status")
	ErrRetentionDateInPast      = errors.New("retention date cannot be in the past")
)

// RetentionError represents an error when trying to delete an object under retention
type RetentionError struct {
	Mode            string
	RetainUntilDate time.Time
}

func (e *RetentionError) Error() string {
	return fmt.Sprintf("Cannot delete object: protected by %s mode Object Lock retention until %s",
		e.Mode, e.RetainUntilDate.Format("2006-01-02 15:04:05 MST"))
}

// NewComplianceRetentionError creates a retention error for COMPLIANCE mode
func NewComplianceRetentionError(retainUntil time.Time) error {
	return &RetentionError{
		Mode:            "COMPLIANCE",
		RetainUntilDate: retainUntil,
	}
}

// NewGovernanceRetentionError creates a retention error for GOVERNANCE mode
func NewGovernanceRetentionError(retainUntil time.Time) error {
	return &RetentionError{
		Mode:            "GOVERNANCE",
		RetainUntilDate: retainUntil,
	}
}
