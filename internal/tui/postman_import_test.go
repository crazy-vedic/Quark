package tui

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

type postmanImporterStub struct {
	path   string
	output string
	err    error
}

func (s *postmanImporterStub) ImportPostman(_ context.Context, path string) (string, error) {
	s.path = path
	return s.output, s.err
}

func TestPostmanSidebarKeyOpensImportModalAndRunsImporter(t *testing.T) {
	importer := &postmanImporterStub{output: "Imported Example"}
	m := New(Deps{PostmanImporter: importer})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = requireImportModel(t, updated)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	m = requireImportModel(t, updated)
	require.Equal(t, postmanImportMode, m.mode)
	require.True(t, m.postmanPath.Focused())
	require.Contains(t, m.View(), "Import Postman collection")

	m.postmanPath.SetValue("  collection.json  ")
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = requireImportModel(t, updated)
	require.True(t, m.postmanBusy)
	require.NotNil(t, cmd)

	updated, _ = m.Update(cmd())
	m = requireImportModel(t, updated)
	require.Equal(t, "collection.json", importer.path)
	require.False(t, m.postmanBusy)
	require.Contains(t, m.postmanOutput, "Imported Example")
	require.Contains(t, m.View(), "Imported Example")
}

func TestPostmanImportModalHandlesEmptyUnavailableFailureBusyAndCancel(t *testing.T) {
	m := New(Deps{}).openPostmanImportModal()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = requireImportModel(t, updated)
	require.Nil(t, cmd)
	require.Contains(t, m.postmanOutput, "Enter the path")

	m.postmanPath.SetValue("collection.json")
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = requireImportModel(t, updated)
	require.Nil(t, cmd)
	require.Contains(t, m.postmanOutput, "unavailable")

	importer := &postmanImporterStub{output: "could not import", err: errors.New("invalid collection")}
	m.postmanImporter = importer
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = requireImportModel(t, updated)
	require.True(t, m.postmanBusy)
	require.NotNil(t, cmd)
	updated, ignored := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = requireImportModel(t, updated)
	require.Nil(t, ignored)
	require.True(t, m.postmanBusy)

	updated, _ = m.Update(cmd())
	m = requireImportModel(t, updated)
	require.False(t, m.postmanBusy)
	require.Contains(t, m.postmanOutput, "could not import")
	require.Contains(t, m.postmanOutput, "invalid collection")

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = requireImportModel(t, updated)
	require.Equal(t, normalMode, m.mode)
	require.False(t, m.postmanPath.Focused())
}

func TestPostmanKeyIsScopedToSidebarPane(t *testing.T) {
	m := New(Deps{}).WithFocus(requestPane)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	m = requireImportModel(t, updated)
	require.Equal(t, normalMode, m.mode)
}
