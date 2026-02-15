package idp

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxiofs/maxiofs/internal/db/migrations"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func setupTestManager(t *testing.T) (*Manager, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "idp-manager-test-*")
	require.NoError(t, err)

	dbPath := filepath.Join(tmpDir, "db")
	require.NoError(t, os.MkdirAll(dbPath, 0755))

	db, err := sql.Open("sqlite", filepath.Join(dbPath, "maxiofs.db")+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	require.NoError(t, err)

	migrationManager := migrations.NewMigrationManager(db, logrus.StandardLogger())
	require.NoError(t, migrationManager.Migrate())

	store := NewStore(db)
	manager := NewManager(store, "test-crypto-secret-key-for-tests")

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return manager, cleanup
}

// --- Provider CRUD via Manager ---

func TestManager_CreateAndGetProvider(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	idp := &IdentityProvider{
		Name:      "Test LDAP",
		Type:      TypeLDAP,
		CreatedBy: "admin",
		Config: ProviderConfig{
			LDAP: &LDAPConfig{
				Host:         "ldap.test.com",
				Port:         636,
				Security:     "tls",
				BindDN:       "cn=svc,dc=test,dc=com",
				BindPassword: "my-secret-bind-password",
				BaseDN:       "dc=test,dc=com",
			},
		},
	}

	err := mgr.CreateProvider(ctx, idp)
	require.NoError(t, err)
	assert.NotEmpty(t, idp.ID, "ID should be auto-generated")
	assert.NotZero(t, idp.CreatedAt)
	assert.Equal(t, StatusTesting, idp.Status, "default status should be testing")
	// After create, the caller should see the plaintext password (decrypted back)
	assert.Equal(t, "my-secret-bind-password", idp.Config.LDAP.BindPassword)

	// GetProvider should decrypt
	got, err := mgr.GetProvider(ctx, idp.ID)
	require.NoError(t, err)
	assert.Equal(t, "my-secret-bind-password", got.Config.LDAP.BindPassword)
}

func TestManager_CreateOAuth2Provider(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	idp := &IdentityProvider{
		Name:      "Google SSO",
		Type:      TypeOAuth2,
		Status:    StatusActive,
		CreatedBy: "admin",
		Config: ProviderConfig{
			OAuth2: &OAuth2Config{
				Preset:       "google",
				ClientID:     "google-client-id",
				ClientSecret: "google-client-secret",
				Scopes:       []string{"openid", "email"},
			},
		},
	}

	err := mgr.CreateProvider(ctx, idp)
	require.NoError(t, err)
	assert.Equal(t, StatusActive, idp.Status)
	assert.Equal(t, "google-client-secret", idp.Config.OAuth2.ClientSecret)
}

func TestManager_GetProviderMasked(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	idp := &IdentityProvider{
		Name:      "Masked LDAP",
		Type:      TypeLDAP,
		CreatedBy: "admin",
		Config: ProviderConfig{
			LDAP: &LDAPConfig{
				Host:         "ldap.test.com",
				Port:         389,
				BindDN:       "cn=admin,dc=test",
				BindPassword: "super-secret",
				BaseDN:       "dc=test",
			},
		},
	}

	require.NoError(t, mgr.CreateProvider(ctx, idp))

	masked, err := mgr.GetProviderMasked(ctx, idp.ID)
	require.NoError(t, err)
	assert.Equal(t, "********", masked.Config.LDAP.BindPassword, "password should be masked")
	assert.Equal(t, "ldap.test.com", masked.Config.LDAP.Host, "non-sensitive fields should remain")
}

func TestManager_GetProviderMasked_OAuth2(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	idp := &IdentityProvider{
		Name:      "Masked OAuth",
		Type:      TypeOAuth2,
		CreatedBy: "admin",
		Config: ProviderConfig{
			OAuth2: &OAuth2Config{
				ClientID:     "public-id",
				ClientSecret: "super-secret",
			},
		},
	}

	require.NoError(t, mgr.CreateProvider(ctx, idp))

	masked, err := mgr.GetProviderMasked(ctx, idp.ID)
	require.NoError(t, err)
	assert.Equal(t, "********", masked.Config.OAuth2.ClientSecret)
	assert.Equal(t, "public-id", masked.Config.OAuth2.ClientID)
}

func TestManager_UpdateProvider(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	idp := &IdentityProvider{
		Name:      "Before Update",
		Type:      TypeLDAP,
		CreatedBy: "admin",
		Config: ProviderConfig{
			LDAP: &LDAPConfig{
				Host:         "old.ldap.com",
				BindPassword: "old-password",
				BaseDN:       "dc=old",
			},
		},
	}
	require.NoError(t, mgr.CreateProvider(ctx, idp))

	idp.Name = "After Update"
	idp.Config.LDAP.Host = "new.ldap.com"
	idp.Config.LDAP.BindPassword = "new-password"

	err := mgr.UpdateProvider(ctx, idp)
	require.NoError(t, err)

	got, err := mgr.GetProvider(ctx, idp.ID)
	require.NoError(t, err)
	assert.Equal(t, "After Update", got.Name)
	assert.Equal(t, "new.ldap.com", got.Config.LDAP.Host)
	assert.Equal(t, "new-password", got.Config.LDAP.BindPassword)
}

func TestManager_DeleteProvider(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	idp := &IdentityProvider{
		Name:      "To Delete",
		Type:      TypeLDAP,
		CreatedBy: "admin",
		Config: ProviderConfig{
			LDAP: &LDAPConfig{Host: "del.ldap.com", BaseDN: "dc=del"},
		},
	}
	require.NoError(t, mgr.CreateProvider(ctx, idp))

	err := mgr.DeleteProvider(ctx, idp.ID)
	require.NoError(t, err)

	_, err = mgr.GetProvider(ctx, idp.ID)
	assert.Error(t, err)
}

func TestManager_ListProviders_MasksSecrets(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, mgr.CreateProvider(ctx, &IdentityProvider{
		Name: "LDAP 1", Type: TypeLDAP, CreatedBy: "admin",
		Config: ProviderConfig{LDAP: &LDAPConfig{Host: "a.com", BindPassword: "secret1", BaseDN: "dc=a"}},
	}))
	require.NoError(t, mgr.CreateProvider(ctx, &IdentityProvider{
		Name: "OAuth 1", Type: TypeOAuth2, CreatedBy: "admin",
		Config: ProviderConfig{OAuth2: &OAuth2Config{ClientID: "id1", ClientSecret: "secret2"}},
	}))

	providers, err := mgr.ListProviders(ctx, "")
	require.NoError(t, err)
	assert.Len(t, providers, 2)

	for _, p := range providers {
		if p.Config.LDAP != nil {
			assert.Equal(t, "********", p.Config.LDAP.BindPassword, "LDAP password should be masked in list")
		}
		if p.Config.OAuth2 != nil {
			assert.Equal(t, "********", p.Config.OAuth2.ClientSecret, "OAuth secret should be masked in list")
		}
	}
}

// --- Group Mapping via Manager ---

func TestManager_GroupMappingCRUD(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	// Create a provider first
	idp := &IdentityProvider{
		Name: "GM Provider", Type: TypeLDAP, CreatedBy: "admin",
		Config: ProviderConfig{LDAP: &LDAPConfig{Host: "gm.ldap.com", BaseDN: "dc=gm"}},
	}
	require.NoError(t, mgr.CreateProvider(ctx, idp))

	// Create mapping
	mapping := &GroupMapping{
		ProviderID:        idp.ID,
		ExternalGroup:     "CN=Engineers,DC=gm",
		ExternalGroupName: "Engineers",
		Role:              "user",
		AutoSync:          true,
	}
	err := mgr.CreateGroupMapping(ctx, mapping)
	require.NoError(t, err)
	assert.NotEmpty(t, mapping.ID)
	assert.NotZero(t, mapping.CreatedAt)

	// Get mapping
	got, err := mgr.GetGroupMapping(ctx, mapping.ID)
	require.NoError(t, err)
	assert.Equal(t, "Engineers", got.ExternalGroupName)
	assert.Equal(t, "user", got.Role)

	// Update mapping
	mapping.Role = "admin"
	err = mgr.UpdateGroupMapping(ctx, mapping)
	require.NoError(t, err)

	got, err = mgr.GetGroupMapping(ctx, mapping.ID)
	require.NoError(t, err)
	assert.Equal(t, "admin", got.Role)

	// List mappings
	list, err := mgr.ListGroupMappings(ctx, idp.ID)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	// Delete mapping
	err = mgr.DeleteGroupMapping(ctx, mapping.ID)
	require.NoError(t, err)

	_, err = mgr.GetGroupMapping(ctx, mapping.ID)
	assert.Error(t, err)
}

// --- Encrypt/Decrypt roundtrip via Manager ---

func TestManager_EncryptDecryptConfig_LDAP(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	config := &ProviderConfig{
		LDAP: &LDAPConfig{
			Host:         "ldap.test.com",
			BindPassword: "plaintext-password",
		},
	}

	err := mgr.encryptConfig(config)
	require.NoError(t, err)
	assert.NotEqual(t, "plaintext-password", config.LDAP.BindPassword, "should be encrypted")

	mgr.decryptConfig(config)
	assert.Equal(t, "plaintext-password", config.LDAP.BindPassword, "should be decrypted back")
}

func TestManager_EncryptDecryptConfig_OAuth2(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	config := &ProviderConfig{
		OAuth2: &OAuth2Config{
			ClientID:     "public",
			ClientSecret: "super-secret",
		},
	}

	err := mgr.encryptConfig(config)
	require.NoError(t, err)
	assert.NotEqual(t, "super-secret", config.OAuth2.ClientSecret)
	assert.Equal(t, "public", config.OAuth2.ClientID, "client ID should not be encrypted")

	mgr.decryptConfig(config)
	assert.Equal(t, "super-secret", config.OAuth2.ClientSecret)
}

func TestManager_MaskConfig(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	ldapConfig := &ProviderConfig{
		LDAP: &LDAPConfig{BindPassword: "secret"},
	}
	mgr.maskConfig(ldapConfig)
	assert.Equal(t, "********", ldapConfig.LDAP.BindPassword)

	oauthConfig := &ProviderConfig{
		OAuth2: &OAuth2Config{ClientSecret: "secret"},
	}
	mgr.maskConfig(oauthConfig)
	assert.Equal(t, "********", oauthConfig.OAuth2.ClientSecret)
}

func TestManager_MaskConfig_EmptySecrets(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()

	ldapConfig := &ProviderConfig{
		LDAP: &LDAPConfig{BindPassword: ""},
	}
	mgr.maskConfig(ldapConfig)
	assert.Empty(t, ldapConfig.LDAP.BindPassword, "empty password should stay empty, not masked")

	oauthConfig := &ProviderConfig{
		OAuth2: &OAuth2Config{ClientSecret: ""},
	}
	mgr.maskConfig(oauthConfig)
	assert.Empty(t, oauthConfig.OAuth2.ClientSecret)
}

func TestManager_UpdateInvalidatesCache(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	idp := &IdentityProvider{
		Name: "Cached", Type: TypeLDAP, CreatedBy: "admin",
		Config: ProviderConfig{LDAP: &LDAPConfig{Host: "cache.ldap.com", BaseDN: "dc=cache"}},
	}
	require.NoError(t, mgr.CreateProvider(ctx, idp))

	// Simulate caching
	mgr.mu.Lock()
	mgr.providers[idp.ID] = nil // placeholder
	mgr.mu.Unlock()

	// Update should clear cache
	idp.Name = "Updated"
	require.NoError(t, mgr.UpdateProvider(ctx, idp))

	mgr.mu.RLock()
	_, cached := mgr.providers[idp.ID]
	mgr.mu.RUnlock()
	assert.False(t, cached, "cache entry should be cleared after update")
}

func TestManager_DeleteInvalidatesCache(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	idp := &IdentityProvider{
		Name: "CacheDel", Type: TypeLDAP, CreatedBy: "admin",
		Config: ProviderConfig{LDAP: &LDAPConfig{Host: "cdel.ldap.com", BaseDN: "dc=cdel"}},
	}
	require.NoError(t, mgr.CreateProvider(ctx, idp))

	mgr.mu.Lock()
	mgr.providers[idp.ID] = nil
	mgr.mu.Unlock()

	require.NoError(t, mgr.DeleteProvider(ctx, idp.ID))

	mgr.mu.RLock()
	_, cached := mgr.providers[idp.ID]
	mgr.mu.RUnlock()
	assert.False(t, cached, "cache entry should be cleared after delete")
}

func TestManager_CountLinkedUsers(t *testing.T) {
	mgr, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	count, err := mgr.CountLinkedUsers(ctx, "idp-123", "ldap")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestGenerateID(t *testing.T) {
	id1 := generateID("idp-")
	id2 := generateID("idp-")
	assert.True(t, len(id1) > 4, "generated ID should have prefix + random hex")
	assert.NotEqual(t, id1, id2, "generated IDs should be unique")
	assert.Contains(t, id1, "idp-")

	gmap := generateID("gmap-")
	assert.Contains(t, gmap, "gmap-")
}
