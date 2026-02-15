package idp

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/db/migrations"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// setupTestStore creates a temporary SQLite database with migrations applied and returns an IDP Store
func setupTestStore(t *testing.T) (*Store, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "idp-store-test-*")
	require.NoError(t, err)

	dbPath := filepath.Join(tmpDir, "db")
	require.NoError(t, os.MkdirAll(dbPath, 0755))

	db, err := sql.Open("sqlite", filepath.Join(dbPath, "maxiofs.db")+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	require.NoError(t, err)

	migrationManager := migrations.NewMigrationManager(db, logrus.StandardLogger())
	require.NoError(t, migrationManager.Migrate())

	store := NewStore(db)

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

func makeTestProvider(id, name, pType, tenantID string) *IdentityProvider {
	now := time.Now().Unix()
	idp := &IdentityProvider{
		ID:        id,
		Name:      name,
		Type:      pType,
		TenantID:  tenantID,
		Status:    StatusActive,
		CreatedBy: "admin",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if pType == TypeLDAP {
		idp.Config = ProviderConfig{
			LDAP: &LDAPConfig{
				Host:         "ldap.example.com",
				Port:         389,
				Security:     "none",
				BindDN:       "cn=readonly,dc=example,dc=com",
				BindPassword: "bind-secret",
				BaseDN:       "dc=example,dc=com",
			},
		}
	} else {
		idp.Config = ProviderConfig{
			OAuth2: &OAuth2Config{
				Preset:       "google",
				ClientID:     "client-123",
				ClientSecret: "secret-456",
				AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
				TokenURL:     "https://oauth2.googleapis.com/token",
				UserInfoURL:  "https://openidconnect.googleapis.com/v1/userinfo",
				Scopes:       []string{"openid", "email", "profile"},
			},
		}
	}

	return idp
}

// --- Provider CRUD ---

func TestCreateAndGetProvider(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	idp := makeTestProvider("idp-test1", "Corp AD", TypeLDAP, "")

	err := store.CreateProvider(idp)
	require.NoError(t, err)

	got, err := store.GetProvider("idp-test1")
	require.NoError(t, err)
	assert.Equal(t, "idp-test1", got.ID)
	assert.Equal(t, "Corp AD", got.Name)
	assert.Equal(t, TypeLDAP, got.Type)
	assert.Equal(t, StatusActive, got.Status)
	assert.Equal(t, "", got.TenantID)
	assert.NotNil(t, got.Config.LDAP)
	assert.Equal(t, "ldap.example.com", got.Config.LDAP.Host)
}

func TestCreateProvider_OAuth2(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	idp := makeTestProvider("idp-oauth1", "Google SSO", TypeOAuth2, "")

	err := store.CreateProvider(idp)
	require.NoError(t, err)

	got, err := store.GetProvider("idp-oauth1")
	require.NoError(t, err)
	assert.Equal(t, "Google SSO", got.Name)
	assert.Equal(t, TypeOAuth2, got.Type)
	assert.Empty(t, got.TenantID)
	assert.NotNil(t, got.Config.OAuth2)
	assert.Equal(t, "google", got.Config.OAuth2.Preset)
	assert.Equal(t, "client-123", got.Config.OAuth2.ClientID)
}

func TestGetProvider_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_, err := store.GetProvider("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpdateProvider(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	idp := makeTestProvider("idp-upd", "Old Name", TypeLDAP, "")
	require.NoError(t, store.CreateProvider(idp))

	idp.Name = "New Name"
	idp.Status = StatusInactive
	idp.UpdatedAt = time.Now().Unix() + 100

	err := store.UpdateProvider(idp)
	require.NoError(t, err)

	got, err := store.GetProvider("idp-upd")
	require.NoError(t, err)
	assert.Equal(t, "New Name", got.Name)
	assert.Equal(t, StatusInactive, got.Status)
}

func TestUpdateProvider_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	idp := makeTestProvider("nonexistent", "X", TypeLDAP, "")
	err := store.UpdateProvider(idp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDeleteProvider(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	idp := makeTestProvider("idp-del", "To Delete", TypeLDAP, "")
	require.NoError(t, store.CreateProvider(idp))

	err := store.DeleteProvider("idp-del")
	require.NoError(t, err)

	_, err = store.GetProvider("idp-del")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDeleteProvider_CascadesGroupMappings(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	idp := makeTestProvider("idp-cascade", "Cascade Test", TypeLDAP, "")
	require.NoError(t, store.CreateProvider(idp))

	mapping := &GroupMapping{
		ID:            "gmap-1",
		ProviderID:    "idp-cascade",
		ExternalGroup: "CN=Admins,DC=example,DC=com",
		Role:          "admin",
		CreatedAt:     time.Now().Unix(),
		UpdatedAt:     time.Now().Unix(),
	}
	require.NoError(t, store.CreateGroupMapping(mapping))

	// Deleting the provider should also delete the mapping
	require.NoError(t, store.DeleteProvider("idp-cascade"))

	_, err := store.GetGroupMapping("gmap-1")
	assert.Error(t, err, "group mapping should be deleted along with provider")
}

func TestDeleteProvider_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	err := store.DeleteProvider("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// createTestTenant inserts a tenant row so FK constraints pass
func createTestTenant(t *testing.T, store *Store, id, name string) {
	t.Helper()
	now := time.Now().Unix()
	_, err := store.db.Exec(`INSERT INTO tenants (id, name, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		id, name, now, now)
	require.NoError(t, err)
}

func TestListProviders_All(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	createTestTenant(t, store, "tenant-a", "Tenant A")
	createTestTenant(t, store, "tenant-b", "Tenant B")

	require.NoError(t, store.CreateProvider(makeTestProvider("idp-1", "Provider 1", TypeLDAP, "")))
	require.NoError(t, store.CreateProvider(makeTestProvider("idp-2", "Provider 2", TypeOAuth2, "tenant-a")))
	require.NoError(t, store.CreateProvider(makeTestProvider("idp-3", "Provider 3", TypeLDAP, "tenant-b")))

	// Global admin sees all
	all, err := store.ListProviders("")
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestListProviders_TenantFiltered(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	createTestTenant(t, store, "tenant-a", "Tenant A")
	createTestTenant(t, store, "tenant-b", "Tenant B")

	require.NoError(t, store.CreateProvider(makeTestProvider("idp-g", "Global", TypeLDAP, "")))
	require.NoError(t, store.CreateProvider(makeTestProvider("idp-a", "Tenant A IDP", TypeOAuth2, "tenant-a")))
	require.NoError(t, store.CreateProvider(makeTestProvider("idp-b", "Tenant B IDP", TypeLDAP, "tenant-b")))

	// Tenant A admin sees global + own
	filtered, err := store.ListProviders("tenant-a")
	require.NoError(t, err)
	assert.Len(t, filtered, 2)

	ids := []string{filtered[0].ID, filtered[1].ID}
	assert.Contains(t, ids, "idp-g")
	assert.Contains(t, ids, "idp-a")
}

func TestListActiveOAuthProviders(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	require.NoError(t, store.CreateProvider(makeTestProvider("idp-ldap", "LDAP", TypeLDAP, "")))

	oauthActive := makeTestProvider("idp-oauth-active", "Google Active", TypeOAuth2, "")
	oauthActive.Status = StatusActive
	require.NoError(t, store.CreateProvider(oauthActive))

	oauthInactive := makeTestProvider("idp-oauth-inactive", "Google Inactive", TypeOAuth2, "")
	oauthInactive.Status = StatusInactive
	require.NoError(t, store.CreateProvider(oauthInactive))

	providers, err := store.ListActiveOAuthProviders()
	require.NoError(t, err)
	assert.Len(t, providers, 1)
	assert.Equal(t, "idp-oauth-active", providers[0].ID)
}

// --- Group Mapping CRUD ---

func TestCreateAndGetGroupMapping(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	require.NoError(t, store.CreateProvider(makeTestProvider("idp-gm", "GM Test", TypeLDAP, "")))

	mapping := &GroupMapping{
		ID:                "gmap-test1",
		ProviderID:        "idp-gm",
		ExternalGroup:     "CN=DevOps,OU=Groups,DC=example,DC=com",
		ExternalGroupName: "DevOps Team",
		Role:              "user",
		AutoSync:          true,
		CreatedAt:         time.Now().Unix(),
		UpdatedAt:         time.Now().Unix(),
	}

	err := store.CreateGroupMapping(mapping)
	require.NoError(t, err)

	got, err := store.GetGroupMapping("gmap-test1")
	require.NoError(t, err)
	assert.Equal(t, "gmap-test1", got.ID)
	assert.Equal(t, "idp-gm", got.ProviderID)
	assert.Equal(t, "DevOps Team", got.ExternalGroupName)
	assert.Equal(t, "user", got.Role)
	assert.True(t, got.AutoSync)
	assert.Zero(t, got.LastSyncedAt)
}

func TestGetGroupMapping_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_, err := store.GetGroupMapping("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestUpdateGroupMapping(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	require.NoError(t, store.CreateProvider(makeTestProvider("idp-gmu", "GM Update", TypeLDAP, "")))

	mapping := &GroupMapping{
		ID:            "gmap-upd",
		ProviderID:    "idp-gmu",
		ExternalGroup: "CN=Users,DC=example,DC=com",
		Role:          "user",
		CreatedAt:     time.Now().Unix(),
		UpdatedAt:     time.Now().Unix(),
	}
	require.NoError(t, store.CreateGroupMapping(mapping))

	mapping.Role = "admin"
	mapping.AutoSync = true
	mapping.UpdatedAt = time.Now().Unix() + 100

	err := store.UpdateGroupMapping(mapping)
	require.NoError(t, err)

	got, err := store.GetGroupMapping("gmap-upd")
	require.NoError(t, err)
	assert.Equal(t, "admin", got.Role)
	assert.True(t, got.AutoSync)
}

func TestUpdateGroupMapping_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	mapping := &GroupMapping{
		ID:            "nonexistent",
		ExternalGroup: "x",
		Role:          "user",
		UpdatedAt:     time.Now().Unix(),
	}
	err := store.UpdateGroupMapping(mapping)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDeleteGroupMapping(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	require.NoError(t, store.CreateProvider(makeTestProvider("idp-gmd", "GM Delete", TypeLDAP, "")))

	mapping := &GroupMapping{
		ID:            "gmap-del",
		ProviderID:    "idp-gmd",
		ExternalGroup: "CN=Test,DC=example,DC=com",
		Role:          "readonly",
		CreatedAt:     time.Now().Unix(),
		UpdatedAt:     time.Now().Unix(),
	}
	require.NoError(t, store.CreateGroupMapping(mapping))

	err := store.DeleteGroupMapping("gmap-del")
	require.NoError(t, err)

	_, err = store.GetGroupMapping("gmap-del")
	assert.Error(t, err)
}

func TestDeleteGroupMapping_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	err := store.DeleteGroupMapping("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListGroupMappings(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	require.NoError(t, store.CreateProvider(makeTestProvider("idp-lm1", "LM1", TypeLDAP, "")))
	require.NoError(t, store.CreateProvider(makeTestProvider("idp-lm2", "LM2", TypeLDAP, "")))

	now := time.Now().Unix()
	require.NoError(t, store.CreateGroupMapping(&GroupMapping{
		ID: "gm-a", ProviderID: "idp-lm1", ExternalGroup: "CN=A", Role: "admin", CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, store.CreateGroupMapping(&GroupMapping{
		ID: "gm-b", ProviderID: "idp-lm1", ExternalGroup: "CN=B", Role: "user", CreatedAt: now + 1, UpdatedAt: now + 1,
	}))
	require.NoError(t, store.CreateGroupMapping(&GroupMapping{
		ID: "gm-c", ProviderID: "idp-lm2", ExternalGroup: "CN=C", Role: "readonly", CreatedAt: now, UpdatedAt: now,
	}))

	// List for provider 1
	mappings, err := store.ListGroupMappings("idp-lm1")
	require.NoError(t, err)
	assert.Len(t, mappings, 2)

	// List for provider 2
	mappings, err = store.ListGroupMappings("idp-lm2")
	require.NoError(t, err)
	assert.Len(t, mappings, 1)
	assert.Equal(t, "gm-c", mappings[0].ID)
}

func TestUpdateGroupMappingSyncTime(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	require.NoError(t, store.CreateProvider(makeTestProvider("idp-sync", "Sync Test", TypeLDAP, "")))

	now := time.Now().Unix()
	require.NoError(t, store.CreateGroupMapping(&GroupMapping{
		ID: "gm-sync", ProviderID: "idp-sync", ExternalGroup: "CN=Sync", Role: "user", CreatedAt: now, UpdatedAt: now,
	}))

	err := store.UpdateGroupMappingSyncTime("gm-sync")
	require.NoError(t, err)

	got, err := store.GetGroupMapping("gm-sync")
	require.NoError(t, err)
	assert.NotZero(t, got.LastSyncedAt, "sync time should be set")
	assert.InDelta(t, time.Now().Unix(), got.LastSyncedAt, 5.0, "sync time should be recent")
}

func TestCountUsersWithProvider(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Count with no matching users — should return 0
	count, err := store.CountUsersWithProvider("idp-count", "ldap")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCreateGroupMapping_UniqueConstraint(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	require.NoError(t, store.CreateProvider(makeTestProvider("idp-uc", "UC Test", TypeLDAP, "")))

	now := time.Now().Unix()
	mapping := &GroupMapping{
		ID: "gm-uc1", ProviderID: "idp-uc", ExternalGroup: "CN=Same,DC=test", Role: "user", CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.CreateGroupMapping(mapping))

	// Duplicate provider_id + external_group should fail
	dup := &GroupMapping{
		ID: "gm-uc2", ProviderID: "idp-uc", ExternalGroup: "CN=Same,DC=test", Role: "admin", CreatedAt: now, UpdatedAt: now,
	}
	err := store.CreateGroupMapping(dup)
	assert.Error(t, err, "duplicate provider+group should fail unique constraint")
}
