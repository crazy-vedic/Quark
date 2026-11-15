package format

import (
	"bytes"
	"encoding/json"
	"strings"
)

// FormatJSONIfValid attempts to pretty-print the input as JSON with consistent
// indentation. If the input is not valid JSON, it returns the input unchanged
// with changed=false. Empty or whitespace-only input is returned unchanged.
// The function preserves the existing indentation style (2 spaces, 4 spaces, tabs)
// when detectable, defaulting to 2 spaces.
func FormatJSONIfValid(body string) (formatted string, changed bool) {
	// Empty or whitespace-only input is unchanged.
	if strings.TrimSpace(body) == "" {
		return body, false
	}

	// Check if it's valid JSON.
	if !json.Valid([]byte(body)) {
		return body, false
	}

	// Detect the indent style from the input.
	indent := detectIndent(body)

	// Decode without type conversion; use json.Number to preserve precision.
	var v any
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&v); err != nil {
		return body, false
	}

	// Re-encode with the detected (or default) indent.
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", indent)
	if err := encoder.Encode(v); err != nil {
		return body, false
	}

	formatted = strings.TrimSuffix(buf.String(), "\n")
	return formatted, formatted != body
}

// detectIndent scans the input for the first indented line and returns the
// whitespace prefix. If no indented line is found, returns 2 spaces as default.
func detectIndent(body string) string {
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			// Found an indented line; extract the indent.
			for i := 0; i < len(line); i++ {
				if line[i] != ' ' && line[i] != '\t' {
					return line[:i]
				}
			}
		}
	}
	return "  " // default: 2 spaces
}
