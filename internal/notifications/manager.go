package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
)

const (
	eventVersion    = "2.1"
	eventSource     = "maxiofs:s3"
	s3SchemaVersion = "1.0"
	webhookTimeout  = 10 * time.Second
	maxRetries      = 3
	retryDelay      = 2 * time.Second
)

// Manager handles bucket notification configurations and event sending
type Manager struct {
	kvStore    metadata.RawKVStore
	httpClient *http.Client
	mu         sync.RWMutex
	// Cache of configurations by bucket path (tenantID/bucketName)
	configCache map[string]*NotificationConfiguration
	// bypassSSRFValidation disables the static SSRF check in PutConfiguration.
	// Must only be set in tests; never in production code!
	bypassSSRFValidation bool
}

// NewManager creates a new notification manager.
// The store parameter must implement metadata.RawKVStore (both BadgerStore and
// PebbleStore satisfy this interface).
func NewManager(store metadata.RawKVStore) *Manager {
	// Use a custom dialer that blocks SSRF by refusing connections to
	// loopback, link-local, and private (RFC-1918/RFC-4193) addresses.
	dialer := &net.Dialer{Timeout: webhookTimeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// addr is "host:port" after DNS resolution within the dialer.
			// We resolve the name ourselves first so we can inspect the IP
			// before a connection is established (prevents DNS-rebinding).
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("webhook SSRF guard: invalid address %q: %w", addr, err)
			}

			ips, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("webhook SSRF guard: DNS lookup failed for %q: %w", host, err)
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("webhook SSRF guard: DNS lookup returned no addresses for %q", host)
			}

			for _, ipStr := range ips {
				ip := net.ParseIP(ipStr)
				if ip == nil {
					continue
				}
				if isBlockedIP(ip) {
					return nil, fmt.Errorf("webhook SSRF guard: address %q resolves to blocked IP %s", host, ipStr)
				}
			}

			// Use the first resolved IP to connect (avoid a second resolution).
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
		},
	}

	return &Manager{
		kvStore: store,
		httpClient: &http.Client{
			Timeout:   webhookTimeout,
			Transport: transport,
			// Do not follow redirects — a redirect could lead to an internal address.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return fmt.Errorf("webhook SSRF guard: redirects are not allowed")
			},
		},
		configCache: make(map[string]*NotificationConfiguration),
	}
}

// isBlockedIP returns true for loopback, link-local, and private/internal addresses
// that must not be reached via outbound webhooks.
func isBlockedIP(ip net.IP) bool {
	// Loopback (127.0.0.0/8, ::1)
	if ip.IsLoopback() {
		return true
	}
	// Unspecified (0.0.0.0, ::0)
	if ip.IsUnspecified() {
		return true
	}
	// Link-local (169.254.0.0/16, fe80::/10) — includes AWS/Azure/GCP metadata
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	// Private / site-local (10/8, 172.16/12, 192.168/16, fc00::/7)
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"fc00::/7",
	}
	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// GetConfiguration retrieves the notification configuration for a bucket
func (m *Manager) GetConfiguration(ctx context.Context, tenantID, bucketName string) (*NotificationConfiguration, error) {
	bucketPath := getBucketPath(tenantID, bucketName)

	// Check cache first
	m.mu.RLock()
	if config, ok := m.configCache[bucketPath]; ok {
		m.mu.RUnlock()
		return config, nil
	}
	m.mu.RUnlock()

	// Fetch from metadata store
	key := fmt.Sprintf("notification:%s", bucketPath)
	data, err := m.kvStore.GetRaw(ctx, key)
	if err != nil {
		if err == metadata.ErrNotFound {
			return nil, nil // No configuration set
		}
		return nil, fmt.Errorf("failed to get notification config: %w", err)
	}

	var config NotificationConfiguration
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal notification config: %w", err)
	}

	// Update cache
	m.mu.Lock()
	m.configCache[bucketPath] = &config
	m.mu.Unlock()

	return &config, nil
}

// PutConfiguration stores the notification configuration for a bucket
func (m *Manager) PutConfiguration(ctx context.Context, config *NotificationConfiguration) error {
	bucketPath := getBucketPath(config.TenantID, config.BucketName)

	// Validate configuration
	if err := m.validateConfiguration(config); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	config.UpdatedAt = time.Now()

	// Marshal to JSON
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal notification config: %w", err)
	}

	// Store in metadata
	key := fmt.Sprintf("notification:%s", bucketPath)
	if err := m.kvStore.PutRaw(ctx, key, data); err != nil {
		return fmt.Errorf("failed to store notification config: %w", err)
	}

	// Update cache
	m.mu.Lock()
	m.configCache[bucketPath] = config
	m.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"bucket":    config.BucketName,
		"tenantId":  config.TenantID,
		"ruleCount": len(config.Rules),
	}).Info("Notification configuration updated")

	return nil
}

// DeleteConfiguration removes the notification configuration for a bucket
func (m *Manager) DeleteConfiguration(ctx context.Context, tenantID, bucketName string) error {
	bucketPath := getBucketPath(tenantID, bucketName)

	// Delete from metadata store
	key := fmt.Sprintf("notification:%s", bucketPath)
	if err := m.kvStore.DeleteRaw(ctx, key); err != nil && err != metadata.ErrNotFound {
		return fmt.Errorf("failed to delete notification config: %w", err)
	}

	// Remove from cache
	m.mu.Lock()
	delete(m.configCache, bucketPath)
	m.mu.Unlock()

	logrus.WithFields(logrus.Fields{
		"bucket":   bucketName,
		"tenantId": tenantID,
	}).Info("Notification configuration deleted")

	return nil
}

// SendEvent sends a notification event for matching rules
func (m *Manager) SendEvent(ctx context.Context, info EventInfo) {
	config, err := m.GetConfiguration(ctx, info.TenantID, info.BucketName)
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"bucket":   info.BucketName,
			"tenantId": info.TenantID,
		}).Error("Failed to get notification configuration")
		return
	}

	if config == nil || len(config.Rules) == 0 {
		return // No notifications configured
	}

	// Create event
	event := m.createEvent(info)

	// Send to matching rules asynchronously
	for _, rule := range config.Rules {
		if m.matchesRule(&rule, info) {
			go m.sendWebhook(rule, event)
		}
	}
}

// createEvent creates an Event from EventInfo
func (m *Manager) createEvent(info EventInfo) Event {
	bucketPath := getBucketPath(info.TenantID, info.BucketName)

	event := Event{
		EventVersion: eventVersion,
		EventSource:  eventSource,
		EventTime:    time.Now().UTC(),
		EventName:    info.EventType,
	}

	event.UserIdentity.PrincipalID = info.UserID
	event.RequestParameters.SourceIPAddress = info.SourceIP
	event.ResponseElements.XAmzRequestID = info.RequestID

	event.S3.S3SchemaVersion = s3SchemaVersion
	event.S3.Bucket.Name = info.BucketName
	event.S3.Bucket.OwnerIdentity.PrincipalID = info.TenantID
	event.S3.Bucket.ARN = fmt.Sprintf("arn:aws:s3:::%s", bucketPath)

	event.S3.Object.Key = info.ObjectKey
	event.S3.Object.Size = info.Size
	event.S3.Object.ETag = info.ETag
	event.S3.Object.VersionID = info.VersionID
	event.S3.Object.Sequencer = generateSequencer()

	return event
}

// matchesRule checks if an event matches a notification rule
func (m *Manager) matchesRule(rule *NotificationRule, info EventInfo) bool {
	if !rule.Enabled {
		return false
	}

	// Check event type match
	eventMatches := false
	for _, ruleEvent := range rule.Events {
		if matchesEventType(ruleEvent, info.EventType) {
			eventMatches = true
			break
		}
	}
	if !eventMatches {
		return false
	}

	// Check prefix filter
	if rule.FilterPrefix != "" && !strings.HasPrefix(info.ObjectKey, rule.FilterPrefix) {
		return false
	}

	// Check suffix filter
	if rule.FilterSuffix != "" && !strings.HasSuffix(info.ObjectKey, rule.FilterSuffix) {
		return false
	}

	return true
}

// sendWebhook sends the event to a webhook URL with retries
func (m *Manager) sendWebhook(rule NotificationRule, event Event) {
	payload := WebhookPayload{
		Records: []Event{event},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		logrus.WithError(err).Error("Failed to marshal webhook payload")
		return
	}

	// Try sending with retries
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryDelay)
		}

		req, err := http.NewRequest("POST", rule.WebhookURL, bytes.NewReader(body))
		if err != nil {
			lastErr = err
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "MaxIOFS/1.0")
		req.Header.Set("X-MaxIOFS-Event", string(event.EventName))
		req.Header.Set("X-MaxIOFS-Bucket", event.S3.Bucket.Name)

		// Add custom headers
		for key, value := range rule.CustomHeaders {
			req.Header.Set(key, value)
		}

		resp, err := m.httpClient.Do(req)
		if err != nil {
			lastErr = err
			logrus.WithError(err).WithFields(logrus.Fields{
				"url":     rule.WebhookURL,
				"attempt": attempt + 1,
			}).Warn("Failed to send webhook")
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			_ = resp.Body.Close()
			logrus.WithFields(logrus.Fields{
				"url":        rule.WebhookURL,
				"event":      event.EventName,
				"bucket":     event.S3.Bucket.Name,
				"key":        event.S3.Object.Key,
				"statusCode": resp.StatusCode,
			}).Debug("Webhook sent successfully")
			return
		}

		lastErr = fmt.Errorf("webhook returned status %d", resp.StatusCode)
		_ = resp.Body.Close()
		logrus.WithFields(logrus.Fields{
			"url":        rule.WebhookURL,
			"statusCode": resp.StatusCode,
			"attempt":    attempt + 1,
		}).Warn("Webhook returned non-2xx status")
	}

	logrus.WithError(lastErr).WithFields(logrus.Fields{
		"url":    rule.WebhookURL,
		"event":  event.EventName,
		"bucket": event.S3.Bucket.Name,
		"key":    event.S3.Object.Key,
	}).Error("Failed to send webhook after all retries")
}

// Helper functions

func getBucketPath(tenantID, bucketName string) string {
	if tenantID != "" {
		return tenantID + "/" + bucketName
	}
	return bucketName
}

func matchesEventType(ruleEvent, actualEvent EventType) bool {
	// Exact match
	if ruleEvent == actualEvent {
		return true
	}

	// Wildcard match (e.g., s3:ObjectCreated:* matches s3:ObjectCreated:Put)
	if strings.HasSuffix(string(ruleEvent), ":*") {
		prefix := strings.TrimSuffix(string(ruleEvent), ":*")
		return strings.HasPrefix(string(actualEvent), prefix)
	}

	return false
}

func (m *Manager) validateConfiguration(config *NotificationConfiguration) error {
	if config.BucketName == "" {
		return fmt.Errorf("bucket name is required")
	}

	if len(config.Rules) == 0 {
		return fmt.Errorf("at least one rule is required")
	}

	for i, rule := range config.Rules {
		if rule.ID == "" {
			return fmt.Errorf("rule %d: ID is required", i)
		}
		if rule.WebhookURL == "" {
			return fmt.Errorf("rule %d: webhook URL is required", i)
		}
		if !strings.HasPrefix(rule.WebhookURL, "http://") && !strings.HasPrefix(rule.WebhookURL, "https://") {
			return fmt.Errorf("rule %d: webhook URL must start with http:// or https://", i)
		}
		// SSRF guard: reject URLs that target loopback or private addresses.
		// Skipped when bypassSSRFValidation is set (tests only).
		if !m.bypassSSRFValidation {
			if err := validateWebhookURL(rule.WebhookURL); err != nil {
				return fmt.Errorf("rule %d: %w", i, err)
			}
		}
		if len(rule.Events) == 0 {
			return fmt.Errorf("rule %d: at least one event is required", i)
		}
	}

	return nil
}

// validateWebhookURL performs a static SSRF check on the given webhook URL.
// It parses the hostname and, if it is a literal IP, verifies it is not a
// blocked (private/loopback/link-local) address.  DNS-based checks are
// performed at delivery time by the custom HTTP transport in NewManager.
func validateWebhookURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("webhook URL missing host")
	}

	// Reject obviously dangerous hostnames outright.
	lower := strings.ToLower(host)
	blocked := []string{"localhost", "ip6-localhost", "ip6-loopback", "broadcasthost"}
	for _, b := range blocked {
		if lower == b {
			return fmt.Errorf("webhook URL target %q is not allowed", host)
		}
	}

	// If the host is a literal IP, validate it immediately.
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("webhook URL resolves to a blocked IP address: %s", ip)
		}
	}

	return nil
}

func generateSequencer() string {
	// Generate a unique sequencer (similar to AWS S3)
	// Using timestamp + random UUID
	timestamp := time.Now().UnixNano()
	id := uuid.New().String()
	return fmt.Sprintf("%016X%s", timestamp, strings.ReplaceAll(id, "-", ""))
}
