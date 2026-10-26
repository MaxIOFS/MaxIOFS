package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/maxiofs/maxiofs/internal/idp"
	"golang.org/x/oauth2"
)

// providerTimeout bounds a code exchange or an identity lookup with the
// provider, so one that does not answer fails a sign-in in time.
var providerTimeout = 15 * time.Second

// Provider implements the idp.Provider interface for OAuth2/OIDC
type Provider struct {
	config      *idp.OAuth2Config
	oauthConfig *oauth2.Config

	oidcMu       sync.Mutex
	oidcProvider *oidc.Provider
	// seenLogouts holds the IDs of the logout tokens taken, by when.
	seenLogouts sync.Map
}

func init() {
	idp.RegisterProvider(idp.TypeOAuth2, func(provider *idp.IdentityProvider, cryptoSecret string) (idp.Provider, error) {
		if provider.Config.OAuth2 == nil {
			return nil, fmt.Errorf("OAuth2 config is required for OAuth2 provider")
		}
		return NewProvider(provider.Config.OAuth2), nil
	})
}

// NewProvider creates a new OAuth2 provider
func NewProvider(config *idp.OAuth2Config) *Provider {
	// Apply presets if specified
	cfg := applyPreset(config)

	oauthConfig := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  cfg.AuthURL,
			TokenURL: cfg.TokenURL,
		},
		Scopes:      cfg.Scopes,
		RedirectURL: cfg.RedirectURI,
	}

	return &Provider{
		config:      cfg,
		oauthConfig: oauthConfig,
	}
}

func (p *Provider) Type() string {
	return idp.TypeOAuth2
}

func (p *Provider) TestConnection(ctx context.Context) error {
	// For OAuth, we can't really test the connection without a browser redirect.
	// Just validate that we have the required fields.
	if p.config.ClientID == "" {
		return fmt.Errorf("client_id is required")
	}
	if p.config.ClientSecret == "" {
		return fmt.Errorf("client_secret is required")
	}
	if p.config.AuthURL == "" {
		return fmt.Errorf("auth_url is required")
	}
	if p.config.TokenURL == "" {
		return fmt.Errorf("token_url is required")
	}
	return nil
}

func (p *Provider) AuthenticateUser(ctx context.Context, username, password string) (*idp.ExternalUser, error) {
	return nil, fmt.Errorf("OAuth2 provider does not support direct username/password authentication. Use the SSO login flow")
}

func (p *Provider) SearchUsers(ctx context.Context, query string, limit int) ([]idp.ExternalUser, error) {
	return nil, fmt.Errorf("OAuth2 provider does not support user search")
}

func (p *Provider) SearchGroups(ctx context.Context, query string, limit int) ([]idp.ExternalGroup, error) {
	return nil, fmt.Errorf("OAuth2 provider does not support group search")
}

func (p *Provider) GetGroupMembers(ctx context.Context, groupID string) ([]idp.ExternalUser, error) {
	return nil, fmt.Errorf("OAuth2 provider does not support group member listing")
}

func (p *Provider) GetAuthURL(state string, loginHint string) (string, error) {
	var opts []oauth2.AuthCodeOption
	if loginHint != "" {
		opts = append(opts, oauth2.SetAuthURLParam("login_hint", loginHint))
	}
	// Google gives a refresh token only to a client that asks for offline
	// access; without one a sign-in cannot be asked about again.
	if p.config.Preset == "google" {
		opts = append(opts, oauth2.AccessTypeOffline)
	}
	url := p.oauthConfig.AuthCodeURL(state, opts...)
	return url, nil
}

func (p *Provider) ExchangeCode(ctx context.Context, code string) (*idp.ExternalUser, error) {
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()

	// Exchange the authorization code for a token
	token, err := p.oauthConfig.Exchange(ctx, code)
	if err != nil {
		err = fmt.Errorf("failed to exchange code: %w", err)
		// A refusal is the provider's answer; anything else is no answer.
		var refused *oauth2.RetrieveError
		if !errors.As(err, &refused) || refused.Response == nil || refused.Response.StatusCode >= 500 {
			err = fmt.Errorf("%w: %w", idp.ErrUnavailable, err)
		}
		return nil, err
	}

	// Use the token to fetch user info
	client := p.oauthConfig.Client(ctx, token)
	user, err := p.fetchUserInfo(ctx, client)
	if err != nil {
		return nil, err
	}
	subject, session, err := p.idTokenGrant(ctx, token)
	if err != nil {
		return nil, err
	}
	if subject != "" {
		if user.Grant.Subject != "" && user.Grant.Subject != subject {
			return nil, fmt.Errorf("userinfo answered for another subject than the ID token")
		}
		user.Grant.Subject = subject
	}
	user.Grant.Session = session
	user.Grant.RefreshToken = token.RefreshToken
	return user, nil
}

// AuthenticateWithToken resolves the user an access token belongs to by calling
func (p *Provider) AuthenticateWithToken(ctx context.Context, accessToken string) (*idp.ExternalUser, error) {
	if accessToken == "" {
		return nil, fmt.Errorf("access token is required")
	}
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()

	client := p.oauthConfig.Client(ctx, &oauth2.Token{
		AccessToken: accessToken,
		TokenType:   "Bearer",
	})
	return p.fetchUserInfo(ctx, client)
}

// fetchUserInfo retrieves user information from the OAuth provider's userinfo endpoint
func (p *Provider) fetchUserInfo(ctx context.Context, client *http.Client) (*idp.ExternalUser, error) {
	if p.config.UserInfoURL == "" {
		return nil, fmt.Errorf("userinfo_url is not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.config.UserInfoURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build the userinfo request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to fetch user info: %w", idp.ErrUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("userinfo request failed with status %d: %s", resp.StatusCode, string(body))
		switch {
		case resp.StatusCode >= 500:
			err = fmt.Errorf("%w: %w", idp.ErrUnavailable, err)
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			err = fmt.Errorf("%w: %w", errUserInfoRefused, err)
		}
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read userinfo response: %w", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse userinfo response: %w", err)
	}

	// Extract user fields from claims
	claimEmail := p.config.ClaimEmail
	if claimEmail == "" {
		claimEmail = "email"
	}
	claimName := p.config.ClaimName
	if claimName == "" {
		claimName = "name"
	}

	email, _ := claims[claimEmail].(string)
	name, _ := claims[claimName].(string)
	sub, _ := claims["sub"].(string)

	externalID := email
	if externalID == "" {
		externalID = sub
	}

	// Extract groups if configured
	var groups []string
	if p.config.ClaimGroups != "" {
		if groupsList, ok := claims[p.config.ClaimGroups].([]interface{}); ok {
			for _, g := range groupsList {
				if gs, ok := g.(string); ok {
					groups = append(groups, gs)
				}
			}
		}
	}

	rawAttrs := make(map[string]string)
	for k, v := range claims {
		rawAttrs[k] = fmt.Sprintf("%v", v)
	}

	return &idp.ExternalUser{
		ExternalID:  externalID,
		Username:    email,
		Email:       email,
		DisplayName: name,
		Groups:      groups,
		RawAttrs:    rawAttrs,
		Grant:       &idp.Grant{Subject: sub},
	}, nil
}

// applyPreset fills in default URLs/scopes based on the preset
func applyPreset(config *idp.OAuth2Config) *idp.OAuth2Config {
	// Make a copy to avoid mutating the original
	cfg := *config

	switch cfg.Preset {
	case "google":
		cfg = ApplyGooglePreset(&cfg)
	case "microsoft":
		if cfg.AuthURL == "" {
			cfg.AuthURL = "https://login.microsoftonline.com/common/oauth2/v2.0/authorize"
		}
		if cfg.TokenURL == "" {
			cfg.TokenURL = "https://login.microsoftonline.com/common/oauth2/v2.0/token"
		}
		if cfg.UserInfoURL == "" {
			cfg.UserInfoURL = "https://graph.microsoft.com/oidc/userinfo"
		}
		if len(cfg.Scopes) == 0 {
			cfg.Scopes = []string{"openid", "profile", "email"}
		}
		// Entra ID gives a refresh token only for offline_access.
		if !slices.Contains(cfg.Scopes, "offline_access") {
			cfg.Scopes = append(slices.Clone(cfg.Scopes), "offline_access")
		}
	}

	return &cfg
}
