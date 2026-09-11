package tui

import "strings"

// commonHeaderNames is intentionally limited to request header names. Header
// values remain free-form because guessing a value can change request
// semantics or expose credentials.
var commonHeaderNames = []string{
	"Accept",
	"Accept-Encoding",
	"Accept-Language",
	"Authorization",
	"Cache-Control",
	"Content-Length",
	"Content-Type",
	"Cookie",
	"If-Match",
	"If-None-Match",
	"Origin",
	"Referer",
	"User-Agent",
	"X-API-Key",
	"X-Request-ID",
	"X-Trace-ID",
}

// headerNameSuggestion returns the first standard header name that extends
// input. Matching is case-insensitive, but the returned completion uses the
// conventional spelling. Empty and already-complete inputs have no match.
func headerNameSuggestion(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	for _, name := range commonHeaderNames {
		if len(name) <= len(input) || !strings.EqualFold(name[:len(input)], input) {
			continue
		}
		return name
	}
	return ""
}
