package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/keybindings"
)

func TestDefault_HasSensibleValues(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)

	assert.Equal(t, "auto", cfg.UI.Theme)
	assert.Equal(t, "GET", cfg.UI.DefaultMethod)
	assert.Equal(t, 30*time.Second, cfg.Timeout())
	assert.True(t, cfg.HTTP.FollowRedirects)
	assert.Equal(t, 10, cfg.Backup.KeepLast)
	assert.True(t, cfg.Backup.AutoBackup)
	assert.Equal(t, filepath.Join(dir, "quark.log"), cfg.Logging.File)
	assert.True(t, cfg.Format.JSONOnSave, "json_on_save defaults to true")
}

func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	dir := t.TempDir()

	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, cfg.Timeout())
}

func TestLoad_ValidFile_OverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o700))

	content := `
[ui]
theme = "dark"
default_method = "POST"

[http]
timeout = "60s"

[backup]
keep_last = 5
`
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "config.toml"),
		[]byte(content), 0o600,
	))

	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "dark", cfg.UI.Theme)
	assert.Equal(t, "POST", cfg.UI.DefaultMethod)
	assert.Equal(t, 60*time.Second, cfg.Timeout())
	assert.Equal(t, 5, cfg.Backup.KeepLast)
}

func TestLastRequestID_RoundTripsWithoutReplacingConfig(t *testing.T) {
	dir := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[ui]\ntheme = \"dark\"\n"), 0o600),
	)

	require.NoError(t, config.SaveLastRequestID(dir, "request-123"))
	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "dark", cfg.UI.Theme)
	assert.Equal(t, "request-123", cfg.UI.LastRequestID)

	require.NoError(t, config.SaveLastRequestID(dir, ""))
	cfg, err = config.Load(dir)
	require.NoError(t, err)
	assert.Empty(t, cfg.UI.LastRequestID)
}

func TestLoad_PartialFile_FillsZerosWithDefaults(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o700))

	// Only override theme; everything else should be default.
	content := `
[ui]
theme = "transparent"
`
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "config.toml"),
		[]byte(content), 0o600,
	))

	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "transparent", cfg.UI.Theme)
	assert.Equal(t, 30*time.Second, cfg.Timeout(), "timeout must default when omitted")
	assert.Equal(t, 10, cfg.Backup.KeepLast, "keep_last must default when omitted")
	assert.True(t, cfg.Format.JSONOnSave, "json_on_save must default to true when omitted")
}

func TestLoad_FormatConfig_JSONOnSaveCanBeDisabled(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(dir, 0o700))

	content := `
[format]
json_on_save = false
`
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "config.toml"),
		[]byte(content), 0o600,
	))

	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.False(t, cfg.Format.JSONOnSave)
}

func TestLoad_MigratesLegacyCurlImportBinding(t *testing.T) {
	dir := t.TempDir()
	content := "[keybindings]\nimport_curl = \"I\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o600))
	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "i", cfg.Keybindings.ImportCurl)
	assert.Equal(t, "I", cfg.Keybindings.SidebarImportPostman)

	content = "[keybindings]\nimport_curl = \"I\"\nsidebar_import_postman = \"P\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o600))
	cfg, err = config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "I", cfg.Keybindings.ImportCurl)
	assert.Equal(t, "P", cfg.Keybindings.SidebarImportPostman)
}

// Issue 1: saving keybindings must never overwrite a config file it could not parse.
// config.toml can hold certificate passwords. Whichever saver creates it, the
// file must be owner-only.
func TestSavers_CreateConfigOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	savers := map[string]func(dir string) error{
		"SaveKeybindings": func(dir string) error {
			return config.SaveKeybindings(dir, keybindings.DefaultKeybindings())
		},
		"SaveLastRequestID": func(dir string) error {
			return config.SaveLastRequestID(dir, "req-1")
		},
		"SaveTheme": func(dir string) error {
			return config.SaveTheme(dir, "dark")
		},
		"SaveClientCertificates": func(dir string) error {
			return config.SaveClientCertificates(dir, []config.ClientCertificate{
				{Host: "api.example", File: "/tmp/c.p12", Password: "secret"},
			})
		},
	}
	for name, save := range savers {
		dir := t.TempDir()
		require.NoError(t, save(dir), name)

		info, err := os.Stat(filepath.Join(dir, "config.toml"))
		require.NoError(t, err, name)
		assert.Equal(
			t,
			os.FileMode(0o600),
			info.Mode().Perm(),
			"%s must create config.toml 0600",
			name,
		)

		entries, err := os.ReadDir(dir)
		require.NoError(t, err, name)
		assert.Len(t, entries, 1, "%s must not leave temp files behind", name)
	}
}

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

// A parse error must name the file, so the user knows what to fix.
func TestLoad_ParseErrorNamesTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("[ui\ntheme = \"dark\"\n"), 0o600))

	_, err := config.Load(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), path)
}

// writeConfig writes content as config.toml in a new directory and returns it.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte(content), 0o600))
	return dir
}

func TestValidate_RejectsValuesThatCannotWork(t *testing.T) {
	tests := []struct {
		name, content, want string
	}{
		{"negative http timeout", "[http]\ntimeout = \"-1s\"\n", "http.timeout"},
		{"negative max redirects", "[http]\nmax_redirects = -1\n", "http.max_redirects"},
		{"negative keep_last", "[backup]\nkeep_last = -1\n", "backup.keep_last"},
		{"zero check interval", "[updates]\ncheck_interval = \"0s\"\n", "updates.check_interval"},
		{"negative retry", "[updates]\nretry_after_failure = \"-1m\"\n",
			"updates.retry_after_failure"},
		{"zero updates timeout", "[updates]\ntimeout = \"0s\"\n", "updates.timeout"},
		{"empty default method", "[ui]\ndefault_method = \"\"\n", "ui.default_method"},
		{"method with space", "[ui]\ndefault_method = \"GE T\"\n", "ui.default_method"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := config.Load(writeConfig(t, tt.content))
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestValidate_AcceptsDefaultsAndZeroValues(t *testing.T) {
	cfg := config.Default(t.TempDir())
	require.NoError(t, cfg.Validate())

	cfg.HTTP.MaxRedirects = 0
	cfg.Backup.KeepLast = 0
	require.NoError(t, cfg.Validate(), "max_redirects = 0 and keep_last = 0 are meaningful")
}

func TestLoad_ZeroHTTPTimeoutMeansNoTimeout(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"),
		[]byte("[http]\ntimeout = \"0s\"\n"), 0o600))

	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), cfg.Timeout())
}

func TestLoad_InvalidValueFailsAndNamesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path,
		[]byte("[updates]\ncheck_interval = \"0s\"\n"), 0o600))

	_, err := config.Load(dir)
	require.Error(t, err)
	assert.ErrorContains(t, err, path)
	assert.ErrorContains(t, err, "updates.check_interval")
}

func TestLoadWithWarnings_ReportsUnknownKeysAndKeepsValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(`
[ui]
theme = "dark"
themee = "light"

[http]
timout = "5s"
`), 0o600))

	cfg, warnings, err := config.LoadWithWarnings(dir)
	require.NoError(t, err)
	assert.Equal(t, "dark", cfg.UI.Theme)
	assert.Equal(t, 30*time.Second, cfg.Timeout(), "misspelled key is ignored")
	require.Len(t, warnings, 2)
	joined := strings.Join(warnings, "\n")
	assert.Contains(t, joined, `unknown setting "ui.themee"`)
	assert.Contains(t, joined, `unknown setting "http.timout"`)
	assert.Contains(t, joined, path)

	var out bytes.Buffer
	config.WriteWarnings(&out, warnings)
	assert.Equal(t, 2, strings.Count(out.String(), "quark: warning: "))
}

func TestLoadWithWarnings_MissingFileHasNoWarnings(t *testing.T) {
	_, warnings, err := config.LoadWithWarnings(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, warnings)
}
