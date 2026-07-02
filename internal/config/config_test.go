package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
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
}
