package bucket

import (
	"errors"
	"time"
)

// Common bucket errors
var (
	ErrBucketNotFound      = errors.New("bucket not found")
	ErrBucketAlreadyExists = errors.New("bucket already exists")
	ErrBucketNotEmpty      = errors.New("bucket not empty")
	ErrInvalidBucketName   = errors.New("invalid bucket name")
	ErrPolicyNotFound      = errors.New("policy not found")
	ErrLifecycleNotFound   = errors.New("lifecycle configuration not found")
	ErrCORSNotFound        = errors.New("CORS configuration not found")
	ErrWebsiteNotFound            = errors.New("website configuration not found")
	ErrEncryptionNotFound         = errors.New("server-side encryption configuration not found")
	ErrPublicAccessBlockNotFound  = errors.New("public access block configuration not found")
	ErrOwnershipControlsNotFound  = errors.New("ownership controls not found")
	ErrLoggingNotFound            = errors.New("logging configuration not found")
)

// WebsiteConfig represents static website hosting configuration for a bucket.
// IndexDocument is the suffix appended to directory requests (e.g. "index.html").
// ErrorDocument is the object key returned on 4xx errors (e.g. "error.html").
type WebsiteConfig struct {
	IndexDocument string               `json:"index_document"`
	ErrorDocument string               `json:"error_document,omitempty"`
	RoutingRules  []WebsiteRoutingRule `json:"routing_rules,omitempty"`
}

// WebsiteRoutingRule represents a single URL rewrite/redirect rule.
type WebsiteRoutingRule struct {
	Condition WebsiteRoutingCondition `json:"condition,omitempty"`
	Redirect  WebsiteRoutingRedirect  `json:"redirect"`
}

// WebsiteRoutingCondition specifies when a routing rule is applied.
type WebsiteRoutingCondition struct {
	HTTPErrorCodeReturnedEquals string `json:"http_error_code,omitempty"`
	KeyPrefixEquals             string `json:"key_prefix_equals,omitempty"`
}

// WebsiteRoutingRedirect describes the redirect to perform.
type WebsiteRoutingRedirect struct {
	HostName             string `json:"host_name,omitempty"`
	HTTPRedirectCode     string `json:"http_redirect_code,omitempty"`
	Protocol             string `json:"protocol,omitempty"`
	ReplaceKeyPrefixWith string `json:"replace_key_prefix_with,omitempty"`
	ReplaceKeyWith       string `json:"replace_key_with,omitempty"`
}

// Policy represents a bucket policy
type Policy struct {
	Version   string      `json:"Version"`
	Statement []Statement `json:"Statement"`
}

// Statement represents a policy statement
type Statement struct {
	Sid       string                 `json:"Sid,omitempty"`
	Effect    string                 `json:"Effect"`
	Principal interface{}            `json:"Principal,omitempty"` // Can be string "*" or map[string]interface{}
	Action    interface{}            `json:"Action"`              // Can be string or []string
	Resource  interface{}            `json:"Resource"`            // Can be string or []string
	Condition map[string]interface{} `json:"Condition,omitempty"`
}

// VersioningConfig represents bucket versioning configuration
type VersioningConfig struct {
	Status string `json:"Status"` // Enabled, Suspended
}

// LifecycleConfig represents bucket lifecycle configuration
type LifecycleConfig struct {
	Rules []LifecycleRule `json:"Rules"`
}

// LifecycleRule represents a single lifecycle rule
type LifecycleRule struct {
	ID                             string                                   `json:"ID"`
	Status                         string                                   `json:"Status"` // Enabled, Disabled
	Filter                         LifecycleFilter                          `json:"Filter"`
	Expiration                     *LifecycleExpiration                     `json:"Expiration,omitempty"`
	NoncurrentVersionExpiration    *NoncurrentVersionExpiration             `json:"NoncurrentVersionExpiration,omitempty"`
	Transition                     *LifecycleTransition                     `json:"Transition,omitempty"`
	AbortIncompleteMultipartUpload *LifecycleAbortIncompleteMultipartUpload `json:"AbortIncompleteMultipartUpload,omitempty"`
}

// LifecycleFilter represents lifecycle rule filter
type LifecycleFilter struct {
	Prefix string `json:"Prefix,omitempty"`
	Tag    *Tag   `json:"Tag,omitempty"`
}

// LifecycleExpiration represents object expiration settings
type LifecycleExpiration struct {
	Days                      *int       `json:"Days,omitempty"`
	Date                      *time.Time `json:"Date,omitempty"`
	ExpiredObjectDeleteMarker *bool      `json:"ExpiredObjectDeleteMarker,omitempty"`
}

// LifecycleTransition represents object transition settings
type LifecycleTransition struct {
	Days         *int       `json:"Days,omitempty"`
	Date         *time.Time `json:"Date,omitempty"`
	StorageClass string     `json:"StorageClass"`
}

// NoncurrentVersionExpiration represents noncurrent version expiration settings
type NoncurrentVersionExpiration struct {
	NoncurrentDays int `json:"NoncurrentDays"` // Delete noncurrent versions after this many days
}

// LifecycleAbortIncompleteMultipartUpload represents incomplete multipart upload abort settings
type LifecycleAbortIncompleteMultipartUpload struct {
	DaysAfterInitiation int `json:"DaysAfterInitiation"`
}

// CORSConfig represents bucket CORS configuration
type CORSConfig struct {
	CORSRules []CORSRule `json:"CORSRules"`
}

// CORSRule represents a single CORS rule
type CORSRule struct {
	ID             string   `json:"ID,omitempty"`
	AllowedHeaders []string `json:"AllowedHeaders,omitempty"`
	AllowedMethods []string `json:"AllowedMethods"`
	AllowedOrigins []string `json:"AllowedOrigins"`
	ExposeHeaders  []string `json:"ExposeHeaders,omitempty"`
	MaxAgeSeconds  *int     `json:"MaxAgeSeconds,omitempty"`
}

// ObjectLockConfig represents bucket object lock configuration
type ObjectLockConfig struct {
	ObjectLockEnabled bool            `json:"objectLockEnabled"`
	Rule              *ObjectLockRule `json:"rule,omitempty"`
}

// ObjectLockRule represents object lock rule
type ObjectLockRule struct {
	DefaultRetention *DefaultRetention `json:"defaultRetention,omitempty"`
}

// DefaultRetention represents default retention settings
type DefaultRetention struct {
	Mode  string `json:"mode"` // GOVERNANCE, COMPLIANCE
	Days  *int   `json:"days,omitempty"`
	Years *int   `json:"years,omitempty"`
}

// Tag represents a key-value tag
type Tag struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}

// NotificationConfig represents bucket notification configuration.
// TopicConfigurations, QueueConfigurations, and LambdaConfigurations map to the three
// S3 target types. In MaxIOFS, the Endpoint field holds the webhook URL that receives
// the S3-compatible event payload (SNS/SQS/Lambda ARNs are treated as HTTP endpoints).
type NotificationConfig struct {
	TopicConfigurations  []NotificationTarget `json:"TopicConfigurations,omitempty"`
	QueueConfigurations  []NotificationTarget `json:"QueueConfigurations,omitempty"`
	LambdaConfigurations []NotificationTarget `json:"LambdaConfigurations,omitempty"`
}

// NotificationTarget is a unified notification target (topic, queue, or lambda).
type NotificationTarget struct {
	ID       string              `json:"Id,omitempty"`
	Endpoint string              `json:"Endpoint"` // webhook URL (ARN treated as URL)
	Events   []string            `json:"Events"`
	Filter   *NotificationFilter `json:"Filter,omitempty"`
}

// NotificationFilter holds simple prefix/suffix key filters for notifications.
type NotificationFilter struct {
	Prefix string `json:"Prefix,omitempty"`
	Suffix string `json:"Suffix,omitempty"`
}

// EncryptionConfig represents bucket encryption configuration
type EncryptionConfig struct {
	Type     string `json:"type"` // AES256, aws:kms
	KMSKeyID string `json:"kmsKeyId,omitempty"`
}

// PublicAccessBlock represents public access block configuration
type PublicAccessBlock struct {
	BlockPublicAcls       bool `json:"blockPublicAcls"`
	IgnorePublicAcls      bool `json:"ignorePublicAcls"`
	BlockPublicPolicy     bool `json:"blockPublicPolicy"`
	RestrictPublicBuckets bool `json:"restrictPublicBuckets"`
}

// OwnershipControlsConfig represents S3 bucket ownership controls configuration.
// ObjectOwnership controls how object ownership is managed for the bucket.
// Valid values: BucketOwnerEnforced (ACLs disabled), BucketOwnerPreferred, ObjectWriter.
type OwnershipControlsConfig struct {
	ObjectOwnership string `json:"objectOwnership"` // BucketOwnerEnforced | BucketOwnerPreferred | ObjectWriter
}

// LoggingConfig represents S3 server access logging configuration for a bucket.
type LoggingConfig struct {
	TargetBucket string `json:"targetBucket"` // Bucket where access logs are delivered
	TargetPrefix string `json:"targetPrefix"` // Key prefix for log objects (e.g. "logs/")
}
