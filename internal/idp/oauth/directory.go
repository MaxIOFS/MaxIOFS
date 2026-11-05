package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/maxiofs/maxiofs/internal/idp"
	"golang.org/x/oauth2"
)

// errNotInDirectory is a directory's answer for a user or group it does not
// have.
var errNotInDirectory = errors.New("not found in the directory")

// directory is a provider's directory, read for the users who do not sign in.
type directory interface {
	// lookUp is the user of subject: one gone or disabled ends the sign-in, and
	// nil means the directory cannot tell.
	lookUp(ctx context.Context, subject string) (*idp.ExternalUser, error)
	// members are the users of group; those disabled are left out.
	members(ctx context.Context, group string) ([]idp.ExternalUser, error)
	// check reads one user, as a sync does.
	check(ctx context.Context) error
}

// directory is the provider's directory, nil without one: a Google Workspace
// with the service account's key, a Microsoft tenant, or a Keycloak realm.
func (p *Provider) directory() directory {
	p.directoryOnce.Do(func() {
		switch p.config.Preset {
		case "google":
			if p.config.DirectoryKey != "" {
				p.dir = newGoogleDirectory(p.config)
			}
		case "microsoft":
			if tenant := microsoftTenant(p.config.Issuer); tenant != "" {
				p.dir = newMicrosoftDirectory(p.config, tenant)
			}
		default:
			if admin := keycloakAdmin(p.config.Issuer); admin != "" {
				p.dir = newKeycloakDirectory(p.config, admin)
			}
		}
	})
	return p.dir
}

// ReadsDirectory reports whether the provider has a directory MaxIOFS reads
// for the users who do not sign in.
func (p *Provider) ReadsDirectory() bool {
	return p.directory() != nil
}

// directoryGroups gives user the groups of its directory: the userinfo of
// Google and Microsoft gives none.
func (p *Provider) directoryGroups(ctx context.Context, user *idp.ExternalUser) error {
	if p.config.Preset != "google" && p.config.Preset != "microsoft" {
		return nil
	}
	dir := p.directory()
	if dir == nil || p.config.ClaimGroups != "" || user.Email == "" {
		return nil
	}
	found, err := dir.lookUp(ctx, user.Email)
	if err != nil {
		return err
	}
	if found != nil {
		user.Groups = found.Groups
	}
	return nil
}

// checkDirectory reads one user of the directory. Sign-ins work without it, so
// a directory that refuses is a warning.
func (p *Provider) checkDirectory(ctx context.Context) idp.Check {
	if err := p.directory().check(ctx); err != nil {
		return check("Directory", idp.CheckWarning, "Auto Sync cannot read the directory: %s", reason(ctx, err))
	}
	return check("Directory", idp.CheckPassed, "MaxIOFS can read the directory")
}

// directoryAPI reads a directory's REST API with tokens of its own.
type directoryAPI struct {
	name string // names the directory in an error
	hint string // what to do when it refuses to show its users
	// tokens outlives each request: one token serves a whole sync.
	tokens oauth2.TokenSource
	// broken is why a configuration cannot get a token.
	broken error
}

// directoryTokenContext is the context of a directory's tokens: they outlive
// the request that asked for the first one.
func directoryTokenContext() context.Context {
	return context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: providerTimeout})
}

// tokenRefusal is why a token was refused: a service account's refusal comes
// with the error in its body only.
func tokenRefusal(refused *oauth2.RetrieveError) string {
	if refused.ErrorCode != "" {
		return providerError(refused.ErrorCode, refused.ErrorDescription)
	}
	var body struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if json.Unmarshal(refused.Body, &body) == nil && body.Error != "" {
		return providerError(body.Error, body.Description)
	}
	return fmt.Sprintf("the token endpoint answered %d", refused.Response.StatusCode)
}

// get reads target into v. A directory that does not answer, or that limits
// the requests, is unavailable: it says nothing of a user.
func (d *directoryAPI) get(ctx context.Context, target string, header http.Header, v any) error {
	if d.broken != nil {
		return d.broken
	}
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	for name, values := range header {
		req.Header[name] = values
	}
	resp, err := oauth2.NewClient(ctx, d.tokens).Do(req)
	if err != nil {
		var refused *oauth2.RetrieveError
		if errors.As(err, &refused) && refused.Response != nil && refused.Response.StatusCode < 500 {
			return fmt.Errorf("%s refused the credentials: %s", d.name, tokenRefusal(refused))
		}
		return fmt.Errorf("%w: %s", idp.ErrUnavailable, reason(ctx, err))
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errNotInDirectory
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%s refused to show its users (%d): %s", d.name, resp.StatusCode, d.hint)
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return fmt.Errorf("%w: %s answered %d", idp.ErrUnavailable, d.name, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s answered %d: %s", d.name, resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
