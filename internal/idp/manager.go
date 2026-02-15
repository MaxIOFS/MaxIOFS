package idp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Manager orchestrates identity provider operations
type Manager struct {
	store        *Store
	cryptoSecret string            // For encrypting bind passwords/secrets
	providers    map[string]Provider // Cached active provider instances
	mu           sync.RWMutex
}

// NewManager creates a new IDP manager
func NewManager(store *Store, cryptoSecret string) *Manager {
	return &Manager{
		store:        store,
		cryptoSecret: cryptoSecret,
		providers:    make(map[string]Provider),
	}
}

// generateID generates a new unique ID with the given prefix
func generateID(prefix string) string {
	b := make([]byte, 8)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// CreateProvider creates a new identity provider
func (m *Manager) CreateProvider(ctx context.Context, idp *IdentityProvider) error {
	if idp.ID == "" {
		idp.ID = generateID("idp-")
	}

	now := time.Now().Unix()
	idp.CreatedAt = now
	idp.UpdatedAt = now

	if idp.Status == "" {
		idp.Status = StatusTesting
	}

	// Encrypt sensitive fields before storage
	if err := m.encryptConfig(&idp.Config); err != nil {
		return fmt.Errorf("failed to encrypt config: %w", err)
	}

	if err := m.store.CreateProvider(idp); err != nil {
		return err
	}

	// Decrypt back for the caller
	m.decryptConfig(&idp.Config)

	logrus.WithFields(logrus.Fields{
		"id":   idp.ID,
		"name": idp.Name,
		"type": idp.Type,
	}).Info("Identity provider created")

	return nil
}

// UpdateProvider updates an existing identity provider
func (m *Manager) UpdateProvider(ctx context.Context, idp *IdentityProvider) error {
	idp.UpdatedAt = time.Now().Unix()

	// Encrypt sensitive fields before storage
	if err := m.encryptConfig(&idp.Config); err != nil {
		return fmt.Errorf("failed to encrypt config: %w", err)
	}

	if err := m.store.UpdateProvider(idp); err != nil {
		return err
	}

	// Invalidate cached provider
	m.mu.Lock()
	delete(m.providers, idp.ID)
	m.mu.Unlock()

	// Decrypt back for the caller
	m.decryptConfig(&idp.Config)

	logrus.WithField("id", idp.ID).Info("Identity provider updated")
	return nil
}

// DeleteProvider removes an identity provider
func (m *Manager) DeleteProvider(ctx context.Context, providerID string) error {
	if err := m.store.DeleteProvider(providerID); err != nil {
		return err
	}

	m.mu.Lock()
	delete(m.providers, providerID)
	m.mu.Unlock()

	logrus.WithField("id", providerID).Info("Identity provider deleted")
	return nil
}

// GetProvider retrieves an identity provider by ID
func (m *Manager) GetProvider(ctx context.Context, providerID string) (*IdentityProvider, error) {
	idp, err := m.store.GetProvider(providerID)
	if err != nil {
		return nil, err
	}

	// Decrypt sensitive fields for the caller
	m.decryptConfig(&idp.Config)
	return idp, nil
}

// GetProviderMasked retrieves a provider with secrets masked (for API responses)
func (m *Manager) GetProviderMasked(ctx context.Context, providerID string) (*IdentityProvider, error) {
	idp, err := m.store.GetProvider(providerID)
	if err != nil {
		return nil, err
	}

	// Mask secrets instead of decrypting
	m.maskConfig(&idp.Config)
	return idp, nil
}

// ListProviders lists identity providers
func (m *Manager) ListProviders(ctx context.Context, tenantID string) ([]*IdentityProvider, error) {
	providers, err := m.store.ListProviders(tenantID)
	if err != nil {
		return nil, err
	}

	// Mask secrets in the response
	for _, p := range providers {
		m.maskConfig(&p.Config)
	}

	return providers, nil
}

// ListActiveOAuthProviders lists active OAuth providers for login page
func (m *Manager) ListActiveOAuthProviders(ctx context.Context) ([]*IdentityProvider, error) {
	providers, err := m.store.ListActiveOAuthProviders()
	if err != nil {
		return nil, err
	}

	// Only return minimal info needed for login buttons
	for _, p := range providers {
		m.maskConfig(&p.Config)
	}

	return providers, nil
}

// TestConnection tests a provider's connection
func (m *Manager) TestConnection(ctx context.Context, providerID string) error {
	provider, err := m.getOrCreateProvider(providerID)
	if err != nil {
		return err
	}

	return provider.TestConnection(ctx)
}

// SearchUsers searches for users in an external directory
func (m *Manager) SearchUsers(ctx context.Context, providerID, query string, limit int) ([]ExternalUser, error) {
	provider, err := m.getOrCreateProvider(providerID)
	if err != nil {
		return nil, err
	}

	return provider.SearchUsers(ctx, query, limit)
}

// SearchGroups searches for groups in an external directory
func (m *Manager) SearchGroups(ctx context.Context, providerID, query string, limit int) ([]ExternalGroup, error) {
	provider, err := m.getOrCreateProvider(providerID)
	if err != nil {
		return nil, err
	}

	return provider.SearchGroups(ctx, query, limit)
}

// GetGroupMembers returns members of a specific group
func (m *Manager) GetGroupMembers(ctx context.Context, providerID, groupID string) ([]ExternalUser, error) {
	provider, err := m.getOrCreateProvider(providerID)
	if err != nil {
		return nil, err
	}

	return provider.GetGroupMembers(ctx, groupID)
}

// AuthenticateExternal authenticates a user against an external provider
func (m *Manager) AuthenticateExternal(ctx context.Context, providerID, username, password string) (*ExternalUser, error) {
	provider, err := m.getOrCreateProvider(providerID)
	if err != nil {
		return nil, err
	}

	return provider.AuthenticateUser(ctx, username, password)
}

// GetOAuthAuthURL gets the OAuth authorization URL for a provider
func (m *Manager) GetOAuthAuthURL(ctx context.Context, providerID, state string) (string, error) {
	provider, err := m.getOrCreateProvider(providerID)
	if err != nil {
		return "", err
	}

	return provider.GetAuthURL(state)
}

// HandleOAuthCallback exchanges an OAuth code for user info
func (m *Manager) HandleOAuthCallback(ctx context.Context, providerID, code string) (*ExternalUser, error) {
	provider, err := m.getOrCreateProvider(providerID)
	if err != nil {
		return nil, err
	}

	return provider.ExchangeCode(ctx, code)
}

// CreateGroupMapping creates a new group mapping
func (m *Manager) CreateGroupMapping(ctx context.Context, mapping *GroupMapping) error {
	if mapping.ID == "" {
		mapping.ID = generateID("gmap-")
	}

	now := time.Now().Unix()
	mapping.CreatedAt = now
	mapping.UpdatedAt = now

	return m.store.CreateGroupMapping(mapping)
}

// UpdateGroupMapping updates a group mapping
func (m *Manager) UpdateGroupMapping(ctx context.Context, mapping *GroupMapping) error {
	mapping.UpdatedAt = time.Now().Unix()
	return m.store.UpdateGroupMapping(mapping)
}

// DeleteGroupMapping deletes a group mapping
func (m *Manager) DeleteGroupMapping(ctx context.Context, mappingID string) error {
	return m.store.DeleteGroupMapping(mappingID)
}

// GetGroupMapping retrieves a group mapping by ID
func (m *Manager) GetGroupMapping(ctx context.Context, mappingID string) (*GroupMapping, error) {
	return m.store.GetGroupMapping(mappingID)
}

// ListGroupMappings lists group mappings for a provider
func (m *Manager) ListGroupMappings(ctx context.Context, providerID string) ([]*GroupMapping, error) {
	return m.store.ListGroupMappings(providerID)
}

// CountLinkedUsers returns the number of users linked to a provider
func (m *Manager) CountLinkedUsers(ctx context.Context, providerID, providerType string) (int, error) {
	return m.store.CountUsersWithProvider(providerID, providerType)
}

// Store returns the underlying store for direct access if needed
func (m *Manager) Store() *Store {
	return m.store
}

// getOrCreateProvider gets a cached provider or creates a new one
func (m *Manager) getOrCreateProvider(providerID string) (Provider, error) {
	m.mu.RLock()
	if p, ok := m.providers[providerID]; ok {
		m.mu.RUnlock()
		return p, nil
	}
	m.mu.RUnlock()

	// Load from store
	idp, err := m.store.GetProvider(providerID)
	if err != nil {
		return nil, err
	}

	// Decrypt config
	m.decryptConfig(&idp.Config)

	// Create provider instance
	provider, err := NewProvider(idp, m.cryptoSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider instance: %w", err)
	}

	m.mu.Lock()
	m.providers[providerID] = provider
	m.mu.Unlock()

	return provider, nil
}

// encryptConfig encrypts sensitive fields in the config
func (m *Manager) encryptConfig(config *ProviderConfig) error {
	if config.LDAP != nil && config.LDAP.BindPassword != "" {
		encrypted, err := Encrypt(config.LDAP.BindPassword, m.cryptoSecret)
		if err != nil {
			return err
		}
		config.LDAP.BindPassword = encrypted
	}

	if config.OAuth2 != nil && config.OAuth2.ClientSecret != "" {
		encrypted, err := Encrypt(config.OAuth2.ClientSecret, m.cryptoSecret)
		if err != nil {
			return err
		}
		config.OAuth2.ClientSecret = encrypted
	}

	return nil
}

// decryptConfig decrypts sensitive fields in the config
func (m *Manager) decryptConfig(config *ProviderConfig) {
	if config.LDAP != nil && config.LDAP.BindPassword != "" {
		decrypted, err := Decrypt(config.LDAP.BindPassword, m.cryptoSecret)
		if err != nil {
			logrus.WithError(err).Warn("Failed to decrypt LDAP bind password")
			return
		}
		config.LDAP.BindPassword = decrypted
	}

	if config.OAuth2 != nil && config.OAuth2.ClientSecret != "" {
		decrypted, err := Decrypt(config.OAuth2.ClientSecret, m.cryptoSecret)
		if err != nil {
			logrus.WithError(err).Warn("Failed to decrypt OAuth client secret")
			return
		}
		config.OAuth2.ClientSecret = decrypted
	}
}

// maskConfig replaces sensitive fields with a masked value
func (m *Manager) maskConfig(config *ProviderConfig) {
	if config.LDAP != nil && config.LDAP.BindPassword != "" {
		config.LDAP.BindPassword = "********"
	}

	if config.OAuth2 != nil && config.OAuth2.ClientSecret != "" {
		config.OAuth2.ClientSecret = "********"
	}
}
