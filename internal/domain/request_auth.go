package domain

import (
	"encoding/json"
	"strings"
)

const (
	AuthTypeNone   = ""
	AuthTypeBearer = "bearer"
	AuthTypeBasic  = "basic"
	AuthTypeAPIKey = "api_key"
	// AuthTypeOAuth2ClientCredentials fetches an access token from TokenURL
	// with the client_credentials grant and caches it until it expires.
	//nolint:gosec // G101 matches the word "Credentials"; this is an auth type name.
	AuthTypeOAuth2ClientCredentials = "oauth2_client_credentials"
	// AuthTypeOAuth2AuthorizationCode signs the user in through AuthURL with
	// PKCE (S256) and a loopback redirect, then exchanges the code at TokenURL.
	AuthTypeOAuth2AuthorizationCode = "oauth2_authorization_code"

	AuthAPIKeyInHeader = "header"
	AuthAPIKeyInQuery  = "query"
)

type AuthConfig struct {
	Token    string `json:"token,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	In       string `json:"in,omitempty"`
	Name     string `json:"name,omitempty"`
	Value    string `json:"value,omitempty"`
	// OAuth 2.0 fields. ClientSecret is optional for the authorization code
	// grant (public clients use PKCE alone).
	AuthURL      string `json:"auth_url,omitempty"`
	TokenURL     string `json:"token_url,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

func NormalizeAuthType(authType string) string {
	switch strings.ToLower(strings.TrimSpace(authType)) {
	case "", "none":
		return AuthTypeNone
	case AuthTypeBearer:
		return AuthTypeBearer
	case AuthTypeBasic:
		return AuthTypeBasic
	case AuthTypeAPIKey:
		return AuthTypeAPIKey
	case AuthTypeOAuth2ClientCredentials:
		return AuthTypeOAuth2ClientCredentials
	case AuthTypeOAuth2AuthorizationCode:
		return AuthTypeOAuth2AuthorizationCode
	default:
		return strings.ToLower(strings.TrimSpace(authType))
	}
}

func NormalizeAPIKeyIn(in string) string {
	switch strings.ToLower(strings.TrimSpace(in)) {
	case "", AuthAPIKeyInHeader:
		return AuthAPIKeyInHeader
	case AuthAPIKeyInQuery:
		return AuthAPIKeyInQuery
	default:
		return strings.ToLower(strings.TrimSpace(in))
	}
}

func ParseAuthConfig(raw string) (AuthConfig, error) {
	if strings.TrimSpace(raw) == "" {
		return AuthConfig{}, nil
	}
	var cfg AuthConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return AuthConfig{}, err
	}
	cfg.In = NormalizeAPIKeyIn(cfg.In)
	return cfg, nil
}

func MustAuthConfigJSON(cfg AuthConfig) string {
	//nolint:gosec // Request auth configs intentionally serialize password fields.
	b, _ := json.Marshal(cfg)
	return string(b)
}
