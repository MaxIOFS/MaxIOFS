package object

import (
	"context"
	"errors"

	"github.com/maxiofs/maxiofs/internal/metadata"
)

// ClassChange moves one write of a key to another storage class: a version,
// or the current object when VersionID is empty. ETag and the times name the
// write, so the change does not touch another one of the key.
type ClassChange struct {
	VersionID    string
	ETag         string
	LastModified int64
	WrittenAt    int64
	StorageClass string
}

// StorageClassChanger moves writes to another storage class, as a lifecycle
// transition does.
type StorageClassChanger interface {
	// ChangeStorageClass moves the write to the class. It reports whether
	// this node moved it: not when the key's write is another one or already
	// of the class, nor in a cluster when another node moves it.
	ChangeStorageClass(ctx context.Context, bucket, key string, change ClassChange) (bool, error)
}

// UploadStorageClasser tells the storage class a multipart upload completes
// in.
type UploadStorageClasser interface {
	UploadStorageClass(ctx context.Context, uploadID string) (string, error)
}

// ChangeStorageClass gives the write its new class here; its data stays
// where it is.
func (om *objectManager) ChangeStorageClass(ctx context.Context, bucket, key string, change ClassChange) (bool, error) {
	defer om.lockKey(bucket, key)()
	entry, err := om.ObjectEntry(ctx, bucket, key, change.VersionID)
	if errors.Is(err, metadata.ErrObjectNotFound) || errors.Is(err, metadata.ErrVersionNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if isMetadataDeleteMarker(entry) || entry.ETag != change.ETag || entry.LastModified.Unix() != change.LastModified ||
		entry.WrittenAt != change.WrittenAt || entry.StorageClass == change.StorageClass {
		return false, nil
	}
	entry.StorageClass = change.StorageClass
	if err := om.saveEntry(ctx, entry); err != nil {
		return false, err
	}
	return true, nil
}

// UploadStorageClass is the storage class a multipart upload completes in.
func (om *objectManager) UploadStorageClass(ctx context.Context, uploadID string) (string, error) {
	upload, err := om.metadataStore.GetMultipartUpload(ctx, uploadID)
	if err != nil {
		return "", err
	}
	return upload.StorageClass, nil
}

// saveEntry stores this node's entry of a write: the current object, or one
// of its versions.
func (om *objectManager) saveEntry(ctx context.Context, entry *metadata.ObjectMetadata) error {
	if entry.VersionID == "" {
		return om.metadataStore.PutObject(ctx, entry)
	}
	return om.metadataStore.PutObjectVersion(ctx, entry, &metadata.ObjectVersion{
		VersionID:    entry.VersionID,
		IsLatest:     entry.IsLatest,
		Key:          entry.Key,
		Size:         entry.Size,
		ETag:         entry.ETag,
		LastModified: entry.LastModified,
		WrittenAt:    entry.WrittenAt,
		StorageClass: entry.StorageClass,
	})
}
