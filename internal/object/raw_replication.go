package object

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/maxiofs/maxiofs/internal/metadata"
	"github.com/sirupsen/logrus"
)

// Raw (ciphertext) transfer for HA replication.
//
// When an object is envelope-encrypted with a cluster-shared KEK version,
// every node in the cluster can unwrap its DEK — so replication can move the
// stored ciphertext as-is instead of decrypting on the source and
// re-encrypting on the destination. GetObjectRaw reads the stored bytes +
// sidecar without decrypting; PutObjectRaw stores them on the replica without
// encrypting, performing the same replica-side bookkeeping as a regular
// replicated PutObject (Pebble entry, metrics, implicit folders).

// RawObjectAccessor is implemented by objectManager and consumed by the HA
// fanout in internal/cluster (promoted through the HAObjectManager embedding).
type RawObjectAccessor interface {
	// GetObjectRaw returns the stored (possibly encrypted) bytes, the sidecar
	// metadata and the Pebble metadata entry, without decrypting.
	GetObjectRaw(ctx context.Context, bucket, key, versionID string) (io.ReadCloser, map[string]string, *metadata.ObjectMetadata, error)
	// PutObjectRaw stores raw bytes + sidecar + Pebble entry on a replica.
	PutObjectRaw(ctx context.Context, bucket, key string, data io.Reader, sidecar map[string]string, metaObj *metadata.ObjectMetadata) error
	// CanReplicateRaw reports whether a sidecar describes an object that any
	// cluster node can decrypt (envelope + cluster-shared KEK version).
	CanReplicateRaw(sidecar map[string]string) bool
}

// CanReplicateRaw: the object must be envelope-encrypted and its wrapping KEK
// version must be cluster-shared. Plaintext and legacy/local-KEK objects fall
// back to the decrypt/re-encrypt replication path (they converge to envelope
// via the rotation worker).
func (om *objectManager) CanReplicateRaw(sidecar map[string]string) bool {
	if sidecar["encrypted"] != "true" || sidecar["wrapped-dek"] == "" {
		return false
	}
	version, err := strconv.Atoi(sidecar["kek-version"])
	if err != nil {
		return false
	}
	return om.kekProvider.IsClusterShared(version)
}

// GetObjectRaw resolves the object path exactly like GetObject but returns
// the stored bytes without decrypting, plus the sidecar and Pebble metadata.
func (om *objectManager) GetObjectRaw(ctx context.Context, bucket, key, versionID string) (io.ReadCloser, map[string]string, *metadata.ObjectMetadata, error) {
	var metaObj *metadata.ObjectMetadata
	var err error
	if versionID != "" {
		metaObj, err = om.metadataStore.GetObject(ctx, bucket, key, versionID)
	} else {
		metaObj, err = om.metadataStore.GetObject(ctx, bucket, key)
	}
	if err != nil {
		if err == metadata.ErrObjectNotFound {
			return nil, nil, nil, ErrObjectNotFound
		}
		return nil, nil, nil, fmt.Errorf("failed to get object metadata: %w", err)
	}
	if isMetadataDeleteMarker(metaObj) {
		return nil, nil, nil, ErrObjectNotFound
	}

	resolvedVersion := versionID
	if resolvedVersion == "" {
		resolvedVersion = metaObj.VersionID
	}
	objectPath := om.getObjectPath(bucket, key)
	if resolvedVersion != "" {
		objectPath = om.getVersionedObjectPath(bucket, key, resolvedVersion)
	}

	reader, sidecar, err := om.storage.Get(ctx, objectPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get object data: %w", err)
	}
	return reader, sidecar, metaObj, nil
}

// PutObjectRaw is the replica-side write of a raw ciphertext transfer. It
// mirrors the bookkeeping of a replicated PutObject (which always bypasses
// quota enforcement and only updates local counters), but stores the received
// bytes and sidecar verbatim — no encryption, no ETag recomputation of the
// plaintext.
func (om *objectManager) PutObjectRaw(ctx context.Context, bucket, key string, data io.Reader, sidecar map[string]string, metaObj *metadata.ObjectMetadata) error {
	if err := om.validateObjectName(key); err != nil {
		return err
	}
	if metaObj == nil {
		return fmt.Errorf("raw replication requires object metadata")
	}

	tenantID, bucketName := om.parseBucketPath(bucket)
	versioned := metaObj.VersionID != ""

	objectPath := om.getObjectPath(bucket, key)
	if versioned {
		objectPath = om.getVersionedObjectPath(bucket, key, metaObj.VersionID)
	}

	// Store the raw bytes with a copy of the source sidecar. storage.Put
	// recomputes size/etag over the received (ciphertext) bytes — identical to
	// the source values — and refreshes last_modified.
	sidecarCopy := make(map[string]string, len(sidecar))
	for k, v := range sidecar {
		sidecarCopy[k] = v
	}
	if err := om.storage.Put(ctx, objectPath, data, sidecarCopy); err != nil {
		return fmt.Errorf("failed to store raw replica: %w", err)
	}

	// Same locking discipline as PutObject's metadata section.
	defer om.lockKey(bucket, key)()

	existingObjBeforeSave, _ := om.metadataStore.GetObject(ctx, bucket, key)

	// Normalise ownership fields the primary set for its own store.
	metaObj.Bucket = bucket
	metaObj.Key = key

	if versioned {
		version := &metadata.ObjectVersion{
			VersionID:    metaObj.VersionID,
			IsLatest:     true,
			Key:          key,
			Size:         metaObj.Size,
			ETag:         metaObj.ETag,
			LastModified: metaObj.LastModified,
			StorageClass: metaObj.StorageClass,
		}
		if err := om.metadataStore.PutObjectVersion(ctx, metaObj, version); err != nil {
			logrus.WithError(err).Warn("Raw replica: failed to save object version metadata")
		}
	} else {
		if err := om.metadataStore.PutObject(ctx, metaObj); err != nil {
			logrus.WithError(err).Warn("Raw replica: failed to save object metadata")
		}
	}

	om.ensureImplicitFolders(ctx, bucket, key)
	om.updateBucketMetricsAfterPut(ctx, tenantID, bucketName, bucket, key, metaObj.Size, versioned, existingObjBeforeSave)
	om.updateTenantQuotaAfterPut(ctx, tenantID, key, metaObj.Size, versioned, existingObjBeforeSave)

	return nil
}
