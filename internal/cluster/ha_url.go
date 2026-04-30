package cluster

import (
	"net/url"
	"strings"
)

// escapeHAObjectKey escapes path-control characters while preserving "/" so
// HA routes keep accepting object keys with slash-separated prefixes.
func escapeHAObjectKey(key string) string {
	parts := strings.Split(key, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
