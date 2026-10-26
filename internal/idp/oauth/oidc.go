package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/maxiofs/maxiofs/internal/idp"
	"golang.org/x/oauth2"
)

// backchannelLogoutEvent is the event a back-channel logout token carries
// (OpenID Connect Back-Channel Logout 1.0).
const backchannelLogoutEvent = "http://schemas.openid.net/event/backchannel-logout"

// logoutTokenMaxAge is how old a logout token may be when it arrives; its ID
// is remembered for that long, so a token sent again is refused.
const logoutTokenMaxAge = 10 * time.Minute

// discovery is the provider's OpenID Connect configuration, read once from
// its issuer: nil without an issuer.
func (p *Provider) discovery() (*oidc.Provider, error) {
	if p.config.Issuer == "" {
		return nil, nil
	}
	p.oidcMu.Lock()
	defer p.oidcMu.Unlock()
	if p.oidcProvider != nil {
		return p.oidcProvider, nil
	}
	// The key set keeps this context to fetch keys later: it has to outlive
	// the request.
	ctx := oidc.ClientContext(context.Background(), &http.Client{Timeout: providerTimeout})
	provider, err := oidc.NewProvider(ctx, p.config.Issuer)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to read the OpenID configuration of %s: %w", idp.ErrUnavailable, p.config.Issuer, err)
	}
	p.oidcProvider = provider
	return provider, nil
}

// idTokenGrant reads the subject and the session of the ID token that came
// with token, checked against the issuer's keys. Without an issuer it has
// nothing to check.
func (p *Provider) idTokenGrant(ctx context.Context, token *oauth2.Token) (subject, session string, err error) {
	provider, err := p.discovery()
	if err != nil || provider == nil {
		return "", "", err
	}
	raw, _ := token.Extra("id_token").(string)
	if raw == "" {
		return "", "", fmt.Errorf("the provider sent no ID token: ask for the openid scope")
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: p.config.ClientID}).Verify(ctx, raw)
	if err != nil {
		return "", "", fmt.Errorf("the ID token does not verify: %w", err)
	}
	var claims struct {
		Sid string `json:"sid"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return "", "", err
	}
	return idToken.Subject, claims.Sid, nil
}

// VerifyLogoutToken checks a back-channel logout token as the standard says:
// signed by the issuer for this client, with the logout event, an ID not seen
// before, recent, without a nonce, naming a subject or a session.
func (p *Provider) VerifyLogoutToken(ctx context.Context, raw string) (string, string, error) {
	provider, err := p.discovery()
	if err != nil {
		return "", "", err
	}
	if provider == nil {
		return "", "", fmt.Errorf("the provider has no issuer: back-channel logout needs one")
	}
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	// A logout token need not expire; its age is checked below.
	token, err := provider.Verifier(&oidc.Config{ClientID: p.config.ClientID, SkipExpiryCheck: true}).Verify(ctx, raw)
	if err != nil {
		return "", "", fmt.Errorf("the logout token does not verify: %w", err)
	}
	var claims struct {
		Sid    string                     `json:"sid"`
		Jti    string                     `json:"jti"`
		Events map[string]json.RawMessage `json:"events"`
		Nonce  *string                    `json:"nonce"`
	}
	if err := token.Claims(&claims); err != nil {
		return "", "", err
	}
	switch {
	case claims.Events[backchannelLogoutEvent] == nil:
		return "", "", errors.New("the logout token has no logout event")
	case claims.Nonce != nil:
		return "", "", errors.New("a logout token has no nonce")
	case claims.Jti == "":
		return "", "", errors.New("the logout token has no ID")
	case token.Subject == "" && claims.Sid == "":
		return "", "", errors.New("the logout token names neither a subject nor a session")
	case token.IssuedAt.IsZero() || time.Since(token.IssuedAt) > logoutTokenMaxAge:
		return "", "", errors.New("the logout token is too old")
	}

	now := time.Now()
	p.seenLogouts.Range(func(id, seen any) bool {
		if now.Sub(seen.(time.Time)) > logoutTokenMaxAge {
			p.seenLogouts.Delete(id)
		}
		return true
	})
	if _, seen := p.seenLogouts.LoadOrStore(claims.Jti, now); seen {
		return "", "", errors.New("the logout token was sent before")
	}
	return token.Subject, claims.Sid, nil
}

// errUserInfoRefused is the error of a userinfo endpoint that refused the
// access token.
var errUserInfoRefused = errors.New("userinfo refused the token")

// Revalidate renews the sign-in with the provider's refresh token and reads
// the user again: a refresh the provider refuses, or a userinfo that refuses
// the new token, ends the sign-in. Without a refresh token it cannot tell.
func (p *Provider) Revalidate(ctx context.Context, grant idp.Grant) (*idp.ExternalUser, error) {
	if grant.RefreshToken == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()

	token, err := p.oauthConfig.TokenSource(ctx, &oauth2.Token{RefreshToken: grant.RefreshToken}).Token()
	if err != nil {
		var refused *oauth2.RetrieveError
		switch {
		case errors.As(err, &refused) && refused.ErrorCode == "invalid_grant":
			return nil, fmt.Errorf("%w: the provider refused the refresh: %w", idp.ErrSessionEnded, err)
		case errors.As(err, &refused) && refused.Response != nil && refused.Response.StatusCode < 500:
			return nil, fmt.Errorf("the provider refused the refresh: %w", err)
		}
		return nil, fmt.Errorf("%w: failed to refresh: %w", idp.ErrUnavailable, err)
	}

	user, err := p.fetchUserInfo(ctx, p.oauthConfig.Client(ctx, token))
	if errors.Is(err, errUserInfoRefused) {
		return nil, fmt.Errorf("%w: %w", idp.ErrSessionEnded, err)
	}
	if err != nil {
		return nil, err
	}
	if grant.Subject != "" && user.Grant.Subject != "" && user.Grant.Subject != grant.Subject {
		return nil, fmt.Errorf("%w: the provider answered for another subject", idp.ErrSessionEnded)
	}
	subject := grant.Subject
	if subject == "" {
		subject = user.Grant.Subject
	}
	user.Grant = &idp.Grant{Subject: subject, Session: grant.Session, RefreshToken: token.RefreshToken}
	return user, nil
}
