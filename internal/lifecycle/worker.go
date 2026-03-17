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

	// Process object expiration by age (Days) or fixed date
	if rule.Expiration != nil && (rule.Expiration.Days != nil || rule.Expiration.Date != nil) {
		w.processObjectExpiration(ctx, bucketPath, rule)
	}

	// Process abort of incomplete multipart uploads
	if rule.AbortIncompleteMultipartUpload != nil && rule.AbortIncompleteMultipartUpload.DaysAfterInitiation > 0 {
		w.processAbortIncompleteMultipartUploads(ctx, bucketPath, rule)
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
			// Lifecycle rules don't support bypass governance
			_, err := w.objectManager.DeleteObject(ctx, bucketPath, obj.Key, false, version.VersionID)
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
// An expired delete marker is a delete marker that is the only remaining version of an object
func (w *Worker) processExpiredDeleteMarkers(ctx context.Context, bucketPath string, rule bucket.LifecycleRule) {
	logrus.WithFields(logrus.Fields{
		"bucket": bucketPath,
		"rule":   rule.ID,
	}).Debug("Processing expired delete markers")

	// List all objects in bucket
	result, err := w.objectManager.ListObjects(ctx, bucketPath, rule.Filter.Prefix, "", "", 10000)
	if err != nil {
		logrus.WithError(err).Error("Failed to list objects for expired delete marker cleanup")
		return
	}

	deletedCount := 0

	// For each object, check if it only has a delete marker
	for _, obj := range result.Objects {
		versions, err := w.objectManager.GetObjectVersions(ctx, bucketPath, obj.Key)
		if err != nil {
			logrus.WithError(err).WithField("key", obj.Key).Warn("Failed to get object versions for delete marker check")
			continue
		}

		// An expired delete marker exists when:
		// 1. There is only one version
		// 2. That version is a delete marker
		// 3. It is the latest version
		if len(versions) == 1 && versions[0].IsDeleteMarker && versions[0].IsLatest {
			// Delete this expired delete marker
			// Use the versionID to delete it permanently
			_, err := w.objectManager.DeleteObject(ctx, bucketPath, obj.Key, false, versions[0].VersionID)
			if err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"key":       obj.Key,
					"versionID": versions[0].VersionID,
				}).Warn("Failed to delete expired delete marker")
			} else {
				deletedCount++
				logrus.WithFields(logrus.Fields{
					"key":       obj.Key,
					"versionID": versions[0].VersionID,
				}).Debug("Deleted expired delete marker")
			}
		}
	}

	if deletedCount > 0 {
		logrus.WithFields(logrus.Fields{
			"bucket":       bucketPath,
			"rule":         rule.ID,
			"deletedCount": deletedCount,
		}).Info("Lifecycle policy deleted expired delete markers")
	}
}

// processObjectExpiration deletes objects that have exceeded their expiration age or date.
// On versioned buckets, DeleteObject without a versionId creates a delete marker (correct S3 behavior).
func (w *Worker) processObjectExpiration(ctx context.Context, bucketPath string, rule bucket.LifecycleRule) {
	var cutoff time.Time
	if rule.Expiration.Days != nil {
		cutoff = time.Now().UTC().AddDate(0, 0, -*rule.Expiration.Days)
	} else {
		cutoff = rule.Expiration.Date.UTC()
	}

	logrus.WithFields(logrus.Fields{
		"bucket":  bucketPath,
		"rule":    rule.ID,
		"cutoff":  cutoff,
	}).Debug("Processing object expiration")

	prefix := rule.Filter.Prefix

	result, err := w.objectManager.ListObjects(ctx, bucketPath, prefix, "", "", 10000)
	if err != nil {
		logrus.WithError(err).WithField("bucket", bucketPath).Error("Failed to list objects for expiration")
		return
	}

	deletedCount := 0
	for _, obj := range result.Objects {
		if obj.LastModified.UTC().Before(cutoff) {
			if _, err := w.objectManager.DeleteObject(ctx, bucketPath, obj.Key, false); err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"bucket": bucketPath,
					"key":    obj.Key,
				}).Warn("Failed to expire object")
			} else {
				deletedCount++
			}
		}
	}

	if deletedCount > 0 {
		logrus.WithFields(logrus.Fields{
			"bucket":       bucketPath,
			"rule":         rule.ID,
			"deletedCount": deletedCount,
		}).Info("Lifecycle policy expired objects")
	}
}

// processAbortIncompleteMultipartUploads aborts multipart uploads that have been
// in progress longer than the configured DaysAfterInitiation.
func (w *Worker) processAbortIncompleteMultipartUploads(ctx context.Context, bucketPath string, rule bucket.LifecycleRule) {
	cutoff := time.Now().UTC().AddDate(0, 0, -rule.AbortIncompleteMultipartUpload.DaysAfterInitiation)

	logrus.WithFields(logrus.Fields{
		"bucket": bucketPath,
		"rule":   rule.ID,
		"cutoff": cutoff,
	}).Debug("Processing abort incomplete multipart uploads")

	uploads, err := w.objectManager.ListMultipartUploads(ctx, bucketPath)
	if err != nil {
		logrus.WithError(err).WithField("bucket", bucketPath).Error("Failed to list multipart uploads for lifecycle")
		return
	}

	abortedCount := 0
	for _, upload := range uploads {
		if upload.Initiated.UTC().Before(cutoff) {
			if err := w.objectManager.AbortMultipartUpload(ctx, upload.UploadID); err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"bucket":   bucketPath,
					"uploadID": upload.UploadID,
				}).Warn("Failed to abort stale multipart upload")
			} else {
				abortedCount++
			}
		}
	}

	if abortedCount > 0 {
		logrus.WithFields(logrus.Fields{
			"bucket":       bucketPath,
			"rule":         rule.ID,
			"abortedCount": abortedCount,
		}).Info("Lifecycle policy aborted stale multipart uploads")
	}
}
