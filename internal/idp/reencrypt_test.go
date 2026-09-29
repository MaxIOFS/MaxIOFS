package idp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Re-encryption rewrites the bind passwords and client secrets the old secret
// encrypted, leaves the ones the new secret encrypted and every other field,
// and names the ones neither decrypts. A check names them and writes nothing.
func TestReencryptRewritesTheProviderSecrets(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	seal := func(plain, key string) string {
		t.Helper()
		v, err := Encrypt(plain, key)
		require.NoError(t, err)
		return v
	}
	create := func(id, pType, secret string) {
		t.Helper()
		p := makeTestProvider(id, id, pType, "")
		if pType == TypeLDAP {
			p.Config.LDAP.BindPassword = secret
		} else {
			p.Config.OAuth2.ClientSecret = secret
		}
		require.NoError(t, store.CreateProvider(p))
	}
	create("mine", TypeLDAP, seal("mine-password", "old"))
	create("theirs", TypeOAuth2, seal("theirs-secret", "new"))
	create("lost", TypeLDAP, seal("lost-password", "other"))
	create("none", TypeLDAP, "")
	config := func(id string) ProviderConfig {
		t.Helper()
		p, err := store.GetProvider(id)
		require.NoError(t, err)
		return p.Config
	}
	run := func(from, to string) []string {
		t.Helper()
		tx, err := store.db.BeginTx(ctx, nil)
		require.NoError(t, err)
		names, err := ReencryptSecrets(ctx, tx, from, to)
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
		return names
	}

	before := config("mine").LDAP.BindPassword
	assert.ElementsMatch(t, []string{"identity provider theirs (OAuth client secret)", "identity provider lost (LDAP bind password)"},
		run("old", "old"))
	assert.Equal(t, before, config("mine").LDAP.BindPassword, "a check writes nothing")

	theirs, lost := config("theirs").OAuth2.ClientSecret, config("lost").LDAP.BindPassword
	assert.Equal(t, []string{"identity provider lost (LDAP bind password)"}, run("old", "new"))
	mine := config("mine")
	plain, err := Decrypt(mine.LDAP.BindPassword, "new")
	require.NoError(t, err)
	assert.Equal(t, "mine-password", plain)
	assert.Equal(t, "ldap.example.com", mine.LDAP.Host, "the rest of the configuration is kept")
	assert.Equal(t, "cn=readonly,dc=example,dc=com", mine.LDAP.BindDN)
	assert.Equal(t, theirs, config("theirs").OAuth2.ClientSecret)
	assert.Equal(t, lost, config("lost").LDAP.BindPassword, "what no secret decrypts is left as it is")
	assert.Empty(t, config("none").LDAP.BindPassword)

	tx, err := store.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback() //nolint:errcheck
	_, err = ReencryptSecrets(ctx, tx, "", "new")
	assert.Error(t, err)
}
