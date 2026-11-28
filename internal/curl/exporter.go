package curl

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
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
	args = append(args, optionArgs(req.Options)...)
	if req.Body != "" {
		args = append(args, "--data-raw", shellQuote(req.Body))
	}
	args = append(args, shellQuote(requestURL))
	return strings.Join(args, " "), nil
}

// optionArgs writes the request's per-request options as the curl flags the
// importer reads back (see parser.go), so a copied command keeps its settings.
// Options left unset are omitted and keep Quark's defaults.
func optionArgs(opts domain.RequestOptions) []string {
	var args []string
	if opts.FollowRedirects != nil {
		if *opts.FollowRedirects {
			args = append(args, "-L")
		} else {
			// Following nothing is the same as following with a zero limit, and
			// that form survives import, unlike a bare "no -L".
			args = append(args, "-L", "--max-redirs", "0")
		}
	}
	if opts.MaxRedirects != nil && (opts.FollowRedirects == nil || *opts.FollowRedirects) {
		args = append(args, "--max-redirs", strconv.Itoa(*opts.MaxRedirects))
	}
	if opts.SkipTLSVerify {
		args = append(args, "-k")
	}
	if opts.Proxy != "" {
		args = append(args, "-x", shellQuote(opts.Proxy))
	}
	if opts.TimeoutMS > 0 {
		seconds := strconv.FormatFloat(float64(opts.TimeoutMS)/1000, 'f', -1, 64)
		args = append(args, "--max-time", seconds)
	}
	return args
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
