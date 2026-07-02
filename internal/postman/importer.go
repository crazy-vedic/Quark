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
	CollectionName string
	Requests       []*domain.Request     // requests to import (with flattened folder names)
	Environments   []*domain.Environment // environments to import (from Postman environment files)
	Warnings       []string              // non-fatal issues (unsupported auth, body modes, etc.)
	Security       SecurityLevel         // max security level across all requests
	Skipped        int                   // number of requests skipped
}

// Importer parses Postman Collection v2.1 JSON files.
type Importer struct{}

// NewImporter constructs an Importer. Zero options are valid.
func NewImporter() *Importer {
	return &Importer{}
}

// Parse reads a Postman Collection JSON from r and returns the parsed result.
// Warnings are sorted lexicographically before returning.
func (im *Importer) Parse(r io.Reader) (*ImportResult, error) {
	c, err := Parse(r)
	if err != nil {
		return nil, err
	}

	result := ImportCollection(c)

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
		CollectionName: result.CollectionName,
		Requests:       result.Requests,
		Warnings:       result.Warnings,
		Security:       maxSecurity,
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
	if strings.Contains(req.URL, "@") && strings.Contains(req.URL, ":") {
		// Might have user:pass@host
		return Dangerous
	}

	// Check body for sensitive data (basic heuristic)
	if strings.Contains(req.Body, "password") || strings.Contains(req.Body, "token") ||
		strings.Contains(req.Body, "secret") {
		return Review
	}

	return Safe
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
