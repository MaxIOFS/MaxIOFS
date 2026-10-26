package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/cluster"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/sirupsen/logrus"
)

// A user an identity provider's group mapping made carries this in its
// metadata: its role, and whether it may sign in, follow the groups the
// provider gives at each sign-in and renewal.
const (
	provisionedByKey    = "provisioned_by"
	provisionedByGroups = "group"
)

func groupManaged(user *auth.User) bool {
	return user != nil && user.Metadata[provisionedByKey] == provisionedByGroups
}

// markGroupManaged marks a user a group mapping makes.
func markGroupManaged(user *auth.User) {
	if user.Metadata == nil {
		user.Metadata = map[string]string{}
	}
	user.Metadata[provisionedByKey] = provisionedByGroups
}

// signInMethod is how user signs in, and the identity provider it signs in
// through.
func signInMethod(user *auth.User) (string, string) {
	switch {
	case strings.HasPrefix(user.AuthProvider, "ldap:"):
		return auth.SessionMethodLDAP, strings.TrimPrefix(user.AuthProvider, "ldap:")
	case strings.HasPrefix(user.AuthProvider, "oauth:"):
		return auth.SessionMethodOAuth, strings.TrimPrefix(user.AuthProvider, "oauth:")
	}
	return auth.SessionMethodPassword, ""
}

// sessionOrigin is how user signed in, for its console session: the method,
// the provider, and what the provider gave to ask about the sign-in again.
func (s *Server) sessionOrigin(user *auth.User, external *idp.ExternalUser) *auth.ConsoleSession {
	method, providerID := signInMethod(user)
	origin := &auth.ConsoleSession{Method: method, ProviderID: providerID}
	if external != nil && external.Grant != nil {
		origin.IdPSubject = external.Grant.Subject
		origin.IdPSession = external.Grant.Session
		origin.IdPRefreshToken = s.sealIdPToken(external.Grant.RefreshToken)
	}
	return origin
}

// sealIdPToken encrypts a provider's refresh token to keep it; without the
// secret to encrypt it with, it is not kept.
func (s *Server) sealIdPToken(token string) string {
	if token == "" || s.encSecret == nil {
		return ""
	}
	sealed, err := idp.Encrypt(token, s.encSecret.Current())
	if err != nil {
		logrus.WithError(err).Warn("Failed to encrypt an identity provider's refresh token")
		return ""
	}
	return sealed
}

func (s *Server) openIdPToken(sealed string) string {
	if sealed == "" || s.encSecret == nil {
		return ""
	}
	token, err := idp.Decrypt(sealed, s.encSecret.Current())
	if err != nil {
		logrus.WithError(err).Warn("Failed to decrypt an identity provider's refresh token")
		return ""
	}
	return token
}

// applyProviderGroups gives a group-managed user the role the groups its
// provider gives now map to, and reports false when they map to none: it may
// no longer sign in. Any other user is left as it is.
func (s *Server) applyProviderGroups(ctx context.Context, user *auth.User, external *idp.ExternalUser) (*auth.User, bool) {
	if !groupManaged(user) || external == nil || s.idpManager == nil {
		return user, true
	}
	_, providerID := signInMethod(user)
	mappings, err := s.idpManager.ListGroupMappings(ctx, providerID)
	if err != nil {
		logrus.WithError(err).WithField("provider_id", providerID).Warn("Failed to read the group mappings of a sign-in")
		return user, true
	}
	role, matched := resolveRoleFromMappings(external.Groups, mappings)
	if !matched {
		return user, false
	}
	if len(user.Roles) == 1 && user.Roles[0] == role {
		return user, true
	}
	logrus.WithFields(logrus.Fields{"user": user.Username, "from": user.Roles, "to": role}).
		Info("Role changed by the identity provider's groups")
	user.Roles = []string{role}
	if err := s.authManager.UpdateUser(ctx, user); err != nil {
		logrus.WithError(err).WithField("user", user.Username).Warn("Failed to store the role the provider's groups give")
	}
	return user, true
}

// errSessionEnded is the error of a revalidation that ends the session, with
// why.
type errSessionEnded struct{ reason string }

func (e *errSessionEnded) Error() string { return e.reason }

// The errors of a renewal other than a token refused: a user without the
// console, and tokens that could not be issued.
var (
	errNoConsoleAccess = errors.New("no access to the console")
	errTokenIssue      = errors.New("failed to issue the session's tokens")
)

// renewSession renews the session of refreshToken: with askProvider it asks
// the session's provider about it again, then issues its tokens. The session
// is read here, so the provider's newest refresh token is used and an end
// recorded meanwhile stands.
func (s *Server) renewSession(ctx context.Context, refreshToken string, askProvider bool) (*auth.TokenPair, error) {
	user, sess, err := s.authManager.RefreshSession(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	if askProvider {
		var ended *errSessionEnded
		user, err = s.revalidateSession(ctx, user, sess)
		if errors.As(err, &ended) {
			if revokeErr := s.authManager.RevokeSession(ctx, sess.ID, ended.reason); revokeErr != nil {
				logrus.WithError(revokeErr).Warn("Failed to record the end of a console session")
			}
			return nil, err
		}
	}
	if !s.userHasConsoleAccess(ctx, user) {
		return nil, errNoConsoleAccess
	}
	pair, err := s.authManager.ContinueSession(ctx, user, sess)
	if err != nil && !errors.Is(err, auth.ErrSessionEnded) {
		return nil, fmt.Errorf("%w: %w", errTokenIssue, err)
	}
	return pair, err
}

// sessionHome is the node that started the session id names, when it is
// another node of the cluster that may answer: it renews the session with its
// identity provider. A node known to be down is not asked.
func (s *Server) sessionHome(ctx context.Context, id string) *cluster.Node {
	nodeID := auth.SessionNode(id)
	if nodeID == "" || s.clusterManager == nil || !s.clusterManager.IsClusterEnabled() {
		return nil
	}
	if local, err := s.clusterManager.GetLocalNodeID(ctx); err != nil || local == nodeID {
		return nil
	}
	node, err := s.clusterManager.GetNode(ctx, nodeID)
	if err != nil || node == nil {
		return nil
	}
	if node.HealthStatus == cluster.HealthStatusUnavailable || node.HealthStatus == cluster.HealthStatusDead {
		return nil
	}
	return node
}

// renewOnHome asks home to renew the session of refreshToken and copies its
// answer back. It reports false when home does not answer.
func (s *Server) renewOnHome(ctx context.Context, w http.ResponseWriter, home *cluster.Node, refreshToken string) bool {
	localNodeID, err := s.clusterManager.GetLocalNodeID(ctx)
	if err != nil {
		return false
	}
	nodeToken, err := s.clusterManager.GetLocalNodeToken(ctx)
	if err != nil {
		return false
	}
	body, err := json.Marshal(map[string]string{"refresh_token": refreshToken})
	if err != nil {
		return false
	}
	forwardCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	proxy := s.clusterManager.ProxyClient()
	req, err := proxy.CreateAuthenticatedRequest(forwardCtx, http.MethodPost,
		home.Endpoint+"/api/internal/cluster/console-session-renew", bytes.NewReader(body), localNodeID, nodeToken)
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	log := logrus.WithField("node_id", home.ID)
	resp, err := proxy.DoAuthenticatedRequest(req)
	if err != nil {
		log.WithError(err).Warn("Session renewal: the node that started the session did not answer; it is kept without asking its provider")
		return false
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		log.WithField("status", resp.StatusCode).Warn("Session renewal: the node that started the session did not answer; it is kept without asking its provider")
		return false
	}
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
	return true
}

// handleClusterSessionRenew renews, for another node of the cluster, a session
// this node started.
func (s *Server) handleClusterSessionRenew(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		s.writeError(w, "refresh_token is required", http.StatusBadRequest)
		return
	}
	user, sess, err := s.authManager.RefreshSession(r.Context(), req.RefreshToken)
	s.renewHere(r.Context(), w, req.RefreshToken, user, sess, err, true)
}

// sessionRenewals lets the renewals of one session that arrive together share
// one: a provider that rotates refresh tokens refuses a second use of one, and
// would end the session.
type sessionRenewals struct {
	mu       sync.Mutex
	inFlight map[string]*sessionRenewal
}

type sessionRenewal struct {
	done chan struct{}
	pair *auth.TokenPair
	err  error
}

// do runs renew for the session id names, or waits for the renewal of it in
// flight, and gives its outcome.
func (r *sessionRenewals) do(id string, renew func() (*auth.TokenPair, error)) (*auth.TokenPair, error) {
	r.mu.Lock()
	if call, ok := r.inFlight[id]; ok {
		r.mu.Unlock()
		<-call.done
		return call.pair, call.err
	}
	call := &sessionRenewal{done: make(chan struct{})}
	if r.inFlight == nil {
		r.inFlight = map[string]*sessionRenewal{}
	}
	r.inFlight[id] = call
	r.mu.Unlock()

	call.pair, call.err = renew()
	r.mu.Lock()
	delete(r.inFlight, id)
	r.mu.Unlock()
	close(call.done)
	return call.pair, call.err
}

// revalidateSession asks the provider of an identity provider's session
// whether it still stands, keeping the provider's new refresh token in sess and
// giving the user the role its groups map to now. A provider that cannot be
// reached ends nothing: the session lasts as long as the console's own.
func (s *Server) revalidateSession(ctx context.Context, user *auth.User, sess *auth.ConsoleSession) (*auth.User, error) {
	if sess.ProviderID == "" || s.idpManager == nil {
		return user, nil
	}
	grant := idp.Grant{Subject: sess.IdPSubject, Session: sess.IdPSession, RefreshToken: s.openIdPToken(sess.IdPRefreshToken)}
	if sess.Method == auth.SessionMethodOAuth && grant.RefreshToken == "" {
		return user, nil
	}
	if sess.Method == auth.SessionMethodLDAP && grant.Subject == "" {
		grant.Subject = user.ExternalID
	}

	external, err := s.idpManager.RevalidateSession(ctx, sess.ProviderID, grant)
	log := logrus.WithFields(logrus.Fields{"user": user.Username, "provider_id": sess.ProviderID, "session": sess.ID})
	switch {
	case errors.Is(err, idp.ErrSessionEnded):
		log.WithError(err).Info("Console session ended by its identity provider")
		return nil, &errSessionEnded{"the identity provider ended the sign-in"}
	case errors.Is(err, idp.ErrUnavailable):
		log.WithError(err).Warn("Identity provider unavailable: the console session is kept")
		return user, nil
	case err != nil:
		log.WithError(err).Warn("Failed to ask the identity provider about a console session: it is kept")
		return user, nil
	case external == nil:
		return user, nil
	}

	if external.Grant != nil && external.Grant.RefreshToken != "" {
		sess.IdPRefreshToken = s.sealIdPToken(external.Grant.RefreshToken)
	}
	user, allowed := s.applyProviderGroups(ctx, user, external)
	if !allowed {
		log.Info("Console session ended: the user is in no group mapped any more")
		return nil, &errSessionEnded{"the user is in no authorized group"}
	}
	return user, nil
}
