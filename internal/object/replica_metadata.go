package object

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/maxiofs/maxiofs/internal/storage"
	"github.com/sirupsen/logrus"
)

// ReplicaMetadataWriter stores the entries of objects whose data other nodes
// hold.
type ReplicaMetadataWriter interface {
	PutReplicaMetadata(ctx context.Context, meta *metadata.ObjectMetadata) error
	ObjectEntry(ctx context.Context, bucket, key, versionID string) (*metadata.ObjectMetadata, error)
}

// LocalNodeSetter is told which cluster node the manager runs on.
type LocalNodeSetter interface {
	SetLocalNode(localNode func() string)
}

// CompareWrites orders two writes of an object: by second, then within one
// second by the time each was written, when both carry it. It is 0 when they
// cannot be told apart.
func CompareWrites(aSec, aWritten, bSec, bWritten int64) int {
	switch {
	case aSec != bSec:
		return cmp.Compare(aSec, bSec)
	case aWritten > 0 && bWritten > 0:
		return cmp.Compare(aWritten, bWritten)
	}
	return 0
}

// PutReplicaMetadata stores another node's entry of an object whose data this
// node does not hold: listings, HEAD and the object's settings are answered
// here, and a GET reads the data from a node that holds it. A version lands as
// the latest only when it is the newest; an entry without a version replaces
// the current one unless that was written later. A file this node kept for
// the entry is removed: the data is held by the nodes the entry names.
func (om *objectManager) PutReplicaMetadata(ctx context.Context, meta *metadata.ObjectMetadata) error {
	if meta == nil {
		return fmt.Errorf("replica metadata is required")
	}
	if err := om.validateObjectName(meta.Key); err != nil {
		return err
	}
	if isMetadataDeleteMarker(meta) {
		return fmt.Errorf("a delete marker is replicated as a delete")
	}
	bucket, key := meta.Bucket, meta.Key
	if _, err := om.loadBucketMetadata(ctx, bucket); err != nil {
		return err
	}
	defer om.lockKey(bucket, key)()

	existing, err := om.metadataStore.GetObject(ctx, bucket, key)
	if err != nil && !errors.Is(err, metadata.ErrObjectNotFound) {
		return fmt.Errorf("failed to read the current entry: %w", err)
	}
	entry := *meta

	if entry.VersionID != "" {
		if err := validateReplicatedVersionID(entry.VersionID); err != nil {
			return err
		}
		landing, err := om.landReplicatedVersion(ctx, bucket, key, entry.VersionID, entry.LastModified, existing)
		if err != nil {
			return err
		}
		entry.IsLatest = landing.latest
		version := &metadata.ObjectVersion{
			VersionID:    entry.VersionID,
			IsLatest:     landing.latest,
			Key:          key,
			Size:         entry.Size,
			ETag:         entry.ETag,
			LastModified: entry.LastModified,
			WrittenAt:    entry.WrittenAt,
			StorageClass: entry.StorageClass,
		}
		if err := om.metadataStore.PutObjectVersion(ctx, &entry, version); err != nil {
			return fmt.Errorf("failed to save the replica entry: %w", err)
		}
		om.dropStaleData(ctx, &entry, om.versionRef(bucket, key, entry.VersionID))
		om.updateUsageAfterReplicatedVersion(ctx, bucket, key, entry.Size, landing, existing)
		return nil
	}

	if laterWriteHere(existing, entry.LastModified, entry.WrittenAt) {
		return nil
	}
	if err := om.metadataStore.PutObject(ctx, &entry); err != nil {
		return fmt.Errorf("failed to save the replica entry: %w", err)
	}
	om.dropStaleData(ctx, &entry, om.objectRef(bucket, key))
	tenantID, bucketName := om.parseBucketPath(bucket)
	om.updateBucketMetricsAfterPut(ctx, tenantID, bucketName, bucket, key, entry.Size, false, existing)
	om.updateTenantQuotaAfterPut(ctx, tenantID, key, entry.Size, false, existing)
	return nil
}

// dropStaleData removes the file of an entry this node does not hold.
func (om *objectManager) dropStaleData(ctx context.Context, entry *metadata.ObjectMetadata, ref storage.ObjectRef) {
	if om.holdsData(entry) {
		return
	}
	if err := om.storage.Delete(ctx, ref); err != nil && !errors.Is(err, storage.ErrObjectNotFound) {
		logrus.WithError(err).WithFields(logrus.Fields{"bucket": ref.Bucket, "key": ref.Key}).
			Warn("Failed to remove the data of an object other nodes hold")
	}
}

// ObjectEntry is this node's entry of an object, or of one of its versions.
func (om *objectManager) ObjectEntry(ctx context.Context, bucket, key, versionID string) (*metadata.ObjectMetadata, error) {
	var entry *metadata.ObjectMetadata
	var err error
	if versionID != "" {
		entry, err = om.metadataStore.GetObject(ctx, bucket, key, versionID)
	} else {
		entry, err = om.metadataStore.GetObject(ctx, bucket, key)
	}
	if err != nil {
		return nil, err
	}
	entry.Bucket, entry.Key = bucket, key
	return entry, nil
}

// laterWriteHere reports whether the current entry, one without a version, was
// written after a copy of a write made at lastModified (writtenAt): the last
// write wins on every node, whichever order the copies arrive in.
func laterWriteHere(current *metadata.ObjectMetadata, lastModified time.Time, writtenAt int64) bool {
	return current != nil && current.VersionID == "" && !isMetadataDeleteMarker(current) &&
		CompareWrites(current.LastModified.Unix(), current.WrittenAt, lastModified.Unix(), writtenAt) > 0
}
