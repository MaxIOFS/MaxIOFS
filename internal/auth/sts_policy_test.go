package auth

// Tests for STS session policies.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const readOnlyOneBucketPolicy = `{
  "Version": "2012-10-17",
  "Statement": [
    {"Effect": "Allow", "Action": ["s3:GetObject", "s3:ListBucket"],
     "Resource": ["arn:aws:s3:::backups", "arn:aws:s3:::backups/*"]}
  ]
}`

// --- Parsing / validation ---

func TestParseSessionPolicy_Valid(t *testing.T) {
	policy, err := ParseSessionPolicy(readOnlyOneBucketPolicy)
	require.NoError(t, err)
	require.NotNil(t, policy)
	assert.Len(t, policy.Statement, 1)
}

func TestParseSessionPolicy_EmptyMeansNoRestriction(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n\t"} {
		policy, err := ParseSessionPolicy(raw)
		require.NoError(t, err)
		assert.Nil(t, policy, "empty document must mean no policy, not an empty one")
	}
}

func TestParseSessionPolicy_Rejects(t *testing.T) {
	cases := map[string]string{
		"not JSON":          `{ nope`,
		"no statements":     `{"Version":"2012-10-17","Statement":[]}`,
		"bad effect":        `{"Statement":[{"Effect":"allow","Action":"s3:*","Resource":"*"}]}`,
		"missing action":    `{"Statement":[{"Effect":"Allow","Resource":"*"}]}`,
		"missing resource":  `{"Statement":[{"Effect":"Allow","Action":"s3:*"}]}`,
		"empty action list": `{"Statement":[{"Effect":"Allow","Action":[],"Resource":"*"}]}`,
		// A Principal would suggest the policy selects who it applies to; it
		// does not — it always applies to the session's own user.
		"principal": `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"*"},"Action":"s3:*","Resource":"*"}]}`,
		// Conditions are not evaluated yet, so accepting one would silently
		// widen a policy its author believed to be narrow.
		"condition": `{"Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"*",
			"Condition":{"IpAddress":{"aws:SourceIp":"10.0.0.0/8"}}}]}`,
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseSessionPolicy(raw)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrSTSInvalidPolicy)
		})
	}
}

func TestParseSessionPolicy_RejectsOversizedDocument(t *testing.T) {
	// A valid document padded past the size cap with a long resource name.
	huge := `{"Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"arn:aws:s3:::` +
		strings.Repeat("b", STSMaxSessionPolicyBytes) + `"}]}`

	_, err := ParseSessionPolicy(huge)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSTSInvalidPolicy)
}

// --- Evaluation ---

func TestEvaluateSessionPolicy(t *testing.T) {
	policy, err := ParseSessionPolicy(readOnlyOneBucketPolicy)
	require.NoError(t, err)

	cases := []struct {
		name     string
		action   string
		resource string
		want     bool
	}{
		{"listed object read", ActionGetObject, "arn:aws:s3:::backups/db.sql", true},
		{"bucket listing", ActionListBucket, "arn:aws:s3:::backups", true},
		{"write to the allowed bucket", ActionPutObject, "arn:aws:s3:::backups/db.sql", false},
		{"read from another bucket", ActionGetObject, "arn:aws:s3:::other/db.sql", false},
		{"bucket prefix is not a prefix match", ActionGetObject, "arn:aws:s3:::backups-2/db.sql", false},
		{"unmapped action denied", "", "arn:aws:s3:::backups/db.sql", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, EvaluateSessionPolicy(policy, tc.action, tc.resource))
		})
	}
}

func TestEvaluateSessionPolicy_NilPolicyAllowsEverything(t *testing.T) {
	// No policy attached: the base pipeline alone decides, so the evaluator
	// must not deny anything on its own.
	assert.True(t, EvaluateSessionPolicy(nil, ActionDeleteObject, "arn:aws:s3:::any/thing"))
}

func TestEvaluateSessionPolicy_ExplicitDenyBeatsAllow(t *testing.T) {
	policy, err := ParseSessionPolicy(`{"Statement":[
		{"Effect":"Allow","Action":"s3:*","Resource":"*"},
		{"Effect":"Deny","Action":"s3:DeleteObject","Resource":"arn:aws:s3:::vault/*"}
	]}`)
	require.NoError(t, err)

	assert.True(t, EvaluateSessionPolicy(policy, ActionGetObject, "arn:aws:s3:::vault/x"))
	assert.False(t, EvaluateSessionPolicy(policy, ActionDeleteObject, "arn:aws:s3:::vault/x"))
	assert.True(t, EvaluateSessionPolicy(policy, ActionDeleteObject, "arn:aws:s3:::scratch/x"))
}

func TestEvaluateSessionPolicy_Wildcards(t *testing.T) {
	policy, err := ParseSessionPolicy(`{"Statement":[
		{"Effect":"Allow","Action":"s3:Get*","Resource":"backups/2026/*"}
	]}`)
	require.NoError(t, err)

	// Bare resources are normalised to ARNs, so a policy written without the
	// arn prefix behaves the way its author expects.
	assert.True(t, EvaluateSessionPolicy(policy, ActionGetObject, "arn:aws:s3:::backups/2026/jan.tar"))
	assert.True(t, EvaluateSessionPolicy(policy, ActionGetObjectTagging, "arn:aws:s3:::backups/2026/jan.tar"))
	assert.False(t, EvaluateSessionPolicy(policy, ActionGetObject, "arn:aws:s3:::backups/2025/jan.tar"))
	assert.False(t, EvaluateSessionPolicy(policy, ActionPutObject, "arn:aws:s3:::backups/2026/jan.tar"))
}

// --- Action mapping ---

func TestS3ActionForRequest(t *testing.T) {
	cases := []struct {
		method, target string
		want           string
	}{
		{"GET", "/", ActionListAllMyBuckets},
		{"GET", "/bucket", ActionListBucket},
		{"HEAD", "/bucket", ActionListBucket},
		{"GET", "/bucket?versions=", ActionListBucketVersions},
		{"GET", "/bucket?uploads=", ActionListBucketMultipartUploads},
		{"PUT", "/bucket", ActionCreateBucket},
		{"DELETE", "/bucket", ActionDeleteBucket},
		{"POST", "/bucket?delete=", ActionDeleteObject},
		{"GET", "/bucket/key.txt", ActionGetObject},
		{"HEAD", "/bucket/key.txt", ActionGetObject},
		{"PUT", "/bucket/key.txt", ActionPutObject},
		{"PUT", "/bucket/key.txt?partNumber=1&uploadId=x", ActionPutObject},
		{"POST", "/bucket/key.txt?uploads=", ActionPutObject},
		{"POST", "/bucket/key.txt?uploadId=x", ActionPutObject},
		{"POST", "/bucket/key.txt?restore=", ActionRestoreObject},
		{"DELETE", "/bucket/key.txt", ActionDeleteObject},
		{"DELETE", "/bucket/key.txt?uploadId=x", ActionAbortMultipartUpload},
		{"GET", "/bucket/key.txt?tagging=", ActionGetObjectTagging},
		// Methods with no S3 meaning must report "unknown" so a restricting
		// caller can fail closed instead of guessing.
		{"PATCH", "/bucket/key.txt", ""},
		{"OPTIONS", "/bucket", ""},
		{"PUT", "/", ""},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.target, func(t *testing.T) {
			r, err := http.NewRequest(tc.method, tc.target, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.want, S3ActionForRequest(r))
		})
	}
}

func TestGetS3ActionKeepsPermissiveFallback(t *testing.T) {
	// The legacy helper must keep its old behaviour: callers that only map
	// actions for logging or future use still get the restrictive default.
	helper := &S3AuthHelper{}
	r, _ := http.NewRequest("PATCH", "/bucket/key.txt", nil)
	assert.Equal(t, ActionGetObject, helper.GetS3Action(r))
}

func TestResourceARNForRequest(t *testing.T) {
	cases := map[string]string{
		"/":                    "arn:aws:s3:::*",
		"/bucket":              "arn:aws:s3:::bucket",
		"/bucket/key.txt":      "arn:aws:s3:::bucket/key.txt",
		"/bucket/deep/key.txt": "arn:aws:s3:::bucket/deep/key.txt",
	}
	for target, want := range cases {
		r, _ := http.NewRequest("GET", target, nil)
		assert.Equal(t, want, ResourceARNForRequest(r))
	}
}

// --- End-to-end through signature validation ---

func TestSTSSessionPolicy_RestrictsSignedRequests(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()

	sess, err := am.IssueSTSSession(context.Background(), user.ID, 0, readOnlyOneBucketPolicy)
	require.NoError(t, err)

	newRequest := func(method, target string) *http.Request {
		r, _ := http.NewRequest(method, target, nil)
		r.Host = "s3.example.com"
		signRequestV4(am, r, sess.TempAccessKeyID, sess.SecretAccessKey, sess.SessionToken, true)
		return r
	}

	t.Run("allowed read passes", func(t *testing.T) {
		got, err := am.ValidateS3SignatureV4(context.Background(), newRequest("GET", "/backups/db.sql"))
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, user.ID, got.ID, "the session must project the base user unchanged")
	})

	t.Run("write to the same bucket is denied", func(t *testing.T) {
		_, err := am.ValidateS3SignatureV4(context.Background(), newRequest("PUT", "/backups/db.sql"))
		assert.ErrorIs(t, err, ErrAccessDenied)
	})

	t.Run("read from another bucket is denied", func(t *testing.T) {
		_, err := am.ValidateS3SignatureV4(context.Background(), newRequest("GET", "/other/db.sql"))
		assert.ErrorIs(t, err, ErrAccessDenied)
	})
}

func TestSTSSessionWithoutPolicyIsUnrestricted(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()

	sess, err := am.IssueSTSSession(context.Background(), user.ID, 0, "")
	require.NoError(t, err)

	// Any operation the base user could perform must still reach the
	// authorization pipeline — a policy-less session restricts nothing.
	r, _ := http.NewRequest("DELETE", "/anything/at-all.txt", nil)
	r.Host = "s3.example.com"
	signRequestV4(am, r, sess.TempAccessKeyID, sess.SecretAccessKey, sess.SessionToken, true)

	got, err := am.ValidateS3SignatureV4(context.Background(), r)
	require.NoError(t, err)
	assert.Equal(t, user.ID, got.ID)
}

func TestIssueSTSSession_RejectsUnenforceablePolicy(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()

	_, err := am.IssueSTSSession(context.Background(), user.ID, 0, `{"Statement":[{"Effect":"Allow"}]}`)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSTSInvalidPolicy)
}

func TestSTSSessionPolicy_UnparsablePolicyDeniesEverything(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()

	sess, err := am.IssueSTSSession(context.Background(), user.ID, 0, readOnlyOneBucketPolicy)
	require.NoError(t, err)

	// Simulate a corrupted row (or one written by a build that understood a
	// construct this one does not): the credential must stop working rather
	// than silently widen to the base user's full permissions.
	_, err = am.store.db.Exec(
		`UPDATE sts_sessions SET session_policy = ? WHERE temp_access_key_id = ?`,
		`{"Statement": [ truncated`, sess.TempAccessKeyID)
	require.NoError(t, err)

	r, _ := http.NewRequest("GET", "/backups/db.sql", nil)
	r.Host = "s3.example.com"
	signRequestV4(am, r, sess.TempAccessKeyID, sess.SecretAccessKey, sess.SessionToken, true)

	_, err = am.ValidateS3SignatureV4(context.Background(), r)
	assert.ErrorIs(t, err, ErrAccessDenied)
}

func TestAuthorizeSTSRequest_PresignedPath(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	sess, err := am.IssueSTSSession(ctx, user.ID, 0, readOnlyOneBucketPolicy)
	require.NoError(t, err)

	allowed, _ := http.NewRequest("GET", "/backups/db.sql", nil)
	got, err := am.AuthorizeSTSRequest(ctx, sess.TempAccessKeyID, sess.SessionToken, allowed)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, user.ID, got.ID)

	denied, _ := http.NewRequest("PUT", "/backups/db.sql", nil)
	_, err = am.AuthorizeSTSRequest(ctx, sess.TempAccessKeyID, sess.SessionToken, denied)
	assert.ErrorIs(t, err, ErrAccessDenied)

	// A wrong token must not authorize regardless of the policy.
	_, err = am.AuthorizeSTSRequest(ctx, sess.TempAccessKeyID, "not-the-token", allowed)
	assert.ErrorIs(t, err, ErrInvalidSignature)

	// A permanent key is not this function's business: it reports "nothing to
	// enforce" so callers can invoke it unconditionally.
	got, err = am.AuthorizeSTSRequest(ctx, "AKIAPERMANENTKEY12345", "", allowed)
	require.NoError(t, err)
	assert.Nil(t, got)
}
