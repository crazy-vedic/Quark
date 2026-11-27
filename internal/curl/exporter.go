package curl

import (
	"fmt"
	"net/http"
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
	bodyType, err := domain.NormalizeBodyType(req.BodyType)
	if err != nil {
		return "", fmt.Errorf("export request body: %w", err)
	}
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
		if bodyType == domain.BodyTypeMultipart && strings.EqualFold(key, "Content-Type") {
			// The send sets the Content-Type, with its boundary.
			continue
		}
		for _, value := range headers[key] {
			args = append(args, "-H", shellQuote(key+": "+value))
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
	// A body type of none sends no body, so none is exported.
	switch {
	case bodyType == domain.BodyTypeNone:
	case bodyType == domain.BodyTypeMultipart:
		multipartArgs, err := multipartExportArgs(req.Body)
		if err != nil {
			return "", err
		}
		args = append(args, multipartArgs...)
	case req.Body != "":
		if ct := domain.DefaultBodyContentType(
			bodyType,
		); ct != "" &&
			!hasHeader(headers, "Content-Type") {
			args = append(args, "-H", shellQuote("Content-Type: "+ct))
		}
		args = append(args, "--data-raw", shellQuote(req.Body))
	}
	args = append(args, shellQuote(requestURL))
	return strings.Join(args, " "), nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// hasHeader reports whether headers has key, ignoring case.
func hasHeader(headers http.Header, key string) bool {
	for name := range headers {
		if strings.EqualFold(name, key) {
			return true
		}
	}
	return false
}

// multipartExportArgs returns the curl options for a multipart body. Text
// parts use --form-string so that a value starting with '@' or '<' is not read
// as a file; file parts use -F with their path.
func multipartExportArgs(stored string) ([]string, error) {
	parts, err := domain.ParseMultipartParts(stored)
	if err != nil {
		return nil, fmt.Errorf("export multipart body: %w", err)
	}
	var args []string
	for _, part := range parts {
		if part.File == "" {
			args = append(args, "--form-string", shellQuote(part.Name+"="+part.Value))
			continue
		}
		field := part.Name + "=@" + part.File
		if part.ContentType != "" {
			field += ";type=" + part.ContentType
		}
		if part.Filename != "" {
			field += ";filename=" + part.Filename
		}
		args = append(args, "-F", shellQuote(field))
	}
	return args, nil
}
