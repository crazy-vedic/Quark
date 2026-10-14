//go:build e2e

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLI_ThemeListSetAndValidate(t *testing.T) {
	t.Setenv("QUARK_THEME", "")
	home := t.TempDir()
	configDir := filepath.Join(home, ".quark")
	require.NoError(t, os.MkdirAll(configDir, 0o700))
	configPath := filepath.Join(configDir, "config.toml")
	require.NoError(
		t,
		os.WriteFile(configPath, []byte("[ui]\ntheme = \"dark\"\neditor = \"nano\"\n"), 0o600),
	)

	out, stderr, code := runQuarkWithHome(t, home, "theme", "list")
	require.Equal(t, 0, code, "list: %s", stderr)
	assert.Contains(t, out, "* dark\n")
	assert.Contains(t, out, "  light\n")

	out, stderr, code = runQuarkWithHome(t, home, "theme", "set", "light")
	require.Equal(t, 0, code, "set: %s", stderr)
	assert.Contains(t, out, "Theme saved: light")
	raw, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `theme = "light"`)
	assert.Contains(t, string(raw), `editor = "nano"`)

	out, stderr, code = runQuarkWithHome(t, home, "theme", "list")
	require.Equal(t, 0, code, "list after set: %s", stderr)
	assert.Contains(t, out, "* light\n")

	out, stderr, code = runQuarkWithHome(t, home, "theme", "validate")
	require.Equal(t, 0, code, "validate: %s", stderr)
	assert.Contains(t, out, "Theme light is valid (light palette)")
	_, err = os.Stat(filepath.Join(configDir, "quark.db"))
	assert.ErrorIs(t, err, os.ErrNotExist, "theme commands should not open the request database")
}

func TestCLI_ThemeCustomFileAndSelectionPrecedence(t *testing.T) {
	t.Setenv("QUARK_THEME", "")
	home := t.TempDir()
	configDir := filepath.Join(home, ".quark")
	themesDir := filepath.Join(configDir, "themes")
	require.NoError(t, os.MkdirAll(themesDir, 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(themesDir, "ocean.toml"),
		[]byte("extends = \"light\"\n[colors]\naccent = \"#123abc\"\nsyntax = \"github\"\n"),
		0o600,
	))
	require.NoError(t, os.WriteFile(filepath.Join(themesDir, "ignored.txt"), []byte(""), 0o600))

	out, stderr, code := runQuarkWithHome(t, home, "theme", "list")
	require.Equal(t, 0, code, "list custom theme: %s", stderr)
	assert.Contains(t, out, "  ocean\n")
	assert.NotContains(t, out, "ignored")

	out, stderr, code = runQuarkWithHome(t, home, "theme", "validate", "ocean")
	require.Equal(t, 0, code, "validate custom theme: %s", stderr)
	assert.Contains(t, out, "Theme ocean is valid (light palette)")

	out, stderr, code = runQuarkWithHome(t, home, "theme", "set", "ocean")
	require.Equal(t, 0, code, "set custom theme: %s", stderr)
	assert.Contains(t, out, "Theme saved: ocean")
	out, stderr, code = runQuarkWithHome(t, home, "theme", "list")
	require.Equal(t, 0, code, "list selected theme: %s", stderr)
	assert.Contains(t, out, "* ocean\n")

	out, stderr, code = runQuarkWithHomeEnv(
		t,
		home,
		[]string{"QUARK_THEME=dark"},
		"theme",
		"validate",
	)
	require.Equal(t, 0, code, "environment override: %s", stderr)
	assert.Contains(t, out, "Theme dark is valid (dark palette)")

	out, stderr, code = runQuarkWithHomeEnv(
		t, home, []string{"QUARK_THEME=dark"}, "--theme", "light", "theme", "validate",
	)
	require.Equal(t, 0, code, "flag override: %s", stderr)
	assert.Contains(t, out, "Theme light is valid (light palette)")

	out, stderr, code = runQuarkWithHomeEnv(
		t, home, []string{"QUARK_THEME=dark"}, "--theme", "light", "theme", "validate", "ocean",
	)
	require.Equal(t, 0, code, "explicit name: %s", stderr)
	assert.Contains(t, out, "Theme ocean is valid (light palette)")

	raw, err := os.ReadFile(filepath.Join(configDir, "config.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `theme = "ocean"`, "session overrides must not persist")
}

func TestCLI_ThemeRejectsInvalidSelectionWithoutOverwritingConfig(t *testing.T) {
	t.Setenv("QUARK_THEME", "")
	home := t.TempDir()
	configDir := filepath.Join(home, ".quark")
	themesDir := filepath.Join(configDir, "themes")
	require.NoError(t, os.MkdirAll(themesDir, 0o700))
	configPath := filepath.Join(configDir, "config.toml")
	original := []byte("[ui]\ntheme = \"dark\"\neditor = \"nano\"\n")
	require.NoError(t, os.WriteFile(configPath, original, 0o600))
	require.NoError(t, os.WriteFile(
		filepath.Join(themesDir, "broken.toml"),
		[]byte("extends = \"dark\"\n[colors]\naccent = \"red\"\n"),
		0o600,
	))

	for _, name := range []string{"broken", "../secret", "missing"} {
		t.Run(name, func(t *testing.T) {
			_, stderr, code := runQuarkWithHome(t, home, "theme", "set", name)
			require.NotZero(t, code, "invalid theme %q was accepted", name)
			assert.NotEmpty(t, stderr)
			raw, err := os.ReadFile(configPath)
			require.NoError(t, err)
			assert.Equal(t, original, raw)
		})
	}

	_, stderr, code := runQuarkWithHome(t, home, "theme", "validate", "broken")
	require.NotZero(t, code)
	assert.Contains(t, stderr, "colors.accent")

	_, stderr, code = runQuarkWithHome(t, home, "--theme", "missing", "tui")
	require.NotZero(t, code)
	assert.True(t, strings.Contains(stderr, `load theme "missing"`), "startup error: %s", stderr)

	_, stderr, code = runQuarkWithHomeEnv(t, home, []string{"QUARK_THEME=missing"}, "tui")
	require.NotZero(t, code)
	assert.Contains(t, stderr, `load theme "missing"`)

	malformed := []byte("[ui\ntheme = \"dark\"\n")
	require.NoError(t, os.WriteFile(configPath, malformed, 0o600))
	_, stderr, code = runQuarkWithHome(t, home, "theme", "set", "light")
	require.NotZero(t, code)
	assert.Contains(t, stderr, "decode config.toml")
	raw, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, malformed, raw)
}
