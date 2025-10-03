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
)

// Policy represents a bucket policy
type Policy struct {
	Version   string      `json:"Version"`
	Statement []Statement `json:"Statement"`
}

// Statement represents a policy statement
type Statement struct {
	Sid       string                 `json:"Sid,omitempty"`
	Effect    string                 `json:"Effect"`
	Principal map[string]interface{} `json:"Principal,omitempty"`
	Action    []string               `json:"Action"`
	Resource  []string               `json:"Resource"`
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

// NotificationConfig represents bucket notification configuration
type NotificationConfig struct {
	TopicConfigurations  []TopicConfiguration  `json:"TopicConfigurations,omitempty"`
	QueueConfigurations  []QueueConfiguration  `json:"QueueConfigurations,omitempty"`
	LambdaConfigurations []LambdaConfiguration `json:"LambdaConfigurations,omitempty"`
}

// TopicConfiguration represents SNS topic notification configuration
type TopicConfiguration struct {
	ID       string   `json:"Id,omitempty"`
	TopicArn string   `json:"Topic"`
	Events   []string `json:"Events"`
	Filter   *Filter  `json:"Filter,omitempty"`
}

// QueueConfiguration represents SQS queue notification configuration
type QueueConfiguration struct {
	ID       string   `json:"Id,omitempty"`
	QueueArn string   `json:"Queue"`
	Events   []string `json:"Events"`
	Filter   *Filter  `json:"Filter,omitempty"`
}

// LambdaConfiguration represents Lambda function notification configuration
type LambdaConfiguration struct {
	ID                string   `json:"Id,omitempty"`
	LambdaFunctionArn string   `json:"CloudWatchConfiguration"`
	Events            []string `json:"Events"`
	Filter            *Filter  `json:"Filter,omitempty"`
}

// Filter represents notification filter
type Filter struct {
	Key *KeyFilter `json:"S3Key,omitempty"`
}

// KeyFilter represents key-based filter
type KeyFilter struct {
	FilterRules []FilterRule `json:"FilterRules,omitempty"`
}

// FilterRule represents a single filter rule
type FilterRule struct {
	Name  string `json:"Name"` // prefix, suffix
	Value string `json:"Value"`
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
