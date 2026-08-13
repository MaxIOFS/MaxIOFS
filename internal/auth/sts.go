package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/audit"
)

const (
	STSKeyPrefix = "ASIA"

	// STSMinSessionDuration is the minimum session lifetime in seconds.
	STSMinSessionDuration = 900
	// STSDefaultSessionDuration is used when the caller omits a duration.
	STSDefaultSessionDuration = 3600
	// STSDefaultMaxSessionDuration caps the requested lifetime unless the
	// security.sts_max_session_duration setting overrides it.
	STSDefaultMaxSessionDuration = 43200

	// STSMaxSessionsPerUser bounds active sessions per user (protects the
	// table from a looping script; no legitimate use case comes close).
	STSMaxSessionsPerUser = 100

	stsMaxDurationSettingKey = "security.sts_max_session_duration"
)

// STS errors
var (
	ErrSTSSessionNotFound = errors.New("sts session not found")
	ErrSTSSessionExpired  = errors.New("sts session expired")
	ErrSTSTooManySessions = errors.New("too many active sts sessions")
	ErrSTSInvalidDuration = errors.New("invalid sts session duration")
)

// STSSession represents a temporary credential set bound to an existing user.
type STSSession struct {
	TempAccessKeyID string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	SessionToken    string `json:"session_token,omitempty"`
	UserID          string `json:"user_id"`
	SessionPolicy   string `json:"session_policy,omitempty"`

	RoleARN         string `json:"role_arn,omitempty"`
	RoleSessionName string `json:"role_session_name,omitempty"`
	PolicyMode      string `json:"policy_mode,omitempty"`

	CreatedAt int64 `json:"created_at"`
	ExpiresAt int64 `json:"expires_at"`
}

// Session policy modes.
const (
	// STSPolicyModeRestrict — the session projects the base user and any
	// session policy only takes permissions away.
	STSPolicyModeRestrict = "restrict"
	// STSPolicyModeRole — the session acts as a role: the role's policies grant
	// the permissions and a session policy narrows them further.
	STSPolicyModeRole = "role"
)

// --- SQLiteStore methods ---

// CreateSTSSession inserts a session row. SecretAccessKey must already be encrypted.
func (s *SQLiteStore) CreateSTSSession(session *STSSession) error {
	mode := session.PolicyMode
	if mode == "" {
		mode = STSPolicyModeRestrict
	}
	_, err := s.db.Exec(`
		INSERT INTO sts_sessions (temp_access_key_id, secret_access_key, session_token, user_id, session_policy,
			role_arn, role_session_name, policy_mode, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, session.TempAccessKeyID, session.SecretAccessKey, session.SessionToken,
		session.UserID, nullString(session.SessionPolicy),
		nullString(session.RoleARN), nullString(session.RoleSessionName), mode,
		session.CreatedAt, session.ExpiresAt)
	if err != nil {
		return fmt.Errorf("failed to create sts session: %w", err)
	}
	return nil
}

// GetSTSSession returns the full session row (encrypted secret included) for
// signature validation. Expiry is NOT checked here — callers decide.
func (s *SQLiteStore) GetSTSSession(tempAccessKeyID string) (*STSSession, error) {
	var sess STSSession
	var policy, roleARN, roleSessionName sql.NullString

	err := s.db.QueryRow(`
		SELECT temp_access_key_id, secret_access_key, session_token, user_id, session_policy,
		       role_arn, role_session_name, policy_mode, created_at, expires_at
		FROM sts_sessions
		WHERE temp_access_key_id = ?
	`, tempAccessKeyID).Scan(
		&sess.TempAccessKeyID, &sess.SecretAccessKey, &sess.SessionToken,
		&sess.UserID, &policy, &roleARN, &roleSessionName, &sess.PolicyMode,
		&sess.CreatedAt, &sess.ExpiresAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrSTSSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	if policy.Valid {
		sess.SessionPolicy = policy.String
	}
	if roleARN.Valid {
		sess.RoleARN = roleARN.String
	}
	if roleSessionName.Valid {
		sess.RoleSessionName = roleSessionName.String
	}
	return &sess, nil
}

// ListSTSSessionsByUser returns a user's sessions with secrets and tokens
// scrubbed — listings never expose credential material. The session policy is
// included: it is a restriction the caller wrote, not a secret.
func (s *SQLiteStore) ListSTSSessionsByUser(userID string) ([]*STSSession, error) {
	rows, err := s.db.Query(`
		SELECT temp_access_key_id, user_id, session_policy, role_arn, role_session_name, policy_mode, created_at, expires_at
		FROM sts_sessions
		WHERE user_id = ?
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSTSSessionList(rows)
}

// ListAllSTSSessions returns every session (admin view), scrubbed.
func (s *SQLiteStore) ListAllSTSSessions() ([]*STSSession, error) {
	rows, err := s.db.Query(`
		SELECT temp_access_key_id, user_id, session_policy, role_arn, role_session_name, policy_mode, created_at, expires_at
		FROM sts_sessions
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSTSSessionList(rows)
}

func scanSTSSessionList(rows *sql.Rows) ([]*STSSession, error) {
	var sessions []*STSSession
	for rows.Next() {
		var sess STSSession
		var policy, roleARN, roleSessionName sql.NullString
		if err := rows.Scan(&sess.TempAccessKeyID, &sess.UserID, &policy,
			&roleARN, &roleSessionName, &sess.PolicyMode, &sess.CreatedAt, &sess.ExpiresAt); err != nil {
			return nil, err
		}
		if policy.Valid {
			sess.SessionPolicy = policy.String
		}
		if roleARN.Valid {
			sess.RoleARN = roleARN.String
		}
		if roleSessionName.Valid {
			sess.RoleSessionName = roleSessionName.String
		}
		sessions = append(sessions, &sess)
	}
	return sessions, rows.Err()
}

// DeleteSTSSession removes a session row (revocation, or expired-on-touch).
func (s *SQLiteStore) DeleteSTSSession(tempAccessKeyID string) error {
	_, err := s.db.Exec(`DELETE FROM sts_sessions WHERE temp_access_key_id = ?`, tempAccessKeyID)
	if err != nil {
		return fmt.Errorf("failed to delete sts session: %w", err)
	}
	return nil
}

// DeleteExpiredSTSSessions removes sessions whose expiry is older than the
func (s *SQLiteStore) DeleteExpiredSTSSessions(cutoff int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM sts_sessions WHERE expires_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to sweep sts sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// CountActiveSTSSessionsByUser counts non-expired sessions for the per-user cap.
func (s *SQLiteStore) CountActiveSTSSessionsByUser(userID string, now int64) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM sts_sessions WHERE user_id = ? AND expires_at > ?`,
		userID, now,
	).Scan(&n)
	return n, err
}

// --- authManager methods ---

// generateSTSAccessKeyID generates a temporary access key ID: ASIA + 16
// uppercase alphanumeric characters (20 chars, AWS-compatible format).
func (am *authManager) generateSTSAccessKeyID() (string, error) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const randomLength = 16

	bytes := make([]byte, randomLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	result := make([]byte, randomLength)
	for i := 0; i < randomLength; i++ {
		result[i] = charset[int(bytes[i])%len(charset)]
	}
	return STSKeyPrefix + string(result), nil
}

// generateSessionToken generates an opaque session token (base64url, 32 random
// bytes). All semantics live in the DB row — SDKs never parse tokens.
func generateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// stsMaxSessionDuration returns the configured cap in seconds.
func (am *authManager) stsMaxSessionDuration() int {
	if am.settingsManager != nil {
		if v, err := am.settingsManager.GetInt(stsMaxDurationSettingKey); err == nil && v >= STSMinSessionDuration {
			return v
		}
	}
	return STSDefaultMaxSessionDuration
}

// IssueSTSSession creates a temporary credential set for userID.
func (am *authManager) IssueSTSSession(ctx context.Context, userID string, durationSeconds int, sessionPolicy string) (*STSSession, error) {
	user, err := am.store.GetUserByID(userID)
	if err != nil {
		return nil, ErrUserNotFound
	}
	if user.Status != UserStatusActive {
		return nil, ErrUserInactive
	}

	if durationSeconds == 0 {
		durationSeconds = STSDefaultSessionDuration
	}
	if durationSeconds < STSMinSessionDuration || durationSeconds > am.stsMaxSessionDuration() {
		return nil, ErrSTSInvalidDuration
	}

	// Reject a policy we cannot enforce exactly as written, while the caller
	// can still fix it — never at request time as an unexplained denial.
	sessionPolicy = strings.TrimSpace(sessionPolicy)
	if _, err := ParseSessionPolicy(sessionPolicy); err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	active, err := am.store.CountActiveSTSSessionsByUser(userID, now)
	if err != nil {
		return nil, fmt.Errorf("failed to count sts sessions: %w", err)
	}
	if active >= STSMaxSessionsPerUser {
		return nil, ErrSTSTooManySessions
	}

	tempKeyID, err := am.generateSTSAccessKeyID()
	if err != nil {
		return nil, err
	}
	secret, err := am.generateSecretAccessKey()
	if err != nil {
		return nil, err
	}
	token, err := generateSessionToken()
	if err != nil {
		return nil, err
	}

	encryptedSecret, err := am.encryptSecret(secret)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt sts session secret: %w", err)
	}

	stored := &STSSession{
		TempAccessKeyID: tempKeyID,
		SecretAccessKey: encryptedSecret,
		SessionToken:    token,
		UserID:          userID,
		SessionPolicy:   sessionPolicy,
		CreatedAt:       now,
		ExpiresAt:       now + int64(durationSeconds),
	}
	if err := am.store.CreateSTSSession(stored); err != nil {
		return nil, err
	}

	am.logAuditEvent(ctx, &audit.AuditEvent{
		TenantID:     user.TenantID,
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    audit.EventTypeSTSSessionIssued,
		ResourceType: audit.ResourceTypeSTSSession,
		ResourceID:   tempKeyID,
		ResourceName: tempKeyID,
		Action:       audit.ActionCreate,
		Status:       audit.StatusSuccess,
		Details: map[string]interface{}{
			"duration_seconds":   durationSeconds,
			"expires_at":         stored.ExpiresAt,
			"has_session_policy": sessionPolicy != "",
		},
	})

	// Return the plaintext credential set — shown once, like permanent keys.
	return &STSSession{
		TempAccessKeyID: tempKeyID,
		SecretAccessKey: secret,
		SessionToken:    token,
		UserID:          userID,
		CreatedAt:       now,
		ExpiresAt:       stored.ExpiresAt,
	}, nil
}

// RevokeSTSSession deletes a session. Non-admin callers can only revoke their
// own sessions; the ownership check happens here so every handler gets it.
func (am *authManager) RevokeSTSSession(ctx context.Context, tempAccessKeyID, requestingUserID string, isAdmin bool) error {
	sess, err := am.store.GetSTSSession(tempAccessKeyID)
	if err != nil {
		return err
	}
	if !isAdmin && sess.UserID != requestingUserID {
		return ErrAccessDenied
	}
	if err := am.store.DeleteSTSSession(tempAccessKeyID); err != nil {
		return err
	}

	am.logAuditEvent(ctx, &audit.AuditEvent{
		UserID:       requestingUserID,
		EventType:    audit.EventTypeSTSSessionRevoked,
		ResourceType: audit.ResourceTypeSTSSession,
		ResourceID:   tempAccessKeyID,
		ResourceName: tempAccessKeyID,
		Action:       audit.ActionDelete,
		Status:       audit.StatusSuccess,
		Details: map[string]interface{}{
			"session_user_id": sess.UserID,
		},
	})
	return nil
}

// ListSTSSessions returns a user's sessions (scrubbed — no credential material).
func (am *authManager) ListSTSSessions(ctx context.Context, userID string) ([]*STSSession, error) {
	return am.store.ListSTSSessionsByUser(userID)
}

// ListAllSTSSessions returns every session (admin view, scrubbed).
func (am *authManager) ListAllSTSSessions(ctx context.Context) ([]*STSSession, error) {
	return am.store.ListAllSTSSessions()
}

// SweepExpiredSTSSessions removes sessions expired for more than an hour (the
// grace keeps a just-expired session visible in the console). Called from the
// server's background loop.
func (am *authManager) SweepExpiredSTSSessions(ctx context.Context) (int64, error) {
	return am.store.DeleteExpiredSTSSessions(time.Now().Unix() - 3600)
}

// ResolveSTSSessionSecret authenticates a temporary credential and returns the
func (am *authManager) ResolveSTSSessionSecret(ctx context.Context, tempAccessKeyID, sessionToken string) (*User, string, error) {
	user, secret, _, err := am.resolveSTSSession(tempAccessKeyID, sessionToken)
	return user, secret, err
}

// AuthorizeSTSRequest re-checks a temporary credential after its signature has
func (am *authManager) AuthorizeSTSRequest(ctx context.Context, tempAccessKeyID, sessionToken string, r *http.Request) (*User, error) {
	if !IsSTSAccessKey(tempAccessKeyID) {
		return nil, nil
	}

	user, _, sess, err := am.resolveSTSSession(tempAccessKeyID, sessionToken)
	if err != nil {
		return nil, err
	}
	if err := am.enforceSTSSession(sess, r); err != nil {
		return nil, err
	}
	am.attachRolePolicySetToRequest(r, sess)
	return roleSessionUser(user, sess), nil
}

// IsSTSAccessKey reports whether an access key ID denotes a temporary credential.
func IsSTSAccessKey(accessKeyID string) bool {
	return strings.HasPrefix(accessKeyID, STSKeyPrefix)
}

// validateSTSSignatureV4 completes SigV4 validation for a temporary credential.
// Called from ValidateS3SignatureV4 once the ASIA prefix is detected.
func (am *authManager) validateSTSSignatureV4(r *http.Request, sig *S3SignatureV4) (*User, error) {
	// The session token must be covered by the signature; otherwise it could be
	// stripped or swapped without invalidating the request.
	if !signedHeadersInclude(sig.SignedHeaders, "x-amz-security-token") {
		return nil, ErrInvalidSignature
	}

	user, secret, sess, err := am.resolveSTSSession(sig.AccessKey, sig.SessionToken)
	if err != nil {
		return nil, err
	}

	if !am.verifyS3SignatureV4(r, sig, secret) {
		return nil, ErrInvalidSignature
	}

	if err := am.enforceSTSSession(sess, r); err != nil {
		return nil, err
	}

	am.attachRolePolicySetToRequest(r, sess)
	return roleSessionUser(user, sess), nil
}

func (am *authManager) attachRolePolicySetToRequest(r *http.Request, sess *STSSession) {
	if r == nil || sess == nil || sess.PolicyMode != STSPolicyModeRole {
		return
	}
	set, err := am.rolePolicySetForSession(sess)
	if err != nil {
		return
	}
	*r = *r.WithContext(WithPolicySet(r.Context(), set))
}

// signedHeadersInclude reports whether name appears in a SigV4 SignedHeaders
// list (semicolon-separated, lowercase by AWS convention).
func signedHeadersInclude(signedHeaders, name string) bool {
	for _, h := range strings.Split(signedHeaders, ";") {
		if strings.EqualFold(strings.TrimSpace(h), name) {
			return true
		}
	}
	return false
}

// resolveSTSSession authenticates a temporary credential during signature
func (am *authManager) resolveSTSSession(tempAccessKeyID, presentedToken string) (*User, string, *STSSession, error) {
	sess, err := am.store.GetSTSSession(tempAccessKeyID)
	if err != nil {
		return nil, "", nil, ErrInvalidCredentials
	}

	if time.Now().Unix() >= sess.ExpiresAt {
		// Expired-on-touch cleanup; the sweep is only a fallback.
		_ = am.store.DeleteSTSSession(tempAccessKeyID)
		return nil, "", nil, ErrSTSSessionExpired
	}

	if presentedToken == "" ||
		subtle.ConstantTimeCompare([]byte(presentedToken), []byte(sess.SessionToken)) != 1 {
		return nil, "", nil, ErrInvalidSignature
	}

	plainSecret, err := am.decryptSecret(sess.SecretAccessKey)
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to decrypt sts session secret: %w", err)
	}

	user, err := am.store.GetUserByID(sess.UserID)
	if err != nil {
		return nil, "", nil, ErrUserNotFound
	}
	if user.Status != UserStatusActive {
		return nil, "", nil, ErrAccessDenied
	}

	return user, plainSecret, sess, nil
}
