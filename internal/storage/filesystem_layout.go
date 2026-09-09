package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	bucketMarkerName     = ".maxiofs-bucket"
	multipartPartsPrefix = ".maxiofs/multipart/parts"
)

// refPath maps an object reference onto its location under the storage root.
func (fs *FilesystemBackend) refPath(ref ObjectRef) string {
	if ref.VersionID == "" {
		return ref.Bucket + "/" + ref.Key
	}
	return ref.Bucket + "/.versions/" + ref.Key + "/" + ref.VersionID
}

func partPath(uploadID string, partNumber int) string {
	return fmt.Sprintf("%s/%s/%05d", multipartPartsPrefix, uploadID, partNumber)
}

func (fs *FilesystemBackend) Put(ctx context.Context, ref ObjectRef, data io.Reader, metadata map[string]string) error {
	return fs.putAt(ctx, fs.refPath(ref), data, metadata)
}

func (fs *FilesystemBackend) Get(ctx context.Context, ref ObjectRef) (io.ReadCloser, map[string]string, error) {
	return fs.getAt(ctx, fs.refPath(ref))
}

func (fs *FilesystemBackend) Delete(ctx context.Context, ref ObjectRef) error {
	return fs.deleteAt(ctx, fs.refPath(ref))
}

func (fs *FilesystemBackend) Exists(ctx context.Context, ref ObjectRef) (bool, error) {
	return fs.existsAt(ctx, fs.refPath(ref))
}

func (fs *FilesystemBackend) GetMetadata(ctx context.Context, ref ObjectRef) (map[string]string, error) {
	return fs.metadataAt(ctx, fs.refPath(ref))
}

func (fs *FilesystemBackend) SetMetadata(ctx context.Context, ref ObjectRef, metadata map[string]string) error {
	return fs.setMetadataAt(ctx, fs.refPath(ref), metadata)
}

func (fs *FilesystemBackend) PutPart(ctx context.Context, uploadID string, partNumber int, data io.Reader, metadata map[string]string) error {
	return fs.putAt(ctx, partPath(uploadID, partNumber), data, metadata)
}

func (fs *FilesystemBackend) GetPart(ctx context.Context, uploadID string, partNumber int) (io.ReadCloser, map[string]string, error) {
	return fs.getAt(ctx, partPath(uploadID, partNumber))
}

func (fs *FilesystemBackend) PartMetadata(ctx context.Context, uploadID string, partNumber int) (map[string]string, error) {
	return fs.metadataAt(ctx, partPath(uploadID, partNumber))
}

func (fs *FilesystemBackend) PartExists(ctx context.Context, uploadID string, partNumber int) (bool, error) {
	return fs.existsAt(ctx, partPath(uploadID, partNumber))
}

func (fs *FilesystemBackend) DeletePart(ctx context.Context, uploadID string, partNumber int) error {
	return fs.deleteAt(ctx, partPath(uploadID, partNumber))
}

func (fs *FilesystemBackend) CreateBucket(ctx context.Context, bucket string) error {
	if err := fs.validatePath(bucket); err != nil {
		return err
	}

	dir := fs.getFullPath(bucket)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return NewErrorWithCause("CreateBucketDir", "Failed to create bucket directory", err)
	}

	marker, err := os.Create(filepath.Join(dir, bucketMarkerName))
	if err != nil {
		return NewErrorWithCause("CreateBucketMarker", "Failed to create bucket marker", err)
	}
	if err := marker.Close(); err != nil {
		return NewErrorWithCause("CreateBucketMarker", "Failed to close bucket marker", err)
	}

	return syncDir(dir)
}

func (fs *FilesystemBackend) DeleteBucket(ctx context.Context, bucket string) error {
	if err := fs.validatePath(bucket); err != nil {
		return err
	}
	return fs.RemoveDirectory(bucket)
}
