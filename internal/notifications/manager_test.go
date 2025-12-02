package notifications

import (
	"context"
	"testing"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) *metadata.BadgerStore {
	tmpDir := t.TempDir()
	opts := metadata.BadgerOptions{
		DataDir: tmpDir,
	}
	store, err := metadata.NewBadgerStore(opts)
	require.NoError(t, err)
	return store
}

func TestNewManager(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	manager := NewManager(store)
	assert.NotNil(t, manager)
	assert.NotNil(t, manager.httpClient)
	assert.NotNil(t, manager.configCache)
}

func TestGetConfiguration_NotFound(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	manager := NewManager(store)
	ctx := context.Background()

	config, err := manager.GetConfiguration(ctx, "tenant-1", "test-bucket")
	require.NoError(t, err)
	assert.Nil(t, config)
}

func TestPutConfiguration(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

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
	defer store.Close()

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
	defer store.Close()

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
	defer store.Close()

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
	defer store.Close()

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
	defer store.Close()

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
	defer store.Close()

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
	defer store.Close()

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
	defer store.Close()

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
	defer store.Close()

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
