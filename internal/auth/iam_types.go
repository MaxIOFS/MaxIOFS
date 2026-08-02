package auth

// IAM entities. See docs/SECURITY.md, "IAM".
//
// MaxIOFS reuses its own users, groups and access keys as the IAM user, group
// and access-key entities; what IAM adds on top is policies (managed, with
// versions, plus inline) and roles (with a trust policy that decides who may
// assume them).
//
// Policies are the whole authorization model. What a user may do is what the
// policies attached to them, their groups, their tenant and their roles allow —
// evaluated AWS-style: default deny, explicit Deny wins. There is no second
// mechanism and no per-request translation of an older one; the permissions an
// installation already had were converted into these entities once, on upgrade
// (policy_migration.go).

import (
	"errors"
	"fmt"
	"strings"
)

// IAM policy attachment targets.
const (
	IAMTargetUser   = "user"
	IAMTargetGroup  = "group"
	IAMTargetRole   = "role"
	IAMTargetTenant = "tenant"
)

// IAM limits. AWS budgets inline policies at 2048 characters and managed
// policies at 6144; Veeam relies on both, switching to managed policies once
// its inline budget is exhausted.
const (
	IAMMaxInlinePolicyBytes  = 2048
	IAMMaxManagedPolicyBytes = 6144
	IAMMaxPolicyVersions     = 5
	IAMMaxPolicyStatements   = 100

	// IAMDefaultMaxSessionDuration is the AssumeRole cap for a role that does
	// not set one, matching the AWS default.
	IAMDefaultMaxSessionDuration = 3600
)

// IAM errors. They map onto the AWS IAM error codes in the query-protocol layer.
var (
	ErrIAMNoSuchEntity   = errors.New("iam: no such entity")
	ErrIAMEntityExists   = errors.New("iam: entity already exists")
	ErrIAMInvalidInput   = errors.New("iam: invalid input")
	ErrIAMLimitExceeded  = errors.New("iam: limit exceeded")
	ErrIAMDeleteConflict = errors.New("iam: entity is still attached")
)

// IAMPolicy is a managed policy. Document carries the default version's
// document when the policy is read through a call that resolves it.
type IAMPolicy struct {
	Name             string `json:"name"`
	ARN              string `json:"arn"`
	Path             string `json:"path"`
	Description      string `json:"description,omitempty"`
	DefaultVersionID string `json:"default_version_id"`
	IsBuiltin        bool   `json:"is_builtin"`
	Document         string `json:"document,omitempty"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
}

// IAMPolicyVersion is one immutable revision of a managed policy's document.
type IAMPolicyVersion struct {
	PolicyName string `json:"policy_name"`
	VersionID  string `json:"version_id"`
	Document   string `json:"document"`
	IsDefault  bool   `json:"is_default"`
	CreatedAt  int64  `json:"created_at"`
}

// IAMInlinePolicy is a policy embedded in a single user, group or role. It has
// no ARN and no versions, and is deleted with its target.
type IAMInlinePolicy struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Name       string `json:"name"`
	Document   string `json:"document"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

// IAMRole is an assumable identity. AssumeRolePolicy is the trust policy: it
// answers "who may assume this role", and is the only thing standing between a
// caller and the role's permissions, so it is required and never defaulted to
// something permissive.
type IAMRole struct {
	Name               string `json:"name"`
	ARN                string `json:"arn"`
	Path               string `json:"path"`
	Description        string `json:"description,omitempty"`
	AssumeRolePolicy   string `json:"assume_role_policy"`
	MaxSessionDuration int    `json:"max_session_duration"`
	TenantID           string `json:"tenant_id,omitempty"`
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `json:"updated_at"`
}

// IAMPolicyARN builds the canonical ARN for a managed policy. MaxIOFS has no
// AWS account, so the account field is empty — valid ARN syntax, and the same
// shape AWS uses for its own managed policies (arn:aws:iam::aws:policy/...).
func IAMPolicyARN(name string) string {
	return "arn:aws:iam:::policy/" + name
}

// IAMRoleARN builds the canonical ARN for a role.
func IAMRoleARN(name string) string {
	return "arn:aws:iam:::role/" + name
}

// IAMUserARN builds the ARN used to name a user as a principal in a trust policy.
func IAMUserARN(username string) string {
	return "arn:aws:iam:::user/" + username
}

// ParseIAMARN extracts the resource type and name from an IAM ARN. It is
// deliberately tolerant of the account field: callers write ARNs by hand and by
// copy-paste from AWS examples, and rejecting "arn:aws:iam::123456789012:role/x"
// while accepting "arn:aws:iam:::role/x" would be a pointless trap when the
// name is what identifies the entity here.
func ParseIAMARN(arn string) (resourceType, name string, err error) {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[2] != "iam" {
		return "", "", fmt.Errorf("%w: %q is not an IAM ARN", ErrIAMInvalidInput, arn)
	}
	resource := parts[5]
	slash := strings.Index(resource, "/")
	if slash <= 0 || slash == len(resource)-1 {
		return "", "", fmt.Errorf("%w: %q has no resource name", ErrIAMInvalidInput, arn)
	}
	// A path may sit between the type and the name ("role/prod/backup"); the
	// name is the last segment.
	resourceType = resource[:slash]
	name = resource[strings.LastIndex(resource, "/")+1:]
	return resourceType, name, nil
}

// ValidateIAMName checks an entity name against the AWS character set. The
// names end up in ARNs and in policy documents, so anything outside this set
// would produce ARNs that clients cannot round-trip.
func ValidateIAMName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name must not be empty", ErrIAMInvalidInput)
	}
	if len(name) > 128 {
		return fmt.Errorf("%w: name must be at most 128 characters", ErrIAMInvalidInput)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '+', r == '=', r == ',', r == '.', r == '@', r == '-', r == '_':
		default:
			return fmt.Errorf("%w: name %q contains an unsupported character %q", ErrIAMInvalidInput, name, r)
		}
	}
	return nil
}

// builtinIAMPolicies are seeded on startup so a fresh install has something to
// attach without writing JSON by hand. They are marked is_builtin and refused
// for deletion, but their documents are only written once — an operator who
// edits one keeps their edit across restarts.
var builtinIAMPolicies = []struct {
	Name        string
	Description string
	Document    string
}{
	{
		Name:        "ReadOnlyAccess",
		Description: "Read objects and list buckets, no writes",
		Document: `{"Version":"2012-10-17","Statement":[{"Effect":"Allow",` +
			`"Action":["s3:Get*","s3:List*","s3:Describe*"],"Resource":["*"]}]}`,
	},
	{
		Name:        "ReadWriteAccess",
		Description: "Full access to buckets and objects",
		Document: `{"Version":"2012-10-17","Statement":[{"Effect":"Allow",` +
			`"Action":["s3:*"],"Resource":["*"]}]}`,
	},
	{
		Name:        "WriteOnlyAccess",
		Description: "Upload objects without being able to read them back",
		Document: `{"Version":"2012-10-17","Statement":[{"Effect":"Allow",` +
			`"Action":["s3:PutObject","s3:AbortMultipartUpload","s3:ListBucketMultipartUploads"],` +
			`"Resource":["*"]}]}`,
	},
}
