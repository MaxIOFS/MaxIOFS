package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) *metadata.PebbleStore {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "maxiofs-notif-test-*")
	require.NoError(t, err)

	opts := metadata.PebbleOptions{
		DataDir: tmpDir,
	}
	store, err := metadata.NewPebbleStore(opts)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = store.Close()
		_ = os.RemoveAll(tmpDir) // ignore error on Windows file locking
	})

	return store
}

func TestNewManager(t *testing.T) {
	store := setupTestStore(t)

	manager := NewManager(store)
	assert.NotNil(t, manager)
	assert.NotNil(t, manager.httpClient)
	assert.NotNil(t, manager.configCache)
}

func TestGetConfiguration_NotFound(t *testing.T) {
	store := setupTestStore(t)

	manager := NewManager(store)
	ctx := context.Background()

	config, err := manager.GetConfiguration(ctx, "tenant-1", "test-bucket")
	require.NoError(t, err)
	assert.Nil(t, config)
}

func TestPutConfiguration(t *testing.T) {
	store := setupTestStore(t)

	manager := NewManager(store)
	ctx := context.Background()

	config := &NotificationConfiguration{
		TenantID:   "tenant-1",
		BucketName: "test-bucket",
		Rules: []NotificationRule{
			{
				ID:         "rule-1",
				WebhookURL: "http://localhost:8080/webhook",
				Events:     []EventType{EventObjectCreated},
				Enabled:    true,
			},
		},
	}

	err := manager.PutConfiguration(ctx, config)
	require.NoError(t, err)
}

func TestGetConfiguration(t *testing.T) {
	store := setupTestStore(t)

	manager := NewManager(store)
	ctx := context.Background()

	config := &NotificationConfiguration{
		TenantID:   "tenant-1",
		BucketName: "test-bucket",
		Rules: []NotificationRule{
			{
				ID:         "rule-1",
				WebhookURL: "http://localhost:8080/webhook",
				Events:     []EventType{EventObjectCreated},
				Enabled:    true,
			},
		},
	}

	err := manager.PutConfiguration(ctx, config)
	require.NoError(t, err)

	retrieved, err := manager.GetConfiguration(ctx, "tenant-1", "test-bucket")
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
	assert.Equal(t, "tenant-1", retrieved.TenantID)
	assert.Equal(t, "test-bucket", retrieved.BucketName)
	assert.Len(t, retrieved.Rules, 1)
	assert.Equal(t, "rule-1", retrieved.Rules[0].ID)
}

func TestGetConfiguration_Cache(t *testing.T) {
	store := setupTestStore(t)

	manager := NewManager(store)
	ctx := context.Background()

	config := &NotificationConfiguration{
		TenantID:   "tenant-1",
		BucketName: "test-bucket",
		Rules: []NotificationRule{
			{
				ID:         "rule-1",
				WebhookURL: "http://localhost:8080/webhook",
				Events:     []EventType{EventObjectCreated},
				Enabled:    true,
			},
		},
	}

	err := manager.PutConfiguration(ctx, config)
	require.NoError(t, err)

	retrieved1, err := manager.GetConfiguration(ctx, "tenant-1", "test-bucket")
	require.NoError(t, err)
	assert.NotNil(t, retrieved1)

	retrieved2, err := manager.GetConfiguration(ctx, "tenant-1", "test-bucket")
	require.NoError(t, err)
	assert.NotNil(t, retrieved2)
	assert.Equal(t, retrieved1, retrieved2)
}

func TestDeleteConfiguration(t *testing.T) {
	store := setupTestStore(t)

	manager := NewManager(store)
	ctx := context.Background()

	config := &NotificationConfiguration{
		TenantID:   "tenant-1",
		BucketName: "test-bucket",
		Rules: []NotificationRule{
			{
				ID:         "rule-1",
				WebhookURL: "http://localhost:8080/webhook",
				Events:     []EventType{EventObjectCreated},
				Enabled:    true,
			},
		},
	}

	err := manager.PutConfiguration(ctx, config)
	require.NoError(t, err)

	err = manager.DeleteConfiguration(ctx, "tenant-1", "test-bucket")
	require.NoError(t, err)

	retrieved, err := manager.GetConfiguration(ctx, "tenant-1", "test-bucket")
	require.NoError(t, err)
	assert.Nil(t, retrieved)
}

func TestPutConfiguration_MultipleRules(t *testing.T) {
	store := setupTestStore(t)

	manager := NewManager(store)
	ctx := context.Background()

	config := &NotificationConfiguration{
		TenantID:   "tenant-1",
		BucketName: "test-bucket",
		Rules: []NotificationRule{
			{
				ID:         "rule-1",
				WebhookURL: "http://localhost:8080/webhook1",
				Events:     []EventType{EventObjectCreated},
				Enabled:    true,
			},
			{
				ID:         "rule-2",
				WebhookURL: "http://localhost:8080/webhook2",
				Events:     []EventType{EventObjectRemoved},
				Enabled:    true,
			},
		},
	}

	err := manager.PutConfiguration(ctx, config)
	require.NoError(t, err)

	retrieved, err := manager.GetConfiguration(ctx, "tenant-1", "test-bucket")
	require.NoError(t, err)
	assert.Len(t, retrieved.Rules, 2)
}

func TestPutConfiguration_WithPrefix(t *testing.T) {
	store := setupTestStore(t)

	manager := NewManager(store)
	ctx := context.Background()

	config := &NotificationConfiguration{
		TenantID:   "tenant-1",
		BucketName: "test-bucket",
		Rules: []NotificationRule{
			{
				ID:           "rule-1",
				WebhookURL:   "http://localhost:8080/webhook",
				Events:       []EventType{EventObjectCreated},
				FilterPrefix: "uploads/",
				Enabled:      true,
			},
		},
	}

	err := manager.PutConfiguration(ctx, config)
	require.NoError(t, err)

	retrieved, err := manager.GetConfiguration(ctx, "tenant-1", "test-bucket")
	require.NoError(t, err)
	assert.Equal(t, "uploads/", retrieved.Rules[0].FilterPrefix)
}

func TestPutConfiguration_WithSuffix(t *testing.T) {
	store := setupTestStore(t)

	manager := NewManager(store)
	ctx := context.Background()

	config := &NotificationConfiguration{
		TenantID:   "tenant-1",
		BucketName: "test-bucket",
		Rules: []NotificationRule{
			{
				ID:           "rule-1",
				WebhookURL:   "http://localhost:8080/webhook",
				Events:       []EventType{EventObjectCreated},
				FilterSuffix: ".jpg",
				Enabled:      true,
			},
		},
	}

	err := manager.PutConfiguration(ctx, config)
	require.NoError(t, err)

	retrieved, err := manager.GetConfiguration(ctx, "tenant-1", "test-bucket")
	require.NoError(t, err)
	assert.Equal(t, ".jpg", retrieved.Rules[0].FilterSuffix)
}

func TestGetBucketPath(t *testing.T) {
	tests := []struct {
		tenantID   string
		bucketName string
		expected   string
	}{
		{"tenant-1", "bucket-1", "tenant-1/bucket-1"},
		{"", "bucket-1", "bucket-1"},
		{"tenant-2", "my-bucket", "tenant-2/my-bucket"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := getBucketPath(tt.tenantID, tt.bucketName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEventInfo_Validation(t *testing.T) {
	info := EventInfo{
		EventType:  EventObjectCreated,
		TenantID:   "tenant-1",
		BucketName: "test-bucket",
		ObjectKey:  "test.txt",
		Size:       1024,
		ETag:       "abc123",
		UserID:     "user-1",
		SourceIP:   "127.0.0.1",
	}

	assert.Equal(t, EventObjectCreated, info.EventType)
	assert.Equal(t, "tenant-1", info.TenantID)
	assert.Equal(t, "test-bucket", info.BucketName)
	assert.Equal(t, "test.txt", info.ObjectKey)
}

func TestNotificationRule_Disabled(t *testing.T) {
	store := setupTestStore(t)

	manager := NewManager(store)
	ctx := context.Background()

	config := &NotificationConfiguration{
		TenantID:   "tenant-1",
		BucketName: "test-bucket",
		Rules: []NotificationRule{
			{
				ID:         "rule-1",
				WebhookURL: "http://localhost:8080/webhook",
				Events:     []EventType{EventObjectCreated},
				Enabled:    false,
			},
		},
	}

	err := manager.PutConfiguration(ctx, config)
	require.NoError(t, err)

	retrieved, err := manager.GetConfiguration(ctx, "tenant-1", "test-bucket")
	require.NoError(t, err)
	assert.False(t, retrieved.Rules[0].Enabled)
}

func TestNotificationConfiguration_UpdatedAt(t *testing.T) {
	store := setupTestStore(t)

	manager := NewManager(store)
	ctx := context.Background()

	config := &NotificationConfiguration{
		TenantID:   "tenant-1",
		BucketName: "test-bucket",
		Rules: []NotificationRule{
			{
				ID:         "rule-1",
				WebhookURL: "http://localhost:8080/webhook",
				Events:     []EventType{EventObjectCreated},
				Enabled:    true,
			},
		},
	}

	err := manager.PutConfiguration(ctx, config)
	require.NoError(t, err)

	retrieved, err := manager.GetConfiguration(ctx, "tenant-1", "test-bucket")
	require.NoError(t, err)
	assert.False(t, retrieved.UpdatedAt.IsZero())
}

func TestEventTypes(t *testing.T) {
	assert.Equal(t, EventType("s3:ObjectCreated:*"), EventObjectCreated)
	assert.Equal(t, EventType("s3:ObjectCreated:Put"), EventObjectCreatedPut)
	assert.Equal(t, EventType("s3:ObjectRemoved:*"), EventObjectRemoved)
}

func TestNotificationRule_CustomHeaders(t *testing.T) {
	store := setupTestStore(t)

	manager := NewManager(store)
	ctx := context.Background()

	config := &NotificationConfiguration{
		TenantID:   "tenant-1",
		BucketName: "test-bucket",
		Rules: []NotificationRule{
			{
				ID:         "rule-1",
				WebhookURL: "http://localhost:8080/webhook",
				Events:     []EventType{EventObjectCreated},
				Enabled:    true,
				CustomHeaders: map[string]string{
					"X-Custom-Header": "value1",
					"Authorization":   "Bearer token",
				},
			},
		},
	}

	err := manager.PutConfiguration(ctx, config)
	require.NoError(t, err)

	retrieved, err := manager.GetConfiguration(ctx, "tenant-1", "test-bucket")
	require.NoError(t, err)
	assert.NotNil(t, retrieved.Rules[0].CustomHeaders)
	assert.Equal(t, "value1", retrieved.Rules[0].CustomHeaders["X-Custom-Header"])
	assert.Equal(t, "Bearer token", retrieved.Rules[0].CustomHeaders["Authorization"])
}

// Tests for matchesEventType helper function
func TestMatchesEventType(t *testing.T) {
	tests := []struct {
		name        string
		ruleEvent   EventType
		actualEvent EventType
		expected    bool
	}{
		{
			name:        "exact match",
			ruleEvent:   EventObjectCreatedPut,
			actualEvent: EventObjectCreatedPut,
			expected:    true,
		},
		{
			name:        "wildcard matches specific event",
			ruleEvent:   EventObjectCreated,
			actualEvent: EventObjectCreatedPut,
			expected:    true,
		},
		{
			name:        "wildcard matches another specific event",
			ruleEvent:   EventObjectCreated,
			actualEvent: EventObjectCreatedPost,
			expected:    true,
		},
		{
			name:        "wildcard ObjectRemoved matches delete",
			ruleEvent:   EventObjectRemoved,
			actualEvent: EventObjectRemovedDelete,
			expected:    true,
		},
		{
			name:        "no match different event types",
			ruleEvent:   EventObjectCreatedPut,
			actualEvent: EventObjectRemovedDelete,
			expected:    false,
		},
		{
			name:        "no match different wildcards",
			ruleEvent:   EventObjectCreated,
			actualEvent: EventObjectRemovedDelete,
			expected:    false,
		},
		{
			name:        "specific event doesn't match wildcard rule",
			ruleEvent:   EventObjectCreatedPut,
			actualEvent: EventObjectCreated,
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesEventType(tt.ruleEvent, tt.actualEvent)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Tests for validateConfiguration helper function
func TestValidateConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		config      *NotificationConfiguration
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid configuration",
			config: &NotificationConfiguration{
				BucketName: "test-bucket",
				TenantID:   "tenant-1",
				Rules: []NotificationRule{
					{
						ID:         "rule-1",
						WebhookURL: "https://example.com/webhook",
						Events:     []EventType{EventObjectCreated},
						Enabled:    true,
					},
				},
			},
			expectError: false,
		},
		{
			name: "missing bucket name",
			config: &NotificationConfiguration{
				TenantID: "tenant-1",
				Rules: []NotificationRule{
					{
						ID:         "rule-1",
						WebhookURL: "https://example.com/webhook",
						Events:     []EventType{EventObjectCreated},
					},
				},
			},
			expectError: true,
			errorMsg:    "bucket name is required",
		},
		{
			name: "no rules",
			config: &NotificationConfiguration{
				BucketName: "test-bucket",
				TenantID:   "tenant-1",
				Rules:      []NotificationRule{},
			},
			expectError: true,
			errorMsg:    "at least one rule is required",
		},
		{
			name: "missing rule ID",
			config: &NotificationConfiguration{
				BucketName: "test-bucket",
				TenantID:   "tenant-1",
				Rules: []NotificationRule{
					{
						WebhookURL: "https://example.com/webhook",
						Events:     []EventType{EventObjectCreated},
					},
				},
			},
			expectError: true,
			errorMsg:    "rule 0: ID is required",
		},
		{
			name: "missing webhook URL",
			config: &NotificationConfiguration{
				BucketName: "test-bucket",
				TenantID:   "tenant-1",
				Rules: []NotificationRule{
					{
						ID:     "rule-1",
						Events: []EventType{EventObjectCreated},
					},
				},
			},
			expectError: true,
			errorMsg:    "rule 0: webhook URL is required",
		},
		{
			name: "invalid webhook URL protocol",
			config: &NotificationConfiguration{
				BucketName: "test-bucket",
				TenantID:   "tenant-1",
				Rules: []NotificationRule{
					{
						ID:         "rule-1",
						WebhookURL: "ftp://example.com/webhook",
						Events:     []EventType{EventObjectCreated},
					},
				},
			},
			expectError: true,
			errorMsg:    "rule 0: webhook URL must start with http:// or https://",
		},
		{
			name: "missing events",
			config: &NotificationConfiguration{
				BucketName: "test-bucket",
				TenantID:   "tenant-1",
				Rules: []NotificationRule{
					{
						ID:         "rule-1",
						WebhookURL: "https://example.com/webhook",
						Events:     []EventType{},
					},
				},
			},
			expectError: true,
			errorMsg:    "rule 0: at least one event is required",
		},
		{
			name: "http URL is valid",
			config: &NotificationConfiguration{
				BucketName: "test-bucket",
				TenantID:   "tenant-1",
				Rules: []NotificationRule{
					{
						ID:         "rule-1",
						WebhookURL: "http://example.com/webhook",
						Events:     []EventType{EventObjectCreated},
					},
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfiguration(tt.config)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Tests for generateSequencer helper function
func TestGenerateSequencer(t *testing.T) {
	// Generate multiple sequencers and ensure they're unique
	sequencers := make(map[string]bool)
	for i := 0; i < 100; i++ {
		seq := generateSequencer()
		assert.NotEmpty(t, seq)
		assert.False(t, sequencers[seq], "sequencer should be unique")
		sequencers[seq] = true
	}
}

// Tests for createEvent method
func TestCreateEvent(t *testing.T) {
	store := setupTestStore(t)

	manager := NewManager(store)

	info := EventInfo{
		EventType:  EventObjectCreatedPut,
		TenantID:   "tenant-1",
		BucketName: "test-bucket",
		ObjectKey:  "test-object.txt",
		Size:       1024,
		ETag:       "abc123",
		VersionID:  "v1",
		UserID:     "user-1",
		RequestID:  "req-123",
		SourceIP:   "192.168.1.1",
	}

	event := manager.createEvent(info)

	// Verify event structure
	assert.Equal(t, "2.1", event.EventVersion)
	assert.Equal(t, "maxiofs:s3", event.EventSource)
	assert.Equal(t, EventObjectCreatedPut, event.EventName)
	assert.False(t, event.EventTime.IsZero())

	// Verify user identity
	assert.Equal(t, "user-1", event.UserIdentity.PrincipalID)

	// Verify request parameters
	assert.Equal(t, "192.168.1.1", event.RequestParameters.SourceIPAddress)

	// Verify response elements
	assert.Equal(t, "req-123", event.ResponseElements.XAmzRequestID)

	// Verify S3 info
	assert.Equal(t, "1.0", event.S3.S3SchemaVersion)
	assert.Equal(t, "test-bucket", event.S3.Bucket.Name)
	assert.Equal(t, "tenant-1", event.S3.Bucket.OwnerIdentity.PrincipalID)
	assert.Equal(t, "arn:aws:s3:::tenant-1/test-bucket", event.S3.Bucket.ARN)

	// Verify object info
	assert.Equal(t, "test-object.txt", event.S3.Object.Key)
	assert.Equal(t, int64(1024), event.S3.Object.Size)
	assert.Equal(t, "abc123", event.S3.Object.ETag)
	assert.Equal(t, "v1", event.S3.Object.VersionID)
	assert.NotEmpty(t, event.S3.Object.Sequencer)
}

// Tests for matchesRule method
func TestMatchesRule(t *testing.T) {
	store := setupTestStore(t)

	manager := NewManager(store)

	tests := []struct {
		name     string
		rule     NotificationRule
		info     EventInfo
		expected bool
	}{
		{
			name: "disabled rule should not match",
			rule: NotificationRule{
				ID:      "rule-1",
				Enabled: false,
				Events:  []EventType{EventObjectCreated},
			},
			info: EventInfo{
				EventType: EventObjectCreatedPut,
			},
			expected: false,
		},
		{
			name: "exact event type match",
			rule: NotificationRule{
				ID:      "rule-1",
				Enabled: true,
				Events:  []EventType{EventObjectCreatedPut},
			},
			info: EventInfo{
				EventType: EventObjectCreatedPut,
				ObjectKey: "test.txt",
			},
			expected: true,
		},
		{
			name: "wildcard event type match",
			rule: NotificationRule{
				ID:      "rule-1",
				Enabled: true,
				Events:  []EventType{EventObjectCreated},
			},
			info: EventInfo{
				EventType: EventObjectCreatedPut,
				ObjectKey: "test.txt",
			},
			expected: true,
		},
		{
			name: "event type mismatch",
			rule: NotificationRule{
				ID:      "rule-1",
				Enabled: true,
				Events:  []EventType{EventObjectRemoved},
			},
			info: EventInfo{
				EventType: EventObjectCreatedPut,
				ObjectKey: "test.txt",
			},
			expected: false,
		},
		{
			name: "prefix filter matches",
			rule: NotificationRule{
				ID:           "rule-1",
				Enabled:      true,
				Events:       []EventType{EventObjectCreated},
				FilterPrefix: "uploads/",
			},
			info: EventInfo{
				EventType: EventObjectCreatedPut,
				ObjectKey: "uploads/test.txt",
			},
			expected: true,
		},
		{
			name: "prefix filter does not match",
			rule: NotificationRule{
				ID:           "rule-1",
				Enabled:      true,
				Events:       []EventType{EventObjectCreated},
				FilterPrefix: "uploads/",
			},
			info: EventInfo{
				EventType: EventObjectCreatedPut,
				ObjectKey: "downloads/test.txt",
			},
			expected: false,
		},
		{
			name: "suffix filter matches",
			rule: NotificationRule{
				ID:           "rule-1",
				Enabled:      true,
				Events:       []EventType{EventObjectCreated},
				FilterSuffix: ".jpg",
			},
			info: EventInfo{
				EventType: EventObjectCreatedPut,
				ObjectKey: "photo.jpg",
			},
			expected: true,
		},
		{
			name: "suffix filter does not match",
			rule: NotificationRule{
				ID:           "rule-1",
				Enabled:      true,
				Events:       []EventType{EventObjectCreated},
				FilterSuffix: ".jpg",
			},
			info: EventInfo{
				EventType: EventObjectCreatedPut,
				ObjectKey: "document.pdf",
			},
			expected: false,
		},
		{
			name: "prefix and suffix filters both match",
			rule: NotificationRule{
				ID:           "rule-1",
				Enabled:      true,
				Events:       []EventType{EventObjectCreated},
				FilterPrefix: "uploads/",
				FilterSuffix: ".jpg",
			},
			info: EventInfo{
				EventType: EventObjectCreatedPut,
				ObjectKey: "uploads/photo.jpg",
			},
			expected: true,
		},
		{
			name: "prefix matches but suffix does not",
			rule: NotificationRule{
				ID:           "rule-1",
				Enabled:      true,
				Events:       []EventType{EventObjectCreated},
				FilterPrefix: "uploads/",
				FilterSuffix: ".jpg",
			},
			info: EventInfo{
				EventType: EventObjectCreatedPut,
				ObjectKey: "uploads/document.pdf",
			},
			expected: false,
		},
		{
			name: "multiple event types, one matches",
			rule: NotificationRule{
				ID:      "rule-1",
				Enabled: true,
				Events:  []EventType{EventObjectCreatedPut, EventObjectCreatedPost},
			},
			info: EventInfo{
				EventType: EventObjectCreatedPost,
				ObjectKey: "test.txt",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.matchesRule(&tt.rule, tt.info)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Tests for sendWebhook method
func TestSendWebhook(t *testing.T) {
	t.Run("successful webhook delivery", func(t *testing.T) {
		// Create test server that returns 200 OK
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify request method
			assert.Equal(t, "POST", r.Method)

			// Verify headers
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "MaxIOFS/1.0", r.Header.Get("User-Agent"))
			assert.Equal(t, "s3:ObjectCreated:Put", r.Header.Get("X-MaxIOFS-Event"))
			assert.Equal(t, "test-bucket", r.Header.Get("X-MaxIOFS-Bucket"))

			// Verify payload
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)

			var payload WebhookPayload
			err = json.Unmarshal(body, &payload)
			require.NoError(t, err)

			assert.Len(t, payload.Records, 1)
			assert.Equal(t, EventObjectCreatedPut, payload.Records[0].EventName)
			assert.Equal(t, "test-bucket", payload.Records[0].S3.Bucket.Name)
			assert.Equal(t, "test-object.txt", payload.Records[0].S3.Object.Key)

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		store := setupTestStore(t)
		manager := NewManager(store)

		rule := NotificationRule{
			ID:         "rule-1",
			WebhookURL: server.URL,
			Events:     []EventType{EventObjectCreatedPut},
			Enabled:    true,
		}

		event := Event{
			EventName: EventObjectCreatedPut,
		}
		event.S3.Bucket.Name = "test-bucket"
		event.S3.Object.Key = "test-object.txt"

		manager.sendWebhook(rule, event)

		// Give webhook goroutine time to complete
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("webhook with custom headers", func(t *testing.T) {
		// Create test server that verifies custom headers
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "custom-value", r.Header.Get("X-Custom-Header"))
			assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		store := setupTestStore(t)
		manager := NewManager(store)

		rule := NotificationRule{
			ID:         "rule-1",
			WebhookURL: server.URL,
			Events:     []EventType{EventObjectCreatedPut},
			Enabled:    true,
			CustomHeaders: map[string]string{
				"X-Custom-Header": "custom-value",
				"Authorization":   "Bearer test-token",
			},
		}

		event := Event{
			EventName: EventObjectCreatedPut,
		}
		event.S3.Bucket.Name = "test-bucket"
		event.S3.Object.Key = "test-object.txt"

		manager.sendWebhook(rule, event)

		// Give webhook goroutine time to complete
		time.Sleep(100 * time.Millisecond)
	})

	t.Run("webhook retry on failure", func(t *testing.T) {
		var attemptCount int32

		// Create test server that fails twice then succeeds
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			count := atomic.AddInt32(&attemptCount, 1)
			if count < 3 {
				w.WriteHeader(http.StatusInternalServerError)
			} else {
				w.WriteHeader(http.StatusOK)
			}
		}))
		defer server.Close()

		store := setupTestStore(t)
		manager := NewManager(store)

		rule := NotificationRule{
			ID:         "rule-1",
			WebhookURL: server.URL,
			Events:     []EventType{EventObjectCreatedPut},
			Enabled:    true,
		}

		event := Event{
			EventName: EventObjectCreatedPut,
		}
		event.S3.Bucket.Name = "test-bucket"

		manager.sendWebhook(rule, event)

		// Give webhook time to retry (2 retries * 2 seconds delay + execution time)
		time.Sleep(5 * time.Second)

		assert.Equal(t, int32(3), atomic.LoadInt32(&attemptCount))
	})

	t.Run("webhook max retries exceeded", func(t *testing.T) {
		var attemptCount int32

		// Create test server that always fails
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&attemptCount, 1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		store := setupTestStore(t)
		manager := NewManager(store)

		rule := NotificationRule{
			ID:         "rule-1",
			WebhookURL: server.URL,
			Events:     []EventType{EventObjectCreatedPut},
			Enabled:    true,
		}

		event := Event{
			EventName: EventObjectCreatedPut,
		}
		event.S3.Bucket.Name = "test-bucket"

		manager.sendWebhook(rule, event)

		// Give webhook time to retry all attempts (3 retries * 2 seconds delay + execution time)
		time.Sleep(7 * time.Second)

		// Should have attempted maxRetries (3) times
		assert.Equal(t, int32(3), atomic.LoadInt32(&attemptCount))
	})

	t.Run("webhook accepts 2xx status codes", func(t *testing.T) {
		statusCodes := []int{200, 201, 202, 204}

		for _, statusCode := range statusCodes {
			t.Run(http.StatusText(statusCode), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(statusCode)
				}))
				defer server.Close()

				store := setupTestStore(t)
				manager := NewManager(store)

				rule := NotificationRule{
					ID:         "rule-1",
					WebhookURL: server.URL,
					Events:     []EventType{EventObjectCreatedPut},
					Enabled:    true,
				}

				event := Event{
					EventName: EventObjectCreatedPut,
				}
				event.S3.Bucket.Name = "test-bucket"

				manager.sendWebhook(rule, event)

				// Give webhook time to complete
				time.Sleep(100 * time.Millisecond)
			})
		}
	})
}

// Tests for SendEvent method
func TestSendEvent(t *testing.T) {
	t.Run("sends event to matching rule", func(t *testing.T) {
		var receivedEvents int32

		// Create test server to receive webhooks
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&receivedEvents, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		store := setupTestStore(t)
		manager := NewManager(store)
		ctx := context.Background()

		// Configure notification
		config := &NotificationConfiguration{
			TenantID:   "tenant-1",
			BucketName: "test-bucket",
			Rules: []NotificationRule{
				{
					ID:         "rule-1",
					WebhookURL: server.URL,
					Events:     []EventType{EventObjectCreated},
					Enabled:    true,
				},
			},
		}
		err := manager.PutConfiguration(ctx, config)
		require.NoError(t, err)

		// Send event
		info := EventInfo{
			EventType:  EventObjectCreatedPut,
			TenantID:   "tenant-1",
			BucketName: "test-bucket",
			ObjectKey:  "test.txt",
			Size:       1024,
			ETag:       "abc123",
			UserID:     "user-1",
			RequestID:  "req-123",
			SourceIP:   "192.168.1.1",
		}

		manager.SendEvent(ctx, info)

		// Give webhook time to be sent
		time.Sleep(200 * time.Millisecond)

		assert.Equal(t, int32(1), atomic.LoadInt32(&receivedEvents))
	})

	t.Run("does not send event when no configuration exists", func(t *testing.T) {
		var receivedEvents int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&receivedEvents, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		store := setupTestStore(t)
		manager := NewManager(store)
		ctx := context.Background()

		// Send event without configuration
		info := EventInfo{
			EventType:  EventObjectCreatedPut,
			TenantID:   "tenant-1",
			BucketName: "test-bucket",
			ObjectKey:  "test.txt",
		}

		manager.SendEvent(ctx, info)

		// Give webhook time to potentially be sent
		time.Sleep(200 * time.Millisecond)

		assert.Equal(t, int32(0), atomic.LoadInt32(&receivedEvents))
	})

	t.Run("does not send event when rule is disabled", func(t *testing.T) {
		var receivedEvents int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&receivedEvents, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		store := setupTestStore(t)
		manager := NewManager(store)
		ctx := context.Background()

		// Configure notification with disabled rule
		config := &NotificationConfiguration{
			TenantID:   "tenant-1",
			BucketName: "test-bucket",
			Rules: []NotificationRule{
				{
					ID:         "rule-1",
					WebhookURL: server.URL,
					Events:     []EventType{EventObjectCreated},
					Enabled:    false,
				},
			},
		}
		err := manager.PutConfiguration(ctx, config)
		require.NoError(t, err)

		// Send event
		info := EventInfo{
			EventType:  EventObjectCreatedPut,
			TenantID:   "tenant-1",
			BucketName: "test-bucket",
			ObjectKey:  "test.txt",
		}

		manager.SendEvent(ctx, info)

		// Give webhook time to potentially be sent
		time.Sleep(200 * time.Millisecond)

		assert.Equal(t, int32(0), atomic.LoadInt32(&receivedEvents))
	})

	t.Run("does not send event when event type does not match", func(t *testing.T) {
		var receivedEvents int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&receivedEvents, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		store := setupTestStore(t)
		manager := NewManager(store)
		ctx := context.Background()

		// Configure notification for ObjectRemoved events
		config := &NotificationConfiguration{
			TenantID:   "tenant-1",
			BucketName: "test-bucket",
			Rules: []NotificationRule{
				{
					ID:         "rule-1",
					WebhookURL: server.URL,
					Events:     []EventType{EventObjectRemoved},
					Enabled:    true,
				},
			},
		}
		err := manager.PutConfiguration(ctx, config)
		require.NoError(t, err)

		// Send ObjectCreated event
		info := EventInfo{
			EventType:  EventObjectCreatedPut,
			TenantID:   "tenant-1",
			BucketName: "test-bucket",
			ObjectKey:  "test.txt",
		}

		manager.SendEvent(ctx, info)

		// Give webhook time to potentially be sent
		time.Sleep(200 * time.Millisecond)

		assert.Equal(t, int32(0), atomic.LoadInt32(&receivedEvents))
	})

	t.Run("sends event to multiple matching rules", func(t *testing.T) {
		var receivedEvents int32
		var mu sync.Mutex
		receivedWebhooks := make(map[string]int)

		// Create test server to receive webhooks
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&receivedEvents, 1)

			// Track which rule sent the webhook
			mu.Lock()
			ruleID := r.URL.Query().Get("rule")
			receivedWebhooks[ruleID]++
			mu.Unlock()

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		store := setupTestStore(t)
		manager := NewManager(store)
		ctx := context.Background()

		// Configure multiple matching rules
		config := &NotificationConfiguration{
			TenantID:   "tenant-1",
			BucketName: "test-bucket",
			Rules: []NotificationRule{
				{
					ID:         "rule-1",
					WebhookURL: server.URL + "?rule=rule-1",
					Events:     []EventType{EventObjectCreated},
					Enabled:    true,
				},
				{
					ID:         "rule-2",
					WebhookURL: server.URL + "?rule=rule-2",
					Events:     []EventType{EventObjectCreatedPut},
					Enabled:    true,
				},
			},
		}
		err := manager.PutConfiguration(ctx, config)
		require.NoError(t, err)

		// Send event
		info := EventInfo{
			EventType:  EventObjectCreatedPut,
			TenantID:   "tenant-1",
			BucketName: "test-bucket",
			ObjectKey:  "test.txt",
		}

		manager.SendEvent(ctx, info)

		// Give webhooks time to be sent
		time.Sleep(300 * time.Millisecond)

		// Both rules should have received the event
		assert.Equal(t, int32(2), atomic.LoadInt32(&receivedEvents))

		mu.Lock()
		assert.Equal(t, 1, receivedWebhooks["rule-1"])
		assert.Equal(t, 1, receivedWebhooks["rule-2"])
		mu.Unlock()
	})

	t.Run("respects prefix filter", func(t *testing.T) {
		var receivedEvents int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&receivedEvents, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		store := setupTestStore(t)
		manager := NewManager(store)
		ctx := context.Background()

		// Configure notification with prefix filter
		config := &NotificationConfiguration{
			TenantID:   "tenant-1",
			BucketName: "test-bucket",
			Rules: []NotificationRule{
				{
					ID:           "rule-1",
					WebhookURL:   server.URL,
					Events:       []EventType{EventObjectCreated},
					FilterPrefix: "uploads/",
					Enabled:      true,
				},
			},
		}
		err := manager.PutConfiguration(ctx, config)
		require.NoError(t, err)

		// Send event that matches prefix
		info1 := EventInfo{
			EventType:  EventObjectCreatedPut,
			TenantID:   "tenant-1",
			BucketName: "test-bucket",
			ObjectKey:  "uploads/test.txt",
		}
		manager.SendEvent(ctx, info1)

		// Send event that doesn't match prefix
		info2 := EventInfo{
			EventType:  EventObjectCreatedPut,
			TenantID:   "tenant-1",
			BucketName: "test-bucket",
			ObjectKey:  "downloads/test.txt",
		}
		manager.SendEvent(ctx, info2)

		// Give webhooks time to be sent
		time.Sleep(300 * time.Millisecond)

		// Only the first event should have been sent
		assert.Equal(t, int32(1), atomic.LoadInt32(&receivedEvents))
	})

	t.Run("respects suffix filter", func(t *testing.T) {
		var receivedEvents int32

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&receivedEvents, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		store := setupTestStore(t)
		manager := NewManager(store)
		ctx := context.Background()

		// Configure notification with suffix filter
		config := &NotificationConfiguration{
			TenantID:   "tenant-1",
			BucketName: "test-bucket",
			Rules: []NotificationRule{
				{
					ID:           "rule-1",
					WebhookURL:   server.URL,
					Events:       []EventType{EventObjectCreated},
					FilterSuffix: ".jpg",
					Enabled:      true,
				},
			},
		}
		err := manager.PutConfiguration(ctx, config)
		require.NoError(t, err)

		// Send event that matches suffix
		info1 := EventInfo{
			EventType:  EventObjectCreatedPut,
			TenantID:   "tenant-1",
			BucketName: "test-bucket",
			ObjectKey:  "photo.jpg",
		}
		manager.SendEvent(ctx, info1)

		// Send event that doesn't match suffix
		info2 := EventInfo{
			EventType:  EventObjectCreatedPut,
			TenantID:   "tenant-1",
			BucketName: "test-bucket",
			ObjectKey:  "document.pdf",
		}
		manager.SendEvent(ctx, info2)

		// Give webhooks time to be sent
		time.Sleep(300 * time.Millisecond)

		// Only the first event should have been sent
		assert.Equal(t, int32(1), atomic.LoadInt32(&receivedEvents))
	})
}
