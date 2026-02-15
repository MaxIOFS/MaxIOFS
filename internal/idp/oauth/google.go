package oauth

import "github.com/maxiofs/maxiofs/internal/idp"

// ApplyGooglePreset fills in Google-specific defaults for the OAuth config
func ApplyGooglePreset(config *idp.OAuth2Config) idp.OAuth2Config {
	cfg := *config

	if cfg.AuthURL == "" {
		cfg.AuthURL = "https://accounts.google.com/o/oauth2/v2/auth"
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = "https://oauth2.googleapis.com/token"
	}
	if cfg.UserInfoURL == "" {
		cfg.UserInfoURL = "https://www.googleapis.com/oauth2/v3/userinfo"
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "profile", "email"}
	}
	if cfg.ClaimEmail == "" {
		cfg.ClaimEmail = "email"
	}
	if cfg.ClaimName == "" {
		cfg.ClaimName = "name"
	}

	return cfg
}
