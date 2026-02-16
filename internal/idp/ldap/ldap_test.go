package ldap

import (
	"testing"

	goLdap "github.com/go-ldap/ldap/v3"
	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Client Unit Tests (no LDAP server required)
// =============================================================================

func TestNewClient(t *testing.T) {
	config := &idp.LDAPConfig{
		Host:         "ldap.example.com",
		Port:         389,
		Security:     "none",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "secret",
		BaseDN:       "dc=example,dc=com",
	}

	client := NewClient(config)
	require.NotNil(t, client)
	assert.Equal(t, "ldap.example.com", client.config.Host)
	assert.Equal(t, 389, client.config.Port)
}

func TestEscapeFilter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"normal text", "john", "john"},
		{"parentheses", "user(admin)", "user\\28admin\\29"},
		{"asterisk", "user*", "user\\2a"},
		{"backslash", "user\\path", "user\\5cpath"},
		{"null byte", "user\x00name", "user\\00name"},
		{"multiple special chars", "(cn=*)", "\\28cn=\\2a\\29"},
		{"LDAP injection attempt", "admin)(|(uid=*))", "admin\\29\\28|\\28uid=\\2a\\29\\29"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EscapeFilter(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetUserAttributes(t *testing.T) {
	t.Run("default attributes", func(t *testing.T) {
		client := NewClient(&idp.LDAPConfig{})
		attrs := client.getUserAttributes()

		assert.Contains(t, attrs, "dn")
		assert.Contains(t, attrs, "uid")
		assert.Contains(t, attrs, "mail")
		assert.Contains(t, attrs, "displayName")
		assert.Contains(t, attrs, "memberOf")
		assert.Contains(t, attrs, "sAMAccountName")
		assert.Contains(t, attrs, "cn")
	})

	t.Run("custom attributes", func(t *testing.T) {
		client := NewClient(&idp.LDAPConfig{
			AttrUsername:    "sAMAccountName",
			AttrEmail:       "proxyAddresses",
			AttrDisplayName: "cn",
			AttrMemberOf:    "groups",
		})
		attrs := client.getUserAttributes()

		assert.Contains(t, attrs, "sAMAccountName")
		assert.Contains(t, attrs, "proxyAddresses")
		assert.Contains(t, attrs, "cn")
		assert.Contains(t, attrs, "groups")
		// Should NOT contain defaults when overridden
		assert.NotContains(t, attrs, "uid")
		assert.NotContains(t, attrs, "mail")
	})
}

func TestEntryToExternalUser(t *testing.T) {
	t.Run("default attribute mapping", func(t *testing.T) {
		client := NewClient(&idp.LDAPConfig{})

		entry := &goLdap.Entry{
			DN: "cn=John Doe,ou=Users,dc=example,dc=com",
			Attributes: []*goLdap.EntryAttribute{
				{Name: "uid", Values: []string{"johnd"}},
				{Name: "mail", Values: []string{"john@example.com"}},
				{Name: "displayName", Values: []string{"John Doe"}},
				{Name: "memberOf", Values: []string{"cn=admins,dc=example,dc=com", "cn=devs,dc=example,dc=com"}},
			},
		}

		user := client.EntryToExternalUser(entry)
		assert.Equal(t, "cn=John Doe,ou=Users,dc=example,dc=com", user.ExternalID)
		assert.Equal(t, "johnd", user.Username)
		assert.Equal(t, "john@example.com", user.Email)
		assert.Equal(t, "John Doe", user.DisplayName)
		assert.Equal(t, []string{"cn=admins,dc=example,dc=com", "cn=devs,dc=example,dc=com"}, user.Groups)
	})

	t.Run("custom attribute mapping", func(t *testing.T) {
		client := NewClient(&idp.LDAPConfig{
			AttrUsername:    "sAMAccountName",
			AttrEmail:       "proxyAddresses",
			AttrDisplayName: "cn",
			AttrMemberOf:    "groups",
		})

		entry := &goLdap.Entry{
			DN: "cn=Jane Smith,ou=Users,dc=corp,dc=com",
			Attributes: []*goLdap.EntryAttribute{
				{Name: "sAMAccountName", Values: []string{"jsmith"}},
				{Name: "proxyAddresses", Values: []string{"jane@corp.com"}},
				{Name: "cn", Values: []string{"Jane Smith"}},
				{Name: "groups", Values: []string{"engineering"}},
			},
		}

		user := client.EntryToExternalUser(entry)
		assert.Equal(t, "jsmith", user.Username)
		assert.Equal(t, "jane@corp.com", user.Email)
		assert.Equal(t, "Jane Smith", user.DisplayName)
		assert.Equal(t, []string{"engineering"}, user.Groups)
	})

	t.Run("falls back to sAMAccountName when uid empty", func(t *testing.T) {
		client := NewClient(&idp.LDAPConfig{})

		entry := &goLdap.Entry{
			DN: "cn=AD User,ou=Users,dc=corp,dc=com",
			Attributes: []*goLdap.EntryAttribute{
				{Name: "uid", Values: []string{""}},
				{Name: "sAMAccountName", Values: []string{"aduser"}},
				{Name: "mail", Values: []string{"ad@corp.com"}},
			},
		}

		user := client.EntryToExternalUser(entry)
		assert.Equal(t, "aduser", user.Username)
	})

	t.Run("falls back to cn when uid and sAMAccountName empty", func(t *testing.T) {
		client := NewClient(&idp.LDAPConfig{})

		entry := &goLdap.Entry{
			DN: "cn=Fallback User,ou=Users,dc=example,dc=com",
			Attributes: []*goLdap.EntryAttribute{
				{Name: "cn", Values: []string{"fallbackuser"}},
				{Name: "mail", Values: []string{"fallback@example.com"}},
			},
		}

		user := client.EntryToExternalUser(entry)
		assert.Equal(t, "fallbackuser", user.Username)
	})

	t.Run("raw attributes populated", func(t *testing.T) {
		client := NewClient(&idp.LDAPConfig{})

		entry := &goLdap.Entry{
			DN: "cn=Raw,dc=example,dc=com",
			Attributes: []*goLdap.EntryAttribute{
				{Name: "uid", Values: []string{"rawuser"}},
				{Name: "mail", Values: []string{"raw@example.com"}},
				{Name: "department", Values: []string{"Engineering"}},
				{Name: "title", Values: []string{"Software Engineer"}},
				{Name: "memberOf", Values: []string{"group1", "group2"}},
			},
		}

		user := client.EntryToExternalUser(entry)
		assert.Equal(t, "Engineering", user.RawAttrs["department"])
		assert.Equal(t, "Software Engineer", user.RawAttrs["title"])
		assert.Equal(t, "group1, group2", user.RawAttrs["memberOf"])
	})
}

// =============================================================================
// Provider Unit Tests
// =============================================================================

func TestNewProvider(t *testing.T) {
	config := &idp.LDAPConfig{
		Host:         "ldap.example.com",
		Port:         636,
		Security:     "tls",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "secret",
		BaseDN:       "dc=example,dc=com",
	}

	p := NewProvider(config)
	require.NotNil(t, p)
	assert.Equal(t, "ldap", p.Type())
	assert.NotNil(t, p.client)
}

func TestProvider_Type(t *testing.T) {
	p := NewProvider(&idp.LDAPConfig{})
	assert.Equal(t, "ldap", p.Type())
}

func TestProvider_GetAuthURL(t *testing.T) {
	p := NewProvider(&idp.LDAPConfig{})
	_, err := p.GetAuthURL("state", "hint")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support OAuth redirect")
}

func TestProvider_ExchangeCode(t *testing.T) {
	p := NewProvider(&idp.LDAPConfig{})
	_, err := p.ExchangeCode(nil, "code")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support OAuth code exchange")
}

// =============================================================================
// Connection Tests (these verify error handling without a real server)
// =============================================================================

func TestConnect_InvalidServer(t *testing.T) {
	t.Run("fails to connect to non-existent server", func(t *testing.T) {
		client := NewClient(&idp.LDAPConfig{
			Host:     "nonexistent.invalid.local",
			Port:     389,
			Security: "none",
		})
		_, err := client.Connect()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to connect")
	})

	t.Run("fails with TLS to non-existent server", func(t *testing.T) {
		client := NewClient(&idp.LDAPConfig{
			Host:     "nonexistent.invalid.local",
			Port:     636,
			Security: "tls",
		})
		_, err := client.Connect()
		assert.Error(t, err)
	})
}

func TestProvider_TestConnection_InvalidServer(t *testing.T) {
	p := NewProvider(&idp.LDAPConfig{
		Host:         "nonexistent.invalid.local",
		Port:         389,
		Security:     "none",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "secret",
		BaseDN:       "dc=example,dc=com",
	})
	err := p.TestConnection(nil)
	assert.Error(t, err)
}

func TestProvider_AuthenticateUser_InvalidServer(t *testing.T) {
	p := NewProvider(&idp.LDAPConfig{
		Host:         "nonexistent.invalid.local",
		Port:         389,
		Security:     "none",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "secret",
		BaseDN:       "dc=example,dc=com",
	})
	_, err := p.AuthenticateUser(nil, "user", "pass")
	assert.Error(t, err)
}

func TestProvider_SearchUsers_InvalidServer(t *testing.T) {
	p := NewProvider(&idp.LDAPConfig{
		Host:         "nonexistent.invalid.local",
		Port:         389,
		Security:     "none",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "secret",
		BaseDN:       "dc=example,dc=com",
	})
	_, err := p.SearchUsers(nil, "query", 10)
	assert.Error(t, err)
}

func TestProvider_SearchGroups_InvalidServer(t *testing.T) {
	p := NewProvider(&idp.LDAPConfig{
		Host:         "nonexistent.invalid.local",
		Port:         389,
		Security:     "none",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "secret",
		BaseDN:       "dc=example,dc=com",
	})
	_, err := p.SearchGroups(nil, "query", 10)
	assert.Error(t, err)
}

func TestProvider_GetGroupMembers_InvalidServer(t *testing.T) {
	p := NewProvider(&idp.LDAPConfig{
		Host:         "nonexistent.invalid.local",
		Port:         389,
		Security:     "none",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "secret",
		BaseDN:       "dc=example,dc=com",
	})
	_, err := p.GetGroupMembers(nil, "cn=group,dc=example,dc=com")
	assert.Error(t, err)
}
