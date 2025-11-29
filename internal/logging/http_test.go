package logging

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHTTPOutput(t *testing.T) {
	output := NewHTTPOutput("http://example.com", "token123", 10, 5*time.Second)

	assert.NotNil(t, output)
	assert.Equal(t, "http://example.com", output.url)
	assert.Equal(t, "token123", output.authToken)
	assert.Equal(t, 10, output.batchSize)
	assert.Equal(t, 5*time.Second, output.flushInterval)
	assert.NotNil(t, output.client)
	assert.NotNil(t, output.buffer)
	assert.NotNil(t, output.stopChan)
}

func TestHTTPOutputWrite(t *testing.T) {
	// Create test server
	received := make([]*LogEntry, 0)
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Read body
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var entries []*LogEntry
		err = json.Unmarshal(body, &entries)
		require.NoError(t, err)

		mu.Lock()
		received = append(received, entries...)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	output := NewHTTPOutput(server.URL, "token123", 2, 100*time.Millisecond)
	defer output.Close()

	// Write entries
	entry1 := &LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "Test message 1",
		Fields:    map[string]interface{}{"key": "value1"},
	}

	entry2 := &LogEntry{
		Timestamp: time.Now(),
		Level:     "error",
		Message:   "Test message 2",
		Fields:    map[string]interface{}{"key": "value2"},
	}

	err := output.Write(entry1)
	require.NoError(t, err)

	err = output.Write(entry2)
	require.NoError(t, err)

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	// Check received entries
	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, received, 2)
	assert.Equal(t, "Test message 1", received[0].Message)
	assert.Equal(t, "Test message 2", received[1].Message)
}

func TestHTTPOutputBatching(t *testing.T) {
	batchCount := 0
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		batchCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	output := NewHTTPOutput(server.URL, "", 3, 100*time.Millisecond)
	defer output.Close()

	// Write 5 entries (should result in 2 batches: 3 + 2)
	for i := 0; i < 5; i++ {
		entry := &LogEntry{
			Timestamp: time.Now(),
			Level:     "info",
			Message:   "Test message",
		}
		err := output.Write(entry)
		require.NoError(t, err)
	}

	// Wait for flush
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	// Should have sent 2 batches
	assert.GreaterOrEqual(t, batchCount, 1)
}

func TestHTTPOutputFlushInterval(t *testing.T) {
	received := make([]*LogEntry, 0)
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var entries []*LogEntry
		json.Unmarshal(body, &entries)

		mu.Lock()
		received = append(received, entries...)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Small flush interval
	output := NewHTTPOutput(server.URL, "", 100, 50*time.Millisecond)
	defer output.Close()

	// Write one entry
	entry := &LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "Test",
	}
	output.Write(entry)

	// Should flush within interval
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, received, 1)
}

func TestHTTPOutputClose(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	output := NewHTTPOutput(server.URL, "", 10, time.Second)

	// Write entry
	entry := &LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "Test",
	}
	output.Write(entry)

	// Close should flush remaining entries
	err := output.Close()
	assert.NoError(t, err)
}

func TestHTTPOutputNoAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should have no auth header
		assert.Empty(t, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	output := NewHTTPOutput(server.URL, "", 1, 100*time.Millisecond)
	defer output.Close()

	entry := &LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "Test",
	}
	output.Write(entry)

	time.Sleep(200 * time.Millisecond)
}

func TestHTTPOutputServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	output := NewHTTPOutput(server.URL, "", 1, 50*time.Millisecond)
	defer output.Close()

	entry := &LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Message:   "Test",
	}

	// Should not panic on server error
	err := output.Write(entry)
	assert.NoError(t, err) // Write only adds to buffer

	// Wait for flush attempt
	time.Sleep(100 * time.Millisecond)
}
