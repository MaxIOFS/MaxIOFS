package oauth

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"

	"github.com/maxiofs/maxiofs/internal/idp"
	"golang.org/x/oauth2/jwt"
)

// googlePage is how many entries one request of the Directory API reads, the
// most it gives.
const googlePage = 200

// googleDirectoryAPI is Google's Admin SDK Directory API.
var googleDirectoryAPI = "https://admin.googleapis.com/admin/directory/v1"

// googleDirectoryScopes read users and groups, and change nothing.
var googleDirectoryScopes = []string{
	"https://www.googleapis.com/auth/admin.directory.user.readonly",
	"https://www.googleapis.com/auth/admin.directory.group.readonly",
}

// googleKey is what MaxIOFS reads of a service account's JSON key.
type googleKey struct {
	Type         string `json:"type"`
	ClientEmail  string `json:"client_email"`
	PrivateKey   string `json:"private_key"`
	PrivateKeyID string `json:"private_key_id"`
}

// googleUser is a user as the Directory API gives it.
type googleUser struct {
	ID           string `json:"id"`
	PrimaryEmail string `json:"primaryEmail"`
	Name         struct {
		FullName string `json:"fullName"`
	} `json:"name"`
	Suspended bool `json:"suspended"`
	Archived  bool `json:"archived"`
}

// googleDirectory is a Google Workspace directory, read by a service account
// with an administrator role of its own: no user is impersonated.
type googleDirectory struct {
	directoryAPI
}

func newGoogleDirectory(config *idp.OAuth2Config) *googleDirectory {
	d := &googleDirectory{directoryAPI{
		name: "Google's directory",
		hint: "give the service account an administrator role that reads users and groups",
	}}
	var key googleKey
	err := json.Unmarshal([]byte(config.DirectoryKey), &key)
	if block, _ := pem.Decode([]byte(key.PrivateKey)); err != nil || key.Type != "service_account" || key.ClientEmail == "" || block == nil {
		d.broken = errors.New("the directory key is not a service account's JSON key")
		return d
	}
	d.tokens = (&jwt.Config{
		Email: key.ClientEmail, PrivateKey: []byte(key.PrivateKey), PrivateKeyID: key.PrivateKeyID,
		Scopes: googleDirectoryScopes, TokenURL: config.TokenURL,
	}).TokenSource(directoryTokenContext())
	return d
}

// read reads path of the Directory API into v.
func (g *googleDirectory) read(ctx context.Context, path string, query url.Values, v any) error {
	return g.get(ctx, googleDirectoryAPI+path+"?"+query.Encode(), nil, v)
}

func (g *googleDirectory) check(ctx context.Context) error {
	var page struct {
		Users []googleUser `json:"users"`
	}
	return g.read(ctx, "/users", url.Values{"customer": {"my_customer"}, "maxResults": {"1"}}, &page)
}

// lookUp is the directory's user of subject, an email or a user ID: Google's
// subject is the ID. One the directory no longer has, has suspended or has
// archived ends the sign-in.
func (g *googleDirectory) lookUp(ctx context.Context, subject string) (*idp.ExternalUser, error) {
	var user googleUser
	err := g.read(ctx, "/users/"+url.PathEscape(subject), url.Values{"projection": {"basic"}, "viewType": {"admin_view"}}, &user)
	if errors.Is(err, errNotInDirectory) {
		return nil, fmt.Errorf("%w: %s is gone from the directory", idp.ErrSessionEnded, subject)
	}
	if err != nil {
		return nil, err
	}
	if user.Suspended || user.Archived {
		return nil, fmt.Errorf("%w: the directory disabled %s", idp.ErrSessionEnded, subject)
	}
	groups, err := g.groupsOf(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return &idp.ExternalUser{
		ExternalID:  user.PrimaryEmail,
		Username:    user.PrimaryEmail,
		Email:       user.PrimaryEmail,
		DisplayName: user.Name.FullName,
		Groups:      groups,
		Grant:       &idp.Grant{Subject: user.ID},
	}, nil
}

// groupsOf are the email and the ID of each group user is a direct member of.
// A group's name says nothing: two groups may share it.
func (g *googleDirectory) groupsOf(ctx context.Context, user string) ([]string, error) {
	var groups []string
	token := ""
	for {
		var page struct {
			Groups []struct {
				ID    string `json:"id"`
				Email string `json:"email"`
			} `json:"groups"`
			NextPageToken string `json:"nextPageToken"`
		}
		query := url.Values{"userKey": {user}, "maxResults": {fmt.Sprint(googlePage)}}
		if token != "" {
			query.Set("pageToken", token)
		}
		if err := g.read(ctx, "/groups", query, &page); err != nil {
			return nil, err
		}
		for _, group := range page.Groups {
			groups = append(groups, group.Email, group.ID)
		}
		if token = page.NextPageToken; token == "" {
			return groups, nil
		}
	}
}

// members are the direct members of group, its email or its ID, read page by
// page: as at a sign-in, the members of a group inside it do not count. Those
// suspended, and whatever is not a user, are left out.
func (g *googleDirectory) members(ctx context.Context, group string) ([]idp.ExternalUser, error) {
	var members []idp.ExternalUser
	token := ""
	for {
		var page struct {
			Members []struct {
				ID     string `json:"id"`
				Email  string `json:"email"`
				Type   string `json:"type"`
				Status string `json:"status"`
			} `json:"members"`
			NextPageToken string `json:"nextPageToken"`
		}
		query := url.Values{"maxResults": {fmt.Sprint(googlePage)}}
		if token != "" {
			query.Set("pageToken", token)
		}
		err := g.read(ctx, "/groups/"+url.PathEscape(group)+"/members", query, &page)
		if errors.Is(err, errNotInDirectory) {
			return nil, fmt.Errorf("the directory has no group %s: name it by its email", group)
		}
		if err != nil {
			return nil, err
		}
		for _, m := range page.Members {
			if m.Type == "USER" && m.Status != "SUSPENDED" && m.Email != "" {
				members = append(members, idp.ExternalUser{
					ExternalID: m.Email, Username: m.Email, Email: m.Email, Grant: &idp.Grant{Subject: m.ID},
				})
			}
		}
		if token = page.NextPageToken; token == "" {
			return members, nil
		}
	}
}
