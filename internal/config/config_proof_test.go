package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/keybindings"
)

// Issue 1: saving keybindings must never overwrite a config file it could not parse.
// Expected to FAIL on master: invalid TOML is silently replaced with an empty doc.
func TestProof_SaveKeybindings_InvalidConfigIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "[ui]\ntheme = \"dark\"\n[ui\n" // unterminated table header
	require.NoError(t, os.WriteFile(path, []byte(original), 0o600))

	err := config.SaveKeybindings(dir, keybindings.DefaultKeybindings())

	require.Error(t, err, "saving must fail when the existing config cannot be parsed")
	got, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, original, string(got), "existing config must be left untouched")
}

// Guard test (expected to PASS): unrelated sections survive a keybindings save.
func TestProof_SaveKeybindings_PreservesOtherSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := "[http]\nmax_redirects = 3\n\n[ui]\ntheme = \"dark\"\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o600))

	require.NoError(t, config.SaveKeybindings(dir, keybindings.DefaultKeybindings()))

	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, 3, cfg.HTTP.MaxRedirects)
	assert.Equal(t, "dark", cfg.UI.Theme)
}
