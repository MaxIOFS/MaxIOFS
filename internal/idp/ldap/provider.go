package ldap

import (
	"context"
	"errors"
	"fmt"

	"github.com/maxiofs/maxiofs/internal/idp"
)

// Provider implements the idp.Provider interface for LDAP/Active Directory
type Provider struct {
	client *Client
	config *idp.LDAPConfig
}

func init() {
	idp.RegisterProvider(idp.TypeLDAP, func(provider *idp.IdentityProvider, cryptoSecret string) (idp.Provider, error) {
		if provider.Config.LDAP == nil {
			return nil, fmt.Errorf("LDAP config is required for LDAP provider")
		}
		return NewProvider(provider.Config.LDAP), nil
	})
}

// NewProvider creates a new LDAP provider
func NewProvider(config *idp.LDAPConfig) *Provider {
	return &Provider{
		client: NewClient(config),
		config: config,
	}
}

func (p *Provider) Type() string {
	return idp.TypeLDAP
}

func (p *Provider) TestConnection(ctx context.Context) error {
	conn, err := p.client.Connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := p.client.BindAdmin(conn); err != nil {
		return err
	}

	// Do a simple search to validate the base DN
	_, err = p.client.SearchUsers(conn, "", 1)
	if err != nil {
		return fmt.Errorf("connection succeeded but search failed: %w", err)
	}

	return nil
}

func (p *Provider) AuthenticateUser(ctx context.Context, username, password string) (*idp.ExternalUser, error) {
	conn, err := p.client.Connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// First bind as admin to find the user
	if err := p.client.BindAdmin(conn); err != nil {
		return nil, err
	}

	// A failure other than an unknown user or a wrong password is the
	// directory's, never the user's.
	entry, err := p.client.FindUser(conn, username)
	if err != nil && !errors.Is(err, errUserNotFound) && !errors.Is(err, idp.ErrUnavailable) {
		err = fmt.Errorf("%w: %w", idp.ErrUnavailable, err)
	}
	if err != nil {
		return nil, fmt.Errorf("user not found in LDAP: %w", err)
	}

	// Now bind as the user to verify their password
	// Need a new connection since we can't re-bind on the same one safely
	userConn, err := p.client.Connect()
	if err != nil {
		return nil, err
	}
	defer userConn.Close()

	if err := p.client.BindUser(userConn, entry.DN, password); err != nil {
		return nil, err
	}

	user := p.client.EntryToExternalUser(entry)
	return &user, nil
}

func (p *Provider) SearchUsers(ctx context.Context, query string, limit int) ([]idp.ExternalUser, error) {
	conn, err := p.client.Connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := p.client.BindAdmin(conn); err != nil {
		return nil, err
	}

	entries, err := p.client.SearchUsers(conn, query, limit)
	if err != nil {
		return nil, err
	}

	users := make([]idp.ExternalUser, 0, len(entries))
	for _, entry := range entries {
		users = append(users, p.client.EntryToExternalUser(entry))
	}

	return users, nil
}

func (p *Provider) SearchGroups(ctx context.Context, query string, limit int) ([]idp.ExternalGroup, error) {
	conn, err := p.client.Connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := p.client.BindAdmin(conn); err != nil {
		return nil, err
	}

	entries, err := p.client.SearchGroups(conn, query, limit)
	if err != nil {
		return nil, err
	}

	groups := make([]idp.ExternalGroup, 0, len(entries))
	for _, entry := range entries {
		members := entry.GetAttributeValues("member")
		groups = append(groups, idp.ExternalGroup{
			ExternalID:  entry.DN,
			Name:        entry.GetAttributeValue("cn"),
			MemberCount: len(members),
		})
	}

	return groups, nil
}

func (p *Provider) GetGroupMembers(ctx context.Context, groupID string) ([]idp.ExternalUser, error) {
	conn, err := p.client.Connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := p.client.BindAdmin(conn); err != nil {
		return nil, err
	}

	entries, err := p.client.GetGroupMembers(conn, groupID)
	if err != nil {
		return nil, err
	}

	users := make([]idp.ExternalUser, 0, len(entries))
	for _, entry := range entries {
		users = append(users, p.client.EntryToExternalUser(entry))
	}

	return users, nil
}

func (p *Provider) GetAuthURL(state string, loginHint string) (string, error) {
	return "", fmt.Errorf("LDAP provider does not support OAuth redirect flow")
}

func (p *Provider) ExchangeCode(ctx context.Context, code string) (*idp.ExternalUser, error) {
	return nil, fmt.Errorf("LDAP provider does not support OAuth code exchange")
}
