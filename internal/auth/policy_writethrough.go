package auth

import (
	"fmt"
	"strings"
)

// bucketPolicyName is the inline policy a bucket grant is stored as.
func bucketPolicyName(bucketName string) string { return "bucket-" + bucketName }

// writeBucketGrantPolicy records a bucket permission as a policy on its holder.
func (s *SQLiteStore) writeBucketGrantPolicy(targetType, targetID, bucketName, level string) error {
	document := bucketGrantDocument(bucketName, levelActions(level))
	if document == "" {
		return fmt.Errorf("%w: unknown permission level %q", ErrIAMInvalidInput, level)
	}
	return s.PutIAMInlinePolicy(targetType, targetID, bucketPolicyName(bucketName), document)
}

// removeBucketGrantPolicy drops the policy a bucket permission was stored as.
func (s *SQLiteStore) removeBucketGrantPolicy(targetType, targetID, bucketName string) error {
	err := s.DeleteIAMInlinePolicy(targetType, targetID, bucketPolicyName(bucketName))
	if err != nil && !isNoSuchEntity(err) {
		return err
	}
	return nil
}

// writeRoleCapabilityPolicies rewrites what a role grants.
//

func isNoSuchEntity(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such entity")
}
