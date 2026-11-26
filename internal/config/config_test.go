package config_test

import (
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

// Savers rewrite only the keys they change: the user's comments, blank lines
// and key order survive.
func TestSavers_PreserveCommentsAndOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := `# My Quark settings

[http]
# the staging API is slow
timeout = "60s"
max_redirects = 3 # keep it short

[ui]
theme = "dark" # night mode
editor = "nano"
`
	require.NoError(t, os.WriteFile(path, []byte(original), 0o600))

	require.NoError(t, config.SaveTheme(dir, "light"))
	require.NoError(t, config.SaveLastRequestID(dir, "req-1"))
	require.NoError(t, config.SaveClientCertificates(dir, []config.ClientCertificate{
		{Host: "api.example", File: "client.p12", PasswordEnv: "CERT_PASS"},
	}))
	binds := keybindings.DefaultKeybindings()
	binds.Quit = "ctrl+q"
	require.NoError(t, config.SaveKeybindings(dir, binds))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	got := string(raw)
	assert.True(t, strings.HasPrefix(got, `# My Quark settings

[http]
# the staging API is slow
timeout = "60s"
max_redirects = 3 # keep it short

[ui]
theme = "light" # night mode
editor = "nano"
last_request_id = "req-1"

[[http.client_certificates]]
host = "api.example"
file = "client.p12"
password_env = "CERT_PASS"

[keybindings]
`), "unexpected config.toml:\n%s", got)
	assert.Contains(t, got, `quit = "ctrl+q"`)

	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "light", cfg.UI.Theme)
	assert.Equal(t, 3, cfg.HTTP.MaxRedirects)
	assert.Equal(t, "ctrl+q", cfg.Keybindings.Quit)
	require.Len(t, cfg.HTTP.ClientCertificates, 1)

	// Saving again with nothing changed leaves the file byte-for-byte alone.
	require.NoError(t, config.SaveKeybindings(dir, binds))
	require.NoError(t, config.SaveTheme(dir, "light"))
	again, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, got, string(again))

	// Removing the last certificate removes its table and nothing else.
	require.NoError(t, config.SaveClientCertificates(dir, nil))
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "[[http.client_certificates]]")
	assert.NotContains(t, string(raw), "\n\n\n")
	assert.Contains(t, string(raw), "max_redirects = 3 # keep it short\n")
}

// A layout the line editor cannot extend (a table made of dotted keys) still
// saves correctly, by re-encoding the whole document.
func TestSaveTheme_DottedKeyTableFallsBackToReencode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("ui.editor = \"nano\"\n"), 0o600))

	require.NoError(t, config.SaveTheme(dir, "light"))

	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "light", cfg.UI.Theme)
	assert.Equal(t, "nano", cfg.UI.Editor)
}
