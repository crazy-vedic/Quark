package format

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// FormatJSONIfValid attempts to pretty-print the input as JSON with consistent
// indentation while preserving key order. If the input is not valid JSON, it returns
// the input unchanged with changed=false. Empty or whitespace-only input is returned
// unchanged. The function preserves the existing indentation style (2 spaces, 4 spaces,
// tabs) when detectable, defaulting to 2 spaces.
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

	// Use token-based formatting to preserve key order
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber() // Preserve number precision
	var buf bytes.Buffer
	err := formatTokens(decoder, &buf, indent, 0)
	if err != nil {
		return body, false
	}

	formatted = buf.String()
	return formatted, formatted != body
}

// formatTokens recursively formats JSON tokens, preserving key order.
// It writes directly to the buffer without using the standard encoder,
// ensuring that object keys maintain their original order.
func formatTokens(decoder *json.Decoder, buf *bytes.Buffer, indent string, depth int) error {
	token, err := decoder.Token()
	if err != nil && err != io.EOF {
		return err
	}

	if token == nil && err == io.EOF {
		return nil
	}

	switch t := token.(type) {
	case json.Delim:
		switch t {
		case '{':
			buf.WriteString("{")
			depth++
			first := true
			for decoder.More() {
				// Read the key
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return err
				}

				if !first {
					buf.WriteString(",\n")
				} else {
					buf.WriteString("\n")
				}
				first = false

				// Write indentation and key
				buf.WriteString(strings.Repeat(indent, depth))
				writeJSONString(buf, key)
				buf.WriteString(": ")

				// Recursively format the value
				err = formatTokens(decoder, buf, indent, depth)
				if err != nil {
					return err
				}
			}
			// Consume the closing }
			_, err := decoder.Token()
			if err != nil {
				return err
			}
			depth--
			if !first {
				buf.WriteString("\n")
				buf.WriteString(strings.Repeat(indent, depth))
			}
			buf.WriteString("}")

		case '[':
			buf.WriteString("[")
			depth++
			first := true
			for decoder.More() {
				if !first {
					buf.WriteString(",\n")
				} else {
					buf.WriteString("\n")
				}
				first = false
				buf.WriteString(strings.Repeat(indent, depth))
				err := formatTokens(decoder, buf, indent, depth)
				if err != nil {
					return err
				}
			}
			// Consume the closing ]
			_, err := decoder.Token()
			if err != nil {
				return err
			}
			depth--
			if !first {
				buf.WriteString("\n")
				buf.WriteString(strings.Repeat(indent, depth))
			}
			buf.WriteString("]")
		}

	case json.Number:
		// Preserve the original number representation
		buf.WriteString(t.String())

	case string:
		writeJSONString(buf, t)

	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}

	case nil:
		buf.WriteString("null")
	}

	return nil
}

// writeJSONString writes a string to buf as valid JSON, handling escapes properly.
func writeJSONString(buf *bytes.Buffer, s string) {
	buf.WriteRune('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if r < 0x20 {
				// Control character - escape as unicode
				fmt.Fprintf(buf, `\u%04x`, r)
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteRune('"')
}

// detectIndent scans the input for the first indented line after a structural
// character ('{' or '[') and returns the whitespace prefix. If no indented line
// is found, returns 2 spaces as default.
func detectIndent(body string) string {
	runes := []rune(body)
	inString := false
	escaped := false

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		// Handle string escaping
		if escaped {
			escaped = false
			continue
		}

		if r == '\\' && inString {
			escaped = true
			continue
		}

		if r == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		// Look for structural characters
		if r == '{' || r == '[' {
			// Scan forward for the next line
			for j := i + 1; j < len(runes); j++ {
				if runes[j] == '\n' {
					// Found newline after structural char
					// Check the next line for indentation
					k := j + 1
					indentStart := k
					for k < len(runes) && (runes[k] == ' ' || runes[k] == '\t') {
						k++
					}
					// If we found indented content (not just whitespace, and not
					// unindented at column 0), return the indent. An empty indent
					// means "not found", so fall back to the default.
					if k > indentStart && k < len(runes) && runes[k] != '\n' && runes[k] != '\r' {
						return string(runes[indentStart:k])
					}
					break
				}
			}
		}
	}

	return "  " // default: 2 spaces
}
