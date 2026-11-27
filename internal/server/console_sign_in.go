package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/sirupsen/logrus"
)

// signInRequest is the console's sign-in with a username and a password.
type signInRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req signInRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	clientIP := getClientIP(r, s.proxyTrust())
	logrus.WithFields(logrus.Fields{"username": req.Username, "ip": clientIP, "method": r.Method}).Debug("Login attempt received")

	// Only failed sign-ins count against an address.
	if !s.authManager.CheckRateLimit(clientIP) {
		logrus.WithFields(logrus.Fields{"ip": clientIP, "username": req.Username}).Warn("Login rate limit exceeded")
		s.writeError(w, "Too many login attempts. Please try again later.", http.StatusTooManyRequests)
		return
	}
	named, err := s.authManager.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		s.refuseUnknownSignIn(w, r, req.Username, clientIP)
		return
	}
	if s.refuseLockedSignIn(w, r, named) {
		return
	}
	user, external, ok := s.signInCredentials(w, r, named, req.Password, clientIP)
	if !ok {
		return
	}
	twoFactor, ok := s.mayStartConsoleSession(w, r, user)
	if !ok {
		return
	}
	if twoFactor {
		s.answerTwoFactorSignIn(w, r, user, external, clientIP)
		return
	}
	s.answerConsoleSession(w, r, user, external, clientIP, req.Username == "admin" && req.Password == "admin")
}

// writeSSOHint sends a sign-in to the identity provider's button.
func writeSSOHint(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]interface{}{"error": message, "sso_hint": true}) //nolint:errcheck
}

// refuseUnknownSignIn answers a sign-in of a username no user has: as wrong
// credentials, so as not to tell which users exist, or, for an email while an
// OAuth provider signs users in, with a hint to use it.
func (s *Server) refuseUnknownSignIn(w http.ResponseWriter, r *http.Request, username, clientIP string) {
	if strings.Contains(username, "@") && s.idpManager != nil {
		if providers, _ := s.idpManager.ListActiveOAuthProviders(r.Context()); len(providers) > 0 {
			writeSSOHint(w, "This email may use SSO. Please use the SSO login button below.")
			return
		}
	}
	s.authManager.RecordFailedLogin(r.Context(), "", clientIP)
	s.writeError(w, "Invalid credentials", http.StatusUnauthorized)
}

// refuseLockedSignIn answers a sign-in to an account locked after failed
// attempts, before its password is read.
func (s *Server) refuseLockedSignIn(w http.ResponseWriter, r *http.Request, user *auth.User) bool {
	locked, lockedUntil, err := s.authManager.IsAccountLocked(r.Context(), user.ID)
	if err != nil {
		logrus.WithError(err).Error("Failed to check account lock status")
		s.writeError(w, "Internal server error", http.StatusInternalServerError)
		return true
	}
	if !locked {
		return false
	}
	logrus.WithFields(logrus.Fields{
		"user_id":        user.ID,
		"username":       user.Username,
		"locked_until":   time.Unix(lockedUntil, 0).Format(time.RFC3339),
		"remaining_time": time.Until(time.Unix(lockedUntil, 0)).String(),
	}).Warn("Login attempt on locked account")
	writeAccountLocked(w, lockedUntil)
	return true
}

// signInCredentials checks the password of named with what signs it in: the
// server for a local user, its directory for one of LDAP. A user of an OAuth
// provider signs in through it.
func (s *Server) signInCredentials(w http.ResponseWriter, r *http.Request, named *auth.User, password, clientIP string) (*auth.User, *idp.ExternalUser, bool) {
	switch {
	case named.AuthProvider == "" || named.AuthProvider == "local":
		user, err := s.authManager.ValidateConsoleCredentials(r.Context(), named.Username, password)
		if err != nil {
			s.authManager.RecordFailedLogin(r.Context(), named.ID, clientIP)
			s.writeError(w, "Invalid credentials", http.StatusUnauthorized)
			return nil, nil, false
		}
		return user, nil, true
	case strings.HasPrefix(named.AuthProvider, "ldap:"):
		return s.ldapSignIn(w, r, named, password, clientIP)
	case strings.HasPrefix(named.AuthProvider, "oauth:"):
		writeSSOHint(w, "This account uses SSO. Please use the SSO login button below.")
		return nil, nil, false
	default:
		s.writeError(w, "Unknown authentication provider", http.StatusInternalServerError)
		return nil, nil, false
	}
}

// ldapSignIn checks the password of named with its directory, and the groups
// it gives, when they decide who signs in. A directory that does not answer
// holds neither the account nor the address to it.
func (s *Server) ldapSignIn(w http.ResponseWriter, r *http.Request, named *auth.User, password, clientIP string) (*auth.User, *idp.ExternalUser, bool) {
	if s.idpManager == nil {
		s.writeError(w, "Identity provider system not available", http.StatusServiceUnavailable)
		return nil, nil, false
	}
	providerID := strings.TrimPrefix(named.AuthProvider, "ldap:")
	external, err := s.idpManager.AuthenticateExternal(r.Context(), providerID, named.ExternalID, password)
	if errors.Is(err, idp.ErrUnavailable) {
		logrus.WithError(err).WithFields(logrus.Fields{"user_id": named.ID, "provider_id": providerID}).
			Warn("LDAP sign-in: directory unavailable")
		s.writeError(w, "The directory that signs this user in is not available. Try again later.", http.StatusServiceUnavailable)
		return nil, nil, false
	}
	if err != nil {
		s.authManager.RecordFailedLogin(r.Context(), named.ID, clientIP)
		logrus.WithFields(logrus.Fields{"user_id": named.ID, "provider_id": providerID, "error": err.Error()}).
			Warn("LDAP authentication failed")
		s.writeError(w, "Invalid credentials", http.StatusUnauthorized)
		return nil, nil, false
	}
	user, allowed := s.applyProviderGroups(r.Context(), named, external)
	if !allowed {
		s.writeError(w, "Your account is in no group of its directory authorized to sign in", http.StatusForbidden)
		return nil, nil, false
	}
	return user, external, true
}

// mayStartConsoleSession refuses a session to an inactive account, or one of
// an inactive tenant, to one without access to the console, and to an
// administrator without two-factor authentication where it is required. It
// reports whether the sign-in waits for a two-factor code.
func (s *Server) mayStartConsoleSession(w http.ResponseWriter, r *http.Request, user *auth.User) (bool, bool) {
	if !s.authManager.AccountActive(r.Context(), user) {
		s.writeError(w, "Invalid credentials", http.StatusUnauthorized)
		return false, false
	}
	if !s.userHasConsoleAccess(r.Context(), user) {
		s.writeError(w, "Your account does not have access to the console", http.StatusForbidden)
		return false, false
	}
	twoFactor, _, err := s.authManager.Get2FAStatus(r.Context(), user.ID)
	if err != nil {
		logrus.WithError(err).Error("Failed to check 2FA status")
		s.writeError(w, "Internal server error", http.StatusInternalServerError)
		return false, false
	}
	if required, _ := s.settingsManager.GetBool("security.require_2fa_admin"); !twoFactor && required && s.isAdmin(user) {
		s.writeError(w, "Admin accounts must have two-factor authentication enabled. Please set up 2FA before logging in.", http.StatusForbidden)
		return false, false
	}
	return twoFactor, true
}

// answerTwoFactorSignIn answers credentials of a user with two-factor
// authentication: the token stands for them until the code is checked.
func (s *Server) answerTwoFactorSignIn(w http.ResponseWriter, r *http.Request, user *auth.User, external *idp.ExternalUser, clientIP string) {
	token, err := s.authManager.StartTwoFactorSession(r.Context(), user, s.sessionOrigin(user, external))
	if err != nil {
		s.writeError(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}
	logrus.WithFields(logrus.Fields{"user_id": user.ID, "username": user.Username, "ip": clientIP}).
		Info("Login credentials validated, 2FA required")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"success":          true,
		"requires_2fa":     true,
		"two_factor_token": token,
		"message":          "Two-factor authentication required",
	})
}

// answerConsoleSession records the sign-in and starts the console session: a
// short-lived access token and a sliding-window refresh token.
func (s *Server) answerConsoleSession(w http.ResponseWriter, r *http.Request, user *auth.User, external *idp.ExternalUser, clientIP string, defaultPassword bool) {
	s.authManager.RecordSuccessfulLogin(r.Context(), user.ID)
	s.auditLoginSuccess(r, user, "password")
	pair, err := s.authManager.StartSession(r.Context(), user, s.sessionOrigin(user, external))
	if err != nil {
		s.writeError(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}
	logrus.WithFields(logrus.Fields{"user_id": user.ID, "username": user.Username, "ip": clientIP}).Info("Successful login")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"success":          true,
		"token":            pair.AccessToken, // kept for backward compat
		"access_token":     pair.AccessToken,
		"refresh_token":    pair.RefreshToken,
		"expires_in":       pair.ExpiresIn,
		"token_type":       pair.TokenType,
		"default_password": defaultPassword,
		// The console keeps the session but must send the user straight to the
		// password form; the API refuses everything else until they do.
		"must_change_password": user.MustChangePassword,
		"user":                 s.sessionUser(user),
	})
}
