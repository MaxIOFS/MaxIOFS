package object

import (
	"context"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
)

// lockFields are the sidecar fields that keep an object's Object Lock state.
var lockFields = []string{storage.MetadataLockModeField, storage.MetadataLockUntilField, storage.MetadataLegalHoldField}

// setLockFields writes an object's Object Lock state into its sidecar
// metadata: the retention, and the legal hold when on.
func setLockFields(meta map[string]string, retention *RetentionConfig, legalHold *LegalHoldConfig) {
	for _, f := range lockFields {
		delete(meta, f)
	}
	if retention != nil && retention.Mode != "" {
		meta[storage.MetadataLockModeField] = retention.Mode
		meta[storage.MetadataLockUntilField] = retention.RetainUntilDate.UTC().Format(time.RFC3339Nano)
	}
	if legalHold != nil && legalHold.Status == LegalHoldStatusOn {
		meta[storage.MetadataLegalHoldField] = LegalHoldStatusOn
	}
}

// setLockFieldsOf writes the Object Lock state of an entry into sidecar
// metadata.
func setLockFieldsOf(meta map[string]string, entry *metadata.ObjectMetadata) {
	obj := fromMetadataObject(entry)
	setLockFields(meta, obj.Retention, obj.LegalHold)
}

// lockFieldsMatch reports whether sidecar metadata keeps the Object Lock state
// given.
func lockFieldsMatch(meta map[string]string, retention *RetentionConfig, legalHold *LegalHoldConfig) bool {
	want := make(map[string]string, len(lockFields))
	setLockFields(want, retention, legalHold)
	for _, f := range lockFields {
		if meta[f] != want[f] {
			return false
		}
	}
	return true
}

// saveLockInSidecar writes an object version's Object Lock state into the
// sidecar of its data on this node; data held elsewhere has none here.
func (om *objectManager) saveLockInSidecar(ctx context.Context, ref storage.ObjectRef, retention *RetentionConfig, legalHold *LegalHoldConfig) error {
	exists, err := om.storage.Exists(ctx, ref)
	if err != nil || !exists {
		return err
	}
	meta, err := om.storage.GetMetadata(ctx, ref)
	if err != nil {
		return err
	}
	if lockFieldsMatch(meta, retention, legalHold) {
		return nil
	}
	setLockFields(meta, retention, legalHold)
	return om.storage.SetMetadata(ctx, ref, meta)
}

// syncLockSidecars writes into the sidecars of the versions of key this node
// holds the Object Lock state their entries have. Lock state exists only in
// buckets with Object Lock.
func (om *objectManager) syncLockSidecars(ctx context.Context, bucket, key string, refs map[storage.ObjectRef]struct{}) error {
	bucketMeta, err := om.loadBucketMetadata(ctx, bucket)
	if err != nil || bucketMeta.ObjectLock == nil || !bucketMeta.ObjectLock.Enabled {
		return nil
	}
	defer om.lockKey(bucket, key)()
	for ref := range refs {
		var entry *metadata.ObjectMetadata
		if ref.VersionID != "" {
			entry, err = om.metadataStore.GetObject(ctx, bucket, key, ref.VersionID)
		} else {
			entry, err = om.metadataStore.GetObject(ctx, bucket, key)
			if err == nil && entry.VersionID != "" {
				continue
			}
		}
		if err != nil || isMetadataDeleteMarker(entry) {
			continue
		}
		obj := fromMetadataObject(entry)
		if err := om.saveLockInSidecar(ctx, ref, obj.Retention, obj.LegalHold); err != nil {
			return err
		}
	}
	return nil
}
