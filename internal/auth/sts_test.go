package auth

// Tests for STS temporary credentials.

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSTSTest returns a manager with one active user ready to issue sessions.
func setupSTSTest(t *testing.T) (*authManager, *User, func()) {
	t.Helper()

	managerInterface, tmpDir := setupTestAuthManager(t)
	am := managerInterface.(*authManager)

	user := &User{
		ID:       "user-sts-test",
		Username: "sts-tester",
		Status:   UserStatusActive,
		Roles:    []string{RoleUser},
	}
	require.NoError(t, am.CreateUser(context.Background(), user))

	return am, user, func() { cleanupTestAuthManager(t, tmpDir) }
}

// signRequestV4 signs r exactly as a client SDK would, using the manager's own
// canonicalisation. sessionToken is sent when non-empty; signToken controls
// whether it is listed in SignedHeaders (and therefore covered by the signature).
func signRequestV4(am *authManager, r *http.Request, keyID, secret, sessionToken string, signToken bool) {
	amzDate := time.Now().UTC().Format("20060102T150405Z")
	dateStamp := amzDate[:8]
	region, service := "us-east-1", "s3"

	r.Header.Set("X-Amz-Date", amzDate)
	if sessionToken != "" {
		r.Header.Set("X-Amz-Security-Token", sessionToken)
	}

	signedHeaders := "host;x-amz-date"
	if signToken {
		signedHeaders = "host;x-amz-date;x-amz-security-token"
	}

	canonical := am.createCanonicalRequest(r, signedHeaders)
	canonicalHash := fmt.Sprintf("%x", sha256.Sum256([]byte(canonical)))
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s/%s/%s/aws4_request\n%s",
		amzDate, dateStamp, region, service, canonicalHash)
	signature := am.calculateSignatureV4(stringToSign, secret, dateStamp, region, service)

	r.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s/%s/%s/aws4_request, SignedHeaders=%s, Signature=%s",
		keyID, dateStamp, region, service, signedHeaders, signature))
}

func newSignedRequest(am *authManager, keyID, secret, token string, signToken bool) *http.Request {
	r, _ := http.NewRequest("GET", "/test-bucket/object.txt", nil)
	r.Host = "s3.example.com"
	signRequestV4(am, r, keyID, secret, token, signToken)
	return r
}

// --- Issuance ---

func TestIssueSTSSession_ReturnsUsableCredentials(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()

	before := time.Now().Unix()
	sess, err := am.IssueSTSSession(context.Background(), user.ID, 0, "")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(sess.TempAccessKeyID, STSKeyPrefix),
		"temp key must carry the ASIA prefix, got %q", sess.TempAccessKeyID)
	assert.Len(t, sess.TempAccessKeyID, 20, "temp key must be AWS-length (20 chars)")
	assert.NotEmpty(t, sess.SecretAccessKey)
	assert.NotEmpty(t, sess.SessionToken)
	assert.GreaterOrEqual(t, sess.ExpiresAt, before+STSDefaultSessionDuration)

	// The stored secret must be encrypted, never the plaintext we returned.
	stored, err := am.store.GetSTSSession(sess.TempAccessKeyID)
	require.NoError(t, err)
	assert.NotEqual(t, sess.SecretAccessKey, stored.SecretAccessKey,
		"secret must be encrypted at rest")
	assert.Empty(t, stored.SessionPolicy, "a session issued without a policy stores none")
}

func TestIssueSTSSession_DurationBounds(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := am.IssueSTSSession(ctx, user.ID, STSMinSessionDuration-1, "")
	assert.ErrorIs(t, err, ErrSTSInvalidDuration, "below minimum must be rejected")

	_, err = am.IssueSTSSession(ctx, user.ID, STSDefaultMaxSessionDuration+1, "")
	assert.ErrorIs(t, err, ErrSTSInvalidDuration, "above maximum must be rejected")

	sess, err := am.IssueSTSSession(ctx, user.ID, STSMinSessionDuration, "")
	require.NoError(t, err, "the minimum itself must be accepted")
	assert.Positive(t, sess.ExpiresAt)
}

func TestIssueSTSSession_RejectsInactiveUser(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	user.Status = UserStatusSuspended
	require.NoError(t, am.UpdateUser(ctx, user))

	_, err := am.IssueSTSSession(ctx, user.ID, 0, "")
	assert.ErrorIs(t, err, ErrUserInactive)
}

func TestIssueSTSSession_PerUserCap(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().Unix()
	for i := 0; i < STSMaxSessionsPerUser; i++ {
		require.NoError(t, am.store.CreateSTSSession(&STSSession{
			TempAccessKeyID: fmt.Sprintf("ASIAFILLER%010d", i),
			SecretAccessKey: "enc:filler",
			SessionToken:    "filler",
			UserID:          user.ID,
			CreatedAt:       now,
			ExpiresAt:       now + 3600,
		}))
	}

	_, err := am.IssueSTSSession(ctx, user.ID, 0, "")
	assert.ErrorIs(t, err, ErrSTSTooManySessions)
}

// --- Signature validation ---

func TestSTSSession_SignatureRoundTrip(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	sess, err := am.IssueSTSSession(ctx, user.ID, 0, "")
	require.NoError(t, err)

	req := newSignedRequest(am, sess.TempAccessKeyID, sess.SecretAccessKey, sess.SessionToken, true)
	got, err := am.ValidateS3SignatureV4(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, user.ID, got.ID, "validation must yield the base user")
}

func TestSTSSession_RejectsTokenTampering(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	sess, err := am.IssueSTSSession(ctx, user.ID, 0, "")
	require.NoError(t, err)

	t.Run("token missing entirely", func(t *testing.T) {
		req := newSignedRequest(am, sess.TempAccessKeyID, sess.SecretAccessKey, "", true)
		_, err := am.ValidateS3SignatureV4(ctx, req)
		assert.Error(t, err)
	})

	t.Run("token present but wrong", func(t *testing.T) {
		req := newSignedRequest(am, sess.TempAccessKeyID, sess.SecretAccessKey, "not-the-token", true)
		_, err := am.ValidateS3SignatureV4(ctx, req)
		assert.ErrorIs(t, err, ErrInvalidSignature)
	})

	t.Run("token sent but not covered by the signature", func(t *testing.T) {
		// A correct token that is not listed in SignedHeaders must still fail:
		// otherwise it could be stripped or swapped in transit.
		req := newSignedRequest(am, sess.TempAccessKeyID, sess.SecretAccessKey, sess.SessionToken, false)
		_, err := am.ValidateS3SignatureV4(ctx, req)
		assert.ErrorIs(t, err, ErrInvalidSignature)
	})

	t.Run("wrong secret", func(t *testing.T) {
		req := newSignedRequest(am, sess.TempAccessKeyID, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", sess.SessionToken, true)
		_, err := am.ValidateS3SignatureV4(ctx, req)
		assert.ErrorIs(t, err, ErrInvalidSignature)
	})
}

func TestSTSSession_UnknownTempKeyRejected(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()

	req := newSignedRequest(am, "ASIADOESNOTEXIST0000", "secret", "token", true)
	_, err := am.ValidateS3SignatureV4(context.Background(), req)
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestSTSSession_ExpiredIsRejectedAndDeleted(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	sess, err := am.IssueSTSSession(ctx, user.ID, 0, "")
	require.NoError(t, err)

	// Force expiry in the past without waiting.
	_, err = am.store.db.Exec(`UPDATE sts_sessions SET expires_at = ? WHERE temp_access_key_id = ?`,
		time.Now().Unix()-1, sess.TempAccessKeyID)
	require.NoError(t, err)

	req := newSignedRequest(am, sess.TempAccessKeyID, sess.SecretAccessKey, sess.SessionToken, true)
	_, err = am.ValidateS3SignatureV4(ctx, req)
	assert.ErrorIs(t, err, ErrSTSSessionExpired)

	// Expired-on-touch cleanup: the row is gone without waiting for the sweep.
	_, err = am.store.GetSTSSession(sess.TempAccessKeyID)
	assert.ErrorIs(t, err, ErrSTSSessionNotFound)
}

func TestSTSSession_RevokedIsRejected(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	sess, err := am.IssueSTSSession(ctx, user.ID, 0, "")
	require.NoError(t, err)
	require.NoError(t, am.RevokeSTSSession(ctx, sess.TempAccessKeyID, user.ID, false))

	req := newSignedRequest(am, sess.TempAccessKeyID, sess.SecretAccessKey, sess.SessionToken, true)
	_, err = am.ValidateS3SignatureV4(ctx, req)
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestSTSSession_DeactivatedUserIsRejectedImmediately(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	sess, err := am.IssueSTSSession(ctx, user.ID, 0, "")
	require.NoError(t, err)

	// Suspending the user must kill live sessions on the next request, with no
	// revocation step and no cluster propagation needed.
	user.Status = UserStatusSuspended
	require.NoError(t, am.UpdateUser(ctx, user))

	req := newSignedRequest(am, sess.TempAccessKeyID, sess.SecretAccessKey, sess.SessionToken, true)
	_, err = am.ValidateS3SignatureV4(ctx, req)
	assert.ErrorIs(t, err, ErrAccessDenied)
}

func TestSTSSession_RejectedOverSigV2(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	sess, err := am.IssueSTSSession(ctx, user.ID, 0, "")
	require.NoError(t, err)

	req, _ := http.NewRequest("GET", "/test-bucket/object.txt", nil)
	req.Host = "s3.example.com"
	req.Header.Set("Authorization", "AWS "+sess.TempAccessKeyID+":anysignature")

	_, err = am.ValidateS3SignatureV2(ctx, req)
	assert.ErrorIs(t, err, ErrInvalidCredentials, "STS credentials are SigV4-only")
}

// --- Permanent-key regression ---

func TestPermanentKeyPathUnaffectedBySTS(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	key, err := am.GenerateAccessKey(ctx, user.ID)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(key.AccessKeyID, "AKIA"))

	// No session token anywhere: the permanent path must behave as before.
	req := newSignedRequest(am, key.AccessKeyID, key.SecretAccessKey, "", false)
	got, err := am.ValidateS3SignatureV4(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, user.ID, got.ID)
}

// --- Revocation, listing and sweep ---

func TestRevokeSTSSession_OwnershipEnforced(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	sess, err := am.IssueSTSSession(ctx, user.ID, 0, "")
	require.NoError(t, err)

	err = am.RevokeSTSSession(ctx, sess.TempAccessKeyID, "someone-else", false)
	assert.ErrorIs(t, err, ErrAccessDenied, "a non-admin cannot revoke another user's session")

	// The session must survive the rejected attempt.
	_, err = am.store.GetSTSSession(sess.TempAccessKeyID)
	require.NoError(t, err)

	require.NoError(t, am.RevokeSTSSession(ctx, sess.TempAccessKeyID, "someone-else", true),
		"an admin may revoke any session")
}

func TestListSTSSessions_HidesCredentialMaterial(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	_, err := am.IssueSTSSession(ctx, user.ID, 0, "")
	require.NoError(t, err)

	sessions, err := am.ListSTSSessions(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Empty(t, sessions[0].SecretAccessKey, "listings must never expose the secret")
	assert.Empty(t, sessions[0].SessionToken, "listings must never expose the token")
	assert.NotEmpty(t, sessions[0].TempAccessKeyID)
}

func TestSweepExpiredSTSSessions_OnlyRemovesLongExpired(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().Unix()
	seed := func(id string, expiresAt int64) {
		require.NoError(t, am.store.CreateSTSSession(&STSSession{
			TempAccessKeyID: id,
			SecretAccessKey: "enc:x",
			SessionToken:    "t",
			UserID:          user.ID,
			CreatedAt:       now - 100000,
			ExpiresAt:       expiresAt,
		}))
	}
	seed("ASIALONGEXPIRED00000", now-7200) // expired 2 h ago → swept
	seed("ASIAJUSTEXPIRED00000", now-60)   // expired 1 min ago → kept (grace)
	seed("ASIASTILLVALID000000", now+3600) // active → kept

	removed, err := am.SweepExpiredSTSSessions(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)

	_, err = am.store.GetSTSSession("ASIALONGEXPIRED00000")
	assert.ErrorIs(t, err, ErrSTSSessionNotFound)
	_, err = am.store.GetSTSSession("ASIAJUSTEXPIRED00000")
	assert.NoError(t, err, "recently expired sessions stay visible in the console for an hour")
	_, err = am.store.GetSTSSession("ASIASTILLVALID000000")
	assert.NoError(t, err)
}

// --- Presigned resolution path ---

func TestResolveSTSSessionSecret(t *testing.T) {
	am, user, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()

	sess, err := am.IssueSTSSession(ctx, user.ID, 0, "")
	require.NoError(t, err)

	gotUser, secret, err := am.ResolveSTSSessionSecret(ctx, sess.TempAccessKeyID, sess.SessionToken)
	require.NoError(t, err)
	assert.Equal(t, user.ID, gotUser.ID)
	assert.Equal(t, sess.SecretAccessKey, secret, "must return the decrypted signing secret")

	_, _, err = am.ResolveSTSSessionSecret(ctx, sess.TempAccessKeyID, "wrong-token")
	assert.ErrorIs(t, err, ErrInvalidSignature)
}

func TestIsSTSAccessKey(t *testing.T) {
	assert.True(t, IsSTSAccessKey("ASIAABCDEFGHIJKLMNOP"))
	assert.False(t, IsSTSAccessKey("AKIAABCDEFGHIJKLMNOP"))
	assert.False(t, IsSTSAccessKey(""))
}
