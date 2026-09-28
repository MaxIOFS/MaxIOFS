package object

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/maxiofs/maxiofs/internal/metadata"
)

// AdoptKeyACLs moves the object ACLs once kept by key into the current object
// of each key that has no ACL of its own, and removes them. A key-wide ACL
// outlived the object it was set on and applied to the next object at the key;
// each object now keeps its own. Objects keep the ACL they answered with
// before. It returns how many ACLs an object adopted.
func AdoptKeyACLs(ctx context.Context, store metadata.Store) (int, error) {
	raw, ok := store.(metadata.RawKVStore)
	if !ok {
		return 0, nil
	}
	type entry struct {
		key  string
		data []byte
	}
	var entries []entry
	err := raw.RawScan(ctx, acl.ObjectACLPrefix, "", func(key string, value []byte) bool {
		entries = append(entries, entry{key, append([]byte(nil), value...)})
		return true
	})
	if err != nil {
		return 0, err
	}

	adopted := 0
	for _, e := range entries {
		bucket, key, found := resolveKeyACL(ctx, store, strings.TrimPrefix(e.key, acl.ObjectACLPrefix))
		if found {
			current, err := store.GetObject(ctx, bucket, key)
			switch {
			case errors.Is(err, metadata.ErrObjectNotFound):
			case err != nil:
				return adopted, err
			case current.ACL == nil && !isMetadataDeleteMarker(current):
				var keyACL acl.ACL
				if err := json.Unmarshal(e.data, &keyACL); err != nil {
					return adopted, err
				}
				current.ACL = toMetadataACL(fromACLManagerType(&keyACL))
				if err := store.PutObject(ctx, current); err != nil {
					return adopted, err
				}
				adopted++
			}
		}
		if err := raw.DeleteRaw(ctx, e.key); err != nil && !errors.Is(err, metadata.ErrNotFound) {
			return adopted, err
		}
	}
	return adopted, nil
}

// resolveKeyACL finds the bucket path and key a key-wide ACL was stored under:
// "tenant:bucket:key" for a tenant's bucket, "bucket:key" otherwise. Bucket
// names hold no ':'; keys may.
func resolveKeyACL(ctx context.Context, store metadata.Store, rest string) (string, string, bool) {
	first, afterFirst, ok := strings.Cut(rest, ":")
	if !ok {
		return "", "", false
	}
	if name, key, ok := strings.Cut(afterFirst, ":"); ok {
		if b, err := store.GetBucketByName(ctx, name); err == nil && b.TenantID == first && first != "" {
			return first + "/" + name, key, true
		}
	}
	if b, err := store.GetBucketByName(ctx, first); err == nil && b.TenantID == "" {
		return first, afterFirst, true
	}
	return "", "", false
}
