package curl

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode"
)

// tokenize splits a curl command string into tokens, respecting single and
// double quotes and backslash escapes. Removes the leading "curl" token.
func tokenize(cmd string) ([]string, error) {
	if !strings.HasPrefix(strings.TrimSpace(cmd), "curl") {
		return nil, fmt.Errorf("curl: input does not start with 'curl'")
	}

	var tokens []string
	var cur strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for _, ch := range cmd {
		if escaped {
			cur.WriteRune(ch)
			escaped = false
			continue
		}
		switch {
		case ch == '\\' && !inSingle:
			escaped = true
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
		case ch == '"' && !inSingle:
			inDouble = !inDouble
		case unicode.IsSpace(ch) && !inSingle && !inDouble:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(ch)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}

	if inSingle || inDouble {
		return nil, fmt.Errorf("curl: unterminated quote in command")
	}

	// Strip leading "curl" token.
	if len(tokens) > 0 && tokens[0] == "curl" {
		tokens = tokens[1:]
	}

	return tokens, nil
}

// parseTokens processes the token stream into an ImportResult.
func parseTokens(tokens []string) (*ImportResult, error) {
	result := &ImportResult{
		Method:  "GET",
		Headers: make(map[string]string),
	}

	hasBody := false
	explicitMethod := false

	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]

		switch {
		// --- URL (positional, no dash prefix) ---
		case !strings.HasPrefix(tok, "-") && result.URL == "":
			result.URL = tok

		// --- Method ---
		case tok == "-X" || tok == "--request":
			if i+1 < len(tokens) {
				i++
				result.Method = strings.ToUpper(tokens[i])
				explicitMethod = true
			}

		// --- Headers ---
		case tok == "-H" || tok == "--header":
			if i+1 < len(tokens) {
				i++
				header := tokens[i]
				if idx := strings.Index(header, ":"); idx > 0 {
					key := strings.TrimSpace(header[:idx])
					val := strings.TrimSpace(header[idx+1:])
					result.Headers[key] = val
					classifyHeader(key, result)
					// Check header value for shell expansion — same risk as body.
					if containsShellExpansion(val) {
						result.Warnings = append(
							result.Warnings,
							"shell expansion detected in header value",
						)
						upgradeSecurityTo(result, Dangerous)
					}
				}
			}

		// --- Body (various forms) ---
		case tok == "-d" || tok == "--data" || tok == "--data-raw":
			if i+1 < len(tokens) {
				i++
				body := tokens[i]
				result.Body = body
				hasBody = true
				switch {
				case isFileRef(body):
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("@filename detected: %s", body[1:]))
					result.Security = Dangerous
				case containsShellExpansion(body):
					result.Warnings = append(result.Warnings, "shell expansion detected in body")
					result.Security = Dangerous
				default:
					upgradeSecurityTo(result, Review)
				}
			}

		case tok == "--data-binary":
			if i+1 < len(tokens) {
				i++
				body := tokens[i]
				switch {
				case body == "@-":
					// stdin reference — treat as file ref
					result.Body = body
					result.Warnings = append(result.Warnings, "@filename detected: - (stdin)")
					result.Security = Dangerous
				case isFileRef(body):
					result.Body = body
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("@filename detected: %s", body[1:]))
					result.Security = Dangerous
				default:
					result.Body = body
					hasBody = true
					upgradeSecurityTo(result, Review)
				}
			}

		case tok == "--data-urlencode":
			if i+1 < len(tokens) {
				i++
				result.Body = tokens[i]
				hasBody = true
				upgradeSecurityTo(result, Review)
			}

		// --- Basic auth ---
		case tok == "-u" || tok == "--user":
			if i+1 < len(tokens) {
				i++
				// Convert to Authorization: Basic header per RFC 7617.
				// curl expects "user:password"; we base64-encode it so the stored
				// request authenticates correctly when replayed.
				result.Headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString(
					[]byte(tokens[i]),
				)
				upgradeSecurityTo(result, Review)
			}

		// --- Cookie ---
		case tok == "-b" || tok == "--cookie":
			if i+1 < len(tokens) {
				i++
				result.Headers["Cookie"] = tokens[i]
				upgradeSecurityTo(result, Review)
			}

		// --- Follow redirects (safe) ---
		case tok == "-L" || tok == "--location":
			// no-op for MVP; safe flag

			// --- Ignore other flags ---
		}
	}

	// Infer POST when body is present and no explicit method was set.
	if hasBody && result.Method == "GET" && !explicitMethod {
		result.Method = "POST"
	}

	if result.URL == "" {
		return nil, fmt.Errorf("curl: no URL found in command")
	}

	return result, nil
}

// classifyHeader upgrades security based on sensitive header names.
func classifyHeader(key string, result *ImportResult) {
	lower := strings.ToLower(key)
	if lower == "authorization" || lower == "cookie" || lower == "x-api-key" {
		upgradeSecurityTo(result, Review)
	}
}

// upgradeSecurityTo only increases the security level, never decreases.
func upgradeSecurityTo(result *ImportResult, level SecurityLevel) {
	if level > result.Security {
		result.Security = level
	}
}

// isFileRef reports whether s is a @filename body reference.
func isFileRef(s string) bool {
	return strings.HasPrefix(s, "@") && s != "@-"
}

// containsShellExpansion reports whether s contains shell expansion patterns.
func containsShellExpansion(s string) bool {
	return strings.Contains(s, "$(") || strings.Contains(s, "`")
}
