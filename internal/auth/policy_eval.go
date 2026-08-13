package auth

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseIAMPolicy validates a policy document and returns its parsed form.
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
