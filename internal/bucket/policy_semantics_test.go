package bucket

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decide is what a request gets from a policy put on bucket "b".
func decide(t *testing.T, statement string, req PolicyEvaluationRequest) PolicyDecision {
	t.Helper()
	policy, err := ValidatePolicy([]byte(`{"Version":"2012-10-17","Statement":[`+statement+`]}`), "b")
	require.NoError(t, err)
	req.Bucket = "b"
	if req.Principal == "" {
		req.Principal = "*"
	}
	return EvaluatePolicy(context.Background(), policy, req)
}

func denyAll(action, resource string) string {
	return fmt.Sprintf(`{"Effect":"Deny","Principal":"*","Action":%q,"Resource":%q}`, action, resource)
}

// An action names the same action whatever its case, as in AWS: a Deny
// written in lower case denies.
func TestAnActionIsMatchedWhateverItsCase(t *testing.T) {
	req := PolicyEvaluationRequest{Action: "s3:DeleteObject", Resource: "arn:aws:s3:::b/k"}
	assert.Equal(t, DecisionExplicitDeny, decide(t, denyAll("s3:deleteobject", "arn:aws:s3:::b/*"), req))
	assert.Equal(t, DecisionExplicitDeny, decide(t, denyAll("S3:DELETE*", "arn:aws:s3:::b/*"), req))
}

// A wildcard in an action matches anywhere in it.
func TestAnActionWildcardMatchesAnywhere(t *testing.T) {
	cases := []struct {
		pattern, action string
		denied          bool
	}{
		{"s3:*Object", "s3:PutObject", true},
		{"s3:*Object", "s3:GetObjectAcl", false},
		{"s3:Get*Acl", "s3:GetObjectAcl", true},
		{"s3:Get*Acl", "s3:GetObject", false},
		{"s3:?etObject", "s3:GetObject", true},
		{"s3:?etObject", "s3:PutObject", false},
	}
	for _, c := range cases {
		got := decide(t, denyAll(c.pattern, "arn:aws:s3:::b/*"), PolicyEvaluationRequest{Action: c.action, Resource: "arn:aws:s3:::b/k"})
		assert.Equal(t, c.denied, got == DecisionExplicitDeny, "%s against %s", c.pattern, c.action)
	}
}

// A wildcard in a resource matches anywhere in it, '?' one character, and the
// rest is matched as written.
func TestAResourceWildcardMatchesAnywhere(t *testing.T) {
	cases := []struct {
		pattern, resource string
		denied            bool
	}{
		{"arn:aws:s3:::b/*/secret/*", "arn:aws:s3:::b/team/secret/x", true},
		{"arn:aws:s3:::b/*/secret/*", "arn:aws:s3:::b/team/public/x", false},
		{"arn:aws:s3:::b/user-?/*", "arn:aws:s3:::b/user-1/k", true},
		{"arn:aws:s3:::b/user-?/*", "arn:aws:s3:::b/user-é/k", true},
		{"arn:aws:s3:::b/user-?/*", "arn:aws:s3:::b/user-12/k", false},
		{"arn:aws:s3:::b/Docs/*", "arn:aws:s3:::b/docs/k", false},
		{"arn:aws:s3:::b/*", "arn:aws:s3:::b", false},
		{"arn:aws:s3:::b", "arn:aws:s3:::b", true},
	}
	for _, c := range cases {
		got := decide(t, denyAll("s3:GetObject", c.pattern), PolicyEvaluationRequest{Action: "s3:GetObject", Resource: c.resource})
		assert.Equal(t, c.denied, got == DecisionExplicitDeny, "%s against %s", c.pattern, c.resource)
	}
}

// Every principal a statement names is matched, of either type.
func TestEveryPrincipalOfAStatementIsMatched(t *testing.T) {
	statement := `{"Effect":"Deny","Principal":{"AWS":["arn:aws:iam:::user/alice"],"CanonicalUser":"bob-id"},` +
		`"Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}`
	for principal, want := range map[string]PolicyDecision{
		"arn:aws:iam:::user/alice": DecisionExplicitDeny,
		"bob-id":                   DecisionExplicitDeny,
		"carol-id":                 DecisionDeny,
	} {
		got := decide(t, statement, PolicyEvaluationRequest{Principal: principal, Action: "s3:GetObject", Resource: "arn:aws:s3:::b/k"})
		assert.Equal(t, want, got, principal)
	}
}

// A statement applies when every condition holds, and a condition when its
// key has any of the values it names.
func TestConditionsDecideAStatement(t *testing.T) {
	allowIf := func(condition string) string {
		return `{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*","Condition":` + condition + `}`
	}
	cases := []struct {
		condition string
		ip        string
		tls       bool
		allowed   bool
	}{
		{`{"IpAddress":{"aws:SourceIp":"10.0.0.0/8"}}`, "10.1.2.3", false, true},
		{`{"IpAddress":{"aws:SourceIp":"10.0.0.0/8"}}`, "192.168.1.1", false, false},
		{`{"IpAddress":{"aws:SourceIp":["192.168.0.0/16","10.0.0.0/8"]}}`, "10.1.2.3", false, true},
		{`{"IpAddress":{"aws:SourceIp":"10.1.2.3"}}`, "10.1.2.3", false, true},
		{`{"IpAddress":{"aws:SourceIp":"10.1.2.3"}}`, "10.1.2.4", false, false},
		{`{"IpAddress":{"aws:SourceIp":"10.0.0.0/8"}}`, "", false, false},
		{`{"NotIpAddress":{"aws:SourceIp":"10.0.0.0/8"}}`, "192.168.1.1", false, true},
		{`{"NotIpAddress":{"aws:SourceIp":"10.0.0.0/8"}}`, "10.1.2.3", false, false},
		{`{"NotIpAddress":{"aws:SourceIp":"10.0.0.0/8"}}`, "", false, false},
		{`{"Bool":{"aws:SecureTransport":"true"}}`, "", true, true},
		{`{"Bool":{"aws:SecureTransport":"TRUE"}}`, "", true, true},
		{`{"Bool":{"aws:SecureTransport":"true"}}`, "", false, false},
		{`{"Bool":{"aws:SecureTransport":"false"}}`, "", false, true},
		{`{"StringEquals":{"aws:SourceIp":"10.1.2.3"}}`, "10.1.2.3", false, true},
		{`{"StringEquals":{"aws:SourceIp":"10.1.2.3"}}`, "10.1.2.4", false, false},
		{`{"StringNotEquals":{"aws:SourceIp":["10.1.2.3","10.1.2.4"]}}`, "10.1.2.4", false, false},
		{`{"StringNotEquals":{"aws:SourceIp":["10.1.2.3","10.1.2.4"]}}`, "10.1.2.5", false, true},
		{`{"StringEqualsIgnoreCase":{"aws:SecureTransport":"TRUE"}}`, "", true, true},
		{`{"StringEqualsIgnoreCase":{"aws:SecureTransport":"TRUE"}}`, "", false, false},
		{`{"StringNotEqualsIgnoreCase":{"aws:SecureTransport":"TRUE"}}`, "", true, false},
		{`{"StringNotEqualsIgnoreCase":{"aws:SecureTransport":"TRUE"}}`, "", false, true},
		{`{"StringLike":{"aws:SourceIp":"10.0.*"}}`, "10.0.7.7", false, true},
		{`{"StringLike":{"aws:SourceIp":["192.*","10.0.0.?"]}}`, "10.0.0.9", false, true},
		{`{"StringLike":{"aws:SourceIp":"10.0.0.?"}}`, "10.0.0.10", false, false},
		{`{"StringNotLike":{"aws:SourceIp":"10.*"}}`, "10.9.9.9", false, false},
		{`{"StringNotLike":{"aws:SourceIp":"10.*"}}`, "192.168.1.1", false, true},
		{`{"ArnLike":{"aws:SourceIp":"10.*"}}`, "10.2.2.2", false, true},
		{`{"ArnLike":{"aws:SourceIp":"10.*"}}`, "11.2.2.2", false, false},
		{`{"ArnNotLike":{"aws:SourceIp":"10.*"}}`, "10.2.2.2", false, false},
		{`{"ArnNotLike":{"aws:SourceIp":"10.*"}}`, "11.2.2.2", false, true},
		{`{"NumericEquals":{"aws:SourceIp":"10"}}`, "10", false, true},
		{`{"NumericEquals":{"aws:SourceIp":"10"}}`, "11", false, false},
		{`{"NumericNotEquals":{"aws:SourceIp":"10"}}`, "10", false, false},
		{`{"NumericNotEquals":{"aws:SourceIp":"10"}}`, "11", false, true},
		{`{"IpAddress":{"aws:SourceIp":"10.0.0.0/8"},"Bool":{"aws:SecureTransport":"true"}}`, "10.1.1.1", true, true},
		{`{"IpAddress":{"aws:SourceIp":"10.0.0.0/8"},"Bool":{"aws:SecureTransport":"true"}}`, "10.1.1.1", false, false},
		{`{"StringEquals":{"aws:SourceIp":"10.1.1.1","aws:SecureTransport":"true"}}`, "10.1.1.1", true, true},
		{`{"StringEquals":{"aws:SourceIp":"10.1.1.1","aws:SecureTransport":"true"}}`, "10.1.1.1", false, false},
	}
	for _, c := range cases {
		got := decide(t, allowIf(c.condition), PolicyEvaluationRequest{
			Action: "s3:GetObject", Resource: "arn:aws:s3:::b/k", SourceIP: c.ip, SecureTransport: c.tls,
		})
		assert.Equal(t, c.allowed, got == DecisionAllow, "%s with ip %q tls %v", c.condition, c.ip, c.tls)
	}
}

// '*' takes any run of characters, going back when what follows it fails, and
// '?' takes exactly one.
func TestWildcardsMatchAsAWSReadsThem(t *testing.T) {
	cases := []struct {
		pattern, text string
		matches       bool
	}{
		{"a*b*c", "axxbyyc", true},
		{"a*b*c", "axxbyy", false},
		{"*b", "aab", true},
		{"*b", "aba", false},
		{"a*a", "aba", true},
		{"a*a", "ab", false},
		{"*a*a", "aaa", true},
		{"a*", "a", true},
		{"a**", "abc", true},
		{"ab?d", "abcd", true},
		{"ab?d", "abd", false},
		{"?", "", false},
		{"", "a", false},
		{"a", "", false},
		{"x", "y", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.matches, wildcardMatch(c.pattern, c.text), "%q against %q", c.pattern, c.text)
	}
}
