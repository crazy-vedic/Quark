package curl

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/crazy-vedic/quark/internal/domain"
)

// ExportRequest formats a saved request as a POSIX-shell-safe curl command.
func ExportRequest(req *domain.Request) (string, error) {
	if req == nil {
		return "", fmt.Errorf("request is required")
	}
	if strings.TrimSpace(req.URL) == "" {
		return "", fmt.Errorf("request URL is required")
	}
	method := strings.TrimSpace(req.Method)
	if method == "" {
		method = "GET"
	}
	requestURL := req.URL
	var headers map[string]string
	if strings.TrimSpace(req.Headers) != "" {
		if err := json.Unmarshal([]byte(req.Headers), &headers); err != nil {
			return "", fmt.Errorf("parse request headers: %w", err)
		}
	}
	args := []string{"curl", "-X", shellQuote(method)}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "-H", shellQuote(key+": "+headers[key]))
	}
	if strings.TrimSpace(req.AuthType) != "" {
		cfg, err := domain.ParseAuthConfig(req.AuthConfig)
		if err != nil {
			return "", fmt.Errorf("parse request authorization: %w", err)
		}
		switch domain.NormalizeAuthType(req.AuthType) {
		case domain.AuthTypeBearer:
			args = append(args, "-H", shellQuote("Authorization: Bearer "+cfg.Token))
		case domain.AuthTypeBasic:
			args = append(args, "-u", shellQuote(cfg.Username+":"+cfg.Password))
		case domain.AuthTypeAPIKey:
			if domain.NormalizeAPIKeyIn(cfg.In) == domain.AuthAPIKeyInQuery {
				parsed, err := url.Parse(requestURL)
				if err != nil {
					return "", fmt.Errorf("parse request URL: %w", err)
				}
				query := parsed.Query()
				query.Set(cfg.Name, cfg.Value)
				parsed.RawQuery = query.Encode()
				requestURL = parsed.String()
			} else {
				args = append(args, "-H", shellQuote(cfg.Name+": "+cfg.Value))
			}
		default:
			return "", fmt.Errorf("unsupported authorization type %q", req.AuthType)
		}
	}
	if req.Body != "" {
		args = append(args, "--data-raw", shellQuote(req.Body))
	}
	args = append(args, shellQuote(requestURL))
	return strings.Join(args, " "), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
