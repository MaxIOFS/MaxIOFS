package idp

import (
	"context"
	"errors"
	"fmt"
)

// ErrUnavailable is the error of a provider that could not be reached or did
// not answer in time; it says nothing of the credentials.
var ErrUnavailable = errors.New("identity provider unavailable")

// ErrSessionEnded is the error of a provider that no longer lets a signed-in
// user in: the user is gone or disabled, or the provider ended its session.
var ErrSessionEnded = errors.New("identity provider ended the sign-in")

// ErrProviderNotFound is the error of an identity provider ID none has.
var ErrProviderNotFound = errors.New("identity provider not found")

// Grant is what a provider gave for a sign-in, kept to ask it again: the
// user's subject (a DN for LDAP), the provider's session and its refresh token.
type Grant struct {
	Subject      string
	Session      string
	RefreshToken string
}

// SessionRevalidator is implemented by providers that can tell, without the
// user, whether a sign-in still stands: ErrSessionEnded when it does not.
type SessionRevalidator interface {
	Revalidate(ctx context.Context, grant Grant) (*ExternalUser, error)
}

// LogoutVerifier is implemented by providers that send OpenID Connect
// back-channel logouts: it checks a logout token and gives its subject and
// session.
type LogoutVerifier interface {
	VerifyLogoutToken(ctx context.Context, token string) (subject, session string, err error)
}

// Provider defines the interface that all identity providers must implement
type Provider interface {
	// Type returns the provider type identifier ("ldap", "oauth2")
	Type() string

	// TestConnection validates the provider configuration
	TestConnection(ctx context.Context) error

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
type TokenAuthenticator interface {
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
