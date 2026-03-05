package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
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

// ssrfBlockingDialer returns a net.Dialer DialContext function that rejects
// connections to loopback, private, link-local, and unspecified IP ranges
// to prevent SSRF attacks via the log HTTP forwarder target URL.
func ssrfBlockingDialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	privateRanges := []string{
		"127.0.0.0/8", "::1/128",
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		"fc00::/7", "fe80::/10",
		"169.254.0.0/16", // AWS/GCP metadata
		"0.0.0.0/8",
	}
	nets := make([]*net.IPNet, 0, len(privateRanges))
	for _, cidr := range privateRanges {
		_, n, _ := net.ParseCIDR(cidr)
		if n != nil {
			nets = append(nets, n)
		}
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid address %q: %w", addr, err)
		}
		ips, err := net.DefaultResolver.LookupHost(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve %q: %w", host, err)
		}
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			for _, n := range nets {
				if n.Contains(ip) {
					return nil, fmt.Errorf("log HTTP forwarder target resolves to a private/internal address: %s", ipStr)
				}
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
	}
}

// validateLogURL ensures the URL scheme is http or https (rejects file://, etc.).
func validateLogURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid log HTTP target URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("log HTTP target URL must use http or https scheme, got %q", u.Scheme)
	}
	return nil
}

// NewHTTPOutput creates a new HTTP output
func NewHTTPOutput(rawURL, authToken string, batchSize int, flushInterval time.Duration) *HTTPOutput {
	output := &HTTPOutput{
		url:           rawURL,
		authToken:     authToken,
		batchSize:     batchSize,
		flushInterval: flushInterval,
		client: &http.Client{
			Timeout: 10 * time.Second,
			// Block redirects to prevent redirect-based SSRF bypass
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return fmt.Errorf("log HTTP forwarder does not follow redirects")
			},
			Transport: &http.Transport{
				DialContext: ssrfBlockingDialer(),
			},
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

	// Send in background to avoid blocking, track goroutine for graceful shutdown
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.sendBatch(entries)
	}()

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
