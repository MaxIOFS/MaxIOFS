package bucket

import (
	"context"
	"errors"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
)

// policyReader answers GetBucketPolicy only.
type policyReader struct {
	Manager
	policy *Policy
	err    error
}

func (p policyReader) GetBucketPolicy(ctx context.Context, tenantID, name string) (*Policy, error) {
	return p.policy, p.err
}

// A policy that cannot be read denies; a bucket without one, or without the
// bucket, leaves the decision to the caller's permissions.
func TestDecideBucketPolicy_WhenThePolicyIsMissingOrUnreadable(t *testing.T) {
	ctx := context.Background()
	const resource = "arn:aws:s3:::data/o"
	principals := []string{"user-id"}

	assert.Equal(t, auth.BucketPolicyDeny,
		DecideBucketPolicy(ctx, policyReader{err: errors.New("disk error")}, "", "s3:GetObject", resource, principals))
	assert.Equal(t, auth.BucketPolicyNoMatch,
		DecideBucketPolicy(ctx, policyReader{err: ErrPolicyNotFound}, "", "s3:GetObject", resource, principals))
	assert.Equal(t, auth.BucketPolicyNoMatch,
		DecideBucketPolicy(ctx, policyReader{err: ErrBucketNotFound}, "", "s3:GetObject", resource, principals))
	assert.Equal(t, auth.BucketPolicyNoMatch,
		DecideBucketPolicy(ctx, policyReader{err: errors.New("disk error")}, "", "s3:GetObject", "*", principals),
		"a resource outside any bucket has no bucket policy")
}
