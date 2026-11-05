package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/maxiofs/maxiofs/internal/idp"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// microsoftGraph is the Microsoft Graph API.
var microsoftGraph = "https://graph.microsoft.com/v1.0"

// graphUserFields are the properties of a user MaxIOFS reads: accountEnabled
// comes only when asked for.
const graphUserFields = "id,displayName,mail,userPrincipalName,accountEnabled"

// graphPageSize is the most entries one request of a group's members gives.
const graphPageSize = 999

// graphUser is a user as Microsoft Graph gives it.
type graphUser struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	Mail              string `json:"mail"`
	UserPrincipalName string `json:"userPrincipalName"`
	AccountEnabled    bool   `json:"accountEnabled"`
}

// graphPage is one page of a collection of directory objects.
type graphPage struct {
	Value []struct {
		Type string `json:"@odata.type"`
		graphUser
	} `json:"value"`
	NextLink string `json:"@odata.nextLink"`
}

// microsoftDirectory is an Entra ID tenant, read through Microsoft Graph with
// the application's own credentials: it needs the application permissions
// User.Read.All and GroupMember.Read.All.
type microsoftDirectory struct {
	directoryAPI
}

func newMicrosoftDirectory(config *idp.OAuth2Config, tenant string) *microsoftDirectory {
	// A token of the application's own is the tenant's: the sign-in's token URL
	// may name no tenant.
	tokenURL := config.TokenURL
	for _, shared := range []string{"/common/", "/organizations/"} {
		tokenURL = strings.Replace(tokenURL, shared, "/"+tenant+"/", 1)
	}
	credentials := &clientcredentials.Config{
		ClientID: config.ClientID, ClientSecret: config.ClientSecret, TokenURL: tokenURL,
		Scopes: []string{"https://graph.microsoft.com/.default"}, AuthStyle: oauth2.AuthStyleInParams,
	}
	return &microsoftDirectory{directoryAPI{
		name:   "Microsoft Graph",
		hint:   "give the application the permissions User.Read.All and GroupMember.Read.All, with an administrator's consent",
		tokens: credentials.TokenSource(directoryTokenContext()),
	}}
}

// microsoftTenant is the tenant an Entra ID issuer names
// (https://login.microsoftonline.com/<tenant>/v2.0); empty for one that names
// none.
func microsoftTenant(issuer string) string {
	parsed, err := url.Parse(issuer)
	if err != nil {
		return ""
	}
	tenant, _, _ := strings.Cut(strings.TrimPrefix(parsed.Path, "/"), "/")
	switch tenant {
	case "common", "organizations", "consumers":
		return ""
	}
	return tenant
}

func (m *microsoftDirectory) check(ctx context.Context) error {
	var page graphPage
	query := url.Values{"$top": {"1"}, "$select": {graphUserFields}}
	return m.get(ctx, microsoftGraph+"/users?"+query.Encode(), nil, &page)
}

// lookUp is the tenant's user of subject, by its email or its user principal
// name. A subject without an @ is the sign-in's, which is the application's own
// for that user and names nobody in the directory: it cannot tell. One the
// tenant no longer has or has disabled ends the sign-in.
func (m *microsoftDirectory) lookUp(ctx context.Context, subject string) (*idp.ExternalUser, error) {
	if !strings.Contains(subject, "@") {
		return nil, nil
	}
	quoted := strings.ReplaceAll(subject, "'", "''")
	var page graphPage
	query := url.Values{
		"$filter": {fmt.Sprintf("mail eq '%s' or userPrincipalName eq '%s'", quoted, quoted)},
		"$select": {graphUserFields},
	}
	if err := m.get(ctx, microsoftGraph+"/users?"+query.Encode(), nil, &page); err != nil {
		return nil, err
	}
	if len(page.Value) == 0 {
		return nil, fmt.Errorf("%w: %s is gone from the tenant", idp.ErrSessionEnded, subject)
	}
	user := page.Value[0].graphUser
	if !user.AccountEnabled {
		return nil, fmt.Errorf("%w: the tenant disabled %s", idp.ErrSessionEnded, subject)
	}
	external := graphExternal(user)
	var err error
	if external.Groups, err = m.groupsOf(ctx, user.ID); err != nil {
		return nil, err
	}
	return &external, nil
}

// groupsOf are the object IDs of the groups user is in, directly or through
// another group. It asks without query parameters: with them Graph answers from
// an index that may lag. A group's name says nothing: two groups may share it.
func (m *microsoftDirectory) groupsOf(ctx context.Context, user string) ([]string, error) {
	var groups []string
	err := m.pages(ctx, microsoftGraph+"/users/"+url.PathEscape(user)+"/transitiveMemberOf", nil, func(page *graphPage) {
		for _, object := range page.Value {
			if object.Type == "#microsoft.graph.group" {
				groups = append(groups, object.ID)
			}
		}
	})
	return groups, err
}

// members are the users of group, its object ID, directly or through another
// group; those disabled are left out. The query needs Graph's advanced
// parameters, so a change may take a moment to show: a later sync sees it.
func (m *microsoftDirectory) members(ctx context.Context, group string) ([]idp.ExternalUser, error) {
	query := url.Values{"$count": {"true"}, "$select": {graphUserFields}, "$top": {fmt.Sprint(graphPageSize)}}
	target := microsoftGraph + "/groups/" + url.PathEscape(group) + "/transitiveMembers/microsoft.graph.user?" + query.Encode()
	header := http.Header{}
	header.Set("ConsistencyLevel", "eventual")

	var members []idp.ExternalUser
	err := m.pages(ctx, target, header, func(page *graphPage) {
		for _, object := range page.Value {
			if external := graphExternal(object.graphUser); object.AccountEnabled && external.Email != "" {
				members = append(members, external)
			}
		}
	})
	if errors.Is(err, errNotInDirectory) {
		return nil, fmt.Errorf("the tenant has no group %s: name it by its object ID", group)
	}
	return members, err
}

// pages reads target page by page. Graph names each next page: the token goes
// nowhere else.
func (m *microsoftDirectory) pages(ctx context.Context, target string, header http.Header, each func(*graphPage)) error {
	for target != "" {
		var page graphPage
		if err := m.get(ctx, target, header, &page); err != nil {
			return err
		}
		each(&page)
		target = page.NextLink
		if target != "" && !strings.HasPrefix(target, microsoftGraph+"/") {
			return fmt.Errorf("Microsoft Graph named a next page elsewhere: %s", target)
		}
	}
	return nil
}

// graphExternal is user as a provider's user: its email names it, as at its
// sign-in, and a user without a mailbox is named by its user principal name.
func graphExternal(user graphUser) idp.ExternalUser {
	email := user.Mail
	if email == "" {
		email = user.UserPrincipalName
	}
	return idp.ExternalUser{
		ExternalID:  email,
		Username:    email,
		Email:       email,
		DisplayName: user.DisplayName,
		Grant:       &idp.Grant{Subject: user.ID},
	}
}
