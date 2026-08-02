package auth

// Policy parsing and evaluation — the one place a permission question is
// answered in MaxIOFS.
//
// Everything a user may do is a list of policy documents (see
// policy_translate.go for how the stored model becomes that list). Deciding is
// then plain AWS semantics: an explicit Deny anywhere wins, otherwise an
// explicit Allow is required, and an action nobody named is denied.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseIAMPolicy validates a policy document and returns its parsed form.
// maxBytes bounds the document — callers pass IAMMaxInlinePolicyBytes or
// IAMMaxManagedPolicyBytes to match the AWS budget for that kind of policy.
//
// Validation is strict: a construct that cannot be enforced exactly as written
// is refused while the author can still fix it, never quietly dropped at
// request time where it would show up as an unexplained denial.
func ParseIAMPolicy(raw string, maxBytes int) (*Policy, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: policy document is empty", ErrIAMInvalidInput)
	}
	if len(trimmed) > maxBytes {
		return nil, fmt.Errorf("%w: policy exceeds %d bytes", ErrIAMInvalidInput, maxBytes)
	}

	var policy Policy
	if err := json.Unmarshal([]byte(trimmed), &policy); err != nil {
		return nil, fmt.Errorf("%w: not valid JSON: %v", ErrIAMInvalidInput, err)
	}
	if len(policy.Statement) == 0 {
		return nil, fmt.Errorf("%w: policy has no statements", ErrIAMInvalidInput)
	}
	if len(policy.Statement) > IAMMaxPolicyStatements {
		return nil, fmt.Errorf("%w: policy has more than %d statements", ErrIAMInvalidInput, IAMMaxPolicyStatements)
	}

	for i, st := range policy.Statement {
		if st.Effect != EffectAllow && st.Effect != EffectDeny {
			return nil, fmt.Errorf("%w: statement %d has Effect %q (must be Allow or Deny)", ErrIAMInvalidInput, i, st.Effect)
		}
		if len(st.Principal) > 0 {
			// An identity policy is already attached to its principal. A
			// Principal field here would read as if it selected who the policy
			// applies to, which is a trust policy's job.
			return nil, fmt.Errorf("%w: statement %d must not set Principal (identity policies are attached, not addressed)", ErrIAMInvalidInput, i)
		}
		if len(st.Condition) > 0 {
			return nil, fmt.Errorf("%w: statement %d uses Condition, which is not supported yet", ErrIAMInvalidInput, i)
		}
		if len(policyStringValues(st.Action)) == 0 {
			return nil, fmt.Errorf("%w: statement %d has no Action", ErrIAMInvalidInput, i)
		}
		if len(policyStringValues(st.Resource)) == 0 {
			return nil, fmt.Errorf("%w: statement %d has no Resource", ErrIAMInvalidInput, i)
		}
	}

	return &policy, nil
}

// EvaluateIAMDocuments applies a set of policy documents to one
// (action, resource) pair. An explicit Deny in any document wins over an Allow
// in any other; without an explicit Allow the answer is no.
//
// A document that fails to parse denies outright. The only ways to store one
// are a corrupted row or a downgrade to a build that cannot read a construct a
// newer build accepted, and in both cases ignoring the document would serve a
// request its author may have meant to forbid.
func EvaluateIAMDocuments(documents []string, action, resource string) bool {
	if action == "" {
		return false
	}

	allowed := false
	for _, raw := range documents {
		var policy Policy
		if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &policy); err != nil {
			return false
		}
		for _, st := range policy.Statement {
			if !stsActionMatches(st.Action, action) || !stsResourceMatches(st.Resource, resource) {
				continue
			}
			if st.Effect == EffectDeny {
				return false
			}
			if st.Effect == EffectAllow {
				allowed = true
			}
		}
	}
	return allowed
}
