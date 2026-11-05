package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/maxiofs/maxiofs/internal/idp"
	"golang.org/x/oauth2/clientcredentials"
)

// keycloakPage is how many members of a group one request of Keycloak's admin
// API reads.
const keycloakPage = 100

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

// keycloakDirectory is a Keycloak realm, read through its admin API with the
// client's own credentials: its service account needs realm-management's
// view-users.
type keycloakDirectory struct {
	directoryAPI
	admin string
}

func newKeycloakDirectory(config *idp.OAuth2Config, admin string) *keycloakDirectory {
	credentials := &clientcredentials.Config{
		ClientID: config.ClientID, ClientSecret: config.ClientSecret, TokenURL: config.TokenURL,
	}
	return &keycloakDirectory{
		directoryAPI: directoryAPI{
			name:   "the realm",
			hint:   "give the client's service account realm-management's view-users",
			tokens: credentials.TokenSource(directoryTokenContext()),
		},
		admin: admin,
	}
}

// keycloakAdmin is the URL of the admin API of a Keycloak realm, beside its
// issuer (https://host/realms/<realm>); empty for another provider.
func keycloakAdmin(issuer string) string {
	issuer = strings.TrimRight(issuer, "/")
	at := strings.LastIndex(issuer, "/realms/")
	if at < 0 || strings.Contains(issuer[at+len("/realms/"):], "/") || at+len("/realms/") == len(issuer) {
		return ""
	}
	return issuer[:at] + "/admin" + issuer[at:]
}

// read reads path of the realm's admin API into v.
func (k *keycloakDirectory) read(ctx context.Context, path string, query url.Values, v any) error {
	target := k.admin + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	return k.get(ctx, target, nil, v)
}

func (k *keycloakDirectory) check(ctx context.Context) error {
	var page []keycloakUser
	return k.read(ctx, "/users", url.Values{"max": {"1"}}, &page)
}

// lookUp is the realm's user of subject, an email or a user ID: one the realm
// no longer has, has disabled, or whose email it did not verify, ends the
// sign-in.
func (k *keycloakDirectory) lookUp(ctx context.Context, subject string) (*idp.ExternalUser, error) {
	var user keycloakUser
	if strings.Contains(subject, "@") {
		var found []keycloakUser
		if err := k.read(ctx, "/users", url.Values{"email": {subject}, "exact": {"true"}}, &found); err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("%w: %s is gone from the realm", idp.ErrSessionEnded, subject)
		}
		user = found[0]
	} else {
		err := k.read(ctx, "/users/"+url.PathEscape(subject), nil, &user)
		if errors.Is(err, errNotInDirectory) {
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
	if err := k.read(ctx, "/users/"+url.PathEscape(user.ID)+"/groups", nil, &groups); err != nil {
		return nil, err
	}
	external := keycloakExternal(user)
	for _, g := range groups {
		external.Groups = append(external.Groups, g.Name, g.Path)
	}
	return &external, nil
}

// members are the members of the realm's group named group, its name or its
// path, read page by page; those disabled, or whose email the realm did not
// verify, are left out.
func (k *keycloakDirectory) members(ctx context.Context, group string) ([]idp.ExternalUser, error) {
	var found []keycloakGroup
	name := group[strings.LastIndex(group, "/")+1:]
	if err := k.read(ctx, "/groups", url.Values{"search": {name}, "exact": {"true"}}, &found); err != nil {
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
		if err := k.read(ctx, "/groups/"+url.PathEscape(id)+"/members", query, &page); err != nil {
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
