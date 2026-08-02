package auth

// Manager-level IAM operations. See docs/SECURITY.md, "IAM".
//
// These sit behind their own interface rather than being bolted onto Manager:
// the IAM surface is optional, only the query-protocol handler needs it, and
// every mock of Manager in the codebase would otherwise have to grow two dozen
// methods it never calls.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/audit"
)

// IAMManager is the set of operations the AWS IAM protocol handler needs.
// Obtain it with a type assertion on a Manager.
type IAMManager interface {
	// Identities
	CreateIAMUser(ctx context.Context, username, path, tenantID string) (*User, error)
	DeleteIAMUser(ctx context.Context, username string) error
	GetIAMUser(ctx context.Context, username string) (*User, error)
	ListIAMUsers(ctx context.Context) ([]*User, error)

	// Managed policies
	CreateIAMPolicy(ctx context.Context, name, path, description, document string) (*IAMPolicy, error)
	GetIAMPolicy(ctx context.Context, name string) (*IAMPolicy, error)
	ListIAMPolicies(ctx context.Context) ([]*IAMPolicy, error)
	DeleteIAMPolicy(ctx context.Context, name string) error

	// Policy versions
	CreateIAMPolicyVersion(ctx context.Context, policyName, document string, setDefault bool) (*IAMPolicyVersion, error)
	GetIAMPolicyVersion(ctx context.Context, policyName, versionID string) (*IAMPolicyVersion, error)
	ListIAMPolicyVersions(ctx context.Context, policyName string) ([]*IAMPolicyVersion, error)
	DeleteIAMPolicyVersion(ctx context.Context, policyName, versionID string) error
	SetDefaultIAMPolicyVersion(ctx context.Context, policyName, versionID string) error

	// Attachments
	CountIAMPolicyAttachments(ctx context.Context, policyName string) int
	AttachIAMPolicy(ctx context.Context, policyName, targetType, targetID string) error
	DetachIAMPolicy(ctx context.Context, policyName, targetType, targetID string) error
	ListAttachedIAMPolicies(ctx context.Context, targetType, targetID string) ([]*IAMPolicy, error)

	// Inline policies
	PutIAMInlinePolicy(ctx context.Context, targetType, targetID, name, document string) error
	GetIAMInlinePolicy(ctx context.Context, targetType, targetID, name string) (*IAMInlinePolicy, error)
	ListIAMInlinePolicies(ctx context.Context, targetType, targetID string) ([]*IAMInlinePolicy, error)
	DeleteIAMInlinePolicy(ctx context.Context, targetType, targetID, name string) error

	// Roles
	CreateIAMRole(ctx context.Context, name, path, description, trustPolicy string, maxSessionDuration int, tenantID string) (*IAMRole, error)
	GetIAMRole(ctx context.Context, name string) (*IAMRole, error)
	ListIAMRoles(ctx context.Context) ([]*IAMRole, error)
	UpdateIAMRoleTrustPolicy(ctx context.Context, name, trustPolicy string) error
	DeleteIAMRole(ctx context.Context, name string) error

	// AssumeRole
	AssumeIAMRole(ctx context.Context, user *User, roleARN, roleSessionName string, durationSeconds int, sessionPolicy string) (*STSSession, error)

	// ResolveIAMUserForPolicy maps an IAM user name to the internal user ID
	// that policies attach to.
	ResolveIAMUserID(ctx context.Context, username string) (string, error)
}

// --- identities ---

// CreateIAMUser creates a user with no password and no role: everything it may
// do comes from the policies attached to it afterwards. It is an ordinary user
// — the authorization path does not distinguish it from any other.
func (am *authManager) CreateIAMUser(ctx context.Context, username, path, tenantID string) (*User, error) {
	if err := ValidateIAMName(username); err != nil {
		return nil, err
	}
	if _, err := am.store.GetUserByUsername(username); err == nil {
		return nil, fmt.Errorf("%w: user %q", ErrIAMEntityExists, username)
	}

	now := time.Now().Unix()
	user := &User{
		ID:           generateIAMUserID(),
		Username:     username,
		DisplayName:  username,
		Status:       UserStatusActive,
		TenantID:     tenantID,
		Roles:        []string{},
		Policies:     []string{},
		AuthProvider: "local",
		CreatedAt:    now,
		UpdatedAt:    now,
		Metadata:     map[string]string{"iam_path": normalizeIAMPath(path)},
	}
	if err := am.store.CreateUser(user); err != nil {
		return nil, err
	}

	am.logAuditEvent(ctx, &audit.AuditEvent{
		TenantID:     tenantID,
		UserID:       user.ID,
		Username:     username,
		EventType:    audit.EventTypeUserCreated,
		ResourceType: audit.ResourceTypeUser,
		ResourceID:   user.ID,
		ResourceName: username,
		Action:       audit.ActionCreate,
		Status:       audit.StatusSuccess,
		Details:      map[string]interface{}{"created_via": "iam"},
	})
	return user, nil
}

// DeleteIAMUser removes a user along with its credentials and policies.
//
// A user holding a role is refused: an integration deleting its own service
// identities must not be able to remove the administrator who configured it,
// and a role is what distinguishes a person's account from a credential holder.
func (am *authManager) DeleteIAMUser(ctx context.Context, username string) error {
	user, err := am.store.GetUserByUsername(username)
	if err != nil {
		return fmt.Errorf("%w: user %q", ErrIAMNoSuchEntity, username)
	}
	if len(user.Roles) > 0 {
		return fmt.Errorf("%w: %q holds a role and must be deleted from the console", ErrIAMInvalidInput, username)
	}

	if err := am.store.DeleteIAMPoliciesForTarget(IAMTargetUser, user.ID); err != nil {
		return err
	}
	if err := am.store.DeleteUser(user.ID); err != nil {
		return err
	}

	am.logAuditEvent(ctx, &audit.AuditEvent{
		TenantID:     user.TenantID,
		UserID:       user.ID,
		Username:     username,
		EventType:    audit.EventTypeUserDeleted,
		ResourceType: audit.ResourceTypeUser,
		ResourceID:   user.ID,
		ResourceName: username,
		Action:       audit.ActionDelete,
		Status:       audit.StatusSuccess,
	})
	return nil
}

// GetIAMUser returns a user by name.
func (am *authManager) GetIAMUser(ctx context.Context, username string) (*User, error) {
	user, err := am.store.GetUserByUsername(username)
	if err != nil {
		return nil, fmt.Errorf("%w: user %q", ErrIAMNoSuchEntity, username)
	}
	return user, nil
}

// ListIAMUsers returns every user. GetUser and ListUsers describe identities,
// and an integration needs to see the ones it did not create in order to avoid
// colliding with them.
func (am *authManager) ListIAMUsers(ctx context.Context) ([]*User, error) {
	users, err := am.store.ListUsers()
	if err != nil {
		return nil, err
	}
	return users, nil
}

// ResolveIAMUserID maps a user name to the ID policies attach to.
func (am *authManager) ResolveIAMUserID(ctx context.Context, username string) (string, error) {
	user, err := am.store.GetUserByUsername(username)
	if err != nil {
		return "", fmt.Errorf("%w: user %q", ErrIAMNoSuchEntity, username)
	}
	return user.ID, nil
}

// --- managed policies ---

func (am *authManager) CreateIAMPolicy(ctx context.Context, name, path, description, document string) (*IAMPolicy, error) {
	if err := ValidateIAMName(name); err != nil {
		return nil, err
	}
	if _, err := ParseIAMPolicy(document, IAMMaxManagedPolicyBytes); err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	policy := &IAMPolicy{
		Name:             name,
		ARN:              IAMPolicyARN(name),
		Path:             normalizeIAMPath(path),
		Description:      description,
		DefaultVersionID: "v1",
		CreatedAt:        now,
		UpdatedAt:        now,
		Document:         document,
	}
	if err := am.store.CreateIAMPolicy(policy, document); err != nil {
		return nil, err
	}
	return policy, nil
}

func (am *authManager) GetIAMPolicy(ctx context.Context, name string) (*IAMPolicy, error) {
	return am.store.GetIAMPolicy(name)
}

func (am *authManager) ListIAMPolicies(ctx context.Context) ([]*IAMPolicy, error) {
	return am.store.ListIAMPolicies()
}

func (am *authManager) DeleteIAMPolicy(ctx context.Context, name string) error {
	return am.store.DeleteIAMPolicy(name)
}

// --- policy versions ---

func (am *authManager) CreateIAMPolicyVersion(ctx context.Context, policyName, document string, setDefault bool) (*IAMPolicyVersion, error) {
	if _, err := ParseIAMPolicy(document, IAMMaxManagedPolicyBytes); err != nil {
		return nil, err
	}
	return am.store.CreateIAMPolicyVersion(policyName, document, setDefault)
}

func (am *authManager) GetIAMPolicyVersion(ctx context.Context, policyName, versionID string) (*IAMPolicyVersion, error) {
	return am.store.GetIAMPolicyVersion(policyName, versionID)
}

func (am *authManager) ListIAMPolicyVersions(ctx context.Context, policyName string) ([]*IAMPolicyVersion, error) {
	return am.store.ListIAMPolicyVersions(policyName)
}

func (am *authManager) DeleteIAMPolicyVersion(ctx context.Context, policyName, versionID string) error {
	return am.store.DeleteIAMPolicyVersion(policyName, versionID)
}

func (am *authManager) SetDefaultIAMPolicyVersion(ctx context.Context, policyName, versionID string) error {
	return am.store.SetDefaultIAMPolicyVersion(policyName, versionID)
}

// --- attachments ---

// CountIAMPolicyAttachments reports 0 rather than an error when the count
// cannot be read: it is descriptive metadata on a response, and failing a whole
// listing over it would be worse than under-reporting it.
func (am *authManager) CountIAMPolicyAttachments(ctx context.Context, policyName string) int {
	n, err := am.store.CountIAMPolicyAttachments(policyName)
	if err != nil {
		return 0
	}
	return n
}

func (am *authManager) AttachIAMPolicy(ctx context.Context, policyName, targetType, targetID string) error {
	if err := validateIAMTargetType(targetType); err != nil {
		return err
	}
	return am.store.AttachIAMPolicy(policyName, targetType, targetID)
}

func (am *authManager) DetachIAMPolicy(ctx context.Context, policyName, targetType, targetID string) error {
	if err := validateIAMTargetType(targetType); err != nil {
		return err
	}
	return am.store.DetachIAMPolicy(policyName, targetType, targetID)
}

func (am *authManager) ListAttachedIAMPolicies(ctx context.Context, targetType, targetID string) ([]*IAMPolicy, error) {
	names, err := am.store.ListAttachedIAMPolicyNames(targetType, targetID)
	if err != nil {
		return nil, err
	}
	policies := make([]*IAMPolicy, 0, len(names))
	for _, name := range names {
		policy, err := am.store.GetIAMPolicy(name)
		if err != nil {
			// An attachment to a policy that no longer exists is stale rather
			// than fatal; skip it instead of failing the whole listing.
			continue
		}
		policies = append(policies, policy)
	}
	return policies, nil
}

// --- inline policies ---

func (am *authManager) PutIAMInlinePolicy(ctx context.Context, targetType, targetID, name, document string) error {
	if err := validateIAMTargetType(targetType); err != nil {
		return err
	}
	if err := ValidateIAMName(name); err != nil {
		return err
	}
	if _, err := ParseIAMPolicy(document, IAMMaxInlinePolicyBytes); err != nil {
		return err
	}
	return am.store.PutIAMInlinePolicy(targetType, targetID, name, document)
}

func (am *authManager) GetIAMInlinePolicy(ctx context.Context, targetType, targetID, name string) (*IAMInlinePolicy, error) {
	return am.store.GetIAMInlinePolicy(targetType, targetID, name)
}

func (am *authManager) ListIAMInlinePolicies(ctx context.Context, targetType, targetID string) ([]*IAMInlinePolicy, error) {
	return am.store.ListIAMInlinePolicies(targetType, targetID)
}

func (am *authManager) DeleteIAMInlinePolicy(ctx context.Context, targetType, targetID, name string) error {
	return am.store.DeleteIAMInlinePolicy(targetType, targetID, name)
}

// --- roles ---

func (am *authManager) CreateIAMRole(ctx context.Context, name, path, description, trustPolicy string, maxSessionDuration int, tenantID string) (*IAMRole, error) {
	if err := ValidateIAMName(name); err != nil {
		return nil, err
	}
	// A trust policy is optional: without one the role is assigned to users,
	// with one it can also be assumed through AssumeRole.
	if strings.TrimSpace(trustPolicy) != "" {
		if _, err := ParseTrustPolicy(trustPolicy); err != nil {
			return nil, err
		}
	}
	if maxSessionDuration == 0 {
		maxSessionDuration = IAMDefaultMaxSessionDuration
	}
	if maxSessionDuration < STSMinSessionDuration || maxSessionDuration > am.stsMaxSessionDuration() {
		return nil, fmt.Errorf("%w: MaxSessionDuration must be between %d and %d seconds",
			ErrIAMInvalidInput, STSMinSessionDuration, am.stsMaxSessionDuration())
	}

	now := time.Now().Unix()
	role := &IAMRole{
		Name:               name,
		ARN:                IAMRoleARN(name),
		Path:               normalizeIAMPath(path),
		Description:        description,
		AssumeRolePolicy:   trustPolicy,
		MaxSessionDuration: maxSessionDuration,
		TenantID:           tenantID,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := am.store.CreateIAMRole(role); err != nil {
		return nil, err
	}
	return role, nil
}

func (am *authManager) GetIAMRole(ctx context.Context, name string) (*IAMRole, error) {
	return am.store.GetIAMRole(name)
}

func (am *authManager) ListIAMRoles(ctx context.Context) ([]*IAMRole, error) {
	return am.store.ListIAMRoles()
}

func (am *authManager) UpdateIAMRoleTrustPolicy(ctx context.Context, name, trustPolicy string) error {
	if _, err := ParseTrustPolicy(trustPolicy); err != nil {
		return err
	}
	role, err := am.store.GetIAMRole(name)
	if err != nil {
		return err
	}
	role.AssumeRolePolicy = trustPolicy
	return am.store.UpdateIAMRole(role)
}

func (am *authManager) DeleteIAMRole(ctx context.Context, name string) error {
	return am.store.DeleteIAMRole(name)
}

// --- helpers ---

// validateIAMTargetType rejects an unknown attachment target. Storing one would
// create a row nothing ever reads, so a policy an operator believed to be in
// force would silently do nothing.
func validateIAMTargetType(targetType string) error {
	switch targetType {
	case IAMTargetUser, IAMTargetGroup, IAMTargetRole, IAMTargetTenant:
		return nil
	default:
		return fmt.Errorf("%w: unknown target type %q", ErrIAMInvalidInput, targetType)
	}
}

// generateIAMUserID mints the internal ID of a service identity. It is random
// rather than derived from the user name because policy attachments key on this
// ID: reusing a deleted identity's name must not resurrect its permissions.
func generateIAMUserID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "iam-" + hex.EncodeToString(b)
}

// normalizeIAMPath returns an AWS-shaped path. Paths are organisational only —
// nothing here resolves entities by path — so an unusable one is corrected
// rather than rejected.
func normalizeIAMPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return path
}
