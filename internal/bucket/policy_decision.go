package bucket

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/maxiofs/maxiofs/internal/auth"
)

// RequestFacts are what bucket policy conditions test about a request.
type RequestFacts struct {
	SourceIP        string
	SecureTransport bool
}

type requestFactsKey struct{}

// WithRequestFacts carries the facts of a request on its context.
func WithRequestFacts(ctx context.Context, facts RequestFacts) context.Context {
	return context.WithValue(ctx, requestFactsKey{}, facts)
}

// RequestFactsFrom returns the facts of the request ctx belongs to.
func RequestFactsFrom(ctx context.Context) RequestFacts {
	facts, _ := ctx.Value(requestFactsKey{}).(RequestFacts)
	return facts
}

// PolicyPrincipals are the names a bucket policy can give a signed user: its
// ID and its user ARN.
func PolicyPrincipals(user *auth.User) []string {
	if user == nil {
		return nil
	}
	names := []string{user.ID}
	if user.Username != "" {
		names = append(names, auth.IAMUserARN(user.Username))
	}
	return names
}

// policyManagementActions are decided by the caller's own permissions: a
// policy that denies everything can still be read, replaced or removed.
var policyManagementActions = map[string]bool{
	auth.ActionGetBucketPolicy:    true,
	auth.ActionPutBucketPolicy:    true,
	auth.ActionDeleteBucketPolicy: true,
}

// DecideBucketPolicy answers a signed request with the policy of the bucket
// the resource is in.
func DecideBucketPolicy(ctx context.Context, m Manager, tenantID, action, resource string, principals []string) auth.BucketPolicyDecision {
	decision, _ := decideBucketPolicy(ctx, m, tenantID, action, resource, principals)
	return decision
}

// DecideAnonymousBucketPolicy answers a request without credentials. Only a
// statement naming everyone ("*") applies. RestrictPublicBuckets voids what a
// public policy grants.
func DecideAnonymousBucketPolicy(ctx context.Context, m Manager, tenantID, action, resource string) auth.BucketPolicyDecision {
	decision, policy := decideBucketPolicy(ctx, m, tenantID, action, resource, nil)
	if decision != auth.BucketPolicyAllow || !IsPublicPolicy(policy) {
		return decision
	}
	pab, err := publicAccessBlockOf(ctx, m, tenantID, bucketOfARN(resource))
	if err != nil {
		return auth.BucketPolicyDeny
	}
	if pab != nil && pab.RestrictPublicBuckets {
		return auth.BucketPolicyNoMatch
	}
	return decision
}

func decideBucketPolicy(ctx context.Context, m Manager, tenantID, action, resource string, principals []string) (auth.BucketPolicyDecision, *Policy) {
	if m == nil || policyManagementActions[action] {
		return auth.BucketPolicyNoMatch, nil
	}
	name := bucketOfARN(resource)
	if name == "" {
		return auth.BucketPolicyNoMatch, nil
	}
	policy, err := m.GetBucketPolicy(ctx, tenantID, name)
	switch {
	case errors.Is(err, ErrPolicyNotFound), errors.Is(err, ErrBucketNotFound):
		return auth.BucketPolicyNoMatch, nil
	case err != nil:
		// A policy that cannot be read may deny: refuse.
		return auth.BucketPolicyDeny, nil
	}

	facts := RequestFactsFrom(ctx)
	if len(principals) == 0 {
		// Without credentials only "*" matches.
		principals = []string{""}
	}
	decision := auth.BucketPolicyNoMatch
	for _, principal := range principals {
		switch EvaluatePolicy(ctx, policy, PolicyEvaluationRequest{
			Principal:       principal,
			Action:          action,
			Resource:        resource,
			Bucket:          name,
			SourceIP:        facts.SourceIP,
			SecureTransport: facts.SecureTransport,
		}) {
		case DecisionExplicitDeny:
			return auth.BucketPolicyDeny, policy
		case DecisionAllow:
			decision = auth.BucketPolicyAllow
		}
	}
	return decision, policy
}

// bucketOfARN returns the bucket an S3 ARN names, or "".
func bucketOfARN(resource string) string {
	rest, ok := strings.CutPrefix(resource, "arn:aws:s3:::")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, "/")
	return name
}

// IsPublicPolicy reports whether a policy grants anything to everyone, as AWS
// S3 defines it: an Allow naming "*" that no aws:SourceIp condition limits to
// ranges of /8 or narrower (IPv4) or /32 or narrower (IPv6).
func IsPublicPolicy(policy *Policy) bool {
	if policy == nil {
		return false
	}
	for _, st := range policy.Statement {
		if st.Effect == "Allow" && namesEveryone(st.Principal) && !limitedToSourceIPs(st.Condition) {
			return true
		}
	}
	return false
}

func namesEveryone(principal interface{}) bool {
	switch p := principal.(type) {
	case string:
		return p == "*"
	case map[string]interface{}:
		for _, v := range p {
			for _, name := range toStringSlice(v) {
				if name == "*" {
					return true
				}
			}
		}
	}
	return false
}

func limitedToSourceIPs(condition map[string]interface{}) bool {
	for operator, block := range condition {
		if !strings.EqualFold(operator, "IpAddress") {
			continue
		}
		keys, ok := block.(map[string]interface{})
		if !ok {
			continue
		}
		for key, value := range keys {
			if !strings.EqualFold(key, "aws:SourceIp") {
				continue
			}
			ranges := toStringSlice(value)
			if len(ranges) == 0 {
				continue
			}
			narrow := true
			for _, r := range ranges {
				narrow = narrow && isNarrowRange(r)
			}
			if narrow {
				return true
			}
		}
	}
	return false
}

func isNarrowRange(value string) bool {
	if !strings.Contains(value, "/") {
		return net.ParseIP(value) != nil
	}
	_, network, err := net.ParseCIDR(value)
	if err != nil {
		return false
	}
	ones, bits := network.Mask.Size()
	if bits == 32 {
		return ones >= 8
	}
	return ones >= 32
}
