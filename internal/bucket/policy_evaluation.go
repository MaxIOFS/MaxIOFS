package bucket

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
)

// PolicyEvaluationRequest contains the context for policy evaluation
type PolicyEvaluationRequest struct {
	Principal       string // User ARN or canonical user ID
	Action          string // S3 action (e.g., "s3:GetObject", "s3:PutObject")
	Resource        string // Resource ARN (e.g., "arn:aws:s3:::bucket/*")
	Bucket          string // Bucket name
	SourceIP        string // Client IP address (for aws:SourceIp conditions)
	SecureTransport bool   // Whether the request uses TLS (for aws:SecureTransport conditions)
}

// PolicyDecision represents the result of policy evaluation
type PolicyDecision int

const (
	// DecisionDeny is the default decision (implicit deny)
	DecisionDeny PolicyDecision = iota
	// DecisionAllow means the action is explicitly allowed
	DecisionAllow
	// DecisionExplicitDeny means the action is explicitly denied (overrides allows)
	DecisionExplicitDeny
)

// EvaluatePolicy evaluates a bucket policy against a request
func EvaluatePolicy(ctx context.Context, policy *Policy, request PolicyEvaluationRequest) PolicyDecision {
	if policy == nil || len(policy.Statement) == 0 {
		// No policy = implicit deny (default deny)
		return DecisionDeny
	}

	hasExplicitAllow := false
	hasExplicitDeny := false

	// Evaluate each statement
	for _, statement := range policy.Statement {
		// Check if statement matches the request
		if !statementMatches(statement, request) {
			continue
		}

		// Statement matches - check effect
		if statement.Effect == "Allow" {
			hasExplicitAllow = true
		} else if statement.Effect == "Deny" {
			hasExplicitDeny = true
			// Explicit deny found - stop evaluation (deny always wins)
			break
		}
	}

	// Apply AWS evaluation logic
	if hasExplicitDeny {
		return DecisionExplicitDeny
	}
	if hasExplicitAllow {
		return DecisionAllow
	}

	// Default deny (no matching allow statements)
	return DecisionDeny
}

// statementMatches checks if a statement matches the request
func statementMatches(statement Statement, request PolicyEvaluationRequest) bool {
	// Check Principal match
	if !principalMatches(statement.Principal, request.Principal) {
		return false
	}

	// Check Action match
	if !actionMatches(statement.Action, request.Action) {
		return false
	}

	// Check Resource match
	if !resourceMatches(statement.Resource, request.Resource, request.Bucket) {
		return false
	}

	// Check Condition match — all conditions must pass (AND logic)
	if !conditionMatches(statement.Condition, request) {
		return false
	}

	return true
}

// principalMatches checks if the principal matches.
// A nil principal means the statement is malformed and does NOT match anyone,
// consistent with AWS S3 behavior (Principal is required in bucket policies).
// {"AWS": ...} and {"CanonicalUser": ...}, a name or a list: any principal of
// either names the caller.
func principalMatches(principal interface{}, requestPrincipal string) bool {
	switch p := principal.(type) {
	case string:
		return matchesPrincipalValue(p, requestPrincipal)
	case map[string]interface{}:
		return matchesPrincipalValue(p["AWS"], requestPrincipal) ||
			matchesPrincipalValue(p["CanonicalUser"], requestPrincipal)
	default:
		return false
	}
}

// matchesPrincipalValue matches a principal value, a name or a list.
func matchesPrincipalValue(principalValue interface{}, requestPrincipal string) bool {
	return slices.ContainsFunc(toStringSlice(principalValue), func(name string) bool {
		return name == "*" || name == requestPrincipal
	})
}

// actionMatches checks if the action, a name or a list, matches.
func actionMatches(action interface{}, requestAction string) bool {
	return slices.ContainsFunc(toStringSlice(action), func(policyAction string) bool {
		return matchAction(policyAction, requestAction)
	})
}

// matchAction matches an action as AWS does: whatever its case, with '*' and
// '?' anywhere in it.
func matchAction(policyAction, requestAction string) bool {
	return wildcardMatch(strings.ToLower(policyAction), strings.ToLower(requestAction))
}

// resourceMatches checks if the resource, a name or a list, matches.
func resourceMatches(resource interface{}, requestResource string, bucketName string) bool {
	return slices.ContainsFunc(toStringSlice(resource), func(policyResource string) bool {
		return matchResource(policyResource, requestResource, bucketName)
	})
}

// matchResource matches a resource as written, with '*' and '?' anywhere in
// it: a key keeps its case.
func matchResource(policyResource, requestResource, bucketName string) bool {
	return wildcardMatch(normalizeResourceARN(policyResource, bucketName), normalizeResourceARN(requestResource, bucketName))
}

// normalizeResourceARN converts various resource formats to consistent ARN format
func normalizeResourceARN(resource, bucketName string) string {
	// Already in ARN format
	if strings.HasPrefix(resource, "arn:aws:s3:::") {
		return resource
	}

	// Bucket-only format: "bucket" -> "arn:aws:s3:::bucket"
	if resource == bucketName {
		return fmt.Sprintf("arn:aws:s3:::%s", bucketName)
	}

	// Object format: "bucket/object" -> "arn:aws:s3:::bucket/object"
	if strings.HasPrefix(resource, bucketName+"/") {
		return fmt.Sprintf("arn:aws:s3:::%s", resource)
	}

	// Wildcard format: "bucket/*" -> "arn:aws:s3:::bucket/*"
	if strings.HasPrefix(resource, bucketName) {
		return fmt.Sprintf("arn:aws:s3:::%s", resource)
	}

	// Unknown format - return as-is
	return resource
}

// ─── Condition evaluation ─────────────────────────────────────────────────────

// conditionMatches evaluates all condition blocks against the request.
// All condition operators must match (AND logic across operators).
// Within each operator, all condition keys must match (AND logic).
func conditionMatches(condition map[string]interface{}, req PolicyEvaluationRequest) bool {
	if len(condition) == 0 {
		return true
	}

	for operator, conditionValue := range condition {
		kvMap, ok := conditionValue.(map[string]interface{})
		if !ok {
			// Malformed condition block — treat as non-matching
			return false
		}
		for condKey, condExpected := range kvMap {
			requestValue := resolveConditionKey(condKey, req)
			if !evaluateOperator(operator, requestValue, condExpected) {
				return false
			}
		}
	}
	return true
}

// resolveConditionKey maps an AWS condition key to its value from the request:
// the keys a policy may name (see ValidatePolicy); any other has none.
func resolveConditionKey(key string, req PolicyEvaluationRequest) string {
	switch strings.ToLower(key) {
	case "aws:sourceip":
		return req.SourceIP
	case "aws:securetransport":
		if req.SecureTransport {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

// conditionOperator is how a condition operator compares the request's value
// with each value a policy names: the condition holds when one of them
// matches or, negated, when none does. An address operator holds for no
// request whose address cannot be read, negated or not.
type conditionOperator struct {
	matches func(requestValue, policyValue string) bool
	negated bool
	address bool
}

// conditionOperators are the condition operators a policy may use, by their
// name in lower case.
var conditionOperators = map[string]conditionOperator{
	"stringequals":              {matches: equalValue},
	"stringnotequals":           {matches: equalValue, negated: true},
	"stringequalsignorecase":    {matches: strings.EqualFold},
	"stringnotequalsignorecase": {matches: strings.EqualFold, negated: true},
	"stringlike":                {matches: likeValue},
	"stringnotlike":             {matches: likeValue, negated: true},
	"bool":                      {matches: strings.EqualFold},
	"ipaddress":                 {matches: inAddressRange, address: true},
	"notipaddress":              {matches: inAddressRange, negated: true, address: true},
	"arnequals":                 {matches: likeValue},
	"arnlike":                   {matches: likeValue},
	"arnnotequals":              {matches: likeValue, negated: true},
	"arnnotlike":                {matches: likeValue, negated: true},
	"numericequals":             {matches: equalValue},
	"numericnotequals":          {matches: equalValue, negated: true},
}

func equalValue(requestValue, policyValue string) bool { return requestValue == policyValue }

func likeValue(requestValue, pattern string) bool { return wildcardMatch(pattern, requestValue) }

func inAddressRange(requestValue, cidr string) bool {
	ip := net.ParseIP(requestValue)
	return ip != nil && ipMatchesCIDR(ip, cidr)
}

// evaluateOperator decides one condition: an unknown operator, or one that
// names no value, does not hold.
func evaluateOperator(operator, requestValue string, expected interface{}) bool {
	op, known := conditionOperators[strings.ToLower(operator)]
	expectedValues := toStringSlice(expected)
	if !known || len(expectedValues) == 0 || (op.address && net.ParseIP(requestValue) == nil) {
		return false
	}
	return slices.ContainsFunc(expectedValues, func(policyValue string) bool {
		return op.matches(requestValue, policyValue)
	}) != op.negated
}

// wildcardMatch matches pattern against s using AWS wildcard conventions:
// '*' matches any sequence of characters, '?' matches any single character —
// a character, not a byte of one.
func wildcardMatch(patternText, text string) bool {
	// Fast paths
	if patternText == text {
		return true
	}

	pattern, s := []rune(patternText), []rune(text)
	pi, si := 0, 0
	starIdx := -1
	var matchIdx int

	for si < len(s) {
		if pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == s[si]) {
			pi++
			si++
		} else if pi < len(pattern) && pattern[pi] == '*' {
			starIdx = pi
			matchIdx = si
			pi++
		} else if starIdx != -1 {
			pi = starIdx + 1
			matchIdx++
			si = matchIdx
		} else {
			return false
		}
	}
	return onlyStars(pattern[pi:])
}

// onlyStars reports whether what is left of a pattern matches nothing more.
func onlyStars(rest []rune) bool {
	for _, r := range rest {
		if r != '*' {
			return false
		}
	}
	return true
}

// ipMatchesCIDR checks whether ip is contained in the CIDR range.
// Also accepts a bare IP address (treated as /32 or /128).
func ipMatchesCIDR(ip net.IP, cidr string) bool {
	// Try CIDR first
	_, network, err := net.ParseCIDR(cidr)
	if err == nil {
		return network.Contains(ip)
	}
	// Try bare IP
	other := net.ParseIP(cidr)
	if other != nil {
		return ip.Equal(other)
	}
	return false
}

// toStringSlice normalises a condition value (string or []interface{}) to []string.
func toStringSlice(v interface{}) []string {
	switch val := v.(type) {
	case string:
		return []string{val}
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return val
	default:
		return nil
	}
}

// IsActionAllowed is a convenience function that returns true if action is allowed
func IsActionAllowed(ctx context.Context, policy *Policy, request PolicyEvaluationRequest) bool {
	decision := EvaluatePolicy(ctx, policy, request)
	return decision == DecisionAllow
}
