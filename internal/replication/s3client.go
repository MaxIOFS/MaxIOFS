package replication

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/sirupsen/logrus"
)

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

// NewS3RemoteClient creates a new S3 client configured for a remote endpoint
func NewS3RemoteClient(endpoint, region, accessKey, secretKey string) *S3RemoteClient {
	// Create custom endpoint resolver
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:               endpoint,
			HostnameImmutable: true,
			SigningRegion:     region,
		}, nil
	})

	// Create AWS config with static credentials
	cfg := aws.Config{
		Region:                      region,
		Credentials:                 credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		EndpointResolverWithOptions: customResolver,
	}

	// Create S3 client
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true // Use path-style URLs for compatibility
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
		"endpoint":   c.endpoint,
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
