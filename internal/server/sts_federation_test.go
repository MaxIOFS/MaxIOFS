package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// federationClientIPs hands each test its own source IP.
var federationClientIPs atomic.Uint32

// postFederation posts a body to a federation endpoint with NO Authorization
// header — these endpoints must be reachable without a console session.
func postFederation(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return postFederationFrom(t, path, body,
		fmt.Sprintf("10.77.%d.%d:1234",
			federationClientIPs.Add(1)/250, federationClientIPs.Load()%250))
}

func postFederationFrom(t *testing.T, path, body, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	server := getSharedServer()

	req := httptest.NewRequest(http.MethodPost, "/api/v1"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()

	switch path {
	case "/sts/ldap-identity":
		server.handleSTSLDAPIdentity(rec, req)
	case "/sts/web-identity":
		server.handleSTSWebIdentity(rec, req)
	default:
		t.Fatalf("unknown federation path %q", path)
	}
	return rec
}

// setFederationEnabled flips the opt-in setting and restores it afterwards.
func setFederationEnabled(t *testing.T, enabled bool) {
	t.Helper()
	server := getSharedServer()

	value := "false"
	if enabled {
		value = "true"
	}
	require.NoError(t, server.settingsManager.Set("security.sts_federation_enabled", value))
	t.Cleanup(func() {
		_ = server.settingsManager.Set("security.sts_federation_enabled", "false")
	})
}

func TestSTSFederation_DisabledByDefault(t *testing.T) {
	// The endpoints accept credentials without a session, so an admin must opt
	// in before they answer at all.
	for _, path := range []string{"/sts/ldap-identity", "/sts/web-identity"} {
		t.Run(path, func(t *testing.T) {
			rec := postFederation(t, path, `{"providerId":"idp-1","username":"u","password":"p","token":"t"}`)

			assert.Equal(t, http.StatusForbidden, rec.Code)
			// Not 401: reaching a 403 proves the route skipped JWT auth. If the
			// public-path wiring regressed, this would be 401 instead.
			assert.Contains(t, rec.Body.String(), "disabled")
		})
	}
}

func TestSTSFederation_RequiresProviderID(t *testing.T) {
	setFederationEnabled(t, true)

	for _, path := range []string{"/sts/ldap-identity", "/sts/web-identity"} {
		t.Run(path, func(t *testing.T) {
			rec := postFederation(t, path, `{"username":"u","password":"p","token":"t"}`)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), "providerId")
		})
	}
}

func TestSTSFederation_RejectsMalformedBody(t *testing.T) {
	setFederationEnabled(t, true)

	rec := postFederation(t, "/sts/ldap-identity", `{ not json`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSTSLDAPIdentity_RequiresCredentials(t *testing.T) {
	setFederationEnabled(t, true)

	rec := postFederation(t, "/sts/ldap-identity", `{"providerId":"idp-1"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "password")
}

func TestSTSWebIdentity_RequiresToken(t *testing.T) {
	setFederationEnabled(t, true)

	rec := postFederation(t, "/sts/web-identity", `{"providerId":"idp-1"}`)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "token")
}

func TestSTSFederation_UnknownProvider(t *testing.T) {
	setFederationEnabled(t, true)

	rec := postFederation(t, "/sts/ldap-identity",
		`{"providerId":"idp-does-not-exist","username":"u","password":"p"}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestSTSFederation_RateLimited(t *testing.T) {
	setFederationEnabled(t, true)

	// A single client gets the console login budget and no more: these are the
	// only STS endpoints that accept credentials without a session, so brute
	// force must cost the same here as against /auth/login.
	const from = "10.99.99.99:5555"
	body := `{"providerId":"idp-does-not-exist","username":"u","password":"p"}`

	sawLimit := false
	for i := 0; i < 12; i++ {
		if postFederationFrom(t, "/sts/ldap-identity", body, from).Code == http.StatusTooManyRequests {
			sawLimit = true
			break
		}
	}
	assert.True(t, sawLimit, "repeated attempts from one IP must hit the rate limiter")
}

func TestSTSFederationRequest_SessionPolicyAcceptsObjectOrString(t *testing.T) {
	document := `{"Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`

	t.Run("object", func(t *testing.T) {
		var req stsFederationRequest
		require.NoError(t, json.Unmarshal(
			[]byte(`{"providerId":"p","sessionPolicy":`+document+`}`), &req))

		// Round-trips to a document the policy parser can read.
		var parsed map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(req.sessionPolicyDocument()), &parsed))
		assert.Contains(t, parsed, "Statement")
	})

	t.Run("string", func(t *testing.T) {
		encoded, err := json.Marshal(document)
		require.NoError(t, err)

		var req stsFederationRequest
		require.NoError(t, json.Unmarshal(
			[]byte(`{"providerId":"p","sessionPolicy":`+string(encoded)+`}`), &req))
		assert.Equal(t, document, req.sessionPolicyDocument())
	})

	t.Run("absent", func(t *testing.T) {
		var req stsFederationRequest
		require.NoError(t, json.Unmarshal([]byte(`{"providerId":"p"}`), &req))
		assert.Empty(t, req.sessionPolicyDocument())
	})
}
