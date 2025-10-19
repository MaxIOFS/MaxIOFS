package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/sirupsen/logrus"
)

// ==================== Multipart Upload Operations ====================

// CreateMultipartUpload initiates a new multipart upload
func (s *BadgerStore) CreateMultipartUpload(ctx context.Context, upload *MultipartUploadMetadata) error {
	if upload == nil {
		return fmt.Errorf("multipart upload metadata cannot be nil")
	}
	if upload.UploadID == "" {
		return fmt.Errorf("upload ID is required")
	}

	return s.db.Update(func(txn *badger.Txn) error {
		// Set timestamps
		if upload.Initiated.IsZero() {
			upload.Initiated = time.Now()
		}

		// Store the upload metadata
		uploadKey := multipartUploadKey(upload.UploadID)
		data, err := json.Marshal(upload)
		if err != nil {
			return fmt.Errorf("failed to marshal multipart upload: %w", err)
		}

		// Set with TTL of 7 days (multipart uploads should be completed or aborted within this time)
		entry := badger.NewEntry(uploadKey, data).WithTTL(7 * 24 * time.Hour)
		if err := txn.SetEntry(entry); err != nil {
			return fmt.Errorf("failed to store multipart upload: %w", err)
		}

		// Create bucket index for listing uploads by bucket
		indexKey := multipartIndexKey(upload.Bucket, upload.UploadID)
		indexEntry := badger.NewEntry(indexKey, []byte{}).WithTTL(7 * 24 * time.Hour)
		if err := txn.SetEntry(indexEntry); err != nil {
			return fmt.Errorf("failed to create multipart index: %w", err)
		}

		s.logger.WithFields(logrus.Fields{
			"upload_id": upload.UploadID,
			"bucket":    upload.Bucket,
			"key":       upload.Key,
		}).Debug("Multipart upload created")

		return nil
	})
}

// GetMultipartUpload retrieves metadata for a multipart upload
func (s *BadgerStore) GetMultipartUpload(ctx context.Context, uploadID string) (*MultipartUploadMetadata, error) {
	var upload MultipartUploadMetadata

	err := s.db.View(func(txn *badger.Txn) error {
		key := multipartUploadKey(uploadID)

		item, err := txn.Get(key)
		if err == badger.ErrKeyNotFound {
			return ErrUploadNotFound
		}
		if err != nil {
			return fmt.Errorf("failed to get multipart upload: %w", err)
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &upload)
		})
	})

	if err != nil {
		return nil, err
	}

	return &upload, nil
}

// ListMultipartUploads lists all in-progress multipart uploads for a bucket
func (s *BadgerStore) ListMultipartUploads(ctx context.Context, bucket, prefix string, maxUploads int) ([]*MultipartUploadMetadata, error) {
	if maxUploads <= 0 {
		maxUploads = 1000
	}

	var uploads []*MultipartUploadMetadata

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = multipartListPrefix(bucket)

		it := txn.NewIterator(opts)
		defer it.Close()

		count := 0
		for it.Rewind(); it.Valid() && count < maxUploads; it.Next() {
			item := it.Item()
			k := string(item.Key())

			// Extract upload ID from index key
			// Format: multipart_idx:{bucket}:{uploadID}
			uploadID := k[len(string(opts.Prefix)):]

			// Fetch the actual upload metadata
			upload, err := s.GetMultipartUpload(ctx, uploadID)
			if err != nil {
				s.logger.WithError(err).Warn("Failed to get multipart upload")
				continue
			}

			// Filter by prefix if provided
			if prefix != "" && !hasPrefix(upload.Key, prefix) {
				continue
			}

			uploads = append(uploads, upload)
			count++
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort by initiated time (newest first)
	sort.Slice(uploads, func(i, j int) bool {
		return uploads[i].Initiated.After(uploads[j].Initiated)
	})

	return uploads, nil
}

// AbortMultipartUpload cancels a multipart upload and cleans up parts
func (s *BadgerStore) AbortMultipartUpload(ctx context.Context, uploadID string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		// Get upload metadata to get bucket name for index cleanup
		uploadKey := multipartUploadKey(uploadID)
		item, err := txn.Get(uploadKey)
		if err == badger.ErrKeyNotFound {
			return ErrUploadNotFound
		}
		if err != nil {
			return fmt.Errorf("failed to get multipart upload: %w", err)
		}

		var upload MultipartUploadMetadata
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &upload)
		})
		if err != nil {
			return fmt.Errorf("failed to unmarshal upload: %w", err)
		}

		// Delete all parts
		partsPrefix := partListPrefix(uploadID)
		opts := badger.DefaultIteratorOptions
		opts.Prefix = partsPrefix

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			if err := txn.Delete(item.Key()); err != nil {
				s.logger.WithError(err).Warn("Failed to delete part")
			}
		}

		// Delete bucket index
		indexKey := multipartIndexKey(upload.Bucket, uploadID)
		if err := txn.Delete(indexKey); err != nil {
			s.logger.WithError(err).Warn("Failed to delete multipart index")
		}

		// Delete the upload metadata
		if err := txn.Delete(uploadKey); err != nil {
			return fmt.Errorf("failed to delete multipart upload: %w", err)
		}

		s.logger.WithFields(logrus.Fields{
			"upload_id": uploadID,
			"bucket":    upload.Bucket,
			"key":       upload.Key,
		}).Debug("Multipart upload aborted")

		return nil
	})
}

// CompleteMultipartUpload marks a multipart upload as complete
func (s *BadgerStore) CompleteMultipartUpload(ctx context.Context, uploadID string, obj *ObjectMetadata) error {
	return s.db.Update(func(txn *badger.Txn) error {
		// Verify upload exists
		uploadKey := multipartUploadKey(uploadID)
		item, err := txn.Get(uploadKey)
		if err == badger.ErrKeyNotFound {
			return ErrUploadNotFound
		}
		if err != nil {
			return fmt.Errorf("failed to get multipart upload: %w", err)
		}

		var upload MultipartUploadMetadata
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &upload)
		})
		if err != nil {
			return fmt.Errorf("failed to unmarshal upload: %w", err)
		}

		// Store the completed object metadata
		objKey := objectKey(obj.Bucket, obj.Key)
		objData, err := json.Marshal(obj)
		if err != nil {
			return fmt.Errorf("failed to marshal object: %w", err)
		}

		if err := txn.Set(objKey, objData); err != nil {
			return fmt.Errorf("failed to store object: %w", err)
		}

		// Delete all parts (we don't need them anymore)
		partsPrefix := partListPrefix(uploadID)
		opts := badger.DefaultIteratorOptions
		opts.Prefix = partsPrefix

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			if err := txn.Delete(item.Key()); err != nil {
				s.logger.WithError(err).Warn("Failed to delete part")
			}
		}

		// Delete bucket index
		indexKey := multipartIndexKey(upload.Bucket, uploadID)
		if err := txn.Delete(indexKey); err != nil {
			s.logger.WithError(err).Warn("Failed to delete multipart index")
		}

		// Delete the upload metadata
		if err := txn.Delete(uploadKey); err != nil {
			return fmt.Errorf("failed to delete multipart upload: %w", err)
		}

		s.logger.WithFields(logrus.Fields{
			"upload_id": uploadID,
			"bucket":    upload.Bucket,
			"key":       upload.Key,
		}).Debug("Multipart upload completed")

		return nil
	})
}

// ==================== Part Operations ====================

// PutPart stores metadata for a multipart upload part
func (s *BadgerStore) PutPart(ctx context.Context, part *PartMetadata) error {
	if part == nil {
		return fmt.Errorf("part metadata cannot be nil")
	}
	if part.UploadID == "" || part.PartNumber <= 0 {
		return fmt.Errorf("invalid part metadata")
	}

	return s.db.Update(func(txn *badger.Txn) error {
		// Verify upload exists
		uploadKey := multipartUploadKey(part.UploadID)
		_, err := txn.Get(uploadKey)
		if err == badger.ErrKeyNotFound {
			return ErrUploadNotFound
		}
		if err != nil {
			return fmt.Errorf("failed to verify upload: %w", err)
		}

		// Set timestamp
		if part.LastModified.IsZero() {
			part.LastModified = time.Now()
		}

		// Store part metadata
		key := partKey(part.UploadID, part.PartNumber)
		data, err := json.Marshal(part)
		if err != nil {
			return fmt.Errorf("failed to marshal part: %w", err)
		}

		// Set with TTL matching the upload TTL
		entry := badger.NewEntry(key, data).WithTTL(7 * 24 * time.Hour)
		if err := txn.SetEntry(entry); err != nil {
			return fmt.Errorf("failed to store part: %w", err)
		}

		s.logger.WithFields(logrus.Fields{
			"upload_id":   part.UploadID,
			"part_number": part.PartNumber,
			"size":        part.Size,
		}).Debug("Part stored")

		return nil
	})
}

// GetPart retrieves metadata for a specific part
func (s *BadgerStore) GetPart(ctx context.Context, uploadID string, partNumber int) (*PartMetadata, error) {
	var part PartMetadata

	err := s.db.View(func(txn *badger.Txn) error {
		key := partKey(uploadID, partNumber)

		item, err := txn.Get(key)
		if err == badger.ErrKeyNotFound {
			return ErrPartNotFound
		}
		if err != nil {
			return fmt.Errorf("failed to get part: %w", err)
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &part)
		})
	})

	if err != nil {
		return nil, err
	}

	return &part, nil
}

// ListParts lists all parts for a multipart upload
func (s *BadgerStore) ListParts(ctx context.Context, uploadID string) ([]*PartMetadata, error) {
	var parts []*PartMetadata

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = partListPrefix(uploadID)

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()

			var part PartMetadata
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &part)
			})
			if err != nil {
				s.logger.WithError(err).Warn("Failed to unmarshal part metadata")
				continue
			}

			parts = append(parts, &part)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort parts by part number
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})

	return parts, nil
}

// ==================== Helper Functions ====================

// hasPrefix checks if a string has a given prefix (case-sensitive)
func hasPrefix(s, prefix string) bool {
	if prefix == "" {
		return true
	}
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
