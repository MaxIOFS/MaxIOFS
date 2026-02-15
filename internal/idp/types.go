package idp

// IdentityProvider represents an external identity provider configuration
type IdentityProvider struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Type      string         `json:"type"`      // "ldap" | "oauth2"
	TenantID  string         `json:"tenantId"`  // empty = global
	Status    string         `json:"status"`    // "active" | "inactive" | "testing"
	Config    ProviderConfig `json:"config"`    // Type-specific configuration
	CreatedBy string         `json:"createdBy"`
	CreatedAt int64          `json:"createdAt"`
	UpdatedAt int64          `json:"updatedAt"`
}

// ProviderConfig is a union type — only one sub-config is populated
type ProviderConfig struct {
	LDAP   *LDAPConfig   `json:"ldap,omitempty"`
	OAuth2 *OAuth2Config `json:"oauth2,omitempty"`
}

// LDAPConfig holds LDAP/Active Directory connection settings
type LDAPConfig struct {
	Host            string `json:"host"`              // "ldap.company.com"
	Port            int    `json:"port"`              // 389 or 636
	Security        string `json:"security"`          // "none" | "tls" | "starttls"
	BindDN          string `json:"bind_dn"`           // "cn=readonly,dc=company,dc=com"
	BindPassword    string `json:"bind_password"`     // Encrypted at rest
	BaseDN          string `json:"base_dn"`           // "dc=company,dc=com"
	UserSearchBase  string `json:"user_search_base"`  // "ou=Users,dc=company,dc=com"
	UserFilter      string `json:"user_filter"`       // "(objectClass=person)"
	GroupSearchBase string `json:"group_search_base"` // "ou=Groups,dc=company,dc=com"
	GroupFilter     string `json:"group_filter"`      // "(objectClass=group)"
	// Attribute mapping
	AttrUsername    string `json:"attr_username"`     // "sAMAccountName" or "uid"
	AttrEmail       string `json:"attr_email"`        // "mail"
	AttrDisplayName string `json:"attr_display_name"` // "displayName"
	AttrMemberOf    string `json:"attr_member_of"`    // "memberOf"
}

// OAuth2Config holds OAuth2/OIDC provider settings
type OAuth2Config struct {
	Preset       string   `json:"preset"`        // "google" | "microsoft" | "custom"
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"` // Encrypted at rest
	AuthURL      string   `json:"auth_url"`
	TokenURL     string   `json:"token_url"`
	UserInfoURL  string   `json:"userinfo_url"`
	Scopes       []string `json:"scopes"`
	RedirectURI  string   `json:"redirect_uri"`
	// Attribute mapping for claims
	ClaimEmail  string `json:"claim_email"`  // "email"
	ClaimName   string `json:"claim_name"`   // "name"
	ClaimGroups string `json:"claim_groups"` // "groups" (optional)
}

// ExternalUser represents a user found in an external directory
type ExternalUser struct {
	ExternalID  string            `json:"externalId"`  // DN for LDAP, sub/email for OAuth
	Username    string            `json:"username"`
	Email       string            `json:"email"`
	DisplayName string            `json:"displayName"`
	Groups      []string          `json:"groups"`      // DNs or group names
	RawAttrs    map[string]string `json:"rawAttrs"`    // All attributes for display
}

// ExternalGroup represents a group found in an external directory
type ExternalGroup struct {
	ExternalID  string `json:"externalId"`  // DN for LDAP
	Name        string `json:"name"`        // CN or display name
	MemberCount int    `json:"memberCount"`
}

// GroupMapping maps an external group to a MaxIOFS role
type GroupMapping struct {
	ID                string `json:"id"`
	ProviderID        string `json:"providerId"`
	ExternalGroup     string `json:"externalGroup"`
	ExternalGroupName string `json:"externalGroupName"`
	Role              string `json:"role"`     // "admin" | "user" | "readonly"
	TenantID          string `json:"tenantId"` // target tenant
	AutoSync          bool   `json:"autoSync"`
	LastSyncedAt      int64  `json:"lastSyncedAt"`
	CreatedAt         int64  `json:"createdAt"`
	UpdatedAt         int64  `json:"updatedAt"`
}

// ImportRequest represents a request to import external users
type ImportRequest struct {
	Users    []ImportUserEntry `json:"users"`
	Role     string           `json:"role"`
	TenantID string           `json:"tenant_id"`
}

// ImportUserEntry represents a single user to import
type ImportUserEntry struct {
	ExternalID string `json:"external_id"`
	Username   string `json:"username"`
}

// ImportResult represents the outcome of importing a single user
type ImportResult struct {
	ExternalID string `json:"external_id"`
	Username   string `json:"username"`
	Status     string `json:"status"` // "imported" | "skipped" | "error"
	Error      string `json:"error,omitempty"`
}

// SyncResult represents the outcome of syncing a group mapping
type SyncResult struct {
	MappingID string `json:"mappingId"`
	Imported  int    `json:"imported"`
	Updated   int    `json:"updated"`
	Removed   int    `json:"removed"`
	Errors    int    `json:"errors"`
}

// Provider status constants
const (
	StatusActive   = "active"
	StatusInactive = "inactive"
	StatusTesting  = "testing"
)

// Provider type constants
const (
	TypeLDAP   = "ldap"
	TypeOAuth2 = "oauth2"
)
