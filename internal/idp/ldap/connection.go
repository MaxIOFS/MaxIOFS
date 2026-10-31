package ldap

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-ldap/ldap/v3"
	"github.com/maxiofs/maxiofs/internal/idp"
)

// testSampleSize is how many users and groups a connection test reads.
const testSampleSize = 100

// TestConnection connects, binds as the service account, and searches the
// users and the groups as a sign-in does.
func (p *Provider) TestConnection(ctx context.Context) []idp.Check {
	var checks []idp.Check
	conn, err := p.client.dial()
	if err != nil {
		return append(checks, check("Connection", idp.CheckFailed, "Cannot connect to %s: %v", p.client.address(), err))
	}
	defer conn.Close()

	switch p.config.Security {
	case "tls":
		checks = append(checks, check("Connection", idp.CheckPassed, "Connected to %s over TLS", p.client.address()))
	case "starttls":
		checks = append(checks, check("Connection", idp.CheckPassed, "Connected to %s", p.client.address()))
		if err := p.client.startTLS(conn); err != nil {
			return append(checks, check("StartTLS", idp.CheckFailed, "The directory did not start TLS: %v", err))
		}
		checks = append(checks, check("StartTLS", idp.CheckPassed, "TLS started"))
	default:
		checks = append(checks, check("Connection", idp.CheckWarning,
			"Connected to %s without encryption: the passwords cross the network in clear", p.client.address()))
	}

	if p.config.BindDN == "" || p.config.BindPassword == "" {
		return append(checks, check("Service account", idp.CheckFailed, "The bind DN and the password of the service account are not set"))
	}
	if err := conn.Bind(p.config.BindDN, p.config.BindPassword); err != nil {
		return append(checks, check("Service account", idp.CheckFailed, "%s", bindFailure(p.config.BindDN, err)))
	}
	checks = append(checks, check("Service account", idp.CheckPassed, "Bound as %s", p.config.BindDN))

	return append(checks, p.checkUsers(conn), p.checkGroups(conn))
}

// checkUsers searches the users: none found fails, and none with the
// username attribute cannot be found by name.
func (p *Provider) checkUsers(conn *ldap.Conn) idp.Check {
	base, filter := p.client.userSearch()
	entries, more, err := sample(conn, base, filter, p.client.getUserAttributes())
	if err != nil {
		return check("Users", idp.CheckFailed, "%s", searchFailure("user", base, filter, err))
	}
	if len(entries) == 0 {
		return check("Users", idp.CheckFailed, "No entry under %s matches %s", base, filter)
	}
	found := count(len(entries), more, "user", "users")
	attr := p.client.usernameAttr()
	for _, entry := range entries {
		if entry.GetAttributeValue(attr) == "" {
			continue
		}
		user := p.client.EntryToExternalUser(entry)
		example := user.Username
		if user.Email != "" {
			example += " (" + user.Email + ")"
		}
		return check("Users", idp.CheckPassed, "%s under %s, such as %s", found, base, example)
	}
	return check("Users", idp.CheckWarning,
		"%s under %s, none with the username attribute %s: a user added by name is not found", found, base, attr)
}

// checkGroups searches the groups: a sign-in searches them too, so a search
// that fails fails the sign-ins; none found leaves the group mappings
// nothing to map.
func (p *Provider) checkGroups(conn *ldap.Conn) idp.Check {
	base, filter := p.client.groupSearch()
	entries, more, err := sample(conn, base, filter, []string{"cn", "member", "uniqueMember"})
	if err != nil {
		return check("Groups", idp.CheckFailed, "%s", searchFailure("group", base, filter, err))
	}
	if len(entries) == 0 {
		return check("Groups", idp.CheckWarning, "No entry under %s matches %s: group mappings find no group", base, filter)
	}
	group := entries[0]
	members := len(group.GetAttributeValues("member")) + len(group.GetAttributeValues("uniqueMember"))
	return check("Groups", idp.CheckPassed, "%s under %s, such as %s (%s)",
		count(len(entries), more, "group", "groups"), base, group.GetAttributeValue("cn"), count(members, false, "member", "members"))
}

// sample is the first entries under base that match filter, and whether
// more match.
func sample(conn *ldap.Conn, base, filter string, attrs []string) ([]*ldap.Entry, bool, error) {
	result, err := conn.Search(ldap.NewSearchRequest(base, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
		testSampleSize, 0, false, filter, attrs, nil))
	if err != nil && result != nil && ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
		return result.Entries, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return result.Entries, false, nil
}

// bindFailure says why the bind as dn failed: credentials the directory
// refused, with what it said of them, or no answer.
func bindFailure(dn string, err error) string {
	var refused *ldap.Error
	switch {
	case ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials):
		detail := fmt.Sprintf("The directory refused %s: wrong bind DN or password", dn)
		if errors.As(err, &refused) && refused.Err != nil && refused.Err.Error() != "" {
			detail += " (" + refused.Err.Error() + ")"
		}
		return detail
	case ldap.IsErrorWithCode(err, ldap.ErrorNetwork):
		return fmt.Sprintf("The directory did not answer the bind of %s: %v", dn, err)
	}
	return fmt.Sprintf("The directory refused %s: %v", dn, err)
}

// searchFailure says why the search of what under base by filter failed.
func searchFailure(what, base, filter string, err error) string {
	switch {
	case ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject):
		return fmt.Sprintf("The %s search base %s does not exist", what, base)
	case ldap.IsErrorWithCode(err, ldap.ErrorFilterCompile):
		return fmt.Sprintf("The %s filter %s is not valid: %v", what, filter, err)
	}
	return fmt.Sprintf("The %s search under %s failed: %v", what, base, err)
}

// count is n of a noun, more than n when more matched.
func count(n int, more bool, one, many string) string {
	noun := many
	if n == 1 && !more {
		noun = one
	}
	if more {
		return fmt.Sprintf("More than %d %s", n, noun)
	}
	return fmt.Sprintf("%d %s", n, noun)
}

func check(name, status, format string, args ...any) idp.Check {
	return idp.Check{Name: name, Status: status, Detail: fmt.Sprintf(format, args...)}
}
