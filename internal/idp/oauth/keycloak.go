package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/maxiofs/maxiofs/internal/idp"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// keycloakPage is how many members of a group one request of Keycloak's admin
// API reads.
const keycloakPage = 100

// errKeycloakNotFound is the admin API's answer for a user or group the realm
// does not have.
var errKeycloakNotFound = errors.New("not found in the realm")

// keycloakUser is a user as Keycloak's admin API gives it.
type keycloakUser struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	FirstName     string `json:"firstName"`
	LastName      string `json:"lastName"`
	Enabled       bool   `json:"enabled"`
	EmailVerified bool   `json:"emailVerified"`
}

// keycloakGroup is a group as Keycloak's admin API gives it.
type keycloakGroup struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Path      string          `json:"path"`
	SubGroups []keycloakGroup `json:"subGroups"`
}

// ReadsDirectory reports whether the provider has a directory MaxIOFS reads
// for the users who do not sign in: a Keycloak realm, through its admin API.
func (p *Provider) ReadsDirectory() bool {
	return p.keycloakAdmin() != ""
}

// keycloakAdmin is the URL of the admin API of the provider's Keycloak realm,
// beside its issuer (https://host/realms/<realm>); empty for another provider.
func (p *Provider) keycloakAdmin() string {
	issuer := strings.TrimRight(p.config.Issuer, "/")
	at := strings.LastIndex(issuer, "/realms/")
	if at < 0 || strings.Contains(issuer[at+len("/realms/"):], "/") || at+len("/realms/") == len(issuer) {
		return ""
	}
	return issuer[:at] + "/admin" + issuer[at:]
}

// keycloakGet reads path of the realm's admin API into v, with a token of the
// client's own credentials: the client's service account needs
// realm-management's view-users.
func (p *Provider) keycloakGet(ctx context.Context, path string, query url.Values, v any) error {
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	client := (&clientcredentials.Config{
		ClientID: p.config.ClientID, ClientSecret: p.config.ClientSecret, TokenURL: p.config.TokenURL,
	}).Client(ctx)
	target := p.keycloakAdmin() + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		var refused *oauth2.RetrieveError
		if errors.As(err, &refused) && refused.Response != nil && refused.Response.StatusCode < 500 {
			return fmt.Errorf("the realm refused the client's credentials: %s", providerError(refused.ErrorCode, refused.ErrorDescription))
		}
		return fmt.Errorf("%w: %s", idp.ErrUnavailable, reason(ctx, err))
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errKeycloakNotFound
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("the realm refused to show its users (%d): give the client's service account realm-management's view-users", resp.StatusCode)
	case resp.StatusCode >= 500:
		return fmt.Errorf("%w: the realm's admin API answered %d", idp.ErrUnavailable, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("the realm's admin API answered %d: %s", resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// lookUpUser is the realm's user of subject, an email or a user ID: one the
// realm no longer has, has disabled, or whose email it did not verify, ends
// the sign-in.
func (p *Provider) lookUpUser(ctx context.Context, subject string) (*idp.ExternalUser, error) {
	var user keycloakUser
	if strings.Contains(subject, "@") {
		var found []keycloakUser
		if err := p.keycloakGet(ctx, "/users", url.Values{"email": {subject}, "exact": {"true"}}, &found); err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("%w: %s is gone from the realm", idp.ErrSessionEnded, subject)
		}
		user = found[0]
	} else {
		err := p.keycloakGet(ctx, "/users/"+url.PathEscape(subject), nil, &user)
		if errors.Is(err, errKeycloakNotFound) {
			return nil, fmt.Errorf("%w: %s is gone from the realm", idp.ErrSessionEnded, subject)
		}
		if err != nil {
			return nil, err
		}
	}
	switch {
	case !user.Enabled:
		return nil, fmt.Errorf("%w: the realm disabled %s", idp.ErrSessionEnded, subject)
	case !user.EmailVerified:
		return nil, fmt.Errorf("%w: %w: %s", idp.ErrSessionEnded, idp.ErrEmailNotVerified, user.Email)
	}
	var groups []keycloakGroup
	if err := p.keycloakGet(ctx, "/users/"+url.PathEscape(user.ID)+"/groups", nil, &groups); err != nil {
		return nil, err
	}
	external := keycloakExternal(user)
	for _, g := range groups {
		external.Groups = append(external.Groups, g.Name, g.Path)
	}
	return &external, nil
}

// keycloakMembers are the members of the realm's group named group, its name
// or its path, read page by page; those disabled, or whose email the realm
// did not verify, are left out.
func (p *Provider) keycloakMembers(ctx context.Context, group string) ([]idp.ExternalUser, error) {
	var found []keycloakGroup
	name := group[strings.LastIndex(group, "/")+1:]
	if err := p.keycloakGet(ctx, "/groups", url.Values{"search": {name}, "exact": {"true"}}, &found); err != nil {
		return nil, err
	}
	id := groupNamed(found, group)
	if id == "" {
		return nil, fmt.Errorf("the realm has no group %s", group)
	}
	var members []idp.ExternalUser
	for first := 0; ; first += keycloakPage {
		var page []keycloakUser
		query := url.Values{"first": {fmt.Sprint(first)}, "max": {fmt.Sprint(keycloakPage)}}
		if err := p.keycloakGet(ctx, "/groups/"+url.PathEscape(id)+"/members", query, &page); err != nil {
			return nil, err
		}
		for _, u := range page {
			if u.Enabled && u.EmailVerified && u.Email != "" {
				members = append(members, keycloakExternal(u))
			}
		}
		if len(page) < keycloakPage {
			return members, nil
		}
	}
}

// groupNamed is the ID of the group of groups, or of their subgroups, whose
// name or path is group.
func groupNamed(groups []keycloakGroup, group string) string {
	for _, g := range groups {
		if strings.EqualFold(g.Name, group) || strings.EqualFold(g.Path, group) {
			return g.ID
		}
		if id := groupNamed(g.SubGroups, group); id != "" {
			return id
		}
	}
	return ""
}

// keycloakExternal is user as a provider's user: its email names it, as at
// its sign-in.
func keycloakExternal(user keycloakUser) idp.ExternalUser {
	return idp.ExternalUser{
		ExternalID:  user.Email,
		Username:    user.Email,
		Email:       user.Email,
		DisplayName: strings.TrimSpace(user.FirstName + " " + user.LastName),
		Grant:       &idp.Grant{Subject: user.ID},
	}
}
