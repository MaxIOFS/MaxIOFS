package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/maxiofs/maxiofs/internal/audit"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/sirupsen/logrus"
)

const (
	stsXMLNamespace = "https://sts.amazonaws.com/doc/2011-06-15/"

	// stsMaxFormBody bounds the form body. An STS request is a handful of
	// parameters; the only large one is a session policy, itself capped at 2 KB.
	stsMaxFormBody = 64 << 10
)

// --- Response documents (AWS shapes) ---

type stsCredentials struct {
	AccessKeyID     string `xml:"AccessKeyId"`
	SecretAccessKey string `xml:"SecretAccessKey"`
	SessionToken    string `xml:"SessionToken"`
	Expiration      string `xml:"Expiration"`
}

type stsAssumedRoleUser struct {
	Arn           string `xml:"Arn"`
	AssumedRoleID string `xml:"AssumedRoleId"`
}

type stsResponseMetadata struct {
	RequestID string `xml:"RequestId"`
}

type stsGetSessionTokenResponse struct {
	XMLName xml.Name `xml:"GetSessionTokenResponse"`
	Xmlns   string   `xml:"xmlns,attr"`
	Result  struct {
		Credentials stsCredentials `xml:"Credentials"`
	} `xml:"GetSessionTokenResult"`
	Metadata stsResponseMetadata `xml:"ResponseMetadata"`
}

type stsAssumeRoleResponse struct {
	XMLName xml.Name `xml:"AssumeRoleResponse"`
	Xmlns   string   `xml:"xmlns,attr"`
	Result  struct {
		Credentials     stsCredentials     `xml:"Credentials"`
		AssumedRoleUser stsAssumedRoleUser `xml:"AssumedRoleUser"`
	} `xml:"AssumeRoleResult"`
	Metadata stsResponseMetadata `xml:"ResponseMetadata"`
}

type stsWebIdentityResponse struct {
	XMLName xml.Name `xml:"AssumeRoleWithWebIdentityResponse"`
	Xmlns   string   `xml:"xmlns,attr"`
	Result  struct {
		Credentials                 stsCredentials     `xml:"Credentials"`
		AssumedRoleUser             stsAssumedRoleUser `xml:"AssumedRoleUser"`
		SubjectFromWebIdentityToken string             `xml:"SubjectFromWebIdentityToken"`
	} `xml:"AssumeRoleWithWebIdentityResult"`
	Metadata stsResponseMetadata `xml:"ResponseMetadata"`
}

type stsLDAPIdentityResponse struct {
	XMLName xml.Name `xml:"AssumeRoleWithLDAPIdentityResponse"`
	Xmlns   string   `xml:"xmlns,attr"`
	Result  struct {
		Credentials     stsCredentials     `xml:"Credentials"`
		AssumedRoleUser stsAssumedRoleUser `xml:"AssumedRoleUser"`
	} `xml:"AssumeRoleWithLDAPIdentityResult"`
	Metadata stsResponseMetadata `xml:"ResponseMetadata"`
}

type stsErrorResponse struct {
	XMLName xml.Name `xml:"ErrorResponse"`
	Xmlns   string   `xml:"xmlns,attr"`
	Error   struct {
		Type    string `xml:"Type"`
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	} `xml:"Error"`
	RequestID string `xml:"RequestId"`
}

// --- Handler ---

// handleAWSQueryRequest serves the AWS STS and IAM query protocols on POST / of
func (s *Server) handleAWSQueryRequest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, stsMaxFormBody)
	if err := r.ParseForm(); err != nil {
		writeSTSError(w, http.StatusBadRequest, "InvalidAction", "The request body could not be parsed as STS or IAM parameters.")
		return
	}

	action := r.PostForm.Get("Action")
	if IsIAMAction(action) {
		s.handleAWSIAMRequest(w, r)
		return
	}

	switch action {
	case "GetSessionToken":
		s.stsGetSessionToken(w, r, false)
	case "AssumeRole":
		s.stsAssumeRole(w, r)
	case "AssumeRoleWithWebIdentity":
		s.stsAssumeRoleWithWebIdentity(w, r)
	case "AssumeRoleWithLDAPIdentity":
		s.stsAssumeRoleWithLDAPIdentity(w, r)
	case "":
		writeSTSError(w, http.StatusBadRequest, "MissingAction", "No action was supplied with this request.")
	default:
		writeSTSError(w, http.StatusBadRequest, "InvalidAction",
			"The action "+action+" is not valid for this endpoint.")
	}
}

// stsAssumeRole serves AssumeRole.
func (s *Server) stsAssumeRole(w http.ResponseWriter, r *http.Request) {
	roleARN := strings.TrimSpace(r.PostForm.Get("RoleArn"))
	if roleARN == "" {
		s.stsGetSessionToken(w, r, true)
		return
	}

	user, ok := auth.GetUserFromContext(r.Context())
	if !ok || user == nil {
		writeSTSError(w, http.StatusForbidden, "AccessDenied",
			"This action must be signed with valid credentials.")
		return
	}
	if stsRequestSignedWithTemporaryCredential(r) {
		writeSTSError(w, http.StatusForbidden, "AccessDenied",
			"Temporary credentials cannot be used to issue new credentials.")
		return
	}

	iamManager, ok := s.authManager.(auth.IAMManager)
	if !ok {
		writeSTSError(w, http.StatusServiceUnavailable, "ServiceUnavailable", "Roles are not available on this server.")
		return
	}

	duration, err := stsDurationSeconds(r)
	if err != nil {
		writeSTSError(w, http.StatusBadRequest, "ValidationError", err.Error())
		return
	}

	session, err := iamManager.AssumeIAMRole(r.Context(), user, roleARN,
		r.PostForm.Get("RoleSessionName"), duration, r.PostForm.Get("Policy"))
	if err != nil {
		s.writeAssumeRoleError(w, r, user, roleARN, err)
		return
	}

	s.afterSTSXMLIssue(r, user, session, "AssumeRole", "")

	resp := &stsAssumeRoleResponse{Xmlns: stsXMLNamespace}
	resp.Result.Credentials = stsCredentialsOf(session)
	resp.Result.AssumedRoleUser = stsAssumedRoleUser{
		Arn:           session.RoleARN + "/" + session.RoleSessionName,
		AssumedRoleID: session.TempAccessKeyID,
	}
	resp.Metadata.RequestID = requestIDOf(r)
	writeSTSXML(w, resp)
}

// writeAssumeRoleError maps a failed AssumeRole onto the codes an SDK reacts to.
// A missing role and a refused one are told apart on purpose: the first is a
// configuration mistake the caller can fix, the second is a decision.
func (s *Server) writeAssumeRoleError(w http.ResponseWriter, r *http.Request, user *auth.User, roleARN string, err error) {
	switch {
	case errors.Is(err, auth.ErrIAMNoSuchEntity):
		writeSTSError(w, http.StatusNotFound, "NoSuchEntity", "The requested role does not exist: "+roleARN)
	case errors.Is(err, auth.ErrIAMInvalidInput):
		writeSTSError(w, http.StatusBadRequest, "ValidationError", err.Error())
	case errors.Is(err, auth.ErrAccessDenied):
		s.auditSTSXMLDenial(r, "AssumeRole", user.Username, "trust policy denied "+roleARN)
		writeSTSError(w, http.StatusForbidden, "AccessDenied",
			"The caller is not authorized to assume this role.")
	default:
		s.writeSTSIssuanceError(w, err)
	}
}

// stsGetSessionToken serves GetSessionToken, and AssumeRole when no RoleArn was
func (s *Server) stsGetSessionToken(w http.ResponseWriter, r *http.Request, asAssumeRole bool) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok || user == nil {
		writeSTSError(w, http.StatusForbidden, "AccessDenied",
			"This action must be signed with valid credentials.")
		return
	}

	// A temporary credential must not be able to mint another one: that would
	// let a leaked session renew itself forever and make expiry meaningless.
	if stsRequestSignedWithTemporaryCredential(r) {
		writeSTSError(w, http.StatusForbidden, "AccessDenied",
			"Temporary credentials cannot be used to issue new credentials.")
		return
	}

	if err := s.authorizeSTSSubject(r.Context(), user); err != nil {
		s.auditSTSXMLDenial(r, actionName(asAssumeRole), user.Username, federationDenialReason(err))
		writeSTSError(w, http.StatusForbidden, "AccessDenied", "Access denied.")
		return
	}

	duration, err := stsDurationSeconds(r)
	if err != nil {
		writeSTSError(w, http.StatusBadRequest, "ValidationError", err.Error())
		return
	}

	session, err := s.authManager.IssueSTSSession(r.Context(), user.ID, duration, r.PostForm.Get("Policy"))
	if err != nil {
		s.writeSTSIssuanceError(w, err)
		return
	}
	s.afterSTSXMLIssue(r, user, session, actionName(asAssumeRole), "")

	if asAssumeRole {
		resp := &stsAssumeRoleResponse{Xmlns: stsXMLNamespace}
		resp.Result.Credentials = stsCredentialsOf(session)
		resp.Result.AssumedRoleUser = assumedRoleUserOf(user)
		resp.Metadata.RequestID = requestIDOf(r)
		writeSTSXML(w, resp)
		return
	}

	resp := &stsGetSessionTokenResponse{Xmlns: stsXMLNamespace}
	resp.Result.Credentials = stsCredentialsOf(session)
	resp.Metadata.RequestID = requestIDOf(r)
	writeSTSXML(w, resp)
}

// stsAssumeRoleWithWebIdentity is the XML face of POST /sts/web-identity.
func (s *Server) stsAssumeRoleWithWebIdentity(w http.ResponseWriter, r *http.Request) {
	if !s.stsFederationReady(w, r, "oauth") {
		return
	}

	token := r.PostForm.Get("WebIdentityToken")
	if token == "" {
		writeSTSError(w, http.StatusBadRequest, "ValidationError", "WebIdentityToken is required.")
		return
	}

	providerID, err := s.stsResolveOAuthProviderID(r)
	if err != nil {
		writeSTSError(w, http.StatusBadRequest, "InvalidParameterValue", err.Error())
		return
	}

	user, err := s.resolveWebIdentity(r.Context(), r, providerID, token)
	if err != nil {
		s.auditSTSXMLDenial(r, "AssumeRoleWithWebIdentity", "", federationDenialReason(err))
		writeSTSError(w, http.StatusForbidden, "IDPRejectedClaim", "The identity token could not be validated.")
		return
	}

	session, ok := s.issueSTSForFederatedSubject(w, r, user, "AssumeRoleWithWebIdentity", providerID)
	if !ok {
		return
	}

	resp := &stsWebIdentityResponse{Xmlns: stsXMLNamespace}
	resp.Result.Credentials = stsCredentialsOf(session)
	resp.Result.AssumedRoleUser = assumedRoleUserOf(user)
	resp.Result.SubjectFromWebIdentityToken = user.Username
	resp.Metadata.RequestID = requestIDOf(r)
	writeSTSXML(w, resp)
}

// stsAssumeRoleWithLDAPIdentity is the XML face of POST /sts/ldap-identity.
func (s *Server) stsAssumeRoleWithLDAPIdentity(w http.ResponseWriter, r *http.Request) {
	if !s.stsFederationReady(w, r, "ldap") {
		return
	}

	username := r.PostForm.Get("LDAPUsername")
	password := r.PostForm.Get("LDAPPassword")
	if username == "" || password == "" {
		writeSTSError(w, http.StatusBadRequest, "ValidationError",
			"LDAPUsername and LDAPPassword are required.")
		return
	}

	// No ProviderId is needed: the local user record names the provider it is
	// linked to, so there is exactly one directory to bind against.
	providerID, err := s.stsResolveLDAPProviderID(r, username)
	if err != nil {
		s.auditSTSXMLDenial(r, "AssumeRoleWithLDAPIdentity", username, err.Error())
		writeSTSError(w, http.StatusForbidden, "AccessDenied", "Access denied.")
		return
	}

	user, err := s.resolveLDAPIdentity(r.Context(), providerID, username, password)
	if err != nil {
		s.auditSTSXMLDenial(r, "AssumeRoleWithLDAPIdentity", username, federationDenialReason(err))
		writeSTSError(w, http.StatusForbidden, "AccessDenied", "Access denied.")
		return
	}

	session, ok := s.issueSTSForFederatedSubject(w, r, user, "AssumeRoleWithLDAPIdentity", providerID)
	if !ok {
		return
	}

	resp := &stsLDAPIdentityResponse{Xmlns: stsXMLNamespace}
	resp.Result.Credentials = stsCredentialsOf(session)
	resp.Result.AssumedRoleUser = assumedRoleUserOf(user)
	resp.Metadata.RequestID = requestIDOf(r)
	writeSTSXML(w, resp)
}

// --- Shared plumbing ---

// stsFederationReady applies the gates the federated actions share with their
// JSON counterparts:
// the opt-in setting, the IDP subsystem, and the login rate limiter.
func (s *Server) stsFederationReady(w http.ResponseWriter, r *http.Request, method string) bool {
	if enabled, err := s.settingsManager.GetBool("security.sts_federation_enabled"); err != nil || !enabled {
		writeSTSError(w, http.StatusForbidden, "AccessDenied",
			"STS federation is disabled on this server.")
		return false
	}
	if s.idpManager == nil {
		writeSTSError(w, http.StatusServiceUnavailable, "ServiceUnavailable",
			"Identity provider system not available.")
		return false
	}

	clientIP := getClientIP(r, s.config.TrustedProxies)
	if !s.authManager.CheckRateLimit(clientIP) {
		logrus.WithFields(logrus.Fields{"ip": clientIP, "method": method}).
			Warn("STS federation rate limit exceeded (XML surface)")
		writeSTSError(w, http.StatusTooManyRequests, "Throttling",
			"Too many attempts. Please try again later.")
		return false
	}
	return true
}

// issueSTSForFederatedSubject runs the shared authorization check and issues the
// session, writing the XML error itself on failure.
func (s *Server) issueSTSForFederatedSubject(
	w http.ResponseWriter, r *http.Request, user *auth.User, action, providerID string,
) (*auth.STSSession, bool) {
	if err := s.authorizeSTSSubject(r.Context(), user); err != nil {
		s.auditSTSXMLDenial(r, action, user.Username, federationDenialReason(err))
		writeSTSError(w, http.StatusForbidden, "AccessDenied", "Access denied.")
		return nil, false
	}

	duration, err := stsDurationSeconds(r)
	if err != nil {
		writeSTSError(w, http.StatusBadRequest, "ValidationError", err.Error())
		return nil, false
	}

	session, err := s.authManager.IssueSTSSession(r.Context(), user.ID, duration, r.PostForm.Get("Policy"))
	if err != nil {
		s.writeSTSIssuanceError(w, err)
		return nil, false
	}

	s.afterSTSXMLIssue(r, user, session, action, providerID)
	return session, true
}

// afterSTSXMLIssue performs the post-issuance side effects every action shares.
func (s *Server) afterSTSXMLIssue(r *http.Request, user *auth.User, session *auth.STSSession, action, providerID string) {
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
			"method":      "aws-sts",
			"sts_action":  action,
			"provider_id": providerID,
			"expires_at":  session.ExpiresAt,
		},
	})
}

// auditSTSXMLDenial records a rejection. As on the JSON endpoints the caller
// only ever sees an opaque error; the reason stays in the audit log.
func (s *Server) auditSTSXMLDenial(r *http.Request, action, subject, reason string) {
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
			"method":     "aws-sts",
			"sts_action": action,
			"reason":     reason,
		},
	})

	logrus.WithFields(logrus.Fields{"sts_action": action, "subject": subject, "reason": reason}).
		Warn("STS XML request denied")
}

// writeSTSIssuanceError maps the shared issuance errors to STS error codes.
func (s *Server) writeSTSIssuanceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrSTSInvalidPolicy):
		writeSTSError(w, http.StatusBadRequest, "MalformedPolicyDocument", err.Error())
	case errors.Is(err, auth.ErrSTSInvalidDuration):
		writeSTSError(w, http.StatusBadRequest, "ValidationError",
			"DurationSeconds must be between 900 and the configured maximum.")
	case errors.Is(err, auth.ErrSTSTooManySessions):
		writeSTSError(w, http.StatusTooManyRequests, "Throttling",
			"Too many active sessions. Revoke unused sessions before issuing new ones.")
	case errors.Is(err, auth.ErrUserInactive):
		writeSTSError(w, http.StatusForbidden, "AccessDenied", "Access denied.")
	default:
		writeSTSError(w, http.StatusInternalServerError, "InternalFailure", err.Error())
	}
}

// stsResolveOAuthProviderID returns the OAuth provider a web-identity request
// should be validated against.
func (s *Server) stsResolveOAuthProviderID(r *http.Request) (string, error) {
	if explicit := r.PostForm.Get("ProviderId"); explicit != "" {
		return explicit, nil
	}

	providers, err := s.idpManager.ListActiveOAuthProviders(r.Context())
	if err != nil || len(providers) == 0 {
		return "", errors.New("no active OAuth identity provider is configured")
	}
	if len(providers) > 1 {
		return "", errors.New("several OAuth identity providers are configured; pass ProviderId to select one")
	}
	return providers[0].ID, nil
}

// stsResolveLDAPProviderID reads the provider an LDAP user is linked to from the
// local user record.
func (s *Server) stsResolveLDAPProviderID(r *http.Request, username string) (string, error) {
	user, err := s.authManager.GetUser(r.Context(), username)
	if err != nil || user == nil || !strings.HasPrefix(user.AuthProvider, "ldap:") {
		return "", errors.New("user is not linked to an LDAP provider")
	}

	providerID := strings.TrimPrefix(user.AuthProvider, "ldap:")
	provider, err := s.idpManager.GetProvider(r.Context(), providerID)
	if err != nil || provider == nil || provider.Type != idp.TypeLDAP || provider.Status != idp.StatusActive {
		return "", errors.New("the user's LDAP provider is unavailable or inactive")
	}
	return providerID, nil
}

// stsDurationSeconds reads the optional DurationSeconds parameter. Range checks
// belong to IssueSTSSession — only the format is validated here.
func stsDurationSeconds(r *http.Request) (int, error) {
	raw := r.PostForm.Get("DurationSeconds")
	if raw == "" {
		return 0, nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 0 {
		return 0, errors.New("DurationSeconds must be a positive integer")
	}
	return seconds, nil
}

// stsRequestSignedWithTemporaryCredential reports whether the Authorization
// header names an ASIA key — the same discriminator signature validation uses.
func stsRequestSignedWithTemporaryCredential(r *http.Request) bool {
	header := r.Header.Get("Authorization")
	idx := strings.Index(header, "Credential=")
	if idx < 0 {
		return false
	}
	credential := header[idx+len("Credential="):]
	if slash := strings.Index(credential, "/"); slash >= 0 {
		credential = credential[:slash]
	}
	return auth.IsSTSAccessKey(strings.TrimSpace(credential))
}

func actionName(asAssumeRole bool) string {
	if asAssumeRole {
		return "AssumeRole"
	}
	return "GetSessionToken"
}

func stsCredentialsOf(session *auth.STSSession) stsCredentials {
	return stsCredentials{
		AccessKeyID:     session.TempAccessKeyID,
		SecretAccessKey: session.SecretAccessKey,
		SessionToken:    session.SessionToken,
		Expiration:      time.Unix(session.ExpiresAt, 0).UTC().Format(time.RFC3339),
	}
}

// assumedRoleUserOf fills the AssumedRoleUser element SDKs expect. MaxIOFS has
// no roles to assume, so it identifies the user the credentials belong to.
func assumedRoleUserOf(user *auth.User) stsAssumedRoleUser {
	return stsAssumedRoleUser{
		Arn:           "arn:aws:sts:::user/" + user.Username,
		AssumedRoleID: user.ID,
	}
}

func requestIDOf(r *http.Request) string {
	if id := r.Header.Get("X-Amz-Request-Id"); id != "" {
		return id
	}
	if id, ok := r.Context().Value("request_id").(string); ok && id != "" {
		return id
	}
	return strconv.FormatInt(time.Now().UnixNano(), 16)
}

func writeSTSXML(w http.ResponseWriter, doc interface{}) {
	body, err := xml.Marshal(doc)
	if err != nil {
		writeSTSError(w, http.StatusInternalServerError, "InternalFailure", "Failed to encode the response.")
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(body)
}

func writeSTSError(w http.ResponseWriter, status int, code, message string) {
	resp := &stsErrorResponse{Xmlns: stsXMLNamespace}
	resp.Error.Type = "Sender"
	if status >= 500 {
		resp.Error.Type = "Receiver"
	}
	resp.Error.Code = code
	resp.Error.Message = message

	body, err := xml.Marshal(resp)
	if err != nil {
		http.Error(w, message, status)
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(body)
}

// stsPayloadHashMiddleware supplies the payload hash for AWS STS requests.
func stsPayloadHashMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/" ||
			r.Header.Get("X-Amz-Content-Sha256") != "" ||
			!strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
			next.ServeHTTP(w, r)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, stsMaxFormBody+1))
		if err != nil {
			writeSTSError(w, http.StatusBadRequest, "InvalidAction", "The request body could not be read.")
			return
		}
		if len(body) > stsMaxFormBody {
			writeSTSError(w, http.StatusBadRequest, "ValidationError", "The request body is too large for an STS request.")
			return
		}
		_ = r.Body.Close()

		sum := sha256.Sum256(body)
		r.Header.Set("X-Amz-Content-Sha256", hex.EncodeToString(sum[:]))
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))

		next.ServeHTTP(w, r)
	})
}
