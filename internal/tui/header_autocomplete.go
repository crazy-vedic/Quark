package tui

import (
	"sort"
	"strings"
)

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

var commonHeaderValues = map[string][]string{
	"accept":        {"application/json", "text/plain", "*/*"},
	"content-type":  {"application/json", "application/x-www-form-urlencoded", "multipart/form-data"},
	"cache-control": {"no-cache", "no-store", "max-age=0"},
	"authorization": {"Bearer ", "Basic "},
}

func headerValueSuggestion(key, input string) string {
	values := commonHeaderValues[strings.ToLower(strings.TrimSpace(key))]
	for _, value := range values {
		if len(value) > len(input) && strings.EqualFold(value[:len(input)], input) {
			return value
		}
	}
	return ""
}

func (m Model) urlSuggestion(input string) string {
	if suggestion := variableCompletion(input, m.variableNames()); suggestion != "" {
		return suggestion
	}
	for _, requests := range m.collectionRequests {
		for _, req := range requests {
			if req != nil && len(req.URL) > len(input) && strings.HasPrefix(strings.ToLower(req.URL), strings.ToLower(input)) {
				return req.URL
			}
		}
	}
	return ""
}

func (m Model) variableNames() []string {
	seen := make(map[string]bool)
	add := func(value string) {
		for start := strings.Index(value, "{{"); start >= 0; {
			value = value[start+2:]
			end := strings.Index(value, "}}")
			if end < 0 {
				break
			}
			name := strings.TrimSpace(value[:end])
			if name != "" {
				seen[name] = true
			}
			value = value[end+2:]
			start = strings.Index(value, "{{")
		}
	}
	for _, requests := range m.collectionRequests {
		for _, req := range requests {
			if req == nil {
				continue
			}
			add(req.URL)
			add(req.Headers)
			add(req.Body)
		}
	}
	for _, variable := range m.envEditor.vars {
		if variable.Key != "" {
			seen[variable.Key] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func variableCompletion(input string, names []string) string {
	start := strings.LastIndex(input, "{{")
	if start < 0 || strings.Contains(input[start+2:], "}}") {
		return ""
	}
	prefix := input[start+2:]
	for _, name := range names {
		if len(name) > len(prefix) && strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			return input[:start+2] + name + "}}" + input[start+2+len(prefix):]
		}
	}
	return ""
}
