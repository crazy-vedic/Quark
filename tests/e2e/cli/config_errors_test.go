//go:build e2e

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A malformed config.toml must stop the command with the parse error. Running
// on silently with defaults hides the user's settings and can make the next
// save overwrite them.
func TestCLI_MalformedConfigFailsCommand(t *testing.T) {
	home, _ := setupHomeWithCollection(t)
	configPath := filepath.Join(home, ".quark", "config.toml")
	const broken = "[ui\ntheme = \"dark\"\n" // unterminated table header
	require.NoError(t, os.WriteFile(configPath, []byte(broken), 0o600))

	_, stderr, code := runQuarkWithHome(t, home, "collection", "list")
	assert.NotEqual(t, 0, code, "a malformed config must fail the command")
	assert.Contains(t, stderr, "config.toml")

	got, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, broken, string(got), "the broken config must not be rewritten")
}
