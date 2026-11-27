package curl

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/crazy-vedic/quark/internal/domain"
)

// ExportOption configures ExportRequest.
type ExportOption func(*exportOptions)

type exportOptions struct {
	fileBaseDir string
}

// WithFileBaseDir sets the directory relative @file references are resolved
// against, so the exported command names the same file Quark sends.
func WithFileBaseDir(dir string) ExportOption {
	return func(o *exportOptions) { o.fileBaseDir = dir }
}

// ExportRequest formats a saved request as a POSIX-shell-safe curl command.
// Body and header values follow Quark's send rules (domain.ParseFileRef): an
// @file body becomes --data-binary @file, an @file header value is read with
// $(cat file), and an escaped "@@..." value is exported as the literal text.
func ExportRequest(req *domain.Request, opts ...ExportOption) (string, error) {
	var o exportOptions
	for _, opt := range opts {
		opt(&o)
	}
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
	headers, err := domain.ParseHeaders(req.Headers)
	if err != nil {
		return "", fmt.Errorf("parse request headers: %w", err)
	}
	args := []string{"curl", "-X", shellQuote(method)}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, value := range headers[key] {
			path, isFile, literal := domain.ParseFileRef(value)
			if isFile {
				resolved := domain.ResolveFileRefPath(o.fileBaseDir, path)
				args = append(args, "-H",
					shellQuote(key+": ")+`"$(cat `+shellQuote(resolved)+`)"`)
				continue
			}
			args = append(args, "-H", shellQuote(key+": "+literal))
		}
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
		path, isFile, literal := domain.ParseFileRef(req.Body)
		if isFile {
			resolved := domain.ResolveFileRefPath(o.fileBaseDir, path)
			args = append(args, "--data-binary", shellQuote("@"+resolved))
		} else {
			args = append(args, "--data-raw", shellQuote(literal))
		}
	}
	args = append(args, shellQuote(requestURL))
	return strings.Join(args, " "), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
