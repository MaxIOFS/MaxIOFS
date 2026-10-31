package ldap

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statuses is each check's name and status.
func statuses(checks []idp.Check) []string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.Name + " " + c.Status
	}
	return out
}

// The connection test checks each step against the directory and names the
// one that fails: the connection, the service account, then the users and
// the groups, searched as a sign-in searches them. It stops at a step the
// next ones need.
func TestTheConnectionTestNamesTheStepThatFails(t *testing.T) {
	ctx := context.Background()
	d := startDirectory(t, standardDirectory())

	checks := NewProvider(d.config()).TestConnection(ctx)
	require.Equal(t, []string{"Connection warning", "Service account passed", "Users passed", "Groups passed"}, statuses(checks))
	assert.Contains(t, checks[0].Detail, "without encryption")
	assert.Contains(t, checks[1].Detail, "cn=svc,dc=example,dc=org")
	assert.Contains(t, checks[2].Detail, "3 users under ou=people,dc=example,dc=org")
	assert.Contains(t, checks[2].Detail, "joanna (joanna@example.org)")
	assert.Contains(t, checks[3].Detail, "2 groups under ou=groups,dc=example,dc=org")
	assert.Contains(t, checks[3].Detail, "admins (1 member)")

	for name, tc := range map[string]struct {
		change func(*idp.LDAPConfig)
		want   []string
		detail string
	}{
		"wrong password": {func(c *idp.LDAPConfig) { c.BindPassword = "wrong" },
			[]string{"Connection warning", "Service account failed"}, "The directory refused cn=svc,dc=example,dc=org: wrong bind DN or password\n"},
		"no password": {func(c *idp.LDAPConfig) { c.BindPassword = "" },
			[]string{"Connection warning", "Service account failed"}, "not set"},
		"no StartTLS": {func(c *idp.LDAPConfig) { c.Security = "starttls" },
			[]string{"Connection passed", "StartTLS failed"}, ""},
		"no TLS": {func(c *idp.LDAPConfig) { c.Security = "tls" },
			[]string{"Connection failed"}, "127.0.0.1:"},
		"user base gone": {func(c *idp.LDAPConfig) { c.UserSearchBase = "ou=staff,dc=example,dc=org" },
			[]string{"Connection warning", "Service account passed", "Users failed", "Groups passed"}, "ou=staff,dc=example,dc=org does not exist"},
		"user filter of nobody": {func(c *idp.LDAPConfig) { c.UserFilter = "(objectClass=person)" },
			[]string{"Connection warning", "Service account passed", "Users failed", "Groups passed"}, "(objectClass=person)"},
		"user filter broken": {func(c *idp.LDAPConfig) { c.UserFilter = "(objectClass=inetOrgPerson" },
			[]string{"Connection warning", "Service account passed", "Users failed", "Groups passed"}, "not valid"},
		"username attribute none has": {func(c *idp.LDAPConfig) { c.AttrUsername = "sAMAccountName" },
			[]string{"Connection warning", "Service account passed", "Users warning", "Groups passed"}, "sAMAccountName"},
		"group base gone": {func(c *idp.LDAPConfig) { c.GroupSearchBase = "ou=teams,dc=example,dc=org" },
			[]string{"Connection warning", "Service account passed", "Users passed", "Groups failed"}, "ou=teams,dc=example,dc=org does not exist"},
		"default group filter": {func(c *idp.LDAPConfig) { c.GroupFilter = "" },
			[]string{"Connection warning", "Service account passed", "Users passed", "Groups warning"}, "(objectClass=group)"},
	} {
		cfg := d.config()
		tc.change(cfg)
		checks := NewProvider(cfg).TestConnection(ctx)
		require.Equal(t, tc.want, statuses(checks), name)
		assert.Contains(t, problems(checks), tc.detail, name)
	}

	gone, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	goneConfig := d.config()
	goneConfig.Port = gone.Addr().(*net.TCPAddr).Port
	require.NoError(t, gone.Close())
	checks = NewProvider(goneConfig).TestConnection(ctx)
	require.Equal(t, []string{"Connection failed"}, statuses(checks))
	assert.Contains(t, checks[0].Detail, fmt.Sprintf("127.0.0.1:%d", goneConfig.Port))
}

// problems is the details of the checks that did not pass.
func problems(checks []idp.Check) string {
	out := ""
	for _, c := range checks {
		if c.Status != idp.CheckPassed {
			out += c.Detail + "\n"
		}
	}
	return out
}
