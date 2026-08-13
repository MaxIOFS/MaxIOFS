package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/sirupsen/logrus"
)

// stsFederationRequest is the common body of both exchange endpoints. Only one
// of (username, password) / token is used, depending on the endpoint.
type stsFederationRequest struct {
	ProviderID      string          `json:"providerId"`
	Username        string          `json:"username"`
	Password        string          `json:"password"`
	Token           string          `json:"token"`
	DurationSeconds int             `json:"durationSeconds"`
	SessionPolicy   json.RawMessage `json:"sessionPolicy,omitempty"`
}

// sessionPolicyDocument returns the policy as a string whether the caller sent a
// JSON object or a JSON string containing the document.
func (req *stsFederationRequest) sessionPolicyDocument() string {
	if len(req.SessionPolicy) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(req.SessionPolicy, &asString); err == nil {
		return asString
	}
	return string(req.SessionPolicy)
}

// handleSTSLDAPIdentity exchanges LDAP credentials for temporary S3 credentials.
// POST /api/v1/sts/ldap-identity
func (s *Server) handleSTSLDAPIdentity(w http.ResponseWriter, r *http.Request) {
	req, ok := s.beginSTSFederation(w, r, "ldap")
	if !ok {
		return
	}

	if req.Username == "" || req.Password == "" {
		s.writeError(w, "username and password are required", http.StatusBadRequest)
		return
	}

	provider, ok := s.resolveFederationProvider(w, r, req.ProviderID, idp.TypeLDAP)
	if !ok {
		return
	}

	user, err := s.resolveLDAPIdentity(r.Context(), provider.ID, req.Username, req.Password)
	if err != nil {
		s.denySTSFederation(w, r, "ldap", provider.ID, req.Username, federationDenialReason(err))
		return
	}

	s.completeSTSFederation(w, r, req, user, "ldap", provider.ID)
}

// federationDenied carries a rejection reason that is safe for the audit log but
// deliberately never reaches the caller — see denySTSFederation.
type federationDenied struct{ reason string }

func (e *federationDenied) Error() string { return e.reason }

// federationDenialReason extracts the audit reason from an identity-resolution
// error, falling back to the raw message for unexpected failures.
func federationDenialReason(err error) string {
	var denied *federationDenied
	if errors.As(err, &denied) {
		return denied.reason
	}
	return err.Error()
}

// resolveLDAPIdentity authenticates username/password against an LDAP provider
func (s *Server) resolveLDAPIdentity(ctx context.Context, providerID, username, password string) (*auth.User, error) {
	user, err := s.authManager.GetUser(ctx, username)
	if err != nil || user == nil || user.AuthProvider != "ldap:"+providerID {
		return nil, &federationDenied{"user is not linked to this LDAP provider"}
	}

	if _, err := s.idpManager.AuthenticateExternal(ctx, providerID, user.ExternalID, password); err != nil {
		logrus.WithFields(logrus.Fields{
			"provider_id": providerID,
			"username":    username,
			"error":       err.Error(),
		}).Warn("STS federation: LDAP authentication failed")
		return nil, &federationDenied{"invalid credentials"}
	}

	return user, nil
}

// resolveWebIdentity validates an OAuth access token against a provider and
// returns the local user it maps to, provisioning through group mappings the
// same way the browser callback does.
func (s *Server) resolveWebIdentity(ctx context.Context, r *http.Request, providerID, token string) (*auth.User, error) {
	// The provider's userinfo endpoint validates the token for us: expired,
	// revoked and forged tokens are all rejected there.
	externalUser, err := s.idpManager.AuthenticateWithAccessToken(ctx, providerID, token)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"provider_id": providerID,
			"error":       err.Error(),
		}).Warn("STS federation: access token rejected by provider")
		return nil, &federationDenied{"token rejected by the identity provider"}
	}
	if externalUser.Email == "" {
		return nil, &federationDenied{"provider returned no email claim"}
	}

	user, _ := s.findOAuthUser(ctx, externalUser.Email)
	if user == nil {
		var errCode string
		user, _, errCode = s.tryAutoProvision(ctx, r, externalUser)
		if user == nil {
			return nil, &federationDenied{errCode}
		}
	}

	return user, nil
}

// handleSTSWebIdentity exchanges an OAuth access token for temporary S3
// credentials. POST /api/v1/sts/web-identity
func (s *Server) handleSTSWebIdentity(w http.ResponseWriter, r *http.Request) {
	req, ok := s.beginSTSFederation(w, r, "oauth")
	if !ok {
		return
	}

	if req.Token == "" {
		s.writeError(w, "token is required", http.StatusBadRequest)
		return
	}

	provider, ok := s.resolveFederationProvider(w, r, req.ProviderID, idp.TypeOAuth2)
	if !ok {
		return
	}

	user, err := s.resolveWebIdentity(r.Context(), r, provider.ID, req.Token)
	if err != nil {
		s.denySTSFederation(w, r, "oauth", provider.ID, "", federationDenialReason(err))
		return
	}

	s.completeSTSFederation(w, r, req, user, "oauth", provider.ID)
}

// beginSTSFederation applies the gates every exchange shares: the feature must
// be enabled, the IDP subsystem present, the caller within the login rate limit,
// and the body parseable.
func (s *Server) beginSTSFederation(w http.ResponseWriter, r *http.Request, method string) (*stsFederationRequest, bool) {
	if enabled, err := s.settingsManager.GetBool("security.sts_federation_enabled"); err != nil || !enabled {
		s.writeError(w, "STS federation is disabled on this server", http.StatusForbidden)
		return nil, false
	}

	if s.idpManager == nil {
		s.writeError(w, "Identity provider system not available", http.StatusServiceUnavailable)
		return nil, false
	}

	// These endpoints accept credentials without a session, so they get the same
	// per-IP budget as the console login endpoint.
	clientIP := getClientIP(r, s.config.TrustedProxies)
	if !s.authManager.CheckRateLimit(clientIP) {
		logrus.WithFields(logrus.Fields{"ip": clientIP, "method": method}).
			Warn("STS federation rate limit exceeded")
		s.writeError(w, "Too many attempts. Please try again later.", http.StatusTooManyRequests)
		return nil, false
	}

	var req stsFederationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, "Invalid request body", http.StatusBadRequest)
		return nil, false
	}
	if req.ProviderID == "" {
		s.writeError(w, "providerId is required", http.StatusBadRequest)
		return nil, false
	}

	return &req, true
}

// resolveFederationProvider loads the provider and checks it is of the expected
// type and active. A provider still in "testing" is not usable for federation:
// it has not been signed off as an authentication source.
func (s *Server) resolveFederationProvider(w http.ResponseWriter, r *http.Request, providerID, wantType string) (*idp.IdentityProvider, bool) {
	provider, err := s.idpManager.GetProvider(r.Context(), providerID)
	if err != nil || provider == nil {
		s.writeError(w, "Identity provider not found", http.StatusNotFound)
		return nil, false
	}
	if provider.Type != wantType {
		s.writeError(w, "Identity provider is not of the expected type for this endpoint", http.StatusBadRequest)
		return nil, false
	}
	if provider.Status != idp.StatusActive {
		s.writeError(w, "Identity provider is not active", http.StatusForbidden)
		return nil, false
	}
	return provider, true
}

// completeSTSFederation runs the account-state and capability checks and issues
// the session. Everything here is the ordinary issuance path — federation only
// changes how the user was authenticated, never what the credential may do.
func (s *Server) completeSTSFederation(
	w http.ResponseWriter, r *http.Request,
	req *stsFederationRequest, user *auth.User,
	method, providerID string,
) {
	if err := s.authorizeSTSSubject(r.Context(), user); err != nil {
		s.denySTSFederation(w, r, method, providerID, user.Username, federationDenialReason(err))
		return
	}

	session, err := s.authManager.IssueSTSSession(r.Context(), user.ID, req.DurationSeconds, req.sessionPolicyDocument())
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrSTSInvalidPolicy):
			s.writeError(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, auth.ErrSTSInvalidDuration):
			s.writeError(w, "Session duration must be between 15 minutes and the configured maximum", http.StatusBadRequest)
		case errors.Is(err, auth.ErrSTSTooManySessions):
			s.writeError(w, "Too many active sessions. Revoke unused sessions before issuing new ones.", http.StatusTooManyRequests)
		case errors.Is(err, auth.ErrUserInactive):
			s.writeError(w, "Access denied", http.StatusForbidden)
		default:
			s.writeError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	s.touchLocalWriteAt(r.Context())
	if s.stsSessionSyncMgr != nil {
		s.stsSessionSyncMgr.TriggerSync(r.Context())
	}

	s.logAuditEvent(r.Context(), &audit.AuditEvent{
		TenantID:     user.TenantID,
		UserID:       user.ID,
		Username:     user.Username,
		EventType:    audit.EventTypeSTSFederationExchange,
		ResourceType: audit.ResourceTypeSTSSession,
		ResourceID:   session.TempAccessKeyID,
		ResourceName: session.TempAccessKeyID,
		Action:       audit.ActionCreate,
		Status:       audit.StatusSuccess,
		IPAddress:    getClientIP(r, s.config.TrustedProxies),
		UserAgent:    r.Header.Get("User-Agent"),
		Details: map[string]interface{}{
			"method":      method,
			"provider_id": providerID,
			"expires_at":  session.ExpiresAt,
		},
	})

	logrus.WithFields(logrus.Fields{
		"user_id":     user.ID,
		"provider_id": providerID,
		"method":      method,
	}).Info("STS federation: temporary credentials issued")

	s.writeJSON(w, stsSessionResponse{
		AccessKeyID:     session.TempAccessKeyID,
		SecretAccessKey: session.SecretAccessKey,
		SessionToken:    session.SessionToken,
		ExpiresAt:       session.ExpiresAt,
	})
}

// authorizeSTSSubject checks that a resolved user may receive temporary
// credentials at all. Shared by every issuance path that does not go through the
// console: the JSON federation endpoints and the AWS STS XML surface.
func (s *Server) authorizeSTSSubject(ctx context.Context, user *auth.User) error {
	if user.Status != auth.UserStatusActive {
		return &federationDenied{"account is not active"}
	}
	if locked, _, _ := s.authManager.IsAccountLocked(ctx, user.ID); locked {
		return &federationDenied{"account is locked"}
	}

	if !s.isAdmin(user) {
		allowed, err := s.authManager.HasCapability(ctx, user.ID, user.Roles, auth.CapKeysManageOwn)
		if err != nil || !allowed {
			return &federationDenied{"user may not manage their own API keys"}
		}
	}
	return nil
}

// denySTSFederation audits a rejected exchange and answers with a single opaque
func (s *Server) denySTSFederation(w http.ResponseWriter, r *http.Request, method, providerID, subject, reason string) {
	s.logAuditEvent(r.Context(), &audit.AuditEvent{
		Username:     subject,
		EventType:    audit.EventTypeSTSFederationDenied,
		ResourceType: audit.ResourceTypeSTSSession,
		ResourceName: subject,
		Action:       audit.ActionCreate,
		Status:       audit.StatusFailed,
		IPAddress:    getClientIP(r, s.config.TrustedProxies),
		UserAgent:    r.Header.Get("User-Agent"),
		Details: map[string]interface{}{
			"method":      method,
			"provider_id": providerID,
			"reason":      reason,
		},
	})

	logrus.WithFields(logrus.Fields{
		"method":      method,
		"provider_id": providerID,
		"subject":     subject,
		"reason":      reason,
	}).Warn("STS federation denied")

	s.writeError(w, "Access denied", http.StatusForbidden)
}
