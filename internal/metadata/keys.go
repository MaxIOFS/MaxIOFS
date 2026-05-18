package metadata

import (
	"errors"
	"fmt"
	"strings"
)

// ==================== Key Naming Scheme ====================
// This defines how metadata keys are structured for efficient Pebble lookups.

func bucketKey(tenantID, name string) []byte {
	return []byte(fmt.Sprintf("bucket:%s:%s", tenantID, name))
}

func bucketListPrefix(tenantID string) []byte {
	return []byte(fmt.Sprintf("bucket:%s:", tenantID))
}

func objectKey(bucket, key string) []byte {
	return []byte(fmt.Sprintf("obj:%s:%s", bucket, key))
}

func objectVersionKey(bucket, key, versionID string) []byte {
	return []byte(fmt.Sprintf("version:%s:%s:%s", bucket, key, versionID))
}

func objectListPrefix(bucket string) []byte {
	return []byte(fmt.Sprintf("obj:%s:", bucket))
}

func objectPrefixKey(bucket, prefix string) []byte {
	return []byte(fmt.Sprintf("obj:%s:%s", bucket, prefix))
}

func multipartUploadKey(uploadID string) []byte {
	return []byte(fmt.Sprintf("multipart:%s", uploadID))
}

func multipartListPrefix(bucket string) []byte {
	return []byte(fmt.Sprintf("multipart_idx:%s:", bucket))
}

func multipartIndexKey(bucket, uploadID string) []byte {
	return []byte(fmt.Sprintf("multipart_idx:%s:%s", bucket, uploadID))
}

func partKey(uploadID string, partNumber int) []byte {
	return []byte(fmt.Sprintf("part:%s:%05d", uploadID, partNumber))
}

func partListPrefix(uploadID string) []byte {
	return []byte(fmt.Sprintf("part:%s:", uploadID))
}

func tagIndexKey(bucket, tagKey, tagValue, objectKey string) []byte {
	return []byte(fmt.Sprintf("tag_idx:%s:%s:%s:%s", bucket, tagKey, tagValue, objectKey))
}

func tagIndexPrefix(bucket, tagKey, tagValue string) []byte {
	return []byte(fmt.Sprintf("tag_idx:%s:%s:%s:", bucket, tagKey, tagValue))
}

// extractObjectKeyFromKey extracts the object name from a metadata key.
func extractObjectKeyFromKey(key string) string {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

var ErrNotFound = errors.New("key not found")

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

// hasPrefix checks if a string has a given prefix (case-sensitive)
func hasPrefix(s, prefix string) bool {
	if prefix == "" {
		return true
	}
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
