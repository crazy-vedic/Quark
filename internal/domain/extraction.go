package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Extraction sources.
const (
	ExtractFromBody   = "body"   // Path is a JSONPath into the response body
	ExtractFromHeader = "header" // Path is a response header name
)

// Extraction copies one value from a response into an environment variable.
type Extraction struct {
	Variable string `json:"variable"`
	Source   string `json:"source"`
	Path     string `json:"path"`
}

// ParseExtractions decodes the JSON array stored in Request.Extractions.
// An empty string means no rules.
func ParseExtractions(raw string) ([]Extraction, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var rules []Extraction
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil, fmt.Errorf("extraction rules are not valid JSON: %w", err)
	}
	return rules, nil
}

// EncodeExtractions encodes rules for Request.Extractions. No rules encode
// as "[]".
func EncodeExtractions(rules []Extraction) string {
	if len(rules) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(rules)
	return string(b)
}

// ParseExtractionText parses the line format used by the TUI editor. Each
// non-blank line that does not start with '#' is one rule:
//
//	token = $.data.token          JSONPath into the response body
//	request_id = header X-Req-Id  response header
func ParseExtractionText(text string) ([]Extraction, error) {
	var rules []Extraction
	for n, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, source, ok := strings.Cut(line, "=")
		name, source = strings.TrimSpace(name), strings.TrimSpace(source)
		if !ok || name == "" || source == "" {
			return nil, fmt.Errorf(
				"line %d: expected 'variable = $.path' or 'variable = header Name'",
				n+1,
			)
		}
		if strings.ContainsAny(name, " \t{}|") {
			return nil, fmt.Errorf(
				"line %d: variable name %q must not contain spaces, braces or '|'",
				n+1,
				name,
			)
		}
		rule := Extraction{Variable: name, Source: ExtractFromBody, Path: source}
		if header, isHeader := cutWord(source, "header"); isHeader {
			if header == "" {
				return nil, fmt.Errorf("line %d: header name is missing", n+1)
			}
			rule.Source, rule.Path = ExtractFromHeader, header
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// FormatExtractionText renders rules in the format read by
// ParseExtractionText.
func FormatExtractionText(rules []Extraction) string {
	var b strings.Builder
	for _, r := range rules {
		b.WriteString(r.Variable)
		b.WriteString(" = ")
		if r.Source == ExtractFromHeader {
			b.WriteString("header ")
		}
		b.WriteString(r.Path)
		b.WriteString("\n")
	}
	return b.String()
}

// cutWord reports whether s starts with word followed by whitespace (or is
// exactly word) and returns the trimmed rest.
func cutWord(s, word string) (string, bool) {
	if !strings.HasPrefix(strings.ToLower(s), word) {
		return "", false
	}
	rest := s[len(word):]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return "", false
	}
	return strings.TrimSpace(rest), true
}
