package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An email names one user: as its email or as its username, in any case,
// whichever way the user signs in.
func TestAnEmailNamesOneUser(t *testing.T) {
	manager, tmpDir := setupTestAuthManager(t)
	defer cleanupTestAuthManager(t, tmpDir)
	ctx := context.Background()

	require.NoError(t, manager.CreateUser(ctx, &User{ID: "juan", Username: "juan", Password: "Secret-pass-1", Email: "juan@example.org"}))
	require.NoError(t, manager.CreateUser(ctx, &User{ID: "ana", Username: "ana", Password: "Secret-pass-1"}))
	require.NoError(t, manager.CreateUser(ctx, &User{ID: "maria", Username: "maria@example.org", Password: "Secret-pass-1"}))

	for name, user := range map[string]*User{
		"its email as another's email":    {ID: "u1", Username: "juan2", Password: "Secret-pass-1", Email: "juan@example.org"},
		"its email as another's name":     {ID: "u4", Username: "maria2", Password: "Secret-pass-1", Email: "MARIA@example.org"},
		"its email as another's username": {ID: "u2", Username: "Juan@Example.org", AuthProvider: "oauth:p1", ExternalID: "Juan@Example.org", Email: "Juan@Example.org"},
		"its username is another's email": {ID: "u3", Username: "juan@example.org", Password: "Secret-pass-1"},
	} {
		t.Run(name, func(t *testing.T) {
			err := manager.CreateUser(ctx, user)
			assert.True(t, errors.Is(err, ErrEmailInUse), "%v", err)
			_, err = manager.GetUserByUsername(ctx, user.Username)
			assert.Error(t, err, "the user must not be created")
		})
	}

	t.Run("an email no user has", func(t *testing.T) {
		require.NoError(t, manager.CreateUser(ctx, &User{ID: "pedro", Username: "pedro@example.org", AuthProvider: "oauth:p1",
			ExternalID: "pedro@example.org", Email: "pedro@example.org"}))
	})

	t.Run("users without an email", func(t *testing.T) {
		require.NoError(t, manager.CreateUser(ctx, &User{ID: "luis", Username: "luis", Password: "Secret-pass-1"}))
	})

	t.Run("an edit to another's email", func(t *testing.T) {
		ana, err := manager.GetUserByUsername(ctx, "ana")
		require.NoError(t, err)
		ana.Email = "PEDRO@example.org"
		assert.True(t, errors.Is(manager.UpdateUser(ctx, ana), ErrEmailInUse))
		stored, err := manager.GetUserByUsername(ctx, "ana")
		require.NoError(t, err)
		assert.Empty(t, stored.Email)
	})

	t.Run("an edit that keeps the email", func(t *testing.T) {
		juan, err := manager.GetUserByUsername(ctx, "juan")
		require.NoError(t, err)
		juan.DisplayName = "Juan Pérez"
		require.NoError(t, manager.UpdateUser(ctx, juan))
	})

	t.Run("an edit of a user that shared its email before the rule", func(t *testing.T) {
		store := manager.(*authManager).store
		_, err := store.db.Exec(`UPDATE users SET email = 'juan@example.org' WHERE id = 'luis'`)
		require.NoError(t, err)
		luis, err := manager.GetUserByUsername(ctx, "luis")
		require.NoError(t, err)
		luis.Roles = []string{RoleUser}
		require.NoError(t, manager.UpdateUser(ctx, luis))
	})

	t.Run("an edit to an email no user has", func(t *testing.T) {
		ana, err := manager.GetUserByUsername(ctx, "ana")
		require.NoError(t, err)
		ana.Email = "ana@example.org"
		require.NoError(t, manager.UpdateUser(ctx, ana))
	})
}
