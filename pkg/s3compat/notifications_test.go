package s3compat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plainHTTPClient returns a basic HTTP client with no SSRF blocking,
// used in tests where the webhook server runs on loopback (127.0.0.1).
func plainHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

// webhookCapture is a test HTTP server that captures incoming webhook payloads.
type webhookCapture struct {
	mu       sync.Mutex
	received []s3EventPayload
	server   *httptest.Server
}

func newWebhookCapture() *webhookCapture {
	wc := &webhookCapture{}
	wc.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload s3EventPayload
		if err := json.Unmarshal(body, &payload); err == nil {
			wc.mu.Lock()
			wc.received = append(wc.received, payload)
			wc.mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
	}))
	return wc
}

func (wc *webhookCapture) close() {
	wc.server.Close()
}

// waitForEvents waits up to maxWait for at least n events to be received.
func (wc *webhookCapture) waitForEvents(n int, maxWait time.Duration) []s3EventPayload {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		wc.mu.Lock()
		if len(wc.received) >= n {
			out := make([]s3EventPayload, len(wc.received))
			copy(out, wc.received)
			wc.mu.Unlock()
			return out
		}
		wc.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	wc.mu.Lock()
	out := make([]s3EventPayload, len(wc.received))
	copy(out, wc.received)
	wc.mu.Unlock()
	return out
}

// TestNotification_PutObjectFiresEvent verifies that uploading an object triggers a webhook.
func TestNotification_PutObjectFiresEvent(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	env.handler.notifHTTPClient = plainHTTPClient()

	ctx := context.Background()
	bucketName := "notif-put-bucket"
	objectKey := "test.txt"

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	// Start webhook capture server
	wc := newWebhookCapture()
	defer wc.close()

	// Configure bucket notifications
	notifConfig := &bucket.NotificationConfig{
		TopicConfigurations: []bucket.NotificationTarget{
			{
				ID:       "put-test",
				Endpoint: wc.server.URL,
				Events:   []string{"s3:ObjectCreated:*"},
			},
		},
	}
	require.NoError(t, env.bucketManager.SetNotification(ctx, env.tenantID, bucketName, notifConfig))

	// Upload an object
	req, w := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, []byte("hello notifications"))
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "PUT should succeed")

	// Wait for webhook delivery (goroutine is async)
	events := wc.waitForEvents(1, 2*time.Second)
	require.Len(t, events, 1, "Should receive exactly 1 webhook event")

	record := events[0].Records[0]
	assert.Equal(t, "s3:ObjectCreated:Put", record.EventName)
	assert.Equal(t, bucketName, record.S3.Bucket.Name)
	assert.Equal(t, objectKey, record.S3.Object.Key)
	assert.NotEmpty(t, record.S3.Object.ETag)
	assert.Equal(t, int64(len([]byte("hello notifications"))), record.S3.Object.Size)
}

// TestNotification_DeleteObjectFiresEvent verifies that deleting fires the ObjectRemoved event.
func TestNotification_DeleteObjectFiresEvent(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	env.handler.notifHTTPClient = plainHTTPClient()

	ctx := context.Background()
	bucketName := "notif-delete-bucket"
	objectKey := "todelete.txt"

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	wc := newWebhookCapture()
	defer wc.close()

	notifConfig := &bucket.NotificationConfig{
		TopicConfigurations: []bucket.NotificationTarget{
			{
				ID:       "delete-test",
				Endpoint: wc.server.URL,
				Events:   []string{"s3:ObjectRemoved:*"},
			},
		},
	}
	require.NoError(t, env.bucketManager.SetNotification(ctx, env.tenantID, bucketName, notifConfig))

	// First upload the object
	reqPut, wPut := env.makeS3Request("PUT", "/"+bucketName+"/"+objectKey, []byte("data"))
	env.router.ServeHTTP(wPut, reqPut)
	require.Equal(t, http.StatusOK, wPut.Code)

	// Now delete it
	reqDel, wDel := env.makeS3Request("DELETE", "/"+bucketName+"/"+objectKey, nil)
	env.router.ServeHTTP(wDel, reqDel)
	require.Equal(t, http.StatusNoContent, wDel.Code)

	events := wc.waitForEvents(1, 2*time.Second)
	require.Len(t, events, 1, "Should receive 1 delete event")
	assert.Contains(t, events[0].Records[0].EventName, "s3:ObjectRemoved:")
	assert.Equal(t, objectKey, events[0].Records[0].S3.Object.Key)
}

// TestNotification_EventFilter_MatchesPrefix verifies that prefix filters work.
func TestNotification_EventFilter_MatchesPrefix(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	env.handler.notifHTTPClient = plainHTTPClient()

	ctx := context.Background()
	bucketName := "notif-prefix-bucket"

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	wc := newWebhookCapture()
	defer wc.close()

	// Only notify for objects under "images/" prefix
	notifConfig := &bucket.NotificationConfig{
		TopicConfigurations: []bucket.NotificationTarget{
			{
				ID:       "prefix-filter",
				Endpoint: wc.server.URL,
				Events:   []string{"s3:ObjectCreated:*"},
				Filter: &bucket.NotificationFilter{
					Prefix: "images/",
				},
			},
		},
	}
	require.NoError(t, env.bucketManager.SetNotification(ctx, env.tenantID, bucketName, notifConfig))

	// Upload object that does NOT match prefix
	reqNo, wNo := env.makeS3Request("PUT", "/"+bucketName+"/docs/readme.txt", []byte("readme"))
	env.router.ServeHTTP(wNo, reqNo)
	require.Equal(t, http.StatusOK, wNo.Code)

	// Upload object that DOES match prefix
	reqYes, wYes := env.makeS3Request("PUT", "/"+bucketName+"/images/photo.jpg", []byte("fake image"))
	env.router.ServeHTTP(wYes, reqYes)
	require.Equal(t, http.StatusOK, wYes.Code)

	// Give goroutines a moment to deliver
	time.Sleep(200 * time.Millisecond)
	events := wc.waitForEvents(1, 2*time.Second)

	// Only the images/ object should trigger a notification
	require.Len(t, events, 1, "Only 1 notification should be fired (prefix filter)")
	assert.Equal(t, "images/photo.jpg", events[0].Records[0].S3.Object.Key)
}

// TestNotification_EventFilter_MatchesSuffix verifies that suffix filters work.
func TestNotification_EventFilter_MatchesSuffix(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()
	env.handler.notifHTTPClient = plainHTTPClient()

	ctx := context.Background()
	bucketName := "notif-suffix-bucket"

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	wc := newWebhookCapture()
	defer wc.close()

	// Only notify for .jpg files
	notifConfig := &bucket.NotificationConfig{
		TopicConfigurations: []bucket.NotificationTarget{
			{
				ID:       "suffix-filter",
				Endpoint: wc.server.URL,
				Events:   []string{"s3:ObjectCreated:*"},
				Filter: &bucket.NotificationFilter{
					Suffix: ".jpg",
				},
			},
		},
	}
	require.NoError(t, env.bucketManager.SetNotification(ctx, env.tenantID, bucketName, notifConfig))

	// Upload .txt (should NOT trigger)
	reqTxt, wTxt := env.makeS3Request("PUT", "/"+bucketName+"/file.txt", []byte("text"))
	env.router.ServeHTTP(wTxt, reqTxt)
	require.Equal(t, http.StatusOK, wTxt.Code)

	// Upload .jpg (should trigger)
	reqJpg, wJpg := env.makeS3Request("PUT", "/"+bucketName+"/image.jpg", []byte("image"))
	env.router.ServeHTTP(wJpg, reqJpg)
	require.Equal(t, http.StatusOK, wJpg.Code)

	events := wc.waitForEvents(1, 2*time.Second)
	require.Len(t, events, 1, "Only the .jpg upload should trigger a notification")
	assert.Equal(t, "image.jpg", events[0].Records[0].S3.Object.Key)
}

// TestNotification_NoConfig_NoDelivery verifies no webhook is called when there's no config.
func TestNotification_NoConfig_NoDelivery(t *testing.T) {
	env := setupCompleteS3Environment(t)
	defer env.cleanup()

	ctx := context.Background()
	bucketName := "notif-none-bucket"

	require.NoError(t, env.bucketManager.CreateBucket(ctx, env.tenantID, bucketName, ""))

	wc := newWebhookCapture()
	defer wc.close()

	// Do NOT configure any notifications
	req, w := env.makeS3Request("PUT", "/"+bucketName+"/file.txt", []byte("data"))
	env.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Wait a bit and verify no events were delivered
	time.Sleep(200 * time.Millisecond)
	events := wc.waitForEvents(1, 500*time.Millisecond)
	assert.Len(t, events, 0, "No notifications should fire when bucket has no notification config")
}

// TestNotification_EventMatchWildcard tests eventMatches helper with wildcard patterns.
func TestNotification_EventMatchWildcard(t *testing.T) {
	assert.True(t, eventMatches("s3:ObjectCreated:Put", []string{"s3:ObjectCreated:*"}))
	assert.True(t, eventMatches("s3:ObjectCreated:Copy", []string{"s3:ObjectCreated:*"}))
	assert.True(t, eventMatches("s3:ObjectRemoved:Delete", []string{"s3:ObjectRemoved:*"}))
	assert.False(t, eventMatches("s3:ObjectCreated:Put", []string{"s3:ObjectRemoved:*"}))
	assert.True(t, eventMatches("s3:ObjectCreated:Put", []string{"s3:ObjectCreated:Put"})) // exact match
	assert.False(t, eventMatches("s3:ObjectCreated:Put", []string{}))
}

// TestNotification_KeyMatches tests keyMatches helper.
func TestNotification_KeyMatches(t *testing.T) {
	assert.True(t, keyMatches("images/photo.jpg", &bucket.NotificationFilter{Prefix: "images/"}))
	assert.False(t, keyMatches("docs/readme.txt", &bucket.NotificationFilter{Prefix: "images/"}))
	assert.True(t, keyMatches("photo.jpg", &bucket.NotificationFilter{Suffix: ".jpg"}))
	assert.False(t, keyMatches("photo.png", &bucket.NotificationFilter{Suffix: ".jpg"}))
	assert.True(t, keyMatches("images/photo.jpg", &bucket.NotificationFilter{Prefix: "images/", Suffix: ".jpg"}))
	assert.False(t, keyMatches("images/photo.png", &bucket.NotificationFilter{Prefix: "images/", Suffix: ".jpg"}))
	assert.True(t, keyMatches("anything", nil)) // nil filter passes all
}
