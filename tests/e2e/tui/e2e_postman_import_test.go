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
	"github.com/crazy-vedic/quark/internal/tui"
)

type postmanImportFake struct {
	path   string
	output string
	err    error
}

func (f *postmanImportFake) ImportPostman(_ context.Context, path string) (string, error) {
	f.path = path
	return f.output, f.err
}

func TestE2E_PostmanImportModalFlow(t *testing.T) {
	importer := &postmanImportFake{output: "Imported Team API"}
	cfg := config.Default(t.TempDir())
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

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("collection.json")})
	m, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assertViewContains(t, m, "Importing")
	m = executeCmdUpdate(t, m, cmd)
	require.Equal(t, "collection.json", importer.path)
	assertViewContains(t, m, "Imported Team API")

	m = callUpdate(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assertViewNotContains(t, m, "Import Postman collection")
}

func TestE2E_PostmanImportFailureAndPaneScope(t *testing.T) {
	importer := &postmanImportFake{output: "bad file", err: errors.New("invalid JSON")}
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
	assertViewContains(t, m, "bad file")
	assertViewContains(t, m, "invalid JSON")
}

func mustImportCommand(t *testing.T, m tui.Model) tea.Cmd {
	t.Helper()
	_, cmd := callUpdateWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	return cmd
}
