package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statuses is each check's name and status.
func statuses(checks []idp.Check) []string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.Name + " " + c.Status
	}
	return out
}

// problems is the details of the checks that did not pass.
func problems(checks []idp.Check) string {
	out := ""
	for _, c := range checks {
		if c.Status != idp.CheckPassed {
			out += c.Detail + "\n"
		}
	}
	return out
}

// The connection test checks the provider without a user: its OpenID
// configuration and keys, its sign-in page for the client and the redirect
// URI, the client's secret by a code no one issued, which a provider that
// takes the secret refuses as invalid_grant, and the userinfo endpoint.
func TestTheConnectionTestChecksTheProvider(t *testing.T) {
	ctx := context.Background()
	f := startIssuer(t)
	u := f.server.URL
	all := []string{"Discovery passed", "Sign-in page passed", "Client secret passed", "Userinfo passed"}

	checks := f.provider().TestConnection(ctx)
	require.Equal(t, all, statuses(checks))
	assert.Contains(t, checks[0].Detail, "1 signing key")
	assert.Contains(t, checks[1].Detail, fakeRedirectURI)

	for name, tc := range map[string]struct {
		change func(*idp.OAuth2Config)
		want   []string
		detail string
	}{
		"wrong secret": {func(c *idp.OAuth2Config) { c.ClientSecret = "nope" },
			[]string{"Discovery passed", "Sign-in page passed", "Client secret failed", "Userinfo passed"}, "unauthorized_client"},
		"unknown client": {func(c *idp.OAuth2Config) { c.ClientID = "nobody" },
			[]string{"Discovery passed", "Sign-in page failed", "Client secret failed", "Userinfo passed"}, "invalid_client"},
		"redirect URI not registered": {func(c *idp.OAuth2Config) { c.RedirectURI = "http://elsewhere/callback" },
			[]string{"Discovery passed", "Sign-in page failed", "Client secret passed", "Userinfo passed"}, "http://elsewhere/callback"},
		"scope refused": {func(c *idp.OAuth2Config) { c.Scopes = []string{"openid", "nope"} },
			[]string{"Discovery passed", "Sign-in page failed", "Client secret passed", "Userinfo passed"}, "invalid_scope"},
		"another issuer": {func(c *idp.OAuth2Config) { c.Issuer = u + "/other" },
			[]string{"Discovery failed", "Sign-in page passed", "Client secret passed", "Userinfo passed"}, u + "/other"},
		"no sign-in page": {func(c *idp.OAuth2Config) { c.AuthURL = u + "/nothing" },
			[]string{"Discovery passed", "Sign-in page failed", "Client secret passed", "Userinfo passed"}, "(404): check the authorization URL"},
		"no signing keys": {func(c *idp.OAuth2Config) { c.Issuer = u + "/keyless" },
			[]string{"Discovery failed", "Sign-in page passed", "Client secret passed", "Userinfo passed"}, "No signing keys"},
		"no token endpoint": {func(c *idp.OAuth2Config) { c.TokenURL = u + "/nothing" },
			[]string{"Discovery passed", "Sign-in page passed", "Client secret failed", "Userinfo passed"}, "404"},
		"token endpoint down": {func(c *idp.OAuth2Config) { c.TokenURL = u + "/answer/503/temporarily_unavailable" },
			[]string{"Discovery passed", "Sign-in page passed", "Client secret failed", "Userinfo passed"}, "503"},
		"token endpoint answering otherwise": {func(c *idp.OAuth2Config) { c.TokenURL = u + "/answer/400/unsupported_grant_type" },
			[]string{"Discovery passed", "Sign-in page passed", "Client secret warning", "Userinfo passed"}, "unsupported_grant_type"},
		"no userinfo endpoint": {func(c *idp.OAuth2Config) { c.UserInfoURL = u + "/nothing" },
			[]string{"Discovery passed", "Sign-in page passed", "Client secret passed", "Userinfo failed"}, "404"},
		"userinfo down": {func(c *idp.OAuth2Config) { c.UserInfoURL = u + "/answer/500/server_error" },
			[]string{"Discovery passed", "Sign-in page passed", "Client secret passed", "Userinfo failed"}, "500"},
		"userinfo without a token": {func(c *idp.OAuth2Config) { c.UserInfoURL = u + "/answer/200/none" },
			[]string{"Discovery passed", "Sign-in page passed", "Client secret passed", "Userinfo warning"}, "200"},
	} {
		cfg := f.config()
		tc.change(cfg)
		checks := NewProvider(cfg).TestConnection(ctx)
		require.Equal(t, tc.want, statuses(checks), name)
		assert.Contains(t, problems(checks), tc.detail, name)
	}

	// Without an issuer there is no OpenID configuration to read.
	cfg := f.config()
	cfg.Issuer = ""
	assert.Equal(t, all[1:], statuses(NewProvider(cfg).TestConnection(ctx)))

	// A token endpoint that takes the secret one way only; one that refuses
	// the header as malformed still refuses a wrong secret in the form.
	for _, way := range []string{"header", "params"} {
		f.secretIn.Store(way)
		assert.Equal(t, all, statuses(f.provider().TestConnection(ctx)), way)
		cfg := f.config()
		cfg.ClientSecret = "nope"
		checks := NewProvider(cfg).TestConnection(ctx)
		assert.Equal(t, "Client secret failed", statuses(checks)[2], way)
		assert.Contains(t, checks[2].Detail, "unauthorized_client", way)
	}
	f.secretIn.Store("")

	// Without the fields a sign-in needs the provider is not asked.
	cfg = f.config()
	cfg.ClientSecret, cfg.UserInfoURL = "", ""
	checks = NewProvider(cfg).TestConnection(ctx)
	require.Equal(t, []string{"Configuration failed"}, statuses(checks))
	assert.Contains(t, checks[0].Detail, "Client Secret, User Info URL")
}

// A provider that takes the requests and does not answer, or that is not
// there, fails each check within the timeout.
func TestTheConnectionTestOfAnUnansweringProviderEndsInTime(t *testing.T) {
	former := providerTimeout
	providerTimeout = 500 * time.Millisecond
	defer func() { providerTimeout = former }()
	ctx := context.Background()

	release := make(chan struct{})
	frozen := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer frozen.Close()
	defer close(release)
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()

	for name, url := range map[string]string{"frozen": frozen.URL, "gone": goneURL} {
		p := NewProvider(&idp.OAuth2Config{
			ClientID: "client", ClientSecret: "secret", RedirectURI: fakeRedirectURI, Issuer: url,
			AuthURL: url + "/auth", TokenURL: url + "/token", UserInfoURL: url + "/userinfo",
		})
		started := time.Now()
		checks := p.TestConnection(ctx)
		assert.Equal(t, []string{"Discovery failed", "Sign-in page failed", "Client secret failed", "Userinfo failed"}, statuses(checks), name)
		assert.Less(t, time.Since(started), 5*time.Second, name)
		// Each says why, not the request again.
		assert.NotContains(t, problems(checks), `Get "`, name)
		assert.NotContains(t, problems(checks), `Post "`, name)
	}
}
