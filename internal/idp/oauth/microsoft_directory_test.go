package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const entraTenant = "6f1c0a2e-tenant"

// entraObject is a directory object of fakeEntra, as Graph gives it.
type entraObject map[string]any

// fakeEntra is an Entra ID tenant: the token endpoints, of the tenant and of
// common, which refuses an application's own token as Entra ID does, its
// userinfo, and Microsoft Graph. A group's transitive members need the advanced
// query parameters, and it pages by @odata.nextLink. Refusing, it answers 403,
// as to an application without its permissions.
type fakeEntra struct {
	server    *httptest.Server
	mu        sync.Mutex
	users     map[string]graphUser
	groups    map[string][]string // group ID to user IDs
	refuse    bool
	down      bool
	elsewhere string // where a page of groups says the next one is
	eventual  atomic.Int32
	userInfo  map[string]any
	filters   []string
}

func startEntra(t *testing.T) *fakeEntra {
	t.Helper()
	e := &fakeEntra{users: map[string]graphUser{}, groups: map[string][]string{}}
	reply := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/"+entraTenant+"/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != "maxiofs" ||
			r.Form.Get("client_secret") != "secret" || r.Form.Get("scope") != "https://graph.microsoft.com/.default" {
			reply(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized_client"})
			return
		}
		reply(w, http.StatusOK, map[string]any{"access_token": "graph-token", "token_type": "Bearer", "expires_in": 3600})
	})
	mux.HandleFunc("/common/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "client_credentials" {
			reply(w, http.StatusBadRequest, map[string]string{"error": "invalid_request", "error_description": "AADSTS90002: a tenant is needed"})
			return
		}
		reply(w, http.StatusOK, map[string]any{"access_token": "user-token", "token_type": "Bearer", "refresh_token": "refresh", "expires_in": 3600})
	})
	mux.HandleFunc("/oidc/userinfo", func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()
		reply(w, http.StatusOK, e.userInfo)
	})
	mux.HandleFunc("/v1.0/", func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()
		switch {
		case r.Header.Get("Authorization") != "Bearer graph-token":
			reply(w, http.StatusUnauthorized, map[string]string{"error": "no token"})
			return
		case e.down:
			reply(w, http.StatusServiceUnavailable, map[string]string{"error": "down"})
			return
		case e.refuse:
			reply(w, http.StatusForbidden, map[string]any{"error": map[string]string{"code": "Authorization_RequestDenied"}})
			return
		}
		query := r.URL.Query()
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1.0/"), "/")
		switch {
		case len(parts) == 1 && parts[0] == "users" && query.Get("$top") == "1":
			reply(w, http.StatusOK, map[string]any{"value": []any{}})
		case len(parts) == 1 && parts[0] == "users":
			e.filters = append(e.filters, query.Get("$filter"))
			found := []entraObject{}
			for _, u := range e.users {
				if e.matches(query.Get("$filter"), u) {
					found = append(found, entraUser(u, query.Get("$select")))
				}
			}
			reply(w, http.StatusOK, map[string]any{"value": found})
		case len(parts) == 2 && parts[0] == "users" && parts[1] == "":
			reply(w, http.StatusBadRequest, map[string]string{"error": "Bad Request"})
		case len(parts) == 3 && parts[0] == "users" && parts[2] == "transitiveMemberOf":
			if r.Header.Get("ConsistencyLevel") != "" || len(query) > 1 {
				reply(w, http.StatusBadRequest, map[string]string{"error": "a user's groups are read fresh"})
				return
			}
			objects := []entraObject{{"@odata.type": "#microsoft.graph.directoryRole", "id": "role-1", "displayName": "Reader"}}
			for group, ids := range e.groups {
				for _, id := range ids {
					if id == parts[1] {
						objects = append(objects, entraObject{"@odata.type": "#microsoft.graph.group", "id": group, "displayName": "Staff"})
					}
				}
			}
			e.page(w, r, reply, objects, 1)
		case len(parts) == 4 && parts[0] == "groups" && parts[2] == "transitiveMembers" && parts[3] == "microsoft.graph.user":
			if r.Header.Get("ConsistencyLevel") != "eventual" || query.Get("$count") != "true" {
				reply(w, http.StatusBadRequest, map[string]any{"error": map[string]string{"code": "Request_UnsupportedQuery"}})
				return
			}
			e.eventual.Add(1)
			ids, ok := e.groups[parts[1]]
			if !ok {
				reply(w, http.StatusNotFound, map[string]any{"error": map[string]string{"code": "Request_ResourceNotFound"}})
				return
			}
			objects := make([]entraObject, 0, len(ids))
			for _, id := range ids {
				objects = append(objects, entraUser(e.users[id], query.Get("$select")))
			}
			limit, _ := strconv.Atoi(query.Get("$top"))
			e.page(w, r, reply, objects, limit)
		default:
			reply(w, http.StatusBadRequest, map[string]string{"error": "Bad Request"})
		}
	})
	e.server = httptest.NewServer(mux)
	t.Cleanup(e.server.Close)

	former := microsoftGraph
	microsoftGraph = e.server.URL + "/v1.0"
	t.Cleanup(func() { microsoftGraph = former })
	return e
}

// page answers one page of objects, naming the next one.
func (e *fakeEntra) page(w http.ResponseWriter, r *http.Request, reply func(http.ResponseWriter, int, any), objects []entraObject, limit int) {
	query := r.URL.Query()
	from, _ := strconv.Atoi(query.Get("$skiptoken"))
	to := min(from+limit, len(objects))
	page := map[string]any{"value": objects[from:to]}
	if to < len(objects) {
		query.Set("$skiptoken", strconv.Itoa(to))
		page["@odata.nextLink"] = e.server.URL + r.URL.Path + "?" + query.Encode()
		if e.elsewhere != "" {
			page["@odata.nextLink"] = e.elsewhere
		}
	}
	reply(w, http.StatusOK, page)
}

// matches reads the one filter MaxIOFS sends: mail eq 'x' or userPrincipalName
// eq 'x', with a quote doubled.
func (e *fakeEntra) matches(filter string, u graphUser) bool {
	value, ok := strings.CutPrefix(filter, "mail eq '")
	if !ok {
		return false
	}
	value, _, ok = strings.Cut(value, "' or userPrincipalName eq '")
	value = strings.ReplaceAll(value, "''", "'")
	return ok && (strings.EqualFold(u.Mail, value) || strings.EqualFold(u.UserPrincipalName, value))
}

// entraUser is u with the properties asked for: accountEnabled only then.
func entraUser(u graphUser, selected string) entraObject {
	object := entraObject{"@odata.type": "#microsoft.graph.user", "id": u.ID, "displayName": u.DisplayName,
		"userPrincipalName": u.UserPrincipalName, "mail": nil}
	if u.Mail != "" {
		object["mail"] = u.Mail
	}
	if strings.Contains(selected, "accountEnabled") {
		object["accountEnabled"] = u.AccountEnabled
	}
	return object
}

func (e *fakeEntra) add(id, mail string, groups ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.users[id] = graphUser{ID: id, DisplayName: "Ana Ruiz", Mail: mail, UserPrincipalName: id + "@empresa.onmicrosoft.com", AccountEnabled: true}
	for _, g := range groups {
		e.groups[g] = append(e.groups[g], id)
	}
}

func (e *fakeEntra) set(change func(*fakeEntra)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	change(e)
}

func (e *fakeEntra) provider() *Provider {
	u := e.server.URL
	return NewProvider(&idp.OAuth2Config{
		Preset: "microsoft", ClientID: "maxiofs", ClientSecret: "secret", RedirectURI: fakeRedirectURI,
		AuthURL: u + "/common/oauth2/v2.0/authorize", TokenURL: u + "/common/oauth2/v2.0/token", UserInfoURL: u + "/oidc/userinfo",
		Issuer: u + "/" + entraTenant + "/v2.0",
	})
}

const staffGroup = "b1f7c2d4-staff"

// Without a sign-in to renew, a Microsoft user is looked up in its tenant by
// email or by user principal name: one gone or disabled is no longer let in. A
// tenant that refuses, or does not answer, ends nothing.
func TestAMicrosoftUserIsLookedUpInItsTenant(t *testing.T) {
	e := startEntra(t)
	e.add("id-ana", "ana@empresa.com", staffGroup)
	e.add("id-beto", "beto@empresa.com", staffGroup)
	e.set(func(e *fakeEntra) {
		beto := e.users["id-beto"]
		beto.AccountEnabled = false
		e.users["id-beto"] = beto
	})
	p := e.provider()
	ctx := context.Background()

	for _, subject := range []string{"ana@empresa.com", "ANA@empresa.com", "id-ana@empresa.onmicrosoft.com"} {
		user, err := p.Revalidate(ctx, idp.Grant{Subject: subject})
		require.NoError(t, err, subject)
		require.NotNil(t, user, subject)
		assert.Equal(t, "ana@empresa.com", user.ExternalID, subject)
		assert.Equal(t, "Ana Ruiz", user.DisplayName, subject)
		assert.Equal(t, []string{staffGroup}, user.Groups, "object IDs, without roles or a group's name")
	}
	for _, subject := range []string{"gone@empresa.com", "beto@empresa.com"} {
		_, err := p.Revalidate(ctx, idp.Grant{Subject: subject})
		assert.ErrorIs(t, err, idp.ErrSessionEnded, subject)
	}

	e.set(func(e *fakeEntra) { e.refuse = true })
	_, err := p.Revalidate(ctx, idp.Grant{Subject: "ana@empresa.com"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, idp.ErrSessionEnded)
	assert.NotErrorIs(t, err, idp.ErrUnavailable)
	assert.Contains(t, err.Error(), "User.Read.All")

	e.set(func(e *fakeEntra) { e.refuse, e.down = false, true })
	_, err = p.Revalidate(ctx, idp.Grant{Subject: "ana@empresa.com"})
	assert.ErrorIs(t, err, idp.ErrUnavailable)
}

// Entra ID's subject is the application's own for a user and names nobody in
// the directory: the tenant cannot tell, and nobody is deactivated for it.
func TestAMicrosoftSubjectIsNotADirectoryID(t *testing.T) {
	e := startEntra(t)
	user, err := e.provider().Revalidate(context.Background(), idp.Grant{Subject: "AAAAAAAAAAAAAAAAAAAAAIkzqFVrSaSaFHy782bbtaQ"})
	require.NoError(t, err)
	assert.Nil(t, user)
}

// A quote in an email does not end the filter.
func TestAQuoteInAnEmailStaysInTheFilter(t *testing.T) {
	e := startEntra(t)
	e.add("id-ana", "o'neil@empresa.com")
	user, err := e.provider().Revalidate(context.Background(), idp.Grant{Subject: "o'neil@empresa.com"})
	require.NoError(t, err)
	assert.Equal(t, "o'neil@empresa.com", user.Email)
	assert.Contains(t, e.filters[0], "mail eq 'o''neil@empresa.com'")

	_, err = e.provider().Revalidate(context.Background(), idp.Grant{Subject: "x' or mail ne 'y@empresa.com"})
	assert.ErrorIs(t, err, idp.ErrSessionEnded, "it names nobody")
}

// The members of a group are read page by page with Graph's advanced
// parameters; those disabled are left out, and a user without a mailbox is named
// by its user principal name.
func TestTheMembersOfAMicrosoftGroup(t *testing.T) {
	e := startEntra(t)
	for i := range graphPageSize + 200 {
		e.add(fmt.Sprintf("id-%d", i), fmt.Sprintf("user%d@empresa.com", i), staffGroup)
	}
	e.add("id-off", "off@empresa.com", staffGroup)
	e.add("id-nomail", "", staffGroup)
	e.set(func(e *fakeEntra) {
		off := e.users["id-off"]
		off.AccountEnabled = false
		e.users["id-off"] = off
	})

	members, err := e.provider().GetGroupMembers(context.Background(), staffGroup)
	require.NoError(t, err)
	assert.Len(t, members, graphPageSize+200+1)
	assert.EqualValues(t, 2, e.eventual.Load(), "two pages, each with the header")
	emails := map[string]bool{}
	for _, m := range members {
		assert.Equal(t, m.Email, m.ExternalID)
		emails[m.Email] = true
	}
	assert.False(t, emails["off@empresa.com"])
	assert.True(t, emails["id-nomail@empresa.onmicrosoft.com"])

	_, err = e.provider().GetGroupMembers(context.Background(), "nobody-group")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "object ID")
}

// The token goes to Graph only, wherever a page says the next one is.
func TestANextPageElsewhereIsNotFollowed(t *testing.T) {
	var asked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { asked.Add(1) }))
	defer other.Close()
	e := startEntra(t)
	e.add("id-ana", "ana@empresa.com", staffGroup, "second-group")
	e.set(func(e *fakeEntra) { e.elsewhere = other.URL + "/v1.0/next" })

	_, err := e.provider().Revalidate(context.Background(), idp.Grant{Subject: "ana@empresa.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "elsewhere")
	assert.NotErrorIs(t, err, idp.ErrSessionEnded)
	assert.Zero(t, asked.Load())
}

// Graph's userinfo gives no groups: a renewal reads them from the tenant. The
// application's token comes from the tenant, though the sign-in's URLs say
// common.
func TestAMicrosoftRenewalGetsItsGroupsFromTheTenant(t *testing.T) {
	e := startEntra(t)
	e.add("id-ana", "ana@empresa.com", staffGroup)
	e.set(func(e *fakeEntra) {
		e.userInfo = map[string]any{"sub": "pairwise", "email": "ana@empresa.com", "name": "Ana Ruiz"}
	})
	p := e.provider()
	ctx := context.Background()

	renewed, err := p.Revalidate(ctx, idp.Grant{Subject: "pairwise", RefreshToken: "refresh"})
	require.NoError(t, err)
	assert.Equal(t, []string{staffGroup}, renewed.Groups)

	e.set(func(e *fakeEntra) { e.down = true })
	_, err = p.Revalidate(ctx, idp.Grant{Subject: "pairwise", RefreshToken: "refresh"})
	assert.ErrorIs(t, err, idp.ErrUnavailable)
}

// The connection test reads the tenant; one that refuses is a warning, and says
// which permissions the application needs.
func TestTheConnectionTestReadsTheMicrosoftTenant(t *testing.T) {
	e := startEntra(t)
	ctx := context.Background()
	check := e.provider().checkDirectory(ctx)
	assert.Equal(t, idp.CheckPassed, check.Status, check.Detail)

	e.set(func(e *fakeEntra) { e.refuse = true })
	check = e.provider().checkDirectory(ctx)
	assert.Equal(t, idp.CheckWarning, check.Status)
	assert.Contains(t, check.Detail, "GroupMember.Read.All")
}
