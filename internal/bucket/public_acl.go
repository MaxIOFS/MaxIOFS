package bucket

import (
	"context"
	"errors"
)

// ErrPublicACLBlocked is a public ACL refused by the bucket's Block Public
// Access (BlockPublicAcls).
var ErrPublicACLBlocked = errors.New("the bucket's Block Public Access blocks public ACLs")

// ErrPublicPolicyBlocked is a public bucket policy refused by the bucket's
// Block Public Access (BlockPublicPolicy).
var ErrPublicPolicyBlocked = errors.New("the bucket's Block Public Access blocks public bucket policies")

// CheckPublicACL returns ErrPublicACLBlocked when the bucket's Block Public
// Access blocks public ACLs. A request carrying a public ACL checks it before
// storing anything; ACLs already stored are kept.
func CheckPublicACL(ctx context.Context, m Manager, tenantID, name string) error {
	pab, err := publicAccessBlockOf(ctx, m, tenantID, name)
	if err != nil {
		return err
	}
	if pab != nil && pab.BlockPublicAcls {
		return ErrPublicACLBlocked
	}
	return nil
}

// CheckPublicPolicy returns ErrPublicPolicyBlocked when the policy is public
// and the bucket's Block Public Access blocks public policies. A policy
// already stored is kept.
func CheckPublicPolicy(ctx context.Context, m Manager, tenantID, name string, policy *Policy) error {
	if !IsPublicPolicy(policy) {
		return nil
	}
	pab, err := publicAccessBlockOf(ctx, m, tenantID, name)
	if err != nil {
		return err
	}
	if pab != nil && pab.BlockPublicPolicy {
		return ErrPublicPolicyBlocked
	}
	return nil
}

// publicAccessBlockOf returns the bucket's Block Public Access, nil when it
// has none.
func publicAccessBlockOf(ctx context.Context, m Manager, tenantID, name string) (*PublicAccessBlock, error) {
	pab, err := m.GetPublicAccessBlock(ctx, tenantID, name)
	if errors.Is(err, ErrPublicAccessBlockNotFound) {
		return nil, nil
	}
	return pab, err
}
