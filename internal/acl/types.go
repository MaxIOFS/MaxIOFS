package acl

import "errors"

// Common ACL errors
var (
	ErrACLNotFound     = errors.New("acl not found")
	ErrInvalidACL      = errors.New("invalid acl")
	ErrInvalidGrantee  = errors.New("invalid grantee")
	ErrInvalidCannedACL = errors.New("invalid canned acl")
)

// ACL represents an Access Control List for a bucket or object
type ACL struct {
	Owner     Owner   `json:"owner"`
	Grants    []Grant `json:"grants"`
	CannedACL string  `json:"canned_acl,omitempty"` // For tracking which canned ACL was used
}

// Owner represents the owner of a resource
type Owner struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// Grant represents a single permission grant
type Grant struct {
	Grantee    Grantee    `json:"grantee"`
	Permission Permission `json:"permission"`
}

// Grantee represents the recipient of a permission grant
type Grantee struct {
	Type         GranteeType `json:"type"`
	ID           string      `json:"id,omitempty"`            // For CanonicalUser
	DisplayName  string      `json:"display_name,omitempty"`  // For CanonicalUser
	EmailAddress string      `json:"email_address,omitempty"` // For AmazonCustomerByEmail
	URI          string      `json:"uri,omitempty"`           // For Group
}

// Permission represents a type of access permission
type Permission string

const (
	PermissionRead        Permission = "READ"
	PermissionWrite       Permission = "WRITE"
	PermissionReadACP     Permission = "READ_ACP"  // Read ACL/Policy
	PermissionWriteACP    Permission = "WRITE_ACP" // Write ACL/Policy
	PermissionFullControl Permission = "FULL_CONTROL"
)

// GranteeType represents the type of grantee
type GranteeType string

const (
	GranteeTypeCanonicalUser     GranteeType = "CanonicalUser"
	GranteeTypeAmazonCustomer    GranteeType = "AmazonCustomerByEmail"
	GranteeTypeGroup             GranteeType = "Group"
)

// Canned ACL constants
const (
	CannedACLPrivate                = "private"
	CannedACLPublicRead             = "public-read"
	CannedACLPublicReadWrite        = "public-read-write"
	CannedACLAuthenticatedRead      = "authenticated-read"
	CannedACLBucketOwnerRead        = "bucket-owner-read"
	CannedACLBucketOwnerFullControl = "bucket-owner-full-control"
	CannedACLLogDeliveryWrite       = "log-delivery-write"
)

// Well-known S3 groups
const (
	GroupAllUsers           = "http://acs.amazonaws.com/groups/global/AllUsers"
	GroupAuthenticatedUsers = "http://acs.amazonaws.com/groups/global/AuthenticatedUsers"
	GroupLogDelivery        = "http://acs.amazonaws.com/groups/s3/LogDelivery"
)

// IsValidCannedACL checks if a canned ACL string is valid
func IsValidCannedACL(cannedACL string) bool {
	switch cannedACL {
	case CannedACLPrivate,
		CannedACLPublicRead,
		CannedACLPublicReadWrite,
		CannedACLAuthenticatedRead,
		CannedACLBucketOwnerRead,
		CannedACLBucketOwnerFullControl,
		CannedACLLogDeliveryWrite:
		return true
	}
	return false
}

// IsValidPermission checks if a permission string is valid
func IsValidPermission(perm Permission) bool {
	switch perm {
	case PermissionRead,
		PermissionWrite,
		PermissionReadACP,
		PermissionWriteACP,
		PermissionFullControl:
		return true
	}
	return false
}
