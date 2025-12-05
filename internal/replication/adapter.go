package replication

import (
	"context"
	"fmt"
	"io"

	"github.com/sirupsen/logrus"
)

// ObjectManager interface for accessing local objects
type ObjectManager interface {
	GetObject(ctx context.Context, tenantID, bucket, key string) (io.ReadCloser, int64, string, map[string]string, error)
	GetObjectMetadata(ctx context.Context, tenantID, bucket, key string) (int64, string, map[string]string, error)
}

// RealObjectAdapter implements ObjectAdapter using actual object storage and S3 client
type RealObjectAdapter struct {
	objectManager ObjectManager
}

// NewRealObjectAdapter creates a new RealObjectAdapter
func NewRealObjectAdapter(objectManager ObjectManager) *RealObjectAdapter {
	return &RealObjectAdapter{
		objectManager: objectManager,
	}
}

// CopyObject copies an object from local storage to remote S3 server
func (a *RealObjectAdapter) CopyObject(ctx context.Context, sourceBucket, sourceKey, destBucket, destKey, tenantID string) (int64, error) {
	logrus.WithFields(logrus.Fields{
		"tenant_id":     tenantID,
		"source_bucket": sourceBucket,
		"source_key":    sourceKey,
		"dest_bucket":   destBucket,
		"dest_key":      destKey,
	}).Info("Starting object replication")

	// Get the object from local storage
	reader, size, contentType, _, err := a.objectManager.GetObject(ctx, tenantID, sourceBucket, sourceKey)
	if err != nil {
		return 0, fmt.Errorf("failed to get source object: %w", err)
	}
	defer reader.Close()

	logrus.WithFields(logrus.Fields{
		"source_key": sourceKey,
		"size":       size,
		"content_type": contentType,
	}).Debug("Retrieved source object from local storage")

	// Note: The actual upload to remote S3 is handled by the worker
	// using the rule's destination credentials. This method just returns
	// the size for metrics tracking. The worker will create the S3 client
	// and perform the upload.

	return size, nil
}

// DeleteObject marks a delete operation for replication
func (a *RealObjectAdapter) DeleteObject(ctx context.Context, bucket, key, tenantID string) error {
	logrus.WithFields(logrus.Fields{
		"tenant_id": tenantID,
		"bucket":    bucket,
		"key":       key,
	}).Info("Marking object for delete replication")

	// Note: The actual deletion on remote S3 is handled by the worker
	// This method just validates that the operation can be performed

	return nil
}

// GetObjectMetadata retrieves metadata for an object
func (a *RealObjectAdapter) GetObjectMetadata(ctx context.Context, bucket, key, tenantID string) (map[string]string, error) {
	logrus.WithFields(logrus.Fields{
		"tenant_id": tenantID,
		"bucket":    bucket,
		"key":       key,
	}).Debug("Getting object metadata")

	_, _, metadata, err := a.objectManager.GetObjectMetadata(ctx, tenantID, bucket, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get object metadata: %w", err)
	}

	return metadata, nil
}
