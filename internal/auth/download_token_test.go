package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func downloadTokenUser(t *testing.T, am *authManager) *User {
	t.Helper()
	user := &User{
		ID:       "download-user",
		Username: "download-user",
		Status:   UserStatusActive,
		Roles:    []string{"user"},
	}
	require.NoError(t, am.store.CreateUser(user))
	return user
}

// TestDownloadToken_IsNotASession is the one that matters most: a token in a
// URL must not be usable as a credential for the rest of the API.
func TestDownloadToken_IsNotASession(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()
	user := downloadTokenUser(t, am)

	token, err := am.GenerateDownloadToken(ctx, user, "tenant\x00bucket\x00key\x00")
	require.NoError(t, err)

	_, err = am.ValidateJWT(ctx, token)
	assert.ErrorIs(t, err, ErrInvalidToken,
		"a download token must not authenticate anything but its own download")
}

// TestDownloadToken_OpensOnlyWhatItNames: the resource is the whole scope.
func TestDownloadToken_OpensOnlyWhatItNames(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()
	user := downloadTokenUser(t, am)

	const granted = "\x00backups\x00report.pdf\x00"
	token, err := am.GenerateDownloadToken(ctx, user, granted)
	require.NoError(t, err)

	resolved, err := am.ValidateDownloadToken(ctx, token, granted)
	require.NoError(t, err)
	assert.Equal(t, user.ID, resolved.ID)

	for _, other := range []string{
		"\x00backups\x00payroll.xlsx\x00",       // another key
		"\x00secrets\x00report.pdf\x00",         // another bucket
		"tenant-a\x00backups\x00report.pdf\x00", // another tenant
		"\x00backups\x00report.pdf\x00v2",       // another version
	} {
		_, err := am.ValidateDownloadToken(ctx, token, other)
		assert.ErrorIs(t, err, ErrInvalidToken,
			"a token for %q must not open %q", granted, other)
	}
}

// TestDownloadToken_SessionTokensAreNotDownloadTokens closes the other
// direction: an ordinary access token must not be redeemable as one, or the
// scoping would be optional.
func TestDownloadToken_SessionTokensAreNotDownloadTokens(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()
	user := downloadTokenUser(t, am)

	access, err := am.GenerateJWT(ctx, user)
	require.NoError(t, err)

	_, err = am.ValidateDownloadToken(ctx, access, "\x00backups\x00report.pdf\x00")
	assert.ErrorIs(t, err, ErrInvalidToken)
}

// TestDownloadToken_RefusesADisabledUser: revoking someone must take effect
// before their outstanding links expire.
func TestDownloadToken_RefusesADisabledUser(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()
	user := downloadTokenUser(t, am)

	const resource = "\x00backups\x00report.pdf\x00"
	token, err := am.GenerateDownloadToken(ctx, user, resource)
	require.NoError(t, err)

	user.Status = UserStatusInactive
	require.NoError(t, am.store.UpdateUser(user))

	_, err = am.ValidateDownloadToken(ctx, token, resource)
	assert.Error(t, err, "a suspended account's links stop working immediately")
}

// TestDownloadToken_RequiresAResource: a token naming nothing would be a
// token for everything.
func TestDownloadToken_RequiresAResource(t *testing.T) {
	am, _, cleanup := setupSTSTest(t)
	defer cleanup()
	ctx := context.Background()
	user := downloadTokenUser(t, am)

	_, err := am.GenerateDownloadToken(ctx, user, "")
	assert.Error(t, err)

	token, err := am.GenerateDownloadToken(ctx, user, "\x00b\x00k\x00")
	require.NoError(t, err)
	_, err = am.ValidateDownloadToken(ctx, token, "")
	assert.ErrorIs(t, err, ErrInvalidToken)
}
