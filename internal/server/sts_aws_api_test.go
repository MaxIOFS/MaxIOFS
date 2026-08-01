package server

// Tests for the AWS STS XML surface.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// postSTSForm sends an STS query-protocol request to the AWS surface. user is
// put in context as the S3 auth middleware would after verifying a signature.
func postSTSForm(t *testing.T, form url.Values, user *auth.User) *httptest.ResponseRecorder {
	t.Helper()
	server := getSharedServer()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "10.55.0.1:9999"
	if user != nil {
		req = req.WithContext(context.WithValue(req.Context(), "user", user))
	}

	rec := httptest.NewRecorder()
	server.handleAWSSTSRequest(rec, req)
	return rec
}

// stsErrorCodeOf extracts the Error/Code from an STS XML error document.
func stsErrorCodeOf(t *testing.T, body string) string {
	t.Helper()
	var doc stsErrorResponse
	require.NoError(t, xml.Unmarshal([]byte(body), &doc), "response is not an STS error document: %s", body)
	return doc.Error.Code
}

func TestSTSXML_MissingAndUnknownAction(t *testing.T) {
	rec := postSTSForm(t, url.Values{}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "MissingAction", stsErrorCodeOf(t, rec.Body.String()))

	rec = postSTSForm(t, url.Values{"Action": {"DeleteEverything"}}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "InvalidAction", stsErrorCodeOf(t, rec.Body.String()))
}

func TestSTSXML_GetSessionTokenRequiresSignature(t *testing.T) {
	// No user in context means the request carried no valid signature.
	rec := postSTSForm(t, url.Values{"Action": {"GetSessionToken"}}, nil)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "AccessDenied", stsErrorCodeOf(t, rec.Body.String()))
}

func TestSTSXML_GetSessionTokenIssuesCredentials(t *testing.T) {
	server := getSharedServer()
	user := &auth.User{
		ID:       "user-sts-xml",
		Username: "sts-xml-tester",
		Status:   auth.UserStatusActive,
		Roles:    []string{"admin"},
	}
	require.NoError(t, server.authManager.CreateUser(t.Context(), user))
	t.Cleanup(func() { _ = server.authManager.DeleteUser(t.Context(), user.ID) })

	rec := postSTSForm(t, url.Values{
		"Action":          {"GetSessionToken"},
		"DurationSeconds": {"3600"},
	}, user)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var doc stsGetSessionTokenResponse
	require.NoError(t, xml.Unmarshal(rec.Body.Bytes(), &doc))

	creds := doc.Result.Credentials
	assert.True(t, strings.HasPrefix(creds.AccessKeyID, auth.STSKeyPrefix),
		"temporary key must carry the ASIA prefix, got %q", creds.AccessKeyID)
	assert.NotEmpty(t, creds.SecretAccessKey)
	assert.NotEmpty(t, creds.SessionToken)
	assert.NotEmpty(t, creds.Expiration)

	// The credentials must actually work: the whole point of the XML surface is
	// that it reaches the same issuance path as the console API.
	_, _, err := server.authManager.ResolveSTSSessionSecret(t.Context(), creds.AccessKeyID, creds.SessionToken)
	assert.NoError(t, err)
}

func TestSTSXML_AssumeRoleIsAnAliasAndIgnoresRoleArn(t *testing.T) {
	server := getSharedServer()
	user := &auth.User{
		ID:       "user-sts-xml-role",
		Username: "sts-xml-role",
		Status:   auth.UserStatusActive,
		Roles:    []string{"admin"},
	}
	require.NoError(t, server.authManager.CreateUser(t.Context(), user))
	t.Cleanup(func() { _ = server.authManager.DeleteUser(t.Context(), user.ID) })

	rec := postSTSForm(t, url.Values{
		"Action":          {"AssumeRole"},
		"RoleArn":         {"arn:aws:iam::123456789012:role/anything-at-all"},
		"RoleSessionName": {"job"},
	}, user)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var doc stsAssumeRoleResponse
	require.NoError(t, xml.Unmarshal(rec.Body.Bytes(), &doc))
	assert.True(t, strings.HasPrefix(doc.Result.Credentials.AccessKeyID, auth.STSKeyPrefix))

	// The credential belongs to the authenticated user, not to the requested
	// role — MaxIOFS has no role ARNs, so RoleArn cannot widen anything.
	assert.Contains(t, doc.Result.AssumedRoleUser.Arn, user.Username)
	assert.Equal(t, user.ID, doc.Result.AssumedRoleUser.AssumedRoleID)
}

func TestSTSXML_RejectsInvalidSessionPolicy(t *testing.T) {
	server := getSharedServer()
	user := &auth.User{
		ID:       "user-sts-xml-policy",
		Username: "sts-xml-policy",
		Status:   auth.UserStatusActive,
		Roles:    []string{"admin"},
	}
	require.NoError(t, server.authManager.CreateUser(t.Context(), user))
	t.Cleanup(func() { _ = server.authManager.DeleteUser(t.Context(), user.ID) })

	rec := postSTSForm(t, url.Values{
		"Action": {"GetSessionToken"},
		// Condition is not enforceable yet, so it is rejected at issuance
		// rather than silently ignored — the same rule the JSON API applies.
		"Policy": {`{"Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"*",
			"Condition":{"IpAddress":{"aws:SourceIp":"10.0.0.0/8"}}}]}`},
	}, user)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "MalformedPolicyDocument", stsErrorCodeOf(t, rec.Body.String()))
}

func TestSTSXML_RejectsInvalidDuration(t *testing.T) {
	rec := postSTSForm(t, url.Values{
		"Action":          {"GetSessionToken"},
		"DurationSeconds": {"not-a-number"},
	}, &auth.User{ID: "u", Username: "u", Status: auth.UserStatusActive, Roles: []string{"admin"}})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "ValidationError", stsErrorCodeOf(t, rec.Body.String()))
}

func TestSTSXML_TemporaryCredentialsCannotMintMore(t *testing.T) {
	// A leaked session that could call GetSessionToken would renew itself
	// forever and make expiry meaningless.
	server := getSharedServer()
	user := &auth.User{
		ID:       "user-sts-xml-renew",
		Username: "sts-xml-renew",
		Status:   auth.UserStatusActive,
		Roles:    []string{"admin"},
	}
	require.NoError(t, server.authManager.CreateUser(t.Context(), user))
	t.Cleanup(func() { _ = server.authManager.DeleteUser(t.Context(), user.ID) })

	req := httptest.NewRequest(http.MethodPost, "/",
		strings.NewReader(url.Values{"Action": {"GetSessionToken"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization",
		"AWS4-HMAC-SHA256 Credential=ASIAEXAMPLETEMPKEY01/20260730/us-east-1/s3/aws4_request, "+
			"SignedHeaders=host;x-amz-date, Signature=deadbeef")
	req = req.WithContext(context.WithValue(req.Context(), "user", user))

	rec := httptest.NewRecorder()
	server.handleAWSSTSRequest(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "Temporary credentials cannot be used")
}

func TestSTSXML_FederatedActionsRespectTheOptInSetting(t *testing.T) {
	// Disabled by default, exactly like the JSON federation endpoints.
	for _, form := range []url.Values{
		{"Action": {"AssumeRoleWithWebIdentity"}, "WebIdentityToken": {"tok"}},
		{"Action": {"AssumeRoleWithLDAPIdentity"}, "LDAPUsername": {"u"}, "LDAPPassword": {"p"}},
	} {
		t.Run(form.Get("Action"), func(t *testing.T) {
			rec := postSTSForm(t, form, nil)
			assert.Equal(t, http.StatusForbidden, rec.Code)
			assert.Equal(t, "AccessDenied", stsErrorCodeOf(t, rec.Body.String()))
			assert.Contains(t, rec.Body.String(), "federation is disabled")
		})
	}
}

func TestSTSXML_WebIdentityRequiresToken(t *testing.T) {
	setFederationEnabled(t, true)

	rec := postSTSForm(t, url.Values{"Action": {"AssumeRoleWithWebIdentity"}}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "ValidationError", stsErrorCodeOf(t, rec.Body.String()))
}

func TestSTSXML_LDAPIdentityRequiresCredentials(t *testing.T) {
	setFederationEnabled(t, true)

	rec := postSTSForm(t, url.Values{"Action": {"AssumeRoleWithLDAPIdentity"}}, nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "ValidationError", stsErrorCodeOf(t, rec.Body.String()))
}

// --- Payload hash middleware ---

func TestSTSPayloadHashMiddleware_SuppliesHashForSTSRequests(t *testing.T) {
	body := url.Values{"Action": {"GetSessionToken"}}.Encode()
	want := sha256.Sum256([]byte(body))

	var seenHash, seenBody string
	handler := stsPayloadHashMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHash = r.Header.Get("X-Amz-Content-Sha256")
		read, _ := io.ReadAll(r.Body)
		seenBody = string(read)
	}))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// AWS STS clients sign the real body hash but send no X-Amz-Content-Sha256;
	// without this the verifier would use UNSIGNED-PAYLOAD and reject them.
	assert.Equal(t, hex.EncodeToString(want[:]), seenHash)
	// The body must still be readable by the handler.
	assert.Equal(t, body, seenBody)
}

func TestSTSPayloadHashMiddleware_LeavesOtherRequestsAlone(t *testing.T) {
	cases := map[string]*http.Request{
		"not a POST":   httptest.NewRequest(http.MethodGet, "/", nil),
		"not the root": httptest.NewRequest(http.MethodPost, "/bucket/key", strings.NewReader("data")),
		"not a form":   httptest.NewRequest(http.MethodPost, "/", strings.NewReader("data")),
		"hash already set": func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("Action=GetSessionToken"))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
			return r
		}(),
	}

	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			before := req.Header.Get("X-Amz-Content-Sha256")

			var after string
			handler := stsPayloadHashMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				after = r.Header.Get("X-Amz-Content-Sha256")
			}))
			handler.ServeHTTP(httptest.NewRecorder(), req)

			assert.Equal(t, before, after, "the middleware must only touch STS-shaped requests")
		})
	}
}
