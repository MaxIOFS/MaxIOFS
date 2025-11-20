package notifications

import "time"

// EventType represents the type of S3 event
type EventType string

const (
	// Object creation events
	EventObjectCreated         EventType = "s3:ObjectCreated:*"
	EventObjectCreatedPut      EventType = "s3:ObjectCreated:Put"
	EventObjectCreatedPost     EventType = "s3:ObjectCreated:Post"
	EventObjectCreatedCopy     EventType = "s3:ObjectCreated:Copy"
	EventObjectCreatedMultipart EventType = "s3:ObjectCreated:CompleteMultipartUpload"

	// Object removal events
	EventObjectRemoved       EventType = "s3:ObjectRemoved:*"
	EventObjectRemovedDelete EventType = "s3:ObjectRemoved:Delete"

	// Object restoration events
	EventObjectRestored EventType = "s3:ObjectRestored:Post"
)

// NotificationConfiguration represents the bucket notification configuration
type NotificationConfiguration struct {
	BucketName string              `json:"bucketName"`
	TenantID   string              `json:"tenantId,omitempty"`
	Rules      []NotificationRule  `json:"rules"`
	UpdatedAt  time.Time           `json:"updatedAt"`
	UpdatedBy  string              `json:"updatedBy"`
}

// NotificationRule represents a single notification rule
type NotificationRule struct {
	ID            string       `json:"id"`
	Enabled       bool         `json:"enabled"`
	WebhookURL    string       `json:"webhookUrl"`
	Events        []EventType  `json:"events"`
	FilterPrefix  string       `json:"filterPrefix,omitempty"`
	FilterSuffix  string       `json:"filterSuffix,omitempty"`
	CustomHeaders map[string]string `json:"customHeaders,omitempty"`
}

// Event represents a notification event to be sent
type Event struct {
	EventVersion string    `json:"eventVersion"`
	EventSource  string    `json:"eventSource"`
	EventTime    time.Time `json:"eventTime"`
	EventName    EventType `json:"eventName"`
	UserIdentity struct {
		PrincipalID string `json:"principalId"`
	} `json:"userIdentity"`
	RequestParameters struct {
		SourceIPAddress string `json:"sourceIPAddress"`
	} `json:"requestParameters"`
	ResponseElements struct {
		XAmzRequestID string `json:"x-amz-request-id"`
	} `json:"responseElements"`
	S3 struct {
		S3SchemaVersion string `json:"s3SchemaVersion"`
		ConfigurationID string `json:"configurationId"`
		Bucket          struct {
			Name          string `json:"name"`
			OwnerIdentity struct {
				PrincipalID string `json:"principalId"`
			} `json:"ownerIdentity"`
			ARN string `json:"arn"`
		} `json:"bucket"`
		Object struct {
			Key       string `json:"key"`
			Size      int64  `json:"size,omitempty"`
			ETag      string `json:"eTag,omitempty"`
			VersionID string `json:"versionId,omitempty"`
			Sequencer string `json:"sequencer"`
		} `json:"object"`
	} `json:"s3"`
}

// WebhookPayload is what gets sent to the webhook URL
type WebhookPayload struct {
	Records []Event `json:"Records"`
}

// EventInfo contains minimal information needed to create an event
type EventInfo struct {
	BucketName string
	TenantID   string
	ObjectKey  string
	Size       int64
	ETag       string
	VersionID  string
	EventType  EventType
	UserID     string
	RequestID  string
	SourceIP   string
}
