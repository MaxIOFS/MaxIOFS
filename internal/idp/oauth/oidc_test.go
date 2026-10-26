package oauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeIssuer is an OpenID Connect provider for tests: discovery, keys, a token
// endpoint that signs ID tokens, refreshes as its refresh tokens say, and a
// userinfo endpoint.
type fakeIssuer struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	groups []string
}

const fakeClientID = "maxiofs"

func startIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &fakeIssuer{key: key, groups: []string{"admins"}}
	mux := http.NewServeMux()
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	writeJSON := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"issuer": f.server.URL, "authorization_endpoint": f.server.URL + "/auth",
			"token_endpoint": f.server.URL + "/token", "userinfo_endpoint": f.server.URL + "/userinfo",
			"jwks_uri": f.server.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		enc := base64.RawURLEncoding
		writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
			"n": enc.EncodeToString(key.N.Bytes()), "e": enc.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			aud := fakeClientID
			if r.Form.Get("code") == "for-another-client" {
				aud = "another-client"
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"access_token": "at-1", "token_type": "Bearer", "refresh_token": "rt-1", "expires_in": 300,
				"id_token": f.sign(t, f.key, map[string]any{"aud": aud, "sub": "sub-1", "sid": "sid-1", "exp": time.Now().Add(time.Hour).Unix()}),
			})
		case "refresh_token":
			switch r.Form.Get("refresh_token") {
			case "rt-1":
				writeJSON(w, http.StatusOK, map[string]any{"access_token": "at-2", "token_type": "Bearer", "refresh_token": "rt-2", "expires_in": 300})
			case "rt-userinfo-refuses":
				writeJSON(w, http.StatusOK, map[string]any{"access_token": "at-refused", "token_type": "Bearer", "expires_in": 300})
			case "rt-wrong-client":
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_client"})
			case "rt-broken":
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "temporarily_unavailable"})
			default:
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "Session not active"})
			}
		}
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer at-1", "Bearer at-2":
			writeJSON(w, http.StatusOK, map[string]any{"sub": "sub-1", "email": "alice@example.org", "name": "Alice", "groups": f.groups})
		default:
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid_token"})
		}
	})
	return f
}

// sign signs claims for the client as the issuer would, with key.
func (f *fakeIssuer) sign(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	full := jwt.MapClaims{"iss": f.server.URL, "iat": time.Now().Unix()}
	for k, v := range claims {
		if v == nil {
			delete(full, k)
			continue
		}
		full[k] = v
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, full)
	token.Header["kid"] = "k1"
	signed, err := token.SignedString(key)
	require.NoError(t, err)
	return signed
}

func (f *fakeIssuer) provider() *Provider {
	u := f.server.URL
	return NewProvider(&idp.OAuth2Config{
		Preset: "custom", ClientID: fakeClientID, ClientSecret: "secret", RedirectURI: "http://localhost/callback",
		AuthURL: u + "/auth", TokenURL: u + "/token", UserInfoURL: u + "/userinfo", ClaimGroups: "groups", Issuer: u,
	})
}

// An OpenID Connect sign-in keeps what the provider gave to ask about it
// again: the subject and the session of the ID token, and the refresh token.
// An ID token for another client is refused.
func TestAnOIDCSignInKeepsItsGrant(t *testing.T) {
	f := startIssuer(t)
	ctx := context.Background()
	user, err := f.provider().ExchangeCode(ctx, "good")
	require.NoError(t, err)
	require.NotNil(t, user.Grant)
	assert.Equal(t, idp.Grant{Subject: "sub-1", Session: "sid-1", RefreshToken: "rt-1"}, *user.Grant)
	assert.Equal(t, []string{"admins"}, user.Groups)

	_, err = f.provider().ExchangeCode(ctx, "for-another-client")
	assert.Error(t, err, "an ID token for another client")
}

// A sign-in is renewed with the refresh token: it stands with the groups the
// provider gives now and the new refresh token. A refresh the provider refuses
// as invalid, or a userinfo that refuses the new token, ends it; a provider
// that fails, or that refuses the client, ends nothing.
func TestRevalidationRenewsTheSignInOrEndsIt(t *testing.T) {
	f := startIssuer(t)
	ctx := context.Background()
	p := f.provider()

	f.groups = []string{"devs"}
	user, err := p.Revalidate(ctx, idp.Grant{Subject: "sub-1", Session: "sid-1", RefreshToken: "rt-1"})
	require.NoError(t, err)
	assert.Equal(t, idp.Grant{Subject: "sub-1", Session: "sid-1", RefreshToken: "rt-2"}, *user.Grant)
	assert.Equal(t, []string{"devs"}, user.Groups)

	_, err = p.Revalidate(ctx, idp.Grant{Subject: "sub-1", RefreshToken: "rt-ended"})
	assert.ErrorIs(t, err, idp.ErrSessionEnded, "a refused refresh")
	_, err = p.Revalidate(ctx, idp.Grant{Subject: "sub-1", RefreshToken: "rt-userinfo-refuses"})
	assert.ErrorIs(t, err, idp.ErrSessionEnded, "a refused userinfo")

	_, err = p.Revalidate(ctx, idp.Grant{Subject: "sub-1", RefreshToken: "rt-broken"})
	assert.ErrorIs(t, err, idp.ErrUnavailable, "a provider that fails")
	assert.NotErrorIs(t, err, idp.ErrSessionEnded, "a provider that fails")
	_, err = p.Revalidate(ctx, idp.Grant{Subject: "sub-1", RefreshToken: "rt-wrong-client"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, idp.ErrSessionEnded, "a client refused: the configuration, not the user")

	user, err = p.Revalidate(ctx, idp.Grant{Subject: "sub-1"})
	assert.NoError(t, err)
	assert.Nil(t, user, "nothing to ask with")
}

// A back-channel logout token is taken as the standard says: signed by the
// issuer for this client, with the logout event and an ID not seen before,
// recent, without a nonce, naming a subject or a session.
func TestALogoutTokenIsCheckedAsTheStandardSays(t *testing.T) {
	f := startIssuer(t)
	ctx := context.Background()
	p := f.provider()
	event := map[string]any{backchannelLogoutEvent: map[string]any{}}
	id := 0
	logout := func(changes map[string]any) string {
		id++
		claims := map[string]any{"aud": fakeClientID, "sub": "sub-1", "sid": "sid-1", "events": event, "jti": "jti-" + strings.Repeat("x", id)}
		for k, v := range changes {
			claims[k] = v
		}
		return f.sign(t, f.key, claims)
	}

	valid := logout(nil)
	subject, session, err := p.VerifyLogoutToken(ctx, valid)
	require.NoError(t, err)
	assert.Equal(t, "sub-1", subject)
	assert.Equal(t, "sid-1", session)
	_, _, err = p.VerifyLogoutToken(ctx, valid)
	assert.Error(t, err, "sent again")

	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	refused := map[string]string{
		"for another client":    logout(map[string]any{"aud": "another-client"}),
		"without the event":     logout(map[string]any{"events": map[string]any{}}),
		"with a nonce":          logout(map[string]any{"nonce": "n"}),
		"without an ID":         logout(map[string]any{"jti": nil}),
		"naming nobody":         logout(map[string]any{"sub": nil, "sid": nil}),
		"too old":               logout(map[string]any{"iat": time.Now().Add(-time.Hour).Unix()}),
		"signed by another key": f.sign(t, other, map[string]any{"aud": fakeClientID, "sub": "sub-1", "events": event, "jti": "jti-other"}),
		"from another issuer":   logout(map[string]any{"iss": "https://elsewhere.example"}),
	}
	for name, token := range refused {
		_, _, err := p.VerifyLogoutToken(ctx, token)
		assert.Error(t, err, name)
	}
}
