package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gorilla/mux"
	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/idp"
	_ "github.com/maxiofs/maxiofs/internal/idp/ldap"
	_ "github.com/maxiofs/maxiofs/internal/idp/oauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A user signs in by its name, whatever its ID: an import, a group sync and
// an SSO sign-up make users whose ID is not their name.
func TestAUserWhoseIDIsNotItsNameSignsIn(t *testing.T) {
	server := newClusterTestNode(t)
	router := mux.NewRouter()
	server.setupConsoleAPIRoutes(router)
	require.NoError(t, server.authManager.CreateUser(context.Background(), &auth.User{
		ID: "user-1a2b3c4d", Username: "carla", Password: "Secret123!", Status: auth.UserStatusActive, Roles: []string{"admin"}}))

	assert.Equal(t, http.StatusOK, signInFrom(t, router, "198.51.100.40", "carla", "Secret123!"))
	assert.Equal(t, http.StatusUnauthorized, signInFrom(t, router, "198.51.100.40", "carla", "wrong"))
}

// A sign-in its SSO provider cannot answer goes back to the login page with
// the provider unavailable, not with a failed sign-in.
func TestASignInTheProviderCannotAnswerSaysItIsUnavailable(t *testing.T) {
	server := newClusterTestNode(t)
	router := mux.NewRouter()
	server.setupConsoleAPIRoutes(router)

	gone, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	base := "http://" + gone.Addr().String()
	require.NoError(t, gone.Close())
	provider := &idp.IdentityProvider{Name: "SSO", Type: idp.TypeOAuth2, Status: idp.StatusActive,
		Config: idp.ProviderConfig{OAuth2: &idp.OAuth2Config{Preset: "custom", ClientID: "client", ClientSecret: "secret",
			AuthURL: base + "/auth", TokenURL: base + "/token", UserInfoURL: base + "/userinfo", RedirectURI: "http://localhost/callback"}}}
	require.NoError(t, server.idpManager.CreateProvider(context.Background(), provider))

	req := httptest.NewRequest("GET", "/auth/oauth/callback?code=c&state="+url.QueryEscape(provider.ID+":csrf"), nil)
	req.AddCookie(&http.Cookie{Name: "oauth_state", Value: "csrf"})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusFound, rr.Code)
	assert.Contains(t, rr.Header().Get("Location"), "error=provider_unavailable")
}

// A sign-in its directory cannot answer is the directory's failure, not the
// user's: it is answered 503, and locks neither the account nor the address.
func TestAnUnavailableDirectoryLocksNoAccount(t *testing.T) {
	server := newClusterTestNode(t)
	router := mux.NewRouter()
	server.setupConsoleAPIRoutes(router)
	ctx := context.Background()

	gone, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := gone.Addr().(*net.TCPAddr).Port
	require.NoError(t, gone.Close())
	provider := &idp.IdentityProvider{Name: "Directory", Type: idp.TypeLDAP, Status: idp.StatusActive,
		Config: idp.ProviderConfig{LDAP: &idp.LDAPConfig{Host: "127.0.0.1", Port: port, Security: "none",
			BindDN: "cn=svc,dc=example,dc=org", BindPassword: "svc", BaseDN: "dc=example,dc=org"}}}
	require.NoError(t, server.idpManager.CreateProvider(ctx, provider))
	require.NoError(t, server.authManager.CreateUser(ctx, &auth.User{
		ID: "user-5e6f7a8b", Username: "ana", Status: auth.UserStatusActive, Roles: []string{"admin"},
		AuthProvider: "ldap:" + provider.ID, ExternalID: "uid=ana,ou=people,dc=example,dc=org"}))

	const address = "198.51.100.41"
	for i := range 8 {
		require.Equal(t, http.StatusServiceUnavailable, signInFrom(t, router, address, "ana", "Ana.2026"), "sign-in %d", i+1)
	}
	locked, _, err := server.authManager.IsAccountLocked(ctx, "user-5e6f7a8b")
	require.NoError(t, err)
	assert.False(t, locked, "the account")
	assert.True(t, server.authManager.CheckRateLimit(address), "the address")
}
