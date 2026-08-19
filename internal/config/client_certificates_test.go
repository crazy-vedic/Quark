package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
)

func TestSaveClientCertificatesPreservesConfig(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[ui]\ntheme = \"dark\"\n"), 0o600))
	certs := []config.ClientCertificate{{
		Host: "access.dev.example.com", File: `C:\certs\dev.pem`, Type: "PEM",
		KeyFile: `C:\certs\dev-key.pem`, CAFile: `C:\certs\root.pem`, PasswordEnv: "QUARK_CERT_PASSWORD", //nolint:gosec // fixture is an environment-variable name.
	}}

	require.NoError(t, config.SaveClientCertificates(dir, certs))
	loaded, err := config.Load(dir)
	require.NoError(t, err)
	require.Equal(t, "dark", loaded.UI.Theme)
	require.Equal(t, certs, loaded.HTTP.ClientCertificates)
}

func TestSaveClientCertificatesRejectsMalformedConfigWithoutRewrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := []byte("[ui\ntheme = \"dark\"\n")
	require.NoError(t, os.WriteFile(path, original, 0o600))
	err := config.SaveClientCertificates(dir, []config.ClientCertificate{{Host: "example.com", File: "client.pem"}})
	require.Error(t, err)
	actual, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.Equal(t, original, actual)
}
