package ldap

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/maxiofs/maxiofs/internal/idp"
)

// dialTimeout bounds the connection to the directory and requestTimeout each
// request on it, so a directory that does not answer fails a sign-in in time.
var (
	dialTimeout    = 10 * time.Second
	requestTimeout = 15 * time.Second
)

// Client wraps an LDAP connection with helper methods
type Client struct {
	config *idp.LDAPConfig
}

// NewClient creates a new LDAP client
func NewClient(config *idp.LDAPConfig) *Client {
	return &Client{config: config}
}

// Connect establishes a connection to the LDAP server
func (c *Client) Connect() (*ldap.Conn, error) {
	addr := net.JoinHostPort(c.config.Host, strconv.Itoa(c.config.Port))
	dialer := ldap.DialWithDialer(&net.Dialer{Timeout: dialTimeout})
	tlsConfig := &tls.Config{ServerName: c.config.Host}

	var conn *ldap.Conn
	var err error
	switch c.config.Security {
	case "tls":
		conn, err = ldap.DialURL("ldaps://"+addr, dialer, ldap.DialWithTLSConfig(tlsConfig))
	default: // "none", "starttls"
		conn, err = ldap.DialURL("ldap://"+addr, dialer)
	}
	if err != nil {
		return nil, unavailable(fmt.Errorf("failed to connect to LDAP server: %w", err))
	}
	conn.SetTimeout(requestTimeout)

	if c.config.Security == "starttls" {
		if err := conn.StartTLS(tlsConfig); err != nil {
			conn.Close()
			return nil, unavailable(fmt.Errorf("failed to start TLS with the LDAP server: %w", err))
		}
	}
	return conn, nil
}

// unavailable marks err as the directory's when it is: the directory could
// not be reached, did not answer in time, or said it is busy.
func unavailable(err error) error {
	if err == nil || errors.Is(err, idp.ErrUnavailable) {
		return err
	}
	if ldap.IsErrorAnyOf(err, ldap.ErrorNetwork, ldap.LDAPResultBusy, ldap.LDAPResultUnavailable) {
		return fmt.Errorf("%w: %w", idp.ErrUnavailable, err)
	}
	return err
}

// BindAdmin binds with the service account credentials. A directory that
// refuses them cannot be used by anybody, so it is unavailable.
func (c *Client) BindAdmin(conn *ldap.Conn) error {
	if err := conn.Bind(c.config.BindDN, c.config.BindPassword); err != nil {
		return fmt.Errorf("%w: failed to bind with admin credentials: %w", idp.ErrUnavailable, err)
	}
	return nil
}

// BindUser binds with user credentials for authentication
func (c *Client) BindUser(conn *ldap.Conn, userDN, password string) error {
	if err := conn.Bind(userDN, password); err != nil {
		return unavailable(fmt.Errorf("LDAP authentication failed: %w", err))
	}
	return nil
}

// search runs req, keeping the entries of one a size limit cut short: a
// directory answers the first entries and then says the limit was reached.
func search(conn *ldap.Conn, req *ldap.SearchRequest) ([]*ldap.Entry, error) {
	result, err := conn.Search(req)
	if err != nil && result != nil && ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
		return result.Entries, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	return result.Entries, nil
}

// SearchUsers searches for users matching the query
func (c *Client) SearchUsers(conn *ldap.Conn, query string, limit int) ([]*ldap.Entry, error) {
	// Sanitize query to prevent LDAP injection
	safeQuery := EscapeFilter(query)

	searchBase := c.config.UserSearchBase
	if searchBase == "" {
		searchBase = c.config.BaseDN
	}

	userFilter := c.config.UserFilter
	if userFilter == "" {
		userFilter = "(objectClass=person)"
	}

	// Build search filter combining user filter with query
	var filter string
	if safeQuery != "" {
		attrUsername := c.config.AttrUsername
		if attrUsername == "" {
			attrUsername = "uid"
		}
		attrEmail := c.config.AttrEmail
		if attrEmail == "" {
			attrEmail = "mail"
		}
		attrDisplayName := c.config.AttrDisplayName
		if attrDisplayName == "" {
			attrDisplayName = "displayName"
		}
		filter = fmt.Sprintf("(&%s(|(%s=*%s*)(%s=*%s*)(%s=*%s*)))",
			userFilter, attrUsername, safeQuery, attrEmail, safeQuery, attrDisplayName, safeQuery)
	} else {
		filter = userFilter
	}

	attrs := c.getUserAttributes()

	searchRequest := ldap.NewSearchRequest(
		searchBase,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		limit,
		0, // no time limit
		false,
		filter,
		attrs,
		nil,
	)

	entries, err := search(conn, searchRequest)
	if err != nil {
		return nil, fmt.Errorf("LDAP search failed: %w", err)
	}
	return entries, nil
}

// SearchGroups searches for groups matching the query
func (c *Client) SearchGroups(conn *ldap.Conn, query string, limit int) ([]*ldap.Entry, error) {
	safeQuery := EscapeFilter(query)

	searchBase := c.config.GroupSearchBase
	if searchBase == "" {
		searchBase = c.config.BaseDN
	}

	groupFilter := c.config.GroupFilter
	if groupFilter == "" {
		groupFilter = "(objectClass=group)"
	}

	var filter string
	if safeQuery != "" {
		filter = fmt.Sprintf("(&%s(cn=*%s*))", groupFilter, safeQuery)
	} else {
		filter = groupFilter
	}

	searchRequest := ldap.NewSearchRequest(
		searchBase,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		limit,
		0,
		false,
		filter,
		[]string{"dn", "cn", "description", "member"},
		nil,
	)

	entries, err := search(conn, searchRequest)
	if err != nil {
		return nil, fmt.Errorf("LDAP group search failed: %w", err)
	}
	return entries, nil
}

// GetGroupMembers retrieves all members of a given group DN
func (c *Client) GetGroupMembers(conn *ldap.Conn, groupDN string) ([]*ldap.Entry, error) {
	// First find the group and get its members
	searchRequest := ldap.NewSearchRequest(
		groupDN,
		ldap.ScopeBaseObject,
		ldap.NeverDerefAliases,
		0,
		0,
		false,
		"(objectClass=*)",
		[]string{"member", "uniqueMember"},
		nil,
	)

	groups, err := search(conn, searchRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to get group: %w", err)
	}

	if len(groups) == 0 {
		return nil, fmt.Errorf("group not found: %s", groupDN)
	}

	// Get member DNs
	memberDNs := groups[0].GetAttributeValues("member")
	if len(memberDNs) == 0 {
		memberDNs = groups[0].GetAttributeValues("uniqueMember")
	}

	// Look up each member
	attrs := c.getUserAttributes()
	var members []*ldap.Entry

	for _, memberDN := range memberDNs {
		memberSearch := ldap.NewSearchRequest(
			memberDN,
			ldap.ScopeBaseObject,
			ldap.NeverDerefAliases,
			1,
			0,
			false,
			"(objectClass=*)",
			attrs,
			nil,
		)

		found, err := search(conn, memberSearch)
		if errors.Is(err, idp.ErrUnavailable) {
			return nil, err
		}
		if err != nil {
			continue // Skip members we can't look up
		}

		if len(found) > 0 {
			members = append(members, found[0])
		}
	}

	return members, nil
}

// errUserNotFound is the error of an external ID no user of the directory has.
var errUserNotFound = errors.New("user not found")

// FindUser finds the user an external ID names, with its attributes: the
// entry at the ID when it is a DN, else the entry whose username attribute is
// the ID. Either way the entry has to match the user filter.
func (c *Client) FindUser(conn *ldap.Conn, externalID string) (*ldap.Entry, error) {
	userFilter := c.config.UserFilter
	if userFilter == "" {
		userFilter = "(objectClass=person)"
	}

	if _, err := ldap.ParseDN(externalID); err == nil && strings.Contains(externalID, "=") {
		entries, err := search(conn, ldap.NewSearchRequest(externalID, ldap.ScopeBaseObject, ldap.NeverDerefAliases,
			1, 0, false, userFilter, c.getUserAttributes(), nil))
		if err != nil && !ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
			return nil, fmt.Errorf("failed to find user: %w", err)
		}
		if len(entries) == 1 {
			return entries[0], nil
		}
	}

	searchBase := c.config.UserSearchBase
	if searchBase == "" {
		searchBase = c.config.BaseDN
	}
	attrUsername := c.config.AttrUsername
	if attrUsername == "" {
		attrUsername = "uid"
	}
	filter := fmt.Sprintf("(&%s(%s=%s))", userFilter, attrUsername, EscapeFilter(externalID))
	entries, err := search(conn, ldap.NewSearchRequest(searchBase, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases,
		2, 0, false, filter, c.getUserAttributes(), nil))
	if err != nil {
		return nil, fmt.Errorf("failed to find user: %w", err)
	}
	if len(entries) != 1 {
		return nil, fmt.Errorf("%w: %s", errUserNotFound, externalID)
	}
	return entries[0], nil
}

// getUserAttributes returns the list of attributes to fetch for users
func (c *Client) getUserAttributes() []string {
	attrs := []string{"dn"}

	addIfSet := func(attr, fallback string) {
		if attr != "" {
			attrs = append(attrs, attr)
		} else {
			attrs = append(attrs, fallback)
		}
	}

	addIfSet(c.config.AttrUsername, "uid")
	addIfSet(c.config.AttrEmail, "mail")
	addIfSet(c.config.AttrDisplayName, "displayName")
	addIfSet(c.config.AttrMemberOf, "memberOf")

	// Also fetch sAMAccountName for AD compatibility
	attrs = append(attrs, "sAMAccountName", "cn")

	return attrs
}

// EntryToExternalUser converts an LDAP entry to an ExternalUser
func (c *Client) EntryToExternalUser(entry *ldap.Entry) idp.ExternalUser {
	attrUsername := c.config.AttrUsername
	if attrUsername == "" {
		attrUsername = "uid"
	}
	attrEmail := c.config.AttrEmail
	if attrEmail == "" {
		attrEmail = "mail"
	}
	attrDisplayName := c.config.AttrDisplayName
	if attrDisplayName == "" {
		attrDisplayName = "displayName"
	}
	attrMemberOf := c.config.AttrMemberOf
	if attrMemberOf == "" {
		attrMemberOf = "memberOf"
	}

	username := entry.GetAttributeValue(attrUsername)
	if username == "" {
		username = entry.GetAttributeValue("sAMAccountName")
	}
	if username == "" {
		username = entry.GetAttributeValue("cn")
	}

	rawAttrs := make(map[string]string)
	for _, attr := range entry.Attributes {
		if len(attr.Values) > 0 {
			rawAttrs[attr.Name] = strings.Join(attr.Values, ", ")
		}
	}

	return idp.ExternalUser{
		ExternalID:  entry.DN,
		Username:    username,
		Email:       entry.GetAttributeValue(attrEmail),
		DisplayName: entry.GetAttributeValue(attrDisplayName),
		Groups:      entry.GetAttributeValues(attrMemberOf),
		RawAttrs:    rawAttrs,
	}
}

// EscapeFilter escapes special characters in LDAP filter values per RFC 4515
func EscapeFilter(s string) string {
	return ldap.EscapeFilter(s)
}
