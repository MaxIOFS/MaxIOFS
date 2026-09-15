package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func authDisabledManager(t *testing.T) (*authManager, func()) {
	t.Helper()
	manager, tmpDir := setupTestAuthManager(t)
	am, ok := manager.(*authManager)
	require.True(t, ok)
	am.config.EnableAuth = false
	return am, func() { cleanupTestAuthManager(t, tmpDir) }
}

// With authentication off the caller used to come back as "anonymous", a user
// that exists in no table, and the next step of the login — the 2FA lookup —
// answered "user not found" with a 500.
func TestAuthDisabled_ConsoleLoginReturnsAUserThatExists(t *testing.T) {
	am, cleanup := authDisabledManager(t)
	defer cleanup()

	user, err := am.ValidateConsoleCredentials(t.Context(), "admin", "whatever")
	require.NoError(t, err)
	require.NotNil(t, user)

	stored, err := am.store.GetUserByID(user.ID)
	require.NoError(t, err, "the user handed to the login flow must be in the database")
	assert.Equal(t, "admin", stored.Username)
	assert.Contains(t, stored.Roles, RoleAdmin)
}

func TestAuthDisabled_S3CredentialsReturnAUserThatExists(t *testing.T) {
	am, cleanup := authDisabledManager(t)
	defer cleanup()

	user, err := am.ValidateCredentials(t.Context(), "any-key", "any-secret")
	require.NoError(t, err)
	require.NotNil(t, user)

	_, err = am.store.GetUserByID(user.ID)
	require.NoError(t, err, "object ownership is recorded against this user")
}
