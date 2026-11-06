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
