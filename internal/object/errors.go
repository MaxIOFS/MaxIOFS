package object

import "errors"

// Common object errors
var (
	ErrObjectNotFound      = errors.New("object not found")
	ErrObjectExists        = errors.New("object already exists")
	ErrBucketNotFound      = errors.New("bucket not found")
	ErrInvalidObjectName   = errors.New("invalid object name")
	ErrObjectLocked        = errors.New("object is locked")
	ErrRetentionPeriod     = errors.New("object is in retention period")
	ErrInvalidPath         = errors.New("invalid object path")
	ErrInvalidRange        = errors.New("invalid range")
	ErrPreconditionFailed  = errors.New("precondition failed")
	ErrNotModified         = errors.New("not modified")
	ErrInvalidUploadID     = errors.New("invalid upload ID")
	ErrPartNotFound        = errors.New("part not found")
	ErrInvalidPartOrder    = errors.New("invalid part order")
	ErrTooManyParts        = errors.New("too many parts")
	ErrPartTooSmall        = errors.New("part too small")
	ErrEntityTooLarge      = errors.New("entity too large")
	ErrMalformedXML        = errors.New("malformed XML")
	ErrInvalidTag          = errors.New("invalid tag")
	ErrTooManyTags         = errors.New("too many tags")
	ErrAccessDenied        = errors.New("access denied")
)