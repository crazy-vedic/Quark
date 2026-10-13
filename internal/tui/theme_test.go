package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/crazy-vedic/quark/internal/config"
)

func selectThemeCommand(t *testing.T, m Model, name string) Model {
	t.Helper()
	m.mode = searchMode
	m.searchInput.SetValue("> theme")
	m.commands = m.filterCommandPalette("theme")
	for i, item := range m.commands {
		if item.ThemeName == name {
			m.searchCursor = i
			return m.previewCommandTheme()
		}
	}
	t.Fatalf("theme %q missing from command palette", name)
	return m
}

func TestThemePreviewCancelAndSave(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	cfg.UI.Theme = "dark"
	m := New(Deps{Config: cfg, ConfigDir: dir})
	original := m.theme.Foreground
	m = selectThemeCommand(t, m, "light")
	if m.theme.Name != "light" || m.theme.Foreground == original {
		t.Fatalf("preview did not change theme: %#v", m.theme)
	}
	m = m.closeSearch()
	if m.theme.Name != "dark" || m.theme.Foreground != original {
		t.Fatalf("cancel did not restore theme: %#v", m.theme)
	}
	m = selectThemeCommand(t, m, "light")
	updated, _ := m.executeSelectedCommand()
	m = updated.(Model)
	if m.theme.Name != "light" || m.mode != normalMode {
		t.Fatalf("selection not applied: %#v", m.theme)
	}
	saved, err := config.Load(dir)
	if err != nil || saved.UI.Theme != "light" {
		t.Fatalf("theme not persisted: %#v %v", saved.UI, err)
	}
}

func TestCustomThemeReloadKeepsLastGoodPalette(t *testing.T) {
	dir := t.TempDir()
	themesDir := filepath.Join(dir, "themes")
	if err := os.MkdirAll(themesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(themesDir, "local.toml")
	if err := os.WriteFile(path, []byte("extends = \"dark\"\n[colors]\naccent = \"#123456\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(dir)
	cfg.UI.Theme = "local"
	m := New(Deps{Config: cfg, ConfigDir: dir})
	if m.theme.Accent != "#123456" {
		t.Fatalf("initial custom theme: %#v", m.theme)
	}
	if err := os.WriteFile(path, []byte("extends = \"dark\"\n[colors]\naccent = \"#abcdef\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated, _ := m.Update(themeWatchTickMsg{})
	m = updated.(Model)
	if m.theme.Accent != "#abcdef" {
		t.Fatalf("theme did not reload: %#v", m.theme)
	}
	if err := os.WriteFile(path, []byte("extends = \"dark\"\n[colors]\naccent = \"invalid\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	updated, _ = m.Update(themeWatchTickMsg{})
	m = updated.(Model)
	if m.theme.Accent != "#abcdef" || m.statusErr == "" {
		t.Fatalf("invalid reload discarded last good palette: %#v, %q", m.theme, m.statusErr)
	}
}

func TestThemePreviewRestoresCustomSnapshot(t *testing.T) {
	dir := t.TempDir()
	themesDir := filepath.Join(dir, "themes")
	if err := os.MkdirAll(themesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(themesDir, "local.toml")
	if err := os.WriteFile(path, []byte("extends = \"dark\"\n[colors]\naccent = \"#123456\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default(dir)
	cfg.UI.Theme = "local"
	m := New(Deps{Config: cfg, ConfigDir: dir})
	m = selectThemeCommand(t, m, "light")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	m = m.closeSearch()
	if m.theme.Name != "local" || m.theme.Accent != "#123456" {
		t.Fatalf("cancel did not restore the preview snapshot: %#v", m.theme)
	}
}

func TestForcedLightThemePaintsReadableBackground(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	cfg := config.Default(t.TempDir())
	cfg.UI.Theme = "light"
	m := New(Deps{Config: cfg})
	m.width, m.height = 80, 24
	view := m.View()
	if !strings.Contains(view, ";48;2;246;248;250m") {
		t.Fatal("forced light theme did not paint its background")
	}
	if lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
		t.Fatalf("themed frame overflowed terminal: %dx%d", lipgloss.Width(view), lipgloss.Height(view))
	}
}
