package lifecycle

import (
	"context"
	"time"

	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/sirupsen/logrus"
)

// Worker handles lifecycle policy execution
type Worker struct {
	bucketManager bucket.Manager
	objectManager object.Manager
	metadataStore metadata.Store
	ticker        *time.Ticker
	stopChan      chan struct{}
}

// NewWorker creates a new lifecycle worker
func NewWorker(bucketManager bucket.Manager, objectManager object.Manager, metadataStore metadata.Store) *Worker {
	return &Worker{
		bucketManager: bucketManager,
		objectManager: objectManager,
		metadataStore: metadataStore,
		stopChan:      make(chan struct{}),
	}
}

// Start begins the lifecycle worker
func (w *Worker) Start(ctx context.Context, interval time.Duration) {
	w.ticker = time.NewTicker(interval)

	logrus.WithField("interval", interval).Info("Lifecycle worker started")

	// Run immediately on start
	go w.processLifecyclePolicies(ctx)

	go func() {
		for {
			select {
			case <-w.ticker.C:
				w.processLifecyclePolicies(ctx)
			case <-w.stopChan:
				w.ticker.Stop()
				logrus.Info("Lifecycle worker stopped")
				return
			case <-ctx.Done():
				w.ticker.Stop()
				logrus.Info("Lifecycle worker stopped due to context cancellation")
				return
			}
		}
	}()
}

// Stop stops the lifecycle worker
func (w *Worker) Stop() {
	close(w.stopChan)
}

// processLifecyclePolicies processes all lifecycle policies for all buckets
func (w *Worker) processLifecyclePolicies(ctx context.Context) {
	logrus.Debug("Processing lifecycle policies...")

	// Get all buckets
	buckets, err := w.bucketManager.ListBuckets(ctx, "") // Empty tenantID lists all buckets
	if err != nil {
		logrus.WithError(err).Error("Failed to list buckets for lifecycle processing")
		return
	}

	for _, bkt := range buckets {
		// Get bucket details to check for lifecycle config
		bucketInfo, err := w.bucketManager.GetBucketInfo(ctx, bkt.TenantID, bkt.Name)
		if err != nil {
			logrus.WithError(err).WithField("bucket", bkt.Name).Warn("Failed to get bucket info")
			continue
		}

		// Skip if no lifecycle configuration
		if bucketInfo.Lifecycle == nil || len(bucketInfo.Lifecycle.Rules) == 0 {
			continue
		}

		// Process each lifecycle rule
		for _, rule := range bucketInfo.Lifecycle.Rules {
			if rule.Status != "Enabled" {
				continue
			}

			w.processLifecycleRule(ctx, bkt.TenantID, bkt.Name, rule)
		}
	}

	logrus.Debug("Lifecycle policy processing completed")
}

// processLifecycleRule processes a single lifecycle rule for a bucket
func (w *Worker) processLifecycleRule(ctx context.Context, tenantID, bucketName string, rule bucket.LifecycleRule) {
	logrus.WithFields(logrus.Fields{
		"bucket": bucketName,
		"rule":   rule.ID,
	}).Debug("Processing lifecycle rule")

	// Construct bucket path
	bucketPath := bucketName
	if tenantID != "" {
		bucketPath = tenantID + "/" + bucketName
	}

	// Process NoncurrentVersionExpiration
	if rule.NoncurrentVersionExpiration != nil && rule.NoncurrentVersionExpiration.NoncurrentDays > 0 {
		w.processNoncurrentVersionExpiration(ctx, bucketPath, rule)
	}

	// Process ExpiredObjectDeleteMarker
	if rule.Expiration != nil && rule.Expiration.ExpiredObjectDeleteMarker != nil && *rule.Expiration.ExpiredObjectDeleteMarker {
		w.processExpiredDeleteMarkers(ctx, bucketPath, rule)
	}
}

// processNoncurrentVersionExpiration deletes noncurrent versions older than specified days
func (w *Worker) processNoncurrentVersionExpiration(ctx context.Context, bucketPath string, rule bucket.LifecycleRule) {
	noncurrentDays := rule.NoncurrentVersionExpiration.NoncurrentDays
	cutoffTime := time.Now().AddDate(0, 0, -noncurrentDays)

	logrus.WithFields(logrus.Fields{
		"bucket":        bucketPath,
		"rule":          rule.ID,
		"noncurrentDays": noncurrentDays,
		"cutoffTime":    cutoffTime,
	}).Debug("Processing noncurrent version expiration")

	// List all objects in bucket
	result, err := w.objectManager.ListObjects(ctx, bucketPath, rule.Filter.Prefix, "", "", 10000)
	if err != nil {
		logrus.WithError(err).Error("Failed to list objects for lifecycle")
		return
	}

	deletedCount := 0

	// For each object, check its versions
	for _, obj := range result.Objects {
		versions, err := w.objectManager.GetObjectVersions(ctx, bucketPath, obj.Key)
		if err != nil {
			logrus.WithError(err).WithField("key", obj.Key).Warn("Failed to get object versions")
			continue
		}

		// Delete noncurrent versions older than cutoff
		for _, version := range versions {
			// Skip latest version
			if version.IsLatest {
				continue
			}

			// Skip if not old enough
			if version.LastModified.After(cutoffTime) {
				continue
			}

			// Delete this noncurrent version
			_, err := w.objectManager.DeleteObject(ctx, bucketPath, obj.Key, version.VersionID)
			if err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"key":       obj.Key,
					"versionID": version.VersionID,
				}).Warn("Failed to delete noncurrent version")
			} else {
				deletedCount++
				logrus.WithFields(logrus.Fields{
					"key":       obj.Key,
					"versionID": version.VersionID,
					"age":       time.Since(version.LastModified).Hours() / 24,
				}).Debug("Deleted noncurrent version")
			}
		}
	}

	if deletedCount > 0 {
		logrus.WithFields(logrus.Fields{
			"bucket":       bucketPath,
			"rule":         rule.ID,
			"deletedCount": deletedCount,
		}).Info("Lifecycle policy deleted noncurrent versions")
	}
}

// processExpiredDeleteMarkers removes expired delete markers
func (w *Worker) processExpiredDeleteMarkers(ctx context.Context, bucketPath string, rule bucket.LifecycleRule) {
	logrus.WithFields(logrus.Fields{
		"bucket": bucketPath,
		"rule":   rule.ID,
	}).Debug("Processing expired delete markers")

	// This would need to iterate through all objects and check for delete markers
	// that are the only version (expired)
	// Implementation depends on how you want to handle this
	// For now, we'll skip this as it's more complex
}
