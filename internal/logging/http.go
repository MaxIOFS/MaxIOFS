package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// HTTPOutput sends logs to an HTTP endpoint
type HTTPOutput struct {
	url           string
	authToken     string
	batchSize     int
	flushInterval time.Duration
	client        *http.Client
	buffer        []*LogEntry
	mu            sync.Mutex
	stopChan      chan struct{}
	wg            sync.WaitGroup
}

// NewHTTPOutput creates a new HTTP output
func NewHTTPOutput(url, authToken string, batchSize int, flushInterval time.Duration) *HTTPOutput {
	output := &HTTPOutput{
		url:           url,
		authToken:     authToken,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		buffer:   make([]*LogEntry, 0, batchSize),
		stopChan: make(chan struct{}),
	}

	// Start background flusher
	output.wg.Add(1)
	go output.flusher()

	return output
}

// Write adds a log entry to the buffer
func (h *HTTPOutput) Write(entry *LogEntry) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.buffer = append(h.buffer, entry)

	// Flush if buffer is full
	if len(h.buffer) >= h.batchSize {
		return h.flushLocked()
	}

	return nil
}

// flusher periodically flushes the buffer
func (h *HTTPOutput) flusher() {
	defer h.wg.Done()

	ticker := time.NewTicker(h.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.mu.Lock()
			if len(h.buffer) > 0 {
				_ = h.flushLocked()
			}
			h.mu.Unlock()

		case <-h.stopChan:
			// Final flush on shutdown
			h.mu.Lock()
			if len(h.buffer) > 0 {
				_ = h.flushLocked()
			}
			h.mu.Unlock()
			return
		}
	}
}

// flushLocked sends buffered logs to HTTP endpoint (caller must hold lock)
func (h *HTTPOutput) flushLocked() error {
	if len(h.buffer) == 0 {
		return nil
	}

	// Copy buffer
	entries := make([]*LogEntry, len(h.buffer))
	copy(entries, h.buffer)
	h.buffer = h.buffer[:0] // Clear buffer

	// Send in background to avoid blocking
	go h.sendBatch(entries)

	return nil
}

// sendBatch sends a batch of log entries to the HTTP endpoint
func (h *HTTPOutput) sendBatch(entries []*LogEntry) error {
	// Marshal entries to JSON
	data, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("failed to marshal log entries: %w", err)
	}

	// Create request
	req, err := http.NewRequest("POST", h.url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	if h.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.authToken)
	}

	// Send request
	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send logs: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP endpoint returned status %d", resp.StatusCode)
	}

	return nil
}

// Close stops the flusher and closes the output
func (h *HTTPOutput) Close() error {
	close(h.stopChan)
	h.wg.Wait()
	return nil
}
