package metadata

import "time"

// ObjectMetadata represents metadata for a stored object
type ObjectMetadata struct {
	// Basic properties
	Bucket       string    `json:"bucket"`
	Key          string    `json:"key"`
	VersionID    string    `json:"version_id,omitempty"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	ETag         string    `json:"etag"`
	ContentType  string    `json:"content_type"`
	StorageClass string    `json:"storage_class,omitempty"`

	// Custom metadata (user-defined headers)
	Metadata map[string]string `json:"metadata,omitempty"`

	// Tags
	Tags map[string]string `json:"tags,omitempty"`

	// Object Lock
	Retention *RetentionMetadata `json:"retention,omitempty"`
	LegalHold bool               `json:"legal_hold,omitempty"`

	// ACL
	ACL *ACLMetadata `json:"acl,omitempty"`

	// Encryption
	SSEAlgorithm string `json:"sse_algorithm,omitempty"`
	SSEKeyID     string `json:"sse_key_id,omitempty"`

	// Multipart upload tracking
	UploadID string `json:"upload_id,omitempty"`

	// Internal tracking
	TenantID  string    `json:"tenant_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BucketMetadata represents metadata for a bucket
type BucketMetadata struct {
	// Basic properties
	Name      string    `json:"name"`
	TenantID  string    `json:"tenant_id,omitempty"`
	OwnerID   string    `json:"owner_id"`
	OwnerType string    `json:"owner_type"` // "user" or "tenant"
	Region    string    `json:"region,omitempty"`
	IsPublic  bool      `json:"is_public"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Configuration
	Versioning        *VersioningMetadata        `json:"versioning,omitempty"`
	ObjectLock        *ObjectLockMetadata        `json:"object_lock,omitempty"`
	Policy            *PolicyMetadata            `json:"policy,omitempty"`
	Lifecycle         *LifecycleMetadata         `json:"lifecycle,omitempty"`
	CORS              *CORSMetadata              `json:"cors,omitempty"`
	Encryption        *EncryptionMetadata        `json:"encryption,omitempty"`
	PublicAccessBlock *PublicAccessBlockMetadata `json:"public_access_block,omitempty"`

	// Tags and custom metadata
	Tags     map[string]string `json:"tags,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`

	// Cached metrics (updated incrementally for performance)
	ObjectCount int64 `json:"object_count"`
	TotalSize   int64 `json:"total_size"`
}

// VersioningMetadata represents bucket versioning configuration
type VersioningMetadata struct {
	Enabled   bool   `json:"enabled"`
	Status    string `json:"status"` // "Enabled", "Suspended"
	MFADelete bool   `json:"mfa_delete,omitempty"`
}

// ObjectLockMetadata represents bucket object lock configuration
type ObjectLockMetadata struct {
	Enabled bool                    `json:"enabled"`
	Rule    *ObjectLockRuleMetadata `json:"rule,omitempty"`
}

// ObjectLockRuleMetadata represents the default retention rule
type ObjectLockRuleMetadata struct {
	DefaultRetention *RetentionMetadata `json:"default_retention,omitempty"`
}

// RetentionMetadata represents object retention configuration
// For bucket default retention: Uses Days/Years (one of them, not both)
// For object retention: Uses RetainUntilDate
type RetentionMetadata struct {
	Mode            string     `json:"mode"` // "GOVERNANCE" or "COMPLIANCE"
	RetainUntilDate time.Time  `json:"retain_until_date,omitempty"`
	Days            *int       `json:"days,omitempty"`  // For bucket default retention
	Years           *int       `json:"years,omitempty"` // For bucket default retention
}

// PolicyMetadata represents bucket policy
type PolicyMetadata struct {
	Version   string            `json:"version"`
	Statement []PolicyStatement `json:"statement"`
}

// PolicyStatement represents a single policy statement
type PolicyStatement struct {
	Sid       string                 `json:"sid,omitempty"`
	Effect    string                 `json:"effect"` // "Allow" or "Deny"
	Principal interface{}            `json:"principal,omitempty"` // Can be string "*" or map[string]interface{}
	Action    interface{}            `json:"action"`              // Can be string or []string
	Resource  interface{}            `json:"resource"`            // Can be string or []string
	Condition map[string]interface{} `json:"condition,omitempty"`
}

// LifecycleMetadata represents bucket lifecycle configuration
type LifecycleMetadata struct {
	Rules []LifecycleRule `json:"rules"`
}

// LifecycleRule represents a single lifecycle rule
type LifecycleRule struct {
	ID                             string                  `json:"id"`
	Status                         string                  `json:"status"` // "Enabled" or "Disabled"
	Filter                         *LifecycleFilter        `json:"filter,omitempty"`
	Expiration                     *LifecycleExpiration    `json:"expiration,omitempty"`
	Transitions                    []LifecycleTransition   `json:"transitions,omitempty"`
	NoncurrentVersionExpiration    *NoncurrentExpiration   `json:"noncurrent_version_expiration,omitempty"`
	NoncurrentVersionTransitions   []NoncurrentTransition  `json:"noncurrent_version_transitions,omitempty"`
	AbortIncompleteMultipartUpload *AbortMultipartMetadata `json:"abort_incomplete_multipart_upload,omitempty"`
}

// LifecycleFilter represents lifecycle rule filter
type LifecycleFilter struct {
	Prefix string            `json:"prefix,omitempty"`
	Tags   map[string]string `json:"tags,omitempty"`
	And    *LifecycleAnd     `json:"and,omitempty"`
}

// LifecycleAnd combines multiple filter criteria
type LifecycleAnd struct {
	Prefix string            `json:"prefix,omitempty"`
	Tags   map[string]string `json:"tags,omitempty"`
}

// LifecycleExpiration represents object expiration
type LifecycleExpiration struct {
	Days                      int    `json:"days,omitempty"`
	Date                      string `json:"date,omitempty"`
	ExpiredObjectDeleteMarker bool   `json:"expired_object_delete_marker,omitempty"`
}

// LifecycleTransition represents storage class transition
type LifecycleTransition struct {
	Days         int    `json:"days,omitempty"`
	Date         string `json:"date,omitempty"`
	StorageClass string `json:"storage_class"`
}

// NoncurrentExpiration represents noncurrent version expiration
type NoncurrentExpiration struct {
	NoncurrentDays int `json:"noncurrent_days"`
}

// NoncurrentTransition represents noncurrent version transition
type NoncurrentTransition struct {
	NoncurrentDays int    `json:"noncurrent_days"`
	StorageClass   string `json:"storage_class"`
}

// AbortMultipartMetadata represents abort incomplete multipart upload configuration
type AbortMultipartMetadata struct {
	DaysAfterInitiation int `json:"days_after_initiation"`
}

// CORSMetadata represents bucket CORS configuration
type CORSMetadata struct {
	Rules []CORSRule `json:"rules"`
}

// CORSRule represents a single CORS rule
type CORSRule struct {
	ID             string   `json:"id,omitempty"`
	AllowedOrigins []string `json:"allowed_origins"`
	AllowedMethods []string `json:"allowed_methods"`
	AllowedHeaders []string `json:"allowed_headers,omitempty"`
	ExposeHeaders  []string `json:"expose_headers,omitempty"`
	MaxAgeSeconds  int      `json:"max_age_seconds,omitempty"`
}

// EncryptionMetadata represents bucket encryption configuration
type EncryptionMetadata struct {
	Rules []EncryptionRule `json:"rules"`
}

// EncryptionRule represents encryption rule
type EncryptionRule struct {
	ApplyServerSideEncryptionByDefault *SSEConfig `json:"apply_server_side_encryption_by_default,omitempty"`
	BucketKeyEnabled                   bool       `json:"bucket_key_enabled,omitempty"`
}

// SSEConfig represents server-side encryption configuration
type SSEConfig struct {
	SSEAlgorithm   string `json:"sse_algorithm"` // "AES256" or "aws:kms"
	KMSMasterKeyID string `json:"kms_master_key_id,omitempty"`
}

// PublicAccessBlockMetadata represents public access block configuration
type PublicAccessBlockMetadata struct {
	BlockPublicAcls       bool `json:"block_public_acls"`
	IgnorePublicAcls      bool `json:"ignore_public_acls"`
	BlockPublicPolicy     bool `json:"block_public_policy"`
	RestrictPublicBuckets bool `json:"restrict_public_buckets"`
}

// ACLMetadata represents object/bucket ACL
type ACLMetadata struct {
	Owner  *Owner  `json:"owner,omitempty"`
	Grants []Grant `json:"grants,omitempty"`
}

// Owner represents the owner of an object/bucket
type Owner struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
}

// Grant represents a single ACL grant
type Grant struct {
	Grantee    *Grantee `json:"grantee"`
	Permission string   `json:"permission"` // "READ", "WRITE", "READ_ACP", "WRITE_ACP", "FULL_CONTROL"
}

// Grantee represents the entity receiving permissions
type Grantee struct {
	Type         string `json:"type"` // "CanonicalUser", "Group", "AmazonCustomerByEmail"
	ID           string `json:"id,omitempty"`
	DisplayName  string `json:"display_name,omitempty"`
	EmailAddress string `json:"email_address,omitempty"`
	URI          string `json:"uri,omitempty"`
}

// MultipartUploadMetadata represents metadata for a multipart upload
type MultipartUploadMetadata struct {
	UploadID     string            `json:"upload_id"`
	Bucket       string            `json:"bucket"`
	Key          string            `json:"key"`
	Initiated    time.Time         `json:"initiated"`
	StorageClass string            `json:"storage_class,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	ContentType  string            `json:"content_type,omitempty"`
	TenantID     string            `json:"tenant_id,omitempty"`
	OwnerID      string            `json:"owner_id"`
}

// PartMetadata represents metadata for a multipart upload part
type PartMetadata struct {
	UploadID     string    `json:"upload_id"`
	PartNumber   int       `json:"part_number"`
	Size         int64     `json:"size"`
	ETag         string    `json:"etag"`
	LastModified time.Time `json:"last_modified"`
}

// ObjectVersion represents a version of an object
type ObjectVersion struct {
	VersionID    string    `json:"version_id"`
	IsLatest     bool      `json:"is_latest"`
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	ETag         string    `json:"etag"`
	LastModified time.Time `json:"last_modified"`
	StorageClass string    `json:"storage_class,omitempty"`
	OwnerID      string    `json:"owner_id,omitempty"`
}
