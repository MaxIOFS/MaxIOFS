package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A username given to a new user after a deletion does not take the tokens
// issued to the user before.
func TestATokenIsNotTheNextUserOfItsUsername(t *testing.T) {
	managerInterface, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)
	manager := managerInterface.(*authManager)
	ctx := context.Background()

	before := &User{ID: "bob-before", Username: "bob", Status: UserStatusActive, Roles: []string{"user"}}
	require.NoError(t, manager.store.CreateUser(before))
	access, err := manager.generateToken(before, TokenTypeAccess, 900)
	require.NoError(t, err)
	download, err := manager.GenerateDownloadToken(ctx, before, "bucket/key")
	require.NoError(t, err)
	twoFactor, err := manager.GenerateTwoFactorToken(ctx, before)
	require.NoError(t, err)
	got, err := manager.ValidateJWT(ctx, access)
	require.NoError(t, err)
	require.Equal(t, "bob-before", got.ID)

	require.NoError(t, manager.store.DeleteUser(before.ID))
	require.NoError(t, manager.store.CreateUser(&User{ID: "bob-after", Username: "bob", Status: UserStatusActive, Roles: []string{"admin"}}))

	_, err = manager.ValidateJWT(ctx, access)
	assert.ErrorIs(t, err, ErrInvalidToken)
	_, err = manager.ValidateDownloadToken(ctx, download, "bucket/key")
	assert.ErrorIs(t, err, ErrInvalidToken)
	_, err = manager.ValidateTwoFactorToken(ctx, twoFactor)
	assert.ErrorIs(t, err, ErrInvalidToken)
}
