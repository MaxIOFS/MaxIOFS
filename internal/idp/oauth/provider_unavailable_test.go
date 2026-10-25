package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maxiofs/maxiofs/internal/idp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func providerAt(url string) *Provider {
	return NewProvider(&idp.OAuth2Config{
		ClientID: "client", ClientSecret: "secret", RedirectURI: "http://localhost/callback",
		AuthURL: url + "/auth", TokenURL: url + "/token", UserInfoURL: url + "/userinfo",
	})
}

// A provider that takes the request and does not answer, at its token or its
// userinfo endpoint, or that is not there, is unavailable within the timeout.
// One that refuses the code is not.
func TestAnUnansweringProviderIsUnavailableInTime(t *testing.T) {
	former := providerTimeout
	providerTimeout = 500 * time.Millisecond
	defer func() { providerTimeout = former }()
	ctx := context.Background()

	release := make(chan struct{})
	hold := func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}
	frozen := httptest.NewServer(http.HandlerFunc(hold))
	defer frozen.Close()
	userinfoFrozen := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"t","token_type":"Bearer"}`))
			return
		}
		hold(w, r)
	}))
	defer userinfoFrozen.Close()
	defer close(release)
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()

	for name, url := range map[string]string{"frozen": frozen.URL, "userinfo frozen": userinfoFrozen.URL, "gone": goneURL} {
		p := providerAt(url)
		started := time.Now()
		_, err := p.ExchangeCode(ctx, "code")
		assert.ErrorIs(t, err, idp.ErrUnavailable, name)
		_, err = p.AuthenticateWithToken(ctx, "token")
		assert.ErrorIs(t, err, idp.ErrUnavailable, name)
		assert.Less(t, time.Since(started), 5*time.Second, name)
	}

	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer refusing.Close()
	_, err := providerAt(refusing.URL).ExchangeCode(ctx, "code")
	require.Error(t, err)
	assert.NotErrorIs(t, err, idp.ErrUnavailable, "a refused code")
}
