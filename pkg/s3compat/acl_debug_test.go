package s3compat

import (
	"context"
	"testing"

	"github.com/maxiofs/maxiofs/internal/acl"
	"github.com/stretchr/testify/require"
)

// TestDebugACLManager verifies ACL manager initialization in test environment
func TestDebugACLManager(t *testing.T) {
	env := setupACLTestEnvironment(t)
	defer env.cleanup()

	ctx := context.Background()

	// Create a bucket
	err := env.bucketManager.CreateBucket(ctx, env.tenantID, "debug-bucket", env.userID)
	require.NoError(t, err, "Should create bucket")

	// Try to get ACL (should fail if no ACL manager)
	bucketACL, err := env.bucketManager.GetBucketACL(ctx, env.tenantID, "debug-bucket")

	t.Logf("GetBucketACL error: %v", err)
	t.Logf("GetBucketACL result: %v", bucketACL)

	// Get ACL Manager directly
	if bmWithACL, ok := env.bucketManager.(interface{ GetACLManager() interface{} }); ok {
		aclMgr := bmWithACL.GetACLManager()
		t.Logf("ACL Manager from bucket manager: %v (nil: %v)", aclMgr, aclMgr == nil)
	} else {
		t.Log("Bucket manager does not have GetACLManager method")
	}

	// Test the handler's getACLManager
	aclMgr := env.handler.getACLManager()
	t.Logf("ACL Manager from handler: %v (nil: %v)", aclMgr, aclMgr == nil)

	// Test checkBucketACLPermission with debug
	hasPermission := env.handler.checkBucketACLPermission(ctx, env.tenantID, "debug-bucket", env.userID, acl.PermissionRead)
	t.Logf("checkBucketACLPermission result: %v", hasPermission)
}
