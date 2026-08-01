package idp

import (
	"context"
	"fmt"
)

// Provider defines the interface that all identity providers must implement
type Provider interface {
	// Type returns the provider type identifier ("ldap", "oauth2")
	Type() string

	// TestConnection validates the provider configuration
	TestConnection(ctx context.Context) error

	// AuthenticateUser validates credentials against the external provider
	// For LDAP: binds with username/password
	// For OAuth: not used (OAuth uses redirect flow)
	AuthenticateUser(ctx context.Context, username, password string) (*ExternalUser, error)

	// SearchUsers searches for users in the external directory
	SearchUsers(ctx context.Context, query string, limit int) ([]ExternalUser, error)

	// SearchGroups searches for groups in the external directory
	SearchGroups(ctx context.Context, query string, limit int) ([]ExternalGroup, error)

	// GetGroupMembers returns members of a specific group
	GetGroupMembers(ctx context.Context, groupID string) ([]ExternalUser, error)

	// GetAuthURL returns the OAuth authorization URL (OAuth only)
	// loginHint is optional — if set, pre-fills the email on the OAuth provider's login page
	GetAuthURL(state string, loginHint string) (string, error)

	// ExchangeCode exchanges an OAuth code for user info (OAuth only)
	ExchangeCode(ctx context.Context, code string) (*ExternalUser, error)
}

// TokenAuthenticator is implemented by providers that can identify a user from
// a token the client already holds, without a browser redirect. It is the basis
// of the STS web-identity exchange: a headless client presents
// its OAuth access token and receives temporary S3 credentials.
//
// Optional — LDAP does not implement it.
type TokenAuthenticator interface {
	// AuthenticateWithToken resolves the user an access token belongs to.
	// Validation is delegated to the provider: the token is only accepted if the
	// provider itself accepts it, so revoked and expired tokens fail here.
	AuthenticateWithToken(ctx context.Context, accessToken string) (*ExternalUser, error)
}

// ProviderFactory creates a Provider from an IdentityProvider config
type ProviderFactory func(idp *IdentityProvider, cryptoSecret string) (Provider, error)

// registry holds registered provider factories
var registry = map[string]ProviderFactory{}

// RegisterProvider registers a provider factory for a given type
func RegisterProvider(providerType string, factory ProviderFactory) {
	registry[providerType] = factory
}

// NewProvider creates a new Provider instance from an IdentityProvider config
func NewProvider(idp *IdentityProvider, cryptoSecret string) (Provider, error) {
	factory, ok := registry[idp.Type]
	if !ok {
		return nil, fmt.Errorf("unknown provider type: %s", idp.Type)
	}
	return factory(idp, cryptoSecret)
}
