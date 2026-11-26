package bucket

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const anyStatement = `{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}`

// A policy is taken in every form AWS writes it.
func TestAPolicyIsTakenInEveryFormAWSWrites(t *testing.T) {
	for _, policy := range []string{
		`{"Version":"2012-10-17","Statement":[` + anyStatement + `]}`,
		`{"Version":"2008-10-17","Statement":[` + anyStatement + `]}`,
		`{"Version":"2012-10-17","Statement":` + anyStatement + `}`,
		"\xEF\xBB\xBF" + `{"Version":"2012-10-17","Statement":[` + anyStatement + `]}`,
		`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Principal":{"AWS":"*"},"Action":"*","Resource":"arn:aws:s3:::b"}]}`,
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["arn:aws:iam:::user/alice","user-id"],"CanonicalUser":"bob-id"},"Action":["s3:GetObject","s3:List*"],"Resource":["arn:aws:s3:::b","arn:aws:s3:::b/*"]}]}`,
	} {
		_, err := ValidatePolicy([]byte(policy), "b")
		assert.NoError(t, err, policy)
	}
}

// What the evaluator would not enforce as written is refused, and the error
// names the statement.
func TestAPolicyIsRefusedWhenItCannotBeEnforced(t *testing.T) {
	cases := []struct{ policy, says string }{
		{`not json`, "not valid JSON"},
		{`{"Statement":[` + anyStatement + `]}`, "Version"},
		{`{"Version":"2012-10-18","Statement":[` + anyStatement + `]}`, "Version"},
		{`{"Version":"2012-10-17","Statement":"x"}`, "Statement must be"},
		{`{"Version":"2012-10-17","Statement":[]}`, "no Statement"},
		{`{"Version":"2012-10-17","Statement":[` + anyStatement + `,{"Effect":"Allow","NotAction":"s3:GetObject","Principal":"*","Resource":"arn:aws:s3:::b/*"}]}`, "statement 2: NotAction is not supported"},
		{`{"Version":"2012-10-17","Statement":[` + anyStatement + `,{"Effect":"Maybe","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`, "statement 2: Effect"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"alice","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`, "Principal must be"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`, "Principal is required"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`, "Principal is empty"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"s3.amazonaws.com"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`, "principal type Service"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":[]},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`, "names no one"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":""},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`, "a principal is empty"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam:::user/"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`, "not supported"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam:::user/a*"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`, "not supported"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:root"},"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`, "not supported"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Resource":"arn:aws:s3:::b/*"}]}`, "Action is required"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"iam:PassRole","Resource":"arn:aws:s3:::b/*"}]}`, "not an S3 action"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject"}]}`, "Resource is required"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::other/*"}]}`, "not this bucket"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b*"}]}`, "not this bucket"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*","Condition":{"NumericLessThan":{"aws:SourceIp":"1"}}}]}`, "operator NumericLessThan"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*","Condition":{"IpAddress":"10.0.0.0/8"}}]}`, "malformed"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*","Condition":{"IpAddress":{}}}]}`, "malformed"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*","Condition":{"StringEquals":{"s3:prefix":"x"}}}]}`, "condition key s3:prefix"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*","Condition":{"IpAddress":{"aws:SourceIp":[]}}}]}`, "has no value"},
		{`{"Version":"2012-10-17"}`, "Statement must be"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*","Condition":{"NotIpAddress":{"aws:SourceIp":"10.0.0.0/33"}}}]}`, "not an address"},
		{`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*","Condition":{"IpAddress":{"aws:SourceIp":["10.0.0.0/8","10.0.0.300"]}}}]}`, "not an address"},
	}
	for _, c := range cases {
		_, err := ValidatePolicy([]byte(c.policy), "b")
		require.ErrorIs(t, err, ErrMalformedPolicy, c.policy)
		assert.Contains(t, err.Error(), c.says, c.policy)
	}
}

// A condition an earlier release stored without checking it, of an operator
// the evaluator does not know or naming no value, does not hold: negated or
// not, the statement does not apply.
func TestAConditionTheEvaluatorCannotReadDoesNotHold(t *testing.T) {
	for _, condition := range []map[string]interface{}{
		{"NumericLessThan": map[string]interface{}{"aws:SourceIp": "1"}},
		{"StringEquals": map[string]interface{}{"aws:SourceIp": []interface{}{}}},
		{"StringNotEquals": map[string]interface{}{"aws:SourceIp": []interface{}{}}},
		{"NotIpAddress": map[string]interface{}{"aws:SourceIp": nil}},
		{"IpAddress": "10.0.0.0/8"},
		{"IpAddress": map[string]interface{}{"aws:SourceIp": "not an address"}},
	} {
		policy := &Policy{Version: "2012-10-17", Statement: []Statement{{
			Effect: "Deny", Principal: "*", Action: "s3:GetObject", Resource: "arn:aws:s3:::b/*", Condition: condition,
		}}}
		got := EvaluatePolicy(context.Background(), policy, PolicyEvaluationRequest{
			Principal: "*", Action: "s3:GetObject", Resource: "arn:aws:s3:::b/k", Bucket: "b", SourceIP: "10.1.1.1",
		})
		assert.Equal(t, DecisionDeny, got, "%v", condition)
	}
}

// A statement an earlier release stored with no principal, or one of a type
// it does not know, names no one.
func TestAStatementWithoutAPrincipalNamesNoOne(t *testing.T) {
	for _, principal := range []interface{}{nil, 42, []interface{}{"*"}} {
		policy := &Policy{Version: "2012-10-17", Statement: []Statement{{
			Effect: "Deny", Principal: principal, Action: "s3:GetObject", Resource: "arn:aws:s3:::b/*",
		}}}
		got := EvaluatePolicy(context.Background(), policy, PolicyEvaluationRequest{
			Principal: "*", Action: "s3:GetObject", Resource: "arn:aws:s3:::b/k", Bucket: "b",
		})
		assert.Equal(t, DecisionDeny, got, "%v", principal)
	}
}
