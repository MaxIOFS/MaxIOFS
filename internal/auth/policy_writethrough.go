package auth

// Keeping the IAM tables the truth.
//
// The console still speaks in bucket permissions and capability overrides —
// those are the words operators use and the screens they have. What changed is
// where the answer comes from: granting a bucket or overriding a capability now
// writes the corresponding IAM policy, and the request path reads only that.
//
// Without this, only the permissions that existed at upgrade time would be
// expressed as policies and everything granted afterwards would be invisible to
// authorization.

import (
	"fmt"
	"strings"
)

// bucketPolicyName is the inline policy a bucket grant is stored as.
func bucketPolicyName(bucketName string) string { return "bucket-" + bucketName }

// writeBucketGrantPolicy records a bucket permission as a policy on its holder.
//
// The level decides what is granted. Unlike the pre-IAM model it is not
// narrowed by the holder's role: a grant grants, which is what a policy means
// everywhere else and what makes this one model rather than two that have to be
// combined at request time.
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
