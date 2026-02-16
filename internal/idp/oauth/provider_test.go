package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProvider(t *testing.T) {
	t.Run("creates provider with basic config", func(t *testing.T) {
		config := &idp.OAuth2Config{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			AuthURL:      "https://auth.example.com/authorize",
			TokenURL:     "https://auth.example.com/token",
			UserInfoURL:  "https://auth.example.com/userinfo",
			Scopes:       []string{"openid", "email"},
			RedirectURI:  "http://localhost:8081/api/v1/auth/oauth/callback",
		}

		p := NewProvider(config)
		require.NotNil(t, p)
		assert.Equal(t, "test-client", p.config.ClientID)
		assert.Equal(t, "test-secret", p.config.ClientSecret)
	})

	t.Run("applies google preset", func(t *testing.T) {
		config := &idp.OAuth2Config{
			Preset:       "google",
			ClientID:     "google-client",
			ClientSecret: "google-secret",
			RedirectURI:  "http://localhost/callback",
		}

		p := NewProvider(config)
		require.NotNil(t, p)
		assert.Equal(t, "https://accounts.google.com/o/oauth2/v2/auth", p.config.AuthURL)
		assert.Equal(t, "https://oauth2.googleapis.com/token", p.config.TokenURL)
		assert.Equal(t, "https://www.googleapis.com/oauth2/v3/userinfo", p.config.UserInfoURL)
		assert.Contains(t, p.config.Scopes, "openid")
		assert.Contains(t, p.config.Scopes, "email")
		assert.Equal(t, "email", p.config.ClaimEmail)
		assert.Equal(t, "name", p.config.ClaimName)
	})

	t.Run("applies microsoft preset", func(t *testing.T) {
		config := &idp.OAuth2Config{
			Preset:       "microsoft",
			ClientID:     "ms-client",
			ClientSecret: "ms-secret",
			RedirectURI:  "http://localhost/callback",
		}

		p := NewProvider(config)
		require.NotNil(t, p)
		assert.Equal(t, "https://login.microsoftonline.com/common/oauth2/v2.0/authorize", p.config.AuthURL)
		assert.Equal(t, "https://login.microsoftonline.com/common/oauth2/v2.0/token", p.config.TokenURL)
		assert.Equal(t, "https://graph.microsoft.com/oidc/userinfo", p.config.UserInfoURL)
		assert.Contains(t, p.config.Scopes, "openid")
	})

	t.Run("preset does not override explicit URLs", func(t *testing.T) {
		config := &idp.OAuth2Config{
			Preset:       "google",
			ClientID:     "client",
			ClientSecret: "secret",
			AuthURL:      "https://custom-auth.example.com",
			TokenURL:     "https://custom-token.example.com",
			RedirectURI:  "http://localhost/callback",
		}

		p := NewProvider(config)
		assert.Equal(t, "https://custom-auth.example.com", p.config.AuthURL)
		assert.Equal(t, "https://custom-token.example.com", p.config.TokenURL)
		// UserInfoURL not set, should be filled by preset
		assert.Equal(t, "https://www.googleapis.com/oauth2/v3/userinfo", p.config.UserInfoURL)
	})
}

func TestProvider_Type(t *testing.T) {
	p := NewProvider(&idp.OAuth2Config{})
	assert.Equal(t, "oauth2", p.Type())
}

func TestProvider_TestConnection(t *testing.T) {
	t.Run("succeeds with valid config", func(t *testing.T) {
		p := NewProvider(&idp.OAuth2Config{
			ClientID:     "client",
			ClientSecret: "secret",
			AuthURL:      "https://auth.example.com/authorize",
			TokenURL:     "https://auth.example.com/token",
		})
		err := p.TestConnection(context.Background())
		assert.NoError(t, err)
	})

	t.Run("fails without client_id", func(t *testing.T) {
		p := NewProvider(&idp.OAuth2Config{
			ClientSecret: "secret",
			AuthURL:      "https://auth.example.com/authorize",
			TokenURL:     "https://auth.example.com/token",
		})
		err := p.TestConnection(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "client_id")
	})

	t.Run("fails without client_secret", func(t *testing.T) {
		p := NewProvider(&idp.OAuth2Config{
			ClientID: "client",
			AuthURL:  "https://auth.example.com/authorize",
			TokenURL: "https://auth.example.com/token",
		})
		err := p.TestConnection(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "client_secret")
	})

	t.Run("fails without auth_url", func(t *testing.T) {
		p := NewProvider(&idp.OAuth2Config{
			ClientID:     "client",
			ClientSecret: "secret",
			TokenURL:     "https://auth.example.com/token",
		})
		err := p.TestConnection(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "auth_url")
	})

	t.Run("fails without token_url", func(t *testing.T) {
		p := NewProvider(&idp.OAuth2Config{
			ClientID:     "client",
			ClientSecret: "secret",
			AuthURL:      "https://auth.example.com/authorize",
		})
		err := p.TestConnection(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "token_url")
	})
}

func TestProvider_AuthenticateUser(t *testing.T) {
	p := NewProvider(&idp.OAuth2Config{})
	_, err := p.AuthenticateUser(context.Background(), "user", "pass")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support direct username/password")
}

func TestProvider_SearchUsers(t *testing.T) {
	p := NewProvider(&idp.OAuth2Config{})
	_, err := p.SearchUsers(context.Background(), "query", 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support user search")
}

func TestProvider_SearchGroups(t *testing.T) {
	p := NewProvider(&idp.OAuth2Config{})
	_, err := p.SearchGroups(context.Background(), "query", 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support group search")
}

func TestProvider_GetGroupMembers(t *testing.T) {
	p := NewProvider(&idp.OAuth2Config{})
	_, err := p.GetGroupMembers(context.Background(), "group-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not support group member listing")
}

func TestProvider_GetAuthURL(t *testing.T) {
	t.Run("generates URL with state", func(t *testing.T) {
		p := NewProvider(&idp.OAuth2Config{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			AuthURL:      "https://auth.example.com/authorize",
			TokenURL:     "https://auth.example.com/token",
			Scopes:       []string{"openid", "email"},
			RedirectURI:  "http://localhost/callback",
		})

		url, err := p.GetAuthURL("mystate123", "")
		require.NoError(t, err)
		assert.Contains(t, url, "https://auth.example.com/authorize")
		assert.Contains(t, url, "client_id=test-client")
		assert.Contains(t, url, "state=mystate123")
		assert.Contains(t, url, "redirect_uri=")
		assert.Contains(t, url, "response_type=code")
	})

	t.Run("includes login_hint when provided", func(t *testing.T) {
		p := NewProvider(&idp.OAuth2Config{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			AuthURL:      "https://auth.example.com/authorize",
			TokenURL:     "https://auth.example.com/token",
			RedirectURI:  "http://localhost/callback",
		})

		url, err := p.GetAuthURL("state", "user@example.com")
		require.NoError(t, err)
		assert.Contains(t, url, "login_hint=user%40example.com")
	})

	t.Run("does not include login_hint when empty", func(t *testing.T) {
		p := NewProvider(&idp.OAuth2Config{
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			AuthURL:      "https://auth.example.com/authorize",
			TokenURL:     "https://auth.example.com/token",
			RedirectURI:  "http://localhost/callback",
		})

		url, err := p.GetAuthURL("state", "")
		require.NoError(t, err)
		assert.NotContains(t, url, "login_hint")
	})
}

func TestProvider_fetchUserInfo(t *testing.T) {
	t.Run("parses standard claims", func(t *testing.T) {
		claims := map[string]interface{}{
			"sub":   "user-123",
			"email": "user@example.com",
			"name":  "Test User",
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(claims)
		}))
		defer server.Close()

		p := NewProvider(&idp.OAuth2Config{
			UserInfoURL: server.URL,
		})

		user, err := p.fetchUserInfo(server.Client())
		require.NoError(t, err)
		assert.Equal(t, "user@example.com", user.Email)
		assert.Equal(t, "user@example.com", user.Username)
		assert.Equal(t, "user@example.com", user.ExternalID)
		assert.Equal(t, "Test User", user.DisplayName)
	})

	t.Run("uses sub as external ID when no email", func(t *testing.T) {
		claims := map[string]interface{}{
			"sub":  "user-456",
			"name": "No Email User",
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(claims)
		}))
		defer server.Close()

		p := NewProvider(&idp.OAuth2Config{
			UserInfoURL: server.URL,
		})

		user, err := p.fetchUserInfo(server.Client())
		require.NoError(t, err)
		assert.Equal(t, "user-456", user.ExternalID)
		assert.Equal(t, "", user.Email)
	})

	t.Run("extracts groups from claims", func(t *testing.T) {
		claims := map[string]interface{}{
			"sub":    "user-789",
			"email":  "user@example.com",
			"name":   "Group User",
			"groups": []interface{}{"admins", "developers", "team-alpha"},
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(claims)
		}))
		defer server.Close()

		p := NewProvider(&idp.OAuth2Config{
			UserInfoURL: server.URL,
			ClaimGroups: "groups",
		})

		user, err := p.fetchUserInfo(server.Client())
		require.NoError(t, err)
		assert.Equal(t, []string{"admins", "developers", "team-alpha"}, user.Groups)
	})

	t.Run("no groups when claim not configured", func(t *testing.T) {
		claims := map[string]interface{}{
			"sub":    "user-789",
			"email":  "user@example.com",
			"groups": []interface{}{"admins"},
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(claims)
		}))
		defer server.Close()

		p := NewProvider(&idp.OAuth2Config{
			UserInfoURL: server.URL,
			// ClaimGroups intentionally not set
		})

		user, err := p.fetchUserInfo(server.Client())
		require.NoError(t, err)
		assert.Nil(t, user.Groups)
	})

	t.Run("uses custom claim names", func(t *testing.T) {
		claims := map[string]interface{}{
			"sub":           "user-custom",
			"mail":          "custom@example.com",
			"display_name":  "Custom Name",
			"team_groups":   []interface{}{"team-1"},
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(claims)
		}))
		defer server.Close()

		p := NewProvider(&idp.OAuth2Config{
			UserInfoURL: server.URL,
			ClaimEmail:  "mail",
			ClaimName:   "display_name",
			ClaimGroups: "team_groups",
		})

		user, err := p.fetchUserInfo(server.Client())
		require.NoError(t, err)
		assert.Equal(t, "custom@example.com", user.Email)
		assert.Equal(t, "Custom Name", user.DisplayName)
		assert.Equal(t, []string{"team-1"}, user.Groups)
	})

	t.Run("populates raw attributes", func(t *testing.T) {
		claims := map[string]interface{}{
			"sub":   "user-raw",
			"email": "raw@example.com",
			"name":  "Raw User",
			"extra": "custom-value",
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(claims)
		}))
		defer server.Close()

		p := NewProvider(&idp.OAuth2Config{
			UserInfoURL: server.URL,
		})

		user, err := p.fetchUserInfo(server.Client())
		require.NoError(t, err)
		assert.Equal(t, "custom-value", user.RawAttrs["extra"])
		assert.Equal(t, "raw@example.com", user.RawAttrs["email"])
	})

	t.Run("error when userinfo_url not configured", func(t *testing.T) {
		p := NewProvider(&idp.OAuth2Config{
			// UserInfoURL intentionally empty
		})

		_, err := p.fetchUserInfo(http.DefaultClient)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "userinfo_url is not configured")
	})

	t.Run("error on non-200 response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("token expired"))
		}))
		defer server.Close()

		p := NewProvider(&idp.OAuth2Config{
			UserInfoURL: server.URL,
		})

		_, err := p.fetchUserInfo(server.Client())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "401")
	})

	t.Run("error on invalid JSON response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("not json"))
		}))
		defer server.Close()

		p := NewProvider(&idp.OAuth2Config{
			UserInfoURL: server.URL,
		})

		_, err := p.fetchUserInfo(server.Client())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parse userinfo response")
	})
}

func TestApplyPreset(t *testing.T) {
	t.Run("unknown preset leaves config unchanged", func(t *testing.T) {
		config := &idp.OAuth2Config{
			Preset:   "unknown",
			ClientID: "client",
		}
		result := applyPreset(config)
		assert.Equal(t, "", result.AuthURL)
		assert.Equal(t, "", result.TokenURL)
	})

	t.Run("does not mutate original config", func(t *testing.T) {
		original := &idp.OAuth2Config{
			Preset:       "google",
			ClientID:     "client",
			ClientSecret: "secret",
		}
		result := applyPreset(original)

		// Original should be unchanged
		assert.Equal(t, "", original.AuthURL)
		// Result should have Google defaults
		assert.NotEqual(t, "", result.AuthURL)
		assert.Contains(t, result.AuthURL, "google")
	})
}

func TestApplyGooglePreset(t *testing.T) {
	t.Run("fills all defaults", func(t *testing.T) {
		config := &idp.OAuth2Config{}
		result := ApplyGooglePreset(config)

		assert.Equal(t, "https://accounts.google.com/o/oauth2/v2/auth", result.AuthURL)
		assert.Equal(t, "https://oauth2.googleapis.com/token", result.TokenURL)
		assert.Equal(t, "https://www.googleapis.com/oauth2/v3/userinfo", result.UserInfoURL)
		assert.Equal(t, []string{"openid", "profile", "email"}, result.Scopes)
		assert.Equal(t, "email", result.ClaimEmail)
		assert.Equal(t, "name", result.ClaimName)
	})

	t.Run("does not override existing values", func(t *testing.T) {
		config := &idp.OAuth2Config{
			AuthURL:    "https://custom-auth.com",
			ClaimEmail: "custom_email",
		}
		result := ApplyGooglePreset(config)

		assert.Equal(t, "https://custom-auth.com", result.AuthURL)
		assert.Equal(t, "custom_email", result.ClaimEmail)
		// But fills in the rest
		assert.Equal(t, "https://oauth2.googleapis.com/token", result.TokenURL)
		assert.Equal(t, "name", result.ClaimName)
	})
}

func TestProvider_ExchangeCode_Error(t *testing.T) {
	// ExchangeCode requires a real OAuth token endpoint — test that it
	// returns a proper error when the endpoint is unavailable/invalid.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid_grant"}`))
	}))
	defer server.Close()

	p := NewProvider(&idp.OAuth2Config{
		ClientID:     "client",
		ClientSecret: "secret",
		AuthURL:      "https://auth.example.com/authorize",
		TokenURL:     server.URL,
		RedirectURI:  "http://localhost/callback",
	})

	_, err := p.ExchangeCode(context.Background(), "invalid-code")
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "exchange") || strings.Contains(err.Error(), "oauth2"))
}
