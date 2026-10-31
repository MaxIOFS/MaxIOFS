package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sessionUser(t *testing.T, m Manager, name string) *User {
	t.Helper()
	user := &User{ID: "id-" + name, Username: name, Password: "Secret123!", Status: UserStatusActive, Roles: []string{"user"}}
	require.NoError(t, m.CreateUser(context.Background(), user))
	return user
}

// A session ends before its tokens expire: its access token and its refresh
// token are refused from then on, another session of the user is not.
func TestASessionIsEndedBeforeItsTokensExpire(t *testing.T) {
	m, dir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, dir)
	ctx := context.Background()
	user := sessionUser(t, m, "ana")

	first, err := m.StartSession(ctx, user, nil)
	require.NoError(t, err)
	second, err := m.StartSession(ctx, user, nil)
	require.NoError(t, err)
	_, err = m.ValidateJWT(ctx, first.AccessToken)
	require.NoError(t, err)
	_, sess, err := m.RefreshSession(ctx, first.RefreshToken)
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, SessionMethodPassword, sess.Method)

	require.NoError(t, m.EndSession(ctx, first.AccessToken, "signed out"))
	_, err = m.ValidateJWT(ctx, first.AccessToken)
	assert.ErrorIs(t, err, ErrSessionEnded, "its access token")
	_, _, err = m.RefreshSession(ctx, first.RefreshToken)
	assert.ErrorIs(t, err, ErrSessionEnded, "its refresh token")

	_, err = m.ValidateJWT(ctx, second.AccessToken)
	assert.NoError(t, err, "another session")
	_, _, err = m.RefreshSession(ctx, second.RefreshToken)
	assert.NoError(t, err, "another session")
}

// A token issued before sessions were kept works until it expires, with no
// session to renew.
func TestATokenIssuedBeforeSessionsWorksUntilItExpires(t *testing.T) {
	m, dir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, dir)
	ctx := context.Background()
	user := sessionUser(t, m, "old")

	pair, err := m.GenerateTokenPair(ctx, user)
	require.NoError(t, err)
	_, err = m.ValidateJWT(ctx, pair.AccessToken)
	require.NoError(t, err)
	refreshed, sess, err := m.RefreshSession(ctx, pair.RefreshToken)
	require.NoError(t, err)
	assert.Equal(t, user.ID, refreshed.ID)
	assert.Nil(t, sess)
}

// A provider's logout ends the sessions of its session, or with none named,
// every one of its subject; not those of another provider.
func TestAProvidersLogoutEndsItsSessions(t *testing.T) {
	m, dir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, dir)
	ctx := context.Background()
	user := sessionUser(t, m, "alice")
	start := func(provider, subject, idpSession string) *TokenPair {
		t.Helper()
		pair, err := m.StartSession(ctx, user, &ConsoleSession{Method: SessionMethodOAuth, ProviderID: provider,
			IdPSubject: subject, IdPSession: idpSession})
		require.NoError(t, err)
		return pair
	}
	live := func(pair *TokenPair) bool {
		_, err := m.ValidateJWT(ctx, pair.AccessToken)
		return err == nil
	}
	s1 := start("kc", "sub-alice", "sid-1")
	s2 := start("kc", "sub-alice", "sid-2")
	other := start("other", "sub-alice", "sid-1")

	n, err := m.RevokeIdPSessions(ctx, "kc", "sub-alice", "sid-1", "provider logout")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.False(t, live(s1))
	assert.True(t, live(s2), "another session of the provider")
	assert.True(t, live(other), "another provider's session")

	n, err = m.RevokeIdPSessions(ctx, "kc", "sub-alice", "", "provider logout")
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.False(t, live(s2))
	assert.True(t, live(other), "another provider's session")
}

// A sign-in with two-factor authentication starts its session when the code is
// checked, with what its first step knew of the provider.
func TestATwoFactorSessionKeepsItsProvidersGrant(t *testing.T) {
	m, dir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, dir)
	ctx := context.Background()
	user := sessionUser(t, m, "carla")

	token, err := m.StartTwoFactorSession(ctx, user, &ConsoleSession{Method: SessionMethodOAuth, ProviderID: "kc",
		IdPSubject: "sub-carla", IdPRefreshToken: "encrypted-refresh"})
	require.NoError(t, err)
	_, err = m.ValidateJWT(ctx, token)
	assert.Error(t, err, "the first step's token is no session")

	pair, err := m.CompleteTwoFactorSession(ctx, user, token)
	require.NoError(t, err)
	_, sess, err := m.RefreshSession(ctx, pair.RefreshToken)
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, "kc", sess.ProviderID)
	assert.Equal(t, "encrypted-refresh", sess.IdPRefreshToken)
	assert.Greater(t, sess.ExpiresAt, time.Now().Unix()+TwoFactorTokenTTL, "the session lives longer than the first step")
}

// Another node's sessions are stored with the later change of each winning,
// and an ended session is never started again.
func TestPeerSessionsNeverStartAnEndedOneAgain(t *testing.T) {
	m, dir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, dir)
	ctx := context.Background()
	user := sessionUser(t, m, "peer")
	pair, err := m.StartSession(ctx, user, nil)
	require.NoError(t, err)
	_, sess, err := m.RefreshSession(ctx, pair.RefreshToken)
	require.NoError(t, err)

	older := *sess
	older.UpdatedAt--
	older.IdPRefreshToken = "older"
	require.NoError(t, m.ApplyPeerSessions(ctx, []*ConsoleSession{&older}))
	_, kept, err := m.RefreshSession(ctx, pair.RefreshToken)
	require.NoError(t, err)
	assert.Empty(t, kept.IdPRefreshToken, "an older change")

	ended := *sess
	ended.RevokedAt, ended.RevokedReason = ended.UpdatedAt, "provider logout"
	require.NoError(t, m.ApplyPeerSessions(ctx, []*ConsoleSession{&ended}))
	_, err = m.ValidateJWT(ctx, pair.AccessToken)
	assert.ErrorIs(t, err, ErrSessionEnded, "ended on another node")

	revived := *sess
	revived.UpdatedAt += 100
	require.NoError(t, m.ApplyPeerSessions(ctx, []*ConsoleSession{&revived}))
	_, _, err = m.RefreshSession(ctx, pair.RefreshToken)
	assert.ErrorIs(t, err, ErrSessionEnded, "a later change does not start it again")
}

// The sessions ended are still ended after a restart.
func TestEndedSessionsStayEndedAfterARestart(t *testing.T) {
	cfg := config.AuthConfig{EnableAuth: true, JWTSecret: "test-secret-key-for-testing-only-minimum-32-chars"}
	m, dir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, dir)
	ctx := context.Background()
	user := sessionUser(t, m, "restart")
	pair, err := m.StartSession(ctx, user, nil)
	require.NoError(t, err)
	require.NoError(t, m.EndSession(ctx, pair.AccessToken, "signed out"))
	require.NoError(t, m.(*authManager).Close())

	again := NewManager(cfg, dir)
	defer again.(*authManager).Close()
	_, err = again.ValidateJWT(ctx, pair.AccessToken)
	assert.ErrorIs(t, err, ErrSessionEnded)
}

// A session ended while a renewal of it asked its provider stays ended: the
// renewal is refused, and its refresh token after a restart too.
func TestAnEndDuringARenewalStands(t *testing.T) {
	cfg := config.AuthConfig{EnableAuth: true, JWTSecret: "test-secret-key-for-testing-only-minimum-32-chars"}
	m, dir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, dir)
	ctx := context.Background()
	user := sessionUser(t, m, "renewing")
	pair, err := m.StartSession(ctx, user, nil)
	require.NoError(t, err)
	_, sess, err := m.RefreshSession(ctx, pair.RefreshToken)
	require.NoError(t, err)

	require.NoError(t, m.RevokeSession(ctx, sess.ID, "provider logout"))
	_, err = m.ContinueSession(ctx, user, sess)
	assert.ErrorIs(t, err, ErrSessionEnded)
	require.NoError(t, m.(*authManager).Close())

	again := NewManager(cfg, dir)
	defer again.(*authManager).Close()
	_, _, err = again.RefreshSession(ctx, pair.RefreshToken)
	assert.ErrorIs(t, err, ErrSessionEnded)
}

// In a cluster a session's ID names the node that started it, read from a
// token whose signature checks; a session started outside one names none.
func TestASessionNamesTheNodeThatStartedIt(t *testing.T) {
	m, dir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, dir)
	ctx := context.Background()
	user := sessionUser(t, m, "named")

	alone, err := m.StartSession(ctx, user, nil)
	require.NoError(t, err)
	assert.Empty(t, SessionNode(m.SessionIDOf(alone.RefreshToken)))

	m.SetSessionNode(func(context.Context) string { return "node-a" })
	pair, err := m.StartSession(ctx, user, nil)
	require.NoError(t, err)
	id := m.SessionIDOf(pair.RefreshToken)
	assert.Equal(t, "node-a", SessionNode(id))
	assert.Equal(t, id, m.SessionIDOf(pair.AccessToken))
	_, sess, err := m.RefreshSession(ctx, pair.RefreshToken)
	require.NoError(t, err)
	assert.Equal(t, id, sess.ID)

	forged := pair.RefreshToken[:len(pair.RefreshToken)-4] + "AAAA"
	assert.Empty(t, m.SessionIDOf(forged), "a token whose signature does not check names nothing")
}

// A renewal right after the sign-in, in the same second, reaches the other
// nodes: they keep the provider's refresh token it gave, not the spent one.
func TestARenewalInTheSameSecondReachesTheOtherNodes(t *testing.T) {
	ctx := context.Background()
	m, dir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, dir)
	peer, peerDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, peerDir)
	user := sessionUser(t, m, "fast")
	sessionUser(t, peer, "fast")
	store := m.(*authManager).store

	pair, err := m.StartSession(ctx, user, &ConsoleSession{Method: SessionMethodOAuth, ProviderID: "idp-1", IdPRefreshToken: "rt-1"})
	require.NoError(t, err)
	id := m.SessionIDOf(pair.RefreshToken)
	started, err := store.GetConsoleSession(id)
	require.NoError(t, err)
	renewing := *started
	renewing.IdPRefreshToken = "rt-2"
	_, err = m.ContinueSession(ctx, user, &renewing)
	require.NoError(t, err)
	renewed, err := store.GetConsoleSession(id)
	require.NoError(t, err)

	require.NoError(t, peer.ApplyPeerSessions(ctx, []*ConsoleSession{started}))
	require.NoError(t, peer.ApplyPeerSessions(ctx, []*ConsoleSession{renewed}))
	got, err := peer.(*authManager).store.GetConsoleSession(id)
	require.NoError(t, err)
	assert.Equal(t, "rt-2", got.IdPRefreshToken)
}

// A peer's older copy of a session that arrives while the session is renewed
// here never replaces the renewal.
func TestAnOlderPeerCopyNeverReplacesARenewal(t *testing.T) {
	ctx := context.Background()
	m, dir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, dir)
	user := sessionUser(t, m, "racing")
	store := m.(*authManager).store

	pair, err := m.StartSession(ctx, user, &ConsoleSession{Method: SessionMethodOAuth, ProviderID: "idp-1", IdPRefreshToken: "rt-0"})
	require.NoError(t, err)
	id := m.SessionIDOf(pair.RefreshToken)
	for i := 1; i <= 200; i++ {
		older, err := store.GetConsoleSession(id)
		require.NoError(t, err)
		older.IdPRefreshToken = "older"
		older.UpdatedAt++
		renewing := *older
		renewing.IdPRefreshToken = fmt.Sprintf("rt-%d", i)

		done := make(chan error, 1)
		go func() { done <- m.ApplyPeerSessions(ctx, []*ConsoleSession{older}) }()
		_, err = m.ContinueSession(ctx, user, &renewing)
		require.NoError(t, err)
		require.NoError(t, <-done)
		got, err := store.GetConsoleSession(id)
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("rt-%d", i), got.IdPRefreshToken, "round %d", i)
	}
}
