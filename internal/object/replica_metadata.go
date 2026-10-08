package object

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
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
	HoldsData(ctx context.Context, bucket, key, versionID string) (bool, error)
	SetLocations(ctx context.Context, bucket, key string, change LocationsChange) error
}

// LocationsChange names the nodes that hold the data of one write: a version,
// or the current object when VersionID is empty. ETag and the times name the
// write, so the change does not touch another one of the key. Gen numbers the
// change: one more than the locations it replaces.
type LocationsChange struct {
	VersionID    string   `json:"version_id,omitempty"`
	ETag         string   `json:"etag"`
	LastModified int64    `json:"last_modified"`
	WrittenAt    int64    `json:"written_at,omitempty"`
	Locations    []string `json:"locations"`
	Gen          int64    `json:"gen"`
}

// compareLocations orders two sets of locations of one write: by the number of
// the change that set each, then by the list, so that every node keeps the
// same.
func compareLocations(aGen int64, a []string, bGen int64, b []string) int {
	if c := cmp.Compare(aGen, bGen); c != 0 {
		return c
	}
	return strings.Compare(strings.Join(a, ","), strings.Join(b, ","))
}

// writeHere is this node's entry of one write of key: the version versionID,
// or without one the current entry when it is the write made at lastModified
// (writtenAt) with etag. It is nil when this node does not have that write.
func (om *objectManager) writeHere(ctx context.Context, bucket, key, versionID string, current *metadata.ObjectMetadata, etag string, lastModified, writtenAt int64) (*metadata.ObjectMetadata, error) {
	entry := current
	if versionID != "" {
		var err error
		entry, err = om.metadataStore.GetObject(ctx, bucket, key, versionID)
		if errors.Is(err, metadata.ErrObjectNotFound) || errors.Is(err, metadata.ErrVersionNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read the entry of the version: %w", err)
		}
	}
	if entry == nil || isMetadataDeleteMarker(entry) || entry.VersionID != versionID || entry.ETag != etag ||
		entry.LastModified.Unix() != lastModified || entry.WrittenAt != writtenAt {
		return nil, nil
	}
	return entry, nil
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
// the current one unless that was written later. An entry of a write this
// node has changes its locations only when they are newer. A file this node
// kept for the entry is removed when the locations do not name this node.
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
	here, err := om.writeHere(ctx, bucket, key, entry.VersionID, existing, entry.ETag, entry.LastModified.Unix(), entry.WrittenAt)
	if err != nil {
		return err
	}
	if here != nil && compareLocations(entry.LocationsGen, entry.Locations, here.LocationsGen, here.Locations) < 0 {
		entry.Locations, entry.LocationsGen = here.Locations, here.LocationsGen
	}

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

// HoldsData reports whether this node has the data of one version of key, or
// of its current object when versionID is empty: the entry names this node, or
// no node, and the file is on the disk.
func (om *objectManager) HoldsData(ctx context.Context, bucket, key, versionID string) (bool, error) {
	entry, err := om.ObjectEntry(ctx, bucket, key, versionID)
	if errors.Is(err, metadata.ErrObjectNotFound) || errors.Is(err, metadata.ErrVersionNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if isMetadataDeleteMarker(entry) || !om.holdsData(entry) {
		return false, nil
	}
	ref := om.objectRef(bucket, key)
	if entry.VersionID != "" {
		ref = om.versionRef(bucket, key, entry.VersionID)
	}
	return om.storage.Exists(ctx, ref)
}

// SetLocations records the nodes that hold the data of one write of key, while
// its entry is still that write. A node the entry no longer names removes its
// file.
func (om *objectManager) SetLocations(ctx context.Context, bucket, key string, change LocationsChange) error {
	defer om.lockKey(bucket, key)()
	entry, err := om.ObjectEntry(ctx, bucket, key, change.VersionID)
	if errors.Is(err, metadata.ErrObjectNotFound) || errors.Is(err, metadata.ErrVersionNotFound) {
		return ErrObjectNotFound
	}
	if err != nil {
		return err
	}
	if isMetadataDeleteMarker(entry) || entry.ETag != change.ETag ||
		entry.LastModified.Unix() != change.LastModified || entry.WrittenAt != change.WrittenAt ||
		compareLocations(change.Gen, change.Locations, entry.LocationsGen, entry.Locations) <= 0 {
		return nil
	}
	entry.Locations, entry.LocationsGen = change.Locations, change.Gen
	ref := om.objectRef(bucket, key)
	if entry.VersionID != "" {
		ref = om.versionRef(bucket, key, entry.VersionID)
		version := &metadata.ObjectVersion{
			VersionID:    entry.VersionID,
			IsLatest:     entry.IsLatest,
			Key:          key,
			Size:         entry.Size,
			ETag:         entry.ETag,
			LastModified: entry.LastModified,
			WrittenAt:    entry.WrittenAt,
			StorageClass: entry.StorageClass,
		}
		if err := om.metadataStore.PutObjectVersion(ctx, entry, version); err != nil {
			return fmt.Errorf("failed to save the entry: %w", err)
		}
	} else if err := om.metadataStore.PutObject(ctx, entry); err != nil {
		return fmt.Errorf("failed to save the entry: %w", err)
	}
	om.dropStaleData(ctx, entry, ref)
	return nil
}

// laterWriteHere reports whether the current entry, one without a version, was
// written after a copy of a write made at lastModified (writtenAt): the last
// write wins on every node, whichever order the copies arrive in.
func laterWriteHere(current *metadata.ObjectMetadata, lastModified time.Time, writtenAt int64) bool {
	return current != nil && current.VersionID == "" && !isMetadataDeleteMarker(current) &&
		CompareWrites(current.LastModified.Unix(), current.WrittenAt, lastModified.Unix(), writtenAt) > 0
}
