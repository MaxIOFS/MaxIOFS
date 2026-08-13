package auth

import "fmt"

// DenyPermission attaches a Deny for every action the capability names, which
// outranks whatever the user's roles and grants allow.
func (s *SQLiteStore) DenyPermission(userID, capability string) error {
	document := capabilityDocument(capability, false)
	if document == "" {
		return fmt.Errorf("%w: unknown capability %q", ErrIAMInvalidInput, capability)
	}
	return s.PutIAMInlinePolicy(IAMTargetUser, userID, "deny-"+capability, document)
}

// AllowPermission removes a Deny previously written by DenyPermission.
func (s *SQLiteStore) AllowPermission(userID, capability string) error {
	err := s.DeleteIAMInlinePolicy(IAMTargetUser, userID, "deny-"+capability)
	if err != nil && !isNoSuchEntity(err) {
		return err
	}
	return nil
}

// DenyPermission and AllowPermission on the manager, for callers that hold one.
func (am *authManager) DenyPermission(userID, capability string) error {
	return am.store.DenyPermission(userID, capability)
}

func (am *authManager) AllowPermission(userID, capability string) error {
	return am.store.AllowPermission(userID, capability)
}
