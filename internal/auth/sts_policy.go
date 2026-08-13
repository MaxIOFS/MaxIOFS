package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	// STSMaxSessionPolicyBytes bounds the stored policy document. AWS uses the
	// same 2048-character budget for session policies.
	STSMaxSessionPolicyBytes = 2048

	// stsMaxSessionPolicyStatements bounds evaluation work per request.
	stsMaxSessionPolicyStatements = 20
)

// ErrSTSInvalidPolicy is returned when a session policy is malformed or uses a
// construct this evaluator does not support.
var ErrSTSInvalidPolicy = errors.New("invalid sts session policy")

// ParseSessionPolicy validates a session policy document and returns the parsed
func ParseSessionPolicy(raw string) (*Policy, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	if len(trimmed) > STSMaxSessionPolicyBytes {
		return nil, fmt.Errorf("%w: policy exceeds %d bytes", ErrSTSInvalidPolicy, STSMaxSessionPolicyBytes)
	}

	var policy Policy
	if err := json.Unmarshal([]byte(trimmed), &policy); err != nil {
		return nil, fmt.Errorf("%w: not valid JSON: %v", ErrSTSInvalidPolicy, err)
	}

	if len(policy.Statement) == 0 {
		return nil, fmt.Errorf("%w: policy has no statements", ErrSTSInvalidPolicy)
	}
	if len(policy.Statement) > stsMaxSessionPolicyStatements {
		return nil, fmt.Errorf("%w: policy has more than %d statements", ErrSTSInvalidPolicy, stsMaxSessionPolicyStatements)
	}

	for i, st := range policy.Statement {
		if st.Effect != "Allow" && st.Effect != "Deny" {
			return nil, fmt.Errorf("%w: statement %d has Effect %q (must be Allow or Deny)", ErrSTSInvalidPolicy, i, st.Effect)
		}
		if len(st.Principal) > 0 {
			return nil, fmt.Errorf("%w: statement %d must not set Principal", ErrSTSInvalidPolicy, i)
		}
		if len(st.Condition) > 0 {
			return nil, fmt.Errorf("%w: statement %d uses Condition, which session policies do not support yet", ErrSTSInvalidPolicy, i)
		}
		if len(policyStringValues(st.Action)) == 0 {
			return nil, fmt.Errorf("%w: statement %d has no Action", ErrSTSInvalidPolicy, i)
		}
		if len(policyStringValues(st.Resource)) == 0 {
			return nil, fmt.Errorf("%w: statement %d has no Resource", ErrSTSInvalidPolicy, i)
		}
	}

	return &policy, nil
}

// EvaluateSessionPolicy reports whether the policy permits action on resource.
// Standard IAM precedence: an explicit Deny wins over everything, otherwise an
// explicit Allow is required — there is no implicit allow.
func EvaluateSessionPolicy(policy *Policy, action, resource string) bool {
	if policy == nil {
		// No policy attached: the session does not restrict anything and the
		// base pipeline alone decides.
		return true
	}
	if action == "" {
		// The request did not map to a known action, so no statement can
		// legitimately match it. Fail closed.
		return false
	}

	allowed := false
	for _, st := range policy.Statement {
		if !stsActionMatches(st.Action, action) || !stsResourceMatches(st.Resource, resource) {
			continue
		}
		if st.Effect == "Deny" {
			return false
		}
		allowed = true
	}
	return allowed
}

// enforceSessionPolicy checks an authenticated request against the policy
func enforceSessionPolicy(sess *STSSession, r *http.Request) error {
	if sess == nil || strings.TrimSpace(sess.SessionPolicy) == "" {
		return nil
	}

	policy, err := ParseSessionPolicy(sess.SessionPolicy)
	if err != nil {
		return ErrAccessDenied
	}

	if !EvaluateSessionPolicy(policy, S3ActionForRequest(r), ResourceARNForRequest(r)) {
		return ErrAccessDenied
	}
	return nil
}

// --- matching helpers ---

// policyStringValues normalises an Action/Resource field (string, []string or
// []interface{} after a JSON round-trip) into a string slice.
func policyStringValues(v interface{}) []string {
	switch val := v.(type) {
	case string:
		if val == "" {
			return nil
		}
		return []string{val}
	case []string:
		return val
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// stsActionMatches reports whether any action pattern in the statement matches
func stsActionMatches(patterns interface{}, action string) bool {
	for _, p := range policyStringValues(patterns) {
		if stsWildcardMatch(strings.ToLower(p), strings.ToLower(action)) {
			return true
		}
	}
	return false
}

// stsResourceMatches reports whether any resource pattern matches the request
// ARN. Bare forms ("bucket", "bucket/*") are accepted and normalised to ARNs so
// a policy written without the arn prefix behaves as its author expects.
func stsResourceMatches(patterns interface{}, resource string) bool {
	for _, p := range policyStringValues(patterns) {
		if stsWildcardMatch(normalizeSTSResource(p), resource) {
			return true
		}
	}
	return false
}

// normalizeSTSResource prefixes a bare resource with the S3 ARN namespace.
// "*" stays as-is so it keeps matching every ARN.
func normalizeSTSResource(resource string) string {
	if resource == "*" || strings.HasPrefix(resource, "arn:aws:s3:::") {
		return resource
	}
	return "arn:aws:s3:::" + strings.TrimPrefix(resource, "/")
}

// stsWildcardMatch matches a pattern against s using AWS wildcard conventions:
// '*' matches any sequence of characters, '?' matches exactly one.
func stsWildcardMatch(pattern, s string) bool {
	if pattern == "*" || pattern == s {
		return true
	}

	pi, si := 0, 0
	starIdx, matchIdx := -1, 0
	for si < len(s) {
		switch {
		case pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == s[si]):
			pi++
			si++
		case pi < len(pattern) && pattern[pi] == '*':
			starIdx = pi
			matchIdx = si
			pi++
		case starIdx != -1:
			pi = starIdx + 1
			matchIdx++
			si = matchIdx
		default:
			return false
		}
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}
