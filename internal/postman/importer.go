package postman

import (
	"encoding/json"
	"io"
	"sort"
	"strings"

	"github.com/crazy-vedic/quark/internal/domain"
)

// SecurityLevel indicates the safety of imported content.
type SecurityLevel int

const (
	Safe      SecurityLevel = 0
	Review    SecurityLevel = 1
	Dangerous SecurityLevel = 2
)

func (s SecurityLevel) String() string {
	switch s {
	case Safe:
		return "Safe"
	case Review:
		return "Review"
	case Dangerous:
		return "Dangerous"
	default:
		return "Unknown"
	}
}

// ImportResult holds the result of parsing a Postman collection.
type ImportResult struct {
	CollectionName      string
	CollectionVariables map[string]string // root collection-scoped variables
	Requests            []*domain.Request // all requests, in Postman traversal order
	Groups              []RequestGroup    // requests grouped by nested folder path
	// AuthType and AuthConfig are the root collection's defaults, set when the
	// Postman collection has collection-level auth Quark can store.
	AuthType     string
	AuthConfig   string
	Environments []*domain.Environment // retained for compatibility
	Warnings     []string              // non-fatal issues (unsupported auth, body modes, etc.)
	Security     SecurityLevel         // max security level across all requests
	Skipped      int                   // number of requests skipped
}

// RequestGroup is a Postman folder path and the requests directly in it.
// Path is slash-delimited and empty means the root collection.
type RequestGroup struct {
	Path     string
	Requests []*domain.Request
	// AuthType and AuthConfig are the folder's default auth, when it sets one.
	AuthType   string
	AuthConfig string
}

// Importer parses Postman Collection v2.1 JSON files.
type Importer struct {
	includeDisabledHeaders bool
}

// Option configures an Importer.
type Option func(*Importer)

// WithDisabledHeaders imports headers that are switched off in Postman as
// active headers. Quark has no per-header switch, so by default they are left
// out; either way the result carries a warning naming each one.
func WithDisabledHeaders() Option {
	return func(im *Importer) { im.includeDisabledHeaders = true }
}

// NewImporter constructs an Importer. Zero options are valid.
func NewImporter(opts ...Option) *Importer {
	im := &Importer{}
	for _, opt := range opts {
		opt(im)
	}
	return im
}

// Parse reads a Postman Collection JSON from r and returns the parsed result.
// Warnings are sorted lexicographically before returning.
func (im *Importer) Parse(r io.Reader) (*ImportResult, error) {
	c, err := Parse(r)
	if err != nil {
		return nil, err
	}

	result := importCollection(c, im.includeDisabledHeaders)

	// Security scoring: scan for credentials in headers, URLs, and auth config.
	maxSecurity := Safe
	for _, req := range result.Requests {
		sec := scoreRequestSecurity(req)
		if sec > maxSecurity {
			maxSecurity = sec
		}
	}

	// Deduplicate and sort warnings.
	result.Warnings = deduplicateAndSort(result.Warnings)
	result.Security = maxSecurity

	return &ImportResult{
		CollectionName:      result.CollectionName,
		CollectionVariables: result.CollectionVariables,
		Requests:            result.Requests,
		Groups:              result.Groups,
		AuthType:            result.AuthType,
		AuthConfig:          result.AuthConfig,
		Warnings:            result.Warnings,
		Security:            maxSecurity,
	}, nil
}

// scoreRequestSecurity checks a single request for potential credentials.
func scoreRequestSecurity(req *domain.Request) SecurityLevel {
	// Check headers for auth credentials
	var headers map[string]string
	if req.Headers != "" && req.Headers != "{}" {
		if err := json.Unmarshal([]byte(req.Headers), &headers); err != nil {
			return Review
		}
	}

	for key, val := range headers {
		lowerKey := strings.ToLower(key)
		lowerVal := strings.ToLower(val)
		if isCredentialHeader(lowerKey) {
			if lowerVal == "" || strings.Contains(lowerVal, "{{") {
				// Variable placeholder or empty: Review (might be resolved at runtime)
				return Review
			}
			// Actual credential value: Dangerous
			return Dangerous
		}
		// Check for embedded tokens in values
		if strings.Contains(lowerVal, "bearer ") && len(val) > 20 {
			return Dangerous
		}
	}

	// Check URL for credentials (e.g., https://user:pass@host.com)
	if urlHasUserinfo(req.URL) {
		return Dangerous
	}

	// Check body for sensitive data (basic heuristic)
	if strings.Contains(req.Body, "password") || strings.Contains(req.Body, "token") ||
		strings.Contains(req.Body, "secret") {
		return Review
	}

	return Safe
}

// urlHasUserinfo reports whether the authority part of rawURL carries
// userinfo (user@host or user:pass@host). It works on the raw text because
// imported URLs may contain {{placeholders}} that net/url cannot parse.
func urlHasUserinfo(rawURL string) bool {
	authority := rawURL
	if i := strings.Index(authority, "://"); i >= 0 {
		authority = authority[i+len("://"):]
	}
	if i := strings.IndexAny(authority, "/?#"); i >= 0 {
		authority = authority[:i]
	}
	return strings.Contains(authority, "@")
}

func isCredentialHeader(key string) bool {
	switch key {
	case "authorization", "cookie", "x-api-key", "x-auth-token", "api-key":
		return true
	}
	return false
}

func deduplicateAndSort(warnings []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(warnings))
	for _, w := range warnings {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	// Lexicographic sort for stable output
	sort.Strings(out)
	return out
}
