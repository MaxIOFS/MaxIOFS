package replication

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/sirupsen/logrus"
)

// validateReplicationEndpoint ensures the destination endpoint is a valid http/https
// URL that does not point to loopback, private, link-local, or AWS/GCP metadata addresses.
// This prevents an admin with replication-rule write access from using the replication
// worker as an SSRF proxy to reach internal services.
// An empty endpoint is allowed (the rule will simply fail at connection time without
// causing any unintended outbound request).
func validateReplicationEndpoint(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid replication destination endpoint: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("replication destination endpoint must use http or https scheme, got %q", u.Scheme)
	}

	// Static check on the hostname as supplied (catches literal IPs and obvious names).
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("replication destination endpoint resolves to a private/internal address: %s", host)
		}
	}
	return nil
}

// ssrfBlockingDialer returns a DialContext function that resolves hostnames before
// connecting and rejects any IP in loopback, private, link-local, or unspecified ranges.
func ssrfBlockingReplicationDialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid replication address %q: %w", addr, err)
		}
		ips, err := net.DefaultResolver.LookupHost(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve replication host %q: %w", host, err)
		}
		for _, ipStr := range ips {
			ip := net.ParseIP(ipStr)
			if isBlockedIP(ip) {
				return nil, fmt.Errorf("replication endpoint resolves to a private/internal address: %s", ipStr)
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0], port))
	}
}

// isBlockedIP returns true if ip falls within loopback, unspecified, link-local, private,
// or cloud-metadata ranges that must never be reachable from user-supplied endpoints.
func isBlockedIP(ip net.IP) bool {
	privateRanges := []string{
		"127.0.0.0/8", "::1/128",
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		"fc00::/7", "fe80::/10",
		"169.254.0.0/16", // AWS/GCP metadata
		"0.0.0.0/8",
	}
	for _, cidr := range privateRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network != nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

// S3Client is an interface for S3 operations (for testing)
type S3Client interface {
	PutObject(ctx context.Context, bucket, key string, data io.Reader, size int64, contentType string, metadata map[string]string) error
	DeleteObject(ctx context.Context, bucket, key string) error
	HeadObject(ctx context.Context, bucket, key string) (map[string]string, int64, error)
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, int64, error)
	CopyObject(ctx context.Context, sourceBucket, sourceKey, destBucket, destKey string) error
	ListObjects(ctx context.Context, bucket, prefix string, maxKeys int32) ([]types.Object, error)
	TestConnection(ctx context.Context) error
}

// S3RemoteClient is a client for communicating with remote S3-compatible servers
type S3RemoteClient struct {
	client   *s3.Client
	endpoint string
	region   string
}

// NewS3RemoteClient creates a new S3 client configured for a remote endpoint.
// The HTTP transport uses an SSRF-blocking dialer that prevents the replication
// worker from being used as a proxy to reach internal/private addresses.
func NewS3RemoteClient(endpoint, region, accessKey, secretKey string) *S3RemoteClient {
	// Build an HTTP client that blocks connections to private/internal IPs.
	ssrfSafeTransport := &http.Transport{
		DialContext:           ssrfBlockingReplicationDialer(),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	ssrfSafeClient := &http.Client{
		Transport: ssrfSafeTransport,
		Timeout:   120 * time.Second,
		// Block redirects to prevent redirect-based SSRF bypass.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("replication client does not follow redirects")
		},
	}

	cfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		HTTPClient:  ssrfSafeClient,
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	return &S3RemoteClient{
		client:   client,
		endpoint: endpoint,
		region:   region,
	}
}

// PutObject uploads an object to the remote S3 server
func (c *S3RemoteClient) PutObject(ctx context.Context, bucket, key string, data io.Reader, size int64, contentType string, metadata map[string]string) error {
	logrus.WithFields(logrus.Fields{
		"endpoint": c.endpoint,
		"bucket":   bucket,
		"key":      key,
		"size":     size,
	}).Debug("Uploading object to remote S3")

	// Convert metadata to AWS format
	awsMetadata := make(map[string]string)
	for k, v := range metadata {
		awsMetadata[k] = v
	}

	input := &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          data,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
		Metadata:      awsMetadata,
	}

	_, err := c.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to put object: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"endpoint": c.endpoint,
		"bucket":   bucket,
		"key":      key,
	}).Info("Successfully uploaded object to remote S3")

	return nil
}

// DeleteObject deletes an object from the remote S3 server
func (c *S3RemoteClient) DeleteObject(ctx context.Context, bucket, key string) error {
	logrus.WithFields(logrus.Fields{
		"endpoint": c.endpoint,
		"bucket":   bucket,
		"key":      key,
	}).Debug("Deleting object from remote S3")

	input := &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	_, err := c.client.DeleteObject(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"endpoint": c.endpoint,
		"bucket":   bucket,
		"key":      key,
	}).Info("Successfully deleted object from remote S3")

	return nil
}

// HeadObject checks if an object exists and returns its metadata
func (c *S3RemoteClient) HeadObject(ctx context.Context, bucket, key string) (map[string]string, int64, error) {
	logrus.WithFields(logrus.Fields{
		"endpoint": c.endpoint,
		"bucket":   bucket,
		"key":      key,
	}).Debug("Checking object existence on remote S3")

	input := &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	result, err := c.client.HeadObject(ctx, input)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to head object: %w", err)
	}

	size := int64(0)
	if result.ContentLength != nil {
		size = *result.ContentLength
	}

	return result.Metadata, size, nil
}

// GetObject downloads an object from the remote S3 server
func (c *S3RemoteClient) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, int64, error) {
	logrus.WithFields(logrus.Fields{
		"endpoint": c.endpoint,
		"bucket":   bucket,
		"key":      key,
	}).Debug("Getting object from remote S3")

	input := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	result, err := c.client.GetObject(ctx, input)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get object: %w", err)
	}

	size := int64(0)
	if result.ContentLength != nil {
		size = *result.ContentLength
	}

	return result.Body, size, nil
}

// CopyObject copies an object within the remote S3 server
func (c *S3RemoteClient) CopyObject(ctx context.Context, sourceBucket, sourceKey, destBucket, destKey string) error {
	logrus.WithFields(logrus.Fields{
		"endpoint":      c.endpoint,
		"source_bucket": sourceBucket,
		"source_key":    sourceKey,
		"dest_bucket":   destBucket,
		"dest_key":      destKey,
	}).Debug("Copying object on remote S3")

	copySource := fmt.Sprintf("%s/%s", sourceBucket, sourceKey)

	input := &s3.CopyObjectInput{
		Bucket:     aws.String(destBucket),
		Key:        aws.String(destKey),
		CopySource: aws.String(copySource),
	}

	_, err := c.client.CopyObject(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to copy object: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"endpoint":    c.endpoint,
		"dest_bucket": destBucket,
		"dest_key":    destKey,
	}).Info("Successfully copied object on remote S3")

	return nil
}

// ListObjects lists objects in a bucket on the remote S3 server
func (c *S3RemoteClient) ListObjects(ctx context.Context, bucket, prefix string, maxKeys int32) ([]types.Object, error) {
	logrus.WithFields(logrus.Fields{
		"endpoint": c.endpoint,
		"bucket":   bucket,
		"prefix":   prefix,
		"max_keys": maxKeys,
	}).Debug("Listing objects on remote S3")

	input := &s3.ListObjectsV2Input{
		Bucket:  aws.String(bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(maxKeys),
	}

	result, err := c.client.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	return result.Contents, nil
}

// TestConnection tests the connection to the remote S3 server
func (c *S3RemoteClient) TestConnection(ctx context.Context) error {
	logrus.WithFields(logrus.Fields{
		"endpoint": c.endpoint,
	}).Debug("Testing connection to remote S3")

	// Try to list buckets as a connectivity test
	input := &s3.ListBucketsInput{}

	_, err := c.client.ListBuckets(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to connect to remote S3: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"endpoint": c.endpoint,
	}).Info("Successfully connected to remote S3")

	return nil
}

// PutObjectFromBytes is a helper to upload data from a byte slice
func (c *S3RemoteClient) PutObjectFromBytes(ctx context.Context, bucket, key string, data []byte, contentType string, metadata map[string]string) error {
	reader := bytes.NewReader(data)
	return c.PutObject(ctx, bucket, key, reader, int64(len(data)), contentType, metadata)
}
