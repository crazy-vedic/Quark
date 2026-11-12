//go:build e2e

package e2e

import "strings"

// EscapePathForJSON ensures a file path is properly escaped for use in JSON strings.
// On Windows, this converts backslashes to forward slashes; on other systems it's a no-op.
// This is needed because filepath.Join produces platform-specific paths (with backslashes
// on Windows), which must be properly escaped when embedded in JSON or shell commands.
func EscapePathForJSON(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}
