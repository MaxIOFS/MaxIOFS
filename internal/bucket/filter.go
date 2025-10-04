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

// CheckUserBucketPermission checks if a user has a specific permission level on a bucket
func CheckUserBucketPermission(ctx context.Context, bucket *Bucket, userID string, userRoles []string, requiredLevel string, permManager auth.PermissionManager) (bool, error) {
	// Admin users have all permissions
	if containsRole(userRoles, "admin") {
		return true, nil
	}

	// Owner has all permissions
	if bucket.OwnerID == userID && bucket.OwnerType == "user" {
		return true, nil
	}

	// Public buckets allow read access
	if bucket.IsPublic && requiredLevel == auth.PermissionLevelRead {
		return true, nil
	}

	// Check explicit permissions
	hasAccess, permLevel, err := permManager.CheckBucketAccess(ctx, bucket.Name, userID)
	if err != nil {
		return false, err
	}

	if !hasAccess {
		return false, nil
	}

	// Check permission level hierarchy
	return hasRequiredPermissionLevel(permLevel, requiredLevel), nil
}

// hasRequiredPermissionLevel checks if a permission level satisfies the requirement
func hasRequiredPermissionLevel(currentLevel, requiredLevel string) bool {
	// Permission hierarchy: admin > write > read
	levels := map[string]int{
		auth.PermissionLevelRead:  1,
		auth.PermissionLevelWrite: 2,
		auth.PermissionLevelAdmin: 3,
	}

	current, okCurrent := levels[currentLevel]
	required, okRequired := levels[requiredLevel]

	if !okCurrent || !okRequired {
		return false
	}

	return current >= required
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
