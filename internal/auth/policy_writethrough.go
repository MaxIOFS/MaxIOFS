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

// denyPolicyName is the inline policy a revoked capability is stored as.
func denyPolicyName(capability string) string {
	return "deny-" + strings.ReplaceAll(capability, ":", "-")
}

// grantPolicyName is the inline policy an explicitly granted capability is
// stored as.
func grantPolicyName(capability string) string {
	return "grant-" + strings.ReplaceAll(capability, ":", "-")
}

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

// writeCapabilityOverridePolicy records an override as a policy: a grant as an
// Allow, a revocation as a Deny that outranks whatever else allows it.
func (s *SQLiteStore) writeCapabilityOverridePolicy(userID, capability string, granted bool) error {
	document := capabilityDocument(capability, granted)
	if document == "" {
		return nil
	}

	// An override replaces whichever direction was stored before, so the two
	// can never both be present and contradict each other.
	if err := s.clearCapabilityOverridePolicies(userID, capability); err != nil {
		return err
	}

	name := grantPolicyName(capability)
	if !granted {
		name = denyPolicyName(capability)
	}
	return s.PutIAMInlinePolicy(IAMTargetUser, userID, name, document)
}

// clearCapabilityOverridePolicies removes both directions of an override.
func (s *SQLiteStore) clearCapabilityOverridePolicies(userID, capability string) error {
	for _, name := range []string{grantPolicyName(capability), denyPolicyName(capability)} {
		if err := s.DeleteIAMInlinePolicy(IAMTargetUser, userID, name); err != nil && !isNoSuchEntity(err) {
			return err
		}
	}
	return nil
}

// writeRoleCapabilityPolicies rewrites what a role grants.
//
// Only the actions that name no resource are held on the role — signing in,
// managing one's own keys, administering IAM. Attaching the rest here would
// grant every bucket to everyone holding the role, which is precisely what a
// resource-scoped policy exists to prevent.
func (s *SQLiteStore) writeRoleCapabilityPolicies(role string, capabilities []string) error {
	if role == RoleAdmin {
		if err := s.ensureCataloguePolicy(PolicyFullAccess); err != nil {
			return err
		}
		return s.AttachIAMPolicy(PolicyFullAccess, IAMTargetRole, role)
	}

	existing, err := s.ListAttachedIAMPolicyNames(IAMTargetRole, role)
	if err != nil {
		return err
	}
	for _, name := range existing {
		if err := s.DetachIAMPolicy(name, IAMTargetRole, role); err != nil {
			return err
		}
	}

	for _, capability := range capabilities {
		for _, name := range PoliciesForCapability(capability) {
			entry, ok := CatalogEntry(name)
			if !ok {
				continue
			}
			if global, _ := splitRoleActions(entry.Actions); len(global) == 0 {
				continue
			}
			if err := s.ensureCataloguePolicy(name); err != nil {
				return err
			}
			if err := s.AttachIAMPolicy(name, IAMTargetRole, role); err != nil {
				return err
			}
		}
	}
	return nil
}

func isNoSuchEntity(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such entity")
}
