package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/maxiofs/maxiofs/internal/idp"
	"golang.org/x/oauth2"
)

// Provider implements the idp.Provider interface for OAuth2/OIDC
type Provider struct {
	config      *idp.OAuth2Config
	oauthConfig *oauth2.Config
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

func (p *Provider) GetAuthURL(state string) (string, error) {
	url := p.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline)
	return url, nil
}

func (p *Provider) ExchangeCode(ctx context.Context, code string) (*idp.ExternalUser, error) {
	// Exchange the authorization code for a token
	token, err := p.oauthConfig.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	// Use the token to fetch user info
	client := p.oauthConfig.Client(ctx, token)
	return p.fetchUserInfo(client)
}

// fetchUserInfo retrieves user information from the OAuth provider's userinfo endpoint
func (p *Provider) fetchUserInfo(client *http.Client) (*idp.ExternalUser, error) {
	if p.config.UserInfoURL == "" {
		return nil, fmt.Errorf("userinfo_url is not configured")
	}

	resp, err := client.Get(p.config.UserInfoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("userinfo request failed with status %d: %s", resp.StatusCode, string(body))
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
	}

	return &cfg
}
