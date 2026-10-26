package ldap

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A directory of more users than one passes the connection test.
func TestTheConnectionTestPassesWithManyUsers(t *testing.T) {
	d := startDirectory(t, standardDirectory())
	require.NoError(t, NewProvider(d.config()).TestConnection(context.Background()))
}

// A search gives the first users the limit takes, however many match.
func TestASearchGivesTheFirstMatchesBeyondTheLimit(t *testing.T) {
	d := startDirectory(t, standardDirectory())
	users, err := NewProvider(d.config()).SearchUsers(context.Background(), "", 2)
	require.NoError(t, err)
	assert.Len(t, users, 2)
}

// A user signs in with the external ID the console kept, the DN a search or a
// group gave or the name typed on the users page, and is that user, not one
// whose name holds it. The service account is no user.
func TestAUserSignsInByDNOrByName(t *testing.T) {
	ctx := context.Background()
	d := startDirectory(t, standardDirectory())
	p := NewProvider(d.config())
	for _, id := range []string{"uid=ann,ou=people,dc=example,dc=org", "ann"} {
		user, err := p.AuthenticateUser(ctx, id, "ann-pass")
		require.NoError(t, err, id)
		assert.Equal(t, "uid=ann,ou=people,dc=example,dc=org", user.ExternalID, id)
		assert.Equal(t, "ann", user.Username, id)
		assert.Equal(t, "ann@example.org", user.Email, id)
	}

	_, err := p.AuthenticateUser(ctx, "ann", "joanna-pass")
	require.Error(t, err)
	assert.NotErrorIs(t, err, idp.ErrUnavailable, "a wrong password")
	_, err = p.AuthenticateUser(ctx, "uid=nobody,ou=people,dc=example,dc=org", "x")
	require.Error(t, err)
	assert.NotErrorIs(t, err, idp.ErrUnavailable, "a DN no entry has")
	_, err = p.AuthenticateUser(ctx, "cn=svc,dc=example,dc=org", "svc-pass")
	assert.Error(t, err, "the service account")
}

// A directory that takes the connection and never answers, or that is not
// there, is unavailable within the timeouts: not a wrong password.
func TestAnUnansweringDirectoryIsUnavailableInTime(t *testing.T) {
	formerDial, formerRequest := dialTimeout, requestTimeout
	dialTimeout, requestTimeout = 500*time.Millisecond, 500*time.Millisecond
	defer func() { dialTimeout, requestTimeout = formerDial, formerRequest }()
	ctx := context.Background()

	frozen := startDirectory(t, &fakeDirectory{frozen: true})
	gone, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	goneConfig := frozen.config()
	goneConfig.Port = gone.Addr().(*net.TCPAddr).Port
	require.NoError(t, gone.Close())

	for name, cfg := range map[string]*idp.LDAPConfig{"frozen": frozen.config(), "gone": goneConfig} {
		p := NewProvider(cfg)
		started := time.Now()
		_, err := p.AuthenticateUser(ctx, "ann", "ann-pass")
		assert.ErrorIs(t, err, idp.ErrUnavailable, name)
		assert.ErrorIs(t, p.TestConnection(ctx), idp.ErrUnavailable, name)
		_, err = p.SearchUsers(ctx, "a", 10)
		assert.ErrorIs(t, err, idp.ErrUnavailable, name)
		assert.Less(t, time.Since(started), 5*time.Second, name)
	}
}

// A sign-in gives the groups the user is in, found by member for a directory
// without memberOf, and the user's DN to ask about the sign-in later.
func TestASignInGivesTheUsersGroupsAndDN(t *testing.T) {
	d := startDirectory(t, standardDirectory())
	user, err := NewProvider(d.config()).AuthenticateUser(context.Background(), "ann", "ann-pass")
	require.NoError(t, err)
	assert.Equal(t, []string{"cn=admins,ou=groups,dc=example,dc=org"}, user.Groups)
	require.NotNil(t, user.Grant)
	assert.Equal(t, "uid=ann,ou=people,dc=example,dc=org", user.Grant.Subject)
}

// A sign-in is asked about again with the service account: it stands with the
// user's groups as they are now, and ends for a user gone from the directory
// or disabled in it. A directory that does not answer ends nothing.
func TestRevalidationEndsTheSignInOfAUserGoneOrDisabled(t *testing.T) {
	ctx := context.Background()
	d := startDirectory(t, standardDirectory())
	p := NewProvider(d.config())
	ann := idp.Grant{Subject: "uid=ann,ou=people,dc=example,dc=org"}
	bob := idp.Grant{Subject: "uid=bob,ou=people,dc=example,dc=org"}

	user, err := p.Revalidate(ctx, ann)
	require.NoError(t, err)
	assert.Equal(t, []string{"cn=admins,ou=groups,dc=example,dc=org"}, user.Groups)

	d.setAttr("cn=admins,ou=groups,dc=example,dc=org", "member", "uid=bob,ou=people,dc=example,dc=org")
	user, err = p.Revalidate(ctx, ann)
	require.NoError(t, err)
	assert.Empty(t, user.Groups, "taken out of admins")

	d.remove(ann.Subject)
	_, err = p.Revalidate(ctx, ann)
	assert.ErrorIs(t, err, idp.ErrSessionEnded, "gone")

	d.setAttr(bob.Subject, "userAccountControl", "514")
	_, err = p.Revalidate(ctx, bob)
	assert.ErrorIs(t, err, idp.ErrSessionEnded, "disabled")

	formerDial, formerRequest := dialTimeout, requestTimeout
	dialTimeout, requestTimeout = 500*time.Millisecond, 500*time.Millisecond
	defer func() { dialTimeout, requestTimeout = formerDial, formerRequest }()
	frozen := startDirectory(t, &fakeDirectory{frozen: true})
	_, err = NewProvider(frozen.config()).Revalidate(ctx, ann)
	assert.ErrorIs(t, err, idp.ErrUnavailable)
	assert.NotErrorIs(t, err, idp.ErrSessionEnded)
}
