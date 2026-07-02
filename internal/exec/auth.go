package exec

import (
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/crazy-vedic/quark/internal/domain"
)

func applyRequestAuth(httpReq *http.Request, req *domain.Request) error {
	authType := domain.NormalizeAuthType(req.AuthType)
	if authType == domain.AuthTypeNone {
		return nil
	}

	cfg, err := domain.ParseAuthConfig(req.AuthConfig)
	if err != nil {
		return fmt.Errorf("invalid auth config: %w", err)
	}

	switch authType {
	case domain.AuthTypeBearer:
		if cfg.Token == "" {
			return fmt.Errorf("invalid auth config: bearer token is required")
		}
		httpReq.Header.Set("Authorization", "Bearer "+cfg.Token)
		return nil
	case domain.AuthTypeBasic:
		if cfg.Username == "" && cfg.Password == "" {
			return fmt.Errorf("invalid auth config: basic username or password is required")
		}
		creds := base64.StdEncoding.EncodeToString([]byte(cfg.Username + ":" + cfg.Password))
		httpReq.Header.Set("Authorization", "Basic "+creds)
		return nil
	case domain.AuthTypeAPIKey:
		if cfg.Name == "" {
			return fmt.Errorf("invalid auth config: api key name is required")
		}
		if cfg.Value == "" {
			return fmt.Errorf("invalid auth config: api key value is required")
		}
		switch domain.NormalizeAPIKeyIn(cfg.In) {
		case domain.AuthAPIKeyInHeader:
			httpReq.Header.Set(cfg.Name, cfg.Value)
			return nil
		case domain.AuthAPIKeyInQuery:
			q := httpReq.URL.Query()
			q.Set(cfg.Name, cfg.Value)
			httpReq.URL.RawQuery = q.Encode()
			return nil
		default:
			return fmt.Errorf("invalid auth config: unsupported api key location %q", cfg.In)
		}
	default:
		return fmt.Errorf("unsupported auth type %q", authType)
	}
}
