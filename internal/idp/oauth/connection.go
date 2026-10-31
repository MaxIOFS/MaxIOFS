package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/maxiofs/maxiofs/internal/idp"
	"golang.org/x/oauth2"
)

// testCode is the authorization code a connection test exchanges. No one
// issued it: a provider that takes the client's credentials refuses it as
// invalid_grant, one that does not refuses the client as invalid_client.
const testCode = "maxiofs-connection-test"

// TestConnection checks the provider without a user, within the provider
// timeout: its OpenID configuration and keys, its sign-in page for the client
// and the redirect URI, the client's secret, and the userinfo endpoint.
func (p *Provider) TestConnection(ctx context.Context) []idp.Check {
	var missing []string
	for _, field := range []struct{ label, value string }{
		{"Client ID", p.config.ClientID}, {"Client Secret", p.config.ClientSecret},
		{"Authorization URL", p.config.AuthURL}, {"Token URL", p.config.TokenURL},
		{"User Info URL", p.config.UserInfoURL}, {"Redirect URI", p.config.RedirectURI},
	} {
		if field.value == "" {
			missing = append(missing, field.label)
		}
	}
	if len(missing) > 0 {
		return []idp.Check{check("Configuration", idp.CheckFailed, "Missing: %s", strings.Join(missing, ", "))}
	}

	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	steps := []func(context.Context) idp.Check{p.checkSignInPage, p.checkClientSecret, p.checkUserInfo}
	if p.config.Issuer != "" {
		steps = append([]func(context.Context) idp.Check{p.checkDiscovery}, steps...)
	}
	checks := make([]idp.Check, len(steps))
	var wg sync.WaitGroup
	for i, step := range steps {
		wg.Go(func() { checks[i] = step(ctx) })
	}
	wg.Wait()
	return checks
}

// checkDiscovery reads the issuer's OpenID configuration and its signing
// keys, as checking an ID token or a logout token does.
func (p *Provider) checkDiscovery(ctx context.Context) idp.Check {
	issuer := p.config.Issuer
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return check("Discovery", idp.CheckFailed, "Cannot read the OpenID configuration of %s: %s", issuer, reason(ctx, err))
	}
	var meta struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := provider.Claims(&meta); err != nil || meta.JWKSURI == "" {
		return check("Discovery", idp.CheckFailed, "The OpenID configuration of %s names no signing keys", issuer)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, meta.JWKSURI, nil)
	if err != nil {
		return check("Discovery", idp.CheckFailed, "The signing keys URL %s is not valid: %v", meta.JWKSURI, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return check("Discovery", idp.CheckFailed, "Cannot read the signing keys at %s: %s", meta.JWKSURI, reason(ctx, err))
	}
	defer resp.Body.Close()
	var keys struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&keys) != nil || len(keys.Keys) == 0 {
		return check("Discovery", idp.CheckFailed, "No signing keys at %s (%d)", meta.JWKSURI, resp.StatusCode)
	}
	noun := "signing keys"
	if len(keys.Keys) == 1 {
		noun = "signing key"
	}
	return check("Discovery", idp.CheckPassed, "OpenID configuration of %s read, %d %s", issuer, len(keys.Keys), noun)
}

// checkSignInPage asks for the sign-in page a user is sent to, following the
// provider's redirects as a browser does: a provider that does not know the
// client or the redirect URI shows an error page, or sends the browser back
// to MaxIOFS with an error.
func (p *Provider) checkSignInPage(ctx context.Context) idp.Check {
	authURL, _ := p.GetAuthURL(testCode, "")
	back, err := url.Parse(p.config.RedirectURI)
	if err != nil {
		return check("Sign-in page", idp.CheckFailed, "The redirect URI %s is not valid: %v", p.config.RedirectURI, err)
	}
	var refusal url.Values
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if strings.EqualFold(req.URL.Host, back.Host) && req.URL.Path == back.Path {
			refusal = req.URL.Query()
			return http.ErrUseLastResponse
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, authURL, nil)
	if err != nil {
		return check("Sign-in page", idp.CheckFailed, "The authorization URL %s is not valid: %v", p.config.AuthURL, err)
	}
	req.Header.Set("Accept", "text/html")
	resp, err := client.Do(req)
	if err != nil {
		return check("Sign-in page", idp.CheckFailed, "No sign-in page at %s: %s", p.config.AuthURL, reason(ctx, err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	switch {
	case refusal.Get("error") != "":
		return check("Sign-in page", idp.CheckFailed, "The provider refused the sign-in: %s",
			providerError(refusal.Get("error"), refusal.Get("error_description")))
	case refusal != nil:
		return check("Sign-in page", idp.CheckPassed, "The provider takes client %s and %s", p.config.ClientID, p.config.RedirectURI)
	case resp.StatusCode == http.StatusNotFound:
		return check("Sign-in page", idp.CheckFailed, "Nothing at %s (404): check the authorization URL", p.config.AuthURL)
	case resp.StatusCode >= 500:
		return check("Sign-in page", idp.CheckFailed, "The provider failed the sign-in page (%d)", resp.StatusCode)
	case resp.StatusCode >= 400:
		return check("Sign-in page", idp.CheckFailed,
			"The provider refused the sign-in (%d): check that client %s exists and has %s as a redirect URI",
			resp.StatusCode, p.config.ClientID, p.config.RedirectURI)
	}
	return check("Sign-in page", idp.CheckPassed, "The provider shows its sign-in page for client %s and %s", p.config.ClientID, p.config.RedirectURI)
}

// checkClientSecret exchanges testCode with the client's credentials in the
// Authorization header and in the form, as a sign-in tries them.
func (p *Provider) checkClientSecret(ctx context.Context) idp.Check {
	var answers []error
	for _, style := range []oauth2.AuthStyle{oauth2.AuthStyleInHeader, oauth2.AuthStyleInParams} {
		config := &oauth2.Config{
			ClientID: p.config.ClientID, ClientSecret: p.config.ClientSecret, RedirectURL: p.config.RedirectURI,
			Endpoint: oauth2.Endpoint{AuthURL: p.config.AuthURL, TokenURL: p.config.TokenURL, AuthStyle: style},
		}
		_, err := config.Exchange(ctx, testCode)
		var refused *oauth2.RetrieveError
		if err == nil || errors.As(err, &refused) && refused.ErrorCode == "invalid_grant" {
			return check("Client secret", idp.CheckPassed, "The provider takes the client ID and secret")
		}
		answers = append(answers, err)
		if !errors.As(err, &refused) {
			break
		}
	}

	for _, err := range answers {
		var refused *oauth2.RetrieveError
		if errors.As(err, &refused) && (refused.ErrorCode == "invalid_client" || refused.ErrorCode == "unauthorized_client") {
			return check("Client secret", idp.CheckFailed, "The provider refused the client: %s",
				providerError(refused.ErrorCode, refused.ErrorDescription))
		}
	}
	err := answers[0]
	var refused *oauth2.RetrieveError
	var transport *url.Error
	switch {
	case errors.As(err, &transport):
		return check("Client secret", idp.CheckFailed, "No answer at %s: %s", p.config.TokenURL, reason(ctx, err))
	case !errors.As(err, &refused):
		return check("Client secret", idp.CheckFailed, "%s did not answer as a token endpoint: %v", p.config.TokenURL, err)
	case refused.Response.StatusCode >= 500:
		return check("Client secret", idp.CheckFailed, "The provider failed the token request (%d)", refused.Response.StatusCode)
	case refused.ErrorCode == "":
		return check("Client secret", idp.CheckFailed, "%s answered %d, not as a token endpoint: check the token URL",
			p.config.TokenURL, refused.Response.StatusCode)
	}
	return check("Client secret", idp.CheckWarning, "The provider answered %s: the secret could not be checked",
		providerError(refused.ErrorCode, refused.ErrorDescription))
}

// checkUserInfo asks the userinfo endpoint without a token: it refuses.
func (p *Provider) checkUserInfo(ctx context.Context) idp.Check {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.config.UserInfoURL, nil)
	if err != nil {
		return check("Userinfo", idp.CheckFailed, "The userinfo URL %s is not valid: %v", p.config.UserInfoURL, err)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return check("Userinfo", idp.CheckFailed, "No answer at %s: %s", p.config.UserInfoURL, reason(ctx, err))
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	switch code := resp.StatusCode; {
	case code == http.StatusBadRequest || code == http.StatusUnauthorized || code == http.StatusForbidden:
		return check("Userinfo", idp.CheckPassed, "The userinfo endpoint asks for a token")
	case code == http.StatusNotFound || code == http.StatusMethodNotAllowed:
		return check("Userinfo", idp.CheckFailed, "Nothing at %s (%d): check the userinfo URL", p.config.UserInfoURL, code)
	case code >= 500:
		return check("Userinfo", idp.CheckFailed, "The provider failed the userinfo request (%d)", code)
	}
	return check("Userinfo", idp.CheckWarning, "%s answered %d without a token: check the userinfo URL", p.config.UserInfoURL, resp.StatusCode)
}

// reason is why a request to the provider failed: no answer in time, or err
// without the request it names.
func reason(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Sprintf("no answer within %s", providerTimeout)
	}
	var request *url.Error
	if errors.As(err, &request) {
		return request.Err.Error()
	}
	return err.Error()
}

// providerError is an OAuth error code with its description.
func providerError(code, description string) string {
	if description == "" {
		return code
	}
	return code + " (" + description + ")"
}

func check(name, status, format string, args ...any) idp.Check {
	return idp.Check{Name: name, Status: status, Detail: fmt.Sprintf(format, args...)}
}
