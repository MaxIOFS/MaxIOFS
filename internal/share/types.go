package share

import (
	"errors"
	"time"
)

// Share represents a shared object with a presigned URL
type Share struct {
	ID          string    `json:"id"`
	BucketName  string    `json:"bucketName"`
	ObjectKey   string    `json:"objectKey"`
	AccessKeyID string    `json:"accessKeyId"`
	SecretKey   string    `json:"-"` // Never expose in JSON
	ShareToken  string    `json:"shareToken"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"` // nil = never expires
	CreatedAt   time.Time `json:"createdAt"`
	CreatedBy   string    `json:"createdBy"` // User ID
}

// ShareCreateRequest represents a request to create a share
type ShareCreateRequest struct {
	ExpiresIn *int64 `json:"expiresIn"` // seconds, nil = never expires
}

// ShareResponse represents the response when creating/getting a share
type ShareResponse struct {
	ID        string     `json:"id"`
	URL       string     `json:"url"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	IsExpired bool       `json:"isExpired"`
}

// Common errors
var (
	ErrShareNotFound = errors.New("share not found")
	ErrShareExpired  = errors.New("share has expired")
)

// IsExpired checks if the share has expired
func (s *Share) IsExpired() bool {
	if s.ExpiresAt == nil {
		return false // Never expires
	}
	return time.Now().UTC().After(*s.ExpiresAt)
}
