package idp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cacheTestProvider is a provider that remembers the client it was built with.
type cacheTestProvider struct {
	Provider
	clientID string
}

const cacheTestType = "cache-test"

func init() {
	RegisterProvider(cacheTestType, func(idp *IdentityProvider, _ string) (Provider, error) {
		return &cacheTestProvider{clientID: idp.Config.OAuth2.ClientID}, nil
	})
}

// A provider changed or deleted through the database, as another node's change
// reaches this one, is not served from the instance cached before: the next
// sign-in uses the stored configuration.
func TestAProviderChangedElsewhereIsNotServedFromTheCache(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	provider := &IdentityProvider{
		Name: "SSO", Type: cacheTestType, Status: StatusActive, CreatedBy: "admin",
		Config: ProviderConfig{OAuth2: &OAuth2Config{
			Preset: "custom", ClientID: "first-client", ClientSecret: "first-secret",
			AuthURL: "https://sso.example.com/auth", TokenURL: "https://sso.example.com/token",
			UserInfoURL: "https://sso.example.com/userinfo", RedirectURI: "https://maxiofs.example.com/callback",
		}},
	}
	require.NoError(t, mgr.CreateProvider(ctx, provider))
	first, err := mgr.getOrCreateProvider(provider.ID)
	require.NoError(t, err)
	again, err := mgr.getOrCreateProvider(provider.ID)
	require.NoError(t, err)
	assert.True(t, first == again, "an unchanged provider is reused")

	// Another node changed the client; the row arrives here through the sync.
	stored, err := mgr.store.GetProvider(provider.ID)
	require.NoError(t, err)
	stored.Config.OAuth2.ClientID = "second-client"
	require.NoError(t, mgr.store.UpdateProvider(stored))
	changed, err := mgr.getOrCreateProvider(provider.ID)
	require.NoError(t, err)
	assert.False(t, first == changed, "the cached instance was built from the old configuration")
	assert.Equal(t, "second-client", changed.(*cacheTestProvider).clientID)

	// Another node deleted it.
	require.NoError(t, mgr.store.DeleteProvider(provider.ID))
	_, err = mgr.getOrCreateProvider(provider.ID)
	assert.Error(t, err, "a deleted provider signs no one in")
}
