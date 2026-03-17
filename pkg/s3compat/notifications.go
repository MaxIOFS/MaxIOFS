package s3compat

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/sirupsen/logrus"
)

// notifSSRFBlockingClient returns an HTTP client that refuses connections to
// loopback, link-local, and private (RFC-1918/RFC-4193) addresses.
// This prevents SSRF attacks where a user-controlled webhook endpoint targets
// internal services (cloud metadata, private network, localhost).
func notifSSRFBlockingClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("webhook SSRF guard: invalid address %q: %w", addr, err)
			}
			ips, err := net.DefaultResolver.LookupHost(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("webhook SSRF guard: DNS lookup failed for %q: %w", host, err)
			}
			for _, ipStr := range ips {
				ip := net.ParseIP(ipStr)
				if ip == nil {
					continue
				}
				if notifIsBlockedIP(ip) {
					return nil, fmt.Errorf("webhook SSRF guard: address %q resolves to blocked IP %s", host, ipStr)
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
		},
	}
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
		// Block redirects — a redirect could lead to an internal address.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("webhook SSRF guard: redirects are not allowed")
		},
	}
}

// notifIsBlockedIP returns true for loopback, link-local, and private/internal addresses.
func notifIsBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	for _, cidr := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"} {
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

// validateNotificationEndpoint performs a static SSRF check on a webhook endpoint URL.
// It validates the scheme and, if the host is a literal IP, checks it is not blocked.
// DNS-based checks happen at delivery time via notifSSRFBlockingClient.
func validateNotificationEndpoint(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("endpoint URL is required")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return fmt.Errorf("endpoint URL must start with http:// or https://")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid endpoint URL: %w", err)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("endpoint URL missing host")
	}
	for _, blocked := range []string{"localhost", "ip6-localhost", "ip6-loopback", "broadcasthost"} {
		if strings.ToLower(host) == blocked {
			return fmt.Errorf("endpoint URL target %q is not allowed", host)
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		if notifIsBlockedIP(ip) {
			return fmt.Errorf("endpoint URL resolves to a blocked IP address: %s", ip)
		}
	}
	return nil
}

// s3EventRecord is the S3-compatible event payload sent to notification endpoints.
type s3EventRecord struct {
	EventVersion string          `json:"eventVersion"`
	EventSource  string          `json:"eventSource"`
	AWSRegion    string          `json:"awsRegion"`
	EventTime    string          `json:"eventTime"`
	EventName    string          `json:"eventName"`
	S3           s3EventS3Detail `json:"s3"`
}

type s3EventS3Detail struct {
	S3SchemaVersion string          `json:"s3SchemaVersion"`
	ConfigurationID string          `json:"configurationId"`
	Bucket          s3EventBucket   `json:"bucket"`
	Object          s3EventObject   `json:"object"`
}

type s3EventBucket struct {
	Name          string                `json:"name"`
	OwnerIdentity s3EventOwnerIdentity  `json:"ownerIdentity"`
	ARN           string                `json:"arn"`
}

type s3EventOwnerIdentity struct {
	PrincipalID string `json:"principalId"`
}

type s3EventObject struct {
	Key       string `json:"key"`
	Size      int64  `json:"size,omitempty"`
	ETag      string `json:"eTag,omitempty"`
	Sequencer string `json:"sequencer"`
}

type s3EventPayload struct {
	Records []s3EventRecord `json:"Records"`
}

// fireNotifications looks up the bucket's notification config and dispatches matching
// events to configured webhook endpoints. The dispatch is non-blocking (goroutine).
func (h *Handler) fireNotifications(ctx context.Context, bucketName, tenantID, objectKey, eventName, etag string, size int64) {
	cfg, err := h.bucketManager.GetNotification(ctx, tenantID, bucketName)
	if err != nil || cfg == nil {
		return
	}

	// Collect all targets from all three target types.
	var targets []bucket.NotificationTarget
	targets = append(targets, cfg.TopicConfigurations...)
	targets = append(targets, cfg.QueueConfigurations...)
	targets = append(targets, cfg.LambdaConfigurations...)

	if len(targets) == 0 {
		return
	}

	record := s3EventRecord{
		EventVersion: "2.1",
		EventSource:  "aws:s3",
		AWSRegion:    "us-east-1",
		EventTime:    time.Now().UTC().Format(time.RFC3339Nano),
		EventName:    eventName,
		S3: s3EventS3Detail{
			S3SchemaVersion: "1.0",
			Bucket: s3EventBucket{
				Name: bucketName,
				ARN:  "arn:aws:s3:::" + bucketName,
			},
			Object: s3EventObject{
				Key:       objectKey,
				Size:      size,
				ETag:      etag,
				Sequencer: randomSequencer(),
			},
		},
	}

	payload := s3EventPayload{Records: []s3EventRecord{record}}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		logrus.WithError(err).Error("notifications: failed to marshal event payload")
		return
	}

	for _, target := range targets {
		if target.Endpoint == "" {
			continue
		}
		if !eventMatches(eventName, target.Events) {
			continue
		}
		if !keyMatches(objectKey, target.Filter) {
			continue
		}
		record.S3.ConfigurationID = target.ID
		endpoint := target.Endpoint
		client := h.notifHTTPClient
		if client == nil {
			client = notifSSRFBlockingClient()
		}
		go deliverNotification(client, endpoint, payloadJSON)
	}
}

// eventMatches reports whether eventName matches any of the configured event patterns.
// S3 event patterns support a trailing wildcard: "s3:ObjectCreated:*".
func eventMatches(eventName string, events []string) bool {
	for _, e := range events {
		if e == eventName {
			return true
		}
		if strings.HasSuffix(e, "*") {
			prefix := strings.TrimSuffix(e, "*")
			if strings.HasPrefix(eventName, prefix) {
				return true
			}
		}
	}
	return false
}

// keyMatches reports whether objectKey satisfies the filter's prefix/suffix constraints.
func keyMatches(key string, f *bucket.NotificationFilter) bool {
	if f == nil {
		return true
	}
	if f.Prefix != "" && !strings.HasPrefix(key, f.Prefix) {
		return false
	}
	if f.Suffix != "" && !strings.HasSuffix(key, f.Suffix) {
		return false
	}
	return true
}

// deliverNotification sends the event payload to a webhook URL via HTTP POST.
// Uses an SSRF-blocking HTTP client that rejects private/internal addresses.
// Failures are logged but not retried.
func deliverNotification(client *http.Client, endpoint string, payload []byte) {
	resp, err := client.Post(endpoint, "application/json", bytes.NewReader(payload))
	if err != nil {
		logrus.WithError(err).WithField("endpoint", endpoint).Warn("notifications: webhook delivery failed")
		return
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		logrus.WithFields(logrus.Fields{
			"endpoint": endpoint,
			"status":   resp.StatusCode,
		}).Warn("notifications: webhook returned error status")
	}
}

// randomSequencer generates a random hex sequencer value (matches S3 format).
func randomSequencer() string {
	b := make([]byte, 8)
	rand.Read(b) //nolint:errcheck
	return strings.ToUpper(hex.EncodeToString(b))
}
