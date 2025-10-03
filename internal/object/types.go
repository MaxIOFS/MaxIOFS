package object

import (
	"time"
)

// ObjectVersion represents a versioned object
type ObjectVersion struct {
	Object
	IsLatest       bool      `json:"is_latest"`
	IsDeleteMarker bool      `json:"is_delete_marker"`
	DeletedAt      time.Time `json:"deleted_at,omitempty"`
}

// MultipartUpload represents a multipart upload session
type MultipartUpload struct {
	UploadID     string            `json:"upload_id"`
	Bucket       string            `json:"bucket"`
	Key          string            `json:"key"`
	Initiated    time.Time         `json:"initiated"`
	StorageClass string            `json:"storage_class"`
	Metadata     map[string]string `json:"metadata"`
	Parts        []Part            `json:"parts,omitempty"`
}

// Part represents a part of a multipart upload
type Part struct {
	PartNumber   int       `json:"part_number"`
	ETag         string    `json:"etag"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
}

// RetentionConfig represents object retention configuration for Object Lock
type RetentionConfig struct {
	Mode            string    `json:"mode"` // GOVERNANCE, COMPLIANCE
	RetainUntilDate time.Time `json:"retainUntilDate"`
}

// LegalHoldConfig represents legal hold configuration for Object Lock
type LegalHoldConfig struct {
	Status string `json:"status"` // ON, OFF
}

// TagSet represents a set of object tags
type TagSet struct {
	Tags []Tag `json:"tags"`
}

// Tag represents a key-value tag
type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ACL represents Access Control List for an object
type ACL struct {
	Owner  Owner   `json:"owner"`
	Grants []Grant `json:"grants"`
}

// Owner represents the owner of an object
type Owner struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// Grant represents a single ACL grant
type Grant struct {
	Grantee    Grantee `json:"grantee"`
	Permission string  `json:"permission"` // FULL_CONTROL, WRITE, WRITE_ACP, READ, READ_ACP
}

// Grantee represents the recipient of an ACL grant
type Grantee struct {
	Type         string `json:"type"` // CanonicalUser, AmazonCustomerByEmail, Group
	ID           string `json:"id,omitempty"`
	DisplayName  string `json:"display_name,omitempty"`
	EmailAddress string `json:"email_address,omitempty"`
	URI          string `json:"uri,omitempty"`
}

// ObjectLockLegalHold represents the legal hold status for Object Lock
type ObjectLockLegalHold struct {
	Status string `json:"Status"` // ON, OFF
}

// ObjectLockRetention represents the retention configuration for Object Lock
type ObjectLockRetention struct {
	Mode            string    `json:"Mode"` // GOVERNANCE, COMPLIANCE
	RetainUntilDate time.Time `json:"RetainUntilDate"`
}

// CopyObjectResult represents the result of a copy operation
type CopyObjectResult struct {
	ETag         string    `json:"etag"`
	LastModified time.Time `json:"last_modified"`
}

// DeleteMarker represents a delete marker for versioned objects
type DeleteMarker struct {
	Key          string    `json:"key"`
	VersionID    string    `json:"version_id"`
	IsLatest     bool      `json:"is_latest"`
	LastModified time.Time `json:"last_modified"`
	Owner        Owner     `json:"owner"`
}

// ListObjectsResult represents the result of listing objects
type ListObjectsResult struct {
	Objects        []Object       `json:"objects"`
	CommonPrefixes []CommonPrefix `json:"common_prefixes"`
	IsTruncated    bool           `json:"is_truncated"`
	NextMarker     string         `json:"next_marker,omitempty"`
	MaxKeys        int            `json:"max_keys"`
	Prefix         string         `json:"prefix"`
	Delimiter      string         `json:"delimiter"`
	Marker         string         `json:"marker"`
}

// CommonPrefix represents a common prefix in object listing
type CommonPrefix struct {
	Prefix string `json:"prefix"`
}

// ListMultipartUploadsResult represents the result of listing multipart uploads
type ListMultipartUploadsResult struct {
	Uploads            []MultipartUpload `json:"uploads"`
	CommonPrefixes     []CommonPrefix    `json:"common_prefixes"`
	IsTruncated        bool              `json:"is_truncated"`
	NextKeyMarker      string            `json:"next_key_marker,omitempty"`
	NextUploadIDMarker string            `json:"next_upload_id_marker,omitempty"`
	MaxUploads         int               `json:"max_uploads"`
	Prefix             string            `json:"prefix"`
	Delimiter          string            `json:"delimiter"`
	KeyMarker          string            `json:"key_marker"`
	UploadIDMarker     string            `json:"upload_id_marker"`
}

// ListPartsResult represents the result of listing parts for a multipart upload
type ListPartsResult struct {
	Parts                []Part `json:"parts"`
	IsTruncated          bool   `json:"is_truncated"`
	NextPartNumberMarker int    `json:"next_part_number_marker,omitempty"`
	MaxParts             int    `json:"max_parts"`
	PartNumberMarker     int    `json:"part_number_marker"`
	StorageClass         string `json:"storage_class"`
	Initiator            Owner  `json:"initiator"`
	Owner                Owner  `json:"owner"`
}

// ObjectAttributes represents object attributes for GetObjectAttributes
type ObjectAttributes struct {
	ETag         string       `json:"etag,omitempty"`
	Checksum     *Checksum    `json:"checksum,omitempty"`
	ObjectParts  *ObjectParts `json:"object_parts,omitempty"`
	StorageClass string       `json:"storage_class,omitempty"`
	ObjectSize   int64        `json:"object_size,omitempty"`
}

// Checksum represents object checksums
type Checksum struct {
	ChecksumCRC32  string `json:"checksum_crc32,omitempty"`
	ChecksumCRC32C string `json:"checksum_crc32c,omitempty"`
	ChecksumSHA1   string `json:"checksum_sha1,omitempty"`
	ChecksumSHA256 string `json:"checksum_sha256,omitempty"`
}

// ObjectParts represents information about object parts
type ObjectParts struct {
	TotalPartsCount      int        `json:"total_parts_count,omitempty"`
	PartNumberMarker     int        `json:"part_number_marker,omitempty"`
	NextPartNumberMarker int        `json:"next_part_number_marker,omitempty"`
	MaxParts             int        `json:"max_parts,omitempty"`
	IsTruncated          bool       `json:"is_truncated,omitempty"`
	Parts                []PartInfo `json:"parts,omitempty"`
}

// PartInfo represents information about a single part
type PartInfo struct {
	PartNumber int       `json:"part_number"`
	Size       int64     `json:"size"`
	Checksum   *Checksum `json:"checksum,omitempty"`
}

// Storage class constants
const (
	StorageClassStandard           = "STANDARD"
	StorageClassReducedRedundancy  = "REDUCED_REDUNDANCY"
	StorageClassStandardIA         = "STANDARD_IA"
	StorageClassOnezoneIA          = "ONEZONE_IA"
	StorageClassIntelligentTiering = "INTELLIGENT_TIERING"
	StorageClassGlacier            = "GLACIER"
	StorageClassDeepArchive        = "DEEP_ARCHIVE"
	StorageClassGlacierIR          = "GLACIER_IR"
)

// Object Lock constants
const (
	ObjectLockModeGovernance = "GOVERNANCE"
	ObjectLockModeCompliance = "COMPLIANCE"

	LegalHoldStatusOn  = "ON"
	LegalHoldStatusOff = "OFF"
)

// ACL permission constants
const (
	PermissionFullControl = "FULL_CONTROL"
	PermissionWrite       = "WRITE"
	PermissionWriteACP    = "WRITE_ACP"
	PermissionRead        = "READ"
	PermissionReadACP     = "READ_ACP"
)

// Grantee type constants
const (
	GranteeTypeCanonicalUser         = "CanonicalUser"
	GranteeTypeAmazonCustomerByEmail = "AmazonCustomerByEmail"
	GranteeTypeGroup                 = "Group"
)

// Pre-defined ACL groups
const (
	GroupAllUsers           = "http://acs.amazonaws.com/groups/global/AllUsers"
	GroupAuthenticatedUsers = "http://acs.amazonaws.com/groups/global/AuthenticatedUsers"
	GroupLogDelivery        = "http://acs.amazonaws.com/groups/s3/LogDelivery"
)
