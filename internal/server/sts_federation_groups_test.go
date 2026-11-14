package server

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/maxiofs/maxiofs/internal/auth"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// passwordDirectory signs in its one user with its one password and gives the
// groups it holds now: an LDAP directory, without the network.
type passwordDirectory struct {
	memberDirectory
	groups []string
}

var passwordDirectories = map[string]*passwordDirectory{}

func init() {
	idp.RegisterProvider("password-directory", func(provider *idp.IdentityProvider, _ string) (idp.Provider, error) {
		return passwordDirectories[provider.Name], nil
	})
}

func (d *passwordDirectory) AuthenticateUser(_ context.Context, username, password string) (*idp.ExternalUser, error) {
	if password != "Directory-pass-1!" {
		return nil, errors.New("invalid credentials")
	}
	return &idp.ExternalUser{ExternalID: username, Username: username, Groups: d.groups}, nil
}

// A user that follows its provider's groups gets temporary credentials as it
// signs in to the console: with the role its groups map to now, and none when
// they map to none. One an administrator keeps is not held to them.
func TestSTSFederationFollowsTheProvidersGroups(t *testing.T) {
	t.Run("web identity", func(t *testing.T) {
		n := newSSONode(t)
		_, failure := n.signIn(t)
		require.Empty(t, failure)
		ctx := context.Background()
		resolve := func() (*auth.User, error) {
			return n.server.resolveWebIdentity(ctx, httptest.NewRequest("POST", "/", nil), n.provider, "at")
		}

		user, err := resolve()
		require.NoError(t, err)
		assert.Equal(t, []string{auth.RoleAdmin}, user.Roles)

		n.issuer.groups = []string{"devs"}
		user, err = resolve()
		require.NoError(t, err)
		assert.Equal(t, []string{auth.RoleUser}, user.Roles, "the role its groups map to now")

		n.issuer.groups = []string{"accounting"}
		_, err = resolve()
		assert.Error(t, err, "in no group mapped")

		kept := userNamed(t, n.server, "alice@example.org")
		delete(kept.Metadata, provisionedByKey)
		require.NoError(t, n.server.authManager.UpdateUser(ctx, kept))
		_, err = resolve()
		assert.NoError(t, err, "a user the administrator keeps")
	})

	t.Run("LDAP", func(t *testing.T) {
		s := newClusterTestNode(t)
		ctx := context.Background()
		dir := &passwordDirectory{groups: []string{"admins"}}
		passwordDirectories["sts-ldap"] = dir
		provider := &idp.IdentityProvider{Name: "sts-ldap", Type: "password-directory", Status: idp.StatusActive}
		require.NoError(t, s.idpManager.CreateProvider(ctx, provider))
		for group, role := range map[string]string{"admins": auth.RoleAdmin, "devs": auth.RoleUser} {
			require.NoError(t, s.idpManager.CreateGroupMapping(ctx, &idp.GroupMapping{ProviderID: provider.ID, ExternalGroup: group, Role: role}))
		}
		user := &auth.User{ID: "bob", Username: "bob", Status: auth.UserStatusActive, Roles: []string{auth.RoleAdmin},
			AuthProvider: "ldap:" + provider.ID, ExternalID: "uid=bob"}
		markGroupManaged(user)
		require.NoError(t, s.authManager.CreateUser(ctx, user))
		resolve := func() (*auth.User, error) {
			return s.resolveLDAPIdentity(ctx, provider.ID, "bob", "Directory-pass-1!")
		}

		got, err := resolve()
		require.NoError(t, err)
		assert.Equal(t, []string{auth.RoleAdmin}, got.Roles)

		dir.groups = []string{"devs"}
		got, err = resolve()
		require.NoError(t, err)
		assert.Equal(t, []string{auth.RoleUser}, got.Roles, "the role its groups map to now")

		dir.groups = []string{"accounting"}
		_, err = resolve()
		assert.Error(t, err, "in no group mapped")
	})
}
