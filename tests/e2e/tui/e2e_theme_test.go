//go:build e2e

package tui_test

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/domain"
	"github.com/crazy-vedic/quark/internal/exec"
	"github.com/crazy-vedic/quark/internal/theme"
	"github.com/crazy-vedic/quark/internal/tui"
	"github.com/crazy-vedic/quark/internal/tuitest"
)

func trueColorThemeTest(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

func themeJourneyModel(t *testing.T, dir, name string) tui.Model {
	t.Helper()
	col := &domain.Collection{ID: "theme-col", Name: "API"}
	req := &domain.Request{
		ID: "theme-req", Name: "Get widgets", CollectionID: col.ID,
		Method: "GET", URL: "https://example.test/widgets",
	}
	st := setupStore(t, col)
	seedRequests(t, st, col.ID, req)
	cfg := config.Default(dir)
	cfg.UI.Theme = name
	m := tuitest.NewModelWithConfigDir(t, st, &mockExecutor{}, cfg, dir)
	m = resize(t, m, 120, 36)
	m = callUpdate(t, m, tui.CollectionsLoadedMsg([]*domain.Collection{col}))
	m = callUpdate(t, m, tui.RequestsLoadedMsg(col.ID, []*domain.Request{req}))
	m, _ = m.SelectRequest(req)
	m = callUpdate(t, m, tui.HttpResponseMsg(&exec.ExecuteResult{
		StatusCode: 200, Status: "200 OK", Body: []byte(`{"status":"ok","count":3}`),
	}))
	return m
}

func themeFrame(t *testing.T, m tui.Model) themeScreen {
	t.Helper()
	tuitest.AssertNoFrameOverflow(t, m)
	return renderThemeScreen(t, m.View(), m.Width(), m.Height())
}

// Lip Gloss converts RGB through its terminal color profile, which can round
// one channel by one. Compare the frame with the exact color it emits for a
// semantic role rather than with the unconverted TOML literal.
func terminalForeground(t *testing.T, color string) string {
	t.Helper()
	sample := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("X")
	return renderThemeScreen(t, sample, 1, 1).rows[0][0].fg
}

func terminalBackground(t *testing.T, color string) string {
	t.Helper()
	if color == "" {
		return ""
	}
	sample := lipgloss.NewStyle().Background(lipgloss.Color(color)).Render("X")
	return renderThemeScreen(t, sample, 1, 1).rows[0][0].bg
}

func TestE2E_ThemeColorsCoverEveryPaneAndCell(t *testing.T) {
	trueColorThemeTest(t)
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "themes"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "themes", "orchid.toml"), []byte(
		"extends = \"dark\"\n[colors]\nforeground = \"#f0e0d0\"\nbackground = \"#203040\"\naccent = \"#ff00aa\"\nmuted = \"#abc123\"\n",
	), 0o600))

	for _, name := range []string{"dark", "light", "orchid", "transparent"} {
		t.Run(name, func(t *testing.T) {
			selected, err := theme.Resolve(dir, name, lipgloss.HasDarkBackground())
			require.NoError(t, err)
			wantAccent := terminalForeground(t, selected.Accent)
			wantBackground := terminalBackground(t, selected.Background)
			m := themeJourneyModel(t, dir, name)
			for _, size := range []struct {
				name       string
				width      int
				height     int
				allVisible bool
			}{
				{"wide", 120, 36, true},
				{"narrow", 60, 30, true},
				{"tiny", 40, 20, false},
			} {
				t.Run(size.name, func(t *testing.T) {
					m = resize(t, m, size.width, size.height)
					for _, focus := range []string{"sidebar", "request", "response"} {
						t.Run(focus, func(t *testing.T) {
							// Focus changes border styles and, in tiny mode, which pane is visible.
							switch focus {
							case "sidebar":
								m = m.WithFocus(tui.SidebarPane)
							case "request":
								m = m.WithFocus(tui.RequestPane)
							case "response":
								m = m.WithFocus(tui.ResponsePane)
							}
							screen := themeFrame(t, m)
							screen.assertBackground(t, wantBackground, selected.Background != "")
							if size.allVisible || focus == "sidebar" {
								screen.assertTextColor(t, "Collections", wantAccent)
							}
							if size.allVisible || focus == "request" {
								screen.assertTextColor(t, "Request", wantAccent)
							}
							if size.allVisible || focus == "response" {
								screen.assertTextColor(t, "Response", wantAccent)
							}
							if size.allVisible {
								screen.assertTextColor(t, "https://example.test/widgets", terminalForeground(t, selected.Foreground))
								screen.assertTextColor(t, "[ Send ]", terminalForeground(t, selected.Success))
								screen.assertTextColor(t, "200 OK", terminalForeground(t, selected.Success))
							}
						})
					}
				})
			}
		})
	}
}

func TestE2E_ThemePreviewCancelApplyAndRestart(t *testing.T) {
	trueColorThemeTest(t)
	dir := t.TempDir()
	m := themeJourneyModel(t, dir, "dark")
	dark, err := theme.Resolve(dir, "dark", true)
	require.NoError(t, err)
	light, err := theme.Resolve(dir, "light", true)
	require.NoError(t, err)

	check := func(want theme.Theme) {
		t.Helper()
		screen := themeFrame(t, m)
		background := terminalBackground(t, want.Background)
		if m.Mode() == tui.SearchMode {
			screen.assertBackgroundOutsideReverse(t, background)
		} else {
			screen.assertBackground(t, background, true)
		}
		if m.Mode() == tui.NormalMode {
			accent := terminalForeground(t, want.Accent)
			screen.assertTextColor(t, "Collections", accent)
			screen.assertTextColor(t, "Request", accent)
			screen.assertTextColor(t, "Response", accent)
		}
	}
	check(dark)
	openLightPreview := func() {
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("> theme: light")})
		require.Equal(t, tui.SearchMode, m.Mode())
		assertViewContains(t, m, "Theme: light")
		check(light)
	}
	openLightPreview()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	check(dark)
	saved, err := config.Load(dir)
	require.NoError(t, err)
	require.Equal(t, "auto", saved.UI.Theme, "preview/cancel must not persist a selection")

	openLightPreview()
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, tui.NormalMode, m.Mode())
	check(light)
	saved, err = config.Load(dir)
	require.NoError(t, err)
	require.Equal(t, "light", saved.UI.Theme)

	// A fresh TUI process model must render the same saved theme in all panes.
	restarted := themeJourneyModel(t, dir, saved.UI.Theme)
	restartedScreen := themeFrame(t, restarted)
	restartedScreen.assertBackground(t, terminalBackground(t, light.Background), true)
	lightAccent := terminalForeground(t, light.Accent)
	restartedScreen.assertTextColor(t, "Collections", lightAccent)
	restartedScreen.assertTextColor(t, "Request", lightAccent)
	restartedScreen.assertTextColor(t, "Response", lightAccent)
}

func TestE2E_CustomThemeReloadKeepsEveryPaneInSync(t *testing.T) {
	trueColorThemeTest(t)
	dir := t.TempDir()
	themesDir := filepath.Join(dir, "themes")
	require.NoError(t, os.MkdirAll(themesDir, 0o700))
	path := filepath.Join(themesDir, "local.toml")
	writeTheme := func(accent, background string) {
		t.Helper()
		contents := "extends = \"dark\"\n[colors]\naccent = \"" + accent +
			"\"\nbackground = \"" + background + "\"\n"
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	}
	writeTheme("#1122ee", "#102030")
	m := themeJourneyModel(t, dir, "local")
	check := func(accent, background string) {
		t.Helper()
		screen := themeFrame(t, m)
		screen.assertBackground(t, terminalBackground(t, background), true)
		wantAccent := terminalForeground(t, accent)
		screen.assertTextColor(t, "Collections", wantAccent)
		screen.assertTextColor(t, "Request", wantAccent)
		screen.assertTextColor(t, "Response", wantAccent)
	}
	check("#1122ee", "#102030")

	writeTheme("#ef4080", "#f0ebe4")
	m = callUpdate(t, m, tui.ThemeWatchTickMsg())
	check("#ef4080", "#f0ebe4")

	require.NoError(t, os.WriteFile(path, []byte(
		"extends = \"dark\"\n[colors]\nbackground = \"broken\"\n",
	), 0o600))
	m = callUpdate(t, m, tui.ThemeWatchTickMsg())
	require.Contains(t, m.StatusErr(), "Theme reload")
	check("#ef4080", "#f0ebe4")

	writeTheme("#11bb88", "#182838")
	m = callUpdate(t, m, tui.ThemeWatchTickMsg())
	check("#11bb88", "#182838")
}

func TestE2E_ThemeOverlaysAndANSI256KeepUniformBackground(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })

	for _, name := range []string{"dark", "light"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			selected, err := theme.Resolve(dir, name, true)
			require.NoError(t, err)
			m := themeJourneyModel(t, dir, name)
			check := func() {
				t.Helper()
				screen := themeFrame(t, m)
				background := terminalBackground(t, selected.Background)
				if m.Mode() == tui.SearchMode {
					screen.assertBackgroundOutsideReverse(t, background)
				} else {
					screen.assertBackground(t, background, true)
				}
			}
			check()

			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
			require.Equal(t, tui.HelpMode, m.Mode())
			check()
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			require.Equal(t, tui.NormalMode, m.Mode())
			check()

			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
			require.Equal(t, tui.SearchMode, m.Mode())
			check()
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			m = callUpdate(t, m, tui.HttpResponseMsg(&exec.ExecuteResult{
				StatusCode: 200, Status: "200 OK", Body: viewerResponseBody(),
			}))
			m = openViewerE2E(t, m)
			require.Equal(t, tui.ViewerMode, m.Mode())
			check()
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
			m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("needle")})
			themeFrame(t, m).assertBackgroundOutsideReverse(t, terminalBackground(t, selected.Background))
		})
	}
}

func TestThemeScreenReaderTracksNestedResets(t *testing.T) {
	s := renderThemeScreen(t, "\x1b[48;2;1;2;3mA\x1b[0mB\x1b[48;2;1;2;3m\x1b[38;2;4;5;6m\x1b[7mC", 3, 1)
	require.Equal(t, "#010203", s.rows[0][0].bg)
	require.Empty(t, s.rows[0][1].bg)
	require.Equal(t, "#040506", s.rows[0][2].bg)
	require.Equal(t, "#010203", s.rows[0][2].fg)
	require.True(t, s.rows[0][2].reverse)
}
