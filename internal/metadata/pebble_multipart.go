package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/sirupsen/logrus"
)

// ==================== Multipart Upload Operations ====================
//
// Pebble does not support per-key TTL natively. Expiry of stale uploads is
// handled by runMultipartCleanup, a goroutine that scans the multipart index
// hourly and removes entries older than 7 days — matching the behaviour
// BadgerDB provided via its WithTTL entry option.

// CreateMultipartUpload initiates a new multipart upload.
func (s *PebbleStore) CreateMultipartUpload(ctx context.Context, upload *MultipartUploadMetadata) error {
	if upload == nil {
		return fmt.Errorf("multipart upload metadata cannot be nil")
	}
	if upload.UploadID == "" {
		return fmt.Errorf("upload ID is required")
	}

	if upload.Initiated.IsZero() {
		upload.Initiated = time.Now()
	}

	data, err := json.Marshal(upload)
	if err != nil {
		return fmt.Errorf("failed to marshal multipart upload: %w", err)
	}

	batch := s.db.NewBatch()
	defer batch.Close() //nolint:errcheck

	uploadKey := multipartUploadKey(upload.UploadID)
	if err := batch.Set(uploadKey, data, nil); err != nil {
		return fmt.Errorf("failed to set multipart upload: %w", err)
	}

	indexKey := multipartIndexKey(upload.Bucket, upload.UploadID)
	if err := batch.Set(indexKey, []byte{}, nil); err != nil {
		return fmt.Errorf("failed to set multipart index: %w", err)
	}

	if err := batch.Commit(pebble.NoSync); err != nil {
		return fmt.Errorf("failed to commit multipart upload: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"upload_id": upload.UploadID,
		"bucket":    upload.Bucket,
		"key":       upload.Key,
	}).Debug("Multipart upload created in Pebble")

	return nil
}

// GetMultipartUpload retrieves metadata for a multipart upload.
func (s *PebbleStore) GetMultipartUpload(ctx context.Context, uploadID string) (*MultipartUploadMetadata, error) {
	key := multipartUploadKey(uploadID)
	data, err := s.pebbleGet(key)
	if err == pebble.ErrNotFound {
		return nil, ErrUploadNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get multipart upload: %w", err)
	}

	var upload MultipartUploadMetadata
	if err := json.Unmarshal(data, &upload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal multipart upload: %w", err)
	}
	return &upload, nil
}

// ListMultipartUploads lists in-progress uploads for a bucket with optional prefix filter.
func (s *PebbleStore) ListMultipartUploads(ctx context.Context, bucket, prefix string, maxUploads int) ([]*MultipartUploadMetadata, error) {
	if maxUploads <= 0 {
		maxUploads = 1000
	}

	lower := multipartListPrefix(bucket)
	iter, err := s.pebbleIter(lower)
	if err != nil {
		return nil, err
	}
	defer iter.Close() //nolint:errcheck

	var uploads []*MultipartUploadMetadata
	count := 0
	for iter.First(); iter.Valid() && count < maxUploads; iter.Next() {
		k := string(iter.Key())
		// multipart_idx:{bucket}:{uploadID}
		uploadID := k[len(string(lower)):]

		upload, err := s.GetMultipartUpload(ctx, uploadID)
		if err != nil {
			s.logger.WithError(err).Warn("Failed to get multipart upload during list")
			continue
		}

		if prefix != "" && !hasPrefix(upload.Key, prefix) {
			continue
		}

		uploads = append(uploads, upload)
		count++
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("failed during multipart list: %w", err)
	}

	sort.Slice(uploads, func(i, j int) bool {
		return uploads[i].Initiated.After(uploads[j].Initiated)
	})
	return uploads, nil
}

// AbortMultipartUpload cancels a multipart upload and removes all its parts.
func (s *PebbleStore) AbortMultipartUpload(ctx context.Context, uploadID string) error {
	// Read the upload to get its bucket (needed for index key)
	upload, err := s.GetMultipartUpload(ctx, uploadID)
	if err != nil {
		return err // already ErrUploadNotFound if not found
	}

	batch := s.db.NewBatch()
	defer batch.Close() //nolint:errcheck

	// Collect all part keys to delete
	partsLower := partListPrefix(uploadID)
	iter, err := s.pebbleIter(partsLower)
	if err != nil {
		return err
	}
	var partKeys [][]byte
	for iter.First(); iter.Valid(); iter.Next() {
		keyCopy := make([]byte, len(iter.Key()))
		copy(keyCopy, iter.Key())
		partKeys = append(partKeys, keyCopy)
	}
	iterErr := iter.Error()
	_ = iter.Close()
	if iterErr != nil {
		return fmt.Errorf("failed iterating parts: %w", iterErr)
	}

	for _, pk := range partKeys {
		if err := batch.Delete(pk, nil); err != nil {
			s.logger.WithError(err).Warn("Failed to delete part in batch")
		}
	}

	indexKey := multipartIndexKey(upload.Bucket, uploadID)
	if err := batch.Delete(indexKey, nil); err != nil {
		s.logger.WithError(err).Warn("Failed to delete multipart index in batch")
	}

	uploadKey := multipartUploadKey(uploadID)
	if err := batch.Delete(uploadKey, nil); err != nil {
		return fmt.Errorf("failed to delete multipart upload in batch: %w", err)
	}

	if err := batch.Commit(pebble.NoSync); err != nil {
		return fmt.Errorf("failed to commit abort: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"upload_id": uploadID,
		"bucket":    upload.Bucket,
		"key":       upload.Key,
	}).Debug("Multipart upload aborted in Pebble")

	return nil
}

// CompleteMultipartUpload finalises an upload: stores the completed object and removes upload/part metadata.
func (s *PebbleStore) CompleteMultipartUpload(ctx context.Context, uploadID string, obj *ObjectMetadata) error {
	// Read the upload to get its bucket (needed for index key)
	upload, err := s.GetMultipartUpload(ctx, uploadID)
	if err != nil {
		return err
	}

	// Marshal the completed object
	now := time.Now()
	if obj.CreatedAt.IsZero() {
		obj.CreatedAt = now
	}
	obj.UpdatedAt = now
	obj.LastModified = now

	objData, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("failed to marshal completed object: %w", err)
	}

	batch := s.db.NewBatch()
	defer batch.Close() //nolint:errcheck

	// Store the completed object
	objKey := objectKey(obj.Bucket, obj.Key)
	if err := batch.Set(objKey, objData, nil); err != nil {
		return fmt.Errorf("failed to set object in batch: %w", err)
	}

	// Collect and delete all part keys
	partsLower := partListPrefix(uploadID)
	iter, err := s.pebbleIter(partsLower)
	if err != nil {
		return err
	}
	var partKeys [][]byte
	for iter.First(); iter.Valid(); iter.Next() {
		keyCopy := make([]byte, len(iter.Key()))
		copy(keyCopy, iter.Key())
		partKeys = append(partKeys, keyCopy)
	}
	iterErr := iter.Error()
	_ = iter.Close()
	if iterErr != nil {
		return fmt.Errorf("failed iterating parts: %w", iterErr)
	}

	for _, pk := range partKeys {
		if err := batch.Delete(pk, nil); err != nil {
			s.logger.WithError(err).Warn("Failed to delete part in batch")
		}
	}

	indexKey := multipartIndexKey(upload.Bucket, uploadID)
	if err := batch.Delete(indexKey, nil); err != nil {
		s.logger.WithError(err).Warn("Failed to delete multipart index in batch")
	}

	uploadKey := multipartUploadKey(uploadID)
	if err := batch.Delete(uploadKey, nil); err != nil {
		return fmt.Errorf("failed to delete multipart upload in batch: %w", err)
	}

	if err := batch.Commit(pebble.NoSync); err != nil {
		return fmt.Errorf("failed to commit complete: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"upload_id": uploadID,
		"bucket":    upload.Bucket,
		"key":       upload.Key,
	}).Debug("Multipart upload completed in Pebble")

	return nil
}

// ==================== Part Operations ====================

// PutPart stores metadata for a multipart upload part.
func (s *PebbleStore) PutPart(ctx context.Context, part *PartMetadata) error {
	if part == nil {
		return fmt.Errorf("part metadata cannot be nil")
	}
	if part.UploadID == "" || part.PartNumber <= 0 {
		return fmt.Errorf("invalid part metadata")
	}

	// Verify upload exists
	uploadKey := multipartUploadKey(part.UploadID)
	if _, closer, err := s.db.Get(uploadKey); err == pebble.ErrNotFound {
		return ErrUploadNotFound
	} else if err != nil {
		return fmt.Errorf("failed to verify upload: %w", err)
	} else {
		_ = closer.Close()
	}

	if part.LastModified.IsZero() {
		part.LastModified = time.Now()
	}

	data, err := json.Marshal(part)
	if err != nil {
		return fmt.Errorf("failed to marshal part: %w", err)
	}

	key := partKey(part.UploadID, part.PartNumber)
	if err := s.db.Set(key, data, pebble.NoSync); err != nil {
		return fmt.Errorf("failed to store part: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"upload_id":   part.UploadID,
		"part_number": part.PartNumber,
		"size":        part.Size,
	}).Debug("Part stored in Pebble")

	return nil
}

// GetPart retrieves metadata for a specific part.
func (s *PebbleStore) GetPart(ctx context.Context, uploadID string, partNumber int) (*PartMetadata, error) {
	key := partKey(uploadID, partNumber)
	data, err := s.pebbleGet(key)
	if err == pebble.ErrNotFound {
		return nil, ErrPartNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get part: %w", err)
	}

	var part PartMetadata
	if err := json.Unmarshal(data, &part); err != nil {
		return nil, fmt.Errorf("failed to unmarshal part: %w", err)
	}
	return &part, nil
}

// ListParts lists all parts for a multipart upload, sorted by part number.
func (s *PebbleStore) ListParts(ctx context.Context, uploadID string) ([]*PartMetadata, error) {
	lower := partListPrefix(uploadID)
	iter, err := s.pebbleIter(lower)
	if err != nil {
		return nil, err
	}
	defer iter.Close() //nolint:errcheck

	var parts []*PartMetadata
	for iter.First(); iter.Valid(); iter.Next() {
		val := iter.Value()
		valCopy := make([]byte, len(val))
		copy(valCopy, val)

		var part PartMetadata
		if err := json.Unmarshal(valCopy, &part); err != nil {
			s.logger.WithError(err).Warn("Failed to unmarshal part metadata")
			continue
		}
		parts = append(parts, &part)
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("failed during parts list: %w", err)
	}

	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})
	return parts, nil
}

// ==================== TTL Cleanup Goroutine ====================

// runMultipartCleanup removes stale multipart uploads (> 7 days old) every hour.
// This replaces the BadgerDB per-entry TTL that Pebble does not support natively.
func (s *PebbleStore) runMultipartCleanup() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.cleanupStaleMultipartUploads()
		}
	}
}

func (s *PebbleStore) cleanupStaleMultipartUploads() {
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	ctx := context.Background()

	// Iterate all multipart_idx: keys to find all upload IDs
	prefix := []byte("multipart_idx:")
	iter, err := s.pebbleIter(prefix)
	if err != nil {
		s.logger.WithError(err).Warn("Failed to create iterator for multipart cleanup")
		return
	}

	var staleUploadIDs []string
	for iter.First(); iter.Valid(); iter.Next() {
		k := string(iter.Key())
		// multipart_idx:{bucket}:{uploadID}
		// We need the uploadID part — everything after the second colon after "multipart_idx:"
		// Format: multipart_idx:{bucket}:{uploadID}
		// Split on ":" with max 3 parts: ["multipart_idx", "{bucket}", "{uploadID}"]
		// But bucket itself shouldn't contain ":", so 3-part split is fine.
		// Actually: multipart_idx:{bucket}:{uploadID} — bucket has no colon so:
		//   parts[0] = "multipart_idx"
		//   parts[1] = bucket
		//   parts[2] = uploadID
		// But we need SplitN(k, ":", 3) to be safe if bucket has slashes.
		keySuffix := k[len("multipart_idx:"):]
		// keySuffix = "{bucket}:{uploadID}"
		colonIdx := -1
		for i := len(keySuffix) - 1; i >= 0; i-- {
			if keySuffix[i] == ':' {
				colonIdx = i
				break
			}
		}
		if colonIdx < 0 {
			continue
		}
		uploadID := keySuffix[colonIdx+1:]

		upload, err := s.GetMultipartUpload(ctx, uploadID)
		if err != nil {
			continue
		}
		if upload.Initiated.Before(cutoff) {
			staleUploadIDs = append(staleUploadIDs, uploadID)
		}
	}
	_ = iter.Error()
	_ = iter.Close()

	for _, uploadID := range staleUploadIDs {
		if err := s.AbortMultipartUpload(ctx, uploadID); err != nil {
			s.logger.WithFields(logrus.Fields{
				"upload_id": uploadID,
			}).WithError(err).Warn("Failed to clean up stale multipart upload")
		} else {
			s.logger.WithField("upload_id", uploadID).Debug("Cleaned up stale multipart upload")
		}
	}

	if len(staleUploadIDs) > 0 {
		s.logger.WithField("count", len(staleUploadIDs)).Info("Cleaned up stale multipart uploads")
	}
}
