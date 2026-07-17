package bucket

import (
	"context"

	"github.com/maxiofs/maxiofs/internal/auth"
)

// FilterBucketsByPermissions filters buckets based on user permissions
func FilterBucketsByPermissions(ctx context.Context, buckets []*Bucket, userID string, userRoles []string, permManager auth.PermissionManager) ([]*Bucket, error) {
	// Admin users see all buckets
	if containsRole(userRoles, "admin") {
		return buckets, nil
	}

	var filtered []*Bucket
	for _, bucket := range buckets {
		// Check if bucket is public
		if bucket.IsPublic {
			filtered = append(filtered, bucket)
			continue
		}

		// Check if user owns the bucket
		if bucket.OwnerID == userID && bucket.OwnerType == "user" {
			filtered = append(filtered, bucket)
			continue
		}

		// Check if user has explicit permission
		hasAccess, _, err := permManager.CheckBucketAccess(ctx, bucket.Name, userID)
		if err == nil && hasAccess {
			filtered = append(filtered, bucket)
		}
	}

	return filtered, nil
}

// containsRole checks if a role is in the user's roles
func containsRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}
