package bucket

import (
	"context"
	"fmt"
	"strings"
)

// PolicyEvaluationRequest contains the context for policy evaluation
type PolicyEvaluationRequest struct {
	Principal string // User ARN or canonical user ID
	Action    string // S3 action (e.g., "s3:GetObject", "s3:PutObject")
	Resource  string // Resource ARN (e.g., "arn:aws:s3:::bucket/*")
	Bucket    string // Bucket name
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
// Returns DecisionAllow, DecisionDeny, or DecisionExplicitDeny
// AWS Policy evaluation logic:
// 1. By default, all requests are denied (implicit deny)
// 2. An explicit allow in a policy overrides the default deny
// 3. An explicit deny in a policy overrides any allows
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

	// TODO: Implement Condition evaluation in future enhancement
	// For now, we ignore conditions

	return true
}

// principalMatches checks if the principal matches
func principalMatches(principal interface{}, requestPrincipal string) bool {
	if principal == nil {
		// No principal specified = matches all (backward compatibility)
		return true
	}

	switch p := principal.(type) {
	case string:
		// Wildcard matches all
		if p == "*" {
			return true
		}
		// Exact match
		return p == requestPrincipal

	case map[string]interface{}:
		// AWS format: {"AWS": "arn:aws:iam::123456789012:user/username"}
		// Or: {"AWS": ["arn1", "arn2"]}
		if awsPrincipal, ok := p["AWS"]; ok {
			return matchesPrincipalValue(awsPrincipal, requestPrincipal)
		}
		// CanonicalUser format
		if canonicalUser, ok := p["CanonicalUser"]; ok {
			return matchesPrincipalValue(canonicalUser, requestPrincipal)
		}
		return false

	default:
		return false
	}
}

// matchesPrincipalValue matches principal value (can be string or array)
func matchesPrincipalValue(principalValue interface{}, requestPrincipal string) bool {
	switch v := principalValue.(type) {
	case string:
		if v == "*" {
			return true
		}
		return v == requestPrincipal

	case []interface{}:
		for _, item := range v {
			if str, ok := item.(string); ok {
				if str == "*" || str == requestPrincipal {
					return true
				}
			}
		}
		return false

	case []string:
		for _, str := range v {
			if str == "*" || str == requestPrincipal {
				return true
			}
		}
		return false

	default:
		return false
	}
}

// actionMatches checks if the action matches
func actionMatches(action interface{}, requestAction string) bool {
	if action == nil {
		return false
	}

	switch a := action.(type) {
	case string:
		return matchAction(a, requestAction)

	case []interface{}:
		for _, item := range a {
			if str, ok := item.(string); ok {
				if matchAction(str, requestAction) {
					return true
				}
			}
		}
		return false

	case []string:
		for _, str := range a {
			if matchAction(str, requestAction) {
				return true
			}
		}
		return false

	default:
		return false
	}
}

// matchAction matches a single action with wildcard support
func matchAction(policyAction, requestAction string) bool {
	// Exact match
	if policyAction == requestAction {
		return true
	}

	// Wildcard match: "s3:*" matches all S3 actions
	if policyAction == "*" || policyAction == "s3:*" {
		return true
	}

	// Prefix wildcard: "s3:Get*" matches "s3:GetObject", "s3:GetBucket", etc.
	if strings.HasSuffix(policyAction, "*") {
		prefix := strings.TrimSuffix(policyAction, "*")
		return strings.HasPrefix(requestAction, prefix)
	}

	return false
}

// resourceMatches checks if the resource matches
func resourceMatches(resource interface{}, requestResource string, bucketName string) bool {
	if resource == nil {
		return false
	}

	switch r := resource.(type) {
	case string:
		return matchResource(r, requestResource, bucketName)

	case []interface{}:
		for _, item := range r {
			if str, ok := item.(string); ok {
				if matchResource(str, requestResource, bucketName) {
					return true
				}
			}
		}
		return false

	case []string:
		for _, str := range r {
			if matchResource(str, requestResource, bucketName) {
				return true
			}
		}
		return false

	default:
		return false
	}
}

// matchResource matches a single resource with wildcard support
func matchResource(policyResource, requestResource, bucketName string) bool {
	// Exact match
	if policyResource == requestResource {
		return true
	}

	// Wildcard match: "*" matches all resources
	if policyResource == "*" {
		return true
	}

	// Normalize resource ARNs
	// Policy might use: "arn:aws:s3:::bucket/*" or just "bucket/*"
	// Request might use: "arn:aws:s3:::bucket/object.txt"
	normalizedPolicy := normalizeResourceARN(policyResource, bucketName)
	normalizedRequest := normalizeResourceARN(requestResource, bucketName)

	// Exact match after normalization
	if normalizedPolicy == normalizedRequest {
		return true
	}

	// Wildcard match: "arn:aws:s3:::bucket/*" matches "arn:aws:s3:::bucket/any/path"
	if strings.HasSuffix(normalizedPolicy, "/*") {
		prefix := strings.TrimSuffix(normalizedPolicy, "/*")
		// Match bucket/* against bucket/object or bucket/folder/object
		if strings.HasPrefix(normalizedRequest, prefix+"/") {
			return true
		}
	}

	// Wildcard match: "arn:aws:s3:::bucket*" matches "arn:aws:s3:::bucket" and "arn:aws:s3:::bucket/anything"
	if strings.HasSuffix(normalizedPolicy, "*") && !strings.HasSuffix(normalizedPolicy, "/*") {
		prefix := strings.TrimSuffix(normalizedPolicy, "*")
		if strings.HasPrefix(normalizedRequest, prefix) {
			return true
		}
	}

	return false
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

// IsActionAllowed is a convenience function that returns true if action is allowed
func IsActionAllowed(ctx context.Context, policy *Policy, request PolicyEvaluationRequest) bool {
	decision := EvaluatePolicy(ctx, policy, request)
	return decision == DecisionAllow
}

// IsActionDenied is a convenience function that returns true if action is explicitly denied
func IsActionDenied(ctx context.Context, policy *Policy, request PolicyEvaluationRequest) bool {
	decision := EvaluatePolicy(ctx, policy, request)
	return decision == DecisionExplicitDeny
}
