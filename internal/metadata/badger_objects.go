package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/sirupsen/logrus"
)

// ==================== Object Operations ====================

// PutObject stores or updates object metadata
func (s *BadgerStore) PutObject(ctx context.Context, obj *ObjectMetadata) error {
	if obj == nil {
		return fmt.Errorf("object metadata cannot be nil")
	}
	if obj.Bucket == "" || obj.Key == "" {
		return ErrInvalidKey
	}

	key := objectKey(obj.Bucket, obj.Key)

	return s.db.Update(func(txn *badger.Txn) error {
		// Set timestamps
		now := time.Now()
		if obj.CreatedAt.IsZero() {
			obj.CreatedAt = now
		}
		obj.UpdatedAt = now
		obj.LastModified = now

		// Marshal and store
		data, err := json.Marshal(obj)
		if err != nil {
			return fmt.Errorf("failed to marshal object metadata: %w", err)
		}

		if err := txn.Set(key, data); err != nil {
			return fmt.Errorf("failed to store object: %w", err)
		}

		// Create tag indices if tags exist
		if len(obj.Tags) > 0 {
			for tagKey, tagValue := range obj.Tags {
				tagKey := tagIndexKey(obj.Bucket, tagKey, tagValue, obj.Key)
				if err := txn.Set(tagKey, []byte{}); err != nil {
					return fmt.Errorf("failed to create tag index: %w", err)
				}
			}
		}

		s.logger.WithFields(logrus.Fields{
			"bucket": obj.Bucket,
			"key":    obj.Key,
			"size":   obj.Size,
		}).Debug("Object metadata stored")

		return nil
	})
}

// GetObject retrieves object metadata
func (s *BadgerStore) GetObject(ctx context.Context, bucket, key string, versionID ...string) (*ObjectMetadata, error) {
	if bucket == "" || key == "" {
		return nil, ErrInvalidKey
	}

	var obj ObjectMetadata

	err := s.db.View(func(txn *badger.Txn) error {
		var objKey []byte
		if len(versionID) > 0 && versionID[0] != "" {
			objKey = objectVersionKey(bucket, key, versionID[0])
		} else {
			objKey = objectKey(bucket, key)
		}

		item, err := txn.Get(objKey)
		if err == badger.ErrKeyNotFound {
			return ErrObjectNotFound
		}
		if err != nil {
			return fmt.Errorf("failed to get object: %w", err)
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &obj)
		})
	})

	if err != nil {
		return nil, err
	}

	// Ensure bucket and key are always set (version metadata doesn't store these fields)
	if obj.Bucket == "" {
		obj.Bucket = bucket
	}
	if obj.Key == "" {
		obj.Key = key
	}

	return &obj, nil
}

// DeleteObject deletes object metadata
func (s *BadgerStore) DeleteObject(ctx context.Context, bucket, key string, versionID ...string) error {
	if bucket == "" || key == "" {
		return ErrInvalidKey
	}

	// Retry logic for transaction conflicts (up to 5 attempts with exponential backoff)
	maxRetries := 5
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		err := s.db.Update(func(txn *badger.Txn) error {
			var objKey []byte
			if len(versionID) > 0 && versionID[0] != "" {
				objKey = objectVersionKey(bucket, key, versionID[0])
			} else {
				objKey = objectKey(bucket, key)
			}

			// Get object to retrieve tags before deletion
			item, err := txn.Get(objKey)
			if err == badger.ErrKeyNotFound {
				return ErrObjectNotFound
			}
			if err != nil {
				return fmt.Errorf("failed to get object: %w", err)
			}

			var obj ObjectMetadata
			err = item.Value(func(val []byte) error {
				return json.Unmarshal(val, &obj)
			})
			if err != nil {
				return fmt.Errorf("failed to unmarshal object: %w", err)
			}

			// Delete tag indices
			if len(obj.Tags) > 0 {
				for tagKey, tagValue := range obj.Tags {
					tagIdxKey := tagIndexKey(bucket, tagKey, tagValue, key)
					if err := txn.Delete(tagIdxKey); err != nil {
						s.logger.WithError(err).Warn("Failed to delete tag index")
					}
				}
			}

			// Delete the object
			if err := txn.Delete(objKey); err != nil {
				return fmt.Errorf("failed to delete object: %w", err)
			}

			s.logger.WithFields(logrus.Fields{
				"bucket": bucket,
				"key":    key,
			}).Debug("Object metadata deleted")

			return nil
		})

		if err == nil {
			return nil // Success
		}

		lastErr = err

		// Check if it's a transaction conflict that we should retry
		if err == badger.ErrConflict {
			if attempt < maxRetries-1 {
				// Exponential backoff: 1ms, 2ms, 4ms, 8ms
				backoff := (1 << attempt) * 1000000 // nanoseconds
				time.Sleep(time.Duration(backoff))
				continue
			}
		} else {
			// For non-conflict errors, return immediately
			return err
		}
	}

	// If we exhausted all retries, return the last error
	return fmt.Errorf("failed after %d retries: %w", maxRetries, lastErr)
}

// ListObjects lists objects in a bucket with optional prefix and pagination
func (s *BadgerStore) ListObjects(ctx context.Context, bucket, prefix, marker string, maxKeys int) ([]*ObjectMetadata, string, error) {
	if bucket == "" {
		return nil, "", fmt.Errorf("bucket name is required")
	}

	if maxKeys <= 0 {
		maxKeys = 1000 // Default max keys
	}

	var objects []*ObjectMetadata
	var nextMarker string

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100

		// Set the prefix for iteration
		if prefix != "" {
			opts.Prefix = objectPrefixKey(bucket, prefix)
		} else {
			opts.Prefix = objectListPrefix(bucket)
		}

		it := txn.NewIterator(opts)
		defer it.Close()

		// Seek to marker if provided
		var seekKey []byte
		if marker != "" {
			seekKey = objectKey(bucket, marker)
		} else {
			seekKey = opts.Prefix
		}

		count := 0
		started := marker == ""

		for it.Seek(seekKey); it.ValidForPrefix(opts.Prefix); it.Next() {
			if count >= maxKeys {
				// Set next marker for pagination
				item := it.Item()
				k := string(item.Key())
				nextMarker = extractObjectKeyFromKey(k)
				break
			}

			item := it.Item()
			k := string(item.Key())
			objectKey := extractObjectKeyFromKey(k)

			// Skip the marker itself
			if !started {
				if objectKey == marker {
					started = true
				}
				continue
			}

			var obj ObjectMetadata
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &obj)
			})
			if err != nil {
				s.logger.WithError(err).Warn("Failed to unmarshal object metadata")
				continue
			}

			objects = append(objects, &obj)
			count++
		}

		return nil
	})

	if err != nil {
		return nil, "", err
	}

	return objects, nextMarker, nil
}

// ObjectExists checks if an object exists
func (s *BadgerStore) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	if bucket == "" || key == "" {
		return false, ErrInvalidKey
	}

	objKey := objectKey(bucket, key)

	err := s.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get(objKey)
		return err
	})

	if err == badger.ErrKeyNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

// ==================== Object Versioning ====================

// PutObjectVersion stores a new version of an object
func (s *BadgerStore) PutObjectVersion(ctx context.Context, obj *ObjectMetadata, version *ObjectVersion) error {
	if obj == nil || version == nil {
		return fmt.Errorf("object and version metadata cannot be nil")
	}

	return s.db.Update(func(txn *badger.Txn) error {
		// If this is the latest version, mark all previous versions as not latest
		if version.IsLatest {
			// Get all existing versions
			opts := badger.DefaultIteratorOptions
			prefix := []byte(fmt.Sprintf("version:%s:%s:", obj.Bucket, obj.Key))
			opts.Prefix = prefix

			it := txn.NewIterator(opts)
			defer it.Close()

			for it.Rewind(); it.Valid(); it.Next() {
				item := it.Item()
				var existingVersion ObjectVersion

				err := item.Value(func(val []byte) error {
					return json.Unmarshal(val, &existingVersion)
				})
				if err != nil {
					continue
				}

				// Mark as not latest
				if existingVersion.IsLatest {
					existingVersion.IsLatest = false
					updatedData, err := json.Marshal(&existingVersion)
					if err != nil {
						continue
					}
					if err := txn.Set(item.Key(), updatedData); err != nil {
						s.logger.WithError(err).Warn("Failed to update existing version")
					}
				}
			}
		}

		// Store the new version
		versionKey := objectVersionKey(obj.Bucket, obj.Key, version.VersionID)
		versionData, err := json.Marshal(version)
		if err != nil {
			return fmt.Errorf("failed to marshal version: %w", err)
		}

		if err := txn.Set(versionKey, versionData); err != nil {
			return fmt.Errorf("failed to store version: %w", err)
		}

		// Update the main object if this is the latest version
		if version.IsLatest {
			objKey := objectKey(obj.Bucket, obj.Key)
			objData, err := json.Marshal(obj)
			if err != nil {
				return fmt.Errorf("failed to marshal object: %w", err)
			}

			if err := txn.Set(objKey, objData); err != nil {
				return fmt.Errorf("failed to store object: %w", err)
			}
		}

		return nil
	})
}

// GetObjectVersions retrieves all versions of an object
func (s *BadgerStore) GetObjectVersions(ctx context.Context, bucket, key string) ([]*ObjectVersion, error) {
	var versions []*ObjectVersion

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		prefix := []byte(fmt.Sprintf("version:%s:%s:", bucket, key))
		opts.Prefix = prefix

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()

			var version ObjectVersion
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &version)
			})
			if err != nil {
				s.logger.WithError(err).Warn("Failed to unmarshal version metadata")
				continue
			}

			versions = append(versions, &version)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort versions by LastModified descending (newest first)
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].LastModified.After(versions[j].LastModified)
	})

	return versions, nil
}

// ListAllObjectVersions lists all versions of all objects in a bucket
func (s *BadgerStore) ListAllObjectVersions(ctx context.Context, bucket, prefix string, maxKeys int) ([]*ObjectVersion, error) {
	var allVersions []*ObjectVersion
	keysWithVersions := make(map[string]bool)

	err := s.db.View(func(txn *badger.Txn) error {
		// First, collect all version entries
		opts := badger.DefaultIteratorOptions
		versionPrefix := []byte(fmt.Sprintf("version:%s:", bucket))
		opts.Prefix = versionPrefix

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()

			var version ObjectVersion
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &version)
			})
			if err != nil {
				s.logger.WithError(err).Warn("Failed to unmarshal version metadata")
				continue
			}

			// Apply prefix filter if specified
			if prefix != "" && !strings.HasPrefix(version.Key, prefix) {
				continue
			}

			allVersions = append(allVersions, &version)
			keysWithVersions[version.Key] = true

			// Apply maxKeys limit if specified
			if maxKeys > 0 && len(allVersions) >= maxKeys {
				return nil
			}
		}

		// Second, collect main object entries for objects without versions (non-versioned buckets)
		objectPrefix := []byte(fmt.Sprintf("obj:%s:", bucket))
		opts2 := badger.DefaultIteratorOptions
		opts2.Prefix = objectPrefix

		it2 := txn.NewIterator(opts2)
		defer it2.Close()

		for it2.Rewind(); it2.Valid(); it2.Next() {
			item := it2.Item()

			var obj ObjectMetadata
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &obj)
			})
			if err != nil {
				s.logger.WithError(err).Warn("Failed to unmarshal object metadata")
				continue
			}

			// Skip if this key already has versions
			if keysWithVersions[obj.Key] {
				continue
			}

			// Apply prefix filter if specified
			if prefix != "" && !strings.HasPrefix(obj.Key, prefix) {
				continue
			}

			// Convert ObjectMetadata to ObjectVersion (for non-versioned objects)
			version := &ObjectVersion{
				Key:          obj.Key,
				VersionID:    "", // Will be converted to "null" by the handler
				IsLatest:     true,
				LastModified: obj.LastModified,
				ETag:         obj.ETag,
				Size:         obj.Size,
			}

			allVersions = append(allVersions, version)

			// Apply maxKeys limit if specified
			if maxKeys > 0 && len(allVersions) >= maxKeys {
				return nil
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Sort versions by Key, then by LastModified descending
	sort.Slice(allVersions, func(i, j int) bool {
		if allVersions[i].Key != allVersions[j].Key {
			return allVersions[i].Key < allVersions[j].Key
		}
		return allVersions[i].LastModified.After(allVersions[j].LastModified)
	})

	return allVersions, nil
}

// DeleteObjectVersion deletes a specific version of an object
func (s *BadgerStore) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	versionKey := objectVersionKey(bucket, key, versionID)

	return s.db.Update(func(txn *badger.Txn) error {
		_, err := txn.Get(versionKey)
		if err == badger.ErrKeyNotFound {
			return ErrVersionNotFound
		}
		if err != nil {
			return fmt.Errorf("failed to get version: %w", err)
		}

		return txn.Delete(versionKey)
	})
}

// ==================== Tags ====================

// PutObjectTags sets tags for an object
func (s *BadgerStore) PutObjectTags(ctx context.Context, bucket, key string, tags map[string]string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		// Get the object
		objKey := objectKey(bucket, key)
		item, err := txn.Get(objKey)
		if err == badger.ErrKeyNotFound {
			return ErrObjectNotFound
		}
		if err != nil {
			return fmt.Errorf("failed to get object: %w", err)
		}

		var obj ObjectMetadata
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &obj)
		})
		if err != nil {
			return fmt.Errorf("failed to unmarshal object: %w", err)
		}

		// Delete old tag indices
		if len(obj.Tags) > 0 {
			for tagKey, tagValue := range obj.Tags {
				tagIdxKey := tagIndexKey(bucket, tagKey, tagValue, key)
				txn.Delete(tagIdxKey)
			}
		}

		// Update tags
		obj.Tags = tags
		obj.UpdatedAt = time.Now()

		// Create new tag indices
		if len(tags) > 0 {
			for tagKey, tagValue := range tags {
				tagIdxKey := tagIndexKey(bucket, tagKey, tagValue, key)
				if err := txn.Set(tagIdxKey, []byte{}); err != nil {
					return fmt.Errorf("failed to create tag index: %w", err)
				}
			}
		}

		// Store updated object
		data, err := json.Marshal(&obj)
		if err != nil {
			return fmt.Errorf("failed to marshal object: %w", err)
		}

		return txn.Set(objKey, data)
	})
}

// GetObjectTags retrieves tags for an object
func (s *BadgerStore) GetObjectTags(ctx context.Context, bucket, key string) (map[string]string, error) {
	obj, err := s.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, err
	}

	if obj.Tags == nil {
		return make(map[string]string), nil
	}

	return obj.Tags, nil
}

// DeleteObjectTags removes all tags from an object
func (s *BadgerStore) DeleteObjectTags(ctx context.Context, bucket, key string) error {
	return s.PutObjectTags(ctx, bucket, key, nil)
}

// ListObjectsByTags finds objects matching specific tags
func (s *BadgerStore) ListObjectsByTags(ctx context.Context, bucket string, tags map[string]string) ([]*ObjectMetadata, error) {
	if len(tags) == 0 {
		return nil, fmt.Errorf("at least one tag is required")
	}

	// For simplicity, we'll search by the first tag and then filter by the rest
	// A more efficient implementation would use a bitmap index
	var firstTagKey, firstTagValue string
	for k, v := range tags {
		firstTagKey = k
		firstTagValue = v
		break
	}

	var candidateKeys []string

	// Find all objects with the first tag
	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = tagIndexPrefix(bucket, firstTagKey, firstTagValue)

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			k := string(item.Key())

			// Extract object key from tag index key: tag_idx:{bucket}:{tagKey}:{tagValue}:{objectKey}
			parts := strings.SplitN(k, ":", 5)
			if len(parts) == 5 {
				candidateKeys = append(candidateKeys, parts[4])
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Fetch and filter objects
	var objects []*ObjectMetadata

	for _, objKey := range candidateKeys {
		obj, err := s.GetObject(ctx, bucket, objKey)
		if err != nil {
			continue
		}

		// Check if object has all required tags
		if matchesTags(obj.Tags, tags) {
			objects = append(objects, obj)
		}
	}

	return objects, nil
}

// matchesFilter checks if an object matches all filter criteria
func matchesFilter(obj *ObjectMetadata, filter *ObjectFilter) bool {
	if filter == nil {
		return true
	}

	// Content-type prefix match
	if len(filter.ContentTypes) > 0 {
		matched := false
		for _, ct := range filter.ContentTypes {
			if strings.HasPrefix(obj.ContentType, ct) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Size range
	if filter.MinSize != nil && obj.Size < *filter.MinSize {
		return false
	}
	if filter.MaxSize != nil && obj.Size > *filter.MaxSize {
		return false
	}

	// Date range
	if filter.ModifiedAfter != nil && !obj.LastModified.After(*filter.ModifiedAfter) {
		return false
	}
	if filter.ModifiedBefore != nil && !obj.LastModified.Before(*filter.ModifiedBefore) {
		return false
	}

	// Tags (AND semantics)
	if len(filter.Tags) > 0 {
		if !matchesTags(obj.Tags, filter.Tags) {
			return false
		}
	}

	return true
}

// SearchObjects searches objects with filters and pagination
func (s *BadgerStore) SearchObjects(ctx context.Context, bucket, prefix, marker string, maxKeys int, filter *ObjectFilter) ([]*ObjectMetadata, string, error) {
	if bucket == "" {
		return nil, "", fmt.Errorf("bucket name is required")
	}

	if maxKeys <= 0 {
		maxKeys = 1000
	}

	// If filter has tags, use tag index to get candidates first
	if filter != nil && len(filter.Tags) > 0 {
		return s.searchObjectsWithTags(ctx, bucket, prefix, marker, maxKeys, filter)
	}

	return s.searchObjectsByScan(ctx, bucket, prefix, marker, maxKeys, filter)
}

// searchObjectsByScan does a prefix scan and applies filters on each object
func (s *BadgerStore) searchObjectsByScan(ctx context.Context, bucket, prefix, marker string, maxKeys int, filter *ObjectFilter) ([]*ObjectMetadata, string, error) {
	var objects []*ObjectMetadata
	var nextMarker string
	scanLimit := 100000 // scan up to 100k to handle sparse matches

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100

		if prefix != "" {
			opts.Prefix = objectPrefixKey(bucket, prefix)
		} else {
			opts.Prefix = objectListPrefix(bucket)
		}

		it := txn.NewIterator(opts)
		defer it.Close()

		var seekKey []byte
		if marker != "" {
			seekKey = objectKey(bucket, marker)
		} else {
			seekKey = opts.Prefix
		}

		count := 0
		scanned := 0
		started := marker == ""

		for it.Seek(seekKey); it.ValidForPrefix(opts.Prefix); it.Next() {
			if count >= maxKeys {
				item := it.Item()
				k := string(item.Key())
				nextMarker = extractObjectKeyFromKey(k)
				break
			}

			if scanned >= scanLimit {
				// Set next marker to allow pagination to continue
				item := it.Item()
				k := string(item.Key())
				nextMarker = extractObjectKeyFromKey(k)
				break
			}
			scanned++

			item := it.Item()
			k := string(item.Key())
			objKeyStr := extractObjectKeyFromKey(k)

			if !started {
				if objKeyStr == marker {
					started = true
				}
				continue
			}

			var obj ObjectMetadata
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &obj)
			})
			if err != nil {
				s.logger.WithError(err).Warn("Failed to unmarshal object metadata during search")
				continue
			}

			if matchesFilter(&obj, filter) {
				objects = append(objects, &obj)
				count++
			}
		}

		return nil
	})

	if err != nil {
		return nil, "", err
	}

	return objects, nextMarker, nil
}

// searchObjectsWithTags uses tag index to find candidates then applies remaining filters
func (s *BadgerStore) searchObjectsWithTags(ctx context.Context, bucket, prefix, marker string, maxKeys int, filter *ObjectFilter) ([]*ObjectMetadata, string, error) {
	// Get candidate keys from tag index using the first tag
	var firstTagKey, firstTagValue string
	for k, v := range filter.Tags {
		firstTagKey = k
		firstTagValue = v
		break
	}

	var candidateKeys []string

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = tagIndexPrefix(bucket, firstTagKey, firstTagValue)

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			k := string(item.Key())
			parts := strings.SplitN(k, ":", 5)
			if len(parts) == 5 {
				objKey := parts[4]
				// Apply prefix filter
				if prefix != "" && !strings.HasPrefix(objKey, prefix) {
					continue
				}
				// Apply marker filter
				if marker != "" && objKey <= marker {
					continue
				}
				candidateKeys = append(candidateKeys, objKey)
			}
		}
		return nil
	})

	if err != nil {
		return nil, "", err
	}

	// Sort candidate keys for consistent ordering
	sort.Strings(candidateKeys)

	// Fetch full metadata and apply remaining filters
	var objects []*ObjectMetadata
	var nextMarker string

	for _, objKey := range candidateKeys {
		if len(objects) >= maxKeys {
			nextMarker = objKey
			break
		}

		obj, err := s.GetObject(ctx, bucket, objKey)
		if err != nil {
			continue
		}

		if matchesFilter(obj, filter) {
			objects = append(objects, obj)
		}
	}

	return objects, nextMarker, nil
}

// matchesTags checks if object tags match all required tags
func matchesTags(objectTags, requiredTags map[string]string) bool {
	if len(requiredTags) == 0 {
		return true
	}

	for reqKey, reqValue := range requiredTags {
		objValue, exists := objectTags[reqKey]
		if !exists || objValue != reqValue {
			return false
		}
	}

	return true
}
