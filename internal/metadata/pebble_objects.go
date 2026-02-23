package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/sirupsen/logrus"
)

// ==================== Object Operations ====================

// PutObject stores or updates object metadata and its tag indices atomically.
func (s *PebbleStore) PutObject(ctx context.Context, obj *ObjectMetadata) error {
	if obj == nil {
		return fmt.Errorf("object metadata cannot be nil")
	}
	if obj.Bucket == "" || obj.Key == "" {
		return ErrInvalidKey
	}

	now := time.Now()
	if obj.CreatedAt.IsZero() {
		obj.CreatedAt = now
	}
	obj.UpdatedAt = now
	obj.LastModified = now

	data, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("failed to marshal object: %w", err)
	}

	batch := s.db.NewBatch()
	defer batch.Close() //nolint:errcheck

	key := objectKey(obj.Bucket, obj.Key)
	if err := batch.Set(key, data, nil); err != nil {
		return fmt.Errorf("failed to set object in batch: %w", err)
	}

	for tagKey, tagValue := range obj.Tags {
		idxKey := tagIndexKey(obj.Bucket, tagKey, tagValue, obj.Key)
		if err := batch.Set(idxKey, []byte{}, nil); err != nil {
			return fmt.Errorf("failed to set tag index in batch: %w", err)
		}
	}

	if err := batch.Commit(pebble.NoSync); err != nil {
		return fmt.Errorf("failed to commit object: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"bucket": obj.Bucket,
		"key":    obj.Key,
		"size":   obj.Size,
	}).Debug("Object metadata stored in Pebble")

	return nil
}

// GetObject retrieves object metadata; optionally retrieves a specific version.
func (s *PebbleStore) GetObject(ctx context.Context, bucket, key string, versionID ...string) (*ObjectMetadata, error) {
	if bucket == "" || key == "" {
		return nil, ErrInvalidKey
	}

	var objKey []byte
	if len(versionID) > 0 && versionID[0] != "" {
		objKey = objectVersionKey(bucket, key, versionID[0])
	} else {
		objKey = objectKey(bucket, key)
	}

	data, err := s.pebbleGet(objKey)
	if err == pebble.ErrNotFound {
		return nil, ErrObjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get object: %w", err)
	}

	var obj ObjectMetadata
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal object: %w", err)
	}

	// Ensure bucket/key are always populated (version entries may omit them)
	if obj.Bucket == "" {
		obj.Bucket = bucket
	}
	if obj.Key == "" {
		obj.Key = key
	}
	return &obj, nil
}

// DeleteObject removes object metadata and its tag indices atomically.
func (s *PebbleStore) DeleteObject(ctx context.Context, bucket, key string, versionID ...string) error {
	if bucket == "" || key == "" {
		return ErrInvalidKey
	}

	var objKey []byte
	if len(versionID) > 0 && versionID[0] != "" {
		objKey = objectVersionKey(bucket, key, versionID[0])
	} else {
		objKey = objectKey(bucket, key)
	}

	// Read current value to find tag indices to remove
	data, err := s.pebbleGet(objKey)
	if err == pebble.ErrNotFound {
		return ErrObjectNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to get object: %w", err)
	}

	var obj ObjectMetadata
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("failed to unmarshal object: %w", err)
	}

	batch := s.db.NewBatch()
	defer batch.Close() //nolint:errcheck

	for tagKey, tagValue := range obj.Tags {
		idxKey := tagIndexKey(bucket, tagKey, tagValue, key)
		if err := batch.Delete(idxKey, nil); err != nil {
			s.logger.WithError(err).Warn("Failed to delete tag index in batch")
		}
	}

	if err := batch.Delete(objKey, nil); err != nil {
		return fmt.Errorf("failed to delete object in batch: %w", err)
	}

	if err := batch.Commit(pebble.NoSync); err != nil {
		return fmt.Errorf("failed to commit delete: %w", err)
	}

	s.logger.WithFields(logrus.Fields{
		"bucket": bucket,
		"key":    key,
	}).Debug("Object metadata deleted from Pebble")

	return nil
}

// ListObjects lists objects in a bucket with optional prefix and marker-based pagination.
func (s *PebbleStore) ListObjects(ctx context.Context, bucket, prefix, marker string, maxKeys int) ([]*ObjectMetadata, string, error) {
	if bucket == "" {
		return nil, "", fmt.Errorf("bucket name is required")
	}
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	var lower []byte
	if prefix != "" {
		lower = objectPrefixKey(bucket, prefix)
	} else {
		lower = objectListPrefix(bucket)
	}

	iter, err := s.pebbleIter(lower)
	if err != nil {
		return nil, "", err
	}
	defer iter.Close() //nolint:errcheck

	var objects []*ObjectMetadata
	var nextMarker string
	count := 0
	started := marker == ""

	var valid bool
	if marker != "" {
		valid = iter.SeekGE(objectKey(bucket, marker))
	} else {
		valid = iter.First()
	}

	for ; valid; valid = iter.Next() {
		objKeyStr := extractObjectKeyFromKey(string(iter.Key()))

		if !started {
			if objKeyStr == marker {
				started = true
			}
			continue
		}

		if count >= maxKeys {
			nextMarker = objKeyStr
			break
		}

		val := iter.Value()
		valCopy := make([]byte, len(val))
		copy(valCopy, val)

		var obj ObjectMetadata
		if err := json.Unmarshal(valCopy, &obj); err != nil {
			s.logger.WithError(err).Warn("Failed to unmarshal object metadata")
			continue
		}
		objects = append(objects, &obj)
		count++
	}

	if err := iter.Error(); err != nil {
		return nil, "", fmt.Errorf("failed during object list: %w", err)
	}
	return objects, nextMarker, nil
}

// ObjectExists checks if an object exists in the store.
func (s *PebbleStore) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	if bucket == "" || key == "" {
		return false, ErrInvalidKey
	}

	objKey := objectKey(bucket, key)
	if _, closer, err := s.db.Get(objKey); err == pebble.ErrNotFound {
		return false, nil
	} else if err != nil {
		return false, err
	} else {
		_ = closer.Close()
		return true, nil
	}
}

// ==================== Object Versioning ====================

// PutObjectVersion stores a new object version, marking previous versions as not-latest.
func (s *PebbleStore) PutObjectVersion(ctx context.Context, obj *ObjectMetadata, version *ObjectVersion) error {
	if obj == nil || version == nil {
		return fmt.Errorf("object and version metadata cannot be nil")
	}

	batch := s.db.NewBatch()
	defer batch.Close() //nolint:errcheck

	if version.IsLatest {
		// Read existing versions and mark them as not-latest
		prefix := []byte(fmt.Sprintf("version:%s:%s:", obj.Bucket, obj.Key))
		iter, err := s.pebbleIter(prefix)
		if err != nil {
			return err
		}

		type versionUpdate struct {
			key  []byte
			data []byte
		}
		var updates []versionUpdate

		for iter.First(); iter.Valid(); iter.Next() {
			var existing ObjectVersion
			if err := json.Unmarshal(iter.Value(), &existing); err != nil {
				continue
			}
			if existing.IsLatest {
				existing.IsLatest = false
				updatedData, err := json.Marshal(&existing)
				if err != nil {
					continue
				}
				keyCopy := make([]byte, len(iter.Key()))
				copy(keyCopy, iter.Key())
				updates = append(updates, versionUpdate{key: keyCopy, data: updatedData})
			}
		}
		iterErr := iter.Error()
		_ = iter.Close()
		if iterErr != nil {
			return fmt.Errorf("failed iterating versions: %w", iterErr)
		}

		for _, u := range updates {
			if err := batch.Set(u.key, u.data, nil); err != nil {
				s.logger.WithError(err).Warn("Failed to update existing version in batch")
			}
		}
	}

	// Store the new version
	versionKey := objectVersionKey(obj.Bucket, obj.Key, version.VersionID)
	versionData, err := json.Marshal(version)
	if err != nil {
		return fmt.Errorf("failed to marshal version: %w", err)
	}
	if err := batch.Set(versionKey, versionData, nil); err != nil {
		return fmt.Errorf("failed to set version in batch: %w", err)
	}

	// Update main object entry if this is the latest version
	if version.IsLatest {
		objKey := objectKey(obj.Bucket, obj.Key)
		objData, err := json.Marshal(obj)
		if err != nil {
			return fmt.Errorf("failed to marshal object: %w", err)
		}
		if err := batch.Set(objKey, objData, nil); err != nil {
			return fmt.Errorf("failed to set object in batch: %w", err)
		}
	}

	return batch.Commit(pebble.NoSync)
}

// GetObjectVersions retrieves all versions of an object sorted newest-first.
func (s *PebbleStore) GetObjectVersions(ctx context.Context, bucket, key string) ([]*ObjectVersion, error) {
	prefix := []byte(fmt.Sprintf("version:%s:%s:", bucket, key))
	iter, err := s.pebbleIter(prefix)
	if err != nil {
		return nil, err
	}
	defer iter.Close() //nolint:errcheck

	var versions []*ObjectVersion
	for iter.First(); iter.Valid(); iter.Next() {
		val := iter.Value()
		valCopy := make([]byte, len(val))
		copy(valCopy, val)

		var version ObjectVersion
		if err := json.Unmarshal(valCopy, &version); err != nil {
			s.logger.WithError(err).Warn("Failed to unmarshal version metadata")
			continue
		}
		versions = append(versions, &version)
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("failed during version list: %w", err)
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].LastModified.After(versions[j].LastModified)
	})
	return versions, nil
}

// ListAllObjectVersions lists all versions of all objects in a bucket.
func (s *PebbleStore) ListAllObjectVersions(ctx context.Context, bucket, prefix string, maxKeys int) ([]*ObjectVersion, error) {
	var allVersions []*ObjectVersion
	keysWithVersions := make(map[string]bool)

	// First pass: collect versioned entries
	versionPrefix := []byte(fmt.Sprintf("version:%s:", bucket))
	vIter, err := s.pebbleIter(versionPrefix)
	if err != nil {
		return nil, err
	}
	for vIter.First(); vIter.Valid(); vIter.Next() {
		val := vIter.Value()
		valCopy := make([]byte, len(val))
		copy(valCopy, val)

		var version ObjectVersion
		if err := json.Unmarshal(valCopy, &version); err != nil {
			s.logger.WithError(err).Warn("Failed to unmarshal version")
			continue
		}
		if prefix != "" && !strings.HasPrefix(version.Key, prefix) {
			continue
		}
		allVersions = append(allVersions, &version)
		keysWithVersions[version.Key] = true
		if maxKeys > 0 && len(allVersions) >= maxKeys {
			break
		}
	}
	vIterErr := vIter.Error()
	_ = vIter.Close()
	if vIterErr != nil {
		return nil, fmt.Errorf("failed iterating versions: %w", vIterErr)
	}

	// Second pass: non-versioned objects (no version entry, just an obj: entry)
	if maxKeys <= 0 || len(allVersions) < maxKeys {
		objectPrefix := []byte(fmt.Sprintf("obj:%s:", bucket))
		oIter, err := s.pebbleIter(objectPrefix)
		if err != nil {
			return nil, err
		}
		for oIter.First(); oIter.Valid(); oIter.Next() {
			val := oIter.Value()
			valCopy := make([]byte, len(val))
			copy(valCopy, val)

			var obj ObjectMetadata
			if err := json.Unmarshal(valCopy, &obj); err != nil {
				s.logger.WithError(err).Warn("Failed to unmarshal object")
				continue
			}
			if keysWithVersions[obj.Key] {
				continue
			}
			if prefix != "" && !strings.HasPrefix(obj.Key, prefix) {
				continue
			}
			allVersions = append(allVersions, &ObjectVersion{
				Key:          obj.Key,
				VersionID:    "",
				IsLatest:     true,
				LastModified: obj.LastModified,
				ETag:         obj.ETag,
				Size:         obj.Size,
			})
			if maxKeys > 0 && len(allVersions) >= maxKeys {
				break
			}
		}
		oIterErr := oIter.Error()
		_ = oIter.Close()
		if oIterErr != nil {
			return nil, fmt.Errorf("failed iterating objects: %w", oIterErr)
		}
	}

	sort.Slice(allVersions, func(i, j int) bool {
		if allVersions[i].Key != allVersions[j].Key {
			return allVersions[i].Key < allVersions[j].Key
		}
		return allVersions[i].LastModified.After(allVersions[j].LastModified)
	})
	return allVersions, nil
}

// DeleteObjectVersion removes a specific version of an object.
func (s *PebbleStore) DeleteObjectVersion(ctx context.Context, bucket, key, versionID string) error {
	versionKey := objectVersionKey(bucket, key, versionID)

	if _, closer, err := s.db.Get(versionKey); err == pebble.ErrNotFound {
		return ErrVersionNotFound
	} else if err != nil {
		return fmt.Errorf("failed to get version: %w", err)
	} else {
		_ = closer.Close()
	}

	return s.db.Delete(versionKey, pebble.NoSync)
}

// ==================== Tags ====================

// PutObjectTags replaces all tags on an object, updating the tag index atomically.
func (s *PebbleStore) PutObjectTags(ctx context.Context, bucket, key string, tags map[string]string) error {
	objKey := objectKey(bucket, key)
	data, err := s.pebbleGet(objKey)
	if err == pebble.ErrNotFound {
		return ErrObjectNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to get object: %w", err)
	}

	var obj ObjectMetadata
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("failed to unmarshal object: %w", err)
	}

	batch := s.db.NewBatch()
	defer batch.Close() //nolint:errcheck

	// Remove old tag indices
	for tagKey, tagValue := range obj.Tags {
		idxKey := tagIndexKey(bucket, tagKey, tagValue, key)
		if err := batch.Delete(idxKey, nil); err != nil {
			return fmt.Errorf("failed to delete tag index %s=%s: %w", tagKey, tagValue, err)
		}
	}

	obj.Tags = tags
	obj.UpdatedAt = time.Now()

	// Create new tag indices
	for tagKey, tagValue := range tags {
		idxKey := tagIndexKey(bucket, tagKey, tagValue, key)
		if err := batch.Set(idxKey, []byte{}, nil); err != nil {
			return fmt.Errorf("failed to set tag index: %w", err)
		}
	}

	newData, err := json.Marshal(&obj)
	if err != nil {
		return fmt.Errorf("failed to marshal object: %w", err)
	}
	if err := batch.Set(objKey, newData, nil); err != nil {
		return fmt.Errorf("failed to set object in batch: %w", err)
	}

	return batch.Commit(pebble.NoSync)
}

// GetObjectTags retrieves the tags for an object.
func (s *PebbleStore) GetObjectTags(ctx context.Context, bucket, key string) (map[string]string, error) {
	obj, err := s.GetObject(ctx, bucket, key)
	if err != nil {
		return nil, err
	}
	if obj.Tags == nil {
		return make(map[string]string), nil
	}
	return obj.Tags, nil
}

// DeleteObjectTags removes all tags from an object.
func (s *PebbleStore) DeleteObjectTags(ctx context.Context, bucket, key string) error {
	return s.PutObjectTags(ctx, bucket, key, nil)
}

// ListObjectsByTags returns objects that have all the specified tags.
func (s *PebbleStore) ListObjectsByTags(ctx context.Context, bucket string, tags map[string]string) ([]*ObjectMetadata, error) {
	if len(tags) == 0 {
		return nil, fmt.Errorf("at least one tag is required")
	}

	// Use first tag to get candidates from tag index, then filter
	var firstTagKey, firstTagValue string
	for k, v := range tags {
		firstTagKey = k
		firstTagValue = v
		break
	}

	idxPrefix := tagIndexPrefix(bucket, firstTagKey, firstTagValue)
	iter, err := s.pebbleIter(idxPrefix)
	if err != nil {
		return nil, err
	}

	var candidateKeys []string
	for iter.First(); iter.Valid(); iter.Next() {
		k := string(iter.Key())
		// tag_idx:{bucket}:{tagKey}:{tagValue}:{objectKey}
		parts := strings.SplitN(k, ":", 5)
		if len(parts) == 5 {
			candidateKeys = append(candidateKeys, parts[4])
		}
	}
	iterErr := iter.Error()
	_ = iter.Close()
	if iterErr != nil {
		return nil, fmt.Errorf("failed iterating tag index: %w", iterErr)
	}

	var objects []*ObjectMetadata
	for _, objKey := range candidateKeys {
		obj, err := s.GetObject(ctx, bucket, objKey)
		if err != nil {
			continue
		}
		if matchesTags(obj.Tags, tags) {
			objects = append(objects, obj)
		}
	}
	return objects, nil
}

// ==================== Search ====================

// SearchObjects searches objects with filters, using the tag index when tags are specified.
func (s *PebbleStore) SearchObjects(ctx context.Context, bucket, prefix, marker string, maxKeys int, filter *ObjectFilter) ([]*ObjectMetadata, string, error) {
	if bucket == "" {
		return nil, "", fmt.Errorf("bucket name is required")
	}
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	if filter != nil && len(filter.Tags) > 0 {
		return s.searchObjectsWithTags(ctx, bucket, prefix, marker, maxKeys, filter)
	}
	return s.searchObjectsByScan(ctx, bucket, prefix, marker, maxKeys, filter)
}

func (s *PebbleStore) searchObjectsByScan(ctx context.Context, bucket, prefix, marker string, maxKeys int, filter *ObjectFilter) ([]*ObjectMetadata, string, error) {
	var lower []byte
	if prefix != "" {
		lower = objectPrefixKey(bucket, prefix)
	} else {
		lower = objectListPrefix(bucket)
	}

	iter, err := s.pebbleIter(lower)
	if err != nil {
		return nil, "", err
	}
	defer iter.Close() //nolint:errcheck

	var objects []*ObjectMetadata
	var nextMarker string
	count := 0
	scanned := 0
	scanLimit := 100000
	started := marker == ""

	var valid bool
	if marker != "" {
		valid = iter.SeekGE(objectKey(bucket, marker))
	} else {
		valid = iter.First()
	}

	for ; valid; valid = iter.Next() {
		if scanned >= scanLimit {
			nextMarker = extractObjectKeyFromKey(string(iter.Key()))
			break
		}
		scanned++

		objKeyStr := extractObjectKeyFromKey(string(iter.Key()))

		if !started {
			if objKeyStr == marker {
				started = true
			}
			continue
		}

		if count >= maxKeys {
			nextMarker = objKeyStr
			break
		}

		val := iter.Value()
		valCopy := make([]byte, len(val))
		copy(valCopy, val)

		var obj ObjectMetadata
		if err := json.Unmarshal(valCopy, &obj); err != nil {
			s.logger.WithError(err).Warn("Failed to unmarshal object during search")
			continue
		}
		if matchesFilter(&obj, filter) {
			objects = append(objects, &obj)
			count++
		}
	}

	if err := iter.Error(); err != nil {
		return nil, "", fmt.Errorf("failed during object search: %w", err)
	}
	return objects, nextMarker, nil
}

func (s *PebbleStore) searchObjectsWithTags(ctx context.Context, bucket, prefix, marker string, maxKeys int, filter *ObjectFilter) ([]*ObjectMetadata, string, error) {
	var firstTagKey, firstTagValue string
	for k, v := range filter.Tags {
		firstTagKey = k
		firstTagValue = v
		break
	}

	idxPrefix := tagIndexPrefix(bucket, firstTagKey, firstTagValue)
	iter, err := s.pebbleIter(idxPrefix)
	if err != nil {
		return nil, "", err
	}

	var candidateKeys []string
	for iter.First(); iter.Valid(); iter.Next() {
		k := string(iter.Key())
		parts := strings.SplitN(k, ":", 5)
		if len(parts) == 5 {
			objKey := parts[4]
			if prefix != "" && !strings.HasPrefix(objKey, prefix) {
				continue
			}
			if marker != "" && objKey <= marker {
				continue
			}
			candidateKeys = append(candidateKeys, objKey)
		}
	}
	iterErr := iter.Error()
	_ = iter.Close()
	if iterErr != nil {
		return nil, "", fmt.Errorf("failed iterating tag index: %w", iterErr)
	}

	sort.Strings(candidateKeys)

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
