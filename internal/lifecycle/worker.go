package lifecycle

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/maxiofs/maxiofs/internal/bgwork"
	"github.com/maxiofs/maxiofs/internal/bucket"
	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/object"
	"github.com/maxiofs/maxiofs/internal/storageclass"
	"github.com/sirupsen/logrus"
)

// Worker handles lifecycle policy execution
type Worker struct {
	bucketManager    bucket.Manager
	objectManager    object.Manager
	metadataStore    metadata.Store
	defaultAbortDays func() int
	expires          func() bool
	ticker           *time.Ticker
	stopChan         chan struct{}
	stopOnce         sync.Once
	wg               sync.WaitGroup
	observe          bgwork.TaskObserver
	now              func() time.Time
}

// SetTaskObserver sets what is told of each lifecycle pass. Set before Start.
func (w *Worker) SetTaskObserver(observe bgwork.TaskObserver) {
	w.observe = observe
}

// runPass runs one pass of every bucket's lifecycle rules.
func (w *Worker) runPass(ctx context.Context) {
	_ = w.observe.Observe("lifecycle", func() error { return w.processLifecyclePolicies(ctx) })
}

// RunOnce runs one pass of every bucket's lifecycle rules now, as the worker
// does at each interval.
func (w *Worker) RunOnce(ctx context.Context) error {
	return w.processLifecyclePolicies(ctx)
}

// NewWorker creates a new lifecycle worker
func NewWorker(bucketManager bucket.Manager, objectManager object.Manager, metadataStore metadata.Store) *Worker {
	return &Worker{
		bucketManager: bucketManager,
		objectManager: objectManager,
		metadataStore: metadataStore,
		stopChan:      make(chan struct{}),
		now:           time.Now,
	}
}

// SetDefaultAbortIncompleteDays supplies the server-wide fallback applied to
// buckets that carry no AbortIncompleteMultipartUpload rule of their own.
func (w *Worker) SetDefaultAbortIncompleteDays(days func() int) {
	w.defaultAbortDays = days
}

// SetObjectManager replaces the object manager the worker deletes through,
// before Start: in a cluster, the one that sends the deletes to the other nodes.
func (w *Worker) SetObjectManager(om object.Manager) {
	w.objectManager = om
}

// SetExpirationGate decides, at each pass, whether this node expires objects.
// With every node holding every bucket, one node expires them and the others
// receive its deletes. Incomplete multipart uploads are aborted on every node:
// each one lives on the node it was started on.
func (w *Worker) SetExpirationGate(expires func() bool) {
	w.expires = expires
}

// bucketAbortsIncompleteUploads reports whether the bucket already decides for
// itself when to abort an incomplete upload.
func (w *Worker) bucketAbortsIncompleteUploads(info *bucket.Bucket) bool {
	if info.Lifecycle == nil {
		return false
	}
	for _, rule := range info.Lifecycle.Rules {
		if rule.Status == "Enabled" && rule.AbortIncompleteMultipartUpload != nil {
			return true
		}
	}
	return false
}

func (w *Worker) applyDefaultAbortIncomplete(ctx context.Context, tenantID, name string) {
	if w.defaultAbortDays == nil {
		return
	}
	days := w.defaultAbortDays()
	if days <= 0 {
		return
	}
	w.processAbortIncompleteMultipartUploads(ctx, bucketPathOf(tenantID, name), bucket.LifecycleRule{
		AbortIncompleteMultipartUpload: &bucket.LifecycleAbortIncompleteMultipartUpload{DaysAfterInitiation: days},
	})
}

func bucketPathOf(tenantID, name string) string {
	if tenantID == "" {
		return name
	}
	return tenantID + "/" + name
}

// Start begins the lifecycle worker
func (w *Worker) Start(ctx context.Context, interval time.Duration) {
	w.ticker = time.NewTicker(interval)

	logrus.WithField("interval", interval).Info("Lifecycle worker started")

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for {
			select {
			case <-w.ticker.C:
				w.runPass(ctx)
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
// Stop signals the worker and waits for a run in progress to finish. The
// caller closes the stores next, and a sweep still walking them would be using
// a closed database.
func (w *Worker) Stop() {
	w.stopOnce.Do(func() { close(w.stopChan) })
	w.wg.Wait()
}

// processLifecyclePolicies processes all lifecycle policies for all buckets
func (w *Worker) processLifecyclePolicies(ctx context.Context) error {
	logrus.Debug("Processing lifecycle policies...")

	// Get all buckets
	buckets, err := w.bucketManager.ListBuckets(ctx, "") // Empty tenantID lists all buckets
	if err != nil {
		logrus.WithError(err).Error("Failed to list buckets for lifecycle processing")
		return err
	}

	for _, bkt := range buckets {
		w.processBucket(ctx, bkt)
	}

	logrus.Debug("Lifecycle policy processing completed")
	return nil
}

// processBucket applies the lifecycle rules of one bucket.
func (w *Worker) processBucket(ctx context.Context, bkt bucket.Bucket) {
	// Get bucket details to check for lifecycle config
	bucketInfo, err := w.bucketManager.GetBucketInfo(ctx, bkt.TenantID, bkt.Name)
	if err != nil {
		logrus.WithError(err).WithField("bucket", bkt.Name).Warn("Failed to get bucket info")
		return
	}

	if !w.bucketAbortsIncompleteUploads(bucketInfo) {
		w.applyDefaultAbortIncomplete(ctx, bkt.TenantID, bkt.Name)
	}

	// Skip if no lifecycle configuration
	if bucketInfo.Lifecycle == nil || len(bucketInfo.Lifecycle.Rules) == 0 {
		return
	}

	expires := w.expires == nil || w.expires()

	// Process each lifecycle rule
	for _, rule := range bucketInfo.Lifecycle.Rules {
		if rule.Status != "Enabled" {
			continue
		}
		if !expires {
			rule.NoncurrentVersionExpiration = nil
			rule.Expiration = nil
		}

		w.processLifecycleRule(ctx, bkt.TenantID, bkt.Name, rule)
	}
	w.processTransitions(ctx, bucketPathOf(bkt.TenantID, bkt.Name), bucketInfo.Lifecycle.Rules)
}

// processTransitions moves the objects and the noncurrent versions the
// bucket's enabled rules take to the coldest storage class they have come to,
// when it is colder than theirs. A write due to expire is not moved: the
// expiration wins. Every node runs them: in a cluster, the node that moves a
// write is the first of those holding it (see object.StorageClassChanger).
func (w *Worker) processTransitions(ctx context.Context, bucketPath string, rules []bucket.LifecycleRule) {
	changer, ok := w.objectManager.(object.StorageClassChanger)
	if !ok {
		return
	}
	var enabled []bucket.LifecycleRule
	moves := false
	for _, rule := range rules {
		if rule.Status != "Enabled" {
			continue
		}
		enabled = append(enabled, rule)
		moves = moves || len(rule.Transitions) > 0 || len(rule.NoncurrentVersionTransitions) > 0
	}
	if !moves {
		return
	}
	versionsByKey, err := w.listLifecycleVersionsByKey(ctx, bucketPath, transitionPrefix(enabled))
	if err != nil {
		logrus.WithError(err).WithField("bucket", bucketPath).Error("Failed to list object versions for lifecycle transitions")
		return
	}
	now := w.now().UTC()
	moved := 0
	for _, versions := range versionsByKey {
		if ctx.Err() != nil {
			return
		}
		for _, v := range versions {
			if v.IsLatest && !isLifecycleDeleteMarker(v) {
				moved += w.moveWrite(ctx, changer, bucketPath, v, w.currentTarget(ctx, bucketPath, enabled, v, now))
			}
		}
		for _, nv := range noncurrentVersions(versions) {
			if !isLifecycleDeleteMarker(nv.version) {
				moved += w.moveWrite(ctx, changer, bucketPath, nv.version, w.noncurrentTarget(ctx, bucketPath, enabled, nv, now))
			}
		}
	}
	if moved > 0 {
		logrus.WithFields(logrus.Fields{"bucket": bucketPath, "moved": moved}).
			Info("Lifecycle policy moved objects to colder storage classes")
	}
}

// transitionPrefix is the prefix every rule's filter starts with.
func transitionPrefix(rules []bucket.LifecycleRule) string {
	if len(rules) == 0 {
		return ""
	}
	prefix := rules[0].Filter.Prefix
	for _, rule := range rules[1:] {
		for !strings.HasPrefix(rule.Filter.Prefix, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

// moveWrite moves version to class when it is colder than its own, and
// reports 1 when this node moved it.
func (w *Worker) moveWrite(ctx context.Context, changer object.StorageClassChanger, bucketPath string, version *metadata.ObjectVersion, class string) int {
	if class == "" || !storageclass.Colder(class, version.StorageClass) {
		return 0
	}
	moved, err := changer.ChangeStorageClass(ctx, bucketPath, version.Key, object.ClassChange{
		VersionID:    version.VersionID,
		ETag:         version.ETag,
		LastModified: version.LastModified.Unix(),
		WrittenAt:    version.WrittenAt,
		StorageClass: class,
	})
	if err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"bucket": bucketPath, "key": version.Key, "versionID": version.VersionID, "storage_class": class,
		}).Warn("Failed to move an object to a colder storage class")
		return 0
	}
	if moved {
		return 1
	}
	return 0
}

// currentTarget is the coldest class the rules' transitions have come to for
// the current object version, or "" when none has or when it is due to
// expire.
func (w *Worker) currentTarget(ctx context.Context, bucketPath string, rules []bucket.LifecycleRule, version *metadata.ObjectVersion, now time.Time) string {
	target := ""
	for _, rule := range rules {
		due := rule.Expiration != nil && expires(rule.Expiration, version.LastModified, now)
		if !due && !slices.ContainsFunc(rule.Transitions, func(t bucket.LifecycleTransition) bool {
			return transitionDue(t, version.LastModified, now) && storageclass.Colder(t.StorageClass, target)
		}) {
			continue
		}
		if !w.matches(ctx, bucketPath, rule.Filter, version) {
			continue
		}
		if due {
			return ""
		}
		for _, t := range rule.Transitions {
			if transitionDue(t, version.LastModified, now) && storageclass.Colder(t.StorageClass, target) {
				target = t.StorageClass
			}
		}
	}
	return target
}

// noncurrentTarget is the coldest class the rules' noncurrent transitions
// have come to for a noncurrent version, or "" when none has or when it is
// due to expire.
func (w *Worker) noncurrentTarget(ctx context.Context, bucketPath string, rules []bucket.LifecycleRule, nv noncurrentVersion, now time.Time) string {
	target := ""
	for _, rule := range rules {
		e := rule.NoncurrentVersionExpiration
		due := e != nil && nv.newer >= e.NewerNoncurrentVersions && !nv.since.After(now.AddDate(0, 0, -e.NoncurrentDays))
		if !due && !slices.ContainsFunc(rule.NoncurrentVersionTransitions, func(t bucket.NoncurrentVersionTransition) bool {
			return noncurrentTransitionDue(t, nv, now) && storageclass.Colder(t.StorageClass, target)
		}) {
			continue
		}
		if !w.matches(ctx, bucketPath, rule.Filter, nv.version) {
			continue
		}
		if due {
			return ""
		}
		for _, t := range rule.NoncurrentVersionTransitions {
			if noncurrentTransitionDue(t, nv, now) && storageclass.Colder(t.StorageClass, target) {
				target = t.StorageClass
			}
		}
	}
	return target
}

// expires reports whether an expiration has come for a write made at written.
func expires(e *bucket.LifecycleExpiration, written, now time.Time) bool {
	switch {
	case e.Days != nil:
		return written.Before(now.AddDate(0, 0, -*e.Days))
	case e.Date != nil:
		return !now.Before(*e.Date) && written.Before(*e.Date)
	}
	return false
}

// transitionDue reports whether a transition has come for a write made at
// written: its days have passed, or its date has come and the write was made
// before it.
func transitionDue(t bucket.LifecycleTransition, written, now time.Time) bool {
	switch {
	case t.Days != nil:
		return written.Before(now.AddDate(0, 0, -*t.Days))
	case t.Date != nil:
		return !now.Before(*t.Date) && written.Before(*t.Date)
	}
	return false
}

// noncurrentTransitionDue reports whether a noncurrent transition has come
// for a noncurrent version.
func noncurrentTransitionDue(t bucket.NoncurrentVersionTransition, nv noncurrentVersion, now time.Time) bool {
	return nv.newer >= t.NewerNoncurrentVersions && !nv.since.After(now.AddDate(0, 0, -t.NoncurrentDays))
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

// processNoncurrentVersionExpiration deletes the versions noncurrent for
// longer than the rule's days, kept but for the newest of each key the rule
// keeps. A version is noncurrent from when the next one was written.
func (w *Worker) processNoncurrentVersionExpiration(ctx context.Context, bucketPath string, rule bucket.LifecycleRule) {
	noncurrentDays := rule.NoncurrentVersionExpiration.NoncurrentDays
	cutoffTime := w.now().AddDate(0, 0, -noncurrentDays)

	logrus.WithFields(logrus.Fields{
		"bucket":         bucketPath,
		"rule":           rule.ID,
		"noncurrentDays": noncurrentDays,
		"cutoffTime":     cutoffTime,
	}).Debug("Processing noncurrent version expiration")

	versionsByKey, err := w.listLifecycleVersionsByKey(ctx, bucketPath, rule.Filter.Prefix)
	if err != nil {
		logrus.WithError(err).Error("Failed to list object versions for lifecycle")
		return
	}

	deletedCount := 0

	kept := rule.NoncurrentVersionExpiration.NewerNoncurrentVersions
	for key, versions := range versionsByKey {
		for _, nv := range noncurrentVersions(versions) {
			version := nv.version
			if nv.newer < kept || nv.since.After(cutoffTime) || !w.matches(ctx, bucketPath, rule.Filter, version) {
				continue
			}

			// Lifecycle rules don't support bypass governance
			_, err := w.objectManager.DeleteObject(ctx, bucketPath, key, false, version.VersionID)
			if err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"key":       key,
					"versionID": version.VersionID,
				}).Warn("Failed to delete noncurrent version")
			} else {
				deletedCount++
				logrus.WithFields(logrus.Fields{
					"key":       key,
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

// noncurrentVersion is a version of a key that a later one replaced at since.
// newer counts the noncurrent versions of the key newer than it, delete
// markers left out; a delete marker is newer than none.
type noncurrentVersion struct {
	version *metadata.ObjectVersion
	since   time.Time
	newer   int
}

// noncurrentVersions are the noncurrent versions of one key, newest first.
func noncurrentVersions(versions []*metadata.ObjectVersion) []noncurrentVersion {
	sorted := slices.Clone(versions)
	slices.SortStableFunc(sorted, func(a, b *metadata.ObjectVersion) int {
		if a.IsLatest != b.IsLatest {
			if a.IsLatest {
				return -1
			}
			return 1
		}
		return -object.CompareWrites(a.LastModified.Unix(), a.WrittenAt, b.LastModified.Unix(), b.WrittenAt)
	})
	var out []noncurrentVersion
	newer := 0
	for i := 1; i < len(sorted); i++ {
		v := sorted[i]
		if v.IsLatest {
			continue
		}
		nv := noncurrentVersion{version: v, since: sorted[i-1].LastModified, newer: maxNewerCounted}
		if !isLifecycleDeleteMarker(v) {
			nv.newer = newer
			newer++
		}
		out = append(out, nv)
	}
	return out
}

// maxNewerCounted is the count of newer versions a delete marker is given: no
// rule keeps one among the newest.
const maxNewerCounted = 1 << 30

// matches reports whether version falls under filter f: its key starts with
// the prefix, its size is within the bounds and it carries every tag. A
// version whose tags cannot be read does not.
func (w *Worker) matches(ctx context.Context, bucketPath string, f bucket.LifecycleFilter, version *metadata.ObjectVersion) bool {
	if !strings.HasPrefix(version.Key, f.Prefix) {
		return false
	}
	if f.ObjectSizeGreaterThan != nil && version.Size <= *f.ObjectSizeGreaterThan {
		return false
	}
	if f.ObjectSizeLessThan != nil && version.Size >= *f.ObjectSizeLessThan {
		return false
	}
	if len(f.Tags) == 0 {
		return true
	}
	tags, err := w.objectManager.GetObjectTagging(ctx, bucketPath, version.Key, version.VersionID)
	if err != nil || tags == nil {
		return false
	}
	carried := make(map[string]string, len(tags.Tags))
	for _, tag := range tags.Tags {
		carried[tag.Key] = tag.Value
	}
	for _, tag := range f.Tags {
		if value, ok := carried[tag.Key]; !ok || value != tag.Value {
			return false
		}
	}
	return true
}

// processExpiredDeleteMarkers removes expired delete markers
// An expired delete marker is a delete marker that is the only remaining version of an object
func (w *Worker) processExpiredDeleteMarkers(ctx context.Context, bucketPath string, rule bucket.LifecycleRule) {
	logrus.WithFields(logrus.Fields{
		"bucket": bucketPath,
		"rule":   rule.ID,
	}).Debug("Processing expired delete markers")

	versionsByKey, err := w.listLifecycleVersionsByKey(ctx, bucketPath, rule.Filter.Prefix)
	if err != nil {
		logrus.WithError(err).Error("Failed to list object versions for expired delete marker cleanup")
		return
	}

	deletedCount := 0

	// For each key, check if it only has a delete marker
	for key, versions := range versionsByKey {
		if len(versions) == 1 && isLifecycleDeleteMarker(versions[0]) && versions[0].IsLatest {
			// Delete this expired delete marker
			// Use the versionID to delete it permanently
			_, err := w.objectManager.DeleteObject(ctx, bucketPath, key, false, versions[0].VersionID)
			if err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"key":       key,
					"versionID": versions[0].VersionID,
				}).Warn("Failed to delete expired delete marker")
			} else {
				deletedCount++
				logrus.WithFields(logrus.Fields{
					"key":       key,
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

func (w *Worker) listLifecycleVersionsByKey(ctx context.Context, bucketPath, prefix string) (map[string][]*metadata.ObjectVersion, error) {
	versions, err := w.metadataStore.ListAllObjectVersions(ctx, bucketPath, prefix, 0)
	if err != nil {
		return nil, err
	}

	byKey := make(map[string][]*metadata.ObjectVersion)
	for _, version := range versions {
		if version == nil {
			continue
		}
		byKey[version.Key] = append(byKey[version.Key], version)
	}
	return byKey, nil
}

// versionOf is obj as a version of its key.
func versionOf(obj *object.Object) *metadata.ObjectVersion {
	return &metadata.ObjectVersion{
		VersionID:    obj.VersionID,
		IsLatest:     true,
		Key:          obj.Key,
		Size:         obj.Size,
		ETag:         obj.ETag,
		LastModified: obj.LastModified,
		WrittenAt:    obj.WrittenAt,
		StorageClass: obj.StorageClass,
	}
}

func isLifecycleDeleteMarker(version *metadata.ObjectVersion) bool {
	return version != nil && version.VersionID != "" && version.Size == 0 && version.ETag == ""
}

// processObjectExpiration deletes the objects the rule's filter takes written
// longer ago than its days, or, once its date has come, written before it.
// On versioned buckets, DeleteObject without a versionId creates a delete
// marker (correct S3 behavior).
func (w *Worker) processObjectExpiration(ctx context.Context, bucketPath string, rule bucket.LifecycleRule) {
	now := w.now().UTC()
	var cutoff time.Time
	if rule.Expiration.Days != nil {
		cutoff = now.AddDate(0, 0, -*rule.Expiration.Days)
	} else {
		cutoff = rule.Expiration.Date.UTC()
		if now.Before(cutoff) {
			return
		}
	}

	logrus.WithFields(logrus.Fields{
		"bucket": bucketPath,
		"rule":   rule.ID,
		"cutoff": cutoff,
	}).Debug("Processing object expiration")

	prefix := rule.Filter.Prefix

	deletedCount := 0
	marker := ""
	for {
		if err := ctx.Err(); err != nil {
			return
		}

		result, err := w.objectManager.ListObjects(ctx, bucketPath, prefix, "", marker, 1000)
		if err != nil {
			logrus.WithError(err).WithField("bucket", bucketPath).Error("Failed to list objects for expiration")
			return
		}

		for _, obj := range result.Objects {
			if obj.LastModified.UTC().Before(cutoff) && w.matches(ctx, bucketPath, rule.Filter, versionOf(&obj)) {
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

		if !result.IsTruncated {
			break
		}
		marker = result.NextMarker
		if marker == "" && len(result.Objects) > 0 {
			marker = result.Objects[len(result.Objects)-1].Key
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

	prefix := rule.Filter.Prefix
	abortedCount := 0
	for _, upload := range uploads {
		if prefix != "" && !strings.HasPrefix(upload.Key, prefix) {
			continue
		}
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
