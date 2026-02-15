package ldap

import (
	"crypto/tls"
	"fmt"
	"strings"

	"github.com/go-ldap/ldap/v3"
	"github.com/maxiofs/maxiofs/internal/idp"
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
	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)

	var conn *ldap.Conn
	var err error

	switch c.config.Security {
	case "tls":
		conn, err = ldap.DialTLS("tcp", addr, &tls.Config{
			ServerName: c.config.Host,
		})
	case "starttls":
		conn, err = ldap.DialURL(fmt.Sprintf("ldap://%s", addr))
		if err != nil {
			return nil, fmt.Errorf("failed to connect to LDAP server: %w", err)
		}
		err = conn.StartTLS(&tls.Config{
			ServerName: c.config.Host,
		})
	default: // "none"
		conn, err = ldap.DialURL(fmt.Sprintf("ldap://%s", addr))
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to LDAP server: %w", err)
	}

	return conn, nil
}

// BindAdmin binds with the service account credentials
func (c *Client) BindAdmin(conn *ldap.Conn) error {
	if err := conn.Bind(c.config.BindDN, c.config.BindPassword); err != nil {
		return fmt.Errorf("failed to bind with admin credentials: %w", err)
	}
	return nil
}

// BindUser binds with user credentials for authentication
func (c *Client) BindUser(conn *ldap.Conn, userDN, password string) error {
	if err := conn.Bind(userDN, password); err != nil {
		return fmt.Errorf("LDAP authentication failed: %w", err)
	}
	return nil
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
		0,    // no time limit
		false,
		filter,
		attrs,
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("LDAP search failed: %w", err)
	}

	return result.Entries, nil
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

	result, err := conn.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("LDAP group search failed: %w", err)
	}

	return result.Entries, nil
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

	result, err := conn.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to get group: %w", err)
	}

	if len(result.Entries) == 0 {
		return nil, fmt.Errorf("group not found: %s", groupDN)
	}

	// Get member DNs
	memberDNs := result.Entries[0].GetAttributeValues("member")
	if len(memberDNs) == 0 {
		memberDNs = result.Entries[0].GetAttributeValues("uniqueMember")
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

		memberResult, err := conn.Search(memberSearch)
		if err != nil {
			continue // Skip members we can't look up
		}

		if len(memberResult.Entries) > 0 {
			members = append(members, memberResult.Entries[0])
		}
	}

	return members, nil
}

// FindUserDN finds the DN of a user by their username attribute
func (c *Client) FindUserDN(conn *ldap.Conn, username string) (string, error) {
	safeUsername := EscapeFilter(username)

	searchBase := c.config.UserSearchBase
	if searchBase == "" {
		searchBase = c.config.BaseDN
	}

	attrUsername := c.config.AttrUsername
	if attrUsername == "" {
		attrUsername = "uid"
	}

	userFilter := c.config.UserFilter
	if userFilter == "" {
		userFilter = "(objectClass=person)"
	}

	filter := fmt.Sprintf("(&%s(%s=%s))", userFilter, attrUsername, safeUsername)

	searchRequest := ldap.NewSearchRequest(
		searchBase,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		1,
		0,
		false,
		filter,
		[]string{"dn"},
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		return "", fmt.Errorf("failed to find user DN: %w", err)
	}

	if len(result.Entries) == 0 {
		return "", fmt.Errorf("user not found: %s", username)
	}

	return result.Entries[0].DN, nil
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
