// Package highlight provides syntax highlighting for response bodies using Chroma.
// It is a thin wrapper around the Chroma library with safe ANSI sanitization.
package highlight

import (
	"bytes"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// ThemeMap maps Quark UI theme names to Chroma style names.
var themeMap = map[string]string{
	"auto":        "monokai",
	"dark":        "monokai",
	"light":       "github",
	"transparent": "monokai",
}

// JSON highlights a JSON string with terminal ANSI colors.
// Returns the original string if highlighting fails (graceful degradation).
func JSON(input string, theme string) string {
	styleName, ok := themeMap[theme]
	if !ok {
		styleName = "monokai"
	}

	lexer := lexers.Get("json")
	if lexer == nil {
		return input
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		return input
	}
	style := styles.Get(styleName)
	if style == nil {
		style = styles.Fallback
	}

	iterator, err := lexer.Tokenise(nil, input)
	if err != nil {
		return input
	}

	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return input
	}

	// Strip any dangerous ANSI sequences (OSC 52, cursor movement, etc.)
	// while preserving the safe color/formatting codes from Chroma.
	return safeStrip(buf.String())
}

// safeStrip removes dangerous ANSI escape sequences (OSC 52 clipboard hijack,
// screen clear, cursor movement) while preserving harmless color/formatting codes (SGR 'm').
func safeStrip(s string) string {
	var result bytes.Buffer
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) {
			// Check what follows the escape character
			next := s[i+1]

			if next == ']' {
				// OSC sequence: strip until \x07 or \x1b\\
				j := i + 2
				for j < len(s) {
					if s[j] == '\x07' {
						j++
						break
					}
					if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
				i = j
				continue
			}

			if next == '[' {
				// CSI sequence: parse until final byte (0x40-0x7E)
				j := i + 2
				// Skip parameter bytes (0x30-0x3F: digits, semicolons, colons, <, =, >, ?)
				for j < len(s) && s[j] >= '0' && s[j] <= '?' {
					j++
				}
				// Skip intermediate bytes (0x20-0x2F: space, !, ", #, $, %, &, ', (, ), *, +, ,, -, ., /)
				for j < len(s) && s[j] >= ' ' && s[j] <= '/' {
					j++
				}
				// Final byte: 0x40-0x7E (@A-Z [ \ ] ^ _ ` a-z { | } ~)
				if j < len(s) && s[j] >= '@' && s[j] <= '~' {
					final := s[j]
					if final == 'm' {
						// SGR (color/style) sequence: keep it
						result.WriteString(s[i : j+1])
					}
					// else: strip it (dangerous CSI)
					j++
					i = j
					continue
				}
				// Incomplete CSI: treat as regular text
			}
			// Other escape sequences (single-byte, multi-byte, etc.): strip ESC and the
			// following command byte(s). Covers 0x20-0x3F (intermediate), 0x40-0x5F
			// (final), and 0x60-0x7E (independent control functions).
			i++ // skip the \x1b
			// Skip intermediate bytes (0x20-0x2F) and final byte (0x30-0x7E)
			for i < len(s) && s[i] >= ' ' && s[i] <= '~' {
				i++
			}
			continue
		}
		result.WriteByte(s[i])
		i++
	}
	return result.String()
}
