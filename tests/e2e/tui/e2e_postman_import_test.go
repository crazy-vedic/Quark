//go:build e2e

package tui_test

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/crazy-vedic/quark/internal/config"
	"github.com/crazy-vedic/quark/internal/keybindings"
	"github.com/crazy-vedic/quark/internal/postmanimport"
	"github.com/crazy-vedic/quark/internal/theme"
	"github.com/crazy-vedic/quark/internal/tui"
)

type postmanImportFake struct {
	path   string
	result postmanimport.Result
	err    error
}

func (f *postmanImportFake) ImportPostman(
	_ context.Context,
	path string,
) (postmanimport.Result, error) {
	f.path = path
	return f.result, f.err
}

var teamAPIImport = postmanimport.Result{Collections: []postmanimport.CollectionResult{
	{Name: "Team API", Outcome: postmanimport.OutcomeCreated, Imported: 1, Total: 1},
}}

func TestE2E_PostmanImportModalFlow(t *testing.T) {
	trueColorThemeTest(t)
	importer := &postmanImportFake{result: teamAPIImport}
	dir := t.TempDir()
	cfg := config.Default(dir)
	cfg.UI.Theme = "light"
	selected, err := theme.Resolve(dir, cfg.UI.Theme, true)
	require.NoError(t, err)
	m := tui.New(tui.Deps{
		PostmanImporter: importer,
		Config:          cfg,
		Resolver:        keybindings.NewResolver(cfg.Keybindings),
		Ctx:             context.Background(),
	}).WithFocus(tui.SidebarPane)
	m = resize(t, m, 100, 30)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	assertViewContains(t, m, "Import Postman collection")
	assertViewContains(t, m, "Collection file or export directory")
	screen := themeFrame(t, m)
	screen.assertBackgroundOutsideReverse(t, terminalBackground(t, selected.Background))
	screen.assertTextColor(t, "Import Postman collection", terminalForeground(t, selected.Accent))

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("collection.json")})
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assertViewContains(t, m, "Importing")
	m = executeCmdUpdate(t, m, cmd)
	require.Equal(t, "collection.json", importer.path)
	assertViewContains(t, m, `Imported 1/1 requests into "Team API"`)

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assertViewNotContains(t, m, "Import Postman collection")
}

func TestE2E_PostmanImportFailureAndPaneScope(t *testing.T) {
	importer := &postmanImportFake{err: errors.New("invalid JSON")}
	cfg := config.Default(t.TempDir())
	m := tui.New(tui.Deps{
		PostmanImporter: importer,
		Config:          cfg,
		Resolver:        keybindings.NewResolver(cfg.Keybindings),
		Ctx:             context.Background(),
	}).WithFocus(tui.RequestPane)
	m = resize(t, m, 100, 30)
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	assertViewNotContains(t, m, "Import Postman collection")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bad.json")})
	m = executeCmdUpdate(t, m, mustImportCommand(t, m))
	assertViewContains(t, m, "Import failed: invalid JSON")
}

func mustImportCommand(t *testing.T, m tui.Model) tea.Cmd {
	t.Helper()
	_, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	return cmd
}
