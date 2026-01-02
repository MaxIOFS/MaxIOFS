package cluster

import (
	"context"
	"fmt"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/sirupsen/logrus"
)

const (
	// MetadataKeyLocation is the metadata key for storing bucket location (node ID)
	MetadataKeyLocation = "cluster:location"
)

// BucketLocationManager handles bucket location tracking in cluster
type BucketLocationManager struct {
	bucketManager bucket.Manager
	cache         *BucketLocationCache
	localNodeID   string
	log           *logrus.Entry
}

// NewBucketLocationManager creates a new bucket location manager
func NewBucketLocationManager(bucketManager bucket.Manager, cache *BucketLocationCache, localNodeID string) *BucketLocationManager {
	return &BucketLocationManager{
		bucketManager: bucketManager,
		cache:         cache,
		localNodeID:   localNodeID,
		log:           logrus.WithField("component", "bucket-location-manager"),
	}
}

// GetBucketLocation retrieves the node ID where a bucket is located
// It first checks the cache, then falls back to querying the bucket metadata
func (blm *BucketLocationManager) GetBucketLocation(ctx context.Context, tenantID, bucketName string) (string, error) {
	// Try cache first
	if nodeID := blm.cache.Get(bucketName); nodeID != "" {
		blm.log.WithFields(logrus.Fields{
			"bucket":  bucketName,
			"node_id": nodeID,
			"source":  "cache",
		}).Debug("Bucket location retrieved from cache")
		return nodeID, nil
	}

	// Cache miss - query bucket metadata
	bkt, err := blm.bucketManager.GetBucketInfo(ctx, tenantID, bucketName)
	if err != nil {
		return "", fmt.Errorf("failed to get bucket: %w", err)
	}

	// Get location from metadata
	location, exists := bkt.Metadata[MetadataKeyLocation]
	if !exists || location == "" {
		// If no location is set, assume it's on the local node (backwards compatibility)
		location = blm.localNodeID
		blm.log.WithFields(logrus.Fields{
			"bucket": bucketName,
			"node_id": location,
		}).Warn("Bucket has no location metadata, assuming local node")
	}

	// Update cache
	blm.cache.Set(bucketName, location)

	blm.log.WithFields(logrus.Fields{
		"bucket":  bucketName,
		"node_id": location,
		"source":  "metadata",
	}).Debug("Bucket location retrieved from metadata")

	return location, nil
}

// SetBucketLocation updates the location of a bucket
// This is used during bucket migration to change the bucket's home node
func (blm *BucketLocationManager) SetBucketLocation(ctx context.Context, tenantID, bucketName, nodeID string) error {
	// Get the bucket
	bkt, err := blm.bucketManager.GetBucketInfo(ctx, tenantID, bucketName)
	if err != nil {
		return fmt.Errorf("failed to get bucket: %w", err)
	}

	// Ensure metadata map exists
	if bkt.Metadata == nil {
		bkt.Metadata = make(map[string]string)
	}

	// Update location in metadata
	oldLocation := bkt.Metadata[MetadataKeyLocation]
	bkt.Metadata[MetadataKeyLocation] = nodeID

	// Update bucket metadata
	if err := blm.bucketManager.UpdateBucket(ctx, tenantID, bucketName, bkt); err != nil {
		return fmt.Errorf("failed to update bucket metadata: %w", err)
	}

	// Invalidate cache to force refresh
	blm.cache.Delete(bucketName)

	blm.log.WithFields(logrus.Fields{
		"bucket":       bucketName,
		"old_location": oldLocation,
		"new_location": nodeID,
	}).Info("Bucket location updated")

	return nil
}

// InitializeBucketLocation sets the initial location for a bucket if not already set
// This is typically called when a bucket is first created
func (blm *BucketLocationManager) InitializeBucketLocation(ctx context.Context, tenantID, bucketName, nodeID string) error {
	bkt, err := blm.bucketManager.GetBucketInfo(ctx, tenantID, bucketName)
	if err != nil {
		return fmt.Errorf("failed to get bucket: %w", err)
	}

	// Check if location is already set
	if bkt.Metadata == nil {
		bkt.Metadata = make(map[string]string)
	}

	if _, exists := bkt.Metadata[MetadataKeyLocation]; exists {
		// Location already set, don't override
		return nil
	}

	// Set initial location
	bkt.Metadata[MetadataKeyLocation] = nodeID

	if err := blm.bucketManager.UpdateBucket(ctx, tenantID, bucketName, bkt); err != nil {
		return fmt.Errorf("failed to initialize bucket location: %w", err)
	}

	// Update cache
	blm.cache.Set(bucketName, nodeID)

	blm.log.WithFields(logrus.Fields{
		"bucket":  bucketName,
		"node_id": nodeID,
	}).Info("Bucket location initialized")

	return nil
}

// InvalidateCache removes a bucket from the location cache
// This should be called after any operation that might change the bucket location
func (blm *BucketLocationManager) InvalidateCache(bucketName string) {
	blm.cache.Delete(bucketName)
	blm.log.WithField("bucket", bucketName).Debug("Bucket location cache invalidated")
}
