package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedWith(t *testing.T, accessKey, secret string) (*authManager, error) {
	t.Helper()
	manager, tmpDir := setupTestAuthManager(t)
	t.Cleanup(func() { cleanupTestAuthManager(t, tmpDir) })

	t.Setenv(BootstrapAccessKeyEnv, accessKey)
	t.Setenv(BootstrapSecretKeyEnv, secret)

	am, ok := manager.(*authManager)
	require.True(t, ok)
	return am, SeedBootstrapAccessKey(t.Context(), manager)
}

func TestBootstrapKey_SeedsTheAdministratorsFirstKey(t *testing.T) {
	am, err := seedWith(t, "minioadmin", "minioadmin")
	require.NoError(t, err)

	key, err := am.GetAccessKey(t.Context(), "minioadmin")
	require.NoError(t, err)
	assert.Equal(t, "minioadmin", key.SecretAccessKey,
		"the secret must come back decrypted, or nothing can sign with it")
	assert.Equal(t, "admin", key.UserID)

	user, err := am.ValidateCredentials(t.Context(), "minioadmin", "minioadmin")
	require.NoError(t, err, "the seeded pair must authenticate an S3 request")
	assert.Equal(t, "admin", user.Username)
}

func TestBootstrapKey_LeavesADeploymentThatAlreadyHasOne(t *testing.T) {
	am, err := seedWith(t, "FIRSTKEY", "first-secret")
	require.NoError(t, err)

	t.Setenv(BootstrapAccessKeyEnv, "SECONDKEY")
	t.Setenv(BootstrapSecretKeyEnv, "second-secret")
	require.NoError(t, SeedBootstrapAccessKey(t.Context(), am))

	_, err = am.GetAccessKey(t.Context(), "SECONDKEY")
	assert.Error(t, err, "changing the variables after the first start must do nothing")

	keys, err := am.ListAccessKeys(t.Context(), "admin")
	require.NoError(t, err)
	assert.Len(t, keys, 1)
}

func TestBootstrapKey_RefusesAPairNothingCouldSignWith(t *testing.T) {
	for _, tc := range []struct{ name, access, secret string }{
		{"secret too short", "minioadmin", "short"},
		{"access key too short", "ab", "a-good-secret"},
		{"slash in the access key", "mini/admin", "a-good-secret"},
		{"space in the secret", "minioadmin", "a good secret"},
		{"only one of the two", "minioadmin", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := seedWith(t, tc.access, tc.secret)
			assert.Error(t, err, "the server must refuse to start rather than ignore it")
		})
	}
}

func TestBootstrapKey_DoesNothingWhenUnset(t *testing.T) {
	am, err := seedWith(t, "", "")
	require.NoError(t, err)

	keys, err := am.ListAccessKeys(t.Context(), "admin")
	require.NoError(t, err)
	assert.Empty(t, keys)
}
