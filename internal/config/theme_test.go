package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveThemePreservesOtherConfigAndRejectsMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := []byte("[ui]\ntheme = \"dark\"\neditor = \"nano\"\n[http]\nfollow_redirects = false\n[[http.client_certificates]]\nhost = \"api.example.com\"\nfile = \"client.p12\"\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveTheme(dir, "light"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Theme != "light" || cfg.UI.Editor != "nano" || cfg.HTTP.FollowRedirects || len(cfg.HTTP.ClientCertificates) != 1 || cfg.HTTP.ClientCertificates[0].Host != "api.example.com" {
		t.Fatalf("other settings changed: %#v", cfg)
	}
	if err := os.WriteFile(path, []byte("[ui\ntheme = \"dark\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveTheme(dir, "light"); err == nil {
		t.Fatal("malformed config was overwritten")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "[ui\ntheme = \"dark\"\n" {
		t.Fatalf("malformed config changed: %q %v", raw, err)
	}
}
