package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The bucket policy and the principal's policies decide together, as in AWS:
// a Deny in either refuses, an Allow in either grants; the account boundary
// and a session policy bound both.
func TestPolicySetAllows_WithTheBucketPolicy(t *testing.T) {
	const read = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"arn:aws:s3:::data/*"}]}`
	const deny = `{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Action":"s3:GetObject","Resource":"arn:aws:s3:::data/*"}]}`
	const other = `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:PutObject","Resource":"arn:aws:s3:::data/*"}]}`
	onlyPut := &Policy{Version: "2012-10-17", Statement: []Statement{{Effect: EffectAllow, Action: "s3:PutObject", Resource: "arn:aws:s3:::data/*"}}}

	request := func(owner ResourceOwner, decision BucketPolicyDecision) AccessRequest {
		return AccessRequest{Action: "s3:GetObject", Resource: "arn:aws:s3:::data/o", Owner: owner, BucketPolicy: decision}
	}
	tenant := OwnedBy("t1")

	for name, tc := range map[string]struct {
		set  *PolicySet
		req  AccessRequest
		want bool
	}{
		"own permission, no statement":     {&PolicySet{TenantID: "t1", Documents: []string{read}}, request(tenant, BucketPolicyNoMatch), true},
		"bucket policy grants":             {&PolicySet{TenantID: "t1", Documents: []string{other}}, request(tenant, BucketPolicyAllow), true},
		"neither grants":                   {&PolicySet{TenantID: "t1", Documents: []string{other}}, request(tenant, BucketPolicyNoMatch), false},
		"bucket policy denies":             {&PolicySet{TenantID: "t1", Documents: []string{read}}, request(tenant, BucketPolicyDeny), false},
		"own Deny beats the bucket policy": {&PolicySet{TenantID: "t1", Documents: []string{deny}}, request(tenant, BucketPolicyAllow), false},
		"session policy bounds the grant":  {&PolicySet{TenantID: "t1", Documents: []string{other}, SessionPolicy: onlyPut}, request(tenant, BucketPolicyAllow), false},
		"another tenant gets nothing":      {&PolicySet{TenantID: "t2", Documents: []string{read}}, request(tenant, BucketPolicyAllow), false},
		"an owner nobody resolved":         {&PolicySet{TenantID: "t1", Documents: []string{read}}, request(ResourceOwner{}, BucketPolicyAllow), false},
		"unreadable document denies":       {&PolicySet{TenantID: "t1", Documents: []string{"{"}}, request(tenant, BucketPolicyAllow), false},
	} {
		assert.Equal(t, tc.want, tc.set.Allows(tc.req), name)
	}
}
