package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Methods a console session was signed in with.
const (
	SessionMethodPassword = "password"
	SessionMethodLDAP     = "ldap"
	SessionMethodOAuth    = "oauth"
)

// ConsoleSession is a sign-in to the console, kept so it can be ended before
// its tokens expire and, for an identity provider's user, renewed against the
// provider. IdPRefreshToken is stored as the caller gives it: encrypted.
type ConsoleSession struct {
	ID              string
	UserID          string
	Method          string
	ProviderID      string
	IdPSubject      string
	IdPSession      string
	IdPRefreshToken string
	CreatedAt       int64
	RefreshedAt     int64
	ExpiresAt       int64
	RevokedAt       int64
	RevokedReason   string
	// UpdatedAt is when it last changed, in nanoseconds: between nodes the
	// latest change wins, and a renewal follows its sign-in within a second.
	UpdatedAt int64
}

const consoleSessionColumns = `id, user_id, method, provider_id, idp_subject, idp_session, idp_refresh_token,
	created_at, refreshed_at, expires_at, revoked_at, revoked_reason, updated_at`

func scanConsoleSession(row interface{ Scan(...any) error }) (*ConsoleSession, error) {
	var s ConsoleSession
	if err := row.Scan(&s.ID, &s.UserID, &s.Method, &s.ProviderID, &s.IdPSubject, &s.IdPSession, &s.IdPRefreshToken,
		&s.CreatedAt, &s.RefreshedAt, &s.ExpiresAt, &s.RevokedAt, &s.RevokedReason, &s.UpdatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

// PutConsoleSession writes s whole, as it is or as a peer sent it.
func (s *SQLiteStore) PutConsoleSession(sess *ConsoleSession) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO console_sessions (`+consoleSessionColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, sess.Method, sess.ProviderID, sess.IdPSubject, sess.IdPSession, sess.IdPRefreshToken,
		sess.CreatedAt, sess.RefreshedAt, sess.ExpiresAt, sess.RevokedAt, sess.RevokedReason, sess.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to store console session: %w", err)
	}
	return nil
}

// RenewConsoleSession writes what a renewal changes in sess, unless the session
// was ended meanwhile, and reports whether it did.
func (s *SQLiteStore) RenewConsoleSession(sess *ConsoleSession) (bool, error) {
	res, err := s.db.Exec(`UPDATE console_sessions SET idp_refresh_token = ?, refreshed_at = ?, expires_at = ?, updated_at = ?
		WHERE id = ? AND revoked_at = 0`, sess.IdPRefreshToken, sess.RefreshedAt, sess.ExpiresAt, sess.UpdatedAt, sess.ID)
	if err != nil {
		return false, fmt.Errorf("failed to renew console session: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// GetConsoleSession returns the session id names, or sql.ErrNoRows.
func (s *SQLiteStore) GetConsoleSession(id string) (*ConsoleSession, error) {
	return scanConsoleSession(s.db.QueryRow(`SELECT `+consoleSessionColumns+` FROM console_sessions WHERE id = ?`, id))
}

// ListConsoleSessions returns the sessions that have not expired by now,
// ended ones included.
func (s *SQLiteStore) ListConsoleSessions(now int64) ([]*ConsoleSession, error) {
	rows, err := s.db.Query(`SELECT `+consoleSessionColumns+` FROM console_sessions WHERE expires_at > ?`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []*ConsoleSession
	for rows.Next() {
		sess, err := scanConsoleSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

// DeleteExpiredConsoleSessions drops the sessions expired before cutoff.
func (s *SQLiteStore) DeleteExpiredConsoleSessions(cutoff int64) error {
	_, err := s.db.Exec(`DELETE FROM console_sessions WHERE expires_at < ?`, cutoff)
	return err
}

func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// sessionLifetimes are the access token's and the session's (the refresh
// token's) lifetimes, in seconds.
func (am *authManager) sessionLifetimes() (int, int) {
	accessTTL, refreshTTL := 900, 86400
	if am.settingsManager != nil {
		if v, err := am.settingsManager.GetInt("security.access_token_lifetime"); err == nil && v > 0 {
			accessTTL = v
		}
		if v, err := am.settingsManager.GetInt("security.session_timeout"); err == nil && v > 0 {
			refreshTTL = v
		}
	}
	return accessTTL, refreshTTL
}

// SetSessionsChangedCallback sets what runs when a console session is made,
// renewed or ended: in a cluster, sending the sessions to the other nodes.
func (am *authManager) SetSessionsChangedCallback(cb func()) {
	am.sessionsChangedCallback = cb
}

// SetSessionNode sets what names the node a session starts on: in a cluster,
// this node, which renews the session with its identity provider.
func (am *authManager) SetSessionNode(node func(ctx context.Context) string) {
	am.sessionNode = node
}

// SessionNode is the node that started the session id names, or "" for one
// started outside a cluster.
func SessionNode(id string) string {
	node, _, named := strings.Cut(id, ".")
	if !named {
		return ""
	}
	return node
}

// SessionIDOf is the session of a console token whose signature checks, or "".
func (am *authManager) SessionIDOf(token string) string {
	claims, err := am.parseToken(token)
	if err != nil {
		return ""
	}
	return claims.SessionID
}

func (am *authManager) sessionsChanged() {
	if am.sessionsChangedCallback != nil {
		am.sessionsChangedCallback()
	}
}

// StartSession records a sign-in of user as origin tells (method, provider and
// the provider's grant) and issues its tokens.
func (am *authManager) StartSession(ctx context.Context, user *User, origin *ConsoleSession) (*TokenPair, error) {
	_, refreshTTL := am.sessionLifetimes()
	sess, err := am.newSession(ctx, user, origin, int64(refreshTTL))
	if err != nil {
		return nil, err
	}
	if err := am.store.PutConsoleSession(sess); err != nil {
		return nil, err
	}
	am.sessionsChanged()
	return am.sessionTokens(user, sess.ID)
}

func (am *authManager) newSession(ctx context.Context, user *User, origin *ConsoleSession, lifetime int64) (*ConsoleSession, error) {
	id, err := newSessionID()
	if err != nil {
		return nil, err
	}
	if am.sessionNode != nil {
		if node := am.sessionNode(ctx); node != "" {
			id = node + "." + id
		}
	}
	sess := &ConsoleSession{Method: SessionMethodPassword}
	if origin != nil {
		copied := *origin
		sess = &copied
	}
	now := time.Now().Unix()
	sess.ID, sess.UserID = id, user.ID
	sess.CreatedAt, sess.RefreshedAt, sess.UpdatedAt = now, now, time.Now().UnixNano()
	sess.ExpiresAt = now + lifetime
	sess.RevokedAt, sess.RevokedReason = 0, ""
	// The sessions of yesterday that expired are of no use to anybody.
	_ = am.store.DeleteExpiredConsoleSessions(now - 86400)
	return sess, nil
}

// ContinueSession renews sess for another lifetime, keeping what the caller
// changed in it (a provider's new refresh token), and issues its tokens. A
// session ended since it was read is not renewed.
func (am *authManager) ContinueSession(ctx context.Context, user *User, sess *ConsoleSession) (*TokenPair, error) {
	_, refreshTTL := am.sessionLifetimes()
	now := time.Now().Unix()
	sess.RefreshedAt, sess.UpdatedAt = now, time.Now().UnixNano()
	sess.ExpiresAt = now + int64(refreshTTL)
	renewed, err := am.store.RenewConsoleSession(sess)
	if err != nil {
		return nil, err
	}
	if !renewed {
		return nil, ErrSessionEnded
	}
	am.sessionsChanged()
	return am.sessionTokens(user, sess.ID)
}

func (am *authManager) sessionTokens(user *User, sessionID string) (*TokenPair, error) {
	accessTTL, refreshTTL := am.sessionLifetimes()
	accessToken, err := am.generateSessionToken(user, TokenTypeAccess, accessTTL, sessionID)
	if err != nil {
		return nil, err
	}
	refreshToken, err := am.generateSessionToken(user, TokenTypeRefresh, refreshTTL, sessionID)
	if err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: accessToken, RefreshToken: refreshToken, ExpiresIn: accessTTL, TokenType: "Bearer"}, nil
}

// StartTwoFactorSession records a sign-in waiting for its second step and
// issues the token that stands for the first: the session is kept for the
// token's lifetime and starts when the code is checked.
func (am *authManager) StartTwoFactorSession(ctx context.Context, user *User, origin *ConsoleSession) (string, error) {
	sess, err := am.newSession(ctx, user, origin, TwoFactorTokenTTL)
	if err != nil {
		return "", err
	}
	if err := am.store.PutConsoleSession(sess); err != nil {
		return "", err
	}
	am.sessionsChanged()
	return am.generateSessionToken(user, TokenTypeTwoFactor, TwoFactorTokenTTL, sess.ID)
}

// CompleteTwoFactorSession starts the session a two-factor token was issued
// for, once its code is checked. A token issued before sessions were kept
// starts a password session.
func (am *authManager) CompleteTwoFactorSession(ctx context.Context, user *User, twoFactorToken string) (*TokenPair, error) {
	claims, err := am.parseToken(twoFactorToken)
	if err != nil || claims.TokenType != TokenTypeTwoFactor {
		return nil, ErrInvalidToken
	}
	if claims.SessionID == "" {
		return am.StartSession(ctx, user, nil)
	}
	sess, err := am.store.GetConsoleSession(claims.SessionID)
	if err != nil || sess.UserID != user.ID {
		return nil, ErrInvalidToken
	}
	if sess.RevokedAt != 0 {
		return nil, ErrSessionEnded
	}
	return am.ContinueSession(ctx, user, sess)
}

// RefreshSession resolves the user and the session of a refresh token. A
// token issued before sessions were kept has no session: it is nil.
func (am *authManager) RefreshSession(ctx context.Context, refreshToken string) (*User, *ConsoleSession, error) {
	user, err := am.ValidateRefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, nil, err
	}
	claims, err := am.parseToken(refreshToken)
	if err != nil {
		return nil, nil, err
	}
	if claims.SessionID == "" {
		return user, nil, nil
	}
	sess, err := am.store.GetConsoleSession(claims.SessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrInvalidToken
	}
	if err != nil {
		return nil, nil, err
	}
	if sess.UserID != user.ID {
		return nil, nil, ErrInvalidToken
	}
	if sess.RevokedAt != 0 {
		return nil, nil, ErrSessionEnded
	}
	return user, sess, nil
}

// RevokeSession ends the session id names, for reason.
func (am *authManager) RevokeSession(ctx context.Context, id, reason string) error {
	sess, err := am.store.GetConsoleSession(id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return am.revoke(sess, reason)
}

func (am *authManager) revoke(sess *ConsoleSession, reason string) error {
	if sess.RevokedAt != 0 {
		return nil
	}
	now := time.Now().Unix()
	sess.RevokedAt, sess.RevokedReason, sess.UpdatedAt = now, reason, time.Now().UnixNano()
	if err := am.store.PutConsoleSession(sess); err != nil {
		return err
	}
	am.revokedSessions.Store(sess.ID, sess.ExpiresAt)
	am.sessionsChanged()
	return nil
}

// EndSession ends the session of a console token: a sign-out.
func (am *authManager) EndSession(ctx context.Context, token, reason string) error {
	claims, err := am.parseToken(token)
	if err != nil || claims.SessionID == "" {
		return nil
	}
	return am.RevokeSession(ctx, claims.SessionID, reason)
}

// RevokeIdPSessions ends the live sessions an identity provider signed in:
// those of its session idpSession, or with none given, every one of its
// subject. It returns how many it ended.
func (am *authManager) RevokeIdPSessions(ctx context.Context, providerID, subject, idpSession, reason string) (int, error) {
	if providerID == "" || (subject == "" && idpSession == "") {
		return 0, nil
	}
	sessions, err := am.store.ListConsoleSessions(time.Now().Unix())
	if err != nil {
		return 0, err
	}
	ended := 0
	for _, sess := range sessions {
		if sess.ProviderID != providerID || sess.RevokedAt != 0 {
			continue
		}
		if idpSession != "" && sess.IdPSession != idpSession {
			continue
		}
		if idpSession == "" && sess.IdPSubject != subject {
			continue
		}
		if err := am.revoke(sess, reason); err != nil {
			return ended, err
		}
		ended++
	}
	return ended, nil
}

// LoadRevokedSessions reads again which sessions were ended, as after a
// peer's sessions are stored.
func (am *authManager) LoadRevokedSessions() error {
	now := time.Now().Unix()
	sessions, err := am.store.ListConsoleSessions(now)
	if err != nil {
		return err
	}
	for _, sess := range sessions {
		if sess.RevokedAt != 0 {
			am.revokedSessions.Store(sess.ID, sess.ExpiresAt)
		}
	}
	am.revokedSessions.Range(func(id, expires any) bool {
		if expires.(int64) < now {
			am.revokedSessions.Delete(id)
		}
		return true
	})
	return nil
}

func (am *authManager) sessionRevoked(id string) bool {
	if id == "" {
		return false
	}
	_, revoked := am.revokedSessions.Load(id)
	return revoked
}

// ApplyPeerSessions stores the sessions another node sent: the later change
// of each wins, and an ended session is never started again.
func (am *authManager) ApplyPeerSessions(ctx context.Context, sessions []*ConsoleSession) error {
	now := time.Now().Unix()
	for _, incoming := range sessions {
		if incoming.ExpiresAt <= now {
			continue
		}
		local, err := am.store.GetConsoleSession(incoming.ID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if local != nil {
			if local.RevokedAt != 0 {
				continue
			}
			if incoming.RevokedAt == 0 && incoming.UpdatedAt <= local.UpdatedAt {
				continue
			}
		}
		if err := am.store.PutConsoleSession(incoming); err != nil {
			return err
		}
	}
	return am.LoadRevokedSessions()
}
