package auth

// IAM roles and real AssumeRole. See docs/SECURITY.md, "IAM".
//
// Assuming a role does NOT project the caller's own permissions the way
// GetSessionToken does. The session acts as the role: the role's policies grant
// what it may do, an optional session policy narrows that further, and the
// caller's own access is not consulted. That is AWS behaviour, and it is what
// makes a role useful — an application gets exactly the role's permissions,
// no more and no less, regardless of who started it.
//
// The control that makes this safe is the trust policy. It is required at
// creation, it names who may assume the role, and creating or editing a role
// needs the same authority as any other IAM change. Without a matching trust
// statement, AssumeRole is refused.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/audit"
)

// ErrIAMTrustDenied is returned when a caller is not permitted to assume a role.
var ErrIAMTrustDenied = fmt.Errorf("%w: not authorized to assume this role", ErrAccessDenied)

// ParseTrustPolicy validates a role's trust policy. Unlike an identity policy,
// a trust policy REQUIRES a Principal — that is the whole point of the document
// — and its Action is the STS verb being permitted, not an S3 action.
func ParseTrustPolicy(raw string) (*Policy, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: trust policy is required", ErrIAMInvalidInput)
	}
	if len(trimmed) > IAMMaxManagedPolicyBytes {
		return nil, fmt.Errorf("%w: trust policy exceeds %d bytes", ErrIAMInvalidInput, IAMMaxManagedPolicyBytes)
	}

	var policy Policy
	if err := json.Unmarshal([]byte(trimmed), &policy); err != nil {
		return nil, fmt.Errorf("%w: trust policy is not valid JSON: %v", ErrIAMInvalidInput, err)
	}
	if len(policy.Statement) == 0 {
		return nil, fmt.Errorf("%w: trust policy has no statements", ErrIAMInvalidInput)
	}

	for i, st := range policy.Statement {
		if st.Effect != EffectAllow && st.Effect != EffectDeny {
			return nil, fmt.Errorf("%w: trust statement %d has Effect %q", ErrIAMInvalidInput, i, st.Effect)
		}
		if len(st.Principal) == 0 {
			return nil, fmt.Errorf("%w: trust statement %d has no Principal — a trust policy must say who may assume the role", ErrIAMInvalidInput, i)
		}
		if len(st.Condition) > 0 {
			return nil, fmt.Errorf("%w: trust statement %d uses Condition, which is not supported yet", ErrIAMInvalidInput, i)
		}
	}
	return &policy, nil
}

// EvaluateTrustPolicy reports whether any of the caller's principal identifiers
// is permitted to assume the role. Deny wins, and an explicit Allow is required.
func EvaluateTrustPolicy(policy *Policy, principals []string) bool {
	if policy == nil || len(principals) == 0 {
		return false
	}

	allowed := false
	for _, st := range policy.Statement {
		if !trustPrincipalMatches(st.Principal, principals) {
			continue
		}
		// A trust statement that names an Action must name an STS one; a
		// statement without an Action covers every way of assuming the role.
		if values := policyStringValues(st.Action); len(values) > 0 && !trustActionMatches(values) {
			continue
		}
		if st.Effect == EffectDeny {
			return false
		}
		allowed = true
	}
	return allowed
}

// trustPrincipalMatches compares a statement's Principal block against the
// caller's identifiers. AWS writes principals as {"AWS": "arn"} or
// {"AWS": ["arn", …]}; the key is accepted in any case and any of its values
// may match.
func trustPrincipalMatches(principal map[string]interface{}, callerPrincipals []string) bool {
	for _, raw := range principal {
		for _, pattern := range policyStringValues(raw) {
			for _, caller := range callerPrincipals {
				if stsWildcardMatch(strings.ToLower(pattern), strings.ToLower(caller)) {
					return true
				}
			}
		}
	}
	return false
}

// trustActionMatches reports whether a trust statement's actions include one of
// the STS verbs that assume a role.
func trustActionMatches(actions []string) bool {
	for _, a := range actions {
		lowered := strings.ToLower(a)
		if lowered == "*" || lowered == "sts:*" || strings.HasPrefix(lowered, "sts:assumerole") {
			return true
		}
	}
	return false
}

// callerPrincipals lists the identifiers a user can be matched by in a trust
// policy: their user ARN, their bare username and ID, their tenant, and the
// catch-all "*" so a role open to everyone can be written the AWS way.
func callerPrincipals(user *User) []string {
	principals := []string{
		"*",
		IAMUserARN(user.Username),
		user.Username,
		user.ID,
	}
	if user.TenantID != "" {
		principals = append(principals, "arn:aws:iam:::tenant/"+user.TenantID)
	}
	for _, role := range user.Roles {
		principals = append(principals, "arn:aws:iam:::maxiofs-role/"+role)
	}
	return principals
}

// AssumeIAMRole issues a temporary credential that acts as a role.
//
// The RoleArn is resolved against the role table: an unknown one is an error,
// not something to shrug off. That is the difference between a role request
// being honoured and being quietly answered with something else.
func (am *authManager) AssumeIAMRole(ctx context.Context, user *User, roleARN, roleSessionName string, durationSeconds int, sessionPolicy string) (*STSSession, error) {
	if user == nil {
		return nil, ErrAccessDenied
	}
	if user.Status != UserStatusActive {
		return nil, ErrUserInactive
	}
	if err := validateRoleSessionName(roleSessionName); err != nil {
		return nil, err
	}

	role, err := am.resolveRole(roleARN)
	if err != nil {
		return nil, err
	}

	// A tenant-scoped role is assumable only from inside that tenant. Crossing
	// the boundary here would hand one tenant a credential for another's data,
	// which no trust policy is meant to be able to express.
	if role.TenantID != "" && role.TenantID != user.TenantID {
		return nil, ErrIAMTrustDenied
	}

	// A role people are assigned carries no trust policy; there is nothing that
	// says who may assume it, so nobody may.
	if strings.TrimSpace(role.AssumeRolePolicy) == "" {
		return nil, fmt.Errorf("%w: role %q is assigned to users, not assumed", ErrIAMTrustDenied, role.Name)
	}

	trust, err := ParseTrustPolicy(role.AssumeRolePolicy)
	if err != nil {
		// A stored trust policy that no longer parses cannot be evaluated, and
		// assuming the role anyway would ignore the only control protecting it.
		return nil, ErrIAMTrustDenied
	}
	if !EvaluateTrustPolicy(trust, callerPrincipals(user)) {
		return nil, ErrIAMTrustDenied
	}

	if durationSeconds == 0 {
		durationSeconds = STSDefaultSessionDuration
	}
	maxDuration := role.MaxSessionDuration
	if maxDuration <= 0 {
		maxDuration = IAMDefaultMaxSessionDuration
	}
	if serverMax := am.stsMaxSessionDuration(); maxDuration > serverMax {
		maxDuration = serverMax
	}
	if durationSeconds < STSMinSessionDuration || durationSeconds > maxDuration {
		return nil, ErrSTSInvalidDuration
	}

	sessionPolicy = strings.TrimSpace(sessionPolicy)
	if _, err := ParseSessionPolicy(sessionPolicy); err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	active, err := am.store.CountActiveSTSSessionsByUser(user.ID, now)
	if err != nil {
		return nil, fmt.Errorf("failed to count sts sessions: %w", err)
	}
	if active >= STSMaxSessionsPerUser {
		return nil, ErrSTSTooManySessions
	}

	tempKeyID, err := am.generateSTSAccessKeyID()
	if err != nil {
		return nil, err
	}
	secret, err := am.generateSecretAccessKey()
	if err != nil {
		return nil, err
	}
	token, err := generateSessionToken()
	if err != nil {
		return nil, err
	}
	encryptedSecret, err := am.encryptSecret(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt sts session secret: %w", err)
	}

	stored := &STSSession{
		TempAccessKeyID: tempKeyID,
		SecretAccessKey: encryptedSecret,
		SessionToken:    token,
		UserID:          user.ID,
		SessionPolicy:   sessionPolicy,
		RoleARN:         role.ARN,
		RoleSessionName: roleSessionName,
		PolicyMode:      STSPolicyModeRole,
		CreatedAt:       now,
		ExpiresAt:       now + int64(durationSeconds),
	}
	if err := am.store.CreateSTSSession(stored); err != nil {
		return nil, err
	}

	am.logAuditEvent(ctx, &audit.AuditEvent{
		TenantID:     user.TenantID,
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    audit.EventTypeSTSSessionIssued,
		ResourceType: audit.ResourceTypeSTSSession,
		ResourceID:   tempKeyID,
		ResourceName: tempKeyID,
		Action:       audit.ActionCreate,
		Status:       audit.StatusSuccess,
		Details: map[string]interface{}{
			"role_arn":           role.ARN,
			"role_session_name":  roleSessionName,
			"duration_seconds":   durationSeconds,
			"expires_at":         stored.ExpiresAt,
			"has_session_policy": sessionPolicy != "",
		},
	})

	return &STSSession{
		TempAccessKeyID: tempKeyID,
		SecretAccessKey: secret,
		SessionToken:    token,
		UserID:          user.ID,
		RoleARN:         role.ARN,
		RoleSessionName: roleSessionName,
		PolicyMode:      STSPolicyModeRole,
		CreatedAt:       now,
		ExpiresAt:       stored.ExpiresAt,
	}, nil
}

// resolveRole finds the role an AssumeRole call named. The ARN is matched
// exactly first; falling back to the name lets a caller that built the ARN with
// a different account field still reach the role it meant, which is the common
// case when a config was copied from an AWS example.
func (am *authManager) resolveRole(roleARN string) (*IAMRole, error) {
	roleARN = strings.TrimSpace(roleARN)
	if roleARN == "" {
		return nil, fmt.Errorf("%w: RoleArn is required", ErrIAMInvalidInput)
	}

	if role, err := am.store.GetIAMRoleByARN(roleARN); err == nil {
		return role, nil
	}

	resourceType, name, err := ParseIAMARN(roleARN)
	if err != nil {
		return nil, fmt.Errorf("%w: role %q", ErrIAMNoSuchEntity, roleARN)
	}
	if resourceType != "role" {
		return nil, fmt.Errorf("%w: %q is not a role ARN", ErrIAMInvalidInput, roleARN)
	}
	return am.store.GetIAMRole(name)
}

// validateRoleSessionName checks the label that identifies the session in audit
// records. AWS requires it and constrains it to the same character set as an
// entity name.
func validateRoleSessionName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: RoleSessionName is required", ErrIAMInvalidInput)
	}
	if len(name) > 64 {
		return fmt.Errorf("%w: RoleSessionName must be at most 64 characters", ErrIAMInvalidInput)
	}
	return ValidateIAMName(name)
}

// authorizeRoleSession applies a role session's permissions to a request: the
// role's own policies decide, and the caller's personal access is irrelevant.
//
// The role's documents are read live rather than frozen into the session at
// AssumeRole time, so editing or deleting a role takes effect on sessions
// already issued from it. A role whose permissions were just revoked would
// otherwise keep working until every outstanding session expired.
func (am *authManager) authorizeRoleSession(sess *STSSession, r *http.Request) error {
	_, roleName, err := ParseIAMARN(sess.RoleARN)
	if err != nil {
		return ErrAccessDenied
	}

	documents, err := am.store.IAMEffectiveDocuments(IAMTargetRole, roleName)
	if err != nil {
		return ErrAccessDenied
	}
	if len(documents) == 0 {
		// The role carries no policies — either it never had any or it was
		// deleted. Both mean this session may do nothing.
		return ErrAccessDenied
	}

	if !EvaluateIAMDocuments(documents, S3ActionForRequest(r), ResourceARNForRequest(r)) {
		return ErrAccessDenied
	}
	return nil
}

// enforceSTSSession applies everything that governs a temporary credential:
// the role's permissions when it is a role session, and the session policy on
// top in either case.
func (am *authManager) enforceSTSSession(sess *STSSession, r *http.Request) error {
	if sess != nil && sess.PolicyMode == STSPolicyModeRole {
		if err := am.authorizeRoleSession(sess, r); err != nil {
			return err
		}
	}
	return enforceSessionPolicy(sess, r)
}

// roleSessionUser returns the identity to put in the request context for a role
// session. The user is unchanged: a role session's permissions are the role's
// policies, and they reach the request through the same policy set every other
// permission does — resolved in authorizeRoleSession, above.
func roleSessionUser(user *User, sess *STSSession) *User {
	return user
}
